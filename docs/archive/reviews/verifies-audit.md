> Archived 2026-11-18 from `docs/dev/` (historical review record; superseded/retired ids below are history).

# `verifies:` annotation audit (2026-11-18)

A `verifies: GG-XXXX` annotation asserts that **a test** demonstrates the
requirement. `req lint` rule (c) counts every such annotation anywhere in the
tree, so an annotation on implementation code, in registry history prose or
on an unrelated test makes an active requirement look verified when it is not.
This list is the input for the code lanes (and the debt-paydown lane for the
gate); the spec-reconciliation lane edits no code beyond the ID renames that
`req supersede` performs.

Scope: every annotation the linter counts outside test sources, plus the
test-side annotations found wrong while reconciling the requirements of
[ADR-0009](../../dev/adr/ADR-0009-recovery-spec-amendments.md) and
[ADR-0011](../../dev/adr/ADR-0011-spec-reconciliation.md). Tree: `wt/spec/reconcile`
after the 2026-11-18 supersessions. Counts per requirement were taken with
the linter's own matching rules (Markdown code spans and fences ignored).

**Update 2026-11-18 (requirement re-issue, [ADR-0012](../../dev/adr/ADR-0012-open-coded-defers.md)
decision 4):** the rows for `GG-0063`/`GG-SCHED-0005` (IoSlotsTest),
`GG-0065` (GoDebugTest), `GG-0066` ×4 and the three `GG-SCHED-0005`
implementation annotations are resolved — see the Action column; the
`GG-0066` rows were marked OK here and were not: v1 tests do not verify the
frontend requirement. `PanicCorpusTest`'s `GG-0067` annotation, which this
audit missed, is in the same class.

**Rule to add:** `verifies:` only in tests — `*_test.go`, `src/test/`, and
corpus inputs under [test/](../../../test/) — never in `src/main`, registry
history or Markdown prose. `req lint` cannot enforce this today (it scans
every file the same way), so the rule is a **tools/req follow-up**: a lint
rule (f) "`verifies:` outside a test source" (rule (e) is now the
locked-text sum check), with
[requirements/](../../../requirements/) history and
[docs/archive/reviews/](./) snapshots excluded from rule (c)
counting. Removing every annotation below keeps rule (c) green: each active
requirement involved keeps test-side annotations, except as noted for
`GG-0027`.

## 1. Test-side annotations that are wrong

| Annotation | Location | Verdict | Action |
|---|---|---|---|
| `GG-0027 (M2: launcher executes real programs …)` | [MainTest.java](../../../internal/backend/truffle/src/test/java/graalgo/launcher/MainTest.java):18 | WRONG — GG-0027 is the pinned GraalVM distribution and the SpawnIsolate/OSR re-verification on it; the test runs programs end to end | write a real verifier first (e.g. the build checks the pinned jars against [LIB.md](../../../internal/backend/truffle/lib/LIB.md)'s checksums; WP-OSR's dated results), then delete. GG-0027 is **active** and this is its only annotation: deleting it alone turns rule (c) red |
| `GG-0061 (M3: expressions + values …)` | [ArithmeticCorpusTest.java](../../../internal/backend/truffle/src/test/java/graalgo/frontend/ArithmeticCorpusTest.java):40 | WRONG — re-pointed mechanically from `GG-0015` by the ADR-0009 supersession; it tests v1's nested `GoStruct`, which `GG-0061` declared non-conforming | **done 2026-11-19:** annotation deleted when `GG-0061` was superseded by the draft GG-0072 (rule (b) rejects it on a superseded id); W4/W13 tests will carry GG-0072 |
| `GG-0063, GG-SCHED-0005 (io slice)` | [IoSlotsTest.java](../../../internal/backend/truffle/src/test/java/graalgo/sched/IoSlotsTest.java):17 | PARTLY WRONG — the test checks `ioEnter`/`ioExit` slot accounting; the epoll poller of GG-0063 does not exist yet | **done 2026-11-18:** now `verifies: GG-SCHED-0006 (io slice: ioEnter/ioExit slot accounting)`; the W8b poller tests carry GG-0063 |
| `GG-0065 (GODEBUG triage: … unknown-knob deny-with-error …)` | [GoDebugTest.java](../../../internal/backend/truffle/src/test/java/graalgo/sched/launcher/GoDebugTest.java):18 | STALE TEXT — the parenthetical describes the superseded rule | **partly done 2026-11-18:** every annotation re-pointed to GG-0068 (draft) and the class parenthetical no longer claims the deny rule; WP-GODEBUG ([RECOVERY.md](../../arch/RECOVERY.md)) still inverts the six tests whose annotations were removed |
| `GG-0066` ×4 | [StraightLineCorpusTest.java](../../../internal/backend/truffle/src/test/java/graalgo/frontend/StraightLineCorpusTest.java):38, [export_test.go](../../../internal/frontend/p3frontend/export_test.go):145, :187, [loader_test.go](../../../internal/frontend/p3frontend/loader_test.go):151 | WRONG (marked OK by the first version of this audit) — v1 frontend tests exercise v1's JSON export, not the requirement (tracked-release provenance, GIR) | **done 2026-11-18:** annotations removed; the only annotation of the successor GG-0069 (draft) is on [release_test.go](../../../internal/toolexec/release/release_test.go) (`TestBuiltByTrackedToolchain`) |
| `GG-0067 (M5: defer/panic/recover/Goexit …)` | [PanicCorpusTest.java](../../../internal/backend/truffle/src/test/java/graalgo/frontend/PanicCorpusTest.java):81 | WRONG — a v1 test; `GG-0067` was activated on it alone | **done 2026-11-18:** annotation removed; GG-0070/GG-0071 (drafts) get theirs with TR-3 |

## 2. Annotations outside tests

Verdicts: **DECORATIVE** — on implementation or prose, verifies nothing;
**WRONG** — also names an unrelated requirement.

| Annotation | Location | Verdict | Action |
|---|---|---|---|
| `GG-0017 (iface slice)` | [IfaceDispatchBuilder.java](../../../internal/backend/interop/src/main/java/graalgo/iface/IfaceDispatchBuilder.java):48 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0014` | [IfaceDispatchNode.java](../../../internal/backend/interop/src/main/java/graalgo/iface/IfaceDispatchNode.java):56 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0014 keeps 30 test-side annotations) |
| `GG-0017 (iface slice)` | [IfaceDispatchNode.java](../../../internal/backend/interop/src/main/java/graalgo/iface/IfaceDispatchNode.java):57 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [ChanCloseNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/ChanCloseNode.java):27 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [ChanLenCapNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/ChanLenCapNode.java):34 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [ChanRecvNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/ChanRecvNode.java):31 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [ChanSendNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/ChanSendNode.java):25 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [ChanWire.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/ChanWire.java):35 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [MakeChanNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/MakeChanNode.java):32 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (chan slice)` | [SelectNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3chan/nodes/SelectNode.java):57 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0020 (intrinsic surface completion)` | [GoCrypto.java](../../../internal/backend/interop/src/main/java/graalgo/p3crypto/GoCrypto.java):38 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020` | [LinknameIntrinsics.java](../../../internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java):61 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020` | [ReflectLinknames.java](../../../internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectLinknames.java):38 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0017 (sync slice)` | [P3SyncMain.java](../../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java):35 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0020` | [AtomicOpNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicOpNode.java):44 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0017 (sync slice)` | [SyncNewNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/SyncNewNode.java):49 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (sync slice)` | [SyncNewNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/SyncNewNode.java):49 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0017 (sync slice)` | [SyncOpNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/SyncOpNode.java):81 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [GoTickerT.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/GoTickerT.java):12 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [GoTimerT.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/GoTimerT.java):65 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [P3TimeRuntime.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/P3TimeRuntime.java):32 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeChan.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/TimeChan.java):18 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeDescriptors.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/TimeDescriptors.java):28 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeAfterFuncNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeAfterFuncNode.java):34 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeAfterNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeAfterNode.java):27 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeCNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeCNode.java):25 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeNewNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeNewNode.java):29 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeNowNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeNowNode.java):26 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeOpNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeOpNode.java):29 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeSleepNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeSleepNode.java):24 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0017 (time slice)` | [TimeTickNode.java](../../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeTickNode.java):29 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (`GG-0017` keeps 38 test-side annotations) |
| `GG-0020 (WS G builtin)` | [AppendNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/AppendNode.java):10 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [CapNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/CapNode.java):8 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0021 (WS G builtin)` | [ClearNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/ClearNode.java):8 | WRONG — `clear` is a builtin, not a linkname (GG-0021 is linkname-by-symbol-name) | delete the annotation |
| `GG-0020 (WS G builtin)` | [ComplexNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/ComplexNode.java):9 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [CopyNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/CopyNode.java):9 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [DeleteNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/DeleteNode.java):8 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [LenNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/LenNode.java):10 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020` | [LinknameRootNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/LinknameRootNode.java):25 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [MakeNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/MakeNode.java):11 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0022 (WS G builtin)` | [MinMaxNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/MinMaxNode.java):8 | WRONG — `min`/`max` builtins have nothing to do with cgo (GG-0022) | delete the annotation |
| `GG-0020 (WS G builtin)` | [NewNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/NewNode.java):8 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [PanicNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/PanicNode.java):8 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G builtin)` | [PrintNode.java](../../../internal/backend/truffle/src/main/java/graalgo/frontend/PrintNode.java):9 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020 (WS G intrinsic surface)` | [BuiltinPrint.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/BuiltinPrint.java):2 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-SCHED-0005 (timer slice)` | [GoTimer.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/GoTimer.java):47 | DECORATIVE — annotation on implementation (src/main), not on a test | **done 2026-11-18:** now a plain reference to GG-SCHED-0006 |
| `GG-0020 (WS G intrinsic surface)` | [Intrinsics.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/Intrinsics.java):2 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0020` | [Linknames.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/Linknames.java):38 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-0048 (timer slice)` | [TimerChan.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/TimerChan.java):23 | WRONG — on implementation, and GG-0048 is the scheduling-observables bar (verified by the torture and latency suites), not timer channels | delete the annotation |
| `GG-SCHED-0005 (timer slice)` | [TimerHeap.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/TimerHeap.java):38 | DECORATIVE — annotation on implementation (src/main), not on a test | **done 2026-11-18:** now a plain reference to GG-SCHED-0006 |
| `GG-0020 (timer slice)` | [Timers.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/Timers.java):17 | DECORATIVE — annotation on implementation (src/main), not on a test | delete (GG-0020 keeps 18 test-side annotations) |
| `GG-SCHED-0005 (scheduler-aware blocking)` | [GoMutex.java](../../../internal/backend/truffle/src/main/java/graalgo/runtime/sync/GoMutex.java):23 | DECORATIVE — annotation on implementation (src/main), not on a test | **done 2026-11-18:** now a plain reference to GG-SCHED-0006 |
| `GG-0013` (history prose) | [GG-0013.yaml](../../../requirements/GG-0013.yaml):14 | DECORATIVE — history prose in the registry quoting an annotation; counted by lint rule (c) | lint fix: skip `requirements/` history when collecting `verifies:` (tools/req follow-up); no edit to the locked record |
| `GG-0017` (history prose) | `requirements/GG-0017.yaml`:15 | DECORATIVE — history prose in the registry quoting an annotation; counted by lint rule (c) | lint fix: skip `requirements/` history when collecting `verifies:` (tools/req follow-up); no edit to the locked record |
| `GG-0017` (history prose) | `requirements/GG-0017.yaml`:16 | DECORATIVE — history prose in the registry quoting an annotation; counted by lint rule (c) | lint fix: skip `requirements/` history when collecting `verifies:` (tools/req follow-up); no edit to the locked record |
| `GG-0023` (history prose) | [GG-0023.yaml](../../../requirements/GG-0023.yaml):14 | DECORATIVE — history prose in the registry quoting an annotation; counted by lint rule (c) | lint fix: skip `requirements/` history when collecting `verifies:` (tools/req follow-up); no edit to the locked record |

## 3. Historical snapshots

The eleven `verifies:` lines under
[docs/archive/reviews/phase1-m5-loopcap/snapshot/](phase1-m5-loopcap/snapshot/)
(ten copies of v1 builtin nodes with `GG-0020`, one copy of
`ArithmeticCorpusTest` with `GG-0061`) are frozen evidence of a past review.
They are not edited, except that the `GG-0061` annotation in the `ArithmeticCorpusTest` copy was deleted on 2026-11-19 (rule (b) scans snapshots, and `GG-0061` was superseded); the tools/req follow-up above excludes them from rule
(c). Markdown prose in the phase checkpoints that quoted annotations was put
in code font in the same change as this audit.

## Summary

| Verdict | Count |
|---|---|
| WRONG (test side) | 3 (+1 stale text) at first audit; +5 found by the 2026-11-18 review (`GG-0066` ×4, `GG-0067` ×1) |
| WRONG (outside tests) | 3 |
| DECORATIVE (implementation) | 49 |
| DECORATIVE (registry history) | 4 |
| Historical snapshot (excluded, not edited) | 11 |
