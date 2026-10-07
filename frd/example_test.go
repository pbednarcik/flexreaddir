package frd_test

import (
	"cmp"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/pbednarcik/flexreaddir/frd"
)

// exampleTree builds a small tree under a temporary directory:
//
//	root/
//	  hello.txt      5 bytes
//	  notes/
//	    a.md         2 bytes
//	    b.md         3 bytes
//	  empty/
//
// It returns the root and a function that removes it.
func exampleTree() (string, func()) {
	root, err := os.MkdirTemp("", "frd-example")
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range []string{"notes", "empty"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			log.Fatal(err)
		}
	}
	for name, data := range map[string]string{"hello.txt": "hello", "notes/a.md": "# a", "notes/b.md": "# b\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			log.Fatal(err)
		}
	}
	return root, func() { os.RemoveAll(root) }
}

// list opens dir, lists it and closes it. Entries come in no particular
// order, so the examples sort them before printing.
func list(dir string, entries []frd.Entry) []frd.Entry {
	f, err := os.Open(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	entries, err = frd.AppendDir(entries, f)
	if err != nil {
		log.Fatal(err)
	}
	slices.SortFunc(entries, func(a, b frd.Entry) int { return cmp.Compare(a.Name, b.Name) })
	return entries
}

func ExampleAppendDir() {
	root, cleanup := exampleTree()
	defer cleanup()

	f, err := os.Open(root)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	entries, err := frd.AppendDir(nil, f)
	if err != nil {
		log.Fatal(err)
	}
	slices.SortFunc(entries, func(a, b frd.Entry) int { return cmp.Compare(a.Name, b.Name) })
	for _, e := range entries {
		switch {
		case e.Type.IsDir():
			fmt.Println(e.Name + "/")
		case e.Type.IsRegular():
			fmt.Println(e.Name, e.Size)
		}
	}
	// Output:
	// empty/
	// hello.txt 5
	// notes/
}

// Passing the previous result truncated to zero length reuses its storage, so
// a loop over many directories allocates little beyond the names.
func ExampleAppendDir_reuse() {
	root, cleanup := exampleTree()
	defer cleanup()

	var entries []frd.Entry
	for _, dir := range []string{"notes", "empty"} {
		f, err := os.Open(filepath.Join(root, dir))
		if err != nil {
			log.Fatal(err)
		}
		entries, err = frd.AppendDir(entries[:0], f)
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(dir, len(entries))
	}
	// Output:
	// notes 2
	// empty 0
}

// A tree walk is a loop over a stack of directories: list one, push its
// subdirectories, count its files. The result slice is reused throughout.
func Example_walk() {
	root, cleanup := exampleTree()
	defer cleanup()

	var entries []frd.Entry
	files, bytes := 0, int64(0)
	pending := []string{root}
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries = list(dir, entries[:0])
		for _, e := range entries {
			switch {
			case e.Type.IsDir():
				pending = append(pending, filepath.Join(dir, e.Name))
			case e.Type.IsRegular():
				files++
				bytes += e.Size
			}
		}
	}
	fmt.Println(files, "files,", bytes, "bytes")
	// Output: 3 files, 12 bytes
}

// A walker keeps one Lister per goroutine, so the buffer the kernel fills is
// allocated once for the whole walk, and again for the next walk if the
// walker keeps its Listers.
func ExampleLister() {
	root, cleanup := exampleTree()
	defer cleanup()

	var l frd.Lister
	var entries []frd.Entry
	files := 0
	pending := []string{root}
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		f, err := os.Open(dir)
		if err != nil {
			log.Fatal(err)
		}
		entries, err = l.AppendDir(entries[:0], f)
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
		for _, e := range entries {
			switch {
			case e.Type.IsDir():
				pending = append(pending, filepath.Join(dir, e.Name))
			case e.Type.IsRegular():
				files++
			}
		}
	}
	fmt.Println(files, "files")
	// Output: 3 files
}

// Entry is comparable, so whether a file changed since an earlier listing is
// one comparison against the Entry kept from that listing.
func ExampleEntry() {
	root, cleanup := exampleTree()
	defer cleanup()
	dir := filepath.Join(root, "notes")

	previous := map[string]frd.Entry{}
	for _, e := range list(dir, nil) {
		previous[e.Name] = e
	}

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# a, longer"), 0o644); err != nil {
		log.Fatal(err)
	}

	for _, e := range list(dir, nil) {
		if e.Type.IsRegular() && previous[e.Name] != e {
			fmt.Println("changed:", e.Name)
		}
	}
	// Output: changed: a.md
}

// ModTime converts the integer timestamp for display or date arithmetic.
func ExampleEntry_ModTime() {
	root, cleanup := exampleTree()
	defer cleanup()

	for _, e := range list(root, nil) {
		if e.Type.IsRegular() && time.Since(e.ModTime()) < time.Hour {
			fmt.Println("written in the last hour:", e.Name)
		}
	}
	// Output: written in the last hour: hello.txt
}

// ErrMalformed never describes the directory being read; it means the kernel
// filled the buffer with records this package does not understand, and is
// worth a bug report.
func ExampleErrMalformed() {
	root, cleanup := exampleTree()
	defer cleanup()

	f, err := os.Open(root)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	_, err = frd.AppendDir(nil, f)
	switch {
	case errors.Is(err, frd.ErrMalformed):
		fmt.Println("this kernel's directory buffer is not understood; please report it")
	case err != nil:
		fmt.Println("listing failed:", err)
	default:
		fmt.Println("listed")
	}
	// Output: listed
}
