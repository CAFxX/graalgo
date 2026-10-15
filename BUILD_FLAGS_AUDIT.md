# Build Flags and Test Parity Audit

**Lane:** build-flags-audit
**Date:** 2026-10-15
**Goal:** Audit `graalgo` driver against `go` for build flags, caching, coverage, cross-compilation, and exit/stdout/stderr parity.

## Summary

The `graalgo` driver (`cmd/graalgo/`) does not parse build flags at all. Flags like
`-o`, `-ldflags`, `-count`, `-cover` are either treated as filenames (build) or
rejected as "unknown flag" (test). This is a drop-in compatibility gap: scripts
and tools that invoke `go build -o ...` or `go test -count=1 ...` will fail.

## Findings

### 1. `graalgo build -o` — BROKEN

**Status:** Flag not parsed; treated as filename.

```
$ graalgo build -o /tmp/mybin hello.go
graalgo build: no such file or directory: -o
exit=1
```

**`go` behavior:** `-o` sets the output binary path. `go build -o /tmp/mybin hello.go`
produces an executable at `/tmp/mybin`.

**Root cause:** `cmdBuild` passes all args directly to `resolveFiles`, which treats
every arg as a file/package path. No flag parsing exists.

**Fix:** Parse `-o` (and `-o=`) in `cmdBuild`. Binary emission is now
implemented (build-executable lane): `graalgo build` validates the program
through the frontend gate and emits an executable artifact — a
self-contained launcher script ([internal/driver/artifact.go](internal/driver/artifact.go))
that runs the program through the graalgo backend.

### 2. `graalgo build -ldflags` — BROKEN

**Status:** Flag not parsed; treated as filename.

```
$ graalgo build -ldflags "-X main.version=1.0" hello.go
graalgo build: no such file or directory: -ldflags
exit=1
```

**`go` behavior:** `-ldflags` passes flags to the linker. `-X importpath.name=value`
sets a string variable at link time.

**Root cause:** Same as §1 — no flag parsing in `cmdBuild`.

**Fix:** Parse `-ldflags` in `cmdBuild`/`cmdRun`. The `-X` form requires backend
support to inject the value. Driver should parse and forward; backend lane needed
for actual `-X` semantics.

### 3. `graalgo test -count` — REJECTED

**Status:** Explicitly rejected as unknown flag.

```
$ graalgo test -count=1
graalgo test: unknown flag "-count=1"
exit=2
```

**`go` behavior:** `-count=n` runs each test n times; `-count=1` disables test
caching. This is widely used in scripts (`go test -count=1 ./...`).

**Root cause:** `cmdTest` only accepts `-v` and `-run`; all other `-` flags are
rejected.

**Fix:** Accept `-count`. Since graalgo has no test caching, `-count=1` is
semantically a no-op (tests always run fresh). Accept and ignore (or implement
n-times repetition for n>1, which is a general mechanism).

### 4. `graalgo test -cover` — REJECTED

**Status:** Explicitly rejected as unknown flag.

```
$ graalgo test -cover
graalgo test: unknown flag "-cover"
exit=2
```

**`go` behavior:** `-cover` enables coverage profiling; prints coverage percentage.

**Root cause:** Same as §3.

**Fix:** Accept `-cover`. Coverage requires backend instrumentation. Driver should
parse the flag; backend lane needed for actual coverage. Until then, emit a clear
"not yet supported" error (better than "unknown flag", which suggests a typo).

### 5. `graalgo test -h` — REJECTED

**Status:** `-h`/`--help` rejected as unknown flag instead of showing help.

**Fix:** Handle help flags in `cmdTest` like `main.go` does for top-level commands.

### 6. Build caching — NONE

**Status:** No caching exists. Every `graalgo build` re-runs the frontend `--check`
from scratch on every file.

**`go` behavior:** Content-based build cache; unchanged packages are not rebuilt.
`go build` on an unchanged package is nearly instant.

**Impact:** Slow edit/build/test cycle (violates the "seconds, not minutes" order).

**Fix:** Requires a content-hash-based cache in the driver or backend. This is a
separate lane (not a flag-parsing fix).

### 7. Test caching — NONE

**Status:** No caching exists. Every `graalgo test` re-runs all tests.

**`go` behavior:** `go test` caches successful results; "ok pkg (cached)" on
re-run without changes. `-count=1` forces re-run.

**Fix:** Requires test result caching. Separate lane.

### 8. Cross-compilation (GOOS/GOARCH) — NOT HONORED

**Status:** `GOOS`/`GOARCH` env vars are ignored. The backend is JVM-based and
always targets the host.

**`go` behavior:** `GOOS=windows go build` produces a Windows binary.

**Fix:** For a Truffle/JVM backend, cross-compilation means something different
(the JVM is the target). The driver should at least recognize `GOOS`/`GOARCH`
and either honor them (if backend supports) or emit a clear error. Currently
silently ignored.

### 9. Exit code parity — MOSTLY OK

**Verified:**
- `graalgo run` with program exit N: prints "exit status N" to stderr, exits 1
  (matches `go run`).
- Unknown command: exits 2 (matches `go`).
- `graalgo test` with unknown flag: exits 2 (matches `go test`).

**Gap:** `graalgo build` on frontend failure exits 1; `go build` also exits 1. OK.

### 10. stdout/stderr routing — OK

**Verified:**
- `graalgoRun` wires `cmd.Stdout = os.Stdout`, `cmd.Stderr = os.Stderr` directly.
- Program stdout goes to stdout; backend errors go to stderr.
- No buffering or reordering introduced by the driver.

## Fix priority

**Driver lane (this lane) — DONE:**
1. ✅ Parse `-o` in `cmdBuild` — accepted; binary emission implemented by the
   build-executable lane (executable launcher artifact via internal/driver)
2. ✅ Parse `-ldflags` in `cmdBuild`/`cmdRun` — accepted, clear "not yet
   supported" error (exit 1)
3. ✅ Accept `-count` in `cmdTest` — parsed and honored (runs tests n times);
   `-count=1` is a no-op without a test cache
4. ✅ Accept `-cover` in `cmdTest` — clear "coverage not yet supported" error
   (exit 1) instead of "unknown flag"
5. ✅ Handle `-h`/`--help` in `cmdTest` — shows usage (exit 0)
6. ✅ Parse `-tags` in `cmdBuild`/`cmdRun` — accepted (backend honors via go/build)
7. ✅ Unknown build flags rejected with exit 2 (matches `go`)

**Separate lanes (not in scope):**
- Build caching (content-hash cache)
- Test caching (result cache)
- `-ldflags -X` backend support (inject string variables at link time)
- `-cover` backend instrumentation
- Cross-compilation semantics for JVM backend
