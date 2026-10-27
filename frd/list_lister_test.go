package frd

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
)

// listerTree writes a directory of files and one subdirectory.
func listerTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := range 20 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", i)), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// typesOf is the set of entries' names and types.
func typesOf(entries []Entry) map[string]os.FileMode {
	m := make(map[string]os.FileMode, len(entries))
	for _, e := range entries {
		m[e.Name] = e.Type
	}
	return m
}

// Rewind lists an os.File listed before in full, by either listing.
func TestRewindListsAFileListedBeforeInFull(t *testing.T) {
	for _, names := range []bool{false, true} {
		dir := listerTree(t)
		f, err := os.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		l := Lister{Names: names}
		first, err := l.AppendDir(nil, f)
		if err != nil {
			_ = f.Close() // only read: a failed close loses nothing
			t.Fatal(err)
		}
		l.Rewind = true
		again, err := l.AppendDir(nil, f)
		if cerr := f.Close(); cerr != nil {
			t.Fatal(cerr)
		}
		if err != nil || len(first) == 0 || !maps.Equal(typesOf(again), typesOf(first)) {
			t.Errorf("names %v: AppendDir again with Rewind = %v, %v; want the first listing's %v", names, typesOf(again), err, typesOf(first))
		}
	}
}

func byName(t *testing.T, entries []Entry) map[string]Entry {
	t.Helper()
	m := map[string]Entry{}
	for _, e := range entries {
		if _, dup := m[e.Name]; dup {
			t.Fatalf("%s listed twice", e.Name)
		}
		m[e.Name] = e
	}
	dropAppleDouble(m)
	return m
}

func TestAListerListsWhatAppendDirLists(t *testing.T) {
	dir := listerTree(t)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	var l Lister
	entries, err := l.AppendDir(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := byName(t, entries), readDir(t, dir); !maps.Equal(got, want) {
		t.Fatalf("Lister.AppendDir = %v, AppendDir = %v", got, want)
	}
}

// Each directory holds different names, so a Lister that handed back an
// earlier directory's entries fails here.
func TestOneListerServesManyDirectories(t *testing.T) {
	var l Lister
	var entries []Entry
	for i, names := range [][]string{{"a.go", "b.go"}, {"c.txt"}, {}, {"d", "e", "f"}} {
		dir := t.TempDir()
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		f, err := os.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		entries, err = l.AppendDir(entries[:0], f)
		mustClose(t, f)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(entries))
		for _, e := range byName(t, entries) {
			if e.Size != int64(len(e.Name)) {
				t.Errorf("directory %d: %s has size %d, want %d", i, e.Name, e.Size, len(e.Name))
			}
			got = append(got, e.Name)
		}
		slices.Sort(got)
		if !slices.Equal(got, names) {
			t.Fatalf("directory %d listed %q, want %q", i, got, names)
		}
	}
}

// Many goroutines list at once, through the pooled function and through a
// Lister each; run with -race.
func TestConcurrentListingsAgree(t *testing.T) {
	dir := listerTree(t)
	want := readDir(t, dir)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			var l Lister
			var entries []Entry
			for range 20 {
				f, err := os.Open(dir)
				if err != nil {
					t.Error(err)
					return
				}
				if g%2 == 0 {
					entries, err = AppendDir(entries[:0], f)
				} else {
					entries, err = l.AppendDir(entries[:0], f)
				}
				if cerr := f.Close(); cerr != nil {
					t.Error(cerr)
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				got := make(map[string]Entry, len(entries)) // not byName: t.Fatal is for the test's own goroutine
				for _, e := range entries {
					got[e.Name] = e
				}
				twice := len(got) != len(entries)
				dropAppleDouble(got)
				if twice || !maps.Equal(got, want) {
					t.Errorf("goroutine %d listed %v, want %v", g, entries, want)
					return
				}
			}
		})
	}
	wg.Wait()
}

// namesOnly is what a Names listing must report for e: its name and type.
func namesOnly(e Entry) Entry { return Entry{Name: e.Name, Type: e.Type} }

// namesOf is readDir's map with every entry cut to its name and type.
func namesOf(t *testing.T, dir string) map[string]Entry {
	t.Helper()
	want := map[string]Entry{}
	for name, e := range readDir(t, dir) {
		want[name] = namesOnly(e)
	}
	return want
}

func TestANamesListingCarriesNamesAndTypesOnly(t *testing.T) {
	dir := listerTree(t)
	if full := readDir(t, dir); full["f00.go"].Size == 0 {
		t.Fatal("the full listing has no size to leave out")
	}
	want := namesOf(t, dir)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	l := Lister{Names: true}
	entries, err := l.AppendDir(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	if got := byName(t, entries); !maps.Equal(got, want) {
		t.Fatalf("Lister{Names: true}.AppendDir = %v, want %v", got, want)
	}
	fd := openRaw(t, dir)
	defer mustCloseRaw(t, fd)
	entries, err = l.AppendHandle(nil, fd, dir)
	if errors.Is(err, errors.ErrUnsupported) {
		return // no native listing on this platform
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := byName(t, entries); !maps.Equal(got, want) {
		t.Fatalf("Lister{Names: true}.AppendHandle = %v, want %v", got, want)
	}
}

func TestANamesListingReadsADirectoryLargerThanOneBuffer(t *testing.T) {
	dir := t.TempDir()
	for i := range 3000 {
		if err := os.WriteFile(filepath.Join(dir, "f"+strconv.Itoa(i)+".go"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	l := Lister{Names: true}
	entries, err := l.AppendDir(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	got := byName(t, entries)
	if len(got) != 3000 {
		t.Fatalf("listed %d names, want 3000", len(got))
	}
	for name, e := range got {
		if e != (Entry{Name: name}) {
			t.Fatalf("%s = %+v, want a regular file with its name only", name, e)
		}
	}
}

func TestANamesListingOfAnEmptyDirectoryListsNothing(t *testing.T) {
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, f)
	l := Lister{Names: true}
	entries, err := l.AppendDir(nil, f)
	if err != nil || len(entries) != 0 {
		t.Fatalf("Lister{Names: true}.AppendDir = %v, %v; want nothing", entries, err)
	}
}

// A Lister may list one directory by names and the next in full.
func TestNamesMaySwitchBetweenDirectories(t *testing.T) {
	short, full := listerTree(t), listerTree(t)
	var l Lister
	for i, c := range []struct {
		dir   string
		names bool
	}{{short, true}, {full, false}, {short, true}} {
		want := readDir(t, c.dir)
		if c.names {
			want = namesOf(t, c.dir)
		}
		f, err := os.Open(c.dir)
		if err != nil {
			t.Fatal(err)
		}
		l.Names = c.names
		entries, err := l.AppendDir(nil, f)
		mustClose(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if got := byName(t, entries); !maps.Equal(got, want) {
			t.Fatalf("listing %d: got %v, want %v", i, got, want)
		}
	}
}
