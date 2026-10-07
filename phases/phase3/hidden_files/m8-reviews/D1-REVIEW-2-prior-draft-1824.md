# D1 Peer Review (methodology + integration) — Reviewer 2

**Scope:** P3-M8 D1 (WS-D multi-result interface dispatch), commits `3d46d9e`
+ `847459a` (branch `m8-pipeline`), against `phases/phase3/hidden_files/m8-d1/DESIGN.md`.
Read-only review; this document is the only write.

**Method:** independent adversarial verification, not re-reading. (1) Re-ran the
D1 pinning tests and full frontend main-package suite at HEAD. (2) Built a
pristine scratch copy of the pre-D1 baseline (`7e53780`, the parent of
`3d46d9e`) and re-ran the D1 test programs there to check the red-green claim
— then applied *only* the `pkgPathOf` import fix to isolate the claimed red
mechanism. (3) Ran the differential myself for corpus programs 12/13/14/16
(gc 1.27.1 vs graalgo, gmp=1 and gmp=4, byte-identical stdout + exit codes).
(4) Diffed every `__`-helper against gc 1.27.1's `src/io/io.go` from
`~/tools/go`. (5) Wrote two adversarial runtime probes (user-interface
structural dispatch; `WriteString` fallback branch) and ran them
differentially. (6) Audited the integration fixes in `3d46d9e`
(ReflectTypes.java, synth_reflect.go, export.go pkgPathOf) for soundness.

**Note on tree movement:** HEAD moved during this review (`0f6f07b`,
`7e4dd2f`, `2b22471`). `2b22471` fixes Reviewer 1's P1 (bare-statement
dispatch) exactly as R1 suggested, with a new pinning test — R1's P1 is
**resolved**, and R1's P2-6 (differential pending) is **closed** by §A below.
This review covers the D1 commits as tasked; all verification below ran at
HEAD `2b22471` unless stated otherwise.

**Relation to Reviewer 1:** R1 covered code correctness. I do not re-file
R1's findings; where we overlap (wiring-test type pins, D6 wording, ReadFrom
negative pin) I reference R1 and add only the new angle.

## Verdict: REQUEST_CHANGES

The D1 dispatch core is sound and genuinely proven (§A). But the D1 commit
ships two fail-fast→silent changes in `ReflectTypes.java` (P1-4, P1-5), the
red-green evidence is misdocumented (P1-1), the itab-wiring test cannot catch
a mis-wired table (P1-2), a corpus re-test trigger fired without being
honored (P1-3), and the commit message misdescribes its own contents (P1-6).
Sign off after the P1s are addressed; P2s are should-fix.

## A. What is PROVEN (independent verification)

1. **Differential, 8/8 byte-identical.** I ran programs 12/13/14/16 under
   stock gc 1.27.1 and graalgo (`P3SyncMain`, gmp=1 and gmp=4): stdout
   byte-identical, exit codes equal, all 8 legs pass. (Scratch runner:
   `~/workspace/tmp/d1_diff.sh`; probes/logs retained there.)
2. **Helpers are verbatim gc 1.27.1.** `__readAll`, `__readAtLeast`,
   `__readFull`, `__writeString`, `__copyBuffer` (minus D6 arms),
   `__copyN` (incl. `written==n`/EOF adjustment), `__copyBufferChecked`
   (empty-buffer panic placement) all diff clean against
   `~/tools/go/src/io/io.go`. The `__stringsReaderRead`/`__stringsReaderReadAt`
   manual byte loops are provably `copy()`-equivalent (`min(len(b),
   remaining)` bytes) and are heavily exercised by program 13 (20000 B).
3. **User-interface structural dispatch works at runtime.** The headline
   design claim ("user-declared interfaces with the same shape also
   dispatch") was previously pinned only at export shape. My probe
   (`type reader interface { Read(p []byte) (int, error) }` holding
   `*strings.Reader`, two sequential `Read` calls + `io.Reader` alongside)
   is byte-identical gc vs graalgo. Recommend promoting it to a corpus
   program (P2-2).
4. **The claimed red mechanism is real — once isolated.** See P1-1: on
   baseline+`pkgPathOf`-only, `n, err := r.Read(buf)` through `io.Reader`
   fails with exactly `gofrontend: io.Reader.Read is not in M8 scope
   (multi-result calls need WS D; re-test when it lands)`.
5. **D6's omission is load-bearing and correct.** Every `WriteTo`/`ReadFrom`
   body in the synthetic tree is fail-fast (`discard`, `multiReader`,
   `nopCloserWriterTo`, `strings.Reader`, `bytes.*`; `os.File`'s are
   named-trap panics). Keeping gc's assertion arms would route
   `io.Copy(io.Discard, …)` into `discard.ReadFrom`'s panic (`var _ ReaderFrom
   = discard{}`, synth_io.go:705 — the arm would *fire*, not lie dead).
   Omission is the only viable choice today. (Doc wording: see P2-5 / R1-P2-4.)
6. **No D1 regression on pre-existing gaps.** The `WriteString` fallback
   probe (user type embedding `io.Writer`) fails with `no method table
   installed for dynamic type "main.myWriter"` — but the single-result
   analogue (`myCloser`/`io.Closer`) fails *identically*, so this is a
   pre-existing user-type method-table gap, not D1-introduced. Likewise the
   io.Pipe/NewSectionReader failures pre-date D1 (baseline panics on
   synthetic import; see P1-1) and remain loud export-time failures.

## B. Findings

### P1-1. Red-green claim is mischaracterized (methodology)

`phases/phase1/frontend/iface_multiresult_test.go:4-6`:
```
// dispatch (D1). Red-green: these fail on the pre-D1 tree (every
// multi-result interface call is a scope error; the method tables carry
// no helper-mapped entries) and pass after.
```
DESIGN.md §6 makes the same claim ("a test failing before the fix
(stashed-tree check for the exporter tests…)").

**It is not true as written.** On the actual pre-D1 tree (`7e53780`) the
tests do not fail with scope errors — the exporter **panics** with a nil
pointer dereference in `describeType` (`t.Obj().Pkg().Path()`,
export.go:2108 pre-fix) during synthetic `io`/`strings` import, before any
multi-result logic runs. I verified: a scratch copy of `7e53780` panics on
*every* program that touches the synthetic `io` or `strings` packages
(`io.Reader` var, `strings.NewReader`, and the D1 interface program alike).
The "method tables carry no helper-mapped entries" half is vacuous — no
tables are emitted because export crashes first.

The scope-error red mechanism *does* exist, but only after the import-panic
fix that `3d46d9e` smuggled in as an "incidental" change: baseline +
`pkgPathOf`-only yields the clean `io.Reader.Read is not in M8 scope`
error. So the red-green pair is confounded — the same commit supplies both
the import fix and D1, and the documented red was never observed in the
documented form. If a stashed-tree check was actually run as DESIGN.md §6
claims, it would have shown panics, not scope errors.

**Fix:** restate the header honestly, e.g.: "Red: on the pre-D1 tree these
fail — the exporter panics on synthetic import at 7e53780 (pre-existing);
with only the import panic fixed, each multi-result interface call fails
with the WS-D scope error (verified on baseline+pkgPathOf-only). Green:
pass with D1." Better: split import fixes into their own commit so the
red-green pair is observable. Process rule: a red-green claim must describe
the red that was *actually observed*, with the tree state named.

### P1-2. `TestD1IfaceMethodTableHelperMapping` cannot catch a mis-wired itab

`phases/phase1/frontend/iface_multiresult_test.go:160-231`. The test scans
*every* named/pointer type entry for *any* method named `Read`/`Write`
pointing at the expected helper (`_ = cases`; `c.typeName` appears only in
error strings). It never checks that the mapping sits on the **right**
type's table. A wiring bug that attached `__stringsReaderRead` to
`*io.LimitedReader`'s `Read` entry, or dropped it from `*strings.Reader`
while present elsewhere, would still pass.

Tellingly, the stronger assertion already exists and is unused:
`d1MethodTable(t, doc, wantName)` (`iface_multiresult_test.go:124`) is
defined but never called — the test was evidently weakened from a
per-type lookup to a global scan.

**Fix:** use the helper as intended —
```go
for _, c := range cases {
    ms := d1MethodTable(t, doc, c.typeName)
    // find entry with name==c.method; assert funcIDs[obj]==c.helper,
    // and assert no OTHER type's table maps these methods to helpers
    // it shouldn't have (negative scan).
}
```
This is the test that guards the feature's central runtime invariant
(itab → correct concrete helper); as written it guards only existence.

### P1-3. Program 15's `.scopelimited` marker is stale; its re-test trigger fired

`phases/phase3/corpus/wave2/io/15_iface_callsite.scopelimited`:
`reason: 2-target call through an interface value (rd.Read where rd is
io.Reader) needs multi-result interface dispatch…`, `retest: WS D
multi-result interface dispatch; then re-run as a full differential
program`.

WS D multi-result interface dispatch **has landed** — the trigger fired —
but the program was kept scope-limited on different, unstated grounds
(PROGRESS.md: "pipe is separate work, not D1"). Worse, the stated reason
is no longer even the actual blocker: at HEAD the program fails export on
`w.Write` / `io.Pipe()` (`io.PipeWriter.Write is not in M8 scope…`),
*before* reaching the `rd.Read` the marker names. A marker whose reason
is false and whose trigger fired-but-was-ignored voids the
`.scopelimited` convention (cf. wave-2 review F7/F8 on harness invariants).

**Fix:** update the marker to the true blocker —
`reason: io.Pipe / concrete *PipeWriter.Write have no Appendix-M desugar
(concrete 2-result method dispatch, not interface dispatch)` with
`retest:` naming that work (concrete multi-result method desugar for pipe
types). Do not leave a fired trigger unhonored.

### P1-4. `ReflectTypes.java`: "invalid type" silently becomes a fake INTERFACE

`phases/phase3/src/main/java/graalgo/p3reflect/ReflectTypes.java:296-303`:
a basic type named `"invalid type"` (i.e. `types.Typ[types.Invalid]` —
a **failed type resolution upstream** in the exporter) is mapped to
`GoKind.INTERFACE` with fabricated size 16 / align 8 "so references don't
NPE". Previously this threw `FrontendException("reflect: unknown basic
type …")` — loud. Now a reflect program touching such a type gets
plausible-looking garbage (`Sizeof`/`Alignof`/kind queries all wrong)
instead of an error. This is three stacked silences: (1) the exporter
leaks `types.Invalid` without failing, (2) Java launders it into a real
kind, (3) size/align are invented. Directly violates the fail-fast
discipline, and it is untested — no test demonstrates the orphan case
before/after.

**Fix (pick one):** (a) revert to the loud throw and fix the upstream
leak — find where `types.Invalid` reaches `describeType` and fail there
with the path; or (b) if the orphans are genuinely benign bootstrap
artifacts, document *exactly which resolutions can fail and why*, and add
a regression test pinning the placeholder behavior. "Safe placeholder"
without evidence is not safe.

### P1-5. `infoOrShell`: forward refs bake incomplete shells into built types, untested

`phases/phase3/src/main/java/graalgo/p3reflect/ReflectTypes.java:482-491`.
Phase B builds `TypeInfo`s in wire-id order; a forward reference now
embeds `shells[id]` (kind/size only, `tok=-1`, **empty fields/methods**)
permanently into the parent — there is no fixup pass (`Table.byTok` is
write-once, `TypeInfo` is immutable). The comment claims "details like
fields are resolved lazily if needed" — nothing in the code does lazy
resolution; consumers reading `.fields` off a baked shell silently get
`[]`, and consumers going through `.tok` get `-1` → `IllegalArgumentException`
(the loud half, by accident). The old code NPE'd here, so this fixes a real
crash — but it trades a loud crash for potentially-silent incomplete type
info, with **zero tests**: no corpus program exercises a forward-referencing
type through reflect, and no test demonstrates the original NPE.

**Fix:** either add a phase-C fixup pass that replaces baked shells with
built infos once available, or add a regression test with a
forward-referencing type (e.g. a signature referencing a later-declared
named type) asserting the built `TypeInfo` is complete. At minimum, record
in the code *which* consumer positions are shell-safe (kind/size/elem)
and which are not (fields/methods), instead of the blanket "safe" claim.

### P1-6. Commit `3d46d9e`'s message misdescribes its own contents

`P3-M8: fix integration blockers (reflect/unsafe/runtime)` — but the diff
is 978 insertions containing the **entire D1 feature**
(`mrdesugar_m5.go` +353, `synth_io.go` +351, `DESIGN.md`, the
`exportIfaceMethodCall`/`methodTableEntries` D1 branches). A reader of
history cannot discover that WS-D interface dispatch landed here. Two
message bullets are factually wrong: "P3SyncMain: add missing
p3unsafe/p3reflect imports" — the diff touches **no** P3SyncMain file
(the imports were added by the coordinator during integration per
CHECKPOINT.md); "synth_reflect.go: remove type aliases (exporter has no
alias kind)" — the exporter **does** emit an alias kind
(`export.go:2346`, `case *types.Alias`), so the stated reason is false
(see P2-7).

**Fix:** history can't be cleanly rewritten mid-pipeline, so record the
true contents in `phases/phase3/CHECKPOINT.md` (one line: "3d46d9e also
lands the D1 feature; message describes only the incidental fixes") and
adopt the process rule: a commit whose message omits its largest hunk is
rejected at review. `847459a` has the same smell at smaller scale
("completion work" burying the load-bearing `synth_strings.go` copy()
fix — without it, *every* `Read` dispatch crashes the export; see §A.2).

### P2-1. Program 16 claims `WriteString` fallback coverage it does not have

`phases/phase3/corpus/wave2/io/16_writestring.go:18-20`:
```
// Fallback: strings.Reader is not a Writer; use a custom check via
// PipeWriter? No — use Discard for the fast path and verify the
// fallback by writing through the Writer interface directly.
```
The "fallback" case calls `w2.Write` directly — an interface `Write`
dispatch, not the `__writeString` fallback branch. **Every** `WriteString`
call in the corpus uses `io.Discard` (a `StringWriter`), so the `else
w.Write([]byte(s))` branch in `__writeString` (synth_io.go:279-287) has
**zero differential coverage**. I confirmed the branch is currently
unreachable in practice: the only way to reach it is a non-`StringWriter`
`Writer` behind an interface, and no synthetic type qualifies (`*os.File`
defines `WriteString`, synth_os.go:1054; user types can't get method
tables — pre-existing gap, §A.6).

**Fix:** correct the comment (it asserts a coverage property that is
false) and record the fallback branch as uncovered with a re-test trigger
(user-type method tables, or a synthetic non-`StringWriter` `Writer`).
Do not present the program as covering both arms.

### P2-2. No corpus runtime coverage of user-interface structural dispatch

`TestD1IfaceMultiResultUserInterface` (export shape only) is the sole pin
for the headline "user-declared interfaces dispatch too" claim. My probe
(§A.3) proves it works at runtime — promote the probe to a corpus program
(e.g. `17_user_iface_dispatch.go`): a user `reader` interface over
`strings.Reader`, sequential reads (stateful `r.i` advancement pins the
helper is really executing, not just the export shape).

### P2-3. Missing negative pin: 2-result name-match with type mismatch

`TestD1IfaceMultiResultStillFailFast` (`iface_multiresult_test.go:307`)
uses a 3-result method. R1's P2-2 asks for the 2-result non-table case
(`ReadFrom`). The remaining unpinned negative is a 2-result method whose
name *is* tabled but whose result types *aren't* (`Read(p []byte)
(int, string)`) — the case that exercises the `types.Identical`
positional guard rather than the arity guard. **Fix:** add it; expect the
WS-D scope error.

### P2-4. `pkgPathOf` trades a loud panic for a silent `""`

`phases/phase1/frontend/export.go:2072-2080`. Nil-safe, and it un-breaks
synthetic import — but any synthetic named type that *should* have a
package now exports `"pkgpath": ""` instead of failing. For
`reflect.Type.PkgPath` differentials that's a silent wrong value, not a
loud error. **Fix:** document precisely when nil `Pkg` is legitimate
(synthetic bootstrap — which phase, which types), and add a test asserting
no *user-visible* named type ever takes the `""` path (e.g. assert in
`describeType`: if the type is reachable from a user program, `Pkg()`
must be non-nil).

### P2-5. D6 wording (extends R1 P2-4)

R1's suggested rewording is right; one addition from this review: the
design text "both assertion arms are dead" is not just imprecise but
backwards — `var _ ReaderFrom = discard{}` (synth_io.go:705) means the
`dst.(ReaderFrom)` arm **would fire** on the corpus's own `io.Discard`
destination and route into `discard.ReadFrom`'s panic. The arms aren't
dead; they're deliberately omitted because firing them panics. **Fix:**
adopt R1's rewording, and state the `discard`-would-fire fact so a future
"restore the arms" (the D6 re-test trigger) doesn't reintroduce the panic
without real bodies.

### P2-6. "18 pre-existing io-corpus failures" is stale

PROGRESS.md claims 18 pre-existing failures (`'call to unknown function
"New"'`, pipe/NewSectionReader). My export inventory at HEAD shows only 3
export failures in `corpus/wave2/io` (07: variadic spread, pre-existing;
11/15: `io.PipeWriter.Write` not in M8 scope). The `errors.New` failure
mode is gone (fixed by the concurrent io-integration work), and 18 >
17 programs, so the number can't be a per-program count as stated.
**Fix:** re-baseline the claim (count legs, name the current failure
modes) or drop the number; a stale count makes future "did D1 regress
pipe?" comparisons impossible.

### P2-7. `synth_reflect.go` alias-removal comment is factually wrong

`phases/phase1/frontend/synth_reflect.go:2295-2296`: `// (aliases removed:
exporter does not support alias kind)`. The exporter *does* support it
(`export.go:2346`). The removal itself is safe (no `reflectlite.Type` /
`reflectlite.Value` references anywhere else in the tree — verified), but
the comment sends the next reader down a false trail, and the *true*
reason (whatever broke with the aliases present — presumably the
ReflectTypes.java crash that P1-4 papers over) is undocumented.
**Fix:** replace with the true reason + a re-test trigger.

### P2-8. `basicKind` shipped without `"rune"` in `3d46d9e`

`3d46d9e` added `basicKind`'s untyped arms but not plain `"rune"`; any
reflect program touching `rune` threw `FrontendException` until `847459a`
added `case "int32", "rune"`. A one-line follow-up, but it means the
"integration blockers" commit was merged with a new crash in it — the
review that should have caught it is the one that didn't happen because
the change was bundled. Process point; no action beyond noting.

### P2-9. Concurrent worker on `ReflectTypes.java`

PROGRESS.md notes another worker is concurrently editing
`synth_reflect.go` / `ReflectTypes.java` ("do not touch"). `3d46d9e`
changed 125 lines of that same file. Overlapping uncoordinated edits to
the reflect runtime are a merge-conflict and semantic-interference risk
(e.g. their alias work vs P1-4/P1-5 here). **Fix:** coordinator to
serialize or explicitly partition the file; at minimum the two workers'
deltas need a joint review before the next integration.

## C. Coverage gaps (multi-result interface scenarios)

1. **Direct dispatch positions:** corpus programs 12/13/14 exercise D1 only
   *indirectly* (inside `__copyBuffer`/`__readAll`/`__readAtLeast`); direct
   `n, err := r.Read(buf)` appears only 3×, all in program 16. Bare
   `r.Read(buf);` now dispatches (fixed at HEAD, `2b22471`, pinned by
   `TestD1BareIfaceCall`) but has no *differential* coverage; `go`/`defer`
   positions likewise untested at runtime. `ValueSpec` initializers and
   `return r.Read(buf)` still fail fast loudly (R1 P2-5) — named limitation,
   needs the documented re-test trigger.
2. **User-interface dispatch at runtime:** only my probe (§A.3); promote to
   corpus (P2-2).
3. **`__writeString` fallback branch:** zero coverage; currently unreachable
   in practice (P2-1).
4. **`copyBuffer` error normalization:** `ErrShortWrite` / `errInvalidWrite`
   arms have zero differential coverage (no failing/short writer in corpus).
5. **Non-EOF read errors** through `ReadAll`/`Copy` (error propagation +
   partial data): uncovered.
6. **Interface embedding** (`interface { io.Reader; io.Writer }`): the
   structural matcher should handle it, but no test or corpus program tries.
7. **3+ results:** correctly pinned fail-fast (`TestD1IfaceMultiResultStillFailFast`).
   **Named result params:** n/a — v1 cannot declare multi-result methods at all.
8. **Nil interface dispatch:** shared `ifacemethod` node machinery with
   single-result; presumably loud, not D1-specific — no D1 test, acceptable.
9. **Multiple dynamic types behind one interface in a single program:**
   uncovered (each corpus program uses one Read impl + `io.Discard`).

## D. What checked out (no findings)

- Pair-shape guards are loud at every link (independently re-verified the
  `types.Identical` positional checks in `classifyIfaceMultiResultCall` and
  `multiResultHelperFunc`; a reordered/renamed/retyped pair fails export).
- Non-table methods (`ReadFrom`, 3-result, concrete no-helper types) keep the
  fail-fast; single-result dispatch untouched.
- `methodTableEntries` helper fallback: `obj` = helper funcID, `recv` from
  the method's declared receiver, `implkey` from the method — matches DESIGN.md §3.
- `__copyBufferChecked` empty-buffer panic placement matches gc.
- Full frontend main-package suite green at HEAD, incl. all 6 D1 tests.
- `TestD1IfaceMultiResultCallNode` and `TestD1IfaceMultiResultUserInterface`
  are genuinely non-tautological (verified red mechanism in §A.4; node-shape
  assertions are specific).
- `captureDescriptors` tolerance: the "fail fast when actually used" claim
  holds — `valueDescriptor()` etc. throw `IllegalStateException` on null.

## E. Recommended actions (in order)

1. P1-4, P1-5: revert-or-justify the two `ReflectTypes.java` silences with
   regression tests (or a phase-C fixup pass for `infoOrShell`). These are
   the only findings that change runtime failure modes from loud to silent.
2. P1-2: strengthen `TestD1IfaceMethodTableHelperMapping` with the
   (currently unused) `d1MethodTable` per-type assertion + negative scan.
3. P1-1: correct the red-green documentation (test header + DESIGN.md §6)
   to the observed reality; split import fixes from feature commits going
   forward.
4. P1-3: update program 15's `.scopelimited` marker to the true blocker and
   a live re-test trigger.
5. P1-6: record `3d46d9e`'s true contents in CHECKPOINT.md; enforce
   message-diff agreement at review.
6. P2s in file order; promote the user-interface probe to a corpus program.

*Reviewer 2 (methodology + integration), 2026-10-07. Independent evidence:
own differential run (8/8), own baseline red-green isolation, own runtime
probes, own gc-source diffs. Did not modify the tree.*
