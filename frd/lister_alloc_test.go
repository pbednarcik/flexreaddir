//go:build windows && !race

package frd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// What AppendHandle adds to opening and closing a raw handle on a 100-file
// directory, with a Lister that has its buffer: the names, nothing else.
// Windows only, where the count was measured; Linux stats each file.
func TestAppendHandleAllocatesOnlyTheNames(t *testing.T) {
	dir := t.TempDir()
	for i := range 100 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%03d.go", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	openClose := testing.AllocsPerRun(50, func() {
		_, closeFd := openRaw(t, dir)
		closeFd()
	})
	var l Lister
	entries := make([]Entry, 0, 200)
	withList := testing.AllocsPerRun(50, func() {
		fd, closeFd := openRaw(t, dir)
		var err error
		entries, err = l.AppendHandle(entries[:0], fd, dir)
		closeFd()
		if err != nil || len(entries) != 100 {
			t.Fatal(len(entries), err)
		}
	})
	if extra := withList - openClose; extra > 1 {
		t.Fatalf("AppendHandle added %v allocations to an open and a close, want 1", extra)
	}
}
