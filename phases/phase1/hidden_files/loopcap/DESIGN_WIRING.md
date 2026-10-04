# Loop-capture wiring — single-owner implementation design (2026-10-04 ~11:45 CEST)

## gc-oracle semantics (pinned, t1–t11)
- Body vars (`:=`/`var` in loop body): per-iteration cell; decl allocates fresh GoBox on each execution.
- For-header vars (`for i := 0`): TWO cells. S_outer (init/cond/post + header closures),
  S_iter (body + body closures). Refresh at top of each iteration seeds S_iter from S_outer.
  Body whole-var writes write through to S_outer (t2, t11). Post/cond use S_outer.
  Header closures snapshot S_outer; body closures snapshot S_iter.
- Range vars: per-iteration cell; Range node allocates fresh GoBox per iteration (define)
  or writes into existing cell (`=` into boxed var).

## Corrections to first worker's partial wiring
1. postMode read S_iter → WRONG for struct field-mutation (body `s.x=5` mutates only the
   body cell's copy; post must see outer). Post reads AND writes S_outer.
2. headerMode/postMode booleans → WRONG for nested loops (inner loop clobbers flags;
   inner init referencing outer header var would misresolve). Replaced by factory-level
   loop-phase stack keyed by loop id.

## Mechanism
- Factory-level (single-threaded build): `nextLoopId`, `Deque<LoopPhase> loopStack`,
  `Map<Integer,Integer> headerLoopIds` (oid → declaring loop id).
- buildFor: prescan populates ctx.boxed + headerLoopIds; pushes (id,INIT); builds
  init; phase=COND builds cond; phase=POST builds post; phase=BODY builds body with
  BoxRefreshNode prologue prepended; pops in finally.
- Phase query: `headerPhase(oid)` walks loopStack for oid's loop id → INIT/COND/POST
  (use S_outer) or BODY (use S_iter).
- readVar: `isBoxedTransitive` (walk ctx parent chain) → readBoxedVar (phase-aware).
- Writes: `boxWriteSlot(ctx, oid)` — snapshot slot if captured (depth>0), else phase-aware
  local slot; post writes S_outer (never through body cell: "post does NOT mutate
  body-captured instances").
- snapshotBoxedVar: phase-aware — header var in BODY phase snapshots S_iter, else S_outer.
- buildFuncLit: drains fctx.boxSpec → FuncLitNode(target, captures, flat, ctx.closureSlot);
  passes snapSlots (boxSpec order) to GoFuncRootNode.
- buildRange: prescan; passes keyBoxed/valBoxed/keyDefine/valDefine to Range nodes.
- buildVarDecl/buildAssign/buildIncDec/buildCommaOk/writeLvalue/readLvalue: box-aware
  via Lhs.oid + helpers. Uncaptured vars keep plain slots (zero fast-path cost).
- declareObj: boxed oids get FrameSlotKind.Object slots (+ $iter slot for header vars).

## Pre-scan (no exporter change; D1 frozen)
- collectVardeclObjs: "vardecl" objs in loop subtree, NOT descending into funclits.
- collectFunclitRefs: ident "obj" ids inside funclit subtrees (nested included).
- boxed = (initDecls ∪ bodyDecls ∪ rangeDecls) ∩ funclitRefs.
- Shadowing safe: go/types oids distinguish; funclit-local decls excluded by no-descend.
