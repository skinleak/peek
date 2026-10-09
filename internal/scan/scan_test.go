package scan

import (
	"net/netip"
	"slices"
	"testing"
)

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		in      string
		want    PortRange
		wantErr bool
	}{
		{in: "3000", want: PortRange{3000, 3000}},
		{in: "1", want: PortRange{1, 1}},
		{in: "65535", want: PortRange{65535, 65535}},
		{in: "3000-3999", want: PortRange{3000, 3999}},
		{in: "80-80", want: PortRange{80, 80}},
		{in: "0", wantErr: true},
		{in: "65536", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "", wantErr: true},
		{in: "3999-3000", wantErr: true},
		{in: "3000-", wantErr: true},
		{in: "-3000", wantErr: true},
		{in: "1-2-3", wantErr: true},
		{in: " 3000", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParsePortRange(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParsePortRange(%q) = %v, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePortRange(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParsePortRange(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestPortRangeString(t *testing.T) {
	if s := (PortRange{3000, 3000}).String(); s != "3000" {
		t.Errorf("got %q", s)
	}
	if s := (PortRange{3000, 3999}).String(); s != "3000-3999" {
		t.Errorf("got %q", s)
	}
}

func TestFilter(t *testing.T) {
	ls := []Listener{{Port: 22}, {Port: 3000}, {Port: 3500}, {Port: 8080}}
	ports := func(ls []Listener) []uint16 {
		var out []uint16
		for _, l := range ls {
			out = append(out, l.Port)
		}
		return out
	}
	tests := []struct {
		name   string
		ranges []PortRange
		want   []uint16
	}{
		{"no filter", nil, []uint16{22, 3000, 3500, 8080}},
		{"single", []PortRange{{3000, 3000}}, []uint16{3000}},
		{"range", []PortRange{{3000, 3999}}, []uint16{3000, 3500}},
		{"multiple", []PortRange{{22, 22}, {8000, 9000}}, []uint16{22, 8080}},
		{"overlapping ranges do not duplicate", []PortRange{{3000, 3500}, {3500, 4000}}, []uint16{3000, 3500}},
		{"no match", []PortRange{{9999, 9999}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ports(Filter(ls, tt.ranges))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestInDirs(t *testing.T) {
	ls := []Listener{
		{Port: 3000, Cwd: "/home/dev/webapp"},
		{Port: 3001, Cwd: "/home/dev/webapp/apps/api"},
		{Port: 3002, Cwd: "/home/dev/webapp-old"}, // shares a prefix, but isn't inside
		{Port: 5432, Cwd: "/"},
		{Port: 8080}, // unknown working directory
	}
	tests := []struct {
		name string
		dirs []string
		want []uint16
	}{
		{"no dirs", nil, []uint16{3000, 3001, 3002, 5432, 8080}},
		{"project and subdirectories", []string{"/home/dev/webapp"}, []uint16{3000, 3001}},
		{"subdirectory only", []string{"/home/dev/webapp/apps"}, []uint16{3001}},
		{"several", []string{"/home/dev/webapp/apps/api", "/home/dev/webapp-old"}, []uint16{3001, 3002}},
		{"root matches everything known", []string{"/"}, []uint16{3000, 3001, 3002, 5432}},
		{"no match", []string{"/srv"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []uint16
			for _, l := range InDirs(ls, tt.dirs) {
				got = append(got, l.Port)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExposed(t *testing.T) {
	tests := map[string]bool{
		"0.0.0.0":     true,
		"::":          true,
		"192.168.1.5": true,
		"127.0.0.1":   false,
		"127.0.0.53":  false,
		"::1":         false,
	}
	for addr, want := range tests {
		l := Listener{Address: netip.MustParseAddr(addr)}
		if got := l.Exposed(); got != want {
			t.Errorf("Exposed(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestDescribePorts(t *testing.T) {
	tests := []struct {
		ranges []PortRange
		want   string
	}{
		{[]PortRange{{3000, 3000}}, "port 3000"},
		{[]PortRange{{3000, 3999}}, "ports 3000-3999"},
		{[]PortRange{{22, 22}, {8080, 8080}}, "ports 22, 8080"},
	}
	for _, tt := range tests {
		if got := DescribePorts(tt.ranges); got != tt.want {
			t.Errorf("DescribePorts(%v) = %q, want %q", tt.ranges, got, tt.want)
		}
	}
}
