package frd

import (
	"fmt"
	"testing"
)

// packed copies names end to end into one block in the order given, so every
// arm reads memory front to back: without it, sorting scatters the reads over
// the captured buffers and a corpus larger than L2 measures cache misses
// instead of branches. cap(name) ends at the name, as the decoder may not
// assume more.
func packed(names [][]byte) [][]byte {
	total := 0
	for _, n := range names {
		total += len(n)
	}
	block := make([]byte, 0, total)
	out := make([][]byte, len(names))
	for i, n := range names {
		start := len(block)
		block = append(block, n...)
		out[i] = block[start:len(block):len(block)]
	}
	return out
}

// sweepNames is n distinct names of exactly units ASCII units each, as
// UTF-16LE, packed.
func sweepNames(n, units int) [][]byte {
	names := make([][]byte, n)
	for i := range names {
		s := fmt.Sprintf("%0*d", units, i)
		u := make([]byte, 2*units)
		for j := range units {
			u[2*j] = s[j]
		}
		names[i] = u
	}
	return packed(names)
}

// BenchmarkAppendUTF16Sweep decodes names of one fixed length, 4 to 24 units,
// on every tier this binary has, for the length at which a kernel call starts
// to pay for itself. The go tier of the native binary is the dispatch with no
// kernel; under -tags purego the only tier is the scalar loop with no
// dispatch, the reference the native go tier is read against.
func BenchmarkAppendUTF16Sweep(b *testing.B) {
	for units := 4; units <= 24; units++ {
		names := sweepNames(4096, units)
		forEachTier(b, func(tier string) {
			b.Run(fmt.Sprintf("units=%02d/tier=%s", units, tier), func(b *testing.B) {
				var scratch [3*255 + 32]byte
				sink := 0
				for b.Loop() {
					for _, n := range names {
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
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(names)), "ns/name")
			})
		})
	}
}
