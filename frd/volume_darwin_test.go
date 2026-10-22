//go:build darwin

package frd

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestEachCapabilityAnswersSetAndKnown(t *testing.T) {
	for name, c := range map[string]struct {
		capabilities, valid uint32
		set, known          bool
	}{
		"valid and set":   {volCapCaseSensitive, volCapCaseSensitive, true, true},
		"valid and clear": {0, volCapCaseSensitive, false, true},
		"not valid":       {volCapCaseSensitive, 0, false, false},
	} {
		caps := VolumeCaps{Capabilities: c.capabilities, Valid: c.valid}
		if set, known := caps.CaseSensitive(); set != c.set || known != c.known {
			t.Errorf("%s: CaseSensitive = %v, %v; want %v, %v", name, set, known, c.set, c.known)
		}
	}
	// the other two methods read their own bit and no other
	all := VolumeCaps{Capabilities: volCapPersistentIDs | volCapDirHardLinks, Valid: volCapPersistentIDs | volCapDirHardLinks}
	if set, known := all.PersistentIDs(); !set || !known {
		t.Errorf("PersistentIDs = %v, %v", set, known)
	}
	if set, known := all.DirHardLinks(); !set || !known {
		t.Errorf("DirHardLinks = %v, %v", set, known)
	}
	if set, known := all.CaseSensitive(); set || known {
		t.Errorf("CaseSensitive = %v, %v on a word without the bit", set, known)
	}
}

// The tests' volume is APFS: ids kept, names folded, no directory hard
// links, and the volume vouches for all three bits.
func TestTheTestsVolumeAnswersAsAPFS(t *testing.T) {
	fd := openRaw(t, t.TempDir())
	defer mustCloseRaw(t, fd)
	caps, err := Volume(fd)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		got func() (bool, bool)
		set bool
	}{
		"PersistentIDs": {caps.PersistentIDs, true},
		"CaseSensitive": {caps.CaseSensitive, false},
		"DirHardLinks":  {caps.DirHardLinks, false},
	} {
		if set, known := c.got(); set != c.set || !known {
			t.Errorf("%s = %v, %v; want %v, true", name, set, known, c.set)
		}
	}
	const three = volCapPersistentIDs | volCapCaseSensitive | volCapDirHardLinks
	if caps.Valid&three != three {
		t.Errorf("Valid = %#x, want the three bits %#x in it", caps.Valid, three)
	}
}

func TestAFATVolumeKeepsNoIDs(t *testing.T) {
	fd := openRaw(t, fatImage(t))
	defer mustCloseRaw(t, fd)
	caps, err := Volume(fd)
	if err != nil {
		t.Fatal(err)
	}
	if set, known := caps.PersistentIDs(); set || !known {
		t.Errorf("PersistentIDs = %v, %v on FAT; want false, true", set, known)
	}
}

func TestVolumeAnswersForAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	dfd := openRaw(t, dir)
	defer mustCloseRaw(t, dfd)
	want, err := Volume(dfd)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Volume(f.Fd()); err != nil || got != want {
		t.Errorf("Volume(file) = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestVolumeOnAClosedDescriptorIsEBADF(t *testing.T) {
	for _, fd := range []uintptr{^uintptr(0), 999} {
		_, err := Volume(fd)
		pe, ok := errors.AsType[*os.PathError](err)
		if !ok || pe.Op != volumeOp || pe.Path != "" || !errors.Is(err, syscall.EBADF) {
			t.Errorf("Volume(%d) = %v, want a %s EBADF with no path", fd, err, volumeOp)
		}
	}
}

func TestAShortAnswerIsNotSupported(t *testing.T) {
	short := volCapabilities{length: 4}
	if _, err := volumeFrom(short); !errors.Is(err, syscall.ENOTSUP) {
		t.Errorf("volumeFrom(length 4) = %v, want ENOTSUP", err)
	}
	full := volCapabilities{length: uint32(unsafe.Sizeof(volCapabilities{})), capabilities: [4]uint32{volCapPersistentIDs}, valid: [4]uint32{volCapPersistentIDs}}
	if got, err := volumeFrom(full); err != nil || got != (VolumeCaps{Capabilities: volCapPersistentIDs, Valid: volCapPersistentIDs}) {
		t.Errorf("volumeFrom(full) = %+v, %v", got, err)
	}
}
