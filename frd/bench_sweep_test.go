package frd

import (
	"fmt"
	"testing"
)

// packed copies names end to end into one block, each name's cap ending at
// the name, as the decoder may not assume more: a SIMD step that reads 32
// bytes cannot run on a name shorter than sixteen units.
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

// packedSlack is packed with every name's cap running to the end of the
// block, which has 64 bytes to spare: the shape a kernel buffer gives, where
// the records after a name are readable and a SIMD step may run on it. One
// block keeps a sorted corpus reading memory front to back.
func packedSlack(names [][]byte) [][]byte {
	total := 0
	for _, n := range names {
		total += len(n)
	}
	block := make([]byte, 0, total+64)
	out := make([][]byte, len(names))
	for i, n := range names {
		start := len(block)
		block = append(block, n...)
		out[i] = block[start:]
	}
	return out
}

// sweepNames is n distinct names of exactly units ASCII units each, as
// UTF-16LE, not packed.
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
	return names
}

// BenchmarkAppendUTF16Sweep decodes names of one fixed length, 4 to 24 units,
// on every tier this binary has, in two cap shapes: tight (cap ends at the
// name) and slack (the kernel buffer's shape), for the length at which a
// SIMD call pays for itself. The go tier of the native binary is the
// dispatch with no SIMD step; under -tags purego the only tier is the scalar
// loop with no dispatch, the reference the native go tier is read against.
func BenchmarkAppendUTF16Sweep(b *testing.B) {
	shapes := []struct {
		name string
		pack func([][]byte) [][]byte
	}{{"tight", packed}, {"slack", packedSlack}}
	for units := 4; units <= 24; units++ {
		for _, shape := range shapes {
			names := shape.pack(sweepNames(4096, units))
			forEachTier(b, func(tier string) {
				b.Run(fmt.Sprintf("units=%02d/cap=%s/tier=%s", units, shape.name, tier), func(b *testing.B) {
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
}
