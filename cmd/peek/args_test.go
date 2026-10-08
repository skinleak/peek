package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/skinleak/peek/internal/scan"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args []string
		want config
	}{
		{nil, config{}},
		{[]string{"--json"}, config{json: true}},
		{[]string{"3000"}, config{ranges: []scan.PortRange{pr(3000, 3000)}}},
		{[]string{"3000-3999", "--json"}, config{json: true, ranges: []scan.PortRange{pr(3000, 3999)}}},
		{[]string{"22", "8000-8100"}, config{ranges: []scan.PortRange{pr(22, 22), pr(8000, 8100)}}},
		{[]string{"kill", "3000"}, config{kill: true, ranges: []scan.PortRange{pr(3000, 3000)}}},
		{[]string{"kill", "3000", "--force", "-y"}, config{kill: true, force: true, yes: true, ranges: []scan.PortRange{pr(3000, 3000)}}},
		{[]string{"-f", "kill", "3000"}, config{kill: true, force: true, ranges: []scan.PortRange{pr(3000, 3000)}}},
		{[]string{"kill", "--", "3000"}, config{kill: true, ranges: []scan.PortRange{pr(3000, 3000)}}},
		{[]string{"-h"}, config{help: true}},
		{[]string{"kill", "--help"}, config{help: true}},
		{[]string{"--version"}, config{version: true}},
		{[]string{"-i"}, config{interactive: true}},
		{[]string{"wait", "5432"}, config{wait: true, ranges: []scan.PortRange{pr(5432, 5432)}}},
		{[]string{"wait", "5432", "6379", "--free"}, config{wait: true, free: true, ranges: []scan.PortRange{pr(5432, 5432), pr(6379, 6379)}}},
		{[]string{"wait", "5432", "--timeout", "30s"}, config{wait: true, timeout: 30 * time.Second, ranges: []scan.PortRange{pr(5432, 5432)}}},
		{[]string{"wait", "--timeout=2m", "5432"}, config{wait: true, timeout: 2 * time.Minute, ranges: []scan.PortRange{pr(5432, 5432)}}},
		{[]string{"-t", "1.5", "wait", "5432"}, config{wait: true, timeout: 1500 * time.Millisecond, ranges: []scan.PortRange{pr(5432, 5432)}}},
		{[]string{"3000-3999", "--interactive"}, config{interactive: true, ranges: []scan.PortRange{pr(3000, 3999)}}},
	}
	for _, tt := range tests {
		got, err := parseArgs(tt.args)
		if err != nil {
			t.Errorf("parseArgs(%q): unexpected error %v", tt.args, err)
			continue
		}
		if !equalConfig(got, tt.want) {
			t.Errorf("parseArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
		}
	}
}

func TestParseArgsErrors(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"kill"}, "kill needs a port"},
		{[]string{"--verbose"}, `unknown flag "--verbose"`},
		{[]string{"abc"}, `invalid port "abc"`},
		{[]string{"70000"}, "between 1 and 65535"},
		{[]string{"3999-3000"}, "start is greater than end"},
		{[]string{"3000", "kill"}, `invalid port "kill"`},
		{[]string{"kill", "3000", "--json"}, "--json cannot be used with kill"},
		{[]string{"3000", "--force"}, "--force only applies"},
		{[]string{"3000", "-y"}, "--yes only applies"},
		{[]string{"-i", "kill", "3000"}, "--interactive cannot be used with kill"},
		{[]string{"-i", "--json"}, "--interactive cannot be used with --json"},
		{[]string{"wait"}, "wait needs a port"},
		{[]string{"wait", "5432", "--json"}, "wait can't be combined"},
		{[]string{"wait", "5432", "-y"}, "wait can't be combined"},
		{[]string{"wait", "5432", "--timeout"}, "--timeout needs a duration"},
		{[]string{"wait", "5432", "--timeout", "soon"}, `invalid timeout "soon"`},
		{[]string{"wait", "5432", "-t", "0"}, "must be more than zero"},
		{[]string{"5432", "--free"}, "--free only applies"},
		{[]string{"5432", "--timeout", "5s"}, "--timeout only applies"},
	}
	for _, tt := range tests {
		_, err := parseArgs(tt.args)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("parseArgs(%q) error = %v, want containing %q", tt.args, err, tt.wantErr)
		}
	}
}

func equalConfig(a, b config) bool {
	return a.kill == b.kill && a.json == b.json && a.interactive == b.interactive &&
		a.wait == b.wait && a.free == b.free && a.timeout == b.timeout &&
		a.force == b.force && a.yes == b.yes && a.help == b.help && a.version == b.version &&
		slices.Equal(a.ranges, b.ranges)
}

func pr(lo, hi uint16) scan.PortRange { return scan.PortRange{Lo: lo, Hi: hi} }
