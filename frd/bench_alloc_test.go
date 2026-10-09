//go:build !race

// The race detector's instrumentation allocates on its own, so allocation
// counts are pinned only without it.

package frd

import (
	"fmt"
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
		if _, err := parseRecords(dst[:0], buf, 100); err != nil {
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
		if _, err := parseRecords(nil, buf, 100); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("parsing 100 records into nil allocated %v times, want 2", allocs)
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
		if _, err := parseDirents(dst[:0], buf, stated); err != nil {
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
		if _, err := parseDirents(nil, buf, stated); err != nil {
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
		if _, err := parseFullInfo(dst[:0], buf); err != nil {
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
		if _, err := parseFullInfo(nil, buf); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("parsing 100 records into nil allocated %v times, want 2", allocs)
	}
}
