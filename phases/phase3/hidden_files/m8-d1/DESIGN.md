# D1 — WS-D multi-result interface dispatch: design

Worker: D1 (WS-D multi-result interface dispatch), 2026-10-07.
Status: design complete; implementation follows this note.

## 1. Problem

`phases/phase1/frontend/synth_io.go` fail-fasts `Copy/CopyN/CopyBuffer/
ReadFull/ReadAll` (plus `WriteString`, `ReadAtLeast`, `LimitedReader.Read`).
They need 2-result calls on interface values (`src.Read(buf)` with
`src io.Reader`, `dst.Write(buf)` with `dst io.Writer`). Today
`exportIfaceMethodCall` (export.go:5920) fail-fasts any multi-result
interface method via `isMultiResultSynthetic`/`multiResultSyntheticError`,
and the itab has no target for multi-result methods (they are skipped from
`funcByObj`/`methodIndex` at synthetic import — v1 cannot emit multi-result
funcdecls at all, export.go:3084).

## 2. Key insight

Every concrete type that can sit behind `io.Reader`/`io.Writer` in a v1
program implements `Read`/`Write` through a **single-result `__`-helper
returning a pair struct** (the Appendix M pattern: `__stringsReaderRead`
→ `stringsReaderReadPair{n,err}`, `__pipeReaderRead`,
`__fileRead`→`readPair`, `__streamReaderRead`, …). User-declared 2-result
methods are impossible (exportFuncDecl scope-errors), so the helper set is
closed and known at export time.

Interface dispatch (`IfaceDispatchNode`) already invokes
`target.func()` as `GoCalls.invoke(func, prepend(data, argValues))` —
**exactly the helpers' calling convention** (`prependRecv`: receiver as
explicit params[0]). So the itab can map `Read` → the helper GoFunc, the
dispatch returns the helper's pair, and downstream field extraction
(`__p5mr_N.n` / `.__err` via `selector` nodes, which read struct fields
**by index** through the runtime descriptor — `SelectorNode.readFieldPath`)
works uniformly with the concrete desugar. **No Java change is needed**:
the wire `methods` entries just carry `obj` = helper funcID.

## 3. Exporter changes (all in `phases/phase1/frontend/`)

1. **New table** `m8IfaceMultiResultMethods` (mrdesugar_m5.go): the
   interface-level pairs —
   `("io","Reader","Read") → ioReadPair{n int, err error}`,
   `("io","Writer","Write") → ioWritePair{n int, err error}`,
   `("io","StringWriter","WriteString") → ioWritePair` (for the
   `WriteString` fast path; see §5).
   New synthetic pair structs in synth_io.go give the `ifacemethod` node
   its static result type. Matching is structural on (method name, result
   count, positional result types) so user-declared interfaces with the
   same shape also dispatch; anything else keeps the existing fail-fast.
2. **`exportIfaceMethodCall`**: when `sig.Results().Len() > 1`, classify
   via the new table. On a hit, emit the same `ifacemethod` node with
   `"type"` = pair type id (no `callType`, which scope-errors on tuples).
   On a miss, keep the old fail-fast (`isMultiResultSynthetic` /
   scope error). Single-result path untouched.
3. **`classifyMultiResultCall`**: recognize interface multi-result calls
   (new `mrIfaceMethod` plan kind). `desugarMultiResult` then emits
   `__p5mr_N := <ifacemethod>` + positional field extractions — identical
   downstream to `mrM7`.
4. **`methodTableEntries`**: when a method is absent from `funcByObj`
   (multi-result synthetics), fall back to the desugar tables
   (`m7`/`os`/`m8`): `(pkg, method, recvType) → __-helper`. Emit the entry
   with `obj` = helper funcID, `recv` from the method's declared receiver
   (so promoted-method adaptation stays correct), `implkey` from the
   method's own signature (keeps the typeassert subset check sound).
   **Loudness guard**: the helper's pair struct field types must be
   positionally identical to the method's result types, else export fails —
   a shape mismatch can never silently mis-extract.
   Methods with no helper keep the old skip (dispatch fails fast
   "missing method" — loud, never silent).

## 4. Synthetic io (synth_io.go): real implementations via __-helpers

v1 cannot emit 2-result funcdecls, so the exact-gc-signature funcs keep
fail-fast bodies for type-checking; the real logic lives in single-result
helpers + new Appendix M table entries (the M7/M8 pattern):

- `__readAtLeast(r, buf, min) readAtLeastPair` — gc loop over `r.Read`.
- `__readFull(r, buf) readFullPair` — via `__readAtLeast` (single→single).
- `__copyBuffer(dst, src, buf) copyBufferPair` — gc loop over
  `src.Read`/`dst.Write` with `errInvalidWrite`/`ErrShortWrite`
  normalization. **Deviation D6**: the `WriterTo`/`ReaderFrom` fast paths
  are omitted — every `WriterTo`/`ReaderFrom` implementation in the
  synthetic tree is a fail-fast or named-trap body (`bytes.Buffer`,
  `discard`, `multiReader`, `nopCloserWriterTo`, `strings.Reader` are
  fail-fast; `os.File`'s `__fileReadFrom`/`__fileWriteTo` are named-trap
  panics at synth_os.go:2334,2347, P0-2), so the fast-path arms can never
  execute successfully. Omitting them is load-bearing, not an
  optimization: with the gc arms, `io.Copy` from an `*os.File` source
  would route into the os.File P0-2 trap. Re-test trigger: when
  WriteTo/ReadFrom dispatch lands, restore the arms.
- `__copy(dst, src) copyPair` — via `__copyBuffer(dst, src, nil)`.
- `__copyN(dst, src, n) copyNPair` — via `__copyBuffer` over
  `LimitReader(src, n)` + gc's `written==n`/EOF adjustment.
- `__readAll(r) readAllPair` — gc's exponential-growth loop over `r.Read`.
- `__writeString(w, s) writeStringPair` — gc's two arms: `w.(StringWriter)`
  fast path (typeassert-to-interface already works — `NopCloser` uses it;
  implkey subset check stays sound via §3.4) else `w.Write([]byte(s))`.
- `__limitedReaderRead(l, p) limitedReaderReadPair` — gc algorithm over
  `l.R.Read`.
- `__discardWrite(d, p) ioWritePair`, `__discardWriteString(d, s)
  ioWritePair` — trivial; make `io.Discard` a usable Writer/StringWriter
  behind interfaces (needed for corpus programs; previously fail-fast).

`ReadAtLeast`/`ReadFull` desugar entries make the public funcs callable;
`LimitedReader.Read` becomes real via its helper (needed by `CopyN`).

## 5. What is NOT in scope (stays fail-fast, loud)

- User-declared multi-result methods (exportFuncDecl) — unchanged.
- Multi-result interface methods with no table entry
  (`Context.Deadline`, `io.ReaderFrom.ReadFrom`, …) — unchanged fail-fast.
- Concrete types with multi-result Read/Write but no helper
  (`bytes.Buffer`, `multiReader`, …) — dispatch fails fast "missing
  method"; corpus programs avoid them.
- Bare `r.Read(buf)` as a statement: works (dispatch evaluates, pair
  discarded). `foo(r.Read(buf))` never type-checks (go/types).

### Non-desugar positions (named loud limitation; REVIEW-1 P2-5)

`exportMultiResultDesugar` handles only 2-target `:=`/`=`
(`*ast.AssignStmt`, incl. `if`-init via `exportOptStmt`) — consistent with
the pre-existing M5/M7 concrete desugar, which never covered other
positions. A multi-result interface call in any other syntactic position
fails fast loudly, never silently:

- `var n, err = r.Read(buf)` (`var` declaration / ValueSpec):
  `exportVarGenDecl` rejects it with the named M8 limitation error. The
  ifacemethod node yields a single pair-typed value which cannot be split
  across the declared names, so exporting it would be a silent wire-shape
  divergence (a 2-name/1-value vardecl).
- `return r.Read(buf)`: only type-checks inside a function whose results
  match the call's; v1 rejects every function with more than one result
  at `exportFuncDecl` (the pre-existing WS D scope error), so no
  multi-result return can reach the exporter.

Re-test trigger: extend `exportMultiResultDesugar` to ValueSpec and
return positions.

## 6. Verification plan

- Phase-1 `go test ./...` green (incl. new pinning tests:
  ifacemethod node shape for `r.Read`, method-table helper mapping incl.
  shape-mismatch loudness, desugar block shape, user-interface structural
  match, non-table method still fail-fasts).
- Red-green: each new behavior has a test failing before the fix
  (stashed-tree check for the exporter tests; fail-fast bodies for io).
- Phase-3 `make test` green (no Java change; single-result dispatch must
  not regress).
- Differential: corpus programs (new + un-scope-limited 12/13/14/15)
  exercising Copy/CopyN/CopyBuffer/ReadAll/ReadFull/WriteString through
  interfaces with `strings.Reader`/`LimitedReader`/pipe/Discard behind
  them; gc vs graalgo at gmp=1,4, byte-identical. `12_copy_family.go`
  must be rewritten (its user-defined 2-result `Write` can never export).
