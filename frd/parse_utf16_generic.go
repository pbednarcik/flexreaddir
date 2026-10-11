//go:build !amd64 || purego

package frd

// asciiRun has no SIMD tier here; appendUTF16Scalar decodes every unit.
func asciiRun(_, _ *byte, _, _ int) int { return 0 }
