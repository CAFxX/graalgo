# ADR-0007 — P3-M6 Plan 9 assembly strategy: per-category dispositions and the fail-fast mechanism

**Date:** 2026-10-07
**Status:** accepted 2026-10-07 — P3-M6 implementation brief (workstream 3/5).
**Scope:** closes PLAN.md P3-M6 item "asm per-category strategy implemented
(evaporate/intrinsify/emulate-then-intrinsify/JVM-covers)". Implements SPEC
§2.8 (GG-0009) mechanically: the inventory
(`phases/phase3/stdlib-fork/asm-inventory-go1.27.1.{json,md}`), the
loader mechanism (`phases/phase3/src/p3frontend/asm.go`, hooked into
`Loader.localRef`/`Loader.mainRef`), and its tests.
**Normative basis:** SPEC §2.8; SPEC §3.7 (syscall/Panama); GG-0018
(netpoller deletion); GG-0023 (Linux-first); ADR-0004 (intrinsic surface,
linkname registry); P3-M4 intrinsic audit
(`phases/phase3/hidden_files/m4/intrinsic-audit.md`); research T4
(`research/T4-stdlib-reuse.md` §"What unchanged really requires").

## Context

The pinned toolchain (go1.27.1) ships **627 Plan 9 assembly files /
187,287 lines** under `~/tools/go-1.27.1/src` (SPEC §2.8 said 485 files /
~127k lines on the older toolchain it was written against — drift
recorded here, not carried: go1.27.1 grew the tree, notably
`crypto/internal/fips140`). graalgo cannot assemble Plan 9 assembly at
all, and before P3-M6 the Phase-3 importer *silently ignored* `.s`
files (`Loader.SelectFiles` only selects `.go`). Silent ignoring is the
failure mode this ADR eliminates: every buildable `.s` file in a
compiled package now resolves to exactly one of five dispositions, or
the build fails fast with an error naming the file, its package, its
category, and the reason.

## Decision

### D1. The five dispositions (SPEC §2.8, made mechanical)

| category | files | meaning |
|---|---|---|
| `evaporate` | 238 | runtime-core asm: no Go stacks on Truffle, the code has no meaning |
| `intrinsify` | 61 | atomics/bytealg: replaced by graalgo intrinsics |
| `emulate-then-intrinsify` | 71 | crypto: emulate leg now (pure-Go generic), intrinsify leg later |
| `jvm-math` | 54 | math package: covered by JVM IEEE754 |
| `unsupported` | 203 | fail-fast: build rejected with file+package+category+reason |

"Uncategorized" is not a sixth category — it is the *absence* of an
inventory entry and fails the build identically. The generator
(`gen-asm-inventory.py`) exits non-zero on any uncategorized file, so
toolchain drift surfaces at generation time.

### D2. What "evaporates" means, concretely

For each of the 238 runtime-core files (package `runtime` and its
subpackages, `internal/runtime/*` except `internal/runtime/atomic`,
`internal/abi`, `internal/reflectlite`, `internal/chacha8rand`,
`internal/cpu`, `internal/syscall/unix`, `runtime/debug`,
`os/signal/sig.s`, `syscall/js`): the file is *dropped from the build*.
Concretely:

1. The assembler's output — TEXT symbols manipulating `SP`/`PC`,
   `NOSPLIT` stack-growth prologues, `FUNCDATA`/`PCDATA` stack maps,
   `getg`/`g` access, morestack trampolines — has no referent: on
   Truffle there are no Go stacks, no gc ABI registers, no stack maps.
2. Every *Go-visible service* those files provided is already provided
   elsewhere, and the inventory's replacement pointer names it:
   - `runtime.fastrand`/`fastrandn` → `Intrinsics.fastrand(n)` via the
     ADR-0004 D1 linkname table (M4 audit: REAL);
   - `runtime.nanotime` → `Timers.nanotime` (same table);
   - `memequal`/`memmove` (incl. bytealg's linkname targets
     `memequal`, `memequal_varlen`, `cmpstring`) →
     `Intrinsics.memequal`/`memmove` (M4 audit: REAL);
   - scheduler/preemption/signal entry points → the P2 scheduler
     (`SchedulerCore`) and the JVM (signals); netpoller-adjacent asm is
     dead by GG-0018.
3. `internal/cpu`'s CPUID/xgetbv asm evaporates with one coupling the
   inventory records: crypto's emulate leg and bytealg read the
   Go-level `internal/cpu` package to select code paths, so the
   Go-level package needs a graalgo-provided host-CPU-feature model
   before the crypto wave (re-test trigger D6).

Evaporation is therefore not deletion-by-hope: each file's Go-visible
dependents are enumerated and their providers named. A runtime-core
`.s` file whose Go-visible symbol had *no* provider would be
`unsupported`, not `evaporate` — no such file was found.

### D3. Intrinsify mapping: atomics and bytealg

Cross-checked against the P3-M4 intrinsic audit (259 REAL, 0 true
stubs post-M4):

- **Atomics** (`sync/atomic/asm.s`, `internal/runtime/atomic/*`,
  15 files): `graalgo.runtime.Atomics` is REAL (audit: 16 public
  methods — int32/int64/uintptr/bool load/store/add/CAS/swap over
  `AtomicInteger`/`AtomicLong`), reachable from Go through the phase-3
  `AtomicOpNode` (`SyncOpNode` `atomic.*` namespace, ADR-0004 D2).
  `sync/atomic/asm.s`'s TEXT symbols are literal jumps into
  `internal∕runtime∕atomic` (verified by reading the file) — one
  intrinsic surface covers both. Differential vs gc on identical source
  (ADR-0004 D2 corpora).
- **bytealg** (46 files): `Intrinsics.memequal` is REAL (audit); the
  hot paths (memchr/memcmp) map to the planned ByteAlg vector nodes
  (T4: via Unsafe/vector APIs). Until those nodes exist, the pure-Go
  fallback is selectable: every bytealg op has a `*_generic.go`
  implementation, and excluding the arch `.s` (which is exactly what
  the mechanism does for a replaced category) selects it. The
  replacement pointer records both the current and the planned
  provider, so the gap is visible, not silent.

### D4. Crypto: the emulate leg now, the intrinsify leg later

All 71 `crypto/**` files (md5, sha1, `fips140/*` incl. the boringssl
marker stubs in `boring/sig` and `boring/bcache`, `x509/internal/macos`
darwin-only trampolines): the **emulate leg** compiles the pure-Go
generic implementation with the arch asm excluded — the mechanism's
disposition for a replaced category. The **intrinsify leg**
(per-algorithm SIMD intrinsics) is explicitly a later phase (Phase 5+,
per the program plan); it is not designed here. Re-test trigger (D6):
when the intrinsify leg lands, re-run the emulate-leg differential
corpora and re-verify every `emulate-then-intrinsify` inventory entry.
Parking the intrinsify leg without the trigger would be the "documented
without fixing" anti-pattern — the trigger is part of this decision.

### D5. `math` is covered by JVM IEEE754

All 54 `math/**` files (42 arch float kernels, 12 `math/big` kernels):

- The JVM implements IEEE754 *exactly*: `java.lang.StrictMath`
  (JLS 15: correctly-rounded, platform-independent) covers every
  `math` arch kernel's contract (Sqrt, Sin, Exp, …). There is no
  precision argument for keeping gc's kernels — StrictMath is the
  stronger guarantee, not an approximation.
- `math/big`'s asm kernels are speedups over mostly-Go code (T4);
  `java.math.BigInteger` semantics is the reference, with the Go
  fallback initially.

### D6. Re-test triggers (not a parking lot)

- Go toolchain upgrade: re-run `gen-asm-inventory.py`; any new or
  moved `.s` file fails the generator; re-verify category counts
  (pinned: 238/61/71/54/203 of 627).
- Crypto intrinsify leg lands: re-run emulate-leg differential
  corpora; re-verify the 71 entries.
- Wave-1 bring-up: decide `hash/crc32` (6 files) — intrinsify vs
  Go-fallback; wave-2: per-file strategy for `syscall` trampolines
  (39 files, SPEC §3.7 Panama FFI) and `reflect` stubs (14 files);
  `simd` API decision (2 files, experimental).
- `internal/cpu` Go-level implementation lands: re-verify the D2
  coupling note (crypto emulate-leg path selection).

### D7. The mechanism (fail-fast, never silent)

`Loader.CheckAsmFiles(dir, importPath)` (asm.go), called from
`Loader.localRef` and `Loader.mainRef` — the two points where a package
directory is accepted for compilation:

1. Selects the `.s` files gc would assemble for linux/amd64, using the
   *same* `goodOSArchFile` + `shouldBuild` filtering as `SelectFiles`
   (verified file-for-file against `go/build`'s `SFiles` on 12 real
   stdlib dirs — `TestAsmSelectionMatchesGoBuildOracle`).
2. Resolves each via `ResolveAsmFile` against the embedded inventory:
   replaced categories return an `AsmResolution{File, Package,
   Category, Replacement, Reason}` and the build proceeds;
   `unsupported`, outside-GOROOT, or uninventoried files fail fast:
   `p3frontend: asm fail-fast: <path> (package "<pkg>"): category
   "<category|uncategorized>": <reason>`.

The four payload fields are asserted by `assertAsmError` in the tests.
The completeness test re-enumerates `GOROOT/src` and requires the
enumerated set == the inventory key set (missing *or* stale entries
fail). Red-green evidence: `hidden_files/m6-asm/redgreen.md` (mutation
run: neutering the fail-fast turns `TestAsmCategoriesEndToEnd`,
`TestAsmUncategorizedFails`, `TestAsmHookInLoader` red with "build
proceeded silently").

## Alternatives considered (and rejected)

- **(a) Keep silently ignoring `.s` files:** rejected — the exact
  failure mode GG-0009's per-category strategy exists to prevent; a
  `hash/crc32` import would then compile without its accelerated
  kernel *or* an explicit decision, silently.
- **(b) Assemble Plan 9 asm via an assembler backend:** rejected —
  out of scope for the program (no Go stacks/ABI on Truffle to
  assemble *for*); the five dispositions cover every file without it.
- **(c) One "unsupported" bucket for all non-runtime asm (no
  intrinsify/emulate/math distinctions):** rejected — loses the
  decided strategy (SPEC §2.8) and the replacement pointers the
  bring-up waves need; the inventory is the waves' work list.
- **(d) Inventory as a hand-maintained list:** rejected — 627 files
  cannot be hand-audited on every toolchain upgrade; the generator's
  fail-on-uncategorized rule makes drift loud.

## Verification

- `go test -count=1 ./...` in `phases/phase3/src/p3frontend`: green
  (full pre-existing suite + 8 new `TestAsm*` tests); `go vet` clean.
- `gen-asm-inventory.py --check`: green (committed artifacts in sync).
- Red-green: `hidden_files/m6-asm/redgreen.md`.
- `verifies: GG-0009` (SPEC §2.8).

## M6 remediation note (2026-10-07; post-acceptance, does not re-open the decision)

Two independent reviews (PASS-with-findings) surfaced gaps in how this
ADR states the mechanism's boundaries. Recorded here explicitly:

1. **Hook coverage boundary -- M7 extension obligation.** The fail-fast
   hook lives in `Loader.localRef`/`Loader.mainRef` only (loader.go).
   The stdlib branch of `ResolveImport` -- the actual population with
   the inventoried assembly files -- is NOT hooked. That is correct for
   M1 scope (`kindStdlib` = resolvable, not compilable; the importer
   rejects stdlib compilation), but it is a boundary, not universal
   coverage. **Obligation:** when M7+ waves make stdlib packages
   compilable, the stdlib branch MUST call `CheckAsmFiles` (or its
   equivalent) and the backend MUST consume the `AsmResolution`
   replacement payloads. A wave-M7 worker reading only the Context/D1
   above would otherwise reasonably believe coverage is already
   universal.

2. **The "replace" half is inert in M1 (why the payload is carried).**
   Both hook sites currently discard the resolutions
   (`if _, err := l.CheckAsmFiles(...)`). The
   evaporate/intrinsify/emulate-then-intrinsify dispositions therefore
   do nothing beyond *not failing* today -- no downstream consumer acts
   on the replacement pointers. The payload is carried (not dropped)
   for two reasons: it is the contract the M7+ backend will consume
   (see obligation 1), and the tests pin it (per-file category +
   replacement pointer, `TestAsmCategoriesEndToEnd`). The reject half
   (fail-fast) is the live M1 contract; the replace half awaits M7.

3. **Inventory 627 -> 639 (supersedes the pinned counts above).** The
   remediation fixed the `.S`/`.sx` blind spot: the inventory now
   covers all three suffixes go/build treats identically as assembly.
   12 `.S` files were added (11 `runtime/cgo` `gcc_*.S`, 1 `cmd/cgo`
   test fixture), all categorized `unsupported`. The D1/D6 pinned
   counts are superseded: evaporate 238 / intrinsify 61 /
   emulate-then-intrinsify 71 / jvm-math 54 / **unsupported 215** of
   **639**. The D6 re-test triggers apply to the 639-file inventory.
