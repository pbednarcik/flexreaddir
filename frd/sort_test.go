package frd

import (
	"slices"
	"testing"
)

// "B" sorts before "a" in bytes but after it case-insensitively, as NTFS lists
// them; U+FF5E sorts before U+1F642 in UTF-8 but after it in UTF-16.
var sortNames = []string{"b.go", "\U0001F642", "a", "B.go", "～", "_", "A"}

func entriesNamed(names []string) []Entry {
	es := make([]Entry, len(names))
	for i, n := range names {
		es[i] = Entry{Name: n}
	}
	return es
}

func TestSortByNameIsByteOrder(t *testing.T) {
	es := entriesNamed(sortNames)
	SortByName(es)
	var got []string
	for _, e := range es {
		got = append(got, e.Name)
	}
	want := []string{"A", "B.go", "_", "a", "b.go", "～", "\U0001F642"}
	if !slices.Equal(got, want) {
		t.Fatalf("sorted %q, want %q", got, want)
	}
}

func TestCompareNameOrdersByName(t *testing.T) {
	a, b := Entry{Name: "a", Size: 9}, Entry{Name: "b"}
	if CompareName(a, b) >= 0 || CompareName(b, a) <= 0 || CompareName(a, Entry{Name: "a"}) != 0 {
		t.Fatal("CompareName does not order by Name alone")
	}
}

func TestSortByNameAllocatesNothing(t *testing.T) {
	orig := entriesNamed(sortNames)
	work := make([]Entry, len(orig))
	if n := testing.AllocsPerRun(20, func() { copy(work, orig); SortByName(work) }); n != 0 {
		t.Fatalf("%v allocations per sort", n)
	}
}

// Ninety entries, so the sort leaves insertion sort behind: slices.SortFunc
// reorders equal names here, slices.SortStableFunc does not.
func TestSortByNameIsStable(t *testing.T) {
	names := []string{"c", "a", "b"}
	es := make([]Entry, 90)
	for i := range es {
		es[i] = Entry{Name: names[i%3], Size: int64(i)}
	}
	SortByName(es)
	for i := 1; i < len(es); i++ {
		if es[i-1].Name == es[i].Name && es[i-1].Size > es[i].Size {
			t.Fatalf("entry %d (size %d) sorted after size %d with the same name", i, es[i].Size, es[i-1].Size)
		}
	}
}
