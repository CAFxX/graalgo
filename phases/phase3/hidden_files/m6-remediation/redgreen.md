# Red-green evidence — P3-M6 remediation (2026-10-07)

Worker: P3-M6 remediation (parent registry `graalgo-phase3-m6-remed`).
Every P1 fix below was demonstrated red-before / green-after; P2 fixes
carry test-level red-green where a test pins the behavior. All commands
run with `GOTOOLCHAIN=local` and `~/tools/go/bin` on PATH.

## P1-1 — `.S`/`.sx` assembly invisible to the asm strategy

**RED (pristine HEAD, isolated copy at /tmp/pristine-p3fe):** a scratch
test calling `CheckAsmFiles` on a module-local dir containing a
build-tagged `evil_amd64.S` (the reviewer's repro):

```
=== RUN   TestRedDemoEvilDotS
    zz_reddemo_test.go:15: RED: evil_amd64.S passed CheckAsmFiles silently (no resolutions, nil error)
--- FAIL: TestRedDemoEvilDotS (0.00s)
```

The file was never even read: the loader filter, the generator, the
embedded inventory and the test enumerator all matched lowercase `.s`
only.

**GREEN (fixed tree):** the permanent regression test
`TestAsmUppercaseSFailsFast` (scratch-goroot uncategorized `.S`,
module-local `evil_amd64.S`, and the `SelectAsmFiles` selection step):

```
--- PASS: TestAsmUppercaseSFailsFast (0.01s)
    --- PASS: TestAsmUppercaseSFailsFast/scratch-goroot
    --- PASS: TestAsmUppercaseSFailsFast/module-local
    --- PASS: TestAsmUppercaseSFailsFast/selection
```

plus the rebuilt oracle (`TestAsmSelectionMatchesGoBuildOracle`,
now comparing on every sampled dir incl. `runtime/cgo` against
`SFiles ∪ MatchFile`-matched `.S`/`.sx`) and the completeness test:

```
inventory complete: 639 files on disk == 639 inventory entries
runtime/cgo: 2 assembly files selected, oracle agrees
```

Inventory regenerated 627 → 639 (`gen-asm-inventory.py --check` green);
the 12 `.S` files are all `unsupported` (11 `runtime/cgo` `gcc_*.S` =
cgo gcc-assembly, 1 `cmd/cgo` test fixture).

Fidelity note (discovered during the fix, documented in `asm.go`):
go/build additionally gates `.S`/`.sx` on cgo (they land in `SFiles`
only when the package has cgo files, else `IgnoredOtherFiles`). The
loader is deliberately stricter -- graalgo has no cgo/gcc path at all
(SPEC §3.7 GG-0058) -- so any constraint-selected `.S`/`.sx` must
resolve or fail fast.

## P1-2 — vendored-tree probe was dead code

**RED (pre-fix tree):** `stdlib-excl validate` with no `--src` (the
exact `make check` gate invocation):

```
validate: OK: ... version 1, 2 entries, cap 13 (...), src /home/hatch/tools/go-1.27.1/src
```

and `stdlib-linkdiff` with no `--goroot`:

```
linkdiff goroot: /home/hatch/tools/go/src
```

Both silently measured the LIVE toolchain even though the vendored
tree existed at `~/workspace/graalgo-stdlib/go1.27.1`: the probe
looked for `<ver>/src`, but the vendor tool lays `$GOROOT/src`
contents FLAT at `<ver>/`.

**GREEN (fixed tree):**

```
$ stdlib-excl validate   # no --src
stdlib tree: using vendored tree /home/hatch/workspace/graalgo-stdlib/go1.27.1 (pin go1.27.1)
validate: OK: ... version 2, 2 entries, cap 13 (...), src /home/hatch/workspace/graalgo-stdlib/go1.27.1
```

`measure` with no `--src` reproduces the published measurement
exactly (399 dirs: 236 pure / 142 coupled / 21 not-linux-amd64, 274
test binaries) against the vendored tree; `linkdiff` with no
`--goroot` reports `goroot: .../graalgo-stdlib/go1.27.1` with
`skip_list_size: 525` (matches the published trial).

Unit red-green for the resolver (`tools/stdlib-excl`, all green
after the fix):

- `TestResolveStdlibTreePrefersVendored` -- vendored tree used when present
- `TestResolveStdlibTreeFallsBackLoud` -- no tree: GOROOT fallback WITH a loud stderr WARNING
- `TestResolveStdlibTreePinSkewFailsLoud` -- manifest `goVersion` != pin fails loud
- `TestResolveStdlibTreeRejectsNonTreeDir` -- same-named non-tree dir refused

## P2-1 — linkdiff never flagged unused/stale routes

**RED:** with the per-route hit counter neutered
(`routeHits[r.Target]++` removed), `TestStaleRoutesFlagged` fails --
the bogus route is no longer distinguished from the live one.

**GREEN:** restored; the test passes. End-to-end demo: a routes file
with a bogus entry produces

```
stdlib-linkdiff: WARNING: 1 route(s) matched zero linknames (stale or mistyped): bogus.gone
```

and the report carries `"stale_routes": ["bogus.gone"]`.

## P2-3 — `measure` emitted `"reasons": null` for pure dirs

**RED:** `TestMeasurePureReasonsMarshalAsEmptyArray` run against the
pre-fix `measure.go` (from git HEAD):

```
--- FAIL: TestMeasurePureReasonsMarshalAsEmptyArray (0.14s)
```

(nil slices marshal as `null`).

**GREEN:** fixed tree -- pure dirs marshal `"reasons": []` and
`"non_test_reasons": []`; the re-baselined
`coupled-packages-go1.27.1.json` contains zero `null` reasons.

## P2-8 — oracle test sample extended (covered by the P1-1 test commit)

`runtime/cgo` (cgo dir, exercises `gcc_amd64.S`) added to the oracle
sample; the old `continue`-on-fail-fast that skipped selection
comparison on unsupported dirs is gone -- selection agreement is now
verified on every sampled dir.

## Final battery (2026-10-07, post-remediation)

- `go test ./...` in `tools/stdlib-excl`: ok (21 tests + 5 new)
- `go test ./...` in `tools/stdlib-linkdiff`: ok (13 tests + 1 new)
- `go test ./...` in `tools/stdlib-vendor`: ok
- `go test -count=1 ./...` in `phases/phase3/src/p3frontend`: ok
- `go vet` clean in all four modules
- `make -C phases/phase3 test`: 58 tests, 0 failures
- `make -C phases/phase3 check`: clean (placeholder/debug gates clean;
  exclusion-list gate validates against the vendored tree)
