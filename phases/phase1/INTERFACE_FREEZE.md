# graalgo Phase 1 — Interface Freeze (M9)

**Status:** FROZEN 2026-10-05 (M9, done-gate 3). **Version:** freeze v1.

This document is the M9 interface freeze for **both** Phase-1 components.
It supersedes the piecemeal freezes (`docs/freeze-stub.md` for the GG-0020
intrinsic surface, `docs/frontend-v1.md` for the wire format) by collecting
every cross-boundary API in one place with a single change policy. The older
documents remain normative for their details; where they conflict, this
document wins and the older one is amended.

## 1. Freeze policy

Every entry below is marked **FROZEN** or **INTERNAL**:

- **FROZEN** — part of the cross-component or cross-workstream contract.
  Changing a FROZEN signature, wire field, CLI flag, exit code, or builtin
  requires: (a) a new ADR, (b) a version bump of the owning artifact
  (`frontend/v1` → `v2`, freeze v1 → v2, or a new `toolVersion`), and
  (c) the machine check in §7 going red first (it must fail *before* the
  change lands, proving the check guards the surface). Silent drift is a
  GG-0020/GG-0021 review failure.
- **INTERNAL** — implementation detail of one component. Free to change;
  listed here only to bound the frozen surface (if it's not in §2–§6, it's
  INTERNAL).

**Components.** (A) `gofrontend` — the Go frontend (`phases/phase1/frontend/`):
type-checks one single-file Go program with the vendored `go/*` packages and
emits the frontend/v1 JSON document. (B) `graalgo-run` — the Java/Truffle
runtime (`phases/phase1/src/`): `NodeFactory` consumes the document, builds
the Truffle AST, executes `main`.

## 2. Component A — `gofrontend` (Go)

### 2.1 CLI contract — FROZEN

| Element | Frozen value |
|---|---|
| Invocation | `gofrontend <file.go>` (exactly one argument) |
| stdout | the frontend/v1 JSON document, single line + trailing newline |
| stderr | human-readable error prefixed `gofrontend: ` (build log; recorded verbatim by the diffharness) |
| Exit 0 | success |
| Exit 1 | usage error (wrong arity) |
| Exit 2 | frontend error: parse, type, or scope rejection |
| `toolVersion` | `"0.1.0"` (emitted as `generator.version`) |

The launcher resolves the binary via `ToolPaths.gofrontend()` (INTERNAL
resolution logic; the *contract above* is FROZEN).

### 2.2 Go API — FROZEN

- `func ExportFile(path string) (string, error)` (`frontend/export.go`) —
  the programmatic entry: type-checks `path`, returns the frontend/v1 JSON
  document. Signature frozen; behavior defined by `docs/frontend-v1.md`.
- `enumerateInstantiations` (export.go, INTERNAL implementation) emits one
  `funcdecl` per monomorphized instantiation; the dedup key format
  `F:<name>|<targs>` (functions) / `M:<recv>|<name>|<targs>` (methods) is
  FROZEN — the Java side keys its function table on it.

### 2.3 Wire format frontend/v1 — FROZEN

The JSON document defined by `docs/frontend-v1.md` is frozen at wire
version `"v1"`. Consequences:

- No field may be renamed, removed, or retyped; new fields must be optional
  (consumer ignores unknown fields — the forward-compatibility rule).
- `frontend: "v1"` — consumers reject any other value.
- Determinism: two runs on the same input are byte-identical
  (`TestDeterministic` in `frontend/export_test.go`).
- Machine check: `frontend/tools/validate_frontend_v1.py` enforces the
  document; it runs in `make check`.

## 3. Component B — `graalgo-run` (Java)

### 3.1 Launcher CLI — FROZEN

`graalgo.launcher.Main`:

| Element | Frozen value |
|---|---|
| Usage | `graalgo-run [--check] [--dump-vars] [--dump-polymorphism[=path]] file.go` |
| `--check` | frontend only (diffharness build step); no execution |
| `--dump-vars` | after executing, print `name=value` per frame slot (debug aid) |
| `--dump-polymorphism[=path]` | M9 (WS L): record call-site polymorphism during execution, then print the JSON snapshot (bare flag) or write it to `path` |
| Exit 0 | success (`func main` completed) |
| Exit 1 | usage error, unreadable input, gofrontend unlaunchable, launcher-internal error |
| Exit 2 | build failure (gofrontend rejected the program; stderr passed through verbatim) **or** uncaught Go panic / Goexit in main (gc: exit 2) |

### 3.2 `NodeFactory` — FROZEN

`graalgo.frontend.NodeFactory`:

- `public NodeFactory(Source source)` — entry point; `source` carries the
  program text for source sections.
- `public Program buildProgram(String jsonText)` — parses a frontend/v1
  document, builds the Truffle AST. Throws `FrontendException` on
  factory-side rejection (never the program's fault).
- `public record Program(GoRootNode root, FrameDescriptor descriptor,
  Map<String,Integer> slotsByName, Map<String,GoKind> kindsByName)` —
  the built program (component names frozen; record accessors frozen).
- `registerExprKind(String, ExprBuilder)`, `registerStmtKind(String,
  StmtBuilder)`, `registerTypeDecoder(TypeDecoder)` — the WS C/D
  extension hooks (frozen signatures; the *set of registered kinds* is
  governed by the builtin registry, §5).
- `public static GoTypeDescriptor basicDescriptorFor(GoKind)`,
  `public static boolean isPredeclaredBasic(GoTypeDescriptor)`.

### 3.3 Node base classes — FROZEN

`graalgo.frontend.GoExprNode` / `GoStmtNode`: the `execute{Long,Int,Float,
Double,Boolean,Object}(VirtualFrame)` protocol, `kind()`, `typeId()`,
`setSourceSection`/`getSourceSection`. All node implementations extend
these; the protocol is frozen, individual nodes are INTERNAL.

`graalgo.frontend.FuncValueCallNode(GoExprNode fn, GoExprNode[] args,
GoKind resultKind)` and the `(…, boolean isMethodCall)` variant — the
single call-execution choke point (M9 probes attach here).

### 3.4 Runtime — FROZEN (intrinsic surface)

The GG-0020 intrinsic surface is frozen in `docs/freeze-stub.md`
(§"GG-0020 intrinsic surface, freeze v1"): `Intrinsics` (memory, slices,
maps, itab, random, traceback), `BuiltinPrint.print/println`,
`Fmt.sprintf/sprint/sprintln/print/printf/println/errorf`, `Timers`,
`Scheduler.spawn/yield/park`, `Netpoll.init/open/close`,
`ChanOps.send/recv/close`, `Panic.throwPanic/recover`,
`safepoint.Safepoint.poll()`. Additionally frozen at M9:

- `graalgo.runtime.GoCalls.invoke(GoFunc fn, Object[] args) -> Object`
  — the single function-invocation entry (nil check, recover-flag
  discipline documented on `FuncValueCallNode`).
- `graalgo.runtime.GoCalls.nilFuncCall() -> GoErrorValue` — the nil-function
  call error value (currently uncalled in-tree; frozen to prevent silent
  removal).
- `graalgo.runtime.Runtime.println(String)` — WS G `println` intrinsic.

### 3.5 Values — FROZEN (cross-boundary types)

The value representations cross the frontend↔runtime boundary (frame slots,
boxing, descriptors) and are frozen as *types* (constructors and accessors
listed; method bodies INTERNAL):

- `GoKind` (enum), `GoSlice`, `GoMap`, `GoString`, `GoStruct` +
  `GoStructDescriptor`, `GoPointer`, `GoInterface` (data carrier:
  `descriptor()/data()/itab()`), `GoFunc` (`target()/closureFrame()/
  boxCells()`), `GoComplex32/64`, `GoBox`, `GoTypeDescriptor`,
  `GoConvert`, `GoEquality`.
- `CallSiteProbes` (M9, WS L): `setEnabled(boolean)`, `isEnabled()`,
  `registerSite(String)->Site`, `snapshotJson()->String`, `reset()`,
  `receiverKey(Object)->Object`; `Site.recordTarget(Object)`,
  `Site.recordReceiver(Object)`.
- `InterfaceDispatchProbes` — future interface-dispatch hooks (no dispatch
  implementation in Phase 1): `setEnabled(boolean)`, `isEnabled()`,
  `registerSite(String)->Site`, `reset()`, `sites()->Map<String,Site>`.

## 4. Builtin registry — FROZEN (the WS D/G boundary)

`NodeFactory.buildBuiltinCall` accepts exactly these 16 builtins; any other
builtin name is a scope error naming WS G (intrinsic surface) or WS D:

`panic`, `recover`, `print`, `println`, `len`, `cap`, `make`, `new`,
`append`, `copy`, `delete`, `complex`, `real`, `imag`, `slice`, `map`.

Adding a builtin requires the same process as changing a FROZEN signature
(§1). The `fmt` package surface (`Sprintf` etc.) is resolved only via the
Phase-1 import overlay (D4), not as builtins.

## 5. Versioning

| Artifact | Current | Bump rule |
|---|---|---|
| frontend/v1 wire format | `v1` | new field renames/removals → `v2` |
| `gofrontend` toolVersion | `0.1.0` | any CLI/JSON behavior change |
| Interface freeze | v1 (this document) | any FROZEN change → v2 + ADR |

## 6. Machine checks

| Check | Guards |
|---|---|
| `FreezeSignatureTest` (suite) | FROZEN Java signatures (§3.2–3.5) — fails on rename/retype/removal |
| `frontend/tools/validate_frontend_v1.py` (`make check`) | frontend/v1 wire format (§2.3) |
| `TestDeterministic` (`frontend/export_test.go`) | wire determinism (§2.3) |
| diffharness build-failure parity | gofrontend exit codes (§2.1), graalgo-run exit codes (§3.1) |

## 7. Done-gate 3

Gate 3 is met when: this document is published, all §6 checks are green,
the polymorphism measurements (§8 of the M9 report) are recorded, and the
M9 review gate (≥2 independent reviews, all findings remediated) is closed.

## 8. INTERNAL (explicitly not frozen)

Individual node implementations (`*Node.java` bodies), `NodeFactory`
private builders, `Fmt`/`PanicRendering` internals, `DeferredCall`/
`DeferChain` internals, `Intrinsics` STUB bodies (the *signatures* are
frozen, the Phase-2 implementations are not), test-only code, and everything
under `tools/` (harness implementation; the *corpus expectations* are
versioned separately).
