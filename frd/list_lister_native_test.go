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
	fd := openRaw(t, dir)
	defer mustCloseRaw(t, fd)
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
	fd := openRaw(t, dir)
	closed := false
	t.Cleanup(func() {
		if !closed {
			mustCloseRaw(t, fd)
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
	if err := closeRaw(fd); err != nil {
		t.Fatalf("closing the handle after AppendHandle: %v", err)
	}
}

// Rewind lists a handle listed before in full, by either listing, and a
// handle never listed the same as without it.
func TestRewindListsAHandleListedBeforeInFull(t *testing.T) {
	for _, names := range []bool{false, true} {
		dir := listerTree(t)
		fd := openRaw(t, dir)
		l := Lister{Names: names}
		first, err := l.AppendHandle(nil, fd, dir)
		if err != nil {
			mustCloseRaw(t, fd)
			t.Fatal(err)
		}
		l.Rewind = true
		for i := range 2 {
			again, err := l.AppendHandle(nil, fd, dir)
			if err != nil || !maps.Equal(typesOf(again), typesOf(first)) {
				t.Errorf("names %v: listing %d with Rewind = %v, %v; want the first listing's %v", names, i+2, typesOf(again), err, typesOf(first))
			}
		}
		mustCloseRaw(t, fd)
		fresh := openRaw(t, dir)
		got, err := l.AppendHandle(nil, fresh, dir)
		mustCloseRaw(t, fresh)
		if err != nil || !maps.Equal(typesOf(got), typesOf(first)) {
			t.Errorf("names %v: a fresh handle with Rewind = %v, %v; want %v", names, typesOf(got), err, typesOf(first))
		}
	}
}

func TestAppendHandleOnAFileIsAPathErrorNamingIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fd := openRaw(t, file)
	defer mustCloseRaw(t, fd)
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
		fd := openRaw(t, dir)
		_, err := l.AppendHandle(nil, fd, dir)
		mustCloseRaw(t, fd)
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
