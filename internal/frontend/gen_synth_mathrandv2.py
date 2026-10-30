#!/usr/bin/env python3
# P3-M9 (tier1c): generate synth_mathrandv2.go (full `math/rand/v2`) from
# gc 1.27.1's math/rand/v2.
#
# Transformations (all documented in the emitted file header):
#  1. //go:linkname runtime_rand runtime.rand -> composed from two
#     runtime.fastrand linkname calls (same as the v1 port; the only
#     consumer is the nondeterministic global auto-seed path).
#  2. internal/chacha8rand is inlined as private __chacha8* declarations:
#     - block_generic's unsafe.Pointer reinterpretation is replaced by
#       explicit little-endian uint32 word extraction/insertion
#       (frontend/v1 unsafe is empty; the differential target is LE).
#     - setup's unsafe.Pointer 64-bit stores are replaced by direct
#       uint32 stores (same values, LE).
#     - qr (multi-result) becomes __chacha8Qr returning [4]uint32.
#     - goarch.BigEndian branches take the LE path (documented).
#     All other chacha8rand logic (State, Next, Init, Refill, Reseed,
#     Marshal, Unmarshal) is verbatim.
#  3. Exported multi-result methods are not emittable and are dropped:
#     ChaCha8.AppendBinary/MarshalBinary, PCG.AppendBinary/
#     MarshalBinary, and the 4-result GetNormalDistributionParameters /
#     GetExponentialDistributionParameters (not used by v2 itself).
#     Unexported multi-result helpers (pcg.next, cutPrefix,
#     readUint8LengthPrefixed) become single-result pair-struct
#     functions with rewritten call sites; 2-target bits.* calls inside
#     the ported sources are rewritten to the __bits* helpers.
#     Retest trigger for the remaining dropped methods: frontend
#     supports multi-result method emission for BinaryMarshaler shapes
#     (ChaCha8.Read was restored on that machinery 2026-10-30).
#  4. rand.go / pcg.go / exp.go / normal.go / zipf.go carried verbatim
#     (pcg.go imports the synthetic internal/byteorder, vendored by the
#     mathbig worker).//go:linkname runtime_rand runtime.rand -> composed from two
#     runtime.fastrand linkname calls (same as the v1 port; the only
#     consumer is the nondeterministic global auto-seed path).
#  2. internal/chacha8rand is inlined as private __chacha8* declarations:
#     - block_generic's unsafe.Pointer reinterpretation is replaced by
#       explicit little-endian uint32 word extraction/insertion
#       (frontend/v1 unsafe is empty; the differential target is LE).
#     - setup's unsafe.Pointer 64-bit stores are replaced by direct
#       uint32 stores (same values, LE).
#     - qr (multi-result) becomes __chacha8Qr returning [4]uint32.
#     - goarch.BigEndian branches take the LE path (documented).
#     All other chacha8rand logic (State, Next, Init, Refill, Reseed,
#     Marshal, Unmarshal) is verbatim.
#  3. rand.go / pcg.go / exp.go / normal.go / zipf.go carried verbatim
#     (pcg.go imports the synthetic internal/byteorder, vendored by the
#     mathbig worker).
import os
import re
import subprocess

# --- P1-R1-2 portability prologue: GOROOT and OUTDIR are environment-driven
# so the generator is CI-runnable and never writes outside the repo.
#   GRAALGO_GOROOT   Go installation whose gc 1.27.1 sources are read
#                    (default: `go env GOROOT`).
#   GEN_SYNTH_OUTDIR directory for the generated file(s) (default: this
#                    script's directory, i.e. in-place regeneration).
_HERE = os.path.dirname(os.path.abspath(__file__))

def _graalgo_goroot():
    g = os.environ.get("GRAALGO_GOROOT")
    if g:
        return g
    try:
        return subprocess.run(["go", "env", "GOROOT"], capture_output=True,
                              text=True, check=True).stdout.strip()
    except Exception as e:
        raise SystemExit(
            "gen_synth: cannot determine GOROOT; set GRAALGO_GOROOT (%s)" % e)

OUTDIR = os.environ.get("GEN_SYNTH_OUTDIR", _HERE)

GOROOT = os.path.join(_graalgo_goroot(), "src", "math", "rand", "v2")

CHACHA = os.path.join(_graalgo_goroot(), "src", "internal", "chacha8rand")

HEADER = '''// P3-M9 (tier1c): synthetic `math/rand/v2` package (FULL port).
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the P3-M7 synthetic math package,
// ADR-0007 D5).
//
// Value model:
//   - rand.go, pcg.go, chacha8.go, exp.go, normal.go, zipf.go are
//     gc 1.27.1's implementations ported verbatim (bit-exact by
//     construction for fixed seeds; ChaCha8 verified byte-identical
//     vs gc, modulo the Deviation 2 adaptations below).
//   - DEVIATION 1 (documented): //go:linkname runtime_rand runtime.rand
//     -> composed from two runtime.fastrand linkname calls (runtime.rand
//     is not in the linkname table). Values differ from gc's
//     runtime.rand, but the only consumer is the nondeterministic
//     global auto-seed path (never pinned by the differential corpus,
//     which uses fixed seeds only).
//   - DEVIATION 2 (documented): internal/chacha8rand is inlined as
//     private __chacha8* declarations (no other worker owns it).
//     block_generic's unsafe.Pointer reinterpretation becomes explicit
//     little-endian uint32 word extraction/insertion (frontend/v1's
//     unsafe package is empty; amd64/LE is the differential target);
//     setup's 64-bit stores become direct uint32 stores (same values);
//     multi-result qr becomes __chacha8Qr returning [4]uint32;
//     goarch.BigEndian branches take the LE path. All other logic is
//     verbatim.
//   - DEVIATION 3 (documented): exported multi-result methods are not
//     emittable and are dropped, except ChaCha8.Read (restored 2026-10-30,
//     review P1-14, via the __chacha8Read pair helper + the m7 desugar
//     table entry, on the r40 multi-result-method machinery — the old
//     retest trigger, now satisfied for Read): ChaCha8.AppendBinary/
//     MarshalBinary and PCG.AppendBinary/MarshalBinary remain dropped,
//     so calling them fails at type-check (loud, never silent).
//     Unexported multi-result helpers (pcg.next, cutPrefix,
//     readUint8LengthPrefixed) become single-result pair-struct
//     functions with rewritten call sites; 2-target bits.* calls inside
//     the ported sources are rewritten to the __bits* helpers.
//     Retest trigger for the remaining dropped methods: frontend
//     supports multi-result method emission for BinaryMarshaler shapes
//     (or the methods are restored via pair helpers like Read was).
//
// Generated by gen_synth_mathrandv2.py from gc 1.27.1; DO NOT EDIT BY
// HAND (edit the generator and re-run).
package main

// syntheticMathRandV2Source is the P3-M9 synthetic `math/rand/v2`
// package (full port).
const syntheticMathRandV2Source = `package rand

import "math"
import "math/bits"
import "errors"

// P3-M9: Local __bits* helpers (the math/bits synthetic's helpers are
// private to that package; v2 needs them for the rewritten call sites).
// Bit-exact implementations (copied from the math/bits synthetic).
type __bitsAdd64Pair struct {
	sum      uint64
	carryOut uint64
}

func __bitsAdd64(x, y, carry uint64) __bitsAdd64Pair {
	sum := x + y + carry
	carryOut := ((x & y) | ((x | y) &^ sum)) >> 63
	return __bitsAdd64Pair{sum: sum, carryOut: carryOut}
}

type __bitsMul64Pair struct {
	hi uint64
	lo uint64
}

func __bitsMul64(x, y uint64) __bitsMul64Pair {
	// 64x64 -> 128 bit multiplication (gc algorithm).
	xlo := uint64(uint32(x))
	xhi := x >> 32
	ylo := uint64(uint32(y))
	yhi := y >> 32
	p0 := xlo * ylo
	p1 := xlo * yhi
	p2 := xhi * ylo
	p3 := xhi * yhi
	mid := (p0 >> 32) + uint64(uint32(p1)) + uint64(uint32(p2))
	lo := (p0 & 0xffffffff) | (mid << 32)
	hi := p3 + (p1 >> 32) + (p2 >> 32) + (mid >> 32)
	return __bitsMul64Pair{hi: hi, lo: lo}
}

type __bitsAdd32Pair struct {
	sum      uint32
	carryOut uint32
}

func __bitsAdd32(x, y, carry uint32) __bitsAdd32Pair {
	sum := x + y + carry
	carryOut := ((x & y) | ((x | y) &^ sum)) >> 31
	return __bitsAdd32Pair{sum: sum, carryOut: carryOut}
}

type __bitsMul32Pair struct {
	hi uint32
	lo uint32
}

func __bitsMul32(x, y uint32) __bitsMul32Pair {
	p := uint64(x) * uint64(y)
	return __bitsMul32Pair{hi: uint32(p >> 32), lo: uint32(p)}
}
import "internal/byteorder"
import _ "unsafe" // for go:linkname

// ---- Deviation 1: runtime_rand via runtime.fastrand (see header) ----
//go:linkname fastrand runtime.fastrand
func fastrand() uint32

func runtime_rand() uint64 {
	return uint64(fastrand())<<32 | uint64(fastrand())
}

// ---- Deviation 2: inlined internal/chacha8rand (see header) ----

const (
	chacha8CtrInc = 4  // increment counter by 4 between block calls
	chacha8CtrMax = 16 // reseed when counter reaches 16
	chacha8Chunk  = 32 // each chunk produced by block is 32 uint64s
	chacha8Reseed = 4  // reseed with 4 words
)

// __chacha8State is internal/chacha8rand.State, inlined.
type __chacha8State struct {
	buf  [32]uint64
	seed [4]uint64
	i    uint32
	n    uint32
	c    uint32
}

// __chacha8Next is (s *State).Next, inlined (single-result: the pair
// is returned as a struct because multi-result funcdecls are not
// emittable).
type __chacha8NextPair struct {
	v  uint64
	ok bool
}

func __chacha8Next(s *__chacha8State) __chacha8NextPair {
	i := s.i
	if i >= s.n {
		return __chacha8NextPair{}
	}
	s.i = i + 1
	return __chacha8NextPair{v: s.buf[i&31], ok: true} // i&31 eliminates bounds check
}

// __chacha8Init is (s *State).Init, inlined.
func __chacha8Init(s *__chacha8State, seed [32]byte) {
	__chacha8Init64(s, [4]uint64{
		byteorder.LEUint64(seed[0*8:]),
		byteorder.LEUint64(seed[1*8:]),
		byteorder.LEUint64(seed[2*8:]),
		byteorder.LEUint64(seed[3*8:]),
	})
}

// __chacha8Init64 is (s *State).Init64, inlined.
func __chacha8Init64(s *__chacha8State, seed [4]uint64) {
	s.seed = seed
	__chacha8Block(&s.seed, &s.buf, 0)
	s.c = 0
	s.i = 0
	s.n = chacha8Chunk
}

// __chacha8Refill is (s *State).Refill, inlined.
func __chacha8Refill(s *__chacha8State) {
	s.c += chacha8CtrInc
	if s.c == chacha8CtrMax {
		// Reseed with generated uint64s for forward secrecy.
		s.seed[0] = s.buf[len(s.buf)-chacha8Reseed+0]
		s.seed[1] = s.buf[len(s.buf)-chacha8Reseed+1]
		s.seed[2] = s.buf[len(s.buf)-chacha8Reseed+2]
		s.seed[3] = s.buf[len(s.buf)-chacha8Reseed+3]
		s.c = 0
	}
	__chacha8Block(&s.seed, &s.buf, s.c)
	s.i = 0
	s.n = uint32(len(s.buf))
	if s.c == chacha8CtrMax-chacha8CtrInc {
		s.n = uint32(len(s.buf)) - chacha8Reseed
	}
}

// __chacha8Reseed is (s *State).Reseed, inlined.
func __chacha8Reseed(s *__chacha8State) {
	var seed [4]uint64
	for i := range seed {
		for {
			p := __chacha8Next(s)
			if p.ok {
				seed[i] = p.v
				break
			}
			__chacha8Refill(s)
		}
	}
	__chacha8Init64(s, seed)
}

// __chacha8Marshal is chacha8rand.Marshal, inlined.
func __chacha8Marshal(s *__chacha8State) []byte {
	data := make([]byte, 6*8)
	copy(data, "chacha8:")
	used := (s.c/chacha8CtrInc)*chacha8Chunk + s.i
	byteorder.BEPutUint64(data[1*8:], uint64(used))
	for i, seed := range s.seed {
		byteorder.LEPutUint64(data[(2+i)*8:], seed)
	}
	return data
}

type __errUnmarshalChaCha8 struct{}

func (*__errUnmarshalChaCha8) Error() string {
	return "invalid ChaCha8 encoding"
}

// __chacha8Unmarshal is chacha8rand.Unmarshal, inlined.
func __chacha8Unmarshal(s *__chacha8State, data []byte) error {
	if len(data) != 6*8 || string(data[:8]) != "chacha8:" {
		return new(__errUnmarshalChaCha8)
	}
	used := byteorder.BEUint64(data[1*8:])
	if used > (chacha8CtrMax/chacha8CtrInc)*chacha8Chunk-chacha8Reseed {
		return new(__errUnmarshalChaCha8)
	}
	for i := range s.seed {
		s.seed[i] = byteorder.LEUint64(data[(2+i)*8:])
	}
	s.c = chacha8CtrInc * (uint32(used) / chacha8Chunk)
	__chacha8Block(&s.seed, &s.buf, s.c)
	s.i = uint32(used) % chacha8Chunk
	s.n = chacha8Chunk
	if s.c == chacha8CtrMax-chacha8CtrInc {
		s.n = chacha8Chunk - chacha8Reseed
	}
	return nil
}

// __chacha8Setup is internal/chacha8rand setup, with the unsafe.Pointer
// 64-bit stores replaced by direct uint32 stores (LE; same values).
func __chacha8Setup(seed *[4]uint64, b32 *[16][4]uint32, counter uint32) {
	// Constants; same as in ChaCha20: "expand 32-byte k".
	// (Each uint64 store below wrote the same uint32 twice, LE.)
	for j := 0; j < 4; j++ {
		b32[0][j] = 0x61707865
		b32[1][j] = 0x3320646e
		b32[2][j] = 0x79622d32
		b32[3][j] = 0x6b206574
	}

	// Seed values.
	var x uint32
	x = uint32(seed[0])
	for j := 0; j < 4; j++ {
		b32[4][j] = x
	}
	x = uint32(seed[0] >> 32)
	for j := 0; j < 4; j++ {
		b32[5][j] = x
	}
	x = uint32(seed[1])
	for j := 0; j < 4; j++ {
		b32[6][j] = x
	}
	x = uint32(seed[1] >> 32)
	for j := 0; j < 4; j++ {
		b32[7][j] = x
	}
	x = uint32(seed[2])
	for j := 0; j < 4; j++ {
		b32[8][j] = x
	}
	x = uint32(seed[2] >> 32)
	for j := 0; j < 4; j++ {
		b32[9][j] = x
	}
	x = uint32(seed[3])
	for j := 0; j < 4; j++ {
		b32[10][j] = x
	}
	x = uint32(seed[3] >> 32)
	for j := 0; j < 4; j++ {
		b32[11][j] = x
	}

	// Counters (LE).
	b32[12][0] = counter + 0
	b32[12][1] = counter + 1
	b32[12][2] = counter + 2
	b32[12][3] = counter + 3

	// Zeros.
	for j := 0; j < 4; j++ {
		b32[13][j] = 0
		b32[14][j] = 0
		b32[15][j] = 0
	}
}

// __chacha8Qr is the ChaCha8 quarter round (gc's qr, restructured to
// single-result: multi-result funcdecls are not emittable).
func __chacha8Qr(a, b, c, d uint32) [4]uint32 {
	a += b
	d ^= a
	d = d<<16 | d>>16
	c += d
	b ^= c
	b = b<<12 | b>>20
	a += b
	d ^= a
	d = d<<8 | d>>24
	c += d
	b ^= c
	b = b<<7 | b>>25
	return [4]uint32{a, b, c, d}
}

// __chacha8Block is internal/chacha8rand block_generic, with the
// unsafe.Pointer view replaced by explicit LE uint32 word
// extraction/insertion (same observable buf contents).
func __chacha8Block(seed *[4]uint64, buf *[32]uint64, counter uint32) {
	var b32 [16][4]uint32
	__chacha8Setup(seed, &b32, counter)

	for i := 0; i < 16; i++ {
		// Load block i into local variables.
		b0 := b32[0][i]
		b1 := b32[1][i]
		b2 := b32[2][i]
		b3 := b32[3][i]
		b4 := b32[4][i]
		b5 := b32[5][i]
		b6 := b32[6][i]
		b7 := b32[7][i]
		b8 := b32[8][i]
		b9 := b32[9][i]
		b10 := b32[10][i]
		b11 := b32[11][i]
		b12 := b32[12][i]
		b13 := b32[13][i]
		b14 := b32[14][i]
		b15 := b32[15][i]

		// 4 iterations of eight quarter-rounds each is 8 rounds.
		for round := 0; round < 4; round++ {
			q := __chacha8Qr(b0, b4, b8, b12)
			b0, b4, b8, b12 = q[0], q[1], q[2], q[3]
			q = __chacha8Qr(b1, b5, b9, b13)
			b1, b5, b9, b13 = q[0], q[1], q[2], q[3]
			q = __chacha8Qr(b2, b6, b10, b14)
			b2, b6, b10, b14 = q[0], q[1], q[2], q[3]
			q = __chacha8Qr(b3, b7, b11, b15)
			b3, b7, b11, b15 = q[0], q[1], q[2], q[3]

			q = __chacha8Qr(b0, b5, b10, b15)
			b0, b5, b10, b15 = q[0], q[1], q[2], q[3]
			q = __chacha8Qr(b1, b6, b11, b12)
			b1, b6, b11, b12 = q[0], q[1], q[2], q[3]
			q = __chacha8Qr(b2, b7, b8, b13)
			b2, b7, b8, b13 = q[0], q[1], q[2], q[3]
			q = __chacha8Qr(b3, b4, b9, b14)
			b3, b4, b9, b14 = q[0], q[1], q[2], q[3]
		}

		// Store block i back. Add b4..b11 back to the original key
		// material, like in ChaCha20, to avoid trivial invertibility.
		b32[0][i] = b0
		b32[1][i] = b1
		b32[2][i] = b2
		b32[3][i] = b3
		b32[4][i] += b4
		b32[5][i] += b5
		b32[6][i] += b6
		b32[7][i] += b7
		b32[8][i] += b8
		b32[9][i] += b9
		b32[10][i] += b10
		b32[11][i] += b11
		b32[12][i] = b12
		b32[13][i] = b13
		b32[14][i] = b14
		b32[15][i] = b15
	}

	// Write the uint32 words back into buf as LE uint64s.
	// (gc's unsafe view: b[i][j] = uint32(buf[i*2+j/2] >> 32*(j%2)).)
	for i := 0; i < 16; i++ {
		buf[i*2+0] = uint64(b32[i][0]) | uint64(b32[i][1])<<32
		buf[i*2+1] = uint64(b32[i][2]) | uint64(b32[i][3])<<32
	}
}

'''


def strip_imports(src):
    src = re.sub(r'^import \(\n(?:.*\n)*?^\)\n', '', src, count=1, flags=re.M)
    src = re.sub(r'^import "[^"]*"\n', '', src, flags=re.M)
    src = re.sub(r'^import _ "[^"]*"( //.*)?\n', '', src, flags=re.M)
    return src


def strip_package(src):
    return re.sub(r"^package rand\n", "", src, count=1, flags=re.M)


# Rewrites 2-target bits.Mul/Add/Sub/Div calls (which are not emittable
# inside synthetic sources) to the single-result __bits* helpers.
# Fields: Mul->hi/lo, Add->sum/carryOut, Sub->diff/borrowOut,
# Div->quo/rem.
_BITS_FIELDS = {
    "Mul": ("hi", "lo"),
    "Add": ("sum", "carryOut"),
    "Sub": ("diff", "borrowOut"),
    "Div": ("quo", "rem"),
}


def rewrite_bits_calls(src):
    # Matches e.g. "\thi, lo = bits.Mul64(p.lo, mulLo)". Args may contain
    # one level of nested parens (e.g. r.Uint64()).
    pat = re.compile(
        r"^([ \t]*)(\w+|_), (\w+|_) (:=|=) bits\.(Mul|Add|Sub|Div)(32|64)?\(((?:[^()]|\([^()]*\))*)\)$",
        re.M,
    )
    counter = [0]

    def repl(m):
        indent, t1, t2, assign, op, width, args = m.groups()
        f1, f2 = _BITS_FIELDS[op]
        counter[0] += 1
        tmp = f"__bw{counter[0]}"
        out = [f"{indent}{tmp} := __bits{op}{width or ''}({args})"]
        if t1 == "_" and t2 == "_":
            return out[0]
        if t1 == "_":
            out.append(f"{indent}{t2} {assign} {tmp}.{f2}")
        elif t2 == "_":
            out.append(f"{indent}{t1} {assign} {tmp}.{f1}")
        else:
            out.append(f"{indent}{t1}, {t2} {assign} {tmp}.{f1}, {tmp}.{f2}")
        return "\n".join(out)

    src, n = pat.subn(repl, src)
    # Ensure no 2-target bits calls remain (outside comments).
    for line in src.split("\n"):
        s = line.strip()
        if s.startswith("//"):
            continue
        assert not re.search(r"bits\.(Mul|Add|Sub|Div)(32|64)?\(", line), f"unrewritten: {line}"
    return src


def build_rand_go():
    with open(f"{GOROOT}/rand.go") as f:
        src = f.read()
    src = strip_package(src)
    src = strip_imports(src)
    src = rewrite_bits_calls(src)
    # Drop the runtime_rand linkname decl (header provides the impl).
    src = src.replace(
        '//go:linkname runtime_rand runtime.rand\nfunc runtime_rand() uint64\n', ""
    )
    return src


def build_pcg_go():
    with open(f"{GOROOT}/pcg.go") as f:
        src = f.read()
    src = strip_package(src)
    src = strip_imports(src)
    src = rewrite_bits_calls(src)
    # next(): multi-result method -> single-result free function with pair.
    src = src.replace(
        "func (p *PCG) next() (hi, lo uint64) {",
        "type __pcgNextPair struct{ hi, lo uint64 }\n\n"
        "func __pcgNext(p *PCG) __pcgNextPair {\n\tvar hi, lo uint64",
    )
    # Rewrite the two call sites (rand.go:87 and the commented example at
    # :90 which is inside a comment; only :87 is code).
    # The body ends with "return hi, lo"; convert to pair return.
    src = src.replace("\treturn hi, lo\n}", "\treturn __pcgNextPair{hi, lo}\n}", 1)
    # Call site in Uint64: "hi, lo := p.next()" -> pair destructuring.
    # (Only the live one at line 87; line 90 is in a comment.)
    src = src.replace(
        "\thi, lo := p.next()\n\n\t// XSL-RR",
        "\t__np := __pcgNext(p)\n\thi, lo := __np.hi, __np.lo\n\n\t// XSL-RR",
    )
    # Drop exported multi-result methods (not emittable; see header).
    src = strip_method(src, "PCG", "AppendBinary")
    src = strip_method(src, "PCG", "MarshalBinary")
    return src


def strip_method(src, recv, name):
    # Remove "func (x *Recv) Name(...) ... \n}\n".
    pat = re.compile(r"^func \([^)]+ \*?" + recv + r"\) " + name + r"\(.*?^}\n", re.M | re.S)
    src, n = pat.subn("", src, count=1)
    assert n == 1, f"strip_method {recv}.{name}: {n}"
    return src


def strip_func(src, name):
    pat = re.compile(r"^func " + name + r"\(.*?^}\n", re.M | re.S)
    src, n = pat.subn("", src, count=1)
    assert n == 1, f"strip_func {name}: {n}"
    return src


def build_verbatim(name):
    with open(f"{GOROOT}/{name}") as f:
        src = f.read()
    src = strip_package(src)
    src = strip_imports(src)
    return src


def build_chacha8_go():
    with open(f"{GOROOT}/chacha8.go") as f:
        src = f.read()
    src = strip_package(src)
    src = strip_imports(src)
    # Replace chacha8rand references with the inlined versions.
    src = src.replace("chacha8rand.State", "__chacha8State")
    src = src.replace("c.state.Init(seed)", "__chacha8Init(&c.state, seed)")
    # Methods on *ChaCha8 that call state methods. __chacha8Next is
    # single-result (pair struct); rewrite the 2-target call site.
    src = src.replace("c.state.Next()", "__chacha8Next(&c.state)")
    src = src.replace("c.state.Refill()", "__chacha8Refill(&c.state)")
    # v2's Uint64 body: "x, ok := __chacha8Next(&c.state)" ->
    # pair-struct form.
    src = src.replace(
        "x, ok := __chacha8Next(&c.state)",
        "p := __chacha8Next(&c.state); x, ok := p.v, p.ok",
    )
    src = src.replace("chacha8rand.Marshal(&c.state)", "__chacha8Marshal(&c.state)")
    src = src.replace("chacha8rand.Unmarshal(&c.state, data)", "__chacha8Unmarshal(&c.state, data)")
    # cutPrefix / readUint8LengthPrefixed: multi-result -> pair structs.
    src = src.replace(
        "func cutPrefix(s, prefix []byte) (after []byte, found bool) {",
        "type __cutPrefixPair struct {\n\tafter []byte\n\tfound bool\n}\n\n"
        "func __cutPrefix(s, prefix []byte) __cutPrefixPair {",
    )
    src = src.replace(
        "func readUint8LengthPrefixed(b []byte) (buf, rest []byte, ok bool) {",
        "type __readUint8LengthPrefixedPair struct {\n\tbuf []byte\n\trest []byte\n\tok   bool\n}\n\n"
        "func __readUint8LengthPrefixed(b []byte) __readUint8LengthPrefixedPair {",
    )
    # Rewrite their return statements and call sites (in UnmarshalBinary).
    src = src.replace(
        "\treturn s, false\n\t}\n\treturn s[len(prefix):], true\n}",
        "\treturn __cutPrefixPair{after: s}\n\t}\n\treturn __cutPrefixPair{after: s[len(prefix):], found: true}\n}",
    )
    src = src.replace(
        "\treturn nil, nil, false\n\t}\n\treturn b[1 : 1+b[0]], b[1+b[0]:], true\n}",
        "\treturn __readUint8LengthPrefixedPair{}\n\t}\n\treturn __readUint8LengthPrefixedPair{buf: b[1 : 1+b[0]], rest: b[1+b[0]:], ok: true}\n}",
    )
    src = src.replace(
        'data, ok := cutPrefix(data, []byte("readbuf:"))',
        '__cp := __cutPrefix(data, []byte("readbuf:")); data, ok := __cp.after, __cp.found',
    )
    src = src.replace(
        "buf, data, ok = readUint8LengthPrefixed(data)",
        "__rp := __readUint8LengthPrefixed(data); buf, data, ok = __rp.buf, __rp.rest, __rp.ok",
    )
    # ChaCha8.Read restored (review 2026-10-30 P1-14): the multi-result
    # method becomes a pair helper + desugar stub (same pattern as the
    # other m7 entries; the itab maps interface dispatch to the helper).
    # The body has a single bare return at the end; the helper declares
    # n/err explicitly and returns the pair. The old panic message named
    # "non-standard ECDSA curves" as the blocker — nonsense for ChaCha8
    # (leaked from the signLegacy use-case); the real blocker was
    # multi-result method emission, which the r40 machinery provides.
    m = re.search(r"^func \(c \*ChaCha8\) Read\(p \[\]byte\) \(n int, err error\) \{(.*?)^\}\n",
                  src, flags=re.M | re.S)
    assert m is not None, "ChaCha8.Read not found in chacha8.go"
    body = m.group(1)
    assert body.count("\n\treturn\n") == 1, "ChaCha8.Read body has unexpected returns"
    body = body.replace("\n\treturn\n", "\n\treturn __chacha8ReadPair{n: n, err: err}\n", 1)
    helper = ("type __chacha8ReadPair struct {\n\tn int\n\terr error\n}\n\n"
              "func __chacha8Read(c *ChaCha8, p []byte) __chacha8ReadPair {\n\tvar n int\n\tvar err error"
              + body + "}\n\n"
              "func (c *ChaCha8) Read(p []byte) (n int, err error) {\n"
              "\tpanic(\"math/rand/v2: ChaCha8.Read: multi-result desugar\")\n}\n")
    src = src[:m.start()] + helper + src[m.end():]
    # Drop the remaining exported multi-result methods (not emittable;
    # see header Deviation 3). Calling them fails at type-check.
    src = strip_method(src, "ChaCha8", "AppendBinary")
    src = strip_method(src, "ChaCha8", "MarshalBinary")
    assert "chacha8rand" not in src, "chacha8rand refs remain"
    return src


def main():
    parts = [HEADER]
    parts.append(build_rand_go().rstrip() + "\n\n")
    parts.append(build_pcg_go().rstrip() + "\n\n")
    # ChaCha8 restored (review 2026-10-30 P1-14): the old scope limit
    # ("requires & on SelectorExpr") is re-tested by the exporter suite
    # (TestChaCha8Exports) on every run of this generator's output.
    parts.append(build_chacha8_go().rstrip() + "\n\n")
    for name in ["exp.go", "normal.go", "zipf.go"]:
        parts.append(build_verbatim(name).rstrip() + "\n\n")
    out = "".join(parts)
    # Remove unused math/bits import (all calls rewritten to __bits*).
    out = out.replace('import "math/bits"\n', '')
    # Move the __bits* helpers to after the last import (they're currently
    # in HEADER before some imports added by build_*).
    # Extract helpers from out.
    helpers_pat = r"// P3-M9: Local __bits\* helpers.*?func __bitsMul32\(x, y uint32\) __bitsMul32Pair \{.*?\n\}\n"
    m = re.search(helpers_pat, out, re.S)
    if m:
        helpers = m.group(0)
        out = out.replace(helpers, "", 1)
        # Find the last line starting with 'import' and insert after it.
        lines = out.split('\n')
        last_import_idx = -1
        for i, line in enumerate(lines):
            if line.strip().startswith('import '):
                last_import_idx = i
        if last_import_idx >= 0:
            lines.insert(last_import_idx + 1, "")
            lines.insert(last_import_idx + 2, helpers.rstrip())
            out = '\n'.join(lines)
    out = out.rstrip() + "\n`\n"
    with open(os.path.join(OUTDIR, "synth_mathrandv2.go"), "w") as f:
        f.write(out)
    print("wrote synth_mathrandv2.go", len(out), "bytes")


main()
