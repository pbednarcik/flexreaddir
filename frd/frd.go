package frd

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

// Entry describes one name in a directory listing.
//
// Size, ModTimeNanos, ChangeTimeNanos and FileID describe regular files only;
// for every other type they are zero on every platform, so an Entry compares
// the same everywhere.
type Entry struct {
	// Name is the entry's name within its directory, without a path. The
	// names from one read share one allocation, so a Name kept long after
	// its neighbours are gone is worth a [strings.Clone].
	Name string

	// Type holds the file type bits only, as [fs.DirEntry].Type reports them:
	// zero for a regular file, [fs.ModeDir], [fs.ModeSymlink] and so on, and
	// [fs.ModeIrregular] for a type the platform does not name.
	Type fs.FileMode

	// Size is the length of a regular file's data in bytes.
	Size int64

	// ModTimeNanos is a regular file's modification time in nanoseconds
	// since the Unix epoch. An integer keeps Entry comparable with == and
	// a change check to one comparison; [Entry.ModTime] converts it. As with
	// [time.Time.UnixNano], a time before the year 1678 or after 2262 does
	// not fit and the value is undefined.
	ModTimeNanos int64

	// ChangeTimeNanos is a regular file's status change time in nanoseconds
	// since the Unix epoch, which every write and every change to the file's
	// metadata moves: st_ctime as ATTR_CMN_CHGTIME on macOS and statx's
	// stx_ctime on Linux, which no Unix call sets; the listing's own
	// ChangeTime on Windows, which SetFileInformationByHandle can set. It is
	// zero on a Windows filesystem that keeps no change time and on the other
	// platforms, where the portable path does not read it. On macOS a
	// filesystem that keeps none, such as FAT32, gives the modification time,
	// as lstat does.
	ChangeTimeNanos int64

	// FileID is a regular file's number on its volume: ATTR_CMN_FILEID on
	// macOS, statx's stx_ino on Linux, and on Windows the listing's
	// FILE_ID_EXTD_DIR_INFO FileId when it fits 64 bits, as on NTFS. It is
	// zero where the platform or the volume gives none: on Windows ReFS and
	// Dev Drive (128-bit ids), FAT, WSL's 9P and shares on servers older than
	// Windows Server 2025; on macOS a volume that clears
	// VOL_CAP_FMT_PERSISTENTOBJECTIDS, such as FAT32, exFAT and NFS, where an
	// empty FAT file gets a new id on every listing; and the platforms without
	// a native listing. Zero means unknown, never equal.
	FileID uint64
}

// ModTime returns the entry's modification time, which is the Unix epoch for
// anything but a regular file.
func (e Entry) ModTime() time.Time { return time.Unix(0, e.ModTimeNanos) }

// ErrMalformed is returned when a buffer the kernel filled does not hold the
// records it claims. It points at a kernel whose layout this package does not
// know, not at anything in the directory being read.
var ErrMalformed = errors.New("frd: malformed directory buffer")

// AppendDir reads the open directory dir from its current offset to its end
// and appends an Entry for each name in it to dst, returning the extended
// slice. Passing a slice from an earlier call, truncated to zero length,
// reuses its storage.
//
// Entries come in no particular order, and "." and ".." are never included.
// A symbolic link is reported as a link and not followed. A name removed
// between the directory read and its stat is left out rather than reported
// as an error.
//
// Any other failure is returned as an [*os.PathError], together with dst
// extended by the entries read before it. Its Path is dir's name joined with
// the name of the entry that failed, or dir's name for any other failure. A
// closed dir is reported as [fs.ErrClosed] and a nil one as [os.ErrInvalid],
// as [os.File]'s methods report them.
//
// On macOS a directory that may be read but not searched fails as a whole,
// even when empty, where Linux lists the names and fails at the first one it
// must stat. In /dev/fd, which holds the caller's own descriptors, macOS
// reports each entry as [fs.ModeIrregular] and stops with EBADF at a
// descriptor lstat cannot describe either, such as the Go runtime's kqueue.
//
// AppendDir is safe to call from many goroutines at once, each with its own
// dir; two calls on one dir share its offset and split its entries between
// them. It borrows the buffer the kernel fills from a [sync.Pool], which may
// drop its buffers at any garbage collection. A walker that lists many
// directories keeps a [Lister] instead, one per goroutine.
func AppendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	return appendDir(dst, dir)
}

// Lister is declared with each platform's state, in list_native.go and
// list_other.go.

// AppendDir is the function [AppendDir] with the Lister's buffer.
func (l *Lister) AppendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	return l.appendDir(dst, dir)
}

// AppendHandle is AppendDir for a directory the caller holds as a raw
// handle on Windows or a file descriptor elsewhere, the value [os.File.Fd]
// returns, so a walker that opens directories itself need not wrap each in
// an [os.File]. The listing starts at the handle's current position. name is
// used only in errors, as [os.NewFile] uses it.
//
// AppendHandle does not close the handle or change its mode, and the caller
// must not close it during the call. A handle taken from os.File.Fd needs
// that File kept reachable until the call returns, since the File's
// finalizer closes it. On platforms other than macOS, Linux and Windows it
// returns an *os.PathError wrapping [errors.ErrUnsupported].
func (l *Lister) AppendHandle(dst []Entry, fd uintptr, name string) ([]Entry, error) {
	return l.appendHandle(dst, fd, name)
}
