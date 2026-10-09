package frd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestReadDirAgreesWithOSReadDir(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.go", "B2.go", "a", "Z", "\U0001F642", "\uFF5E"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d entries, os.ReadDir %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name() || got[i].Type != want[i].Type() {
			t.Errorf("entry %d: %q %v, os.ReadDir %q %v", i, got[i].Name, got[i].Type, want[i].Name(), want[i].Type())
		}
	}
}

func TestReadDirOfAMissingDirectoryIsTheOpensPathError(t *testing.T) {
	_, err := ReadDir(filepath.Join(t.TempDir(), "missing"))
	var pe *os.PathError
	if !errors.As(err, &pe) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want an *os.PathError wrapping fs.ErrNotExist", err)
	}
}

func TestReadDirOfARegularFileIsENOTDIR(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDir(file); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("err = %v, want ENOTDIR as os.ReadDir gives", err)
	}
}

func TestReadDirOfAnEmptyDirectoryIsAnEmptyNonNilSlice(t *testing.T) {
	got, err := ReadDir(t.TempDir())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want a non-nil empty slice", got, err)
	}
}

// Go adds the \\?\ prefix for a long path on Windows.
func TestReadDirListsADirectoryBeyondMaxPath(t *testing.T) {
	dir := t.TempDir()
	deep := dir
	for len(deep) < 300 {
		deep = filepath.Join(deep, strings.Repeat("d", 40))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Skip(err) // a filesystem that refuses the depth is not this test's subject
	}
	if err := os.WriteFile(filepath.Join(deep, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDir(deep)
	if err != nil || len(got) != 1 || got[0].Name != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
