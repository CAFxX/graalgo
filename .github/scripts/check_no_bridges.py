#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic stubs
- New wire node kinds for stdlib functions
- New synthetic function registrations

Existing bridges are grandfathered (they're in the removal queue — see
docs/arch/bridge-inventory.md). This check only looks at ADDED lines in the diff.

Diff sources, in order:
  1. origin/master...HEAD  (PR / feature branch)
  2. git diff HEAD         (uncommitted working-tree changes)
  3. git diff --cached     (staged changes)
  4. HEAD~1...HEAD         (direct master push: wt.py integrate + push, no PR —
                           checkout lands exactly on origin/master, so 1-3 are
                           all empty; inspect what the push itself introduced)

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for source 4 to resolve.
"""

import re
import subprocess
import sys

# Adjudicated real implementations (NOT bridges): additions the mechanical
# checks below flag by pattern, but an independent reviewer cleared as
# genuine implementations. Narrowly scoped to the exact adjudicated symbols —
# do NOT broaden an entry or add one without a fresh independent review.
# Inventory record: docs/arch/bridge-inventory.md ("Adjudicated non-bridges").
ADJUDICATED_WIRE_NODES = {
    # (file substring, wire node kind)
    # PKG-023 (commit 1d4e7964, 2026-10-16; independent review CLEAN, no
    # veto): "runtimenumgoroutine" is a real implementation following the
    # runtimenumcpu precedent (backend NumGoroutineNode on the scheduler's
    # live-goroutine counter), not a new bridge.
    ("internal/frontend/export.go", "runtimenumgoroutine"),
    # PKG-040 (lane commit 19786941 + coordinator adjudication, 2026-10-16;
    # independent review CLEAN, no veto): "runtimegosched" is a real implementation
    # following the runtimenumgoroutine precedent (backend GoschedNode on
    # the scheduler's voluntary-yield primitive SchedulerCore.yieldCurrent),
    # not a new bridge.
    ("internal/frontend/export.go", "runtimegosched"),
}
ADJUDICATED_REGISTRATIONS = {
    # (file substring, addFunc'd symbol name)
    # PKG-023 (commit 1d4e7964, 2026-10-16; independent review CLEAN, no
    # veto): addFunc("NumGoroutine", ...) declares genuine stdlib surface in
    # the synthetic runtime package; the backend implements it for real.
    ("internal/frontend/export.go", "NumGoroutine"),
    # PKG-040 (lane commit 19786941 + coordinator adjudication, 2026-10-16;
    # independent review CLEAN, no veto): the Gosched overlay declaration (provided by
    # PKG-038 on master; the lane's duplicate declaration was dropped at
    # integration) is genuine stdlib surface — gc 1.27.1 src/runtime/proc.go
    # — and the backend implements it for real via GoschedNode. The
    # registerStmtKind("runtimegosched", ...) P3 registrations are the
    # public register-kind hook (no Phase-1 factory change), not synthetic
    # stdlib logic.
    ("internal/frontend/export.go", "Gosched"),
}

# Complete stdlib import-path list (P1-2 gate hardening, 2026-10-17).
# Enumerated from the Go 1.27.1 GOROOT (~/tools/go/src): every directory
# containing a non-test .go file whose package clause names the directory,
# plus bare top-level roots that only group subpackages (archive, compress,
# ...), plus "internal". When the Go floor moves, regenerate with:
#   find ~/tools/go/src -maxdepth 4 -type d \( -name testdata -o -path "*/cmd*" \
#     -o -path "*/vendor*" \) -prune -o -type d -print
# and keep directories whose package clause matches the directory base name.
STDLIB_PATHS = frozenset([
    "archive",
    "archive/tar",
    "archive/zip",
    "arena",
    "bufio",
    "builtin",
    "bytes",
    "cmp",
    "compress",
    "compress/bzip2",
    "compress/flate",
    "compress/gzip",
    "compress/lzw",
    "compress/zlib",
    "container",
    "container/heap",
    "container/list",
    "container/ring",
    "context",
    "crypto",
    "crypto/aes",
    "crypto/boring",
    "crypto/cipher",
    "crypto/des",
    "crypto/dsa",
    "crypto/ecdh",
    "crypto/ecdsa",
    "crypto/ed25519",
    "crypto/elliptic",
    "crypto/fips140",
    "crypto/hkdf",
    "crypto/hmac",
    "crypto/hpke",
    "crypto/internal/boring",
    "crypto/internal/boring/bbig",
    "crypto/internal/boring/bcache",
    "crypto/internal/boring/sig",
    "crypto/internal/boring/syso",
    "crypto/internal/constanttime",
    "crypto/internal/cryptotest",
    "crypto/internal/cryptotest/wycheproof",
    "crypto/internal/cryptotest/x509limbo",
    "crypto/internal/entropy",
    "crypto/internal/fips140",
    "crypto/internal/fips140/aes",
    "crypto/internal/fips140/alias",
    "crypto/internal/fips140/bigmod",
    "crypto/internal/fips140/check",
    "crypto/internal/fips140/drbg",
    "crypto/internal/fips140/ecdh",
    "crypto/internal/fips140/ecdsa",
    "crypto/internal/fips140/ed25519",
    "crypto/internal/fips140/edwards25519",
    "crypto/internal/fips140/hkdf",
    "crypto/internal/fips140/hmac",
    "crypto/internal/fips140/mldsa",
    "crypto/internal/fips140/mlkem",
    "crypto/internal/fips140/nistec",
    "crypto/internal/fips140/pbkdf2",
    "crypto/internal/fips140/rsa",
    "crypto/internal/fips140/sha256",
    "crypto/internal/fips140/sha3",
    "crypto/internal/fips140/sha512",
    "crypto/internal/fips140/ssh",
    "crypto/internal/fips140/subtle",
    "crypto/internal/fips140/tls12",
    "crypto/internal/fips140/tls13",
    "crypto/internal/fips140cache",
    "crypto/internal/fips140deps/byteorder",
    "crypto/internal/fips140deps/cpu",
    "crypto/internal/fips140deps/godebug",
    "crypto/internal/fips140deps/time",
    "crypto/internal/fips140hash",
    "crypto/internal/fips140only",
    "crypto/internal/impl",
    "crypto/internal/rand",
    "crypto/internal/randutil",
    "crypto/internal/sysrand",
    "crypto/md5",
    "crypto/mldsa",
    "crypto/mlkem",
    "crypto/mlkem/mlkemtest",
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
    "crypto/tls/fipsonly",
    "crypto/tls/internal/fips140tls",
    "crypto/x509",
    "crypto/x509/internal/macos",
    "crypto/x509/pkix",
    "database",
    "database/sql",
    "database/sql/driver",
    "database/sql/internal",
    "debug",
    "debug/buildinfo",
    "debug/dwarf",
    "debug/elf",
    "debug/gosym",
    "debug/macho",
    "debug/pe",
    "debug/plan9obj",
    "embed",
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
    "encoding/json/internal",
    "encoding/json/internal/jsonflags",
    "encoding/json/internal/jsonopts",
    "encoding/json/internal/jsontest",
    "encoding/json/internal/jsonwire",
    "encoding/json/jsontext",
    "encoding/pem",
    "encoding/xml",
    "errors",
    "expvar",
    "flag",
    "fmt",
    "go",
    "go/ast",
    "go/build",
    "go/build/constraint",
    "go/constant",
    "go/doc",
    "go/doc/comment",
    "go/format",
    "go/importer",
    "go/internal/gccgoimporter",
    "go/internal/gcimporter",
    "go/parser",
    "go/printer",
    "go/scanner",
    "go/token",
    "go/types",
    "hash",
    "hash/adler32",
    "hash/crc32",
    "hash/crc64",
    "hash/fnv",
    "hash/maphash",
    "html",
    "html/template",
    "image",
    "image/color",
    "image/color/palette",
    "image/draw",
    "image/gif",
    "image/internal/imageutil",
    "image/jpeg",
    "image/png",
    "index",
    "index/suffixarray",
    "internal",
    "internal/abi",
    "internal/asan",
    "internal/bisect",
    "internal/buildcfg",
    "internal/bytealg",
    "internal/byteorder",
    "internal/cfg",
    "internal/cgrouptest",
    "internal/chacha8rand",
    "internal/coverage",
    "internal/coverage/calloc",
    "internal/coverage/cfile",
    "internal/coverage/cformat",
    "internal/coverage/cmerge",
    "internal/coverage/decodecounter",
    "internal/coverage/decodemeta",
    "internal/coverage/encodecounter",
    "internal/coverage/encodemeta",
    "internal/coverage/pods",
    "internal/coverage/rtcov",
    "internal/coverage/slicereader",
    "internal/coverage/slicewriter",
    "internal/coverage/stringtab",
    "internal/coverage/uleb128",
    "internal/cpu",
    "internal/dag",
    "internal/diff",
    "internal/exportdata",
    "internal/filepathlite",
    "internal/fmtsort",
    "internal/fuzz",
    "internal/gate",
    "internal/goarch",
    "internal/godebug",
    "internal/godebugs",
    "internal/goexperiment",
    "internal/goos",
    "internal/goroot",
    "internal/gover",
    "internal/goversion",
    "internal/lazyregexp",
    "internal/lazytemplate",
    "internal/msan",
    "internal/nettest",
    "internal/nettrace",
    "internal/obscuretestdata",
    "internal/oserror",
    "internal/pkgbits",
    "internal/platform",
    "internal/poll",
    "internal/profile",
    "internal/profilerecord",
    "internal/race",
    "internal/reflectlite",
    "internal/routebsd",
    "internal/runtime/atomic",
    "internal/runtime/cgobench",
    "internal/runtime/cgroup",
    "internal/runtime/exithook",
    "internal/runtime/gc",
    "internal/runtime/gc/scan",
    "internal/runtime/maps",
    "internal/runtime/math",
    "internal/runtime/pprof/label",
    "internal/runtime/startlinetest",
    "internal/runtime/sys",
    "internal/runtime/syscall/linux",
    "internal/runtime/syscall/windows",
    "internal/saferio",
    "internal/singleflight",
    "internal/strconv",
    "internal/stringslite",
    "internal/sync",
    "internal/synctest",
    "internal/syscall/execenv",
    "internal/syscall/unix",
    "internal/syscall/windows",
    "internal/syscall/windows/registry",
    "internal/syscall/windows/sysdll",
    "internal/sysinfo",
    "internal/syslist",
    "internal/testenv",
    "internal/testhash",
    "internal/testlog",
    "internal/testpty",
    "internal/trace",
    "internal/trace/internal/testgen",
    "internal/trace/internal/tracev1",
    "internal/trace/raw",
    "internal/trace/testtrace",
    "internal/trace/tracev2",
    "internal/trace/traceviewer",
    "internal/trace/traceviewer/format",
    "internal/trace/version",
    "internal/txtar",
    "internal/types/errors",
    "internal/unsafeheader",
    "internal/xcoff",
    "internal/zstd",
    "io",
    "io/fs",
    "io/ioutil",
    "iter",
    "log",
    "log/internal",
    "log/slog",
    "log/slog/internal",
    "log/slog/internal/benchmarks",
    "log/slog/internal/buffer",
    "log/syslog",
    "maps",
    "math",
    "math/big",
    "math/big/internal/asmgen",
    "math/bits",
    "math/cmplx",
    "math/rand",
    "mime",
    "mime/multipart",
    "mime/quotedprintable",
    "net",
    "net/http",
    "net/http/cgi",
    "net/http/cookiejar",
    "net/http/fcgi",
    "net/http/httptest",
    "net/http/httptrace",
    "net/http/httputil",
    "net/http/internal",
    "net/http/internal/ascii",
    "net/http/internal/http2",
    "net/http/internal/httpcommon",
    "net/http/internal/httpsfv",
    "net/http/internal/testcert",
    "net/http/pprof",
    "net/internal/cgotest",
    "net/internal/socktest",
    "net/mail",
    "net/netip",
    "net/rpc",
    "net/rpc/jsonrpc",
    "net/smtp",
    "net/textproto",
    "net/url",
    "os",
    "os/exec",
    "os/exec/internal/fdtest",
    "os/signal",
    "os/user",
    "path",
    "path/filepath",
    "plugin",
    "reflect",
    "reflect/internal/example1",
    "reflect/internal/example2",
    "regexp",
    "regexp/syntax",
    "runtime",
    "runtime/_mkmalloc/astutil",
    "runtime/asan",
    "runtime/cgo",
    "runtime/coverage",
    "runtime/debug",
    "runtime/metrics",
    "runtime/msan",
    "runtime/pprof",
    "runtime/race",
    "runtime/race/internal/amd64v1",
    "runtime/race/internal/amd64v3",
    "runtime/secret",
    "runtime/trace",
    "simd",
    "simd/archsimd",
    "simd/archsimd/_gen/sgutil",
    "simd/archsimd/_gen/unify",
    "simd/archsimd/internal/test_helpers",
    "simd/internal/bridge",
    "slices",
    "sort",
    "strconv",
    "strings",
    "structs",
    "sync",
    "sync/atomic",
    "syscall",
    "syscall/js",
    "testing",
    "testing/cryptotest",
    "testing/fstest",
    "testing/internal/testdeps",
    "testing/iotest",
    "testing/quick",
    "testing/slogtest",
    "testing/synctest",
    "text",
    "text/scanner",
    "text/tabwriter",
    "text/template",
    "text/template/parse",
    "time",
    "time/tzdata",
    "unicode",
    "unicode/utf16",
    "unicode/utf8",
    "unique",
    "unsafe",
    "uuid",
    "weak",
])

# Base package names: "net/http" -> "http". Quoting the full import path in
# a conditional is still a conditional on the base package name, so the
# gate tests both the full path and the base.
STDLIB_BASES = frozenset(p.rsplit("/", 1)[-1] for p in STDLIB_PATHS)

def get_diff():
    """Get the diff of added lines, plus a label naming the diff source.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.
    """
    # Try vs origin/master first (for PRs), fall back to working tree,
    # then to the master-push delta. A shallow checkout (no HEAD~1)
    # yields nothing from the last source — that is a failure, not a skip.
    candidates = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
        ("HEAD~1...HEAD (master push)", ["git", "diff", "HEAD~1...HEAD", "--", "."]),
    ]
    for name, cmd in candidates:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except Exception:
            continue
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout, name
    return "", ""

def _is_stdlib_prefixed(node_kind):
    """True if node_kind starts with a stdlib package base name followed by
    a lowercase letter (the stdlib-specific wire-node naming shape, e.g.
    "runtimenumcpu", "tlsverify", "cryptox509")."""
    for base in STDLIB_BASES:
        if node_kind.startswith(base):
            rest = node_kind[len(base):]
            if rest and "a" <= rest[0] <= "z":
                return True
    return False


def check_no_bridges(diff):
    errors = []
    added_lines = []
    current_file = None

    for line in diff.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:]
        elif line.startswith("+") and not line.startswith("+++"):
            added_lines.append((current_file, line[1:]))

    # 1. New synth_*.go files. Only genuinely NEW files count (the --- line
    #    is /dev/null); modifying a grandfathered synth_*.go is not a new
    #    bridge — without this, every master push touching a synthetic file
    #    would fail the gate.
    new_synth = set()
    prev_was_devnull = False
    for line in diff.split("\n"):
        if line.startswith("--- /dev/null"):
            prev_was_devnull = True
        elif line.startswith("+++ b/"):
            if prev_was_devnull and "synth_" in line and line.endswith(".go"):
                new_synth.add(line[6:])
            prev_was_devnull = False
        elif line.startswith("--- "):
            prev_was_devnull = False
    for f in new_synth:
        errors.append(f"NEW SYNTHETIC FILE: {f} — synthetic stdlib packages are banned")

    # 2. Package-name conditionals (stdlib names). Matches full import paths
    #    ("net/http") as well as bare base names ("http"): each quoted path in
    #    a name-comparison context (==, !=, case) is tested as the full path
    #    AND as its base, so quoting "a/b" instead of "b" does not evade the
    #    gate. (P1-2: the old 20-entry bare-name list missed `== "net/http"`,
    #    `case "encoding/json"`, and every package not on the list, e.g.
    #    "slices".)
    cond_re = re.compile(r'[=!]=\s*"([^"]+)"|case\s*"([^"]+)"')
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        # Allow in test files and docs. ("_test." covers *_test.go harnesses,
        # e.g. drift-test importers that resolve synthetic import paths like
        # the pre-existing `if path == "bytes"` stubs.)
        if "/test" in fname or "test_" in fname or "_test." in fname:
            continue
        hit = None
        for m in cond_re.finditer(line):
            path = m.group(1) or m.group(2)
            if path in STDLIB_PATHS or path.rsplit("/", 1)[-1] in STDLIB_BASES:
                hit = path
                break
        if hit:
            errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")

    # 3. New panic stubs. The old regex only caught literal "not implemented"/
    #    "unsupported"/"todo" messages; fail-fast stubs also hide behind bare
    #    panic(), panic(""), panic("unreachable"), and composed messages like
    #    panic(fmt.Sprintf("... not implemented")). Fail loud on all of them —
    #    a human decides whether an added panic is a legitimate invariant.
    panic_stub_res = [
        r'panic\s*\(\s*\)',            # bare panic() — nothing to go on
        r'panic\s*\(\s*""\s*\)',       # panic("") — nothing to go on
        r'panic\s*\(\s*"(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd)',
        # Stub keyword anywhere in the first string arg of a composed panic.
        r'panic\s*\(\s*(?:fmt\.Sprintf|fmt\.Errorf|errors\.New)\s*\(\s*"[^"]*(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet)',
    ]
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        for pat in panic_stub_res:
            if re.search(pat, line, re.I):
                errors.append(f"PANIC STUB in {fname}: {line.strip()[:80]}")
                break

    # 4. New wire node kinds for stdlib. Flags any new node kind in
    #    export.go that starts with a stdlib package base name ("tlsverify"
    #    via crypto/tls, "cryptox509" via crypto, "nethttpserver" via
    #    net/http, ...). The old 5-prefix heuristic (runtime|fmt|sync|os|net)
    #    missed every other stdlib root (P1-2).
    #    NOTE: generic AST node names that collide with a stdlib base
    #    (e.g. a hypothetical "gostart", like the existing "gostmt"/"timec"/
    #    "syncop") fail LOUD here and need an adjudication entry — that is
    #    intentional: a human decides, the gate never guesses.
    for fname, line in added_lines:
        if not fname or "export.go" not in fname:
            continue
        m = re.search(r'e\.node\s*\(\s*"([^"]+)"', line)
        if m:
            node_kind = m.group(1)
            if _is_stdlib_prefixed(node_kind):
                # Adjudicated real implementations are not bridges.
                if any(f_sub in fname and nk == node_kind
                       for f_sub, nk in ADJUDICATED_WIRE_NODES):
                    continue
                errors.append(f"SUSPICIOUS WIRE NODE in {fname}: {node_kind}")

    # 5. New synthetic function registrations
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if re.search(r'addFunc\s*\(|synthetic.*Package\s*\(|registerSynthetic', line):
            # Allow in existing synth files (grandfathered), flag in new contexts
            if "synth_" not in fname:
                # Adjudicated real implementations are not bridges.
                m = re.search(r'addFunc\s*\(\s*"([^"]+)"', line)
                if m and any(f_sub in fname and sym == m.group(1)
                             for f_sub, sym in ADJUDICATED_REGISTRATIONS):
                    continue
                errors.append(f"SYNTHETIC REGISTRATION in {fname}: {line.strip()[:80]}")

    return errors

def main():
    diff, source = get_diff()
    if not diff.strip():
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against. Skipping here is
        # exactly the false-green hole this check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        print("  could not determine any diff to inspect (no PR diff, no")
        print("  working-tree changes, and HEAD~1 is unavailable — is this a")
        print("  shallow checkout? CI must use fetch-depth: 0).")
        print("  Refusing to pass vacuously.")
        return 1

    print(f"No-bridges check: inspecting added lines from {source}.")
    errors = check_no_bridges(diff)
    if errors:
        print("NO-BRIDGES CHECK FAILED:")
        print()
        for e in errors:
            print(f"  ✗ {e}")
        print()
        print("His order (2026-10-15): No new bridges. Fix the compiler/runtime properly.")
        return 1

    print("No-bridges check passed.")
    return 0

if __name__ == "__main__":
    sys.exit(main())
