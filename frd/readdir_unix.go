//go:build darwin || linux

package frd

import (
	"os"

	"golang.org/x/sys/unix"
)

// listDir lists name through a bare descriptor: an os.File would cost an
// fstat on macOS and fcntl calls on Linux that the listing never uses. The
// open succeeds only on a directory, as os.ReadDir's does, so a FIFO cannot
// block it and a regular file fails there with ENOTDIR.
func listDir(name string) ([]Entry, error) {
	fd, err := openDir(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	defer func() { _ = unix.Close(fd) }() // read-only: a close error has nothing to report
	l := listers.Get().(*Lister)
	entries, err := l.appendHandle([]Entry{}, uintptr(fd), name)
	listers.Put(l)
	return entries, err
}

func openDir(name string) (int, error) {
	for {
		fd, err := unix.Open(name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != unix.EINTR {
			return fd, err
		}
	}
}
