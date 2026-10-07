#!/usr/bin/env python3
"""Mechanical stage-1 transform: vendored go1.27.1 crypto sources -> synthetic
stage files (plain Go, real import paths). Applies only behavior-preserving,
mechanical rewrites; package-specific semantic edits are separate functions
below. Output: m8-tmp/crypto/stage/<flat>.go
"""
import os
import re
import sys

STD = os.path.expanduser("~/tools/go-1.27.1/src")
STAGE = os.path.expanduser(
    "~/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-tmp/crypto/stage")

# ---------------------------------------------------------------------------
# byteorder helper header (injected once per stage file that used byteorder).
# Names are bo-prefixed to avoid collisions with ported identifiers.
# ---------------------------------------------------------------------------
BO_HEADER = '''
// -- synthetic byteorder helpers (inlined from internal/byteorder) --
func boLEUint32(b []byte) uint32 {
\treturn uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func boLEPutUint32(b []byte, v uint32) {
\tb[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24)
}
func boLEUint64(b []byte) uint64 {
\treturn uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
\t\tuint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
func boLEPutUint64(b []byte, v uint64) {
\tb[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24)
\tb[4] = byte(v >> 32); b[5] = byte(v >> 40); b[6] = byte(v >> 48); b[7] = byte(v >> 56)
}
func boBEUint32(b []byte) uint32 {
\treturn uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24
}
func boBEPutUint32(b []byte, v uint32) {
\tb[0] = byte(v >> 24); b[1] = byte(v >> 16); b[2] = byte(v >> 8); b[3] = byte(v)
}
func boBEUint64(b []byte) uint64 {
\treturn uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |
\t\tuint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56
}
func boBEPutUint64(b []byte, v uint64) {
\tb[0] = byte(v >> 56); b[1] = byte(v >> 48); b[2] = byte(v >> 40); b[3] = byte(v >> 32)
\tb[4] = byte(v >> 24); b[5] = byte(v >> 16); b[6] = byte(v >> 8); b[7] = byte(v)
}
func boBEAppendUint32(b []byte, v uint32) []byte {
\treturn append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func boBEAppendUint64(b []byte, v uint64) []byte {
\treturn append(b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
\t\tbyte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func boLEAppendUint32(b []byte, v uint32) []byte {
\treturn append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
func boLEAppendUint64(b []byte, v uint64) []byte {
\treturn append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
\t\tbyte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}
// -- end synthetic byteorder helpers --
'''

BITS_HEADER = '''
// -- synthetic math/bits helpers (inlined; the used operations only) --
// NB: the shift count is a SIGNED int, matching math/bits.RotateLeftN:
// callers pass negative counts (e.g. bits.RotateLeft32(x, -17)).
func rotl32(x uint32, n int) uint32 { return x<<uint(n&31) | x>>uint((32-n)&31) }
func rotl64(x uint64, n int) uint64 { return x<<uint(n&63) | x>>uint((64-n)&63) }
func add64(x, y, carry uint64) (sum, carryOut uint64) {
\tsum1 := x + y
\tc1 := uint64(0)
\tif sum1 < x {
\t\tc1 = 1
\t}
\tsum = sum1 + carry
\tc2 := uint64(0)
\tif sum < sum1 {
\t\tc2 = 1
\t}
\treturn sum, c1 | c2
}
func sub64(x, y, borrow uint64) (diff, borrowOut uint64) {
\tdiff1 := x - y
\tb1 := uint64(0)
\tif diff1 > x {
\t\tb1 = 1
\t}
\tdiff = diff1 - borrow
\tb2 := uint64(0)
\tif diff > diff1 {
\t\tb2 = 1
\t}
\treturn diff, b1 | b2
}
func len64(x uint64) int {
\tn := 0
\tfor x != 0 {
\t\tn++
\t\tx >>= 1
\t}
\treturn n
}
// -- end synthetic math/bits helpers --
'''

# Imports to drop entirely (internal-only or handled by rewrite).
DROP_IMPORTS = {
    "crypto/internal/boring",
    "crypto/internal/fips140only",
    "crypto/internal/fips140",
    "crypto/internal/fips140hash",
    "crypto/internal/fips140deps/byteorder",
    "internal/byteorder",
    "internal/cpu",
    "crypto/internal/fips140deps/cpu",
    "internal/runtime/sys",
    "crypto/internal/rand",
    "crypto/internal/fips140/drbg",
    "crypto/internal/fips140/hmac",
    "crypto/internal/fips140/alias",
    "crypto/internal/constanttime",
    "crypto/internal/fips140/subtle",
    "math/bits",
    "unsafe",
    "runtime",
    "sync",
}

# Per-package import rewrites: old -> new (None = drop).
IMPORT_REMAP = {
    # cipher: the AES fast-path dispatch would need crypto/aes, but
    # crypto/aes imports crypto/cipher (NewCipher returns cipher.Block):
    # an import cycle. Resolution (documented): synthetic cipher routes
    # AES blocks through the generic mode implementations, which are
    # algorithm-identical to the purego fast paths.
    "crypto/internal/fips140/aes": None,
    # gcm internals: only GHASH is ported (as local code); the GCM type
    # itself is not (all GCM routes via newGCMFallback).
    "crypto/internal/fips140/aes/gcm": None,
    # sha256/sha512/sha3 wrappers: fips140 digest is merged into the
    # synthetic package itself.
    "crypto/internal/fips140/sha256": None,
    "crypto/internal/fips140/sha512": None,
    "crypto/internal/fips140/sha3": None,
}


def transform_imports(text, pkg):
    """Rewrite the import block(s): drop internal-only, remap per table."""
    out = []
    in_import = False
    for line in text.split("\n"):
        s = line.strip()
        if s == "import (":
            in_import = True
            out.append(line)
            continue
        if in_import and s == ")":
            in_import = False
            out.append(line)
            continue
        if in_import:
            m = re.match(r'\s*(?:[\w.]+\s+)?"([^"]+)"', line)
            if m:
                ip = m.group(1)
                if ip in DROP_IMPORTS:
                    continue
                if ip in IMPORT_REMAP:
                    new = IMPORT_REMAP[ip]
                    if new is None:
                        continue
                    line = line.replace('"%s"' % ip, '"%s"' % new)
            out.append(line)
            continue
        # single-line imports
        m = re.match(r'import\s+(?:[\w.]+\s+)?"([^"]+)"', s)
        if m:
            ip = m.group(1)
            if ip in DROP_IMPORTS or IMPORT_REMAP.get(ip) is None and ip in IMPORT_REMAP:
                continue
        out.append(line)
    return "\n".join(out)


def delete_boring(text):
    # boring.Unreachable() is a documented no-op in the standard toolchain.
    text = re.sub(r'(?m)^\s*boring\.Unreachable\(\)\n', '', text)
    # if boring.Enabled { ... } -> delete the whole if statement.
    # Handled with a brace-matching pass for the known shapes.
    return delete_if_blocks(text, "boring.Enabled")


def delete_if_blocks(text, cond):
    """Delete `if <cond> { ... }` blocks (brace-matched)."""
    out = []
    i = 0
    pat = re.compile(r'if\s+' + re.escape(cond) + r'\s*\{')
    while True:
        m = pat.search(text, i)
        if not m:
            out.append(text[i:])
            break
        # check that the match starts a statement (prev non-space char is \n or { or })
        out.append(text[i:m.start()])
        depth = 1
        j = m.end()
        while depth > 0 and j < len(text):
            if text[j] == '{':
                depth += 1
            elif text[j] == '}':
                depth -= 1
            j += 1
        i = j
        # also swallow one trailing newline to avoid blank-line buildup
        if i < len(text) and text[i] == '\n':
            i += 1
    return "".join(out)


def delete_fips140only(text):
    return delete_if_blocks(text, "fips140only.Enforced()")


def delete_fips140_records(text):
    text = re.sub(r'(?m)^\s*fips140\.RecordApproved\(\)\n', '', text)
    text = re.sub(r'(?m)^\s*fips140\.RecordNonApproved\(\)\n', '', text)
    return text


def mechanical(text):
    # build tags / generate directives / noinline
    text = re.sub(r'(?m)^//go:build .*\n', '', text)
    text = re.sub(r'(?m)^//go:generate .*\n', '', text)
    text = re.sub(r'(?m)^//go:noinline\n', '', text)
    text = re.sub(r'(?m)^//go:linkname .*\n', '', text)
    # byteorder
    text = text.replace("byteorder.", "bo")
    # bits
    text = text.replace("bits.RotateLeft32(", "rotl32(")
    text = text.replace("bits.RotateLeft64(", "rotl64(")
    text = re.sub(r'bits\.Add64\(', 'add64(', text)
    text = re.sub(r'bits\.Sub64\(', 'sub64(', text)
    text = re.sub(r'bits\.Len64\(', 'len64(', text)
    return text


def fix_clear(text):
    """clear(x) -> for i := range x { x[i] = 0 } (v1 has no clear builtin)."""
    def rep(m):
        ind, expr = m.group(1), m.group(2).strip()
        return ("%sfor _m8ci := range %s {\n%s\t%s[_m8ci] = 0\n%s}" %
                (ind, expr, ind, expr, ind))
    return re.sub(r'(?m)^(\s*)clear\(([^)]+)\)\s*$', rep, text)


def fix_range_int(text):
    """for i := range N (int constant) -> classic for loop."""
    return re.sub(r'for (\w+) := range (\d+)\b', r'for \1 := 0; \1 < \2; \1++', text)


def apply_common(text):
    text = delete_boring(text)
    text = delete_fips140only(text)
    text = delete_fips140_records(text)
    text = fix_clear(text)
    text = fix_range_int(text)
    return text

# ---------------------------------------------------------------------------
# Package assembly
# ---------------------------------------------------------------------------

def read(src):
    with open(os.path.join(STD, src)) as f:
        return f.read()

def strip_pkg_clause(text):
    """Remove the `package x` clause (we emit our own)."""
    return re.sub(r'(?m)^package\s+\w+\s*\n', '', text, count=1)

def clean_import_block(text):
    """Drop import blocks that became empty after filtering."""
    # remove `import ()` empty blocks
    text = re.sub(r'(?m)^import \(\s*\)\s*\n', '', text)
    text = re.sub(r'(?m)^import \(\n\)\n', '', text)
    return text

def assemble(pkg_name, files, post=None, need_bo=False, need_bits=False):
    """files: list of vendored paths. Returns the stage source text."""
    parts = []
    for src in files:
        t = read(src)
        t = strip_pkg_clause(t)
        t = transform_imports(t, pkg_name)
        t = mechanical(t)
        t = apply_common(t)
        t = clean_import_block(t)
        parts.append(t)
    body = "\n".join(parts)
    specs, body = merge_imports(body)
    # collapse 3+ blank lines
    body = re.sub(r'\n{3,}', '\n\n', body)
    helpers = ""
    if need_bo:
        helpers += BO_HEADER + "\n"
    if need_bits:
        helpers += BITS_HEADER + "\n"
    if helpers:
        # body has no package clause and no imports at this point (both are
        # prepended below), so helpers go at the very start of body, which
        # lands them right after the import block.
        body = helpers + body
    src = "package %s\n\n" % pkg_name
    if specs:
        src += "import (\n" + "\n".join("\t" + s for s in specs) + "\n)\n\n"
    src += body
    if post:
        src = post(src)
    return src


def merge_imports(text):
    """Collect import specs from all import blocks / single imports in text.
    Returns (specs, text_without_imports)."""
    specs = []
    def grab_block(m):
        for line in m.group(0).split("\n"):
            s = line.strip()
            if not s or s in ("import (", ")"):
                continue
            if s not in specs:
                specs.append(s)
        return ""
    text = re.sub(r'(?m)^import \(.*?^\).*?\n', grab_block, text,
                  flags=re.MULTILINE | re.DOTALL)
    def grab_single(m):
        s = m.group(1).strip()
        if s not in specs:
            specs.append(s)
        return ""
    text = re.sub(r'(?m)^import\s+((?:[\w.]+\s+)?"[^"]+")\s*$', grab_single, text)
    # drop now-empty import blocks
    text = re.sub(r'(?m)^import \(\s*\)\s*\n', '', text)
    return specs, text


def write_stage(flat, text):
    path = os.path.join(STAGE, flat + ".go")
    with open(path, "w") as f:
        f.write(text)
    print("wrote", path, len(text.split("\n")), "lines")

# ---------------- hash ----------------
def post_hash(t):
    # add ClonePair support type + __-helpers (real Go, single-result)
    t += '''
// ClonePair is the result of cloning a hash. It is exported only so that
// synthetic hash implementations in other packages (crypto/hmac) can
// implement hash cloning with single-result methods: the v1 frontend
// cannot emit or call multi-result functions (WS D), so Clone is served
// through the mrM8 desugar (see m8-staging/crypto/03-mrdesugar.patch)
// and the real logic lives in cloneSinglePair. Not part of the gc hash
// API. Re-test trigger: WS D lands.
type ClonePair struct {
\tC   Cloner
\tErr error
}

type hashWritePair struct {
\tn   int
\terr error
}

type hashReadPair struct {
\tn   int
\terr error
}

// __hashWrite serves h.Write(p) through the mrM8 desugar. The digest's
// public Write has the exact gc signature with a fail-fast body; the
// real logic is the single-result writeSingle, which every synthetic
// digest implements. Custom (non-synthetic) hash.Hash implementations
// fail fast here: there is no v1-expressible way to call their
// multi-result Write (re-test: WS D).
func __hashWrite(h Hash, p []byte) hashWritePair {
\tif w, ok := h.(interface{ writeSingle([]byte) int }); ok {
\t\treturn hashWritePair{w.writeSingle(p), nil}
\t}
\tpanic("hash: Write on non-synthetic hash.Hash implementation (needs WS D; re-test when it lands)")
}

// __hashRead serves x.Read(p) for hash.XOF through the mrM8 desugar.
func __hashRead(x XOF, p []byte) hashReadPair {
\tif r, ok := x.(interface{ readSingle([]byte) int }); ok {
\t\treturn hashReadPair{r.readSingle(p), nil}
\t}
\tpanic("hash: Read on non-synthetic hash.XOF implementation (needs WS D; re-test when it lands)")
}

// __hashClone serves h.Clone() through the mrM8 desugar.
func __hashClone(h Hash) ClonePair {
\tif c, ok := h.(interface{ cloneSinglePair() ClonePair }); ok {
\t\treturn c.cloneSinglePair()
\t}
\tpanic("hash: Clone on non-synthetic hash.Hash implementation (needs WS D; re-test when it lands)")
}
'''
    return t

# ---------------- per-package post-processing ----------------

def post_md5(t):
    # glue: block -> blockGeneric (purego: haveAsm=false)
    t = re.sub(r'const haveAsm = false\n\n', '', t)
    t = t.replace("func block(dig *digest, p []byte) {\n\tblockGeneric(dig, p)\n}",
                  "func block(dig *digest, p []byte) {\n\tblockGeneric(dig, p)\n}")
    # Write's haveAsm branch -> direct blockGeneric
    t = t.replace("""\t\tif d.nx == BlockSize {
\t\t\tif haveAsm {
\t\t\t\tblock(d, d.x[:])
\t\t\t} else {
\t\t\t\tblockGeneric(d, d.x[:])
\t\t\t}
\t\t\td.nx = 0
\t\t}""", """\t\tif d.nx == BlockSize {
\t\t\tblockGeneric(d, d.x[:])
\t\t\td.nx = 0
\t\t}""")
    t = t.replace("""\tif len(p) >= BlockSize {
\t\tn := len(p) &^ (BlockSize - 1)
\t\tif haveAsm {
\t\t\tfor n > maxAsmSize {
\t\t\t\tblock(d, p[:maxAsmSize])
\t\t\t\tp = p[maxAsmSize:]
\t\t\t\tn -= maxAsmSize
\t\t\t}
\t\t\tblock(d, p[:n])
\t\t} else {
\t\t\tblockGeneric(d, p[:n])
\t\t}
\t\tp = p[n:]
\t}""", """\tif len(p) >= BlockSize {
\t\tn := len(p) &^ (BlockSize - 1)
\t\tblockGeneric(d, p[:n])
\t\tp = p[n:]
\t}""")
    t = re.sub(r'(?m)^const maxAsmIters = 1024\n', '', t)
    t = re.sub(r'(?m)^const maxAsmSize = BlockSize \* maxAsmIters // 64KiB\n', '', t)
    return t

def hash_iface_boilerplate(t, pkg, digest_recv="d *digest"):
    """Add writeSingle/marshalSingle/appendSingle/cloneSinglePair + fail-fast
    stubs, replacing the multi-result methods. Applied to md5/sha1/sha256/sha512
    digest types (sha3 handled separately)."""
    # This is done via explicit per-package string surgery below instead.
    return t

def post_sha1(t):
    # glue file: keep block->blockGeneric only
    t = re.sub(r'func block\(dig \*digest, p \[\]byte\) \{\n\tblockGeneric\(dig, p\)\n\}',
              'func block(dig *digest, p []byte) {\n\tblockGeneric(dig, p)\n}', t)
    return t

    # ---------------- sha256/sha512 ----------------
def post_fips_digest(t, pkg):
    # Merge: fips140 Digest -> unexported digest; New/New224 -> newDigest*.
    # Word-boundary replace of the type name (comments included: harmless).
    t = re.sub(r'\bDigest\b', 'digest', t)
    # fips140 constructors -> unexported (wrapper keeps public New/New224)
    t = re.sub(r'func New\(\) \*digest', 'func newDigest() *digest', t)
    return t

def post_sha256_wrap(t):
    t = t.replace("return sha256.New()", "return newDigest()")
    t = t.replace("return sha256.New224()", "return newDigest224()")
    # Sum256/Sum224: statement-position h.Write -> concrete writeSingle
    t = t.replace("""	h := New()
	h.Write(data)
	var sum [Size]byte
	h.Sum(sum[:0])
	return sum""", """	d := newDigest()
	d.writeSingle(data)
	var sum [Size]byte
	d.Sum(sum[:0])
	return sum""")
    t = t.replace("""	h := New224()
	h.Write(data)
	var sum [Size224]byte
	h.Sum(sum[:0])
	return sum""", """	d := newDigest224()
	d.writeSingle(data)
	var sum [Size224]byte
	d.Sum(sum[:0])
	return sum""")
    # fips140 New224 -> newDigest224 (post_fips_digest only handled New)
    t = t.replace("func New224() *digest", "func newDigest224() *digest")
    return t

def post_sha512_wrap(t):
    t = t.replace("return sha512.New()", "return newDigest()")
    t = t.replace("return sha512.New512_224()", "return newDigest512_224()")
    t = t.replace("return sha512.New512_256()", "return newDigest512_256()")
    t = t.replace("return sha512.New384()", "return newDigest384()")
    for name, size in [("Sum512", "Size"), ("Sum384", "Size384"),
                       ("Sum512_224", "Size224"), ("Sum512_256", "Size256")]:
        pass
    t = re.sub(r'h := (New\(\)|New512_224\(\)|New512_256\(\)|New384\(\))\n\th\.Write\(data\)',
               lambda m: {'New()': 'd := newDigest()\n\td.writeSingle(data)',
                           'New512_224()': 'd := newDigest512_224()\n\td.writeSingle(data)',
                           'New512_256()': 'd := newDigest512_256()\n\td.writeSingle(data)',
                           'New384()': 'd := newDigest384()\n\td.writeSingle(data)'}[m.group(1)], t)
    t = t.replace("h.Sum(sum[:0])", "d.Sum(sum[:0])")
    t = t.replace("func New512_224() *digest", "func newDigest512_224() *digest")
    t = t.replace("func New512_256() *digest", "func newDigest512_256() *digest")
    t = t.replace("func New384() *digest", "func newDigest384() *digest")
    return t

    return t


def mech_one(path):
    t = read(path)
    t = strip_pkg_clause(t)
    t = transform_imports(t, "sha3")
    t = mechanical(t)
    t = apply_common(t)
    t = clean_import_block(t)
    return t

def build_sha3():
    # fips140 files: rename Digest->digest, SHAKE->shake BEFORE mechanical
    f_digest = read("crypto/internal/fips140/sha3/sha3.go")
    f_digest = re.sub(r'\bDigest\b', 'digest', f_digest)
    f_hashes = read("crypto/internal/fips140/sha3/hashes.go")
    f_hashes = re.sub(r'\bDigest\b', 'digest', f_hashes)
    f_shake = read("crypto/internal/fips140/sha3/shake.go")
    f_shake = re.sub(r'\bDigest\b', 'digest', f_shake)
    f_shake = re.sub(r'\bSHAKE\b', 'shake', f_shake)
    f_keccak = read("crypto/internal/fips140/sha3/keccakf.go")
    f_keccak = re.sub(r'\bDigest\b', 'digest', f_keccak)
    f_noasm = read("crypto/internal/fips140/sha3/sha3_noasm.go")
    f_noasm = re.sub(r'\bDigest\b', 'digest', f_noasm)

    # keccak always-copy rewrite (replaces the unsafe aliasing fast path)
    f_keccak = re.sub(
        r'func keccakF1600Generic\(da \*\[200\]byte\) \{\n\tvar a \*\[25\]uint64\n\tif cpu\.BigEndian \{\n\t\ta = new\(\[25\]uint64\)\n\t\tfor i := range a \{\n\t\t\ta\[i\] = byteorder\.LEUint64\(da\[i\*8:\]\)\n\t\t\}\n\t\tdefer func\(\) \{\n\t\t\tfor i := range a \{\n\t\t\t\tbyteorder\.LEPutUint64\(da\[i\*8:\], a\[i\]\)\n\t\t\t\}\n\t\t\}\(\)\n\t\} else \{\n\t\ta = \(\*\[25\]uint64\)\(unsafe\.Pointer\(da\)\)\n\t\}\n',
        '''func keccakF1600Generic(da *[200]byte) {
\t// P3-M8: always-copy rewrite of the stdlib's unsafe fast path. The
\t// stdlib aliases da as *[25]uint64 via unsafe.Pointer on little-endian
\t// hosts (perf-only); the v1 frontend has no unsafe, so we always copy
\t// through a [25]uint64 exactly like the stdlib's big-endian path.
\t// Output-identical on all hosts (covered by the SHA-3 KAT differential).
\ta := new([25]uint64)
\tfor i := 0; i < 25; i++ {
\t\ta[i] = byteorder.LEUint64(da[i*8:])
\t}
\tdefer func() {
\t\tfor i := 0; i < 25; i++ {
\t\t\tbyteorder.LEPutUint64(da[i*8:], a[i])
\t\t}
\t}()
''',
        f_keccak, count=1)

    parts = []
    for name, ft in [("sha3.go", f_digest), ("hashes.go", f_hashes),
                     ("shake.go", f_shake), ("keccakf.go", f_keccak),
                     ("sha3_noasm.go", f_noasm)]:
        t = ft
        t = strip_pkg_clause(t)
        t = transform_imports(t, "sha3")
        t = mechanical(t)
        t = apply_common(t)
        t = clean_import_block(t)
        parts.append((name, t))

    # wrapper last (its renames depend on fips140 renames above)
    fw = read("crypto/sha3/sha3.go")
    fw = strip_pkg_clause(fw)
    fw = transform_imports(fw, "sha3")
    fw = mechanical(fw)
    fw = apply_common(fw)
    fw = clean_import_block(fw)
    parts.append(("wrapper", fw))

    body = "\n".join(t for _, t in parts)
    specs, body = merge_imports(body)
    body = re.sub(r'\n{3,}', '\n\n', body)

    # ---- post renames on the merged text ----
    t = body
    # fips140 constructors -> unexported
    t = t.replace("func New224() *digest", "func newDigest224() *digest")
    t = t.replace("func New256() *digest", "func newDigest256() *digest")
    t = t.replace("func New384() *digest", "func newDigest384() *digest")
    t = t.replace("func New512() *digest", "func newDigest512() *digest")
    t = t.replace("func NewLegacyKeccak256() *digest", "func newLegacyKeccak256() *digest")
    t = t.replace("func NewLegacyKeccak512() *digest", "func newLegacyKeccak512() *digest")
    t = t.replace("func NewShake128() *shake", "func newShake128() *shake")
    t = t.replace("func NewShake256() *shake", "func newShake256() *shake")
    t = t.replace("func NewCShake128(N, S []byte) *shake", "func newCShake128(N, S []byte) *shake")
    t = t.replace("func NewCShake256(N, S []byte) *shake", "func newCShake256(N, S []byte) *shake")
    t = t.replace("func newCShake128(c *shake, N, S []byte) *shake",
                  "func newCShake128Inner(c *shake, N, S []byte) *shake")
    t = t.replace("func newCShake256(c *shake, N, S []byte) *shake",
                  "func newCShake256Inner(c *shake, N, S []byte) *shake")
    # inner write/read/sum dispatchers -> single-result names; drop wrappers
    t = t.replace("func (d *digest) write(p []byte) (n int, err error) {\n\treturn d.writeGeneric(p)\n}", "")
    t = t.replace("func (d *digest) read(out []byte) (n int, err error) {\n\treturn d.readGeneric(out)\n}", "")
    t = t.replace("func (d *digest) sum(b []byte) []byte {\n\treturn d.sumGeneric(b)\n}", "")
    t = t.replace("func (d *digest) writeGeneric(p []byte) (n int, err error) {",
                  "func (d *digest) writeSingle(p []byte) int {")
    t = t.replace("func (d *digest) readGeneric(out []byte) (n int, err error) {",
                  "func (d *digest) readSingle(out []byte) int {")
    # writeGeneric/readGeneric bodies end with bare `return`; adapt
    # (writeSingle returns len(p); readSingle returns len(out))
    # -> handled by targeted surgery below
    src = "package sha3\n\n"
    if specs:
        # ensure bytes + subtle + errors + hash + crypto survive
        src += "import (\n" + "\n".join("\t" + s for s in specs) + "\n)\n\n"
    src += BO_HEADER + "\n" + BITS_HEADER + "\n" + t
    return src


def post_aes(t):
    # Merge: wrapper NewCipher -> keep; fips140 New -> newBlockCipher (unexported)
    # to avoid clash with public NewCipher. Actually the wrapper calls aes.New;
    # after merge, rename fips140's New to newAesBlock.
    t = re.sub(r'func New\(key \[\]byte\) \(\*Block, error\)', 'func newAesBlock(key []byte) (*Block, error)', t)
    # wrapper: return aes.New(key) -> return newAesBlock(key)
    t = t.replace("return aes.New(key)", "return newAesBlock(key)")
    # drop boring branch (already handled by delete_if_blocks)
    # Drop fips140 duplicates of BlockSize and KeySizeError (wrapper's win;
    # they're identical). Since both copies are textually identical, drop all
    # and re-add one.
    t = re.sub(r'// BlockSize is the AES block size in bytes.\nconst BlockSize = 16\n\n', '', t)
    t = re.sub(r'type KeySizeError int\n\nfunc \(k KeySizeError\) Error\(\) string \{\n\treturn "crypto/aes: invalid key size " \+ strconv\.Itoa\(int\(k\)\)\n\}\n\n', '', t)
    # Re-add a single KeySizeError (with its Error method) after the imports.
    # (BlockSize is already defined by the wrapper.)
    if 'type KeySizeError' not in t:
        t += '\n// KeySizeError is returned for invalid key sizes.\ntype KeySizeError int\n\nfunc (k KeySizeError) Error() string {\n\treturn "crypto/aes: invalid key size " + strconv.Itoa(int(k))\n}\n'
    # Provide alias helpers (InexactOverlap/AnyOverlap) for the Encrypt/Decrypt
    # overlap checks. P3-M8: &x[0]==&y[0] fast path; else fail fast LOUDLY
    # (needs unsafe; re-test trigger: unsafe draft).
    t += '''
// inexactOverlap reports whether x and y share memory at a non-zero offset.
// P3-M8: fail-fast without unsafe (see crypto/subtle).
func inexactOverlap(x, y []byte) bool {
\tif len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {
\t\treturn false
\t}
\tpanic("crypto/aes: inexactOverlap: cannot verify disjointness without unsafe (re-test: unsafe draft)")
}
'''
    t = t.replace("alias.InexactOverlap", "inexactOverlap")
    t = t.replace("alias.AnyOverlap", "inexactOverlap")  # conservative
    return t


def build_aes():
    parts = []
    for path in ["crypto/aes/aes.go",
                 "crypto/internal/fips140/aes/aes.go",
                 "crypto/internal/fips140/aes/aes_generic.go",
                 "crypto/internal/fips140/aes/aes_noasm.go",
                 "crypto/internal/fips140/aes/const.go"]:
        t = read(path)
        t = strip_pkg_clause(t)
        t = transform_imports(t, "aes")
        t = mechanical(t)
        t = apply_common(t)
        t = clean_import_block(t)
        parts.append(t)
    body = "\n".join(parts)
    specs, body = merge_imports(body)
    body = re.sub(r'\n{3,}', '\n\n', body)
    t = post_aes(body)
    src = "package aes\n\n"
    if specs:
        src += "import (\n" + "\n".join("\t" + s for s in specs) + "\n)\n\n"
    src += BO_HEADER + "\n" + t
    return src


def build_des():
    parts = []
    for path in ["crypto/des/cipher.go", "crypto/des/block.go",
                 "crypto/des/const.go"]:
        t = read(path)
        t = strip_pkg_clause(t)
        t = transform_imports(t, "des")
        t = mechanical(t)
        t = apply_common(t)
        t = clean_import_block(t)
        parts.append(t)
    body = "\n".join(parts)
    specs, body = merge_imports(body)
    body = re.sub(r'\n{3,}', '\n\n', body)
    t = body
    # fips140only: drop (P3-M8 non-FIPS)
    t = re.sub(r'(?m)^\tif fips140only\.Enforced\(\) \{\n\t\treturn nil, errors\.New\("crypto/des: [^"]*"\)\n\t\}\n\n', '', t)
    # sync.Once -> eager init. The stdlib lazily initializes feistelBox via
    # feistelBoxOnce.Do(initFeistelBox); synthetic packages run var
    # initializers at import, so we initialize eagerly and drop the Do calls.
    # initFeistelBox is changed to return bool so `var _ = initFeistelBox()`
    # works as an eager initializer.
    t = t.replace("var feistelBoxOnce sync.Once",
                  "var _ = initFeistelBox()")
    t = t.replace("\n\tfeistelBoxOnce.Do(initFeistelBox)\n", "\n")
    t = t.replace("func initFeistelBox() {", "func initFeistelBox() bool {")
    # Add `return true` at the end of initFeistelBox's body. The func ends
    # with the feistelBox assignment loop; append return before final brace.
    t = re.sub(r'(func initFeistelBox\(\) bool \{.*?\n\})',
               lambda m: m.group(1)[:-2] + "\n\treturn true\n}",
               t, flags=re.DOTALL)
    src = "package des\n\n"
    if specs:
        # drop sync and errors (no longer needed)
        specs = [s for s in specs if s not in ('"sync"', '"errors"')]
        src += "import (\n" + "\n".join("\t" + s for s in specs) + "\n)\n\n"
    src += BO_HEADER + "\n" + t
    return src

# ---------------- rc4 ----------------
def build_rc4():
    t = read("crypto/rc4/rc4.go")
    t = strip_pkg_clause(t)
    t = transform_imports(t, "rc4")
    t = mechanical(t)
    t = apply_common(t)
    t = clean_import_block(t)
    specs, body = merge_imports(t)
    body = re.sub(r'\n{3,}', '\n\n', body)
    # fips140only: drop
    body = re.sub(r'(?m)^\tif fips140only\.Enforced\(\) \{\n\t\treturn nil, errors\.New\("crypto/rc4: [^"]*"\)\n\t\}\n', '', body)
    # alias -> inexactOverlap helper
    body += '''
// inexactOverlap: see crypto/des (P3-M8 fail-fast without unsafe).
func inexactOverlap(x, y []byte) bool {
\tif len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {
\t\treturn false
\t}
\tpanic("crypto/rc4: inexactOverlap: cannot verify disjointness without unsafe (re-test: unsafe draft)")
}
'''
    body = body.replace("alias.InexactOverlap", "inexactOverlap")
    src = "package rc4\n\n"
    if specs:
        specs = [s for s in specs if s != '"errors"']
        src += "import (\n" + "\n".join("\t" + s for s in specs) + "\n)\n\n"
    src += body
    return src

    os.makedirs(STAGE, exist_ok=True)

    # hash: trivial
    write_stage("hash", assemble("hash", ["hash/hash.go"], post=post_hash))

    # crypto root: verbatim (imports hash, io, strconv)
    write_stage("crypto", assemble("crypto", ["crypto/crypto.go"]))

    # md5
    write_stage("crypto_md5", assemble(
        "md5",
        ["crypto/md5/md5.go", "crypto/md5/md5block.go",
         "crypto/md5/md5block_generic.go"],
        post=post_md5, need_bo=True, need_bits=True))

    print("stage-1 done")
    # ---------------- sha1 ----------------
    # sha1
    write_stage("crypto_sha1", assemble(
        "sha1",
        ["crypto/sha1/sha1.go", "crypto/sha1/sha1block.go",
         "crypto/sha1/sha1block_generic.go"],
        post=post_sha1, need_bo=True, need_bits=True))

    # sha256: wrapper + merged fips140 digest
    t256 = assemble(
        "sha256",
        ["crypto/sha256/sha256.go",
         "crypto/internal/fips140/sha256/sha256.go",
         "crypto/internal/fips140/sha256/sha256block.go",
         "crypto/internal/fips140/sha256/sha256block_noasm.go"],
        need_bo=True, need_bits=True)
    t256 = post_fips_digest(t256, "sha256")
    t256 = post_sha256_wrap(t256)
    write_stage("crypto_sha256", t256)

    # sha512: wrapper + merged fips140 digest
    t512 = assemble(
        "sha512",
        ["crypto/sha512/sha512.go",
         "crypto/internal/fips140/sha512/sha512.go",
         "crypto/internal/fips140/sha512/sha512block.go",
         "crypto/internal/fips140/sha512/sha512block_noasm.go"],
        need_bo=True, need_bits=True)
    t512 = post_fips_digest(t512, "sha512")
    t512 = post_sha512_wrap(t512)
    write_stage("crypto_sha512", t512)

    print("stage-1 done (sha1/sha256/sha512)")

    # sha3: custom merge (wrapper + fips140 digest/shake/keccak)
    write_stage("crypto_sha3", build_sha3())
    print("stage-1 done (sha3)")

    # aes: wrapper + fips140/aes (generic only)
    write_stage("crypto_aes", build_aes())
    print("stage-1 done (aes)")

    # des: cipher.go + block.go + const.go (sync.Once -> eager)
    write_stage("crypto_des", build_des())
    # rc4: single file
    write_stage("crypto_rc4", build_rc4())
    print("stage-1 done (des/rc4)")

# ---------------- sha3 (custom merge) ----------------

# ---------------- aes ----------------
# ---------------- des ----------------

# ---------------- cipher ----------------
def build_cipher():
    parts = []
    for path in ["crypto/cipher/cipher.go", "crypto/cipher/cbc.go",
                 "crypto/cipher/ctr.go", "crypto/cipher/cfb.go",
                 "crypto/cipher/ofb.go", "crypto/cipher/io.go"]:
        t = read(path)
        t = strip_pkg_clause(t)
        t = transform_imports(t, "cipher")
        t = mechanical(t)
        t = apply_common(t)
        t = clean_import_block(t)
        parts.append(t)
    body = "\n".join(parts)
    specs, body = merge_imports(body)
    body = re.sub(r'\n{3,}', '\n\n', body)
    t = body
    # Drop AES fast-paths (avoid cipher->aes import cycle; P3-M8 uses generic
    # paths which are algorithm-identical under purego).
    # cbc.go: remove aes.Block type assertions and cbcEncAble
    t = re.sub(r'\tif b, ok := b\.\(\*aes\.Block\); ok \{\n\t\treturn aes\.NewCBCEncrypter\(b, \[16\]byte\(iv\)\)\n\t\}\n', '', t)
    t = re.sub(r'\tif b, ok := b\.\(\*aes\.Block\); ok \{\n\t\treturn aes\.NewCBCDecrypter\(b, \[16\]byte\(iv\)\)\n\t\}\n', '', t)
    t = re.sub(r'\tif _, ok := x\.b\.\(\*aes\.Block\); ok \{\n\t\tpanic\("crypto/cipher: internal error: generic [A-Z]+ used with AES"\)\n\t\}\n', '', t)
    # Remove cbcEncAble/cbcDecAble interfaces and their uses
    t = re.sub(r'// cbcEncAble is an interface[^\n]*\ntype cbcEncAble interface \{\n\tNewCBCEncrypter\(iv \[\]byte\) BlockMode\n\}\n\n', '', t)
    t = re.sub(r'\tif cbc, ok := b\.\(cbcEncAble\); ok \{\n\t\treturn cbc\.NewCBCEncrypter\(iv\)\n\t\}\n', '', t)
    # ctr.go: aes fast path (including the wrapper type)
    t = re.sub(r'\tif block, ok := block\.\(\*aes\.Block\); ok \{\n\t\treturn aesCtrWrapper\{aes\.NewCTR\(block, iv\)\}\n\t\}\n', '', t)
    t = re.sub(r'// aesCtrWrapper hides extra methods from aes\.CTR\.\ntype aesCtrWrapper struct \{\n\tc \*aes\.CTR\n\}\n\nfunc \(x aesCtrWrapper\) XORKeyStream\(dst, src \[\]byte\) \{\n\tx\.c\.XORKeyStream\(dst, src\)\n\}\n\n', '', t)
    # fips140only: drop
    t = re.sub(r'(?m)^\tif fips140only\.Enforced\(\) \{\n\t\tpanic\("[^"]*"\)\n\t\}\n', '', t)
    # alias -> inexactOverlap
    t = t.replace("alias.InexactOverlap", "inexactOverlap")
    t = t.replace("alias.AnyOverlap", "anyOverlap")
    # Add overlap helpers
    t += '''
// inexactOverlap/anyOverlap: P3-M8 fail-fast without unsafe (see crypto/subtle).
func inexactOverlap(x, y []byte) bool {
\tif len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {
\t\treturn false
\t}
\tpanic("crypto/cipher: inexactOverlap: cannot verify disjointness without unsafe (re-test: unsafe draft)")
}
func anyOverlap(x, y []byte) bool {
\tif len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {
\t\treturn &x[0] == &y[0]
\t}
\tpanic("crypto/cipher: anyOverlap: cannot verify without unsafe (re-test: unsafe draft)")
}
'''
    # bytes.Clone -> manual (bytes import may be dropped; keep it simple)
    src = "package cipher\n\n"
    if specs:
        # drop fips140/aes, fips140/alias, fips140only, subtle? (subtle needed for XORBytes)
        specs = [s for s in specs if "fips140" not in s]
        # subtle is needed; keep it (will be m8synth/crypto/subtle in validation)
        src += "import (\n" + "\n".join("\t" + s for s in specs) + "\n)\n\n"
    src += t
    return src

if __name__ == "__main__":
    # cipher: modes (GCM handled separately)
    write_stage("crypto_cipher_modes", build_cipher())
    print("stage-1 done (cipher modes)")
