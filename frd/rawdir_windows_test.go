package frd

import (
	"testing"

	"golang.org/x/sys/windows"
)

// openRaw opens path as a bare handle, without an os.File, the way a walker
// that manages its own handles would. FILE_FLAG_BACKUP_SEMANTICS is what
// lets CreateFile open a directory.
func openRaw(tb testing.TB, path string) (uintptr, func() error) {
	tb.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		tb.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		tb.Fatal(err)
	}
	return uintptr(h), func() error { return windows.CloseHandle(h) }
}
