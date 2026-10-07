#!/usr/bin/env python3
"""Materialize the m8synth validation tree: stage2 -> GOPATH/src/m8synth/...,
with the gc-shim applied (fail-fast stubs delegate to __-helpers so the full
public API works under gc for differential validation)."""
import os
import re
import shutil

HERE = os.path.expanduser(
    "~/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-tmp/crypto")
STAGE2 = os.path.join(HERE, "stage2")
GOPATH = os.path.join(HERE, "gopath")
SYN = os.path.join(GOPATH, "src")

# stage import path -> m8synth import path
REMAP = {
    '"hash"': '"m8synth/hash"',
    '"crypto"': '"m8synth/crypto"',
    '"crypto/md5"': '"m8synth/crypto/md5"',
    '"crypto/sha1"': '"m8synth/crypto/sha1"',
    '"crypto/sha256"': '"m8synth/crypto/sha256"',
    '"crypto/sha512"': '"m8synth/crypto/sha512"',
    '"crypto/sha3"': '"m8synth/crypto/sha3"',
    '"crypto/subtle"': '"m8synth/crypto/subtle"',
    '"crypto/hmac"': '"m8synth/crypto/hmac"',
    '"crypto/aes"': '"m8synth/crypto/aes"',
    '"crypto/des"': '"m8synth/crypto/des"',
    '"crypto/rc4"': '"m8synth/crypto/rc4"',
    '"crypto/rand"': '"m8synth/crypto/rand"',
}

# (flat, importpath, is stage2?)
PACKAGES = [
    ("hash", "m8synth/hash", False),
    ("crypto", "m8synth/crypto", False),
    ("crypto_md5", "m8synth/crypto/md5", True),
    ("crypto_sha1", "m8synth/crypto/sha1", True),
    ("crypto_sha256", "m8synth/crypto/sha256", True),
    ("crypto_sha512", "m8synth/crypto/sha512", True),
    ("crypto_sha3", "m8synth/crypto/sha3", True),
    ("crypto_subtle", "m8synth/crypto/subtle", True),
    ("crypto_hmac", "m8synth/crypto/hmac", True),
    ("crypto_aes", "m8synth/crypto/aes", True),
    ("crypto_des", "m8synth/crypto/des", True),
    ("crypto_rc4", "m8synth/crypto/rc4", True),
    ("crypto_cipher_modes", "m8synth/crypto/cipher", True),
    ("crypto_cipher_gcm", "m8synth/crypto/cipher", True),
    ("crypto_rand", "m8synth/crypto/rand", True),
]

# gc-shim: (flat, func-signature-regex, replacement body)
# Replaces the fail-fast stub body so the public API is fully functional
# under gc. Delegates to the LOCAL single-result method (same package, so
# no visibility issue). This exercises the real logic; the __-helpers
# (which the mrM8 desugar calls in graalgo) are trivial delegates verified
# by inspection.
SHIMS = [
    ("crypto_md5", r'func \(d \*digest\) Write\(p \[\]byte\) \(nn int, err error\)',
     '{\n\treturn d.WriteSingle(p), nil\n}'),
    ("crypto_md5", r'func \(d \*digest\) MarshalBinary\(\) \(\[\]byte, error\)',
     '{\n\treturn d.MarshalSingle(), nil\n}'),
    ("crypto_md5", r'func \(d \*digest\) AppendBinary\(b \[\]byte\) \(\[\]byte, error\)',
     '{\n\treturn d.AppendSingle(b), nil\n}'),
    ("crypto_md5", r'func \(d \*digest\) Clone\(\) \(hash\.Cloner, error\)',
     '{\n\tpr := d.CloneSinglePair()\n\treturn pr.C, pr.Err\n}'),
]
# same shims for sha1/sha256/sha512
for _flat in ["crypto_sha1", "crypto_sha256", "crypto_sha512"]:
    for _pat, _body in [s[1:] for s in SHIMS]:
        SHIMS.append((_flat, _pat, _body))
# sha3 shims
SHIMS += [
    ("crypto_sha3", r'func \(s \*SHA3\) Write\(p \[\]byte\) \(n int, err error\)',
     '{\n\treturn s.WriteSingle(p), nil\n}'),
    ("crypto_sha3", r'func \(s \*SHA3\) MarshalBinary\(\) \(\[\]byte, error\)',
     '{\n\treturn s.MarshalSingle(), nil\n}'),
    ("crypto_sha3", r'func \(s \*SHAKE\) Write\(p \[\]byte\) \(n int, err error\)',
     '{\n\treturn s.WriteSingle(p), nil\n}'),
    ("crypto_sha3", r'func \(s \*SHAKE\) Read\(p \[\]byte\) \(n int, err error\)',
     '{\n\treturn s.ReadSingle(p), nil\n}'),
    ("crypto_sha3", r'func \(s \*SHAKE\) MarshalBinary\(\) \(\[\]byte, error\)',
     '{\n\treturn s.MarshalSingle(), nil\n}'),
    ("crypto_sha3", r'func \(d \*SHA3\) Clone\(\) \(hash\.Cloner, error\)',
     '{\n\tpr := d.CloneSinglePair()\n\treturn pr.C, pr.Err\n}'),
]
# fix: SHA3 AppendBinary shim (param named p)
SHIMS.append(
    ("crypto_sha3", r'func \(s \*SHA3\) AppendBinary\(p \[\]byte\) \(\[\]byte, error\)',
     '{\n\treturn s.AppendSingle(p), nil\n}'))
SHIMS.append(
    ("crypto_sha3", r'func \(s \*SHAKE\) AppendBinary\(p \[\]byte\) \(\[\]byte, error\)',
     '{\n\treturn s.AppendSingle(p), nil\n}'))
SHIMS += [
    ("crypto_hmac", r'func \(h \*hmac\) Write\(p \[\]byte\) \(nn int, err error\)',
     '{\n\treturn h.WriteSingle(p), nil\n}'),
    ("crypto_hmac", r'func \(h \*hmac\) Clone\(\) \(hash\.Cloner, error\)',
     '{\n\tpr := h.CloneSinglePair()\n\treturn pr.C, pr.Err\n}'),
    ("crypto_aes", r'func NewCipher\(key \[\]byte\) \(cipher\.Block, error\)',
     '{\n\tpr := __aesNewCipher(key)\n\treturn pr.block, pr.err\n}'),
    ("crypto_des", r'func NewCipher\(key \[\]byte\) \(cipher\.Block, error\)',
     '{\n\tpr := __desNewCipher(key)\n\treturn pr.block, pr.err\n}'),
    ("crypto_des", r'func NewTripleDESCipher\(key \[\]byte\) \(cipher\.Block, error\)',
     '{\n\tpr := __desNewTripleDESCipher(key)\n\treturn pr.block, pr.err\n}'),
    ("crypto_rc4", r'func NewCipher\(key \[\]byte\) \(\*Cipher, error\)',
     '{\n\tpr := __rc4NewCipher(key)\n\treturn pr.cipher, pr.err\n}'),
    ("crypto_cipher_gcm", r'func NewGCM\(cipher Block\) \(AEAD, error\)',
     '{\n\tpr := __newGCM(cipher)\n\treturn pr.aead, pr.err\n}'),
    ("crypto_cipher_gcm", r'func NewGCMWithNonceSize\(cipher Block, size int\) \(AEAD, error\)',
     '{\n\tpr := __newGCMWithNonceSize(cipher, size)\n\treturn pr.aead, pr.err\n}'),
    ("crypto_cipher_gcm", r'func NewGCMWithTagSize\(cipher Block, tagSize int\) \(AEAD, error\)',
     '{\n\tpr := __newGCMWithTagSize(cipher, tagSize)\n\treturn pr.aead, pr.err\n}'),
    ("crypto_cipher_gcm", r'func \(g \*gcmFallback\) Open\(dst, nonce, ciphertext, additionalData \[\]byte\) \(\[\]byte, error\)',
     '{\n\tpr := g.OpenSingle(dst, nonce, ciphertext, additionalData)\n\treturn pr.out, pr.err\n}'),
    ("crypto_cipher_modes", r'func \(r StreamReader\) Read\(dst \[\]byte\) \(n int, err error\)',
     '{\n\tpr := __streamReaderRead(r, dst)\n\treturn pr.n, pr.err\n}'),
    ("crypto_cipher_modes", r'func \(w StreamWriter\) Write\(src \[\]byte\) \(n int, err error\)',
     '{\n\tpr := __streamWriterWrite(w, src)\n\treturn pr.n, pr.err\n}'),
]

def brace_end(text, open_idx):
    depth = 0
    for p in range(open_idx, len(text)):
        if text[p] == "{":
            depth += 1
        elif text[p] == "}":
            depth -= 1
            if depth == 0:
                return p + 1
    raise ValueError("unbalanced")


def apply_shim(text, pat, body):
    m = re.search(pat, text)
    if not m:
        raise SystemExit("shim pattern not found: %s" % pat)
    k = text.index("{", m.start())
    e = brace_end(text, k)
    return text[:k] + body + text[e:]



def inject_unsafe_overlap(t):
    """Validation-only: replace the fail-fast inexactOverlap with a working
    unsafe-based implementation (mirrors crypto/internal/fips140/alias).
    The synthetic source keeps the fail-fast; this exercises the XORBytes
    logic under gc."""
    start = t.index("func inexactOverlap(x, y []byte) bool {")
    # brace-match to end
    depth = 0
    p = t.index("{", start)
    while True:
        if t[p] == "{":
            depth += 1
        elif t[p] == "}":
            depth -= 1
            if depth == 0:
                break
        p += 1
    end = p + 1
    # Also remove existing anyOverlap if present (we'll add our own)
    if "func anyOverlap(x, y []byte) bool {" in t[end:]:
        astart = t.index("func anyOverlap(x, y []byte) bool {", end)
        depth = 0
        p = t.index("{", astart)
        while True:
            if t[p] == "{":
                depth += 1
            elif t[p] == "}":
                depth -= 1
                if depth == 0:
                    break
            p += 1
        t = t[:astart] + t[p+1:]
    new_fn = "func inexactOverlap(x, y []byte) bool {\n"
    new_fn += "\tif len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {\n"
    new_fn += "\t\treturn false\n\t}\n"
    new_fn += "\treturn anyOverlap(x, y)\n}\n\n"
    new_fn += "func anyOverlap(x, y []byte) bool {\n"
    new_fn += "\treturn len(x) > 0 && len(y) > 0 &&\n"
    new_fn += "\t\tuintptr(unsafe.Pointer(&x[0])) <= uintptr(unsafe.Pointer(&y[len(y)-1])) &&\n"
    new_fn += "\t\tuintptr(unsafe.Pointer(&y[0])) <= uintptr(unsafe.Pointer(&x[len(x)-1]))\n}\n"
    t = t[:start] + new_fn + t[end:]
    # Add unsafe import after package clause (whatever the package name)
    t = re.sub(r'(?m)^package \w+\n', lambda m: m.group(0) + '\nimport "unsafe"\n', t, count=1)
    return t

if __name__ == "__main__":
    if os.path.exists(SYN):
        shutil.rmtree(SYN)
    for flat, importpath, is2 in PACKAGES:
        src = os.path.join(STAGE2 if is2 else os.path.join(HERE, "stage"),
                           flat + ".go")
        # hash.go/crypto.go were copied to stage2; prefer stage2
        alt = os.path.join(STAGE2, flat + ".go")
        if os.path.exists(alt):
            src = alt
        t = open(src).read()
        for f, pat, body in SHIMS:
            if f == flat:
                t = apply_shim(t, pat, body)
        if flat in ("crypto_subtle", "crypto_des", "crypto_aes", "crypto_rc4", "crypto_cipher_modes"):
            t = inject_unsafe_overlap(t)
        for a, b in REMAP.items():
            t = t.replace(a, b)
        # package clause: flat "crypto_md5" -> package md5
        pkg = importpath.split("/")[-1]
        t = re.sub(r'(?m)^package\s+\w+', 'package ' + pkg, t, count=1)
        d = os.path.join(SYN, importpath)
        os.makedirs(d, exist_ok=True)
        # Multiple files per package: use flat name if not the main file
        fname = pkg + ".go"
        if os.path.exists(os.path.join(d, fname)):
            fname = flat + ".go"
        with open(os.path.join(d, fname), "w") as f:
            f.write(t)
        print("materialized", importpath)
