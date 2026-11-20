# internal/backend/interop — v1 stdlib stand-ins (status: **v1, deleted at M3**)

**Do not build on this module.** Every package here is a hand-written Java
stand-in for a Go std package or runtime piece used by the v1 pipeline
([bridge-inventory.md](../../../docs/arch/bridge-inventory.md) B-8;
[toolexec-design.md](../../../docs/arch/toolexec-design.md) §9.1). The new
path does not load it: [graalgo-bundle-run](../graalgo-bundle-run) puts only
the truffle module on the classpath, and
[check_layering.py](../../../.github/scripts/check_layering.py) fails on any
`graalgo.go.*` edge into it. [check_no_bridges.py](../../../.github/scripts/check_no_bridges.py)
rejects additions.

Packages (`src/main/java/graalgo/`): `p3abi`, `p3builtin`, `p3chan`,
`p3crypto`, `p3link`, `p3log`, `p3math`, `p3net`, `p3os`, `p3reflect`,
`p3sync` (incl. `p3sync.launcher.P3SyncMain`, the v1 product launcher run by
[graalgo-sync-run](../graalgo-sync-run)), `p3time`, `p3tls`, `p3unsafe`,
`p3x509`, `iface`, `rtstack`, `rtmemstats`, plus a few `graalgo.frontend`
wire-kind registrars.

**New-path equivalent:** intrinsics in
[graalgo/go/intrinsics/](../truffle/src/main/java/graalgo/go/intrinsics/) +
upstream std source; see [docs/MAP.md](../../../docs/MAP.md).

**Test (v1 gates):** `make build test-java-fast snapshot-check`.
