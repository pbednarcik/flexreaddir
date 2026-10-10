//go:build darwin || linux

package frd

import (
	"os"
	"testing"
)

// refuseSearch leaves dir readable but not searchable, skipping the test for
// root, who is never refused, and on a filesystem that keeps no permissions,
// such as FAT, where the mode does not change.
func refuseSearch(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root is never refused a search")
	}
	if err := os.Chmod(dir, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Skipf("this filesystem keeps no permissions: the mode is %v", info.Mode().Perm())
	}
}
