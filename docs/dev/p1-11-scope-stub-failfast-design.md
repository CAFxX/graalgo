# P1-11 Design: compile-time fail-fast for single/zero-result scope-gap stubs

**Lane:** RV22-P1-11 (remediation for review 2026-10-18 22:00 CEST, §P1-11).
**Status:** design + mechanism + representative conversion (`sort` package).
**Date:** 2026-10-19.

## 1. Problem

316 `panic("…not in M{5,7,8,9} scope…")` bodies live in shipped synthetic
stdlib sources (`internal/frontend/synth_*.go`). A user program calling
`os.Chown` compiles cleanly and dies at *runtime*. The codebase already
owns the better mechanism — compile-time `scopeErrorf` via
`isMultiResultSyntheticFailFast` (synth_machinery.go) — but it covers only
multi-result members absent from the desugar tables. Single/zero-result
unimplemented functions got runtime panics instead. A user-reachable
panic is a bodge (review verdict).

## 2. Census (mechanical, not by hand)

Grep census: 316 hits for `panic("…not in M[5789] scope…")` in 29
`synth_*.go` files. AST census of the *embedded* synthetic sources
(single-statement-body shape check): 306 confirmed.

Categorization:

- **(a) Faithful mirrors of upstream gc behavior — PRESERVED UNTOUCHED.**
  These are *not* in the 316: they are panics *inside* real function
  bodies (multi-statement), e.g. base32 alphabet validation
  ("encoding alphabet is not 32-bytes long"), zip "SetOffset called
  after data was written", `errors.As`'s "target cannot be nil". The
  single-statement shape check excludes them by construction.

- **(b1) Multi-result scope stubs: 228** (2 results: 211; 3: 16; 4: 1).
  Already have a design: Appendix-M desugar tables + the
  `isMultiResultSyntheticFailFast` compile-time gate. **Not touched by
  this change** — clean separation of concerns. (Their 8 currently-red
  tests are a *separate* regression from the real-x509 lane's `adc14e0f`,
  triaged in §8 below — different root cause, different owner.)

- **(b2) Single/zero-result scope stubs: 78** (0 results: 4; 1: 74).
  **This lane.** No desugar table, no fail-fast — the panic is reachable
  from ordinary user code today. By milestone: M5: 29, M7: 158→(b2
  subset), M8: 108→(subset), M9: 11. By package (b2): `os` (Chown,
  Lchown, File.Chown, Expand, CopyFS, Root.*, Process.*…), `time`
  (Time.AddDate/Day/GoString/Hour/In/Location/Minute/Month/Nanosecond/
  Second/Weekday/Year/YearDay), `json` (Compact, HTMLEscape, Indent,
  Unmarshal, Decoder.Decode, Encoder.Encode, …), `xml` (5), `sort`
  (Slice, SliceStable, SliceIsSorted), `fs` (WalkDir + 12 `__`
  helpers), `strings` (Title), `reflect` (4, retired source),
  `singleflight` (2), `base32` (encoder.Close).

- **Other single-panic families (not P1-11):** 725 single-statement
  `panic(...)` bodies with *different* messages — multi-result desugar
  redirects ("use __-helper (WS D)"), "boringcrypto: not available"
  (43, `crypto/internal/boring`), "multi-result desugar" notes. Separate
  mechanisms/follow-ups; the shape check does not claim them.

## 3. Options considered

**A. Hand-maintained capability manifest** (`map[package]map[func]…`,
checked at import). Rejected: duplicates information already present in
the stub body (drift when generators regenerate); name-keyed, which the
repo's own generality rule forbids.

**B. Structural classification (chosen).** The stub body *is* the
manifest. At synthetic-package build time, scan the parsed AST: a
FuncDecl whose body is exactly one statement,
`panic("<string literal>")`, with the literal matching
`not in M[5789] scope` and ≤1 result, is a scope-gap stub. Record
`*types.Func → message`. Properties:

- Derived from source truth — generators maintain it; no second list.
- Structural, never name-keyed (precedent: `classifyTrivialForwarder`
  in mrdesugar_m5.go, which classifies synthetic stub bodies by AST
  shape).
- The message already carries the milestone and the re-test trigger
  (e.g. `sort.Slice: not in M7 scope (needs reflect; re-test when
  reflect lands)`) — reused verbatim as the compile-time error, so the
  error is greppable and self-documenting.
- `*types.Func` identity is reliable: `overlayImporter` returns the
  cached `getSyntheticPackage(path).pkg`, so user type-check and
  synthetic type-check share identical objects.

**C. Delete the stubs / stop emitting them.** Rejected: the stubs keep
exact gc signatures for type-checking — that is why they exist
("Original function with panic body (for type-checking)" in the
generators). Removing them breaks type-checking of user programs.

## 4. Mechanism

1. `syntheticPackage.scopeStubs map[*types.Func]string` — built in
   `buildSyntheticPackage` right after type-check (via
   `sp.info.Defs[fd.Name]`; covers methods too). Nil when the package
   has no single/zero-result stubs.

2. `(*exporter).scopeGapStub(fn *types.Func) (string, bool)` —
   resolves `fn.Pkg().Path()` through `syntheticSources`, returns the
   recorded message. Returns false while `e.inSyntheticEmission` is set
   (see §6). Multi-result functions are never recorded (results > 1
   excluded at build time), so the Appendix-M machinery stays
   authoritative for them.

3. Wiring (representative: package-level calls + value positions):
   - `exportSyntheticPkgCall`: after the `isMultiResultSyntheticFailFast`
     check — `if msg, ok := e.scopeGapStub(fn); ok { return nil, true,
     scopeErrorf("%s", msg) }`. Covers direct calls in every statement
     position (`go`/`defer` lower through the same CallExpr path).
   - `exportSyntheticQualified`, `case *types.Func`: before the
     `funcByObj` (M6 func-value) branch — a stub has no honest value
     form, mirroring the multi-result "fail-fast stub … cannot be used
     in value position" precedent.

4. Error shape: `gofrontend: sort.Slice: not in M7 scope (needs
   reflect; re-test when reflect lands)` — the stub's own words.

## 5. Representative conversion: `sort`

`sort.Slice` (0 results), `sort.SliceStable` (0), `sort.SliceIsSorted`
(1) — package-level funcs, no intrinsics, no transitive internal
callers (verified by AST: no real synthetic body calls them).
`sort.Find` (multi-result stub) stays on the existing machinery —
the classifier must *not* claim it (non-interference, pinned by test).

Tests (`internal/frontend/synth_scopestub_test.go`):

- Direct calls `sort.Slice(...)`, `sort.SliceStable(...)`,
  `sort.SliceIsSorted(...)` fail with "M7 scope" errors naming the
  function.
- Value position `f := sort.Slice` fails at compile time.
- Unit: `scopeGapStub` claims `Slice`/`SliceIsSorted`, does not claim
  `Find` (multi-result) or `SearchInts` (real body).
- Faithful-mirror preservation: `base32.NewEncoding` (real body whose
  panics validate the alphabet, gc-faithful) is *not* claimed;
  `base32`'s single-result stub `encoder.Close` *is* claimed. A program
  using only real `sort`/`base32` functions still exports cleanly.

## 6. Transitive internal references — exemption, then follow-up

AST check found real synthetic bodies calling single-result stubs:

- `os.ExpandEnv` (real) → `os.Expand` (stub)
- `json.__decoderToken` (real helper) → `jsonReadAll` (stub)
- `fs.__fsGlobDir` / `fs.__fsWalkDir` (real) → `__fsReadDir` (stub)

Firing the fail-fast during synthetic body emission would make
*importing* `os`/`encoding/json`/`io/fs` a compile error — wrong scope
for this lane. Therefore the check applies to **user-code references
only**: `e.inSyntheticEmission` is set during `exportSyntheticBodies`
and the check is exempt while set. (User call sites are all exported
before synthetic bodies; the flag is unambiguous.)

The transitive closure — functions unimplementable *because* they call
a stub (e.g. `os.ExpandEnv` panics at runtime today via `Expand`) — is
the honest end state for those paths, but it is a per-package audit
that can cascade, so it is **follow-up work**, not this lane. The
three cases above are the starting list.

## 7. Full-migration plan (follow-up lane)

1. Method-call paths: wire `scopeGapStub` into `exportMethodCall` /
   `exportFuncCall` / `resolveMethodCall` (one choke point preferred
   over the 7 current `isMultiResultSyntheticFailFast` sites) for
   method stubs (`os.File.Chown`, `json.Decoder.Decode`, …).
2. The remaining ~75 single/zero-result stubs (`os`, `time`, `json`,
   `xml`, `fs`, `strings`, `singleflight`, `base32`).
3. Transitive-closure audit (§6): mark transitively-unimplementable
   functions, package by package.
4. `boringcrypto: not available` family (43): adjudicate (separate
   message pattern — extend the shape check or keep as-is with
   rationale).
5. Re-test triggers: each stub message already names its trigger
   ("re-test when reflect lands", …); the migration lane re-runs the
   per-package export tests as stubs are implemented.
6. No generator changes needed — the mechanism derives from the
   generated bodies.

## 8. Triage of the 8 red tests (sibling lane RV22-P1-10's find)

All 8 are the *existing multi-result* fail-fast family — **different
root cause** from P1-11 (missing mechanism vs regressed mechanism).
Not fixed here; documented for their owners:

- `TestSyntheticBytesM7`, `TestSyntheticErrorsSortM7` (wire doc
  contains multi-result funcdecls): `adc14e0f` (real-x509, 2026-10-15)
  "Register fail-fast stubs in funcByObj/synthFuncIDs (so call sites
  link to them)" — stubs are now emitted as funcdecls instead of
  skipped at import, and `isMultiResultSyntheticFailFast` returns false
  for registered functions.
- `TestSyntheticBytesCutFailsFast`, `TestSyntheticSortFindFailsFast`,
  `TestEnccoreFailFast` (generic WS D error instead of named M7):
  same `adc14e0f` root cause — the named-error check now returns false,
  call sites fall through to "multi-valued call result is not
  supported in frontend/v1 scope (WS D)".
- `TestMRFuncValueSyntheticAccept` (no global `AcceptFunc`):
  `cc1d636e` (real-x509) introduced `etaExpandMRFuncValues`, which
  *removes* package-level `var x = pkg.F` ValueSpecs for multi-result
  F and aliases call sites — a competing design for the same construct
  the test pins (PKG-035: value lowers to the `__accept` helper ident).
  Needs lane-vs-lane adjudication.
- `TestMRFuncValueFailFastKept` (`time.ParseDuration` value position,
  got nil): `adc14e0f` registered the stub in `funcByObj`, so
  `exportSyntheticQualified`'s M6 func-value branch wins before the
  fail-fast checks.
- `TestDeadlineNotCallableM5` (expected type error, got success):
  `046afc86` *deliberately* restored `Deadline()` to the synthetic
  Context interface (general interface-MR support) — the test's
  expectation is stale after an intentional behavior change; needs a
  test update or revert decision.

This lane's mechanism is designed to compose with (not paper over)
that machinery: it excludes multi-result signatures, so fixing the
regression later does not conflict.

## 9. Non-goals

- No changes to generators (mechanism derives from their output).
- No change to multi-result desugar/fail-fast behavior.
- No new bridges, no name-keyed special cases, no package-name-keyed
  logic. The only "list" is the per-package AST-derived manifest.
- The stub funcdecls remain emitted (dead) — emission skipping is
  follow-up, mirroring the multi-result "skipped at export" design.
