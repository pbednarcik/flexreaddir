//go:build darwin

package frd

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
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
	common:      attrReturned | attrName | attrError | attrFsid | attrObjtype | attrModtime | attrChgtime | attrFileID,
	file:        attrDataLength,
}

const listOp = bulkOp

// namesListOp is the call a Names listing's errors name here.
const namesListOp = namesOp

func readAll(dst []Entry, h uintptr, buf []byte, names bool) ([]Entry, error) {
	fd := int(h)
	if names {
		return readNames(dst, fd, buf)
	}
	persistent := func(fsid uint64) bool { return persistentIDs(fd, fsid) }
	for {
		n, err := getattrlistbulk(fd, &requested, buf)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			return dst, err
		}
		if dst, err = parseRecords(dst, buf, n, persistent); err != nil {
			return dst, err
		}
	}
}

// readNames lists fd by getdirentries64, the call readdir makes, which reads
// the directory's own records and creates no vnode for the files it names. A
// DT_UNKNOWN record is typed by fstatat of the name, never following a link.
func readNames(dst []Entry, fd int, buf []byte) ([]Entry, error) {
	var st unix.Stat_t
	stat := func(name []byte, e *Entry) (bool, error) {
		var err error
		for {
			if err = unix.Fstatat(fd, string(name), &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		if errors.Is(err, syscall.ENOENT) {
			return false, nil
		}
		if err != nil {
			return false, &os.PathError{Op: "fstatat", Path: string(name), Err: err}
		}
		e.Type = statMode(uint32(st.Mode))
		return true, nil
	}
	var pos int64
	for {
		n, err := getdirentries64(fd, buf, &pos)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			if errors.Is(err, syscall.EINVAL) && notDir(fd) {
				err = syscall.ENOTDIR // what the bulk call says
			}
			return dst, &os.PathError{Op: namesOp, Err: err}
		}
		if n == 0 {
			return dst, nil
		}
		if dst, err = parseDirentries(dst, buf[:n], stat); err != nil {
			return dst, err
		}
	}
}

// notDir reports whether fd is open on something other than a directory,
// which getdirentries64 refuses with EINVAL.
func notDir(fd int) bool {
	var st unix.Stat_t
	return unix.Fstat(fd, &st) == nil && st.Mode&unix.S_IFMT != unix.S_IFDIR
}

// volumes holds, by fsid, whether each volume seen keeps its file ids:
// FAT32, exFAT and NFS clear VOL_CAP_FMT_PERSISTENTOBJECTIDS, and FSKit gives
// an empty FAT file a new id on every listing. The answer is kept for the
// life of the process, so a later volume given the same fsid takes it.
var volumes sync.Map

// persistentIDs reports whether the volume fsid keeps its file ids, asking
// the kernel once per volume through the directory fd. Only a regular file's
// record asks, and a regular file lies on its directory's volume: a mount
// point is a directory.
func persistentIDs(fd int, fsid uint64) bool {
	if keep, ok := volumes.Load(fsid); ok {
		return keep.(bool)
	}
	keep, known := keepsIDs(fd)
	if known {
		volumes.LoadOrStore(fsid, keep)
	}
	return keep
}

// keepsIDs asks the kernel whether the volume holding fd keeps its file ids.
// A volume that cannot be asked is taken not to, and known is false, so the
// next listing asks again.
func keepsIDs(fd int) (keep, known bool) {
	var caps volCapabilities
	for {
		err := fgetattrlist(fd, &volumeCapabilities, unsafe.Pointer(&caps), unsafe.Sizeof(caps))
		if err == nil {
			return persists(caps), true
		}
		if !errors.Is(err, syscall.EINTR) {
			return false, false
		}
	}
}

// persists reads VOL_CAP_FMT_PERSISTENTOBJECTIDS; a volume whose answer is
// not valid keeps its ids.
func persists(caps volCapabilities) bool {
	valid := caps.valid[volCapabilitiesFormat]
	return valid&volCapPersistentIDs == 0 || valid&caps.capabilities[volCapabilitiesFormat]&volCapPersistentIDs != 0
}

var volumeCapabilities = attrlist{bitmapCount: 5, volume: attrVolInfo | attrVolCapabilities}

// volCapabilities is the length getattrlist returns followed by
// vol_capabilities_attr_t from <sys/attr.h>.
type volCapabilities struct {
	length       uint32
	capabilities [4]uint32
	valid        [4]uint32
}

const (
	attrVolInfo           = 0x80000000 // ATTR_VOL_INFO
	attrVolCapabilities   = 0x00020000 // ATTR_VOL_CAPABILITIES
	volCapabilitiesFormat = 0          // VOL_CAPABILITIES_FORMAT
	volCapPersistentIDs   = 0x00000001 // VOL_CAP_FMT_PERSISTENTOBJECTIDS
)

func fgetattrlist(fd int, al *attrlist, buf unsafe.Pointer, size uintptr) error {
	_, _, errno := syscall_syscall6(libc_fgetattrlist_trampoline_addr,
		uintptr(fd), uintptr(unsafe.Pointer(al)), uintptr(buf), size, 0, 0)
	runtime.KeepAlive(buf)
	if errno != 0 {
		return errno
	}
	return nil
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

// getdirentries64 is libSystem's __getdirentries64, the private entry point
// readdir is built on: it fills buf from the descriptor's offset and writes
// the offset it started from to pos, which must not be nil.
func getdirentries64(fd int, buf []byte, pos *int64) (int, error) {
	r, _, errno := syscall_syscall6(libc___getdirentries64_trampoline_addr,
		uintptr(fd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(pos)), 0, 0)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(pos)
	if errno != 0 {
		return 0, errno
	}
	return int(int64(r)), nil
}

var libc___getdirentries64_trampoline_addr uintptr

//go:cgo_import_dynamic libc___getdirentries64 __getdirentries64 "/usr/lib/libSystem.B.dylib"

var libc_getattrlistbulk_trampoline_addr uintptr

//go:cgo_import_dynamic libc_getattrlistbulk getattrlistbulk "/usr/lib/libSystem.B.dylib"

var libc_fgetattrlist_trampoline_addr uintptr

//go:cgo_import_dynamic libc_fgetattrlist fgetattrlist "/usr/lib/libSystem.B.dylib"
