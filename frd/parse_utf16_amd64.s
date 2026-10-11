//go:build amd64 && !purego

#include "textflag.h"

// The kernel packs first and tests after. PACKUSWB saturates a unit of 0x100
// to 0x7FFF to 0xFF and a unit of 0x8000 or more, signed negative, to 0, so a
// signed compare "greater than zero" on the packed bytes holds exactly for
// units 1 to 0x7F: NUL, every non-ASCII unit and every surrogate fail it.
// Units past the name are masked out of the test; the store writes the whole
// step, which the caller has room for. Loads and stores are unaligned, since
// Go aligns no slice (A-11.4, K-7.7). Plan 9 order is source first.

// func asciiRunKernel(dst, src *byte, units, readable int, avx2, sse2 bool) int
//
// asciiRun's two step loops in one call, so a name pays one call however many
// steps it takes (M-25.22; measured 2026-10-11).
// While avx2, n+16 < units and readable-2n >= 64, an AVX2 step packs the
// thirty-two units at src+2n; then, while sse2, n < units and
// readable-2n >= 32, an SSE2 step packs sixteen. A step's mask keeps only the
// units the name still has, min(units-n, 32 or 16), so the units after it
// never fail a step. A failed AVX2 step falls to the SSE2 loop at the same n;
// a failed SSE2 step ends the run.
//
// AVX2 step: VPACKUSWB packs within each 128-bit lane (units 0-7, 16-23 |
// 8-15, 24-31), so VPERMQ $0xD8 puts the quadwords back in order (K-7.5).
// VZEROUPPER runs once the AVX2 loop is left, before any legacy SSE
// instruction and before RET, since the Go code around it is legacy SSE
// (A-13.6, K-7.6). SSE2 step: legacy encodings only; PACKUSWB X1, X0 is Intel
// packuswb xmm0, xmm1 (units 0-7, then 8-15); PCMPGTB X2, X3 is pcmpgtb xmm3,
// xmm2, so X3 = X3 > 0.
//
// Registers: DI dst, SI src, BX units, R9 readable, AX n, R13 m, R8 mask,
// CX shift count, DX movemask, R10, R11 and R12 scratch.
TEXT ·asciiRunKernel(SB), NOSPLIT, $0-48
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	units+16(FP), BX
	MOVQ	readable+24(FP), R9
	XORQ	AX, AX
	MOVBLZX	avx2+32(FP), R10
	TESTQ	R10, R10
	JZ	sse

avxloop:
	LEAQ	16(AX), R11
	CMPQ	R11, BX
	JGE	avxdone
	MOVQ	R9, R12
	SUBQ	AX, R12
	SUBQ	AX, R12
	CMPQ	R12, $64
	JLT	avxdone
	MOVQ	BX, R13
	SUBQ	AX, R13
	CMPQ	R13, $32
	JLE	avxmask
	MOVQ	$32, R13
avxmask:
	MOVL	$0xFFFFFFFF, R8
	MOVQ	$32, CX
	SUBQ	R13, CX
	SHRL	CX, R8
	VMOVDQU	(SI)(AX*2), Y0
	VMOVDQU	32(SI)(AX*2), Y1
	VPACKUSWB	Y1, Y0, Y0
	VPERMQ	$0xD8, Y0, Y0
	VPXOR	Y2, Y2, Y2
	VPCMPGTB	Y2, Y0, Y3
	VPMOVMSKB	Y3, DX
	ANDL	R8, DX
	CMPL	DX, R8
	JNE	avxdone
	VMOVDQU	Y0, (DI)(AX*1)
	ADDQ	R13, AX
	JMP	avxloop

avxdone:
	VZEROUPPER

sse:
	MOVBLZX	sse2+33(FP), R10
	TESTQ	R10, R10
	JZ	done

sseloop:
	CMPQ	AX, BX
	JGE	done
	MOVQ	R9, R12
	SUBQ	AX, R12
	SUBQ	AX, R12
	CMPQ	R12, $32
	JLT	done
	MOVQ	BX, R13
	SUBQ	AX, R13
	CMPQ	R13, $16
	JLE	ssemask
	MOVQ	$16, R13
ssemask:
	MOVL	$0xFFFF, R8
	MOVQ	$16, CX
	SUBQ	R13, CX
	SHRL	CX, R8
	MOVOU	(SI)(AX*2), X0
	MOVOU	16(SI)(AX*2), X1
	PACKUSWB	X1, X0
	PXOR	X2, X2
	MOVO	X0, X3
	PCMPGTB	X2, X3
	PMOVMSKB	X3, DX
	ANDL	R8, DX
	CMPL	DX, R8
	JNE	done
	MOVOU	X0, (DI)(AX*1)
	ADDQ	R13, AX
	JMP	sseloop

done:
	MOVQ	AX, ret+40(FP)
	RET
