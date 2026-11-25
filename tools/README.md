# tools — developer tooling, not the product (status per entry)

| Path | Status | What |
|---|---|---|
| [req/](req/README.md) | shared | requirements CLI over [requirements/](../requirements/); `req lint` runs in `make check` |
| [mutation/](mutation/) | v1-driven | mutates graalgo, requires the snapshot gate to go red (`make mutation`, nightly) |
| [fuzz-soak/](fuzz-soak/) | v1-driven | program-generator fuzzing via `graalgo-sync-run` (nightly soak; v1's `tools/fuzz` was deleted, m3-plan D0); the new path's fuzzer is [test/fuzz/](../test/fuzz/README.md) |
| [torture/](torture/) | shared | scheduler torture battery |
| [godebug-diff/](godebug-diff/) | v1-driven | emulated GODEBUG knobs vs gc (runs v1 `SchedMain`) |
| [stdlib-fork/](stdlib-fork/) | v1-era data | Phase-3 std inventories (asm, linkname, coupled packages), vendor manifest, `exclusions.yaml` (checked by `make check`); the new path's fork is [stdlib/](../stdlib/README.md) |
| [stdlib-vendor/](stdlib-vendor/), [stdlib-linkdiff/](stdlib-linkdiff/README.md), [stdlib-excl/](stdlib-excl/README.md) | v1-era | vendor a pinned GOROOT, linkname diff, exclusion-list validator |
| [run_stdlib_gate.sh](run_stdlib_gate.sh) | v1-driven | layer-3 wave-corpus gate (manual `stdlib-gate.yml`) |
| [gate1/](gate1/README.md) | **v1, deleted at M3** | Phase-3 `go test` trial harness |

"v1-driven" = runs programs through the v1 launchers; such a tool must be
re-pointed or deleted at M3 (not yet in
[toolexec-design.md §9.1](../docs/arch/toolexec-design.md); see
[ADR-0017](../docs/dev/adr/ADR-0017-repo-layout.md)).

**Not here:** test gates ([test/](../test/)), CI scripts and repo gates
([.github/scripts/](../.github/scripts/)). Map: [docs/MAP.md](../docs/MAP.md).

**Test:** `req`, `fuzz`, `mutation`, `stdlib-excl`, `stdlib-linkdiff`,
`stdlib-vendor` are separate Go modules: `cd tools/<name> && go test ./...`.
