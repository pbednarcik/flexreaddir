//go:build darwin || linux

package frd

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A FIFO's name fails the open with ENOTDIR rather than waiting for a writer.
func TestReadDirOfAPipeIsENOTDIRWithoutBlocking(t *testing.T) {
	pipe := filepath.Join(t.TempDir(), "pipe")
	if err := unix.Mkfifo(pipe, 0o644); err != nil {
		t.Skipf("this filesystem cannot hold a pipe: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadDir(pipe)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, syscall.ENOTDIR) {
			t.Fatalf("err = %v, want ENOTDIR", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadDir of a pipe blocked")
	}
}
