package frd

import (
	"syscall"

	"golang.org/x/sys/windows"
)

const listOp = "GetFileInformationByHandleEx"

// refuseFileID makes the first listing call behave as a volume that refuses
// FILE_ID_EXTD_DIR_INFO, so tests run the fallback on any volume.
var refuseFileID = false

// readAll fills buf from the handle's current position until the kernel has
// no more. It asks for FILE_ID_EXTD_DIR_INFO, which carries a file id; a
// volume that refuses that class on the first call (FAT32, WSL's 9P, SMB
// servers older than Windows Server 2025) is listed with FILE_FULL_DIR_INFO
// instead, and its entries carry no FileID. The class never changes once a
// call has succeeded, since the handle's position has moved.
// ERROR_FILE_NOT_FOUND on the first call is an empty root directory
// (MS-FSA 2.1.5.6.3), not a failure. The buffer must be 8-byte aligned.
func readAll(dst []Entry, h uintptr, buf []byte) ([]Entry, error) {
	class, l := uint32(windows.FileIdExtdDirectoryInfo), idExtdDirInfo
	for first := true; ; first = false {
		var err error
		if first && refuseFileID {
			err = windows.ERROR_INVALID_PARAMETER
		} else {
			err = windows.GetFileInformationByHandleEx(windows.Handle(h), class, &buf[0], uint32(len(buf)))
		}
		if first && class == windows.FileIdExtdDirectoryInfo && refused(err) {
			class, l = windows.FileFullDirectoryInfo, fullDirInfo
			err = windows.GetFileInformationByHandleEx(windows.Handle(h), class, &buf[0], uint32(len(buf)))
		}
		if err == syscall.ERROR_NO_MORE_FILES || first && err == syscall.ERROR_FILE_NOT_FOUND {
			return dst, nil
		}
		if err != nil {
			return dst, err
		}
		if dst, err = parseFullInfo(dst, buf, l); err != nil {
			return dst, err
		}
	}
}

// refused reports whether err is how a volume says it does not serve an
// information class.
func refused(err error) bool {
	return err == windows.ERROR_INVALID_PARAMETER || err == windows.ERROR_NOT_SUPPORTED || err == windows.ERROR_INVALID_FUNCTION
}
