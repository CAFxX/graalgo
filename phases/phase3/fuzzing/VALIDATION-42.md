# Generator fix validation — 42 pilot-E seeds re-run (2026-10-11)

Fixed generator: commit 6c1b14b (wt/graalgo-p3/m9-fuzz).
Method: `bash /tmp/rerun42.sh` — regenerate each seed with the fixed
generator, `go build` (build seeds), build + `timeout 10` run with output
size check (hang seeds). Full log: /tmp/rerun42.log.

## 38 GC_BUILD_FAIL seeds (constant division by zero)

BEFORE (pilot E): 0/38 built (`invalid operation: division by zero`).
AFTER: **38/38 build OK**.

Root cause: `float_divisor_safe` had two defects —
(1) the outer-paren strip removed a leading `(` and trailing `)` without
checking they matched, unbalancing `((A)) / (B)` so `eval` threw and the
divisor passed through unchanged;
(2) scientific notation (`1e10`) contains the letter `e`, fooling the
identifier check into misclassifying a constant-zero divisor as runtime.
Seed 1000061: divisor `((1.5-float32(1.5)) * ...)` == 0.0 slipped through;
now replaced with `1.0`.

## 4 GC_HANG seeds (output bombs / hangs)

BEFORE (pilot E): 0/4 terminated in 10s (seed 1000794: 3.4GB gc.out).
AFTER: **4/4 terminate with bounded output**.

| seed    | rc | output bytes (before → after) |
|---------|----|-------------------------------|
| 1000299 | 0  | hang → 2,589                  |
| 1000325 | 0  | hang → 195                    |
| 1000375 | 0  | hang → 721                    |
| 1000794 | 0  | 3.4GB → 95                    |

Root cause: unbounded `for` loop nesting (no depth limit) with prints
and appends inside. Fix: cap loop nesting depth at 2.

## Harness

`diff_one.sh` now classifies >1MB gc output as `OUTPUT_BOMB`
(generator-rejected) and skips the graalgo leg on bombs.
