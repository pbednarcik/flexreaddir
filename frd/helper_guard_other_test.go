//go:build !darwin && !linux && !windows

package frd

import (
	"os"
	"testing"
)

// guardedPage returns one page of ordinary memory: no guard here, so a read
// past its end goes unnoticed, as on any slice.
func guardedPage(*testing.T) []byte { return make([]byte, os.Getpagesize()) }
