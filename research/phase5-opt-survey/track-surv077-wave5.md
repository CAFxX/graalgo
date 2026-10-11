# SURV-077 mechanism question — wave-5 worker report

**Question (from wave-4 synthesis):** SURV-077 ports Graal GR-79590 ("speculative store
checks": remove array store checks by speculating the array's *declared type* is exact;
default-on, `-Djdk.graal.SpeculativeStoreCheck=false`; resweep note
`[track-resweep-wave4.md](track-resweep-wave4.md):48-80`) to Go. But Go has no array covariance — Java's store
check exists because `Object[]` may dynamically hold `String[]` — so the direct analog
may be vacuous. Settle it: enumerate candidate Go checks, vet each, KILL or CONFIRM.

**Method.** Grounded in Go 1.27.1 compiler source (`~/tools/go-1.27.1/src/cmd/compile/...`,
`src/runtime/iface.go`) and in disassembly of a compiled `[]any` store (`go tool compile
-S`, reproduced below). GR-79590 mechanism taken from the wave-4 resweep (compiler
CHANGELOG fetched 2026-10-04); Graal internals not re-fetched — not load-bearing, since
the verdict turns on the Go side. golang/go issues not exhaustively searched — verdict is
structural (from compiler source + disassembly), not precedent-dependent.

**The pattern's requirements** (what GR-79590 needs to be non-vacuous):
(R1) a runtime check emitted at a hot site;
(R2) the check verifies a property *implied by the declared type* (so guarding the
     declaration is cheaper than the check);
(R3) the guard is strictly cheaper than the check it replaces;
(R4) gc doesn't already remove the check where removable (no duplication of gc machinery).

---

## 1. The direct analog is vacuous — disassembly proof

Go spec assignability: `[]string` is **not** assignable to `[]any` (no covariance, unlike
Java where `String[]` is assignable to `Object[]`). Every slice/array store's element
type is therefore *always* exact — there is no dynamic property for a store check to
verify, and gc emits none. The wave-4 raw-candidate note ("Go's `any` slices are the
exact analog of Java's `Object[]`-declared-exact arrays", `[track-resweep-wave4.md](track-resweep-wave4.md):70-80`)
is **wrong**: the analogy fails at covariance, not at exactness.

Empirical confirmation — `go tool compile -S` of:

```go
func storeAny(s []any, i int, v string) { s[i] = v }
```

emits, in order:
1. `CALL runtime.convTstring(SB)` — boxes `string`→`any`. No check (always valid).
2. `CMPQ CX, DX; JCC panic` — the **bounds check** (`i < len(s)`). Length property, not type.
3. `MOVQ type:string(SB)→(BX)(CX)` — store of the type word. No check.
4. `CMPL runtime.writeBarrier(SB), $0; JEQ skip` — the **write-barrier enabled** check
   (global GC-phase flag; see §2 candidate C7).
5. `CALL runtime.gcWriteBarrier2(SB)` — barrier buffering.

**No type/store check exists at any slice or array store in Go.** (R1 fails for the direct
analog.) The only per-store checks are bounds (C6) and the write-barrier flag (C7).

---

## 2. Candidate enumeration and vetting

### C1 — Slice/array element stores `s[i] = v` (the direct analog)
- (a) SSA: `OpStore` (+ separate `OpIsInBounds` for the index). No type-check opcode exists
  on the store path. Disassembly above confirms.
- (b) Hot: yes, the hottest store shape in Go programs — irrelevant, since no check.
- (c) gc overlap: n/a — nothing to remove.
- (d) Declared-type property: the declared element type is always exact (no covariance);
  there is nothing to guard. The GR-79590 speculation ("dynamic type == declared type")
  is a tautology in Go.
- **Verdict: KILL — vacuous.** (R1 fails; no check exists.)

### C2 — Concrete→interface boxing at stores (`s[i] = v` into `[]any`, `x.f = v` into `any` field)
- (a) SSA: `OpIMake` (+ possible alloc); `runtime.convT*` calls. Boxing is data
  representation, always type-valid — no check opcode.
- (b) Hot: yes (`[]any` temporaries, `fmt` args).
- (c) gc overlap: n/a.
- (d) No declared-type property to guard; the store side never fails. The *re-assertion*
  side is C3.
- **Verdict: KILL — vacuous at the store.** (R1 fails.)

### C3 — Type assertion to concrete type `x.(T)` / `x, ok := x.(T)`
- (a) SSA: `ssagen/ssa.go dottype1` — inlined as **one pointer compare**
  (`OpEqPtr`: interface type word vs. target type/itab constant) + branch to panic/ok-path
  (`ssa.go:6505+`, "type assertion inlined"). The precomputed target itab
  (`reflectdata.ITabAddrAt`, `walk/expr.go walkDotType`) is a link-time constant.
- (b) Hot: yes — textbook hot paths are type switches/assertions in `encoding/json`
  decode and `fmt` printing (commonly observed; not load-bearing for this verdict).
- (c) gc overlap: none — gc already minimal (single compare); nothing left to remove.
- (d) No declared-type property: the declared type of `x` is the interface itself; the
  property being checked *is* the dynamic type. Any guard ("dynamic type is T") **is**
  the check — one pointer compare either way.
- **Verdict: KILL — guard is the check.** (R2/R3 fail; nothing cheaper to guard.)

### C4 — Interface→interface assertion/conversion `x.(I2)` (getitab path)
- (a) SSA: `dottype1` interface-target path — nil check, then an inline cache probe
  (atomic load of descriptor cache, hash&mask probe loop, `ssa.go:6418+`), falling back to
  `runtime.typeAssert` → `runtime.getitab` (`src/runtime/iface.go:44`). `getitab` itself
  has a lock-free fast path (atomic load of `itabTable` + quadratic probe) before the
  locked slow path. Real per-site cost on the inline path: roughly an atomic load + a few
  dependent loads + branches (order ~10–20 cycles on hit; author's estimate, not measured).
- (b) Hot: moderate — `fmt`'s per-operand `Formatter`/`Stringer` checks (`any`→non-empty
  interface), `errors.As` traversal, error→narrow-interface narrowing in servers.
- (c) gc overlap: none — gc's inline cache is the existing mitigation; residual cost is
  the probe, which gc cannot remove.
- (d) **Pattern mismatch.** A cheaper guard exists in principle — "dynamic type of `x`
  is exactly T0" (one type-word compare) → load precomputed constant itab(T0, I2),
  deopt/fallback on mismatch — but T0 comes from *profile*, not from the declaration.
  The declared (source-interface) type implies nothing about the dynamic type. This is
  **profile-guided concrete-type speculation**, i.e. the devirtualization family
  (SURV-004 monomorphic → SURV-070 skewed), not declared-type speculation.
- **Verdict: KILL for SURV-077 — no declared-type property to guard** (R2 fails).
  Recorded below as a raw candidate for the devirt lane (no SURV-ID assigned).

### C5 — Type switches `switch x.(type)`
- (a) `walk/switch.go walkSwitchType`: gc lowers to **hash dispatch** (`h := e._type.hash`,
  jump table on type hash) — already O(1), not a linear itab-compare chain.
- (b) Hot: yes (`encoding/json`, `fmt`).
- (c) gc overlap: the dispatch is already minimal.
- (d) Dominant-case speculation would again be profile-guided (devirt family), not
  declared-type-guided.
- **Verdict: KILL for SURV-077** (R2 fails; same family as C4).

### C6 — Bounds checks (`OpIsInBounds` / `OpIsSliceInBounds`) at hot stores
- (a) SSA: explicit check ops; elimination in `ssa/prove.go` (relation-based proof over
  dominating comparisons, incl. loop induction) + `ssa/loopbce.go`.
- (b) Hot: yes — every unproven index.
- (c) **gc overlap is the kill reason alongside the pattern mismatch.** gc's prove pass
  removes every statically-provable check; the residuals are indices gc cannot prove
  (from maps, channels, calls, memory). For those, any guard ("`i` in bounds") **is**
  the bounds check — identical cost. Hoisting the check (`len(s) >= K` once) doesn't
  remove the per-iteration compare unless the index is also bounded, which is exactly
  what prove.go already exploits when provable.
- (d) Length property, not a declared-*type* property. Speculative-BCE would be
  length speculation (adjacent to SURV-017 capacity speculation), not the GR-79590 pattern.
- **Verdict: KILL — guard is the check; must not duplicate gc BCE.** (R2/R3/R4 fail.)

### C7 — Write-barrier enabled check at pointer stores
- (a) `ssa/writebarrier.go`: stores needing barriers become `OpStoreWB/...`; the pass
  emits `if writeBarrier.enabled { buf := gcWriteBarrierN(); ... }` — a branch on the
  **global GC-phase flag**, grouped so one branch covers a contiguous WB-store sequence
  per block; `(WB ...) => (LoweredWB ...)` (`AMD64.rules:538`); disassembly confirms
  `CMPL runtime.writeBarrier(SB), $0`.
- (b) Hot: yes — every heap pointer store.
- (c) gc overlap: n/a (always emitted by design).
- (d) The guarded property is *global GC phase*, not a declared-type property. The guard
  would be the same load-and-test of the flag. (Speculating "no GC marking during this
  region" needs re-validation at safepoints — equivalent cost, unsound otherwise since
  Go's collector is concurrent.)
- **Verdict: KILL — no declared-type property; guard is the check.** (R2/R3 fail.)

### C8 — Interface method-call itab checks
- (a) `ssagen/ssa.go getClosureAndRcvr`: `OpITab` load + `s.nilCheck(itab)`
  (`OpNilCheck` — nilability, subject to `ssa/nilcheck.go` elimination, otherwise
  fault-based) + indirect call through `itab.fun[]`. **There is no type check** — dispatch
  is a load, not a verification.
- (b) Hot: yes — every interface call.
- (c) gc overlap: nilcheckelim already handles the nil check.
- (d) No declared-type property; and call-site speculation (guard dominant receiver type)
  is exactly SURV-004 (monomorphic) / SURV-070 (skewed) — the devirt cascade.
- **Verdict: KILL — no check; speculation shape already owned by SURV-004/070.**
  (R1 fails; R4 via devirt lane.)

### C9 — Slice-to-array-pointer conversion `(*[N]T)(s)`
- (a) `ssagen/ssa.go:3675` (`ir.OSLICE2ARRPTR`): lowers to `s.boundsCheck(arrlen, cap,
  ssa.BoundsConvert, false)` — i.e. **a bounds check** (`len(s) >= N`), then `OpSlicePtr`.
- (b) Hot: rare (mostly FFI/cgo-adjacent and fixed-size header parsing).
- (c/d) Guard is the check (C6 reasoning); length property, not type.
- **Verdict: KILL — guard is the check; rare.** (R2/R3 fail.)

### C10 — Map index assign `m[k] = v`, channel send/recv
- (a) Opaque runtime calls (`runtime.mapassign`, `runtime.chansend`); no per-site SSA
  check opcodes. Internal algorithmic work (hash, tophash, grow), no type verification.
- (b) Hot: yes.
- (c/d) No check, no declared-type property to guard.
- **Verdict: KILL — vacuous.** (R1/R2 fail.)

### C11 — Misc: div-by-zero, nil dereference, interface `==`, `append` growth, `unsafe.Slice`
- Div-by-zero: fault-based on amd64 (no emitted check) — vacuous.
- Nil dereference: `OpNilCheck` + prove/nilcheckelim; guard is the check; nilability, not type.
- Interface `==`: `runtime.efaceeq/ifaceeq` calls; dynamic-type equality *is* the check.
- `append` growth check: length property, not type.
- `unsafe.Slice` nil+len check: rare, guard is the check.
- **Verdict: KILL (all) — vacuous or guard-is-the-check, none type-declared.**

---

## 3. Overlap analysis

**vs SURV-070 (skewed-dispatch speculation at polymorphic call sites).** No collision:
nothing confirms for SURV-077, so there is nothing to collide. The only surviving shape
(C4, iface→iface assertion) uses *profile-guided concrete-type guarding*, which is the
devirt-cascade mechanism (SURV-004 → SURV-070), not SURV-077's declared-type pattern. If
the C4 raw candidate is ever pursued, it must coordinate with SURV-070's guard-budget
discipline (SURV-055 failure budgets) and the E4 type-profile design — same guard
machinery, different site kind (assertion sites, not call sites). Explicitly: SURV-070
covers call-site dispatch; C4 covers assertion-site itab materialization; no overlap in
claimed sites, shared infra at most.

**vs SURV-074 (speculative contention-free lock fast path).** No overlap. The wave-4
raw-candidate note flagged "same guard-a-declaration pattern, different check" —
concretely the checks, guards, and site kinds are disjoint (contention state vs type
state; mutex sites vs store/assertion sites). Killing SURV-077 changes nothing about
SURV-074.

**vs gc's existing machinery.** C6 explicitly excluded to avoid duplicating
prove.go/loopbce.go; C8-nil and C11-nil excluded to avoid duplicating nilcheck.go; C3
excluded because gc's single-compare assertion is already minimal; C5 excluded because
gc's hash-dispatch type switch is already O(1). No surviving candidate duplicates gc.

---

## 4. Per-candidate verdicts (summary)

| # | Candidate | Verdict | Precise reason |
|---|-----------|---------|----------------|
| C1 | slice/array element stores | **KILL** | vacuous — no store check exists (no covariance) |
| C2 | concrete→interface boxing stores | **KILL** | vacuous at store — boxing never fails |
| C3 | `x.(T)` concrete assertion | **KILL** | guard is the check (1 ptr compare); no declared property |
| C4 | `x.(I2)` iface→iface assertion | **KILL (for SURV-077)** | real cost, but guard is profile-guided, not declared-type-guided → devirt family |
| C5 | type switches | **KILL (for SURV-077)** | hash dispatch already O(1); speculation would be profile-guided |
| C6 | bounds checks | **KILL** | guard is the check; length not type; must not duplicate gc BCE |
| C7 | write-barrier enabled check | **KILL** | global GC-phase flag, not declared-type; guard is the check |
| C8 | interface call itab checks | **KILL** | no check; speculation shape owned by SURV-004/070 |
| C9 | slice→array-ptr conversion | **KILL** | reduces to bounds check; rare |
| C10 | map assign / channel ops | **KILL** | runtime calls; no check, no declared property |
| C11 | div-zero / nil / iface == / append / unsafe.Slice | **KILL** | vacuous or guard-is-the-check |

---

## 5. Overall verdict: KILL SURV-077

**Zero candidates confirm.** The direct analog is vacuous (C1/C2: Go's lack of array
covariance means no store check exists at any slice/array store — proven by language
semantics and by disassembly). Every other hot check in Go either *is* its own cheapest
guard (C3, C6, C7, C9, C11), is already minimal in gc (C3, C5), has no check at all
(C8, C10), or admits only a *profile-guided* guard rather than a *declared-type* guard
(C4, C5) — which fails the GR-79590 pattern's defining requirement (R2) and belongs to
the devirt family instead.

**Recommendation:** strike SURV-077 to **§5** ("Surveyed, nothing applicable"). Draft §5
entry for the synthesis pass:

> - **Graal GR-79590 speculative store checks → Go (SURV-077, Tier 2):** the direct
>   analog is vacuous — Go has no array covariance, so no store check exists at any
>   slice/array store (verified: `[]any` store compiles to box + bounds check +
>   write-barrier-flag branch, nothing else; Go 1.27.1 `go tool compile -S`). The
>   transferable pattern (guard a declared-type property + specialize + deopt) finds no
>   target: every hot Go check either is its own cheapest guard (type assertion = one
>   itab/type-word compare; bounds/nil/write-barrier checks), is already minimal in gc
>   (hash-dispatched type switches), has no check at all (interface dispatch), or
>   admits only a profile-guided guard (iface→iface itab materialization — devirt
>   family, not declared-type speculation). Nothing to port; do not re-survey without a
>   newly discovered check class.

Also correct the record: the wave-4 note "Go's `any` slices are the exact analog of
Java's `Object[]`" is false — covariance is the load-bearing difference, and Go lacks it.

**Raw candidate spun off (no SURV-ID assigned — IDs come at synthesis):**
*Speculative dominant-type guard for iface→iface assertion sites.* At hot `x.(I2)` /
iface→iface conversion sites with a skewed dynamic-type profile (e.g. `fmt`'s
per-operand `Formatter`/`Stringer` checks, `errors.As` traversals), guard
`dynamic-type(x) == T0` with one type-word compare; on hit load the precomputed
constant itab(T0, I2) (possibly nil → fast miss path), skipping gc's inline cache probe
(atomic load + hash loop) and the `getitab` path; on miss fall back to the existing
cache-probe/runtime path (diamond, no deopt strictly required — semantics-preserving
fallback; deopt only if fully specialized). Benchmark sketch: fmt-heavy logging loop
with a dominant operand dynamic type vs gc+PGO; measure the itab-probe cycles removed
per assertion. **Lane:** devirt/speculation family (SURV-004 → SURV-070); coordinate
guard-budget policy with SURV-055/SURV-070 if pursued. Expected win is small
(cache-probe ≈ order-10 cycles on hit, author's estimate) — prototype-gated at best.

**What was not verified:** GR-79590 internals beyond the wave-4 changelog fetch (not
load-bearing — the Go side settles it); exhaustive golang/go issue search (verdict is
structural from compiler source + disassembly); quantitative hotness of C4 sites
(estimates labeled as such above).
