# Snapshot quarantine manifest

The behavior snapshot ([`go1.27.1.json`](go1.27.1.json)) covers the
differential corpus ([`../differential/corpus/`](../differential/corpus/))
**minus** the programs quarantined here. Quarantine is a gate discipline,
not a parking lot: every exclusion has a dated owner lane, and the list
may only grow through review.

<!-- approved_count: 11 -->
<!-- The line above is machine-read by check-quarantine.py: the count of
     exclusions in backends-p3.json and in the snapshot MUST equal it.
     Growing the list means updating this file in the same change (that
     update IS the review record) and re-running the gate. -->

## Rules

1. **Every exclusion has an owner and a `since` date** — in both the live
   list ([`../differential/backends-p3.json`](../differential/backends-p3.json),
   top-level `exclusions`) and the recorded snapshot (`excluded`). The
   owner is the owning lane area (a person name may replace it when the
   lane has a named owner); `since` is the date the exclusion entered the
   list. [`check-quarantine.py`](check-quarantine.py) enforces this in
   `make snapshot-check` (hence per-push CI via `make all`).
2. **Growth requires review.** Adding an exclusion without updating the
   `approved_count` line above and the table below fails the gate. The
   PR that grows the list is the review: it must state why the program
   cannot be in the baseline yet and which lane owns the fix.
3. **Shrinkage is the goal.** When an owning lane fixes the underlying
   gap, the program leaves the list (as `43_range_over_func` did on
   2026-10-15 when PKG-039 landed) and the count here drops with it.
4. **Re-recording drops the metadata — re-attach it.** `make snapshot`
   regenerates the snapshot from the config, and the harness
   `Exclusion` struct carries only `program` + `reason`, so a fresh
   record loses `owner`/`since` in the snapshot file. After any
   re-record, run `python3 test/snapshots/check-quarantine.py --sync`
   (copies `owner`/`since` from `backends-p3.json` into the snapshot for
   matching program+reason pairs) and re-run the gate before committing.
   The gate fails loudly if this step is skipped, so a re-record can
   never silently un-quarantine the discipline.
5. **This lane does not fix the underlying bugs.** In particular
   `14_goroutines_sum` is a known scheduler bug ("scheduler loses partial
   sums") owned by the scheduler lane; this manifest only records its
   quarantine, it does not authorize parking it here indefinitely — the
   owning lane's fix is what removes the row.

## Current exclusions (11 of 49 corpus programs; 38 recorded)

| Program | Owner lane | Since | Reason (from the list) |
|---|---|---|---|
| `10_closures` | frontend-lane | 2026-10-13 | call via index expr `ops[0](5)` not in frontend/v1 scope (WS D) |
| `14_goroutines_sum` | scheduler-lane | 2026-10-13 | flaky: scheduler loses partial sums (32/34 vs 36); scheduler bug filed |
| `17_generics` | frontend-lane | 2026-10-13 | union type constraints (`~int\|~int64`) not supported by P3SyncMain |
| `18_errors` | stdlib-synth-lane | 2026-10-13 | `errors.As` needs reflect (not in scope) |
| `19_sort` | stdlib-synth-lane | 2026-10-13 | `sort.Slice` needs reflect (not in scope) |
| `27_panic_trace` | backend-runtime-lane | 2026-10-13 | panic trace format differs (`main.level3(...)` vs `level3()`); fidelity gap |
| `28_exit_code` | stdlib-synth-lane | 2026-10-13 | `os.Exit`/`os.Stderr` not in synthetic os scope |
| `37_utf8` | stdlib-synth-lane | 2026-10-13 | `strings.Builder.WriteString` not in synthetic strings |
| `39_named_returns` | frontend-lane | 2026-10-13 | named result assignment frontend bug (reference to unknown object id) |
| `40_method_values` | frontend-lane | 2026-10-13 | method values not in scope |
| `42_json` | stdlib-synth-lane | 2026-10-13 | `encoding/json.Marshal` not in synthetic json scope |

History: the list held 12 programs at creation (2026-10-13, M10 wiring);
`43_range_over_func` graduated on 2026-10-15 with the PKG-039 fix.
