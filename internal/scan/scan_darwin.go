//go:build darwin

package scan

import (
	"errors"

	"golang.org/x/sys/unix"
)

// New returns the scanner for the current platform.
func New() Scanner {
	return &libprocScanner{lookupUser: cachedUserLookup()}
}

// libprocScanner discovers listeners with libproc, the same interface lsof
// uses on macOS. Without root it only sees the current user's processes.
type libprocScanner struct {
	lookupUser func(uid int) string
}

// socketKey identifies one socket in one process; a socket shared by several
// file descriptors of the same process is reported once.
type socketKey struct {
	pid int
	id  uint64
}

func (s *libprocScanner) Scan() ([]Listener, error) {
	pidBuf := readGrowing(4096*4, procListPIDs)
	if pidBuf == nil {
		return nil, errors.New("listing processes failed")
	}

	seen := make(map[socketKey]bool)
	sockBuf := make([]byte, socketFDInfoSize)
	established := make(map[uint16]int)
	var out []Listener
	for _, pid := range parsePIDs(pidBuf) {
		fdBuf := readGrowing(256*procFDInfoSize, func(b []byte) int {
			return procPIDInfo(pid, procPIDListFDs, b)
		})
		var proc *Listener // process details, read once we find a listener
		for _, fd := range parseSocketFDs(fdBuf) {
			n := procPIDFDInfo(pid, fd, procPIDFDSocketInfo, sockBuf)
			if n <= 0 {
				continue
			}
			if port, ok := parseEstablishedPort(sockBuf[:n]); ok {
				established[port]++
				continue
			}
			sock, ok := parseSocketFDInfo(sockBuf[:n])
			if !ok || seen[socketKey{pid, sock.ID}] {
				continue
			}
			seen[socketKey{pid, sock.ID}] = true
			if proc == nil {
				p := s.readProc(pid)
				proc = &p
			}
			l := *proc
			l.Port, l.Protocol, l.Address = sock.Port, sock.Protocol, sock.Address
			out = append(out, l)
		}
	}
	// Only connections held by processes we can inspect are counted.
	for i := range out {
		out[i].Connections = established[out[i].Port]
	}
	sortListeners(out)
	return out, nil
}

// readProc fills in the process fields of a Listener. Fields that cannot be
// read are left empty.
func (s *libprocScanner) readProc(pid int) Listener {
	l := Listener{PID: pid}
	buf := make([]byte, vnodePathInfoSize)
	if n := procPIDInfo(pid, procPIDTBSDInfo, buf[:bsdInfoSize]); n > 0 {
		if info, ok := parseBSDInfo(buf[:n]); ok {
			l.ProcessName = info.Name
			l.StartTime = info.Start
			l.User = s.lookupUser(info.UID)
		}
	}
	if n := procPIDInfo(pid, procPIDVnodePathInfo, buf); n > 0 {
		l.Cwd = parseVnodePathCwd(buf[:n])
	}
	if b, err := unix.SysctlRaw("kern.procargs2", pid); err == nil {
		l.Command = parseProcArgs2(b)
	}
	return l
}

// readGrowing calls fill with increasingly large buffers until the result
// fits, since lists of processes and file descriptors can grow between calls.
// It returns nil if fill fails.
func readGrowing(size int, fill func([]byte) int) []byte {
	const limit = 64 << 20
	for ; size <= limit; size *= 2 {
		buf := make([]byte, size)
		n := fill(buf)
		if n <= 0 {
			return nil
		}
		if n < size {
			return buf[:n]
		}
	}
	return nil
}
