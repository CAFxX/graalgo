#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic/fail-fast stubs (Go, Java, Python)
- New wire node kinds (Go e.node emissions anywhere + Java
  registerStmtKind/registerKind registrations)
- New synthetic function registrations
- New syntheticSources overlay entries / synthetic source consts

Existing bridges are grandfathered (they're in the removal queue — see
docs/arch/bridge-inventory.md). This check only looks at ADDED lines in the diff.

Diff sources, in order:
  1. $NO_BRIDGES_PUSH_BASE...HEAD  (push events: CI passes
     github.event.before, the pre-push base — the whole pushed range.
     wt.py integrate ff-merges whole lane branches, so HEAD~1...HEAD
     would inspect only the tip commit. Skipped when unset or all-zeros,
     e.g. PR/schedule/dispatch runs and branch-creation pushes.)
  2. origin/master...HEAD  (PR / feature branch)
  3. git diff HEAD         (uncommitted working-tree changes)
  4. git diff --cached     (staged changes)
  5. HEAD~1...HEAD         (direct master push without a push base)

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for the push-base and
HEAD~1 sources to resolve.
"""

import os
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

# Generic (non-stdlib-specific) wire node kinds: the closed AST vocabulary
# the frontend emitter and the backend kind factories share. The wire-node
# check is INVERTED: an added e.node("kind") emission (any .go file) or
# registerStmtKind/registerKind("kind") registration (any .java file) whose
# kind is not in this set — and not adjudicated above — is flagged as a
# suspicious new wire node. The old family-prefix regex missed the live
# time*/unsafe*/reflectop*/abifuncpc families; inversion cannot miss a
# family. A genuinely new generic kind extends this set through the
# adjudication process (independent review), not by lane edit.
# Derived 2026-10-17 from the 80 live e.node kinds in internal/frontend
# (49 generic below; 31 stdlib-specific: time*/runtime*/unsafe* families,
# syncop, atomicaddr, reflectop, abifuncpc — enumerated in
# docs/arch/bridge-inventory.md B-3).
GENERIC_WIRE_KINDS = frozenset([
    "addrof", "assign", "basiclit", "binary", "blank", "block", "branch",
    "call", "caseclause", "chanclose", "clear", "complit", "convert",
    "defer", "deref", "exprstmt", "for", "funcdecl", "funcfieldcall",
    "funclit", "gostmt", "ident", "if", "ifacemethod", "incdec", "index",
    "labeled", "makechan", "methodref", "methodvalue", "minmax", "nil",
    "param", "paren", "range", "recv", "return", "select", "selectcase",
    "selector", "send", "sliceexpr", "sliceoverlap", "switch", "typeassert",
    "typecase", "typeswitch", "unary", "vardecl",
])

# Frozen Java fail-fast stubs: intentional Phase-1 API contracts, not
# bodges. Each file freezes signatures a later phase owns; every method
# throws UnsupportedOperationException with a phase-scoped message and the
# file header says STUB. The Java fail-fast-stub check grandfathers new
# throw sites in exactly these files; a new throw anywhere else fails the
# gate. Inventory record: docs/arch/bridge-inventory.md
# ("Frozen API contracts (not bridges)").
FROZEN_JAVA_STUB_FILES = (
    "graalgo/runtime/Netpoll.java",      # frozen netpoller contract (WS G, GG-0020)
    "graalgo/runtime/ChanOps.java",      # frozen channel/select surface (WS G, GG-0020)
    "graalgo/runtime/Scheduler.java",    # frozen scheduler glue (WS G, GG-0020)
    "graalgo/runtime/GcInterop.java",    # Phase-1 stub, P2-M4 (ADR-0002 D4)
    "graalgo/safepoint/Safepoint.java",  # Phase-1 no-op seam (PLAN.md D8, GG-0017)
)

# Marker written by the gen_synth_*.py pipeline into generated synth files
# ("Generated by gen_synth_archive_mime.py from gc 1.27.1; DO NOT EDIT").
# The syntheticSources-const check exempts const definitions in files
# carrying this marker: regenerating the pipeline is not a new bridge.
# Adding the overlay MAP ENTRY for a new package is always flagged — the
# generators never touch the map (it is hand-maintained), so wiring a new
# package into the overlay is always a human-reviewed event.
GEN_SYNTH_MARKER = "Generated by gen_synth_"

_repo_root_cache = None


def _repo_root():
    """Repo top level, or None when not inside a git checkout."""
    global _repo_root_cache
    if _repo_root_cache is None:
        try:
            r = subprocess.run(["git", "rev-parse", "--show-toplevel"],
                               capture_output=True, text=True, timeout=10)
            _repo_root_cache = r.stdout.strip() if r.returncode == 0 else ""
        except Exception:
            _repo_root_cache = ""
    return _repo_root_cache or None


def _is_test_file(fname):
    """Narrow test-file exclusion: real test files by path suffix only.

    The old substring test ("/test" in fname, "test_" in fname) excluded any
    file under a */test/* dir and any file with test_ anywhere in its name —
    a hiding spot. Only *_test.go harnesses and Maven/Gradle src/test trees
    are excluded now.
    """
    return fname.endswith("_test.go") or "/src/test/" in fname


def _file_has_gen_synth_marker(abs_path):
    """True when the file header carries the generator-pipeline marker."""
    try:
        with open(abs_path, "r", encoding="utf-8", errors="replace") as f:
            for i, line in enumerate(f):
                if i > 25:
                    break
                if GEN_SYNTH_MARKER in line:
                    return True
    except OSError:
        pass
    return False


def _is_gen_synth_output(repo_rel_path):
    """True when repo_rel_path is a generator-pipeline output file.

    Unreadable or missing files are NOT treated as generated — fail loud.
    """
    root = _repo_root()
    if not root:
        return False
    return _file_has_gen_synth_marker(os.path.join(root, repo_rel_path))


def _stdlib_package_names():
    """Mechanical stdlib package list.

    The embedded GOROOT-derived set (complete for the pinned toolchain)
    plus the live syntheticSources overlay keys read from the repo at check
    time — the overlay is the documented extension surface and holds
    non-GOROOT entries (internal/graalgo/fmtstate, golang.org/x/...,
    reflect_m7) no static list can cover.
    """
    pkgs = set(_GOROOT_STDLIB_PKGS)
    root = _repo_root()
    if root:
        try:
            with open(os.path.join(root, "internal/frontend/synth_machinery.go"),
                      "r", encoding="utf-8") as f:
                src = f.read()
            m = re.search(r"var syntheticSources = map\[string\]string\{(.*?)\n\}",
                          src, re.S)
            if m:
                pkgs.update(re.findall(r'^\s*"([^"]+)"\s*:', m.group(1), re.M))
        except OSError:
            pass
    return pkgs


_pkg_shape_res_cache = None


def _pkg_conditional_res():
    """Compiled package-name-conditional shapes over the mechanical list.

    Shapes: == "pkg", case "pkg", Name == "pkg", strings.HasPrefix/HasSuffix/
    Contains(x, "pkg"), Java startsWith/endsWith("pkg"), map index
    m["pkg"], and .Path() == "pkg". One alternation per shape over all
    package names (longest-first so net/http wins over net).
    """
    global _pkg_shape_res_cache
    if _pkg_shape_res_cache is None:
        alt = "|".join(sorted((re.escape(p) for p in _stdlib_package_names()),
                              key=len, reverse=True))
        q = r'"(?:' + alt + r')"'
        _pkg_shape_res_cache = [
            re.compile(r'==\s*' + q),
            re.compile(r'case\s*' + q),
            re.compile(r'Name\s*==\s*' + q),
            re.compile(r'HasPrefix\s*\([^)]*' + q),
            re.compile(r'HasSuffix\s*\([^)]*' + q),
            re.compile(r'Contains\s*\([^)]*' + q),
            re.compile(r'startsWith\s*\(\s*' + q),
            re.compile(r'endsWith\s*\(\s*' + q),
            re.compile(r'\[' + q + r'\]'),
            re.compile(r'\.Path\(\)\s*==\s*' + q),
        ]
    return _pkg_shape_res_cache


def _diff_candidates():
    """Ordered (label, argv) diff sources; see module docstring."""
    cands = []
    push_base = os.environ.get("NO_BRIDGES_PUSH_BASE", "").strip()
    # All-zeros = branch/tag creation push: no base to diff against.
    if push_base and set(push_base) != {"0"}:
        cands.append(
            (f"push base {push_base}...HEAD (whole pushed range)",
             ["git", "diff", f"{push_base}...HEAD", "--", "."]))
    cands.extend([
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
        ("HEAD~1...HEAD (master push, tip commit only)",
         ["git", "diff", "HEAD~1...HEAD", "--", "."]),
    ])
    return cands


def get_diff():
    """Get the diff of added lines, plus a label naming the diff source.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.
    """
    # Push-base first (whole pushed range on master pushes), then PR diff,
    # then working tree, then the tip-commit fallback. A shallow checkout
    # (no base, no HEAD~1) yields nothing from any source — that is a
    # failure, not a skip.
    for name, cmd in _diff_candidates():
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except Exception:
            continue
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout, name
    return "", ""
# Mechanical stdlib list, derived 2026-10-17 from the pinned go1.27.1
# GOROOT: every package dir containing .go files, excluding cmd/, vendor/,
# and zzdiag/ (local scratch, not upstream). Regenerate with:
#   find $GOROOT/src -name "*.go" -not -path "*/cmd/*" -not -path "*/vendor/*" -not -path "*/zzdiag/*" |
#     sed "s|^\./||; s|/[^/]*\.go$||" | sort -u
_GOROOT_STDLIB_PKGS = frozenset([
    "archive/tar",
    "archive/zip",
    "arena",
    "bufio",
    "builtin",
    "bytes",
    "cmp",
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
    "crypto/internal/cryptotest/wycheproof/_schema",
    "crypto/internal/cryptotest/x509limbo",
    "crypto/internal/cryptotest/x509limbo/_schema",
    "crypto/internal/entropy",
    "crypto/internal/entropy/v1.0.0",
    "crypto/internal/fips140",
    "crypto/internal/fips140/aes",
    "crypto/internal/fips140/aes/_asm/ctr",
    "crypto/internal/fips140/aes/_asm/standard",
    "crypto/internal/fips140/aes/gcm",
    "crypto/internal/fips140/aes/gcm/_asm/gcm",
    "crypto/internal/fips140/alias",
    "crypto/internal/fips140/bigmod",
    "crypto/internal/fips140/bigmod/_asm",
    "crypto/internal/fips140/check",
    "crypto/internal/fips140/check/checktest",
    "crypto/internal/fips140/drbg",
    "crypto/internal/fips140/ecdh",
    "crypto/internal/fips140/ecdsa",
    "crypto/internal/fips140/ed25519",
    "crypto/internal/fips140/edwards25519",
    "crypto/internal/fips140/edwards25519/field",
    "crypto/internal/fips140/edwards25519/field/_asm",
    "crypto/internal/fips140/hkdf",
    "crypto/internal/fips140/hmac",
    "crypto/internal/fips140/mldsa",
    "crypto/internal/fips140/mlkem",
    "crypto/internal/fips140/nistec",
    "crypto/internal/fips140/nistec/_asm",
    "crypto/internal/fips140/nistec/fiat",
    "crypto/internal/fips140/pbkdf2",
    "crypto/internal/fips140/rsa",
    "crypto/internal/fips140/sha256",
    "crypto/internal/fips140/sha256/_asm",
    "crypto/internal/fips140/sha3",
    "crypto/internal/fips140/sha3/_asm",
    "crypto/internal/fips140/sha512",
    "crypto/internal/fips140/sha512/_asm",
    "crypto/internal/fips140/ssh",
    "crypto/internal/fips140/subtle",
    "crypto/internal/fips140/tls12",
    "crypto/internal/fips140/tls13",
    "crypto/internal/fips140cache",
    "crypto/internal/fips140deps",
    "crypto/internal/fips140deps/byteorder",
    "crypto/internal/fips140deps/cpu",
    "crypto/internal/fips140deps/godebug",
    "crypto/internal/fips140deps/time",
    "crypto/internal/fips140hash",
    "crypto/internal/fips140only",
    "crypto/internal/fips140test",
    "crypto/internal/impl",
    "crypto/internal/rand",
    "crypto/internal/randutil",
    "crypto/internal/sysrand",
    "crypto/internal/sysrand/internal/seccomp",
    "crypto/md5",
    "crypto/md5/_asm",
    "crypto/mldsa",
    "crypto/mlkem",
    "crypto/mlkem/mlkemtest",
    "crypto/pbkdf2",
    "crypto/rand",
    "crypto/rc4",
    "crypto/rsa",
    "crypto/sha1",
    "crypto/sha1/_asm",
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
    "database/sql",
    "database/sql/driver",
    "database/sql/internal",
    "debug/buildinfo",
    "debug/buildinfo/testdata/go117",
    "debug/dwarf",
    "debug/elf",
    "debug/gosym",
    "debug/gosym/testdata",
    "debug/macho",
    "debug/pe",
    "debug/plan9obj",
    "embed",
    "embed/internal/embedtest",
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
    "encoding/json/v2",
    "encoding/pem",
    "encoding/xml",
    "errors",
    "expvar",
    "flag",
    "fmt",
    "go/ast",
    "go/build",
    "go/build/constraint",
    "go/build/testdata/alltags",
    "go/build/testdata/cgo_disabled",
    "go/build/testdata/directives",
    "go/build/testdata/doc",
    "go/build/testdata/multi",
    "go/build/testdata/non_source_tags",
    "go/build/testdata/other",
    "go/build/testdata/other/file",
    "go/build/testdata/withvendor/src/a/b",
    "go/build/testdata/withvendor/src/a/vendor/c/d",
    "go/constant",
    "go/doc",
    "go/doc/comment",
    "go/doc/testdata",
    "go/doc/testdata/examples",
    "go/doc/testdata/pkgdoc",
    "go/format",
    "go/importer",
    "go/internal/gccgoimporter",
    "go/internal/gccgoimporter/testdata",
    "go/internal/gcimporter",
    "go/internal/gcimporter/testdata",
    "go/internal/gcimporter/testdata/versions",
    "go/internal/srcimporter",
    "go/internal/srcimporter/testdata/issue20855",
    "go/internal/srcimporter/testdata/issue23092",
    "go/internal/srcimporter/testdata/issue24392",
    "go/parser",
    "go/parser/testdata/goversion",
    "go/parser/testdata/issue42951",
    "go/parser/testdata/issue42951/not_a_file.go",
    "go/printer",
    "go/printer/testdata",
    "go/scanner",
    "go/token",
    "go/types",
    "go/types/testdata",
    "go/types/testdata/local",
    "go/version",
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
    "index/suffixarray",
    "internal/abi",
    "internal/abi/testdata",
    "internal/asan",
    "internal/bisect",
    "internal/buildcfg",
    "internal/bytealg",
    "internal/byteorder",
    "internal/cfg",
    "internal/cgrouptest",
    "internal/chacha8rand",
    "internal/copyright",
    "internal/coverage",
    "internal/coverage/calloc",
    "internal/coverage/cfile",
    "internal/coverage/cfile/testdata",
    "internal/coverage/cfile/testdata/issue56006",
    "internal/coverage/cfile/testdata/issue59563",
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
    "internal/coverage/test",
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
    "internal/runtime/gc/internal/gen",
    "internal/runtime/gc/scan",
    "internal/runtime/maps",
    "internal/runtime/math",
    "internal/runtime/pprof/label",
    "internal/runtime/startlinetest",
    "internal/runtime/sys",
    "internal/runtime/syscall/linux",
    "internal/runtime/syscall/windows",
    "internal/runtime/wasitest",
    "internal/runtime/wasitest/testdata",
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
    "internal/trace/testdata",
    "internal/trace/testdata/generators",
    "internal/trace/testdata/testprog",
    "internal/trace/testtrace",
    "internal/trace/tracev2",
    "internal/trace/traceviewer",
    "internal/trace/traceviewer/format",
    "internal/trace/version",
    "internal/txtar",
    "internal/types/errors",
    "internal/types/testdata/check",
    "internal/types/testdata/check/decls2",
    "internal/types/testdata/check/importdecl0",
    "internal/types/testdata/check/importdecl1",
    "internal/types/testdata/check/issue25008",
    "internal/types/testdata/examples",
    "internal/types/testdata/fixedbugs",
    "internal/types/testdata/spec",
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
    "math/rand/v2",
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
    "net/http/pprof/testdata",
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
    "runtime/_mkmalloc",
    "runtime/_mkmalloc/astutil",
    "runtime/asan",
    "runtime/cgo",
    "runtime/coverage",
    "runtime/debug",
    "runtime/metrics",
    "runtime/msan",
    "runtime/pprof",
    "runtime/pprof/testdata/mappingtest",
    "runtime/race",
    "runtime/race/internal/amd64v1",
    "runtime/race/internal/amd64v3",
    "runtime/race/testdata",
    "runtime/secret",
    "runtime/secret/testdata",
    "runtime/testdata/testexithooks",
    "runtime/testdata/testfaketime",
    "runtime/testdata/testfds",
    "runtime/testdata/testgoroutineleakprofile",
    "runtime/testdata/testgoroutineleakprofile/goker",
    "runtime/testdata/testprog",
    "runtime/testdata/testprogcgo",
    "runtime/testdata/testprogcgo/goasm",
    "runtime/testdata/testprogcgo/windows",
    "runtime/testdata/testprognet",
    "runtime/testdata/testsuid",
    "runtime/testdata/testsynctest",
    "runtime/testdata/testsyscall",
    "runtime/testdata/testsyscall/testsyscallc",
    "runtime/testdata/testwinlib",
    "runtime/testdata/testwinlibsignal",
    "runtime/testdata/testwinlibthrow",
    "runtime/testdata/testwinsignal",
    "runtime/testdata/testwintls",
    "runtime/trace",
    "simd",
    "simd/archsimd",
    "simd/archsimd/_gen",
    "simd/archsimd/_gen/midway",
    "simd/archsimd/_gen/sgutil",
    "simd/archsimd/_gen/simdgen",
    "simd/archsimd/_gen/simdgen/arm64",
    "simd/archsimd/_gen/tmplgen",
    "simd/archsimd/_gen/unify",
    "simd/archsimd/_gen/wasmgen",
    "simd/archsimd/internal/simd_test",
    "simd/archsimd/internal/test_helpers",
    "simd/archsimd/testdata",
    "simd/archsimd/testdata/arm64",
    "simd/internal/bridge",
    "simd/testdata",
    "simd/testdata/iface",
    "simd/testdata/pkg",
    "simd/testdata/simd",
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

    # 2. Package-name conditionals. The package list is mechanical
    #    (_stdlib_package_names): the GOROOT-derived set plus the live
    #    syntheticSources overlay keys. Shapes cover ==, case, Name ==,
    #    HasPrefix/HasSuffix/Contains, Java startsWith/endsWith, map-index
    #    m["pkg"], and .Path() == "pkg". Test files (*_test.go, src/test/)
    #    are excluded — narrowly, by suffix (D7).
    pkg_res = _pkg_conditional_res()
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        if _is_test_file(fname):
            continue
        for pat in pkg_res:
            if pat.search(line):
                errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")
                break

    # 3. New fail-fast stubs.
    #    Go: the old regex only caught literal "not implemented"/
    #    "unsupported"/"todo" messages; fail-fast stubs also hide behind bare
    #    panic(), panic(""), panic("unreachable"), and composed messages like
    #    panic(fmt.Sprintf("... not implemented")). Fail loud on all of them —
    #    a human decides whether an added panic is a legitimate invariant.
    #    Java: throw new UnsupportedOperationException (+ TODO/FIXME in a
    #    throw site's message). The five frozen API-contract files are
    #    grandfathered (FROZEN_JAVA_STUB_FILES); anything else is flagged.
    #    Python: the NotImplementedError-raising equivalent in generators/templates.
    panic_stub_res = [
        r'panic\s*\(\s*\)',            # bare panic() — nothing to go on
        r'panic\s*\(\s*""\s*\)',       # panic("") — nothing to go on
        r'panic\s*\(\s*"(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd)',
        # Stub keyword anywhere in the first string arg of a composed panic.
        r'panic\s*\(\s*(?:fmt\.Sprintf|fmt\.Errorf|errors\.New)\s*\(\s*"[^"]*(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet)',
    ]
    java_stub_res = [
        r'throw\s+new\s+UnsupportedOperationException\b',
        # TODO/FIXME (or stub keyword) in a throw site's message.
        r'throw\s+new\s+\w[\w.]*\s*\([^)]*(?:TODO|FIXME|not implemented|unimplemented|not supported|not yet)',
    ]
    py_stub_res = [
        r'raise\s+NotImplementedError\b',
        r'NotImplementedError\s*\(',
    ]
    for fname, line in added_lines:
        if not fname:
            continue
        if fname.endswith(".go"):
            pats = panic_stub_res
        elif fname.endswith(".java"):
            # Frozen API contracts extend their stub surface freely.
            if any(fname.endswith(suf) for suf in FROZEN_JAVA_STUB_FILES):
                continue
            pats = java_stub_res
        elif fname.endswith(".py"):
            pats = py_stub_res
        else:
            continue
        for pat in pats:
            if re.search(pat, line, re.I):
                errors.append(f"FAIL-FAST STUB in {fname}: {line.strip()[:80]}")
                break

    # 4. New wire node kinds (INVERTED). Any added e.node("kind") emission
    #    in any .go file, or registerStmtKind/registerKind("kind") in any
    #    .java file, whose kind is outside the shared generic vocabulary
    #    (GENERIC_WIRE_KINDS) is flagged — unless adjudicated. Comment-only
    #    lines (kind mentions in prose) are skipped: a comment emits nothing.
    wire_go_re = re.compile(r'\.node\s*\(\s*"([^"]+)"')
    wire_java_re = re.compile(r'register(?:Stmt)?Kind\s*\(\s*"([^"]+)"')
    for fname, line in added_lines:
        if not fname:
            continue
        stripped = line.lstrip()
        if stripped.startswith("//"):
            continue
        kind = None
        if fname.endswith(".go"):
            m = wire_go_re.search(line)
            if m:
                kind = m.group(1)
        elif fname.endswith(".java"):
            m = wire_java_re.search(line)
            if m:
                kind = m.group(1)
        if kind is None or kind in GENERIC_WIRE_KINDS:
            continue
        # Adjudicated real implementations are not bridges. The Go emission
        # is file-scoped (the adjudication names export.go); the Java
        # registerStmtKind half is kind-scoped — the PKG-040 inventory entry
        # explicitly clears "the registerStmtKind("runtimegosched", ...) P3
        # registrations" as the public register-kind hook, not synthetic
        # stdlib logic. Same entries, no broadening.
        if fname.endswith(".java"):
            if kind in {nk for _, nk in ADJUDICATED_WIRE_NODES}:
                continue
        elif any(f_sub in fname and nk == kind for f_sub, nk in ADJUDICATED_WIRE_NODES):
            continue
        errors.append(f"SUSPICIOUS WIRE NODE in {fname}: {kind}")

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

    # 6. New synthetic stdlib packages via the syntheticSources overlay.
    #    export.go documents: "new synthetic packages work automatically when
    #    added to syntheticSources". Wiring one in is a one-line map entry
    #    ("newpkg": syntheticNewpkgSource,) plus a const definition — checks
    #    1/5 match neither. The generators never touch the map, so every
    #    added map entry is flagged. Added const definitions are flagged
    #    unless the file is a generator-pipeline output (regeneration is not
    #    a new bridge); an unreadable file is never treated as generated.
    synth_map_entry_re = re.compile(r'^\s*"[^"]+"\s*:\s*synthetic\w+Source\b')
    synth_const_re = re.compile(r'^\s*(?:const\s+)?synthetic\w+Source\s*=')
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if line.lstrip().startswith("//"):
            continue
        if synth_map_entry_re.search(line):
            errors.append(
                f"NEW SYNTHETIC PACKAGE (syntheticSources entry) in {fname}: "
                f"{line.strip()[:80]}")
        elif synth_const_re.search(line):
            if not _is_gen_synth_output(fname):
                errors.append(
                    f"NEW SYNTHETIC PACKAGE (source const) in {fname}: "
                    f"{line.strip()[:80]}")

    return errors


def main():
    diff, source = get_diff()
    if not diff.strip():
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against. Skipping here is
        # exactly the false-green hole this check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        print("  could not determine any diff to inspect (no push-base diff,")
        print("  no PR diff, no working-tree changes, and HEAD~1 is")
        print("  unavailable — is this a shallow checkout? CI must use")
        print("  fetch-depth: 0).")
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
