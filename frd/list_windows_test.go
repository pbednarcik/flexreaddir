package frd

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A junction is a reparse point the standard library calls irregular since
// Go 1.23, and AppendDir says the same.
func TestAJunctionIsListedAsTheStandardLibraryListsIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J failed: %v: %s", err, out)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var want fs.FileMode
	for _, de := range des {
		if de.Name() == "junction" {
			want = de.Type()
		}
	}
	if want != fs.ModeIrregular {
		t.Fatalf("os.ReadDir types the junction %v; this test expects Go 1.23 semantics", want)
	}
	if got := readDir(t, dir)["junction"]; got != (Entry{Name: "junction", Type: want}) {
		t.Fatalf("junction = %+v, want type %v and nothing else", got, want)
	}
}
