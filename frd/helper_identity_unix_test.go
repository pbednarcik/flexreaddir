//go:build darwin || linux

package frd

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// identity is name's change time and file id as its own lstat reports them,
// and no id on a volume whose ids do not persist.
func identity(t *testing.T, name string) (changeNanos int64, fileID uint64) {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(name, &st); err != nil {
		t.Fatal(err)
	}
	if !idsPersist(t, filepath.Dir(name)) {
		return st.Ctim.Nano(), 0
	}
	return st.Ctim.Nano(), uint64(st.Ino)
}
