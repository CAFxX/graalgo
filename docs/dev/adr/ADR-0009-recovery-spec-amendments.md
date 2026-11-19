# ADR-0009 — Recovery SPEC amendments (value model, unsafe, poller, process spawn, host access)

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 (program owner).
**Scope:** [SPEC.md](../../arch/SPEC.md) §1.3, §2.4, §2.7, §3.4, §3.5, §3.6,
§3.8, §3.9, §4.3, §7.3, §7.4, §10.1, Appendix B; requirements `GG-0015`, `GG-0016`,
`GG-0018` (superseded), `GG-0061`–GG-0064 (new, locked);
[toolexec-design.md](../../arch/toolexec-design.md) §14;
[truffle-design.md](../../arch/truffle-design.md) §14.
**Evidence:** [abi-spike.md](../../arch/abi-spike.md) (P1, P2: both GO);
[truffle-design.md](../../arch/truffle-design.md) §0.2, §5.6, §7.3–§7.4.

## Context

The recovery plan ([RECOVERY.md](../../arch/RECOVERY.md)) replaces v1 with the
per-package `-toolexec` path, compiling the real standard library verbatim
(GG-0009). The two de-risking prototypes and the Truffle design showed four
places where the SPEC of 2026-10-03 either cannot hold with verbatim std or was
under-specified, and two where the SPEC may not be achievable in image modes.

## Decisions

1. **Flat value model (`GG-0015` → `GG-0061`).** Structs and arrays are flat
   (`long[]` + `Object[]`, nested fields inlined) with per-type leaf tables
   from the gc layout; a pointer is (referent, gc byte offset). v1's nested
   `GoStruct` is not carried over: it caused 20 of the 50 reflect-probe
   failures and does not converge.
2. **unsafe semantics (`GG-0016` → GG-0062).** The handle offset is a gc byte
   offset; in-object arithmetic is defined; `uintptr` values come from a
   range-allocating handle table. Escapes are Go fatal errors with gc's
   `checkptr` texts (exit 2), never an uncaught Java exception — the audit
   found v1 crashing with `UnsafeEscapeException`.
3. **Runtime epoll poller (`GG-0018` → GG-0063).** "Netpoller deleted, blocking
   NIO" cannot coexist with verbatim `net`/`internal/poll`, which hand the
   runtime raw fds. A small epoll poller driven through the Panama syscall
   executor replaces it; waiting goroutines park through the slot-releasing
   path (`GG-SCHED-0004`).
4. **Process creation (new GG-0064).** A `posix_spawn`ed helper binary in the
   distribution implements `os/exec`/`syscall.ForkExec`; the JVM never forks.
5. **Host access for `graalgo/java` (SPEC §2.4/§4.3, truffle-design A4).**
   Embedded: the embedder's `HostAccess`. Standalone: allow-listed public
   access only when the program imports `graalgo/java`. GG-0003/GG-0005 are
   unchanged (they state no policy). A3 (polyglot `Value` wording in §4.3) is
   independent of this decision and stays pending, as do A5 (Bytecode DSL,
   after the TR-13 bake-off) and A6.
6. **Measure first (truffle-design A1, A2).** Goroutines as platform threads
   in image modes and the meaning of GG-0050's "performant without the
   runtime JIT" are `[OPEN]` architecture items in SPEC §3.5/§3.9, gated on
   measurements P-D (`engine.Compilation=false`) and P-F (native-image thread
   behaviour), truffle-design §13.2. GG-0050 is not amended.
7. **Toolexec plan** (no SPEC text): W4 builds a new idiomatic Truffle node
   package (+6–10 agent-days on M1a; v1 nodes deleted at M3), and the ten
   amendments of abi-spike §3 are applied (toolexec-design §14).

## Consequences

- v1 code that implements the superseded requirements (nested `GoStruct`,
  identity-token `uintptr`, the deleted `Netpoll` stub) is non-conforming and
  is deleted at toolexec M3; its comments now say so. Historical records
  (phase plans, reviews, `spec-extract.md`, earlier ADRs) quote the old IDs
  in code font, which the requirements linter treats as quotation.
- SPEC §1.3 now records two non-go/no-go `[OPEN]` items.
