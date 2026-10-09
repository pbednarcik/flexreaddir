//go:build darwin || linux || windows

package frd

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const bufferBytes = 64 << 10

// A Lister lists directories into a buffer it owns, so a walker that keeps
// one per goroutine for a whole walk allocates that buffer once.
//
// The zero Lister is ready to use. A Lister must not be used by two
// goroutines at once, nor copied after its first use.
type Lister struct {
	buf []byte // what the kernel fills, allocated on first use
}

// listers lend the function AppendDir a Lister and its buffer.
var listers = sync.Pool{New: func() any { return new(Lister) }}

func appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	l := listers.Get().(*Lister)
	defer listers.Put(l)
	return l.appendDir(dst, dir)
}

// appendDir lends readAll the directory's descriptor for the length of the
// call, through SyscallConn, which keeps dir from being closed meanwhile.
func (l *Lister) appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	if dir == nil {
		return dst, &os.PathError{Op: listOp, Err: os.ErrInvalid}
	}
	conn, err := dir.SyscallConn()
	if err != nil {
		return dst, &os.PathError{Op: listOp, Path: dir.Name(), Err: err}
	}
	var readErr error
	if err := conn.Control(func(fd uintptr) { dst, readErr = l.read(dst, fd) }); err != nil {
		// Control fails only on a closed file, with poll's "use of closed
		// file", which os reports as ErrClosed.
		return dst, &os.PathError{Op: listOp, Path: dir.Name(), Err: os.ErrClosed}
	}
	return dst, pathError(readErr, dir.Name())
}

func (l *Lister) appendHandle(dst []Entry, fd uintptr, name string) ([]Entry, error) {
	dst, err := l.read(dst, fd)
	return dst, pathError(err, name)
}

func (l *Lister) read(dst []Entry, fd uintptr) ([]Entry, error) {
	if l.buf == nil {
		l.buf = make([]byte, bufferBytes)
	}
	return readAll(dst, fd, l.buf)
}

// pathError names the directory in err. listOp names the platform's listing
// call; readAll returns an *os.PathError of its own for an entry that failed,
// which is named within the directory.
func pathError(err error, name string) error {
	if err == nil {
		return nil
	}
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		pe.Path = filepath.Join(name, pe.Path)
		return pe
	}
	return &os.PathError{Op: listOp, Path: name, Err: err}
}
