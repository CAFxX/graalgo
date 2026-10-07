# P3-M8 analysis synthesis (accumulating)

## io (worker 2/5, done 2026-10-07 ~14:16 CEST)

- Scope: FULL (io.go + multi.go + pipe.go); io/fs, io/ioutil OUT.
- Imports: errors (M7 synth OK), sync (M2 OK) EXCEPT sync.Pool missing ->
  discard.ReadFrom must be rewritten without Pool (documented deviation) OR
  M2 sync surface extended (staged option; phase2 file, check vs remediation
  at rebase).
- M7 forward triggers discharging in M8: bytes.Buffer.ReadFrom/WriteTo,
  strings.Reader io methods, base32 NewEncoder/NewDecoder + drift test,
  xml io.Reader/Writer stand-in migration. These modify EXISTING wave-1
  synth files -> STAGE AS PATCHES for rebase time (do not edit now).
- Multi-result: Copy family, Read/Write/Seek/ReadAt/WriteAt, ReadAll,
  WriteString, Pipe, ReadRune/Outer triples need mrM7-style desugar entries
  in mrdesugar_m5.go -> STAGE AS PATCH (shared file).
- Open design Q: interface-value call sites (r.Read(p) on io.Reader) —
  implementer must check ADR-0003 interface dispatch path.
- Top risks: Pipe rendezvous/close races under P3 scheduler; copyBuffer
  WriterTo/ReaderFrom dispatch + error normalization; ReadAll growth
  schedule + ReadAtLeast 4-way edge matrix.
- Corpus: 12 programs sketched.

## os (worker 3/5) — pending
## syscall (worker 1/5) — pending
## reflect+unsafe (worker 5/5) — pending
## crypto (worker 4/5) — pending

## syscall (worker 5/5, done 2026-10-07 ~14:20 CEST)

- Design: SUPERSEDED BY RULING.md R1/R2 (2026-10-07). The analysis below
  proposed ~30 TYPED Java intrinsics (exporter-lowered wire nodes,
  time.Now precedent) behind a JVM-level fd table. R1 overruled this:
  ALL M8 OS-touching intrinsics use //go:linkname (ADR-0004); R2 gives
  I/O ownership to os (GoOs.java), syscall does not implement I/O
  wrappers. Preserved here for the op inventory and scope verdict, which
  stand. No JNI/JNA exists (PinAudit.java
  asserts it); none needed. Panama FFI = Phase-4 re-test trigger.
- KEY value-model constraint (SPEC §7.3): unsafe.Pointer is handle-table
  emulation -> trap args arrive as handle tokens, not native addresses ->
  NO generic Syscall dispatcher possible. Arbitrary-trap Syscall/RawSyscall/
  Syscall6/RawSyscall6 FAIL-FAST (with trap-numbered panic messages).
- Do NOT reuse zsyscall wrappers verbatim (funnel untyped uintptrs through
  Syscall); re-author ~35 wrappers os needs as typed Go -> typed intrinsics.
- Scope IN: consts/types, Errno/Signal/WaitStatus, errnoErr, env family
  (Go-copy-only runtimeSetenv), ~30 file/dir/stat/proc/time intrinsics,
  entersyscall/exitsyscall no-ops, linkname surface (runtime_envs,
  Getpagesize=4096, Exit).
- FAIL-FAST: arbitrary-trap Syscall*; StartProcess/ForkExec/Wait4 (retest:
  os/exec milestone); sockets/netlink/SyscallConn (retest: net); Mmap;
  Flock/rlimit/setuid/epoll/inotify (retest: per-consumer demand).
- Top risks: (1) exception->errno misclassification at Java boundary
  (breaks os.IsNotExist/IsPermission chains SILENTLY — hand-map per
  intrinsic); (2) kernel-struct layout fidelity (Stat_t, linux_dirent64,
  Utsname — hand-authored, field-by-field differential tests); (3)
  late-discovery API break on arbitrary-trap fail-fast (mitigated: zero
  direct Syscall uses in linux/amd64 os — CONFIRMED).
- os->syscall minimal contract defined in doc; 10 corpus sketches; 4 open
  questions (notably: confirm M8 os doesn't need StartProcess; amend M6
  inventory's "Panama FFI" line for asm_linux_amd64.s).
- New Java intrinsics = NEW files (allowed). Exporter lowering: check
  time.Now precedent for which files are touched -> stage if shared.

## crypto (worker 4/5, done 2026-10-07 ~14:22 CEST)

- IN (M8): `hash` + `crypto` root (new synthetics); md5, sha1 (self-contained
  pure-Go, trivial trims); sha256, sha512, sha3 (trimmed fips140 digests;
  sha3 keccak unsafe->always-copy rewrite); subtle (plain-loop XORBytes,
  DIT->f()); hmac (non-generic rewrite); cipher (CBC/CTR/CFB/OFB + GCM via
  newGCMFallback + ported GHASH, error/behavior parity verified); aes; des,
  rc4 (zero asm); rand CONDITIONALLY (Reader/Read/Text real via
  syscall-track entropy; Int/Prime fail-fast pending math/big).
- DEFERRED (M9+): ecdh/ecdsa/rsa/ed25519/elliptic/dsa/hkdf/pbkdf2/mldsa/
  mlkem/hpke as one asymmetric milestone (re-test: math/big -> rsa/ecdsa;
  rand -> keygen). x509/tls = wave 3 (need net + asymmetric set).
  crypto/internal/* never imported (inline/trim donors only).
- math/big: DEFER, do NOT vendor a minimal big (throwaway fork violates
  no-placeholders bar; M6 prices it as clean jvm-math bring-up for M9).
- Key enabler: FIPS/boring opt-in, default off -> module machinery
  (Record*, CAST self-tests, DRBG, boring branches) trims away; synthetics
  implement the standard non-FIPS pure-Go path only (re-test recorded).
- Top risks: (1) HMAC marshaled fast path + digest marshal wire formats
  (1-byte divergence invisible to KATs; fires only on Reset-after-Sum;
  dedicated corpus per hmac x digest); (2) buffer-overlap panic parity
  NEEDS unsafe — 15+ fail-fast sites, no correct pure-Go approximation —
  HARD DEPENDENCY on M8 unsafe track (fallback: documented divergence +
  scopelimited corpus); (3) GCM fallback-routing edges (exact-overlap
  Seal/Open, random-nonce memmove, non-12 nonces) need differential proof.
- Corpus: 10-14 programs sketched (KATs vs gc + adversarial boundaries).
- ORDERING: unsafe + syscall tracks before crypto (overlap parity, entropy).

## reflect+unsafe (worker 1/5, done 2026-10-07 ~14:24 CEST)

- unsafe verdict: FULL (all 9 members real). Pointer = real frontend type
  (alias types.UnsafePointer; kind mapping exists GoKind.java:186);
  Sizeof/Alignof/Offsetof = frontend constant-fold intrinsics (needs
  const-context AST pre-pass; verified constant in gc 1.27.1);
  Slice/SliceData/String/StringData = call-site-desugared runtime
  intrinsics over UnsafeHandles table. FAIL-FAST: Add traps (D5/GG-0015 —
  handles admit no byte arithmetic); Pointer<->uintptr conversions (existing
  trap). String copies (DIV-US-1: GoString immutable; post-call writes
  invisible — documented).
- reflect verdict: FULL with named fail-fasts. Wire type table already
  carries everything (field offsets/tags, method tables, signatures);
  missing piece = runtime GoRType table built from it, canonical per
  GoTypeDescriptor. Synthetic Go mirrors gc Value{typ,ptr,flag} over ~35
  small intrinsics mapped to existing Java capabilities (GoCalls.invoke,
  ChanOps, Intrinsics.map*, wire table). DeepEqual/Swapper/VisibleFields =
  pure synthetic Go. FAIL-FAST only: iter-family (Seq, Fields, Methods,
  Ins/Outs, CanSeq), type constructors (ArrayOf..StructOf — needs type
  synthesis), TypeAssert[T] (generics), uintptr-leaking accessors (D5).
  Rationale: fmt/json/text-template need the write API + MethodByName+Call;
  a subset strands all three.
- Top risks: (a) unexported-field flagRO propagation parity (missed =
  silent encapsulation break; over-eager = spurious panics in fmt/json);
  (b) method-value/MakeFunc trampolines (new objId->call-target registry
  in NodeFactory — SHARED FILE -> stage patch; panic translation across
  Go->Java->Go boundary); (c) unsafe.Slice referent-shape dispatch +
  handle interning (== parity, DeepEqual cycle keys; interning pins memory).
- Linknames: all 13 reflect asm files unneeded (trampolines replace
  callReflect/makeFuncStub/ifaceE2I); runtime linknames have analogs;
  internal/reflectlite = trivial synthetic adapter; third-party
  reflect.(*rtype) linknamers correctly fail at resolution.
- Corpus: 10 unsafe + 12 reflect programs sketched (trap pins, const-context
  Sizeof, interning ==, unexported both-directions, DeepEqual cycles/funcs,
  MakeFunc panic propagation, fail-fast pins with exact messages).
- SHARED-FILE EDITS NEEDED (stage as patches): NodeFactory.java (objId
  registry), mrdesugar_m5.go (call-site desugars), synth_machinery.go
  (registration), export.go (unsafe empty-package removal?).
