package frd

import (
	"encoding/binary"
	"io/fs"
	"slices"
	"syscall"
)

// Attribute bits from <sys/attr.h>.
const (
	attrName       = 0x00000001 // ATTR_CMN_NAME
	attrObjtype    = 0x00000008 // ATTR_CMN_OBJTYPE
	attrModtime    = 0x00000400 // ATTR_CMN_MODTIME
	attrChgtime    = 0x00000800 // ATTR_CMN_CHGTIME
	attrFileID     = 0x02000000 // ATTR_CMN_FILEID
	attrError      = 0x20000000 // ATTR_CMN_ERROR
	attrReturned   = 0x80000000 // ATTR_CMN_RETURNED_ATTRS
	attrDataLength = 0x00000200 // ATTR_FILE_DATALENGTH, not TOTALSIZE, which counts a resource fork

	errnoNotExist = 2 // ENOENT on Darwin
)

// Object types, enum vtype in <sys/vnode.h>.
const (
	typeRegular = 1
	typeDir     = 2
	typeBlock   = 3
	typeChar    = 4
	typeLink    = 5
	typeSocket  = 6
	typeFifo    = 7
)

// recordHeader is a record's length followed by its five returned attribute groups.
const recordHeader = 4 + 5*4

func vtypeMode(objtype uint32) fs.FileMode {
	switch objtype {
	case typeRegular:
		return 0
	case typeDir:
		return fs.ModeDir
	case typeLink:
		return fs.ModeSymlink
	case typeFifo:
		return fs.ModeNamedPipe
	case typeSocket:
		return fs.ModeSocket
	case typeChar:
		return fs.ModeDevice | fs.ModeCharDevice
	case typeBlock:
		return fs.ModeDevice
	}
	return fs.ModeIrregular
}

// parseRecords appends an Entry for each of the n getattrlistbulk records
// packed in buf, with one allocation for their names and at most one to grow
// dst. On a malformed record it returns the entries before it with the error.
func parseRecords(dst []Entry, buf []byte, n int) ([]Entry, error) {
	nameBytes, entries := 0, 0
	_ = eachRecord(buf, n, func(r record) {
		nameBytes += len(r.name)
		entries++
	})
	dst = slices.Grow(dst, entries)
	var names nameBlock
	names.grow(nameBytes)
	err := eachRecord(buf, n, func(r record) {
		e := Entry{Name: names.add(r.name), Type: r.mode}
		if r.mode.IsRegular() {
			e.Size, e.ModTimeNanos, e.ChangeTimeNanos, e.FileID = r.size, r.modTime, r.chgTime, r.fileID
		}
		dst = append(dst, e)
	})
	return dst, err
}

// record is one entry's attributes as the kernel returned them.
type record struct {
	name    []byte
	mode    fs.FileMode
	size    int64
	modTime int64
	chgTime int64
	fileID  uint64
}

// eachRecord calls visit for the n records in buf, skipping an entry removed
// before its attributes were read. Attributes follow the returned groups in
// bit order, except that an entry's error comes first; an attribute the
// kernel could not supply is absent, and the returned groups say so.
func eachRecord(buf []byte, n int, visit func(record)) error {
	le := binary.LittleEndian // every Mac is little-endian
	at := 0
	for range n {
		if len(buf)-at < recordHeader {
			return ErrMalformed
		}
		length := int(le.Uint32(buf[at:]))
		if length < recordHeader || length > len(buf)-at {
			return ErrMalformed
		}
		rec := buf[at : at+length]
		at += length
		common, file := le.Uint32(rec[4:]), le.Uint32(rec[16:])
		p := recordHeader
		field := func(size int) ([]byte, bool) {
			if len(rec)-p < size {
				return nil, false
			}
			f := rec[p : p+size]
			p += size
			return f, true
		}
		if common&attrError != 0 {
			f, ok := field(4)
			if !ok {
				return ErrMalformed
			}
			errno := le.Uint32(f)
			if errno == errnoNotExist {
				continue
			}
			if errno != 0 {
				return syscall.Errno(errno)
			}
		}
		var r record
		if common&attrName != 0 {
			ref := p
			f, ok := field(8)
			if !ok {
				return ErrMalformed
			}
			start := int64(ref) + int64(int32(le.Uint32(f)))
			size := int64(le.Uint32(f[4:]))
			if size < 1 || start < 0 || start+size > int64(len(rec)) {
				return ErrMalformed
			}
			r.name = rec[start : start+size-1] // the length counts the NUL
		}
		r.mode = fs.ModeIrregular
		if common&attrObjtype != 0 {
			f, ok := field(4)
			if !ok {
				return ErrMalformed
			}
			r.mode = vtypeMode(le.Uint32(f))
		}
		if common&attrModtime != 0 {
			f, ok := field(16)
			if !ok {
				return ErrMalformed
			}
			r.modTime = int64(le.Uint64(f))*1e9 + int64(le.Uint64(f[8:]))
		}
		if common&attrChgtime != 0 {
			f, ok := field(16)
			if !ok {
				return ErrMalformed
			}
			r.chgTime = int64(le.Uint64(f))*1e9 + int64(le.Uint64(f[8:]))
		}
		if common&attrFileID != 0 {
			f, ok := field(8)
			if !ok {
				return ErrMalformed
			}
			r.fileID = le.Uint64(f)
		}
		if file&attrDataLength != 0 {
			f, ok := field(8)
			if !ok {
				return ErrMalformed
			}
			r.size = int64(le.Uint64(f))
		}
		visit(r)
	}
	return nil
}
