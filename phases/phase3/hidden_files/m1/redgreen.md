# P3-M1 red-green-first evidence

Date: 2026-10-06. Both mutations were applied to the working tree, observed
red, then reverted; the full suite was re-run green afterwards
(`go test ./...`: 6 tests incl. 2 cycle subtests, all PASS).

## RED (a): flattener selector rewrite disabled

**Mutation:** in `flatten.go`, `rewriter.expr`'s `*ast.SelectorExpr` case —
the branch collapsing `pkg.X` to the renamed identifier was removed, so
package-qualified selectors were left as-is while the import specs were
still dropped.

**Result:** `TestTwoPackageDifferential` FAILS (fail-fast, not silent):

```
--- FAIL: TestTwoPackageDifferential (0.07s)
    export_test.go:173: ExportDir: p3frontend: gofrontend: gofrontend: type error:
    /tmp/p3frontend1598297099/p3merged.go:50:14: undefined: helper
```

The merged program references `helper.Add` with no import; the frozen
exporter rejects it. The pipeline refuses to emit a wrong program rather
than diverging from gc.

## RED (b): build-tag suffix filtering disabled

**Mutation:** in `loader.go`, `goodOSArchFile` was stubbed to always return
true (filename `_GOOS`/`_GOARCH` suffixes ignored).

**Result:** `TestBuildTagFiltering` FAILS against the `go/build` oracle:

```
--- FAIL: TestBuildTagFiltering (0.01s)
    loader_test.go:116: SelectFiles(/tmp/TestBuildTagFiltering648589201/001) != oracle:
         extra=[a_arm64.go a_darwin.go a_linux_arm64.go a_windows.go x_windows.go]
         missing=[]
```

The wrongly-included files are exactly the ones the suffix rules exclude
(including `x_windows.go`, the `//go:build linux`-tagged windows file).
Real stdlib dirs (`src/os`, `src/strings`) were not reached in this run —
the synthetic corpus already falsified the mutation, as intended.

## GREEN (after revert)

```
--- PASS: TestSyntheticBootstrapRetained (0.06s)
--- PASS: TestTwoPackageDifferential (13.75s)
--- PASS: TestNoLanguagePinning (0.03s)
--- PASS: TestDeterministic (0.03s)
--- PASS: TestBuildTagFiltering (0.07s)
--- PASS: TestImportCycle (0.00s)
PASS
ok      p3frontend      13.957s
```

`git status` after the revert shows only the intended new files; the two
mutations left no residue (verified via `git diff --stat` before commit).
