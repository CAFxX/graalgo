#!/usr/bin/env python3
"""
Synth-twin completeness gate (2026-11-03, P1-2 lane).

`check_no_bridges.py` (diff-based) already fails on any genuinely NEW
synth_*.go file. This gate closes the three holes that check leaves open,
and it needs no build and no diff source, so it can run before `make all`:

1. ORPHAN MIRROR: a p3frontend synth mirror whose v1 package is no longer
   a live registry entry (e.g. a v1 conversion that forgot the mirror and
   its drift tests) fails. Mirrors retire WITH their v1 package, in the
   same change.
2. MISSING DRIFT TEST: every p3frontend synth file must have at least one
   *_test.go in p3frontend that references BOTH a mirror function defined
   in that file AND the v1 source filename it mirrors. A mirror whose
   drift test was deleted or weakened to a self-comparison fails here.
3. REGISTRY GROWTH / UNREGISTERED FILE: the active syntheticSources entry
   set must be a SUBSET of the baseline below (post-lane: 128 entries),
   and every non-test synth_*.go file in internal/frontend must be a
   registry/fragment file, the registry machinery itself, the dedicated
   unsafe stub, or one of the grandfathered dead files. Re-registering a
   package (e.g. a retired one, reusing an existing file — invisible to
   the diff-based new-file check) or adding an unregistered synth file
   fails. Growing the baseline is an explicit, reviewed edit to this
   script, never a silent side effect of a feature change.

The gate NEVER passes vacuously: an unparseable registry, a missing
directory, or an empty mirror/test set fails loudly.

Grandfathered dead files (P1-2 inventory 2026-11-03; flagged for deletion,
never for re-registration):
- synth_filepath.go: path/filepath already resolves from disk; the file's
  syntheticFilepathSource is referenced by no code.
- synth_math_rand.go: minimal math/rand stub superseded by the generated
  full port (synth_mathrand.go); kept on disk for reference only.
Removing either file keeps this gate green; adding any other unregistered
file does not.
"""

import os
import re
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FE = os.path.join(REPO, "internal", "frontend")
P3 = os.path.join(FE, "p3frontend")

# Baseline: active syntheticSources entries AFTER the P1-2 lane retired
# "cmp" and "structs" (130 entries at base bfd20e87 minus those two).
# Generated from the P1-2 inventory; keep sorted.
BASELINE_ENTRIES = {
    "archive/tar",
    "archive/zip",
    "bufio",
    "bytes",
    "compress/bzip2",
    "compress/flate",
    "compress/gzip",
    "compress/lzw",
    "compress/zlib",
    "container/heap",
    "container/list",
    "container/ring",
    "context",
    "crypto",
    "crypto/aes",
    "crypto/cipher",
    "crypto/des",
    "crypto/dsa",
    "crypto/ecdh",
    "crypto/ecdsa",
    "crypto/ed25519",
    "crypto/elliptic",
    "crypto/hkdf",
    "crypto/hmac",
    "crypto/internal/boring",
    "crypto/internal/boring/bbig",
    "crypto/internal/constanttime",
    "crypto/internal/fips140",
    "crypto/internal/fips140/alias",
    "crypto/internal/fips140/bigmod",
    "crypto/internal/fips140/check",
    "crypto/internal/fips140/drbg",
    "crypto/internal/fips140/ecdh",
    "crypto/internal/fips140/ecdsa",
    "crypto/internal/fips140/ed25519",
    "crypto/internal/fips140/edwards25519",
    "crypto/internal/fips140/edwards25519/field",
    "crypto/internal/fips140/hkdf",
    "crypto/internal/fips140/hmac",
    "crypto/internal/fips140/nistec",
    "crypto/internal/fips140/nistec/fiat",
    "crypto/internal/fips140/pbkdf2",
    "crypto/internal/fips140/rsa",
    "crypto/internal/fips140/sha256",
    "crypto/internal/fips140/sha3",
    "crypto/internal/fips140/sha512",
    "crypto/internal/fips140/subtle",
    "crypto/internal/fips140cache",
    "crypto/internal/fips140deps/byteorder",
    "crypto/internal/fips140hash",
    "crypto/internal/fips140only",
    "crypto/internal/rand",
    "crypto/md5",
    "crypto/pbkdf2",
    "crypto/rand",
    "crypto/rc4",
    "crypto/rsa",
    "crypto/sha1",
    "crypto/sha256",
    "crypto/sha3",
    "crypto/sha512",
    "crypto/subtle",
    "crypto/tls",
    "crypto/x509",
    "crypto/x509/pkix",
    "encoding",
    "encoding/ascii85",
    "encoding/asn1",
    "encoding/base32",
    "encoding/base64",
    "encoding/binary",
    "encoding/csv",
    "encoding/gob",
    "encoding/hex",
    "encoding/json",
    "encoding/pem",
    "encoding/xml",
    "errors",
    "flag",
    "golang.org/x/crypto/cryptobyte",
    "golang.org/x/crypto/cryptobyte/asn1",
    "hash",
    "hash/adler32",
    "hash/crc32",
    "hash/crc64",
    "hash/fnv",
    "hash/maphash",
    "internal/bytealg",
    "internal/byteorder",
    "internal/graalgo/fmtstate",
    "internal/nettrace",
    "internal/poll",
    "internal/singleflight",
    "io",
    "io/fs",
    "iter",
    "maps",
    "math",
    "math/big",
    "math/bits",
    "math/cmplx",
    "math/rand",
    "math/rand/v2",
    "mime",
    "mime/multipart",
    "mime/quotedprintable",
    "net",
    "net/netip",
    "net/textproto",
    "net/url",
    "os",
    "path",
    "reflect_m7",
    "regexp/syntax",
    "slices",
    "sort",
    "strconv",
    "strings",
    "syscall",
    "testing",
    "testing/iotest",
    "testing/quick",
    "text/scanner",
    "time",
    "unicode",
    "unicode/utf16",
    "unicode/utf8",
    "unique",
}

# p3frontend mirror file -> v1 import paths it mirrors (from the mirror
# functions and headers; 12 files, 21 v1 packages).
MIRRORS = {
    "synth_bytes.go": ["bytes"],
    "synth_enc_core.go": ["encoding/base32", "encoding/base64", "encoding/hex",
                          "encoding/binary", "encoding/ascii85", "encoding/pem",
                          "encoding/asn1", "encoding"],
    "synth_enc_csv_gob.go": ["encoding/csv", "encoding/gob"],
    "synth_errors.go": ["errors"],
    "synth_io.go": ["io"],
    "synth_json.go": ["encoding/json"],
    "synth_math.go": ["math"],
    "synth_sort.go": ["sort"],
    "synth_strconv.go": ["strconv"],
    "synth_strings.go": ["strings"],
    "synth_time_context.go": ["time", "context"],
    "synth_xml.go": ["encoding/xml"],
}

# v1 import path -> v1 synth file backing it (for the drift-test v1-file
# reference check). Multi-entry files share one filename.
V1_FILE = {
    "bytes": "synth_bytes.go",
    "encoding/base32": "synth_base32.go",
    "encoding/base64": "synth_base64.go",
    "encoding/hex": "synth_hex.go",
    "encoding/binary": "synth_binary.go",
    "encoding/ascii85": "synth_ascii85.go",
    "encoding/pem": "synth_pem.go",
    "encoding/asn1": "synth_asn1.go",
    "encoding": "synth_encoding.go",
    "encoding/csv": "synth_enc_csv_gob.go",
    "encoding/gob": "synth_enc_csv_gob.go",
    "errors": "synth_errors.go",
    "io": "synth_io.go",
    "encoding/json": "synth_json.go",
    "math": "synth_math.go",
    "sort": "synth_sort.go",
    "strconv": "synth_strconv.go",
    "strings": "synth_strings.go",
    "time": "synth_time_context.go",
    "context": "synth_time_context.go",
    "encoding/xml": "synth_xml.go",
}

GRANDFATHERED_DEAD = {"synth_filepath.go", "synth_math_rand.go"}
NON_PACKAGE_FILES = {"synth_machinery.go", "synth_unsafe.go"}


def fail(errors):
    for e in errors:
        print(f"check_synth_twins: FAIL: {e}", file=sys.stderr)
    print(f"check_synth_twins: {len(errors)} violation(s)", file=sys.stderr)
    sys.exit(1)


def read(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def main():
    errors = []
    if not os.path.isdir(FE) or not os.path.isdir(P3):
        fail(["frontend directories missing: "
              f"{FE} / {P3} (run from the repo root)"])

    # --- active registry entries (comment lines excluded) ---
    mach = read(os.path.join(FE, "synth_machinery.go"))
    m = re.search(r"var syntheticSources = map\[string\]string\{(.*?)\n\}", mach, re.S)
    if not m:
        fail(["syntheticSources map not found in synth_machinery.go"])
    active = {}
    for line in m.group(1).split("\n"):
        s = line.strip()
        if s.startswith("//"):
            continue
        mm = re.match(r'"([^"]+)":\s*(\w+)', s)
        if mm:
            active[mm.group(1)] = mm.group(2)
    if not active:
        fail(["syntheticSources parsed to zero entries (refusing vacuous pass)"])

    # --- check 3a: registry growth ---
    for p in sorted(active):
        if p not in BASELINE_ENTRIES:
            errors.append(
                f"NEW SYNTHETIC REGISTRY ENTRY: {p!r} — not in the baseline; "
                "retiring a package is the only direction allowed, and a "
                "re-registration needs an explicit, reviewed baseline "
                "change to this script")

    # --- registry -> file mapping, incl. fragments concatenated in the map ---
    code_lines = [l for l in m.group(1).split("\n") if not l.strip().startswith("//")]
    map_refs = set(re.findall(r"synthetic\w+Source", "\n".join(code_lines)))
    var2file = {}
    v1_files = sorted(f for f in os.listdir(FE)
                      if f.startswith("synth_") and f.endswith(".go")
                      and not f.endswith("_test.go"))
    for fn in v1_files:
        src = read(os.path.join(FE, fn))
        for var in re.findall(r"^(?:const|var)\s+(synthetic\w+Source)\s*=", src, re.M):
            var2file[var] = fn
    registry_files = set()
    for path, var in active.items():
        if var not in var2file:
            # Concatenated fragments (e.g. cipher + cipher_gcm) are caught
            # by the map_refs pass below; a truly missing var is an error.
            if var not in map_refs:
                errors.append(f"REGISTRY VAR WITHOUT FILE: {path!r} -> {var}")
        else:
            registry_files.add(var2file[var])
    for fn in v1_files:
        src = read(os.path.join(FE, fn))
        vars_here = re.findall(r"^(?:const|var)\s+(synthetic\w+Source)\s*=", src, re.M)
        if any(v in map_refs for v in vars_here):
            registry_files.add(fn)

    # --- check 3b: unregistered files ---
    for fn in v1_files:
        if fn in registry_files or fn in NON_PACKAGE_FILES or fn in GRANDFATHERED_DEAD:
            continue
        errors.append(f"UNREGISTERED SYNTH FILE: internal/frontend/{fn} — "
                      "backs no registry entry (new bridge or dead file; "
                      "registering it needs a reviewed baseline change, "
                      "deleting it is always allowed)")

    # --- p3frontend mirrors ---
    p3_files = sorted(f for f in os.listdir(P3)
                      if f.startswith("synth_") and f.endswith(".go"))
    if not p3_files:
        fail(["no p3frontend synth files found (refusing vacuous pass)"])
    test_srcs = {}
    for fn in sorted(os.listdir(P3)):
        if fn.endswith("_test.go"):
            test_srcs[fn] = read(os.path.join(P3, fn))
    if not test_srcs:
        fail(["no p3frontend test files found (refusing vacuous pass)"])
    for fn in p3_files:
        if fn not in MIRRORS:
            errors.append(f"UNMAPPED P3 MIRROR: p3frontend/{fn} — add its v1 "
                          "packages to MIRRORS in this script (reviewed) or "
                          "remove the mirror")
            continue
        src = read(os.path.join(P3, fn))
        mirrors = re.findall(r"^func (synthetic\w+Package\w*)\(", src, re.M)
        if not mirrors:
            errors.append(f"MIRROR WITHOUT FUNCTIONS: p3frontend/{fn}")
            continue
        # check 1: orphan mirror
        for p in MIRRORS[fn]:
            if p not in active:
                errors.append(f"ORPHAN MIRROR: p3frontend/{fn} mirrors {p!r}, "
                              "which is no longer a live registry entry — "
                              "retire the mirror and its drift tests with "
                              "the v1 package, in the same change")
        # check 2: drift test referencing mirror fn + v1 file
        v1_names = {V1_FILE[p] for p in MIRRORS[fn] if p in V1_FILE}
        ok = False
        for tfn, tsrc in test_srcs.items():
            if any(mr in tsrc for mr in mirrors) and any(v in tsrc for v in v1_names):
                ok = True
                break
        if not ok:
            errors.append(f"MIRROR WITHOUT DRIFT TEST: p3frontend/{fn} "
                          f"(mirrors {mirrors}) has no *_test.go referencing "
                          f"both a mirror function and a v1 file {sorted(v1_names)}")
    # mirrors must cover exactly the mapped set (a deleted manifest entry
    # for a live mirror is caught above as UNMAPPED; extra manifest entries
    # for deleted mirrors are caught here)
    for fn in sorted(MIRRORS):
        if fn not in p3_files:
            errors.append(f"STALE MANIFEST ENTRY: {fn} in MIRRORS but no such "
                          "p3frontend file exists")

    if errors:
        fail(errors)
    print(f"check_synth_twins: OK ({len(active)} registry entries, "
          f"{len(p3_files)} p3frontend mirrors with drift tests, "
          f"{len(v1_files)} v1 synth files accounted for)")


if __name__ == "__main__":
    main()
