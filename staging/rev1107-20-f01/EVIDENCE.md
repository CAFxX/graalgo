# R20-F01 — RED/GREEN evidence (lane rev1107-20-f01, 2026-11-07 ~20:40 CET)

Base: graalgo HEAD `59f0a247` (verified `git rev-parse HEAD`, export.go clean).
Fix: `internal/frontend/export.go` — three sites gate on `obj.Pkg().Path() ==
"runtime"` instead of `obj.Pkg().Name() == "runtime"` (isGoschedCall ~:6914,
Goexit guard ~:8564, runtime.* block ~:8585). Goexit site restructured from a
negative gate (scope error when not runtime-named) to a positive intercept
with fall-through — a literal Name→Path swap there would have kept the
spurious scope error for a non-stdlib `runtime.Goexit`.
New test: `internal/frontend/runtime_nonstdlib_test.go`
(`TestNonStdlibRuntimePackageNotIntercepted`) — fake package
`example.com/rev1107-20-f01/runtime` (temp GOPATH) defining
Version/NumCPU/Gosched/Goexit; asserts no `runtimeversion`, `runtimenumcpu`,
`runtimegosched` nodes and no `"goexit": true` marker.

Verification method (home quota CLOSED — real 8 KB probe failed EDQUOT):
module copied to /tmp (no testdata), built with the shared warm cache
/tmp/graalgo-gocache (stdlib entries warm; frontend package compiled fresh).
`go build .` OK (7.6 s). Test binaries built with `-ldflags="-s -w"`.

## GREEN (patched code) — PASS

```
=== RUN   TestNonStdlibRuntimePackageNotIntercepted
--- PASS: TestNonStdlibRuntimePackageNotIntercepted (0.01s)
PASS
rc=0
```

## RED (unpatched export.go, same new test) — FAIL, all four assertions

```
=== RUN   TestNonStdlibRuntimePackageNotIntercepted
    runtime_nonstdlib_test.go:69: exported JSON contains "kind": "runtimeversion": non-stdlib runtime call was intercepted as stdlib runtime
    runtime_nonstdlib_test.go:69: exported JSON contains "kind": "runtimenumcpu": non-stdlib runtime call was intercepted as stdlib runtime
    runtime_nonstdlib_test.go:69: exported JSON contains "kind": "runtimegosched": non-stdlib runtime call was intercepted as stdlib runtime
    runtime_nonstdlib_test.go:69: exported JSON contains "goexit": true: non-stdlib runtime call was intercepted as stdlib runtime
--- FAIL: TestNonStdlibRuntimePackageNotIntercepted (0.01s)
FAIL
rc=1
```

## No-regression (patched code) — all 18 existing runtime/Goexit tests PASS

`go test -run "TestRuntime|TestGoexit"`: TestGoexitRenamedImport (canonical +
renamed), TestRuntimeCallersExport, TestRuntimeCallersFramesExport,
TestRuntimeCallerExport, TestRuntimeFuncExport, TestRuntimeStackFailFast,
TestRuntimeStackFrameFields, TestRuntimeNumGoroutineExport,
TestRuntimeErrorView, TestRuntimeErrorShape, TestRuntimeViewDecls,
TestRuntimeViewFuncsHonest, TestRuntimeViewCompleteness,
TestRuntimeGoschedExport, TestRuntimeGoschedDeferGoExport,
TestRuntimeFuncNameMethodValueExport,
TestRuntimeFuncNameMethodValueMultiResult, TestRuntimeFuncNameGenericContext.
rc=0.

## Not verified (quota-blocked, resume agent must do)

- Full frontend suite (`go test ./...` in internal/frontend) — needs testdata/
  and a warm cache; only the targeted subset above ran.
- `gofrontend` end-to-end / `make snapshot-check` differential — needs a
  worktree build on home.
- `gofmt` clean on both staged files (verified with ~/tools/go/bin/gofmt).

## Mechanism verification (disk truth, this run)

- Exactly three Name()-based sites in export.go (repo-wide grep for
  `Pkg().Name() == "runtime"` / `!= "runtime"` in internal/frontend + cmd):
  :6914, :8564, :8585. No others.
- Overlay runtime package path IS "runtime": export.go:250
  `types.NewPackage("runtime", "runtime")` — Path() matches the overlay
  exactly, as it did for Name().
- Precedents all Path()-based: atomic :8519, reflect :11199/:11229,
  runtimeStackMethod :639, mrdesugar :1698/:2300. mrdesugar `case "runtime":`
  switches on `pn.Imported().Path()` — desugar side already clean.
- R-1107-03 read (REVIEW-2026-11-07-07.md): same bug class, spelling vs
  resolved package; fix is to resolve through type info.
- Outer `pkgIdent.Name == "runtime"` pre-filters left untouched: they only
  narrow interception (skip when the local name differs), never cause a
  hijack. Aliased stdlib runtime (`rt "runtime"; rt.NumCPU()`) bypasses the
  :8585 block today via that pre-filter and falls through to exportFuncCall
  — pre-existing behavior, unchanged by this fix; flagged as a follow-up
  question for the reviewer, not fixed here.
