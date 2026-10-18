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
  `internal/frontend/diskimport.go` — third-party import paths resolve
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

**Resolution (2026-10-11):** FIXED (general) — commit pending.
- `transformDiskMultiResult` (mr_disk.go) now also processes `*ast.InterfaceType` declarations: for each method with >1 result, generates a pair type `__<Iface>_<Method>Pair` (fields f1..fN) and registers a spec with `recvType` = interface name, `helper` = "" (no body). The interface declaration itself is unchanged (synthetic `io.Reader` precedent).
- New `classifyDiskIfaceMultiResultCall` (mr_disk.go), consulted in `exportIfaceMethodCall` after the two static classifiers: matches the interface method's declaring interface name against the dynamic diskMRSpecs table, returns `mrPlan{kind: mrIfaceMethod}` with the interface pair type. The itab dispatches to each concrete type's `__`-helper at runtime; field extraction is by name.
- `multiResultHelperFor` (mrdesugar_m5.go) gained a disk fallback consulting `lookupDiskMR`, so `methodTableEntries` maps concrete disk methods to their `__`-helpers for itab construction. `multiResultHelperFunc` gained a disk branch for helper lookup via `getDiskPackage`.
- `classifyDiskMultiResultCall` skips interface specs (`helper == ""`) so concrete-position desugar never matches them.
- Also fixed a pre-existing ident-reuse bug in `rewriteReturn`: the package-func branch reused the `__mr_tmp` definition-site ident in the use position; now uses fresh idents (like the method branch).

## BUG-MODPROBE-006: Multi-result calls nested in argument position not hoisted

**Date:** 2026-10-11  
**Status:** Open (fix in progress)  
**Owner:** MODULE-PROBE lane (general frontend fix)

**Symptom:**  
`gofrontend: call to unknown function NewRandom` when exporting `github.com/google/uuid` (`version4.go`: `return Must(NewRandom())`).

**Root cause:**  
`transformDiskMultiResult` only rewrote bodies of multi-result FuncDecls and var initializers. A multi-result call nested as an argument to another call (e.g., `Must(NewRandom())` inside single-result `New()`) reached export as a call to the kept-for-resolution stub.

**Fix (general):** new `hoistNestedMRInBlock`/`hoistNestedMRInStmt`/`hoistNestedMRExpr` pass at the end of `transformDiskMultiResult`: for every FuncDecl body, hoist direct package-level MR calls in argument position to `__mr_arg_N := __F(args)` temps and splice the pair fields. Handles Return/Assign/Expr/If/For/Range/Switch/Defer/Go statements and chained calls. (Selector/method MR calls nested in args are a separate case, filed if encountered.)

**Reproducer:** `modprobe/uuid/case01/main.go` (via `uuid.New()` → `Must(NewRandom())`).

## BUG-MODPROBE-007: Disk-package qualified const/var access not exported

**Date:** 2026-10-11  
**Status:** Open (fix in progress)  
**Owner:** MODULE-PROBE lane (general frontend fix)

**Symptom:**  
`gofrontend: identifier "uuid" denotes *types.PkgName` when exporting `uuid.RFC4122` (case05).

**Root cause:**  
`exportSelector` has a hardcoded list of synthetic package paths for qualified const/var access. Disk-built packages (e.g., `github.com/google/uuid`) are not in the list, so they fall through to `exportExpr(x.X)` which fails on the PkgName.

**Fix (general):** after the hardcoded list, check `getDiskPackage(p)`; if it resolves, use `exportSyntheticQualified` (disk packages are in syntheticPackage shape).

## BUG-MODPROBE-008: statement-position multi-result call on unlisted interface method emits invalid assign (uuid probe 0/8)

**Date:** 2026-10-18
**Status:** BLOCKED on `wt/graalgo-independent-codebase-review/fix-p0-ss-readrune` (fix written, awaiting independent review + integration)
**Owner:** coordinator (review + integrate existing branch); no new fix work needed

**Symptom:**
Full end-to-end proof (`make build` exit=0, `make generate` exit=0, then
`test/e2e/run_module.sh test/e2e/uuid`, all 8 cases, gc leg vs graalgo legs
at `--gmp=1` and `--gmp=4`): **0 passed, 8 failed**. Every graalgo leg dies
at export with:
```
graalgo-sync-run: internal error building AST: assign.fields: lhs/rhs length mismatch
```
(exit 1; empty stdout). All gc legs pass. Per-case table in the proof log:
`FAIL case01..case07 [stdout@1; exit1(1!=0); stdout@4; exit4(1!=0);]`
(`case07` gr4 leg hung — see BUG-MODPROBE-009, killed → exit 143;
`case08` is the panic-path case: gc exit=2, graalgo exit=1, stderr first
lines also differ).

**Root cause (verified against the wire, not inferred):**
`gofrontend` exits 0 on the probe but emits exactly one malformed `assign`
node in the whole 182 MB export (found by scanning the wire JSON for
`len(lhs) != len(rhs)` outside the comma-ok shape): a 3-lhs / 1-rhs assign
whose rhs is an `ifacemethod` call — `r, size, err = s.rs.ReadRune()` at
`fmt/scan.go:189` (disk-imported `fmt`; `s.rs` is `io.RuneScanner`).
`NodeFactory.buildAssign` (`internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java:4525`)
rejects it. The statement-position classifier (`classifyMultiResultCall`,
`internal/frontend/mrdesugar_m5.go:1518`) misses this call shape: the
interface arm only tries the `m8IfaceMultiResultMethods` whitelist (no
`ReadRune` entry) and the disk-iface classifier, with no general fallback
on master — so the raw node reaches the backend. This is the known
DESIGN-RV-P0-ss-readrune defect (`hidden_files/codebase-review/DESIGN-RV-P0-ss-readrune.md`).

**Reproducer:** `test/e2e/uuid/case01/main.go` (any probe case; `fmt` is in
every case's closure). Module sources unmodified (hard rule 1).

**Fix (general, already written — do NOT re-fix):**
branch `wt/graalgo-independent-codebase-review/fix-p0-ss-readrune`, tip
`0c966680` "Fix statement-position multi-result desugar for unlisted
interface methods": adds the general fallback `classifyUserIfaceMultiResultCall`
(synthesize the interface pair on first use; itab dispatches to the concrete
type's pair-returning `__`-helper). The branch is based on master `56a3dd68`
and is also pushed to origin. What remains is independent review + integration
— coordinator territory.

**Diagnostics gap (observation for the fixing lane):** the frontend emits the
invalid node and exits 0; the backend error carries no source position, so the
failure was only localizable by scanning the wire. A frontend-side arity
validation (or position info on `FrontendException`) would have pointed at
`fmt/scan.go:189` immediately.

**Blocks:** uuid probe case01..case08 (whole corpus); the milestone's
"published module end-to-end" criterion.

## BUG-MODPROBE-009: graalgo-sync-run --gmp=4 hangs intermittently with no output

**Date:** 2026-10-18
**Status:** OPEN, needs diagnosis (mechanism unverified — out of scope for the e2e lane)
**Owner:** fixing lane (general launcher/runtime fix)

**Symptom:**
During the same proof run, two of the eight `--gmp=4` legs hung: the JVM
started, printed nothing (stdout and stderr both 0 bytes), consumed ~0 CPU
(00:00:00–00:00:06 over 1.5–9 min), sat with ~11–19 threads in futex wait,
spawned no `gofrontend` child, and never exited. Observed:
- `case07` gr4 leg: hung ~9 min, killed by the lane (SIGTERM) → leg exit 143.
- `case08` gr4 leg: hung, SIGTERM ignored, SIGKILL required.
The other six `--gmp=4` legs completed normally (export error + exit 1, same
as the `--gmp=1` legs). All eight `--gmp=1` legs completed normally.

**Correlation (not root cause):** only `--gmp=4` legs hung; both hangs
occurred while box load was elevated (11.66/9.47/10.91, other lanes'
runners active). Earlier `--gmp=4` legs under lower load exited fine —
intermittent, possibly a scheduler/carrier startup race under load. The
mechanism was not diagnosed (out of scope for this lane).

**Impact on the probe:** `run_module.sh` has no per-leg timeout, so a hung
leg stalls the whole corpus indefinitely; the lane killed the JVMs manually
to complete the proof. The recorded `exit4(143!=0)` rows reflect the kills,
not launcher behavior.

**Reproducer (intermittent):**
`internal/backend/graalgo-sync-run --gmp=4 test/e2e/uuid/case07/main.go`
under concurrent box load. Prerequisites: `make build`, `make generate`.

**Fix required (general):** diagnose the `--gmp=4` startup hang (launcher
init vs scheduler carrier bring-up) and make the launcher exit promptly on
all failure paths; consider a per-leg timeout in `run_module.sh` as defense
in depth (separate runner-hardening change, not a substitute for the
launcher fix).
