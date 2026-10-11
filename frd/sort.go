package frd

import (
	"slices"
	"strings"
)

// CompareName orders two entries by Name, byte by byte, the order os.ReadDir
// returns. It fits slices.SortFunc, slices.BinarySearchFunc and merges.
func CompareName(a, b Entry) int { return strings.Compare(a.Name, b.Name) }

// SortByName sorts entries in place by Name in CompareName's order. It is
// stable, allocates nothing, and is faster than slices.SortFunc on a listing
// that is already nearly sorted, as NTFS returns it.
func SortByName(entries []Entry) { slices.SortStableFunc(entries, CompareName) }
