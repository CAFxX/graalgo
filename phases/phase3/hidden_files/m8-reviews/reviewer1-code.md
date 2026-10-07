# Code Review — graalgo P3-M8 wave 2 (reviewer1)

**Reviewer:** independent adversarial reviewer (did not write the code)
**Tree:** `~/workspace/graalgo-m8-pipe`, branch `m8-pipeline` (read-only review)
**Date:** 2026-10-07
**Scope:** wave-2 implementation — `phases/phase1/frontend/synth_{io,iofs,os,syscall,unsafe,reflect,crypto*}.go`,
`phases/phase3/src/main/java/graalgo/p3os/GoOs.java`,
`phases/phase3/src/main/java/graalgo/p3unsafe/` (11 files),
`phases/phase3/src/main/java/graalgo/p3reflect/` (5 files),
corpus `phases/phase3/corpus/wave2/`, staged patches `m8-staging/`,
analysis `m8-analysis/` (SYNTHESIS.md, RULING.md R1–R13, per-package docs).

**Verdict: REQUEST_CHANGES**

Two P0s block the milestone: (1) `inexactOverlap` panics on the happy path, making the
entire symmetric-crypto surface (AES, DES, RC4, all cipher modes, GCM, subtle.XORBytes)
unusable — every `Encrypt`/`Decrypt`/`XORKeyStream` with distinct dst/src panics, and the
corpus KATs that claim PASS would panic; (2) `__fileReadFrom`/`__fileWriteTo` have
multi-result bodies the exporter emits unconditionally, breaking every program that
imports `os`. Nine P1s (silent syscall divergence, reflect VisibleFields gap, four
os-seek/errno misclassifications, a 3-way unlisted patch conflict, unsafe interning
non-idempotency) and eleven P2s follow. The hash/digest cores (md5/sha1/sha256/sha512,
sha3, HMAC incl. the marshaled fast path) are KAT-verified correct against gc 1.27.1 —
I compiled the synthetic sources under stock gc and ran NIST/RFC vectors; all pass.

---

## Methodology

Every finding was verified against the actual code — no phantom findings. Techniques used:

1. **Empirical gc probing** (stock go1.27.1, `~/tools/go/bin/go`): dir `Seek` after iteration,
   `ReadAt`/`WriteAt` on directories, `StringData("")`, `VisibleFields` on embedded `*T`,
   sha512/sha3 reference digests. Where my memory of a test vector was wrong I re-checked
   against gc rather than trusting myself (three of my memorized vectors were wrong; the
   code was right every time).
2. **gc KAT harness**: extracted the synthetic crypto sources verbatim from
   `synth_crypto_*.go`, compiled them under stock gc 1.27.1 in a scratch module
   (`~/workspace/tmp/kat`, test-only `Write→WriteSingle` delegation exactly as the mrM8
   desugar performs), and ran NIST/RFC KAT vectors plus reset/clone/marshal paths.
3. **Byte-level table diffs**: AES sbox0/sbox1 vs `crypto/internal/fips140/aes/const.go`
   (identical); sha256 marshal/unmarshal vs `crypto/internal/fips140/sha256/sha256.go`
   (verbatim port); gc `visiblefields.go` vs synthetic `VisibleFields`.
4. **Patch-conflict verification**: applied staged-patch combinations on a scratch copy
   with `git apply --check` (per AGENTS.md: machine-generated patches, coordinator
   re-verifies; I re-verified the worker's "pre-tested" claim and found it stale).
5. **Java review**: `GoOs.java` (full), `UnsafeTable`/`UnsafeFromPtrNode`/`UnsafeOps`
   (full), `ReflectOpNode`/`ReflectTypes`/`ReflectValues` (flagRO paths), generator
   `gen_synth_syscall.py` (full read, not just output sampling).

`TMPDIR=~/workspace/tmp` was used for all compiler invocations (the 512M `/tmp` tmpfs
fills under parallel builds; my own 506M `/tmp/m8review` scratch was removed afterward).

---

## P0 — must fix before sign-off

### P0-1. `inexactOverlap` panics on the happy path — entire symmetric-crypto surface unusable

**Files:**
- `phases/phase1/frontend/synth_crypto_aes.go` — `func inexactOverlap` (~line 775 in
  synthetic body); called from `(*Block).Encrypt`/`Decrypt`
- `phases/phase1/frontend/synth_crypto_cipher.go` — 5 call sites (CBC enc/dec, CTR, CFB, OFB)
- `phases/phase1/frontend/synth_crypto_cipher_gcm.go` — `Seal`/`Open` (`inexactOverlap(out, plaintext)` etc.)
- `phases/phase1/frontend/synth_crypto_des.go` — 4 call sites
- `phases/phase1/frontend/synth_crypto_rc4.go` — `XORKeyStream`
- `phases/phase1/frontend/synth_crypto_subtle.go` — `XORBytes`

**What's wrong:**
```go
func inexactOverlap(x, y []byte) bool {
	if len(x) == 0 || len(y) == 0 || &x[0] == &y[0] {
		return false
	}
	panic("crypto/aes: inexactOverlap: cannot verify disjointness without unsafe (re-test: unsafe draft)")
}
```
For any two **distinct** non-empty slices, `&x[0] == &y[0]` is false, so the function
panics. Every `Block.Encrypt(dst, src)`, `Decrypt`, `XORKeyStream`, `GCM.Seal/Open`,
and `subtle.XORBytes` calls `inexactOverlap(dst, src)` unconditionally — so the normal,
gc-legal case of disjoint dst/src buffers **always panics**. The function was meant to
detect *partial* overlap (gc panics `"crypto/aes: invalid buffer overlap"` only then);
instead it panics on the common path and never detects actual partial overlap.

**Why it matters:** AES, DES, RC4, every cipher mode, GCM, and `subtle.XORBytes` cannot
process a single block. The corpus programs that claim differential PASS all use
distinct buffers and would panic:
- `corpus/wave2/crypto/aes/01_fips197.go` (`c.Encrypt(ct, pt)`)
- `corpus/wave2/crypto/cipher/01_modes.go`, `02_gcm.go`, `03_gcm_adversarial.go`
- `corpus/wave2/crypto/subtle/01_xor.go` (`subtle.XORBytes(dst, a, b)` → expects `n == 64`)

I confirmed the panic fires under stock gc by compiling the synthetic aes verbatim:
`panic: crypto/aes: inexactOverlap: cannot verify disjointness without unsafe`. None of
these corpus programs carry a `.scopelimited`/`.divergent` marker, so the corpus
overstates what works. The analysis (`m8-analysis/crypto.md` §2) records this as a "HARD
DEPENDENCY on M8 unsafe track" with a "re-test: unsafe draft" trigger — but M8 **did**
land `unsafe` (synth_unsafe.go is in the tree, R10/R11 sequencing explicitly schedules
"overlap-panic-parity work AFTER the unsafe … draft is readable"). The follow-up was
never done.

**Suggested fix:** implement the overlap check with the now-available synthetic
`unsafe` package (`unsafe.SliceData` + range comparison), exactly the R10 follow-up.
Until then, mark every affected corpus program `.scopelimited` with the trigger, and do
not claim the cipher corpus passes. Note the fix must preserve gc's panic message
(`"crypto/aes: invalid buffer overlap"`) for genuine partial overlap.

### P0-2. `__fileReadFrom` / `__fileWriteTo` break every program that imports `os`

**File:** `phases/phase1/frontend/synth_os.go:2295` (`__fileReadFrom`), `:2318`
(`__fileWriteTo`).

**What's wrong:** both helpers have bodies that call multi-result functions
(`io.ReadAll`, `io.WriteString`) which are not in the synthetic single-result tables.
`exportSyntheticBodies` emits **all** single-result synthetic bodies unconditionally,
so any program importing `os` fails at export time. A sweep of all `__`-helpers in
synth_os.go confirmed these are the only two with this defect, but "only two" still
means the whole `os` package is unshippable.

**Suggested fix:** replace the bodies with fail-fast panics (named, with re-test
trigger), e.g. `panic("os: __fileReadFrom: multi-result io.ReadAll needs the mrM8
desugar (re-test: WS D)")`. These are cold paths (`ReadFrom`/`WriteTo` optimizations);
a loud trap is correct until the desugar covers them.

---

## P1 — high severity

### P1-1. Generator's `StringByteSlice` silently drops gc's NUL panic

**File:** `phases/phase3/hidden_files/m8-tmp/syscall/gen_synth_syscall.py`
(`StringByteSlice` template).

**What's wrong:** gc's `syscall.StringByteSlice` panics with
`"syscall: string with NUL passed to StringByteSlice"` when the string contains a NUL
byte. The generated synthetic version silently copies the bytes, truncating at nothing
and returning wrong data. The generator's comment claims "gc truncates" — this is
factually false; gc panics. This is a **silent divergence** (wrong data, no trap),
violating bar 2.

**Suggested fix:** inline the NUL check + panic with the exact gc message. Add a corpus
program asserting the panic (falsifiable: it must fail if the check is removed).

### P1-2. `reflect.VisibleFields` misses fields promoted through embedded `*T`

**File:** `phases/phase1/frontend/synth_reflect.go` (~line 2070, `VisibleFields`).

**What's wrong:** the synthetic descends into embedded fields only when
`f.Anonymous && ft.Kind() == Struct`. gc's `visiblefields.go` (go1.27.1, lines 94–101)
first dereferences embedded pointers:
```go
if f.Anonymous {
	if f.Type.Kind() == Pointer {
		f.Type = f.Type.Elem()
	}
	if f.Type.Kind() == Struct {
		w.walk(f.Type)
	}
}
```
A struct embedding `*T` (where `T` is a struct) therefore loses all of `T`'s promoted
fields in the synthetic `VisibleFields` output — wrong field list, wrong indices,
silent wrong data for any caller (e.g. struct-tag-driven serializers).

**Suggested fix:** mirror gc: dereference one pointer level before the struct-kind
check. Add a corpus program with `struct { *T; U }` asserting the full visible set.

### P1-3. `UnsafeTable.intern` not idempotent for scalar referents — `p1 != p2` for same pointer

**File:** `phases/phase3/src/main/java/graalgo/p3unsafe/UnsafeTable.java:142–153`
(`intern`), `:206–` (`normalizeScalar`).

**What's wrong:** the class javadoc (lines 24–27) documents the contract:
> `intern`: identity interning — the same referent object always yields the same
> handle, so `p1 := unsafe.Pointer(&x); p2 := unsafe.Pointer(&x); p1 == p2` is true
> (gc parity). … Bare scalar boxes (`Integer`, `Long`, … — what `new(T)` produces for
> scalar `T`) are normalized to single-element carrier arrays *before* interning.

But `normalizeScalar` allocates a **fresh** carrier array on every call:
```java
if (referent instanceof Integer i) {
    return new int[]{i.intValue()};   // fresh object every time
}
```
`interned` is an `IdentityHashMap`, so the fresh array never hits → every
`unsafe.Pointer(gp)` for `gp := new(int)` mints a new handle. Hence:
```go
gp := new(int)
p1 := unsafe.Pointer(gp)
p2 := unsafe.Pointer(gp)
fmt.Println(p1 == p2) // gc: true; graalgo: FALSE
```
This is exactly what corpus `wave2/unsafe/02_pointer_roundtrip.go` asserts (`p1 == p2`
→ true). `GoPointer.referent()` returns the same stored object each call
(`phases/phase1/src/main/java/graalgo/values/GoPointer.java:22-24`), so the
non-idempotency is entirely inside `normalizeScalar`. (`(*int)(p1)` round-trips happen
to work because they re-intern the already-normalized carrier.)

**Suggested fix:** cache the box→carrier mapping in a second `IdentityHashMap`
(populated on first normalize, cleared on `free`), so the same box always normalizes
to the same carrier. Re-test trigger: run corpus `02_pointer_roundtrip.go` and assert
`p1 == p2` is true.

### P1-4. Dir `Seek` after iteration returns EISDIR; gc succeeds

**File:** `phases/phase1/frontend/synth_os.go` (~line 1055, `dirIterStarted` logic).

**What's wrong:** after `Readdirnames` has started iteration, `Seek` on a directory
returns EISDIR. Probed against stock gc 1.27.1 (2026-10-07):
```go
f, _ := os.Open(dir)
f.Readdirnames(2)
off, err := f.Seek(5, 0)  // gc: off=5, err=nil
```
The synthetic returns an error. The code comment claims "verified go1.27.1" — that
claim is false.

**Suggested fix:** allow `Seek` on directories to set the offset (gc semantics: the
offset is just recorded; subsequent `Readdirnames` continues from the new offset —
verify that second half too). Correct the comment.

### P1-5. `GoOs.seek` DirHandle branch: three errno misclassifications

**File:** `phases/phase3/src/main/java/graalgo/p3os/GoOs.java` (`seek`, DirHandle branch).

**What's wrong** (all probed against gc 1.27.1):
1. `SEEK_END` on a directory returns `Files.size(dir) + off`. gc returns **EINVAL**
   (seek on directories: only `SEEK_SET` to 0 is meaningful; gc's `File.Seek` on a
   dir → `EINVAL` for SEEK_END).
2. Negative `off` with `SEEK_SET` returns the raw negative value, which the Go side
   misreads as `-EPERM`. gc returns **EINVAL** for negative offsets.
3. `SEEK_CUR` ignores the current position (returns `off` as if position were 0);
   gc returns current+off (and EINVAL on dirs for non-zero, but the position
   bookkeeping is still wrong for regular files if shared).

**Suggested fix:** map dir `SEEK_END`→EINVAL, negative offset→EINVAL at the Java
boundary (before the value crosses to Go), and thread the real current position
through `SEEK_CUR`.

### P1-6. `__fileReadAt` on a directory → ESPIPE; gc → EISDIR (probed)

**File:** `phases/phase1/frontend/synth_os.go` (`__fileReadAt`); errno mapping in
`GoOs.java`.

**What's wrong:** `ReadAt` on an `O_RDONLY` directory descriptor: gc returns
`EISDIR` (`read /tmp/d: is a directory`). The synthetic surfaces ESPIPE. Probed
2026-10-07 against stock gc.

**Suggested fix:** in the Java `pread` path (or the Go wrapper), detect directory
handles and return EISDIR for read-like syscalls, matching gc's
`syscall.Read`→EISDIR behavior on dirs.

### P1-7. `__fileWriteAt` on a directory → ESPIPE; gc (O_RDONLY dir) → EBADF (probed)

**File:** `phases/phase1/frontend/synth_os.go` (`__fileWriteAt`).

**What's wrong:** `WriteAt` on a directory opened `O_RDONLY`: gc returns `EBADF`
(bad file descriptor — write on a read-only fd). The synthetic surfaces ESPIPE.
Probed 2026-10-07. (Note: a directory opened `O_RDWR` — where permitted — would give
EISDIR on write; the O_RDONLY→EBADF case is the common one.)

**Suggested fix:** check the open mode before the handle kind: write syscalls on
`O_RDONLY` handles → EBADF regardless of kind; write syscalls on directories → EISDIR.

### P1-8. `Readdirnames`/`Readdir`/`ReadDir` return nil slices; gc guarantees non-nil

**File:** `phases/phase1/frontend/synth_os.go` (`__fileReaddirnames` and friends).

**What's wrong:** gc's `os` package guarantees non-nil empty slices from
`Readdirnames(-1)` on an empty directory (and `ReadDir` returns non-nil `[]DirEntry`).
The synthetic returns nil. Code that does `names[0]` panics identically, but
`reflect.DeepEqual(names, []string{})`, JSON marshaling (`null` vs `[]`), and
`len()`-plus-nil-check idioms diverge silently.

**Suggested fix:** return `[]string{}` / `[]DirEntry{}` (non-nil) on empty/error paths,
matching `os/dir.go`.

### P1-9. Unlisted 3-way staged-patch conflict (`isMultiResultSynthetic` tail + two gates)

**Files:** staged patches `m8-staging/io/03-*`, `m8-staging/os/01-*`,
`m8-staging/reflect/01-*`, all touching `phases/phase1/frontend/synth_machinery.go`.

**What's wrong:** three conflict sites are **not** listed in `INTEGRATION-PLAN.md`
(C1–C5):
1. the tail of `isMultiResultSynthetic` (io/03 × os/01 × reflect/01),
2. the `exportSyntheticPkgCall` gate (os/01 × reflect/01),
3. `multiResultSyntheticError` (3-way).

I verified with `git apply --check` on a scratch worktree (`~/workspace/tmp/m8review`):
applying io/03 then os/01 then reflect/01, the reflect/01 hunk **fails**. The
coordinator's "pre-tested" claim predates the reflect worker, so the integration plan
is stale. Per AGENTS.md ("Staged patches must be machine-generated; coordinator
re-verifies on scratch"), the coordinator must regenerate the combined diff and
re-run `git apply --check` itself — a worker's claim is not verification.

**Suggested fix:** regenerate the three patches via `git diff` on a scratch tree that
already contains the other two, in a fixed order (io → os → reflect), and update
INTEGRATION-PLAN.md's conflict list. Re-run the full combination pre-test.

---

## P2 — medium severity

### P2-1. `read` on a directory reached via symlink → EIO, gc → EISDIR
**File:** `GoOs.java` errno mapper. A symlink resolving to a directory yields EIO from
the mapper; gc returns EISDIR. Fix: resolve-then-kind check before errno mapping.

### P2-2. `SameFile` on the two ends of a pipe → false, gc → true
**File:** `synth_os.go` (`__sameFile`). gc's `SameFile` compares the underlying file
identity; both pipe ends share the pipe. The synthetic compares handle identity.
Fix: compare the pipe object identity for fifo/pipe handles.

### P2-3. `fileStat.Sys()` returns nil; gc returns `*syscall.Stat_t`
**File:** `synth_os.go` (`__fileStat`). Any caller doing `st.Sys().(*syscall.Stat_t)`
gets a nil-interface panic instead of stat data. Fix: populate a synthetic `Stat_t`
(via the already-generated syscall tables) or fail fast with a named trap.

### P2-4. Dangling-symlink parent → ENOENT; gc → ENOTDIR
**File:** `GoOs.java` path resolution. `open("dangling-link/x")` should be ENOTDIR
(a component of the path is not a directory); the synthetic returns ENOENT. Fix in
the symlink-following loop.

### P2-5. `environ()` returns sorted entries
**File:** `synth_os.go` / `GoOs.java`. Documented as by-design, but gc preserves OS
order; programs depending on `os.Environ()[0]` (rare but real) diverge silently.
Keep the behavior but add a loud doc comment + a `.divergent` corpus marker.

### P2-6. `hostname()` may block on DNS
**File:** `GoOs.java` (`hostname`). `InetAddress.getLocalHost()` can trigger a DNS
lookup and block for seconds. gc reads the kernel hostname without DNS. Fix: read
from `/proc/sys/kernel/hostname` (Linux) with fallback, never DNS.

### P2-7. No finalizer/cleanup for directory handles
**File:** `synth_os.go` / `GoOs.java`. `os.md` N1 not
...[truncated 4713 chars]