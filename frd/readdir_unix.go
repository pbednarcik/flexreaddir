//go:build darwin || linux

package frd

import (
	"os"
	"syscall"
)

// openDir opens name only if it is a directory, as os.ReadDir does, so a FIFO
// cannot block the open and a regular file fails here with ENOTDIR.
func openDir(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY, 0)
}
