# stdlib-excl

P3-M6 workstreams 4+5: the stdlib exclusion-list validator and the
coupled-package re-measurement tool (GG-0009, GG-0033 machinery).

No external dependencies. The YAML reader is a minimal line parser with the
same semantics as `tools/diffharness` `LoadExclusions` and the Java
`readExclusions` in `TypeparamDifferentialTest` — those readers parse this
tool's `exclusions.yaml` files unchanged (the `reason_codes:` vocabulary
section is invisible to them: `reason_codes:` does not carry the `reason:`
prefix and `- code:` does not carry the `- file:` prefix).

## validate

```
stdlib-excl validate --list phases/phase3/stdlib-fork/exclusions.yaml \
    [--src DIR] [--max N] [--test-binaries N] [--baseline PATH]
```

Enforces the exclusion-list contract; rejects (exit 1) when:

1. the file has no `version:` line, or no `reason_codes:` vocabulary;
2. an entry lacks `reason:` (mandatory), `detail:` (v1 no-parking rule: every
   entry carries evidence), or `file:`;
3. an entry's reason is not in the file's `reason_codes:` vocabulary;
4. a `file:` value is duplicated;
5. the entry count exceeds the review cap: `--max` (default 25) **or** 5% of
   stdlib test binaries, whichever binds first. `--test-binaries` overrides
   the count (default: counted from `--src` as population dirs containing
   `_test.go`; on go1.27.1 that's 274, so the 5% clause binds at 13);
6. the entry set or vocabulary changed without a version bump — compared
   against `--baseline`, defaulting to the git HEAD version of the list
   (a file new in the working tree skips this check with a warning);
7. an entry's package path is not a directory in the stdlib tree — a stale
   entry.

`--src` default: the vendored tree when the vendoring worker has landed
(`phases/phase3/stdlib-fork/pinned-version.txt` + the target dir exist),
else `GOROOT/src` of the pinned toolchain (`~/tools/go/bin/go`).

Wired into `make -C phases/phase3 check` (stdlib exclusion-list gate).

## measure

```
stdlib-excl measure --src DIR --out report.json [--go-version V]
```

Walks the stdlib source root, applies the strict pure-Go check per package
dir for GOOS=linux GOARCH=amd64 (definition in
`phases/phase3/stdlib-fork/coupled-packages-go1.27.1.md` §1), and writes a
versioned JSON report: per-dir class (`pure` / `coupled` / `not-linux-amd64`)
with reasons (`asm`, `cgo`, `linkname`, `build-tags`), the non-test-only
secondary metric, counts, and the test-binary count. `--go-version`
defaults to `go version`.

Drift helpers (exported for tests): `DriftPct(spec, measured)` and
`DriftReopensBoundary(specCoupled, measuredCoupled)` — the 15% P3-D1
re-test trigger, strict (exactly 15% does not fire).

## Tests

`GOTOOLCHAIN=local go test ./...` — fixture trees cover every coupled
reason, the cap, the version-stale rule, and both sides of the 15% drift
boundary. Red-green evidence: each test asserts the reject/accept outcome,
so removing the corresponding check turns it red (mutation runs recorded
in the P3-M6 workstream report).
