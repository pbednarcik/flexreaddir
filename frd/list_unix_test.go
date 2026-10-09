//go:build unix

package frd

import (
	"io/fs"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAPipeIsListedAsOneWithoutBeingOpened(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skipf("this filesystem cannot hold a pipe: %v", err)
	}
	if got, want := readDir(t, dir)["pipe"], (Entry{Name: "pipe", Type: fs.ModeNamedPipe}); got != want {
		t.Fatalf("pipe = %+v, want %+v", got, want)
	}
}
