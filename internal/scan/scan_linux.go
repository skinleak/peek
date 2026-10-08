//go:build linux

package scan

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// New returns the scanner for the current platform.
func New() Scanner {
	return &procScanner{
		root:       "/proc",
		order:      binary.NativeEndian,
		lookupUser: cachedUserLookup(),
	}
}

// procScanner discovers listeners by reading procfs.
type procScanner struct {
	root       string
	order      binary.ByteOrder
	lookupUser func(uid int) string
}

// procInfo is what we know about a process. Fields we could not read are left zero.
type procInfo struct {
	name  string
	args  []string
	cwd   string
	start time.Time
}

func (s *procScanner) Scan() ([]Listener, error) {
	sockets, established, err := s.readSockets()
	if err != nil {
		return nil, err
	}
	if len(sockets) == 0 {
		return nil, nil
	}

	wanted := make(map[uint64]bool, len(sockets))
	for _, sock := range sockets {
		wanted[sock.Inode] = true
	}
	owners := s.socketOwners(wanted)
	boot, _ := s.bootTime() // without it we just omit start times

	procs := make(map[int]procInfo)
	var out []Listener
	for _, sock := range sockets {
		base := Listener{
			Port:     sock.Port,
			Protocol: sock.Protocol,
			Address:  sock.Address,
			User:     s.lookupUser(sock.UID),
			// Counted per port: a connection to 127.0.0.1:3000 is attributed
			// to every listener on port 3000.
			Connections: established[sock.Port],
		}
		pids := owners[sock.Inode]
		if len(pids) == 0 {
			out = append(out, base) // owner not visible to us (permissions)
			continue
		}
		for _, pid := range pids {
			info, ok := procs[pid]
			if !ok {
				info = s.readProc(pid, boot)
				procs[pid] = info
			}
			l := base
			l.PID = pid
			l.ProcessName = info.name
			l.Command = info.args
			l.Cwd = info.cwd
			l.StartTime = info.start
			out = append(out, l)
		}
	}
	sortListeners(out)
	return out, nil
}

// readSockets returns listening sockets from /proc/net/tcp and tcp6, and the
// number of established connections per local port. The tcp6 table is
// optional because it is absent when IPv6 is disabled.
func (s *procScanner) readSockets() ([]socketEntry, map[uint16]int, error) {
	var all []socketEntry
	established := make(map[uint16]int)
	for _, t := range []struct {
		proto    string
		optional bool
	}{{"tcp", false}, {"tcp6", true}} {
		path := filepath.Join(s.root, "net", t.proto)
		f, err := os.Open(path)
		if err != nil {
			if t.optional && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, nil, fmt.Errorf("reading socket table: %w", err)
		}
		entries, conns, err := parseProcNet(f, t.proto, s.order)
		f.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		all = append(all, entries...)
		for port, n := range conns {
			established[port] += n
		}
	}
	return all, established, nil
}

// socketOwners maps each wanted socket inode to the PIDs holding it open.
// Processes whose fd directory we cannot read (other users' processes when
// not root, or processes that exited mid-scan) are silently skipped.
func (s *procScanner) socketOwners(wanted map[uint64]bool) map[uint64][]int {
	owners := make(map[uint64][]int)
	for _, pid := range s.pids() {
		fdDir := filepath.Join(s.root, strconv.Itoa(pid), "fd")
		fds, err := readDirNames(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd))
			if err != nil {
				continue
			}
			inode, ok := parseSocketLink(target)
			if !ok || !wanted[inode] || slices.Contains(owners[inode], pid) {
				continue
			}
			owners[inode] = append(owners[inode], pid)
		}
	}
	return owners
}

// pids lists the numeric entries of the proc root.
func (s *procScanner) pids() []int {
	names, err := readDirNames(s.root)
	if err != nil {
		return nil
	}
	var pids []int
	for _, name := range names {
		if pid, err := strconv.Atoi(name); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func (s *procScanner) readProc(pid int, boot time.Time) procInfo {
	dir := filepath.Join(s.root, strconv.Itoa(pid))
	var info procInfo
	if b, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil {
		info.name = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		info.args = parseCmdline(b)
		info.name = fullName(info.name, info.args)
	}
	if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
		info.cwd = cwd
	}
	if !boot.IsZero() {
		if b, err := os.ReadFile(filepath.Join(dir, "stat")); err == nil {
			if ticks, err := parseStatStartTicks(string(b)); err == nil {
				info.start = startTimeFromTicks(boot, ticks)
			}
		}
	}
	return info
}

func (s *procScanner) bootTime() (time.Time, error) {
	f, err := os.Open(filepath.Join(s.root, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	return parseBootTime(f)
}

// readDirNames lists a directory without sorting or stat-ing its entries.
func readDirNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}
