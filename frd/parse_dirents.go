package frd

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"slices"
)

// Linux directory entry types, DT_* in <dirent.h>.
const (
	dtUnknown = 0
	dtFifo    = 1
	dtChar    = 2
	dtDir     = 4
	dtBlock   = 6
	dtRegular = 8
	dtLink    = 10
	dtSocket  = 12
)

// direntHeader is d_ino, d_off, d_reclen and d_type of a linux_dirent64,
// which the kernel writes in the machine's byte order.
const direntHeader = 8 + 8 + 2 + 1

// parseDirents appends an Entry for each linux_dirent64 record in buf, with
// one allocation for their names and at most one to grow dst. The record's
// type settles every entry but a regular file or an unknown type, which stat
// describes from the name as it lies in buf, NUL after it; stat reports false
// for a name removed since the read. stat fills the Entry where it lies in
// dst, so no Entry is copied on the way. Under namesOnly a regular file is
// not stat'ed and stat fills Type only.
func parseDirents(dst []Entry, buf []byte, stat func(name []byte, e *Entry) (bool, error), namesOnly bool) ([]Entry, error) {
	nameBytes, entries := 0, 0
	_ = eachDirent(buf, func(name []byte, _ byte) error {
		nameBytes += len(name)
		entries++
		return nil
	})
	dst = slices.Grow(dst, entries)
	var names nameBlock
	names.grow(nameBytes)
	err := eachDirent(buf, func(name []byte, dtype byte) error {
		mode, known := direntMode(dtype)
		dst = append(dst, Entry{Type: mode})
		e := &dst[len(dst)-1]
		if !known || !namesOnly && mode.IsRegular() {
			if found, err := stat(name, e); !found || err != nil {
				dst = dst[:len(dst)-1]
				return err
			}
		}
		e.Name = names.add(name)
		return nil
	})
	return dst, err
}

// eachDirent calls visit for every linux_dirent64 record in buf except "."
// and "..". The name stays in buf, followed by its NUL, so visit may hand it
// to the kernel without a copy.
func eachDirent(buf []byte, visit func(name []byte, dtype byte) error) error {
	for len(buf) > 0 {
		if len(buf) < direntHeader {
			return ErrMalformed
		}
		length := int(binary.NativeEndian.Uint16(buf[16:]))
		if length <= direntHeader || length > len(buf) {
			return ErrMalformed
		}
		rec := buf[:length]
		buf = buf[length:]
		name := rec[direntHeader:]
		end := bytes.IndexByte(name, 0)
		if end < 1 {
			return ErrMalformed
		}
		name = name[:end]
		if name[0] == '.' && (len(name) == 1 || len(name) == 2 && name[1] == '.') {
			continue
		}
		if err := visit(name, rec[direntHeader-1]); err != nil {
			return err
		}
	}
	return nil
}

// direntMode reports false for DT_UNKNOWN, which some filesystems return for
// every entry, and for any type it does not name; only a stat resolves those.
func direntMode(dtype byte) (fs.FileMode, bool) {
	switch dtype {
	case dtRegular:
		return 0, true
	case dtDir:
		return fs.ModeDir, true
	case dtLink:
		return fs.ModeSymlink, true
	case dtFifo:
		return fs.ModeNamedPipe, true
	case dtSocket:
		return fs.ModeSocket, true
	case dtChar:
		return fs.ModeDevice | fs.ModeCharDevice, true
	case dtBlock:
		return fs.ModeDevice, true
	}
	return 0, false
}

// statMode maps the S_IFMT bits of st_mode, which are the same on every Unix.
func statMode(mode uint32) fs.FileMode {
	switch mode & 0o170000 {
	case 0o100000:
		return 0
	case 0o040000:
		return fs.ModeDir
	case 0o120000:
		return fs.ModeSymlink
	case 0o010000:
		return fs.ModeNamedPipe
	case 0o140000:
		return fs.ModeSocket
	case 0o020000:
		return fs.ModeDevice | fs.ModeCharDevice
	case 0o060000:
		return fs.ModeDevice
	}
	return fs.ModeIrregular
}
