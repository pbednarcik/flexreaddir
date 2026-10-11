package frd

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16le(units []uint16) []byte {
	var b []byte
	for _, u := range units {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

// Every tier must give the scalar reference's bytes and verdict, whatever
// sits inside or after its steps.
func TestEveryTierAgreesWithTheScalarReference(t *testing.T) {
	long := utf16.Encode([]rune(strings.Repeat("abcdefgh", 9) + "ijk")) // 75 units: two AVX2 steps and a tail
	cases := [][]uint16{nil, long[:1], long[:3], long[:4], long[:15], long[:16], long[:17], long[:31], long[:32], long[:33], long[:48], long[:64], long[:65], long}
	for at := range 68 { // every position in the first two AVX2 steps, and into the step after
		for _, ins := range [][]uint16{{0}, {0x7F}, {0x80}, {0xDF}, {0x100}, {0x20AC}, {0x7FFF}, {0x8000}, {0xD83D, 0xDE42}, {0xD83D}, {0xDE42}, {0xFFFF}} {
			u := append(append(append([]uint16{}, long[:at]...), ins...), long[at:]...)
			cases = append(cases, u)
		}
	}
	prefix := []byte("keep:")
	forEachTier(t, func(tier string) {
		for _, u := range cases {
			src := utf16le(u)
			want, wantOK := appendUTF16Scalar(append([]byte{}, prefix...), src)
			got, gotOK := appendUTF16(append([]byte{}, prefix...), src)
			if gotOK != wantOK || wantOK && !bytes.Equal(got, want) {
				t.Errorf("%s, %x: packed %q %v, scalar %q %v", tier, u, got, gotOK, want, wantOK)
			}
			// Not only against the reference: a name with a NUL unit is
			// refused by every tier, whatever the reference says.
			if gotOK && slices.Contains(u, 0) {
				t.Errorf("%s, %x: a NUL unit accepted", tier, u)
			}
		}
	})
}

// A SIMD tier that never packs would pass every differential test, since the
// scalar loop decodes what it rejects; this pins that each one packs.
func TestEverySIMDTierPacksAnASCIIRun(t *testing.T) {
	// 40 units with 64 readable bytes after them, as inside a kernel buffer:
	// every step has room to read.
	src := append(utf16le(utf16.Encode([]rune(strings.Repeat("abcdefgh", 5)))), make([]byte, 64)...)
	dst := make([]byte, 40+32)
	forEachTier(t, func(tier string) {
		want := 40
		if tier == "go" {
			want = 0
		}
		if n := asciiRun(&dst[0], &src[0], 40, len(src)); n != want {
			t.Errorf("%s packed %d of 40 ASCII units, want %d", tier, n, want)
		}
	})
}

// A step reads 32 (SSE2) or 64 (AVX2) bytes; a name near the end of what may
// be read must not change, whatever follows it.
func TestEveryTierIgnoresWhatFollowsAName(t *testing.T) {
	forEachTier(t, func(tier string) {
		for _, name := range []string{"short-name", strings.Repeat("x", 20), strings.Repeat("y", 40)} {
			nu := utf16le(utf16.Encode([]rune(name)))
			for _, tail := range [][]byte{bytes.Repeat([]byte{0xFF}, 72), bytes.Repeat([]byte("\x80\xD8"), 36), bytes.Repeat([]byte("a\x00"), 36), make([]byte, 72)} {
				for readable := len(nu); readable <= len(nu)+72; readable++ {
					buf := append(append([]byte{}, nu...), tail...)
					u := buf[:len(nu):readable]
					if got, ok := appendUTF16(nil, u); !ok || string(got) != name {
						t.Errorf("%s, %q, readable %d, tail %x: %q %v", tier, name, readable, tail[:2], got, ok)
					}
				}
			}
		}
	})
}

// Every tier against the scalar reference on every name shape the SIMD steps
// can tell apart: each length from 0 to 255 units (a Windows name's limit),
// all ASCII, or with one unit at each of the pack's saturation boundaries at
// each position. Each name ends 0 to 65 bytes before its readable bound, at
// the slacks either side of a step's 32- and 64-byte reads, and the page after
// the bound faults where the platform allows (guardedPage): a step that reads
// past readable crashes.
// Fuzzing cannot stand in for this; coverage guidance does not see assembly,
// and random units are almost never a run of ASCII long enough to step.
func TestEveryTierAgreesOnEveryNameShape(t *testing.T) {
	mem := guardedPage(t)
	for i := range mem {
		mem[i] = 0xFF // past the name: a unit no step may pass
	}
	bad := []uint16{0, 0x80, 0xFF, 0x100, 0x7FFF, 0x8000, 0xD800, 0xDFFF, 0xFFFF}
	var got, want []byte
	forEachTier(t, func(tier string) {
		decodes := 0
		for _, slack := range []int{0, 1, 2, 31, 32, 33, 63, 64, 65} {
			for units := 0; units <= 255; units++ {
				end := len(mem) - slack
				src := mem[end-2*units : end : len(mem)]
				for i := range units { // every unit from 1 to 0x7F, 37 being prime to 127
					binary.LittleEndian.PutUint16(src[2*i:], uint16(1+i*37%0x7F))
				}
				decode := func(at int, b uint16) {
					var gotOK, wantOK bool
					want, wantOK = appendUTF16Scalar(want[:0], src)
					got, gotOK = appendUTF16(got[:0], src)
					if gotOK != wantOK || wantOK && !bytes.Equal(got, want) {
						t.Fatalf("%s, %d units, slack %d, %#x at %d: packed %q %v, scalar %q %v", tier, units, slack, b, at, got, gotOK, want, wantOK)
					}
					if gotOK && at >= 0 && b == 0 {
						t.Fatalf("%s, %d units, slack %d: a NUL unit at %d accepted", tier, units, slack, at)
					}
					decodes++
				}
				decode(-1, 0)
				for at := range units {
					keep := binary.LittleEndian.Uint16(src[2*at:])
					for _, b := range bad {
						binary.LittleEndian.PutUint16(src[2*at:], b)
						decode(at, b)
					}
					binary.LittleEndian.PutUint16(src[2*at:], keep)
				}
				for i := range src {
					src[i] = 0xFF
				}
			}
		}
		t.Logf("%s: %d decodes agree", tier, decodes)
	})
}

func FuzzAppendUTF16(f *testing.F) {
	f.Add(utf16le(utf16.Encode([]rune("a plain ascii name.go"))))
	f.Add(utf16le(utf16.Encode([]rune(strings.Repeat("abcdefgh", 5)))))
	f.Add(utf16le([]uint16{'a', 'b', 'c', 'd', 0xD83D, 0xDE42, 'e', 'f', 'g', 'h', 'i'}))
	f.Add(utf16le(append(utf16.Encode([]rune(strings.Repeat("z", 30))), 0)))
	f.Fuzz(func(t *testing.T, src []byte) {
		want, wantOK := appendUTF16Scalar(nil, src)
		forEachTier(t, func(tier string) {
			got, gotOK := appendUTF16(nil, src)
			if gotOK != wantOK || wantOK && !bytes.Equal(got, want) {
				t.Fatalf("%s: packed %q %v, scalar %q %v", tier, got, gotOK, want, wantOK)
			}
		})
	})
}
