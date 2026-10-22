package frd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

// finfo is one directory record as the kernel packs it, in either layout, for
// tests that cannot ask the kernel for a malformed one. units, when set, is
// the name as UTF-16 units and lets a test pack an unpaired surrogate. lo and
// hi are the 128-bit FileId's halves, written only in the extended layout.
type finfo struct {
	name       string
	units      []uint16
	attrs      uint32
	tag        uint32
	size       int64
	writeTime  int64
	changeTime int64
	lo, hi     uint64
}

func (r finfo) bytes(l layout, next uint32) []byte {
	le := binary.LittleEndian
	units := r.units
	if units == nil {
		units = utf16.Encode([]rune(r.name))
	}
	b := make([]byte, l.header, l.header+2*len(units))
	le.PutUint32(b, next)
	le.PutUint64(b[24:], uint64(r.writeTime))
	le.PutUint64(b[32:], uint64(r.changeTime))
	le.PutUint64(b[40:], uint64(r.size))
	le.PutUint32(b[56:], r.attrs)
	le.PutUint32(b[60:], uint32(2*len(units)))
	le.PutUint32(b[l.tag:], r.tag)
	if l.id != 0 {
		le.PutUint64(b[l.id:], r.lo)
		le.PutUint64(b[l.id+8:], r.hi)
	}
	for _, u := range units {
		b = le.AppendUint16(b, u)
	}
	return b
}

// finfosIn chains records as the kernel does: every NextEntryOffset points at
// the next 8-aligned record and the last one's is zero.
func finfosIn(l layout, rs ...finfo) []byte {
	var b []byte
	for i, r := range rs {
		rec := r.bytes(l, 0)
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

// finfos is finfosIn for the layout the package lists with.
func finfos(rs ...finfo) []byte { return finfosIn(idExtdDirInfo, rs...) }

var layouts = []struct {
	name string
	l    layout
}{{"FILE_FULL_DIR_INFO", fullDirInfo}, {"FILE_ID_EXTD_DIR_INFO", idExtdDirInfo}}

// unixFiletime is the FILETIME of sec seconds and nsec nanoseconds after the
// Unix epoch.
func unixFiletime(sec, nsec int64) int64 { return windowsEpoch + sec*1e7 + nsec/100 }

func TestAFullInfoRegularFileCarriesItsSizeTimesAndID(t *testing.T) {
	for _, lc := range layouts {
		got, err := parseFullInfo(nil, finfosIn(lc.l,
			finfo{name: "a.go", attrs: 0x20, size: 10, writeTime: unixFiletime(1700000000, 123456700), changeTime: unixFiletime(1700000001, 987654300), lo: 0x0001000000000ABC},
			finfo{name: "b.go", attrs: 0x20, size: 1, writeTime: unixFiletime(1, 0), lo: 7, hi: 1}, // no change time kept; a 128-bit id: unknown
			finfo{name: "d", attrs: attrDirectory, writeTime: unixFiletime(1, 0), changeTime: unixFiletime(2, 0), lo: 9},
		), lc.l, false)
		if err != nil {
			t.Fatal(err)
		}
		id := uint64(0)
		if lc.l.id != 0 {
			id = 0x0001000000000ABC
		}
		want := []Entry{
			{Name: "a.go", Size: 10, ModTimeNanos: 1700000000*1e9 + 123456700, ChangeTimeNanos: 1700000001*1e9 + 987654300, FileID: id},
			{Name: "b.go", Size: 1, ModTimeNanos: 1e9},
			{Name: "d", Type: fs.ModeDir},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s: got %+v, want %+v", lc.name, got, want)
		}
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
	for _, lc := range layouts {
		for _, c := range cases {
			got, err := parseFullInfo(nil, finfosIn(lc.l, finfo{name: "x", attrs: c.attrs, tag: c.tag, size: 1, writeTime: unixFiletime(1, 0)}), lc.l, false)
			if err != nil {
				t.Fatal(err)
			}
			if got[0].Type != c.want {
				t.Errorf("%s, %s: type %v, want %v", lc.name, c.name, got[0].Type, c.want)
			}
			if regular := c.want == 0; (got[0].Size != 0) != regular {
				t.Errorf("%s, %s: size %d on a %v", lc.name, c.name, got[0].Size, got[0].Type)
			}
		}
	}
}

func TestFullInfoSkipsDotAndDotDot(t *testing.T) {
	for _, lc := range layouts {
		got, err := parseFullInfo(nil, finfosIn(lc.l, finfo{name: "."}, finfo{name: ".."}, finfo{name: "..."}, finfo{name: ".a"}), lc.l, false)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range got {
			names = append(names, e.Name)
		}
		if want := []string{"...", ".a"}; !slices.Equal(names, want) {
			t.Fatalf("%s: listed %q, want %q", lc.name, names, want)
		}
	}
}

func TestAMalformedFullInfoBufferIsErrMalformedWithTheEntriesBeforeIt(t *testing.T) {
	for _, lc := range layouts {
		two := finfosIn(lc.l, finfo{name: "first"}, finfo{name: "ab"})
		second := (lc.l.header + 10 + 7) &^ 7 // "first" is 10 name bytes, padded to 8
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
			{"truncated header", two[:second+lc.l.header-1]},
			{"name past the record", set(second, 60, 1000)},
			{"odd name length", set(second, 60, 3)},
			{"zero-length name", set(second, 60, 0)},
			{"a NUL unit in the name", set(second, lc.l.header, 0)},
			{"next past the buffer", set(0, 0, 4096)},
			{"next inside its own record", set(0, 0, 8)},
		}
		for _, c := range cases {
			got, err := parseFullInfo(nil, c.buf, lc.l, false)
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("%s, %s: err = %v, want ErrMalformed", lc.name, c.name, err)
			}
			wantFirst := c.name != "empty"
			if gotFirst := len(got) == 1 && got[0].Name == "first"; gotFirst != wantFirst || len(got) > 1 {
				t.Errorf("%s, %s: entries before the error = %+v", lc.name, c.name, got)
			}
		}
	}
}

func TestFullInfoSizesCountWhatTheWalkVisits(t *testing.T) {
	for _, lc := range layouts {
		two := finfosIn(lc.l, finfo{name: "first"}, finfo{name: "ab"})
		second := (lc.l.header + 10 + 7) &^ 7 // "first" is 10 name bytes, padded to 8
		set := func(at, field int, v uint32) []byte {
			b := slices.Clone(two)
			binary.LittleEndian.PutUint32(b[at+field:], v)
			return b
		}
		// A next of 8 lands inside the first record, on bytes that read as a
		// record with a name of one unit.
		inside := set(0, 0, 8)
		binary.LittleEndian.PutUint32(inside[8+60:], 2)
		cases := []struct {
			name           string
			buf            []byte
			entries, units int
		}{
			{"the dots skipped, the units exact", finfosIn(lc.l, finfo{name: "."}, finfo{name: ".."}, finfo{name: "a.go"}, finfo{name: "straße.go"}, finfo{name: "🙂"}), 3, 15},
			{"no records", nil, 0, 0},
			{"a header cut short", two[:second+lc.l.header-1], 1, 5},
			{"a header cut to its first bytes", two[:second+10], 1, 5},
			{"a name past the record", set(second, 60, 1000), 1, 5},
			{"a name just past the buffer", set(second, 60, uint32(lc.l.header)), 1, 5},
			{"an odd name length", set(second, 60, 3), 1, 5},
			{"a zero-length name", set(second, 60, 0), 1, 5},
			{"a NUL unit, which only the decode refuses", set(second, lc.l.header, 0), 2, 7},
			{"a next past the buffer", set(0, 0, 4096), 1, 5},
			{"a next inside its own record", set(0, 0, 8), 1, 5},
			{"a next inside its own record, onto a record", inside, 1, 5},
		}
		for _, c := range cases {
			if entries, units := fullInfoSizes(c.buf, lc.l); entries != c.entries || units != c.units {
				t.Errorf("%s, %s: fullInfoSizes = %d, %d; want %d, %d", lc.name, c.name, entries, units, c.entries, c.units)
			}
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
	for _, lc := range layouts {
		for _, c := range cases {
			got, err := parseFullInfo(nil, finfosIn(lc.l, finfo{units: c.units}), lc.l, false)
			if err != nil {
				t.Fatal(err)
			}
			if oracle := string(utf16.Decode(c.units)); oracle != c.want {
				t.Fatalf("the test's own expectation %q disagrees with utf16.Decode %q", c.want, oracle)
			}
			if got[0].Name != c.want {
				t.Errorf("%s: %v decoded as %q, want %q", lc.name, c.units, got[0].Name, c.want)
			}
		}
	}
}

// TestFullInfoNamesStayWholeWhenTheirBlockGrows parses one buffer of ASCII
// names followed by names of three UTF-8 bytes a unit, which outgrow the space
// reserved for the buffer's names, and checks every name after the whole
// buffer is parsed: a name handed out must survive the names decoded after it.
func TestFullInfoNamesStayWholeWhenTheirBlockGrows(t *testing.T) {
	var rs []finfo
	var want []string
	for i := range 40 {
		n := fmt.Sprintf("ascii-%02d-%s.txt", i, strings.Repeat("x", i))
		rs, want = append(rs, finfo{name: n}), append(want, n)
	}
	for i := range 40 {
		n := fmt.Sprintf("中文-%02d-%s", i, strings.Repeat("字", 20+i))
		rs, want = append(rs, finfo{name: n}), append(want, n)
	}
	for _, lc := range layouts {
		got, err := parseFullInfo(nil, finfosIn(lc.l, rs...), lc.l, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: %d entries, want %d", lc.name, len(got), len(want))
		}
		for i, e := range got {
			if e.Name != want[i] {
				t.Errorf("%s: entry %d is %q, want %q", lc.name, i, e.Name, want[i])
			}
		}
	}
}

func TestAFullInfoNamesListingCarriesNamesAndTypesOnly(t *testing.T) {
	for _, lc := range layouts {
		got, err := parseFullInfo(nil, finfosIn(lc.l,
			finfo{name: "a.go", attrs: 0x20, size: 10, writeTime: unixFiletime(1700000000, 0), changeTime: unixFiletime(1, 0), lo: 5},
			finfo{name: "d", attrs: attrDirectory, writeTime: unixFiletime(1, 0)},
		), lc.l, true)
		if err != nil {
			t.Fatal(err)
		}
		if want := []Entry{{Name: "a.go"}, {Name: "d", Type: fs.ModeDir}}; !slices.Equal(got, want) {
			t.Fatalf("%s: got %+v, want %+v", lc.name, got, want)
		}
	}
}

// parseFullInfo fills each Entry in dst's next slot field by field, so a slot
// a walker reuses from its last listing must keep nothing of it, whatever
// fields Entry gains.
func TestParseFullInfoKeepsNothingOfAReusedSlot(t *testing.T) {
	var junk Entry
	v := reflect.ValueOf(&junk).Elem()
	for i := range v.NumField() {
		switch f := v.Field(i); f.Kind() {
		case reflect.String:
			f.SetString("junk")
		case reflect.Int64:
			f.SetInt(-1)
		case reflect.Uint32, reflect.Uint64:
			f.SetUint(^uint64(0))
		default:
			t.Fatalf("Entry.%s: a kind this test cannot fill", v.Type().Field(i).Name)
		}
	}
	buf := finfos(
		finfo{name: "a.go", attrs: 0x20, size: 1, writeTime: unixFiletime(1, 0), changeTime: unixFiletime(2, 0), lo: 5},
		finfo{name: "b.go", attrs: 0x20, size: 2, writeTime: unixFiletime(3, 0)}, // no change time, no id
		finfo{name: "d", attrs: attrDirectory, writeTime: unixFiletime(1, 0), lo: 6},
	)
	for _, names := range []bool{false, true} {
		want, err := parseFullInfo(nil, buf, idExtdDirInfo, names)
		if err != nil {
			t.Fatal(err)
		}
		reused := slices.Repeat([]Entry{junk}, len(want))
		if got, err := parseFullInfo(reused[:0], buf, idExtdDirInfo, names); err != nil || !slices.Equal(got, want) {
			t.Fatalf("names=%v: got %+v, %v; want %+v", names, got, err, want)
		}
	}
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

// eachFullInfo is the record walk parseFullInfo writes out, kept as the tests'
// reference. It calls visit for every record chained in buf except "." and
// "..", stopping at the first error visit returns.
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

// parseFullInfoByVisitor is parseFullInfo built on eachFullInfo, with each
// name copied; FuzzFullInfo holds the two equal.
func parseFullInfoByVisitor(dst []Entry, buf []byte, l layout, names bool) ([]Entry, error) {
	var block []byte
	err := eachFullInfo(buf, l, func(r fullInfo) error {
		var ok bool
		if block, ok = appendUTF16(block[:0], r.name); !ok {
			return ErrMalformed
		}
		e := Entry{Name: string(block), Type: attrMode(r.attrs, r.tag)}
		if !names && e.Type.IsRegular() {
			e.Size, e.ModTimeNanos, e.FileID = r.size, (r.writeTime-windowsEpoch)*100, r.fileID
			if r.changeTime != 0 {
				e.ChangeTimeNanos = (r.changeTime - windowsEpoch) * 100
			}
		}
		dst = append(dst, e)
		return nil
	})
	return dst, err
}
