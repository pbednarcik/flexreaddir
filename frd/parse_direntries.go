package frd

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"slices"
)

// direntryHeader is d_ino, d_seekoff, d_reclen, d_namlen and d_type of a
// macOS dirent (__DARWIN_STRUCT_DIRENTRY in <sys/dirent.h>); every Mac is
// little-endian.
const direntryHeader = 8 + 8 + 2 + 2 + 1

// dtWhiteout is DT_WHT, a union mount's deleted name.
const dtWhiteout = 14

// parseDirentries appends an Entry for each getdirentries64 record in buf,
// with one allocation for their names and at most one to grow dst. The
// record's type settles every entry but DT_UNKNOWN, which stat types from the
// name as it lies in buf, NUL after it; stat reports false for a name removed
// since the read. Nothing but Name and Type is filled.
func parseDirentries(dst []Entry, buf []byte, stat func(name []byte, e *Entry) (bool, error)) ([]Entry, error) {
	entries, nameBytes := direntrySizes(buf)
	dst = slices.Grow(dst, entries)
	var names nameBlock
	names.grow(nameBytes)
	err := eachDirentry(buf, func(name []byte, dtype byte) error {
		mode, known := direntryMode(dtype)
		dst = append(dst, Entry{Type: mode})
		e := &dst[len(dst)-1]
		if !known {
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

// direntrySizes counts the records in buf and the bytes of their names from
// the lengths the records claim, checking nothing: a wrong count costs an
// allocation, never a wrong name, and a name counts at most its record.
func direntrySizes(buf []byte) (entries, nameBytes int) {
	le := binary.LittleEndian
	for len(buf) >= direntryHeader {
		length := int(le.Uint16(buf[16:]))
		if length <= direntryHeader || length > len(buf) {
			break
		}
		entries++
		nameBytes += min(int(le.Uint16(buf[18:])), length-direntryHeader)
		buf = buf[length:]
	}
	return entries, nameBytes
}

// eachDirentry calls visit for every record in buf except ".", ".." and a
// record whose d_ino is 0, a name deleted but not yet removed. The name stays
// in buf, followed by its NUL, so visit may hand it to the kernel without a
// copy.
func eachDirentry(buf []byte, visit func(name []byte, dtype byte) error) error {
	le := binary.LittleEndian
	for len(buf) > 0 {
		if len(buf) < direntryHeader {
			return ErrMalformed
		}
		length := int(le.Uint16(buf[16:]))
		if length <= direntryHeader || length > len(buf) {
			return ErrMalformed
		}
		rec := buf[:length]
		buf = buf[length:]
		namlen := int(le.Uint16(rec[18:]))
		if namlen < 1 || namlen >= length-direntryHeader { // the NUL must fit
			return ErrMalformed
		}
		name := rec[direntryHeader : direntryHeader+namlen]
		if rec[direntryHeader+namlen] != 0 || bytes.IndexByte(name, 0) >= 0 {
			return ErrMalformed
		}
		if le.Uint64(rec) == 0 {
			continue
		}
		if name[0] == '.' && (len(name) == 1 || len(name) == 2 && name[1] == '.') {
			continue
		}
		if err := visit(name, rec[20]); err != nil {
			return err
		}
	}
	return nil
}

// direntryMode is direntMode with DT_WHT, which macOS returns and Linux does
// not, as fs.ModeIrregular.
func direntryMode(dtype byte) (fs.FileMode, bool) {
	if dtype == dtWhiteout {
		return fs.ModeIrregular, true
	}
	return direntMode(dtype)
}
