package frd

import (
	"strings"
	"testing"
)

func FuzzRecords(f *testing.F) {
	f.Add(records(rec{common: every, file: attrDataLength, name: "a.go", objtype: typeRegular, size: 1}), 1)
	f.Add(records(rec{common: attrName | attrError, errno: errnoNotExist, name: "gone"}, rec{common: every, name: "d", objtype: typeDir}), 2)
	f.Add(records(rec{common: attrName | attrObjtype | attrModtime, file: attrDataLength, name: "old.go", objtype: typeRegular, size: 1},
		rec{common: every, file: attrDataLength, name: "b.go", objtype: typeRegular, chg: 1, fileID: 2, size: 3}), 2)
	f.Fuzz(func(t *testing.T, buf []byte, n int) {
		got, err := parseRecords(nil, buf, n, keepIDs)
		if err != nil {
			return
		}
		if n <= 0 && len(got) != 0 {
			t.Fatalf("%d records claimed, %d entries returned", n, len(got))
		}
		for _, e := range got {
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
		})
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
		got, _ := parseFullInfo(nil, buf, l)
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
	})
}
