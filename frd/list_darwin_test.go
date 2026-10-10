package frd

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
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
	defer f.Close()
	_, err = AppendDir(nil, f)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != listOp || pe.Path != dir || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("AppendDir = %v, want a %s permission error naming the directory", err, listOp)
	}
}

// FAT keeps no file ids and FSKit makes them up, a new one for an empty file
// on every listing, so the volume says its ids do not persist and the listing
// reports none. The test attaches a FAT disk image with hdiutil.
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
	t.Cleanup(func() { exec.Command(hdiutil, "detach", "-quiet", "-force", mnt).Run() })
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

// A volume that cannot be asked keeps its ids for this listing, and is asked
// again by the next one rather than remembered.
func TestAVolumeThatCannotBeAskedIsNotRemembered(t *testing.T) {
	const fsid = 1<<63 | 42
	if !persistentIDs(-1, fsid) {
		t.Fatal("a volume that cannot be asked lost its ids")
	}
	if _, remembered := volumes.Load(uint64(fsid)); remembered {
		t.Fatal("the failed answer was cached")
	}
}

// /dev/fd holds the caller's own descriptors. The bulk call reports them as
// no type it names and fails at one lstat cannot describe either, such as a
// kqueue, as an error naming that entry.
func TestDevFdListsIrregularEntriesAndStopsAtAKqueue(t *testing.T) {
	kq, err := unix.Kqueue()
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(kq)
	f, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
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
