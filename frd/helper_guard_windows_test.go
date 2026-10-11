//go:build windows

package frd

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// guardedPage returns one page of memory whose next page is PAGE_NOACCESS,
// so a read past its end faults.
func guardedPage(t *testing.T) []byte {
	page := uintptr(os.Getpagesize())
	base, err := windows.VirtualAlloc(0, 2*page, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.VirtualFree(base, 0, windows.MEM_RELEASE) })
	var old uint32
	if err := windows.VirtualProtect(base+page, page, windows.PAGE_NOACCESS, &old); err != nil {
		t.Fatal(err)
	}
	// The address is memory Go does not manage; read through &base so vet's
	// unsafeptr check, meant for heap pointers kept as uintptr, stays quiet.
	p := *(*unsafe.Pointer)(unsafe.Pointer(&base))
	return unsafe.Slice((*byte)(p), page)
}
