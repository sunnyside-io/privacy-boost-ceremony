package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func extractShellFunction(t *testing.T, scriptPath, name string) string {
	t.Helper()
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	lines := strings.Split(string(content), "\n")
	start := -1
	for i, line := range lines {
		if line == name+"() {" {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("function %s() not found in %s", name, scriptPath)
	}
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			return strings.Join(lines[start:i+1], "\n")
		}
	}
	t.Fatalf("closing brace for %s() not found in %s", name, scriptPath)
	return ""
}

func TestResolveSignerIdentitySupportsOfficialTransition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash scripts are not supported on Windows")
	}
	repoRoot := filepath.Join("..", "..")
	cases := []struct {
		script  string
		repoVar string
	}{
		{filepath.Join(repoRoot, "circuit-setup", "contribute.sh"), "SIGNER_REPO"},
		{filepath.Join(repoRoot, "circuit-setup", "contribute_quickstart.sh"), "CEREMONY_SIGNER_REPO"},
	}

	for _, testCase := range cases {
		t.Run(filepath.Base(testCase.script), func(t *testing.T) {
			fn := extractShellFunction(t, testCase.script, "resolve_signer_identity")
			run := func(repo, regexp, tag string) string {
				t.Helper()
				script := fn + "\n" +
					testCase.repoVar + "='" + repo + "'\n" +
					"CEREMONY_SIGNER_IDENTITY_REGEXP='" + regexp + "'\n" +
					"resolve_signer_identity '" + tag + "'\n" +
					`printf '%s\n%s\n' "${SIGNER_IDENTITY_FLAG}" "${SIGNER_IDENTITY_VALUE}"` + "\n"
				out, err := exec.Command("bash", "-c", script).CombinedOutput()
				if err != nil {
					t.Fatalf("resolve_signer_identity failed: %v\n%s", err, out)
				}
				return strings.TrimRight(string(out), "\n")
			}

			got := run("", "", "ceremony/v1.2.3")
			want := "--certificate-identity-regexp\n" +
				`^https://github\.com/sunnyside-io/privacy-boost-(backend|ceremony)/\.github/workflows/ceremony-release\.yml@refs/tags/\Qceremony/v1.2.3\E$`
			if got != want {
				t.Fatalf("official transition identity:\ngot:  %q\nwant: %q", got, want)
			}
			identityRegexp, err := regexp.Compile(strings.TrimPrefix(got, "--certificate-identity-regexp\n"))
			if err != nil {
				t.Fatalf("compile official transition identity: %v", err)
			}
			for _, identity := range []string{
				"https://github.com/sunnyside-io/privacy-boost-backend/.github/workflows/ceremony-release.yml@refs/tags/ceremony/v1.2.3",
				"https://github.com/sunnyside-io/privacy-boost-ceremony/.github/workflows/ceremony-release.yml@refs/tags/ceremony/v1.2.3",
			} {
				if !identityRegexp.MatchString(identity) {
					t.Errorf("official identity did not match transition regexp: %s", identity)
				}
			}
			for _, identity := range []string{
				"https://github.com/sunnyside-io/privacy-boost-ceremony/.github/workflows/ceremony-release.yml@refs/tags/ceremony/v1.2.30",
				"https://github.com/attacker/privacy-boost-ceremony/.github/workflows/ceremony-release.yml@refs/tags/ceremony/v1.2.3",
			} {
				if identityRegexp.MatchString(identity) {
					t.Errorf("untrusted identity matched transition regexp: %s", identity)
				}
			}
			if longer := run("", "", "ceremony/v1.2.30"); longer == got {
				t.Fatalf("a longer tag produced the same identity: %q", longer)
			}

			got = run("owner/repo", "", "ceremony/v1.2.3")
			want = "--certificate-identity\n" +
				"https://github.com/owner/repo/.github/workflows/ceremony-release.yml@refs/tags/ceremony/v1.2.3"
			if got != want {
				t.Fatalf("exact signer override:\ngot:  %q\nwant: %q", got, want)
			}

			got = run("owner/repo", "^https://example.com/.*", "ceremony/v1.2.3")
			want = "--certificate-identity-regexp\n^https://example.com/.*"
			if got != want {
				t.Fatalf("regexp override:\ngot:  %q\nwant: %q", got, want)
			}
		})
	}
}
