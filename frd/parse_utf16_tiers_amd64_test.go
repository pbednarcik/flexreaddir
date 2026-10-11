//go:build amd64 && !purego

package frd

import (
	"testing"

	"golang.org/x/sys/cpu"
)

// forEachTier runs f once per decoder tier this machine can run: go (no
// SIMD), sse2, and avx2 where the CPU has it, restoring the switches after.
func forEachTier(tb testing.TB, f func(tier string)) {
	tb.Helper()
	sse2, avx2 := useSSE2, useAVX2
	defer func() { useSSE2, useAVX2 = sse2, avx2 }()
	tiers := []struct {
		name       string
		sse2, avx2 bool
	}{{"go", false, false}, {"sse2", true, false}, {"avx2", true, true}}
	for _, tr := range tiers {
		if tr.avx2 && !cpu.X86.HasAVX2 {
			tb.Logf("this CPU has no AVX2; the avx2 tier is not run")
			continue
		}
		useSSE2, useAVX2 = tr.sse2, tr.avx2
		f(tr.name)
	}
}
