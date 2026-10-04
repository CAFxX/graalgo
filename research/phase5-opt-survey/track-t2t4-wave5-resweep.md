# Track T2/T4 — Wave-5 continuous re-sweep & watches (W5)

**Wave:** 5 · **Worker:** T2 re-sweep + T4 watches · **Fetch date:** 2026-10-04
(~11:40 CEST)
**Mission:** (T2) Graal 25.x changelog past wave-4's snapshot; JEP 544 watch;
Valhalla JEP 401 / primitive-class movement. (T4) golang/go #24314 status;
#77893 five CLs MERGED/ABANDONED check; label-sweep capacity check +
standout new issues. Wave-4 snapshot baseline: 2026-10-04 ~11:20 CEST
(`track-resweep-wave4.md`).
**Scope:** raw writeups only — no SURVEY.md/CHECKPOINT.md edits, no SURV- ID
assignment, no graalgo/SPEC.md/PHASES.md/registry edits. New findings clearing
the candidate bar enter as raw sketches (coordinator assigns SURV- IDs).
Unchanged items are recorded as negatives with the exact check performed. All
vendor/project numbers are source claims, never our measurements.

**Headline:** one watch trigger fired — **JEP 544 has moved Candidate →
Targeted** for JDK 28 (direct JEP-page evidence). Everything else is a
negative.

---

## T2.1 Graal 25.x compiler changelog (re-sweep vs wave-4 snapshot)

**Negative — unchanged since wave 4.**

Exact checks performed (2026-10-04 ~11:40 CEST):
1. Raw fetch of `https://raw.githubusercontent.com/oracle/graal/master/compiler/CHANGELOG.md`
   (14,501 bytes). The file still tops out at **GraalVM 25.5 (Internal Version
   25.5.5)** — the same 12 entries wave 4 recorded (GR-79881/79883, GR-79595,
   GR-79593, GR-79589, GR-79594, GR-79588, GR-79526, GR-79587, GR-79585,
   GR-79597, GR-79590, GR-79031). **No 25.6 section, no new compiler entries
   past the wave-4 snapshot.** Note the GR-77137 priority-inlining entry sits
   in the 25.3 section and GR-28213 vectorization likewise — both unchanged.
2. `https://api.github.com/repos/oracle/graal/releases?per_page=15` — returns
   only the legacy community releases (newest: vm-19.3.1, 2020-01-23; "Releases
   have moved"). This API is a dead end for current GraalVM releases; the
   CHANGELOG.md master fetch is the authoritative channel, and it shows no
   movement.

**Disposition:** no new candidate bar material; the speculation story remains
GR-79590 (speculative store checks) + GR-79031 (exact arithmetic via
loop-limit deopt). Still no new escape-analysis capability, no new inlining
policy since wave 3.

---

## T2.2 JEP 544 watch (Leyden "Terminus" line / JDK 28)

**WATCH TRIGGER FIRED — status movement Candidate → Targeted.**

Exact check: direct fetch of `https://openjdk.org/jeps/544`, JEP-page metadata
table:
- Title: **JEP 544: Ahead-of-Time Code Compilation**
- Owner: John Rose
- **Status: Targeted**
- **Release: 28**
- Component: hotspot / compiler

Wave 4 recorded "still Candidate for JDK 28 — no Proposed→Target transition"
based on JVM Weekly vol. 192 and InfoWorld ("has been proposed"). The JEP page
itself now reads **Targeted** with Release 28. Dating the transition:
InfoWorld's "Java 28 starts to take shape" (crawled 2026-10-03, ~10 days old
per wave 4) still described it as *proposed*, and the LavX News piece (~24
days old) likewise calls it a "feature proposal" — so the targeting
transition happened roughly within the last ~10 days, well ahead of the
wave-4 note's expected JDK 28 ramp-down window (~Dec 2026–Jan 2027). A third
source checked (JVM Weekly vol. 192, crawled ~11h before this fetch) still
lists 544 among September candidates — now stale relative to the JEP page.

**Relevance (no SURV- ID assigned):** JEP 544 is the Leyden "Terminus"-line
AOT method profiling / ahead-of-time code compilation feature. Its targeting
for JDK 28 strengthens the standing relevance of the Leyden bucket for
graalgo's AOT/JIT-hybrid thesis; it is a status movement, not a new
technique, so no raw candidate is filed — the coordinator decides whether any
bucket status changes on this news.

**Watch update:** the §6 Proposed→Target trigger for JEP 544 is now
satisfied; the remaining watch is whether it survives ramp-down and ships in
28 GA (March 2027).

---

## T2.3 Valhalla JEP 401 / primitive-class movement

**Negative — no status change.**

Exact checks (2026-10-04):
1. Direct fetch of `https://openjdk.org/jeps/401`, JEP-page metadata table:
   Title "JEP 401: Value Objects (Preview)"; Owner Dan Smith; **Status:
   Integrated**; **Release: 28**; Scope SE; Effort XL. Unchanged from wave 4
   (integrated in mainline, JDK 28 preview).
2. Web search for primitive-class movement (October 2026): Project Valhalla's
   public feature-stream list remains (1) Value Objects, (2) Null-Restricted
   Storage, (3) Array Enhancements, (4) **Unifying Primitives and Classes** —
   still future; (5) Parametric JVM — still future. No new JEP number for
   primitive classes; JEP 402 (Enhanced Primitive Boxing, Preview) and the
   Null-Restricted Value Class Types draft remain at their prior standing.
   No identity-freedom / flattening-relevant movement to feed SURV-061's
   value-field-inlining line.

**Watch note (data point, not movement):** JVM Weekly vol. 192 (crawled ~11h
before this fetch) reports Ivan Ponomarev measured `System.arraycopy` as
**8 to 20 times slower than a plain loop on null-restricted flat value
arrays** in the JDK 28 EA builds (their claim; 28-ea context), after Dan
Smith's Aug 10 call to test with and without `--enable-preview` because the
integration "perturbs a very large amount of code". A bulk-copy slowdown on
flattened value arrays is relevant context for SURV-061's flattening-performance
line — recorded as a watch note, not a candidate (it's an EA performance
blemish in the platform, not a transferrable technique).

---

## T4.1 golang/go #24314 — status check

**Negative — open, no movement.**

Exact check: `https://api.github.com/repos/golang/go/issues/24314`:
state=**open**, `updated_at`=2022-07-13T16:58:42Z, comments=8,
labels=[Performance, compiler/runtime], title
"cmd/compile: slice update with self subslice - write barrier probably not
needed (?)". No new comments, no label or state change since wave 4 (and
since 2022). The barrier-elision negative stands.

---

## T4.2 golang/go #77893 — the five CLs (MERGED/ABANDONED check)

**Negative — none of the five CLs has moved; SURV-041's gap does NOT narrow.**

Exact check: Gerrit REST API `https://go-review.googlesource.com/changes/<n>?o=CURRENT_REVISION`
for each CL, 2026-10-04 ~11:45 CEST:

| CL | Subject | Owner | Status | Updated |
|---|---|---|---|---|
| [758140](https://go.dev/cl/758140) | runtime: add starting stack size benchmark and add GODEBUG=gcstacksizetrace | thepudds | **NEW** | 2026-03-23 |
| [758141](https://go.dev/cl/758141) | runtime: exclude inactive goroutines from starting stack size heuristic | thepudds | **NEW** | 2026-03-30 |
| [764220](https://go.dev/cl/764220) | runtime: estimate starting stack sizes by calculating percentiles of scanned sizes | thepudds | **NEW** | 2026-04-08 |
| [793780](https://go.dev/cl/793780) | runtime: cache grown goroutine stacks to reduce stack-growth cost | geekswaroop | **NEW** | 2026-07-16 |
| [793920](https://go.dev/cl/793920) | runtime: add stack size watermark for initial stack size heuristic | thepudds | **NEW** | 2026-06-24 |

All five statuses and their `updated` timestamps are byte-identical to the
wave-4 snapshot. The §6 trigger (any CL → MERGED/ABANDONED) has **not**
fired; SURV-041's distinct edge (per-site adaptive sizing, vs the
global-heuristic CLs) remains intact; bucket THINKING unchanged.

---

## T4.3 Label-sweep capacity check + standout new issues

**Capacity check: PASS — adequate, unchanged vs wave 4. Not a full re-sweep
(per task scope).**

Exact checks performed (2026-10-04 ~11:50 CEST):
1. Anonymous GitHub API budget: 60/hr, 56 remaining after the probes below —
   a full two-label sweep costs 28 list calls at 100/page, well within one
   anonymous hour.
2. Totals via search API (2 calls, `total_count`):
   - `repo:golang/go label:Performance state:open` → **686** open (7 pages @100)
   - `repo:golang/go label:compiler/runtime state:open` → **2068** open (21 pages @100)
3. Probe: top-8 open issues by creation date for each label (2 list calls,
   `sort=created&direction=desc&per_page=8`).

Standout new issues (watch notes only — none individually clears the
candidate bar; all are small compiler/backend fixes without adaptive or
speculative optimization content):
- [#81887](https://github.com/golang/go/issues/81887) (2026-09-30, 3
  comments): `slices.BinarySearchFunc` not inlined, slower than `sort.Search`
  — inlining-heuristic/mid-end observation.
- [#81846](https://github.com/golang/go/issues/81846) (2026-09-29, 3
  comments): `simd.Float32s.Neg` rebuilds its sign-bit constant on every loop
  iteration — loop-invariant constant materialization.
- [#81755](https://github.com/golang/go/issues/81755) (2026-09-25, 5
  comments): arm64 rematerializes multi-instruction 64-bit constants inside
  loops — same theme as #81846, on arm64. The pair suggests a recurring
  loop-invariant constant-materialization gap in Go's backends; recorded as
  a theme watch, not a candidate.
- [#81957](https://github.com/golang/go/issues/81957) (2026-10-02, 3
  comments): GC stack shrinking is O(n²) in the number of goroutines blocked
  on one sync.Mutex — runtime scaling note.
- [#81968](https://github.com/golang/go/issues/81968) (2026-10-02, 1
  comment): "runtime: bound overtakes of lock2 sleepers" — scheduler
  fairness-adjacent (lock2 overtaking bound); one-line watch, no detail yet.

None of these changes any bucket status. Next full re-sweep remains feasible
on the standing cadence within anonymous API capacity.

---

## Raw candidate sketches (wave 5)

**None.** The only movement in this wave (JEP 544 Candidate→Targeted) is a
status change on an already-known platform feature, not a new transferrable
technique; the graalgo-side relevance sits in existing backlog lines (Leyden
bucket). The Graal changelog has no new entries, and the T4 watches show no
movement.

---

## First-class negatives / watch notes (wave 5)

1. **Graal 25.x re-sweep:** CHANGELOG.md (master) unchanged since wave 4 —
   still tops at GraalVM 25.5 with the same 12 entries. No new compiler
   optimization, no speculation-policy change. (Check: raw master fetch +
   releases API, which is a dead end.)
2. **JEP 544:** TRIGGER FIRED — **Targeted** for JDK 28 (owner John Rose),
   transition dated roughly within the last ~10 days, ahead of the expected
   Dec 2026–Jan 2027 ramp-down window. No candidate filed; bucket-relevance
   decision to coordinator.
3. **JEP 401:** unchanged — Integrated / Release 28 / "Value Objects
   (Preview)". No primitive-class JEP movement; feature-stream 4 ("Unifying
   Primitives and Classes") still future. SURV-061's line unaffected.
   Watch note: EA report of System.arraycopy 8–20× slower than a plain loop
   on null-restricted flat value arrays (Ivan Ponomarev via JVM Weekly
   vol. 192 — their claim).
4. **#24314:** open, no movement (updated 2022-07-13, 8 comments).
5. **#77893:** all five CLs still NEW with wave-4 timestamps; no
   MERGED/ABANDONED. SURV-041's gap does not narrow.
6. **Label-sweep capacity:** anonymous 60/hr budget adequate — 686 open
   `label:Performance` (7 pages) + 2068 open `label:compiler/runtime` (21
   pages) = 28 calls < 60. Standout probe issues: #81887 (inlining),
   #81846 + #81755 (loop-invariant constant materialization theme), #81957
   (GC stack-shrink O(n²)), #81968 (lock2 sleeper overtakes). Watch notes
   only.

---

## Sources (all accessed/fetched 2026-10-04)

- `https://raw.githubusercontent.com/oracle/graal/master/compiler/CHANGELOG.md`
  (direct fetch; still GraalVM 25.5 section only).
- `https://api.github.com/repos/oracle/graal/releases?per_page=15` (legacy
  community releases only — dead end for current releases).
- `https://openjdk.org/jeps/544` (direct fetch: Status=Targeted, Release=28,
  Owner=John Rose).
- `https://openjdk.org/jeps/401` (direct fetch: Status=Integrated, Release=28,
  Owner=Dan Smith, Scope SE).
- `https://www.infoworld.com/article/4205791/java-28-starts-to-take-shape.html`
  (crawled 2026-10-03 — still says "proposed"; bounds the transition date).
- `https://www.jvm-weekly.com/p/jdk-27-is-here-jvm-weekly-vol-192` (crawled
  ~11h before fetch — 544 among September candidates, now stale; arraycopy
  value-array slowdown report; JEP 401 integrated note).
- `https://news.lavx.hu/article/jep-544-brings-ahead-of-time-code-compilation-to-java-hotspot`
  (feature-proposal framing, bounds transition date).
- Web search: Project Valhalla primitive classes October 2026 — no new JEP
  movement (feature streams per valhalla-docs/_index.md, Wikipedia project
  components).
- `https://api.github.com/repos/golang/go/issues/24314` (state open, 8
  comments, updated 2022-07-13).
- `https://go-review.googlesource.com/changes/{758140,758141,764220,793780,793920}?o=CURRENT_REVISION`
  — all status NEW.
- `https://api.github.com/repos/golang/go/issues?labels=...&sort=created&direction=desc&per_page=8`
  (Performance + compiler/runtime probes).
- `https://api.github.com/search/issues?q=repo:golang/go+label:...+state:open&per_page=1`
  (totals: 686 Performance, 2068 compiler/runtime).
- `https://api.github.com/rate_limit` (60/hr anonymous budget).
