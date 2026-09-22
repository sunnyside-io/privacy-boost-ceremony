package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyReleaseConfigPin(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ceremony.config.json")
	if err := os.WriteFile(configPath, []byte(`{"id":"prod-ceremony-test"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	sha, err := sha256FileHex(configPath)
	if err != nil {
		t.Fatalf("hash config: %v", err)
	}

	originalPin := releaseConfigSHA256
	t.Cleanup(func() { releaseConfigSHA256 = originalPin })

	releaseConfigSHA256 = ""
	if err := verifyReleaseConfigPin(configPath); err != nil {
		t.Fatalf("unstamped build should skip the pin, got %v", err)
	}

	releaseConfigSHA256 = sha
	if err := verifyReleaseConfigPin(configPath); err != nil {
		t.Fatalf("matching config should pass the pin, got %v", err)
	}

	releaseConfigSHA256 = strings.Repeat("0", 64)
	if err := verifyReleaseConfigPin(configPath); err == nil {
		t.Fatal("mismatched config should fail the pin")
	}
}

func TestVerifyReleaseVersionPin(t *testing.T) {
	originalVersion := releaseVersion
	t.Cleanup(func() { releaseVersion = originalVersion })

	releaseVersion = "dev"
	if err := verifyReleaseVersionPin("ceremony/v1.2.3"); err == nil {
		t.Fatal("unstamped build should not satisfy a release expectation")
	}

	releaseVersion = "1.2.3"
	for _, tag := range []string{"ceremony/v1.2.3", "v1.2.3", "1.2.3"} {
		if err := verifyReleaseVersionPin(tag); err != nil {
			t.Fatalf("matching tag %q should pass the pin, got %v", tag, err)
		}
	}
	if err := verifyReleaseVersionPin("ceremony/v9.9.9"); err == nil {
		t.Fatal("mismatched tag should fail")
	}
}

func TestRunCeremonyCLIVersionExpect(t *testing.T) {
	originalVersion, originalPin := releaseVersion, releaseConfigSHA256
	t.Cleanup(func() {
		releaseVersion = originalVersion
		releaseConfigSHA256 = originalPin
	})
	releaseVersion = "1.2.3"
	releaseConfigSHA256 = "abc123"

	if err := RunCeremonyCLI([]string{"version", "--expect", "ceremony/v1.2.3"}); err != nil {
		t.Fatalf("matching --expect should pass, got %v", err)
	}
	if err := RunCeremonyCLI([]string{"version", "--expect", "ceremony/v9.9.9"}); err == nil {
		t.Fatal("mismatched --expect should fail")
	}
}
