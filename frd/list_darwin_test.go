package frd

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// getattrlistbulk refuses, as a whole, a directory that may be read but not
// searched, even an empty one, so the error names the directory.
func TestADirectoryThatCannotBeSearchedIsAPathErrorNamingIt(t *testing.T) {
	dir := t.TempDir()
	refuseSearch(t, dir)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	_, err = AppendDir(nil, f)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != listOp || pe.Path != dir || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("AppendDir = %v, want a %s permission error naming the directory", err, listOp)
	}
}

// A FAT volume keeps no file ids (FSKit gives an empty file a new one on
// every listing), so it reports them as not persistent and the listing gives
// none. The test attaches a FAT disk image with hdiutil.
func TestAVolumeWhoseIDsDoNotPersistListsNoFileIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("attaches a disk image")
	}
	hdiutil, err := exec.LookPath("hdiutil")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	img, mnt := filepath.Join(dir, "fat.dmg"), filepath.Join(dir, "mnt")
	if out, err := exec.Command(hdiutil, "create", "-quiet", "-size", "8m", "-fs", "MS-DOS", "-volname", "FRD", img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil create: %v: %s", err, out)
	}
	if out, err := exec.Command(hdiutil, "attach", "-quiet", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil attach: %v: %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command(hdiutil, "detach", "-quiet", "-force", mnt).CombinedOutput(); err != nil {
			t.Errorf("hdiutil detach: %v: %s", err, out)
		}
	})
	sub := filepath.Join(mnt, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{sub, mnt} {
		for name, data := range map[string]string{"empty": "", "full": "data"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The volume is asked from the first directory listed on it, here one
	// below its root, so the cache must not hold an answer already.
	volumes.Clear()
	for _, dir := range []string{sub, mnt} {
		got := readDir(t, dir)
		for name, size := range map[string]int64{"empty": 0, "full": 4} {
			if e := got[name]; e.Size != size || e.ModTimeNanos == 0 || e.FileID != 0 {
				t.Errorf("%s: %s = %+v, want size %d, a time and no file id", dir, name, e, size)
			}
		}
	}
}

func TestTheVolumeCapabilitiesSayWhetherIDsPersist(t *testing.T) {
	for name, c := range map[string]struct {
		capabilities, valid uint32
		want                bool
	}{
		"valid and set":   {volCapPersistentIDs, volCapPersistentIDs, true},
		"valid and clear": {0, volCapPersistentIDs, false},
		"not valid":       {0, 0, true},
	} {
		caps := volCapabilities{capabilities: [4]uint32{c.capabilities}, valid: [4]uint32{c.valid}}
		if got := persists(caps); got != c.want {
			t.Errorf("%s: persists = %v, want %v", name, got, c.want)
		}
	}
}

// A volume that cannot be asked gives no ids for this listing, and is asked
// again by the next one rather than remembered.
func TestAVolumeThatCannotBeAskedGivesNoIDsAndIsNotRemembered(t *testing.T) {
	const fsid = 1<<63 | 42
	if persistentIDs(-1, fsid) {
		t.Fatal("a volume that cannot be asked kept its ids")
	}
	if _, remembered := volumes.Load(uint64(fsid)); remembered {
		t.Fatal("the failed answer was cached")
	}
}

// In /dev/fd the bulk call reports every entry as irregular and fails, naming
// the entry, at a descriptor lstat cannot describe either, such as a kqueue.
func TestDevFdListsIrregularEntriesAndStopsAtAKqueue(t *testing.T) {
	kq, err := unix.Kqueue()
	if err != nil {
		t.Fatal(err)
	}
	defer mustCloseRaw(t, uintptr(kq))
	f, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	entries, err := AppendDir(nil, f)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != listOp || filepath.Dir(pe.Path) != "/dev/fd" || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("AppendDir(/dev/fd) error = %v, want a %s EBADF naming an entry of /dev/fd", err, listOp)
	}
	for _, e := range entries {
		if e.Type != fs.ModeIrregular {
			t.Errorf("%s is %v, want irregular", e.Name, e.Type)
		}
	}
}

// idsPersist reports whether the volume holding dir keeps its file ids, by
// its file system's name rather than the capability the package asks for.
func idsPersist(t *testing.T, dir string) bool {
	t.Helper()
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		t.Fatal(err)
	}
	switch unix.ByteSliceToString(st.Fstypename[:]) {
	case "msdos", "exfat", "nfs":
		return false
	}
	return true
}

// getdirentries64 needs list permission only, so a Names listing of a
// directory that may be read but not searched succeeds where the full one
// fails.
func TestADirectoryThatCannotBeSearchedListsItsNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	refuseSearch(t, dir)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	l := Lister{Names: true}
	entries, err := l.AppendDir(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Entry{{Name: "a.go"}}; !slices.Equal(entries, want) {
		t.Fatalf("Lister{Names: true}.AppendDir = %+v, want %+v", entries, want)
	}
}

// getdirentries64 answers EINVAL for a file, where the bulk call answers
// ENOTDIR; a Names listing says what the full one says.
func TestARegularFileUnderNamesIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	l := Lister{Names: true}
	_, err = l.AppendDir(nil, f)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != namesOp || pe.Path != file || !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("Lister{Names: true}.AppendDir on a file = %v, want a %s *os.PathError naming it and wrapping ENOTDIR", err, namesOp)
	}
}

// A Names listing's errors name its own call, the ones the Lister makes
// before any listing included.
func TestAClosedOrNilDirectoryUnderNamesNamesItsCall(t *testing.T) {
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustClose(t, f)
	l := Lister{Names: true}
	_, err = l.AppendDir(nil, f)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Op != namesOp || pe.Path != f.Name() || !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Lister{Names: true}.AppendDir on a closed directory = %v, want a %s *os.PathError naming it and wrapping fs.ErrClosed", err, namesOp)
	}
	_, err = l.AppendDir(nil, nil)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Op != namesOp || !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("Lister{Names: true}.AppendDir(nil, nil) = %v, want a %s *os.PathError wrapping os.ErrInvalid", err, namesOp)
	}
}
