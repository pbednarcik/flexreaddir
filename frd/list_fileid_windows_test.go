package frd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileIDInfo is FILE_ID_INFO from <winbase.h>.
type fileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

// fileIndex is name's file id as its own handle reports it, in the form the
// listing gives: the 64-bit index when FILE_ID_INFO's high half is 0, else 0.
// The two calls are checked against each other, so the oracle is not one
// number read twice.
func fileIndex(t *testing.T, name string) uint64 {
	t.Helper()
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	var fii fileIDInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&fii)), uint32(unsafe.Sizeof(fii))); err != nil {
		t.Fatal(err)
	}
	lo := *(*uint64)(unsafe.Pointer(&fii.FileID[0]))
	hi := *(*uint64)(unsafe.Pointer(&fii.FileID[8]))
	if hi != 0 {
		return 0
	}
	var bhi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &bhi); err != nil {
		t.Fatal(err)
	}
	if idx := uint64(bhi.FileIndexHigh)<<32 | uint64(bhi.FileIndexLow); idx != lo {
		t.Fatalf("%s: FILE_ID_INFO %#x, file index %#x", name, lo, idx)
	}
	return lo
}

// onNTFS reports whether dir's volume is NTFS, where every file has a 64-bit id.
func onNTFS(t *testing.T, dir string) bool {
	t.Helper()
	h := openRaw(t, dir)
	defer closeRaw(h)
	var fsName [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformationByHandle(windows.Handle(h), nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))); err != nil {
		t.Fatal(err)
	}
	return windows.UTF16ToString(fsName[:]) == "NTFS"
}

// withFileIDClass lists with FILE_ID_EXTD_DIR_INFO for the test's duration.
func withFileIDClass(t *testing.T) {
	t.Helper()
	listClass = windows.FileIdExtdDirectoryInfo
	t.Cleanup(func() { listClass = windows.FileFullDirectoryInfo })
}

// The default class is FILE_FULL_DIR_INFO: it lists without FileIDs, and the
// same directory listed with FILE_ID_EXTD_DIR_INFO agrees with it in every
// other field. The id class costs a warm listing 5 to 9% (measured
// 2026-10-09), which is why it is not the default.
func TestTheDefaultClassListsWithoutFileIDs(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if listClass != windows.FileFullDirectoryInfo {
		t.Fatalf("the default class is %d, want FILE_FULL_DIR_INFO (%d)", listClass, windows.FileFullDirectoryInfo)
	}
	got := readDir(t, dir)
	withFileIDClass(t)
	want := readDir(t, dir)
	if len(got) != len(want) {
		t.Fatalf("default class listed %d entries, id class %d", len(got), len(want))
	}
	for n, e := range got {
		if e.FileID != 0 {
			t.Errorf("%s: the default class lists a FileID %#x", n, e.FileID)
		}
		w := want[n]
		w.FileID = 0
		if e != w {
			t.Errorf("%s: default class %+v, id class %+v", n, e, w)
		}
	}
}

func TestWindowsFileIDIsTheFilesOwnID(t *testing.T) {
	withFileIDClass(t)
	dir := t.TempDir()
	if !onNTFS(t, dir) {
		t.Skip("the temporary directory is not on NTFS")
	}
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(a, filepath.Join(dir, "a-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := os.Symlink("a", filepath.Join(dir, "sym")) == nil
	got := readDir(t, dir)
	for _, n := range []string{"a", "b", "a-link"} {
		if own := fileIndex(t, filepath.Join(dir, n)); got[n].FileID == 0 || got[n].FileID != own {
			t.Errorf("%s: listed FileID %#x, its own %#x", n, got[n].FileID, own)
		}
	}
	if got["a"].FileID == got["b"].FileID {
		t.Errorf("two files share the FileID %#x", got["a"].FileID)
	}
	if got["a"].FileID != got["a-link"].FileID {
		t.Errorf("a hard link's FileID %#x differs from its target's %#x", got["a-link"].FileID, got["a"].FileID)
	}
	if got["sub"].FileID != 0 || link && got["sym"].FileID != 0 {
		t.Errorf("a directory or a link carries a FileID: %+v %+v", got["sub"], got["sym"])
	}
}

// Every buffer of a big listing uses the class the first one did.
func TestEveryFileOfABigDirectoryCarriesItsID(t *testing.T) {
	withFileIDClass(t)
	dir := t.TempDir()
	if !onNTFS(t, dir) {
		t.Skip("the temporary directory is not on NTFS")
	}
	const files = 1500 // 1,500 records of 88 bytes and a 90-byte name exceed 64 KiB
	for i := range files {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("a-name-long-enough-to-fill-the-buffer-%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := readDir(t, dir)
	if len(got) != files {
		t.Fatalf("listed %d of %d", len(got), files)
	}
	for n, e := range got {
		if e.FileID == 0 {
			t.Fatalf("%s has no FileID", n)
		}
	}
}

func TestARefusedFileIDClassFallsBackToTheFullListing(t *testing.T) {
	withFileIDClass(t)
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ntfs := onNTFS(t, dir)
	want := readDir(t, dir)
	refuseFileIDWith = windows.ERROR_INVALID_PARAMETER
	t.Cleanup(func() { refuseFileIDWith = 0 })
	got := readDir(t, dir)
	if len(got) != len(want) {
		t.Fatalf("fallback listed %d entries, want %d", len(got), len(want))
	}
	for n, e := range want {
		if ntfs && e.FileID == 0 {
			t.Fatalf("%s: the first listing has no FileID, so this test proves nothing", n)
		}
		e.FileID = 0
		if got[n] != e {
			t.Errorf("%s: fallback %+v, want %+v", n, got[n], e)
		}
	}
}

// A filesystem may refuse the class with a code no one measured (a filter
// driver, a user-mode filesystem); the listing must still work wherever the
// old class does.
func TestAnyFirstCallFailureFallsBack(t *testing.T) {
	withFileIDClass(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, code := range []syscall.Errno{windows.ERROR_INVALID_PARAMETER, windows.ERROR_CALL_NOT_IMPLEMENTED, windows.ERROR_ACCESS_DENIED} {
		refuseFileIDWith = code
		got := readDir(t, dir)
		refuseFileIDWith = 0
		if len(got) != 1 || got["a"].Size != 1 {
			t.Errorf("refused with %v: listed %+v", code, got)
		}
	}
}

// An empty root's ERROR_FILE_NOT_FOUND is not a refusal.
func TestOnlyARefusalTriggersTheFallback(t *testing.T) {
	for _, err := range []error{windows.ERROR_INVALID_PARAMETER, windows.ERROR_NOT_SUPPORTED, windows.ERROR_INVALID_FUNCTION, windows.ERROR_CALL_NOT_IMPLEMENTED, windows.ERROR_ACCESS_DENIED} {
		if !refused(err) {
			t.Errorf("%v is not taken as a refusal", err)
		}
	}
	for _, err := range []error{nil, syscall.ERROR_FILE_NOT_FOUND, syscall.ERROR_NO_MORE_FILES} {
		if refused(err) {
			t.Errorf("%v is taken as a refusal", err)
		}
	}
}

// A FAT volume refuses FILE_ID_EXTD_DIR_INFO (measured 2026-10-09). Set
// FRD_FAT_DIR to a directory on one to run this.
func TestAFATVolumeListsWithoutFileIDs(t *testing.T) {
	withFileIDClass(t)
	dir := os.Getenv("FRD_FAT_DIR")
	if dir == "" {
		t.Skip("FRD_FAT_DIR is not set")
	}
	got := readDir(t, dir)
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(des) {
		t.Fatalf("listed %d, os.ReadDir %d", len(got), len(des))
	}
	for n, e := range got {
		if e.FileID != 0 {
			t.Errorf("%s: FileID %#x on FAT", n, e.FileID)
		}
	}
}
