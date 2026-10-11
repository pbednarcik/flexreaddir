package frd

import (
	"errors"
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

// captureFullInfo returns every FILE_ID_EXTD_DIR_INFO buffer the kernel fills
// for dir, each in its own 64 KiB slice, as readAll would see them.
func captureFullInfo(tb testing.TB, dir string) [][]byte {
	tb.Helper()
	fd := openRaw(tb, dir)
	defer mustCloseRaw(tb, fd)
	var bufs [][]byte
	for {
		buf := make([]byte, bufferBytes)
		err := windows.GetFileInformationByHandleEx(windows.Handle(fd), windows.FileIdExtdDirectoryInfo, &buf[0], uint32(len(buf)))
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
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
		if err := eachFullInfo(buf, idExtdDirInfo, func(r fullInfo) error {
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
	_ = eachFullInfo(finfos(rs...), idExtdDirInfo, func(r fullInfo) error {
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
					if dst, err = parseFullInfo(dst, buf, idExtdDirInfo); err != nil {
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
				defer mustCloseRaw(b, fd)
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

// BenchmarkAppendUTF16Real decodes every name of a corpus once per iteration,
// in kernel order, and reports the cost per name and per code unit. Names
// are packed with slack (packedSlack), as inside a kernel buffer. The fixed11
// arm is the synthetic benchmarks' eleven-unit shape; the bylength arms hold
// the same names sorted by length, the control for a learned loop exit.
func BenchmarkAppendUTF16Real(b *testing.B) {
	type set struct {
		label string
		names [][]byte
	}
	sets := []set{{"fixed11", packedSlack(fixedNames(4096))}}
	for _, d := range realDirs {
		if _, err := os.Stat(d.path); err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		names := realNames(b, captureFullInfo(b, d.path))
		sets = append(sets, set{d.name, packedSlack(names)}, set{d.name + "-bylength", packedSlack(byLength(names))})
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

// naturalMergeSort finds the ascending runs already in s and merges
// neighbours pairwise through one buffer, allocated only when s is not
// sorted. It is a benchmark arm, not the package's sort.
func naturalMergeSort(s []Entry) {
	var buf []Entry
	for {
		merged := false
		for lo := 0; lo < len(s); {
			mid := runEnd(s, lo)
			if mid == len(s) {
				break
			}
			hi := runEnd(s, mid)
			if buf == nil {
				buf = make([]Entry, len(s))
			}
			mergeInto(buf[lo:hi], s[lo:mid], s[mid:hi])
			copy(s[lo:hi], buf[lo:hi])
			merged = true
			lo = hi
		}
		if !merged {
			return
		}
	}
}

func runEnd(s []Entry, lo int) int {
	i := lo + 1
	for i < len(s) && CompareName(s[i-1], s[i]) <= 0 {
		i++
	}
	return i
}

func mergeInto(dst, a, b []Entry) {
	i, j, k := 0, 0, 0
	for i < len(a) && j < len(b) {
		if CompareName(b[j], a[i]) < 0 {
			dst[k], j = b[j], j+1
		} else {
			dst[k], i = a[i], i+1
		}
		k++
	}
	k += copy(dst[k:], a[i:])
	copy(dst[k:], b[j:])
}

// BenchmarkSortListing sorts real listings, in the order the kernel returned
// them, with three sorts. NTFS returns names in its own case-insensitive
// order, which byte order breaks wherever case or a character between the
// cases ("_") differs: System32's mixed-case names into many runs, WinSxS's
// mostly lower-case ones into few.
func BenchmarkSortListing(b *testing.B) {
	sorts := []struct {
		name string
		sort func([]Entry)
	}{
		{"pdqsort", func(s []Entry) { slices.SortFunc(s, CompareName) }},
		{"stable", SortByName},
		{"natural-merge", naturalMergeSort},
	}
	for _, d := range realDirs {
		f, err := os.Open(d.path)
		if err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		orig, err := AppendDir(nil, f)
		mustClose(b, f)
		if err != nil {
			b.Fatal(err)
		}
		check := slices.Clone(orig)
		SortByName(check)
		for _, s := range sorts {
			b.Run("dir="+d.name+"/sort="+s.name, func(b *testing.B) {
				work := make([]Entry, len(orig))
				copy(work, orig)
				s.sort(work)
				if !slices.Equal(work, check) {
					b.Fatalf("%s sorts differently from SortByName", s.name)
				}
				b.ReportAllocs()
				for b.Loop() {
					copy(work, orig)
					s.sort(work)
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(orig)), "ns/entry")
			})
		}
	}
}
