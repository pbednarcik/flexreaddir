//go:build darwin || linux || windows

package frd

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendHandleListsWhatAppendDirLists(t *testing.T) {
	dir := listerTree(t)
	fd, closeFd := openRaw(t, dir)
	defer closeFd()
	var l Lister
	entries, err := l.AppendHandle(nil, fd, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := byName(t, entries), readDir(t, dir); !maps.Equal(got, want) {
		t.Fatalf("AppendHandle = %v, AppendDir = %v", got, want)
	}
}

// The listing reads from the handle's position, like AppendDir: a second
// call on the same handle finds the directory read to its end, and the
// handle is still the caller's to close.
func TestAppendHandleLeavesTheHandleOpenAtItsEnd(t *testing.T) {
	dir := listerTree(t)
	fd, closeFd := openRaw(t, dir)
	closed := false
	t.Cleanup(func() {
		if !closed {
			closeFd()
		}
	})
	var l Lister
	if _, err := l.AppendHandle(nil, fd, dir); err != nil {
		t.Fatal(err)
	}
	again, err := l.AppendHandle(nil, fd, dir)
	if err != nil || len(again) != 0 {
		t.Fatalf("second AppendHandle = %d entries, %v; want none and no error", len(again), err)
	}
	closed = true
	if err := closeFd(); err != nil {
		t.Fatalf("closing the handle after AppendHandle: %v", err)
	}
}

func TestAppendHandleOnAFileIsAPathErrorNamingIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fd, closeFd := openRaw(t, file)
	defer closeFd()
	var l Lister
	_, err := l.AppendHandle(nil, fd, file)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Path != file || pe.Op != listOp {
		t.Fatalf("AppendHandle on a file = %v, want an *os.PathError from %s naming it", err, listOp)
	}
}

// The buffer is allocated on the Lister's first listing and kept.
func TestAListerAllocatesItsBufferOnce(t *testing.T) {
	var l Lister
	if l.buf != nil {
		t.Fatal("the zero Lister holds a buffer")
	}
	var first *byte
	for range 3 {
		dir := listerTree(t)
		fd, closeFd := openRaw(t, dir)
		_, err := l.AppendHandle(nil, fd, dir)
		closeFd()
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = &l.buf[0]
		} else if &l.buf[0] != first {
			t.Fatal("the Lister allocated a second buffer")
		}
	}
}
