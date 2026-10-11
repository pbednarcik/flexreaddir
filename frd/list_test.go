package frd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readDir(t *testing.T, dir string) map[string]Entry {
	t.Helper()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	entries, err := AppendDir(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Entry{}
	for _, e := range entries {
		if _, dup := byName[e.Name]; dup {
			t.Fatalf("%s listed twice", e.Name)
		}
		byName[e.Name] = e
	}
	dropAppleDouble(byName)
	return byName
}

// dropAppleDouble removes the ._name file macOS writes beside a file with
// extended attributes on a volume that cannot store them, such as FAT or
// exFAT, so the tests also pass there.
func dropAppleDouble(byName map[string]Entry) {
	for name := range byName {
		if rest, ok := strings.CutPrefix(name, "._"); ok {
			if _, companion := byName[rest]; companion {
				delete(byName, name)
			}
		}
	}
}

func TestAppendDirAgreesWithLstat(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := os.Symlink("a.go", filepath.Join(dir, "link")) == nil
	got := readDir(t, dir)
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	change, id := identity(t, file)
	if want := (Entry{Name: "a.go", Size: info.Size(), ModTimeNanos: info.ModTime().UnixNano(), ChangeTimeNanos: change, FileID: id}); got["a.go"] != want {
		t.Errorf("a.go = %+v, want %+v", got["a.go"], want)
	}
	if want := (Entry{Name: "sub", Type: fs.ModeDir}); got["sub"] != want {
		t.Errorf("sub = %+v, want %+v", got["sub"], want)
	}
	if link {
		if want := (Entry{Name: "link", Type: fs.ModeSymlink}); got["link"] != want {
			t.Errorf("link = %+v, want %+v", got["link"], want)
		}
	}
	want := 2
	if link {
		want++
	}
	if len(got) != want {
		t.Errorf("listed %d entries, want %d: %v", len(got), want, got)
	}
}

// More entries than one kernel buffer holds, so the listing takes several
// calls and none of them may drop or repeat a name.
func TestAppendDirReadsADirectoryLargerThanOneBuffer(t *testing.T) {
	dir := t.TempDir()
	const files = 3000
	for i := range files {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-with-a-long-name-%04d.go", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := readDir(t, dir)
	if len(got) != files {
		t.Fatalf("listed %d entries, want %d", len(got), files)
	}
	for i := range files {
		if _, ok := got[fmt.Sprintf("file-with-a-long-name-%04d.go", i)]; !ok {
			t.Fatalf("file %d is missing", i)
		}
	}
}

func TestAClosedDirectoryIsAPathErrorNamingIt(t *testing.T) {
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustClose(t, f)
	_, err = AppendDir(nil, f)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Path != f.Name() || !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("AppendDir on a closed directory = %v, want an *os.PathError naming it and wrapping fs.ErrClosed", err)
	}
	var l Lister
	if _, err = l.AppendDir(nil, f); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Lister.AppendDir on a closed directory = %v, want fs.ErrClosed", err)
	}
}

func TestANilFileIsAPathError(t *testing.T) {
	_, err := AppendDir(nil, nil)
	if _, ok := errors.AsType[*os.PathError](err); !ok || !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("AppendDir(nil, nil) = %v, want an *os.PathError wrapping os.ErrInvalid", err)
	}
}

func TestModTimeIsTheModificationTime(t *testing.T) {
	e := Entry{ModTimeNanos: 1700000000*1e9 + 5}
	if got := e.ModTime(); !got.Equal(time.Unix(1700000000, 5)) {
		t.Fatalf("ModTime() = %v", got)
	}
}

func TestAnEmptyDirectoryListsNothing(t *testing.T) {
	if got := readDir(t, t.TempDir()); len(got) != 0 {
		t.Fatalf("listed %v in an empty directory", got)
	}
}

func TestNamesOutsideASCIIAgreeWithReadDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"straße.go", "🙂.go", "a🙂ß.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]Entry{}
	for _, de := range des {
		names[de.Name()] = Entry{}
	}
	dropAppleDouble(names)
	got := readDir(t, dir)
	if len(got) != len(names) {
		t.Fatalf("listed %d entries, os.ReadDir %d", len(got), len(names))
	}
	for name := range names {
		if _, ok := got[name]; !ok {
			t.Errorf("os.ReadDir lists %q, AppendDir does not: %v", name, got)
		}
	}
}

func TestARegularFileIsAPathErrorNamingIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	_, err = AppendDir(nil, f)
	if pe, ok := errors.AsType[*os.PathError](err); !ok || pe.Path != file {
		t.Fatalf("AppendDir on a file = %v, want an *os.PathError naming it", err)
	}
}
