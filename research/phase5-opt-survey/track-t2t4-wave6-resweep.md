# Track T2/T4 — Wave-6 continuous re-sweep & watches (W6)

**Wave:** 6 · **Worker:** T2 re-sweep + T4 watches · **Fetch date:** 2026-10-04
(~12:50 CEST)
**Mission:** (T2) Graal 25.x changelog past wave-5's snapshot; JEP 544
ramp-down survival watch; Valhalla JEP 401 / primitive-class movement; T2 lane
(OpenJ9/Azul/.NET) bar-clearing items only. (T4) golang/go #24314 status;
#77893 five CLs MERGED/ABANDONED check; label-probe watch notes
#81887, #81846/#81755, #81957, #81968 activity check. Wave-5 snapshot
baseline: 2026-10-04 ~11:40–11:50 CEST (`track-t2t4-wave5-resweep.md`).
**Scope:** raw writeups only — no SURVEY.md/CHECKPOINT.md edits, no SURV- ID
assignment, no graalgo/SPEC.md/PHASES.md/registry edits. Unchanged items are
recorded as negatives with the exact check performed. All vendor/project
numbers are source claims, never our measurements.

**Headline:** no watch trigger fired in wave 6. Everything is a negative,
with two method notes: (1) the direct JEP-544 page fetch was refused by the
browser tool this wave (HTTP 403, tool-level block) and was not retried via
any other path, so JEP 544's status could not be re-verified directly —
secondary sources show no further movement; (2) one new T2-lane watch note —
Azul's Oct-1 fleet-wide Cloud Native Compiler warm-up announcement (marketing
claim, not a new technique, no candidate filed).

---

## T2.1 Graal 25.x compiler changelog (re-sweep vs wave-5 snapshot)

**Negative — byte-identical to wave 5.**

Exact checks performed (2026-10-04 ~12:45 CEST):
1. Raw fetch of `https://raw.githubusercontent.com/oracle/graal/master/compiler/CHANGELOG.md`
   — **14,501 bytes**, identical byte count to wave 5's fetch. Top section
   still **GraalVM 25.5 (Internal Version 25.5.5)**; 10 `##` sections total
   (25.5, 25.4, 25.3, 25.1, 25, then the 24.x/23.x legacy series); the full
   GR-entry list is unchanged (same 25.5 dozen GR-79881/79883…GR-79031;
   GR-77137 priority-inlining and GR-28213 vectorization still in the 25.3
   section). **No 25.6 section, no new compiler entries past the wave-5
   snapshot.**
2. `https://api.github.com/repos/oracle/graal/tags?per_page=10` — only legacy
   community tags (`vm-enterprise-0.33` … `vm-enterprise-0.28.1`,
   `vscode-20.3.0`); no 25.x release signal. Consistent with wave-5's
   "releases API is a dead end"; the CHANGELOG.md master fetch remains the
   authoritative channel and shows no movement.

**Disposition:** no new candidate bar material; the speculation story remains
GR-79590 (speculative store checks) + GR-79031 (exact arithmetic via
loop-limit deopt). Still no new escape-analysis capability, no new inlining
policy since wave 3.

---

## T2.2 JEP 544 watch (JDK 28 ramp-down survival watch)

**Negative — no evidence of further movement; direct re-verification was
blocked this wave.**

Exact checks performed (2026-10-04 ~12:50 CEST):
1. `browser.open` on `https://openjdk.org/jeps/544` — **refused (HTTP 403,
   upstream access rejected, tool-level block)**. Per the subagent
   constraints this request was **not retried, neither through the same tool
   nor through any other tool/endpoint** (no curl fetch of the JEP page this
   wave). Last direct verification stands at wave 5: **Status=Targeted,
   Release=28** (direct JEP-page fetch 2026-10-04 ~11:40 CEST).
2. Full-text re-read of InfoWorld "Java 28 starts to take shape"
   (fetched via browser.open 2026-10-04; article ~10 days old, crawled 1 day
   ago): still frames AOT code compilation as "has been proposed" — the
   same stale framing wave 5 recorded; **no update to reflect the
   Candidate→Targeted transition, and no mention of Integrated status or
   ramp-down notes.**
3. JVM Weekly vol. 192 (crawled 12h before the wave-6 check): still lists 544
   among September candidates — stale relative to wave 5's direct evidence.
4. LavX News piece (crawled 8 days ago): still "feature proposal" framing.

**Result:** no secondary source reports Targeted→Integrated movement, and none
reports ramp-down notes or survival concerns either. The JDK 28 ramp-down
window (Dec 2026–Jan 2027, GA ~Mar 2027) is still months out; nothing to
survive yet. **Recorded survival status: last verified Targeted for JDK 28
(wave 5); wave-6 re-verification blocked — must be re-checked directly at the
next wave.**

---

## T2.3 Valhalla JEP 401 / primitive-class movement

**Negative — no status change, no primitive-class JEP movement.**

Exact checks performed (2026-10-04 ~12:45 CEST):
1. Direct curl fetch of `https://openjdk.org/jeps/401` (HTTP 200): metadata
   rows read verbatim — Owner=Dan Smith, Type=Feature, Scope=SE,
   **Status=Integrated, Release=28**. Unchanged from waves 4–5 ("Value
   Objects (Preview)").
2. Web search for primitive-class movement (October 2026):
   `valhalla-docs/site/_index.md` (crawled 3 days ago) still lists the same
   five feature streams — **(4) "Unifying Primitives and Classes" remains a
   future feature set**, described as boxing improvements for method
   invocations/generics + conversions/operators for value classes. No new JEP
   number for primitive classes. Wikipedia's Project Valhalla page likewise
   treats primitive classes as future (inline storage, boxing to value-class
   versions).

**Disposition:** SURV-061's value-field-inlining line unaffected; no new
value-semantics optimization precedents. The JVM Weekly vol. 192 arraycopy
watch note from wave 5 stands (no follow-up seen this wave).

---

## T2.4 T2 lane (OpenJ9 / Azul / .NET) — bar-clearing items only

**One watch note (Azul); OpenJ9 negative; .NET nothing surfaced.**

Exact checks performed (2026-10-04 ~12:55 CEST):
1. **Azul — watch note (marketing claim, no candidate):** Azul press release
   dated 2026-10-01 via Business Wire (search crawl 1 day ago) announces Azul
   Prime's "Cloud Native Compiler" delivering **2x–5x faster application
   warm-up vs standard OpenJDK** (their claim) by centralizing and caching JIT
   compilations across a fleet of JVMs via Optimizer Hub, streaming fully
   optimized compiled code to new instances at startup before they take
   traffic. This is a product announcement, not a new published technique —
   ReadyNow/Optimizer Hub profile-and-compilation reuse is pre-existing
   capability. Recorded as a watch note because fleet-wide sharing of
   profile-guided/compiled code is adjacent to graalgo's AOT/JIT-hybrid
   thesis and the Leyden bucket; no raw candidate filed (vendor claim, no
   technical disclosure of a transferrable technique). Source:
   `https://www.businesswire.com/news/home/20261001497258/en/Azul-Prime-Delivers-2x-5x-Faster-Java-Warm-up-Time-Versus-Standard-OpenJDK-By-Enabling-JVMs-to-Learn-from-Each-Other`
   (fetched 2026-10-04 via search index; claim not independently verified).
2. **OpenJ9 — negative:** `https://api.github.com/repos/eclipse-openj9/openj9/releases?per_page=5`
   shows latest **openj9-0.62.0** (published 2026-09-15T20:16:10Z). Keyword
   scan of the 28,530-char release body (JIT/jit, inline, optimization,
   speculat*, escape, vector, SIMD, PGO, profile) found only housekeeping:
   "Print total bytecode size for a method including inlined methods" (a
   -Xjit verbose/debug item), clang-format application, and "Enable and run
   constrefs-related optimizations" (Valhalla constref support for JDK 28).
   **No new JIT optimizer technique.** Not bar-clearing.
3. **.NET — negative:** the October-2026 search sweep surfaced no .NET
   JIT/runtime optimizer news (results dominated by the Azul announcement
   and OpenJ9 release notes). No exact-checkable candidate material.

---

## T4.1 golang/go #24314 — status check

**Negative — open, no movement.**

Exact check: `https://api.github.com/repos/golang/go/issues/24314`
(2026-10-04 ~12:45 CEST): state=**open**, `updated_at`=2022-07-13T16:58:42Z,
comments=8, labels=[Performance, compiler/runtime], title
"cmd/compile: slice update with self subslice - write barrier probably not
needed (?)". Byte-identical to wave 4 and wave 5. The barrier-elision
negative stands.

---

## T4.2 golang/go #77893 — the five CLs (MERGED/ABANDONED check)

**Negative — none of the five CLs has moved; SURV-041's gap does NOT narrow.**

Exact check: Gerrit REST API `https://go-review.googlesource.com/changes/<n>?o=CURRENT_REVISION`
for each CL, 2026-10-04 ~12:45 CEST (note: the JSON field is `_number`, not
`number`):

| CL | Subject | Status | Updated |
|---|---|---|---|
| [758140](https://go.dev/cl/758140) | runtime: add starting stack size benchmark and add GODEBUG=gcstacksizetrace | **NEW** | 2026-03-23 |
| [758141](https://go.dev/cl/758141) | runtime: exclude inactive goroutines from starting stack size heuristic | **NEW** | 2026-03-30 |
| [764220](https://go.dev/cl/764220) | runtime: estimate starting stack sizes by calculating percentiles of scanned sizes | **NEW** | 2026-04-08 |
| [793780](https://go.dev/cl/793780) | runtime: cache grown goroutine stacks to reduce stack-growth cost | **NEW** | 2026-07-16 |
| [793920](https://go.dev/cl/793920) | runtime: add stack size watermark for initial stack size heuristic | **NEW** | 2026-06-24 |

All five statuses and `updated` timestamps are byte-identical to the wave-5
snapshot. The §6 trigger (any CL → MERGED/ABANDONED) has **not** fired;
SURV-041's distinct edge (per-site adaptive sizing, vs the global-heuristic
CLs) remains intact; bucket THINKING unchanged.

---

## T4.3 Label-probe watch notes #81887, #81846/#81755, #81957, #81968

**Negative — zero activity on all five since wave 5.**

Exact check: GitHub API `https://api.github.com/repos/golang/go/issues/<n>`
for each, 2026-10-04 ~12:50 CEST (anonymous budget 60/hr, full budget
available — not consumed by a full re-sweep, which remains in scope for the
standing cadence):

| Issue | State | Comments | Updated | Delta vs wave 5 |
|---|---|---|---|---|
| [#81887](https://github.com/golang/go/issues/81887) slices.BinarySearchFunc not inlined | open | 3 | 2026-10-01T14:27:25Z | none |
| [#81846](https://github.com/golang/go/issues/81846) simd.Float32s.Neg sign-bit constant | open | 3 | 2026-09-29T18:27:16Z | none |
| [#81755](https://github.com/golang/go/issues/81755) arm64 rematerializes 64-bit constants | open | 5 | 2026-09-29T18:15:58Z | none |
| [#81957](https://github.com/golang/go/issues/81957) GC stack shrinking O(n²) | open | 3 | 2026-10-03T10:50:39Z | none |
| [#81968](https://github.com/golang/go/issues/81968) bound overtakes of lock2 sleepers | open | 1 | 2026-10-02T21:47:09Z | none |

All comment counts and `updated_at` timestamps are byte-identical to wave 5's
probe. No new comments, no label or state changes. Nothing bar-clearing;
the loop-invariant constant-materialization theme (#81846/#81755) remains a
theme watch only.

---

## Raw candidate sketches (wave 6)

**None.** No new transferrable technique surfaced in any lane. The Azul Cloud
Native Compiler item is a vendor marketing claim on pre-existing product
capability, not a published technique — recorded as a watch note only.

---

## First-class negatives / watch notes (wave 6)

1. **Graal 25.x re-sweep:** CHANGELOG.md (master) byte-identical to wave 5
   (14,501 bytes) — still tops at GraalVM 25.5 with the same entries. Graal
   tags API shows only legacy community tags. No new compiler optimization,
   no speculation-policy change. (Check: raw master fetch + tags API,
   2026-10-04.)
2. **JEP 544:** no further movement evidenced; **direct re-verification was
   blocked this wave** (browser tool refused the JEP page, HTTP 403; not
   retried per subagent constraints). Last verified: Targeted / Release 28
   (wave 5 direct fetch). Secondary sources (InfoWorld full article, JVM
   Weekly vol. 192, LavX — all fetched/crawled 2026-10-04) still frame it as
   proposed/candidate with no ramp-down notes. **Next wave must re-verify
   the JEP page directly.** Survival watch: JDK 28 ramp-down Dec 2026–Jan
   2027 still months out.
3. **JEP 401:** unchanged — Integrated / Release 28 / Owner Dan Smith
   (direct curl fetch of the JEP page, metadata rows read verbatim,
   2026-10-04). No primitive-class JEP movement; Valhalla feature stream 4
   ("Unifying Primitives and Classes") still future. SURV-061's line
   unaffected.
4. **Azul (new watch note):** Oct-1-2026 announcement — Azul Prime Cloud
   Native Compiler claims 2x–5x faster warm-up via fleet-wide sharing of
   fully optimized compiled code (Business Wire, their claim, unverified).
   Pre-existing capability, no published technique — watch note only.
5. **OpenJ9:** v0.62.0 (2026-09-15) release body keyword-scanned — only
   housekeeping JIT items + constref/Valhalla support. No bar-clearing
   optimizer work.
6. **#24314:** open, no movement (updated 2022-07-13, 8 comments).
7. **#77893:** all five CLs still NEW with wave-5 timestamps; no
   MERGED/ABANDONED. SURV-041's gap does not narrow.
8. **Label-probe watches:** #81887, #81846, #81755, #81957, #81968 — zero
   activity since wave 5 (comment counts + updated_at byte-identical).
   Nothing bar-clearing.

---

## Method notes

- The browser tool refused `https://openjdk.org/jeps/544` (HTTP 403,
  upstream access rejected) while `browser.open` worked for infoworld.com
  and `browser.search` worked throughout; the JEP-page fetch is
  site-and-tool specific, not a global browser-tool outage. Direct curl was
  deliberately NOT attempted for jeps/544 per the subagent constraint
  against re-requesting a refused fetch through another path — this is a
  methodology limitation of this wave, recorded explicitly rather than
  silently worked around. `https://openjdk.org/jeps/401` fetched fine via
  curl the same day, so openjdk.org is not globally unreachable from this
  VM.
- Gerrit REST: the change number field is `_number`, not `number` (the
  initial wave-6 query failed with KeyError until corrected — no data
  impact; statuses re-read cleanly on retry).
- GitHub anonymous budget: full 60/hr available at end of the wave; the
  standing label-sweep cadence remains affordable (28 list calls for the
  two labels).

## Sources (all accessed/fetched 2026-10-04)

- `https://raw.githubusercontent.com/oracle/graal/master/compiler/CHANGELOG.md`
  (direct fetch; 14,501 bytes, still GraalVM 25.5 section only).
- `https://api.github.com/repos/oracle/graal/tags?per_page=10` (legacy
  community tags only — dead end for current releases).
- `https://openjdk.org/jeps/544` (browser.open refused: HTTP 403, tool-level
  block; NOT retried elsewhere — last direct verification wave 5,
  Status=Targeted, Release=28).
- `https://www.infoworld.com/article/4205791/java-28-starts-to-take-shape.html`
  (full-text browser.open 2026-10-04 — still "has been proposed" framing).
- `https://www.jvm-weekly.com/p/jdk-27-is-here-jvm-weekly-vol-192` (crawled
  ~12h before check — 544 still among September candidates).
- `https://news.lavx.hu/article/jep-544-brings-ahead-of-time-code-compilation-to-java-hotspot`
  (crawled 8 days ago — feature-proposal framing).
- `https://openjdk.org/jeps/401` (direct curl fetch, HTTP 200: Status=
  Integrated, Release=28, Owner=Dan Smith, Scope=SE).
- `https://github.com/openjdk/valhalla-docs/blob/HEAD/site/_index.md`
  (crawled 3 days ago — five feature streams, primitive unification still
  future).
- `https://en.wikipedia.org/wiki/Project_Valhalla_(Java_language)` (crawled
  5 days ago — primitive classes future).
- `https://www.businesswire.com/news/home/20261001497258/en/Azul-Prime-Delivers-2x-5x-Faster-Java-Warm-up-Time-Versus-Standard-OpenJDK-By-Enabling-JVMs-to-Learn-from-Each-Other`
  (Azul Oct-1 announcement, via search index 2026-10-04 — vendor claim,
  unverified).
- `https://api.github.com/repos/eclipse-openj9/openj9/releases?per_page=5`
  + `/releases/tags/openj9-0.62.0` (latest v0.62.0, 2026-09-15; release body
  keyword-scanned).
- `https://api.github.com/repos/golang/go/issues/24314` (state open, 8
  comments, updated 2022-07-13).
- `https://go-review.googlesource.com/changes/{758140,758141,764220,793780,793920}?o=CURRENT_REVISION`
  — all status NEW.
- `https://api.github.com/repos/golang/go/issues/{81887,81846,81755,81957,81968}`
  (all byte-identical comment counts + updated_at vs wave 5).
- `https://api.github.com/rate_limit` (60/hr anonymous budget).
