# Synthetic stdlib retirement tracker

This is the per-package record required by
[REPO_STRUCTURE.md](../REPO_STRUCTURE.md) principle 4 ("Stdlib honesty"):
for every synthetic package, the root cause that keeps the real stdlib
source from compiling, and what fixes it. It is the working document for
the no-bridges removal program; the phased order and the grandfather list
live in the [bridge inventory](bridge-inventory.md) (B-4). Every
`synth_*.go` file header links to its row here; the row is the
authoritative statement, the header link is the pointer.

Scope, counted at `d7298a00` (2026-10-30):

- **130** import paths registered in `syntheticSources`
  ([synth_machinery.go](../../internal/frontend/synth_machinery.go)).
- Plus: `path/filepath` (source present but unregistered),
  `math/rand` (reference stub superseded by the full port), `unsafe`
  (dedicated constructor, not a `syntheticSources` entry), the
  `reflect_m7` desugar helper, and `internal/graalgo/fmtstate`.
- Plus: 12 `p3frontend` mirrors (appendix A) and the retired packages
  (appendix B). Test files (`*_test.go`), `synth_machinery.go` itself,
  and the `synth_crypto_cipher_gcm.go` fragment (part of `crypto/cipher`,
  not a standalone package) carry no separate row.

## How to read a row

- **Blocker** — the root cause, in one of the families below. Where the
  file header states no per-package blocker beyond the family, the row
  says so explicitly instead of inventing one: such rows are marked
  **needs verification** — retirement still requires proving the real
  package compiles via the disk importer and passes its corpus.
- **Retires when** — the concrete trigger. A row is actionable only when
  its trigger is checkable (a landed frontend capability, a runtime
  surface, a corpus result), not "someday".
- **Trigger** — `pending` (trigger not yet satisfied), `FIRED — re-eval`
  (the trigger the file header named is already satisfied on disk, but
  nobody has re-evaluated the package since — these rows are the
  highest-value retirement candidates), or `n/a`.

## Blocker families

- **F-A — WS D multi-result.** The real sources use multi-result
  functions, methods, or 2-result interface calls, which frontend/v1
  cannot emit. The synthetic serves them via the Appendix-M pattern:
  single-result `__`-helpers returning pair structs plus desugar-table
  entries. Retires when the frontend emits multi-result values natively
  (WS D) and the `__`-helpers + desugar entries are deleted.
- **F-B — Frontend-construct adaptations.** The real sources use
  constructs frontend/v1 cannot express (generics the frontend cannot
  monomorphize in a synthetic, method values, func-typed fields holding
  method expressions, `*ast.StarExpr` assignment, the `max` builtin,
  `sync.OnceValue`, …). Generated files carry these as adaptation
  patches. Retires when the frontend supports the constructs and the
  adaptations/patches are deleted.
- **F-C — Asm / linkname / intrinsic surface.** The real sources need
  Plan 9 assembly, `//go:linkname` into the runtime, or runtime
  intrinsics (`runtime.memhash`, FIPS/boring plumbing, `internal/cpu`
  detection). Retires when the asm strategy (emulate-then-intrinsify),
  the linkname registry, or the intrinsic covers the package, or when
  the disk importer handles the package's pure-Go file selection.
- **F-D — Runtime-trap backed.** The real sources need runtime traps or
  JDK bindings (netpoller, poll server, timer heap, the `graalgo.os.*`
  linkname table, DNS resolution). Retires when the trap/linkname surface
  is complete and the real sources compile via the disk importer.
- **F-E — Reflect-dependent subset.** The real sources need `reflect`
  (struct codecs, `Marshal`/`Unmarshal`, `Slice*`, `As`/`AsType`). Real
  `reflect` landed via the disk importer on 2026-10-16, so every F-E row
  whose only blocker was reflect is **FIRED — re-eval**: the package
  must be re-tried against real reflect before any other work.
- **F-F — Bring-up synthetic, no specific blocker diagnosed.** A pure-Go
  package ported during wave bring-up for coverage speed. No
  per-package compile blocker is stated in its file header. Retires when
  the real package is proven to compile via the disk importer and pass
  its differential corpus — that verification is the work, not an
  assumption.
- **F-G — JDK-delegated.** The Go side delegates to the JDK
  (`java.security`) where semantics match, or needs a JDK-interop design
  for the remainder. Retires when the real package compiles via the disk
  importer with the JDK delegation design covering the remaining parts.
- **F-H — Graalgo-only support.** Not a stdlib package at all: a helper
  that exists only to serve synthetics (desugar tables, the `fmt.State`
  stand-in, the `unsafe` constructor). Retires with its consumer.

## Core runtime packages

### time

- File: `internal/frontend/synth_time_context.go` (with `context`).
- Blocker: **F-D.** Clock/timer entry points (`Now`, `Sleep`, `After`,
  `Tick`, `NewTimer`, `NewTicker`, `AfterFunc`) are exporter-intercepted
  intrinsics lowered to `timenew` wire nodes over the Java timer heap;
  the real `time` sources need that runtime plus wall-clock
  decomposition of `Time` values. Calendar decomposition members are
  currently fail-fast panics (lane L02, review R0-2, owns that gap).
- Retires when: the real `time` compiles via the disk importer with
  timer/clock runtime support, and the `timenew` interception is deleted.
- Trigger: pending.

### context

- File: `internal/frontend/synth_time_context.go` (with `time`).
- Blocker: **F-D.** Real `context` needs goroutine-free cancellation
  propagation; the synthetic implements the channel-mutex design
  (cancelCtx with done/cancelCh/muChan/children, `timerCtx` via
  `time.AfterFunc`) plus Appendix-M desugar for the multi-result
  operations.
- Retires when: the real `context` compiles via the disk importer on a
  runtime that supports its cancellation/timer behavior.
- Trigger: pending.

### errors

- File: `internal/frontend/synth_errors.go`.
- Blocker: **F-E + F-A.** `As`/`AsType` need the reflect intrinsics
  (via the synthetic `internal/reflectlite` adapter); multi-result
  members are desugared.
- Retires when: real `errors` compiles via the disk importer with
  working `As`/`AsType` over real reflect.
- Trigger: **FIRED — re-eval** (real `reflect` and real
  `internal/reflectlite` both landed via the disk importer, 2026-10-16).

### os

- File: `internal/frontend/synth_os.go`.
- Blocker: **F-D.** The real `os` needs the `graalgo.os.*` linkname
  table (~40 intrinsics, FROZEN namespace) bound by `GoOs.java` over
  host JVM APIs, plus the fail-fast surface documented in the restored
  [M8 os analysis](phase3-analysis/m8-analysis/os.md) (§6).
- Retires when: the real `os` compiles via the disk importer with the
  full `graalgo.os.*` surface (file/dir/stat/path/env/argv/identity
  plus the §6 fail-fast members given real bodies).
- Trigger: pending.

### syscall

- File: `internal/frontend/synth_syscall.go`.
- Blocker: **F-D.** Vocabulary + scalar services only (errno/`SYS_*`/
  `O_*`/`S_*`/`DT_*` constants, struct types, `Errno`/`Signal`/
  `WaitStatus`, scalar wrappers binding the same `graalgo.os.*` table
  as `os`). FD/socket types and the syscall trampolines are fail-fast.
- Retires when: the full `syscall` surface (FD/socket types, trampolines)
  lands behind the `graalgo.os.*` table, or real `syscall` compiles via
  the disk importer.
- Trigger: pending.

### io

- File: `internal/frontend/synth_io.go`.
- Blocker: **F-A.** Full public API signatures; real implementations
  where v1 can express them, fail-fast bodies elsewhere. Multi-result
  members with real `__`-helpers desugar at 2-target call sites; every
  other multi-result position fails fast.
- Retires when: WS D lands (F-A) and the real `io` compiles via the
  disk importer.
- Trigger: pending.

### io/fs

- File: `internal/frontend/synth_iofs.go`.
- Blocker: **F-F + F-A.** Minimal core only (types, sentinels, `FS`
  interface stub); everything else (`Glob`, `ReadFile`, `WalkDir`, …)
  was deferred to ride on the synthetic `os` package. The header's
  re-test note pointed at the restored
  [M8 os analysis](phase3-analysis/m8-analysis/os.md) §6 (the header
  cited a §6.8 that does not exist in that document; the citation now
  points at §6).
- Retires when: the full `io/fs` compiles via the disk importer.
- Trigger: pending (the `os` landing it waited on has happened, but the
  full-`io/fs` re-evaluation has not been recorded).

### unsafe (dedicated stub)

- File: `internal/frontend/synth_unsafe.go` (constructor, not a
  `syntheticSources` entry).
- Blocker: **F-H.** `unsafe` members are compiler intrinsics:
  `go/types` recognizes them as builtins, so the frontend inherits
  gc-exact checking. `unsafe.Add` always traps (inventory B-6).
- Retires when: a byte-addressed value model (or documented trap
  contract) resolves `unsafe.Add`, and the remaining builtins lower
  without the dedicated constructor.
- Trigger: pending.

### reflect_m7

- File: `internal/frontend/synth_reflect.go` (the `reflect_m7`
  registration; the `reflect` and `internal/reflectlite` registrations
  themselves are retired — appendix B).
- Blocker: **F-H.** The M7 desugaring table still needs the
  `__`-helpers and pair types from the retired `synth_reflect.go`
  source, registered under the distinct name `reflect_m7` so
  `getSyntheticPackage` works.
- Retires when: the M7 desugar tables are deleted (WS D) and no
  synthetic package references the helpers.
- Trigger: pending.

### internal/graalgo/fmtstate

- File: `internal/frontend/synth_fmtstate.go`.
- Blocker: **F-H.** Graalgo-only support package: the `fmt.State`
  implementation type (`PrinterState`) whose methods are implemented in
  Java (`FmtHandleMethods`) and installed into the method table at
  launcher startup. It exists so the descriptor name is stable for the
  real `fmt` package.
- Retires when: the Java `fmt` method handling no longer requires the
  Go-side `PrinterState` type.
- Trigger: pending.

### iter

- File: `internal/frontend/synth_reflect.go` (with the retired reflect
  sources).
- Blocker: **F-H.** Minimal generic sequence declarations only
  (`Seq`/`Seq2`), so exact gc signatures mentioning them type-check.
  The methods are fail-fast.
- Retires when: the real `iter` compiles via the disk importer
  (generic sequence support) — the header's own re-test trigger is
  "iter package bring-up".
- Trigger: pending.

### structs

- File: `internal/frontend/synth_structs.go`.
- Blocker: **F-F.** Marker type only, exact gc 1.27.1 API. No
  per-package compile blocker stated in the header.
- Retires when: the real `structs` compiles via the disk importer.
- Trigger: pending.

### unique

- File: `internal/frontend/synth_unique.go`.
- Blocker: **F-B.** Generic canonicalization (`Unique[T]`): the real
  package is generic and the synthetic is the monomorphization-safe
  port.
- Retires when: the real `unique` compiles via the disk importer with
  generic support proven for it.
- Trigger: pending.

### cmp

- File: `internal/frontend/synth_cmp.go`.
- Blocker: **F-F.** Verbatim gc `cmp.go`, self-contained, no imports,
  no intrinsics needed. No per-package compile blocker stated.
- Retires when: the real `cmp` compiles via the disk importer.
- Trigger: pending.

### slices

- File: `internal/frontend/synth_slices.go`.
- Blocker: **F-B + F-C.** Generic algorithms adapted from gc:
  `math/bits` replaced by local helpers, unsafe-based
  overlaps/start-index replaced by value snapshots (pointer-to-uintptr
  conversion is the D5 fail-fast trap), imports synthetic `cmp` and
  `iter`.
- Retires when: the real `slices` compiles via the disk importer
  (generic + unsafe-overlap support) and the adaptations are deleted.
- Trigger: pending.

### maps

- File: `internal/frontend/synth_maps.go`.
- Blocker: **F-B + F-C.** Adapted from gc; the linkname'd `clone` is
  replaced by a pure-Go `Clone`; imports synthetic `iter`.
- Retires when: the real `maps` compiles via the disk importer and the
  linkname replacement is no longer needed.
- Trigger: pending.

### internal/bytealg

- File: `internal/frontend/synth_bytealg.go`.
- Blocker: **F-C.** The real package's SIMD/assembly fast paths
  (`compare_amd64.s` etc.) are replaced by the portable `*_generic.go`
  algorithms; `MakeNoZero` differs observably only outside its
  documented contract; the `offsetPPC64*`/`offsetX86*` consts
  (`unsafe.Offsetof` into `internal/cpu` records) are omitted for lack
  of an assembly consumer.
- Retires when: the asm strategy covers `internal/bytealg`, or a
  synthetic `internal/cpu` lands (the header's own re-test trigger for
  the omitted consts).
- Trigger: pending.

### internal/byteorder

- File: `internal/frontend/synth_byteorder.go`.
- Blocker: **F-F.** Verbatim gc `byteorder.go`, pure Go, no imports.
  Needed by `math/big`'s marshaling paths. No per-package compile
  blocker stated.
- Retires when: the real `internal/byteorder` compiles via the disk
  importer.
- Trigger: pending.

### math

- File: `internal/frontend/synth_math.go`.
- Blocker: **F-C.** Pure-Go functions are gc ports; where gc's amd64
  path uses arch assembly, the synthetic carries a transliteration of
  the exact amd64 algorithm (`Exp` FMA path, `Log`, `Floor`/`Ceil`/
  `Trunc` with sNaN quieting, …).
- Retires when: the asm strategy covers `math`, or the disk importer
  handles the pure-Go selection.
- Trigger: pending.

### math/bits

- File: `internal/frontend/synth_mathbits.go` (generated by
  `gen_synth_mathbits.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus frontend
  adaptations (reconstructed overflow/divide sentinels, imports only
  the synthetic `errors` package).
- Retires when: the frontend supports the adapted constructs and the
  real `math/bits` compiles via the disk importer.
- Trigger: pending.

### math/cmplx

- File: `internal/frontend/synth_mathcmplx.go` (generated by
  `gen_synth_mathcmplx.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source; imports the synthetic
  `math` and `math/bits` packages.
- Retires when: the real `math/cmplx` compiles via the disk importer
  (with real `math`/`math/bits`).
- Trigger: pending.

### math/big

- File: `internal/frontend/synth_math_big.go`.
- Blocker: **F-C + F-B.** The stdlib's own `math_big_pure_go` pure-Go
  fallback is used for the nat asm kernels, so no assembly is involved;
  plus frontend adaptations (see the file header). The asymmetric
  crypto tier is the G-c hard gate that depends on this package.
- Retires when: the real `math/big` compiles via the disk importer
  (pure-Go selection proven, or asm kernels covered).
- Trigger: pending.

### math/rand

- File: `internal/frontend/synth_mathrand.go` (generated full port by
  `gen_synth_mathrand.py`).
- Blocker: **F-B.** Full port; uses a mutex-guarded global instead of
  `atomic.Pointer` (the frontend cannot monomorphize a synthetic
  generic helper there); strict superset of the former minimal stub.
- Retires when: the real `math/rand` compiles via the disk importer.
- Trigger: pending.

### math/rand (reference stub)

- File: `internal/frontend/synth_math_rand.go`.
- Blocker: **n/a — not a live package.** The minimal
  `syntheticMathRandSource` stub, superseded by the full port above
  and kept on disk for reference (per the `synth_machinery.go`
  comment). It is not registered in `syntheticSources`.
- Retires when: deleted as a reference file once the full port's history
  makes it unnecessary. Docs-only lane does not delete code; this row
  records the status so a later lane can act.
- Trigger: n/a.

### math/rand/v2

- File: `internal/frontend/synth_mathrandv2.go` (generated by
  `gen_synth_mathrandv2.py`).
- Blocker: **F-B.** Full port; imports the synthetic `math`,
  `math/bits`, and `internal/byteorder` packages.
- Retires when: the real `math/rand/v2` compiles via the disk importer
  (with real `math`/`math/bits`/`internal/byteorder`).
- Trigger: pending.

### sort

- File: `internal/frontend/synth_sort.go`.
- Blocker: **F-A + F-E.** `Find` has two results (WS D — declaration
  skipped at export, call sites fail fast); `Slice`/`SliceStable`/
  `SliceIsSorted` need reflect (exact signatures, fail-fast bodies).
  The rest of the API is expressible in frontend/v1 per the header.
- Retires when: WS D lands (for `Find`) and the reflect-based members
  are re-tried against real reflect.
- Trigger: **FIRED — re-eval** for the reflect members (real `reflect`
  landed 2026-10-16); pending for `Find` (WS D).

### strconv

- File: `internal/frontend/synth_strconv.go`.
- Blocker: **F-A.** Full public API, pure-Go, no imports; fail-fast
  bodies for the multi-result declarations (desugared).
- Retires when: WS D lands and the real `strconv` compiles via the
  disk importer.
- Trigger: pending.

### strings

- File: `internal/frontend/synth_strings.go`.
- Blocker: **F-A.** Full in-scope API, pure-Go, no imports; fail-fast
  bodies for out-of-scope members. Case mapping/folding use the
  unicode `CaseRanges`/`asciiFold`/`caseOrbit` tables inlined in the
  file — the untracked `m7-strings/gen_tables.py` generator that
  produced them is gone, so the tables are committed inline and cannot
  be regenerated (see the
  [phase3-analysis README](phase3-analysis/README.md)); the restored
  [m7-strings red-green evidence](phase3-analysis/m7-strings/redgreen.md)
  is the remaining audit trail.
- Retires when: WS D lands and the real `strings` compiles via the
  disk importer; the inlined tables go away with the file.
- Trigger: pending.

### bytes

- File: `internal/frontend/synth_bytes.go`.
- Blocker: **F-A.** Single-result members are real clean-room ports of
  gc; multi-result members keep exact signatures with fail-fast bodies
  (WS D); `Buffer.ReadFrom`/`WriteTo`/`Reader.WriteTo` need 2-result
  interface calls (WS D). 8 members are omitted outright (need
  `iter.Seq` / `unicode.SpecialCase`). The unicode/utf8 codec and case
  tables are generated by exhaustive enumeration over gc's unicode
  package — the untracked `m7-bytes/gen_tables.go` generator is gone,
  so the tables are committed inline and cannot be regenerated; the
  restored [m7-bytes red-green evidence](phase3-analysis/m7-bytes/redgreen.md)
  is the remaining audit trail. (The header's `m7-bytes/linkname-routing.md`
  citation was never a tracked file and is not restored.)
- Retires when: WS D lands, real `iter`/`unicode` land via the disk
  importer, and the real `bytes` compiles via the disk importer.
- Trigger: pending.

### bufio

- File: `internal/frontend/synth_bufio.go` (generated by
  `gen_synth_bufio.py` from gc 1.27.1, plus the `gen_synth_adapt`
  patch).
- Blocker: **F-B + F-A.** DEVIATION 1: `unicode/utf8` helpers inlined
  as `__bufio*` helpers (the overlay lacked `unicode/utf8` at the
  time). DEVIATION 2: multi-result methods served via Appendix-M
  `__`-helpers (WS D). Custom `SplitFunc` panics (lane L13, review
  R1-19, owns that gap).
- Retires when: the inlined utf8 helpers are deleted (trigger fired —
  see below) and WS D lands for the multi-result methods.
- Trigger: **FIRED — re-eval** for DEVIATION 1 (`unicode`/`unicode/utf8`
  are registered synthetics); pending for DEVIATION 2 (WS D).

### path

- File: `internal/frontend/synth_path.go` (generated by
  `gen_synth_path.py` from gc 1.27.1).
- Blocker: **F-A.** `Match`/`scanChunk`/`matchChunk`/`getEsc` are
  DROPPED (3-result functions + `utf8.DecodeRuneInString`); only
  `ErrBadPattern` is ported. A program calling `path.Match` fails at
  compile time with "undefined" (lane L13, review R1-18, owns the port
  or a fail-fast stub).
- Retires when: WS D lands and the `Match` family is ported, or L13's
  fail-fast stub lands.
- Trigger: **FIRED — re-eval** (the header's re-test trigger,
  `unicode/utf8` in the overlay, has landed).

### path/filepath (unregistered)

- File: `internal/frontend/synth_filepath.go`.
- Blocker: **n/a — not registered.** The source is present (linux-only
  port of `path/filepath/{path,path_unix,match,symlink}.go` plus the
  unix branch of `internal/filepathlite`; Windows/Plan9 branches out of
  scope per GG-0023), but `syntheticFilepathSource` is referenced
  nowhere outside its own file and `path/filepath` is absent from
  `syntheticSources`. The real `path/filepath` presumably resolves via
  the disk importer when admitted — but that wiring is not documented
  anywhere on disk.
- Retires when: a lane confirms the wiring (disk-imported, with the
  synthetic source dead) and deletes the file, or registers it if it
  is actually needed. Docs-only lane does not delete code; this row
  records the anomaly.
- Trigger: n/a.

### flag

- File: `internal/frontend/synth_flag.go`.
- Blocker: **F-B + F-A.** Real-Go port of gc `flag.go` with documented
  behavior-preserving adaptations: `sortFlags` uses `sort.Sort` over a
  `sort.Interface` (`slices.SortFunc` is not in synthetic scope);
  duration parsing is a self-contained port of `time.ParseDuration`
  (synthetic `time.ParseDuration` fail-fasts — re-test trigger: time
  multi-result desugar); `TextVar` uses the synthetic `encoding`
  package's `TextMarshaler`/`TextUnmarshaler` interfaces.
- Retires when: the real `flag` compiles via the disk importer and the
  adaptations are deleted.
- Trigger: pending.

### testing

- File: `internal/frontend/synth_testing.go`.
- Blocker: **F-B.** Faithful port of gc `testing.go`/`match.go`/
  `benchmark.go` (scoped)/`example.go`/`fuzz.go` (fail-fast), adapted
  to the synthetic scope (full `T`/`B`/`M`/`TB` API, `-test.*` flags).
- Retires when: the real `testing` compiles via the disk importer
  (including the runtime test hooks it needs).
- Trigger: pending.

### testing/quick

- File: `internal/frontend/synth_testing_quick.go`.
- Blocker: **F-B + F-E.** Ported to the synthetic scope; the real
  package needs `reflect` for arbitrary-value generation.
- Retires when: the real `testing/quick` compiles via the disk importer
  with working reflect support.
- Trigger: **FIRED — re-eval** (real `reflect` landed 2026-10-16).

### testing/iotest

- File: `internal/frontend/synth_testing_iotest.go`.
- Blocker: **F-A.** Ported to the synthetic scope over the synthetic
  `io` package; multi-result members desugared.
- Retires when: WS D lands and the real `testing/iotest` compiles via
  the disk importer.
- Trigger: pending.

### text/scanner

- File: `internal/frontend/synth_textscanner.go` (generated by
  `gen_synth_textscanner.py` from gc 1.27.1).
- Blocker: **F-B.** DEVIATION 1: `unicode/utf8` helpers inlined as
  `__ts*` helpers. DEVIATION 2: `unicode.IsLetter`/`IsDigit` are
  ASCII-only checks — non-ASCII letters/digits are silently
  mis-tokenised (lane L03, review R0-3, owns the fix; the header's
  retest trigger, "full unicode tables land, tier1a", has arguably
  fired since `unicode`/`unicode/utf8` are registered synthetics).
- Retires when: the helpers are wired to the synthetic `unicode`
  tables (after L03 verifies those tables are complete) and the real
  `text/scanner` compiles via the disk importer.
- Trigger: **FIRED — re-eval** (unicode tables present as synthetics;
  completeness unverified — L03's job).

### regexp/syntax

- File: `internal/frontend/synth_regexpsyntax.go` (generated by
  `gen_synth_regexpsyntax.py` from gc 1.27.1).
- Blocker: **F-F.** Gc's sources concatenated verbatim, one import
  block per file. No per-package compile blocker stated in the header
  beyond the generated-file mechanism.
- Retires when: the real `regexp/syntax` compiles via the disk importer.
- Trigger: pending.

## Unicode

### unicode

- File: `internal/frontend/synth_unicode.go`.
- Blocker: **F-F.** Adapted gc source; the tables are the package's
  content. No per-package compile blocker stated beyond the wave-3
  bring-up.
- Retires when: the real `unicode` compiles via the disk importer.
- Trigger: pending.

### unicode/utf8

- File: `internal/frontend/synth_unicode_utf8.go`.
- Blocker: **F-F.** Verbatim gc `utf8.go`, self-contained, no imports.
- Retires when: the real `unicode/utf8` compiles via the disk importer.
- Trigger: pending.

### unicode/utf16

- File: `internal/frontend/synth_unicode_utf16.go`.
- Blocker: **F-F.** Adapted gc source, same pattern as `unicode/utf8`.
- Retires when: the real `unicode/utf16` compiles via the disk importer.
- Trigger: pending.

## Net family (Phase-1 in-flight: `net`; `net/http` retired)

### net

- File: `internal/frontend/synth_net.go`.
- Blocker: **F-D.** Socket path wired to the netpoller runtime traps
  per the restored [net fork contract](phase3-analysis/m9-tmp/net-fork-contract.md):
  netFD on the trap linknames + `internal/poll` FD for deadlines;
  Dial/Listen/Accept over loopback TCP; UDP via sendto/recvfrom; DNS
  functions are fail-fast (no resolver in M9). `os.ErrDeadlineExceeded`
  is a plain `errors.New` without `Timeout()` (gc-divergent, documented
  in the header).
- Retires when: a DNS resolver lands, the full socket surface is proven
  over the trap linknames, and the real `net` compiles via the disk
  importer. This is one of the two genuinely in-flight Phase-1 packages
  (inventory B-4).
- Trigger: pending.

### net/netip

- File: `internal/frontend/synth_netip.go`.
- Blocker: **F-A.** Adapted gc `netip.go` + `uint128.go`; every
  multi-result function reachable from the corpus surface is renamed to
  a `__`-form returning a pair/quad struct, desugared at call sites.
- Retires when: WS D lands and the real `net/netip` compiles via the
  disk importer.
- Trigger: pending.

### net/url

- File: `internal/frontend/synth_neturl.go`.
- Blocker: **F-A.** Adapted gc `url.go`; multi-result functions renamed
  to `__`-forms returning pair/triple structs, desugared at call sites.
- Retires when: WS D lands and the real `net/url` compiles via the
  disk importer.
- Trigger: pending.

### net/textproto

- File: `internal/frontend/synth_textproto.go`.
- Blocker: **F-A.** Pure parts only (`MIMEHeader`,
  `CanonicalMIMEHeaderKey`, `TrimString`, the full `Reader` type);
  `Writer`/`Conn`/`Pipeline`/`DotReader` are NOT ported (re-test when
  needed). `canonicalMIMEHeaderKey` is desugared via a `__`-form.
- Retires when: WS D lands, the remaining types are ported, and the
  real `net/textproto` compiles via the disk importer.
- Trigger: pending.

### internal/poll

- File: `internal/frontend/synth_poll.go`.
- Blocker: **F-D.** Adapted gc `fd_poll_runtime.go` + `errno_unix.go`
  per the restored [net fork contract](phase3-analysis/m9-tmp/net-fork-contract.md):
  logic unmodified, but the declaration block is replaced with
  single-result `//go:linkname` declarations (v1 rejects multi-result
  declarations even for linknames); `pollDesc` follows the cookie
  model (`runtime_pollOpen` takes a socket cookie, returns a pollDesc
  cookie, errno via thread-local `runtime_pollOpenErrno`); the gc FD
  type is replaced by a minimal synthetic FD.
- Retires when: the real `internal/poll` compiles via the disk importer
  with the trap linknames proven (retires with `net`).
- Trigger: pending.

### internal/nettrace

- File: `internal/frontend/synth_nettrace.go`.
- Blocker: **F-F.** Verbatim gc `nettrace.go`, no imports, no
  multi-result functions. Imported (later) by `net/http`/`httptrace`;
  the synthetic `net` package does not trace (trace points omitted, not
  faked).
- Retires when: the real `internal/nettrace` compiles via the disk
  importer.
- Trigger: pending.

### internal/singleflight

- File: `internal/frontend/synth_singleflight.go`.
- Blocker: **F-A.** Adapted gc `singleflight.go`; the frontend cannot
  invoke a multi-result func VALUE (the Appendix-M desugar covers only
  named functions in its tables), so `Do`/`DoChan`/`doCall` keep exact
  gc signatures with fail-fast bodies while `ForgetUnshared` is fully
  working.
- Retires when: WS D covers multi-result func-value invocation and the
  real `internal/singleflight` compiles via the disk importer.
- Trigger: pending.

## Encoding family

### encoding

- File: `internal/frontend/synth_encoding.go`.
- Blocker: **F-F.** The six gc 1.27.1 marshaling interfaces, verbatim.
- Retires when: the real `encoding` compiles via the disk importer.
- Trigger: pending.

### encoding/ascii85

- File: `internal/frontend/synth_ascii85.go`.
- Blocker: **F-A.** Real encode/decode core; multi-result `Decode`
  desugared; the io-dependent streaming API (`NewEncoder`/`NewDecoder`)
  is omitted (io was out of M7 scope).
- Retires when: WS D lands and the streaming API is declared over the
  synthetic core (io has since landed — see trigger).
- Trigger: **FIRED — re-eval** (the header's trigger, "io lands
  (P3-M8)", has happened; the omission has not been revisited).

### encoding/asn1

- File: `internal/frontend/synth_asn1.go`.
- Blocker: **F-E + F-A.** Real-Go DER core for the reflect-free subset
  (type switches); structs, slices (other than `[]byte`), maps,
  `big.Int`, `time.Time`, and named types need reflect and fail fast;
  `MarshalWithParams`/`UnmarshalWithParams` need reflect for params
  interpretation. Multi-result `Marshal` desugared; `Unmarshal` has no
  desugar (decoding stores through the recipient pointer, which v1
  cannot express — WS D).
- Retires when: the reflect-dependent surface is re-tried against real
  reflect and WS D lands for `Unmarshal`.
- Trigger: **FIRED — re-eval** for the reflect surface (real `reflect`
  landed 2026-10-16); pending for `Unmarshal` (WS D).

### encoding/base32

- File: `internal/frontend/synth_base32.go`.
- Blocker: **F-A.** Real encode/decode core; multi-result members
  desugared; streaming `Read`/`Write`/`Close` stay fail-fast (they need
  2-result interface calls — WS D). Constructors are real since io
  landed (P3-M8).
- Retires when: WS D lands (2-result interface dispatch) and the real
  `encoding/base32` compiles via the disk importer.
- Trigger: pending.

### encoding/base64

- File: `internal/frontend/synth_base64.go`.
- Blocker: **F-A.** Real encode/decode core (streaming encoder/decoder
  real over the synthetic core since io landed in P3-M8); multi-result
  members desugared, including `(*encoder).Write` /
  `(*decoder).Read`.
- Retires when: WS D lands and the real `encoding/base64` compiles via
  the disk importer.
- Trigger: pending.

### encoding/binary

- File: `internal/frontend/synth_binary.go`.
- Blocker: **F-E + F-A.** Real byte-order/varint core (io- and
  reflect-free); the reflect fallback for arrays/structs/named types
  fails fast; `Append`/`Encode`/`Decode` need reflect; the io streaming
  API (`Read`/`Write`/`ReadUvarint`/`ReadVarint`) was omitted (io and
  reflect out of M7 scope).
- Retires when: the reflect codec and the io API are re-tried against
  real reflect / landed io, and WS D lands for the multi-result members.
- Trigger: **FIRED — re-eval** (both named triggers — reflect landed,
  io landed — have happened; neither omission has been revisited).

### encoding/hex

- File: `internal/frontend/synth_hex.go`.
- Blocker: **F-A.** Real encode/decode core; multi-result members
  desugared; the io-dependent streaming API is omitted (io was out of
  M7 scope).
- Retires when: WS D lands and the streaming API is declared over the
  synthetic core.
- Trigger: **FIRED — re-eval** (io landed P3-M8; the omission has not
  been revisited).

### encoding/pem

- File: `internal/frontend/synth_pem.go`.
- Blocker: **F-A.** Real block parsing and PEM generation; multi-result
  member desugared; io-dependent `Encode` omitted.
- Retires when: WS D lands and the real `encoding/pem` compiles via
  the disk importer.
- Trigger: pending (the io trigger for `Encode` is satisfied by io
  having landed, but the header states no explicit re-test trigger for
  it — recorded here as an open question, not as fired).

### encoding/csv

- File: `internal/frontend/synth_enc_csv_gob.go` (with `encoding/gob`).
- Blocker: **F-A + F-E.** Complete gc CSV grammar ported to
  single-result style (imports only synthetic `errors`); io-based
  constructors are absent (fail fast); M7 extensions (`NewReaderString`,
  `NewBufferWriter`, `Bytes`) are marked.
- Retires when: WS D lands and the real `encoding/csv` compiles via
  the disk importer.
- Trigger: pending.

### encoding/gob

- File: `internal/frontend/synth_enc_csv_gob.go` (with `encoding/csv`).
- Blocker: **F-E + F-A.** Encoder/Decoder over the closed dynamic
  any-model (nil, bool, all int/uint widths, float32/64, string,
  `[]byte`, `[]any`, `map[string]any`) with a byte-identical wire
  format to gc for the supported subset; general user types need
  reflect (fail fast, "not in M7 scope (needs reflect, P3-M8)").
- Retires when: the reflect-dependent surface is re-tried against real
  reflect and WS D lands.
- Trigger: **FIRED — re-eval** for the reflect surface (real `reflect`
  landed 2026-10-16).

### encoding/json

- File: `internal/frontend/synth_json.go`.
- Blocker: **F-E + F-A.** Clean-room float formatting/parsing over
  integer arithmetic only (no `math`, no `unsafe`), byte-identical
  with gc per the differential float tests; the codec core is real Go.
  The full `Marshal`/`Unmarshal` surface over arbitrary user types
  needs reflect (the file header states the wave only; the reflect
  dependency is the standard json-codec blocker, and the reflect
  landing is the trigger to re-try it).
- Retires when: the real `encoding/json` compiles via the disk importer
  with working reflect support.
- Trigger: **FIRED — re-eval** (real `reflect` landed 2026-10-16).

### encoding/xml

- File: `internal/frontend/synth_xml.go`.
- Blocker: **F-E + F-A.** Token stream in scope (`Decoder.Token`/
  `RawToken` desugared, full XML grammar byte-exact vs gc, `Encoder`
  over a `Writer`, `EscapeText`/`Escape`, token types, `SyntaxError`,
  `Skip`, `InputOffset`, HTML entity tables); OUT and fail-fast:
  `Unmarshal`/`Marshal`/`MarshalIndent`/`Decode`/`DecodeElement`/
  `Encode`/`EncodeElement` (need reflect, P3-M8), `InputPos`
  (multi-result, WS D), `Decoder.CharsetReader` (needs `io`, P3-M8).
  The full rationale is the restored
  [m7-enc-xml red-green note](phase3-analysis/m7-enc-xml/redgreen.md).
- Retires when: the reflect surface is re-tried against real reflect,
  WS D lands for `InputPos`, and the real `encoding/xml` compiles via
  the disk importer.
- Trigger: **FIRED — re-eval** for the reflect and io surfaces (both
  landed); pending for `InputPos` (WS D).

### hash

- File: `internal/frontend/synth_crypto_hash.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification);
  see the restored [M8 crypto analysis](phase3-analysis/m8-analysis/crypto.md).
- Retires when: the hash interface's intrinsification (or disk-import
  of the real package) lands.
- Trigger: pending.

### hash/adler32

- File: `internal/frontend/synth_hash_adler32.go`.
- Blocker: **F-F.** Pure-Go port of the stdlib Adler-32 (chunked update
  loop verbatim, gc-identical values; marshal state format ported with
  hand-rolled big-endian codecs, no `internal/byteorder` dependency).
- Retires when: the real `hash/adler32` compiles via the disk importer.
- Trigger: pending.

### hash/crc32

- File: `internal/frontend/synth_hash_crc32.go`.
- Blocker: **F-C.** Pure-Go port; the arch-specific and slicing-by-8
  fast paths are NOT ported (the byte-at-a-time table walk computes
  bit-identical values); marshal state format ported.
- Retires when: the asm strategy covers the crc32 fast paths, or the
  disk importer handles the pure-Go selection.
- Trigger: pending.

### hash/crc64

- File: `internal/frontend/synth_hash_crc64.go`.
- Blocker: **F-C.** Pure-Go port; the slicing-by-8 fast path is NOT
  ported (bit-identical values via the table walk); marshal state
  format ported.
- Retires when: the asm strategy covers the crc64 fast paths, or the
  disk importer handles the pure-Go selection.
- Trigger: pending.

### hash/fnv

- File: `internal/frontend/synth_hash_fnv.go`.
- Blocker: **F-F.** Pure-Go port of FNV-1/FNV-1a (32/64/128-bit); the
  128-bit multiply is hand-rolled 64x64→128 (the synthetic tree does
  not register `math/bits` as an importable package for this file);
  marshal state formats ported.
- Retires when: the real `hash/fnv` compiles via the disk importer.
- Trigger: pending.

### hash/maphash

- File: `internal/frontend/synth_hash_maphash.go`.
- Blocker: **F-C.** VALUE DIVERGENCE, documented and deliberate: gc
  funnels every input through `runtime.memhash` (`//go:linkname` into
  the Go runtime, AES-based on amd64), which no pure-Go synthetic can
  reproduce bit-exactly. The synthetic implements the full API over its
  own pure-Go seeded 64-bit hash (splitmix64); values are deterministic
  within a graalgo process but NOT equal to gc's for the same seed.
  The corpus pins only structural properties.
- Retires when: `runtime.memhash` is available to the real `maphash`
  via the disk importer (or the divergence is accepted as permanent by
  an explicit design decision — not by silence).
- Trigger: pending.

## Crypto family

General note (applies to every row in this section unless the row says
otherwise): the M8 packages are software implementations only (no
intrinsification); the tier-5 packages are pure-Go ports of the gc
sources. The analysis behind the M8 selection is the restored
[M8 crypto analysis](phase3-analysis/m8-analysis/crypto.md): every
`crypto/*` package is written against boringcrypto, FIPS 140-3, and
standard modes, and the synthetics implement the standard mode
(FIPS/boring branches trimmed as dead code). Retires when: the asm
strategy (emulate-then-intrinsify) covers the package, or the disk
importer handles the package's pure-Go file selection. Trigger: pending,
unless the row says otherwise.

### crypto

- File: `internal/frontend/synth_crypto.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/md5

- File: `internal/frontend/synth_crypto_md5.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/sha1

- File: `internal/frontend/synth_crypto_sha1.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/sha256

- File: `internal/frontend/synth_crypto_sha256.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/sha512

- File: `internal/frontend/synth_crypto_sha512.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/sha3

- File: `internal/frontend/synth_crypto_sha3.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/subtle

- File: `internal/frontend/synth_crypto_subtle.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/hmac

- File: `internal/frontend/synth_crypto_hmac.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/cipher

- File: `internal/frontend/synth_crypto_cipher.go` +
  `internal/frontend/synth_crypto_cipher_gcm.go` (a fragment
  concatenated after the main source in `synth_machinery.go`, not a
  standalone package).
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/aes

- File: `internal/frontend/synth_crypto_aes.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/des

- File: `internal/frontend/synth_crypto_des.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/rc4

- File: `internal/frontend/synth_crypto_rc4.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification).
- Retires when: see the general note above.

### crypto/rand

- File: `internal/frontend/synth_crypto_rand.go`.
- Blocker: **F-C.** Software implementation only (no intrinsification);
  randomness ultimately needs a runtime entropy source, not just
  pure-Go code.
- Retires when: see the general note above.

### crypto/elliptic

- File: `internal/frontend/synth_crypto_elliptic.go`.
- Blocker: **F-C.** Tier-5 pure-Go port; curve arithmetic is software
  (see also the hand-rolled EC arithmetic in `GoX509.java`, inventory
  B-5, which serves `crypto/x509`, not this package).
- Retires when: see the general note above.

### crypto/ecdh

- File: `internal/frontend/synth_crypto_ecdh.go`.
- Blocker: **F-C.** Tier-5 pure-Go port.
- Retires when: see the general note above.

### crypto/dsa

- File: `internal/frontend/synth_crypto_dsa.go`.
- Blocker: **F-C.** Tier-5 pure-Go port.
- Retires when: see the general note above.

### crypto/ecdsa

- File: `internal/frontend/synth_crypto_ecdsa.go`.
- Blocker: **F-C.** Tier-5 pure-Go port.
- Retires when: see the general note above.

### crypto/ed25519

- File: `internal/frontend/synth_crypto_ed25519.go`.
- Blocker: **F-C.** Tier-5 pure-Go port.
- Retires when: see the general note above.

### crypto/rsa

- File: `internal/frontend/synth_crypto_rsa.go`.
- Blocker: **F-C.** Tier-5 pure-Go port (needs `math/big`, also
  synthetic — see its row).
- Retires when: see the general note above.

### crypto/hkdf

- File: `internal/frontend/synth_crypto_hkdf.go`.
- Blocker: **F-C.** Tier-5 pure-Go port.
- Retires when: see the general note above.

### crypto/pbkdf2

- File: `internal/frontend/synth_crypto_pbkdf2.go`.
- Blocker: **F-C.** Tier-5 pure-Go port.
- Retires when: see the general note above.

### crypto/internal/boring

- File: `internal/frontend/synth_crypto_boring.go`.
- Blocker: **F-C.** Tier-5 port of the boringcrypto shim; in the
  standard toolchain `boring.Enabled` is a false constant and the
  branches are dead code (see the M8 analysis §1).
- Retires when: the real package compiles via the disk importer, or
  boringcrypto mode is explicitly out of scope by design decision.
- Trigger: pending.

### crypto/internal/boring/bbig

- File: `internal/frontend/synth_crypto_bbig.go`.
- Blocker: **F-C.** Tier-5 port of the boring `bbig` adapter.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/constanttime

- File: `internal/frontend/synth_crypto_constanttime.go`.
- Blocker: **F-C.** Tier-5 port; constant-time primitives are pure Go
  here (no assembly).
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/rand

- File: `internal/frontend/synth_crypto_internal_rand.go`.
- Blocker: **F-C.** Tier-5 port of the internal rand adapter.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140

- File: `internal/frontend/synth_crypto_fips140.go`.
- Blocker: **F-C.** Tier-5 port of the FIPS 140-3 module scaffolding;
  with FIPS disabled the module degenerates to pass-through behavior
  (see the M8 analysis §1).
- Retires when: the real package compiles via the disk importer, or
  FIPS mode is explicitly out of scope by design decision.
- Trigger: pending.

### crypto/internal/fips140/check

- File: `internal/frontend/synth_crypto_fips140_check.go`.
- Blocker: **F-C.** Stub: the real package does FIPS 140 load-time
  verification, a no-op when FIPS is disabled. Empty here.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/alias

- File: `internal/frontend/synth_crypto_fips140_alias.go`.
- Blocker: **F-C + F-D (trap).** The real package uses
  `uintptr(unsafe.Pointer(...))` for overlap detection, which traps
  under D5 (GG-0016). Calls to `AnyOverlap`/`InexactOverlap` are
  intercepted by the frontend (`isAliasIntrinsicFunc`) and lowered to
  the `sliceoverlap` wire node; the Go bodies exist for type-checking
  and as a fallback.
- Retires when: a byte-addressed value model resolves the D5 trap and
  the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/drbg

- File: `internal/frontend/synth_crypto_fips140_drbg.go`.
- Blocker: **F-C.** Tier-5 port; with FIPS off, `drbg.Read`
  degenerates to `sysrand.Read` (see the M8 analysis §1).
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/nistec

- File: `internal/frontend/synth_crypto_fips140_nistec.go`.
- Blocker: **F-C.** Tier-5 port of the NIST-curve implementation.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/nistec/fiat

- File: `internal/frontend/synth_crypto_fips140_nistec_fiat.go`.
- Blocker: **F-C.** Tier-5 port of the fiat-crypto generated field
  arithmetic (pure Go; the generated code, not assembly).
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/edwards25519

- File: `internal/frontend/synth_crypto_fips140_edwards25519.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/edwards25519/field

- File: `internal/frontend/synth_crypto_fips140_edwards25519_field.go`.
- Blocker: **F-C.** Tier-5 port of the field arithmetic.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/bigmod

- File: `internal/frontend/synth_crypto_fips140_bigmod.go`.
- Blocker: **F-C.** Tier-5 port (needs `math/big`, also synthetic).
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/subtle

- File: `internal/frontend/synth_crypto_fips140_subtle.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/sha256

- File: `internal/frontend/synth_crypto_fips140_sha256.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/sha512

- File: `internal/frontend/synth_crypto_fips140_sha512.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/sha3

- File: `internal/frontend/synth_crypto_fips140_sha3.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/hmac

- File: `internal/frontend/synth_crypto_fips140_hmac.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/hkdf

- File: `internal/frontend/synth_crypto_fips140_hkdf.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/pbkdf2

- File: `internal/frontend/synth_crypto_fips140_pbkdf2.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/ecdh

- File: `internal/frontend/synth_crypto_fips140_ecdh.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/ecdsa

- File: `internal/frontend/synth_crypto_fips140_ecdsa.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/ed25519

- File: `internal/frontend/synth_crypto_fips140_ed25519.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140/rsa

- File: `internal/frontend/synth_crypto_fips140_rsa.go`.
- Blocker: **F-C.** Tier-5 port (needs `math/big`, also synthetic).
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140cache

- File: `internal/frontend/synth_crypto_fips140cache.go`.
- Blocker: **F-C.** Tier-5 port of the FIPS cache scaffolding.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140hash

- File: `internal/frontend/synth_crypto_fips140hash.go`.
- Blocker: **F-C.** Tier-5 port.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140only

- File: `internal/frontend/synth_crypto_fips140only.go`.
- Blocker: **F-C.** Tier-5 port; `Enforced()` defaults false (see the
  M8 analysis §1).
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### crypto/internal/fips140deps/byteorder

- File: `internal/frontend/synth_crypto_fips140deps_byteorder.go`.
- Blocker: **F-F.** Tier-5 port of the byte-order helpers.
- Retires when: the real package compiles via the disk importer.
- Trigger: pending.

### golang.org/x/crypto/cryptobyte

- File: `internal/frontend/synth_cryptobyte.go`.
- Blocker: **F-F.** Tier-5 port of the `x/crypto` cryptobyte package
  (not stdlib; carried because `crypto/x509` and friends need it).
- Retires when: the real `x/crypto` package is vendored/compiled via
  the disk importer, or the consumers retire.
- Trigger: pending.

### golang.org/x/crypto/cryptobyte/asn1

- File: `internal/frontend/synth_cryptobyte_asn1.go`.
- Blocker: **F-F.** Tier-5 port, same reason as `cryptobyte`.
- Retires when: the real package is available via the disk importer,
  or the consumers retire.
- Trigger: pending.

### crypto/x509

- File: `internal/frontend/synth_crypto_x509.go`.
- Blocker: **F-G.** Strategy (per the 2026-10-10 stdlib strategy):
  delegate to Java's certificate handling where semantics match,
  rather than reimplementing in Go. Certificate parsing delegates to
  `CertificateFactory` and PKCS#8 private-key parsing delegates to
  `KeyFactory` (both verified in
  [`GoX509.java`](../../internal/backend/interop/src/main/java/graalgo/p3x509/GoX509.java)).
  What is still hand-rolled, and therefore still the bridge (inventory
  B-5, narrowed by this change): SEC1/PKCS#1 DER walking
  (`parsePkcs1Integers`, `extractSec1PrivateKey`,
  `extractSec1CurveOid`, …) and EC arithmetic (`scalarMultiply`,
  `ecAdd`, `ecDouble`). Certificate creation (`CreateCertificate`)
  stays in Go (no standard JDK API).
- Retires when: the real `crypto/x509` compiles via the disk importer
  (worktree `real-x509` per the inventory) with the JDK delegation
  design covering the remaining hand-rolled parts, or those parts are
  themselves delegated to the JDK. One of the two genuinely in-flight
  Phase-1 packages (inventory B-4).
- Trigger: pending.

### crypto/x509/pkix

- File: `internal/frontend/synth_crypto_x509_pkix.go`.
- Blocker: **F-G.** Companion to `crypto/x509` (certificate structures
  shared with the JDK-delegated path).
- Retires when: the real `crypto/x509/pkix` compiles via the disk
  importer, with `crypto/x509`.
- Trigger: pending.

### crypto/tls

- File: `internal/frontend/synth_crypto_tls.go`.
- Blocker: **F-G + F-D.** TLS needs the runtime via `//go:linkname`
  plus `crypto/x509` and `net`, all still synthetic. (The 10-second
  server-handshake sleep at `synth_crypto_tls.go:748` is a separate
  correctness gap owned by lane L01, review R0-1 — not a retirement
  blocker and not fixed by this docs change.)
- Retires when: real `crypto/x509` and real `net` land and the real
  `crypto/tls` compiles via the disk importer with its runtime needs
  met.
- Trigger: pending.

## Container, archive, compress, mime

### container/heap

- File: `internal/frontend/synth_container_heap.go`.
- Blocker: **F-F.** Pure-Go port of the stdlib heap operations; the
  only dependency is the synthetic `sort` package. No per-package
  compile blocker stated.
- Retires when: the real `container/heap` compiles via the disk
  importer (with real `sort`).
- Trigger: pending.

### container/list

- File: `internal/frontend/synth_container_list.go`.
- Blocker: **F-F.** Pure-Go port of the stdlib doubly linked list.
  No per-package compile blocker stated.
- Retires when: the real `container/list` compiles via the disk
  importer.
- Trigger: pending.

### container/ring

- File: `internal/frontend/synth_container_ring.go`.
- Blocker: **F-F.** Pure-Go port of the stdlib circular list.
  No per-package compile blocker stated.
- Retires when: the real `container/ring` compiles via the disk
  importer.
- Trigger: pending.

### archive/tar

- File: `internal/frontend/synth_archive_tar.go` (generated by
  `gen_synth_archive_mime.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `archive/tar` compiles via the disk importer.
- Trigger: pending.

### archive/zip

- File: `internal/frontend/synth_archive_zip.go` (generated by
  `gen_synth_archive_mime.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `archive/zip` compiles via the disk importer.
- Trigger: pending.

### mime

- File: `internal/frontend/synth_mime.go` (generated by
  `gen_synth_archive_mime.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `mime` compiles via the disk importer.
- Trigger: pending.

### mime/multipart

- File: `internal/frontend/synth_mime_multipart.go` (generated by
  `gen_synth_archive_mime.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `mime/multipart` compiles via the disk importer.
- Trigger: pending.

### mime/quotedprintable

- File: `internal/frontend/synth_mime_quotedprintable.go` (generated by
  `gen_synth_archive_mime.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `mime/quotedprintable` compiles via the disk importer.
- Trigger: pending.

### compress/bzip2

- File: `internal/frontend/synth_compress_bzip2.go` (generated by
  `gen_synth_compress.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling; no per-package adaptation (header: "(none)").
- Retires when: the frontend supports the adapted constructs and the
  real `compress/bzip2` compiles via the disk importer.
- Trigger: pending.

### compress/flate

- File: `internal/frontend/synth_compress_flate.go` (generated by
  `gen_synth_compress.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  handling and the header's three adaptations: `sync.OnceValue` →
  eager init (the frontend cannot monomorphize a synthetic generic
  helper), func-typed fill/step fields → int enums dispatched by
  helpers (WS H method expressions), amd64 const vendored
  (graalgo targets amd64).
- Retires when: the frontend supports the adapted constructs and the
  real `compress/flate` compiles via the disk importer.
- Trigger: pending.

### compress/gzip

- File: `internal/frontend/synth_compress_gzip.go` (generated by
  `gen_synth_compress.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  handling and one adaptation: `*p = Struct{...}` assignments
  (unsupported: WS D, no assignment to `*ast.StarExpr`) replaced by
  field-by-field assignment.
- Retires when: the frontend supports the adapted constructs and the
  real `compress/gzip` compiles via the disk importer.
- Trigger: pending.

### compress/lzw

- File: `internal/frontend/synth_compress_lzw.go` (generated by
  `gen_synth_compress.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `compress/lzw` compiles via the disk importer.
- Trigger: pending.

### compress/zlib

- File: `internal/frontend/synth_compress_zlib.go` (generated by
  `gen_synth_compress.py` from gc 1.27.1).
- Blocker: **F-B.** Generated verbatim gc source plus Appendix-M
  multi-result handling.
- Retires when: the frontend supports the adapted constructs and the
  real `compress/zlib` compiles via the disk importer.
- Trigger: pending.

## Appendix A — p3frontend mirrors

The 12 `synth_*.go` mirrors under
[`internal/frontend/p3frontend/`](../../internal/frontend/p3frontend/)
pin the synthetic signatures for p3frontend's own type-check (it cannot
reuse the frozen exporter's unexported signatures). A mirror retires
with the top-level file it mirrors; the rows above are the record, and
each mirror file header links to the row(s) it mirrors — except one:

- `p3frontend/synth_math.go` is generated by `m7-math/gen_mirror.py`,
  which is not in the tree (deleted in the Phase-4 migration). The file
  is therefore **frozen**: it carries no tracker link (hand-editing a
  `DO NOT EDIT` file whose generator is gone would be dishonest
  bookkeeping), and the `math` row above is its record. If the mirror
  needs to change, the generator must be recovered or the mirror
  re-derived from the assembled `math` body first.
- The other 11 mirrors (`synth_bytes`, `synth_enc_core`,
  `synth_enc_csv_gob`, `synth_errors`, `synth_io`, `synth_json`,
  `synth_sort`, `synth_strconv`, `synth_strings`, `synth_time_context`,
  `synth_xml`) are maintained copies and their headers link to the
  corresponding rows.

## Appendix B — Retired (no longer synthetic)

These packages used to be synthetic and now compile from GOROOT via the
disk importer. They appear here so a reader of an old header that names
them can find where they went. The inventory's Retired section is the
parallel record.

### fmt (retired)

Retired 2026-10-16 (real-fmt2 lane; inventory cites master `c2c0718c`).
Synthetic `fmt` registration removed from `dedicatedSyntheticStubs`;
`exportFmtCall`/`fmtOverlayFuncs` deleted. Only
`internal/graalgo/fmtstate` (a graalgo-only support package, row above)
remains synthetic in the `fmt` area.

### reflect (retired)

Retired 2026-10-16 (`d2804a52`, real-reflect2 lane). Registration
commented out in `synth_machinery.go` ("real reflect now compiles from
GOROOT via the disk importer"). The `synth_reflect.go` source remains on
disk only as the `reflect_m7` desugar helper (row above) and the `iter`
declarations.

### internal/reflectlite (retired)

Retired with `reflect` (same change family; real package from GOROOT via
the disk importer, needed by real `reflect` and real `errors`).

### net/http (retired)

Retired 2026-10-16 (`cc0a78f4`, real-nethttp lane). The synthetic
sources are retired; real `net/http` compiles from GOROOT via the
general disk overlay.

### net/http/httptest (retired)

Retired with `net/http` (same change family).

## Appendix C — Non-package files (no row, by design)

- `internal/frontend/synth_machinery.go` — the overlay machinery itself
  (`syntheticSources`, `overlayImporter`, desugar tables), not a package
  implementation. Its `phases/phase3` pointers (the `p3reflect` mention
  and the M8 crypto citation) are repointed by this change; see the
  [phase3-analysis README](phase3-analysis/README.md).
- `internal/frontend/*_test.go` and `*_desugar_test.go` — tests and
  desugar-table tests, not package implementations.
- `internal/frontend/gen_synth_*.py` + `gen_synth_adapt/*.patch` — the
  generators for the 18 generated files. A generated file's tracker
  link lives in its generator's header template, not in the generated
  file by hand, so `make gen-synth` reproduces it.

## Maintenance

- When a package retires, move its row to appendix B in the same change
  that removes the code (docs are part of the change), and delete the
  header link line from the deleted file with it.
- When a row's trigger fires (a WS lands, a runtime surface completes),
  update the row's Trigger field and re-evaluate the package; do not
  leave `FIRED — re-eval` rows stale — they are the retirement queue.
- This tracker is verified mechanically: every headed row must have a
  header link in its file (except the frozen `p3frontend/synth_math.go`
  mirror), and every header link must resolve to a row. A future lane
  may add that check to `make check`.
