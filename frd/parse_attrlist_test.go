package frd

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"slices"
	"syscall"
	"testing"
)

// rec is one getattrlistbulk record as the kernel packs it, for tests that
// cannot ask the kernel for a malformed one.
type rec struct {
	common, file uint32 // returned attribute groups; attrReturned is added
	errno        uint32
	name         string
	fsid         uint64
	objtype      uint32
	sec, nsec    int64
	chg, chgNsec int64
	fileID       uint64
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
	if common&attrFsid != 0 {
		b = le.AppendUint64(b, r.fsid)
	}
	if common&attrObjtype != 0 {
		b = le.AppendUint32(b, r.objtype)
	}
	if common&attrModtime != 0 {
		b = le.AppendUint64(b, uint64(r.sec))
		b = le.AppendUint64(b, uint64(r.nsec))
	}
	if common&attrChgtime != 0 {
		b = le.AppendUint64(b, uint64(r.chg))
		b = le.AppendUint64(b, uint64(r.chgNsec))
	}
	if common&attrFileID != 0 {
		b = le.AppendUint64(b, r.fileID)
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

// keepIDs is a volume whose file ids persist.
func keepIDs(uint64) bool { return true }

const every = attrName | attrError | attrFsid | attrObjtype | attrModtime | attrChgtime | attrFileID

func TestARegularFileCarriesItsSizeAndModificationTime(t *testing.T) {
	buf := records(rec{common: every, file: attrDataLength, name: "a.go", objtype: typeRegular, sec: 1700000000, nsec: 123456789, size: 42})
	got, err := parseRecords(nil, buf, 1, keepIDs)
	if err != nil {
		t.Fatal(err)
	}
	want := Entry{Name: "a.go", Size: 42, ModTimeNanos: 1700000000*1e9 + 123456789}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("parseRecords = %+v, want [%+v]", got, want)
	}
}

func TestARegularFileCarriesItsChangeTimeAndFileID(t *testing.T) {
	buf := records(rec{common: every, file: attrDataLength, name: "a.go", objtype: typeRegular, sec: 1700000000, chg: 1700000001, chgNsec: 987654321, fileID: 1 << 40, size: 42})
	got, err := parseRecords(nil, buf, 1, keepIDs)
	if err != nil {
		t.Fatal(err)
	}
	want := Entry{Name: "a.go", Size: 42, ModTimeNanos: 1700000000 * 1e9, ChangeTimeNanos: 1700000001*1e9 + 987654321, FileID: 1 << 40}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("parseRecords = %+v, want [%+v]", got, want)
	}
}

// FAT keeps no change time and its records carry none; stat(2) reports the
// modification time in its place, and so does the listing.
func TestAFileWithoutAChangeTimeTakesItsModificationTime(t *testing.T) {
	buf := records(rec{common: every &^ attrChgtime, file: attrDataLength, name: "a.go", objtype: typeRegular, sec: 1700000000, nsec: 5, fileID: 9, size: 42})
	got, err := parseRecords(nil, buf, 1, keepIDs)
	if err != nil {
		t.Fatal(err)
	}
	want := Entry{Name: "a.go", Size: 42, ModTimeNanos: 1700000000*1e9 + 5, ChangeTimeNanos: 1700000000*1e9 + 5, FileID: 9}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("parseRecords = %+v, want [%+v]", got, want)
	}
}

// FAT32, exFAT and NFS do not keep an id across mounts, and FSKit gives an
// empty FAT file a new one on every listing, so a file on a volume whose ids
// do not persist carries none. The question is the volume's, per record.
func TestAFileOnAVolumeWhoseIDsDoNotPersistHasNoFileID(t *testing.T) {
	buf := records(
		rec{common: every, file: attrDataLength, name: "fat.go", fsid: 7, objtype: typeRegular, sec: 1, chg: 2, fileID: 9, size: 3},
		rec{common: every, file: attrDataLength, name: "apfs.go", fsid: 8, objtype: typeRegular, sec: 1, chg: 2, fileID: 9, size: 3},
	)
	got, err := parseRecords(nil, buf, 2, func(fsid uint64) bool { return fsid != 7 })
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: "fat.go", Size: 3, ModTimeNanos: 1e9, ChangeTimeNanos: 2e9},
		{Name: "apfs.go", Size: 3, ModTimeNanos: 1e9, ChangeTimeNanos: 2e9, FileID: 9},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parseRecords = %+v, want %+v", got, want)
	}
}

// The answer is cached by the caller, but a buffer of one volume's files asks
// it once, not once per file.
func TestTheVolumeIsAskedOnceForABufferOfItsFiles(t *testing.T) {
	var rs []rec
	for _, name := range []string{"a", "b", "c"} {
		rs = append(rs, rec{common: every, file: attrDataLength, name: name, fsid: 7, objtype: typeRegular, fileID: 1})
	}
	asked := 0
	if _, err := parseRecords(nil, records(rs...), 3, func(uint64) bool { asked++; return true }); err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Fatalf("the volume was asked %d times, want once", asked)
	}
}

func TestOnlyARegularFileCarriesASizeAndATime(t *testing.T) {
	buf := records(rec{common: every, name: "d", objtype: typeDir, sec: 1700000000, nsec: 1, chg: 1700000000, fileID: 7})
	got, err := parseRecords(nil, buf, 1, keepIDs)
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
		got, err := parseRecords(nil, records(rec{common: every, name: "x", objtype: objtype}), 1, keepIDs)
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
	got, err := parseRecords(nil, buf, 2, keepIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "kept" {
		t.Fatalf("parseRecords = %+v, want only kept", got)
	}
}

func TestAnyOtherEntryErrorFailsTheListingNamingTheEntry(t *testing.T) {
	const eacces = 13
	_, err := parseRecords(nil, records(rec{common: attrName | attrError, errno: eacces, name: "locked"}), 1, keepIDs)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != bulkOp || pe.Path != "locked" || !errors.Is(err, syscall.Errno(eacces)) {
		t.Fatalf("parseRecords error = %v, want a getattrlistbulk *os.PathError naming locked with errno %d", err, eacces)
	}
}

// The kernel returns the name with an entry's error; without one the error
// stands alone, and the listing names the directory.
func TestAnEntryErrorWithoutANameIsTheBareErrno(t *testing.T) {
	const eacces = 13
	_, err := parseRecords(nil, records(rec{common: attrError, errno: eacces}), 1, keepIDs)
	if _, ok := errors.AsType[*os.PathError](err); ok || !errors.Is(err, syscall.Errno(eacces)) {
		t.Fatalf("parseRecords error = %v, want the bare errno %d", err, eacces)
	}
}

func TestParseAppendsToWhatItIsGiven(t *testing.T) {
	got, err := parseRecords([]Entry{{Name: "first"}}, records(rec{common: every, name: "second", objtype: typeDir}), 1, keepIDs)
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
	cut := func(r rec, short int) []byte {
		b := r.bytes()
		b = b[:len(b)-short]
		binary.LittleEndian.PutUint32(b, uint32(len(b)))
		return b
	}
	for name, c := range map[string]struct {
		buf []byte
		n   int
	}{
		"a record shorter than its header": {whole[:12], 1},
		"a length past the buffer":         {whole[:len(whole)-4], 1},
		"more records claimed than held":   {whole, 2},
		"a name outside its record":        {nameEscapes, 1},
		"a change time cut short":          {cut(rec{common: attrChgtime}, 8), 1},
		"a file id cut short":              {cut(rec{common: attrFileID}, 4), 1},
		"a volume id cut short":            {cut(rec{common: attrFsid}, 4), 1},
	} {
		if _, err := parseRecords(nil, c.buf, c.n, keepIDs); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: parseRecords error = %v, want ErrMalformed", name, err)
		}
	}
}
