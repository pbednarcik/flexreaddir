//go:build !race

// The race detector's instrumentation allocates on its own, so allocation
// counts are pinned only without it.

package frd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// hundredFiles names the regular files the allocation pins list.
func hundredFiles() []string {
	names := make([]string, 100)
	for i := range names {
		names[i] = fmt.Sprintf("file%03d.go", i)
	}
	return names
}

func hundredRecords() []byte {
	var rs []rec
	for _, name := range hundredFiles() {
		rs = append(rs, rec{common: every, file: attrDataLength, name: name, objtype: typeRegular, size: 1})
	}
	return records(rs...)
}

func TestParseRecordsAllocatesOnceForAllTheNamesInABuffer(t *testing.T) {
	buf := hundredRecords()
	dst := make([]Entry, 0, 100)
	allocs := testing.AllocsPerRun(20, func() {
		if _, _, err := parseRecords(dst[:0], buf, 100, keepIDs); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("parsing 100 records allocated %v times, want 1", allocs)
	}
}

func TestParseRecordsIntoANilSliceAllocatesOnceMoreForTheEntries(t *testing.T) {
	buf := hundredRecords()
	allocs := testing.AllocsPerRun(20, func() {
		if _, _, err := parseRecords(nil, buf, 100, keepIDs); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("parsing 100 records into nil allocated %v times, want 2", allocs)
	}
}

func hundredDirentries() []byte {
	var parts [][]byte
	for _, name := range hundredFiles() {
		parts = append(parts, direntry(name, dtRegular))
	}
	return slices.Concat(parts...)
}

func TestParseDirentriesAllocatesOnceForAllTheNamesInABuffer(t *testing.T) {
	buf := hundredDirentries()
	dst := make([]Entry, 0, 100)
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseDirentries(dst[:0], buf, statedType); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("parsing 100 direntries allocated %v times, want 1", allocs)
	}
}

func TestParseDirentriesIntoANilSliceAllocatesOnceMoreForTheEntries(t *testing.T) {
	buf := hundredDirentries()
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseDirentries(nil, buf, statedType); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("parsing 100 direntries into nil allocated %v times, want 2", allocs)
	}
}

// An empty directory's buffer holds "." and "..", which the kernel returns
// and the listing skips: nothing to allocate for.
func TestParseDirentriesOfAnEmptyDirectoryAllocatesNothing(t *testing.T) {
	buf := slices.Concat(direntry(".", dtDir), direntry("..", dtDir))
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseDirentries(nil, buf, statedType); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("parsing an empty directory allocated %v times, want 0", allocs)
	}
}

func hundredDirents() []byte {
	var parts [][]byte
	for _, name := range hundredFiles() {
		parts = append(parts, dirent(name, dtRegular))
	}
	return slices.Concat(parts...)
}

func TestParseDirentsAllocatesOnceForAllTheNamesInABuffer(t *testing.T) {
	buf := hundredDirents()
	dst := make([]Entry, 0, 100)
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseDirents(dst[:0], buf, stated, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("parsing 100 dirents allocated %v times, want 1", allocs)
	}
}

func TestParseDirentsIntoANilSliceAllocatesOnceMoreForTheEntries(t *testing.T) {
	buf := hundredDirents()
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseDirents(nil, buf, stated, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("parsing 100 dirents into nil allocated %v times, want 2", allocs)
	}
}

func hundredFullInfos() []byte {
	var rs []finfo
	for _, name := range hundredFiles() {
		rs = append(rs, finfo{name: name, attrs: 0x20, size: 1, writeTime: windowsEpoch})
	}
	return finfos(rs...)
}

func TestParseFullInfoAllocatesOnceForAllTheNamesInABuffer(t *testing.T) {
	buf := hundredFullInfos()
	dst := make([]Entry, 0, 100)
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseFullInfo(dst[:0], buf, idExtdDirInfo, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("parsing 100 records allocated %v times, want 1", allocs)
	}
}

func TestParseFullInfoIntoANilSliceAllocatesOnceMoreForTheEntries(t *testing.T) {
	buf := hundredFullInfos()
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseFullInfo(nil, buf, idExtdDirInfo, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("parsing 100 records into nil allocated %v times, want 2", allocs)
	}
}

// An empty directory other than a volume root lists only "." and "..": no
// names, so nothing to allocate.
func TestParseFullInfoOfAnEmptyDirectoryAllocatesNothing(t *testing.T) {
	buf := finfos(finfo{name: "."}, finfo{name: ".."})
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := parseFullInfo(nil, buf, idExtdDirInfo, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("parsing an empty directory allocated %v times, want 0", allocs)
	}
}

// ReadDir on macOS, Linux and Windows lists through a bare handle: the open's
// path, the entries and their names, and nothing for an os.File.
func TestReadDirAllocatesAtMost(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("ReadDir opens an os.File here")
	}
	dir := t.TempDir()
	for _, name := range hundredFiles()[:10] {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(100, func() {
		if entries, err := ReadDir(dir); err != nil || len(entries) != 10 {
			t.Fatalf("%d entries, %v; want 10", len(entries), err)
		}
	})
	if allocs > 3 {
		t.Fatalf("ReadDir of 10 files allocated %v times, want 3", allocs)
	}
}
