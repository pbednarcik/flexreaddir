//go:build !darwin && !linux && !windows

package frd

import (
	"errors"
	"os"
	"testing"
)

func TestAppendHandleIsUnsupportedWithoutANativeListing(t *testing.T) {
	dir := t.TempDir()
	fd := openRaw(t, dir)
	defer closeRaw(fd)
	var l Lister
	_, err := l.AppendHandle(nil, fd, dir)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Path != dir || !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("AppendHandle = %v, want an *os.PathError naming %s and wrapping errors.ErrUnsupported", err, dir)
	}
}
