//go:build linux || darwin

package scan

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestScanFindsOwnListener checks the real scanner against the live system:
// it opens listeners in this process and expects to find them with full
// process details. On macOS this is what validates the libproc struct offsets.
func TestScanFindsOwnListener(t *testing.T) {
	addrs := []string{"127.0.0.1:0"}
	if ln, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		ln.Close()
		addrs = append(addrs, "[::1]:0")
	}

	for _, a := range addrs {
		t.Run(a, func(t *testing.T) {
			ln, err := net.Listen("tcp", a)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			ap := netip.MustParseAddrPort(ln.Addr().String())

			ls, err := New().Scan()
			if err != nil {
				t.Fatal(err)
			}
			var found *Listener
			for i, l := range ls {
				if l.Port == ap.Port() && l.PID == os.Getpid() {
					found = &ls[i]
				}
			}
			if found == nil {
				t.Fatalf("listener on %v (pid %d) not found among %d listeners", ap, os.Getpid(), len(ls))
			}

			if found.Address != ap.Addr() {
				t.Errorf("address = %v, want %v", found.Address, ap.Addr())
			}
			if found.ProcessName == "" {
				t.Error("process name is empty")
			}
			if found.User == "" {
				t.Error("user is empty")
			}
			if want := realPath(t, mustGetwd(t)); realPath(t, found.Cwd) != want {
				t.Errorf("cwd = %q, want %q", found.Cwd, want)
			}
			if age := time.Since(found.StartTime); age < 0 || age > time.Hour {
				t.Errorf("start time %v is not within the last hour", found.StartTime)
			}
		})
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}
