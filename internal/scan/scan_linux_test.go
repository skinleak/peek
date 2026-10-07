//go:build linux

package scan

import (
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fakeProc builds a minimal procfs tree in a temp dir from the testdata fixtures.
//
//	pid 100 "node"   holds 127.0.0.1:3000 (11111) on two fds, plus a non-socket fd
//	pid 200 "nginx"  holds 0.0.0.0:8080 (22222) and [::]:8080 (44444)
//	pid 201 "nginx"  pre-forked worker sharing 0.0.0.0:8080 (22222)
//	pid 300          has no fd directory (as if permission was denied)
//	nobody visibly owns [::1]:22 (55555) or 127.0.0.1:5001 (66666)
func fakeProc(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyFixture(t, "tcp", filepath.Join(root, "net", "tcp"))
	copyFixture(t, "tcp6", filepath.Join(root, "net", "tcp6"))
	copyFixture(t, "stat", filepath.Join(root, "stat"))

	addProc(t, root, 100, "node", "/home/dev/app", map[int]string{
		3: "socket:[11111]", 4: "/dev/null", 7: "socket:[11111]",
	})
	addProc(t, root, 200, "nginx", "/", map[int]string{
		6: "socket:[22222]", 7: "socket:[44444]", 8: "socket:[99999]",
	})
	addProc(t, root, 201, "nginx", "/", map[int]string{6: "socket:[22222]"})
	mustMkdir(t, filepath.Join(root, "300"))
	mustMkdir(t, filepath.Join(root, "self")) // non-numeric entries are ignored
	return root
}

func addProc(t *testing.T, root string, pid int, comm, cwd string, fds map[int]string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	mustMkdir(t, filepath.Join(dir, "fd"))
	mustWrite(t, filepath.Join(dir, "comm"), comm+"\n")
	mustWrite(t, filepath.Join(dir, "stat"), strconv.Itoa(pid)+" ("+comm+") S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 360000 0 0")
	if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
		t.Fatal(err)
	}
	for fd, target := range fds {
		if err := os.Symlink(target, filepath.Join(dir, "fd", strconv.Itoa(fd))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProcScanner(t *testing.T) {
	s := &procScanner{
		root:       fakeProc(t),
		order:      binary.LittleEndian,
		lookupUser: func(uid int) string { return map[int]string{0: "root", 1000: "dev"}[uid] },
	}
	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}

	started := time.Unix(1759831200+3600, 0)
	addr := netip.MustParseAddr
	want := []Listener{
		{Port: 22, Protocol: "tcp6", Address: addr("::1"), User: "root"},
		{Port: 3000, Protocol: "tcp", Address: addr("127.0.0.1"), User: "dev", PID: 100, ProcessName: "node", Cwd: "/home/dev/app", StartTime: started},
		{Port: 5001, Protocol: "tcp6", Address: addr("127.0.0.1"), User: "dev"},
		{Port: 8080, Protocol: "tcp", Address: addr("0.0.0.0"), User: "root", PID: 200, ProcessName: "nginx", Cwd: "/", StartTime: started},
		{Port: 8080, Protocol: "tcp", Address: addr("0.0.0.0"), User: "root", PID: 201, ProcessName: "nginx", Cwd: "/", StartTime: started},
		{Port: 8080, Protocol: "tcp6", Address: addr("::"), User: "root", PID: 200, ProcessName: "nginx", Cwd: "/", StartTime: started},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d listeners, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if !equalListener(got[i], want[i]) {
			t.Errorf("listener %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func TestProcScannerMissingTCP6(t *testing.T) {
	root := fakeProc(t)
	if err := os.Remove(filepath.Join(root, "net", "tcp6")); err != nil {
		t.Fatal(err)
	}
	s := &procScanner{root: root, order: binary.LittleEndian, lookupUser: strconv.Itoa}
	got, err := s.Scan()
	if err != nil {
		t.Fatalf("missing tcp6 should not be an error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d listeners, want 3 (IPv4 only)", len(got))
	}
}

func TestProcScannerMissingTCP(t *testing.T) {
	root := fakeProc(t)
	if err := os.Remove(filepath.Join(root, "net", "tcp")); err != nil {
		t.Fatal(err)
	}
	s := &procScanner{root: root, order: binary.LittleEndian, lookupUser: strconv.Itoa}
	if _, err := s.Scan(); err == nil {
		t.Fatal("expected error when /proc/net/tcp is missing")
	}
}

func equalListener(a, b Listener) bool {
	return a.Port == b.Port && a.Protocol == b.Protocol && a.Address == b.Address &&
		a.PID == b.PID && a.ProcessName == b.ProcessName && a.Cwd == b.Cwd &&
		a.User == b.User && a.StartTime.Equal(b.StartTime)
}

func copyFixture(t *testing.T, name, dst string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Dir(dst))
	mustWrite(t, dst, string(b))
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
