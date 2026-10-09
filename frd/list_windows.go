package frd

import (
	"syscall"

	"golang.org/x/sys/windows"
)

const listOp = "GetFileInformationByHandleEx"

// listClass is the information class a listing asks for. FILE_FULL_DIR_INFO
// is the default. FILE_ID_EXTD_DIR_INFO fills Entry.FileID, but its 20 more
// bytes per record cost a warm NTFS listing 5 to 9% (measured 2026-10-09,
// docs/superpowers/evidence/2026-10-09-fileid-windows.md); its parser and
// fallback stay, and the tests select it.
var listClass uint32 = windows.FileFullDirectoryInfo

// refuseFileIDWith, when not 0, makes the first listing call fail with that
// error, as a volume that refuses FILE_ID_EXTD_DIR_INFO does, so tests run the
// fallback on any volume.
var refuseFileIDWith syscall.Errno

func layoutOf(class uint32) layout {
	if class == windows.FileIdExtdDirectoryInfo {
		return idExtdDirInfo
	}
	return fullDirInfo
}

// readAll fills buf from the handle's current position until the kernel has
// no more, asking for listClass. With FILE_ID_EXTD_DIR_INFO, a volume that
// refuses the class on the first call (FAT32, WSL's 9P, SMB servers older than
// Windows Server 2025), with whatever error, is listed with FILE_FULL_DIR_INFO
// instead, and its entries carry no FileID. The class never changes once a
// call has succeeded, since the handle's position has moved.
// ERROR_FILE_NOT_FOUND on the first call is an empty root directory
// (MS-FSA 2.1.5.6.3), not a failure. The buffer must be 8-byte aligned.
func readAll(dst []Entry, h uintptr, buf []byte) ([]Entry, error) {
	class, l := listClass, layoutOf(listClass)
	for first := true; ; first = false {
		var err error
		if first && refuseFileIDWith != 0 {
			err = refuseFileIDWith
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

// refused reports whether a first call's err may be the volume refusing the
// class: FAT32 and 9P answer ERROR_INVALID_PARAMETER, SMB servers
// ERROR_NOT_SUPPORTED, and other filesystems may use other codes. Anything but
// the end of the listing and an empty root is retried with the old class,
// which fails the same way when the error was real, at the cost of one call.
func refused(err error) bool {
	return err != nil && err != syscall.ERROR_NO_MORE_FILES && err != syscall.ERROR_FILE_NOT_FOUND
}
