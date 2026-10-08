package kill

import (
	"bytes"
	"strings"
	"testing"

	"github.com/skinleak/peek/internal/scan"
)

func TestTargets(t *testing.T) {
	ls := []scan.Listener{
		{Port: 22}, // owner hidden
		{Port: 8080, PID: 200, ProcessName: "nginx"}, // IPv4
		{Port: 8080, PID: 201, ProcessName: "nginx"}, // pre-forked worker
		{Port: 8080, PID: 200, ProcessName: "nginx"}, // IPv6, same port
		{Port: 8443, PID: 200, ProcessName: "nginx"},
	}
	targets, hidden := Targets(ls)
	if len(hidden) != 1 || hidden[0].Port != 22 {
		t.Errorf("hidden = %+v, want port 22", hidden)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2: %+v", len(targets), targets)
	}
	if got := targets[0].String(); got != "nginx (PID 200) on ports 8080, 8443" {
		t.Errorf("targets[0] = %q", got)
	}
	if got := targets[1].String(); got != "nginx (PID 201) on port 8080" {
		t.Errorf("targets[1] = %q", got)
	}
}

func TestConfirm(t *testing.T) {
	one := []Target{{PID: 1234, Name: "node", Ports: []uint16{3000}}}
	tests := map[string]bool{
		"y\n": true, "Y\n": true, "yes\n": true, " YES \n": true, "y": true,
		"\n": false, "n\n": false, "no\n": false, "": false, "yep\n": false,
	}
	for input, want := range tests {
		var out bytes.Buffer
		got, err := confirm(strings.NewReader(input), &out, []string{one[0].Action(false)})
		if err != nil {
			t.Fatalf("confirm(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("confirm(%q) = %v, want %v", input, got, want)
		}
		if !strings.Contains(out.String(), "Send SIGTERM to node (PID 1234) on port 3000? [y/N]") {
			t.Errorf("unexpected prompt %q", out.String())
		}
	}
}

func TestTargetsGroupsContainers(t *testing.T) {
	ls := []scan.Listener{
		// docker-proxy for 0.0.0.0 and :: are separate processes for one container
		{Port: 5432, PID: 900, ProcessName: "docker-proxy", Container: "db", ContainerID: "abc"},
		{Port: 5432, PID: 901, ProcessName: "docker-proxy", Container: "db", ContainerID: "abc"},
		// hidden owner, but Docker told us which container it is
		{Port: 6379, Container: "cache", ContainerID: "def"},
	}
	targets, hidden := Targets(ls)
	if len(hidden) != 0 {
		t.Errorf("hidden = %+v, want none", hidden)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2: %+v", len(targets), targets)
	}
	if got := targets[0].Action(false); got != "Stop Docker container db on port 5432" {
		t.Errorf("action = %q", got)
	}
	if got := targets[1].Action(true); got != "Kill Docker container cache on port 6379" {
		t.Errorf("forced action = %q", got)
	}
}

func TestRunStopsContainer(t *testing.T) {
	target := Target{Name: "db", ContainerID: "abc", Ports: []uint16{5432}}
	var gotID string
	var gotForce bool
	var out bytes.Buffer
	err := Run([]Target{target}, Options{
		Yes:   true,
		Force: true,
		Out:   &out,
		StopContainer: func(id string, force bool) error {
			gotID, gotForce = id, force
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotID != "abc" || !gotForce {
		t.Errorf("StopContainer(%q, %v), want (abc, true)", gotID, gotForce)
	}
	if !strings.Contains(out.String(), "Stopped Docker container db on port 5432") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunContainerWithoutDocker(t *testing.T) {
	target := Target{Name: "db", ContainerID: "abc", Ports: []uint16{5432}}
	err := Run([]Target{target}, Options{Yes: true, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "docker stop db") {
		t.Fatalf("got %v, want hint to run docker stop", err)
	}
}
