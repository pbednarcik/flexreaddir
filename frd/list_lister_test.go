package frd

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

func byName(t *testing.T, entries []Entry) map[string]Entry {
	t.Helper()
	m := map[string]Entry{}
	for _, e := range entries {
		if _, dup := m[e.Name]; dup {
			t.Fatalf("%s listed twice", e.Name)
		}
		m[e.Name] = e
	}
	return m
}

func TestAListerListsWhatAppendDirLists(t *testing.T) {
	dir := listerTree(t)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
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
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(entries))
		for _, e := range entries {
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
				f.Close()
				if err != nil {
					t.Error(err)
					return
				}
				got := make(map[string]Entry, len(entries)) // not byName: t.Fatal is for the test's own goroutine
				for _, e := range entries {
					got[e.Name] = e
				}
				if len(got) != len(entries) || !maps.Equal(got, want) {
					t.Errorf("goroutine %d listed %v, want %v", g, entries, want)
					return
				}
			}
		})
	}
	wg.Wait()
}
