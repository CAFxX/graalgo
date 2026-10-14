# GODEBUG surface (P2-M5, WS E)

Versioned registry of every GODEBUG knob: [src/main/resources/graalgo/sched/godebug/registry.json](../../../../internal/backend/runtime/src/main/resources/graalgo/sched/godebug/registry.json)
(current version **3**, 78 knobs + 9 removed). The registry is loaded at
launcher startup by `graalgo.sched.launcher.GoDebug`; unknown knob names,
malformed values, and non-default values of unsupported knobs are hard
errors (exit 2), never silently ignored.

## Triage principles

1. **EMULATED** — we can produce the gc-observable behavior.
2. **UNSUPPORTED** — the user explicitly asks for output/behavior we
   cannot produce and would observe its absence; setting a non-default
   value is a hard error.
3. **NO-OP** — the knob cannot have observable effect in this runtime
   (subsystem absent, or its precondition is impossible). Accepted and
   ignored, with a documented justification.
4. Knobs whose effective behavior permanently equals a *non-default*
   value are NO-OP and the effective values are documented in
   [DIVERGENCES.md](DIVERGENCES.md) DIV-M5-5.

## Dispositions

- **EMULATED** — graalgo implements gc-compatible observable behavior.
- **NO-OP** — accepted and ignored, with a documented justification.
- **UNSUPPORTED** — setting it to a non-default value is a hard error,
  with a documented justification. The default value is always accepted.
- **REMOVED** — gc-deleted knobs: setting an "old" value is a hard error
  (matching gc's startup fatal); other values are accepted and ignored.

## Deliberate divergences from gc

1. **Unknown knob → hard error** (gc silently ignores). Rationale: a typo
   (`GODEBUG=schedtace=100`) silently doing nothing is worse than failing loud.
   ([DIVERGENCES.md](DIVERGENCES.md) DIV-M5-1)
2. **Malformed value → hard error** (gc keeps the old value silently).
   (DIV-M5-2)
3. **Bare field without `=` → hard error** (gc skips silently). (DIV-M5-3)
4. **Emulated trace formats are graalgo-shaped, not byte-identical.**
   `schedtrace` prints `SCHED <ms>ms: goroutines=N cap=C carriers=K`
   (gc's P/M fields have no counterpart); `gctrace` prints
   `gc <n> @<s>s <name>: <ms>ms, heap <b>-><a> MB` with JVM-heap numbers.
   The emulated observable is "a periodic/per-cycle line on stderr".
   (DIV-M5-4)
5. **Five NO-OP knobs permanently behave as a non-default value**
   (`asyncpreemptoff`, `dontfreezetheworld`, `containermaxprocs`,
   `updatemaxprocs`, `tracebacklabels`; see DIV-M5-5).
6. **`panicnil` unrecovered rendering**: with `panicnil=1`, `recover()`
   after `panic(nil)` returns nil exactly like gc (byte-identical), and
   an uncaught `panic(nil)` prints `panic: nil`. (No divergence.)

## Emulated knobs in detail

### schedtrace=N / scheddetail=1

Every N milliseconds a daemon thread prints to stderr:

```
SCHED 3315ms: goroutines=3 cap=2 carriers=2
```

With `scheddetail=1`, an additional line per tick:

```
SCHED-DETAIL 3315ms: quantum=10ms maxinflight=3
```

### gctrace=1

Every JVM GC cycle prints to stderr:

```
gc 1 @1.422s G1 Young Generation: 7ms, heap 25->13 MB
```

Numbers are JVM-heap sourced. Verified differential vs gc by
`tools/godebug-diff` (both emit per-cycle lines; program stdout identical).

### panicnil=1

Restores pre-Go-1.21 `panic(nil)` semantics: `recover()` returns nil
instead of `*runtime.PanicNilError`. Implemented via the Phase-2
`graalgo.runtime.Panic` same-FQN overlay (P2-D8 shadowing); drift-guarded
by `PanicOverlayDriftTest`. Verified byte-identical vs
`GODEBUG=panicnil=1 go run`.

## Registry


### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `adaptivestackstart` | `1` | gc varies initial goroutine stack size adaptively | Goroutine stacks are JVM carrier stacks of fixed size (-Xss); there is no adaptive initial stack to enable or disable. |
| `allowmultiplevcs` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `asyncpreemptoff` | `0` | disables SIGURG-based async preemption | No signal-based async preemption exists in graalgo; preemption is via cooperative safepoint polls. Effective behavior always equals asyncpreemptoff=1. |
| `cgocheck` | `1` | cgo pointer checks (0=off, 1=cheap, 2=expensive via GOEXPERIMENT) | No cgo in graalgo; there are no cgo pointers to check. Values 0-1 accepted; >1 denied like gc (requires GOEXPERIMENT=cgocheck2 at build time). |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `checkfinalizers` | `0` | GC runs partial STW collections to diagnose finalizer issues | Requires gc's partial stop-the-world analysis of finalizer reachability; JVM Cleaner-based finalizers (P2-M4) cannot be analyzed in this mode. |
| `clobberfree` | `0` | fills freed spans with garbage to catch use-after-free | Would require allocator cooperation to poison freed spans; the JVM heap offers no such hook. Setting it would silently provide no checking. |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `containermaxprocs` | `1` | derive GOMAXPROCS from cgroup CPU quota (def 1) | Cgroup CPU quota is never consulted; GOMAXPROCS comes from --gmp or host carriers. Effective behavior always equals containermaxprocs=0. |
| `cryptocustomrand` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `dataindependenttiming` | `0` | crypto/subtle constant-time discipline | No crypto/subtle package; no data-dependent timing to normalize. |
| `decoratemappings` | `1` | annotate /proc/self/maps with [anon: Go: ...] (def 1) | No direct memory-mapping management; nothing to annotate. |
| `disablethp` | `0` | disable transparent huge pages for the heap (Linux) | Memory management is delegated to the JVM; THP policy is not controllable from the runtime. |
| `dontfreezetheworld` | `0` | don't preempt all threads on fatal panic/throw | The fatal-panic path never freezes the world; effective behavior always equals dontfreezetheworld=1. |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `efence` | `0` | electric-fence allocator: one object per page | Would require one-object-per-page allocation; impossible on the JVM heap. Setting it would silently provide no checking. |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `embedfollowsymlinks` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `execerrdot` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `fips140` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `fips140ems` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `gccheckmark` | `0` | checkmark GC mode: verify mark bits with second STW cycle | Requires running the collector in a verifying checkmark mode; the JVM GC cannot be driven this way. |
| `gcpacertrace` | `0` | log GC pacer decisions | The JVM GC has no Go-style pacer; there are no pacer decisions to log. |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `gcshrinkstackoff` | `0` | disable goroutine stack shrinking | No shrinkable Go stacks; goroutines run on JVM carrier stacks. |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `gcstoptheworld` | `0` | 1=force STW GC, 2=log STW timing | Cannot force a stop-the-world collection per cycle on the JVM GC, nor attribute STW timing in gc's terms. |

### EMULATED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `gctrace` | `0` | print a line per GC cycle to stderr | Emulated: a line is printed to stderr per JVM GC cycle (shape-compatible with gc's gctrace line; numbers are JVM-heap sourced, documented in GODEBUG.md). |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `gocachehash` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `gocachetest` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `gocacheverify` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `gotestjsonbuildtext` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `harddecommit` | `0` | use MADV_DONTNEED aggressively (decommit vs advise) | Memory management is delegated to the JVM. |
| `htmlmetacontenturlescape` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `http2client` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `http2debug` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `http2server` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `httpcookiemaxnum` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `httplaxcontentlength` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `httpmuxgo121` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `httpservecontentkeepheaders` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `inittrace` | `0` | print package initialization timing | Package initialization lives in the Phase-1 frontend (frozen); init timing cannot be traced from Phase 2. |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `installgoroot` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `invalidptr` | `1` | crash on obviously-invalid pointers (def 1) | The value model has no unsafe pointer arithmetic; the 0x1 poison check has nothing to check. |
| `jstmpllitinterp` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `madvdontneed` | `1` | use MADV_DONTNEED instead of MADV_FREE (def 1 on Linux) | Memory management is delegated to the JVM. |
| `memprofilerate` | `524288` | heap profile sample rate in bytes (def 524288) | No heap profiler is produced; the sampling rate is moot. |
| `multipartmaxheaders` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `multipartmaxparts` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `multipathtcp` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `netdns` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `netedns0` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |

### EMULATED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `panicnil` | `0` | 1=pre-1.21 panic(nil) semantics (recover returns nil) | Emulated: panicnil=1 restores pre-1.21 panic(nil) semantics (recover() returns nil instead of *PanicNilError) via the Phase-2 graalgo.runtime.Panic overlay. |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `profstackdepth` | `128` | max stack depth captured in profiles (def 128) | No CPU/heap profiles are produced; the depth limit is moot. |
| `randautoseed` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `randseednop` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `rsa1024min` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `sbrk` | `0` | use sbrk allocator (ancient) | No sbrk allocator exists. |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `scavtrace` | `0` | log scavenger activity | JVM scavenging is not observable in gc's scavenger-trace terms. |

### EMULATED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `scheddetail` | `0` | extra detail in schedtrace output | Emulated: adds per-carrier and quantum detail to the schedtrace lines (graalgo format, documented in GODEBUG.md). |
| `schedtrace` | `0` | print scheduler state every N ms to stderr | Emulated: a scheduler-state line is printed to stderr every N ms (graalgo format, documented in GODEBUG.md; gc's P/M fields have no counterpart). |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `tarinsecurepath` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `tlsmaxrsasize` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `tlsmlkem` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `tlssecpmlkem` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `tlssha1` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `traceadvanceperiod` | `0` | execution-tracer advance period | No execution tracer exists. |
| `traceallocfree` | `0` | detailed alloc/free events in execution traces | No execution tracer exists. |

### UNSUPPORTED

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `tracebackancestors` | `0` | include goroutine creator ancestors in traces | Goroutine creators are not tracked; ancestor labels cannot be produced. |

### NOOP

| knob | default | gc behavior | graalgo disposition |
|---|---|---|---|
| `tracebacklabels` | `1` | include pprof labels in goroutine headers (def 1) | Goroutine labels cannot exist in graalgo (no pprof label API); with no labels to show or hide, both values are observably identical. |
| `tracecheckstackownership` | `0` | execution-tracer stack-ownership checks | No execution tracer exists. |
| `tracefpunwindoff` | `0` | disable frame-pointer unwinding in tracer | No execution tracer exists. |
| `updatemaxprocs` | `1` | update GOMAXPROCS on cgroup quota change (def 1) | Cgroup CPU quota is never consulted; effective behavior always equals updatemaxprocs=0. |
| `urlmaxqueryparams` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `urlstrictcolons` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `winreadlinkvolume` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `winsymlink` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `x509negativeserial` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `x509rsacrt` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `x509sha256skid` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `x509sslcertoverrideplatform` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `x509usefallbackroots` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `x509usepolicies` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |
| `zipinsecurepath` | `0` | non-runtime subsystem knob (see internal/godebugs) | Subsystem not present in the graalgo runtime (no net/http, crypto, cmd/go, etc.); the knob is accepted and ignored. |

### REMOVED (gc-deleted; old value denied like gc's fatal)

| knob | removed in | old values |
|---|---|---|
| `asynctimerchan` | Go 1.27 | 1, 2 |
| `gotypesalias` | Go 1.27 | 0 |
| `tls10server` | Go 1.27 | 1 |
| `tls3des` | Go 1.27 | 1 |
| `tlskyber` | Go 1.24 | 0 |
| `tlsrsakex` | Go 1.27 | 1 |
| `tlsunsafeekm` | Go 1.27 | 1 |
| `x509keypairleaf` | Go 1.27 | 0 |
| `x509sha1` | Go 1.24 | 1 |

## Changing the registry

1. Edit `registry.json` (add/remove/retriage a knob).
2. Bump `version`.
3. Update `EXPECTED_HASH` in `GoDebugTest.testRegistryKnobHash` (it fails
   until you do — that is the machine check; it covers name+disposition+default).
4. Regenerate this document's table from the JSON.
5. `godebug-diff` green if an EMULATED knob changed.
