// Trampolines into libSystem's libproc, so peek can call it without cgo.
// The same mechanism golang.org/x/sys/unix uses for darwin system calls.

#include "textflag.h"

TEXT libc_proc_listpids_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_proc_listpids(SB)
GLOBL	·libc_proc_listpids_trampoline_addr(SB), RODATA, $8
DATA	·libc_proc_listpids_trampoline_addr(SB)/8, $libc_proc_listpids_trampoline<>(SB)

TEXT libc_proc_pidinfo_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_proc_pidinfo(SB)
GLOBL	·libc_proc_pidinfo_trampoline_addr(SB), RODATA, $8
DATA	·libc_proc_pidinfo_trampoline_addr(SB)/8, $libc_proc_pidinfo_trampoline<>(SB)

TEXT libc_proc_pidfdinfo_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_proc_pidfdinfo(SB)
GLOBL	·libc_proc_pidfdinfo_trampoline_addr(SB), RODATA, $8
DATA	·libc_proc_pidfdinfo_trampoline_addr(SB)/8, $libc_proc_pidfdinfo_trampoline<>(SB)
