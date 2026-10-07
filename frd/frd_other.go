//go:build !darwin && !linux && !windows

package frd

import (
	"errors"
	"io/fs"
	"os"
)

func appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	if dir == nil {
		return dst, &os.PathError{Op: "readdir", Err: os.ErrInvalid}
	}
	read, readErr := dir.ReadDir(-1)
	for _, de := range read {
		e := Entry{Name: de.Name(), Type: de.Type()}
		if e.Type.IsRegular() {
			info, err := de.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return dst, err
			}
			e.Size, e.ModTimeNanos = info.Size(), info.ModTime().UnixNano()
		}
		dst = append(dst, e)
	}
	return dst, readErr
}

// A Lister lists directories into a buffer it owns, so a walker that keeps
// one per goroutine for a whole walk allocates that buffer once. Here, where
// os.File.ReadDir brings its own buffer, it holds nothing.
//
// The zero Lister is ready to use. A Lister must not be used by two
// goroutines at once, nor copied after its first use.
type Lister struct{}

func (l *Lister) appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	return appendDir(dst, dir)
}

// appendHandle has no os.File to read through: wrapping fd in one would hand
// it a finalizer that closes the caller's descriptor.
func (l *Lister) appendHandle(dst []Entry, _ uintptr, name string) ([]Entry, error) {
	return dst, &os.PathError{Op: "AppendHandle", Path: name, Err: errors.ErrUnsupported}
}
