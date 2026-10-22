//go:build darwin

package frd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestEachCapabilityAnswersSetAndKnown(t *testing.T) {
	const all = volCapPersistentIDs | volCapCaseSensitive | volCapDirHardLinks
	for name, m := range map[string]struct {
		bit    uint32
		method func(VolumeCaps) (set, known bool)
	}{
		"PersistentIDs": {volCapPersistentIDs, VolumeCaps.PersistentIDs},
		"CaseSensitive": {volCapCaseSensitive, VolumeCaps.CaseSensitive},
		"DirHardLinks":  {volCapDirHardLinks, VolumeCaps.DirHardLinks},
	} {
		others := all &^ m.bit
		for row, c := range map[string]struct {
			capabilities, valid uint32
			set, known          bool
		}{
			"set and valid":   {m.bit, m.bit, true, true},
			"clear and valid": {0, m.bit, false, true},
			"not valid":       {m.bit, 0, false, false},
			"only the others": {others, others, false, false},
		} {
			caps := VolumeCaps{Capabilities: c.capabilities, Valid: c.valid}
			if set, known := m.method(caps); set != c.set || known != c.known {
				t.Errorf("%s, %s: %v, %v; want %v, %v", name, row, set, known, c.set, c.known)
			}
		}
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
	fd := openRaw(t, mountedImage(t, "MS-DOS"))
	defer mustCloseRaw(t, fd)
	caps, err := Volume(fd)
	if err != nil {
		t.Fatal(err)
	}
	if set, known := caps.PersistentIDs(); set || !known {
		t.Errorf("PersistentIDs = %v, %v on FAT; want false, true", set, known)
	}
}

func TestACaseSensitiveAPFSVolumeSaysSo(t *testing.T) {
	fd := openRaw(t, mountedImage(t, "Case-sensitive APFS"))
	defer mustCloseRaw(t, fd)
	caps, err := Volume(fd)
	if err != nil {
		t.Fatal(err)
	}
	if set, known := caps.CaseSensitive(); !set || !known {
		t.Errorf("CaseSensitive = %v, %v on case-sensitive APFS; want true, true", set, known)
	}
}

func TestAnHFSPlusVolumeAllowsDirectoryHardLinks(t *testing.T) {
	fd := openRaw(t, mountedImage(t, "HFS+"))
	defer mustCloseRaw(t, fd)
	caps, err := Volume(fd)
	if err != nil {
		t.Fatal(err)
	}
	if set, known := caps.DirHardLinks(); !set || !known {
		t.Errorf("DirHardLinks = %v, %v on HFS+; want true, true", set, known)
	}
}

// devfs leaves VOL_CAP_FMT_DIR_HARDLINKS out of the bits it vouches for.
func TestDevfsDoesNotVouchForDirectoryHardLinks(t *testing.T) {
	fd := openRaw(t, "/dev")
	defer mustCloseRaw(t, fd)
	caps, err := Volume(fd)
	if err != nil {
		t.Fatal(err)
	}
	if set, known := caps.DirHardLinks(); set || known {
		t.Errorf("DirHardLinks = %v, %v on devfs; want false, false", set, known)
	}
	if _, known := caps.PersistentIDs(); !known {
		t.Errorf("PersistentIDs not known on devfs (Valid %#x)", caps.Valid)
	}
}

func TestVolumeThroughSyscallConnMatchesTheRawDescriptor(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	conn, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got VolumeCaps
	if cerr := conn.Control(func(fd uintptr) { got, err = Volume(fd) }); cerr != nil {
		t.Fatal(cerr)
	}
	if err != nil {
		t.Fatal(err)
	}
	fd := openRaw(t, dir)
	defer mustCloseRaw(t, fd)
	if want, err := Volume(fd); err != nil || got != want {
		t.Errorf("Volume through SyscallConn = %+v; the raw descriptor's = %+v, %v", got, want, err)
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

func TestADirectoryOnAPFSHasOneLink(t *testing.T) {
	fd := openRaw(t, t.TempDir())
	defer mustCloseRaw(t, fd)
	if n, err := DirLinks(fd); err != nil || n != 1 {
		t.Fatalf("DirLinks = %d, %v; want 1, nil", n, err)
	}
}

func TestDirLinksOnAFileIsENOTDIR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	_, err = DirLinks(f.Fd())
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != volumeOp || pe.Path != "" || !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("DirLinks(file) = %v, want a %s ENOTDIR with no path", err, volumeOp)
	}
}

func TestDirLinksOnAClosedDescriptorIsEBADF(t *testing.T) {
	for _, fd := range []uintptr{^uintptr(0), 999} {
		_, err := DirLinks(fd)
		pe, ok := errors.AsType[*os.PathError](err)
		if !ok || pe.Op != volumeOp || pe.Path != "" || !errors.Is(err, syscall.EBADF) {
			t.Errorf("DirLinks(%d) = %v, want a %s EBADF with no path", fd, err, volumeOp)
		}
	}
}

// A pipe, a socket or a kqueue is not a vnode, and the kernel answers EINVAL.
func TestANonVnodeDescriptorIsEINVAL(t *testing.T) {
	var p [2]int
	if err := unix.Pipe(p[:]); err != nil {
		t.Fatal(err)
	}
	defer mustCloseRaw(t, uintptr(p[1]))
	defer mustCloseRaw(t, uintptr(p[0]))
	r := uintptr(p[0])
	if _, err := Volume(r); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Volume(pipe) = %v, want EINVAL", err)
	}
	if _, err := DirLinks(r); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("DirLinks(pipe) = %v, want EINVAL", err)
	}
}

// After a forced unmount under an open directory the kernel still answers the
// directory-set ask, short, and fstat answers EBADF: the error is that EBADF,
// as Volume's is, and never ENOTDIR.
func TestDirLinksAfterAForcedUnmountIsEBADF(t *testing.T) {
	mnt := mountedImage(t, "MS-DOS")
	sub := filepath.Join(mnt, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fd := openRaw(t, sub)
	t.Cleanup(func() { _ = closeRaw(fd) })
	if out, err := exec.Command("hdiutil", "detach", "-quiet", "-force", mnt).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil detach: %v: %s", err, out)
	}
	_, err := DirLinks(fd)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Op != volumeOp || !errors.Is(err, syscall.EBADF) {
		t.Errorf("DirLinks after a forced unmount = %v, want a %s EBADF", err, volumeOp)
	}
	if _, err := Volume(fd); !errors.Is(err, syscall.EBADF) {
		t.Errorf("Volume after a forced unmount = %v, want EBADF", err)
	}
}

func TestAnOmittedCountIsNotSupportedOnADirectory(t *testing.T) {
	short := dirLinkCount{length: 4}
	if _, err := dirLinksFrom(short, nil, true); !errors.Is(err, syscall.ENOTSUP) {
		t.Errorf("dirLinksFrom(short, dir) = %v, want ENOTSUP", err)
	}
	if _, err := dirLinksFrom(short, nil, false); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("dirLinksFrom(short, file) = %v, want ENOTDIR", err)
	}
	if _, err := dirLinksFrom(short, syscall.EBADF, false); !errors.Is(err, syscall.EBADF) {
		t.Errorf("dirLinksFrom(short, fstat EBADF) = %v, want EBADF", err)
	}
	full := dirLinkCount{length: 8, count: 3}
	if n, err := dirLinksFrom(full, nil, true); err != nil || n != 3 {
		t.Errorf("dirLinksFrom(full) = %d, %v; want 3, nil", n, err)
	}
}

func BenchmarkVolume(b *testing.B) {
	fd := openRaw(b, b.TempDir())
	defer mustCloseRaw(b, fd)
	for b.Loop() {
		if _, err := Volume(fd); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDirLinks(b *testing.B) {
	fd := openRaw(b, b.TempDir())
	defer mustCloseRaw(b, fd)
	for b.Loop() {
		if _, err := DirLinks(fd); err != nil {
			b.Fatal(err)
		}
	}
}

// The listing asks, with the capabilities, for the file system's name, and
// keeps whether it is APFS.
func TestTheListingKnowsWhichVolumesAreAPFS(t *testing.T) {
	for _, c := range []struct {
		name string
		dir  func() string
		apfs bool
	}{
		{"the tests' volume", t.TempDir, true},
		{"the system volume", func() string { return "/System/Library" }, true},
		{"case-sensitive APFS", func() string { return mountedImage(t, "Case-sensitive APFS") }, true},
		{"HFS+", func() string { return mountedImage(t, "HFS+") }, false},
		{"FAT", func() string { return mountedImage(t, "MS-DOS") }, false},
		{"devfs", func() string { return "/dev" }, false},
	} {
		fd := openRaw(t, c.dir())
		v, err := askVolumeInfo(int(fd))
		mustCloseRaw(t, fd)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if v.apfs != c.apfs {
			t.Errorf("%s: apfs = %v, want %v", c.name, v.apfs, c.apfs)
		}
		if _, known := v.caps.PersistentIDs(); !known {
			t.Errorf("%s: the capabilities are missing: %+v", c.name, v.caps)
		}
	}
}

// The type's name is read only where its reference places it within the
// answer, and only "apfs" is APFS; the capabilities follow volumeFrom's rule.
func TestTheFileSystemNameIsReadWithinTheAnswer(t *testing.T) {
	caps := volCapabilities{capabilities: [4]uint32{volCapPersistentIDs}, valid: [4]uint32{volCapPersistentIDs}}
	answer := func(length uint32, offset int32, size uint32, name string) volCapabilitiesAndType {
		a := volCapabilitiesAndType{volCapabilities: caps, typeName: attrReference{offset: offset, length: size}}
		a.length = length
		copy(a.name[:], name)
		return a
	}
	const full = uint32(unsafe.Sizeof(volCapabilitiesAndType{}))
	const ref = int32(unsafe.Offsetof(volCapabilitiesAndType{}.name) - unsafe.Offsetof(volCapabilitiesAndType{}.typeName))
	for name, c := range map[string]struct {
		a    volCapabilitiesAndType
		apfs bool
	}{
		"apfs":                           {answer(52, ref, 5, "apfs\x00"), true},
		"hfs":                            {answer(52, ref, 4, "hfs\x00"), false},
		"a longer name":                  {answer(52, ref, 6, "apfsx\x00"), false},
		"no NUL":                         {answer(52, ref, 4, "apfs"), false},
		"no type in the answer":          {answer(uint32(unsafe.Sizeof(volCapabilities{})), ref, 5, "apfs\x00"), false},
		"a name past the answer":         {answer(48, ref, 5, "apfs\x00"), false},
		"a name past the buffer":         {answer(full, ref+12, 5, "apfs\x00"), false},
		"a name before its reference":    {answer(52, -4, 5, "apfs\x00"), false},
		"a length the answer can't hold": {answer(52, ref, 1<<31, "apfs\x00"), false},
		"an empty name":                  {answer(52, ref, 0, "apfs\x00"), false},
	} {
		v, err := volumeInfoFrom(c.a)
		if err != nil || v.apfs != c.apfs || v.caps != (VolumeCaps{Capabilities: volCapPersistentIDs, Valid: volCapPersistentIDs}) {
			t.Errorf("%s: volumeInfoFrom = %+v, %v; want apfs %v and the capabilities", name, v, err, c.apfs)
		}
	}
	if _, err := volumeInfoFrom(answer(28, ref, 5, "apfs\x00")); !errors.Is(err, syscall.ENOTSUP) {
		t.Errorf("an answer without the capabilities: %v, want ENOTSUP", err)
	}
}
