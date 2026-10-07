package kill

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aaron03EM/peek/internal/scan"
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
		got, err := confirm(strings.NewReader(input), &out, one, "SIGTERM")
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
