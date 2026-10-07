package docker

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaron03EM/peek/internal/scan"
)

var addr = netip.MustParseAddr

func TestAnnotate(t *testing.T) {
	cs := []Container{
		{ID: "db1", Name: "db", Ports: []Port{{IP: addr("0.0.0.0"), Public: 5432}, {IP: addr("::"), Public: 5432}}},
		{ID: "web1", Name: "web-local", Ports: []Port{{IP: addr("127.0.0.1"), Public: 8080}}},
		{ID: "web2", Name: "web-public", Ports: []Port{{IP: addr("0.0.0.0"), Public: 8080}}},
	}
	ls := []scan.Listener{
		{Port: 5432, Address: addr("0.0.0.0"), PID: 900, ProcessName: "docker-proxy"},
		{Port: 5432, Address: addr("::"), PID: 0},                                           // hidden owner
		{Port: 8080, Address: addr("0.0.0.0"), PID: 901, ProcessName: "docker-proxy"},       // exact IP beats port-only
		{Port: 8080, Address: addr("127.0.0.1"), PID: 902, ProcessName: "docker-proxy"},     // exact IP
		{Port: 5432, Address: addr("127.0.0.1"), PID: 1234, ProcessName: "postgres"},        // not a proxy: untouched
		{Port: 9999, Address: addr("0.0.0.0"), PID: 903, ProcessName: "com.docker.backend"}, // no container
	}
	Annotate(ls, cs)

	want := []string{"db", "db", "web-public", "web-local", "", ""}
	for i, w := range want {
		if ls[i].Container != w {
			t.Errorf("listener %d (%d %v): container = %q, want %q", i, ls[i].Port, ls[i].Address, ls[i].Container, w)
		}
	}
	if ls[0].ContainerID != "db1" {
		t.Errorf("container ID = %q, want db1", ls[0].ContainerID)
	}
}

func TestNeedsLookup(t *testing.T) {
	if needsLookup([]scan.Listener{{PID: 1, ProcessName: "node"}}) {
		t.Error("plain process should not trigger a Docker lookup")
	}
	if !needsLookup([]scan.Listener{{PID: 1, ProcessName: "docker-proxy"}}) {
		t.Error("docker-proxy should trigger a lookup")
	}
	if !needsLookup([]scan.Listener{{PID: 0}}) {
		t.Error("hidden owner should trigger a lookup")
	}
}

func TestSocketPathFromDockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///custom/docker.sock")
	if got := socketPath(); got != "/custom/docker.sock" {
		t.Errorf("got %q", got)
	}
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.5:2376")
	if got := socketPath(); got != "" {
		t.Errorf("remote DOCKER_HOST: got %q, want none", got)
	}
}

const containersJSON = `[
  {"Id": "abc123", "Names": ["/myapp-db"], "Ports": [
    {"IP": "0.0.0.0", "PrivatePort": 5432, "PublicPort": 5432, "Type": "tcp"},
    {"IP": "::", "PrivatePort": 5432, "PublicPort": 5432, "Type": "tcp"},
    {"PrivatePort": 9000, "Type": "tcp"},
    {"IP": "0.0.0.0", "PrivatePort": 53, "PublicPort": 53, "Type": "udp"}
  ]},
  {"Id": "def456", "Names": [], "Ports": []}
]`

// fakeDocker serves a minimal Docker API on a unix socket and points
// DOCKER_HOST at it.
func fakeDocker(t *testing.T, handler http.Handler) {
	t.Helper()
	dir, err := os.MkdirTemp("", "peek") // short path: unix sockets have a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	t.Setenv("DOCKER_HOST", "unix://"+sock)
}

func TestClientContainers(t *testing.T) {
	fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(containersJSON))
	}))
	c := Connect()
	if c == nil {
		t.Fatal("Connect returned nil")
	}
	cs, err := c.Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d containers, want 2", len(cs))
	}
	if cs[0].Name != "myapp-db" || cs[0].ID != "abc123" {
		t.Errorf("container 0 = %+v", cs[0])
	}
	// Only published TCP ports are kept.
	if len(cs[0].Ports) != 2 || cs[0].Ports[0].Public != 5432 || cs[0].Ports[1].IP != addr("::") {
		t.Errorf("ports = %+v", cs[0].Ports)
	}
	if cs[1].Name != "def456" {
		t.Errorf("unnamed container should fall back to its ID, got %q", cs[1].Name)
	}
}

func TestClientStop(t *testing.T) {
	var paths []string
	fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/containers/abc/stop":
			w.WriteHeader(http.StatusNoContent)
		case "/containers/abc/kill":
			w.WriteHeader(http.StatusNotModified) // already stopped counts as success
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "No such container: missing"}`))
		}
	}))
	c := Connect()
	ctx := context.Background()
	if err := c.Stop(ctx, "abc", false); err != nil {
		t.Errorf("stop: %v", err)
	}
	if err := c.Stop(ctx, "abc", true); err != nil {
		t.Errorf("kill: %v", err)
	}
	err := c.Stop(ctx, "missing", false)
	if err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("missing container: got %v", err)
	}
	want := "POST /containers/abc/stop,POST /containers/abc/kill,POST /containers/missing/stop"
	if got := strings.Join(paths, ","); got != want {
		t.Errorf("requests = %s\nwant       %s", got, want)
	}
}

func TestEnrichWithoutDocker(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock")
	ls := []scan.Listener{{Port: 5432, PID: 900, ProcessName: "docker-proxy"}}
	if c := Enrich(ls); c != nil {
		t.Error("expected nil client when Docker is unreachable")
	}
	if ls[0].Container != "" {
		t.Error("listener should be untouched")
	}
}
