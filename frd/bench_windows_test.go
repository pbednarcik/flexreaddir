package frd

import (
	"fmt"
	"os"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// realDirs are the directories whose kernel buffers the real-name benchmarks
// parse: System32 for short, mostly ASCII names in the thousands, WinSxS for
// long ones. Their records come back in the order NTFS keeps them, so the
// branch predictor sees name lengths as a walker would.
var realDirs = []struct{ name, path string }{
	{"system32", `C:\Windows\System32`},
	{"winsxs", `C:\Windows\WinSxS`},
}

// captureFullInfo returns every FILE_FULL_DIR_INFO buffer the kernel fills
// for dir, each in its own 64 KiB slice, as readAll would see them.
func captureFullInfo(tb testing.TB, dir string) [][]byte {
	tb.Helper()
	fd := openRaw(tb, dir)
	defer closeRaw(fd)
	var bufs [][]byte
	for {
		buf := make([]byte, bufferBytes)
		err := windows.GetFileInformationByHandleEx(windows.Handle(fd), windows.FileFullDirectoryInfo, &buf[0], uint32(len(buf)))
		if err == syscall.ERROR_NO_MORE_FILES {
			return bufs
		}
		if err != nil {
			tb.Fatal(err)
		}
		bufs = append(bufs, buf)
	}
}

// realNames is the UTF-16LE name of every entry in bufs, in kernel order,
// each still inside its buffer as appendUTF16 receives it.
func realNames(tb testing.TB, bufs [][]byte) [][]byte {
	tb.Helper()
	var names [][]byte
	for _, buf := range bufs {
		if err := eachFullInfo(buf, func(r fullInfo) error {
			names = append(names, r.name)
			return nil
		}); err != nil {
			tb.Fatal(err)
		}
	}
	return names
}

// corpus logs what a name set holds, so a run records the shape it measured.
func corpus(tb testing.TB, label string, names [][]byte) (units int) {
	tb.Helper()
	longest, nonASCII := 0, 0
	for _, n := range names {
		u := len(n) / 2
		units += u
		longest = max(longest, u)
		for i := 1; i < len(n); i += 2 {
			if n[i] != 0 || n[i-1] >= 0x80 {
				nonASCII++
				break
			}
		}
	}
	tb.Logf("%s: %d names, %.1f units on average, longest %d, %d with a non-ASCII unit",
		label, len(names), float64(units)/float64(len(names)), longest, nonASCII)
	return units
}

// fixedNames is the shape BenchmarkParseFullInfo uses, file0000.go onward:
// every name eleven units, which a predictor learns.
func fixedNames(n int) [][]byte {
	rs := make([]finfo, n)
	for i := range rs {
		rs[i] = finfo{name: fmt.Sprintf("file%04d.go", i), attrs: 0x20}
	}
	var names [][]byte
	_ = eachFullInfo(finfos(rs...), func(r fullInfo) error {
		names = append(names, r.name)
		return nil
	})
	return names
}

// BenchmarkParseFullInfoReal parses the captured kernel buffers of real
// directories, no system call in the loop, and reports the cost per entry.
func BenchmarkParseFullInfoReal(b *testing.B) {
	for _, d := range realDirs {
		if _, err := os.Stat(d.path); err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		bufs := captureFullInfo(b, d.path)
		entries := len(realNames(b, bufs))
		b.Run("dir="+d.name, func(b *testing.B) {
			dst := make([]Entry, 0, entries)
			b.SetBytes(int64(len(bufs) * bufferBytes))
			b.ReportAllocs()
			for b.Loop() {
				dst = dst[:0]
				for _, buf := range bufs {
					var err error
					if dst, err = parseFullInfo(dst, buf); err != nil {
						b.Fatal(err)
					}
				}
				if len(dst) != entries {
					b.Fatalf("%d entries, want %d", len(dst), entries)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*entries), "ns/entry")
		})
	}
}

// BenchmarkListReal lists the real directories whole, system calls included,
// as a walker would: a bare handle opened per listing, one Lister kept. Its
// ns/entry against BenchmarkAppendUTF16Real's ns/name for the same directory
// is the most a faster decoder could ever save on a listing.
func BenchmarkListReal(b *testing.B) {
	for _, d := range realDirs {
		if _, err := os.Stat(d.path); err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		b.Run("dir="+d.name, func(b *testing.B) {
			var l Lister
			var entries []Entry
			list := func() int {
				fd := openRaw(b, d.path)
				defer closeRaw(fd)
				var err error
				if entries, err = l.AppendHandle(entries[:0], fd, d.path); err != nil {
					b.Fatal(err)
				}
				return len(entries)
			}
			want := list() // warms the OS cache and the Lister's buffer
			if want == 0 {
				b.Fatal("listed nothing")
			}
			b.ReportAllocs()
			for b.Loop() {
				if n := list(); n != want {
					b.Fatalf("%d entries, want %d", n, want)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*want), "ns/entry")
		})
	}
}

// byLength is names sorted by length, stable, so lengths come in long runs
// and the loop exit is predictable: the same work as kernel order without
// the per-name mispredict, which makes it the control for that question.
func byLength(names [][]byte) [][]byte {
	sorted := slices.Clone(names)
	slices.SortStableFunc(sorted, func(a, b []byte) int { return len(a) - len(b) })
	return sorted
}

// packed copies names end to end into one block in the order given, so every
// arm reads memory front to back: without it, sorting scatters the reads over
// the captured buffers and a corpus larger than L2 measures cache misses
// instead of branches.
func packed(names [][]byte) [][]byte {
	total := 0
	for _, n := range names {
		total += len(n)
	}
	block := make([]byte, 0, total)
	out := make([][]byte, len(names))
	for i, n := range names {
		start := len(block)
		block = append(block, n...)
		out[i] = block[start:len(block):len(block)]
	}
	return out
}

// BenchmarkAppendUTF16Real decodes every name of a corpus once per iteration,
// in kernel order, and reports the cost per name and per code unit. Names
// are packed (see packed), so cap(name) ends at the name, as the decoder
// may not assume more. The fixed arm is the eleven-unit shape of the
// synthetic benchmarks and the bylength arms the same names sorted by
// length, for the question whether a learned loop exit flatters them.
func BenchmarkAppendUTF16Real(b *testing.B) {
	type set struct {
		label string
		names [][]byte
	}
	sets := []set{{"fixed11", packed(fixedNames(4096))}}
	for _, d := range realDirs {
		if _, err := os.Stat(d.path); err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		names := realNames(b, captureFullInfo(b, d.path))
		sets = append(sets, set{d.name, packed(names)}, set{d.name + "-bylength", packed(byLength(names))})
	}
	for _, s := range sets {
		units := corpus(b, s.label, s.names)
		b.Run("names="+s.label, func(b *testing.B) {
			var scratch [3 * 255]byte
			sink := 0
			b.ReportAllocs()
			for b.Loop() {
				for _, n := range s.names {
					out, ok := appendUTF16(scratch[:0], n)
					if !ok {
						b.Fatalf("NUL unit in %q", n)
					}
					sink += len(out)
				}
			}
			if sink == 0 {
				b.Fatal("decoded nothing")
			}
			ns := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(ns/float64(len(s.names)), "ns/name")
			b.ReportMetric(ns/float64(units), "ns/unit")
		})
	}
}
