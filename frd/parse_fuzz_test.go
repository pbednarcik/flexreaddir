package frd

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func FuzzRecords(f *testing.F) {
	f.Add(records(rec{common: every, file: attrDataLength, name: "a.go", objtype: typeRegular, size: 1}), 1)
	f.Add(records(rec{common: attrName | attrError, errno: errnoNotExist, name: "gone"}, rec{common: every, name: "d", objtype: typeDir}), 2)
	f.Add(records(rec{common: attrName | attrObjtype | attrModtime, file: attrDataLength, name: "old.go", objtype: typeRegular, size: 1},
		rec{common: every, file: attrDataLength, name: "b.go", objtype: typeRegular, chg: 1, fileID: 2, size: 3}), 2)
	f.Fuzz(func(t *testing.T, buf []byte, n int) {
		got, end, err := parseRecords(nil, buf, n, keepIDs)
		if end < 0 || end > len(buf) {
			t.Fatalf("the records ended at %d of a %d-byte buffer", end, len(buf))
		}
		if err != nil {
			return
		}
		if n <= 0 && len(got) != 0 {
			t.Fatalf("%d records claimed, %d entries returned", n, len(got))
		}
		for _, e := range got {
			if e.Name == "" || strings.IndexByte(e.Name, 0) >= 0 {
				t.Fatalf("listed the name %q", e.Name)
			}
			if e.Type != 0 && (e.Size != 0 || e.ModTimeNanos != 0 || e.ChangeTimeNanos != 0 || e.FileID != 0) {
				t.Fatalf("%+v: only a regular file carries a size, its times and its id", e)
			}
		}
	})
}

func FuzzDirents(f *testing.F) {
	f.Add(append(dirent(".", dtDir), dirent("a.go", dtRegular)...))
	f.Fuzz(func(t *testing.T, buf []byte) {
		got, _ := parseDirents(nil, buf, func(name []byte, _ *Entry) (bool, error) {
			if name[:len(name)+1][len(name)] != 0 {
				t.Fatalf("%q reached stat without its NUL after it", name)
			}
			return true, nil
		}, false)
		for _, e := range got {
			if e.Name == "" || e.Name == "." || e.Name == ".." || strings.IndexByte(e.Name, 0) >= 0 {
				t.Fatalf("listed the name %q", e.Name)
			}
		}
	})
}

func FuzzFullInfo(f *testing.F) {
	f.Add(finfosIn(fullDirInfo, finfo{name: "a.go", attrs: 0x20, size: 1, writeTime: windowsEpoch}), false)
	f.Add(finfosIn(fullDirInfo, finfo{name: "."}, finfo{name: "d", attrs: attrDirectory}, finfo{name: "🙂"}), false)
	f.Add(finfos(finfo{name: "a.go", attrs: 0x20, size: 1, writeTime: windowsEpoch, lo: 5}), true)
	f.Add(finfos(finfo{name: "r.go", attrs: 0x20, size: 1, lo: 5, hi: 3}, finfo{name: "d", attrs: attrDirectory, lo: 6}), true)
	f.Fuzz(func(t *testing.T, buf []byte, extd bool) {
		l := fullDirInfo
		if extd {
			l = idExtdDirInfo
		}
		got, _ := parseFullInfo(nil, buf, l, false)
		for _, e := range got {
			if e.Name == "" || e.Name == "." || e.Name == ".." || strings.IndexByte(e.Name, 0) >= 0 {
				t.Fatalf("listed the name %q", e.Name)
			}
			if e.Type != 0 && (e.Size != 0 || e.ModTimeNanos != 0 || e.ChangeTimeNanos != 0 || e.FileID != 0) {
				t.Fatalf("%+v: only a regular file carries a size, its times and its id", e)
			}
			if !extd && e.FileID != 0 {
				t.Fatalf("%+v: FILE_FULL_DIR_INFO has no file id", e)
			}
		}
		// The written-out walk against the reference, into reused slots.
		junk := Entry{Name: "junk", Type: fs.ModeDir, Size: -1, ModTimeNanos: -1, ChangeTimeNanos: -1, FileID: 1}
		for _, names := range []bool{false, true} {
			got, err := parseFullInfo(slices.Repeat([]Entry{junk}, 4)[:0], buf, l, names)
			want, wantErr := parseFullInfoByVisitor(nil, buf, l, names)
			if (err == nil) != (wantErr == nil) || !slices.Equal(got, want) {
				t.Fatalf("names=%v: got %+v, %v; the reference gives %+v, %v", names, got, err, want, wantErr)
			}
		}
	})
}

func FuzzDirentries(f *testing.F) {
	f.Add(slices.Concat(direntry(".", dtDir), direntry("a.go", dtRegular), direntry("odd", dtUnknown)))
	f.Add(slices.Concat(direntryIno("gone", dtRegular, 0), direntry("wh", dtWhiteout)))
	f.Fuzz(func(t *testing.T, buf []byte) {
		got, _ := parseDirentries(nil, buf, func(name []byte, e *Entry) (bool, error) {
			if name[:len(name)+1][len(name)] != 0 {
				t.Fatalf("%q reached stat without its NUL after it", name)
			}
			e.Type = fs.ModeSymlink
			return true, nil
		})
		for _, e := range got {
			if e.Name == "" || e.Name == "." || e.Name == ".." || strings.IndexByte(e.Name, 0) >= 0 {
				t.Fatalf("listed the name %q", e.Name)
			}
			if e.Size != 0 || e.ModTimeNanos != 0 || e.ChangeTimeNanos != 0 || e.FileID != 0 {
				t.Fatalf("%+v: a names listing carries no stamps", e)
			}
		}
	})
}
