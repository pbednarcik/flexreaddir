//go:build darwin || linux

package frd

import (
	"os"
	"slices"

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

// sortListing sorts a listing for ReadDir with pdqsort, which beat the
// stable sort on APFS's hash order and on the Linux listings measured, on
// btrfs and overlayfs (docs/notes/evidence/2026-10-22-readdir-sort.md). The
// names in one listing are unique, so the order is SortByName's.
func sortListing(entries []Entry) { slices.SortFunc(entries, CompareName) }

func openDir(name string) (int, error) {
	for {
		fd, err := unix.Open(name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != unix.EINTR {
			return fd, err
		}
	}
}
