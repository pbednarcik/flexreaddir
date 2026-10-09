//go:build amd64 && !purego

#include "textflag.h"

// Both kernels pack first and test after. PACKUSWB saturates a unit of 0x100
// to 0x7FFF to 0xFF and a unit of 0x8000 or more, signed negative, to 0, so a
// signed compare "greater than zero" on the packed bytes holds exactly for
// units 1 to 0x7F: NUL, every non-ASCII unit and every surrogate fail it.
// Units past the name are masked out of the test; the store writes the whole
// step, which the caller has room for. Loads and stores are unaligned, since
// Go aligns no slice (A-11.4, K-7.7). Plan 9 order is source first.

// func ascii16(dst, src *byte, mask uint32) bool
//
// SSE2, XMM registers and legacy encodings only: no VZEROUPPER, and it runs
// on every amd64. PACKUSWB X1, X0 is Intel packuswb xmm0, xmm1 (units 0-7,
// then 8-15); PCMPGTB X2, X3 is pcmpgtb xmm3, xmm2, so X3 = X3 > 0.
TEXT ·ascii16(SB), NOSPLIT, $0-25
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVL	mask+16(FP), R8
	MOVOU	(SI), X0
	MOVOU	16(SI), X1
	PACKUSWB	X1, X0
	PXOR	X2, X2
	MOVO	X0, X3
	PCMPGTB	X2, X3
	PMOVMSKB	X3, DX
	ANDL	R8, DX
	CMPL	DX, R8
	JNE	fail16
	MOVOU	X0, (DI)
	MOVB	$1, ret+24(FP)
	RET
fail16:
	MOVB	$0, ret+24(FP)
	RET

// func ascii32(dst, src *byte, mask uint32) bool
//
// AVX2. VPACKUSWB packs within each 128-bit lane (units 0-7, 16-23 | 8-15,
// 24-31), so VPERMQ $0xD8 puts the quadwords back in order (K-7.5).
// VZEROUPPER ends both return paths, since the Go code around it is legacy
// SSE (A-13.6, K-7.6).
TEXT ·ascii32(SB), NOSPLIT, $0-25
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVL	mask+16(FP), R8
	VMOVDQU	(SI), Y0
	VMOVDQU	32(SI), Y1
	VPACKUSWB	Y1, Y0, Y0
	VPERMQ	$0xD8, Y0, Y0
	VPXOR	Y2, Y2, Y2
	VPCMPGTB	Y2, Y0, Y3
	VPMOVMSKB	Y3, DX
	ANDL	R8, DX
	CMPL	DX, R8
	JNE	fail32
	VMOVDQU	Y0, (DI)
	VZEROUPPER
	MOVB	$1, ret+24(FP)
	RET
fail32:
	VZEROUPPER
	MOVB	$0, ret+24(FP)
	RET
