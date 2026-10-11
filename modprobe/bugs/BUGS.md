# Module-probe bug log (lane: m9-modprobe, worktree `wt/graalgo-p3/m9-modprobe`)

Every failure from a module probe gets a BUG entry with a minimal reproducer.
Fixed-by-general-fix entries name the commit. Blocked-on-other-lane entries
name the owner. No module source is ever patched (hard rule 1); every fix is
general, never module-specific (hard rule 2).

## BUG-MODPROBE-001 — third-party imports rejected by the frontend (2026-10-11)
- **Status:** FIXED (general) — commit pending.
- **Symptom:** any program importing a published module (e.g.
  `github.com/google/uuid`) failed at export with
  `gofrontend: import "github.com/google/uuid": imports are not supported in
  frontend/v1 scope (WS G import overlay)` (minimal reproducer:
  `modprobe/uuid/case01/main.go` before the fix).
- **Root cause:** `overlayImporter.Import` resolved only the fixed synthetic
  package list; the ExportFile gate, the importer-selection loop, and the
  needX switch all assumed third-party paths were out of scope.
- **Fix (general, WS G as always intended):** new
  `phases/phase1/frontend/diskimport.go` — third-party import paths resolve
  from local disk ($GOMODCACHE/<path>@<highest version>, then GOPATH/src);
  files selected constraint-aware via go/build (GOOS/GOARCH build tags);
  type-checked with the same overlayImporter (recursive imports work);
  merged through the exact syntheticPackage machinery (per-file source
  tracking for multi-file packages: `srcForFile`/`srcForDecl`, `file` on
  funcDeclInfo/synthVarSpec). Transitive closure imported dependencies-first
  (post-order DFS) via the new `importPkgTree`; the three import gates in
  export.go gained a disk fallback (`isDiskImportable`).

## BUG-MODPROBE-002 — stdlib outside the synthetic overlay unresolvable (2026-10-11)
- **Status:** FIXED (general) — commit pending.
- **Symptom:** `gofrontend: ... could not import database/sql/driver
  (import "database/sql/driver": imports are not supported in frontend/v1
  scope (WS G import overlay))` when exporting a program importing
  `github.com/google/uuid` (its `null.go` needs `database/sql/driver`).
  Minimal reproducer: `modprobe/uuid/case01/main.go` before the fix.
- **Root cause:** only synthetic-overlay and third-party paths resolved;
  plain stdlib packages with no synthetic equivalent had no source.
- **Fix (general):** `resolveDiskPackageDir` falls back to
  `GOROOT/src/<path>` (via `runtime.GOROOT()` — the toolchain that built
  gofrontend, matching the gc leg). Synthetic-overlay paths never reach
  the fallback (the importer's cases claim them first), so no shadowing.
  `database/sql/driver` + its `database/sql/internal` dep now resolve.

## BUG-MODPROBE-003 — synthetic net lacks Interface/Interfaces (2026-10-11)
- **Status:** FIXED (general) — commit pending.
- **Symptom:** `gofrontend: ... node_net.go:11:22: undefined:
  net.Interface` when exporting a program importing
  `github.com/google/uuid`. Minimal reproducer:
  `modprobe/uuid/case01/main.go` before the fix.
- **Root cause:** the synthetic `net` package had no `Interface` type or
  `Interfaces()` function (uuid's v1 node-ID path needs them).
- **Fix (general):** `net.Interface` struct (Index/MTU/Name/HardwareAddr/
  Flags) + `net.Flags` constants + `Interfaces()` added to the synthetic
  net package, backed by a new `graalgo.net.interfaces` host trap
  (`GoNet.interfaces`, registered in `LinknameIntrinsics`) enumerating
  `java.net.NetworkInterface` and marshaling via the established
  int64/[]byte trap ABI (LE-int32 blob; -ENOMEM grow-and-retry). This is
  also the "interface listing" `InterfaceAddrs`'s note was waiting for
  (left for a follow-up; out of this probe's scope).

## (further entries appended as the probe runs)

## BUG-MODPROBE-005: Disk-package multi-result transformation does not handle interface method specs

**Date:** 2026-10-11  
**Status:** Open, blocking uuid probe  
**Owner:** MODULE-PROBE lane (general frontend fix)

**Symptom:**  
`database/sql/driver` (imported by `github.com/google/uuid` via `null.go`) defines interfaces with multi-result methods:
```go
type ValueConverter interface {
    ConvertValue(v any) (Value, error)  // 2 results
}
type Valuer interface {
    Value() (Value, error)  // 2 results
}
```
The frontend fails with: `interface method ConvertValue has 2 results; at most one result is supported in frontend/v1 scope (WS D)`.

**Reproducer:**  
Compile any package importing `database/sql/driver` via the disk importer:
```bash
/tmp/gofrontend-test modprobe/uuid/case01/main.go
# fails at database/sql/driver/types.go ValueConverter interface
```

**Root cause:**  
The `transformDiskMultiResult` mechanism (mr_disk.go) transforms concrete `FuncDecl`s with multi-result signatures to pair-returning helpers, but does not transform `*ast.InterfaceType` method specs. Interface method specs with >1 result are rejected by the v1 frontend's WS D gate.

**Fix required (general):**  
Extend the disk-package transformation to:
1. Rewrite interface method specs with multi-result to return a shared pair type (e.g., `__ValueConverter_ConvertValuePair`).
2. Ensure all implementations of the interface method return the SAME pair type (not per-receiver types).
3. Extend the export-time desugar (`classifyDiskMultiResultCall` / interface dispatch) to handle calls through the transformed interface.

This is a general WS D gap for disk-imported packages, not uuid-specific.

**Workaround:** None (module compiles as-published; cannot skip `null.go` or `database/sql/driver`).

**Blocks:** uuid probe case01..case08 (all require `github.com/google/uuid` which imports `database/sql/driver` via `null.go`).
