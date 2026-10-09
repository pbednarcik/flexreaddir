package frd

import (
	"syscall"

	"golang.org/x/sys/windows"
)

const listOp = "GetFileInformationByHandleEx"

// readAll fills buf with FILE_FULL_DIR_INFO records from the handle's current
// position until the kernel has no more. ERROR_FILE_NOT_FOUND on the first
// call is an empty root directory (MS-FSA 2.1.5.6.3), not a failure.
func readAll(dst []Entry, h uintptr, buf []byte) ([]Entry, error) {
	for first := true; ; first = false {
		err := windows.GetFileInformationByHandleEx(windows.Handle(h), windows.FileFullDirectoryInfo, &buf[0], uint32(len(buf)))
		if err == syscall.ERROR_NO_MORE_FILES || first && err == syscall.ERROR_FILE_NOT_FOUND {
			return dst, nil
		}
		if err != nil {
			return dst, err
		}
		if dst, err = parseFullInfo(dst, buf); err != nil {
			return dst, err
		}
	}
}
