package frd

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

// Entry describes one name in a directory listing.
//
// Size and ModTimeNanos describe regular files only; for every other type
// they are zero on every platform, so an Entry compares the same everywhere.
type Entry struct {
	// Name is the entry's name within its directory, without a path. The
	// names from one read share one allocation, so a Name kept long after
	// its neighbours are gone is worth a strings.Clone.
	Name string

	// Type holds the file type bits only, as fs.DirEntry.Type reports them:
	// zero for a regular file, fs.ModeDir, fs.ModeSymlink and so on, and
	// fs.ModeIrregular for a type the platform does not name.
	Type fs.FileMode

	// Size is the length of a regular file's data in bytes.
	Size int64

	// ModTimeNanos is a regular file's modification time in nanoseconds
	// since the Unix epoch. An integer keeps Entry comparable with == and
	// a change check to one comparison; ModTime converts it.
	ModTimeNanos int64
}

// ModTime returns the entry's modification time, which is the zero Unix time
// for anything but a regular file.
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
// Any other failure is returned as an *os.PathError, together with dst
// extended by the entries read before it.
func AppendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	return appendDir(dst, dir)
}
