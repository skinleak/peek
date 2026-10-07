//go:build darwin

package scan

import (
	"syscall"
	"unsafe"
)

// libproc is part of libSystem, which every Go binary on macOS already links
// against. Calling it through these trampolines avoids cgo, so peek stays a
// static, cross-compilable binary. See libproc_darwin.s.

//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

var (
	libc_proc_listpids_trampoline_addr  uintptr
	libc_proc_pidinfo_trampoline_addr   uintptr
	libc_proc_pidfdinfo_trampoline_addr uintptr
)

//go:cgo_import_dynamic libc_proc_listpids proc_listpids "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_proc_pidinfo proc_pidinfo "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_proc_pidfdinfo proc_pidfdinfo "/usr/lib/libSystem.B.dylib"

// The libproc functions return a C int: the number of bytes written, or
// 0 / -1 on failure. Only the low 32 bits of the return register are defined.
func cInt(r1 uintptr) int { return int(int32(r1)) }

// procListPIDs wraps int proc_listpids(uint32_t type, uint32_t typeinfo, void *buf, int size).
func procListPIDs(buf []byte) int {
	r1, _, _ := syscall_syscall6(libc_proc_listpids_trampoline_addr,
		procAllPIDs, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	return cInt(r1)
}

// procPIDInfo wraps int proc_pidinfo(int pid, int flavor, uint64_t arg, void *buf, int size).
func procPIDInfo(pid, flavor int, buf []byte) int {
	r1, _, _ := syscall_syscall6(libc_proc_pidinfo_trampoline_addr,
		uintptr(pid), uintptr(flavor), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	return cInt(r1)
}

// procPIDFDInfo wraps int proc_pidfdinfo(int pid, int fd, int flavor, void *buf, int size).
func procPIDFDInfo(pid int, fd int32, flavor int, buf []byte) int {
	r1, _, _ := syscall_syscall6(libc_proc_pidfdinfo_trampoline_addr,
		uintptr(pid), uintptr(fd), uintptr(flavor), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	return cInt(r1)
}
