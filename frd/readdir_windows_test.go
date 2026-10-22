package frd

import (
	"errors"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// The runtime opts the process into long paths on the same test and marks it
// in the PEB (runtime/os_windows.go), which is when os.Open stops adding the
// \\?\ prefix: ReadDir's own open must agree with it.
func TestTheLongPathTestIsTheRuntimes(t *testing.T) {
	const longPathAware = 0x80 // IsLongPathAwareProcess in the PEB's BitField
	if got, want := longPaths(), windows.RtlGetCurrentPeb().BitField&longPathAware != 0; got != want {
		t.Fatalf("longPaths() = %v, the runtime's opt-in = %v", got, want)
	}
}

// A device opens like a file but is not a directory: ENOTDIR, as before the
// bare open.
func TestReadDirOfADeviceIsENOTDIR(t *testing.T) {
	if _, err := ReadDir("NUL"); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("err = %v, want ENOTDIR", err)
	}
}
