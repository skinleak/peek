package tui

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/muesli/termenv"

	"github.com/skinleak/peek/internal/ui"
)

// browseURL is the address to open for a row: localhost when the port is
// reachable through loopback, otherwise the address it's bound to.
func browseURL(r ui.Row) string {
	host := ""
	for _, l := range r {
		if l.Address.IsLoopback() || l.Address.IsUnspecified() {
			host = "localhost"
			break
		}
		if host == "" {
			host = hostString(l.Address)
		}
	}
	return "http://" + host + ":" + strconv.Itoa(int(r[0].Port))
}

func hostString(a netip.Addr) string {
	if a.Is6() {
		return "[" + a.String() + "]"
	}
	return a.String()
}

// openURL opens url in the default browser without waiting for it.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("can't open a browser: %w", err)
	}
	go cmd.Wait() //nolint:errcheck // the browser's exit status doesn't matter
	return nil
}

// copyText puts s on the clipboard with the platform's clipboard tool, or,
// when none is available (for example over SSH), asks the terminal to do it
// with an OSC 52 escape sequence, which most modern terminals support.
func copyText(s string) error {
	for _, tool := range clipboardTools() {
		if _, err := exec.LookPath(tool[0]); err != nil {
			continue
		}
		cmd := exec.Command(tool[0], tool[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	if os.Getenv("TERM") == "dumb" {
		return errors.New("no clipboard available")
	}
	termenv.NewOutput(os.Stdout).Copy(s)
	return nil
}

// clipboardTools lists clipboard commands to try, in order of preference.
func clipboardTools() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	}
	var tools [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		tools = append(tools, []string{"wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		tools = append(tools, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return tools
}
