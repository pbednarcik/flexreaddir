package frd

import (
	"os"
	"syscall"
)

// ReadDir reads the directory name and returns its entries sorted by name,
// as os.ReadDir does: "." and ".." are left out, links are not followed, and
// an empty directory gives an empty, non-nil slice.
//
// If reading fails part way, ReadDir returns the entries read before the
// error, sorted, with the error. A name that is not a directory gives
// syscall.ENOTDIR, as with os.ReadDir. The function AppendDir is the
// unsorted, reusable form.
func ReadDir(name string) ([]Entry, error) {
	f, err := openDir(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: a close error has nothing to report
	entries, err := AppendDir([]Entry{}, f)
	SortByName(entries)
	if err != nil {
		if st, serr := f.Stat(); serr == nil && !st.IsDir() {
			err = &os.PathError{Op: "readdir", Path: name, Err: syscall.ENOTDIR}
		}
	}
	return entries, err
}
