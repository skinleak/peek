// Package kill terminates the processes that own listening sockets.
package kill

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/skinleak/peek/internal/scan"
)

// ErrAborted is returned when the user declines the confirmation prompt.
var ErrAborted = errors.New("aborted")

// Target is a process to be signalled, or a Docker container to be stopped,
// with the ports it listens on.
type Target struct {
	PID         int
	Name        string // process or container name
	User        string
	ContainerID string // set when the ports are published by a container
	Ports       []uint16
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
	if t.ContainerID != "" {
		return fmt.Sprintf("Docker container %s on %s %s", t.Name, noun, strings.Join(ports, ", "))
	}
	return fmt.Sprintf("%s (PID %d) on %s %s", t.Name, t.PID, noun, strings.Join(ports, ", "))
}

// Action describes what Run will do to t, e.g. "Send SIGTERM to node (PID 42) on port 3000".
func (t Target) Action(force bool) string {
	switch {
	case t.ContainerID != "" && force:
		return "Kill " + t.String()
	case t.ContainerID != "":
		return "Stop " + t.String()
	case force:
		return "Send SIGKILL to " + t.String()
	default:
		return "Send SIGTERM to " + t.String()
	}
}

// Targets groups listeners by owning container or process. Ports whose owner
// could not be determined are returned separately in hidden.
func Targets(ls []scan.Listener) (targets []Target, hidden []scan.Listener) {
	index := make(map[string]int)
	for _, l := range ls {
		var key string
		var t Target
		switch {
		case l.ContainerID != "":
			key = "container:" + l.ContainerID
			t = Target{Name: l.Container, ContainerID: l.ContainerID, User: l.User}
		case l.PID != 0:
			key = fmt.Sprint("pid:", l.PID)
			t = Target{PID: l.PID, Name: l.ProcessName, User: l.User}
		default:
			hidden = append(hidden, l)
			continue
		}
		i, ok := index[key]
		if !ok {
			i = len(targets)
			index[key] = i
			targets = append(targets, t)
		}
		if !slices.Contains(targets[i].Ports, l.Port) {
			targets[i].Ports = append(targets[i].Ports, l.Port)
		}
	}
	return targets, hidden
}

// Options configure Run.
type Options struct {
	Force bool          // SIGKILL / docker kill instead of SIGTERM / docker stop
	Yes   bool          // skip the confirmation prompt
	Wait  time.Duration // how long to wait for processes to exit
	In    io.Reader     // where confirmation answers are read from
	Out   io.Writer     // where prompts and progress are written

	// ForceHint tells the user how to retry with SIGKILL when a process
	// ignores SIGTERM. It defaults to the CLI's "use --force to send SIGKILL".
	ForceHint string

	// StopContainer stops a Docker container; nil if Docker is unavailable.
	StopContainer func(id string, force bool) error
}

// Run confirms with the user and then stops every target, waiting up to
// o.Wait for each process to exit. It returns an error if any target could
// not be stopped.
func Run(targets []Target, o Options) error {
	actions := make([]string, len(targets))
	for i, t := range targets {
		if t.PID == 1 && t.ContainerID == "" {
			return fmt.Errorf("refusing to signal PID 1 (%s), which holds port %d; stop the service or socket unit instead", t.Name, t.Ports[0])
		}
		actions[i] = t.Action(o.Force)
	}

	if !o.Yes {
		ok, err := confirm(o.In, o.Out, actions)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAborted
		}
	}

	sig := syscall.SIGTERM
	if o.Force {
		sig = syscall.SIGKILL
	}
	var failed []error
	for _, t := range targets {
		var err error
		if t.ContainerID != "" {
			err = stopContainer(t, o)
		} else {
			err = stop(t, sig, o)
		}
		if err != nil {
			failed = append(failed, err)
			continue
		}
		fmt.Fprintf(o.Out, "Stopped %s\n", t)
	}
	return errors.Join(failed...)
}

func confirm(in io.Reader, out io.Writer, actions []string) (bool, error) {
	if len(actions) == 1 {
		fmt.Fprintf(out, "%s? [y/N] ", actions[0])
	} else {
		fmt.Fprintln(out, "peek will:")
		for _, a := range actions {
			fmt.Fprintf(out, "  %s\n", a)
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

func stopContainer(t Target, o Options) error {
	if o.StopContainer == nil {
		return fmt.Errorf("can't reach Docker to stop %s; try 'docker stop %s'", t, t.Name)
	}
	if err := o.StopContainer(t.ContainerID, o.Force); err != nil {
		return fmt.Errorf("stopping %s: %w", t, err)
	}
	return nil
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
			hint = "; " + cmp.Or(o.ForceHint, "use --force to send SIGKILL")
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
