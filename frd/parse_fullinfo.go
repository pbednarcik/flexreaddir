package frd

import (
	"encoding/binary"
	"io/fs"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

// File attribute bits and reparse tags from <winnt.h>.
const (
	attrDirectory    = 0x00000010 // FILE_ATTRIBUTE_DIRECTORY
	attrReparsePoint = 0x00000400 // FILE_ATTRIBUTE_REPARSE_POINT

	tagMountPoint = 0xA0000003 // IO_REPARSE_TAG_MOUNT_POINT
	tagSymlink    = 0xA000000C // IO_REPARSE_TAG_SYMLINK
	tagDedup      = 0x80000013 // IO_REPARSE_TAG_DEDUP
	tagAFUnix     = 0x80000023 // IO_REPARSE_TAG_AF_UNIX
	tagSurrogate  = 0x20000000 // the bit IsReparseTagNameSurrogate tests
)

// layout says where a directory record's fixed part ends and where its
// reparse tag and file id sit. FILE_FULL_DIR_INFO has no file id and carries
// the reparse tag in its EaSize slot, as os/types_windows.go reads it;
// FILE_ID_EXTD_DIR_INFO has a real EaSize at 64, the tag at 68 and a 128-bit
// FileId at 72. id is 0 where the layout has none.
type layout struct{ header, tag, id int }

var (
	fullDirInfo   = layout{header: 68, tag: 64}
	idExtdDirInfo = layout{header: 88, tag: 68, id: 72}
)

// windowsEpoch is 1601-01-01, the FILETIME origin, in 100 ns units before the
// Unix epoch.
const windowsEpoch = 116444736000000000

// parseFullInfo appends an Entry for each record of layout l chained in buf,
// with one allocation for their names and at most one to grow dst. On a
// malformed record it returns the entries before it with the error.
func parseFullInfo(dst []Entry, buf []byte, l layout) ([]Entry, error) {
	units, entries := 0, 0
	_ = eachFullInfo(buf, l, func(r fullInfo) error {
		units += len(r.name) / 2
		entries++
		return nil
	})
	dst = slices.Grow(dst, entries)
	var names nameBlock
	names.grow(units)            // exact for ASCII names; a name that needs more UTF-8 grows the block once more
	var scratch [3*255 + 32]byte // a Windows name is at most 255 units of at most 3 bytes each, and asciiRun may write 32 past them
	err := eachFullInfo(buf, l, func(r fullInfo) error {
		name, ok := appendUTF16(scratch[:0], r.name)
		if !ok {
			return ErrMalformed // a NUL unit; no Windows name holds one
		}
		e := Entry{Name: names.add(name), Type: attrMode(r.attrs, r.tag)}
		if e.Type.IsRegular() {
			e.Size, e.ModTimeNanos, e.FileID = r.size, (r.writeTime-windowsEpoch)*100, r.fileID
			if r.changeTime != 0 { // zero: a filesystem that keeps no change time
				e.ChangeTimeNanos = (r.changeTime - windowsEpoch) * 100
			}
		}
		dst = append(dst, e)
		return nil
	})
	return dst, err
}

// fullInfo is one entry as the kernel returned it; name is its UTF-16LE bytes.
type fullInfo struct {
	name       []byte
	attrs      uint32
	tag        uint32
	size       int64
	writeTime  int64  // FILETIME
	changeTime int64  // FILETIME
	fileID     uint64 // the FileId's low half when its high half is 0, else 0
}

// eachFullInfo calls visit for every record chained in buf except "." and
// "..", stopping at the first error visit returns. Records are chained by
// NextEntryOffset and the last one's is zero; the bytes after it are whatever
// the buffer held before. The name's units are the visitor's to check.
func eachFullInfo(buf []byte, l layout, visit func(fullInfo) error) error {
	le := binary.LittleEndian
	for {
		if len(buf) < l.header {
			return ErrMalformed
		}
		next := int(le.Uint32(buf))
		nameLen := int(le.Uint32(buf[60:]))
		if nameLen < 2 || nameLen%2 != 0 || nameLen > len(buf)-l.header {
			return ErrMalformed
		}
		name := buf[l.header : l.header+nameLen]
		if !dotOrDotDot(name) {
			r := fullInfo{
				name:       name,
				attrs:      le.Uint32(buf[56:]),
				tag:        le.Uint32(buf[l.tag:]),
				size:       int64(le.Uint64(buf[40:])),
				writeTime:  int64(le.Uint64(buf[24:])),
				changeTime: int64(le.Uint64(buf[32:])),
			}
			// A 128-bit id (ReFS, Dev Drive) or the all-ones "unknown" does
			// not fit FileID; 0 says unknown, never a truncated id.
			if l.id != 0 && le.Uint64(buf[l.id+8:]) == 0 {
				r.fileID = le.Uint64(buf[l.id:])
			}
			if err := visit(r); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		if next < l.header+nameLen || next > len(buf) {
			return ErrMalformed
		}
		buf = buf[next:]
	}
}

// dotOrDotDot reports whether the UTF-16LE name is "." or "..".
func dotOrDotDot(name []byte) bool {
	switch len(name) {
	case 2:
		return name[0] == '.' && name[1] == 0
	case 4:
		return name[0] == '.' && name[1] == 0 && name[2] == '.' && name[3] == 0
	}
	return false
}

// attrMode maps a listing's attributes and reparse tag to the type bits the
// standard library reports for the same entry (os/types_windows.go, Go 1.23
// semantics): a name-surrogate reparse point is never a directory, and a
// reparse tag the library does not name is irregular.
func attrMode(attrs, tag uint32) fs.FileMode {
	var m fs.FileMode
	reparse := attrs&attrReparsePoint != 0
	if attrs&attrDirectory != 0 && !(reparse && tag&tagSurrogate != 0) {
		m = fs.ModeDir
	}
	if reparse {
		switch tag {
		case tagSymlink:
			m |= fs.ModeSymlink
		case tagAFUnix:
			m |= fs.ModeSocket
		case tagDedup: // rewritten by the deduplication service, still a regular file
		default:
			m |= fs.ModeIrregular
		}
	}
	return m
}

// appendUTF16 appends the UTF-16LE units in u to b as UTF-8, reporting false
// at a NUL unit, which no Windows name holds. A leading run of units 1 to
// 0x7F is packed by asciiRun with the widest tier the CPU has; asciiRun may
// read to cap(u) and write 32 bytes past the last unit it packs, which the
// Grow below makes room for. The rest goes through appendUTF16Scalar.
func appendUTF16(b, u []byte) ([]byte, bool) {
	if units := len(u) / 2; units >= 4 {
		b = slices.Grow(b, units+32)
		n := asciiRun(unsafe.SliceData(b[len(b):cap(b)]), unsafe.SliceData(u), units, cap(u))
		b = b[:len(b)+n]
		u = u[2*n:]
	}
	return appendUTF16Scalar(b, u)
}

// appendUTF16Scalar is the Go tier and the reference the others are tested
// against: it appends the UTF-16LE units in u to b as UTF-8, one at a time,
// reporting false at a NUL unit. An unpaired surrogate becomes U+FFFD, as in
// syscall.UTF16ToString, so a name agrees with os.ReadDir's byte for byte.
func appendUTF16Scalar(b, u []byte) ([]byte, bool) {
	le := binary.LittleEndian
	for i := 0; i+1 < len(u); i += 2 {
		c := rune(le.Uint16(u[i:]))
		if c < utf8.RuneSelf {
			if c == 0 {
				return b, false
			}
			b = append(b, byte(c))
			continue
		}
		if utf16.IsSurrogate(c) {
			if i+3 < len(u) {
				if r := utf16.DecodeRune(c, rune(le.Uint16(u[i+2:]))); r != utf8.RuneError {
					b = utf8.AppendRune(b, r)
					i += 2
					continue
				}
			}
			c = utf8.RuneError
		}
		b = utf8.AppendRune(b, c)
	}
	return b, true
}
