package frd

import (
	"slices"
	"strings"
)

// CompareName orders two entries by Name, byte by byte, the order os.ReadDir
// returns. It fits slices.SortFunc, slices.BinarySearchFunc and merges.
func CompareName(a, b Entry) int { return strings.Compare(a.Name, b.Name) }

// SortByName sorts entries in place by Name in CompareName's order. It
// allocates nothing.
func SortByName(entries []Entry) { slices.SortFunc(entries, CompareName) }
