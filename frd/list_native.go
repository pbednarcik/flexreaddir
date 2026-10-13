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
	// Names, when set, lists names and types only: every Entry carries Name
	// and Type, and Size, ModTimeNanos, ChangeTimeNanos and FileID are zero.
	// On macOS it lists by getdirentries64, which reads the directory's
	// records alone, so it costs the same whether or not the files' vnodes
	// are cached, and needs list permission only; an entry whose record
	// carries no type (never on APFS; /dev/fd for one) is stat'ed for it,
	// which needs search permission too. Set it before the first
	// call on a directory and keep it for that directory: the two macOS
	// calls keep the descriptor's position differently, so mixing them on
	// one descriptor repeats or skips entries.
	//
	// getdirentries64 is libSystem's private __getdirentries64, the call
	// readdir is built on, and it is linked by every macOS binary that
	// imports this package, whether or not Names is set; the App Store
	// rejects a submitted binary that imports it. A command-line tool or a
	// server is not affected.
	Names bool

	buf []byte // what the kernel fills, allocated on first use
}

// listers lend the function AppendDir a Lister and its buffer.
var listers = sync.Pool{New: func() any { return new(Lister) }}

func appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	l := listers.Get().(*Lister)
	defer listers.Put(l)
	return l.appendDir(dst, dir)
}

// op names the call this Lister's errors carry.
func (l *Lister) op() string {
	if l.Names {
		return namesListOp
	}
	return listOp
}

// appendDir lends readAll the directory's descriptor for the length of the
// call, through SyscallConn, which keeps dir from being closed meanwhile.
func (l *Lister) appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	if dir == nil {
		return dst, &os.PathError{Op: l.op(), Err: os.ErrInvalid}
	}
	conn, err := dir.SyscallConn()
	if err != nil {
		return dst, &os.PathError{Op: l.op(), Path: dir.Name(), Err: err}
	}
	var readErr error
	if err := conn.Control(func(fd uintptr) { dst, readErr = l.read(dst, fd) }); err != nil {
		// Control fails only on a closed file, with poll's "use of closed
		// file", which os reports as ErrClosed.
		return dst, &os.PathError{Op: l.op(), Path: dir.Name(), Err: os.ErrClosed}
	}
	return dst, pathError(readErr, dir.Name(), l.op())
}

func (l *Lister) appendHandle(dst []Entry, fd uintptr, name string) ([]Entry, error) {
	dst, err := l.read(dst, fd)
	return dst, pathError(err, name, l.op())
}

func (l *Lister) read(dst []Entry, fd uintptr) ([]Entry, error) {
	if l.buf == nil {
		// Heap-allocated, so 8-byte aligned: every Windows listing class
		// requires it (ERROR_NOACCESS otherwise).
		l.buf = make([]byte, bufferBytes)
	}
	return readAll(dst, fd, l.buf, l.Names)
}

// pathError names the directory in err under op, the listing call; readAll
// returns an *os.PathError of its own for an entry that failed, whose path,
// relative to the directory, is joined to name, and the macOS names-only
// listing returns one with no path under its own call's name.
func pathError(err error, name, op string) error {
	if err == nil {
		return nil
	}
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		pe.Path = filepath.Join(name, pe.Path)
		return pe
	}
	return &os.PathError{Op: op, Path: name, Err: err}
}
