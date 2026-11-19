# ABI spike: reflect/`internal/abi` on the JVM value model (P1) and the syscall boundary (P2)

**Status:** spike report, 2026-11-18, lane `wt/recovery/abi-spike`. The
owner accepted its proposals the same day: §3 is applied to
[toolexec-design.md](toolexec-design.md) (its §14), and §4's decisions are
SPEC amendments (`GG-0061`, since re-issued as GG-0072, and GG-0062–GG-0064, recorded in
[ADR-0009](../dev/adr/ADR-0009-recovery-spec-amendments.md)). Requirement IDs
quoted below in code font are the ones in force when the report was written.
It answers the two prototype questions of
[toolexec-design.md](toolexec-design.md) §12 (P1, P2) behind risks R1 and R2
of its §10. [SPEC.md](SPEC.md) is authoritative; this document proposes
amendments (§4) and decides nothing by itself. All numbers were measured on
go1.27.1 linux/amd64 on this box; anything else is labelled an estimate.

| | Verdict | Mechanism | Effort (agent-days, estimate) |
|---|---|---|---|
| **P1** | **Go** for verbatim `internal/abi` + `internal/reflectlite` + `reflect`. The fallback (150–200 patched functions) is not needed. | Byte-exact gc-layout descriptor images + a layout-addressed pointer model (§1.3) | 14–20 for W9 (was "37–200 intrinsics", unsized) |
| **P2** | **Go** for the role table: it covers every pointer shape std passes, nested pointers included. One conflict with SPEC §3.5 needs a decision (§2.5). | Per-number role table naming Go types + one Panama downcall to libc `syscall(2)`, copy in/out through a call-scoped arena (§2.3) | 10–15 for W8 incl. poller and process spawn; 5–7 of it for M1c |

## 1. P1 — verbatim reflect on handle-table / descriptor views

### 1.1 What the source actually does (static inventory)

Tool: [unsafescan](../../test/spikes/abi/unsafescan/main.go) type-checks the
packages from `GOROOT/src` and assigns every occurrence to exactly one
pattern (type-directed, not textual). Columns: `internal/abi`,
`internal/reflectlite`, `reflect`, `sync`, `internal/sync`, `sync/atomic`,
then the sum over all 13 scanned packages (the six named plus `strings`,
`bytes`, `fmt`, `encoding/json`, `sort`, `slices`, `internal/unsafeheader`).

| Pattern | abi | rlite | reflect | sync | i/sync | atomic | all | Mechanism |
|---|---|---|---|---|---|---|---|---|
| `unsafe.Pointer` in type position (no operation) | 13 | 24 | 135 | 8 | 3 | 12 | 197 | none |
| `unsafe.Pointer(&x)` | 3 | 3 | 52 | 19 | 17 | 15 | 113 | (a) |
| `unsafe.Pointer(p)` from a typed pointer | 35 | 15 | 100 | 9 | 6 | 8 | 174 | (a) |
| `unsafe.Pointer(uintptr(p) + n)` | 3 | 1 | 1 | 1 | 0 | 0 | 6 | (a) |
| `unsafe.Pointer(integer)` not derived from a pointer | 1 | 0 | 3 | 0 | 0 | 0 | 4 | (a)¹ |
| `(*T)(p)`, T a type descriptor struct | 28 | 11 | 93 | 0 | 0 | 0 | 132 | (b) |
| `(*T)(p)`, T an interface/slice/string header | 1 | 5 | 31 | 0 | 0 | 0 | 37 | (b) |
| `(*T)(p)`, T scalar or array of scalars | 3 | 5 | 83 | 0 | 0 | 0 | 91 | (a) |
| `(*T)(p)`, T pointer/string/slice/interface/func/map/chan | 1 | 17 | 59 | 3 | 0 | 2 | 82 | (a) |
| `(*T)(p)`, T another struct | 4 | 0 | 0 | 2 | 2 | 12 | 21 | (a)/(b)² |
| `uintptr(p)` | 4 | 1 | 12 | 5 | 0 | 0 | 27 | (a) token |
| `unsafe.Add` / `add()` | 0 | 1 | 29 | 0 | 0 | 0 | 30 | (a)³ |
| `unsafe.Slice/String/SliceData/StringData` | 3 | 2 | 8 | 0 | 0 | 0 | 15 | (a) node kinds |
| `unsafe.Sizeof/Offsetof/Alignof` | 18 | 0 | 2 | 3 | 0 | 0 | 24 | frontend constant |
| atomic op on an `unsafe.Pointer` word | 0 | 0 | 0 | 3 | 0 | 0 | 3 | (a) |
| body-less function (linkname/assembly) | 2 | 7 | 70 | 16 | 8 | 41 | 144 | (c) by definition |
| field read/write on a descriptor struct | 67 | 50 | 449 | 0 | 3 | 0 | 569 | (b) |
| arithmetic past the end of a descriptor struct or into name bytes | 6 | 1 | 0 | 0 | 0 | 0 | 7 | (b) |

(a) = handled by the handle model (object identity + byte offset); (b) =
needs a typed view over synthesized descriptor/header memory; (c) = must be
an intrinsic or a patched function. `strings` has 4 operations (Builder's
`unsafe.String`/`StringData` and its `noescape` self-pointer), `fmt` 1
(`uintptr` of a pointer for `%p`), `slices` 9 (overlap tests), `bytes`,
`sort`, `encoding/json` none.

¹ `abi.go`'s register-bitmap helper and three `(*byte)(unsafe.Pointer(new(uintptr)))`
allocations in type constructors — all derive from a live object.
² 12 of the 21 are `sync/atomic.Value`'s `efaceWords`: an interface header
operated on one word at a time with atomics, with a non-descriptor sentinel
pointer stored in the type word. It needs the interface header view to keep
two raw words; if that proves awkward, `atomic.Value`'s four methods are the
one candidate for a patch outside the call path.
³ 19 of reflect's 29 are in the call-frame code (below).

Per function (declared functions and methods; strongest mechanism any of
its operations needs):

| Package | funcs | no unsafe op | (a) only | (b) | body-less (c) |
|---|---|---|---|---|---|
| `internal/abi` | 83 | 51 | 8 | 22 | 2 |
| `internal/reflectlite` | 68 | 40 | 11 | 10 | 7 |
| `reflect` | 393 | 184 | 59 | 80 | 70 |
| `sync` + `internal/sync` + `sync/atomic` | 205 | 101 | 39 | 0 | 65 |
| `strings`, `bytes`, `fmt`, `encoding/json`, `sort`, `slices` | 574 | 570 | 4 | 0 | 0 |

Two facts decide the design:

1. **The layout-dependent code is small and concentrated.** All arithmetic
   that leaves a descriptor *struct* (trailing `UncommonType`, method array,
   `FuncType` parameter array, name bytes) is in **7 sites in 7 functions**:
   `abi.(*UncommonType).Methods/ExportedMethods`, `(*FuncType).InSlice/OutSlice`,
   `Name.DataChecked/Data` and reflectlite's copy, plus `(*Type).Uncommon`'s
   wide-struct casts. Everything else is 569 plain field selections and 132
   pointer conversions between descriptor struct types. A view that is
   byte-exact makes all of them work without a per-site rule.
2. **reflect's (c) set is the declared runtime contract, not hidden
   coupling.** 70 body-less functions: 34 runtime linknames, 33 `badlinkname`
   self-aliases (resolved by D9 rule 1), 3 assembly. On
   top, one cluster cannot be served by any view because it manufactures gc
   call frames: `reflect/abi.go` (15 functions), `Value.call`, `callReflect`,
   `callMethod`, `methodReceiver`, `storeRcvr`, `funcLayout`,
   `makeMethodValue`, `moveMakeFuncArgPtrs` — 23 Go functions, of which
   **4 need a patch** (`Value.call`, `callReflect`, `callMethod`,
   `makeMethodValue` call an invoke intrinsic with `[]Value`); the other 19
   become unreachable and stay verbatim.

### 1.2 What the existing backend does with the verbatim source (dynamic evidence)

The v1 disk-import path already compiles `reflect`, `internal/reflectlite`
and `internal/abi` **unmodified from `GOROOT/src`** (synthetic reflect was
retired on 2026-10-16) and runs them on an object-graph model: descriptors
are synthesized as nested `GoStruct`s
([AbiTypes.java](../../internal/backend/interop/src/main/java/graalgo/p3reflect/AbiTypes.java))
and each kind of `unsafe` reinterpretation gets a view rule
([PrefixView.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/PrefixView.java),
[HeaderView.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/HeaderView.java),
[UnsafeToPtrNode.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeToPtrNode.java)).
About 11,400 lines of Java live in `p3reflect/` and `p3unsafe/`.

[reflectprobe](../../test/spikes/abi/reflectprobe/main.go) asks 126
questions across the reflect API (type headers, names, navigation, methods,
relations, constructors, value reads and writes, maps, calls, package
algorithms), one process per check, gc as oracle
([run.sh](../../test/spikes/abi/reflectprobe/run.sh)). Result on the
integrated tree `8792d9b1`
([recorded output](../../test/spikes/abi/reflectprobe/testdata/v1-result-8792d9b1.txt)):

**76 of 126 checks (60 %) agree with gc.** The 50 failures:

| Failure class | Checks | Typical symptom |
|---|---|---|
| Memory model: byte offset into a nested struct, aggregate copy through an offset, view/representation confusion | 20 | `byte-offset derivation from a non-array referent traps`, `typedmemmove: aggregate store through a byte-offset view`, `ByteRef cannot be cast to GoStruct` |
| Descriptor content missing or wrong (methods, interface methods, `ChanDir`, func parameters, `PkgPath`, `[]byte`) | 18 | `NumMethod` 0, `Implements` always true, `<-chan int` printed as `chan int` |
| Type identity (a descriptor copy is not the canonical one) | 2 | `TypeOf(&x).Elem() != TypeOf(x)` |
| Linkname intrinsic arity drift against go1.27.1 | 3 | `reflect.makemap takes 1 arguments, got 2` |
| Other (`Call`, `MakeFunc`, `Select`, `TypeAssert`, `Slice`, `SetLen`, `IsZero`) | 7 | wrong value or panic |

Reading: the question "can verbatim reflect run on the JVM value model" has
a working existence proof for the easy 60 %, and its failures name the two
things the object-graph approach cannot give: **addressing by byte offset**
(40 % of failures) and **complete, canonical descriptors** (40 %). Both are
properties of the representation, not missing cases; adding view rules one
cast at a time is what produced 11 k lines and still traps on
`v.Field(i).Field(j)`.

### 1.3 Recommended mechanism: layout-addressed pointers over byte-exact descriptors

Three pieces, all consistent with SPEC §3.4/§3.6/§7.3 (handle = object
identity + offset; "exact `rtype`/itab layouts"):

1. **Descriptor images.** For every type the backend builds the descriptor
   gc's compiler would emit: the kind-specific `internal/abi` struct, the
   trailing `UncommonType`, parameter pointers and method array, and the
   separately allocated names, field arrays and interface-method arrays —
   as bytes plus a parallel reference array for the pointer-shaped words.
   `NameOff`/`TypeOff`/`TextOff` are IDs in a process-wide table with gc's
   `reflectOffs` numbering (negative, allocated on demand), which is the
   scheme gc's runtime itself uses for type data outside a module.
   `resolveNameOff`/`resolveTypeOff`/`resolveTextOff`/`addReflectOff` are
   table lookups. The image is built from the frontend's type table alone;
   the offsets of `abi.Type`'s own fields come from `go/types` applied to
   `internal/abi` (§3.4's layout table), so **no offset is hard-coded per Go
   release**.
2. **One pointer model.** An `unsafe.Pointer`, and any `*T` obtained from
   one by conversion, is `(object, byte offset)`. Every addressable object
   kind answers four operations in gc byte offsets — load/store scalar of
   N bytes, load/store pointer word: descriptor images (array reads), flat
   values (offset → slot through a per-type leaf table derived from the gc
   layout), arrays (offset / element size), and the three header shapes
   (interface: type word + data word; string: data + len; slice: data + len
   + cap). A field access through a view pointer is "load at
   `ptr.offset + Offsetof(field)`" — the constant the frontend already
   folds. Arithmetic that leaves the object traps (`GG-0016`; now GG-0062).
3. **A small intrinsic set** — exactly the body-less functions of §1.1 plus
   the four patched call functions.

Why this and not the two alternatives:

- *Object-graph views (v1):* measured above; does not converge.
- *Patch `rtype`/`Value` accessors into intrinsics (the §5.6 fallback):*
  the per-function table says 80 reflect functions need a view and 59 only
  the handle model; replacing them means re-implementing reflect's semantics
  (flag propagation, RO rules, panics with gc's messages) in Java, by hand,
  re-audited every release. The image builder is ~300 lines against a
  layout table and is checked mechanically against gc (below).

### 1.4 Prototype

[test/spikes/abi/descview/](../../test/spikes/abi/descview/) (standalone
Java + Go; no Truffle, no JVM flags; `run.sh` takes about a minute):

| Piece | Role |
|---|---|
| [fixture/fixture.go](../../test/spikes/abi/descview/fixture/fixture.go) | 25 root types (58 with their closure): structs with tags, embedding, unexported fields, methods on value and pointer receivers; pointers, slices, maps, arrays, directional channels, variadic funcs, anonymous structs, named and unnamed interfaces, basic types |
| [typetab](../../test/spikes/abi/descview/typetab/main.go) | the frontend's role: `go/types` + `types.SizesFor("gc","amd64")` → type table (kind, size, alignment, pointer bitmap, gc type string and link string, fields, method sets, flags) and the layout of `internal/abi`'s own structs. Never looks at compiler output |
| [Images.java](../../test/spikes/abi/descview/java/abispike/Images.java) | builds the descriptor images (205 blobs, 6,179 bytes for the 58 types) |
| [Mem.java](../../test/spikes/abi/descview/java/abispike/Mem.java), [Values.java](../../test/spikes/abi/descview/java/abispike/Values.java) | the pointer model; flat values (`long[]` + `Object[]`, SPEC §3.4) addressable by gc offset; interface/string/slice header views |
| [Abi.java](../../test/spikes/abi/descview/java/abispike/Abi.java), [Reflect.java](../../test/spikes/abi/descview/java/abispike/Reflect.java) | go1.27.1 `internal/abi` and `reflect` functions transliterated statement by statement onto the pointer model, each quoting its Go source: `Kind`, `Size`, `Elem`, `Key`, `Len`, `ChanDir`, `Uncommon`, `ExportedMethods`, `InSlice/OutSlice`, `Name.ReadVarint/Name/Tag/IsExported/IsEmbedded`, `rtype.String/Name/PkgPath/NumMethod`, `unpackEface`, `Value.Elem/Field/Index/Int/Uint/Float/Bool/String/Len/SetInt/SetFloat` |
| [imgcheck](../../test/spikes/abi/descview/imgcheck/main.go) | **gc's own unmodified reflect as the judge**: loads the Java-built images into real (mmap'd) memory, relocates pointers and registers offsets through `reflect.addReflectOff`, wraps each descriptor as a `reflect.Type`, and compares reflect's answers with its answers for the compiler-emitted descriptor of the same type; values are read through `reflect.NewAt(synthesizedType, &realValue)` |

Results ([run.sh](../../test/spikes/abi/descview/run.sh)):

- **607 / 607** answers of gc's reflect are identical on synthesized and on
  compiler-emitted descriptors: kind, size, alignments, `String`, `Name`,
  `PkgPath`, `Comparable`, fields (name, type, offset, tag, embedded,
  exported, `PkgPath`, `FieldByName` index paths, `VisibleFields`), methods
  (names and `Method(i).Type`, which goes through `FuncOf` over synthesized
  parameter descriptors), interface methods, `Elem`/`Key`/`Len`/`ChanDir`,
  func parameters and variadic bit, `PointerTo`, `SliceOf`, and 21 value
  reads/writes including nested fields, an embedded struct, an array
  element, `SetInt`/`SetFloat` through a pointer and the unexported-field
  panic.
- The fixed `abi.Type` header is **byte-identical to gc's for 25 / 25 root
  types in every field**: `Size_`, `PtrBytes`, `Hash` (SHA-256 of the link
  string as in `cmd/compile`), `TFlag` (ignoring gc's on-demand GC-mask
  bit), `Align_`, `FieldAlign_`, `Kind_`, `Equal` nil-ness.
- The transliterated code on the Java pointer model produced **427 answers,
  all identical to gc's** (the subset of keys it covers), and an
  out-of-object offset traps.
- Failability: a single corrupted field offset in the images is caught by
  gc's reflect (11 mismatches).
- Cost, indicative only (plain JVM JIT, unoptimized prototype, megamorphic
  interface calls, no Truffle): `ValueOf(x).Field(1).Int()` 33 ns against
  gc's 1.6 ns; `t.String()` 23 ns against 3.3 ns; `t.Kind()+t.Size()` folds
  to nothing on both. Image size: 107 bytes per type on average.

How the prototype routes the verbatim source: the **source-level** half
(real `reflect` compiled and run) is the v1 experiment of §1.2 — it is the
only compile path that executes today (the toolexec skeleton does not run
on the JVM, [toolexec-design.md](toolexec-design.md) §13). The
**mechanism** half is standalone because v1's nested-`GoStruct` model is
exactly what has to change, and v1 is scheduled for deletion (D11).
Transliteration by hand is the gap between the two; it is mechanical (every
Go statement maps to one model operation) but it is not a compiler run.

### 1.5 Fraction verbatim, and what is not

| | Functions | Verbatim | Patched | Intrinsic (body-less upstream) |
|---|---|---|---|---|
| `internal/abi` | 83 | 83 | 0 | 2 (`FuncPCABI0`, `FuncPCABIInternal`) |
| `internal/reflectlite` | 68 | 68 | 0 | 7 |
| `reflect` | 393 | 389 | **4** (`Value.call`, `callReflect`, `callMethod`, `makeMethodValue`) | 70, of which 37 need Java code (34 linknames + 3 assembly → 1 invoke intrinsic) |
| `sync`, `internal/sync`, `sync/atomic` | 205 | 205 | 0 (the `sync.Pool.indexLocal` patch of §5.5 is unnecessary: it is in-array arithmetic) | 65 |

So **99 % of reflect's functions (389 / 393) and 100 % of `internal/abi`
and `internal/reflectlite` stay verbatim**; the fork for this group is one
diff of 4 functions. The measured 60 % (§1.2) is what the *current*
representation delivers; the prototype shows the missing 40 % is reachable
without touching the Go source.

Not demonstrated by the prototype (sized in the estimate, in decreasing
risk):

1. **Descriptors made at run time by reflect** (`StructOf`, `FuncOf`,
   `MapOf`, `ArrayOf`, `SliceOf`, `ChanOf`, `PointerTo`). Verbatim reflect
   allocates an `abi.StructType` as an ordinary Go value and fills it; when
   such a pointer reaches the runtime (`Value.Interface`, `unsafe_New`,
   `typedmemmove`, map intrinsics) the backend must read the descriptor
   *through the same pointer model* and intern a backend type for it — the
   inverse of the image builder. imgcheck exercised `PointerTo`, `SliceOf`
   and `FuncOf` only on the gc side. Alternative: patch the constructors'
   allocate-and-fill tails into one "intern this shape" intrinsic (7 more
   patched functions). Recommendation: inverse reader first; patch only if
   map group layouts (`MapOf` builds the Swiss-table group struct) make it
   expensive.
2. **Writes through header views**: `packEface` stores the two words of an
   interface separately; `sync/atomic.Value` does it with atomics and a
   sentinel in the type word; `Value.SetLen` writes a slice header word.
   Each needs the addressed variable to tolerate a half-written header.
   Mechanical for cells; decide whether locals whose address reaches
   `unsafe.Pointer` are boxed as two/three raw words (frontend already flags
   address-taken locals).
3. **`typedmemmove`/`typedmemclr`/`typedslicecopy` over the leaf tables**
   (aggregate copies between two addresses). v1 fails exactly here; with
   leaf tables it is a loop over leaves.
4. **`uintptr` tokens** (27 sites): `Value.Pointer`, `UnsafeAddr`, `%p`,
   alignment tests, overlap tests in `slices`. Token table per spike 4;
   `uintptr(p) % align` must be answered from the offset.
5. **Map iteration types**: reflect's `MapIter` embeds
   `internal/runtime/maps.Iter`; the facade package (W5) must provide it.
6. **Performance under Truffle**: the indicative 15–20× over gc is before
   any inline caching; reflect-heavy code (`fmt`, `encoding/json`) will sit
   on this path. Needs a PE-friendly `Addressable` dispatch (one cached
   class per call site) — measure in W9's first week.

### 1.6 Effort (estimate)

| Work | Agent-days |
|---|---|
| Descriptor image builder in the backend from GIR `TYPES`/`TYPEDEFS`, incl. itab and code-pointer (`TextOff`) tables, GC masks, map type fields; imgcheck promoted to a gate over a 300-type corpus | 3–4 |
| Pointer model in the node classes: view pointers for `(*T)(unsafe.Pointer)`, offset → slot leaf tables for structs/arrays, header views with stores, traps | 4–6 |
| Runtime intrinsics over the model: `typedmemmove` family, `unsafe_New*`, `ifaceE2I`, `resolve*Off`, `addReflectOff`, `typelinks`, map/chan helpers (the 34 + 7 linknames) | 3–4 |
| Call path: invoke intrinsic + the 4-function patch; `MakeFunc`, method values | 2–3 |
| Inverse descriptor reader (run-time-constructed types) | 2–3 |
| **Total W9** | **14–20** |

This replaces the "15–30" of milestone M1d's reflect share and removes the
150–200-function fallback from the plan. The pointer-model item is shared
with W8 (P2 marshals the same handles) and with `sync`/`strings`.

### 1.7 How to rerun

```
export PATH=~/tools/go/bin:~/tools/graalvm-jdk-25/bin:$PATH JAVA_HOME=~/tools/graalvm-jdk-25
(cd test/spikes/abi/unsafescan && go run . [-funcs] [pkg ...])     # §1.1 tables, ~40 s
test/spikes/abi/descview/run.sh /some/scratch/dir                   # §1.4, ~1 min
make build graalgo && test/spikes/abi/reflectprobe/run.sh /some/scratch/dir   # §1.2, ~10 min, one JVM at a time
```

## 2. P2 — the syscall boundary

### 2.1 What std asks the kernel for (static inventory)

Tool: [sysscan](../../test/spikes/abi/sysscan/main.go) type-checks the
dependency closure of `os`, `net` and `os/exec` (`CGO_ENABLED=0`,
linux/amd64), finds every call of the eight raw entry points
(`syscall.Syscall/Syscall6/RawSyscall/RawSyscall6/rawSyscallNoError/
rawVforkSyscall/AllThreadsSyscall/AllThreadsSyscall6`) outside the runtime
family, and classifies each argument **from the typed call site itself**.

| | Distinct syscalls |
|---|---|
| issued anywhere by `syscall` + `internal/syscall/unix` (all wrappers) | **148** (213 call sites); the design's "~110" was an estimate |
| reachable from `os`'s functions (M1c) | 70, of which 17 only inside the fork/exec child path |
| reachable from `net`'s functions | 47 |
| `os` ∪ `net` | 85 |
| with at least one pointer argument | 89 of 148 |

Argument shapes over all 148 (per syscall, position and alternative):
443 plain values, 57 pointers to one fixed-size Go type, 45 C strings,
29 buffers with a length argument, 6 untyped pointers whose length is a
sibling argument (sockaddr, sockopt value), 2 caller-supplied words
(`fcntl` arg, `pidfd_send_signal` info).

Findings that shape the table:

- **Nested pointers exist in exactly two argument struct types in all of
  std: `syscall.Iovec` (`Base`) and `syscall.Msghdr` (`Name`, `Iov`,
  `Control`)** — plus `execve`'s `**byte` vectors, which are in the
  fork/exec child path. Every other pointee (33 types, 1 to 390 bytes:
  `Stat_t`, `EpollEvent`, `Timespec`, `RawSockaddrAny`, `Rusage`,
  `Utsname`, …) is flat. R2's worry is four rules, not a class of problems.
- **`epoll_*`, `select`, `mmap`/`munmap` are not issued by `os`/`net`
  code.** In gc, epoll lives in `runtime/netpoll_epoll.go`; `os` and `net`
  reach it only through `internal/poll`'s `runtime_poll*` linknames. `mmap`
  is only behind the `syscall.Mmap` API. (`time.now` is likewise the
  runtime's vDSO call, not `clock_gettime` from Go.)
- **Three numbers need a second key**: `ioctl` (request), `fcntl` (command)
  and `ptrace` (request) change the meaning of their last argument.
- **Five calls return a length through a pointer** (`recvfrom`,
  `getsockname`, `getpeername`, `accept4`, `getsockopt`): std passes a
  fixed 112-byte `RawSockaddrAny` for four of them, so a fixed-size struct
  role is exact; `getsockopt` needs a "length is behind argument N" role.
- The fork/exec child (`forkAndExecInChild1`, 17 syscalls incl. `clone`,
  `clone3`, `execve`, `setsid`, `capset`, `unshare`) cannot run in a JVM:
  the process is multi-threaded and the child would share the heap. This is
  the "fork/exec glue" patch the design already names.

### 2.2 Per-family executor choice

| Family | Choice | Why |
|---|---|---|
| Everything `syscall` issues by number (files, metadata, pipes, sockets, `getdents64`, `sendmsg`, …) | **Panama downcall to libc `syscall(2)`** | Verbatim `syscall`/`internal/poll`/`os` hold raw descriptor numbers and decode kernel structs (`Stat_t`, `linux_dirent64`, `Msghdr`) and exact `errno`s. JDK NIO exposes none of these: no channel can be made from an fd number, `Files` has no fd-based stat, errors are exceptions. One downcall handle of one shape (`(long×7)long`, `captureCallState("errno")`) serves every number — a single entry in native-image's foreign-call configuration |
| `setuid`/`setgid`/`setgroups`/… via `AllThreadsSyscall` | Panama downcall to the **libc wrapper** of the same name | process-wide credential changes must reach every thread; glibc's wrappers do that, raw `syscall(2)` on one carrier does not |
| Readiness waiting (`runtime_poll*`, 12 functions) | **decision needed, §2.5** | SPEC §3.5 says blocking NIO; verbatim `internal/poll` hands the runtime raw fds |
| Process creation (`forkExec`) | patched: `posix_spawn` of a small helper that applies `SysProcAttr` and `exec`s (the JDK's own `jspawnhelper` pattern) | fork in a JVM is not possible; `ProcessBuilder` cannot express fds-by-number, `Setsid`, `Ctty`, credentials, pidfd |
| `time.now`, `nanotime`, env, args, exit, signals | JDK APIs inside the runtime facade | Go never sees an fd or a struct there |
| `mmap` via `syscall.Mmap` | Panama; the returned address is registered as foreign memory (SPEC §3.4's raw-address FFM mode) at its true address | the only place an integer from the kernel becomes a Go pointer |

GG-0022 (cgo→Panama) shares the arena/handle machinery; GG-0004 is served
by regenerating the inventory each release and diffing it against the
table (new or re-typed syscalls show up as a table-vs-source mismatch).

### 2.3 The role table

One entry per syscall number: name, "may block", and up to six roles
([SyscallTable.java](../../test/spikes/abi/sysexec/java/abispike/SyscallTable.java)):

| Role | Meaning | Examples |
|---|---|---|
| `Val` | integer | fd, flags, length, offset |
| `CStr` | NUL-terminated bytes, in | paths |
| `Bytes(lenArg, dir, filled)` | `arg[lenArg]` bytes; OUT buffers copy back `result` or all | `read`, `write`, `getdents64`, `getcwd` |
| `Struct(type, dir, nestedDir, nullable)` | one value of a named Go type | `fstat`→`syscall.Stat_t`, `pipe2`→`[2]int32`, `sendmsg`→`syscall.Msghdr` |
| `Array(elemType, countArg, dir, nestedDir, filled)` | `arg[countArg]` consecutive values | `epoll_wait`→`syscall.EpollEvent`, `writev`→`syscall.Iovec` |
| nested rule (per type, not per syscall) | pointer field + the field holding its length or count + element type | `Iovec.Base/Len`; `Msghdr.Iov/Iovlen → Iovec`, `Name/Namelen`, `Control/Controllen` |

Two properties keep the table small and checkable:

- **A role names a Go type; it never states a size or an offset.** Sizes,
  field offsets and which leaves are pointers come from the same type table
  as P1 (the fixture for the prototype is
  [fixture.go](../../test/spikes/abi/sysexec/fixture/fixture.go): go/types'
  view of package `syscall`). The marshaller walks the leaf table: scalar
  leaves are copied by width, pointer leaves must have a nested rule or the
  call traps.
- **Direction is an optimization, not a correctness input**, except for
  partially filled OUT buffers (`read`), where the table says "copy back
  `result` units". Nested OUT buffers are copied in first so that an
  unfilled tail reads back unchanged.

The skeleton of all 148 entries can be generated from sysscan's output
(number, shape and pointee type per position); direction, `filled`,
"may block" and the three second-key calls are the hand-written part.

Pointers arrive as integers (`uintptr(unsafe.Pointer(p))`), so the executor
needs integer → pointer.
[HandleTable.java](../../test/spikes/abi/sysexec/java/abispike/HandleTable.java)
gives every object whose address is taken a range in a private address
space above any real user address: the integer is `base + offset`, stable
per object, and `uintptr(p) + n` converts back to `(object, offset + n)` by
a floor lookup. This also settles P1's six `unsafe.Pointer(uintptr(p)+n)`
sites and 27 `uintptr(p)` uses without frontend pattern matching, and an
integer that is not inside a live object traps instead of reaching the
kernel.

### 2.4 Prototype

[test/spikes/abi/sysexec/](../../test/spikes/abi/sysexec/): 17 syscalls
(`read`, `write`, `close`, `fstat`, `readv`, `writev`, `getpid`, `sendmsg`,
`recvmsg`, `socketpair`, `getdents64`, `clock_gettime`, `epoll_wait`,
`epoll_ctl`, `openat`, `epoll_create1`, `pipe2`), called in Go's shape —
`syscall6(trap, a1..a6)` with handle integers — from
[SysMain.java](../../test/spikes/abi/sysexec/java/abispike/SysMain.java),
executed by
[Syscalls.java](../../test/spikes/abi/sysexec/java/abispike/Syscalls.java)
(270 lines). Go-side values are P1's flat values (`long[]` + `Object[]`);
no Go object is pinned and no native address outlives a call.

**48 / 48 checks pass** ([run.sh](../../test/spikes/abi/sysexec/run.sh)),
expected values taken from the JDK's file APIs or from data the test wrote:

- `write(1)`; `openat`/`read`/`close` of a file, with the unfilled buffer
  tail intact and a pointer into the middle of a buffer;
- `fstat` into a flat `syscall.Stat_t`: `Size`, `Mode`, `Ino`, `Nlink`,
  `Mtim.Sec` equal `java.nio.file` attributes;
- `getdents64`: directory listing parsed from the Go-side buffer;
- `pipe2` into a `[2]int32`; `epoll_create1`/`epoll_ctl`/`epoll_wait` over
  a `[4]syscall.EpollEvent` (12-byte packed elements), only reported
  elements copied back;
- **nested, one level**: `writev`/`readv` over `[2]syscall.Iovec` whose
  `Base` fields are Go pointers, scatter into two Go buffers;
- **nested, two levels**: `sendmsg`/`recvmsg` over a socketpair,
  `Msghdr → []Iovec → bytes`, with `Msghdr.Flags` written back and the Go
  pointer fields left untouched;
- `clock_gettime` into a `Timespec`; `getpid`;
- `errno` paths (`EBADF`, `ENOENT`: `r1 = -1`, nothing copied back);
- loud failures: number not in the table, an integer that is not a
  pointer, a length or offset beyond the Go object, nil where a struct is
  required.

Failability was checked by mutating the table (not committed): `read`
copying back the whole buffer → 1 check fails; `readv` with nested
direction IN → 1 fails; `Msghdr.Iov` declared as bytes → trap; and
**`fstat` declared with `Timespec` instead of `Stat_t` → the kernel writes
144 bytes into a 16-byte allocation and the JVM dies with SIGSEGV** — see
risk 1 below.

Cost, indicative (best of 5 × 10⁶ calls, plain JVM): `getpid` 132 ns vs
gc's 89 ns; `write` of 64 bytes to `/dev/null` 242 vs 194 ns; `fstat`
524 vs 310 ns. The executor adds roughly 45–215 ns per call (arena, role
dispatch, leaf-by-leaf copy of a 144-byte struct); system calls are not a
hot path at this ratio.

### 2.5 The conflict that needs a decision: raw fds versus "the netpoller is deleted"

SPEC §3.5/§7.4 (`GG-0018`, since superseded by GG-0063) decided that blocking `java.nio` operations
replace the netpoller, and toolexec-design §5.6 says `internal/poll`'s
`runtime_poll*` bind to "the existing blocking-NIO netpoll contract". The
existing implementation
([GoNetpoll.java](../../internal/backend/interop/src/main/java/graalgo/p3net/GoNetpoll.java))
is a JDK `Selector` over `SelectableChannel`s, which works only because
v1's synthetic `net`/`os` create the sockets as NIO channels.

With verbatim `syscall` + `internal/poll` + `net` (D8), sockets are created
by `socket(2)` through the raw entry and `runtime_pollOpen(fd uintptr)`
receives a **descriptor number**. The JDK has no supported way to wait on,
or build a channel from, a descriptor number. The two decisions cannot both
hold. Options:

1. **A minimal poller over raw fds in the runtime** (recommended): one
   daemon platform thread in `epoll_wait` through the same executor
   (prototyped calls: `epoll_create1/ctl/wait`), unparking goroutines
   through the scheduler's wake lane, deadlines on the existing timer heap.
   About 300 lines of Java replacing `GoNetpoll`'s selector half; keeps
   `internal/poll`, `net`, `os` verbatim. It is a netpoller in the sense of
   `GG-0018`'s (superseded) text, though not the gc event loop: no carrier ever blocks in
   it and blocked goroutines consume no carriers.
2. **Keep NIO, fork the lower half of `os`/`net`/`internal/poll`** so that
   `poll.FD` wraps a channel: by sysscan's call graph that is the 47
   syscalls `net` reaches plus `internal/poll`'s fd code — the files
   affected are `internal/poll` (2.4 k lines on linux) and `net`'s fd/socket
   files (4.5 k lines), against GG-0009, and every `x/sys/unix` user of raw
   fds still needs option 1.

Blocking syscalls outside the poller (regular-file I/O, `wait4`, `flock`)
run on the calling goroutine's carrier with the scheduler slot released
around the downcall (the table's "may block" bit is `entersyscall`).

### 2.6 Risks and cost

1. **A wrong table entry is memory-unsafe** (demonstrated): the kernel
   writes by its own notion of the struct. Mitigations, all cheap: (a) a
   gate that compares each entry's Go type with the pointee type sysscan
   derives from std's call sites; (b) roles never state sizes; (c) debug
   mode with guard pages after each arena allocation; (d) unknown numbers
   fail.
2. `ioctl`/`fcntl`/`ptrace` second key: `fcntl` and the `ioctl`s std uses
   (6 sites) are enumerable; arbitrary user `ioctl`s need a default "third
   argument is a value" plus an opt-in registration API, or they trap.
3. Third-party code calling `syscall.Syscall` with numbers outside the
   table (io_uring, bpf, perf): fails loudly by design; the table grows on
   demand. Passing a Go pointer inside a `uintptr` field of a struct the
   table does not know cannot be made to work.
4. Thread-directed syscalls (`gettid`, `unshare`, `setns`, `prctl`,
   `sched_setaffinity`) act on whichever carrier runs the goroutine;
   correct only under `runtime.LockOSThread`, which must pin the goroutine
   to a dedicated carrier.
5. Copying: two copies per buffer byte for I/O. `Linker.Option.critical(true)`
   (heap segments passed directly) removes them for non-blocking calls and
   is an optimization to measure, not a dependency.
6. Carrier pinning during a blocking downcall is by design (SPEC §3.7,
   "cgo calls consume an M"); the scheduler must compensate as gc does for
   blocked Ms.

Effort (estimate, agent-days): table for all 148 numbers from the generated
skeleton + table-vs-source gate 2–3; executor in the backend incl. slot
release, `EINTR`, errno, `AllThreads*` 2–3; raw-fd poller (§2.5 option 1)
2–3; process spawn helper + `forkExec` patch 3–4; foreign memory for
`mmap`, signals glue 1–2. **Total 10–15**; M1c needs the first two items
and none of `net`: **5–7**.

### 2.7 How to rerun

```
(cd test/spikes/abi/sysscan && go run . [-sites] [root ...])      # §2.1, ~20 s
test/spikes/abi/sysexec/run.sh /some/scratch/dir [bench]            # §2.4, ~30 s; run from a scratch cwd
```

## 3. Proposed amendments to toolexec-design.md

1. **§0 D8, §5.1, §5.6 (reflect row), §10 R1, §12 P1:** P1 is answered:
   `internal/abi` and `internal/reflectlite` verbatim, `reflect` verbatim
   except a 4-function call-path patch. Remove the "150–200 patched
   functions" fallback and "37–200" intrinsics; state the mechanism of
   §1.3. R1 likelihood → low; residual risks are §1.5's list.
2. **§5.1 "Patched" list, §5.5:** drop `internal/abi`,
   `internal/reflectlite` and `sync` (pool). `sync.Pool.indexLocal` is
   in-array pointer arithmetic that the pointer model handles; P5
   (`HashTrieMap`, `sync.Map`) is expected verbatim for the same reason
   (39 handle-only functions, no view, no patch).
3. **§3.2 `TYPEDEFS`, §3.4:** the type table must carry, for every type
   (not only named ones): gc type string and link string, kind, size,
   alignments, pointer bitmap, comparable / regular-memory / direct-iface
   flags, struct package path, field tags/embedded/exported, and complete
   sorted method sets with the receiver-less signature type of each method
   ([typetab](../../test/spikes/abi/descview/typetab/main.go) is the
   reference). The layout of `internal/abi`'s own structs is taken from the
   same table; the backend hard-codes no descriptor offset.
4. **§3.4 last bullet, §5.6 `unsafe` row:** specify the pointer model:
   `unsafe.Pointer` and view pointers are `(object, gc byte offset)`; every
   addressable object kind implements scalar/pointer load/store by offset
   through per-type leaf tables; `uintptr` conversions use the
   range-allocating handle table of §2.3. This presupposes SPEC §3.4's flat
   struct representation — v1's nested `GoStruct` is the measured cause of
   20 of the 50 probe failures and must not be carried into W4.
5. **§5.6 `syscall` row, §12 P2, §10 R2:** replace "which arguments are
   pointers and how many bytes are read and written" by the role model of
   §2.3 (roles name Go types; nested rules per type; second key for
   `ioctl`/`fcntl`/`ptrace`; libc wrappers for `AllThreadsSyscall`
   numbers). R2 likelihood → low. Add the measured counts (148 / 70 / 47).
6. **§5.6 `internal/poll` row, new risk:** "bind to the existing
   blocking-NIO netpoll contract" does not hold for raw fds; record the
   decision of §2.5.
7. **§5.6 `syscall` row:** name the fork/exec patch concretely
   (`posix_spawn` + helper binary under `$GRAALGO_HOME/libexec`).
8. **§10, new risk R15:** syscall table errors are memory-unsafe
   (§2.6 risk 1) with the table-vs-source gate as mitigation.
9. **§8 M1c/M1d, §9 W8/W9:** W9 14–20 agent-days, W8 10–15 (M1c share
   5–7). W8 and W9 share the pointer model and handle table; schedule that
   item once, before both. Promote `descview` (imgcheck) and the syscall
   table-vs-source check to gates when W9/W8 start.
10. **§11:** add experiments E11 (unsafescan), E12 (reflectprobe on v1:
    76/126), E13 (descview: 607/607, 427/427), E14 (sysscan), E15 (sysexec:
    48/48).

## 4. Decisions needed from the owner

1. **Netpoller (SPEC §3.5 / §7.4, `GG-0018`, superseded; decided: option 1, GG-0063):** accept a minimal epoll-based
   poller over raw fds in the runtime (§2.5 option 1, recommended), or fork
   the lower half of `net`/`os`/`internal/poll` onto NIO channels. SPEC
   wording changes either way.
2. **SPEC §3.4 wording (decided: `GG-0061`, re-issued as GG-0072; GG-0062):** make "(object identity + field offset)" precise as
   *gc byte offset*, and state that `uintptr` values are range-allocated
   handles on which in-object arithmetic is defined (today: "arithmetic
   escaping the object traps" — unchanged, but in-object arithmetic becomes
   specified rather than implementation-defined).
3. **Process creation (decided: helper binary, GG-0064):** ship a spawn helper binary in the distribution
   (affects packaging, GG-0050), or restrict `os/exec` to what
   `posix_spawn` alone can express.
4. Minor, defaults proposed: `sync/atomic.Value` verbatim via a two-word
   interface header view (patch its four methods only if that view is
   awkward); run-time type constructors verbatim with an inverse descriptor
   reader (patch seven constructor tails only if `MapOf` group layouts make
   it expensive).
