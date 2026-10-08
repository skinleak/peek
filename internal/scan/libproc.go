package scan

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"time"
)

// Parsers for the structs returned by macOS libproc (<sys/proc_info.h>).
// They live in an untagged file so they can be tested on any OS. Offsets are
// for the LP64 layout shared by darwin/amd64 and darwin/arm64, and all
// integers are little-endian on both.

const (
	// proc_listpids / proc_pidinfo flavors
	procAllPIDs           = 1
	procPIDListFDs        = 1
	procPIDTBSDInfo       = 3
	procPIDVnodePathInfo  = 9
	procPIDFDSocketInfo   = 3
	proxFDTypeSocket      = 2
	procFDInfoSize        = 8    // struct proc_fdinfo
	bsdInfoSize           = 136  // struct proc_bsdinfo
	vnodePathInfoSize     = 2352 // struct proc_vnodepathinfo
	socketFDInfoSize      = 792  // struct socket_fdinfo
	maxPathLen            = 1024 // MAXPATHLEN
	afInet6Darwin         = 30   // AF_INET6 on darwin
	sockInfoTCP           = 2    // SOCKINFO_TCP
	tcpStateListen        = 1    // TSI_S_LISTEN
	tcpStateEstablished   = 4    // TSI_S_ESTABLISHED
	inIPv4                = 0x1  // INI_IPV4
	inIPv6                = 0x2  // INI_IPV6
	socketInfoOffset      = 24   // socket_fdinfo.psi, after struct proc_fileinfo
	soiSoOffset           = socketInfoOffset + 136
	soiFamilyOffset       = socketInfoOffset + 160
	soiKindOffset         = socketInfoOffset + 232
	soiProtoOffset        = socketInfoOffset + 240 // union soi_proto (tcp_sockinfo)
	insiLportOffset       = soiProtoOffset + 4
	insiVflagOffset       = soiProtoOffset + 24
	insiLaddrOffset       = soiProtoOffset + 48
	tcpsiStateOffset      = soiProtoOffset + 80
	minSocketFDInfoLength = tcpsiStateOffset + 4
	vnodeInfoSize         = 152 // struct vnode_info, before vip_path
)

var le = binary.LittleEndian

// darwinSocket is a listening TCP socket as described by socket_fdinfo.
type darwinSocket struct {
	ID       uint64 // kernel socket address (soi_so), identifies shared sockets
	Protocol string
	Address  netip.Addr
	Port     uint16
}

// parseSocketFDInfo decodes a socket_fdinfo buffer and reports whether it
// describes a TCP socket in the LISTEN state.
func parseSocketFDInfo(b []byte) (darwinSocket, bool) {
	if len(b) < minSocketFDInfoLength {
		return darwinSocket{}, false
	}
	if int32(le.Uint32(b[soiKindOffset:])) != sockInfoTCP ||
		int32(le.Uint32(b[tcpsiStateOffset:])) != tcpStateListen {
		return darwinSocket{}, false
	}
	s := darwinSocket{
		ID:       le.Uint64(b[soiSoOffset:]),
		Protocol: "tcp",
		// insi_lport is an int holding the port in network byte order.
		Port: binary.BigEndian.Uint16(b[insiLportOffset:]),
	}
	if int32(le.Uint32(b[soiFamilyOffset:])) == afInet6Darwin {
		s.Protocol = "tcp6"
	}
	laddr := b[insiLaddrOffset : insiLaddrOffset+16]
	if b[insiVflagOffset]&inIPv4 != 0 {
		// struct in4in6_addr: 12 bytes of padding, then the IPv4 address.
		s.Address = netip.AddrFrom4([4]byte(laddr[12:16]))
	} else {
		s.Address = netip.AddrFrom16([16]byte(laddr)).Unmap()
	}
	return s, true
}

// parseEstablishedPort reports the local port of an established TCP
// connection described by a socket_fdinfo buffer.
func parseEstablishedPort(b []byte) (uint16, bool) {
	if len(b) < minSocketFDInfoLength ||
		int32(le.Uint32(b[soiKindOffset:])) != sockInfoTCP ||
		int32(le.Uint32(b[tcpsiStateOffset:])) != tcpStateEstablished {
		return 0, false
	}
	return binary.BigEndian.Uint16(b[insiLportOffset:]), true
}

// bsdInfo is the subset of struct proc_bsdinfo that peek uses.
type bsdInfo struct {
	UID   int
	Name  string
	Start time.Time
}

func parseBSDInfo(b []byte) (bsdInfo, bool) {
	if len(b) < bsdInfoSize {
		return bsdInfo{}, false
	}
	name := cString(b[64:96]) // pbi_name, the full name
	if name == "" {
		name = cString(b[48:64]) // pbi_comm, truncated to 16 bytes
	}
	sec, usec := int64(le.Uint64(b[120:])), int64(le.Uint64(b[128:]))
	return bsdInfo{
		UID:   int(le.Uint32(b[20:])), // pbi_uid
		Name:  name,
		Start: time.Unix(sec, usec*1000),
	}, true
}

// parseVnodePathCwd extracts the current directory (pvi_cdir.vip_path) from
// a proc_vnodepathinfo buffer.
func parseVnodePathCwd(b []byte) string {
	if len(b) < vnodeInfoSize+maxPathLen {
		return ""
	}
	return cString(b[vnodeInfoSize : vnodeInfoSize+maxPathLen])
}

// parseSocketFDs returns the socket file descriptors from a list of
// struct proc_fdinfo.
func parseSocketFDs(b []byte) []int32 {
	var fds []int32
	for i := 0; i+procFDInfoSize <= len(b); i += procFDInfoSize {
		if le.Uint32(b[i+4:]) == proxFDTypeSocket {
			fds = append(fds, int32(le.Uint32(b[i:])))
		}
	}
	return fds
}

// parsePIDs decodes the int32 PIDs returned by proc_listpids.
func parsePIDs(b []byte) []int {
	var pids []int
	for i := 0; i+4 <= len(b); i += 4 {
		if pid := int(int32(le.Uint32(b[i:]))); pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// parseProcArgs2 extracts the arguments from the kern.procargs2 sysctl:
// argc as a 32-bit integer, the executable path, NUL padding, then argc
// NUL-terminated arguments followed by the environment.
func parseProcArgs2(b []byte) []string {
	if len(b) < 4 {
		return nil
	}
	argc := int(int32(le.Uint32(b)))
	b = b[4:]
	i := bytes.IndexByte(b, 0) // end of the executable path
	if i < 0 {
		return nil
	}
	b = bytes.TrimLeft(b[i:], "\x00")
	var args []string
	for len(args) < argc && len(b) > 0 {
		end := bytes.IndexByte(b, 0)
		if end < 0 {
			end = len(b)
		}
		args = append(args, string(b[:end]))
		b = b[min(end+1, len(b)):]
	}
	return args
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
