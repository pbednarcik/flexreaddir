//go:build darwin || linux

package frd

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// guardedPage returns one page of memory whose next page is PROT_NONE, so a
// read past its end faults.
func guardedPage(t *testing.T) []byte {
	page := os.Getpagesize()
	mem, err := unix.Mmap(-1, 0, 2*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Munmap(mem) })
	if err := unix.Mprotect(mem[page:], unix.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	return mem[:page:page]
}
