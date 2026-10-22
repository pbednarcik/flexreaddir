package frd

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
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

// Also with merges, buffered and split: the merge buffer lives on the stack.
func TestSortByNameAllocatesNothing(t *testing.T) {
	interleaved := make([]Entry, 1000)
	for i := range interleaved {
		interleaved[i] = Entry{Name: fmt.Sprintf("f%04d", (i%500)*2+i/500)}
	}
	for _, orig := range [][]Entry{entriesNamed(sortNames), interleaved} {
		work := make([]Entry, len(orig))
		if n := testing.AllocsPerRun(20, func() { copy(work, orig); SortByName(work) }); n != 0 {
			t.Fatalf("%d entries: %v allocations per sort", len(orig), n)
		}
	}
}

// SortByName gives what slices.SortStableFunc gives, equal names in their
// first order, on every shape a listing comes in and at every length around
// the block and buffer sizes, so each merge path runs: the trims, the
// buffered merges from either end, both splits and every rotation.
func TestSortByNameAgreesWithTheStableSort(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	word := func(alphabet string, n int) string {
		b := make([]byte, 1+rng.IntN(n))
		for i := range b {
			b[i] = alphabet[rng.IntN(len(alphabet))]
		}
		return string(b)
	}
	shapes := map[string]func(n int) []Entry{
		"random": func(n int) []Entry {
			es := make([]Entry, n)
			for i := range es {
				es[i] = Entry{Name: word("abcdefgh", 6)}
			}
			return es
		},
		"few names": func(n int) []Entry {
			es := make([]Entry, n)
			for i := range es {
				es[i] = Entry{Name: word("ab", 2)}
			}
			return es
		},
		"NTFS order": func(n int) []Entry { // case-insensitive, which byte order breaks
			es := make([]Entry, n)
			for i := range es {
				es[i] = Entry{Name: word("aAbB_cC.", 8)}
			}
			slices.SortStableFunc(es, func(a, b Entry) int { return strings.Compare(strings.ToUpper(a.Name), strings.ToUpper(b.Name)) })
			return es
		},
		"sorted": func(n int) []Entry {
			es := make([]Entry, n)
			for i := range es {
				es[i] = Entry{Name: fmt.Sprintf("f%06d", i)}
			}
			return es
		},
		"reversed": func(n int) []Entry {
			es := make([]Entry, n)
			for i := range es {
				es[i] = Entry{Name: fmt.Sprintf("f%06d", n-i)}
			}
			return es
		},
		"two interleaved runs": func(n int) []Entry {
			es := make([]Entry, n)
			for i := range es {
				es[i] = Entry{Name: fmt.Sprintf("f%06d", (i%(n/2+1))*2+i/(n/2+1))}
			}
			return es
		},
		"one name": func(n int) []Entry { return slices.Repeat([]Entry{{Name: "same"}}, n) },
	}
	for shape, gen := range shapes {
		for _, n := range []int{0, 1, 2, 3, 19, 20, 21, 31, 32, 33, 39, 40, 41, 63, 64, 65, 100, 160, 257, 1000, 5000} {
			es := gen(n)
			for i := range es {
				es[i].Size = int64(i) // tells equal names apart
			}
			want := slices.Clone(es)
			slices.SortStableFunc(want, CompareName)
			SortByName(es)
			if !slices.Equal(es, want) {
				t.Fatalf("%s, %d entries: the order differs from slices.SortStableFunc's", shape, n)
			}
		}
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
