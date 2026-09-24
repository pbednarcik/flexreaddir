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
	if os.Geteuid() == 0 {
		t.Skip("root is never refused a stat")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
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
