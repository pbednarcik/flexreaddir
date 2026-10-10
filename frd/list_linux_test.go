package frd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A directory that may be read but not searched lists its names and refuses
// every stat, so the error names the entry and the call that failed.
func TestAnEntryThatCannotBeStatedIsAPathErrorNamingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	refuseSearch(t, dir)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = AppendDir(nil, f)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != "statx" || pe.Path != filepath.Join(dir, "a.go") || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("AppendDir = %v, want a statx permission error naming a.go", err)
	}
}

// A kernel older than 4.11 answers statx with ENOSYS, and the listing then
// stats through fstatat, which must fill every field statx does.
func TestTheFstatatFallbackListsWhatStatxLists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := readDir(t, dir)["a.go"]
	noStatx.Store(true)
	t.Cleanup(func() { noStatx.Store(false) })
	if got := readDir(t, dir)["a.go"]; got != want {
		t.Fatalf("through fstatat %+v, through statx %+v", got, want)
	}
}

// idsPersist is true: statx's stx_ino is the kernel's inode number, which
// lstat reports too.
func idsPersist(*testing.T, string) bool { return true }
