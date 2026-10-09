//go:build amd64 && !purego

package frd

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

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
// first sixteen; the scalar loop takes the rest.
func asciiRun(dst, src *byte, units, readable int) int {
	n := 0
	if useAVX2 {
		for n+16 < units && readable-2*n >= 64 {
			m := min(units-n, 32)
			if !ascii32(at(dst, n), at(src, 2*n), uint32(uint64(1)<<m-1)) {
				break
			}
			n += m
		}
	}
	if useSSE2 {
		for n < units && readable-2*n >= 32 {
			m := min(units-n, 16)
			if !ascii16(at(dst, n), at(src, 2*n), uint32(1)<<m-1) {
				break
			}
			n += m
		}
	}
	return n
}

func at(p *byte, off int) *byte { return (*byte)(unsafe.Add(unsafe.Pointer(p), off)) }

// ascii16 narrows the sixteen units at src to sixteen bytes at dst when every
// unit mask covers (one bit per unit) is 1 to 0x7F, and reports whether it did.
//
//go:noescape
func ascii16(dst, src *byte, mask uint32) bool

// ascii32 is ascii16 for thirty-two units, with AVX2.
//
//go:noescape
func ascii32(dst, src *byte, mask uint32) bool
