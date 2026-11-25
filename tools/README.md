# tools — developer tooling, not the product (status per entry)

| Path | Status | What |
|---|---|---|
| [req/](req/README.md) | shared | requirements CLI over [requirements/](../requirements/); `req lint` runs in `make check` |
| [mutation/](mutation/) | new | plants the new-path mutants of [mutants.json](mutation/mutants.json) on a scratch copy and requires `snapshot-check` and the m1a/m1r/m1c/m1d/m1w13 gates, narrowed to each mutant's kill programs (survivors re-run in full), to go red (`make mutation`, nightly; m3-plan D3) |
| [torture/](torture/) | shared | scheduler torture battery |
| [godebug-diff/](godebug-diff/) | new | emulated GODEBUG knobs vs gc (probes built through graalgo-tool, run on graalgo-bundle-run; m3-plan D3) |
| [stdlib-fork/](stdlib-fork/) | v1-era data | Phase-3 std inventories (asm, linkname, coupled packages), vendor manifest, `exclusions.yaml` (checked by `make check`); the new path's fork is [stdlib/](../stdlib/README.md) |
| [stdlib-vendor/](stdlib-vendor/), [stdlib-linkdiff/](stdlib-linkdiff/README.md), [stdlib-excl/](stdlib-excl/README.md) | v1-era | vendor a pinned GOROOT, linkname diff, exclusion-list validator |
| [gate1/](gate1/README.md) | **v1, deleted at M3** | Phase-3 `go test` trial harness |

m3-plan D3 re-pointed the v1-driven tools (`mutation`, `godebug-diff`) to
the new path and deleted the rest: `fuzz-soak/` (its generator lives on as
[test/fuzz/gen.py](../test/fuzz/gen.py)) and `run_stdlib_gate.sh` (now
`make stdlib-gate`); see
[ADR-0017](../docs/dev/adr/ADR-0017-repo-layout.md).

**Not here:** test gates ([test/](../test/)), CI scripts and repo gates
([.github/scripts/](../.github/scripts/)). Map: [docs/MAP.md](../docs/MAP.md).

**Test:** `req`, `mutation`, `stdlib-excl`, `stdlib-linkdiff`,
`stdlib-vendor` are separate Go modules: `cd tools/<name> && go test ./...`.
