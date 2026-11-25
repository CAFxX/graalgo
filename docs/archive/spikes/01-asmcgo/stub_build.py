#!/usr/bin/env python3
"""Build the graalgos01stub module.

Copies selected stdlib package trees (sync/atomic, bytes, strings and the
internal packages they need) under ~/workspace/graalgo/spikes/01-asmcgo/stub,
replacing every Plan-9 .s file with the spike's high-level formulations:

  internal/runtime/atomic/*.s  -> internal/runtime/atomic/form.go (striped-lock emulation)
  internal/bytealg asm          -> internal/bytealg/bytealg.go (pure-Go formulations)
  sync/atomic asm.s/race.s     -> sync/atomic/wiring.go (direct Go delegation)

Import paths internal/X are rewritten to graalgos01stub/internal/X; all other
imports keep pointing at GOROOT so the stub exercises the formulations against
the real runtime. Deterministic: same inputs -> same outputs.
"""
import os
import re
import shutil
import sys

GOROOT = os.path.expanduser("~/tools/go/src")
SPIKE = os.path.expanduser("~/workspace/graalgo/spikes/01-asmcgo")
HARNESS = os.path.join(SPIKE, "harness")
STUB = os.path.join(SPIKE, "stub")
MOD = "graalgos01stub"

REWRITES = {
    '"internal/bytealg"': '"%s/internal/bytealg"' % MOD,
    '"internal/stringslite"': '"%s/internal/stringslite"' % MOD,
    '"internal/abi"': '"%s/internal/abi"' % MOD,
    '"internal/testenv"': '"%s/internal/testenv"' % MOD,
    '"internal/asan"': '"%s/internal/asan"' % MOD,
    '"internal/goarch"': '"%s/internal/goarch"' % MOD,
    '"internal/cfg"': '"%s/internal/cfg"' % MOD,
    '"internal/platform"': '"%s/internal/platform"' % MOD,
    # self-package references inside the copied test files
    '"sync/atomic"': '"%s/sync/atomic"' % MOD,
    '"bytes"': '"%s/bytes"' % MOD,
    '"strings"': '"%s/strings"' % MOD,
}


def rewrite_imports(src):
    """Rewrite import paths, but only inside import declarations."""
    out = []
    in_block = False
    for line in src.split("\n"):
        s = line.strip()
        if re.match(r"^import\s*\($", s):
            in_block = True
        elif s == ")" and in_block:
            in_block = False
        elif re.match(r'^import\s+"[^"]+"$', s):
            for old, new in REWRITES.items():
                s = s.replace(old, new)
            line = line[: len(line) - len(line.lstrip())] + s
            out.append(line)
            continue
        if in_block:
            for old, new in REWRITES.items():
                line = re.sub(
                    r'(?<![\w/])' + re.escape(old) + r'(?![\w/])', new, line
                )
        out.append(line)
    return "\n".join(out)


def wrel(path, content):
    full = os.path.join(STUB, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(content)


def cpgo(srcdir, dstdir):
    """Copy *.go files (never .s), skipping nothing else."""
    os.makedirs(os.path.join(STUB, dstdir), exist_ok=True)
    for fn in sorted(os.listdir(os.path.join(GOROOT, srcdir))):
        if not fn.endswith(".go"):
            continue
        with open(os.path.join(GOROOT, srcdir, fn)) as f:
            src = f.read()
        wrel(os.path.join(dstdir, fn), rewrite_imports(src))


BODIES = {
    "SwapInt32": "return rtatomic.Xchgint32(addr, new)",
    "CompareAndSwapInt32": "return rtatomic.Casint32(addr, old, new)",
    "AddInt32": "return rtatomic.Xaddint32(addr, delta)",
    "AndInt32": "return rtatomic.Andint32(addr, mask)",
    "OrInt32": "return rtatomic.Orint32(addr, mask)",
    "LoadInt32": "return rtatomic.Loadint32(addr)",
    "StoreInt32": "rtatomic.Storeint32(addr, val)",
    "SwapUint32": "return rtatomic.Xchg(addr, new)",
    "CompareAndSwapUint32": "return rtatomic.Cas(addr, old, new)",
    "AddUint32": "return rtatomic.Xadd(addr, int32(delta))",
    "AndUint32": "return rtatomic.And32(addr, mask)",
    "OrUint32": "return rtatomic.Or32(addr, mask)",
    "LoadUint32": "return rtatomic.Load(addr)",
    "StoreUint32": "rtatomic.Store(addr, val)",
    "SwapInt64": "return rtatomic.Xchgint64(addr, new)",
    "CompareAndSwapInt64": "return rtatomic.Casint64(addr, old, new)",
    "AddInt64": "return rtatomic.Xaddint64(addr, delta)",
    "AndInt64": "return rtatomic.Andint64(addr, mask)",
    "OrInt64": "return rtatomic.Orint64(addr, mask)",
    "LoadInt64": "return rtatomic.Loadint64(addr)",
    "StoreInt64": "rtatomic.Storeint64(addr, val)",
    "SwapUint64": "return rtatomic.Xchg64(addr, new)",
    "CompareAndSwapUint64": "return rtatomic.Cas64(addr, old, new)",
    "AddUint64": "return rtatomic.Xadd64(addr, int64(delta))",
    "AndUint64": "return rtatomic.And64(addr, mask)",
    "OrUint64": "return rtatomic.Or64(addr, mask)",
    "LoadUint64": "return rtatomic.Load64(addr)",
    "StoreUint64": "rtatomic.Store64(addr, val)",
    "SwapUintptr": "return rtatomic.Xchguintptr(addr, new)",
    "CompareAndSwapUintptr": "return rtatomic.Casuintptr(addr, old, new)",
    "AddUintptr": "return rtatomic.Xadduintptr(addr, delta)",
    "AndUintptr": "return rtatomic.Anduintptr(addr, mask)",
    "OrUintptr": "return rtatomic.Oruintptr(addr, mask)",
    "LoadUintptr": "return rtatomic.Loaduintptr(addr)",
    "StoreUintptr": "rtatomic.Storeuintptr(addr, val)",
    "LoadPointer": "return rtatomic.Loadp(unsafe.Pointer(addr))",
    "StorePointer": "rtatomic.StorePointerWB(addr, val)",
    "SwapPointer": "return rtatomic.XchgPointer(addr, new)",
    "CompareAndSwapPointer": "return rtatomic.CasPointerWB(addr, old, new)",
}


def transform_decls(src):
    """Split bodyless func decls (with doc comments) from the rest of a file.

    Comment/blank lines preceding an extracted decl move with it into the
    wiring output; all other lines are kept verbatim.
    """
    lines = src.split("\n")
    wparts = []
    kept = []
    comments = []

    def flush_comments():
        kept.extend(comments)
        del comments[:]

    i = 0
    n = 0
    while i < len(lines):
        line = lines[i]
        m = re.match(r"^func (\w+)\((.*)\)( \(.*\))?$", line)
        bodyless = False
        if m:
            j = i + 1
            while j < len(lines) and lines[j].strip() == "":
                j += 1
            bodyless = j >= len(lines) or not lines[j].startswith("{")
        if m and bodyless:
            name = m.group(1)
            if name not in BODIES:
                print("FATAL: no wiring body for %s" % name)
                sys.exit(1)
            # //go:noescape / //go:linknamestd belonged to the asm bodies; the
            # formulations are ordinary Go functions, so drop those directives.
            doclines = [
                c for c in comments
                if not c.startswith("//go:noescape")
                and not c.startswith("//go:linknamestd")
            ]
            wparts.append("\n".join(doclines) + "\n" if doclines else "")
            wparts.append(
                "func %s(%s)%s {\n\t%s\n}\n\n"
                % (name, m.group(2), m.group(3) or "", BODIES[name])
            )
            n += 1
            del comments[:]
            i += 1
            continue
        if line.startswith("//") or line.strip() == "":
            comments.append(line)
        else:
            # package clause or any other real line: comments belong to it
            flush_comments()
            kept.append(line)
        i += 1
    flush_comments()
    kept_text = "\n".join(kept)
    # the Pointer decls (unsafe users) moved to wiring.go; drop a now-unused import
    if '"unsafe"' in kept_text and "unsafe." not in kept_text:
        kept_text = re.sub(r'(?m)^\s*import\s+"unsafe"\s*\n', "", kept_text)
        kept_text = re.sub(r'(?m)^\t"unsafe"\n', "", kept_text)
        kept_text = re.sub(r"(?m)^import \(\n\)\n", "", kept_text)
    wired_text = ""
    for block in wparts:
        wired_text += block
    # wparts holds raw comment lines; join each comment block properly
    return kept_text, wired_text, n


def main():
    if os.path.exists(STUB):
        shutil.rmtree(STUB)

    wrel("go.mod", "module %s\n\ngo 1.27\n" % MOD)

    # ---- internal support packages (pure Go copies) ----
    cpgo("internal/stringslite", "internal/stringslite")
    cpgo("internal/abi", "internal/abi")
    # internal/abi: only escape.go (pure Go); the rest is arch asm / compiler internals
    for fn in os.listdir(os.path.join(STUB, "internal/abi")):
        if fn != "escape.go":
            os.remove(os.path.join(STUB, "internal/abi", fn))
    for pkg in ["internal/testenv", "internal/cfg", "internal/goarch",
                "internal/platform"]:
        cpgo(pkg, pkg)
    # drop test files and generator mains from copied internal packages
    for root, _, files in os.walk(os.path.join(STUB, "internal")):
        for fn in files:
            if fn.endswith("_test.go"):
                os.remove(os.path.join(root, fn))
    gg = os.path.join(STUB, "internal/goarch", "gengoarch.go")
    if os.path.exists(gg):
        with open(gg) as f:
            if "package main" in f.read():
                os.remove(gg)
    # internal/asan: only the no-asan variant (pure Go); asan.go needs the asan runtime
    cpgo("internal/asan", "internal/asan")
    asan_go = os.path.join(STUB, "internal/asan", "asan.go")
    if os.path.exists(asan_go):
        os.remove(asan_go)

    # internal/runtime/gc: only the generated size-class tables (self-contained,
    # no imports); needed for a faithful MakeNoZero capacity formulation.
    os.makedirs(os.path.join(STUB, "internal/runtime/gc"), exist_ok=True)
    with open(os.path.join(GOROOT, "internal/runtime/gc/sizeclasses.go")) as f:
        wrel("internal/runtime/gc/sizeclasses.go", f.read())

    # ---- internal/runtime/atomic: formulations replace all .s ----
    adst = "internal/runtime/atomic"
    for fn in ["doc.go", "types.go", "types_64bit.go", "unaligned.go", "xchg8.go"]:
        with open(os.path.join(GOROOT, "internal/runtime/atomic", fn)) as f:
            src = f.read()
        if fn == "types.go":
            # Drop the bodyless casPointer declaration: upstream its body ships
            # in atomic_amd64.s; the stub provides the body in form.go.
            src = src.replace(
                "func casPointer(ptr *unsafe.Pointer, old, new unsafe.Pointer) bool\n",
                "",
            )
        wrel(os.path.join(adst, fn), rewrite_imports(src))
    with open(os.path.join(HARNESS, "atomic", "atomic.go")) as f:
        form = f.read()
    form = form.replace("package atomicform", "package atomic", 1)
    form += '''
// ---- spike additions: exact-width And/Or and write-barriered pointer ops ----

//go:nosplit
func Andint32(ptr *int32, val int32) int32 {
\treturn int32(And32((*uint32)(unsafe.Pointer(ptr)), uint32(val)))
}

//go:nosplit
func Orint32(ptr *int32, val int32) int32 {
\treturn int32(Or32((*uint32)(unsafe.Pointer(ptr)), uint32(val)))
}

//go:nosplit
func Andint64(ptr *int64, val int64) int64 {
\treturn int64(And64((*uint64)(unsafe.Pointer(ptr)), uint64(val)))
}

//go:nosplit
func Orint64(ptr *int64, val int64) int64 {
\treturn int64(Or64((*uint64)(unsafe.Pointer(ptr)), uint64(val)))
}

// StorePointerWB stores val into *addr atomically *with* a GC write barrier
// (typed unsafe.Pointer store, so gc emits the barrier). Used by the
// sync/atomic StorePointer / SwapPointer / CompareAndSwapPointer wiring.
func StorePointerWB(addr *unsafe.Pointer, val unsafe.Pointer) {
\tp := unsafe.Pointer(addr)
\tlock(p)
\tdefer unlock(p)
\t*addr = val
}

// XchgPointer swaps *addr with v atomically, with a write barrier.
func XchgPointer(addr *unsafe.Pointer, v unsafe.Pointer) (old unsafe.Pointer) {
\tp := unsafe.Pointer(addr)
\tlock(p)
\tdefer unlock(p)
\told = *addr
\t*addr = v
\treturn old
}

// CasPointerWB compares and swaps *addr atomically, with a write barrier.
func CasPointerWB(addr *unsafe.Pointer, old, new unsafe.Pointer) bool {
\tp := unsafe.Pointer(addr)
\tlock(p)
\tdefer unlock(p)
\tok := *addr == old
\tif ok {
\t\t*addr = new
\t}
\treturn ok
}

// casPointer is declared bodyless in types.go (its body ships in atomic_amd64.s
// upstream). Formulation with write-barrier semantics.
func casPointer(ptr *unsafe.Pointer, old, new unsafe.Pointer) bool {
\treturn CasPointerWB(ptr, old, new)
}
'''
    wrel(os.path.join(adst, "form.go"), form)

    # ---- internal/bytealg: formulations replace all asm ----
    with open(os.path.join(HARNESS, "bytealg", "bytealg.go")) as f:
        bform = f.read()
    bform = bform.replace("package bytealgform", "package bytealg", 1)
    bform = bform.replace(
        "package bytealg\n",
        "package bytealg\n\nimport gc \"graalgos01stub/internal/runtime/gc\"\n",
        1,
    )
    bform += '''
// ---- spike additions: symbols bytes/strings need beyond the difftest set ----

// CompareString compares two strings, returning -1/0/+1.
func CompareString(a, b string) int {
\treturn Compare([]byte(a), []byte(b))
}

// IndexRabinKarp returns the index of the first instance of sep in s.
// Formulation: naive O(n*m) search; the Rabin-Karp rolling hash in the real
// implementation is a performance optimization with identical semantics.
func IndexRabinKarp[T string | []byte](s, sep T) int {
\treturn Index([]byte(s), []byte(sep))
}

// LastIndexRabinKarp returns the index of the last instance of sep in s.
func LastIndexRabinKarp[T string | []byte](s, sep T) int {
\tsb, sepp := []byte(s), []byte(sep)
\tn := len(sepp)
\tif n == 0 {
\t\treturn len(sb)
\t}
\tif n > len(sb) {
\t\treturn -1
\t}
\tfor i := len(sb) - n; i >= 0; i-- {
\t\tif Equal(sb[i:i+n], sepp) {
\t\t\treturn i
\t\t}
\t}
\treturn -1
}

// MaxBruteForce mirrors the amd64 tuning constant: max input size for which
// bytes.Index calls bytealg.Index directly. Semantics-neutral threshold.
const MaxBruteForce = 64

// MaxLen mirrors the amd64 (AVX2) tuning constant: max needle length for the
// bytealg.Index path. Semantics-neutral threshold.
const MaxLen = 63

// Cutover reports the number of IndexByte failures to tolerate before
// switching to Index; n is bytes processed so far. Mirrors amd64 semantics.
func Cutover(n int) int {
\treturn (n + 16) / 8
}

// MakeNoZero returns a slice of length n. The real implementation
// (runtime.bytealg_MakeNoZero, linknamed from the runtime) skips zeroing and
// rounds the capacity UP to the allocator size class:
//
//	cap := roundupsize(uintptr(len), true)
//	return unsafe.Slice((*byte)(mallocgc(cap, nil, false)), cap)[:len]
//
// The "capacity of at least n" contract is load-bearing: strings.Builder.Grow
// relies on size-class rounding (TestBuilderGrowSizeclasses pins it). This
// formulation replicates the rounding with the runtime's own generated size
// class tables (internal/runtime/gc, copied verbatim). It returns zeroed
// memory, which is a conservative, always-correct instance of the "caller
// must overwrite before reading" contract.
func MakeNoZero(n int) []byte {
\tc := roundUpSizeClass(n)
\tb := make([]byte, c)
\treturn b[:n]
}

// roundUpSizeClass mirrors runtime.roundupsize(size, true) (noscan=true, the
// case for byte slices: no malloc header is added) using the generated tables.
func roundUpSizeClass(n int) int {
\tconst (
\t\tsmallSizeMax = 1024
\t\tsmallSizeDiv = 8
\t\tlargeSizeDiv = 128
\t\tmaxSmallSize = 32768
\t\tpageSize     = 4096 // runtime.pageSize on amd64
\t)
\tif n <= maxSmallSize-8 {
\t\tif n <= smallSizeMax-8 {
\t\t\treturn int(gc.SizeClassToSize[gc.SizeToSizeClass8[(n+smallSizeDiv-1)/smallSizeDiv]])
\t\t}
\t\treturn int(gc.SizeClassToSize[gc.SizeToSizeClass128[(n-smallSizeMax+largeSizeDiv-1)/largeSizeDiv]])
\t}
\treturn (n + pageSize - 1) &^ (pageSize - 1)
}
'''
    wrel("internal/bytealg/bytealg.go", bform)

    # ---- sync/atomic: doc.go + doc_64.go decls -> wiring.go ----
    syncdst = "sync/atomic"
    waccum = []
    total = 0
    for declfile in ["doc.go", "doc_64.go"]:
        with open(os.path.join(GOROOT, "sync/atomic", declfile)) as f:
            docsrc = f.read()
        kept, parts, n = transform_decls(docsrc)
        wrel(os.path.join(syncdst, declfile), kept)
        waccum.append(parts)
        total += n
    wiring = (
        "// Code generated by stub_build.py: sync/atomic wiring.\n"
        "// Every function below was declared bodyless in doc.go/doc_64.go\n"
        "// (body in asm.s upstream) and is implemented here by direct\n"
        "// delegation to the graalgos01stub/internal/runtime/atomic formulations.\n\n"
        "package atomic\n\n"
        "import (\n"
        '\trtatomic "graalgos01stub/internal/runtime/atomic"\n'
        '\t"unsafe"\n'
        ")\n\n" + "".join(waccum)
    )
    wrel(os.path.join(syncdst, "wiring.go"), wiring)
    print("wired %d sync/atomic functions" % total)

    for fn in ["type.go", "value.go", "doc_32.go",
               "atomic_test.go", "value_test.go", "example_test.go"]:
        with open(os.path.join(GOROOT, "sync/atomic", fn)) as f:
            src = f.read()
        if fn == "value.go":
            # Upstream, runtime pushes an implementation for the symbol
            # sync/atomic.runtime_procPin via
            #   //go:linkname sync_atomic_runtime_procPin sync/atomic.runtime_procPin
            # in runtime/proc.go. That push targets the std package path only,
            # so the stub links the same functions explicitly instead.
            src = src.replace(
                "// Disable/enable preemption, implemented in runtime.\n"
                "func runtime_procPin() int\n"
                "func runtime_procUnpin()",
                "// Disable/enable preemption, implemented in runtime.\n"
                "//go:linkname runtime_procPin runtime.procPin\n"
                "func runtime_procPin() int\n"
                "\n"
                "//go:linkname runtime_procUnpin runtime.procUnpin\n"
                "func runtime_procUnpin()",
            )
        wrel(os.path.join(syncdst, fn), rewrite_imports(src))

    # ---- bytes, strings (+ their tests) ----
    for pkg in ["bytes", "strings"]:
        srcdir = os.path.join(GOROOT, pkg)
        for fn in sorted(os.listdir(srcdir)):
            if not fn.endswith(".go"):
                continue
            with open(os.path.join(srcdir, fn)) as f:
                src = f.read()
            wrel(os.path.join(pkg, fn), rewrite_imports(src))

    wrel("README.md", """# graalgos01stub — stdlib packages with asm replaced by formulations

Mechanism: selected stdlib package trees are copied from GOROOT
(`~/tools/go`, go1.27.1) into this self-contained module. Every Plan-9 `.s`
file in the copied trees is DELETED and replaced by the spike's high-level
formulations:

| package | upstream asm | stub replacement |
|---|---|---|
| `internal/runtime/atomic` | 13 `.s` files (amd64) | `form.go`: striped-channel-lock emulation of every op |
| `internal/bytealg` | SIMD kernels | `bytealg.go`: pure-Go formulations |
| `sync/atomic` | `asm.s` (+`race.s`) trampolines | `wiring.go`: direct Go delegation to the atomic formulations |

All other imports still resolve to GOROOT (real runtime, testing, etc.).
`internal/abi` is reduced to `escape.go` (already pure Go upstream);
`internal/asan` to `noasan.go`. Test files are copied unmodified apart from
import-path rewrites into this module.

Run: `go test ./sync/atomic/ ./bytes/ ./strings/` (without -race: the race
build needs race.s trampolines into the race runtime, out of scope).
""")
    print("stub built at", STUB)


if __name__ == "__main__":
    main()
