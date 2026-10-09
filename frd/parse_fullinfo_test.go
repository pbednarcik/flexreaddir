package frd

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"slices"
	"testing"
	"unicode/utf16"
)

// finfo is one FILE_FULL_DIR_INFO record as the kernel packs it, for tests
// that cannot ask the kernel for a malformed one. units, when set, is the
// name as UTF-16 units and lets a test pack an unpaired surrogate.
type finfo struct {
	name       string
	units      []uint16
	attrs      uint32
	tag        uint32
	size       int64
	writeTime  int64
	changeTime int64
}

func (r finfo) bytes(next uint32) []byte {
	le := binary.LittleEndian
	units := r.units
	if units == nil {
		units = utf16.Encode([]rune(r.name))
	}
	b := make([]byte, fullInfoHeader, fullInfoHeader+2*len(units))
	le.PutUint32(b, next)
	le.PutUint64(b[24:], uint64(r.writeTime))
	le.PutUint64(b[32:], uint64(r.changeTime))
	le.PutUint64(b[40:], uint64(r.size))
	le.PutUint32(b[56:], r.attrs)
	le.PutUint32(b[60:], uint32(2*len(units)))
	le.PutUint32(b[64:], r.tag)
	for _, u := range units {
		b = le.AppendUint16(b, u)
	}
	return b
}

// finfos chains records as the kernel does: every NextEntryOffset points at
// the next 8-aligned record and the last one's is zero.
func finfos(rs ...finfo) []byte {
	var b []byte
	for i, r := range rs {
		rec := r.bytes(0)
		if i < len(rs)-1 {
			for len(rec)%8 != 0 {
				rec = append(rec, 0)
			}
			binary.LittleEndian.PutUint32(rec, uint32(len(rec)))
		}
		b = append(b, rec...)
	}
	return b
}

// unixFiletime is the FILETIME of sec seconds and nsec nanoseconds after the
// Unix epoch.
func unixFiletime(sec, nsec int64) int64 { return windowsEpoch + sec*1e7 + nsec/100 }

func TestAFullInfoRegularFileCarriesItsSizeAndTimes(t *testing.T) {
	got, err := parseFullInfo(nil, finfos(
		finfo{name: "a.go", attrs: 0x20, size: 10, writeTime: unixFiletime(1700000000, 123456700), changeTime: unixFiletime(1700000001, 987654300)},
		finfo{name: "b.go", attrs: 0x20, size: 1, writeTime: unixFiletime(1, 0)}, // no change time kept
		finfo{name: "d", attrs: attrDirectory, writeTime: unixFiletime(1, 0), changeTime: unixFiletime(2, 0)},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: "a.go", Size: 10, ModTimeNanos: 1700000000*1e9 + 123456700, ChangeTimeNanos: 1700000001*1e9 + 987654300},
		{Name: "b.go", Size: 1, ModTimeNanos: 1e9},
		{Name: "d", Type: fs.ModeDir},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Every row is what os.ReadDir(...).Type() reports for the same attributes
// and tag under Go 1.23 semantics (os/types_windows.go, fileStat.mode).
func TestFullInfoTypesAreTheStandardLibrarys(t *testing.T) {
	cases := []struct {
		name  string
		attrs uint32
		tag   uint32
		want  fs.FileMode
	}{
		{"regular file", 0x20, 0, 0},
		{"directory", attrDirectory, 0, fs.ModeDir},
		{"symlink to a file", attrReparsePoint, tagSymlink, fs.ModeSymlink},
		{"symlink to a directory", attrReparsePoint | attrDirectory, tagSymlink, fs.ModeSymlink},
		{"junction", attrReparsePoint | attrDirectory, tagMountPoint, fs.ModeIrregular},
		{"unix socket", attrReparsePoint, tagAFUnix, fs.ModeSocket},
		{"deduplicated file", attrReparsePoint, tagDedup, 0},
		{"deduplicated directory", attrReparsePoint | attrDirectory, tagDedup, fs.ModeDir},
		{"cloud placeholder file", attrReparsePoint, 0x9000001A, fs.ModeIrregular},
		{"cloud placeholder directory", attrReparsePoint | attrDirectory, 0x9000001A, fs.ModeDir | fs.ModeIrregular},
	}
	for _, c := range cases {
		got, err := parseFullInfo(nil, finfos(finfo{name: "x", attrs: c.attrs, tag: c.tag, size: 1, writeTime: unixFiletime(1, 0)}))
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Type != c.want {
			t.Errorf("%s: type %v, want %v", c.name, got[0].Type, c.want)
		}
		if regular := c.want == 0; (got[0].Size != 0) != regular {
			t.Errorf("%s: size %d on a %v", c.name, got[0].Size, got[0].Type)
		}
	}
}

func TestFullInfoSkipsDotAndDotDot(t *testing.T) {
	got, err := parseFullInfo(nil, finfos(finfo{name: "."}, finfo{name: ".."}, finfo{name: "..."}, finfo{name: ".a"}))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if want := []string{"...", ".a"}; !slices.Equal(names, want) {
		t.Fatalf("listed %q, want %q", names, want)
	}
}

func TestAMalformedFullInfoBufferIsErrMalformedWithTheEntriesBeforeIt(t *testing.T) {
	two := finfos(finfo{name: "first"}, finfo{name: "ab"})
	const second = 80 // "first" is 68 + 10 bytes, padded to 8; "ab" then fills 4 name bytes
	set := func(at, field int, v uint32) []byte {
		b := slices.Clone(two)
		binary.LittleEndian.PutUint32(b[at+field:], v)
		return b
	}
	cases := []struct {
		name string
		buf  []byte
	}{
		{"empty", nil},
		{"truncated header", two[:second+fullInfoHeader-1]},
		{"name past the record", set(second, 60, 1000)},
		{"odd name length", set(second, 60, 3)},
		{"zero-length name", set(second, 60, 0)},
		{"a NUL unit in the name", set(second, fullInfoHeader, 0)},
		{"next past the buffer", set(0, 0, 4096)},
		{"next inside its own record", set(0, 0, 8)},
	}
	for _, c := range cases {
		got, err := parseFullInfo(nil, c.buf)
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", c.name, err)
		}
		wantFirst := c.name != "empty"
		if gotFirst := len(got) == 1 && got[0].Name == "first"; gotFirst != wantFirst || len(got) > 1 {
			t.Errorf("%s: entries before the error = %+v", c.name, got)
		}
	}
}

func TestFullInfoNamesAreDecodedFromUTF16(t *testing.T) {
	cases := []struct {
		units []uint16
		want  string
	}{
		{utf16.Encode([]rune("plain.go")), "plain.go"},
		{utf16.Encode([]rune("straße.go")), "straße.go"},
		{utf16.Encode([]rune("🙂.go")), "🙂.go"},
		{utf16.Encode([]rune("a🙂ß")), "a🙂ß"},
		{[]uint16{0xD83D, '.', 'g', 'o'}, "�.go"}, // a high surrogate with no low after it
		{[]uint16{0xDE42}, "�"},                   // a lone low surrogate
		{[]uint16{'a', 0xD83D}, "a�"},             // a high surrogate ending the name
	}
	for _, c := range cases {
		got, err := parseFullInfo(nil, finfos(finfo{units: c.units}))
		if err != nil {
			t.Fatal(err)
		}
		if oracle := string(utf16.Decode(c.units)); oracle != c.want {
			t.Fatalf("the test's own expectation %q disagrees with utf16.Decode %q", c.want, oracle)
		}
		if got[0].Name != c.want {
			t.Errorf("%v decoded as %q, want %q", c.units, got[0].Name, c.want)
		}
	}
}
