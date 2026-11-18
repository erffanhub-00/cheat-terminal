// Package clipboard copies text to the system clipboard using whatever
// tool the OS provides: pbcopy on macOS, clip on Windows, and wl-copy,
// xclip or xsel on Linux.
package clipboard

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// Copy puts text on the system clipboard.
func Copy(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		switch {
		case lookPath("wl-copy"):
			cmd = exec.Command("wl-copy")
		case lookPath("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard")
		case lookPath("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--input")
		default:
			return errors.New("no clipboard tool found (install wl-copy, xclip or xsel)")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
