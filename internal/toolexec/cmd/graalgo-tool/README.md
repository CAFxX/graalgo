# graalgo-tool — the `-toolexec` wrapper (status: **new**)

`go build -toolexec graalgo-tool` makes cmd/go run every toolchain tool as
`graalgo-tool <tool> <args>`. For `compile` it runs the stock gc compile
(export data, build ID), type-checks the same files with go/types, emits
GIR ([../../emit/](../../emit/)) and appends it to the archive; for `link`
it resolves symbols ([../../link/](../../link/)) and writes
[graalgo-stub](../graalgo-stub/) + bundle as the executable. Other tools
pass through. Protocol: [toolexec-design.md](../../../../docs/arch/toolexec-design.md) §2.

**Not here:** the argv/response-file parsing ([../../argv/](../../argv/)),
the launcher that injects this wrapper ([../graalgo-launcher/](../graalgo-launcher/)).

**Test:** `cd internal/toolexec && go test ./cmd/graalgo-tool` (end-to-end
protocol tests: `e2e_test.go`, `link_e2e_test.go`, `launcher_e2e_test.go`).
Test scripts build it via [test/lib/toolexec.sh](../../../../test/lib/toolexec.sh).
