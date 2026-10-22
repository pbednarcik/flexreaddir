//go:build darwin

package frd

import (
	"errors"
	"math"
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
	return readBulk(dst, fd, buf, getattrlistbulk)
}

// readBulk lists fd by bulk, which is getattrlistbulk; the tests pass one
// that counts the calls.
func readBulk(dst []Entry, fd int, buf []byte, bulk func(int, *attrlist, []byte) (int, error)) ([]Entry, error) {
	var apfs bool
	persistent := func(fsid uint64) bool {
		v, ok := volumeFor(fd, fsid)
		apfs = v.apfs
		return ok && persists(v.caps)
	}
	for {
		n, err := bulk(fd, &requested, buf)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			return dst, err
		}
		var end int
		if dst, end, err = parseRecords(dst, buf, n, persistent); err != nil {
			return dst, err
		}
		// APFS is seen to pack records while one more fits, which
		// getattrlistbulk(2) does not promise, so a call that left room for
		// the largest stopped at the directory's end, and the next would
		// return 0 from the end the descriptor keeps. Elsewhere, HFS+ and FAT
		// among them, a call can return a short batch before the end.
		if apfs && len(buf)-end >= largestRecord {
			return dst, nil
		}
	}
}

// largestRecord is the most bytes one record of requested takes.
var largestRecord = recordBound(requested)

// recordBound is the most bytes one getattrlistbulk record of al can take:
// the length and the returned groups, each fixed-size attribute al asks for,
// and a name of at most NAME_MAX * 3 + 1 bytes with its NUL (getattrlist(2)),
// padded to 4, the whole padded to 8. An attribute it cannot size gives a
// bound no buffer meets.
func recordBound(al attrlist) int {
	const nameMax = 255 // NAME_MAX
	sizes := [...]struct {
		common, file uint32
		bytes        int
	}{
		{attrReturned, 0, 0}, // the groups, counted in recordHeader
		{attrError, 0, 4},
		{attrName, 0, 8 + (nameMax*3+1+3)&^3},
		{attrFsid, 0, 8},
		{attrObjtype, 0, 4},
		{attrModtime, 0, 16},
		{attrChgtime, 0, 16},
		{attrFileID, 0, 8},
		{0, attrDataLength, 8},
	}
	bound, common, file := recordHeader, al.common, al.file
	for _, s := range sizes {
		if common&s.common != 0 || file&s.file != 0 {
			bound += s.bytes
			common &^= s.common
			file &^= s.file
		}
	}
	if common|file|al.volume|al.directory|al.fork != 0 {
		return math.MaxInt
	}
	return (bound + 7) &^ 7
}

// readNames lists fd by getdirentries64, the call readdir makes, which reads
// the directory's own records and creates no vnode for the files it names,
// and stops at the end the kernel reports in the buffer's last four bytes,
// so a directory that fits one buffer costs one call. A DT_UNKNOWN record is
// typed by fstatat of the name, never following a link.
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
		if len(buf) >= extendedBufferSize {
			clear(buf[len(buf)-4:]) // a kernel that writes no flag word leaves no end claimed
		}
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
		if endOfDirectory(buf, n) {
			return dst, nil
		}
	}
}

// notDir reports whether fd is open on something other than a directory,
// which getdirentries64 refuses with EINVAL.
func notDir(fd int) bool {
	var st unix.Stat_t
	return unix.Fstat(fd, &st) == nil && st.Mode&unix.S_IFMT != unix.S_IFDIR
}

// volumes holds, by fsid, the volumeInfo of each volume seen. The
// capabilities' persistent-ids bit decides whether its file ids are listed:
// FAT32, exFAT and NFS clear VOL_CAP_FMT_PERSISTENTOBJECTIDS, and FSKit gives
// an empty FAT file a new id on every listing. The answer is kept for the life
// of the process, so a later volume given the same fsid takes it.
var volumes sync.Map

// volumeFor answers for the volume fsid, asking the kernel once per volume
// through the directory fd. The records of one listing carry one fsid, a
// mount point's included, which getattrlistbulk describes as the directory
// it covers; on the sealed system volume it is the APFS volume group's,
// shared with the Data volume. Its high word is the file system's type
// number, statfs's f_type, on every volume seen, so a volume given an
// earlier one's fsid is of the same type. A volume that cannot be asked gives
// no answer, ok false, and is not remembered, so the next listing asks again.
func volumeFor(fd int, fsid uint64) (v volumeInfo, ok bool) {
	if v, ok := volumes.Load(fsid); ok {
		return v.(volumeInfo), true
	}
	v, err := askVolumeInfo(fd)
	if err != nil {
		return volumeInfo{}, false
	}
	volumes.LoadOrStore(fsid, v)
	return v, true
}

// persists reads VOL_CAP_FMT_PERSISTENTOBJECTIDS; a volume whose answer is
// not valid keeps its ids.
func persists(caps VolumeCaps) bool {
	set, known := caps.PersistentIDs()
	return !known || set
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
