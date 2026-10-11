package frd

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileBasicInfo is FILE_BASIC_INFO from <winbase.h>; x/sys/windows has the
// information class but not the structure.
type fileBasicInfo struct {
	CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64 // FILETIME
	FileAttributes                                          uint32
	_                                                       uint32
}

// identity is name's change time and file id as its own handle reports them.
func identity(t *testing.T, name string) (changeNanos int64, fileID uint64) {
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
	defer mustCloseRaw(t, uintptr(h))
	var fbi fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&fbi)), uint32(unsafe.Sizeof(fbi))); err != nil {
		t.Fatal(err)
	}
	return (fbi.ChangeTime - windowsEpoch) * 100, fileIndex(t, name)
}
