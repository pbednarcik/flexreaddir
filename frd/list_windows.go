package frd

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

const listOp = "GetFileInformationByHandleEx"

// namesListOp is the call a Names listing's errors name: the same one.
const namesListOp = listOp

// listClass is the information class a listing asks for. FILE_ID_EXTD_DIR_INFO
// is the default: it fills Entry.FileID at the cost of 20 more bytes per
// record. FILE_FULL_DIR_INFO is the fallback, and the tests' comparison.
var listClass uint32 = windows.FileIdExtdDirectoryInfo

// refuseFileIDWith, when not 0, makes the first listing call fail with that
// error, as a volume that refuses FILE_ID_EXTD_DIR_INFO does, so tests run the
// fallback on any volume.
var refuseFileIDWith syscall.Errno

// classFor is the class a listing asks for: a Names listing leaves FileID
// zero, so it asks for FILE_FULL_DIR_INFO, whose shorter records the kernel
// fills faster.
func classFor(names bool) uint32 {
	if names {
		return windows.FileFullDirectoryInfo
	}
	return listClass
}

func layoutOf(class uint32) layout {
	if class == windows.FileIdExtdDirectoryInfo {
		return idExtdDirInfo
	}
	return fullDirInfo
}

// readAll fills buf from the handle's current position, or from its first
// entry under rewind, until the kernel has no more, asking for classFor(names). A volume that refuses FILE_ID_EXTD_DIR_INFO
// on the first call (FAT32, WSL's 9P and older SMB servers do) is listed with
// FILE_FULL_DIR_INFO, and its entries carry no FileID. Only the first call
// may switch: after it, the handle's position has moved. ERROR_FILE_NOT_FOUND
// on the first call is an empty root directory (MS-FSA 2.1.5.6.3), not a
// failure. buf must be 8-byte aligned.
func readAll(dst []Entry, h uintptr, buf []byte, names, rewind bool) ([]Entry, error) {
	class := classFor(names)
	l := layoutOf(class)
	for first := true; ; first = false {
		var err error
		if first && class == windows.FileIdExtdDirectoryInfo && refuseFileIDWith != 0 {
			err = refuseFileIDWith
		} else {
			err = windows.GetFileInformationByHandleEx(windows.Handle(h), asked(class, first && rewind), &buf[0], uint32(len(buf)))
		}
		if first && class == windows.FileIdExtdDirectoryInfo && refused(err) {
			class, l = windows.FileFullDirectoryInfo, fullDirInfo
			err = windows.GetFileInformationByHandleEx(windows.Handle(h), asked(class, rewind), &buf[0], uint32(len(buf)))
		}
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) || first && errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return dst, nil
		}
		if err != nil {
			return dst, err
		}
		if dst, err = parseFullInfo(dst, buf, l, names); err != nil {
			return dst, err
		}
	}
}

// The restart forms of the two listing classes, which list from the first
// entry whatever the handle's position; x/sys does not name them.
const (
	fileFullDirectoryRestartInfo   = 15
	fileIDExtdDirectoryRestartInfo = 20
)

// asked is the class a call asks for: class, or its restart form when
// restart is set.
func asked(class uint32, restart bool) uint32 {
	switch {
	case !restart:
		return class
	case class == windows.FileIdExtdDirectoryInfo:
		return fileIDExtdDirectoryRestartInfo
	default:
		return fileFullDirectoryRestartInfo
	}
}

// refused reports whether a first call's err may be the volume refusing the
// class: FAT32 and 9P answer ERROR_INVALID_PARAMETER, SMB servers
// ERROR_NOT_SUPPORTED, and other filesystems may use other codes. Anything but
// the end of the listing and an empty root is retried with the old class,
// which fails the same way when the error was real, at the cost of one call.
func refused(err error) bool {
	return err != nil && !errors.Is(err, syscall.ERROR_NO_MORE_FILES) && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND)
}
