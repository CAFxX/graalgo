# graalgos01stub — stdlib packages with asm replaced by formulations

Mechanism: selected stdlib package trees are copied from GOROOT
(`~/tools/go`, go1.27.1) into this self-contained module. Every Plan-9 `.s`
file in the copied trees is DELETED and replaced by the spike's high-level
formulations:

| package | upstream asm | stub replacement |
|---|---|---|
| [internal/runtime/atomic/](internal/runtime/atomic/) | 13 `.s` files (amd64) | `form.go`: striped-channel-lock emulation of every op |
| [internal/bytealg/](internal/bytealg/) | SIMD kernels | `bytealg.go`: pure-Go formulations |
| [sync/atomic/](sync/atomic/) | `asm.s` (+`race.s`) trampolines | `wiring.go`: direct Go delegation to the atomic formulations |

All other imports still resolve to GOROOT (real runtime, testing, etc.).
[internal/abi/](internal/abi/) is reduced to `escape.go` (already pure Go upstream);
[internal/asan/](internal/asan/) to `noasan.go`. Test files are copied unmodified apart from
import-path rewrites into this module.

Run: `go test ./sync/atomic/ ./bytes/ ./strings/` (without -race: the race
build needs race.s trampolines into the race runtime, out of scope).
