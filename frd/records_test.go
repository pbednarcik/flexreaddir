package frd

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

// rec is one getattrlistbulk record as the kernel packs it, for tests that
// cannot ask the kernel for a malformed one.
type rec struct {
	common, file uint32 // returned attribute groups; attrReturned is added
	errno        uint32
	name         string
	objtype      uint32
	sec, nsec    int64
	size         int64
}

func (r rec) bytes() []byte {
	le := binary.LittleEndian
	common := r.common | attrReturned
	b := le.AppendUint32(nil, 0)
	for _, g := range []uint32{common, 0, 0, r.file, 0} {
		b = le.AppendUint32(b, g)
	}
	if common&attrError != 0 {
		b = le.AppendUint32(b, r.errno)
	}
	ref := -1
	if common&attrName != 0 {
		ref = len(b)
		b = append(b, make([]byte, 8)...)
	}
	if common&attrObjtype != 0 {
		b = le.AppendUint32(b, r.objtype)
	}
	if common&attrModtime != 0 {
		b = le.AppendUint64(b, uint64(r.sec))
		b = le.AppendUint64(b, uint64(r.nsec))
	}
	if r.file&attrDataLength != 0 {
		b = le.AppendUint64(b, uint64(r.size))
	}
	if ref >= 0 {
		le.PutUint32(b[ref:], uint32(len(b)-ref))
		le.PutUint32(b[ref+4:], uint32(len(r.name)+1))
		b = append(b, r.name...)
		b = append(b, 0)
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
	}
	le.PutUint32(b, uint32(len(b)))
	return b
}

func records(rs ...rec) []byte {
	var b []byte
	for _, r := range rs {
		b = append(b, r.bytes()...)
	}
	return b
}

const every = attrName | attrError | attrObjtype | attrModtime

func TestARegularFileCarriesItsSizeAndModificationTime(t *testing.T) {
	buf := records(rec{common: every, file: attrDataLength, name: "a.go", objtype: typeRegular, sec: 1700000000, nsec: 123456789, size: 42})
	got, err := parseRecords(nil, buf, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := Entry{Name: "a.go", Size: 42, ModTimeNanos: 1700000000*1e9 + 123456789}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("parseRecords = %+v, want [%+v]", got, want)
	}
}

func TestOnlyARegularFileCarriesASizeAndATime(t *testing.T) {
	buf := records(rec{common: every, name: "d", objtype: typeDir, sec: 1700000000, nsec: 1})
	got, err := parseRecords(nil, buf, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Entry{Name: "d", Type: fs.ModeDir}); len(got) != 1 || got[0] != want {
		t.Fatalf("parseRecords = %+v, want [%+v]", got, want)
	}
}

func TestEveryObjectTypeMapsToTheModeADirEntryWouldReport(t *testing.T) {
	for objtype, want := range map[uint32]fs.FileMode{
		typeRegular: 0,
		typeDir:     fs.ModeDir,
		typeLink:    fs.ModeSymlink,
		typeFifo:    fs.ModeNamedPipe,
		typeSocket:  fs.ModeSocket,
		typeChar:    fs.ModeDevice | fs.ModeCharDevice,
		typeBlock:   fs.ModeDevice,
		0:           fs.ModeIrregular,
	} {
		got, err := parseRecords(nil, records(rec{common: every, name: "x", objtype: objtype}), 1)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Type != want {
			t.Errorf("object type %d = %v, want %v", objtype, got[0].Type, want)
		}
	}
}

func TestAnEntryRemovedAfterTheListingIsSkipped(t *testing.T) {
	buf := records(
		rec{common: attrName | attrError, errno: errnoNotExist, name: "gone"},
		rec{common: every, file: attrDataLength, name: "kept", objtype: typeRegular, size: 1},
	)
	got, err := parseRecords(nil, buf, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "kept" {
		t.Fatalf("parseRecords = %+v, want only kept", got)
	}
}

func TestAnyOtherEntryErrorFailsTheListing(t *testing.T) {
	const eacces = 13
	_, err := parseRecords(nil, records(rec{common: attrName | attrError, errno: eacces, name: "locked"}), 1)
	if !errors.Is(err, syscall.Errno(eacces)) {
		t.Fatalf("parseRecords error = %v, want errno %d", err, eacces)
	}
}

func TestParseAppendsToWhatItIsGiven(t *testing.T) {
	got, err := parseRecords([]Entry{{Name: "first"}}, records(rec{common: every, name: "second", objtype: typeDir}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Fatalf("parseRecords = %+v, want first then second", got)
	}
}

func TestAMalformedBufferIsAnErrorNotAPanic(t *testing.T) {
	whole := records(rec{common: every, file: attrDataLength, name: "a.go", objtype: typeRegular, size: 1})
	nameEscapes := records(rec{common: every, name: "a.go", objtype: typeDir})
	nameEscapes[29] = 0xff // the name's offset, after length, groups and error, now points past the record
	for name, c := range map[string]struct {
		buf []byte
		n   int
	}{
		"a record shorter than its header": {whole[:12], 1},
		"a length past the buffer":         {whole[:len(whole)-4], 1},
		"more records claimed than held":   {whole, 2},
		"a name outside its record":        {nameEscapes, 1},
	} {
		if _, err := parseRecords(nil, c.buf, c.n); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: parseRecords error = %v, want ErrMalformed", name, err)
		}
	}
}
