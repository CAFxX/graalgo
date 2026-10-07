# D1 progress checkpoint (2026-10-07 ~20:30 CEST, updated ~22:30 UTC)

## Done
- DESIGN.md written (this dir).
- Exporter: `exportIfaceMethodCall` multi-result branch, `mrIfaceMethod`
  plan kind, `classifyIfaceMultiResultCall`, `classifyIoMultiResultCall`,
  `desugarIfaceMultiResult`/`desugarExtract` refactor, `methodTableEntries`
  helper fallback + `multiResultHelperFunc` (with positional shape guard).
- synth_io.go: real `__`-helpers for WriteString, ReadAtLeast, ReadFull,
  Copy, CopyN, CopyBuffer (+`__copyBufferChecked` for the empty-buffer
  panic), LimitedReader.Read, ReadAll, discard.Write/WriteString;
  `ioReadPair`/`ioWritePair` interface carriers; header inventory +
  deviation D6 updated.
- Incidental fixes needed for a green suite (all pre-existing red at
  HEAD 7e53780, verified against ~/workspace/tmp/m8base baseline):
  (1) removed bogus `func (p *Process) Pid()` in synth_os.go (field/
  method name clash — panicked every os import); (2) stale
  testdata/bad_import.go now imports "net" (os is synthetic since wave 2);
  (3) `__stringsReaderRead`/`__stringsReaderReadAt` used copy() from a
  string source (frozen factory rejects) — replaced with manual byte
  loops; (4) ReflectTypes.java `basicKind` was missing `"rune"` — added.
  A concurrent worker landed the strings→io `needIo` fix mid-session
  (verified working, not mine) and is editing synth_reflect.go /
  ReflectTypes.java — do not touch.
- `go build`, `go vet`, `gofmt` clean; full frontend `go test` green.
- **End-to-end verified**: P3SyncMain runs a program using `r.Read` through
  io.Reader, `io.ReadAll`, `io.Copy` (strings.Reader -> io.Discard) with
  correct output.

## Pinning tests (all pass; red verified on baseline)
- `phases/phase1/frontend/iface_multiresult_test.go` (5 tests):
  TestD1IoDesugarWiring (10 table entries), TestD1IfaceMethodTableHelperMapping
  (itab Read/Write -> helper obj ids), TestD1IfaceMultiResultCallNode
  (ifacemethod node typed ioReadPair + desugared block shape),
  TestD1IfaceMultiResultStillFailFast (3-result method keeps fail-fast),
  TestD1IfaceMultiResultUserInterface (structural match). Baseline: same
  programs panic (red).

## Differential corpus
- Rewrote `12_copy_family.go`, `13_readall_growth.go`,
  `14_readatleast_edges.go` with synthetic types behind interfaces
  (user-defined 2-result methods impossible in v1); added
  `16_writestring.go`. Un-scope-limited 12/13/14.
- Byte-identical check running (gc vs graalgo, gmp=1,4).
- NOTE: 18 pre-existing io-corpus failures ("call to unknown function
  New", programs using io.Pipe/NewSectionReader) — broken at HEAD before
  D1 (baseline panics). Program 15 stays scope-limited; pipe is separate
  work, not D1.

## Next
- Confirm differential byte-identical.
- Two peer reviews per the quality contract.
- Handoff to coordinator.
