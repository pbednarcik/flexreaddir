package frd

import "strings"

// nameBlock gives every name read from one kernel buffer a single allocation.
// A builder never rewrites bytes it has handed out, so a short grow costs an
// extra allocation, never a wrong name.
type nameBlock struct{ b strings.Builder }

func (nb *nameBlock) grow(n int) { nb.b.Grow(n) }

func (nb *nameBlock) add(name []byte) string {
	start := nb.b.Len()
	nb.b.Write(name)
	return nb.b.String()[start:]
}
