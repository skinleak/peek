package main

import (
	"slices"
	"strings"
	"testing"

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
		{[]string{"kill"}, config{kill: true}},
		{[]string{"kill", "-f"}, config{kill: true, force: true}},
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
		a.force == b.force && a.yes == b.yes && a.help == b.help && a.version == b.version &&
		slices.Equal(a.ranges, b.ranges)
}

func pr(lo, hi uint16) scan.PortRange { return scan.PortRange{Lo: lo, Hi: hi} }
