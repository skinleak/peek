package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/skinleak/peek/internal/docker"
	"github.com/skinleak/peek/internal/scan"
)

const (
	// waitInterval is how often peek wait rescans.
	waitInterval = 250 * time.Millisecond
	// exitInterrupted is the conventional status for a command stopped by
	// Ctrl+C (128 + SIGINT).
	exitInterrupted = 130
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// waiter polls until every port range is listening, or with free, until
// none is.
type waiter struct {
	ranges   []scan.PortRange
	free     bool
	timeout  time.Duration // 0 waits forever
	interval time.Duration
	scan     func() ([]scan.Listener, error)
	sleep    func(time.Duration)
	now      func() time.Time
	// progress, if not nil, gets a spinner line redrawn on every poll.
	progress io.Writer
}

// run waits for the condition and returns the listeners found by the last
// scan and whether the condition was met before the timeout.
func (w waiter) run() (ls []scan.Listener, ok bool, err error) {
	start := w.now()
	for frame := 0; ; frame++ {
		ls, err = w.scan()
		if err != nil {
			return nil, false, err
		}
		pending := w.pending(ls)
		if len(pending) == 0 {
			return ls, true, nil
		}
		elapsed := w.now().Sub(start)
		if w.timeout > 0 && elapsed >= w.timeout {
			return ls, false, nil
		}
		if w.progress != nil {
			fmt.Fprintf(w.progress, "\r\033[K%s Waiting for %s (%s)", spinFrames[frame%len(spinFrames)],
				w.goal(pending), elapsed.Truncate(time.Second))
		}
		w.sleep(w.interval)
	}
}

// pending returns the ranges that aren't in the wanted state yet.
func (w waiter) pending(ls []scan.Listener) []scan.PortRange {
	var out []scan.PortRange
	for _, r := range w.ranges {
		if listening := len(scan.Filter(ls, []scan.PortRange{r})) > 0; listening == w.free {
			out = append(out, r)
		}
	}
	return out
}

// goal says what peek is waiting for: "port 5432 to listen".
func (w waiter) goal(pending []scan.PortRange) string {
	if w.free {
		return scan.DescribePorts(pending) + " to be free"
	}
	return scan.DescribePorts(pending) + " to listen"
}

func runWait(cfg config) int {
	w := waiter{
		ranges:   cfg.ranges,
		free:     cfg.free,
		timeout:  cfg.timeout,
		interval: waitInterval,
		scan:     scan.New().Scan,
		sleep:    time.Sleep,
		now:      time.Now,
	}
	if isTerminal(os.Stderr) {
		w.progress = os.Stderr
		// On Ctrl+C, don't leave the spinner behind for the shell prompt.
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-interrupt
			fmt.Fprint(os.Stderr, "\r\033[K")
			os.Exit(exitInterrupted)
		}()
	}
	ls, ok, err := w.run()
	if w.progress != nil {
		fmt.Fprint(os.Stderr, "\r\033[K") // clear the spinner line
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek: %v\n", err)
		return exitNotFound
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "peek: timed out after %s waiting for %s\n", cfg.timeout, w.goal(w.pending(ls)))
		return exitNotFound
	}
	if !cfg.free {
		docker.Enrich(ls) // name containers in the messages below
	}
	for _, r := range cfg.ranges {
		fmt.Fprintln(os.Stderr, readyMessage(r, ls, cfg.free))
	}
	return exitOK
}

// readyMessage reports a range that reached the wanted state, naming who
// listens on it: "Port 5432 is listening (postgres, PID 812)".
func readyMessage(r scan.PortRange, ls []scan.Listener, free bool) string {
	what := scan.DescribePorts([]scan.PortRange{r})
	what = strings.ToUpper(what[:1]) + what[1:] // "Port 5432", "Ports 3000-3999"
	if free {
		if r.Lo == r.Hi {
			return what + " is free"
		}
		return what + " are free"
	}
	l := scan.Filter(ls, []scan.PortRange{r})[0]
	owner := ""
	switch {
	case l.Container != "":
		owner = fmt.Sprintf(" (%s, docker)", l.Container)
	case l.PID != 0:
		owner = fmt.Sprintf(" (%s, PID %d)", l.ProcessName, l.PID)
	}
	if r.Lo == r.Hi {
		return what + " is listening" + owner
	}
	return fmt.Sprintf("%s: port %d is listening%s", what, l.Port, owner)
}
