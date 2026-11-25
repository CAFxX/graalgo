# Loop-capture fix worker — checkpoint 2026-10-04 ~09:30 UTC

**Status: STOPPED due to concurrent-worker conflict. NodeFactory wiring NOT completed.**

## What was done
- Root-cause analysis complete (see final report): loop-declared variables share one
  frame slot across iterations; FuncLitNode materializes the same parent frame each
  iteration → all closures see the last write.
- gc-oracle semantics pinned (t1–t11 in /tmp/loopvar): body vars per-iteration (all
  versions); for-header vars go1.22+ = two cells (outer for init/cond/post + header
  closures; per-iteration body cell for body + body closures; body writes write
  through to outer; post does NOT mutate body-captured instances); range vars
  per-iteration.
- Node-layer refactors COMPLETE and compiling (before the conflict):
  - `values/GoBox.java`: added `outer` field (write-through target for for-header body cells).
  - `values/GoFunc.java`: dropped `boxOuters` (superseded by GoBox.outer).
  - `frontend/FuncLitNode.java`: boxSpec simplified to (slot, snapSlot) pairs (source is
    always in the creating frame after capture propagation).
  - `frontend/BoxWriteNode.java`: 3-arg `(kind, cellSlot, value)`; write-through via
    `Boxing.setBoxedCell`.
  - `frontend/Boxing.java`: added `setBoxedCell` helper.
  - `frontend/BoxRefreshNode.java`: body cell carries outer pointer.
  - `frontend/Range{Slice,Array,String,Map}Node.java`: added keyDefine/valDefine flags;
    `=`-into-boxed writes into the existing cell (not a fresh one).
- Pristine (pre-work) copies: `hidden_files/loopcap/pristine/` + MD5SUMS.
- NodeFactory wiring: DESIGNED (pre-scan for funclit refs, BoxRecord/LoopBoxInfo,
  box-aware read/write/decl/incdec, FuncLitNode boxSpec with propagation, for-prologue
  refresh) but NOT implemented — stopped when a concurrent writer was detected.

## The conflict
- 09:12–09:14 UTC: M6 node skeletons appeared (previous worker).
- 09:25:44+ UTC: a second worker began heavily editing `NodeFactory.java`
  (2560+ lines, still changing at 09:26) AND `frontend/export.go`, implementing an
  INCOMPATIBLE design: exporter-computed wire `"boxed"` field + `headerMode`/`postMode`
  flags (vs my factory pre-scan, no exporter change).
- Their NodeFactory calls `new BoxWriteNode(kind, slot, value)` — the 3-arg signature
  from MY refactor, so my node changes must NOT be reverted (would break their compile).
- Per the M5 coordination note, reporting immediately and standing down. Parent must
  pick ONE design and ONE worker.

## Design doc for whoever continues
See the final report to parent (root cause, gc-oracle table, full factory-wiring design).
