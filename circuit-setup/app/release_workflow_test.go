package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCeremonyReleaseWorkflowPreservesTagAndTrustChain(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "ceremony-release.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(raw)

	required := []string{
		"- 'ceremony/v*'",
		`TAG="ceremony/v${VERSION}"`,
		`"${GITHUB_REF}" != "refs/tags/${TAG}"`,
		"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/app",
		"releaseVersion=",
		"releaseConfigSHA256=",
		"version --expect",
		"id-token: write",
		"cosign sign-blob --yes",
		"cosign verify-blob",
		"${{ github.repository }}/.github/workflows/ceremony-release.yml@${{ github.ref }}",
		"production.ceremony.config.json.cosign.bundle",
		"contribute.sh.cosign.bundle",
		"${{ github.repository }}/.github/workflows/ceremony-release.yml@refs/tags/${{ needs.validate.outputs.tag }}",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing trust invariant %q", want)
		}
	}

	forbidden := []string{
		"      - 'v*'",
		`TAG="v${VERSION}"`,
		"uses: actions/checkout@v",
		"uses: actions/setup-go@v",
		"uses: actions/upload-artifact@v",
		"uses: actions/download-artifact@v",
		"uses: softprops/action-gh-release@v",
	}
	for _, unwanted := range forbidden {
		if strings.Contains(workflow, unwanted) {
			t.Errorf("release workflow contains unsafe or tag-changing form %q", unwanted)
		}
	}
}
