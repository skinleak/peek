package scan

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"
)

// Offsets below are written out as literals, derived by hand from
// <sys/proc_info.h>, so a mistake in the parser's constants is caught here.

func socketFDInfo(kind, state, family uint32, vflag byte, port uint16, laddr [16]byte) []byte {
	b := make([]byte, 792)
	binary.LittleEndian.PutUint64(b[160:], 0xdeadbeef) // soi_so
	binary.LittleEndian.PutUint32(b[184:], family)     // soi_family
	binary.LittleEndian.PutUint32(b[256:], kind)       // soi_kind
	binary.BigEndian.PutUint16(b[268:], port)          // insi_lport, network order
	b[288] = vflag                                     // insi_vflag
	copy(b[312:], laddr[:])                            // insi_laddr
	binary.LittleEndian.PutUint32(b[344:], state)      // tcpsi_state
	return b
}

func TestParseSocketFDInfo(t *testing.T) {
	var v4 [16]byte
	copy(v4[12:], []byte{127, 0, 0, 1})
	loopback6 := netip.MustParseAddr("::1").As16()
	var any6 [16]byte

	tests := []struct {
		name   string
		buf    []byte
		want   darwinSocket
		wantOK bool
	}{
		{
			name:   "IPv4 listener",
			buf:    socketFDInfo(2, 1, 2, 0x1, 3000, v4),
			want:   darwinSocket{ID: 0xdeadbeef, Protocol: "tcp", Address: netip.MustParseAddr("127.0.0.1"), Port: 3000},
			wantOK: true,
		},
		{
			name:   "IPv6 loopback listener",
			buf:    socketFDInfo(2, 1, 30, 0x2, 8080, loopback6),
			want:   darwinSocket{ID: 0xdeadbeef, Protocol: "tcp6", Address: netip.MustParseAddr("::1"), Port: 8080},
			wantOK: true,
		},
		{
			name:   "IPv6 wildcard listener",
			buf:    socketFDInfo(2, 1, 30, 0x2, 22, any6),
			want:   darwinSocket{ID: 0xdeadbeef, Protocol: "tcp6", Address: netip.MustParseAddr("::"), Port: 22},
			wantOK: true,
		},
		{name: "established connection", buf: socketFDInfo(2, 4, 2, 0x1, 3000, v4)},
		{name: "UDP socket", buf: socketFDInfo(1, 0, 2, 0x1, 53, v4)},
		{name: "truncated", buf: socketFDInfo(2, 1, 2, 0x1, 3000, v4)[:300]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSocketFDInfo(tt.buf)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseBSDInfo(t *testing.T) {
	b := make([]byte, 136)
	binary.LittleEndian.PutUint32(b[20:], 501) // pbi_uid
	copy(b[48:], "short-comm")                 // pbi_comm
	copy(b[64:], "a-much-longer-process-name") // pbi_name
	binary.LittleEndian.PutUint64(b[120:], 1759831200)
	binary.LittleEndian.PutUint64(b[128:], 250000)

	got, ok := parseBSDInfo(b)
	if !ok {
		t.Fatal("parse failed")
	}
	want := bsdInfo{UID: 501, Name: "a-much-longer-process-name", Start: time.Unix(1759831200, 250_000_000)}
	if got.UID != want.UID || got.Name != want.Name || !got.Start.Equal(want.Start) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	clear(b[64:96]) // no pbi_name: fall back to pbi_comm
	if got, _ := parseBSDInfo(b); got.Name != "short-comm" {
		t.Errorf("fallback name = %q, want short-comm", got.Name)
	}
	if _, ok := parseBSDInfo(b[:100]); ok {
		t.Error("expected failure on truncated buffer")
	}
}

func TestParseVnodePathCwd(t *testing.T) {
	b := make([]byte, 2352)
	copy(b[152:], "/Users/dev/code/app")
	if got := parseVnodePathCwd(b); got != "/Users/dev/code/app" {
		t.Errorf("got %q", got)
	}
	if got := parseVnodePathCwd(b[:500]); got != "" {
		t.Errorf("truncated buffer: got %q, want empty", got)
	}
}

func TestParseSocketFDs(t *testing.T) {
	b := make([]byte, 0, 32)
	for _, fd := range [][2]uint32{{0, 1}, {3, 2}, {4, 5}, {7, 2}} { // {fd, type}; 2 = socket
		b = binary.LittleEndian.AppendUint32(b, fd[0])
		b = binary.LittleEndian.AppendUint32(b, fd[1])
	}
	got := parseSocketFDs(b)
	if len(got) != 2 || got[0] != 3 || got[1] != 7 {
		t.Errorf("got %v, want [3 7]", got)
	}
}

func TestParsePIDs(t *testing.T) {
	var b []byte
	for _, pid := range []int32{1, 0, 4242, -1} {
		b = binary.LittleEndian.AppendUint32(b, uint32(pid))
	}
	got := parsePIDs(b)
	if len(got) != 2 || got[0] != 1 || got[1] != 4242 {
		t.Errorf("got %v, want [1 4242]", got)
	}
}
