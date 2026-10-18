# Test-driver fidelity: `graalgoT` methods and `TestMain` support

Lane: `review3-p1-testfidelity` (remediation for independent-review P1-6, P1-7).
Scope: [cmd/graalgo/testsynth.go](../../cmd/graalgo/testsynth.go),
[cmd/graalgo/test.go](../../cmd/graalgo/test.go). Read-only with respect to
the backend: everything here is driver-synthesis (build tooling), not a
synthetic stdlib — no bridge implications.

Reference semantics below are taken from the Go toolchain's testing package
(go1.27.1, the version vendored by this repo). Anything stated as "gc
does X" was read there, not recalled.

## P1-6: `Parallel`, `Cleanup`, `TempDir`, `Setenv` on `graalgoT`

### gc guarantees (verified in source)

- `Cleanup(f)`: appended to the test's cleanup list; runs **LIFO** at test
  end. Runs even when the test ends via `Fatal`/`Goexit`, `Skip`, or a
  panic: in gc the runner's cleanup loop sits in a deferred frame, so it
  executes during unwind before the panic is recovered and reported.
- `TempDir()`: creates a numbered subdirectory of a per-test parent under
  `$GOTMPDIR` (pattern derived from the test name); registers a `Cleanup`
  doing `RemoveAll`, and on removal failure calls
  `Errorf("TempDir RemoveAll cleanup: %v", err)`. Creation failure calls
  `Fatalf("TempDir: %v", err)`. Removal is unconditional — it happens on
  failure too.
- `Setenv(key, value)`: `os.LookupEnv` + `os.Setenv`; on `Setenv` error
  `Fatalf("cannot set environment variable: %v", err)`; registers a
  `Cleanup` restoring the previous value (or unsetting). Env interaction
  with parallelism is enforced both ways with the exact panic text
  ``testing: test using t.Setenv, t.Chdir, or cryptotest.SetGlobalRandom can not use t.Parallel``:
  - `Setenv` after `Parallel` on the test *or any ancestor* panics
    (`checkParallel` walks the parent chain), then arms `denyParallel`.
  - `Parallel` after `Setenv` on the same test panics (via `denyParallel`).
- `Parallel()`: panics `testing: t.Parallel called multiple times` on a
  second call. Semantically it pauses the test until all sequential tests
  finish, then runs it concurrently with other parallel tests.

### Design decisions

`graalgoT` gains `parent *graalgoT`, `isParallel`, `denyParallel bool`,
and `cleanups []func()`. `Run` sets `sub.parent = t`.

- **Cleanup**: `t.cleanups = append(t.cleanups, f)`; `runCleanups()` drains
  LIFO. Wiring (ordering vs the fatal/skip panic protocol — see
  §Interaction with the P1-2 lane):
  - Top-level test wrapper in generated `main`: the `recover` defer is
    registered **first**, `defer tt.runCleanups()` **second**, so cleanups
    run *before* the panic is classified — exactly the gc order
    (cleanups during unwind, then the framework reports the panic).
  - `Run`: `f(sub)` is wrapped so `defer sub.runCleanups()` executes on
    return *and* on panic unwind; the panic then continues to the
    top-level `recover`. This matches gc: a subtest's cleanups run when
    the subtest dies, even via `Fatal`.
- **TempDir**: `os.MkdirTemp(os.Getenv("GOTMPDIR"), "graalgotest-*")`
  (honors `GOTMPDIR` like gc; the per-test numbered-subdir layout is an
  internal detail with no observable effect on a sequential driver).
  Creation failure: mark failed, log `TempDir: …`, panic with the
  established `"graalgo test fatal: "` prefix so the existing `recover`
  classifies it as fatal. Removal via `Cleanup` + `RemoveAll`; removal
  failure marks failed and logs `TempDir RemoveAll cleanup: …` (gc's
  `Errorf` text). Unconditional on pass/fail, since `Cleanup` always runs.
- **Setenv**: `checkParallel` walk over self + ancestors with gc's exact
  panic text; arm `denyParallel`; `LookupEnv`/`Setenv` with gc's
  `cannot set environment variable: …` fatal path; restore-or-unset via
  `Cleanup` (LIFO ordering gives correct nesting for repeated `Setenv` of
  the same key).
- **Parallel**: records `isParallel = true` with gc's two panic guards
  (double-call; after own `Setenv`). **Scheduling is a documented no-op**:
  the driver executes tests strictly sequentially, so there is nothing to
  pause for and no concurrency to join. The observable contract that is
  kept: the test still runs exactly once, to completion, in declaration
  order; and the `isParallel` mark preserves gc's `Setenv`-after-`Parallel`
  panic. Real parallel execution would require goroutine-per-test plus a
  concurrency-safe shim — a separate design, not this lane.

Error-value rendering in shim-internal failure paths uses the shim's
existing `sprintf("%v", …)` (which renders `<value>` for non-scalars),
consistent with how test bodies' `%v` already degrade in this shim. The
shim's own code stays within backend-proven surface (package-level
`func`s; no interface method calls such as `err.Error()`).

`testCallRewrites` is **not** extended: these four methods need no
`file:line` (only their rare failure paths log, and those name the test).
Calls through stored `*graalgoT` receivers (not rewritten) hit the same
public methods, so there is exactly one implementation.

### Deliberately out of scope (residual gaps, not regressions)

`t.Context()`, `t.Deadline()`, `t.Chdir()` remain unimplemented; a test
using them still fails driver compilation, which is loud, not silent.
`Chdir` belongs to the same `parallelConflict` family and is the natural
follow-up.

## P1-7: `TestMain(m *testing.M)` support

### Current defect

`discoverTests` collects *any* `Test`-prefixed one-param function —
including `func TestMain(m *testing.M)`. The parameter rewrite only maps
`*testing.T` → `*graalgoT`, so the driver ends up calling `TestMain(tt)`
with a `*graalgoT` where `*testing.M` is required: a driver **compile
error**. (The review's "silently never run" predates the `*testing.M`
check; either way the package's setup/teardown contract is broken.)

### gc guarantees (verified in source)

- `TestMain` runs **once per test binary**, replacing the default runner:
  it typically does setup, `code := m.Run()`, teardown, `os.Exit(code)`.
- `(*M).Run() int` runs the full test set and returns an exit code
  (1 on any failure, 0 on success); it records `m.exitCode`, so if
  `TestMain` returns without calling `os.Exit`, the binary exits with
  `m.exitCode` (0 if `Run` was never called — tests then never run).
  Multiple `Run` calls are tolerated (no once-guard).
- `TestMain` must have exactly the shape `func TestMain(m *testing.M)`;
  anything else is not a hook (and a zero-param `TestMain` is not a test
  either — it matches neither the hook nor the `TestXxx(t *testing.T)`
  shape).
- More than one `TestMain` in the test binary is a compile error under gc.

### Design decisions

- `discoverTests` explicitly skips `TestMain` (it is not a test).
- New `hasTestMain(path) (bool, error)`: parses the file, reports whether
  it declares `func TestMain(m *testing.M)` — no receiver, exactly one
  parameter of type `*testing.M`. `runPackage` evaluates it over all
  buildable `*_test.go` files (not just `-run`-selected ones: `TestMain`
  is the entry point and always runs under gc).
- Multiple definitions → loud failure naming the package:
  `graalgo test: <importPath>: multiple definitions of TestMain (…)`,
  instead of a downstream redeclaration error.
- Synthesis (`synthesizeTestDriver` gains a `testMainFile string`
  parameter):
  - The `TestMain` declaration is gathered from whichever file declares
    it (selected-test file *or* sibling-helper file — the sibling loop's
    entry-point skip exempts it), its `*testing.M` parameter rewritten to
    `*graalgoM` (the existing `rewriteParam` is generalized; the
    `*graalgoT` call-rewrite pass is a no-op on it since `m` is not a
    `*graalgoT` param).
  - `TestMain` is never renamed by the external-package collision pass
    (explicit guard): gc requires the hook to keep its name, and a
    white-box `TestMain` colliding with it fails loudly at driver compile
    time, as it should.
  - Generated `main` for the TestMain path:
    ```go
    func main() {
        m := &graalgoM{}
        TestMain(m)
        os.Exit(m.exitCode)
    }
    ```
    `(*graalgoM).Run()` executes the shared per-test harness
    (`graalgoRunTests`, factored out of the existing inline loop so the
    non-TestMain path is byte-identical in behavior), sets `exitCode`
    1/0, returns it. `os.Exit` in the driver already propagates the exit
    code through `graalgoRun` (the existing failure path relies on it).
  - `-run` filtering still applies at discovery: `m.Run()` runs the
    selected set, matching gc where `-run` filters inside the binary.
    `-count=n` re-runs the whole binary (including `TestMain`) per
    iteration, as gc does.
  - `TestMain` present with zero selected tests: the driver is still
    synthesized (previously skipped when `ftList` was empty); with
    `-v` it prints `testing: warning: no tests to run`, matching gc.
- `testing.M`'s deprecated surface (`StartCPUProfile` etc.) is not
  shimmed; `Run` is the contract.

## Interaction with the P1-2 lane (`review3-p1-testsynth-fatal`)

No design doc exists in that lane yet (branch at master base as of
2026-10-18); this section records the composition contract so both
designs agree:

- The fatal/skip panic-string protocol (`"graalgo test fatal: "`,
  `"graalgo test skip: "` prefixes; classification in `main`'s deferred
  `recover`) is **unchanged** by this lane. New panics introduced here
  (`parallelConflict`, double-`Parallel`) deliberately use *different*
  prefixes so the existing classifier treats them as generic panics
  (`panic: …` + failed), exactly like gc reporting a test panic.
- Cleanup defer ordering is defined *relative* to the P1-2 `recover`
  defer (registered first → runs last). If P1-2 restructures the
  wrapper, the invariant to preserve is: **cleanups drain before the
  panic is classified**, on every exit path (normal, fatal, skip,
  generic panic).
- Known residual divergence (P1-2's territory, not this lane's): a
  `Fatal` inside a *subtest* unwinds through `Run` to the top-level
  `recover`, aborting the parent's remaining subtests; the parent's own
  cleanups then run at wrapper unwind, whereas gc runs them only when
  the parent test function actually returns. Fixing that requires
  changing fatal propagation, which belongs to the fatal/skip design.

## Verification

- Unit (string-level, existing `testsynth_test.go` style): `TestMain`
  discovery (positive/negative/wrong-signature), driver wiring with and
  without `TestMain`, shim method presence, no `graalgoM` emitted when
  unneeded.
- Behavioral: new `testdriver_exec_test.go` synthesizes drivers from
  fixture packages and executes them under **gc** (`go run`), asserting
  exit codes, output, and side effects — the honest fidelity proof:
  - `Cleanup` LIFO order, cleanup-on-`Fatal`, cleanup-on-panic;
  - `TempDir` creation, nested subtest removal timing, removal on
    failure;
  - `Setenv` set/restore/unset, `Setenv`-after-`Parallel` panic,
    `Parallel`-after-`Setenv` panic, `Parallel` double-call panic,
    `Parallel` as scheduling no-op (test still runs);
  - `TestMain`: setup runs before tests, `m.Run()` exit code on
    failure (1) and success (0), `TestMain` without `m.Run()` runs no
    tests and exits 0.
- `go vet`/`gofmt` clean; `make check`-relevant gates unaffected
  (no new generated files).

## Requirements

New registry entries: [GG-TEST-0001](../../requirements/GG-TEST-0001.yaml)
(`graalgoT` method fidelity), [GG-TEST-0002](../../requirements/GG-TEST-0002.yaml)
(`TestMain` support).
