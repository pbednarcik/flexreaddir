//go:build amd64 && !purego

package frd

import "golang.org/x/sys/cpu"

// The kernels asciiRun may use. Every amd64 has SSE2; AVX2 is checked once.
// Tests switch them off to run each tier on any machine.
var (
	useSSE2 = true
	useAVX2 = cpu.X86.HasAVX2
)

// asciiRun packs the leading run of units 1 to 0x7F at src into dst and
// returns how many it packed: thirty-two per AVX2 step while more than
// sixteen units remain, then sixteen per SSE2 step. readable is how many
// bytes may be read from src; a step reads 64 (AVX2) or 32 (SSE2). A step
// packs only when every unit of the name it covers passes, so the run stops
// at the first step holding a NUL, a non-ASCII unit or a surrogate; a failed
// AVX2 step hands its position to the SSE2 step, which may still pack the
// first sixteen; the scalar loop takes the rest. Both loops run inside one
// assembly call, asciiRunKernel.
func asciiRun(dst, src *byte, units, readable int) int {
	return asciiRunKernel(dst, src, units, readable, useAVX2, useSSE2)
}

// asciiRunKernel is asciiRun's step loops in assembly; avx2 and sse2 say
// which tiers may run.
//
//go:noescape
func asciiRunKernel(dst, src *byte, units, readable int, avx2, sse2 bool) int
