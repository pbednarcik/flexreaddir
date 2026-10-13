package frd

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"slices"
	"testing"
)

// dirent packs one linux_dirent64 record: inode, offset, record length,
// type, then the NUL-terminated name, the record padded to a multiple of
// eight bytes.
func dirent(name string, dtype byte) []byte {
	ne := binary.NativeEndian
	b := ne.AppendUint64(nil, 1)
	b = ne.AppendUint64(b, 0)
	b = ne.AppendUint16(b, 0)
	b = append(b, dtype)
	b = append(b, name...)
	b = append(b, 0)
	for len(b)%8 != 0 {
		b = append(b, 0)
	}
	ne.PutUint16(b[16:], uint16(len(b)))
	return b
}

type named struct {
	name  string
	dtype byte
}

func dirents(t *testing.T, buf []byte) []named {
	t.Helper()
	var got []named
	err := eachDirent(buf, func(name []byte, dtype byte) error {
		got = append(got, named{string(name), dtype})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEveryDirentButTheDotsIsVisited(t *testing.T) {
	buf := slices.Concat(dirent(".", dtDir), dirent("..", dtDir), dirent("a.go", dtRegular), dirent("sub", dtDir))
	want := []named{{"a.go", dtRegular}, {"sub", dtDir}}
	if got := dirents(t, buf); !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

// On Linux the listing hands a name to statx in place, so the byte after it
// in the buffer must be its NUL.
func TestEveryVisitedNameIsFollowedByItsNUL(t *testing.T) {
	buf := slices.Concat(dirent("a.go", dtRegular), dirent("longer-name.go", dtRegular))
	err := eachDirent(buf, func(name []byte, _ byte) error {
		if cap(name) <= len(name) || name[:len(name)+1][len(name)] != 0 {
			t.Errorf("%q is not followed by its NUL", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestADotPrefixedNameIsNotADot(t *testing.T) {
	want := []named{{".git", dtDir}, {"...", dtRegular}}
	if got := dirents(t, slices.Concat(dirent(".git", dtDir), dirent("...", dtRegular))); !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestAMalformedDirentIsAnErrorNotAPanic(t *testing.T) {
	whole := dirent("a.go", dtRegular)
	zero := slices.Clone(whole)
	binary.NativeEndian.PutUint16(zero[16:], 0)
	noNUL := slices.Clone(whole)
	for i := 19; i < len(noNUL); i++ {
		noNUL[i] = 'x'
	}
	for name, buf := range map[string][]byte{
		"shorter than its header":  whole[:10],
		"a length past the buffer": whole[:len(whole)-8],
		"a zero length":            zero,
		"a name with no NUL":       noNUL,
	} {
		err := eachDirent(buf, func([]byte, byte) error { return nil })
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: error = %v, want ErrMalformed", name, err)
		}
	}
}

func TestAVisitorErrorStopsTheWalk(t *testing.T) {
	stop := errors.New("stop")
	calls := 0
	err := eachDirent(slices.Concat(dirent("a", dtRegular), dirent("b", dtRegular)), func([]byte, byte) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("error %v after %d calls, want stop after 1", err, calls)
	}
}

type typed struct {
	mode  fs.FileMode
	known bool
}

func TestEveryDirentTypeMapsToTheModeADirEntryWouldReport(t *testing.T) {
	for dtype, want := range map[byte]typed{
		dtRegular: {0, true}, dtDir: {fs.ModeDir, true}, dtLink: {fs.ModeSymlink, true},
		dtFifo: {fs.ModeNamedPipe, true}, dtSocket: {fs.ModeSocket, true},
		dtChar: {fs.ModeDevice | fs.ModeCharDevice, true}, dtBlock: {fs.ModeDevice, true},
		dtUnknown: {0, false},
	} {
		got, known := direntMode(dtype)
		if got != want.mode || known != want.known {
			t.Errorf("d_type %d = %v %v, want %v %v", dtype, got, known, want.mode, want.known)
		}
	}
}

func TestEveryStatTypeMapsToTheModeADirEntryWouldReport(t *testing.T) {
	for ifmt, want := range map[uint32]fs.FileMode{
		0o100000: 0, 0o040000: fs.ModeDir, 0o120000: fs.ModeSymlink, 0o010000: fs.ModeNamedPipe,
		0o140000: fs.ModeSocket, 0o020000: fs.ModeDevice | fs.ModeCharDevice, 0o060000: fs.ModeDevice,
	} {
		if got := statMode(ifmt | 0o644); got != want {
			t.Errorf("st_mode %o = %v, want %v", ifmt, got, want)
		}
	}
}

// stated is a stat for parseDirents that gives every name it is asked about
// a size, so a test can tell which entries were stat'ed.
func stated(_ []byte, e *Entry) (bool, error) { e.Size = 7; return true, nil }

func TestParseDirentsStatsOnlyWhatTheRecordTypeDoesNotSettle(t *testing.T) {
	buf := slices.Concat(dirent("sub", dtDir), dirent("a.go", dtRegular), dirent("odd", dtUnknown))
	got, err := parseDirents(nil, buf, stated, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "sub", Type: fs.ModeDir}, {Name: "a.go", Size: 7}, {Name: "odd", Size: 7}}
	if !slices.Equal(got, want) {
		t.Fatalf("parseDirents = %+v, want %+v", got, want)
	}
}

func TestParseDirentsLeavesOutANameRemovedBeforeItsStat(t *testing.T) {
	buf := slices.Concat(dirent("gone.go", dtRegular), dirent("kept.go", dtRegular))
	got, err := parseDirents([]Entry{{Name: "earlier"}}, buf, func(name []byte, e *Entry) (bool, error) {
		if string(name) == "gone.go" {
			e.Size = 99 // a stat that wrote before it found the name gone
			return false, nil
		}
		return stated(name, e)
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Entry{{Name: "earlier"}, {Name: "kept.go", Size: 7}}; !slices.Equal(got, want) {
		t.Fatalf("parseDirents = %+v, want %+v", got, want)
	}
}

func TestParseDirentsStopsAtAStatError(t *testing.T) {
	denied := errors.New("denied")
	buf := slices.Concat(dirent("a.go", dtRegular), dirent("b.go", dtRegular), dirent("c.go", dtRegular))
	got, err := parseDirents(nil, buf, func(name []byte, e *Entry) (bool, error) {
		if string(name) == "b.go" {
			e.Size = 99 // a stat that wrote before it failed
			return false, denied
		}
		return stated(name, e)
	}, false)
	if !errors.Is(err, denied) || !slices.Equal(got, []Entry{{Name: "a.go", Size: 7}}) {
		t.Fatalf("parseDirents = %+v, %v; want a.go and the stat error", got, err)
	}
}

func TestParseDirentsUnderNamesStatsOnlyTheUnknown(t *testing.T) {
	buf := slices.Concat(dirent("sub", dtDir), dirent("a.go", dtRegular), dirent("odd", dtUnknown))
	got, err := parseDirents(nil, buf, func(name []byte, e *Entry) (bool, error) {
		if string(name) != "odd" {
			t.Errorf("%q was stat'ed under Names", name)
		}
		e.Type = fs.ModeSymlink
		return true, nil
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "sub", Type: fs.ModeDir}, {Name: "a.go"}, {Name: "odd", Type: fs.ModeSymlink}}
	if !slices.Equal(got, want) {
		t.Fatalf("parseDirents = %+v, want %+v", got, want)
	}
}

func TestParseDirentsUnderNamesLeavesOutAnUnknownNameRemovedBeforeItsStat(t *testing.T) {
	buf := slices.Concat(dirent("gone", dtUnknown), dirent("kept.go", dtRegular))
	got, err := parseDirents(nil, buf, func([]byte, *Entry) (bool, error) { return false, nil }, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Entry{{Name: "kept.go"}}; !slices.Equal(got, want) {
		t.Fatalf("parseDirents = %+v, want %+v", got, want)
	}
}
