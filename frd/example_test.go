package frd_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/pbednarcik/flexreaddir/frd"
)

func ExampleAppendDir() {
	dir, err := os.MkdirTemp("", "frd-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		log.Fatal(err)
	}

	f, err := os.Open(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	entries, err := frd.AppendDir(nil, f)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range entries {
		if e.Type.IsRegular() {
			fmt.Println(e.Name, e.Size)
		}
	}
	// Output: hello.txt 5
}

// Passing the previous result truncated to zero length reuses its storage, so
// a walk over many directories allocates little beyond the names.
func ExampleAppendDir_reuse() {
	dirs := []string{".", ".."}
	var entries []frd.Entry
	for _, dir := range dirs {
		f, err := os.Open(dir)
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
}

// Entry is comparable, so whether a file changed since an earlier listing is
// one comparison.
func ExampleEntry() {
	var previous map[string]frd.Entry // from an earlier listing
	f, err := os.Open(".")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	entries, err := frd.AppendDir(nil, f)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range entries {
		if e.Type.IsRegular() && previous[e.Name] != e {
			fmt.Println("changed:", e.Name)
		}
	}
}
