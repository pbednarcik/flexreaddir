package frd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkAppendDir lists one directory of small files, stats each, and
// compares the portable ways of getting the same answer. The frd arm reuses
// its slice across listings, as a tree walker would.
func BenchmarkAppendDir(b *testing.B) {
	for _, files := range []int{10, 1000} {
		dir := b.TempDir()
		for i := range files {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%04d.go", i)), []byte("package p\n"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		b.Run(fmt.Sprintf("files=%d", files), func(b *testing.B) {
			b.Run("frd", func(b *testing.B) {
				var entries []Entry
				benchListing(b, dir, files, func(f *os.File) (n int, err error) {
					entries, err = AppendDir(entries[:0], f)
					return len(entries), err
				})
			})
			b.Run("readdir-info", func(b *testing.B) {
				benchListing(b, dir, files, func(f *os.File) (int, error) {
					read, err := f.ReadDir(-1)
					for _, de := range read {
						if _, err := de.Info(); err != nil {
							return 0, err
						}
					}
					return len(read), err
				})
			})
			b.Run("readdir-legacy", func(b *testing.B) {
				benchListing(b, dir, files, func(f *os.File) (int, error) {
					infos, err := f.Readdir(-1)
					return len(infos), err
				})
			})
		})
	}
}

// benchListing opens dir, lists it with list and closes it, once per
// iteration, and fails unless list saw all the files.
func benchListing(b *testing.B, dir string, files int, list func(*os.File) (int, error)) {
	b.ReportAllocs()
	for b.Loop() {
		f, err := os.Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		n, err := list(f)
		f.Close()
		if err != nil || n != files {
			b.Fatalf("%d entries, %v; want %d", n, err, files)
		}
	}
}

// BenchmarkParseRecords is the user-space cost of one kernel buffer of 1,000
// macOS attribute records, with no system call in the loop.
func BenchmarkParseRecords(b *testing.B) {
	var rs []rec
	for i := range 1000 {
		rs = append(rs, rec{common: every, file: attrDataLength, name: fmt.Sprintf("file%04d.go", i), objtype: typeRegular, sec: 1700000000, size: 10})
	}
	buf := records(rs...)
	dst := make([]Entry, 0, len(rs))
	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseRecords(dst[:0], buf, len(rs)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseDirents is the user-space cost of one buffer of 1,000 Linux
// getdents64 records, with a stat that asks no kernel.
func BenchmarkParseDirents(b *testing.B) {
	var buf []byte
	for i := range 1000 {
		buf = append(buf, dirent(fmt.Sprintf("file%04d.go", i), dtRegular)...)
	}
	dst := make([]Entry, 0, 1000)
	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseDirents(dst[:0], buf, stated); err != nil {
			b.Fatal(err)
		}
	}
}
