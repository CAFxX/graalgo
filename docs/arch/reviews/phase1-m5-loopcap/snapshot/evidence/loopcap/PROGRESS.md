# Loop-capture wiring — implementation progress (2026-10-04 ~12:30 CEST)

## Completed
- NodeFactory.java restored from pristine (md5 3c4b7949129926ebbf67f44e4e53e36f).
- export.go 8 markers reverted; build.sh build + check green.
- BuildCtx: removed headerMode/postMode/headerObjs; kept boxed/boxIterSlots/boxSnapSlots/boxSpec; added boxKinds.
- Factory: loopStack (LoopPhase), nextLoopId, headerLoopIds, LP_INIT/COND/POST/BODY.
- Helpers: isBoxedTransitive, headerIterSlot, headerPhase, snapshotBoxedVar (phase-aware),
  readVar (transitive), readBoxedVar (phase-aware), boxWriteSlot.
- Pre-scan: nodeKind, collectVardeclObjs, collectIdents, collectFunclitRefs,
  prescanForBoxes (returns headerOids), prescanRangeBoxes.
- buildFor: prescan, iter slots, phase stack push/pop, BoxRefreshNode prologue.
- buildRange: prescan, keyBoxed/valBoxed/define flags to Range constructors.
- buildFuncLit: drains boxSpec, passes snapSlots to GoFuncRootNode, 4-arg FuncLitNode.
- declareObj: box-aware (Object slots for boxed).
- buildVarDecl: box-aware (BoxDeclNode for zero/single/multi; comma-ok Lhs has oid).
- buildAssign: ident-only plain (assignTwoPhaseBoxed), ident-only op (writeIdentSlot/readIdentSlot),
  general path (writeIdentSlot).
- writeLvalue/readLvalue: box-aware via writeIdentSlot/readIdentSlot.
- buildIncDec: box-aware ident case.
- buildCommaOk: box-aware (BoxDeclNode on :=, BoxWriteNode on =).
- Lhs.oid field; classifyLhs sets it.
- Removed dead prescanBoxed/prescanCollectRefs/prescanWalk.
- Compiles clean with -Xlint:deprecation -Werror.

## In progress
- `make test`: 38 failures (investigating). Baseline (pristine) test running to
  determine if pre-existing.
- Failure modes: (1) `blank: missing required key "fields"` in pn12/pn13/pn20;
  (2) panic exit code 1 vs expected 2 in pn14/pn15/pn17/pn18/pn19.

## Next
1. Determine baseline failures vs my regressions.
2. Fix regressions.
3. Restore pn13 corpus file; write cf12-cf19 loop-capture corpus + test methods.
4. Red-green: fail on pristine, pass on fixed.
5. Full make test green; self-attack; final report.
