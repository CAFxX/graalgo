#!/usr/bin/env python3
"""Stage-2: hand-edit layer. Applies the mrM8 multi-result surgery and other
semantic (but behavior-preserving) rewrites to the stage-1 files. Every rule
is explicit below; the script is the audit trail.
"""
import os
import re
import sys

STAGE1 = os.path.expanduser(
    "~/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-tmp/crypto/stage")
STAGE2 = os.path.expanduser(
    "~/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-tmp/crypto/stage2")


def load(flat):
    with open(os.path.join(STAGE1, flat + ".go")) as f:
        return f.read()


def save(flat, text):
    os.makedirs(STAGE2, exist_ok=True)
    text = re.sub(r'\n{3,}', '\n\n', text)
    with open(os.path.join(STAGE2, flat + ".go"), "w") as f:
        f.write(text)
    print("stage2:", flat, len(text.split("\n")), "lines")


def find_func(text, start_pat):
    """Return (start, end) of the func whose declaration matches start_pat
    (regex), brace-matched to its closing brace."""
    m = re.search(start_pat, text)
    if not m:
        raise SystemExit("func not found: %s" % start_pat)
    k = text.index("{", m.start())
    depth = 0
    p = k
    while True:
        if text[p] == "{":
            depth += 1
        elif text[p] == "}":
            depth -= 1
            if depth == 0:
                return m.start(), p + 1
        p += 1


def mr8_digest(t, pkg, recv):
    """Write -> writeSingle + stub; Marshal/Append -> *Single + stubs;
    Clone -> cloneSinglePair + stub."""
    # ---- Write ----
    s, e = find_func(t, r'func \(%s\) Write\(p \[\]byte\) \(nn int, err error\) \{' % re.escape(recv))
    body = t[s:e]
    assert "nn = len(p)" in body, pkg
    body = body.replace("func (%s) Write(p []byte) (nn int, err error) {" % recv,
                        "func (%s) writeSingle(p []byte) int {" % recv)
    body = body.replace("\tnn = len(p)\n", "")
    body = body.replace("d.len += uint64(nn)", "d.len += uint64(len(p))")
    body = body.rstrip()
    assert body.endswith("\treturn\n}"), pkg
    body = body[: -len("\treturn\n}")] + "\treturn len(p)\n}"
    stub = ("\n// Write implements hash.Hash. The v1 frontend cannot emit or call\n"
            "// multi-result functions (WS D): this keeps the exact gc signature\n"
            "// for type-checking with a fail-fast body. Real callers go through\n"
            "// the mrM8 desugar to hash.__hashWrite (see\n"
            "// m8-staging/crypto/03-mrdesugar.patch), which delegates to the\n"
            "// single-result writeSingle above. Re-test trigger: WS D lands.\n"
            "func (%s) Write(p []byte) (nn int, err error) {\n"
            "\tpanic(\"crypto/%s: Write: multi-result call needs the mrM8 desugar (re-test: WS D)\")\n"
            "}\n" % (recv, pkg))
    t = t[:s] + body + stub + t[e:]

    # ---- MarshalBinary ----
    recvvar = recv.split()[0]
    s, e = find_func(t, r'func \(%s\) MarshalBinary\(\) \(\[\]byte, error\) \{' % re.escape(recv))
    t = t[:s] + ("func (%s) marshalSingle() []byte {\n"
                 "\treturn %s.appendSingle(make([]byte, 0, marshaledSize))\n"
                 "}\n"
                 "// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;\n"
                 "// real logic is marshalSingle, served via the mrM8 desugar to\n"
                 "// encoding.__binaryMarshal). Re-test trigger: WS D lands.\n"
                 "func (%s) MarshalBinary() ([]byte, error) {\n"
                 "\tpanic(\"crypto/%s: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)\")\n"
                 "}\n" % (recv, recvvar, recv, pkg)) + t[e:]

    # ---- AppendBinary ----
    s, e = find_func(t, r'func \(%s\) AppendBinary\(b \[\]byte\) \(\[\]byte, error\) \{' % re.escape(recv))
    body = t[s:e]
    body = body.replace(
        "func (%s) AppendBinary(b []byte) ([]byte, error) {" % recv,
        "func (%s) appendSingle(b []byte) []byte {" % recv)
    body = body.rstrip()
    assert body.endswith("\treturn b, nil\n}"), pkg
    body = body[: -len("\treturn b, nil\n}")] + "\treturn b\n}"
    stub = ("\n// AppendBinary implements encoding.BinaryAppender (fail-fast stub;\n"
            "// real logic is appendSingle, served via the mrM8 desugar to\n"
            "// encoding.__binaryAppend). Re-test trigger: WS D lands.\n"
            "func (%s) AppendBinary(b []byte) ([]byte, error) {\n"
            "\tpanic(\"crypto/%s: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)\")\n"
            "}\n" % (recv, pkg))
    t = t[:s] + body + stub + t[e:]

    # ---- Clone ----
    s, e = find_func(t, r'func \(%s\) Clone\(\) \(hash\.Cloner, error\) \{' % re.escape(recv))
    t = t[:s] + ("func (%s) cloneSinglePair() hash.ClonePair {\n"
                 "\tr := *%s\n"
                 "\treturn hash.ClonePair{C: &r}\n"
                 "}\n"
                 "// Clone implements hash.Cloner (fail-fast stub; real logic is\n"
                 "// cloneSinglePair, served via the mrM8 desugar to hash.__hashClone).\n"
                 "// Re-test trigger: WS D lands.\n"
                 "func (%s) Clone() (hash.Cloner, error) {\n"
                 "\tpanic(\"crypto/%s: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)\")\n"
                 "}\n" % (recv, recvvar, recv, pkg)) + t[e:]
    return t


def registration(t, pkg, reg_calls):
    """Replace `func init() {...}` with a package-var registration.
    Constructors returning a concrete type (e.g. sha3.New224() *SHA3) are
    wrapped in `func() hash.Hash { return ... }` closures, exactly like the
    stdlib (func() *T is not assignable to func() hash.Hash)."""
    s, e = find_func(t, r'func init\(\) \{')
    wrapped = []
    for c in reg_calls:
        # c is like "crypto.RegisterHash(crypto.SHA3_224, New224)"
        m = __import__("re").match(r'(crypto\.RegisterHash\(crypto\.\w+, )([^)]+)\)', c)
        if m:
            wrapped.append("%sfunc() hash.Hash { return %s() })" % (m.group(1), m.group(2)))
        else:
            wrapped.append(c)
    body = "\n".join("\t" + c for c in wrapped)
    cap = pkg[0].upper() + pkg[1:]
    new = ("// registered wires this package into crypto.RegisterHash without\n"
           "// init() (synthetic packages have no init support; package var\n"
           "// initializers run at synthetic-package import).\n"
           "var %sRegistered = register%s()\n\n"
           "func register%s() bool {\n%s\n\treturn true\n}\n"
           % (pkg, cap, cap, body))
    return t[:s] + new + t[e:]


STUB_NOTE = """
// %s implements %s. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
""".strip()


def stub_for(recv, name, sig, pkg):
    return (STUB_NOTE % (name, sig) +
            "\nfunc (%s) %s {\n\tpanic(\"crypto/%s: %s: multi-result call needs the mrM8 desugar (re-test: WS D)\")\n}\n"
            % (recv, name, pkg, name.split("(")[0]))


def post_sha3(t):
    pkg = "sha3"
    # ---------- inner digest ----------
    # delete the multi Write (superseded by writeSingle + public stub)
    t = re.sub(r'func \(d \*digest\) Write\(p \[\]byte\) \(n int, err error\) \{ return d\.write\(p\) \}\n', '', t)
    # writeSingle body: drop `n = len(p)`, return len(p)
    s, e = find_func(t, r'func \(d \*digest\) writeSingle\(p \[\]byte\) int \{')
    body = t[s:e].replace("\tn = len(p)\n\n", "")
    assert body.rstrip().endswith("\treturn\n}"), "wS"
    body = body.rstrip()[: -len("\treturn\n}")] + "\treturn len(p)\n}"
    t = t[:s] + body + t[e:]
    # readSingle body
    s, e = find_func(t, r'func \(d \*digest\) readSingle\(out \[\]byte\) int \{')
    body = t[s:e].replace("\tn = len(out)\n\n", "")
    assert body.rstrip().endswith("\treturn\n}"), "rS"
    body = body.rstrip()[: -len("\treturn\n}")] + "\treturn len(out)\n}"
    t = t[:s] + body + t[e:]
    # sumGeneric -> sumSingle, use readSingle
    t = t.replace("func (d *digest) sumGeneric(b []byte) []byte {",
                  "func (d *digest) sumSingle(b []byte) []byte {")
    t = t.replace("\tdup.read(hash)", "\tdup.readSingle(hash)")
    # writeSingle: replace subtle.XORBytes(d.a[d.n:d.rate], d.a[d.n:d.rate], p)
    # with a direct loop. The XOR is exact-overlap by construction (dst == x);
    # the loop is semantically identical and avoids the overlap check, which
    # needs unsafe (fail-fast in subtle). P is user input, disjoint from d.a
    # by construction (internal state). Behavior-preserving.
    t = t.replace("\t\tx := subtle.XORBytes(d.a[d.n:d.rate], d.a[d.n:d.rate], p)\n\t\td.n += x\n\t\tp = p[x:]",
                  "\t\tx := len(p)\n\t\tif x > d.rate-d.n {\n\t\t\tx = d.rate - d.n\n\t\t}\n\t\tfor i := 0; i < x; i++ {\n\t\t\td.a[d.n+i] ^= p[i]\n\t\t}\n\t\td.n += x\n\t\tp = p[x:]")
    # Sum: call sumSingle (RecordApproved already stripped by apply_common)
    t = re.sub(r'func \(d \*digest\) Sum\(b \[\]byte\) \[\]byte \{\n\treturn d\.sum\(b\)\n\}',
               'func (d *digest) Sum(b []byte) []byte {\n\treturn d.sumSingle(b)\n}', t)
    # inner Clone: keep (single-result *digest), used by sumSingle
    # inner Marshal/Append -> marshalSingle/appendSingle (no public stubs needed;
    # the public SHA3/SHAKE expose their own)
    s, e = find_func(t, r'func \(d \*digest\) MarshalBinary\(\) \(\[\]byte, error\) \{')
    t = t[:s] + ("func (d *digest) marshalSingle() []byte {\n"
                 "\treturn d.appendSingle(make([]byte, 0, marshaledSize))\n}\n") + t[e:]
    s, e = find_func(t, r'func \(d \*digest\) AppendBinary\(b \[\]byte\) \(\[\]byte, error\) \{')
    body = t[s:e].replace(
        "func (d *digest) AppendBinary(b []byte) ([]byte, error) {",
        "func (d *digest) appendSingle(b []byte) []byte {")
    assert body.rstrip().endswith("\treturn b, nil\n}"), "aS"
    body = body.rstrip()[: -len("\treturn b, nil\n}")] + "\treturn b\n}"
    t = t[:s] + body + t[e:]

    # ---------- inner shake ----------
    t = re.sub(r'func \(s \*shake\) Write\(p \[\]byte\) \(n int, err error\) \{ return s\.d\.Write\(p\) \}\n',
               'func (s *shake) writeSingle(p []byte) int { return s.d.writeSingle(p) }\n', t)
    s, e = find_func(t, r'func \(s \*shake\) Read\(out \[\]byte\) \(n int, err error\) \{')
    t = t[:s] + ("func (s *shake) readSingle(out []byte) int {\n"
                 "\treturn s.d.readSingle(out)\n}\n") + t[e:]
    # shake Marshal/Append -> singles delegating to d.appendSingle
    s, e = find_func(t, r'func \(s \*shake\) MarshalBinary\(\) \(\[\]byte, error\) \{')
    t = t[:s] + ("func (s *shake) marshalSingle() []byte {\n"
                 "\treturn s.appendSingle(make([]byte, 0, marshaledSize+len(s.initBlock)))\n}\n") + t[e:]
    s, e = find_func(t, r'func \(s \*shake\) AppendBinary\(b \[\]byte\) \(\[\]byte, error\) \{')
    t = t[:s] + ("func (s *shake) appendSingle(b []byte) []byte {\n"
                 "\tb = s.d.appendSingle(b)\n"
                 "\tb = append(b, s.initBlock...)\n"
                 "\treturn b\n}\n") + t[e:]
    # shake UnmarshalBinary: inner d.UnmarshalBinary is single-result; keep,
    # delegating form unchanged (it already calls s.d.UnmarshalBinary).
    # shake Clone: single-result *shake; keep as-is.
    # bytepadWrite: c.Write -> c.writeSingle
    # newCShake128/256 (2-arg): fix inner calls to the renamed 3-arg helpers
    t = t.replace("return newCShake128(c, N, S)", "return newCShake128Inner(c, N, S)")
    t = t.replace("return newCShake256(c, N, S)", "return newCShake256Inner(c, N, S)")
    t = t.replace("*NewShake128()", "*newShake128()")
    t = t.replace("*NewShake256()", "*newShake256()")
    # bytepadWrite uses statement-position c.Write (results discarded)
    t = t.replace("\tc.Write(rateEnc)\n", "\tc.writeSingle(rateEnc)\n")
    t = t.replace("\tc.Write(data)\n", "\tc.writeSingle(data)\n")
    t = t.replace("\tc.Write(make([]byte, padlen, maxRate))",
                  "\tc.writeSingle(make([]byte, padlen, maxRate))")
    return t

def post_sha3_public(t):
    pkg = "sha3"
    # ---- struct fields: s sha3.Digest -> d digest ; s sha3.SHAKE -> d shake ----
    t = t.replace("type SHA3 struct {\n\ts sha3.Digest\n}",
                  "type SHA3 struct {\n\td digest\n}")
    t = t.replace("type SHAKE struct {\n\ts sha3.SHAKE\n}",
                  "type SHAKE struct {\n\td shake\n}")
    # ---- drop the linkname func (directive already stripped by mechanical) ----
    s, e = find_func(t, r'func fips140hash_sha3Unwrap\(s \*SHA3\)')
    t = t[:s] + t[e:]
    # ---- constructors ----
    for n in ["224", "256", "384", "512"]:
        t = t.replace("return &SHA3{*sha3.New%s()}" % n,
                      "return &SHA3{*newDigest%s()}" % n)
    t = t.replace("return &SHAKE{*sha3.NewShake128()}", "return &SHAKE{*newShake128()}")
    t = t.replace("return &SHAKE{*sha3.NewShake256()}", "return &SHAKE{*newShake256()}")
    t = t.replace("return &SHAKE{*sha3.NewCShake128(N, S)}", "return &SHAKE{*newCShake128(N, S)}")
    t = t.replace("return &SHAKE{*sha3.NewCShake256(N, S)}", "return &SHAKE{*newCShake256(N, S)}")
    # ---- init() ----
    t = t.replace("if s.s.Size() == 0 {", "if s.d.Size() == 0 {")
    # ---- field accesses s.s. -> s.d. (whole-word) ----
    t = re.sub(r'\bs\.s\.', 's.d.', t)

    # ---- SHA3 methods ----
    s, e = find_func(t, r'func \(s \*SHA3\) Write\(p \[\]byte\) \(n int, err error\) \{')
    t = t[:s] + ("func (s *SHA3) writeSingle(p []byte) int {\n"
                 "\ts.init()\n\treturn s.d.writeSingle(p)\n}\n" +
                 stub_for("s *SHA3", "Write(p []byte) (n int, err error)", "hash.Hash", pkg)) + t[e:]
    def replace_delegate(t, name, sig, delegate, ret=True):
        s, e = find_func(t, r'func \(s \*SHA3\) ' + name + r'\(')
        new = "func (s *SHA3) %s {\n\ts.init()\n\t%s%s\n}\n" % (
            sig, "return " if ret else "", delegate)
        return t[:s] + new + t[e:]
    t = replace_delegate(t, "Sum", "Sum(b []byte) []byte", "s.d.Sum(b)")
    t = replace_delegate(t, "Reset", "Reset()", "s.d.Reset()", ret=False)
    t = replace_delegate(t, "Size", "Size() int", "s.d.Size()")
    t = replace_delegate(t, "BlockSize", "BlockSize() int", "s.d.BlockSize()")
    t = replace_delegate(t, "UnmarshalBinary", "UnmarshalBinary(data []byte) error",
                         "s.d.UnmarshalBinary(data)")
    # SHA3 marshal/append singles + stubs
    s, e = find_func(t, r'func \(s \*SHA3\) MarshalBinary\(')
    t = t[:s] + ("func (s *SHA3) marshalSingle() []byte {\n"
                 "\ts.init()\n\treturn s.d.marshalSingle()\n}\n" +
                 stub_for("s *SHA3", "MarshalBinary() ([]byte, error)", "encoding.BinaryMarshaler", pkg)) + t[e:]
    s, e = find_func(t, r'func \(s \*SHA3\) AppendBinary\(')
    t = t[:s] + ("func (s *SHA3) appendSingle(p []byte) []byte {\n"
                 "\ts.init()\n\treturn s.d.appendSingle(p)\n}\n" +
                 stub_for("s *SHA3", "AppendBinary(p []byte) ([]byte, error)", "encoding.BinaryAppender", pkg)) + t[e:]
    # SHA3 Clone -> cloneSinglePair + stub
    s, e = find_func(t, r'func \(d \*SHA3\) Clone\(\) \(hash\.Cloner, error\) \{')
    t = t[:s] + ("func (d *SHA3) cloneSinglePair() hash.ClonePair {\n"
                 "\tr := *d\n\treturn hash.ClonePair{C: &r}\n}\n" +
                 stub_for("d *SHA3", "Clone() (hash.Cloner, error)", "hash.Cloner", pkg)) + t[e:]
    return t

def post_sha3_public_shake(t):
    pkg = "sha3"
    # ---- public SHAKE methods ----
    s, e = find_func(t, r'func \(s \*SHAKE\) Write\(p \[\]byte\) \(n int, err error\) \{')
    t = t[:s] + ("func (s *SHAKE) writeSingle(p []byte) int {\n"
                 "\ts.init()\n\treturn s.d.writeSingle(p)\n}\n" +
                 stub_for("s *SHAKE", "Write(p []byte) (n int, err error)", "hash.Hash", pkg)) + t[e:]
    s, e = find_func(t, r'func \(s \*SHAKE\) Read\(p \[\]byte\) \(n int, err error\) \{')
    t = t[:s] + ("func (s *SHAKE) readSingle(p []byte) int {\n"
                 "\ts.init()\n\treturn s.d.readSingle(p)\n}\n" +
                 stub_for("s *SHAKE", "Read(p []byte) (n int, err error)", "hash.XOF", pkg)) + t[e:]

    def rep(t, name, sig, delegate, ret=True):
        s, e = find_func(t, r'func \(s \*SHAKE\) ' + name + r'\(')
        new = "func (s *SHAKE) %s {\n\ts.init()\n\t%s%s\n}\n" % (
            sig, "return " if ret else "", delegate)
        return t[:s] + new + t[e:]
    t = rep(t, "Reset", "Reset()", "s.d.Reset()", ret=False)
    t = rep(t, "BlockSize", "BlockSize() int", "s.d.BlockSize()")
    t = rep(t, "UnmarshalBinary", "UnmarshalBinary(data []byte) error",
            "s.d.UnmarshalBinary(data)")

    s, e = find_func(t, r'func \(s \*SHAKE\) MarshalBinary\(')
    t = t[:s] + ("func (s *SHAKE) marshalSingle() []byte {\n"
                 "\ts.init()\n\treturn s.d.marshalSingle()\n}\n" +
                 stub_for("s *SHAKE", "MarshalBinary() ([]byte, error)", "encoding.BinaryMarshaler", pkg)) + t[e:]
    s, e = find_func(t, r'func \(s \*SHAKE\) AppendBinary\(')
    t = t[:s] + ("func (s *SHAKE) appendSingle(p []byte) []byte {\n"
                 "\ts.init()\n\treturn s.d.appendSingle(p)\n}\n" +
                 stub_for("s *SHAKE", "AppendBinary(p []byte) ([]byte, error)", "encoding.BinaryAppender", pkg)) + t[e:]

    # ---- Sum224/256/384/512, sumSHAKE128/256: statement-position Write/Read ----
    t = re.sub(r'h := sha3\.(New\d+)\(\)', lambda m: "h := newDigest%s()" % m.group(1)[3:], t)
    t = t.replace("\th.Write(data)\n", "\th.writeSingle(data)\n")
    t = t.replace("h := sha3.NewShake128()", "h := newShake128()")
    t = t.replace("h := sha3.NewShake256()", "h := newShake256()")
    t = t.replace("\th.Read(out)\n", "\th.readSingle(out)\n")
    return t


def post_aes_stage2(t):
    pkg = "aes"
    # NewCipher -> __aesNewCipher helper + fail-fast stub
    t += '''
// newCipherPair is the single-result form of NewCipher's results.
type newCipherPair struct {
    block cipher.Block
    err   error
}

// __aesNewCipher serves aes.NewCipher through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func __aesNewCipher(key []byte) newCipherPair {
    k := len(key)
    switch k {
    default:
        return newCipherPair{nil, KeySizeError(k)}
    case 16, 24, 32:
        break
    }
    b, err := newAesBlock(key)
    if err != nil {
        return newCipherPair{nil, err}
    }
    return newCipherPair{b, nil}
}
'''
    # Replace NewCipher with stub
    s, e = find_func(t, r'func NewCipher\(key \[\]byte\) \(cipher\.Block, error\) \{')
    t = t[:s] + ('''// NewCipher creates and returns a new [cipher.Block].
// The key argument must be the AES key, either 16, 24, or 32 bytes to select
// AES-128, AES-192, or AES-256.
//
// P3-M8: fail-fast stub (exact gc signature for type-checking). Real callers
// go through the mrM8 desugar to __aesNewCipher. Re-test trigger: WS D lands.
func NewCipher(key []byte) (cipher.Block, error) {
\tpanic("crypto/aes: NewCipher: multi-result call needs the mrM8 desugar (re-test: WS D)")
}
''') + t[e:]
    return t


def post_des_stage2(t):
    # NewCipher/NewTripleDESCipher -> helpers + stubs
    t += '''
// newCipherPair is the single-result form of NewCipher's results.
type newCipherPair struct {
    block cipher.Block
    err   error
}

// __desNewCipher serves des.NewCipher through the mrM8 desugar.
func __desNewCipher(key []byte) newCipherPair {
    if len(key) != 8 {
        return newCipherPair{nil, KeySizeError(len(key))}
    }
    c := new(desCipher)
    c.generateSubkeys(key)
    return newCipherPair{c, nil}
}

// __desNewTripleDESCipher serves des.NewTripleDESCipher through the mrM8 desugar.
func __desNewTripleDESCipher(key []byte) newCipherPair {
    if len(key) != 24 {
        return newCipherPair{nil, KeySizeError(len(key))}
    }
    c := new(tripleDESCipher)
    c.cipher1.generateSubkeys(key[:8])
    c.cipher2.generateSubkeys(key[8:16])
    c.cipher3.generateSubkeys(key[16:])
    return newCipherPair{c, nil}
}
'''
    for name, helper in [("NewCipher", "__desNewCipher"),
                         ("NewTripleDESCipher", "__desNewTripleDESCipher")]:
        s, e = find_func(t, r'func %s\(key \[\]byte\) \(cipher\.Block, error\) \{' % name)
        t = t[:s] + ('''// %s creates and returns a new [cipher.Block].
// P3-M8: fail-fast stub (exact gc signature). Real callers go through the
// mrM8 desugar to %s. Re-test trigger: WS D lands.
func %s(key []byte) (cipher.Block, error) {
\tpanic("crypto/des: %s: multi-result call needs the mrM8 desugar (re-test: WS D)")
}
''' % (name, helper, name, name)) + t[e:]
    # alias -> inexactOverlap (already in build_des? check)
    if "alias.InexactOverlap" in t:
        t = t.replace("alias.InexactOverlap", "inexactOverlap")
        if "func inexactOverlap" not in t:
            t += '''
func inexactOverlap(x, y []byte) bool {
\tif len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {
\t\treturn false
\t}
\tpanic("crypto/des: inexactOverlap: cannot verify disjointness without unsafe (re-test: unsafe draft)")
}
'''
    return t

def post_rc4_stage2(t):
    t += '''
// newCipherPair is the single-result form of NewCipher's results.
type newCipherPair struct {
    cipher *Cipher
    err    error
}

// __rc4NewCipher serves rc4.NewCipher through the mrM8 desugar.
func __rc4NewCipher(key []byte) newCipherPair {
    k := len(key)
    if k < 1 || k > 256 {
        return newCipherPair{nil, KeySizeError(k)}
    }
    var c Cipher
    for i := 0; i < 256; i++ {
        c.s[i] = uint32(i)
    }
    var j uint8 = 0
    for i := 0; i < 256; i++ {
        j += uint8(c.s[i]) + key[i%k]
        c.s[i], c.s[j] = c.s[j], c.s[i]
    }
    return newCipherPair{&c, nil}
}
'''
    s, e = find_func(t, r'func NewCipher\(key \[\]byte\) \(\*Cipher, error\) \{')
    t = t[:s] + ('''// NewCipher creates and returns a new [Cipher].
// P3-M8: fail-fast stub (exact gc signature). Real callers go through the
// mrM8 desugar to __rc4NewCipher. Re-test trigger: WS D lands.
func NewCipher(key []byte) (*Cipher, error) {
\tpanic("crypto/rc4: NewCipher: multi-result call needs the mrM8 desugar (re-test: WS D)")
}
''') + t[e:]
    return t

if __name__ == "__main__":
    # ---- md5 ----
    t = load("crypto_md5")
    t = re.sub(r'\nfunc block\(dig \*digest, p \[\]byte\) \{\n\tblockGeneric\(dig, p\)\n\}\n', '\n', t)
    t = mr8_digest(t, "md5", "d *digest")
    t = registration(t, "md5", ["crypto.RegisterHash(crypto.MD5, New)"])
    save("crypto_md5", t)

    for flat, pkg, recv, regs in [
        ("crypto_sha1", "sha1", "d *digest",
         ["crypto.RegisterHash(crypto.SHA1, New)"]),
        ("crypto_sha256", "sha256", "d *digest",
         ["crypto.RegisterHash(crypto.SHA224, New224)",
          "crypto.RegisterHash(crypto.SHA256, New)"]),
        ("crypto_sha512", "sha512", "d *digest",
         ["crypto.RegisterHash(crypto.SHA384, New384)",
          "crypto.RegisterHash(crypto.SHA512, New)",
          "crypto.RegisterHash(crypto.SHA512_224, New512_224)",
          "crypto.RegisterHash(crypto.SHA512_256, New512_256)"]),
    ]:
        u = load(flat)
        u = mr8_digest(u, pkg, recv)
        u = registration(u, pkg, regs)
        save(flat, u)

    # aes
    u = load("crypto_aes")
    u = post_aes_stage2(u)
    save("crypto_aes", u)

    # sha3
    u = load("crypto_sha3")
    u = post_sha3(u)
    u = post_sha3_public(u)
    u = post_sha3_public_shake(u)
    u = registration(u, "sha3", [
        "crypto.RegisterHash(crypto.SHA3_224, New224)",
        "crypto.RegisterHash(crypto.SHA3_256, New256)",
        "crypto.RegisterHash(crypto.SHA3_384, New384)",
        "crypto.RegisterHash(crypto.SHA3_512, New512)",
    ])
    save("crypto_sha3", u)
    print("stage-2 done (sha1/sha256/sha512/sha3)")

    # des/rc4
    u = load("crypto_des")
    u = post_des_stage2(u)
    save("crypto_des", u)
    u = load("crypto_rc4")
    u = post_rc4_stage2(u)
    save("crypto_rc4", u)
