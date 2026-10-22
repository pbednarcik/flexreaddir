package frd

import (
	"slices"
	"strings"
)

// CompareName orders two entries by Name, byte by byte, the order os.ReadDir
// returns. It fits slices.SortFunc, slices.BinarySearchFunc and merges.
func CompareName(a, b Entry) int { return strings.Compare(a.Name, b.Name) }

// SortByName sorts entries in place by Name in CompareName's order. It is
// stable, allocates nothing, and is fastest on a listing that is already
// nearly sorted, as NTFS returns it.
func SortByName(entries []Entry) {
	// Blocks sorted by insertion, then neighbouring runs merged, doubling.
	// A merge of runs already in order costs one comparison.
	n := len(entries)
	for lo := 0; lo < n; lo += sortBlock {
		insertionSort(entries[lo:min(lo+sortBlock, n)])
	}
	if n <= sortBlock {
		return // no merge: the buffer below need not be zeroed
	}
	var buf [mergeBuf]Entry
	for width := sortBlock; width < n; width *= 2 {
		for lo := 0; lo+width < n; lo += 2 * width {
			mergeRuns(entries[lo:min(lo+2*width, n)], width, buf[:])
		}
	}
}

const (
	sortBlock = 20 // entries sorted by insertion before the first merge
	mergeBuf  = 32 // entries a merge or a rotation holds aside on the stack
)

// insertionSort sorts s by Name, equal names in their first order.
func insertionSort(s []Entry) {
	for i := 1; i < len(s); i++ {
		if s[i].Name >= s[i-1].Name {
			continue
		}
		e := s[i]
		j := i - 1
		for j > 0 && e.Name < s[j-1].Name {
			j--
		}
		copy(s[j+1:i+1], s[j:i])
		s[j] = e
	}
}

// mergeRuns merges the sorted runs s[:mid] and s[mid:] in place, an equal
// name from the first run before one from the second. When a run fits buf the
// merge goes through it; otherwise the longer run is split at its middle, the
// two halves are swapped into place by one rotation, and each side is merged
// on its own.
func mergeRuns(s []Entry, mid int, buf []Entry) {
	for mid > 0 && mid < len(s) && s[mid-1].Name > s[mid].Name {
		// The first run's head up to the second's first name, and the second
		// run's tail from the first's last name, are already in place.
		lo := upperBound(s[:mid], s[mid].Name)
		hi := mid + lowerBound(s[mid:], s[mid-1].Name)
		s, mid = s[lo:hi], mid-lo
		if s[0].Name > s[len(s)-1].Name {
			// Every name of the second run sorts before every name of the
			// first, as in a reversed listing: one rotation.
			rotate(s, mid, buf)
			return
		}
		if mid <= len(buf) {
			mergeFromFront(s, mid, buf)
			return
		}
		if len(s)-mid <= len(buf) {
			mergeFromBack(s, mid, buf)
			return
		}
		// s[i:mid] and s[mid:j] trade places: everything left of the new
		// boundary sorts no later than everything right of it.
		var i, j int
		if mid >= len(s)-mid {
			i = mid / 2
			j = mid + lowerBound(s[mid:], s[i].Name)
		} else {
			j = mid + (len(s)-mid)/2
			i = upperBound(s[:mid], s[j].Name)
		}
		rotate(s[i:j], mid-i, buf)
		k := i + j - mid
		left, right := s[:k], s[k:]
		// Recurse into the shorter side, loop on the longer.
		if len(left) < len(right) {
			mergeRuns(left, i, buf)
			s, mid = right, mid-i
		} else {
			mergeRuns(right, mid-i, buf)
			s, mid = left, i
		}
	}
}

// mergeFromFront merges with the first run, at most len(buf), set aside.
func mergeFromFront(s []Entry, mid int, buf []Entry) {
	a := buf[:copy(buf, s[:mid])]
	i, j, k := 0, mid, 0
	for i < len(a) && j < len(s) {
		if s[j].Name < a[i].Name {
			s[k] = s[j]
			j++
		} else {
			s[k] = a[i]
			i++
		}
		k++
	}
	copy(s[k:], a[i:])
}

// mergeFromBack merges with the second run, at most len(buf), set aside.
func mergeFromBack(s []Entry, mid int, buf []Entry) {
	b := buf[:copy(buf, s[mid:])]
	i, j, k := mid-1, len(b)-1, len(s)-1
	for i >= 0 && j >= 0 {
		if b[j].Name < s[i].Name {
			s[k] = s[i]
			i--
		} else {
			s[k] = b[j]
			j--
		}
		k--
	}
	copy(s, b[:j+1])
}

// rotate moves s[k:] in front of s[:k], through buf when either side fits.
func rotate(s []Entry, k int, buf []Entry) {
	switch n := len(s) - k; {
	case k <= len(buf):
		copy(buf, s[:k])
		copy(s, s[k:])
		copy(s[n:], buf[:k])
	case n <= len(buf):
		copy(buf, s[k:])
		copy(s[n:], s[:k])
		copy(s, buf[:n])
	default:
		slices.Reverse(s[:k])
		slices.Reverse(s[k:])
		slices.Reverse(s)
	}
}

// lowerBound is the index of the first entry in s whose Name is not less
// than name, or len(s).
func lowerBound(s []Entry, name string) int {
	lo, hi := 0, len(s)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if s[m].Name < name {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

// upperBound is the index of the first entry in s whose Name is greater
// than name, or len(s).
func upperBound(s []Entry, name string) int {
	lo, hi := 0, len(s)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if s[m].Name <= name {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}
