//go:build darwin || linux

package frd

import (
	"testing"

	"golang.org/x/sys/unix"
)

// identity is name's change time and file id as its own lstat reports them.
func identity(t *testing.T, name string) (changeNanos int64, fileID uint64) {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(name, &st); err != nil {
		t.Fatal(err)
	}
	return st.Ctim.Nano(), uint64(st.Ino)
}
