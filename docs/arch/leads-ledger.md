# Leads ledger

His standing order: **never bury a lead**. Every open-ended investigation
keeps its promising leads, open questions, and deferred validations here
until a terminal verdict — a lead is added the moment it is found and kept
up to date until it is resolved (implemented, killed with evidence, or
superseded).

Each entry states: the problem, why the current state is insufficient, the
design (or the open design question), phasing, and the re-test trigger.
"Needs design" entries are deliverables in their own right — the entry is
the design until the implementation lands.

## Open

### L-1. synctest bubble semantics (review3-traps P1-I1, 2026-10-17)

**Problem.** `testing/synctest.Run` / `internal/synctest.inBubble` are real
Go APIs (Go 1.23+); graalgo fails them loud with a Go panic
(`Synctest.java`: `run` / `inBubble` → "not implemented by graalgo").
The panic is honest — running `f` without a bubble would silently drop
synctest's fake-time and deadlock-detection contract — but it is still a
trap on legal Go, and increasingly hit as stdlib-adjacent test code
adopts `synctest`.

**Current degraded mode (documented, not silent).**
`IsInBubble` → false, `associate` → `Unbubbled`, `Wait` → gc's exact
unbubbled panic, `Run`/`inBubble` → loud Go panic. The javadoc on
[`Synctest.java`](../../internal/backend/interop/src/main/java/graalgo/p3link/Synctest.java)
is the contract.

**Design (phased).**
- **Phase A — bubble membership.** A per-goroutine bubble context (nil =
  unbubbled). `Run(f)` installs a bubble on the current goroutine, runs
  `f`, uninstalls. `inBubble(b, f)` installs the given bubble, runs `f`.
  `IsInBubble` becomes meaningful. Documented degradation: **no fake time
  yet** — `f` runs against the real clock. This is the minimal honest
  step: the API executes instead of dying, and the remaining gap (time
  control) is explicit, not silent.
- **Phase B — quiescence `Wait`.** `synctest.Wait()` inside a bubble parks
  until no bubble-member goroutine is runnable (scheduler-tracked
  membership); all-blocked with no live timer → gc's deadlock panic.
- **Phase C — fake time.** The time package's clock source consults bubble
  state for bubbled goroutines (`time.Sleep` advances the bubble clock,
  `time.Now` reads it). This is the deep change; Phases A–B are its
  prerequisite.

**Open question.** Phase A trades the current fail-loud contract for
documented degraded execution. If that trade is rejected, the alternative
is keeping the panic and documenting `synctest` as unsupported until
Phase C — the entry then tracks the full design, not Phase A.

**Re-test trigger.** When Phase A lands, `Run`/`inBubble` stop panicking;
the "not implemented" panic tests and this entry update together.
