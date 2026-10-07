# Methodology review — P3-M8 wave 2 (reviewer 2)

Reviewer: independent methodology reviewer (did not design this work).
Tree: `~/workspace/graalgo-m8-pipe`, branch `m8-pipeline` (read-only review;
this document is the only write). Date: 2026-10-07 ~17:30 CEST.

Method: re-read RULING.md (R1–R13), INTEGRATION-PLAN.md, corpus/wave2/README.md
+ run_wave.sh, all 31 staged patches, SYNTHESIS.md + per-package analysis docs,
CHECKPOINT.md entries, and 8 corpus programs. Re-ran the evidence where it was
cheap: created a pristine scratch worktree at 720731d and applied every staged
patch standalone (`git apply --check`: **all 31 clean**) and sequentially in
the plan's integration order (found the conflicts below). Verified corpus
counts by listing, `.divergent`/`.panic` pairing by inspection, and the
`.args` gap by grepping the runner.

## Verdict: REQUEST_CHANGES

The design rulings are sound (see §A) and the corpus is genuinely adversarial
(see §C), but the integration plan is stale and incomplete (§B), two corpus
harness gaps void coverage of ruled behaviors (§D), worker counts are
internally inconsistent (§E), and — most fundamentally — **nothing about
graalgo's behavior is proven yet**: zero graalgo legs have run because all
shared-file integration is staged for rebase (§F). Sign-off is not possible
now regardless of the other findings.

## A. Coordinator rulings — assessment

**R1 (linkname over wire nodes): SOUND and correctly justified.** The os
worker's analysis (os.md §2.1) already recommended the ADR-0004 path; the
ruling's five reasons check out: (1) exporter frozen — consistent with the
pipelining constraint; (2) `Linknames` (P3-M4 WS C) exists for exactly this —
verified `Linknames.java`: symbol registry, arity-checked dispatch, fail-fast
on unknown symbols; (4) D5 boxing covers the needed shapes — verified against
the frozen §2.4 symbol table: every symbol takes/returns only scalars (Long),
strings (GoString — documented in `Linknames.java`), and byte buffers
(GoSlice, which is a D5 reference type per GoSlice.java) — no struct returns,
no callbacks; (5) bring-up-not-optimization is a reasonable scope cut. The
syscall worker's OP INVENTORY and SCOPE VERDICT were preserved; only the
mechanism changed. No gap found.

**R2 (os owns I/O; syscall I/O wrappers fail-fast): COHERENT.** The single
cookie-domain design (GoOs.java, new file) avoids the two-domain seam the
ruling names (`Fd()` returns the cookie per os.md §5; `SyscallConn`/`NewFile`
fail-fast in §6.4; syscall.Open et al. fail-fast per R2 so no second domain
can form). The "dissolves Stat_t/linux_dirent64/Utsname" claim is substantiated
in the §2.4 table (scalar `stat*` intrinsics via the NIO `unix:` attribute
view; `dirNext` iterator encoding; Uname via InetAddress + /proc fallback).
The joint re-test trigger (fd-domain unification, Phase 4/Panama) is recorded.
One residual risk, not a ruling flaw: `Fd()` returning a cookie is a documented
approximation that only stays coherent while syscall cannot consume fds — the
re-test trigger covers exactly this. Fine.

**R12 (desugar entries only with real helpers): RIGHT.** Directionally the
correct call (earlier, louder failure beats a runtime panic), and the io
worker executed it properly: 00-INDEX.md documents name/field-for-field
verification of all 11 entries against synth_io.go / synth_strings.go, and I
spot-checked that the referenced `__`-helpers (`__pipe`,
`__pipeReaderRead`, `__sectionReaderSeek`, `__sectionReaderOuter`,
`__offsetWriterSeek`, …) exist in synth_io.go. Micro-caveat (not a finding):
the rationale's exact failure mode ("a tabled entry without a helper compiles
then panics at runtime") is asserted, not demonstrated — a helperless entry
would more likely fail at export/wire time. The discipline itself is sound.

**R11 SUPERSEDED / R13:** both are coordinator-verified improvements, not
process gaps. R13's doc update (os.md §2.4 entropy row) and the
synth_crypto_rand.go rewrite are in the tree; the rand.Text fix (26
chars/base32 per go1.27.1 text.go) is correct.

## B. Integration plan — findings

**F1 [P0] INTEGRATION-PLAN.md is stale: 8 patches have no conflict analysis.**
The plan was written 2026-10-07 ~16:40; crypto landed ~17:00 and reflect
~17:15, and the plan was never updated. Its C1–C5 inventory covers only
io/os/syscall/unsafe. The reflect (6) and crypto (2) patches touch the same
shared files — synth_machinery.go (reflect/01, crypto/01), mrdesugar_m5.go
(reflect/04, crypto/03), export.go (reflect/02, reflect/06), P3SyncMain.java
(reflect/05) — with zero documented resolutions. The plan's step 7
("reflect/*, crypto/* (workers running; same treatment on arrival)") is not a
plan; it is a placeholder. My sequential apply confirms real collisions
(F2–F4). Fix: extend C1–C5 (or add C6–C8) with the reflect/crypto patches
before rebase; do not integrate from this plan as written.

**F2 [P0] C3's participant list is factually wrong.** The plan says the
synth_machinery.go:499 gate conflict is "unsafe/02 vs syscall/01 vs os/01".
Verified by reading the patches: **syscall/01 does not touch the 499 gate**
(its hunks are at 66 and 94); **reflect/01 does** — unsafe/02, os/01, and
reflect/01 each replace the *same* old gate line with a different
`&& path != "…"` extension (`"unsafe"` / `"os" && "io/fs"` /
`"reflect" && "internal/reflectlite" && "iter"`). A merge following the plan's
participant list would omit reflect's clause and silently strand the reflect
package at the import gate. Fix: correct C3 to unsafe/02 + os/01 + reflect/01
and write the single merged conjunction explicitly in the plan.

**F3 [P1] Undocumented P3SyncMain.java triple conflict.** The plan never
mentions P3SyncMain.java, yet three patches touch it: os/05 (argv), unsafe/05
(registration block), reflect/05 (registration block). Verified on scratch:
after os/05 + unsafe/05 apply, **reflect/05 fails** (`patch failed:
P3SyncMain.java:226`) — unsafe/05's `factory.registerExprKind("unsafe…")`
block sits exactly where reflect/05's hunks anchor (its context includes the
`mathfma` registration line). No duplicate kind names (reflect registers only
`reflectop`; verified), so the merge is mechanical — but it is unanalyzed and
unowned. Fix: add it to the plan with an explicit order (e.g. unsafe/05,
then reflect/05's block after it, os/05's argv hunks are in disjoint regions).

**F4 [P1] Duplicate behavioral edit: syscall/01 and os/01 both flip
`parser.ParseFile` to `parser.AllErrors|parser.ParseComments`.** Same old
line, same code change, different comments ("synthetic syscall binds the
frozen graalgo.os.* table" vs "linkname directives in synthetic sources
(os)"). Two workers made the identical shared-file behavioral edit
independently — neither knew about the other. Naive sequential application
fails on the second. Worse, this changes parsing for **all** synthetic
sources, and the "comment-free sources are unaffected" assurance is
worker-asserted, not tested. Fix: dedup to a single hunk with one owner at
integration, and add a regression check that doc-comment attachment does not
alter export of the existing (M7) synthetic packages.

**F5 [P2] Plan order inverts a flagged cross-track dependency.** syscall/01's
own header warns: the synthetic syscall source imports `io/fs`, so "the os
worker io/fs registration (R5) must land first or together with this entry."
The plan applies syscall (step 5) *before* os (step 6, which registers
`io/fs`). Harmless at rebase (everything lands in one integration), but the
plan should state the constraint rather than contradict it.

**F6 [P2] io/02 carries a whitespace defect** (`patch:47: space before tab in
indent`). Applies cleanly, but the "patches must be machine-generated" lesson
should include a `git apply --check` + whitespace-error gate on the
coordinator's side.

## C. Corpus methodology — assessment

**The `.divergent` marker is SOUND, not a loophole.** The runner enforces the
key invariant: `.divergent` without `.panic` is a harness FAIL (verified in
run_wave.sh), so divergence can never be silent. All 6 markers (reflect
09/10/11, unsafe 03a/03b/03c) have `.panic` pins, and the sources document the
divergence and its design justification (spot-checked unsafe/03a_add_trap.go:
D5/GG-0015 rationale present). Each maps to a frozen design decision (D5
pointer-arithmetic traps; type-constructor fail-fasts), not to an open bug.
Residual P2: markers are self-approved by workers — recommend a coordinator
allowlist of approved divergences in RULING.md so a future worker cannot bless
a real bug as "intentional."

**The corpus is adversarial ENOUGH — spot checks (4 programs):**
1. `reflect/01_unexported_read.go` — recovers the expected flagRO panic and
   prints `panic1: <msg>`; a missing flagRO propagation prints "no panic
   (unexpected)" and fails. Directly targets the analysis's top risk (a).
   Capable of failing. ✓
2. `unsafe/02_pointer_roundtrip.go` — pins interning `==` parity, nil
   round-trips, `*T→Pointer→*T` referent preservation. Capable of failing. ✓
3. `crypto/aes/01_fips197.go` — real FIPS-197 Appendix B vectors
   (AES-128/256 encrypt+decrypt round-trip). Not a tautology. ✓
4. `io/08_error_vars.go` — pins every error var's message + seek consts.
   Weak but legitimate surface pinning; fails on message drift. ✓
Also verified: `os/06_identity.go` prints only relations/stable values (rule-1
compliant); `unsafe/03a` documents its divergence. The `.scopelimited`
convention is well-designed (gc leg still runs, so invalid Go surfaces as
FAIL; missing `retest:` is a harness FAIL).

## D. Corpus harness gaps — findings

**F7 [P1] `run_wave.sh` ignores `<base>.args` — R6's argv behavior has zero
corpus coverage.** `grep -n "args\|\.args" run_wave.sh` returns nothing: the
runner never reads `os/10_args.go.args` ("hello world 123") and never passes
`--`. Yet `10_args.go`'s header claims "The runner passes the fixture args
after `--`" — false. The program passes vacuously today (both legs print `0`
with no args) while the actually-staged behavior (os/05's argv parsing, args
after `--`, `GoOs.init(argv)`) is exercised by nothing. And os.md §4.1
explicitly required "a `<base>.args` runner convention (like
`.timing`/`.panic`) in `run_wave.sh`" — the requirement was dropped between
analysis and implementation. Fix: implement `.args` handling in run_wave.sh
(pass contents after `--` to both legs), or delete the fixture and record R6's
argv path as untested-by-corpus with an owner.

**F8 [P1] R7's required EPIPE corpus program does not exist.** R7: "a corpus
program asserting the death (exit 141) differentially — a missing raise fails
loudly, never drifts." Grep over corpus/wave2 finds no SIGPIPE/EPIPE/exit-141
program (the only "141" hit is a byte in an AES test vector). The guarded
`Signal.raise("PIPE")` path in GoOs.java therefore has no differential
coverage, and the "loud fallback" half of R7 is untested. Fix: add the os
corpus program R7 calls for.

## E. Counts and evidence — findings

**F9 [P1] Unsafe corpus counts are inconsistent three ways** (the program's
bar is *honest counts*). CHECKPOINT.md: "11 corpus programs (8 differential +
3 trap pins + 2 scopelimited)" — arithmetic is 13, not 11. Actual files: 12
(00,01,02,04,05,06,08 = 7 differential; 03a/b/c = 3 trap pins; 07,09 = 2
scope-limited). The corpus README says "11 programs (9 differential, 2
scope-limited) + 3 trap pins" (= 14) but lists only 7 differential programs.
Three sources, three totals, none matching the tree. (Other packages'
counts verified accurate: io 16, os 14, syscall 11, reflect 12, crypto 12.)
Fix: reconcile to the tree (12: 7+3+2) in both documents.

**F10 [P1] Red-green/verification ledger: most worker claims are
coordinator-unverified.** What the coordinator independently verified:
patches `apply --check` clean (all; regenerated unsafe/01+02 itself), gofmt
nits fixed itself (syscall, io, unsafe, reflect), R11 supersede (own
go1.27.1 probe), R13 (did the work itself), reflect's 9 gc legs (ran them),
crypto's rewritten rand program gc leg (ran it). What remains
**worker-claimed only**: io's "2 seeded mutations caught" + "8 behavioral
tests via __-helpers green" (no evidence retained anywhere in the tree —
searched m8-tmp, analysis docs, CHECKPOINT); os's "GoOs compiles
-Xlint:deprecation -Werror, 32/32 unit tests pass" and "desugar table
machine-checked 30/30"; gc legs for io/os/syscall/unsafe/crypto (except the
rand rewrite). Fix: before sign-off, the coordinator re-runs the gc legs for
all packages itself and either reproduces the mutation/unit-test evidence or
downgrades the claims.

## F. Pipelining discipline and sign-off readiness

**Discipline check (new-files-only + staged patches):** `git status` shows a
single modified tracked file — `phases/phase3/CHECKPOINT.md`, the
coordinator's own log. All worker deliverables are new files; all
shared-file edits are staged patches (31 files, all standalone
`apply --check` clean at 720731d — verified). **F11 [P2]:** R9 as written
("NEW FILES ONLY in the tree") has no stated scope — the coordinator's own
in-place CHECKPOINT.md edit technically violates it. State the discipline's
scope explicitly (shared *code* files vs coordinator bookkeeping) so it is
enforceable as written.

**F12 [P0] Nothing about graalgo behavior is proven; sign-off cannot happen
now.** Zero graalgo legs have run for any wave-2 program — by design, since
all shared-file integration is staged for rebase. Every differential claim in
the corpus is therefore gc-only evidence today. Additionally, the staged
patches were pre-tested only against 720731d (pre-remediation); the M7
remediation (proceeding on a separate tree) may touch the same shared files
(export.go, synth_machinery.go, mrdesugar_m5.go, P3SyncMain.java), which would
invalidate the patches' context. The plan has no re-verification step at
rebase. Fix: at rebase, re-run `git apply --check` of all 31 patches against
the *remediated* tree before merging (regenerate where stale), then run the
full wave-2 corpus on both legs plus the full phase suites before any
sign-off.

**F13 [P2] Docs-currency gaps.** SYNTHESIS.md's syscall section still
describes the R1/R2-overruled design ("~30 TYPED Java intrinsics
(exporter-lowered wire nodes…)" behind "a JVM-level fd table") — a reader
meeting the doc first gets the wrong architecture. reflect-unsafe.md still
mandates the R11-superseded "const-context AST pre-pass." Per the program's
docs-current bar, update both or mark them superseded-by-RULING.md.

**F14 [P2] Unexplained numbering gaps:** staged patches skip unsafe/03 and
crypto/02 with no note (likely the dropped R11 pre-pass patch and a
renumbering, but undocumented). Also `.synthetic` is documented only in
run_wave.sh's header, not in the README's program-rules section (docs drift;
README rules 1–7 omit it while three syscall programs use it).

## What is PROVEN now vs provable only after rebase

Proven now: the five design rulings are sound (R1/R2/R12 verified above; R11
supersede probe-verified); all 31 staged patches are individually valid
against 720731d; the corpus programs are valid Go and genuinely adversarial
(4 spot-checked, none tautological); the `.divergent`/`.panic` and
`.scopelimited` harness invariants hold on all marked programs; per-package
counts are accurate except unsafe (F9).
Provable only after rebase: every graalgo leg of the corpus; merged-tree
compilation (Go + Java); the C1–C8 conflict resolutions composing correctly;
full phase suites green; argv (R6), SIGPIPE (R7), and errno-mapping behaviors
differentially; io mutation evidence and os unit-test claims (F10).

## Recommended actions (in order)

1. Update INTEGRATION-PLAN.md: correct C3 participants, add the P3SyncMain
   triple, dedup the ParseComments edit (F4), add reflect/crypto conflicts
   (F1), state the io/fs ordering constraint (F5). (P0/P1)
2. Implement `.args` in run_wave.sh; add the R7 EPIPE corpus program.
   (P1 — F7, F8)
3. Reconcile unsafe counts; reproduce-or-downgrade the unverified worker
   claims (F9, F10). (P1)
4. Define the rebase gate explicitly: re-`apply --check` all patches against
   the remediated tree, regenerate stale ones, then full corpus (both legs)
   + full suites before sign-off (F12). (P0)
5. Mark SYNTHESIS.md's syscall section and reflect-unsafe.md's pre-pass as
   superseded; document `.synthetic` in the README; explain the patch-number
   gaps; scope R9 explicitly (F11, F13, F14). (P2)
