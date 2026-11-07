#!/usr/bin/env python3
"""Build GoFloatFmtTest.java from gc-computed gc-vectors.txt.
Expected strings come from gc (strconv), values from IEEE bit patterns.
Groups route to the matching GoFloatFmt entry point:
  formatFloat for e/E/f/g/G, formatHex for x/X, formatBinary for b,
  formatComplex for complex (bitSize 128 -> float64 parts, 64 -> float32).
"""
import re, sys

groups = {
    'short64': [], 'short32': [], 'gprec': [], 'fixed': [],
    'hex': [], 'bin': [], 'complex': [],
}
seen = set()
for raw in open('/tmp/rev1107-22-s04/gc-vectors.txt'):
    raw = raw.rstrip('\n')
    if not raw:
        continue
    m = re.match(r'^(F64|F32|C128|C64)\s+(0x[0-9A-F]+)(?:\s+(0x[0-9A-F]+))?\s+(\S)\s+(-?\d+)\s+("(?:[^"\\]|\\.)*")$', raw)
    assert m, raw
    kind, b1, b2, verb, prec, exp = m.groups()
    prec = int(prec)
    if kind == 'C128':
        line = f'        checkComplex(0x{b1[2:]}L, 0x{b2[2:]}L, \'{verb}\', {prec}, 128, {exp});'
        g = 'complex'
    elif kind == 'C64':
        line = f'        checkComplex32(0x{b1[2:]}, 0x{b2[2:]}, \'{verb}\', {prec}, {exp});'
        g = 'complex'
    elif verb in 'xX':
        if kind == 'F64':
            line = f'        checkHex(0x{b1[2:]}L, \'{verb}\', {prec}, 64, {exp});'
        else:
            line = f'        checkHex32(0x{b1[2:]}, \'{verb}\', {prec}, {exp});'
        g = 'hex'
    elif verb == 'b':
        if kind == 'F64':
            line = f'        checkBinary(0x{b1[2:]}L, 64, {exp});'
        else:
            line = f'        checkBinary32(0x{b1[2:]}, {exp});'
        g = 'bin'
    elif kind == 'F64' and verb in 'gG' and prec == -1:
        line = f'        check(0x{b1[2:]}L, \'{verb}\', {prec}, 64, {exp});'
        g = 'short64'
    elif kind == 'F32' and verb in 'gG' and prec == -1:
        line = f'        check32(0x{b1[2:]}, \'{verb}\', {prec}, {exp});'
        g = 'short32'
    elif verb in 'gG':
        if kind == 'F64':
            line = f'        check(0x{b1[2:]}L, \'{verb}\', {prec}, 64, {exp});'
        else:
            line = f'        check32(0x{b1[2:]}, \'{verb}\', {prec}, {exp});'
        g = 'gprec'
    else:
        if kind == 'F64':
            line = f'        check(0x{b1[2:]}L, \'{verb}\', {prec}, 64, {exp});'
        else:
            line = f'        check32(0x{b1[2:]}, \'{verb}\', {prec}, {exp});'
        g = 'fixed'
    if line in seen:
        continue
    seen.add(line)
    groups[g].append(line)

def block(name, lines):
    return '\n'.join(lines)

header = '''package graalgo.runtime;

/**
 * Direct unit tests for {@link GoFloatFmt} (REVIEW-2026-11-07-07 S-04 /
 * REVIEW-2026-11-07-22 S-04): the hand-rolled shortest-digits port had no
 * direct tests — only indirect differential-corpus coverage, if a corpus
 * program happened to print a float.
 *
 * <p>Every expected string in this file was computed by gc
 * ({@code strconv.FormatFloat}/{@code strconv.FormatComplex}, go1.27.1,
 * {@code ~/tools/go}) from the IEEE bit pattern of the input — never by
 * running the code under test. Regenerate with
 * {@code /tmp/rev1107-22-s04/genvectors.go} (kept with the lane's staged
 * evidence until quota recovery lands this test) and re-run
 * {@code gen_test.py}; a test that copies the implementation's outputs
 * would be tautological.
 *
 * <p>Coverage: shortest {@code %g} (float64 + float32) including subnormals
 * (5e-324, 9.88e-324, and the float32 1.4e-45 family) and the subnormal
 * tie-break path, the {@code %g} e/f boundary (exp -4/-5 and exp &gt;=
 * eprec), fixed-precision {@code %e}/{@code %f} including half-to-even
 * rounding (0.125 -&gt; "0.12", 2.5 -&gt; "2", 3.5 -&gt; "4"), values that
 * round to zero (1e-300 {@code %f}), rounding carries (999999.5 {@code %f}
 * prec 0 -&gt; "1000000"), specials (NaN, &plusmn;Inf, &plusmn;0) at every
 * verb, hex float {@code %x}/{@code %X} (prec -1..20, incl. the prec-15
 * rounding edge), binary {@code %b}, and complex formatting
 * (complex64/complex128, incl. subnormal and NaN/Inf components).
 */
public final class GoFloatFmtTest {

    private static void assertEquals(String expected, String actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static void check(long bits, char fmt, int prec, int bitSize, String expected) {
        String actual = GoFloatFmt.formatFloat(Double.longBitsToDouble(bits), fmt, prec, bitSize);
        assertEquals(expected, actual,
                "formatFloat(0x" + Long.toHexString(bits) + ", '" + fmt + "', " + prec + ", " + bitSize + ")");
    }

    private static void check32(int bits, char fmt, int prec, String expected) {
        String actual = GoFloatFmt.formatFloat((double) Float.intBitsToFloat(bits), fmt, prec, 32);
        assertEquals(expected, actual,
                "formatFloat32(0x" + Integer.toHexString(bits) + ", '" + fmt + "', " + prec + ")");
    }

    private static void checkHex(long bits, char fmt, int prec, int bitSize, String expected) {
        String actual = GoFloatFmt.formatHex(Double.longBitsToDouble(bits), fmt, prec, bitSize);
        assertEquals(expected, actual,
                "formatHex(0x" + Long.toHexString(bits) + ", '" + fmt + "', " + prec + ", " + bitSize + ")");
    }

    private static void checkHex32(int bits, char fmt, int prec, String expected) {
        String actual = GoFloatFmt.formatHex((double) Float.intBitsToFloat(bits), fmt, prec, 32);
        assertEquals(expected, actual,
                "formatHex32(0x" + Integer.toHexString(bits) + ", '" + fmt + "', " + prec + ")");
    }

    private static void checkBinary(long bits, int bitSize, String expected) {
        String actual = GoFloatFmt.formatBinary(Double.longBitsToDouble(bits), bitSize);
        assertEquals(expected, actual,
                "formatBinary(0x" + Long.toHexString(bits) + ", " + bitSize + ")");
    }

    private static void checkBinary32(int bits, String expected) {
        String actual = GoFloatFmt.formatBinary((double) Float.intBitsToFloat(bits), 32);
        assertEquals(expected, actual,
                "formatBinary32(0x" + Integer.toHexString(bits) + ")");
    }

    private static void checkComplex(long reBits, long imBits, char fmt, int prec, int bitSize, String expected) {
        String actual = GoFloatFmt.formatComplex(
                Double.longBitsToDouble(reBits), Double.longBitsToDouble(imBits), fmt, prec, bitSize);
        assertEquals(expected, actual,
                "formatComplex(0x" + Long.toHexString(reBits) + ", 0x" + Long.toHexString(imBits)
                        + ", '" + fmt + "', " + prec + ", " + bitSize + ")");
    }

    private static void checkComplex32(int reBits, int imBits, char fmt, int prec, String expected) {
        String actual = GoFloatFmt.formatComplex(
                (double) Float.intBitsToFloat(reBits), (double) Float.intBitsToFloat(imBits), fmt, prec, 64);
        assertEquals(expected, actual,
                "formatComplex32(0x" + Integer.toHexString(reBits) + ", 0x" + Integer.toHexString(imBits)
                        + ", '" + fmt + "', " + prec + ")");
    }
'''

methods = [
    ('testShortestFloat64',
     'Shortest %g (prec -1) for float64: normals, the 1e-4/1e-5 and 1e20/1e21\n     * e/f boundaries, max/min-normal, and the whole subnormal family\n     * (bits 1..16, largest subnormal) incl. the 9.88e-324 power-of-ten\n     * carry that exercises the second unit alignment in\n     * shortestSubnormalParts.',
     'short64'),
    ('testShortestFloat32',
     'Shortest %g for float32: normals, max/min-normal, and the subnormal\n     * family down to 1.4e-45 (bits 1..16, largest subnormal).',
     'short32'),
    ('testGPrecision',
     'Fixed-precision %g/%G: the e/f decision at exp -4/-5 and exp &gt;= eprec\n     * (1234567 prec 6 -&gt; e, prec 7 -&gt; f; 100000 prec 5 -&gt; e), prec 0\n     * (one significant digit), prec 17/21 expansions, and rounding of\n     * 1.5/2.5/3.5 to one digit.',
     'gprec'),
    ('testFixedEAndF',
     'Fixed-precision %e/%E/%f: half-to-even rounding on exact binary halves\n     * (0.125 -&gt; "0.12", 0.375 -&gt; "0.38", 0.625 -&gt; "0.62", 2.5 -&gt; "2",\n     * 3.5 -&gt; "4"), values rounding to zero (5e-324, 1e-300 %f), the\n     * 999999.5 carry to "1000000"/"1e+06", subnormals at %e prec 0/2/6,\n     * and specials (&plusmn;0, NaN, &plusmn;Inf) at fixed precision.',
     'fixed'),
    ('testHexFloat',
     'Hex float %x/%X: shortest and fixed precisions -1..20 (the prec-15\n     * rounding edge is the boundary of the port\'s prec &lt; 15 guard),\n     * subnormals (5e-324 -&gt; 0x0.0000000000001p-1022), min normal,\n     * max, &plusmn;0, NaN/Inf, and float32 hex.',
     'hex'),
    ('testBinaryFloat',
     'Binary %b: exact mantissa/exponent rendering for normals,\n     * subnormals, &plusmn;0, NaN/Inf, float64 and float32.',
     'bin'),
    ('testComplex',
     'Complex: (re+imi) layout, sign insertion for negative imaginary\n     * parts, -0 components, subnormal components, NaN/Inf components,\n     * and complex64 (float32 parts) vs complex128.',
     'complex'),
]

parts = [header]
for name, doc, g in methods:
    parts.append(f'\n    /**\n     * {doc}\n     */\n    public void {name}() {{\n')
    parts.append('\n'.join(groups[g]))
    parts.append('\n    }\n')
parts.append('}\n')
open('/tmp/rev1107-22-s04/GoFloatFmtTest.java', 'w').write(''.join(parts))
total = sum(len(v) for v in groups.values())
print(f"wrote GoFloatFmtTest.java: {total} checks", file=sys.stderr)
for g, lines in groups.items():
    print(f"  {g}: {len(lines)}", file=sys.stderr)
