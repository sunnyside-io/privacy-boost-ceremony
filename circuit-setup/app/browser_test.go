package app

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBrowserCommandPerPlatform(t *testing.T) {
	// Arrange - capture the launcher without opening a browser.
	var gotArgs []string
	original := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotArgs = append([]string{name}, args...)
		return original(os.Args[0], "-test.run=^$")
	}
	t.Cleanup(func() { execCommand = original })

	cases := []struct {
		goos string
		want []string
	}{
		{"darwin", []string{"open", "https://example.test/device"}},
		{"windows", []string{"rundll32", "url.dll,FileProtocolHandler", "https://example.test/device"}},
		{"linux", []string{"xdg-open", "https://example.test/device"}},
	}
	for _, testCase := range cases {
		// Act - resolve the launcher for the target platform.
		gotArgs = nil
		cmd, err := browserCommand(testCase.goos, "https://example.test/device")

		// Assert - the launcher receives the URL as a distinct argument.
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", testCase.goos, err)
		}
		if cmd == nil {
			t.Fatalf("%s: nil command", testCase.goos)
		}
		if strings.Join(gotArgs, " ") != strings.Join(testCase.want, " ") {
			t.Fatalf("%s: arguments %v, want %v", testCase.goos, gotArgs, testCase.want)
		}
	}

	if _, err := browserCommand("plan9", "https://example.test/device"); err == nil {
		t.Fatal("expected an error for an unsupported platform")
	}
}
