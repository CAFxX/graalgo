# P3-M6 asm strategy — red-green evidence (2026-10-07)

Mechanism: `phases/phase3/src/p3frontend/asm.go` (`ResolveAsmFile` /
`CheckAsmFiles`, hooked into `Loader.localRef` / `Loader.mainRef`);
inventory: `phases/phase3/stdlib-fork/asm-inventory-go1.27.1.{json,md}`;
tests: `phases/phase3/src/p3frontend/asm_test.go`.

## R1 — fail-fast is load-bearing (mutation)

Method (workspace lesson "copy first, verify the copy is clean"): copied
`src/p3frontend` to `/tmp/asm-mut/p3frontend`, grepped the copy for
mutation markers (none), then neutered ONLY the fail-fast:

```go
fail := func(category AsmCategory, reason string) (AsmResolution, error) {
    _ = reason
    return AsmResolution{File: abs, Package: importPath, Category: string(category)}, nil
}
```

i.e. unsupported/uncategorized files silently succeed. Result
(`go test -run TestAsm ./...` on the mutated copy):

```
--- FAIL: TestAsmCategoriesEndToEnd (0.09s)
--- FAIL: TestAsmUncategorizedFails (0.03s)
--- FAIL: TestAsmHookInLoader (0.01s)
FAIL    p3frontend  1.145s
```

with the failure lines:

```
asm_test.go:266: uncategorized .s file under scratch GOROOT: build proceeded silently
asm_test.go:279: module-local .s file: build proceeded silently
asm_test.go:345: dep with uncategorized .s: LoadClosure proceeded silently
```

The red condition is exactly the forbidden behavior: the build silently
succeeding. On the real tree the same tests are green (R2). Mutation
copy removed after the run (`/tmp/asm-mut` deleted).

## R2 — green on the real tree

`go test -count=1 ./...` in `phases/phase3/src/p3frontend`: **ok**
(full pre-existing suite + the 8 new `TestAsm*` tests). `go vet`
clean.

## R3 — oracle agreement (non-tautological selection)

`TestAsmSelectionMatchesGoBuildOracle` compares the .s buildability
filter against the independent `go/build` `ImportDir` SFiles on 12 real
stdlib dirs (runtime, internal/bytealg, internal/cpu, math, math/big,
sync/atomic, crypto/sha1, crypto/internal/fips140/aes, hash/crc32,
syscall, reflect, internal/abi): file-for-file agreement on every
replaced category; the three unsupported dirs fail fast as designed
(hash/crc32, syscall, reflect).

## R4 — completeness checker can fail

`TestAsmCompletenessCheckerDetectsFakeFile`: the same checker logic
that asserts inventory completeness on the real tree is run against a
scratch tree containing `fakepkg/sneaky_amd64.s`; it reports exactly
that file. (On the real tree `TestAsmInventoryComplete` asserts the
627 enumerated files == 627 inventory entries, no missing, no stale.)

## R5 — error contract

`assertAsmError` (used by the end-to-end, uncategorized, and hook
tests) asserts the fail-fast message contains all four of: the file
path, the package (quoted), the category (quoted, or "uncategorized"),
and the reason substring — plus the `asm fail-fast` marker.
