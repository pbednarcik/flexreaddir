package frd

import (
	"io"
	"testing"
)

// mustClose closes c and fails the test if the close fails. Nothing but the
// close runs on success, so it is safe inside a benchmark loop. It calls
// Fatal, so it belongs to the test's own goroutine.
func mustClose(tb testing.TB, c io.Closer) {
	if err := c.Close(); err != nil {
		tb.Helper()
		tb.Fatalf("closing: %v", err)
	}
}

// mustCloseRaw closes a raw handle or descriptor and fails the test if the
// close fails. AppendHandle must never close the caller's handle, so a close
// that fails here can mean it did. Nothing but the close runs on success, so
// it is safe inside a benchmark loop or an AllocsPerRun.
func mustCloseRaw(tb testing.TB, fd uintptr) {
	if err := closeRaw(fd); err != nil {
		tb.Helper()
		tb.Fatalf("closing the raw handle: %v", err)
	}
}
