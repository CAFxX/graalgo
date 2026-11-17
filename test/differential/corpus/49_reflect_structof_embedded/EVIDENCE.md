# P1-4 evidence — review-1616, `reflect.StructOf` embedded types (resume lane r2)

Lane: `review-1616-p1-reflect-embedded-resume-r2` · base master `a0df481b` ·
branch `wt/graalgo-independent-codebase-review/review-1616-p1-reflect-embedded` ·
2026-11-16. DO NOT integrate — master is red; integration is the coordinator's job.

## 1. Verdict

The P1-4 premise as stated is **refuted**. The real defect — missing method
promotion for embedded types in `reflect.StructOf` products — is **fixed and
verified** in the backend table. Corpus 49 runs rc=0 under the fixed build:
**22 of 29 output lines byte-identical to the gc 1.27.1 oracle**; the 7
diverging lines share one root cause (missing uncommon-type data in the abi
shells) that is a separate follow-up feature, documented in §4. Corpus 47
passes (rc=0, unchanged). `make check` rc=0. No-bridges check passed.

## 2. The refuted premise (gc evidence)

The review claimed the three panics in (retired) `synth_reflect.go:2646-2668`
were a gap. gc 1.27.1 panics with the **identical messages in the identical
cases** (Issue 15924, `reflect/type.go:2256-2420`):

- `reflect: embedded type with methods not implemented if there is more than
  one field` (case K, V8)
- `reflect: embedded type with methods not implemented if type is not first
  field` (L, M)
- `reflect: embedded type with methods not implemented for non-pointer type`
  (V3, V4)

Verified by two independent gc probe runs (2026-11-16, go1.27.1) and by the
corpus-49 gc oracle (`/var/tmp/p1-49-gc.out`, 29 lines, rc=0). Those panics
are gc-faithful, not a gap. The panics are delivered by **real gc reflect
code running under graalgo** (synthetic reflect is retired — see §7), so
graalgo inherits them exactly once the `Uncommon()` gate is fixed (§4).

## 3. What was fixed (this lane)

All in `internal/backend/interop/src/main/java/graalgo/p3reflect/`:

1. **Method promotion** (`ReflectTypes.promoteMethods`, `methodSetFor`):
   embedded interfaces promote as stubs (2); the pointer branch appends elem
   methods for ANY elem kind (gc appends for non-struct elems — `*MyInt` →
   3 methods `[Get Ptr Get]`); the non-pointer branch promotes any kind with
   methods (named int/chan/func — `MyInt`/`MyChan`/`MyFunc` → 1 method each).
   `methodSetFor` dropped its `.sort()` — gc append order
   (`*E` → `[Get Ptr Pub unexp Get Pub unexp]`).
2. **Embedded-interface stubs** (`interfaceStubMethods`,
   `makeStructOfIfaceStub`, `interfaceMethodsFromWire`): promoted interface
   methods carry the true signature (resolved from the wire type-table entry
   via `cachedTypes` + `tokForWireId`) and panic on call with gc's exact
   message `reflect: StructOf does not support methods of embedded
   interfaces`. Every promoted method reports PkgPath `""` (gc-observed,
   including unexported). A NAMED interface entry carries no methods — they
   live on its underlying interface entry; an ALIAS entry carries none either
   (they live on its rhs). `interfaceMethodsFromWire` follows
   underlying/rhs links (capped 8 hops).
3. **`implementsDetailed`** now uses the computed `methodSetFor(t.tok)`
   instead of the raw table entry (StructOf products carry no methods in the
   entry). NOTE: this path is only reachable from the `tImplements`
   reflectop (synthetic/intrinsic callers); real reflect's `Implements` runs
   gc's `implements()` over the abi shells — see §4.3.
4. **Bounds checks** (`ReflectOpNode.methodChecked`): `method()`,
   `methodVal()` and the `typeMethod` dispatch raise gc's
   `reflect: Method index out of range` (verified in gc `type.go:651`,
   `value.go:1915`) instead of a Java `IndexOutOfBoundsException`.
   `methodVal()` now uses the computed method set.
5. **`boxType` corrected** (pre-existing defect, not the lane's): it boxed a
   `reflect.Type` value as the bare token under descriptor `reflect.rtype`
   (value — the named rtype entry has no methods, so no itab was installed
   and the first `Method.Type` dispatch died with `interface dispatch: no
   method table installed for dynamic type "reflect.rtype"`). The correct
   shape is `*reflect.rtype` holding the abi.Type view struct
   (`AbiTypes.abiPointerForTok(tok).referent()` — the canonical memoized
   shell). Two wrong shapes were tried and are documented in the `boxType`
   javadoc: descriptor `*reflect.rtype` with the bare token Long (died with
   `ClassCastException: Long cannot be cast to GoStruct` because gc's
   `Kind()` reads `t.t.Kind()` off the struct field). This was the
   corpus-49 blocker: corpus 49 printed nothing before the fix; it runs
   rc=0 after.
6. **`structOf` javadoc** corrected (false "shallower wins / only exported"
   claims replaced with gc-verified rules).

Verification (2026-11-16, final build, `-Xmx1024m -Xms256m`):

- corpus 47: rc=0 (regression-free, re-run after `make check`).
- corpus 49: rc=0, 22/29 lines byte-identical to the gc oracle; output stable
  across re-runs and recorded in `test/snapshots/go1.27.1.json`
  (`49_reflect_structof_embedded`).
- `make check`: rc=0. `check_no_bridges.py`: passed.
- gc probes: two independent subagents (probe 1, probe 2, 2026-11-16) plus
  `probeU.go` (§6) and `probeC9.go` (§4.3) — all run under go1.27.1.

## 4. Remaining divergences (corpus 49 vs gc oracle) — one root cause

Diverging lines: **K, L, M, V3, V4, V8** (graalgo `NO PANIC`, gc panics —
§2) and **IMP** (`IMP Get: true Nope: true` vs gc `Get: true Nope: false`).

### 4.1 Root cause: abi shells carry no uncommon-type / method data

gc's `StructOf` panic branches are gated on `ptr.Uncommon()` /
`ft.Uncommon()` (`Mcount > 0`, `len(fields) > 1`, `IsDirectIface()`), and
`Uncommon()` (`internal/abi/type.go`) returns nil unless
`TFlag&TFlagUncommon` (bit 0) is set. Graalgo's
`AbiTypes.fillTypeFields` sets TFlag = named?`1<<2`:0 |
directIface?`1<<5`:0 — **bit 0 (TFlagUncommon) is NEVER set**, so
`Uncommon()` returns nil for every type and the panic branches never fire.
The wire confirms the shape: `abi.Type` has 11 fields (no uncommon tail);
`abi.UncommonType` exists (`PkgPath, Mcount, Xcount, Moff, _`);
`abi.StructType` embeds `Type` + `PkgPath` + `Fields` (no `u` field);
`abi.Method` exists.

### 4.2 Follow-up design (uncommon-type support — separate lane, do NOT
### attempt as a blind fix here)

Two options, both needing a design decision:

- **(a) Mint shells with a wider synthetic descriptor** (kind-struct +
  `u UncommonType` tail, so
  `(*structTypeUncommon)(unsafe.Pointer(t)).u` reads a real field) +
  set TFlagUncommon + fill the Methods slice (whose `Methods()` accessor
  computes from Moff — needs design).
- **(b) Frontend interception of `Uncommon()`** returning a synthesized
  `UncommonType` built from the ReflectTypes table (needs `Methods()`
  support too — it reads a slice from memory at Moff).

Note the coupling: gc's `StructOf` builds the product's uncommon data
itself (allocating a `structTypeUncommon` / synthetic struct with a trailing
Method array via `New(StructOf(...))` and `copy(...)` — raw memory-layout
tricks). Any fix must compose with that code path, not fight it.

### 4.3 The IMP line: same root, both sides

gc's `implements(T, V)` (`reflect/type.go:1488`): `if len(t.Methods) == 0
{ return true }` — the interface shell's Methods list is empty under
graalgo (`AbiTypes.fillExtension`, INTERFACE case: "Methods left empty
(V1)"), so the check is vacuously true for every interface. Then, for a
concrete V, it needs `V.Uncommon()` — nil under graalgo → false. Verified
by `probeC9.go`: gc `E implements Nope: false, Get: true` (both NumMethod
1); graalgo both NumMethod 0, both `implements` true. **Do NOT fix one side
alone**: populating interface Methods without `Uncommon()` would make
`implements()` proceed to the nil-Uncommon branch and return false for
BOTH Get and Nope — flipping the currently-correct Get to wrong. The two
sides must land together, as part of the §4.2 feature.

## 5. Assert-path gap (verified from code)

`NodeFactory.dynamicMethodSets` (`internal/backend/truffle/.../NodeFactory.java:2184`)
builds the map ONLY from wire entries of kind `named` and `pointer`.
A StructOf product is runtime-created — it has NO wire entry — so
`TypeAssertNode.holds()` gets the empty set and returns **false** for any
non-empty interface assertion on a StructOf product value, where gc
succeeds. A follow-up fix must register the product's method set under its
descriptor name at StructOf time (the map is built once per build ctx and
cached — a runtime-mutable registry or a fallback to the ReflectTypes table
is needed). Not fixed in this lane.

## 6. Static-type gaps (verified by live gc-vs-graalgo probes)

### 6.1 Static-interface NumMethod = 0 (pre-existing)

`reflect.TypeOf((*Iface)(nil)).Elem().NumMethod()` is **0** under graalgo
vs **1** under gc (`probeC9.go`, `isolate.go`). `methodSetFor` returns empty
for INTERFACE kind (`t.methods` empty, kind != STRUCT). Interface TypeInfos
carry only implkeys, no MethodInfo list. Building MethodInfos for interface
methods needs a GoFunc that does not exist (abstract methods); only the
`implementsDetailed` u-side (implkeys) was in scope. Separate follow-up.

### 6.2 Static-type unexported-count divergence (pre-existing, `probeU.go`)

gc's `NumMethod` returns the **exported** count (Xcount), and `Method(i)`
indexes exported methods only; `MethodByName` on an unexported method
returns ok=false. Verified:

```
gc:     E NumMethod: 2, *E NumMethod: 2, MethodByName("unexp") ok=false
graalgo: E NumMethod: 3, *E NumMethod: 3, MethodByName("unexp") ok=true (PkgPath "<unknown>")
```

Root cause: graalgo's `methodSetFor` returns ALL methods (including
unexported) and `NumMethod`/`Method` count/index them all — Mcount and
Xcount are conflated. Contrast with StructOf products, where gc sets
`ut.Xcount = ut.Mcount` (`reflect/type.go`), so ALL promoted methods
(including unexported ones) count — corpus 49's `numMethod=3 [Get Pub
unexp]` is correct. Fix shape: an exported-only view for static types, a
full view for StructOf products — but that touches the shared method-set
contract (`NumMethod`, `Method`, `MethodByName`, `methodVal`,
`implementsDetailed`), so it needs its own lane with regression coverage,
not a change riding on this one.

## 7. Synthetic reflect is retired (changes what is live)

`internal/frontend/synth_machinery.go` has `"reflect"` commented out of
`syntheticSources` (`RETIRED - uses disk importer`); real reflect now
compiles from GOROOT via the disk importer. `synth_reflect.go` is compiled
only as the `"reflect_m7"` helper package (M7 `__`-helpers + pair types).
`reflectCheckEmbedded`'s only caller is the retired synthetic `StructOf`.
Edits to `synth_reflect.go` in this area are dead code — the lane's earlier
comment-only edits there were reverted. The K/L/M/V3/V4/V8 and
interface-unexported behaviors are delivered by real gc reflect code
running under graalgo. Proof from the wire: gofrontend on probeC6 produced
a 112 MB wire JSON; the `Kind` funcdecl body is
`return Kind(t.t.Kind())` — real gc reflect (`type rtype struct { t
abi.Type }`, `toRType(t *abi.Type) *rtype =
(*rtype)(unsafe.Pointer(t))`, `StructOf` returns
`addToCache(toType(&typ.Type))`, go1.27.1).

## 8. Reproduction

```
# build (clone /var/tmp/p1-structof — tmpfs, lost on reboot; see §9)
cd /var/tmp/p1-structof
nice -n 19 env TMPDIR=/var/tmp GOCACHE=/var/tmp/p1-gocache GOPATH=/var/tmp/p1-gopath \
  JAVA_TOOL_OPTIONS="-Xmx1024m -Xms256m" timeout 900 make build
# run corpus 49; diff vs gc oracle
nice -n 10 env TMPDIR=/var/tmp GOCACHE=/var/tmp/p1-gocache GOPATH=/var/tmp/p1-gopath \
  JAVA_TOOL_OPTIONS="-Xmx1024m -Xms256m" timeout 300 \
  internal/backend/graalgo-run test/differential/corpus/49_reflect_structof_embedded/main.go
# gc oracle: go run with GOCACHE=/var/tmp/p1-gocache GOPATH=/var/tmp/p1-gopath
```

Heap note: `-Xmx512m` is TOO SMALL (JVM OOM in `Main.runGofrontend`,
`Main.java:541`); use `-Xmx1024m -Xms256m`. `graalgo-run` sets no `-Xmx`;
the heap comes from `JAVA_TOOL_OPTIONS`.

## 9. Durability / handoff

- This clone lives on tmpfs (`/var/tmp/p1-structof`) and is lost on the next
  reboot. The durable record is the pushed branch (tip SHA in the lane's
  final report) plus the rescue staged under
  `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/review-1616-p1-reflect-embedded/`
  (patch + gc oracle + this note).
- Files changed: `ReflectTypes.java`, `ReflectOpNode.java`,
  `test/differential/corpus/49_reflect_structof_embedded/main.go` (new),
  `test/snapshots/go1.27.1.json` (+11 lines, single-program record),
  this file (new).
- Snapshot semantics: the snapshot records **graalgo's** behavior
  (`graalgo-behavior-snapshot/v1`), so corpus 49's record is graalgo's
  current output (22/29 gc-identical). The gc-divergence is tracked by the
  differential corpus vs the gc oracle, not by the snapshot. When the §4.2
  feature lands, the snapshot record must be updated (K/L/M/V3/V4/V8 and
  IMP will change).

## 10. Green-run exclusion (fixup for re-review B2, 2026-11-17)

Independent re-review of `c37d74ca` REJECTed on B2: the green run
(`run.sh` stage [4/5], CI run 43998294054) compares gc vs graalgo live and
fails on corpus 49 line 16 (K: gc panics, graalgo `NO PANIC`). Option (a) --
landing the section 4.2 uncommon-type follow-up in this fixup lane -- was
assessed and NOT taken: it needs a design decision (section 4.2 options
(a)/(b) are both open), wider synthetic descriptors with a real
`u UncommonType` tail and a materialized method array, interface `Methods`
population, and both sides together (section 4.3); it touches every abi
shell, and this lane has no local build capacity to verify it (home quota
EDQUOT, /var/tmp 95% full). A narrow backend-side panic check was also
ruled out: the corpus exercises real gc `StructOf` (the `tStructOf`
reflectop is reachable only from the retired synthetic reflect), so such a
check would be dead code on this path. Corpus 49 is therefore excluded
from the green run via the established mechanism (`backends-p3.json`
`exclusions` + snapshot `excluded` mirror, identical reason; the 49
snapshot record is removed, as excluded programs carry no record -- see
`snapshot.go`, which would otherwise report the record as drift). The
corpus program itself is unchanged and still documents the 7 divergences;
when the section 4.2 feature lands, remove the exclusion and re-record
the snapshot (K/L/M/V3/V4/V8 will panic, IMP will read
`Get: true Nope: false`).
