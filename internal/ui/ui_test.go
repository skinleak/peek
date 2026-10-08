package ui

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/muesli/termenv"

	"github.com/skinleak/peek/internal/scan"
)

func TestTildify(t *testing.T) {
	tests := []struct{ path, home, want string }{
		{"/home/dev", "/home/dev", "~"},
		{"/home/dev/app", "/home/dev", "~/app"},
		{"/home/developer/app", "/home/dev", "/home/developer/app"},
		{"/srv/app", "/home/dev", "/srv/app"},
		{"/srv/app", "", "/srv/app"},
		{"/srv/app", "/", "/srv/app"},
	}
	for _, tt := range tests {
		if got := tildify(tt.path, tt.home); got != tt.want {
			t.Errorf("tildify(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{42 * time.Second, "42s"},
		{7*time.Minute + 30*time.Second, "7m"},
		{3 * time.Hour, "3h"},
		{3*time.Hour + 12*time.Minute, "3h12m"},
		{24 * time.Hour, "1d"},
		{5*24*time.Hour + 4*time.Hour + 59*time.Minute, "5d4h"},
	}
	for _, tt := range tests {
		if got := formatUptime(tt.d); got != tt.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTruncateLeft(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  string
	}{
		{"~/app", 10, "~/app"},
		{"~/code/project/api", 10, "…oject/api"},
		{"abc", 1, "…"},
	}
	for _, tt := range tests {
		if got := truncateLeft(tt.s, tt.width); got != tt.want {
			t.Errorf("truncateLeft(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func sample() []scan.Listener {
	return []scan.Listener{
		{Port: 22, Protocol: "tcp6", Address: netip.MustParseAddr("::"), User: "root"},
		{
			Port: 3000, Protocol: "tcp", Address: netip.MustParseAddr("127.0.0.1"),
			PID: 4242, ProcessName: "node", Cwd: "/home/dev/code/web", User: "dev",
			StartTime: now.Add(-(2*time.Hour + 5*time.Minute)),
		},
	}
}

func TestTablePlain(t *testing.T) {
	var buf bytes.Buffer
	err := Table(&buf, sample(), Options{Home: "/home/dev", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := "" +
		"PORT  PROCESS   PID  ADDRESS    CWD         UPTIME\n" +
		"  22  -           -  ::         -           -\n" +
		"3000  node     4242  127.0.0.1  ~/code/web  2h5m\n"
	if got := buf.String(); got != want {
		t.Errorf("table mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableNoANSIWhenColorDisabled(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, sample(), Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("output contains ANSI escapes:\n%q", buf.String())
	}
}

func TestTableTruncatesCwdToWidth(t *testing.T) {
	ls := sample()
	ls[1].Cwd = "/srv/some/really/deeply/nested/project/directory"
	var buf bytes.Buffer
	if err := Table(&buf, ls, Options{Now: now, Width: 60}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if w := len([]rune(line)); w > 60 {
			t.Errorf("line is %d wide, want <= 60: %q", w, line)
		}
	}
	if !strings.Contains(buf.String(), "…") || !strings.Contains(buf.String(), "directory") {
		t.Errorf("expected left-truncated cwd keeping its tail:\n%s", buf.String())
	}
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("empty JSON = %q, want []", got)
	}

	buf.Reset()
	if err := JSON(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`"port": 3000`, `"address": "127.0.0.1"`, `"pid": 4242`, `"process": "node"`, `"start_time": "2026-10-07T09:55:00Z"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %s:\n%s", want, out)
		}
	}
	// Unknown fields are omitted rather than reported as zero values.
	first := out[:strings.Index(out, "},")]
	for _, absent := range []string{`"pid"`, `"process"`, `"cwd"`, `"start_time"`} {
		if strings.Contains(first, absent) {
			t.Errorf("first entry should omit %s:\n%s", absent, first)
		}
	}
}

func TestTableMergesAddressesOfOneOwner(t *testing.T) {
	addr := netip.MustParseAddr
	ls := []scan.Listener{
		{Port: 53, Address: addr("127.0.0.53"), User: "systemd-resolve"},
		{Port: 53, Address: addr("127.0.0.54"), User: "systemd-resolve"},
		{Port: 6379, Address: addr("127.0.0.1"), PID: 900, ProcessName: "redis-server"},
		{Port: 6379, Address: addr("::1"), PID: 900, ProcessName: "redis-server"},
		{Port: 8080, Address: addr("0.0.0.0"), PID: 200, ProcessName: "nginx"},
		{Port: 8080, Address: addr("0.0.0.0"), PID: 201, ProcessName: "nginx"},
	}
	var buf bytes.Buffer
	if err := Table(&buf, ls, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"PORT  PROCESS       PID  ADDRESS                CWD  UPTIME\n" +
		"  53  -               -  127.0.0.53,127.0.0.54  -    -\n" +
		"6379  redis-server  900  127.0.0.1,::1          -    -\n" +
		"8080  nginx         200  0.0.0.0                -    -\n" +
		"8080  nginx         201  0.0.0.0                -    -\n"
	if got := buf.String(); got != want {
		t.Errorf("table mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableDescribesInterpreters(t *testing.T) {
	ls := sample()
	ls[1].Command = []string{"node", "/home/dev/code/web/node_modules/.bin/vite", "--port", "3000"}
	var buf bytes.Buffer
	if err := Table(&buf, ls, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "node (vite)") {
		t.Errorf("expected process to read \"node (vite)\":\n%s", buf.String())
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"nginx", []string{"nginx", "-g", "daemon off;"}, ""},
		{"node", []string{"node"}, ""},
		{"node", []string{"node", "server.js"}, "server.js"},
		{"node", []string{"node", "--inspect", "-r", "dotenv/config", "dist/main.js"}, "main.js"},
		{"node", []string{"/usr/bin/node", "/app/node_modules/vite/bin/vite.js"}, "vite"},
		{"node", []string{"node", "/app/node_modules/@nestjs/cli/bin/nest.js", "start"}, "@nestjs/cli"},
		{"node", []string{"node", "/app/node_modules/.bin/next", "dev"}, "next"},
		{"python3", []string{"python3", "manage.py", "runserver"}, "manage.py"},
		{"python3.12", []string{"python3.12", "-m", "http.server", "8000"}, "http.server"},
		{"python", []string{"python", "-c", "import x"}, ""},
		{"java", []string{"java", "-Xmx1g", "-jar", "/opt/app/service.jar"}, "service.jar"},
		{"java", []string{"java", "-cp", "lib/*", "org.gradle.launcher.daemon.bootstrap.GradleDaemon"}, "GradleDaemon"},
		{"deno", []string{"deno", "run", "-A", "main.ts"}, "main.ts"},
		{"node", []string{"node", "/srv/a-really-long-script-name-for-a-server.js"}, "a-really-long-script-na…"},
	}
	for _, tt := range tests {
		if got := describe(tt.name, tt.args); got != tt.want {
			t.Errorf("describe(%q, %q) = %q, want %q", tt.name, tt.args, got, tt.want)
		}
	}
}

func TestLinesStylesSelectedRowOnly(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, true)
	r.SetColorProfile(termenv.TrueColor)
	rows := GroupRows(sample())
	lines := Lines(r, rows, Options{Now: now}, func(i int) RowStyle { return RowStyle{Selected: i == 1} })
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header + 2 rows", len(lines))
	}
	// 48;2 starts a 24-bit background color.
	if strings.Contains(lines[1], "48;2") || !strings.Contains(lines[2], "48;2") {
		t.Errorf("only the selected row should have a background:\n%q\n%q", lines[1], lines[2])
	}
}
