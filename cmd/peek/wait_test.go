package main

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/skinleak/peek/internal/scan"
)

func listening(ports ...uint16) []scan.Listener {
	var ls []scan.Listener
	for _, p := range ports {
		ls = append(ls, scan.Listener{Port: p, Address: netip.MustParseAddr("127.0.0.1"), PID: int(p), ProcessName: "srv"})
	}
	return ls
}

// fakeWaiter returns a waiter whose scans return each of scans in turn
// (repeating the last), on a clock that advances by the interval per poll.
func fakeWaiter(ranges []scan.PortRange, free bool, timeout time.Duration, scans ...[]scan.Listener) (*waiter, *int) {
	clock := time.Unix(0, 0)
	polls := 0
	return &waiter{
		ranges:   ranges,
		free:     free,
		timeout:  timeout,
		interval: time.Second,
		scan: func() ([]scan.Listener, error) {
			ls := scans[min(polls, len(scans)-1)]
			polls++
			return ls, nil
		},
		sleep: func(d time.Duration) { clock = clock.Add(d) },
		now:   func() time.Time { return clock },
	}, &polls
}

func TestWaitUntilListening(t *testing.T) {
	ranges := []scan.PortRange{pr(5432, 5432), pr(3000, 3999)}
	w, polls := fakeWaiter(ranges, false, 0,
		nil,
		listening(5432),       // still nothing in 3000-3999
		listening(5432, 3001), // both ready
	)
	ls, ok, err := w.run()
	if err != nil || !ok {
		t.Fatalf("run() = %v, %v", ok, err)
	}
	if *polls != 3 {
		t.Errorf("polled %d times, want 3", *polls)
	}
	if got := readyMessage(ranges[1], ls, false); got != "Ports 3000-3999: port 3001 is listening (srv, PID 3001)" {
		t.Errorf("readyMessage = %q", got)
	}
	if got := readyMessage(ranges[0], ls, false); got != "Port 5432 is listening (srv, PID 5432)" {
		t.Errorf("readyMessage = %q", got)
	}
}

func TestWaitUntilFree(t *testing.T) {
	w, polls := fakeWaiter([]scan.PortRange{pr(3000, 3000)}, true, 0, listening(3000), listening(3000), nil)
	_, ok, err := w.run()
	if err != nil || !ok || *polls != 3 {
		t.Fatalf("run() = %v, %v after %d polls", ok, err, *polls)
	}
	if got := readyMessage(pr(3000, 3000), nil, true); got != "Port 3000 is free" {
		t.Errorf("readyMessage = %q", got)
	}
}

func TestWaitTimesOut(t *testing.T) {
	w, polls := fakeWaiter([]scan.PortRange{pr(5432, 5432)}, false, 3*time.Second, nil)
	var progress bytes.Buffer
	w.progress = &progress
	ls, ok, err := w.run()
	if err != nil || ok {
		t.Fatalf("run() = %v, %v; want a timeout", ok, err)
	}
	// Polls at 0s, 1s and 2s show progress; the one at 3s gives up.
	if *polls != 4 {
		t.Errorf("polled %d times, want 4", *polls)
	}
	if got := w.goal(w.pending(ls)); got != "port 5432 to listen" {
		t.Errorf("goal = %q", got)
	}
	if !strings.Contains(progress.String(), "Waiting for port 5432 to listen (2s)") {
		t.Errorf("progress = %q", progress.String())
	}
}

func TestWaitScanError(t *testing.T) {
	w, _ := fakeWaiter([]scan.PortRange{pr(5432, 5432)}, false, 0, nil)
	w.scan = func() ([]scan.Listener, error) { return nil, errors.New("boom") }
	if _, _, err := w.run(); err == nil {
		t.Fatal("expected the scan error")
	}
}
