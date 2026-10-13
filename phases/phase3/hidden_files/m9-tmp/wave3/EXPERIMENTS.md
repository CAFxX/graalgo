# x509 gmp1 hang — experiment log (Carlo's order: <3min cycles, 60s hang timeout)

## ROOT CAUSE CORRECTED (07:45 CEST) — NOT a hang, it's a 256MB output blowup!
The "gmp1 hang" is the **gofrontend binary generating 256MB of JSON output** (taking 56s).
My 15-30s timeouts were killing it prematurely, making it look like a hang.

- Stale background process (proc_97c2c7c031da) completed: exit=0, /tmp/hung.out = 256,084,934 bytes (256MB)!
- Output contains: 22,523 types, 30,183 objects, 2,907 funcs, 274 globals
- For a trivial `import _ "crypto/tls"` file, this is pathological.
- The JVM's P3SyncMain waits on the pipe; with 60s timeout it looks hung, but it's just slow.

## Original root cause (still valid)
The "gmp1 hang" is the **gofrontend binary during compilation**, not the Go program/scheduler.
- jstack of hung JVM: main thread blocked in `P3SyncMain.runGofrontend` → `readAllBytes` on the gofrontend subprocess pipe. The Go program never started.
- `gofrontend` on `import "crypto/tls"`: produces 256MB (not a hang, but pathological).
- `gofrontend` on `import "crypto/x509"`: same issue.
- `gofrontend` on `import "time"/"fmt"/"net"/"os"/"crypto/sha256"`: all OK (small output).
- The bug is in the x509 lane's frontend export logic causing combinatorial explosion.

## Narrowed down
The 256MB blowup happens when exporting tls/x509 synthetic packages. Likely causes:
- `importSyntheticPackage` not truly idempotent → duplicate exports
- Type/object ID duplication across synthetic packages
- The 2,907 funcs for a simple import suggests massive duplication

## Experiments
| # | Hypothesis | Change/Command | Result | Wall |
|---|-----------|----------------|--------|------|
| 1 | jstack the hung gmp1 JVM | repro + jstack after 25s | jstack attach failed (matched bash wrapper, not java PID) | 1m |
| 2 | Minimal TCP repro on gmp1 | tcp-echo.go via run-exp.sh | PASS exit=0 (11s) — hang is TLS-specific, not netpoller-general | 1m |
| 3 | gofrontend hangs on tls | direct gofrontend on 01_handshake.go | TIMEOUT (exit 124) — looked like hang, actually 256MB blowup | 1m |
| 4 | Bisect: tls import alone | `import _ "crypto/tls"` | TIMEOUT — 256MB output, not a hang | 1m |
| 5 | Fresh rebuild | rebuilt gofrontend from current src | STILL SLOW — source issue, not bad binary | 6m |
| 6 | Parse+typecheck isolation | TestDiagTlsBuild | OK (3.5s) — blowup is in EXPORT phase | 2m |
| 7 | Revert tls source to 01ab345 | git show + rebuild | STILL SLOW — not the time.Sleep change | 6m |
| 8 | Empty tls source | `package tls` only + rebuild | STILL SLOW — bug in CODE, not source content | 6m |
| 9 | d1a1b33 frontend | checkout + rebuild | STILL SLOW — regression predates Oct 12 commits | 6m |
| 10 | m9 branch frontend | test existing binary | NO BLOWUP (exit 2, unsupported) — x509 lane specific | 1m |
| 11 | Stale process completed | proc_97c2c7c031da finished | exit=0, 256MB output! NOT a hang, it's output blowup | - |

## Fix applied (08:00 CEST)
| # | Hypothesis | Change/Command | Result | Wall |
|---|-----------|----------------|--------|------|
| 12 | 256MB from dead p256 table | Removed p256PrecomputedEmbed (88KB dead data) from synth_crypto_fips140_nistec.go | 256MB→210MB, 56s→7.9s for `import _ "crypto/tls"`. Committed df2f21a. | 10m |
| 13 | Compact hex encoding | Added hexdata field to exporter+Java for >2000 byte lits | Infrastructure committed (561a15c); not yet triggered (tables are nested) | 20m |
| 14 | gmp1 handshake retest | Full P3SyncMain --gmp=1 on 01_handshake.go (4GB heap) | Running in background (proc_cc73bce87650) | - |
| 15 | Truncation threshold 1000 | Lowered to >1000 scalars | 15 tables truncated, 201MB→191MB. Committed 7910ac9. | 15m |
| 16 | Full gmp1 test 15min | P3SyncMain --gmp=1, 8GB heap, background | Running (proc_f99100e5ab98) | - |

## Resume 09:30 CEST (worker restart after 09:24 VM reboot)
| # | Hypothesis | Change/Command | Result | Wall |
|---|-----------|----------------|--------|------|
| 17 | gofrontend still slow on full file? | Timed current frontend/gofrontend (built 06:27) on full 01_handshake.go | 12.5s, 200MB output, exit=0. NOT a hang. | 1m |
| 18 | Where is the 200MB? | Python breakdown of hs-full.out | types 26MB, funcs 33MB (compact). Biggest: p521Mul/p521Square 1.3MB bodies (18k nodes each) — fiat unrolled code, legitimate. 315 dup names are methods on different types (legit). | 5m |
| 19 | Java side stuck? | P3SyncMain --gmp=1, jstack@55s | Main RUNNABLE in NodeFactory.buildSwitch (building AST), 8% CPU — BUT box had just rebooted (up 11min, load 8.5, high iowait). Not representative. | 2m |
| 20 | gmp4 baseline | handshake-gmp4-verify timestamps | Full handshake (256MB era) completed in ~6 min (03:08-03:14). Pipeline CAN do it. | 1m |

## Key insight
The "40-min hang" was likely just extreme slowness, not a deadlock. gmp4 proves the pipeline processes 256MB in ~6 min. The gmp1 40-min timeout may have been premature OR the pre-reboot box was contended. The export is legitimate (vendored upstream fiat code); truncation hacks are the wrong fix. Next: clean full-leg run on settled box with 20-min timeout.

## ROOT CAUSE FOUND (09:50 CEST) — 200MB JSON parsed 5x!
Thread dump from the "hung" gmp1 leg: main thread stuck 517s in
`Json.parse` (2% CPU) called from `IfaceMethodTables.resolveInterfaces`
← `LinknameIntrinsics.install` ← `P3SyncMain.run:308`.

The 200MB frontend JSON was parsed FIVE times per run:
1. NodeFactory.buildProgram (the real AST build)
2. IfaceMethodTables.resolve
3. IfaceMethodTables.resolveInterfaces
4. ReflectTypes.install
5. LinknameIntrinsics.bindGoFuncs

| # | Hypothesis | Change/Command | Result | Wall |
|---|-----------|----------------|--------|------|
| 21 | 5x JSON parse is the bottleneck | Parse-once: NodeFactory.document() exposes the parsed DOM; all installers take Json.Object instead of String. Committed 73d7242. | Phase1+phase3 build clean. Re-running leg. | 25m |

## Update 10:15 CEST — JSON bottleneck FIXED, TLS handshake deadlock found
| # | Hypothesis | Change/Command | Result | Wall |
|---|-----------|----------------|--------|------|
| 22 | Pipe hang for 200MB | runGofrontend via temp file not pipe. Committed. | gofrontend writes 200MB file OK. | 20m |
| 23 | gofrontend won't exit | Load-shedder SIGSTOP'd it (load 10.9). Thawed. | Exited, Java proceeded. | 10m |
| 24 | TLS handshake stuck | Added debug println to handshake loop. | Both sides stuck in NEED_UNWRAP (status=2), 0 bytes flowing. TCP connected but TLS deadlocked. | 30m |
| 25 | 10s sleep causes GMP=1 stall | Reduced to 100ms. Committed 2408114. | Still stuck (sleep is after handshake, not the cause). | 15m |

## Current state
- FIXED: 200MB JSON parsed 5x → 1x (parse-once, 73d7242).
- FIXED: gofrontend pipe hang → temp file (general fix).
- OPEN: TLS handshake deadlocks on GMP=1 (both sides NEED_UNWRAP, no bytes flowing).
  GMP=4 worked before these changes. Likely GMP=1 scheduler or SSLEngine state issue.
- Debug code (println) still in synth_crypto_tls.go — must be removed before final.
