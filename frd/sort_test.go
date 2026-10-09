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
