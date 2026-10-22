package frd

import (
	"os"
	"syscall"
)

// listDir lists name through a bare handle. syscall.Open is the open os.Open
// makes; an os.File would add a GetFileType call, two allocations and a
// finalizer that the listing never uses. A name of 248 bytes or more goes
// through os.Open, which lengthens it past MAX_PATH where Windows needs it.
func listDir(name string) ([]Entry, error) {
	if name == "" || len(name) >= 248 {
		return listDirFile(name)
	}
	h, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	defer func() { _ = syscall.CloseHandle(h) }() // read-only: a close error has nothing to report
	l := listers.Get().(*Lister)
	entries, err := l.appendHandle([]Entry{}, uintptr(h), name)
	listers.Put(l)
	if err != nil {
		var fi syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(h, &fi) == nil && fi.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			err = notADirectory(name)
		}
	}
	return entries, err
}
