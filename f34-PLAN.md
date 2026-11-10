# Lane graalgo-review-22-f-34 — R22-F-3/F-4 value-copy remediation

- Registry: `graalgo-review-22-f-34`, chat `graalgo-independent-codebase-review`
- Branch: `wt/graalgo-independent-codebase-review/review-22-f-34`, base `a0df481b`
- Resume: VM reboot #124 (boot `6c796cd0` → `071f0393`); dispatch session
  `6f11561c-2e6e-48e0-b6d7-1bc9a3a4824e`, ~03:02Z 2026-11-10.
- Source of findings: `REVIEW-2026-11-09-22-53.md` (§P1 R22-F-3/F-4 + lanes
  L22-F-3/L22-F-4) — NOTE the brief named `REVIEW-2026-11-09-22.md`, which
  contains only R22-P2-1..P2-6; the F-findings live in the `-53` file.
  Re-derived from disk; the review text was treated as a lead, not truth.

## First-act checks (done 03:0xZ, all from disk truth)

1. Live owner: NONE. No process mentions `review-22-f-34`; registry heartbeat
   `2026-11-10T00:52:08Z` (stale, ~2h10m); sibling lanes (f-5/f-6, p1-a/p1-i)
   have live processes but are different lanes. Proceeded.
2. Master-ancestry: branch exists (local + remote), worktree exists, CLEAN at
   `a0df481b` = master HEAD; `git log --grep` finds no F-3/F-4 remediation in
   master. NOT integrated pre-reboot. Proceeded.
3. Lane state re-derived: worktree clean, zero commits, zero uncommitted diff,
   no runner logs in worktree. Pre-reboot lane did nothing durable — fresh
   start from master. Worktree is a SPARSE checkout (4.4M): root Makefile +
   `internal/backend/{interop,runtime,truffle}/{Makefile,src}` only.
   No `lib/` (Truffle jars), no `test/corpus/`, no `internal/frontend/`.

## Environment (measured this run)

- Home quota CLOSED: 8 MiB unique-content probe wrote 2,129,920/8,388,608
  bytes then EDQUOT; even a ~1 KB registry heartbeat write fails with
  EDQUOT at times (flapping, trending down). Registry status IS `resuming`
  (recorded before the flap); heartbeat stuck at 00:52:08Z — retries fail.
- Backoff CLEAR (load ~3.08), MemAvailable ~3.4 GB.
- Consequence: ALL writes go to /var/tmp (tmpfs, 1.3G avail): build shadow
  `/var/tmp/f34` (sparse tree copy + symlinked `truffle/lib` from main repo
  + gofrontend binary copied from main repo @a0df481b, 13.5 MB). Sources are
  edited in the shadow and copied back to the home worktree ONLY when a
  quota window opens (edits are a few KB; retry the copy-back periodically).
  Evidence/logs live in `/var/tmp/f34-lane/`; copy to the goal hidden_files
  lanes dir when quota allows. DO NOT edit home files while EDQUOT — a
  failed mid-write could truncate a source file.

## Findings re-verified from disk (all CONFIRMED at a0df481b)

### F-3 — bulk-copy sites alias struct/array elements

- (1) `AppendNode.store()` default branch stores `v` directly; every other
  store site calls `Boxing.copyForAssign` (IndexWriteNode:41, AssignNode:39,
  CompositeLitNode:124,131,156, DeferNode:69, FuncValueCallNode:101-102,
  DerefWriteNode:43,59, InterfaceBoxNode:40, MapKeys:36).
- (1b) `AppendNode` ellipsis slice-spread: same-component-type path uses
  raw `Intrinsics.memmove` (ref copy); different-type path uses
  `Boxing.arrayGet/arraySet` loop (ref store). Neither copies.
- (2) `Intrinsics.growslice:257` — `memmove(array, 0, old.array(), ...)` is
  the untyped raw `System.arraycopy` memmove.
- (3) `Intrinsics.slicecopy:546+` → typed `memmove(..., elemKind)` default
  branch is raw `System.arraycopy` on `Object[]`.
- Canonical copiers exist: `Boxing.copyForAssign` (STRUCT→`GoStruct.deepCopy`,
  ARRAY→`GoStruct.deepCopyArray`, atomic wrappers cloned) and
  `GoStruct.deepCopy/deepCopyArray` (recursive; sync primitives fresh per
  slot in arrays). `newTypedArray` zero-fill is fine (fresh structs/slot).
- Untyped `memmove` has a third caller that must stay raw:
  `ReflectLinknames.java:106` (byte-count memmove). Do NOT change its
  semantics; add a separate element-copying method.

### F-4 — iface dispatch + method values never copy

- `IfaceDispatchNode.boxArg:343` boxes args raw; no `copyForAssign`.
  Direct-call path DOES copy (`FuncValueCallNode:101-102`, same call shape).
- Receiver `callArgs[0]`: direct `data`, promoted `readFieldPath(..., !pointerReceiver())`
  — that boolean is `derefResult` (trailing deref), NOT a copy flag — and
  embedded-redispatch `curGi.data()`. None copies for value receivers.
- `MethodValueNode:43` binds raw `Boxing.box` result into `GoFunc.boundArgs`.
- `GoCalls.callArgs:36-37` prepends bound args verbatim.
- `GoFuncRootNode:191-192` comment "(Struct by-value params are not
  deep-copied today...)" is wrong about the direct path (clean since
  FuncValueCallNode copies) — correct it.
- `GoInterface` is immutable (final descriptor/data/itab) — sharing the
  interface object IS a header copy.
- Family member found while verifying (same bug, reflect path):
  `ReflectCalls.invokeCallable` BoundMethodVal branch prepends
  `bmv.receiver` verbatim per call; `MethodInfo.pointerReceiver` + receiver
  kind (`funcType.ins[0]`) available. Include the fix (same helper).
- Follow-ups (NOT in this lane; same family, different sites):
  `Linknames.callGo` passes struct args verbatim to Go functions (bridge
  path); `ReflectOpNode.unboxArgs` unboxes reflect Call args verbatim.
  Record for coordinator; do not expand scope.

## Fix design (targeted, per finding; no root-entry bulk copy)

1. `Boxing.java`: add `copyReceiver(GoKind kind, Object value)` — copies a
   by-value receiver at a call boundary. `kind` STRUCT/ARRAY (null when
   unknown, e.g. test doubles): infer from the runtime value (GoStruct /
   array / atomic wrappers). `GoInterface`-wrapped struct/array data is
   copied via `copyForAssign(INTERFACE, gi)` (new interface, copied data).
   Other kinds pass through (references/immutables).
2. `AppendNode.store`: default branch → `Boxing.copyForAssign(elemKind, v)`.
   (Primitive/complex/bool branches untouched; copyForAssign is identity
   for SLICE/MAP/POINTER/FUNC/STRING/INTERFACE-header kinds — safe by
   construction; STRUCT/ARRAY deep-copied.)
3. `AppendNode` ellipsis spread: same-type path → new
   `Intrinsics.memmoveCopy(arr, base, spreadArr, spreadBase, n, elemKind)`;
   different-type path → `copyForAssign` each element in the arraySet loop.
4. `Intrinsics.java`: add `memmoveCopy(dst, dstOff, src, srcOff, n, elemKind)`
   — STRUCT/ARRAY elements deep-copied element-wise via
   `Boxing.copyForAssign` (overlap-safe: snapshot refs first, then write
   copies); all other kinds raw `System.arraycopy` (incl. COMPLEX branches —
   immutable records; INTERFACE — header copy only, data shared per gc).
   NOTE: do NOT deep-copy INTERFACE element data in bulk copies (gc's
   `copy`/append of `[]any` copies headers, shares data). Use in
   `growslice` (replaces line-257 memmove) and in the typed `memmove`
   default branch used by `slicecopy`.
5. `IfaceDispatchNode.boxArg`: `Boxing.copyForAssign(arg.kind(), boxed)` —
   identical call shape to FuncValueCallNode (consistency: the INTERFACE
   branch deep-copies interface data on BOTH paths today; if that invariant
   is wrong it is a separate finding, not this lane's to diverge on).
6. `IfaceDispatchNode.dispatch`: after `callArgs[0]` is computed, if
   `!target.pointerReceiver() && !target.addrOf() &&
   !receiverParamIsInterface(target)` → `callArgs[0] =
   Boxing.copyReceiver(null, callArgs[0])` (kind inferred from value;
   receiver is always raw dynamic data here, never a GoInterface except
   the guarded interface-param case).
7. `GoFuncRootNode.java`: add `receiverKind()` (paramKinds[0] or null, no
   clone) + fix the misleading comment at :191-192 to state the real
   contract (call sites copy; root still clones atomic cells).
8. `MethodValueNode.executeObject`: if the target root is a GoFuncRootNode
   with a STRUCT/ARRAY receiver kind → bind
   `Boxing.copyReceiver(kind, recvVal)` (formation-time snapshot).
9. `GoCalls.callArgs`: if `bound != null && bound.length > 0` and the
   target root is a GoFuncRootNode with a STRUCT/ARRAY receiver kind →
   copy `bound[0]` per invocation via `Boxing.copyReceiver(kind, bound[0])`.
   (Covers method values, deferred method values via DeferredCall, and
   reflect MethodVal calls. Plain functions have null boundArgs — one null
   check on the hot path.)
10. `ReflectCalls.invokeCallable` BoundMethodVal branch: if
    `!bmv.method.pointerReceiver` → prepend
    `Boxing.copyReceiver(receiverKind, bmv.receiver)` per call, kind from
    `bmv.method.funcType.ins.get(0).kind` (guard missing/empty ins).
11. `GoFuncRootNode` comment correction (see 7).

## Tests (red first, then green; all in the sparse tree)

- `SliceGrowthTest` (truffle test tree, registered in Runner):
  `testGrowsliceStructElementsCopied`, `testGrowsliceArrayElementsCopied`,
  `testSlicecopyStructElementsCopied`, `testSlicecopyOverlapStructSafe`
  (overlap copy keeps values), `testSlicecopyInterfaceElementsShareData`
  (negative: `[]any` copy shares struct data — pins the header-only rule).
  Direct `Intrinsics.growslice/slicecopy` calls with GoStruct elements.
- `ArrayValueSemanticsTest` (or new `ValueCopyBulkTest` in same package):
  `testAppendNodeStoreCopiesStruct` — needs a frame + AppendNode; check
  harness cost, else cover via Go repro below.
- `IfaceDispatchNodeTest` (interop, IfaceTestRunner): value-receiver target
  (`pointerReceiver=false`): `testStructArgIsCopied` (struct arg mutated
  by CaptureRoot-visible copy — assert received arg `!=` original and
  original unchanged after mutating the received copy),
  `testValueReceiverIsCopied` (struct receiver `!=` original),
  `testPointerReceiverIsShared` (negative: pointer receiver passes the
  same object), `testPromotedValueReceiverCopied`.
  Existing `testPromotedValueReceiverDeref` uses a plain-Object referent
  (not GoStruct) → unaffected by the copy (identity assert still holds).
- `GoCalls` bound-receiver copy: unit test via a GoFuncRootNode target?
  Prefer the Go-level repro (below) + a small unit test if a cheap root
  construction exists.
- Go-level repros (run under graalgo vs gc in /var/tmp with the copied
  gofrontend @a0df481b + rebuilt classes): append-struct-then-mutate,
  growing-append aliasing, copy(dst,src) struct aliasing, iface call with
  struct arg mutated by callee, method value value-receiver mutated +
  repeated calls independent, deferred method value, interface holding
  struct through iface dispatch. Each must FAIL before the fix and PASS
  after (run red set against baseline classes first).
- Suites: `SingleTestLauncher` for the new/changed classes; interop
  `IfaceTestRunner` (full); truffle Runner subset for
  SliceGrowthTest/ArrayValueSemanticsTest/BoxingArrayStorageTest/ValuesTest
  (corpus tests need gofrontend+repo corpus — run only if the harness
  cooperates in the shadow; full `make test` is 30+ min and NOT required
  for this lane — full-tree gates are owed at integration per the R03-T3
  precedent). `make snapshot-check` needs the full tree — NOT runnable from
  the sparse shadow; flag as owed-at-integration.

## Verification gates before reporting

- `javac -Xlint:deprecation -Werror` clean on all three modules (joint round).
- New tests RED on baseline classes, GREEN after fix (record both runs).
- Go repros RED before / GREEN after (record outputs vs gc).
- No other test regressions in the run subsets (record counts).
- `git diff --stat` + commit in the worktree (explicit pathspecs) when
  quota allows; report for independent review. DO NOT integrate/push/rebase.

## Open questions for the reviewer / coordinator

- Q1: `copyForAssign(INTERFACE, gi)` deep-copies interface data. Direct-call
  path already does this; this lane makes iface dispatch consistent with it.
  Is the deep-copy-data invariant itself gc-correct? (Counter-scenario:
  `p := i.(*S); p.F = 1` after passing `i` — gc shares data, graalgo's
  copy would not.) If wrong, it is a separate finding spanning both paths.
- Q2: `copyForAssign(STRUCT, GoMutex)` shares the mutex while
  `deepCopyArray` fresh-instances mutex elements — inconsistent; out of scope.
- Q3: `Linknames.callGo` struct-arg aliasing + `ReflectOpNode.unboxArgs`
  verbatim args — same family, follow-up lanes.
- Q4: home quota CLOSED for the whole run so far — heartbeat cannot be
  refreshed (EDQUOT on ~1 KB writes); copy-back + commit + goal-files
  updates are all pending a quota window. If no window opens, the finished
  diff + logs stay in /var/tmp (reboot-volatile) — flag loudly in the report.

## GREEN phase (successor lane, 2026-11-10 ~04:15–04:35Z)

Fix implemented in the shadow /var/tmp/f34 per §Fix design, all 11 items:
1. Boxing.copyReceiver(kind, value) added (public) + copyForAssign made
   public (runtime/interop callers). GoPointer referents dereferenced first;
   GoInterface re-boxed with copied data; kind==null infers from value.
2. AppendNode.store default branch -> copyForAssign(elemKind, v).
3. AppendNode ellipsis spread: same-type -> Intrinsics.memmoveCopy(...,
   elemKind); different-type -> copyForAssign per element.
4. Intrinsics.memmoveCopy added (public); growslice:257 + typed-memmove
   default branch use it. STRUCT/ARRAY deep-copy element-wise
   (overlap-safe via snapshot); INTERFACE header-only (raw arraycopy).
   Untyped memmove untouched (ReflectLinknames byte copy stays raw).
5. IfaceDispatchNode.boxArg -> copyForAssign(arg.kind(), boxed).
6. IfaceDispatchNode.dispatch receiver copy after addrOf block, guarded by
   !pointerReceiver && !addrOf && !receiverParamIsInterface.
7. GoFuncRootNode.receiverKind() added; comment at :191 corrected to the
   real contract (call sites copy; root only clones atomic cells).
8. MethodValueNode formation copy when root receiverKind is STRUCT/ARRAY.
9. GoCalls.callArgs per-invocation copy of bound[0] when the target root
   is a GoFuncRootNode with STRUCT/ARRAY receiver kind. Only MethodValueNode
   produces boundArgs (verified by grep); DeferredCall goes through
   GoCalls.invoke, so deferred method values are covered.
10. ReflectCalls BoundMethodVal branch: per-call copy when
    !pointerReceiver and funcType.ins[0].kind is STRUCT/ARRAY (null/empty
    ins guarded).
11. Comment correction (see 7).

Build: runtime joint round rc=0, interop main rc=0, truffle stubs rc=0
(-Werror throughout). truffle `make main/test` rc=2 ONLY on the
gofrontend rebuild step (no internal/frontend/ in the sparse shadow; the
prebuilt /var/tmp/f34/gofrontend @a0df481b is untouched). Test classes
compiled directly with the Makefile's javac line instead.

Verification (fix classes, shadow):
- SliceGrowthTest 7/7 PASS (RED was 5/7 fail).
- ValueCopyBulkTest 4/4 PASS (RED was 4/4 fail).
- MethodValueCopyTest 6/6 PASS (RED was 3/6 fail).
- ArrayValueSemanticsTest 3/3, ValuesTest 15/15 PASS.
- BoxingArrayStorageTest 2/3: testByteSliceStringRoundtrip FAILS with
  IOException "Cannot run program gofrontend" — identical on BASELINE
  classes (shadow walk-up cannot find the binary); pre-existing
  environmental, not a regression.
- IfaceTestRunner full: 198 tests, 52 failures. BASELINE (same test
  classes, baseline mains): 198 tests, 58 failures. Zero regressions:
  the 6 delta tests are exactly the new RED tests now passing
  (BoundMethodCopyTest 1 + IfaceDispatchNodeTest 5). The 52 remaining
  failures are pre-existing shadow-environment failures (GoNetpollTest 17,
  ChanBuilderTest 24, RangeConcurrentRegressionTest 5, LinknameEndToEndTest
  2, GoOsErrnoTest 3, IfaceMethodTablesTest 1).
- Go repros r1-r9 under graalgo: run in progress (log
  /var/tmp/f34-lane/logs/fix-graalgo-repro.log); gc baseline in
  /var/tmp/f34-lane/logs/gc-repro.log.

Open questions answered from code (not assumption):
- Q1: CONFIRMED suspect. FuncValueCallNode:101-102 copies INTERFACE args
  via copyForAssign(INTERFACE), which deep-copies struct/array data.
  gc copies only the (type, data-pointer) header, so `f(i)` where f
  type-asserts to *S and mutates would be visible to the caller under gc
  but hidden by graalgo's copy. The invariant is likely WRONG vs gc and
  spans both paths (direct + iface dispatch, now consistent). Separate
  finding for the coordinator — NOT diverged on in this lane.
- Q2: CONFIRMED inconsistent. GoStruct.deepCopy() does NOT fresh-instance
  GoMutex/GoRWMutex/GoWaitGroup/GoOnce/GoPool fields (shared across a
  struct copy), while deepCopyArray() fresh-instances them per slot and
  freshZeroCopy() fresh-instances them too. Out of scope; follow-up.
- Q3: CONFIRMED follow-ups. Linknames.callGo (runtime/Linknames.java:133)
  forwards args verbatim to GoCalls.invoke; ReflectOpNode.unboxArgs
  (p3reflect/ReflectOpNode.java:1374) unboxes reflect Call args verbatim.
  Same family, separate lanes.
- Q4: home quota STILL CLOSED at 04:30Z (8 KiB probe EDQUOT). Copy-back of
  PLAN.md + 7 test files + 8 fixed sources, commit, registry heartbeat
  all pending a quota window. Everything durable lives only in /var/tmp
  (reboot-volatile) + this shadow worktree.

## GREEN phase, continued (2026-11-10 ~04:35-04:40Z)

Go repros under graalgo (fix classes) vs gc — ALL 9 GREEN, byte-identical
to gc (log /var/tmp/f34-lane/logs/fix-graalgo-repro.log; gc baseline
/var/tmp/f34-lane/logs/gc-repro.log):
- r1 append-struct: v.F: 1
- r2 grow-append: s[0].F: 1 t[0].F: 99 len: 2 cap: 2
- r3 copy-struct: n: 2 src[0].F: 1 dst[0].F: 99
- r4 iface-arg: v.F: 1
- r5 method-value: call1: 99, call2: 99, s.F: 1, call3: 1, s.F: 2
  (re-run without head truncation to confirm the final s.F: 2 line)
- r6 iface-ptr-held: s.F: 1
- r7 defer-methodvalue: in run s.F: 5, last: 1
- r8 append-spread: src[0].F: 1 out[0].F: 99
- r9 pointer-receiver negative control: s.F: 99 (gc agrees)
