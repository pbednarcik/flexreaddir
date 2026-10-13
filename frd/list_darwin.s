// Each trampoline jumps to the libSystem function that list_darwin.go imports
// under its name with //go:cgo_import_dynamic.

#include "textflag.h"

TEXT libc_getattrlistbulk_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_getattrlistbulk(SB)
GLOBL	·libc_getattrlistbulk_trampoline_addr(SB), RODATA, $8
DATA	·libc_getattrlistbulk_trampoline_addr(SB)/8, $libc_getattrlistbulk_trampoline<>(SB)

TEXT libc_fgetattrlist_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_fgetattrlist(SB)
GLOBL	·libc_fgetattrlist_trampoline_addr(SB), RODATA, $8
DATA	·libc_fgetattrlist_trampoline_addr(SB)/8, $libc_fgetattrlist_trampoline<>(SB)

TEXT libc___getdirentries64_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc___getdirentries64(SB)
GLOBL	·libc___getdirentries64_trampoline_addr(SB), RODATA, $8
DATA	·libc___getdirentries64_trampoline_addr(SB)/8, $libc___getdirentries64_trampoline<>(SB)
