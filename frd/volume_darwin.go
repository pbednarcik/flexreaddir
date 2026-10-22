//go:build darwin

package frd

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// Volume answers what the volume holding fd can do, from one fgetattrlist on
// fd. It is macOS only. fd may be open on a directory or a file; the error is
// an *os.PathError from the call, and a failure is never turned into a
// default: the caller chooses its own.
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
func (c VolumeCaps) PersistentIDs() (set, known bool) {
	const bit = volCapPersistentIDs
	return c.Capabilities&bit != 0 && c.Valid&bit != 0, c.Valid&bit != 0
}

// CaseSensitive is whether two names differing only in case are two files.
func (c VolumeCaps) CaseSensitive() (set, known bool) {
	const bit = volCapCaseSensitive
	return c.Capabilities&bit != 0 && c.Valid&bit != 0, c.Valid&bit != 0
}

// DirHardLinks is whether a directory can have more than one link.
func (c VolumeCaps) DirHardLinks() (set, known bool) {
	const bit = volCapDirHardLinks
	return c.Capabilities&bit != 0 && c.Valid&bit != 0, c.Valid&bit != 0
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

const (
	attrVolInfo           = 0x80000000 // ATTR_VOL_INFO
	attrVolCapabilities   = 0x00020000 // ATTR_VOL_CAPABILITIES
	volCapabilitiesFormat = 0          // VOL_CAPABILITIES_FORMAT
	volCapPersistentIDs   = 0x00000001 // VOL_CAP_FMT_PERSISTENTOBJECTIDS
	volCapCaseSensitive   = 0x00000100 // VOL_CAP_FMT_CASE_SENSITIVE
	volCapDirHardLinks    = 0x00040000 // VOL_CAP_FMT_DIR_HARDLINKS
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
