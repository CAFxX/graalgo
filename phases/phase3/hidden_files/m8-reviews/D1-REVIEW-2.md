# D1 Methodology Review — Reviewer 2 (independent)

**Scope:** P3-M8 D1 (WS-D multi-result interface dispatch): DESIGN.md,
implementation commits `3d46d9e` + `847459a` + **`2b22471`** (the REVIEW-1
P1 fix), `phases/phase1/frontend/iface_multiresult_test.go`,
`phases/phase3/hidden_files/m8-d1/PROGRESS.md`. Read-only review; this
document is the only write.

**Tree state pinned:** all verification below ran against the *committed*
D1 scope (`3d46d9e`, `847459a`, `2b22471`) via scratch worktrees, except
where "working tree" or a later commit is stated explicitly. The tree
moved during this review (`50254ea`, `b79e393`, `0d0317b`, **`1ff7567`**
"fix REVIEW-1 P2-2..P2-5", plus uncommitted remediation hunks) — the
findings below are stated against the tasked scope; §F records what has
since landed and what must still be re-verified.

**Relation to the earlier methodology draft:** an 18:24 UTC draft of
`D1-REVIEW-2.md` (preserved as `D1-REVIEW-2-prior-draft-1824.md`) covered
`3d46d9e`+`847459a` thoroughly. This review is independent and tasked
specifically with the five methodology questions below, **including the
`2b22471` P1 fix, which the earlier draft did not examine**. Where I
independently reproduced a prior finding I say so; the new findings
(P1-1, P1-2, P2-1, P2-4) are mine.

**Method:** (1) read all three commits' diffs; (2) built scratch
worktrees at `7e53780` (pre-D1), `7e4dd2f` (pre-P1-fix), `2b22471`
(post-fix) and ran the D1 tests plus purpose-built probes there —
red/green observed, not asserted; (3) traced the `ValueSpec` and
`go`/`defer` positions through exporter and Java builder code;
(4) audited differential-corpus coverage per call position from corpus
sources; (5) attempted silent-divergence construction.

## Verdict: REQUEST_CHANGES

The D1 core remains sound, but the `2b22471` P1 fix **regressed a
neighboring position** (`var n, err = r.Read(buf)` went from a clean
scope error to an uncontrolled builder crash — P1-1; since fixed at
HEAD `1ff7567`, but the methodology hole that produced it is unpatched),
the fix's stated blast radius (`go`/`defer`) was never actually in its
blast radius (P2-1), the position the fix *did* change has **zero runtime
verification** (P1-2), and the red-green documentation is still
mischaracterized (P2-4). Sign off after §F's re-verification on the
final tree; the ValueSpec fix itself has landed.

## A. What checked out (independent verification)

1. **All 6 pinning tests are genuinely red-green capable** — verified by
   execution, not by reading the header comment:
   - `TestD1BareIfaceCall` on `7e4dd2f` (pre-fix): FAILS with exactly
     `gofrontend: io.Reader.Read is not in M8 scope (multi-result calls
     need WS D; re-test when it lands)`. The test is valid; the worker
     simply never recorded the red run (P2-3).
   - `TestD1IfaceMethodTableHelperMapping` on `7e53780` (pre-D1): FAILS
     on the assertion (no helper-mapped entries) — clean red.
   - `TestD1IfaceMultiResultCallNode`, `TestD1IfaceMultiResultUserInterface`,
     `TestD1IfaceMultiResultStillFailFast` on `7e53780`: FAIL via an
     exporter nil-pointer panic in `describeType`, *not* via the
     documented scope error. Red, but the documented red mechanism is
     wrong for these three (P2-4; independently confirms prior-draft
     P1-1).
   - `TestD1IoDesugarWiring` on pre-D1: fails to compile
     (`ioMultiResultFuncs` undefined) — degenerate red.
2. **No silent divergence constructible.** Attempts:
   - *Nil interface* (`var r io.Reader; n, err := r.Read(buf)`):
     `IfaceDispatchNode.requireReceiver` panics loudly
     (`GoErrors.nilDereference()`) — loud, not silent.
   - *Field-index extraction mismatch*: statically guarded at export
     (`classifyIfaceMultiResultCall` + `multiResultHelperFunc` positional
     `types.Identical` checks); a mismatch fails export loudly.
   - *`go`/`defer`*: fail loudly on the pre-existing
     "method value via interface (P3-M3 scope)" error — see P2-1.
   - *`ValueSpec`*: not silent either — an uncontrolled
     `IndexOutOfBoundsException` in `NodeFactory.buildVarDecl`
     (`values.get(1)` on a 1-element list), i.e. a loudness *regression*,
     not a silent path (P1-1).
   The "no silent divergence" claim holds for the committed scope, with
   the caveat in P1-2.
3. **The 8/8 byte-identical differential is real.** The prior draft's
   scratch differential (programs 12/13/14/16 × gmp=1,4, gc 1.27.1 vs
   graalgo) has retained artifacts (`~/workspace/tmp/d1diff/`); the
   runner (`~/workspace/tmp/d1_diff.sh`) is sound (byte compare +
   exit-code compare). This closes REVIEW-1 P2-6 for the committed
   scope — but see P1-2 for what it does *not* cover.
4. **Helper algorithms match gc 1.27.1** (prior draft §A.2 diffed them;
   I re-confirmed the `__copyBufferChecked` empty-buffer panic placement
   and `__copyN`'s `written==n`/EOF adjustment by reading the sources).
   No independent finding.

## B. Findings

### P1-1. The `2b22471` P1 fix regressed `ValueSpec`: clean scope error → uncontrolled builder crash

**Evidence (pre/post probe, same program):**

```go
var r io.Reader = strings.NewReader("hello")
buf := make([]byte, 8)
var n, err = r.Read(buf)   // ValueSpec: legal Go
```

- On `7e4dd2f` (pre-fix): export fails **cleanly** —
  `gofrontend: io.Reader.Read is not in M8 scope (multi-result calls
  need WS D; re-test when it lands)` (verified by execution).
- On `2b22471` (post-fix): export **succeeds** and emits
  `vardecl{names:[n err], values:[<ifacemethod pair node>]}` (verified by
  wire dump). No desugar runs for `ValueSpec`.

Downstream, `NodeFactory.buildVarDecl` (`phases/phase1/src/main/java/
graalgo/frontend/NodeFactory.java:3405`) permits the 2-name/1-value
shape only for the comma-ok case; the fall-through loop does
`values.get(i)` for `i` in `0..objs.size()`, so `values.get(1)` throws
**unchecked `IndexOutOfBoundsException`** (`Json.Array.get` →
`items.get(i)`, no bounds check, not a `FrontendException`). A user
program in this position now crashes the launcher with a Java stack
trace instead of getting the named M8 scope error.

**Methodology hole:** the fix's blast radius was never analyzed. Moving
the interface branch above `isMultiResultSynthetic` in `exportCall`
changes *every* `exportCall` position, not just bare `ExprStmt` — but
only the bare-statement position got a pinning test. REVIEW-1's P2-5
said ValueSpec "fail[s] fast loudly"; that was true pre-fix and the fix
invalidated it without anyone re-checking. The single-position pin
(`TestD1BareIfaceCall`) is exactly the narrow-verification failure mode
the quality contract warns against.

**Status (update 19:05 UTC):** this regression is now FIXED at HEAD
(`1ff7567` "fix REVIEW-1 P2-2..P2-5", which also landed the
`exportVarGenDecl` ValueSpec check swept in via `50254ea`, pinned by
`TestD1VarDeclMultiResultFailFast`). The commit message describes
counterfactual checks for the neighboring test changes. The methodology
finding stands — the fix shipped the regression and it was caught by
adversarial review, not by the worker's own blast-radius analysis — and
§F.1 still requires the red-green runs to be recorded on the final
tree.

### P1-2. The P1-fixed position has zero runtime verification

`2b22471`'s entire behavioral change is: bare `r.Read(buf)` as a
statement now dispatches instead of scope-erroring. Verification of that
change is exactly one exporter-shape unit test
(`TestD1BareIfaceCall` — counts `ifacemethod` nodes in `main`; does not
assert statement position, execution, or the discarded-pair semantics).

The differential corpus contains **no** bare-statement multi-result
interface call at runtime (verified by grep over all 17 io corpus
programs; the only bare `.Write(`/`.Read(` statements are concrete
`*PipeWriter` calls in pipe programs). The 8/8 byte-identical evidence
therefore does not cover the one position the P1 fix changed. Neither
do `if`-init, plain `=` (the desugar's `define=false` path), or
user-interface structural dispatch at runtime (prior-draft P2-2 stands).

**Fix:** add a differential corpus program exercising the newly-working
positions — bare `r.Read(buf);` (stateful: assert the reader advanced),
`if n, err := r.Read(buf); err != nil`, and plain `=` reassignment —
gc vs graalgo byte-identical. Exporter pins are necessary but not
sufficient for a dispatch-semantics change.

### P2-1. REVIEW-1's P1 (and the fix) misdescribed the `go`/`defer` positions

REVIEW-1 P1 claimed bare `r.Read(buf)` "(*and* `go`/`defer` positions)
fail fast with the M8 scope error" and recommended pinning "go/defer
variants". Probe on `2b22471`:

```go
go r.Read(buf)
defer r.Read(buf)
```
→ `gofrontend: method value Read is not supported: method value via
interface (P3-M3 scope)` — a **different, pre-existing** limitation
(`exportGoStmt`/`exportDeferStmt` treat `r.Read` as a method value).
The P1 fix did not touch these positions: they failed before on the
method-value error and fail identically after. The "go/defer" half of
the P1 was never real, and the recommended go/defer pinning test would
have pinned a behavior the fix didn't change.

**Fix:** correct the record (this review does so); do not add go/defer
pins for D1 — they belong to the method-value work, with its own
re-test trigger. DESIGN.md §5 should name the method-value limitation
for go/defer rather than leaving the positions undiscussed.

### P2-2. `return r.Read(buf)` is unreachable in v1 — REVIEW-1 P2-5's example is moot

REVIEW-1 P2-5 listed "`return r.Read(buf)`" alongside ValueSpec as
failing fast. A 2-result call can only type-check as a return value
inside a function with 2 results, and v1 rejects *every* 2-result
`funcdecl` at `exportFuncDecl` ("at most one result is supported").
Probe confirms: the failure is on the *function declaration*, before
any call position is reached. There is no return-position behavior to
pin or document beyond what exists. (The working-tree DESIGN.md §5
already states this correctly.)

### P2-3. `TestD1BareIfaceCall` was never red-green verified by the worker

The test is good — I verified it fails on `7e4dd2f` with exactly the
M8 scope error the fix removes (§A.1). But no red run was recorded:
the commit message ("Add TestD1BareIfaceCall pinning test") claims
nothing, PROGRESS.md still says "5 tests", and CHECKPOINT.md records
only the commit. Under the project's testing doctrine ("tests must be
demonstrably capable of failing"), an unrecorded red run is an
unverified test. **Fix:** record the red evidence (this review §A.1
serves); process rule: a fix commit that adds a pinning test must cite
the red observation (tree state + failure text) in the message.

### P2-4. Red-green documentation still mischaracterizes the observed red

`iface_multiresult_test.go` header (still, at HEAD): *"Red-green: these
fail on the pre-D1 tree (every multi-result interface call is a scope
error…)"*. My baseline runs show: tests 3/4/5 fail via an exporter
**panic** (`describeType` nil `Pkg()`), test 2 via assertion failure,
test 1 via compile failure, test 6 (on its baseline) via scope error.
Only 1 of 6 matches the documented mechanism. PROGRESS.md's
"Baseline: same programs panic (red)" is closer to the truth but still
imprecise. (Independently confirms prior-draft P1-1.)

**Fix:** restate the header to the observed reality, e.g.:
*"Red (pre-D1 `7e53780`): tests 2/3/4/5 fail — the exporter panics on
synthetic import before reaching multi-result logic (pre-existing);
test 1 fails to compile (table undefined). With only the import panic
fixed, each multi-result interface call fails with the WS-D scope
error. Green: pass with D1."* Name the tree state; never describe a
red that wasn't observed.

## C. Differential-corpus coverage vs. D1 call positions (task Q2)

| Position | Claimed (DESIGN.md §5) | Differential coverage |
|---|---|---|
| 2-target `:=` | desugars | YES — corpus 12/13/14 (via helpers) + 16 (2 direct) |
| 2-target `=` | desugars | NO — no corpus program uses plain `=` |
| `if`-init (`if n, err := …;`) | routes via `exportOptStmt` | NO |
| Bare `r.Read(buf);` | works (post-P1-fix) | NO — exporter pin only (P1-2) |
| `go`/`defer` | — (method-value limitation) | N/A — loud pre-existing scope error |
| `ValueSpec` (`var n, err = …`) | loud limitation | N/A — was broken at `2b22471` (P1-1); fixed at HEAD `1ff7567` |
| `return` | unreachable in v1 | N/A (P2-2) |
| User-interface structural dispatch | dispatches | exporter pin only; prior draft's runtime probe not yet a corpus program (their P2-2) |

Positions the design claims that have no differential coverage: bare
`ExprStmt` (the P1 fix's own position), plain `=`, `if`-init.

## D. Task Q4: DESIGN.md §6 vs. differential evidence

DESIGN.md §6 is a verification *plan*, not a claim; the claims lived in
PROGRESS.md ("End-to-end verified" smoke test) and CHECKPOINT.md ("8
differential runs byte-identical" at handoff). Timeline:

1. Handoff claim (8 runs) was on a tree predating the io-integration
   fixes (`0f6f07b`); a subsequent fuller run found **12 failures**
   (duplicate-global-id exporter bug) — proving the 8-run claim never
   established corpus-wide byte-identicality.
2. The prior methodology draft independently re-ran 8/8 byte-identical
   at `2b22471` with retained artifacts — this is the credible evidence,
   and it postdates the integration fixes.
3. Program 15's `.scopelimited` re-test trigger (WS D) fired and was
   ignored on unstated grounds (prior-draft P1-3, confirmed) — the §6
   plan item "un-scope-limited 12/13/14/15" was not honored for 15.

So: the byte-identical check **was** eventually completed and
evidenced — by the prior reviewer, not by the worker — for the four
programs × two gmp settings. It does not cover the P1-fixed position
(P1-2), and the corpus still carries a fired-but-ignored re-test
trigger.

## E. Task Q5: methodology gaps in the P1 fix — summary

1. **Unanalyzed blast radius** → P1-1 (ValueSpec regression).
2. **Misdescribed blast radius** → P2-1 (go/defer never affected).
3. **Unrecorded red-green** → P2-3 (test valid, verification skipped).
4. **No runtime verification of the changed position** → P1-2.
5. REVIEW-1's P2s: P2-2 (ReadFrom case), P2-3 (field-type pins), P2-4
   (D6 wording), P2-5 (ValueSpec docs + loudness fix) are now addressed
   at HEAD `1ff7567`.

## F. What the coordinator must re-verify on the final tree

HEAD is now `1ff7567` (REVIEW-1 P2-2..P2-5 landed: ReadFrom negative
subtest, field-type pins, D6 rewording, ValueSpec loudness fix +
`TestD1VarDeclMultiResultFailFast`, DESIGN.md §5). Uncommitted
remediation continues (`mrdesugar_m5.go` bare-concrete desugar,
`ReflectTypes.java`, `run_wave.sh`). Before sign-off:

1. Record the red-green runs for `TestD1VarDeclMultiResultFailFast`
   (red on `2b22471` per my probe §B.P1-1; green at HEAD) — do not
   assume.
2. Re-run the full frontend suite and the 8-leg D1 differential on the
   final tree (the `types.Invalid` guard in `typeID` touches the D1
   wire path; `1ff7567` changed the exporter).
3. Add the P1-2 corpus program (bare/if-init/`=` positions).
4. Fix the test-header red-green documentation (P2-4).
5. Update program 15's `.scopelimited` marker to its true blocker
   (prior-draft P1-3).

*Independent methodology review, 2026-10-07. Evidence: scratch
worktrees at `7e53780`/`7e4dd2f`/`2b22471` under
`~/workspace/tmp/d1-redcheck/` (removed after review), wire dumps and
probe outputs quoted above. No tree modifications made.*
