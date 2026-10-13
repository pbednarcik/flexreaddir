package frd

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

// direntry packs one macOS dirent (__DARWIN_STRUCT_DIRENTRY): inode, seek
// offset, record length, name length, type, then the NUL-terminated name.
// The record is sized as APFS sizes it, from a 24-byte base rounded up to
// eight ("." is 32 bytes, an 8-byte name 40, a 255-byte name 280), so it
// carries the slack after the NUL that real records do.
func direntry(name string, dtype byte) []byte { return direntryIno(name, dtype, 1) }

func direntryIno(name string, dtype byte, ino uint64) []byte {
	le := binary.LittleEndian
	b := le.AppendUint64(nil, ino)
	b = le.AppendUint64(b, 0)
	b = le.AppendUint16(b, 0)
	b = le.AppendUint16(b, uint16(len(name)))
	b = append(b, dtype)
	b = append(b, name...)
	b = append(b, 0)
	for len(b) < 24+len(name)+1 || len(b)%8 != 0 {
		b = append(b, 0)
	}
	le.PutUint16(b[16:], uint16(len(b)))
	return b
}

func TestTheFixtureIsSizedAsAPFSSizesARecord(t *testing.T) {
	for name, want := range map[string]int{".": 32, "eightchr": 40, strings.Repeat("n", 255): 280} {
		if got := len(direntry(name, dtRegular)); got != want {
			t.Errorf("a record for %q is %d bytes, want %d", name, got, want)
		}
	}
}

// A deleted record is skipped before its name is checked: the kernel owes
// no name to an entry it has already dropped.
func TestADeletedDirentryIsSkippedWhateverItsName(t *testing.T) {
	gone := direntryIno("gone", dtRegular, 0)
	binary.LittleEndian.PutUint16(gone[18:], 0)
	if got := direntries(t, slices.Concat(gone, direntry("kept", dtRegular))); !slices.Equal(got, []named{{"kept", dtRegular}}) {
		t.Fatalf("visited %v, want kept alone", got)
	}
}

func direntries(t *testing.T, buf []byte) []named {
	t.Helper()
	var got []named
	err := eachDirentry(buf, func(name []byte, dtype byte) error {
		got = append(got, named{string(name), dtype})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEveryDirentryButTheDotsAndTheDeletedIsVisited(t *testing.T) {
	buf := slices.Concat(direntry(".", dtDir), direntry("..", dtDir), direntry("a.go", dtRegular),
		direntryIno("deleted", dtRegular, 0), direntry("sub", dtDir), direntry(".git", dtDir), direntry("...", dtRegular))
	want := []named{{"a.go", dtRegular}, {"sub", dtDir}, {".git", dtDir}, {"...", dtRegular}}
	if got := direntries(t, buf); !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestEveryVisitedDirentryNameIsFollowedByItsNUL(t *testing.T) {
	buf := slices.Concat(direntry("a.go", dtRegular), direntry("longer-name.go", dtRegular))
	err := eachDirentry(buf, func(name []byte, _ byte) error {
		if cap(name) <= len(name) || name[:len(name)+1][len(name)] != 0 {
			t.Errorf("%q is not followed by its NUL", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTheLongestNameParsesWhole(t *testing.T) {
	long := strings.Repeat("n", 255)
	if got := direntries(t, direntry(long, dtRegular)); len(got) != 1 || got[0].name != long {
		t.Fatalf("visited %v, want the 255-byte name", got)
	}
}

func TestAMalformedDirentryIsAnErrorNotAPanic(t *testing.T) {
	whole := direntry("a.go", dtRegular)
	le := binary.LittleEndian
	zero := slices.Clone(whole)
	le.PutUint16(zero[16:], 0)
	emptyName := slices.Clone(whole)
	le.PutUint16(emptyName[18:], 0)
	namePastRecord := slices.Clone(whole)
	le.PutUint16(namePastRecord[18:], uint16(len(whole)-direntryHeader))
	noNUL := slices.Clone(whole)
	for i := direntryHeader; i < len(noNUL); i++ {
		noNUL[i] = 'x'
	}
	withNUL := direntry("a\x00b", dtRegular)
	for name, buf := range map[string][]byte{
		"shorter than its header":  whole[:12],
		"a length past the buffer": whole[:len(whole)-8],
		"a zero length":            zero,
		"a name of no bytes":       emptyName,
		"a name past its record":   namePastRecord,
		"a name with no NUL":       noNUL,
		"a NUL inside the name":    withNUL,
	} {
		err := eachDirentry(buf, func([]byte, byte) error { return nil })
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: error = %v, want ErrMalformed", name, err)
		}
	}
}

func TestADirentryVisitorErrorStopsTheWalk(t *testing.T) {
	stop := errors.New("stop")
	calls := 0
	err := eachDirentry(slices.Concat(direntry("a", dtRegular), direntry("b", dtRegular)), func([]byte, byte) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("error %v after %d calls, want stop after 1", err, calls)
	}
}

func TestEveryDirentryTypeMapsToTheModeADirEntryWouldReport(t *testing.T) {
	for dtype, want := range map[byte]typed{
		dtRegular: {0, true}, dtDir: {fs.ModeDir, true}, dtLink: {fs.ModeSymlink, true},
		dtFifo: {fs.ModeNamedPipe, true}, dtSocket: {fs.ModeSocket, true},
		dtChar: {fs.ModeDevice | fs.ModeCharDevice, true}, dtBlock: {fs.ModeDevice, true},
		dtWhiteout: {fs.ModeIrregular, true}, dtUnknown: {0, false},
	} {
		got, known := direntryMode(dtype)
		if got != want.mode || known != want.known {
			t.Errorf("d_type %d = %v %v, want %v %v", dtype, got, known, want.mode, want.known)
		}
	}
}

func TestDirentrySizesCountWithinTheBuffer(t *testing.T) {
	two := slices.Concat(direntry("a.go", dtRegular), direntry("sub", dtDir))
	long := direntry("a.go", dtRegular)
	binary.LittleEndian.PutUint16(long[18:], 0xfff0)
	for name, c := range map[string]struct {
		buf              []byte
		entries, nameLen int
	}{
		"two records":                   {two, 2, 7},
		"a name longer than its record": {long, 1, len(long) - direntryHeader},
		"a record cut short":            {two[:12], 0, 0},
	} {
		if entries, nameLen := direntrySizes(c.buf); entries != c.entries || nameLen != c.nameLen {
			t.Errorf("%s: direntrySizes = %d, %d; want %d, %d", name, entries, nameLen, c.entries, c.nameLen)
		}
	}
}

// statedType is a stat for parseDirentries that types every name it is asked
// about as a symlink, so a test can tell which entries were stat'ed.
func statedType(_ []byte, e *Entry) (bool, error) { e.Type = fs.ModeSymlink; return true, nil }

func TestParseDirentriesStatsOnlyTheUnknown(t *testing.T) {
	buf := slices.Concat(direntry("sub", dtDir), direntry("a.go", dtRegular), direntry("wh", dtWhiteout), direntry("odd", dtUnknown))
	got, err := parseDirentries(nil, buf, statedType)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "sub", Type: fs.ModeDir}, {Name: "a.go"}, {Name: "wh", Type: fs.ModeIrregular}, {Name: "odd", Type: fs.ModeSymlink}}
	if !slices.Equal(got, want) {
		t.Fatalf("parseDirentries = %+v, want %+v", got, want)
	}
}

func TestParseDirentriesLeavesOutANameRemovedBeforeItsStat(t *testing.T) {
	buf := slices.Concat(direntry("gone", dtUnknown), direntry("kept", dtUnknown))
	got, err := parseDirentries([]Entry{{Name: "earlier"}}, buf, func(name []byte, e *Entry) (bool, error) {
		if string(name) == "gone" {
			e.Type = fs.ModeDir // a stat that wrote before it found the name gone
			return false, nil
		}
		return statedType(name, e)
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Entry{{Name: "earlier"}, {Name: "kept", Type: fs.ModeSymlink}}; !slices.Equal(got, want) {
		t.Fatalf("parseDirentries = %+v, want %+v", got, want)
	}
}

func TestParseDirentriesStopsAtAStatError(t *testing.T) {
	denied := errors.New("denied")
	buf := slices.Concat(direntry("a", dtUnknown), direntry("b", dtUnknown), direntry("c", dtUnknown))
	got, err := parseDirentries(nil, buf, func(name []byte, e *Entry) (bool, error) {
		if string(name) == "b" {
			return false, denied
		}
		return statedType(name, e)
	})
	if !errors.Is(err, denied) || !slices.Equal(got, []Entry{{Name: "a", Type: fs.ModeSymlink}}) {
		t.Fatalf("parseDirentries = %+v, %v; want a and the stat error", got, err)
	}
}

func TestParseDirentriesAppendsToWhatItIsGiven(t *testing.T) {
	got, err := parseDirentries([]Entry{{Name: "first"}}, direntry("second", dtDir), statedType)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Entry{{Name: "first"}, {Name: "second", Type: fs.ModeDir}}; !slices.Equal(got, want) {
		t.Fatalf("parseDirentries = %+v, want %+v", got, want)
	}
}

// A malformed buffer names the call that filled it, not the bulk call the
// directory's name is joined under.
func TestAMalformedDirentryBufferNamesItsCall(t *testing.T) {
	_, err := parseDirentries(nil, direntry("a.go", dtRegular)[:12], statedType)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != namesOp || pe.Path != "" || !errors.Is(err, ErrMalformed) {
		t.Fatalf("parseDirentries error = %v, want a %s *os.PathError with no path wrapping ErrMalformed", err, namesOp)
	}
}
