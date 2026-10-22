//go:build darwin

package frd

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Volume answers what the volume holding fd can do, from one fgetattrlist on
// fd. It is macOS only. fd may be open on a directory or a file; the error is
// an *os.PathError from the call: EBADF for a closed descriptor, EINVAL for
// one that is not a file or directory (a pipe, a socket, a kqueue), ENOTSUP
// for a volume whose answer leaves the capabilities out. A failure is never
// turned into a default: the caller chooses its own.
func Volume(fd uintptr) (VolumeCaps, error) {
	caps, err := volume(int(fd))
	if err != nil {
		return VolumeCaps{}, &os.PathError{Op: volumeOp, Err: err}
	}
	return caps, nil
}

// VolumeCaps is the format set of a volume's capabilities: the bits the
// volume reports, and the bits it says are meaningful. Each method answers
// one bit as (set, known); a caller chooses what an unknown bit means.
type VolumeCaps struct {
	Capabilities uint32 // VOL_CAPABILITIES_FORMAT, as reported
	Valid        uint32 // the bits of Capabilities the volume vouches for
}

// PersistentIDs is whether the volume keeps a file's id across runs, so a
// listed FileID can be trusted. APFS does; FAT and exFAT do not.
func (c VolumeCaps) PersistentIDs() (set, known bool) { return c.bit(volCapPersistentIDs) }

// CaseSensitive is whether two names differing only in case are two files.
func (c VolumeCaps) CaseSensitive() (set, known bool) { return c.bit(volCapCaseSensitive) }

// DirHardLinks is whether a directory can have more than one link.
func (c VolumeCaps) DirHardLinks() (set, known bool) { return c.bit(volCapDirHardLinks) }

func (c VolumeCaps) bit(mask uint32) (set, known bool) {
	known = c.Valid&mask != 0
	return known && c.Capabilities&mask != 0, known
}

const volumeOp = "fgetattrlist"

// volume asks the kernel for the capabilities of the volume holding fd,
// returning its errno bare.
func volume(fd int) (VolumeCaps, error) {
	var caps volCapabilities
	for {
		err := fgetattrlist(fd, &volumeCapabilities, unsafe.Pointer(&caps), unsafe.Sizeof(caps))
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return VolumeCaps{}, err
		}
		return volumeFrom(caps)
	}
}

// volumeFrom reads the format set of an answer, which must be of the full
// length: a shorter one omits the attribute, and no default stands in for it.
func volumeFrom(caps volCapabilities) (VolumeCaps, error) {
	if caps.length < uint32(unsafe.Sizeof(caps)) {
		return VolumeCaps{}, syscall.ENOTSUP
	}
	return VolumeCaps{
		Capabilities: caps.capabilities[volCapabilitiesFormat],
		Valid:        caps.valid[volCapabilitiesFormat],
	}, nil
}

var volumeCapabilities = attrlist{bitmapCount: 5, volume: attrVolInfo | attrVolCapabilities}

// volCapabilities is the length getattrlist returns followed by
// vol_capabilities_attr_t from <sys/attr.h>.
type volCapabilities struct {
	length       uint32
	capabilities [4]uint32
	valid        [4]uint32
}

// DirLinks is the link count of the directory fd, from one fgetattrlist on
// fd: ATTR_DIR_LINKCOUNT, the directory's own hard-link count, not st_nlink,
// which on APFS is its entry count plus two. It is macOS only. The error is an
// *os.PathError: ENOTDIR for a file's or a symlink's descriptor, EINVAL for a
// pipe, a socket or a kqueue, ENOTSUP for a volume that does not report the
// count, and any other errno the call or fstat answers.
func DirLinks(fd uintptr) (uint32, error) {
	var got dirLinkCount
	for {
		err := fgetattrlist(int(fd), &dirLinkCountAttrs, unsafe.Pointer(&got), unsafe.Sizeof(got))
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return 0, &os.PathError{Op: volumeOp, Err: err}
		}
		break
	}
	var statErr error
	var isDir bool
	if got.length < uint32(unsafe.Sizeof(got)) {
		var st unix.Stat_t
		statErr = unix.Fstat(int(fd), &st)
		isDir = st.Mode&unix.S_IFMT == unix.S_IFDIR
	}
	n, err := dirLinksFrom(got, statErr, isDir)
	if err != nil {
		return 0, &os.PathError{Op: volumeOp, Err: err}
	}
	return n, nil
}

// dirLinksFrom reads the count from an answer of the full length. A shorter
// one omits the attribute, and fstat's answer for fd says why: its own error
// (a volume force-unmounted under fd answers short, then EBADF), a directory
// on a volume that does not report the count, or anything else, which is not
// a directory.
func dirLinksFrom(got dirLinkCount, statErr error, isDir bool) (uint32, error) {
	switch {
	case got.length >= uint32(unsafe.Sizeof(got)):
		return got.count, nil
	case statErr != nil:
		return 0, statErr
	case isDir:
		return 0, syscall.ENOTSUP
	default:
		return 0, syscall.ENOTDIR
	}
}

var dirLinkCountAttrs = attrlist{bitmapCount: 5, directory: attrDirLinkCount}

// dirLinkCount is the length getattrlist returns followed by the
// ATTR_DIR_LINKCOUNT u_int32_t.
type dirLinkCount struct {
	length, count uint32
}

const (
	attrVolInfo           = 0x80000000 // ATTR_VOL_INFO
	attrVolCapabilities   = 0x00020000 // ATTR_VOL_CAPABILITIES
	volCapabilitiesFormat = 0          // VOL_CAPABILITIES_FORMAT
	volCapPersistentIDs   = 0x00000001 // VOL_CAP_FMT_PERSISTENTOBJECTIDS
	volCapCaseSensitive   = 0x00000100 // VOL_CAP_FMT_CASE_SENSITIVE
	volCapDirHardLinks    = 0x00040000 // VOL_CAP_FMT_DIR_HARDLINKS
	attrDirLinkCount      = 0x00000001 // ATTR_DIR_LINKCOUNT
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
