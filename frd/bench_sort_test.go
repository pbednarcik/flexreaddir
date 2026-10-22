package frd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// BenchmarkSortListingInKernelOrder sorts listings, in the order the kernel
// returned them, with the stable sort SortByName uses and with pdqsort. A
// generated directory of 10 and of 1,000 files is listed on every system; so
// are /usr/lib and /System/Library/Frameworks where they can be listed, and
// the directories FRD_SORT_DIRS names (a path list). APFS and ext4 return
// names in hash order, NTFS nearly sorted. The copy that restores the
// kernel's order is timed with each sort alike.
func BenchmarkSortListingInKernelOrder(b *testing.B) {
	sorts := []struct {
		name string
		sort func([]Entry)
	}{
		{"stable", SortByName},
		{"pdq", func(s []Entry) { slices.SortFunc(s, CompareName) }},
	}
	type listing struct{ name, path string }
	var dirs []listing
	for _, files := range []int{10, 1000} {
		dir := b.TempDir()
		for i := range files {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%04d.go", i)), []byte("package p\n"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		dirs = append(dirs, listing{fmt.Sprintf("files=%d", files), dir})
	}
	dirs = append(dirs, listing{"usr-lib", "/usr/lib"}, listing{"frameworks", "/System/Library/Frameworks"})
	for _, path := range filepath.SplitList(os.Getenv("FRD_SORT_DIRS")) {
		dirs = append(dirs, listing{filepath.Base(path), path})
	}
	for _, d := range dirs {
		f, err := os.Open(d.path)
		if err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		orig, err := AppendDir(nil, f)
		mustClose(b, f)
		if err != nil {
			b.Logf("skipping %s: %v", d.name, err)
			continue
		}
		runs := 1
		for i := 1; i < len(orig); i++ {
			if orig[i].Name < orig[i-1].Name {
				runs++
			}
		}
		b.Logf("%s: %d entries in %d ascending runs", d.name, len(orig), runs)
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
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*max(len(orig), 1)), "ns/entry")
			})
		}
	}
}
