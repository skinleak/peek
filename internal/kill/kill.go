// Package kill terminates the processes that own listening sockets.
package kill

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"peek/internal/scan"
)

// ErrAborted is returned when the user declines the confirmation prompt.
var ErrAborted = errors.New("aborted")

// Target is a process to be signalled, with the ports it listens on.
type Target struct {
	PID   int
	Name  string
	User  string
	Ports []uint16
}

func (t Target) String() string {
	ports := make([]string, len(t.Ports))
	for i, p := range t.Ports {
		ports[i] = fmt.Sprint(p)
	}
	noun := "port"
	if len(ports) > 1 {
		noun = "ports"
	}
	return fmt.Sprintf("%s (PID %d) on %s %s", t.Name, t.PID, noun, strings.Join(ports, ", "))
}

// Targets groups listeners by owning process. Ports whose owner could not be
// determined are returned separately in hidden.
func Targets(ls []scan.Listener) (targets []Target, hidden []scan.Listener) {
	index := make(map[int]int)
	for _, l := range ls {
		if l.PID == 0 {
			hidden = append(hidden, l)
			continue
		}
		i, ok := index[l.PID]
		if !ok {
			i = len(targets)
			index[l.PID] = i
			targets = append(targets, Target{PID: l.PID, Name: l.ProcessName, User: l.User})
		}
		if !slices.Contains(targets[i].Ports, l.Port) {
			targets[i].Ports = append(targets[i].Ports, l.Port)
		}
	}
	return targets, hidden
}

// Options configure Run.
type Options struct {
	Force bool          // send SIGKILL instead of SIGTERM
	Yes   bool          // skip the confirmation prompt
	Wait  time.Duration // how long to wait for processes to exit
	In    io.Reader     // where confirmation answers are read from
	Out   io.Writer     // where prompts and progress are written
}

// Run confirms with the user and then signals every target, waiting up to
// o.Wait for each to exit. It returns an error if any target could not be
// signalled or is still running afterwards.
func Run(targets []Target, o Options) error {
	for _, t := range targets {
		if t.PID == 1 {
			return fmt.Errorf("refusing to signal PID 1 (%s), which holds port %d; stop the service or socket unit instead", t.Name, t.Ports[0])
		}
	}
	sig, sigName := syscall.SIGTERM, "SIGTERM"
	if o.Force {
		sig, sigName = syscall.SIGKILL, "SIGKILL"
	}

	if !o.Yes {
		ok, err := confirm(o.In, o.Out, targets, sigName)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAborted
		}
	}

	var failed []error
	for _, t := range targets {
		if err := stop(t, sig, o); err != nil {
			failed = append(failed, err)
			continue
		}
		fmt.Fprintf(o.Out, "Stopped %s\n", t)
	}
	return errors.Join(failed...)
}

func confirm(in io.Reader, out io.Writer, targets []Target, sigName string) (bool, error) {
	if len(targets) == 1 {
		fmt.Fprintf(out, "Send %s to %s? [y/N] ", sigName, targets[0])
	} else {
		fmt.Fprintf(out, "Send %s to %d processes:\n", sigName, len(targets))
		for _, t := range targets {
			fmt.Fprintf(out, "  %s\n", t)
		}
		fmt.Fprint(out, "Continue? [y/N] ")
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	if errors.Is(err, io.EOF) && line == "" {
		fmt.Fprintln(out) // keep the shell prompt off the question line
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func stop(t Target, sig syscall.Signal, o Options) error {
	p, err := os.FindProcess(t.PID)
	if err != nil {
		return fmt.Errorf("finding %s: %w", t, err)
	}
	if err := p.Signal(sig); err != nil {
		switch {
		case errors.Is(err, os.ErrProcessDone):
			return nil
		case errors.Is(err, syscall.EPERM):
			return fmt.Errorf("permission denied signalling %s (owned by %s); try again with sudo", t, t.User)
		default:
			return fmt.Errorf("signalling %s: %w", t, err)
		}
	}
	if !waitExit(p, o.Wait) {
		hint := ""
		if sig != syscall.SIGKILL {
			hint = "; use --force to send SIGKILL"
		}
		return fmt.Errorf("%s is still running after %s%s", t, o.Wait, hint)
	}
	return nil
}

// waitExit polls until p has exited or timeout elapses.
func waitExit(p *os.Process, timeout time.Duration) bool {
	const interval = 25 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return true // ErrProcessDone, or ESRCH on older kernels
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}
