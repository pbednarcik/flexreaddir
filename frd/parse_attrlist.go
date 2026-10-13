package frd

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"os"
	"slices"
	"syscall"
)

// Attribute bits from <sys/attr.h>.
const (
	attrName       = 0x00000001 // ATTR_CMN_NAME
	attrFsid       = 0x00000004 // ATTR_CMN_FSID
	attrObjtype    = 0x00000008 // ATTR_CMN_OBJTYPE
	attrModtime    = 0x00000400 // ATTR_CMN_MODTIME
	attrChgtime    = 0x00000800 // ATTR_CMN_CHGTIME
	attrFileID     = 0x02000000 // ATTR_CMN_FILEID
	attrError      = 0x20000000 // ATTR_CMN_ERROR
	attrReturned   = 0x80000000 // ATTR_CMN_RETURNED_ATTRS
	attrDataLength = 0x00000200 // ATTR_FILE_DATALENGTH, not TOTALSIZE, which counts a resource fork

	errnoNotExist = 2 // ENOENT on macOS
)

// bulkOp names the listing call in its errors.
const bulkOp = "getattrlistbulk"

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
// dst. A regular file keeps its id only where persistent says its volume's ids
// persist, asked once for each run of records from one volume. On a malformed
// record it returns the entries before it with the error.
func parseRecords(dst []Entry, buf []byte, n int, persistent func(fsid uint64) bool) ([]Entry, error) {
	dst = slices.Grow(dst, max(0, min(n, len(buf)/recordHeader)))
	var names nameBlock
	names.grow(nameBytes(buf, n))
	var fsid uint64
	asked, keep := false, false
	err := eachRecord(buf, n, func(r record) {
		e := Entry{Name: names.add(r.name), Type: r.mode}
		if r.mode.IsRegular() {
			if !asked || r.fsid != fsid {
				fsid, asked, keep = r.fsid, true, persistent(r.fsid)
			}
			e.Size, e.ModTimeNanos, e.ChangeTimeNanos = r.size, r.modTime, r.chgTime
			if keep {
				e.FileID = r.fileID
			}
		}
		dst = append(dst, e)
	})
	return dst, err
}

// nameBytes sizes the names of the n records in buf from the lengths their
// name references claim, before any record is checked: a wrong size costs
// nameBlock an allocation or some spare bytes, never a wrong name. Each name
// counts at most its record's length, so the size stays within buf.
func nameBytes(buf []byte, n int) int {
	le := binary.LittleEndian
	total, at := 0, 0
	for range n {
		if len(buf)-at < recordHeader {
			break
		}
		length := int(le.Uint32(buf[at:]))
		if length < recordHeader || length > len(buf)-at {
			break
		}
		rec := buf[at : at+length]
		at += length
		common := le.Uint32(rec[4:])
		ref := recordHeader
		if common&attrError != 0 {
			ref += 4
		}
		if common&attrName == 0 || len(rec)-ref < 8 {
			continue
		}
		if size := int64(le.Uint32(rec[ref+4:])); size > 1 { // the length counts the NUL
			total += int(min(size-1, int64(length)))
		}
	}
	return total
}

// record is one entry's attributes as the kernel returned them.
type record struct {
	name    []byte
	fsid    uint64
	mode    fs.FileMode
	size    int64
	modTime int64
	chgTime int64
	fileID  uint64
}

// eachRecord calls visit for the n records in buf. It skips an entry removed
// before its attributes were read and stops at any other entry error, as an
// *os.PathError naming the entry when the kernel returned its name.
// Attributes follow the returned groups in bit order, except that an entry's
// error comes first; an attribute the kernel could not supply is absent, and
// the returned groups say so. A name is NUL-terminated, holds no other NUL,
// and lies after the fixed-size attributes; every entry without an error has
// one.
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
		var errno uint32
		if common&attrError != 0 {
			f, ok := field(4)
			if !ok {
				return ErrMalformed
			}
			errno = le.Uint32(f)
		}
		if errno == errnoNotExist {
			continue
		}
		var r record
		var nameStart int64
		if common&attrName != 0 {
			ref := p
			f, ok := field(8)
			if !ok {
				return ErrMalformed
			}
			nameStart = int64(ref) + int64(int32(le.Uint32(f)))
			size := int64(le.Uint32(f[4:])) // the length counts the NUL
			if size < 2 || nameStart < int64(p) || nameStart+size > int64(len(rec)) {
				return ErrMalformed
			}
			r.name = rec[nameStart : nameStart+size-1]
			if rec[nameStart+size-1] != 0 || bytes.IndexByte(r.name, 0) >= 0 {
				return ErrMalformed
			}
		}
		if errno != 0 {
			if r.name == nil {
				return syscall.Errno(errno)
			}
			return &os.PathError{Op: bulkOp, Path: string(r.name), Err: syscall.Errno(errno)}
		}
		if common&attrFsid != 0 {
			f, ok := field(8)
			if !ok {
				return ErrMalformed
			}
			r.fsid = le.Uint64(f)
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
		} else {
			r.chgTime = r.modTime // a filesystem without one, such as FAT; stat(2) does the same
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
		if r.name == nil || nameStart < int64(p) {
			return ErrMalformed
		}
		visit(r)
	}
	return nil
}
