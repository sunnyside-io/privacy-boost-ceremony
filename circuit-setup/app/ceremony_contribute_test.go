package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/model"
)

func TestParseContributeOptionsUsesConfigCircuitsAndCompatibilityFlags(t *testing.T) {
	configStateDir := filepath.Join(t.TempDir(), "config-state")
	configPath := filepath.Join(t.TempDir(), "ceremony.config.json")
	writeJSONFile(t, configPath, model.CeremonyConfig{
		ID:         "config-selected-circuits",
		AccessMode: model.AccessPublic,
		StateDir:   configStateDir,
		Phase1: model.Phase1Spec{
			SourceURL: "https://example.com/phase1-{power}.ptau",
			ExpectedSHA256ByPower: map[string]string{
				"1": "phase1-sha",
			},
		},
		GitHubAuth: model.GitHubAuthSpec{Enabled: true, ClientID: "client-id"},
		Circuits: []model.CircuitSpec{
			{ID: "custom-a", Name: "custom-a", Type: model.CircuitTypeEpoch, Depth: 4},
			{ID: "custom-b", Name: "custom-b", Type: model.CircuitTypeForced, Depth: 4},
		},
	})

	originalPin := releaseConfigSHA256
	releaseConfigSHA256 = ""
	t.Cleanup(func() { releaseConfigSHA256 = originalPin })

	overrideStateDir := filepath.Join(t.TempDir(), "override-state")
	opts, err := parseContributeOptions([]string{
		"--config", configPath,
		"--coordinator-url", "https://coordinator.example",
		"--state-dir", overrideStateDir,
		"--quiet",
		"--no-browser",
	})
	if err != nil {
		t.Fatalf("parseContributeOptions returned error: %v", err)
	}

	if opts.coordinatorURL != "https://coordinator.example" {
		t.Fatalf("expected coordinator URL from flags, got %q", opts.coordinatorURL)
	}
	if !opts.quiet || !opts.noBrowser {
		t.Fatalf("expected compatibility flags to be preserved, got quiet=%t noBrowser=%t", opts.quiet, opts.noBrowser)
	}
	if opts.cfg.StateDir != overrideStateDir {
		t.Fatalf("expected state override %q, got %q", overrideStateDir, opts.cfg.StateDir)
	}
	if len(opts.cfg.Circuits) != 2 || opts.cfg.Circuits[0].ID != "custom-a" || opts.cfg.Circuits[1].ID != "custom-b" {
		t.Fatalf("expected circuits from the supplied config, got %+v", opts.cfg.Circuits)
	}
}

func TestRunAuthFlowOpenLoginControlsBrowserLaunch(t *testing.T) {
	// Arrange - serve a complete device flow and replace the platform launcher.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/github/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-code",
				"user_code":        "USER",
				"verification_uri": "https://example.test/device",
				"expires_in":       30,
				"interval":         1,
			})
		case "/v1/auth/github/complete":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessionToken":  "session-token",
				"participantId": "alice",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var launches int
	original := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		launches++
		return original(os.Args[0], "-test.run=^$")
	}
	t.Cleanup(func() { execCommand = original })
	v := verbosity{quiet: true}

	// Act - run once with browser opening disabled and once with it enabled.
	if _, _, err := runAuthFlow(v, srv.URL, false); err != nil {
		t.Fatalf("run auth flow without browser: %v", err)
	}
	if launches != 0 {
		t.Fatalf("disabled browser launch ran %d times", launches)
	}
	if _, _, err := runAuthFlow(v, srv.URL, true); err != nil {
		t.Fatalf("run auth flow with browser: %v", err)
	}

	// Assert - only the enabled flow invokes the platform launcher.
	if launches != 1 {
		t.Fatalf("enabled browser launch ran %d times, want 1", launches)
	}
}

func TestDownloadInputArtifactSendsQueryAndHeaderAuth(t *testing.T) {
	const token = "session-token-123"
	const payload = "phase2-input"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sessionToken"); got != token {
			t.Fatalf("expected sessionToken query param %q, got %q", token, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("expected Authorization header %q, got %q", "Bearer "+token, got)
		}
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "input.ph2")
	err := downloadInputArtifact(
		srv.URL,
		"/v1/contribute/input/s1/s1-lease",
		token,
		dst,
		verbosity{quiet: true},
	)
	if err != nil {
		t.Fatalf("downloadInputArtifact returned error: %v", err)
	}

	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read downloaded artifact: %v", err)
	}
	if string(b) != payload {
		t.Fatalf("expected payload %q, got %q", payload, string(b))
	}
}

// TestWriteContributionReceiptKeepsLocalArtifactMetadata verifies that the
// contributor receipt now preserves the local artifact audit trail by default.
//
// The receipt is the contributor's durable record after a successful run. This
// test ensures the saved JSON includes both the original transcript fields and
// the richer local metadata such as file paths, digests, and byte sizes.
func TestWriteContributionReceiptKeepsLocalArtifactMetadata(t *testing.T) {
	stateDir := t.TempDir()
	receiptPath := filepath.Join(stateDir, "contribution-receipt.json")

	// Seed one contributed circuit result with representative local artifact
	// metadata so the test exercises the enriched receipt shape.
	results := []circuitResult{{
		id:                "s1",
		status:            "contributed",
		hashHex:           "hash-1",
		createdAt:         "2026-01-01T00:00:00Z",
		leaseID:           "lease-1",
		inputDownloadPath: "/v1/contribute/input/s1/lease-1",
		inputPath:         filepath.Join(stateDir, "input.ph2"),
		outputPath:        filepath.Join(stateDir, "output.ph2"),
		inputSHA256:       "input-sha",
		outputSHA256:      "output-sha",
		inputBytes:        11,
		outputBytes:       22,
	}}

	// Write the receipt and decode it through the public JSON surface so the
	// assertions check the serialized format contributors actually keep.
	if err := writeContributionReceipt(
		receiptPath,
		"alice",
		"https://coordinator.example",
		stateDir,
		results,
	); err != nil {
		t.Fatalf("writeContributionReceipt returned error: %v", err)
	}

	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}

	var receipt struct {
		Participant    string `json:"participant"`
		CoordinatorURL string `json:"coordinatorUrl"`
		StateDir       string `json:"stateDir"`
		GeneratedAt    string `json:"generatedAt"`
		Circuits       []struct {
			CircuitID         string `json:"circuitId"`
			Status            string `json:"status"`
			Hash              string `json:"hash"`
			CreatedAt         string `json:"createdAt"`
			LeaseID           string `json:"leaseId"`
			InputDownloadPath string `json:"inputDownloadPath"`
			InputPath         string `json:"inputPath"`
			InputSHA256       string `json:"inputSha256"`
			InputBytes        int64  `json:"inputBytes"`
			OutputPath        string `json:"outputPath"`
			OutputSHA256      string `json:"outputSha256"`
			OutputBytes       int64  `json:"outputBytes"`
		} `json:"circuits"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatalf("decode receipt json: %v", err)
	}

	if receipt.Participant != "alice" {
		t.Fatalf("expected participant alice, got %q", receipt.Participant)
	}
	if receipt.CoordinatorURL != "https://coordinator.example" {
		t.Fatalf("expected coordinator url preserved, got %q", receipt.CoordinatorURL)
	}
	if receipt.StateDir != stateDir {
		t.Fatalf("expected state dir %q, got %q", stateDir, receipt.StateDir)
	}
	if receipt.GeneratedAt == "" {
		t.Fatal("expected generatedAt to be populated")
	}
	if len(receipt.Circuits) != 1 {
		t.Fatalf("expected one circuit entry, got %d", len(receipt.Circuits))
	}

	entry := receipt.Circuits[0]
	if entry.CircuitID != "s1" || entry.LeaseID != "lease-1" {
		t.Fatalf("expected circuit metadata preserved, got %+v", entry)
	}
	if entry.InputSHA256 != "input-sha" || entry.OutputSHA256 != "output-sha" {
		t.Fatalf("expected artifact digests preserved, got %+v", entry)
	}
	if entry.InputBytes != 11 || entry.OutputBytes != 22 {
		t.Fatalf("expected artifact sizes preserved, got %+v", entry)
	}
}

// TestCleanupContributionArtifactsRemovesLocalFiles verifies that declining the
// receipt can clean up local input/output artifacts after a successful run.
//
// The contributor CLI now ties artifact retention to the same prompt that asks
// whether to save the richer receipt. This test protects the cleanup path that
// runs when the user answers "no".
func TestCleanupContributionArtifactsRemovesLocalFiles(t *testing.T) {
	stateDir := t.TempDir()
	inputPath := filepath.Join(stateDir, "input.ph2")
	outputPath := filepath.Join(stateDir, "output.ph2")

	// Materialize representative contribution artifacts so the helper can remove
	// the exact file set recorded in the circuit result.
	for _, path := range []string{inputPath, outputPath} {
		if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
			t.Fatalf("write test file %s: %v", path, err)
		}
	}

	// Run cleanup through the helper and then assert that both local files are gone.
	if err := cleanupContributionArtifacts([]circuitResult{{
		inputPath:  inputPath,
		outputPath: outputPath,
	}}); err != nil {
		t.Fatalf("cleanupContributionArtifacts returned error: %v", err)
	}

	for _, path := range []string{inputPath, outputPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got err=%v", path, err)
		}
	}
}

func TestPostJSONReturnsStructuredRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":     "contribution claim is temporarily unavailable",
			"code":      "claim_busy",
			"retryable": true,
		})
	}))
	defer srv.Close()

	err := postJSON(srv.URL, map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected structured api error")
	}

	apiErr, ok := err.(*coordinatorAPIError)
	if !ok {
		t.Fatalf("expected coordinatorAPIError, got %T", err)
	}
	if apiErr.Code != "claim_busy" {
		t.Fatalf("expected claim_busy code, got %q", apiErr.Code)
	}
	if !apiErr.Retryable {
		t.Fatal("expected retryable api error")
	}
	if !isRetryableClaimErr(err) {
		t.Fatal("expected claim_busy api error to be retryable")
	}
}
