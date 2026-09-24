//go:build darwin || linux

package frd

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const bufferBytes = 64 << 10

var buffers = sync.Pool{New: func() any { b := make([]byte, bufferBytes); return &b }}

// appendDir lends readAll the directory's descriptor and a pooled buffer for
// the kernel to fill. listOp names the platform's listing call in errors;
// readAll returns an *os.PathError of its own for an entry that failed.
func appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	conn, err := dir.SyscallConn()
	if err != nil {
		return dst, err
	}
	b := buffers.Get().(*[]byte)
	defer buffers.Put(b)
	var readErr error
	if err = conn.Control(func(fd uintptr) { dst, readErr = readAll(dst, int(fd), *b) }); err == nil {
		err = readErr
	}
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		pe.Path = filepath.Join(dir.Name(), pe.Path) // an entry's own error names it within dir
		return dst, pe
	}
	if err != nil {
		return dst, &os.PathError{Op: listOp, Path: dir.Name(), Err: err}
	}
	return dst, nil
}
