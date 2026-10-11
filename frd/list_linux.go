//go:build linux

package frd

import (
	"errors"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const listOp = "getdents64"

func readAll(dst []Entry, h uintptr, buf []byte) ([]Entry, error) {
	fd := int(h)
	var stx unix.Statx_t
	stat := func(name []byte, e *Entry) (bool, error) {
		err := statx(fd, name, &stx)
		if errors.Is(err, syscall.ENOENT) {
			return false, nil
		}
		if err != nil {
			return false, &os.PathError{Op: "statx", Path: string(name), Err: err}
		}
		e.Type = statMode(uint32(stx.Mode))
		if e.Type.IsRegular() {
			e.Size = int64(stx.Size)
			e.ModTimeNanos = stx.Mtime.Sec*1e9 + int64(stx.Mtime.Nsec)
			e.ChangeTimeNanos = stx.Ctime.Sec*1e9 + int64(stx.Ctime.Nsec)
			e.FileID = stx.Ino
		}
		return true, nil
	}
	for {
		n, err := unix.Getdents(fd, buf)
		if err == syscall.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return dst, err
		}
		if dst, err = parseDirents(dst, buf[:n], stat); err != nil {
			return dst, err
		}
	}
}

const statxMask = unix.STATX_TYPE | unix.STATX_SIZE | unix.STATX_MTIME | unix.STATX_CTIME | unix.STATX_INO

// statxFlags: the name itself, never a symlink's target; and never mount an
// automount point to describe it, as fstatat and os.Lstat never do (the
// kernel adds AT_NO_AUTOMOUNT to them; statx(2) asks directory scanners to
// pass it). Only regular files and DT_UNKNOWN entries are statted, so only an
// automount point on a filesystem that reports DT_UNKNOWN could be reached.
const statxFlags = unix.AT_SYMLINK_NOFOLLOW | unix.AT_NO_AUTOMOUNT

// noStatx is set once a kernel older than 4.11 has answered ENOSYS.
var noStatx atomic.Bool

// statx stats name relative to fd, passing it to the kernel where it lies in
// the getdents buffer instead of copying it the way unix.Fstatat does.
func statx(fd int, name []byte, stx *unix.Statx_t) error {
	for !noStatx.Load() {
		_, _, errno := unix.Syscall6(unix.SYS_STATX, uintptr(fd), uintptr(unsafe.Pointer(&name[0])),
			statxFlags, statxMask, uintptr(unsafe.Pointer(stx)), 0)
		runtime.KeepAlive(name)
		runtime.KeepAlive(stx)
		switch errno {
		case 0:
			return nil
		case syscall.EINTR:
			continue
		case syscall.ENOSYS:
			noStatx.Store(true)
		default:
			return errno
		}
	}
	var st unix.Stat_t
	for {
		err := unix.Fstatat(fd, string(name), &st, unix.AT_SYMLINK_NOFOLLOW)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		*stx = unix.Statx_t{Mode: uint16(st.Mode), Size: uint64(st.Size), Ino: uint64(st.Ino),
			Mtime: unix.StatxTimestamp{Sec: int64(st.Mtim.Sec), Nsec: uint32(st.Mtim.Nsec)},
			Ctime: unix.StatxTimestamp{Sec: int64(st.Ctim.Sec), Nsec: uint32(st.Ctim.Nsec)}}
		return nil
	}
}
