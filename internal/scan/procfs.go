package scan

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// tcpListen is the TCP_LISTEN state as printed in /proc/net/tcp.
const tcpListen = "0A"

// userHZ is the tick rate the kernel uses for times exported to userspace
// (USER_HZ). It is 100 on every mainstream Linux architecture and, unlike the
// internal HZ, is part of the ABI, so it is safe to hard-code without cgo.
const userHZ = 100

// socketEntry is one row of /proc/net/tcp or /proc/net/tcp6.
type socketEntry struct {
	Protocol string
	Address  netip.Addr
	Port     uint16
	UID      int
	Inode    uint64
}

// parseProcNet reads the contents of /proc/net/tcp or /proc/net/tcp6 and
// returns the sockets in LISTEN state. Addresses are printed by the kernel as
// 32-bit words in host byte order, so order must be the byte order of the
// machine that produced the data (binary.NativeEndian for the live system).
func parseProcNet(r io.Reader, protocol string, order binary.ByteOrder) ([]socketEntry, error) {
	var entries []socketEntry
	sc := bufio.NewScanner(r)
	for lineNo := 1; sc.Scan(); lineNo++ {
		if lineNo == 1 {
			continue // header
		}
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 10 {
			return nil, fmt.Errorf("%s line %d: expected at least 10 fields, got %d", protocol, lineNo, len(fields))
		}
		if fields[3] != tcpListen {
			continue
		}
		e, err := parseSocketFields(fields, protocol, order)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", protocol, lineNo, err)
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s table: %w", protocol, err)
	}
	return entries, nil
}

func parseSocketFields(fields []string, protocol string, order binary.ByteOrder) (socketEntry, error) {
	addrHex, portHex, ok := strings.Cut(fields[1], ":")
	if !ok {
		return socketEntry{}, fmt.Errorf("malformed local address %q", fields[1])
	}
	addr, err := decodeAddr(addrHex, order)
	if err != nil {
		return socketEntry{}, err
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return socketEntry{}, fmt.Errorf("malformed port %q", portHex)
	}
	uid, err := strconv.Atoi(fields[7])
	if err != nil {
		return socketEntry{}, fmt.Errorf("malformed uid %q", fields[7])
	}
	inode, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil {
		return socketEntry{}, fmt.Errorf("malformed inode %q", fields[9])
	}
	return socketEntry{
		Protocol: protocol,
		Address:  addr,
		Port:     uint16(port),
		UID:      uid,
		Inode:    inode,
	}, nil
}

// decodeAddr turns the kernel's hex encoding of an IPv4 (8 hex digits) or
// IPv6 (32 hex digits) address into a netip.Addr. IPv4-mapped IPv6 addresses
// are unmapped so they display as plain IPv4.
func decodeAddr(s string, order binary.ByteOrder) (netip.Addr, error) {
	raw, err := hex.DecodeString(s)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.Addr{}, fmt.Errorf("malformed address %q", s)
	}
	// Each 32-bit word was printed as a host-order integer; re-serialise it
	// in host order to recover the original network-order bytes.
	b := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		order.PutUint32(b[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	addr, _ := netip.AddrFromSlice(b)
	return addr.Unmap(), nil
}

// parseSocketLink extracts the inode from a /proc/<pid>/fd link target of the
// form "socket:[12345]".
func parseSocketLink(target string) (uint64, bool) {
	rest, ok := strings.CutPrefix(target, "socket:[")
	if !ok {
		return 0, false
	}
	num, ok := strings.CutSuffix(rest, "]")
	if !ok {
		return 0, false
	}
	inode, err := strconv.ParseUint(num, 10, 64)
	return inode, err == nil
}

// parseStatStartTicks returns the process start time (field 22 of
// /proc/<pid>/stat) in clock ticks since boot. The command name in field 2 may
// contain spaces and parentheses, so fields are counted from the last ')'.
func parseStatStartTicks(stat string) (uint64, error) {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, fmt.Errorf("malformed stat: no command terminator")
	}
	// fields[0] is field 3 (state), so field 22 is fields[19].
	fields := strings.Fields(stat[end+1:])
	const idx = 22 - 3
	if len(fields) <= idx {
		return 0, fmt.Errorf("malformed stat: only %d fields after command", len(fields))
	}
	ticks, err := strconv.ParseUint(fields[idx], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed stat start time %q", fields[idx])
	}
	return ticks, nil
}

// parseBootTime returns the system boot time from the "btime" line of /proc/stat.
func parseBootTime(r io.Reader) (time.Time, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "btime ")
		if !ok {
			continue
		}
		secs, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("malformed btime %q", rest)
		}
		return time.Unix(secs, 0), nil
	}
	if err := sc.Err(); err != nil {
		return time.Time{}, err
	}
	return time.Time{}, fmt.Errorf("btime not found")
}

// startTimeFromTicks converts a start time in ticks since boot to wall time.
func startTimeFromTicks(boot time.Time, ticks uint64) time.Time {
	// Split into whole seconds and remainder: ticks*time.Second would
	// overflow int64 after about three years of uptime.
	secs := time.Duration(ticks/userHZ) * time.Second
	frac := time.Duration(ticks%userHZ) * time.Second / userHZ
	return boot.Add(secs + frac)
}
