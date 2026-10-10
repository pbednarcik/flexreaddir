package frd

import (
	"slices"
	"strings"
)

// CompareName orders two entries by Name, byte by byte, the order os.ReadDir
// returns. It fits slices.SortFunc, slices.BinarySearchFunc and merges.
func CompareName(a, b Entry) int { return strings.Compare(a.Name, b.Name) }

// SortByName sorts entries in place by Name in CompareName's order. It
// allocates nothing. The sort is stable, and on the nearly sorted order a
// kernel returns it is 1.5x to 7x faster than pdqsort (measured, see
// docs/notes/evidence/2026-10-09-sort.md).
func SortByName(entries []Entry) { slices.SortStableFunc(entries, CompareName) }
