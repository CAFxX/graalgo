# Phase 4 Driver Drop-in Audit

**Lane:** phase4-driver  
**Date:** 2026-10-15  
**Goal:** Audit `graalgo` driver commands against real `go` for true drop-in behavior.

## Divergences Found

### 1. `graalgo test -run` used substring instead of regex — FIXED

**Status:** Fixed in this lane.

`go test -run` takes a regex (unanchored). The driver used `strings.Contains`,
so `-run 'Add$'` matched nothing instead of matching `TestAdd`.

**Fix:** Compile `-run` as `regexp.Regexp`, use `MatchString`. Invalid regex
exits 1 (matching `go test`).

### 2. `os.Args[0]` in `graalgo run` is the source file — DOCUMENTED DIVERGENCE

`go run` sets `os.Args[0]` to the compiled binary's temp path.
`graalgo run` sets it to the source file path (backend documents this at
`P3SyncMain.java:121-122` as deliberate).

**Impact:** Programs that inspect `os.Args[0]` see a `.go` file instead of a
binary path. The exact temp path from `go run` is not stable or meaningful,
but the file-vs-binary distinction is observable.

**Recommendation:** Driver could pass a synthetic argv[0]; needs backend
support to override. Filed for backend lane.

### 3. `graalgo run` on multi-file packages is broken — NEEDS BACKEND

`cmdRun` checks each file with `--check`, then runs only `files[0]`
(alphabetically first). For a package with `main.go` + `helper.go`, it tries
to run `helper.go` alone → "no func main found".

**Root cause:** Backend is single-file scoped. Driver-side concatenation would
be a workaround. True fix requires backend multi-file package support.

**Status:** Documented, not fixed in driver lane.

### 4. `graalgo build` produces no binary — DOCUMENTED DIVERGENCE

`go build` produces an executable. `graalgo build` only runs the frontend
`--check` (documented in help text as intentional: "the Truffle backend is
JIT-compiled at run time").

**Impact:** Any workflow expecting a binary artifact breaks. This is a
fundamental Phase-4 architecture question, not a driver bug.

**Status:** Documented, needs product decision.

### 5. `graalgo vet` has no analyzers — DOCUMENTED GAP

`cmdVet` just runs the frontend `--check`. No `printf`, `atomic`, `copylocks`,
or other vet analyzers. Help text admits: "deeper vet analyses arrive with
the driver work."

**Status:** Documented, needs analyzer implementation.

### 6. `graalgo test -v` output format differs — DOCUMENTED GAP

`go test -v` prints `=== RUN   TestAdd`, `--- PASS: TestAdd (0.00s)`, `PASS`,
`ok      pkg 0.123s`. The driver prints a different format (`=== RUN file.go`,
`    --- PASS: TestAdd`, etc.).

**Status:** Documented, needs output format alignment.

## Verified Identical

- `graalgo run hello.go` vs `go run hello.go`: stdout, stderr, exit code identical.
- `graalgo run` with non-zero exit: both exit 1 with "exit status N" on stderr.
- `graalgo test -run` with regex (after fix): matches `go test` selection.
- `graalgo test` with invalid `-run` regex: both exit 1 with error on stderr.
- `graalgo fmt` vs `gofmt -w`: byte-identical (shells out to gofmt).
- `graalgo version`: prints version, exit 0.
- Unknown command: both exit 2.
