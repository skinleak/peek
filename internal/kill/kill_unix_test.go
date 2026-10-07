//go:build unix

package kill

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// start launches a child process and reaps it in the background so that it
// does not linger as a zombie once signalled.
func start(t *testing.T, script string) Target {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sh: %v", err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-done
	})
	time.Sleep(50 * time.Millisecond) // let sh install traps
	return Target{PID: cmd.Process.Pid, Name: "sh", Ports: []uint16{3000}}
}

func TestRunTerminates(t *testing.T) {
	target := start(t, "exec sleep 30")
	var out bytes.Buffer
	err := Run([]Target{target}, Options{Yes: true, Wait: 2 * time.Second, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Stopped sh (PID") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunDeclined(t *testing.T) {
	target := start(t, "exec sleep 30")
	var out bytes.Buffer
	err := Run([]Target{target}, Options{Wait: time.Second, In: strings.NewReader("n\n"), Out: &out})
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("got %v, want ErrAborted", err)
	}
	if err := syscall.Kill(target.PID, 0); err != nil {
		t.Errorf("process is gone despite declining: %v", err)
	}
}

func TestRunIgnoredTermNeedsForce(t *testing.T) {
	// Ignored signal dispositions survive exec, so sleep ignores SIGTERM.
	target := start(t, `trap "" TERM; exec sleep 30`)
	var out bytes.Buffer

	err := Run([]Target{target}, Options{Yes: true, Wait: 200 * time.Millisecond, Out: &out})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("got %v, want a still-running error suggesting --force", err)
	}

	err = Run([]Target{target}, Options{Yes: true, Force: true, Wait: 2 * time.Second, Out: &out})
	if err != nil {
		t.Fatalf("--force: %v", err)
	}
}

func TestRunRefusesInit(t *testing.T) {
	err := Run([]Target{{PID: 1, Name: "systemd", Ports: []uint16{631}}}, Options{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("got %v, want refusal", err)
	}
}
