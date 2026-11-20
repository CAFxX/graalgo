# graalgo-launcher — the `graalgo` command of the new path (status: **new**)

Installed as `GRAALGO_HOME/bin/graalgo`. Execs the stock `go` command of
graalgo's GOROOT with [graalgo-tool](../graalgo-tool/) injected as
`-toolexec`; it reimplements no go command. Replaces the v1 driver
[cmd/graalgo/](../../../../cmd/graalgo/) at the M3 cutover.
Design: [toolexec-design.md](../../../../docs/arch/toolexec-design.md) §1;
temporary deviations: [ADR-0010](../../../../docs/dev/adr/ADR-0010-launcher-deviations.md).

**Not here:** environment/argv composition ([../../launcher/](../../launcher/)).

**Test:** `cd internal/toolexec && go test ./cmd/graalgo-launcher ./launcher`.
