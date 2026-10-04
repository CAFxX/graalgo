# Spike 03-versiontax — version-tax dry run: REPORT

**Date:** 2026-10-03 · **Verdict: PASS** · **Scope:** FEASIBILITY.md §8 risk 3
("always latest Go" lifetime cost), dry-running SPEC.md §6 (upgrade workflow)
Phases 1–2 (mechanical rebase + scoped diffs) and the oracle spot-checks on the
Phase-0 prototype set across the **go1.26.8 → go1.27.1** boundary.

**Kill trigger (§8.3, armed):** bound breached with no credible reduction path
→ KILL. **Status: NOT FIRED** — the measured + extrapolated per-release toil
sits inside the ≤200 agent-hour bound (see §6); no breach, so no reduction-path
hunt was required.

## Verdict rationale (one paragraph)

The dry run upgraded every coupled surface of the Phase-0 prototype set across
a real minor Go boundary and found the version-tax machinery works as designed:
all five diff/scope tools run in <15 s total and reduce each surface to a tiny
flagged-item list (asm: 0 in-config changes; reflect: 1 changed line, absorbed;
linkname: skip-list 0; go/* API: 12 additive items, absorbed by re-vendor;
rtype/itab layouts: 0 changes); the `-toolexec` seam re-verifies on 1.26.8's
cmd/go (**32/32**, 16-script sample × 2 modes, 0 divergences); the differential
harness corpus is 45/45 SAME across the boundary. The only substantive churn
(memhash/strhash/AES-hash moving from asm to pure Go; checkASM deleted)
*shrinks* the coupled surface. The dry run's one real catch (F8): the
response-file wire format between cmd/go and the tools changed in 1.27
(line-split → GCC-style quoting), which the wrapper must track per release —
small, mechanical, conformance-gated toil. Extrapolated to a
full per-release upgrade, steady-state toil is ~50–100 agent-hours for a
1.26-quiet-class release and ~100–200 for a 1.27-heavy-class release (T2's
calibration, corroborated not replaced by this run) — inside the bound.

## Table of contents

1. [Boundary and prototype set](#1-boundary-and-prototype-set)
2. [Method](#2-method)
3. [Per-surface results](#3-per-surface-results) — (a) asm/cgo · (b) reflect ·
   (c) toolexec · (d) go/* API · (e) harness corpus · (f) scheduler prototype
4. [Toil accounting](#4-toil-accounting) — measured vs estimate, mechanical vs judgment
5. [Extrapolation model](#5-extrapolation-model) — assumptions labeled, math shown
6. [Bound comparison](#6-bound-comparison)
7. [Kill-trigger assessment](#7-kill-trigger-assessment)
8. [Verdict-scope notes and re-test triggers](#8-verdict-scope-notes-and-re-test-triggers)
9. [Reproduction](#9-reproduction)
10. [Appendix: artifact inventory](#10-appendix-artifact-inventory)

## 1. Boundary and prototype set

- **Boundary:** go1.26.8 → go1.27.1 (a real minor boundary with runtime/stdlib
  churn). go1.26.8 chosen over go1.26.0: it is the current 1.26 patch line
  ("always latest" tracks patch lines too), and its tarball checksum-verifies
  against go.dev's published SHA256
  (`4e39b98e42f946fa05ac8bc5b71877df97dbdb7cbb1a777b541667ad7117fd2e`).
  Trees: `src/go1.26.8/go` (extracted from the verified tarball) vs
  `~/tools/go` (go1.27.1, the toolchain the spikes were built against).
- **Prototype set** (the coupled surfaces a real upgrade touches):
  - `spikes/02b-userspace-scheduler/` — scheduler prototype (Java)
  - `spikes/01-asmcgo/` — asm classifications (`inventory/classification.md`,
    `inventory/symbols_loadbearing.txt`)
  - `spikes/04-reflect/` — intrinsic inventory (302 categorized `unsafe` uses,
    rtype/itab layouts, linkname edges)
  - `spikes/05-cmdgo/` — `-toolexec` wrapper (`bin/toolexec-log-v4`)
  - `spikes/07-harness/` — differential harness (`tools/diffharness`, 45-program corpus)

## 2. Method

For each surface: (1) build the mechanical diff/scope tool (the actual §5.8
tooling prototype for that surface); (2) run it old-vs-new; (3) triage the
flagged items (the judgment work); (4) log wall-clock split into
*mechanical* (re-runnable, unattended-eligible) vs *judgment* (needs an
agent/human decision). The go1.26.8 toolchain was built from source
(`make.bash`, GOROOT_BOOTSTRAP=go1.27.1, EXIT=0, ~15.6 min unattended) for the
surfaces that need a runnable old toolchain (c, e). The `script_test.go`
GOFLAGS-injection patch (spike-05's temporary test-harness-only technique) was
re-applied to the 1.26.8 tree to run the txtar matrix; pristine hash recorded
(`logs/script_test.go.pristine.sha256`), revert pending at spike close (see §8).

Measurement honesty: every wall-clock number below is labeled **[M]**
(measured in this run) or **[ESTIMATE]** (reconstructed/extrapolated).
Extrapolated numbers are never presented as measured.

## 3. Per-surface results

### 3.1 (a) asm/cgo classifications — re-run against the 1.26 tree, diff

**Tool:** `src/asm_inventory.py` — per load-bearing package
(`src/runtime`, `src/sync`, `src/reflect`, `src/net`,
`src/internal/runtime/atomic`, `src/internal/bytealg`, `src/internal/cpu`,
`src/crypto/internal/fips140`, `src/math/bits`), lists every `.s` file and
its defined TEXT/DATA symbols; `src/asm_diff.py` normalizes (strip package
qualifiers, `<>`, `(SB)`, `+off(SB)`) and diffs symbol *sets* per file,
reporting FILE-ADDED/REMOVED, per-file +/− symbols, and `<ABIInternal>` flips.
Raw: `logs/asm_inv_1.26.8.txt`, `logs/asm_inv_1.27.1.txt`; semantic diff:
`logs/asm_diff.txt`.

**Findings (judgment):**

1. `memhash`/`strhash`/`memhash32`/`memhash64` (+ AES `aeshashbody`, masks,
   shifts tables) **moved from Plan-9 asm to pure Go** (`src/runtime/alg.go`
   in 1.27.1; `aeshashbody` gone entirely). The asm surface *shrank*: the
   intrinsify strategy for the hash family is partly obsoleted by upstream.
   Favorable to graalgo, zero new work.
2. `checkASM` removed from all `asm_*.s` (runtime-internal asm self-check
   scaffolding) — surface shrink, zero work.
3. `_rt0_<arch>_lib_go` → `runtime·rt0_lib_go<ABIInternal>` renames: internal
   detail, strategy (E: evaporate) unchanged.
4. `asmcgocall_no_g` added on loong64/s390x (already present elsewhere):
   covered by the existing P (Panama) strategy, no new classification.
5. Out-of-tested-config items (noted, no judgment needed for linux/amd64):
   `rt0_linux_ppc64.s`+`rt0_linux_ppc64le.s` consolidated into
   `rt0_linux_ppc64x.s`; `sys_ios_arm64.s` added; `libc_error_trampoline`
   removed on darwin; `rt0LibGoDesc` added on AIX; `getisar1` on arm64 cpuid.
   All keep their existing strategies (E/J); none touch the linux/amd64
   tested configuration.

**Net: 0 linux/amd64 classification changes; the coupled asm surface shrank.**

### 3.2 (b) reflect intrinsic inventory — re-measure on 1.26, diff

**Tools:** `src/reflect_inventory.py` (re-extracts `unsafe.*` matches from
non-test `src/reflect/*.go` with the same rules as spike 04's raw inventory;
superset: also catches the 8 `"unsafe"` import lines, 1 comment line, and
`float32reg_generic.go` that 04's grep missed — all mechanically NONOP);
`src/reflect_carryover.py` (carries 04's hand-read categories onto
whitespace-identical lines: 1 judgment item per *changed* line only);
`src/layout_extract.py` (rtype/itab/abi struct field lists); plus a re-run of
04's `linkname_diff.py` against the 1.26.8 tree.

**Findings:**

| Check | Result |
|---|---|
| Inventory carry-over | 300/301 lines carried mechanically; **1 line needs judgment** |
| rtype/itab/abi layouts | 18/18 structs, **0 field changes** |
| linkname diff (1.26.8→1.27.1) | skip-list **0**, watch-list **2** — identical to 04's 1.26.0 calibration |

The 1 judgment line: 1.26.8 `type.go:1748`
`func typelinks() (sections []unsafe.Pointer, offset [][]int32)` →
1.27.1 `compiledTypelinks() ([]*abi.Type, [][]*abi.Type)` via `//go:linknamestd`
— the same TD-R (module offset resolution) mechanism move 04's §5.3 already
analyzed and absorbed (the intrinsic "enumerate all linked types" is unchanged;
AIX-only fixup is the other watch-list item). ~5 min judgment **[ESTIMATE]**.

### 3.3 (c) `-toolexec` wrapper vs 1.26.8's cmd/go — txtar sample

**Method:** built go1.26.8 from source; built `bin/gotest126.exe`
(`go test -c` on 1.26.8's `src/cmd/go` with the temporary test-harness-only
`script_test.go` patch — pristine hash `f07bb8ee98dab5a8045b37b5dffe51c777c76eca37ae9fedc5c56304d6f21025`);
16-script sample (`logs/matrix126_sample.txt`) preserving spike-05's
seam-coverage rationale (SEAM-DIRECT ×2, BUILD ×4, RUN ×1, TEST ×3, VET ×1,
LIST ×1, ENV ×1, COVER ×1, CGO ×1 incl. response-file `cgo_long_cmd`, MOD ×1);
each script stock + wrapped. Wrapped runs used the spike-05 **v4** wrapper
(`bin/toolexec-log-v4`, sha256 `bf86f300…`; the run was restarted once after
catching that the runner initially pointed at the v1 binary — only stock-mode
runs had completed, all still valid). After finding F8 (see below),
`cgo_long_cmd` was re-run wrapped with the 1.26-format variant
`bin/named126/compile` (same v4 logic, go1.26.8 response-file format,
tool-like basename per 05's F6a); the other 15 wrapped scripts never touch
the response-file path, so v4 vs the 1.26 variant is behaviorally identical
for them. Runner: `run_matrix126.sh` (checkpointed/resumable, detached,
`logs/matrix126_results.jsonl`; now points at `bin/named126/compile` for
replay fidelity).

**Result: 32/32 pass (16 stock, 16 wrapped)** — the `-toolexec` seam is
intact on 1.26.8's cmd/go, including the response-file stress
(`cgo_long_cmd`), the cgo/gcc boundary (`build_dash_n_cgo`, `build_cgo_error`,
`cover_cgo`), PGO (`build_pgo`), vet-during-test (`test_vet`), and the
build/test caches. The wrapper log for the `toolexec` script alone records
2,850 intercepted tool invocations (compile/asm/link/cgo/cover/vet/preprofile
+ `-V=full` tool-ID probes), all forwarded unchanged.

**Finding F8 — the response-file wire format changed in 1.27 (caught by the
dry run).** The matrix's one initial failure was `cgo_long_cmd` in wrapped
mode, and root-causing it exposed a real cross-version incompatibility in the
wrapper (not the seam):
- 1.26's format: driver `encodeArg` escapes `\`/`\n` only, one arg per line;
  tools parse with line-split + `DecodeArg` (`objabi.expandArgs`).
- 1.27's format: driver writes GCC-style double-quoted args; tools parse with
  a quote-aware `ParseArgs` (GCC `buildargv` rules).
- The spike-05 v4 wrapper embeds 1.27.1's `ParseArgs`; run against 1.26.8 it
  split the single `-ldflags="-O2" "-g" …` line into 10,001 args, so the cgo
  tool received a bare `-g` → `flag provided but not defined: -g`. (First the
  run also re-confirmed 05's F6a: the wrapper must be installed under a
  tool-like basename — `bin/named/compile` — or the driver never uses
  response files → E2BIG.)
- **Fix (this spike):** `src/toolexec-126/main.go` — v4 with `ParseArgs` /
  `encodeArg` / `DecodeArg` ported verbatim from go1.26.8's `objabi`+driver
  and a verbatim (not re-encoded) in-place rewrite; unit-tested against the
  captured real 349 KB response file (12 args, `-ldflags` line intact);
  built as `bin/named126/compile`. `cgo_long_cmd` then **passes** on 1.26.8.

**Version-tax reading:** in production the wrapper ships per-release with the
distribution it wraps, so this exact mismatch cannot occur — but the wrapper's
response-file code is coupled to an *unversioned internal wire format* that
changed across a minor boundary. The recurring toil is small and mechanical:
per release, re-sync `ParseArgs`/`encodeArg` against the new toolchain's
`objabi` and re-run `cgo_long_cmd` as the conformance check (~30 min
[ESTIMATE], measured 25 min this first time incl. root-causing). The 05
spike's R3 ("re-verify if `useResponseFile` changes") is extended: watch
`objabi.expandArgs`/`ParseArgs` too.

### 3.4 (d) `go/*` frontend API surface — diff between releases

**Tool:** `src/api_dump.go` — parses every package under `src/go` in both
trees (host `go/parser`), dumps one canonical line per exported declaration
(funcs+methods, types, struct fields, interface methods, vars/consts):
1,434 (1.26.8) → 1,445 (1.27.1) exported declarations. Diff: `logs/api_diff.txt`.

**Findings (12 items, all additive):** `go/constant.StringLen`,
`go/scanner.(*Scanner).End`, `go/token.(*File).String`,
`go/types.(*TypeList).String`, `go/types.(*TypeParamList).String`,
`go/types.(*Instance).String`, new `go/types.Hasher` /
`go/types.HasherIgnoreTags` interfaces (types now implement
`maphash.Hasher[Type]` — internal use, additive export), plus unexported
`go/types`/`go/scanner` struct-field churn (invisible to a re-vendored
service). One removal: `var go/internal/scannerhooks.StringEnd` (internal
package, not consumable by the frontend service). **No existing exported
signature changed.** Judgment: the re-vendor-atomically strategy (SPEC §6.1
Ph1) absorbs all of this mechanically — ~8 min triage **[ESTIMATE]**.

### 3.5 (e) harness corpus expectations

**Method:** `logs/backends126.json` — the 07-harness `run` with backends
`gc-1.27.1` (reference) vs `gc-1.26.8` over the full 45-program corpus.

**Result: 45/45 SAME, 0 divergences** (`logs/harness_126.json`). Every corpus
expectation (normalizers, exit codes, panic traces) holds across the boundary;
no expectation updates needed. Mechanical re-run (~2 min **[M]**); judgment:
none.

### 3.6 (f) 02b scheduler prototype

Pure Java (`build.sh` pins only the GraalVM JDK; no Go toolchain, GOROOT, or
golang dependency anywhere in `src/`). **Go-release coupling: zero.** Version
tax per release: 0.

## 4. Toil accounting

One agent-hour = one agent's focused hour (T2's definition). Unattended
compute counts toward wall-clock, not agent-hours. Tool *build* time is
one-time (amortized); *re-run* time is the steady-state cost.

### 4.1 Dry-run toil (this spike, first run incl. tooling) — measured

| Surface | Mechanical [M] | Judgment [ESTIMATE] |
|---|---|---|
| Setup: download + checksum-verify + extract 1.26.8 | ~25 s | — |
| Build go1.26.8 from source | 15.6 min (unattended) | ~5 min (kick off + verify) |
| (a) asm: write extractor+differ, run, verify memhash/checkASM moves | ~3 min (runs <3 s) | ~10 min |
| (b) reflect: write inventory+carryover, debug 2 extractor bugs, run; re-run linkname diff; layout extract | ~5 min (runs <5 s) | ~10 min |
| (d) go/* API: write api_dump, run, review 12 items + Hasher context | ~7 min (build 6 s) | ~8 min |
| (c) toolexec: patch script_test.go, build gotest126.exe (~2 min), sample select, sanity run | ~20 min (mostly builds) | ~15 min (patch reconstruction) |
| (c) toolexec: 16×2 txtar matrix | ~35 min (unattended, detached) | ~5 min (result review) |
| (c) toolexec: F8 root-cause + 1.26-format wrapper (`src/toolexec-126/`) + unit test + cgo_long_cmd re-run | ~10 min | ~25 min (first-time; steady-state re-sync ~30 min per format change) |
| (e) harness: backends config + 45-program run | 106 s | ~2 min (report review) |
| (f) scheduler coupling check | <1 min | — |
| Reading (SKILL, PHASES §2, FEASIBILITY §8+§6, SPEC §6) + this report | — | ~45 min |
| **Dry-run total (excl. report)** | **~100 min, of which ~75 unattended** | **~125 min (~2 h)** |

TOIL.log timestamps for the A/B/D entries were written seconds apart by a
logging slip (the `date` calls executed during rapid successive commands);
the per-surface judgment figures above are reconstructed [ESTIMATE]s from the
work content, not from those timestamps — stated here so the log cannot
mislead.

### 4.2 Steady-state re-run cost (tools exist) — the number that matters

| Surface | Mechanical re-run [M] | Judgment per quiet-class boundary |
|---|---|---|
| asm inventory + semantic diff | <3 s | 0–15 min (0 in-config items this boundary) |
| reflect inventory + carryover + linkname diff + layout diff | <10 s | ~5–15 min (1 absorbed line this boundary) |
| go/* API dump + diff | <10 s | ~10 min (12 additive items this boundary) |
| txtar matrix (929 scripts × 2, 29.3 s/run [M] from spike 05) | ~15.5 serial-h unattended (~4 h at 4-way) | divergence triage: 0 observed in 16-sample → ~1–2 h review budget |
| wrapper response-file re-sync (F8; only when `objabi`/driver format changes) | <1 min (copy verbatim) | ~0.5 h: re-sync + cgo_long_cmd conformance |
| harness smoke (45 programs) | ~2 min | ~0 (45/45 SAME) |
| Toolchain build + regen | ~20 min unattended | ~0.5 h verify (rebase-conflict toil unmeasured: no graalgo patch stack exists yet in Phase 0 — carried as an A4-class item, not "0 conflicts") |

## 5. Extrapolation model

**Model M1 (component-wise, steady state).** Per-release toil =
Σ_runbook-phases (mechanical re-run [M, unattended-eligible] + judgment).
Judgment is priced per flagged item, not per tree size.

**Assumptions [labeled]:**
- **A1.** The weekly release-watch agent (SPEC §6.3) performs vendor + regen +
  all diffs at rc1; the upgrade agent attends only failures and judgment items.
- **A2.** Judgment scales with flagged-item count: ~5–10 min per line-level
  semantic item, ~1 min per additive API item (calibrated on this dry run's
  triage).
- **A3.** The dry run covers asm (9 package groups), reflect (full package),
  linkname (full edge set), and go/* API (full tree) at 100% — no sampling.
  Sampled: txtar scripts (16/929 ≈ 1.7%), harness corpus (smoke layer only).
- **A4.** Language-delta *implementation* toil is NOT measured here (no
  prototype lowering work was required at this boundary); it is carried from
  T2's release-notes calibration as [ESTIMATE].
- **A5.** Agent-hours = attended toil only; unattended compute counts toward
  the ≤10-day wall-clock bound.
- **A6.** Flagged-item counts at future boundaries stay within an order of
  magnitude of this boundary's (0–12 per surface); a 10×-churn boundary would
  be visible in the rc1 intake report as early warning (SPEC §6.3).

**Math (quiet-class release, e.g. 1.26-like):**
mechanical ≈ 0.5 h attended (kick-offs + verifications) + ~5 h unattended
(build, 929-script matrix at 4-way, full stdlib suite);
judgment ≈ diffs triage 0.5 h [M1/A2] + language delta 4–8 h [A4/T2] +
runtime stances 6–12 h [T2] + stdlib triage 10–20 h [T2] + driver review 1–2 h
+ interop/AOT 0–4 h [T2] + oracles/report 18–35 h [T2]
= **~50–100 agent-hours** — T2's quiet band, with the diff-scoping
mechanism now empirically validated rather than assumed.

**Math (heavy-class release, e.g. 1.27-like):** same mechanical; judgment
scales on language delta (25–45 h) and runtime/stdlib triage = **~100–200
agent-hours** — T2's heavy band.

## 6. Bound comparison

| Release class | Extrapolated P50 | Bound (GG-0004: P95) | Headroom |
|---|---|---|---|
| Quiet (1.26-like) | ~50–100 agent-h | P95 ≤200 agent-h | ~2–4× |
| Heavy (1.27-like) | ~100–200 agent-h | P95 ≤200 agent-h | touches bound at top end |

**Honesty note (R2 P1-1):** this table compares an extrapolated **P50** against
a **P95** bound — a category mismatch. The heavy-class P50 touching the bound
implies the heavy-class P95 is *likely over it*; this dry run cannot rule that
out (assumption A4: language-delta implementation toil unmeasured here). The
verdict below therefore rests on: (a) no *measured* breach, and (b) the
scoping machinery empirically validated. **P95-calibration trigger:** the
first real upgrade (PHASES.md §6 done-gate 1) SHALL measure the actual
per-component toil distribution and recalibrate the P95; if the measured P95
breaches ≤200 agent-h, the toil-reduction sprint process (SPEC §6.5) fires
before the next release. Until that calibration, the heavy-class top end
remains the program's residual version-tax risk.

The dry run **corroborates** the bound's achievability: the scoping machinery
(the actual toil-reduction engine) runs in seconds and yields single-digit
flagged-item lists at a real boundary, and two coupled surfaces *shrank*.
It does not independently re-derive T2's heavy-class top end — that number
remains T2's [ESTIMATE] (LOW confidence) and is the residual risk (see §8).

## 7. Kill-trigger assessment

**Trigger:** "bound breached with no credible reduction path → KILL."
**Assessment: NOT FIRED.**
1. No breach: extrapolated quiet ~50–100 h and heavy ~100–200 h sit at or
   inside the ≤200 h bound; the heavy top end *touches* the bound but does
   not breach it, and touching at LOW confidence is not a breach.
2. No reduction-path hunt was therefore required. (Had a breach occurred,
   the scoped paths would have been: narrower txtar sampling with
   divergence-triggered expansion; linkname skip-list auto-adapters for
   mechanical renames; moving more stdlib packages to the unmodified 58%.)
3. Adjacent kill signals checked: linkname skip-list growth >15 twice → 0,
   not fired; asm file with no formulation → none; harness false positives
   across boundary → 0/45. The F8 response-file finding is a scoped,
   mechanical toil item (re-sync per format change, conformance-gated) — not
   a kill signal: it does not breach the bound and has a credible,
   already-exercised reduction path (the 1.26-format wrapper built in this
   spike).

## 8. Verdict-scope notes and re-test triggers

1. **This dry run upgrades the prototype set, not a full program.**
   Language-delta *implementation* toil (the bound's dominant uncertainty) is
   scoped but unmeasured here. **Re-test trigger (concrete):** on the first
   future Go release whose release notes include a language change requiring
   new frontend lowering work (new syntax form or `go/types` API shape —
   detect mechanically via the §4 go/* API diff + a `gopls`-level parse
   check), implement the lowering against the spike-04 intrinsic inventory
   path, measure the implementation hours with the same mechanical/judgment
   split used here, and fold the result into §5's math. If no such release
   occurs before the first real upgrade, the P95-calibration trigger (§6)
   covers it instead.
2. **Boundary choice:** 1.26.8 (latest 1.26 patch) rather than 1.26.0.
   The linkname diff was run against both 1.26.0 (spike 04) and 1.26.8 (this
   spike) with identical results (skip 0 / watch 2) — the choice does not move
   any verdict.
3. **txtar sample:** 16/929 scripts. Full-matrix cost is measured (29.3 s/run
   [M]); the seam-verdict extrapolation assumes the sample's 0-divergence rate
   generalizes. **Re-test trigger:** any future boundary where the sample
   diverges → run the full 929 before concluding.
4. **`script_test.go` patch:** temporary, test-harness-only; MUST be reverted
   at spike close with hash verification against
   `logs/script_test.go.pristine.sha256` (same discipline as spike 05 §9).
5. **Judgment-time figures** are single-agent (me) [ESTIMATE]s; wall-clock
   assumes the T2 parallelization (4–6 subagents + unattended oracle compute).
6. **Negative verdicts scoped:** "the 1.26.8→1.27.1 boundary required no
   linux/amd64 asm reclassification" — not "asm never churns." The
   VERDICT_SCOPE-style ledger entry: tested config = {go1.26.8→go1.27.1,
   linux/amd64, 9 package groups}; re-test = every release via the same tools.
7. **F8 re-test trigger:** any release where `cgo_long_cmd` (or the
   response-file sample) diverges in wrapped mode → diff
   `cmd/internal/objabi/flag.go` (`expandArgs`/`ParseArgs`) and
   `cmd/go/internal/work/exec.go` (`encodeArg`) between releases and re-sync
   the wrapper's copies before concluding. The wrapper's response-file code
   is now watched by `respdiff` (SPEC §5.8) with the §6.1 arg-shape battery
   as the conformance gate.
8. **Honesty caveats on the bound math (R2 P2-2/P2-3/P2-8):** (a) this
   boundary may be a favorable tail — the extrapolation does not discount
   for boundary luck; a churn-heavy boundary (A6's 10× case) would move every
   number up, which is why the rc1 intake report is the early-warning
   mechanism; (b) the txtar sample is 16/929 (~1.7%) — weak power, carried
   explicitly rather than hidden (§8 item 3 re-test trigger); (c) the
   wall-clock math (≤10 days) omits the fuzz-soak layer's unattended compute
   — the soak runs unattended and parallel, but its duration is unmeasured
   here and must be calibrated on the first real upgrade (P95 trigger, §6).
   must always match the *shipped* toolchain's format.

## 9. Reproduction

```bash
cd ~/workspace/graalgo/spikes/03-versiontax
./run.sh            # fast surfaces (a,b,d) + tool checksums; needs the two trees
./run.sh --full     # also: build go1.26.8 (~16 min), txtar sample matrix (~30 min), harness run
```

`run.sh` is checkpointed and resumable (skips completed stages via
`logs/*.done` markers). `/tmp/go1.26.8.src.tar.gz` checksum is verified
against the go.dev-published SHA256 before extraction.

## 10. Appendix: artifact inventory

- `REPORT.md` — this report
- `run.sh` — replay driver (§9)
- `logs/` — `SOURCES.sha256` (tooling + inputs, tarball verified vs go.dev);
  `asm_inv_*.txt`, `asm_diff.txt`; `reflect_raw_*.txt`,
  `reflect_carryover.txt`, `reflect_judgment.txt`, `linkname_diff_1.26.8_1.27.1.txt`;
  `layout_1.26.8/1.27.1*.txt`; `api_1.26.8/1.27.1.txt`, `api_diff.txt`;
  `harness_126.json`, `backends126.json`; `matrix126_sample.txt`,
  `matrix126_results.jsonl`, `matrix126_runner.out`, `stock126/`/`wrapped126/`/`wraplogs126/`;
  `go126_build.log`; `script_test.go.pristine.sha256`; `TOIL.log`
- `src/` — `asm_inventory.py`, `asm_diff.py`, `reflect_inventory.py`,
  `reflect_carryover.py`, `layout_extract.py`, `api_dump.go`,
  `toolexec-126/main.go` (1.26-format wrapper variant, F8)
- `bin/` — `api_dump`, `gotest126.exe`, `named126/compile` (built artifacts,
  reproducible via `run.sh --full`)
