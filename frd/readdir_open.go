//go:build !darwin && !linux

package frd

import (
	"os"
	"syscall"
)

// listDirFile lists name through an os.File.
func listDirFile(name string) ([]Entry, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: a close error has nothing to report
	entries, err := AppendDir([]Entry{}, f)
	if err != nil {
		if st, serr := f.Stat(); serr == nil && !st.IsDir() {
			err = notADirectory(name)
		}
	}
	return entries, err
}

// notADirectory is the error for a failed listing of a name the open let
// through that is not a directory.
func notADirectory(name string) error {
	return &os.PathError{Op: "readdir", Path: name, Err: syscall.ENOTDIR}
}
