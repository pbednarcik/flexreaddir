//go:build !darwin && !linux && !windows

package frd

import (
	"errors"
	"io/fs"
	"os"
)

func listDir(name string) ([]Entry, error) { return listDirFile(name) }

func appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	return appendDirNames(dst, dir, false)
}

func appendDirNames(dst []Entry, dir *os.File, names bool) ([]Entry, error) {
	if dir == nil {
		return dst, &os.PathError{Op: "readdir", Err: os.ErrInvalid}
	}
	read, readErr := dir.ReadDir(-1)
	for _, de := range read {
		e := Entry{Name: de.Name(), Type: de.Type()}
		if !names && e.Type.IsRegular() {
			info, err := de.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return dst, err
			}
			// The name may have been replaced since the read: the stat decides.
			if e.Type = info.Mode().Type(); e.Type.IsRegular() {
				e.Size, e.ModTimeNanos = info.Size(), info.ModTime().UnixNano()
			}
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
type Lister struct {
	// Names, when set, lists names and types only: every Entry carries Name
	// and Type, and Size, ModTimeNanos, ChangeTimeNanos and FileID are zero.
	Names bool
}

func (l *Lister) appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	return appendDirNames(dst, dir, l.Names)
}

// appendHandle has no os.File to read through: wrapping fd in one would hand
// it a finalizer that closes the caller's descriptor.
func (l *Lister) appendHandle(dst []Entry, _ uintptr, name string) ([]Entry, error) {
	return dst, &os.PathError{Op: "AppendHandle", Path: name, Err: errors.ErrUnsupported}
}
