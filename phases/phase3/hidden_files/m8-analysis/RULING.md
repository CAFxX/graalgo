# P3-M8 coordinator rulings (2026-10-07 ~14:30 CEST, coordinator b99aed91)

All five analyses are in (io, os, syscall, reflect+unsafe, crypto). The
os and syscall workers disagreed on mechanism; the following rulings are
FINAL for the implementation phase. Contracts below are FROZEN — any
needed change goes through the coordinator via handoff, not peer edits.

## R1. Mechanism: //go:linkname (ADR-0004) for ALL M8 OS-touching intrinsics

Applies to: os, syscall. The syscall worker's wire-node proposal is
OVERRULED for M8. Reasons: (1) the exporter is frozen — new wire-node
kinds need per-op interception code in it; linkname needs only additive
registration data; (2) `Linknames` (P3-M4 WS C: `LinknameRootNode` +
dispatch, arity-checked, fail-fast on unknown symbols) was built for
exactly this; (3) one registration serves binders from both synthetic
packages; (4) D5 boxing covers every shape needed (Long, GoString,
GoSlice); (5) M8 is bring-up, not optimization — Truffle specialization
of I/O-bound calls buys nothing. The syscall worker's OP INVENTORY and
SCOPE VERDICT stand unchanged; only the binding mechanism changes
(wire nodes -> linkname symbols).

## R2. I/O home: os owns it; syscall does NOT implement I/O wrappers in M8

Synthetic `os` binds directly to `graalgo.os.*` symbols (`GoOs.java`,
NEW file). Synthetic `syscall`'s `Open/Read/Write/Close/Stat/Mkdir/...`
FAIL FAST with trap-style messages ("syscall.Open: no graalgo intrinsic;
use os; re-test: fd-domain unification, Phase 4/Panama"). Reasons: one
handle domain (GoOs cookies) stays coherent — a JVM fake-fd table PLUS
GoOs cookies would be two file domains with an incoherent seam
(`Fd()`/`NewFile`/`SyscallConn`); the single-domain cut DISSOLVES the
`Stat_t`/`linux_dirent64`/`Utsname` layout problems (§2.6 of os.md).
Joint re-test trigger (both docs): fd-domain unification when syscall
needs real fds (Panama FFI, net, os/exec).

## R3. syscall owns the vocabulary (pure Go, zero intrinsics)

`Errno` + `Is`, errno/`O_*`/`S_*`/`DT_*` constants, struct types. Scalar
services (`Getenv/Getpid/Getuid/Exit/...`) are 3-line real-Go wrappers
in synthetic syscall binding the SAME `graalgo.os.*` symbols (gc's call
graph preserved). os imports syscall for error identity only.

## R4. Symbol namespace and ownership

All M8 OS intrinsics: `graalgo.os.<name>`. The os worker OWNS the symbol
table (os.md §2.4, ~40 symbols — FROZEN). The syscall worker binds its
scalar wrappers to it and reports needed additions via handoff.

## R5. Synthetic io/fs core: owned by the os implementation worker

~150 lines, NEW file `synth_iofs.go`: PathError, FileInfo, DirEntry,
FileMode + bit constants, Err* sentinels, FS interface stub. (The io
worker scoped io/fs OUT; os cannot build error identity without it.)

## R6. os.Args[0] = input-file path

Documented analog of `go run`'s temp-binary path. Launcher argv
convention (args after `--`, `GoOs.init(argv)` before Go init,
argc/argv(i) intrinsics) STAGED AS PATCH (P3SyncMain.java is existing;
remediation hasn't touched it but it stays hands-off until rebase).

## R7. EPIPE/SIGPIPE: guarded raise, loud fallback

Attempt `Signal.raise("PIPE")` guarded by availability; if unavailable,
return the wrapped EPIPE error AND a corpus program asserting the death
(exit 141) differentially — a missing raise fails loudly, never drifts.

## R8. M6 inventory amendments (worker applies at rebase time)

- os/signal line: "`os.Signal` the TYPE is pure Go and ships; only
  DELIVERY is JVM-owned" (was: "the JVM owns signals").
- `asm_linux_amd64.s` "Panama FFI" line STAYS (Phase-4 trigger).

## R9. Staging discipline (hard, all implementation workers)

SCOPE: R9 governs shared *code* files (existing .go/.java sources).
It does not govern coordinator bookkeeping (CHECKPOINT.md appends,
analysis docs, review docs) or new files. The discipline:

NEW FILES ONLY in the tree for implementation work. Every edit to an
EXISTING shared code file
(synth_machinery.go registration, mrdesugar_m5.go desugar entries,
export.go, NodeFactory.java, LinknameIntrinsics.java registrations,
P3SyncMain.java) is drafted as a patch file under
`phases/phase3/hidden_files/m8-staging/<pkg>/NN-<what>.patch` with
file:line anchors and surrounding context. DO NOT APPLY. The
coordinator applies them in dependency order at rebase time.

## R10. crypto sequencing

Hash/symmetric core first (no cross-track deps); `rand` and overlap-
panic-parity work AFTER the unsafe + syscall drafts are readable in
the tree. math/big: NOT vendored (no throwaway forks).

## R11. unsafe const-fold needs an exporter pre-pass

Sizeof/Alignof/Offsetof as constant-fold intrinsics require a
const-context AST pre-pass — STAGED AS PATCH (shared exporter files).
`Add` traps per D5/GG-0015 (already the design); String copies
(DIV-US-1 documented).

## R12. Desugar entries require real helpers (2026-10-07, io worker question)

APPROVED the io worker's approach: Appendix-M desugar entries are tabled
ONLY for members with real `__`-helpers. Members without helpers get a
NAMED export-time scope error (isMultiResultSynthetic milestone), not a
tabled-but-helperless entry. Rationale: a tabled entry without a helper
compiles then panics at runtime — strictly worse than an export-time
named error (asn1.Unmarshal precedent). Both are honest fail-fasts; the
export-time one is earlier and louder. Applies to all M8 workers.

## R11 SUPERSEDED (2026-10-07, unsafe worker deviation — verified)

R11 required a const-context AST pre-pass for Sizeof/Alignof/Offsetof
constant folding. The unsafe worker instead used `*types.Builtin`
shared from `types.Unsafe`, relying on go/types' own folding.
COORDINATOR VERIFIED with the go1.27.1 toolchain (gotypes-probe):
`unsafe.Sizeof`, `unsafe.Alignof`, `unsafe.Offsetof` all fold to
constants in go/types (const S/A/O + array-length position confirmed).
The worker's deviation is CORRECT and strictly better (inherits gc's
operand checking too). R11's pre-pass is NOT needed.

## R13. Entropy symbol added post-analysis (2026-10-07, coordinator)

The frozen graalgo.os.* table (os.md §2.4) had no entropy source; the
crypto worker fail-fasted crypto/rand.Reader/Read/Text pending it.
Coordinator ADDED `graalgo.os.getrandom(b)` (SecureRandom fill, returns
count): new method in GoOs.java (new file), registration line added to
staged os/04 patch, os.md §2.4 table updated, and synth_crypto_rand.go
rewritten to bind it (Reader/Read/Text now REAL; Int/Prime still
omitted pending math/big). Also fixed the worker's rand.Text (was 32
chars/alphanumeric — gc 1.27.1 is 26 chars/base32, text.go verified).
Corpus rand program rewritten for real entropy (property checks).
