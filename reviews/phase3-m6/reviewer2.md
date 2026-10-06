# P3-M6 independent review — Reviewer 2 (methodology + integration)

**Scope:** workstream commits cf59713, bef101b, 188b90a, 62cc214, d1195ca (HEAD).
**Reviewer brief:** asm strategy fail-fast, P3-D1 re-examination, cross-workstream
coherence, vendoring procedure, suite re-verification.
**Verdict: PASS-WITH-FINDINGS** — 0 P0, 2 P1, 8 P2.
**Independent P3-D1 verdict: STANDS** (see §3; steelman included).

All quantitative claims below were re-derived independently (own scripts against
the pinned trees), not copied from workstream artifacts. A live slip-through
demonstration for finding F1 was executed in /tmp (since cleaned up); no repo
files were modified by this review.

---

## 1. What was verified green

1. **Inventory completeness (lowercase `.s`):** re-enumerated `~/tools/go-1.27.1/src`
   independently — 627 files; set-equality with `asm-inventory-go1.27.1.json` is
   exact (0 missing, 0 stale). Category counts match the workstream: evaporate
   238, unsupported 203, emulate-then-intrinsify 71, intrinsify 61, jvm-math 54.
2. **Buildable set (linux/amd64, go/build oracle, CgoEnabled=false):** 67 `.s`
   files buildable; every one has an inventory entry. Live fail-fast set = 10
   files (syscall, reflect, hash/crc32 amd64 + 4 cmd/* + 2 vendor/* + runtime/cgo
   asm_amd64.s); live replaced set = 57 (28 evaporate / 7 intrinsify /
   16 emulate-then-intrinsify / 6 jvm-math).
3. **Category spot-checks (5 per category, random seed):** all defensible.
   Evaporate = runtime-core/internal ABI/GC/stack asm; intrinsify =
   bytealg+atomics with named real intrinsics; emulate-then-intrinsify = crypto
   incl. fips140 (verified `aes_generic.go` fallback exists); jvm-math = math +
   math/big arch kernels; unsupported = cmd/*, vendor/*, testdata (never
   assembled as package input), plus syscall/reflect/hash-crc32/simd with
   per-file re-test triggers. One nit: `crypto/x509/internal/macos/corefoundation.s`
   is labeled "crypto SIMD kernels" though it is a darwin-only CoreFoundation
   trampoline — inert on the target (never buildable on linux/amd64), so the
   disposition is safe but the label is wrong (folded into F9).
4. **Coupled-package decomposition redone from `coupled-packages-go1.27.1.json`:**
   47 build-tags-only / 26 linkname-only / 25 build-tags+linkname / 12 cgo(±others)
   / 32 asm(±others) = 142. Matches the addendum exactly, including the
   cgo-takes-precedence bucketing (runtime, runtime/cgo, syscall carry asm too).
5. **Suites re-run on this tree:** `make -C phases/phase3 test` → 58 tests,
   0 failures; `make -C phases/phase3 check` → clean (exclusion gate validates:
   v1, 2 entries, cap 13); `go test ./...` in `p3frontend` → ok (42.8s);
   `tools/stdlib-vendor`, `tools/stdlib-excl`, `tools/stdlib-linkdiff` → ok.
6. **Phase-1/phase-2 isolation:** `git show --name-only` for all five commits —
   zero files under `phases/phase1/` or `phases/phase2/`. Nothing to break.
7. **Linkname accounting coherence:** 615 directives = 8 SATISFIED + 1 ROUTED +
   606 MISSING; skip-list 525 = distinct MISSING targets; 69 dirs carry the
   linkname reason. All three numbers reconcile.
8. **GOROOT spellings:** `~/tools/go` is a symlink to `go-1.27.1`; both report
   go1.27.1 and their `src` trees are file-identical (diff of full file lists:
   empty). No live inconsistency — but see F5.

---

## 2. Findings

### F1 (P1) — `.S` / `.sx` assembly files are invisible to the entire asm strategy; fail-fast is breachable

**Files:** `phases/phase3/src/p3frontend/asm.go:179`,
`phases/phase3/stdlib-fork/gen-asm-inventory.py:268`,
`phases/phase3/src/p3frontend/asm_test.go:41`

**Trigger:** go/build treats `.s`, `.S`, and `.sx` identically as assembly
(`~/tools/go-1.27.1/src/go/build/build.go:1097`: `case ".s", ".S", ".sx":` →
`SFiles`). The tree contains 12 `.S` files the inventory never enumerates
(`runtime/cgo/gcc_{386,aix_ppc64,amd64,arm,arm64,linux_ppc64x,loong64,mips64x,mipsx,riscv64,s390x}.S`
plus `cmd/cgo/internal/test/stubtest_linux_ppc64x.S`) and 0 `.sx`.

**Expected:** "every buildable .s file in a compiled package resolves to a
category or fails fast" (ADR-0007); generator "fails on any uncategorized file".

**Actual:** four layers share the lowercase-only blind spot — the generator
(`fn.endswith(".s")`), the embedded inventory (627, not 639), the loader
filter (`strings.HasSuffix(name, ".s")`), and the completeness test's own
enumerator (same suffix check, so the test cannot catch the gap).

**Demonstrated live:** a module-local package with a build-tagged
`evil_amd64.S` passed `CheckAsmFiles` with 0 resolutions and **nil error**
(silently ignored — the file is never even read); renaming it to `.s` produced
the fail-fast error. Constructible today by any user module; also bites on the
next toolchain upgrade if Go adds a `.S`/`.sx` file to a compiled package.

**Blast radius today is limited** (the 12 files sit in cgo/toolchain dirs
outside M1's compilable set), which is why this is P1 not P0 — but the
mechanism's headline contract is what is under review, and it is broken.

### F2 (P1) — `stdlib-excl` vendored-tree detection can never fire (dead fallback, latent silent skew)

**File:** `tools/stdlib-excl/validate.go:213`

**Trigger:** `defaultSrcRoot()` prefers the vendored tree "when present" via
`$HOME/workspace/graalgo-stdlib/<ver>/src`. But the vendor tool copies
`$GOROOT/src` *contents* directly into the target dir (manifest `targetDir` is
the src root — verified: manifest entries resolve at `targetDir/<rel>`, and
`targetDir/src/<rel>` does not exist). The `<ver>/src` path never exists.

**Expected:** on a pinned machine, validate/measure read the vendored tree
(VENDORING.md: "single source of truth every downstream step reads").

**Actual:** the vendored branch is dead; the tool always falls through to
`go env GOROOT`. Results are correct *today* (trees identical), but after a
toolchain upgrade the gate would silently measure the new toolchain against the
old pin — exactly the version-skew failure mode the vendoring workstream was
built to prevent.

### F3 (P2) — VENDORING.md's "single source of truth" claim is aspirational, not actual

**File:** `phases/phase3/stdlib-fork/VENDORING.md` §1.

**Expected per doc:** "The vendored stdlib tree is the single source of truth
every downstream step reads."

**Actual:** nothing reads it by default — loader → `~/tools/go-1.27.1`
(`loader.go:103`); linkdiff → `$GOROOT`/ `~/tools/go/src` (`main.go:91-101`);
excl → live GOROOT (via the broken F2 fallback). Either wire the tools to the
vendored tree (fixing F2) or downgrade the claim to the intended end state.

### F4 (P2) — Re-vendoring procedure omits the asm-inventory regeneration step

**File:** `phases/phase3/stdlib-fork/VENDORING.md` §3.

**Trigger:** step 4 says "re-run the linkname-diff tooling and the coupled-package
measurement" but never mentions `gen-asm-inventory.py` (which also emits the
embedded `zz_asm_inventory_gen.go` the loader enforces).

**Expected:** a complete, executable per-release procedure.

**Actual:** an operator following §3 verbatim upgrades the tree while the loader
keeps enforcing the *old* version-pinned inventory — every new `.s` file fails
as uncategorized. Loud, not silent, but the procedure is incomplete as written.
(The asm inventory's own re-test triggers do record this; VENDORING.md does not.)

### F5 (P2) — GOROOT spelling fragmentation with no canonical resolver or pin cross-check

**Files:** `loader.go:103`, `gen-asm-inventory.py:420`, `tools/stdlib-linkdiff/main.go:91-101`,
`tools/stdlib-excl/validate.go:232-237`, trial reports.

Five spellings of "the toolchain" (`~/tools/go-1.27.1`, `~/tools/go-1.27.1/src`,
`~/tools/go/src`, `$GOROOT`, `~/workspace/graalgo-stdlib/go1.27.1/`) across four
workstreams, and no tool cross-checks its tree against `pinned-version.txt`.
`stdlib-linkdiff` in particular will scan a stale tree and report a
`SKIP_LIST_SIZE` with exit 0. Harmless today (all resolve to identical bytes);
a real inconsistency risk on the next upgrade. Recommend one canonical resolver
(vendored-when-present, else pinned toolchain, else fail) shared by all tools.

### F6 (P2) — Exclusion reason code `ASM_UNCATEGORIZED` contradicts the asm taxonomy

**Files:** `phases/phase3/stdlib-fork/exclusions.yaml` (reason_codes),
`phases/phase3/src/p3frontend/asm.go` (AsmUncategorized).

**Expected:** reason codes align with the asm categories and linkdiff
classifications (LINKNAME_MISSING ↔ MISSING aligns fine).

**Actual:** `asm.go` defines "uncategorized" as *absence of an inventory
entry*, which the generator invariant makes impossible ("zero uncategorized").
`exclusions.yaml` defines `ASM_UNCATEGORIZED` as ".s files with no assigned
P3-M6 asm strategy category yet" — but every inventoried file *has* a category,
and the category that actually means "no strategy decided yet" is
`unsupported`. No live misclassification (v1 uses only CGO/UNSUPPORTED_FEATURE),
but the vocabulary's definition contradicts the mechanism's invariant; on a
toolchain upgrade the two readings would classify a new file differently.

### F7 (P2) — ADR-0007 overclaims the hook's coverage; no recorded M7 extension obligation

**File:** `phases/phase3/adr/ADR-0007-asm-strategy.md` (Context, D1).

**Expected:** "every buildable .s file in a compiled package now resolves to
exactly one of five dispositions, or the build fails fast."

**Actual:** the hook lives in `localRef`/`mainRef` only (`loader.go:378,453`).
The stdlib branch of `ResolveImport` (`loader.go:421`) — the actual population
with 627 `.s` files — is unhooked. Correct for M1 scope (`kindStdlib` =
"resolvable, not compilable in M1"; `importer.go:104` rejects stdlib
compilation), but the ADR never states this boundary or records the obligation
to extend the hook when M7+ waves make stdlib compilable. A wave-M7 worker
reading only the ADR would reasonably believe coverage is already universal.

### F8 (P2) — `AsmResolution` replacement payload is unconsumed (the "replace" half is inert)

**Files:** `phases/phase3/src/p3frontend/loader.go:378,453`.

Both hook sites discard resolutions (`if _, err := l.CheckAsmFiles(...)`).
The evaporate/intrinsify/emulate-then-intrinsify dispositions therefore
currently do nothing beyond *not failing* — no downstream consumer acts on the
replacement pointers. The reject half (fail-fast) is live and tested; the
replace half awaits M7. Not a correctness bug, but the ADR presents a
"strategy" whose execution half is dead code today; worth one explicit sentence.

### F9 (P2) — Oracle test skips selection comparison exactly where it matters most

**File:** `phases/phase3/src/p3frontend/asm_test.go:141-175`
(`TestAsmSelectionMatchesGoBuildOracle`).

**Trigger:** on fail-fast (unsupported dirs: hash/crc32, syscall, reflect) the
test logs and `continue`s — the file-for-file oracle comparison runs only on
replaced-category dirs.

**Expected:** selection agreement verified everywhere; a filter bug that
*spuriously drops* an unsupported file (missing fail-fast) would be caught.

**Actual:** that bug class is invisible to the test. Related: the 12 sampled
dirs exclude `runtime/cgo` — the only dir containing `.S` files — so the test
suite as written could never have caught F1.

### F10 (P2) — ADR-0001-addendum: sound verdict, loose rhetoric in two places

**File:** `phases/phase3/adr/ADR-0001-addendum-p3d1-reexamination.md`.

1. "44 real-adaptation packages — within the spirit of the original ~78"
   compares a strict-measurement *subset* against a loose-measurement
   *superset*. The honest bound is simpler and stronger: the strict
   real-adaptation subset (44) is smaller than the entire loose coupled set
   (78), so the fork-adaptation budget is not exceeded even under the
   strictest reading.
2. "Part of the drift is real (new `arena`, `crypto/boring`, `simd/*`
   packages…)": `arena` and `simd/*` are classed `not-linux-amd64`, not
   coupled — they are not in the 142. The actual coupled drift drivers are
   `crypto/internal/fips140/*` (19 dirs, post-SPEC) and linkname growth
   (69 dirs).

Neither affects the verdict (§3).

### F11 (P2) — VENDORING.md editorial nits

1. §1 table says the go1.27.1 tree is "157M"; "Current pin" says 132,185,622
   bytes (126 MiB). Pick one number.
2. §3 step 3's manifest-diff is a manual "jq/python" recipe with no provided
   tool — error-prone for a required review step on every upgrade.

---

## 3. P3-D1 re-examination: independent verdict — STANDS

**Re-derived decomposition** (from `coupled-packages-go1.27.1.json`, independent
bucketing): 47 / 26 / 25 / 12 / 32 = 142. Exact match. The bucketing rule
(cgo-takes-precedence over asm) is consistently applied.

**Why "stands" is justified, not hand-waving:**

1. P3-D1 is a *fork-vs-overlay* boundary decision: do coupled packages get
   real, lightly-adapted sources, or re-synthesized overlays? The
   decomposition shows every one of the 142 falls into: pure (236, no work),
   mechanical build-tag filtering (47, already built in P3-M1), linkname
   provision with *unchanged* sources (51, tracked workstream GG-0021 with its
   own SPEC §3.6 kill trigger), or genuine asm/cgo adaptation (44). No package
   *forces* re-synthesis — the alternative to "stands" has no exhibit.
2. The 44 real-adaptation packages are all addressed by decided strategies:
   the 32 asm-bucket dirs are covered file-by-file by the five asm dispositions
   (§1 item 2 — I checked the buildable set against the inventory), and the 12
   cgo-bucket dirs route to the Panama FFI strategy (P3-M8) or existing
   exclusions (runtime/cgo is already excluded as CGO).
3. The strict real-adaptation subset (44) is smaller than the entire loose
   SPEC-era coupled set (~78): the fork-adaptation budget is not exceeded under
   any reading. The "82% drift" is dominated by reclassification (47
   build-tags-only were never adaptation work) plus genuinely new surface
   (fips140, linkname growth) that lands in *other* tracked workstreams, not in
   fork adaptation.

**Steelman for re-opening (strongest case against "stands"):**

1. The original decision's cost model plausibly assumed the linkname surface
   was small. It is not: 606 missing linkname targets (525 distinct) across 51
   dirs is the largest single work item in M7–M9, and "sources compile unchanged
   *once provided*" (addendum) compresses that entire program into a
   subordinate clause. If P3-D1's "lightly adapted" premise implicitly priced
   linkname provision as trivial, the premise changed even if the boundary
   didn't.
2. The 47 build-tags-only exclusion is definitional: the strict check *defines*
   them out of "coupled-in-the-adaptation-sense". A skeptic could call this
   moving the goalposts to protect the decision — the ~78 was measured with a
   different (undocumented) check, so "142 vs 78" and "44 vs 78" are both
   apples-to-oranges comparisons, and the addendum leans on whichever
   comparison flatters the verdict.
3. `reflect` (asm+linkname, asm files "unsupported"/fail-fast, "reimplemented
   on Truffle in wave-2") strains "never re-synthesized": reimplementing
   makeFuncStub/callReflect on Truffle is re-synthesis of the package's
   machine-level contract, even if the Go sources stay forked.

**Why the steelman doesn't carry:** (1) linkname provision was always a
separate tracked surface (GG-0021, SPEC §3.6 kill trigger on skip-list growth) —
the addendum doesn't hide it, it points at the tracking; the boundary question
is only whether sources must be re-synthesized, and they must not. (2) The
definitional point cuts both ways, but the *bound* argument (44 < 78 under the
strictest reading) holds regardless of which check you prefer. (3) reflect's Go
sources remain forked; only the ABI stubs (which have no meaning on Truffle —
no Go stacks) are reimplemented, which is exactly what the evaporate/unsupported
dispositions already bless for runtime-core asm.

**Bottom line:** the trigger fired correctly, the re-examination was actually
performed (per-dir reasons, versioned list), and the boundary stands on the
merits. The two rhetorical imprecisions are recorded as F10.

---

## 4. Integration notes (no finding, for the record)

- `CheckAsmFiles` discarding resolutions at the hook sites is consistent with
  M1 scope (stdlib not compiled); the fail-fast half is the live contract.
- `resolveCache` in `ResolveImport` does not bypass the hook: `CheckAsmFiles`
  runs inside `localRef` before the ref is cached.
- The `measure` tool's `asm` reason is buildability-aware via go/build
  `SFiles` (so it *is* `.S`-aware); only its build-tags glob (`*.s`) shares the
  lowercase blind spot — no live misclassification since the asm reason fires
  first.
- `TestAsmHookInLoader` proves the hook fires through the real
  mainRef→LoadClosure path; the control module loads clean.

## 5. Recommended follow-ups (for parent/coordinator triage)

1. Fix F1: match `.s`/`.S`/`.sx` in `asm.go:179`, `gen-asm-inventory.py:268`,
   `asm_test.go:41`; add the 12 `.S` files to the inventory with categories
   (runtime/cgo `gcc_*.S` → cgo-adjacent disposition or explicit unsupported;
   the cmd/cgo test fixture → unsupported/testdata); add a regression test
   with a buildable `.S` file asserting fail-fast.
2. Fix F2: `validate.go:213` should probe the target dir itself (manifest
   `targetDir`), not `<target>/src`.
3. On the next toolchain upgrade, treat F3/F4/F5 as a single work item:
   canonical tree resolver + pin cross-check in every tool, and complete the
   VENDORING.md §3 procedure (asm inventory regeneration, manifest-diff
   tooling).
4. Record the M7 hook-extension obligation from F7 in ADR-0007 or the M7 wave
   plan so it is not lost.
