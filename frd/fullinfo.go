package frd

import (
	"encoding/binary"
	"io/fs"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
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

// fullInfoHeader is FILE_FULL_DIR_INFO up to FileName.
const fullInfoHeader = 68

// windowsEpoch is 1601-01-01, the FILETIME origin, in 100 ns units before the
// Unix epoch.
const windowsEpoch = 116444736000000000

// parseFullInfo appends an Entry for each FILE_FULL_DIR_INFO record chained
// in buf, with one allocation for their names and at most one to grow dst. On
// a malformed record it returns the entries before it with the error.
func parseFullInfo(dst []Entry, buf []byte) ([]Entry, error) {
	units, entries := 0, 0
	_ = eachFullInfo(buf, func(r fullInfo) error {
		units += len(r.name) / 2
		entries++
		return nil
	})
	dst = slices.Grow(dst, entries)
	var names nameBlock
	names.grow(units)         // exact for ASCII names; a name that needs more UTF-8 grows the block once more
	var scratch [3 * 255]byte // a Windows name is at most 255 units of at most 3 bytes each
	err := eachFullInfo(buf, func(r fullInfo) error {
		name, ok := appendUTF16(scratch[:0], r.name)
		if !ok {
			return ErrMalformed // a NUL unit; no Windows name holds one
		}
		e := Entry{Name: names.add(name), Type: attrMode(r.attrs, r.tag)}
		if e.Type.IsRegular() {
			e.Size, e.ModTimeNanos = r.size, (r.writeTime-windowsEpoch)*100
		}
		dst = append(dst, e)
		return nil
	})
	return dst, err
}

// fullInfo is one entry as the kernel returned it; name is its UTF-16LE bytes.
type fullInfo struct {
	name      []byte
	attrs     uint32
	tag       uint32
	size      int64
	writeTime int64 // FILETIME
}

// eachFullInfo calls visit for every record chained in buf except "." and
// "..", stopping at the first error visit returns. Records are chained by
// NextEntryOffset and the last one's is zero; the bytes after it are whatever
// the buffer held before. The name's units are the visitor's to check.
func eachFullInfo(buf []byte, visit func(fullInfo) error) error {
	le := binary.LittleEndian
	for {
		if len(buf) < fullInfoHeader {
			return ErrMalformed
		}
		next := int(le.Uint32(buf))
		nameLen := int(le.Uint32(buf[60:]))
		if nameLen < 2 || nameLen%2 != 0 || nameLen > len(buf)-fullInfoHeader {
			return ErrMalformed
		}
		name := buf[fullInfoHeader : fullInfoHeader+nameLen]
		if !dotOrDotDot(name) {
			err := visit(fullInfo{
				name:      name,
				attrs:     le.Uint32(buf[56:]),
				tag:       le.Uint32(buf[64:]),
				size:      int64(le.Uint64(buf[40:])),
				writeTime: int64(le.Uint64(buf[24:])),
			})
			if err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		if next < fullInfoHeader+nameLen || next > len(buf) {
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
// at a NUL unit, which no Windows name holds. An unpaired surrogate becomes
// U+FFFD, as in syscall.UTF16ToString, so a name agrees with os.ReadDir's
// byte for byte.
func appendUTF16(b, u []byte) ([]byte, bool) {
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
