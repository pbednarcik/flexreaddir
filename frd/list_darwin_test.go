package frd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
	mnt := mountedImage(t, "MS-DOS")
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

// mountedImage makes an 8 MB disk image of the file system fs (an hdiutil
// -fs name such as "MS-DOS"), attaches it for the test's life and returns its
// mount point. It skips the test under -short, and where hdiutil is missing or
// fails. A test may detach the image itself.
func mountedImage(t *testing.T, fs string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("attaches a disk image")
	}
	hdiutil, err := exec.LookPath("hdiutil")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	img, mnt := filepath.Join(dir, "image.dmg"), filepath.Join(dir, "mnt")
	if out, err := exec.Command(hdiutil, "create", "-quiet", "-size", "8m", "-fs", fs, "-volname", "FRD", img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil create -fs %s: %v: %s", fs, err, out)
	}
	if out, err := exec.Command(hdiutil, "attach", "-quiet", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil attach: %v: %s", err, out)
	}
	t.Cleanup(func() {
		if !mounted(mnt) {
			return
		}
		if out, err := exec.Command(hdiutil, "detach", "-quiet", "-force", mnt).CombinedOutput(); err != nil {
			t.Errorf("hdiutil detach: %v: %s", err, out)
		}
	})
	return mnt
}

// mounted is whether a volume is mounted at mnt: its file system is not that
// of the directory holding it.
func mounted(mnt string) bool {
	var at, above unix.Statfs_t
	if unix.Statfs(mnt, &at) != nil || unix.Statfs(filepath.Dir(mnt), &above) != nil {
		return false
	}
	return at.Fsid != above.Fsid
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
		caps := VolumeCaps{Capabilities: c.capabilities, Valid: c.valid}
		if got := persists(caps); got != c.want {
			t.Errorf("%s: persists = %v, want %v", name, got, c.want)
		}
	}
}

// A volume that cannot be asked gives no answer for this listing, so no ids
// and no end skipped, and is asked again by the next one rather than
// remembered.
func TestAVolumeThatCannotBeAskedGivesNoIDsAndIsNotRemembered(t *testing.T) {
	const fsid = 1<<63 | 42
	if v, ok := volumeFor(-1, fsid); ok || v != (volumeInfo{}) {
		t.Fatalf("volumeFor(-1) = %+v, %v; want nothing", v, ok)
	}
	if _, remembered := volumes.Load(uint64(fsid)); remembered {
		t.Fatal("the failed answer was cached")
	}
}

// A volume asked once is remembered whole, so a later question about any
// other capability needs no second call.
func TestTheCacheHoldsTheWholeAnswer(t *testing.T) {
	const fsid = 1<<63 | 43
	fd := openRaw(t, t.TempDir())
	defer mustCloseRaw(t, fd)
	volumes.Delete(uint64(fsid))
	defer volumes.Delete(uint64(fsid))
	if v, ok := volumeFor(int(fd), fsid); !ok || !persists(v.caps) || !v.apfs {
		t.Fatalf("volumeFor = %+v, %v; the tests' volume is APFS and keeps its ids", v, ok)
	}
	v, ok := volumes.Load(uint64(fsid))
	info, isInfo := v.(volumeInfo)
	if !ok || !isInfo {
		t.Fatalf("cached %T %v, want a volumeInfo", v, v)
	}
	if set, known := info.caps.CaseSensitive(); set || !known {
		t.Errorf("the cached answer lost the case bit: %v, %v", set, known)
	}
	if !info.apfs {
		t.Error("the cached answer lost the volume's type")
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

// The largest record the listing's attributes can produce: the length and
// the five returned groups (24 bytes), the error (4), the name's reference
// and the fsid (8 each), the object type (4), the two times (16 each), the
// file id and the data length (8 each), 96 in all; then the name, at most
// NAME_MAX * 3 + 1 bytes with its NUL, 766 padded to 768. 864 is a multiple
// of 8, as every record's length is.
func TestTheLargestRecordIs864Bytes(t *testing.T) {
	if got := recordBound(requested); got != 864 {
		t.Fatalf("recordBound(requested) = %d, want 864", got)
	}
}

// An attribute recordBound cannot size gives a bound no buffer meets, so no
// end call is skipped.
func TestAnAttributeOfUnknownSizeLeavesNoRoomForTheLargestRecord(t *testing.T) {
	const devID = 0x00000002 // ATTR_CMN_DEVID
	for name, al := range map[string]attrlist{
		"a common one":    {bitmapCount: 5, common: requested.common | devID, file: requested.file},
		"a directory one": {bitmapCount: 5, common: requested.common, directory: attrDirLinkCount, file: requested.file},
	} {
		if got := recordBound(al); got <= bufferBytes {
			t.Errorf("%s: recordBound = %d, want more than the %d-byte buffer", name, got, bufferBytes)
		}
	}
}

// bulkCalls lists dir through the package's bulk loop, counting the calls:
// it returns the entries and the records each call returned.
func bulkCalls(t *testing.T, dir string) ([]Entry, []int) {
	t.Helper()
	fd := openRaw(t, dir)
	defer mustCloseRaw(t, fd)
	var calls []int
	count := func(fd int, al *attrlist, buf []byte) (int, error) {
		n, err := getattrlistbulk(fd, al, buf)
		if err == nil {
			calls = append(calls, n)
		}
		return n, err
	}
	got, err := readBulk(nil, int(fd), make([]byte, bufferBytes), count)
	if err != nil {
		t.Fatal(err)
	}
	return got, calls
}

// makeFiles creates n empty files in dir, named by name(i).
func makeFiles(t *testing.T, dir string, n int, name func(int) string) {
	t.Helper()
	for i := range n {
		if err := os.WriteFile(filepath.Join(dir, name(i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// listsWhole fails the test unless got holds exactly the n names name(i).
func listsWhole(t *testing.T, got []Entry, n int, name func(int) string) {
	t.Helper()
	seen := make(map[string]bool, len(got))
	for _, e := range got {
		seen[e.Name] = true
	}
	if len(got) != n || len(seen) != n {
		t.Fatalf("listed %d entries, %d names; want %d", len(got), len(seen), n)
	}
	for i := range n {
		if !seen[name(i)] {
			t.Fatalf("%s is missing", name(i))
		}
	}
}

func shortName(i int) string { return fmt.Sprintf("file%04d.go", i) }

// variedName gives records of 104 to 344 bytes, so a full call leaves up to
// a few hundred bytes free, less than the record that did not fit.
func variedName(i int) string { return fmt.Sprintf("%04d-%s", i, strings.Repeat("x", i*37%240)) }

// On APFS a call that left room for the largest record was the directory's
// last, so a directory that fits one buffer takes one call: 576 records of
// 112 bytes leave 1,024 of the 65,536.
func TestAnAPFSDirectoryThatFitsOneBufferTakesOneCall(t *testing.T) {
	dir := t.TempDir()
	const files = 576
	makeFiles(t, dir, files, shortName)
	got, calls := bulkCalls(t, dir)
	listsWhole(t, got, files, shortName)
	if !slices.Equal(calls, []int{files}) {
		t.Fatalf("the calls returned %v records, want one call of %d", calls, files)
	}
}

// A directory of several buffers lists whole on APFS, and its last call,
// short, ends it: no call returns 0.
func TestAnAPFSDirectoryOfSeveralBuffersListsWholeWithoutTheEndCall(t *testing.T) {
	dir := t.TempDir()
	const files = 3000
	makeFiles(t, dir, files, variedName)
	got, calls := bulkCalls(t, dir)
	listsWhole(t, got, files, variedName)
	if len(calls) < 2 || slices.Contains(calls, 0) {
		t.Fatalf("the calls returned %v records, want several and none of 0", calls)
	}
}

// HFS+ returns short batches before a directory's end, so there the listing
// asks until a call returns 0.
func TestAnHFSPlusDirectoryListsWhole(t *testing.T) {
	dir := filepath.Join(mountedImage(t, "HFS+"), "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const files = 500
	makeFiles(t, dir, files, shortName)
	got, calls := bulkCalls(t, dir)
	listsWhole(t, got, files, shortName)
	if calls[len(calls)-1] != 0 {
		t.Fatalf("the calls returned %v records, want the last to return 0", calls)
	}
	if got := readDir(t, dir); len(got) != files {
		t.Fatalf("AppendDir listed %d entries, want %d", len(got), files)
	}
}

// FAT is not APFS and lists whole; case-sensitive APFS is, and ends its
// listing at the short call. FAT keeps each file's extended attributes in a
// "._" file beside it, so the files listed are those os.ReadDir lists.
func TestAFATAndACaseSensitiveAPFSDirectoryListWhole(t *testing.T) {
	for _, c := range []struct {
		fs   string
		apfs bool
	}{{"MS-DOS", false}, {"Case-sensitive APFS", true}} {
		dir := filepath.Join(mountedImage(t, c.fs), "sub")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		const files = 500
		makeFiles(t, dir, files, shortName)
		want, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, calls := bulkCalls(t, dir)
		listsWhole(t, got, len(want), func(i int) string { return want[i].Name() })
		if endCall := calls[len(calls)-1] == 0; endCall == c.apfs {
			t.Errorf("%s: the calls returned %v records", c.fs, calls)
		}
		if got := readDir(t, dir); len(got) != files {
			t.Errorf("%s: AppendDir listed %d files, want %d", c.fs, len(got), files)
		}
	}
}

// untilZero lists dir by calling getattrlistbulk until it returns 0.
func untilZero(t *testing.T, dir string) []Entry {
	t.Helper()
	fd := openRaw(t, dir)
	defer mustCloseRaw(t, fd)
	buf := make([]byte, bufferBytes)
	var got []Entry
	for {
		n, err := getattrlistbulk(int(fd), &requested, buf)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return got
		}
		if got, _, err = parseRecords(got, buf, n, keepIDs); err != nil {
			t.Fatal(err)
		}
	}
}

// cjkName is k copies of one of eight three-byte characters: 255 of them,
// 765 bytes, the longest name APFS takes, make the largest record, 864 bytes.
func cjkName(i int) string {
	return strings.Repeat(string(rune('中'+i/255)), i%255+1)
}

// Records of 104 to 864 bytes leave full calls up to 863 bytes free, so a
// bound short of the largest record ends a listing early. APFS refuses a
// name of 256 three-byte characters, which keeps the bound at 864.
func TestAnAPFSDirectoryOfTheLongestNamesListsWhole(t *testing.T) {
	dir := t.TempDir()
	const files = 8 * 255
	makeFiles(t, dir, files, cjkName)
	got, calls := bulkCalls(t, dir)
	listsWhole(t, got, files, cjkName)
	if len(calls) < 2 || slices.Contains(calls, 0) {
		t.Fatalf("the calls returned %v records, want several and none of 0", calls)
	}
	names := func(es []Entry) []string {
		var s []string
		for _, e := range es {
			s = append(s, e.Name)
		}
		slices.Sort(s)
		return s
	}
	if !slices.Equal(names(got), names(untilZero(t, dir))) {
		t.Fatal("the listing differs from the one that calls until 0")
	}
	err := os.WriteFile(filepath.Join(dir, strings.Repeat("中", 256)), nil, 0o644)
	if !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Fatalf("a name of 768 bytes: %v, want ENAMETOOLONG", err)
	}
}
