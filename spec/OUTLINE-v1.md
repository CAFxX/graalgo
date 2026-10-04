# graalgo SPEC.md — outline v1

**Date:** 2026-10-03 · **Protocol:** greenfield Phase 0 (structure fixpoint before writing)
**Inputs:** `FEASIBILITY.md` (§2, §4, §5, §6.5a, §7, §8, §10, §11), `PHASES.md` (§2 done-gates),
`research/T1`–`T8`.

## Conventions for the spec
- Every claim carries `[HIST]` / `[VERIFIED]` / `[PROPOSAL]` / `[DECIDED]` / `[OPEN]`
  (labels defined in §1). Zero `[OPEN]` on go/no-go items.
- Every requirement is stated once, in the GG registry (`requirements/GG-XXXX.yaml`,
  status `draft`); the spec cites GG IDs and never restates requirement text.
- Load-bearing claims name their falsifying experiment before claiming they hold (§9).
- No computed-as-measured numbers; estimates labeled `[ESTIMATE]`.

## Planned sections

### 1. Introduction
- 1.1 What graalgo is (one paragraph; pointer to FEASIBILITY.md §1 for the verdict)
- 1.2 Document status: Phase-0 spec, no implementation; relation to study / PHASES.md / spikes / ADR
- 1.3 Evidence-label legend + reading rules (go/no-go = must be [DECIDED]/[VERIFIED]/[HIST])
- 1.4 Terminology: fixed glossary seeds (backend, intrinsic surface, toolexec seam, AOT+JIT hybrid, oracle, exclusion list, parity stance, toil bound)

### 2. Program requirements
- 2.1 Drop-in (GG-0001, GG-0002): 19 commands, flags, exit codes, module/build-cache/test/vet behavior; what drop-in does NOT require (byte-identical binaries/cache entries)
- 2.2 Extra JVM APIs (GG-0003): graalgo/java surface alongside, not instead of, stock behavior
- 2.3 Version tracking (GG-0004): bounded agent-driven upgrades; P95 ≤ 200 agent-hours / ≤ 10 days; breach → toil-reduction sprint
- 2.4 Go↔Java interop (GG-0005): both directions, shared data types, copy-vs-view contract
- 2.5 Three execution modes (GG-0006): (a) JIT-only, (b) AOT-only, (c) AOT+JIT hybrid default; one AST, packaging-only differences; mode-selection mechanism
- 2.6 Beyond-gc optimizations (GG-0007): gc+PGO bar; devirtualization cascade thesis §2.6.1; claim-strike rule
- 2.7 Modern JVM integration (GG-0008): virtual threads, async I/O, isolates, JFR/VisualVM-grade observability
- 2.8 Stdlib reuse (GG-0009): verbatim vendor + adapted fork limited to coupled packages; asm strategy per category
- 2.9 Testing as first-class deliverable (GG-0010): built early (Phase-0 harness seed)

### 3. Architecture
- 3.1 Driver: wrap, don't reimplement, don't fork (GG-0011): stock distribution + `-toolexec` seam; fork as priced escape hatch; driver cost = conformance gate
- 3.2 Frontend service (GG-0012, GG-0013): go/parser+go/types vendored verbatim, atomic re-vendor per release; `-lang` gates for old code, never pinning
- 3.3 Truffle AST (GG-0014): node taxonomy, typed execute methods, DSL specialization budget only at dynamic sites (interface PIC on itab, type-switch decision trees, map fast path); @TruffleBoundary slow paths
- 3.4 Value model (GG-0015, GG-0016): Java objects throughout; descriptor structs, typed-array slices, (itab,data) interfaces, byte[] strings; pointer = opaque reference; unsafe.Pointer v1 = handle-table
- 3.5 Concurrency runtime (GG-0017, GG-0018, GG-0019): goroutine=virtual thread 1:1, back-edge safepoint yields, custom carrier pool for GOMAXPROCS; netpoller deleted → blocking NIO; channels/select port; panic=AbstractTruffleException, Goexit=ControlFlowException, explicit defer chains, panic(nil) semantics
- 3.6 Intrinsic surface & linkname contract (GG-0020, GG-0021): ~60–70 functions; exact rtype/itab layouts for reflect; linkname compat by symbol name; linkname-diff per release
- 3.7 cgo → Panama (GG-0022, GG-0058): stock cgo tool + host gcc kept; graalgo authors the C ABI struct-layout engine; platform list; MM-0017 verifier gate on the FFI path
- 3.8 OS scope (GG-0023): Linux-first; darwin deferred; windows out of v1 with re-test trigger
- 3.9 Observability & debugging (GG-0024, GG-0008): Delve structurally impossible → Truffle Chrome Inspector; go tool pprof via profile.proto translation; JFR custom events
- 3.10 momo isolates (GG-0025, GG-0026, GG-0027): isolate-per-generation; MaxHeapSize + isResourceExhausted; no CPU quota in v1; JIT-mode fallback = processes; SpawnIsolate verified on pinned GraalVM

### 4. Interfaces & contracts
- 4.1 AST↔runtime intrinsic contract (GG-0059): signatures versioned, Phase-1 interface freeze
- 4.2 `-toolexec` argv contract (GG-0059): intercepted tool set (compile/asm/cgo/link/cover/pack), argv mapping, toolID behavior
- 4.3 Go↔Java interop contract (GG-0005): per-type mapping table, copy-vs-view defaults, error↔exception rule (never panic), channel handles
- 4.4 Build-cache identity (GG-0028): behavioral compat; toolID auto-namespaces backends
- 4.5 GODEBUG contract (GG-0055): per-knob registry (emulated / no-op / unsupported), default-deny, per-release review

### 5. Testing strategy (four layers + oracle machinery)
- 5.1 Layer 1: fast differential smoke (GG-0029, GG-0030, GG-0031)
- 5.2 Layer 2: conformance corpora (GG-0032)
- 5.3 Layer 3: stdlib + cmd/go script suites (GG-0029)
- 5.4 Layer 4: generative differential fuzzing (GG-0037)
- 5.5 The differential oracle: compared/excluded sets, normalization, nondeterminism controls (GG-0030, GG-0031)
- 5.6 Exclusion list: versioned, reason-coded, review-gated growth (GG-0033)
- 5.7 Oracle validation: mutation/red-green, no tautological tests (GG-0034)
- 5.8 API/ABI diffing as the toil engine: apidiff, signature diff, rtlink, flagdiff, godebug-diff; tooling red-green (GG-0035)
- 5.9 Behavior snapshots (GG-0036)
- 5.10 Gate integration: per-commit vs per-release; triage records; no unrecorded waivers (GG-0038)

### 6. Upgrade workflow
- 6.1 Trigger at rc1; 9-phase runbook (GG-0039)
- 6.2 Patch-release light path (GG-0040)
- 6.3 Weekly release-watch agent: prepares, never merges (GG-0041)
- 6.4 Toil accounting + bound enforcement (GG-0042)
- 6.5 Breach handling: toil-reduction sprint; two consecutive breaches → re-scope with Carlo (GG-0043)
- 6.6 Patch-stack discipline: near zero, ≤5 conflicts (GG-0044)
- 6.7 Parity stances: decided once, in the architecture log (GG-0045)
- 6.8 GOTOOLCHAIN semantics: track-latest + faithful selection (GG-0046)

### 7. §10 open-question closures (one subsection each, ending with the label)
- 7.1 Sulong vs direct AST → decision rule (GG-0047)
- 7.2 Preemption-equivalence bar → observable list (GG-0048)
- 7.3 unsafe.Pointer v1 semantics → handle-table (GG-0016)
- 7.4 Netpoller deletion vs adaptation → deletion (GG-0018)
- 7.5 cgo→Panama authorship + platform list (GG-0022)
- 7.6 AOT reachability metadata: tracing agent vs hand-authored (GG-0052)
- 7.7 plugin.Open under closed-world AOT (GG-0051)
- 7.8 Hybrid default confirmation → six experiments + meta-gate (GG-0049)
- 7.9 SpawnIsolate maturity + isolate CPU quotas (GG-0027, GG-0025)
- 7.10 go vet: stock binary vs rehost (GG-0053)
- 7.11 Pure-JIT mode momo-isolation story (GG-0026)
- 7.12 OSR in Native Image verification (GG-0054)
- 7.13 Windows support (GG-0023)
- 7.14 Agent-leverage discount (GG-0056)
- 7.15 GODEBUG surface triage (GG-0055)

### 8. Risks & falsification matrix
- 8.1 Risk → falsifying experiment table (5 study risks + hybrid-mode risks): asm/cgo spike, Loom spike, version-tax dry run, reflect/unsafe spike, cmd/go spike; image-size delta, warmup, safepoint overhead, pinning audit, deopt torture, stack-depth parity
- 8.2 Pinning audit requirement (GG-0057)
- 8.3 Kill criteria pointer (PHASES.md §11); program-level: two consecutive toil breaches

### 9. Estimates, staffing, critical path
- 9.1 Phase ranges + total 20–40 PY, 2–4 PY/yr forever [ESTIMATE, LOW confidence]
- 9.2 No agent-leverage discount (GG-0056); leverage measured as program metric
- 9.3 Staffing notes + critical path 0→1→2→3→5 ≈ 4–5 yr

### 10. Provenance & verdict scope
- 10.1 Claim provenance ledger (extends FEASIBILITY.md §11)
- 10.2 Verdict-scope ledger: negative verdicts scoped to tested configuration + re-test triggers

### Appendices
- A. Glossary (normative term definitions)
- B. Requirement index: GG-ID → section map
- C. Pinned-dependency record: GraalVM/JDK pin (GG-0027), Go release pin

## Review checklist (for the v1→v2 round)
- [ ] Every §10 question (15) has exactly one home in §7 — no orphans, no duplicates
- [ ] Every §8 risk (5) has a falsifying experiment named in §8.1
- [ ] Every GG requirement has exactly one primary spec home (see planned ID ranges below)
- [ ] No section duplicates another's normative content (interfaces in §4, not scattered)
- [ ] Go/no-go items (§2 program requirements, §7 closures, §8 risks) end [DECIDED]/[VERIFIED]/[HIST]

## Planned GG-ID ranges (mint order)
- GG-0001–GG-0010: program requirements (§2)
- GG-0011–GG-0028: architecture (§3)
- GG-0029–GG-0038: testing strategy (§5)
- GG-0039–GG-0046: upgrade workflow (§6)
- GG-0047–GG-0060: §10 closures, contracts, cross-cutting (§4, §7, §8)
