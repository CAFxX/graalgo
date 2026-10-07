# D1 Peer Review (code) — Reviewer 1

**Scope:** P3-M8 D1 (WS-D multi-result interface dispatch), commit `847459a`
(branch `m8-pipeline`). Files reviewed: `phases/phase1/frontend/mrdesugar_m5.go`,
`phases/phase1/frontend/export.go` (D1 hunks), `phases/phase1/frontend/synth_io.go`
(D1 helpers), `phases/phase1/frontend/iface_multiresult_test.go`, against
`phases/phase3/hidden_files/m8-d1/DESIGN.md`.

**Method:** read every D1 hunk; traced all call paths that can reach a
multi-result interface call (`exportDefine`/`exportAssign` → `exportMultiResultDesugar`;
`exportExpr` → `exportCall`; `exportGoStmt`/`exportDeferStmt` → `exportCall`;
`ValueSpec` initializers → `exportExpr`); checked the shape guards for
soundness and for loudness on mismatch; verified D6's "dead arms" claim
against every `WriteTo`/`ReadFrom` body in the synthetic tree; ran the
frontend suite (`go test ./...` green, incl. all 5 D1 tests).

## Verdict: REQUEST_CHANGES

One P1: the implementation contradicts DESIGN.md §5 — bare `r.Read(buf)`
(and `go`/`defer` positions) fail fast with the M8 scope error instead of
dispatching as the design claims. Everything else is P2. No silent
divergence found: every shape-mismatch path I could construct fails loudly,
and single-result dispatch is untouched.

## Findings

### P1

**1. DESIGN.md §5 claim is false: non-desugar positions of multi-result
interface calls fail fast (export.go:4931-4933).**

DESIGN.md §5 states: "Bare `r.Read(buf)` as a statement: works (dispatch
evaluates, pair discarded)." It does not. In `exportCall`
(`phases/phase1/frontend/export.go:4931`), the pre-existing P3-M5 fail-fast

```go
if f, ok := selection.Obj().(*types.Func); ok && isMultiResultSynthetic(f) {
    return nil, multiResultSyntheticError(f)
}
```

fires *before* the `recvIface(selection.Recv()) != nil` branch
(export.go:4937) that routes to `exportIfaceMethodCall`. `isMultiResultSynthetic`
(synth_machinery.go:648) returns true for any 2-result method in package
`io` — including the interface methods `io.Reader.Read`/`io.Writer.Write` —
so every syntactic position that is not a 2-target `:=`/`=` desugar
(bare `ExprStmt` via export.go:3387→`exportExpr`→`exportCall`; `go`/`defer`
via export.go:3612/3447→`exportCall`) gets the M8 scope error. The desugar
path works only because `desugarIfaceMultiResult` (mrdesugar_m5.go:1118)
calls `exportIfaceMethodCall` directly, bypassing `exportCall`.

The failure is loud, not silent — but it is a design-implementation
contradiction on documented behavior, and the pre-D1 comment at the
fail-fast ("the shape must not reach dispatch") is now stale for the
table-hit shapes.

**Fix:** in `exportCall`, move the interface-dispatch branch above the
`isMultiResultSynthetic` fail-fast:

```go
// P3-M8 (WS D): interface calls route to exportIfaceMethodCall FIRST —
// it owns the multi-result decision (pairPlan dispatch for table methods,
// fail-fast otherwise). The fail-fast below then applies only to
// concrete (non-interface) calls.
if recvIface(selection.Recv()) != nil {
    return e.exportIfaceMethodCall(c, sel, selection)
}
if f, ok := selection.Obj().(*types.Func); ok && isMultiResultSynthetic(f) {
    return nil, multiResultSyntheticError(f)
}
```

This is behavior-preserving for every other case: non-table multi-result
interface methods still fail fast *inside* `exportIfaceMethodCall`
(export.go:5982-5988, same `multiResultSyntheticError`/WS-D errors as
today), and concrete multi-result calls still hit the fail-fast below.
Add a pinning test: bare `r.Read(buf);` (and `go`/`defer` variants) through
`io.Reader` must export an `ifacemethod` node instead of erroring. Either
fix the code or correct DESIGN.md §5 — they must agree.

### P2

**2. `TestD1IfaceMultiResultStillFailFast` does not pin the case DESIGN §6
names (iface_multiresult_test.go:288).**

The test uses a 3-result method on a *user* interface, which exercises the
generic WS-D scope error (export.go:5984). It does not cover a 2-result
*synthetic* interface method with no D1 table entry — `io.ReaderFrom.ReadFrom`,
the exact example in DESIGN.md §6 — which takes the `multiResultSyntheticError`
M8 path (export.go:5985-5986). Add a case, e.g.:

```go
var rf io.ReaderFrom = io.Discard // implements ReaderFrom (fail-fast body)
n, err := rf.ReadFrom(strings.NewReader("hi"))
// expect export error containing the M8 multi-result scope error
```

**3. `TestD1IoDesugarWiring` pins pair field names but not field types
(iface_multiresult_test.go:60).**

The export-time guards (`multiResultHelperFunc`, mrdesugar_m5.go:925;
`classifyIfaceMultiResultCall`, mrdesugar_m5.go:825) do check types
positionally and fail loudly, and the synthetic sources are go/types-checked,
so a type change cannot slip through silently today. Still, extend the wiring
test with `types.Identical(st.Field(i).Type(), <expected>)` per entry — the
test's stated purpose is pinning the table against synth_io.go, and types are
half of that contract (e.g. `copyPair.written int64` vs `Copy`'s `int64`).

**4. Deviation D6 justification is imprecise (DESIGN.md §4; synth_io.go:420).**

"no synthetic type implements them with a real body, so the arms are dead"
undersells it: `os.File`'s `__fileReadFrom`/`__fileWriteTo` *exist* but are
named-trap panics (synth_os.go:2334, 2347, P0-2), and every other
`WriteTo`/`ReadFrom` body in the synthetic tree (`bytes.Buffer`, `discard`,
`multiReader`, `nopCloserWriterTo`, `strings.Reader`) is fail-fast. The
omission is load-bearing, not merely an optimization: *with* the gc arms,
`io.Copy` from an `*os.File` source would hit the P0-2 trap; without them the
generic `src.Read`/`dst.Write` loop produces correct output. Verified the
claim holds; suggest rewording to: "every WriterTo/ReaderFrom implementation
in the synthetic tree is a fail-fast or named-trap body, so the fast-path
arms can never execute successfully — omitting them avoids routing Copy into
the os.File P0-2 trap."

**5. `var n, err = r.Read(buf)` (ValueSpec) and `return r.Read(buf)` fail
fast loudly — document the limitation (export.go:3994, exportCall).**

`exportMultiResultDesugar` handles only `*ast.AssignStmt` (`:=`/`=`); a
`ValueSpec` initializer routes through `exportExpr` → `exportCall` →
the 4931 fail-fast, and `return r.Read(buf)` likewise. Both are legal Go and
fail with a clear scope error (no silent divergence), and the limitation is
consistent with the pre-existing M5/M7 concrete desugar — but neither
DESIGN.md nor the code comments state it. Either extend the desugar to
`ValueSpec` or record it as a named loud limitation with a re-test trigger.

**6. Differential byte-identical verification was still running at handoff.**

PROGRESS.md ("Byte-identical check running (gc vs graalgo, gmp=1,4)") —
the end-to-end behavioral equivalence claim in DESIGN §6 is not yet
confirmed. The exporter's static story checks out (this review), and the
worker reports a P3SyncMain smoke test passing, but sign-off should wait for
the differential to complete, given findings 1 and 5 touch runtime-reachable
call positions.

## What checked out (no findings)

1. **Pair shapes / positional guard soundness.** The chain is airtight and
   loud at every link: `classifyIfaceMultiResultCall` (mrdesugar_m5.go:825)
   requires name + arity + positionally `types.Identical` field types *and*
   field names against the interface pair (`ioReadPair`/`ioWritePair`);
   `multiResultHelperFunc` (mrdesugar_m5.go:925) requires the concrete
   helper's pair field types positionally identical to the *method's* results
   (mismatch → `ok=false` → method omitted from itab → dispatch fails fast
   "missing method"). Transitivity via Go's identical-signature method
   satisfaction makes the runtime by-index extraction (`SelectorNode.readFieldPath`)
   sound. A reordered/renamed/retyped pair struct fails the export loudly —
   it cannot silently mis-extract.
2. **Fail-fast for non-table methods preserved.** 3-result user methods,
   2-result synthetic non-table methods (`ReadFrom`, `WriteTo`), concrete
   multi-result methods with no helper (`bytes.Buffer`, `multiReader`),
   promoted-via-interface-embedding methods (no receiver → `multiResultHelperFunc`
   bails), and user-declared multi-result methods (fail at `exportFuncDecl`,
   export.go:3088 — which is also why the pre-existing
   `TestIfaceMultiResultScopeError` still passes unchanged) all fail loudly.
   I could not construct a silent path.
3. **No interference with single-result dispatch.** The `pairPlan` branch in
   `exportIfaceMethodCall` (export.go:5982) activates only when
   `sig.Results().Len() > 1`; the `helperMapped` branch in `methodTableEntries`
   (export.go:5714) activates only on `funcByObj` miss *and* ≥2 results;
   the new `classifyMultiResultCall` arm (mrdesugar_m5.go:550) is last and
   interface-only. Full frontend `go test ./...` green (incl. all 5 D1 tests).
4. **Concrete-position desugar (2-target `:=`/`=`, incl. `if`-init via
   `exportOptStmt`) routes correctly**; `desugarIfaceMultiResult` builds the
   temp and positional extractions identically to the `mrM7` path.
5. **Helper algorithms match gc 1.27.1** (`__readAtLeast`/`__readFull`/
   `__copyBuffer`/`__copyN`/`__readAll`/`__writeString`/`__limitedReaderRead`/
   `__copyBufferChecked` empty-buffer panic placement) — including the
   `written==n`/EOF adjustment in `__copyN` and the `errInvalidWrite`/
   `ErrShortWrite` normalization in `__copyBuffer`. The `discard` value
   receivers and `*LimitedReader` pointer receiver get the correct `recv`
   ("value"/"pointer") from the *method's* declared receiver, so dispatcher
   receiver adaptation matches the concrete desugar's `prependRecv`
   convention.
6. **Structural matching for user interfaces** (test 5) is sound: a
   same-shaped user method on a non-synthetic concrete type can never resolve
   to a helper (`multiResultHelperFor` requires the synthetic package path),
   and promoted interface-embedded methods are excluded by the nil-receiver
   check — both loud, never silent.
