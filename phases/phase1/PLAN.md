# Phase 1 plan — Go language core on Truffle

**Status:** r3 content-complete (spec-extract.md + req-phase-map.md consumed). For parent review.
**Normative:** SPEC.md (ACCEPTED 2026-10-03), ADR-0001, PHASES.md §3. 70 GG IDs locked.
**Extracts:** `spec-extract.md` (54 items), `req-phase-map.md` (31 Phase-1 IDs).

## 1. Objective and scope

Complete Go language (through generics) executing on the Truffle AST:
`go/parser`+`go/types` frontend service, Truffle nodes for all
statements/expressions, descriptor-based value model, defer/panic/Goexit,
intrinsic-surface stubs for language tests, differential harness vs stock gc.

In scope: the 31 Phase-1 GG IDs (req-phase-map.md §2). Out of scope: goroutines/
channels/GC interop (Phase 2), stdlib incl. net/os (Phase 3), cmd/go driver
wiring (Phase 4), AOT/JIT modes (Phase 5). The `-toolexec` argv contract is
frozen (documented + versioned) but not wired — Phase 1 tests run through the
direct `graalgo-run` launcher, never the wrapper (req-phase-map §3 flag 8).

## 2. Work breakdown (workstream → GG IDs → done-gate)

| WS | Workstream | GG IDs | Gate |
|---|---|---|---|
| A | Toolchain & repo skeleton (pinned jars, javac build, `graalgo-run` launcher, placeholder grep gate) | GG-0027 (pin stable), GG-0023 (linux/amd64) | — |
| B | Frontend service: `gofrontend` (vendored go/* verbatim) → versioned typed-AST JSON (`frontend/v1`) → Java `NodeFactory` | GG-0012, GG-0013, GG-0044 | 3 (part) |
| C | Value model (`graalgo.values`): descriptors, GoStruct{long[],Object[]}, GoSlice, GoMap (open-addressing), GoString{byte[]}, GoInterface{(descriptor,data),itab}, GoFunc{RootCallTarget,Frame}, handle-table unsafe.Pointer | GG-0015, GG-0016 | 1 |
| D | Expression nodes: typed execute methods, unboxed primitives; **Safepoint.poll() seam** at function entries; polymorphism probe hooks at interface dispatch | GG-0014, GG-0017 (seam slice) | 1 |
| E | Statement nodes incl. loops (`LoopNode`/`RepeatingNode`, `@ExplodeLoop`-friendly); **Safepoint.poll() seam** at loop back-edges; accurate SourceSections on every node | GG-0014, GG-0024 (inspect slice) | 1 |
| F | defer (explicit per-goroutine chain) / panic (`AbstractTruffleException`, lazy traces) / recover / Goexit (`ControlFlowException`); panic(nil)→*PanicNilError* | GG-0019 | 1 |
| G | Intrinsics: builtins (len/cap/make/new/append/copy/delete/complex/real/imag/print/println/panic/recover) + runtime stubs (memmove/memequal, makeslice/growslice, map ops, itab, fastrand, traceback) + minimal `fmt` stub with verb-conformance matrix; versioned signatures for the **full ~60–70 catalog** (stubs where Phase 2 owns the impl) | GG-0020 (stub slice), GG-0021 (symbol-name slice), GG-0059 (part) | 1, 3 |
| H | Generics: **monomorphize at the frontend** — one RootNode per go/types instantiation (D2, [DECIDED] below) | GG-0014 | 1 |
| I | diffharness `graalgo` backend + corpus: 45 smoke + GOROOT/test 356 + test/typeparam 266 (filtered; exclusions versioned per GG-0033) + generated programs | GG-0010, GG-0029/0030/0031/0032, GG-0036, GG-0038 | 1 |
| J | Fuzz-soak: template-based generator (gosmith is [UNVERIFIED] — assume fallback), differential, mutation-validated | GG-0037, GG-0034 | 2 |
| K | Interface freeze: (a) AST↔runtime intrinsic signatures, (b) `-toolexec` argv contract — versioned, documented; CI gate "AST calls only frozen signatures" | GG-0059, GG-0045 (stances) | 3 |
| L | Call-site polymorphism measurement: itab-identity histograms at interface PICs; `--dump-polymorphism`; numbers table (greenfield item 9) | carried obligation | — |
| M | Per-milestone peer reviews — see §4 review gate (each milestone M1–M9 closes with ≥2 independent reviews remediated); M10 additionally: self-attack (pathological generics, deep recursion, defer storms) + grep gates + cross-doc consistency audit + closeout | GG-0007, GG-0042, GG-0056 | 4, 5 |

Activation policy: GG ID →active per-workstream, exactly when its first real test
lands with a `verifies: GG-XXXX` annotation (req lint rule (c)); never before.

## 3. Architecture decisions (recorded)

- **D1 [DECIDED]: frontend protocol = typed-AST JSON.** `gofrontend` (Go program;
  vendored go/* verbatim, zero local patches per GG-0044) type-checks one package
  and emits `frontend/v1` JSON: AST nodes + per-expression type refs into a
  serialized type table (from go/types) + source positions + package metadata.
  Java `NodeFactory` parses JSON and builds Truffle nodes. Rationale: debuggable,
  versionable, process-boundary clean; the -toolexec wrapper invokes it per
  `compile` action in Phase 4. Falsifier: any go/* API the exporter needs that
  breaks the additive-only assumption (GG-0012) → spike-3 toil re-measurement.
- **D2 [DECIDED]: generics = frontend monomorphization.** `go/types` yields fully
  instantiated types; the exporter emits one node subtree per instantiation
  (functions and methods, incl. generic methods); descriptors are per-instantiation.
  Rationale: simplest correct lowering; static typing already vacates runtime type
  discovery (SPEC §3.3); code size is not a Phase-1 metric. Falsifier: differential
  red on test/typeparam or self-attack pathological generics (higher-order type
  params, recursive instantiation).
- **D3 [DECIDED by SPEC]: panic = `AbstractTruffleException` (lazy traces);
  Goexit = `ControlFlowException`.** No alternative considered (scoped verdict T5 §4.4).
- **D4 [DECIDED]: minimal intrinsic-backed `fmt` stub (revised 2026-10-03).** Corpus
  measurement: GOROOT/test 356 files — 67 need `fmt`, 83 need other stdlib, 192
  need nothing; typeparam 266 — 67 need `fmt`. Excluding all `fmt` users would gut
  ~40% of gate-1 coverage, so Phase 1 ships a stub `fmt` = {Print, Printf,
  Println, Sprint, Sprintf, Sprintln, Errorf} (the pure functions; measured usage
  is Sprintf 407, Printf 115, Println 101). The stub is (a) resolved only via the
  frontend's Phase-1 import overlay, (b) listed symbol-by-symbol in the freeze doc
  as replaceable-in-Phase-3, (c) held to its own verb-conformance matrix
  differential vs gc's `fmt` (verbs × types × flags) — a stub divergence is a
  stub bug, never masked. Files needing Fprintf-to-files, os, net, sync, etc.
  stay on the versioned exclusion list (GG-0033).
- **D5 [DECIDED]: value-model Java mapping.** int8/16/32+uint8/16/32→int;
  int64/uint64/int/uint/uintptr→long; float32→float (custom executeFloat; correct
  rounding is semantic); float64→double; bool→boolean; string→GoString{byte[]};
  slice→GoSlice{typedArray,offset,len,cap}; array→typed Java array (value copy);
  struct→GoStruct{descriptor,long[],Object[]}; map→GoMap; interface→GoInterface;
  func→GoFunc{RootCallTarget,MaterializedFrame|null}; pointer→opaque Java ref;
  unsafe.Pointer→handle-table entry (traps on escape). Per SPEC §3.4.
- **D6 [DECIDED by SPEC]: specialization budget.** PIC (limit ~3, hash fallback)
  only at interface dispatch; decision trees + ValueProfile at type switches;
  table-version-guarded fast path + @TruffleBoundary slow path at maps; typed
  execute methods with no PICs everywhere else. Itab speculation needs no
  Assumptions (immutable itabs).
- **D7 [DECIDED]: freeze covers the full catalog.** The frozen signature set is
  the full ~60–70-function GG-0020 surface (incl. scheduler glue, netpoller API
  contract, timers — Phase-2-owned impls), each marked stub-vs-real; Phase 2/4
  build against it (spec-extract item 52).
- **D8 [DECIDED]: Safepoint seam in Phase 1.** `graalgo.safepoint.Safepoint.poll()` intrinsic
  signature frozen; AST inserts calls at loop back-edges + function entries;
  Phase-1 impl is a no-op; Phase 2 gives it semantics (GG-0017 seam slice).

**Program-owner review 2026-10-04:** D2/D4/D7/D8 approved as decided (no
alternatives to explore). GraalVM pin: **Oracle GraalVM 25.0.4.1.1, LTS line**
— Innovation line (25.4.x) re-evaluated at Phase 5; SPEC Appendix C `[OPEN]`
closed with this pin. **Standing directive:** avoid relying on any deprecated
API wherever possible — enforced by `-Xlint:deprecation -Werror` in the build
(deprecated-API use = build break). Truffle jars in `lib/` re-pinned to
25.0.4.1.1 with SHA256SUMS.

## 4. Milestones and acceptance

- M1: toolchain + `graalgo-run` executes hand-built "hello" AST; diffharness backend registered (GG-0027)
- M2: `gofrontend` emits typed AST; NodeFactory builds straight-line programs (GG-0012→active)
- M3: expressions + values differential-green on arithmetic corpus (GG-0015→active)
- M4: control flow, composite types, maps green (GG-0014→active)
- M5: defer/panic/recover/Goexit green incl. adversarial (GG-0019→active)
- M6: generics green incl. pathological (test/typeparam differential)
- M7: full corpus differential green, zero unexplained divergences → gate 1
- M8: fuzz-soak operational + mutation-validated → gate 2
- M9: interface freeze published (both components) + polymorphism numbers → gate 3
- M10: reviews + self-attack + audit → gates 4, 5 → closeout

**Review gate (program-owner directive 2026-10-04):** every milestone M1–M9
closes with ≥2 independent peer reviews; every finding is remediated and
re-tested before the milestone sign-off is recorded. Reviews run against a
fixed snapshot or diff of the milestone's scope (never the moving live tree);
review evidence (snapshot checksums, per-reviewer reports, findings list with
remediation at named targets) lives under `reviews/`. M10 retains the final
cross-cutting review: self-attack (pathological generics, deep recursion,
defer storms), grep gates, cross-doc consistency audit, closeout.

**Worker commit checklist (M6 P1-4/P1-5 remediation — the 4b24b89 escape):**
4b24b89 committed references to two source files that were still untracked,
so a clean checkout could not build. History is permanent; the process fix
is mechanical. Before every commit the worker MUST:
1. `bash tools/precommit-check.sh` — fails on untracked `*.java`/`*.go`
   outside scratch dirs (a commit must never silently omit sources);
2. `make -C phases/phase1 test` green on the tree as committed
   (never commit red or untested state);
3. one logical change per commit, message naming what and why
   (never batch unrelated changes; never `--amend` someone else's commit —
   fix forward with a new commit, or amend only your own unpushed work).
A commit that fails (1) is not a commit — add the files first.

## 5. Test strategy

`graalgo-run file.go`: frontend → AST → execute `main`; exit code = program's.
Harness build step = `graalgo-run --check` (frontend only, for build-failure
parity); run step = execute. Corpus (deterministic, GOMAXPROCS=1, scrubbed env):
(1) 45 smoke programs (14/15/26 excluded w/ reason codes — Phase 2);
(2) GOROOT/test/*.go 356 files via adapter (filtered: no imports beyond builtins;
exclusions versioned); (3) test/typeparam/*.go 266 files via adapter;
(4) generated programs (seeded, checked in). Every new lowering lands
red-green-first (SPEC §5.2). Each new node family extends mutations.txt.

## 6. Risks and kill triggers

- Systematic semantic divergence vs gc unclosable by intrinsic-surface design → **escalate**
- Interface freeze impossible within toil bound → **architecture replan**
- panic(nil) vs `-lang`<1.21: needs parity stance (GG-0045) before freeze — default:
  replicate gc exactly per -lang gate (to be decided at M5, recorded in freeze doc)
- gosmith [UNVERIFIED] → template generator is the plan; no dependency

## 7. Closeout checklist

[ ] gates 1–5 evidenced [ ] per-milestone review gates closed (M1–M9: ≥2 independent
reviews each, every finding remediated at named targets and re-tested)
[ ] self-attack report [ ] grep gates [ ] cross-doc consistency audit (greenfield 11)
[ ] numbers table with log-line citations (greenfield 9) [ ] GG states recorded

## 8. Falsifiers per owned GG ID (greenfield item 4)

- GG-0012: frontend rebase toil > 200 agent-hours on next dry run → bound broken
- GG-0013: `-lang`-gated file with semantics ≠ gc → differential red
- GG-0014: any mode-specific AST assumption or second backend → review fail
- GG-0015: value-semantics divergence (copy/reference/nil) vs gc → red
- GG-0016: escaping unsafe arithmetic that doesn't trap, or any silent divergence → P0
- GG-0019: defer/panic/recover/Goexit/panic(nil) ≠ gc → red
- GG-0020: AST calls a non-frozen signature → CI freeze gate red
- GG-0021: stub symbol-name mismatch vs catalog → freeze review fail
- GG-0059: freeze incomplete at M9 → gate 3 red; Phase 4 blocked
- GG-0009: any local go/* patch without GG-0044 accounting → fail
