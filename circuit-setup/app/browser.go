package app

import (
	"fmt"
	"os/exec"
	"runtime"
)

// execCommand is indirected so tests can inspect browser launcher arguments.
var execCommand = exec.Command

// openBrowser makes a best-effort attempt to open the device-flow URL.
func openBrowser(url string) error {
	cmd, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func browserCommand(goos, url string) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		return execCommand("open", url), nil
	case "windows":
		return execCommand("rundll32", "url.dll,FileProtocolHandler", url), nil
	case "linux", "freebsd", "openbsd", "netbsd":
		return execCommand("xdg-open", url), nil
	default:
		return nil, fmt.Errorf("no known browser launcher for %s", goos)
	}
}
