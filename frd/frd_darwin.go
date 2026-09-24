//go:build darwin

package frd

import (
	"runtime"
	"syscall"
	"unsafe"
)

// attrlist is struct attrlist from <sys/attr.h>.
type attrlist struct {
	bitmapCount uint16
	_           uint16
	common      uint32
	volume      uint32
	directory   uint32
	file        uint32
	fork        uint32
}

var requested = attrlist{
	bitmapCount: 5,
	common:      attrReturned | attrName | attrError | attrObjtype | attrModtime,
	file:        attrDataLength,
}

const listOp = "getattrlistbulk"

func readAll(dst []Entry, fd int, buf []byte) ([]Entry, error) {
	for {
		n, err := getattrlistbulk(fd, &requested, buf)
		if err == syscall.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return dst, err
		}
		if dst, err = parseRecords(dst, buf, n); err != nil {
			return dst, err
		}
	}
}

func getattrlistbulk(fd int, al *attrlist, buf []byte) (int, error) {
	r, _, errno := syscall_syscall6(libc_getattrlistbulk_trampoline_addr,
		uintptr(fd), uintptr(unsafe.Pointer(al)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	runtime.KeepAlive(buf)
	if errno != 0 {
		return 0, errno
	}
	return int(int32(r)), nil
}

// libSystem is called without cgo the way golang.org/x/sys/unix calls it: the
// runtime keeps syscall.syscall6 reachable by linkname for such packages.
//
//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

var libc_getattrlistbulk_trampoline_addr uintptr

//go:cgo_import_dynamic libc_getattrlistbulk getattrlistbulk "/usr/lib/libSystem.B.dylib"
