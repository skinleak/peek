package scan

import (
	"encoding/binary"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseProcNetTCP(t *testing.T) {
	got, err := parseProcNet(openFixture(t, "tcp"), "tcp", binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	want := []socketEntry{
		{Protocol: "tcp", Address: netip.MustParseAddr("127.0.0.1"), Port: 3000, UID: 1000, Inode: 11111},
		{Protocol: "tcp", Address: netip.MustParseAddr("0.0.0.0"), Port: 8080, UID: 0, Inode: 22222},
		// the ESTABLISHED row (inode 33333) must be skipped
	}
	assertEntries(t, got, want)
}

func TestParseProcNetTCP6(t *testing.T) {
	got, err := parseProcNet(openFixture(t, "tcp6"), "tcp6", binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	want := []socketEntry{
		{Protocol: "tcp6", Address: netip.MustParseAddr("::"), Port: 8080, UID: 0, Inode: 44444},
		{Protocol: "tcp6", Address: netip.MustParseAddr("::1"), Port: 22, UID: 0, Inode: 55555},
		// IPv4-mapped addresses are unmapped
		{Protocol: "tcp6", Address: netip.MustParseAddr("127.0.0.1"), Port: 5001, UID: 1000, Inode: 66666},
	}
	assertEntries(t, got, want)
}

func assertEntries(t *testing.T, got, want []socketEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func TestParseProcNetErrors(t *testing.T) {
	const header = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	tests := map[string]string{
		"too few fields": "   0: 0100007F:0BB8 00000000:0000 0A\n",
		"bad address":    "   0: ZZ00007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1 1\n",
		"bad port":       "   0: 0100007F:XYZW 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1 1\n",
		"bad inode":      "   0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 x 1\n",
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseProcNet(strings.NewReader(header+line), "tcp", binary.LittleEndian)
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDecodeAddrBigEndian(t *testing.T) {
	got, err := decodeAddr("7F000001", binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParseAddr("127.0.0.1"); got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseSocketLink(t *testing.T) {
	tests := []struct {
		in    string
		inode uint64
		ok    bool
	}{
		{"socket:[12345]", 12345, true},
		{"pipe:[12345]", 0, false},
		{"/dev/null", 0, false},
		{"socket:[abc]", 0, false},
		{"socket:[123", 0, false},
	}
	for _, tt := range tests {
		inode, ok := parseSocketLink(tt.in)
		if inode != tt.inode || ok != tt.ok {
			t.Errorf("parseSocketLink(%q) = %d, %v; want %d, %v", tt.in, inode, ok, tt.inode, tt.ok)
		}
	}
}

func TestParseStatStartTicks(t *testing.T) {
	b, err := os.ReadFile("testdata/pid_stat")
	if err != nil {
		t.Fatal(err)
	}
	ticks, err := parseStatStartTicks(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if ticks != 360000 {
		t.Errorf("got %d ticks, want 360000", ticks)
	}

	if _, err := parseStatStartTicks("1234 (truncated) S 1 2"); err == nil {
		t.Error("expected error for truncated stat")
	}
}

func TestParseBootTime(t *testing.T) {
	got, err := parseBootTime(openFixture(t, "stat"))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Unix(1759831200, 0); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if _, err := parseBootTime(strings.NewReader("cpu 1 2 3\n")); err == nil {
		t.Error("expected error when btime is missing")
	}
}

func TestStartTimeFromTicks(t *testing.T) {
	boot := time.Unix(1_000_000, 0)
	if got, want := startTimeFromTicks(boot, 12345), boot.Add(123450*time.Millisecond); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// ~10 years of uptime must not overflow.
	const tenYears = 10 * 365 * 24 * 3600 * userHZ
	if got := startTimeFromTicks(boot, tenYears); !got.After(boot) {
		t.Errorf("overflow: start time %v is not after boot %v", got, boot)
	}
}
