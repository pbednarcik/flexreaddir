package frd

import (
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

// listDir lists name through a bare handle. syscall.Open is the open os.Open
// makes where the process can use long paths; an os.File would add a
// GetFileType call, two allocations and a finalizer that the listing never
// uses. Elsewhere every name goes through os.Open, which may add the \\?\
// prefix.
func listDir(name string) ([]Entry, error) {
	if !longPaths() {
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
	if err != nil && notDir(h) {
		err = notADirectory(name)
	}
	return entries, err
}

// notDir reports whether h is open on something other than a directory: a
// device such as NUL or a pipe, or a file.
func notDir(h syscall.Handle) bool {
	if t, err := syscall.GetFileType(h); err == nil && t != syscall.FILE_TYPE_DISK {
		return true
	}
	var fi syscall.ByHandleFileInformation
	return syscall.GetFileInformationByHandle(h, &fi) == nil && fi.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0
}

// longPaths is the runtime's test for opting the process into long paths
// (runtime/os_windows.go): Windows 10 build 15063 or later. From there on
// os.Open hands a name to CreateFile unchanged.
var longPaths = sync.OnceValue(func() bool {
	v := windows.RtlGetVersion()
	return !(v.MajorVersion < 10 || v.MajorVersion == 10 && v.MinorVersion == 0 && v.BuildNumber < 15063)
})
