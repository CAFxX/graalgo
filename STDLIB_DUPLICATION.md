# Stdlib Duplication Audit — real-fmt lane (2026-10-14)

Per Carlo's 2026-10-14 order: zero duplication of stdlib logic anywhere.
Every package must compile for real; graalgo never reimplements stdlib logic.

## Killed

1. **Frontend**: `fmt.Fprint/Fprintf/Fprintln` desugaring (mrFmtF) → `w.Write([]byte(fmt.S...(...)))`
   - Commit: 41ad340
   - Was: workaround for real fmt not compiling
   - Now: F-calls flow as fmtcall nodes; backend handles via GoMethodDispatch

## Found (to kill)

2. **Backend**: `internal/backend/truffle/src/main/java/graalgo/frontend/FmtCallNode.java`
   + `internal/backend/runtime/src/main/java/graalgo/runtime/Fmt.java`
   - Java reimplementation of fmt formatting logic (sprint/sprintf/sprintln,
     print/printf/println, append/appendf/appendln, errorf, formatString)
   - Kill when: real fmt compiles (needs reflect)

3. **Backend**: `internal/backend/runtime/src/main/java/graalgo/runtime/FmtScan.java`
   - Java reimplementation of fmt scanning logic (sscan/sscanf/sscanln,
     fscan/fscanf/fscanln, scan/scanf/scanln)
   - Kill when: real fmt compiles (needs reflect, io)

## Legitimate (not duplication)

- Multi-result desugarings (`math.Sincos` → `__sincos`, `math/cmplx.Polar` → `__cmplxPolar`,
  `math/bits.Div` → `__bitsDiv`, etc.): same function, different calling convention
  for Go's multi-return. This is ABI handling, not logic duplication.
- Language construct desugarings (for-range over channels, `go f(args)`, etc.):
  lowering Go syntax to IR core. Legitimate compiler work.

## Next steps

- Real fmt needs: working `reflect` (currently stubs), real `io`, real `os`,
  real `strconv`, real `unicode/utf8`, `internal/fmtsort`
- The reflect gap is the big one — most of synth_reflect.go is panic stubs
