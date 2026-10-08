package kill

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestZombieCountsAsExited(t *testing.T) {
	// A child that exits stays a zombie until we wait for it.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait() //nolint:errcheck
	deadline := time.Now().Add(2 * time.Second)
	for !isZombie(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("child never became a zombie")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waitExit(cmd.Process, 100*time.Millisecond) {
		t.Error("waitExit should treat a zombie as exited")
	}
	if isZombie(os.Getpid()) {
		t.Error("the test process isn't a zombie")
	}
}
