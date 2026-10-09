package main

import (
	"bytes"
	"testing"

	"github.com/skinleak/peek/internal/scan"
)

func TestWritePorts(t *testing.T) {
	ls := []scan.Listener{
		{Port: 5432, Container: "webapp-db"},
		{Port: 3000, PID: 1, ProcessName: "node", Command: []string{"node", "node_modules/.bin/vite"}},
		{Port: 3000, PID: 1, ProcessName: "node"}, // same port on another address
		{Port: 22},
	}
	var b bytes.Buffer
	writePorts(&b, ls)
	want := "22\t-\n3000\tnode (vite)\n5432\twebapp-db (docker)\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
