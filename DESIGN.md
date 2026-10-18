# DESIGN — R23-P1-3: TLS error channel (no host exceptions across the Truffle boundary)

- **Review:** R23 (interop deep audit), finding R23-3 (P1)
- **Base:** master tip `56a3dd68`
- **Audit context:** `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/review23-interop-audit.md` §R23-3
- **Scope:** design only. The implementation lane builds exactly this.

## 1. Problem

Every entry point of `TlsRuntime` can throw a host (Java) exception across the
interop boundary, which aborts the whole process. Under gc the same conditions
surface as Go `error` values returned from `Dial`/`Listen`/`Handshake`, never as
process aborts. This also contradicts the file's own `-1`/`null` error-signalling
convention used by `beginHandshake`, `engineStatus`, `wrap`, `unwrap`.

Design goal: **no `Throwable` of type `Exception` ever escapes a `graalgo.tls.*`
trap.** Failures are signalled with in-band sentinels; detail travels through an
explicit error channel the Go synthetic side translates into Go `error` values.
No bridges, no special cases: one uniform mechanism for all traps.

---

## 2. Exact failure points

File: `internal/backend/interop/src/main/java/graalgo/p3tls/TlsRuntime.java`
(line numbers at base `56a3dd68`; verified in the worktree).

| # | Location | Current behavior |
|---|----------|------------------|
| 1 | `num()`, lines 146–151 | `throw new IllegalArgumentException("expected Number, got " + o.getClass())` on wrong host type. Called with the *handle* argument outside any try-block in `beginHandshake` (412), `engineStatus` (430), `wrap` (456), `unwrap` (533), `runTasks` (622), `peerCertificates` (638), `connectionState` (665), `hasPending` (771), `close` (780) → host exception escapes. |
| 2 | `str()`, lines 153–158 | Same throw pattern for wrong host type. Reached from `newClientEngine`/`newServerEngine` — currently inside their try-blocks, so caught and re-wrapped (see 4), but still part of the throw-based discipline. |
| 3 | `bytes()`, lines 160–180 | Same throw pattern (line 179); also an unchecked `(byte[]) gs.array()` cast (line 171) that can throw `ClassCastException`. Called inside try-blocks today, but `wrap`/`unwrap` call it for *data* args where failure must become a per-call error, not a crash. |
| 4 | `newClientEngine`, lines 281–320 | `catch (Exception e)` at 313–315 → `throw new IllegalStateException("TlsRuntime.newClientEngine failed", e)`. Triggered by e.g. invalid SNI server name (`new SNIHostName(serverName)` at ~305 throws `IllegalArgumentException`), bad `SSLContext` init, unknown cipher-suite names from `setEnabledCipherSuites` (`IllegalArgumentException`). |
| 5 | `newServerEngine`, lines 326–368 | Same pattern at 361–363. Triggered by malformed `chainDER`/`keyParams`, `CertificateFactory` parse failures, `buildPrivateKey` failures, and the explicit `throw new IllegalArgumentException("unsupported keyAlg: " + keyAlg)` at line 404. |
| 6 | `runTasks`, lines 621–631 | **No try/catch at all.** `task.run()` at line 628 executes arbitrary JDK handshake-task code; any exception it throws escapes directly across the boundary. (Also `num(handleArg)` at 622 throws outside try — see 1.) |
| 7 | `beginHandshake`, lines 411–422 | `num()` at 412 outside try (see 1). The `catch (Exception e)` at 418–420 returns `-1` with no detail — silent. |
| 8 | `engineStatus`, lines 429–… | `num()` at 430 outside try (see 1). `engine.getHandshakeStatus()` is unguarded. Returns `-1` for bad handle with no detail. |
| 9 | `wrap`, lines 455–… | `num(handleArg)` at 456 outside try (see 1). `catch` returns `null` with no detail. |
| 10 | `unwrap`, lines 532–… | `num(handleArg)` at 533 outside try (see 1). `catch` returns `null` with no detail. Note: `null` is *also* the legitimate peer-closed signal (CLOSED path, ~595–601) — the two must stay distinguishable (see §3.5). |
| 11 | `peerCertificates`, lines 637–… | `num()` outside try (see 1). `catch` returns empty bytes with no detail. |
| 12 | `connectionState`, lines 664–… | `num()` outside try (see 1). `catch` returns `-1` with no detail. |
| 13 | `hasPending`, lines 770–773 | `num()` at 771 throws on wrong type (see 1); no failure path at all otherwise. |
| 14 | `close`, lines 779–794 | `void` return; `num(handleArg)` at 780 throws on wrong type (see 1). No way to signal failure. |

Deliberately out of scope (separate lanes): R23-1/R23-2 (cipher-suite name
tables), R23-4 (dead `ECPoint w`), R23-5 (PENDING buffering on CLOSED),
R23-6 (threading contract doc), R23-7 (`splitLengthPrefixed` truncation —
note: if that lane later throws on malformed input, it lands inside
`newServerEngine`'s try-block and is automatically channeled by this design).

---

## 3. Proposed error-channel mechanism

### 3.1 Core idea

One error store, one accessor, one sentinel contract:

- **Sentinel contract for `long`-returning traps:** any negative return value is
  an *error key* (never a valid result — see §3.4 for why no valid result can be
  negative). The Go side MUST pass it to `graalgo.tls.lastError(key)` to obtain
  the message. Non-negative returns keep their existing meanings.
- **Sentinel contract for `Object`-returning traps** (`wrap`, `unwrap`,
  `peerCertificates`): `null`/empty keep their existing meanings (failure /
  peer-closed / no-certs). When the failure carries detail, it is stashed under
  the trap's engine-handle key and the Go side retrieves it with
  `graalgo.tls.lastError(handle)` (see §3.5 for the `unwrap`-CLOSED
  disambiguation).
- Error keys for the slot namespace are **negative** (`-1, -2, …`);
  handle-scoped errors use the **positive** engine-handle key. The two key
  spaces are sign-disjoint, so one map and one accessor serve both with no
  collision possible.

### 3.2 Java side — new state and helpers

Add to `TlsRuntime`:

```java
/** Error detail store. Negative keys: error slots (consume-on-read).
 *  Positive keys: handle-scoped errors for Object-returning traps. */
private static final ConcurrentHashMap<Long, String> ERRORS =
        new ConcurrentHashMap<>();
private static final AtomicLong NEXT_ERROR_ID = new AtomicLong(1);
private static final int MAX_ERROR_LEN = 512;
```

Helpers (all `private static`, none throws):

```java
private static Long optNum(Object o) {
    return (o instanceof Number) ? ((Number) o).longValue() : null;
}
private static String optStr(Object o) {
    if (o instanceof GoString) {
        return new String(((GoString) o).bytes(),
                java.nio.charset.StandardCharsets.UTF_8);
    }
    return null;
}
/** As today: Go nil slice (Java null) -> empty array. Wrong type -> null. */
private static byte[] optBytes(Object o) {
    if (o == null) {
        return new byte[0];
    }
    if (o instanceof GoSlice) {
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();   // D5: []byte is byte[]-backed
        int off = gs.offset();
        int len = gs.len();
        byte[] out = new byte[len];
        System.arraycopy(arr, off, out, 0, len);
        return out;
    }
    return null;
}
private static String typeName(Object o) {
    return o == null ? "null" : o.getClass().getName();
}
```

Failure constructors:

```java
/** Allocates an error slot; returns the (negative) key to hand to Go. */
private static long fail(String what, Throwable e) {
    long key = -NEXT_ERROR_ID.getAndIncrement();
    ERRORS.put(key, truncate("tls." + what + " failed: " + flat(e)));
    return key;
}
private static long failArg(String what, Object badArg, String expected) {
    long key = -NEXT_ERROR_ID.getAndIncrement();
    ERRORS.put(key, truncate("tls." + what + ": expected " + expected
            + ", got " + typeName(badArg)));
    return key;
}
/** Handle-scoped variants for Object-returning traps. */
private static void failHandle(long handle, String what, Throwable e) {
    ERRORS.put(handle, truncate("tls." + what + " failed: " + flat(e)));
}
private static void failHandleArg(long handle, String what,
                                  Object badArg, String expected) {
    ERRORS.put(handle, truncate("tls." + what + ": expected " + expected
            + ", got " + typeName(badArg)));
}
private static String flat(Throwable e) {
    // One line: SimpleName + message, plus one cause level. No stack trace.
    StringBuilder sb = new StringBuilder();
    sb.append(e.getClass().getSimpleName()).append(": ")
      .append(String.valueOf(e.getMessage()));
    Throwable c = e.getCause();
    if (c != null && c != e) {
        sb.append(" caused by ").append(c.getClass().getSimpleName())
          .append(": ").append(String.valueOf(c.getMessage()));
    }
    return sb.toString();
}
private static String truncate(String s) {
    String flat = s.replace('\n', ' ').replace('\r', ' ');
    return flat.length() <= MAX_ERROR_LEN ? flat
            : flat.substring(0, MAX_ERROR_LEN);
}
private static Object toGoString(String s) {
    // Same pattern as p3x509.GoX509.toGoString: Go declares `string` return.
    return new GoString(s.getBytes(java.nio.charset.StandardCharsets.UTF_8));
}
```

The accessor (12th intrinsic):

```java
/**
 * Consumes and returns the error detail for a failed trap call.
 * key: the negative value a long-returning trap returned (error slot),
 *      or an engine handle (handle-scoped error from an Object-returning
 *      trap). Returns "" when nothing is recorded. Never throws.
 * graalgo.tls.lastError(key) -> string
 */
public static Object lastError(Object keyArg) {
    Long k = optNum(keyArg);
    String m = (k == null) ? null : ERRORS.remove(k);
    return toGoString(m == null ? "" : m);
}
```

Notes:

- `lastError` returns an **empty `GoString`, never null**, on miss — this avoids
  depending on how the D5 layer unboxes a Java null into a Go `string`.
- `catch (Exception e)` scope is kept everywhere (not `Throwable`): an `Error`
  (`OutOfMemoryError`, linkage errors, …) still propagates and aborts, matching
  gc's fatal treatment of truly fatal conditions. Document this in the class
  javadoc.
- The existing `throw` statements in `num()`/`str()`/`bytes()` (lines 146–179)
  are **deleted** and replaced by the `opt*` variants above. No other code in
  the file may throw `IllegalArgumentException`/`IllegalStateException`
  anymore; the two `throw new IllegalStateException(...)` in the constructors
  become `return fail(...)`.
- `insecureArg` of `newClientEngine` stays ignored (pre-existing; out of scope).

### 3.3 Per-trap new shapes

`newClientEngine` / `newServerEngine` — keep the whole body in one
`try { … } catch (Exception e) { return fail("<op>", e); }`; replace each
`str()/num()/bytes()` call with its `opt*` variant plus an explicit null check,
e.g.:

```java
String serverName = optStr(serverNameArg);
if (serverName == null) {
    return failArg("newClientEngine.serverName", serverNameArg, "GoString");
}
```

`beginHandshake`, `engineStatus`, `hasPending`, `runTasks`, `connectionState` —
open with:

```java
Long h = optNum(handleArg);
if (h == null) {
    return failArg("<op>.handle", handleArg, "Number");
}
```

then unknown-handle becomes a *detailed* failure instead of bare `-1`:

```java
SSLEngine engine = ENGINES.get(h);
if (engine == null) {
    return failArg("<op>.handle", h, "live engine handle");
}
```

(`failArg` with a `Long` value formats as `expected live engine handle, got
java.lang.Long` — acceptable; or add a dedicated message. Implementation lane
may use `fail("<op>", new IllegalArgumentException("unknown engine handle " + h))`
instead — equivalent. Pick one and be consistent.)

- `engineStatus`: additionally wrap `engine.getHandshakeStatus()` in the
  try/catch (it is currently unguarded).
- `runTasks`: wrap the whole delegated-task loop in try/catch — this closes
  failure point 6 (`task.run()` throwing across the boundary).
- `hasPending`: after the `optNum` check the body cannot fail; keep `1`/`0`.

`wrap` / `unwrap` / `peerCertificates` — open with:

```java
Long h = optNum(handleArg);
if (h == null) {
    return null;   // see §3.5 "no key" limitation
}
SSLEngine engine = ENGINES.get(h);
if (engine == null) {
    return null;   // unchanged: Go pre-checks handle != 0; unreachable otherwise
}
```

Inside the try-block, data-arg violations stash under the handle:

```java
byte[] plain = optBytes(plaintextArg);
if (plain == null) {
    failHandleArg(h, "wrap.plaintext", plaintextArg, "GoSlice");
    return null;
}
```

and the terminal catch becomes:

```java
} catch (Exception e) {
    failHandle(h, "wrap", e);
    return null;
}
```

`unwrap` keeps its CLOSED early-`return null` **without** stashing (see §3.5).
`peerCertificates` keeps returning empty bytes on all failure paths, stashing
under the handle only on the exception path.

`close` — signature change `void` → `long`:

```java
/**
 * Closes the SSLEngine and releases resources. Idempotent.
 * Returns 0; negative error-slot key only if the handle arg has the wrong
 * host type. graalgo.tls.close(handle) -> 0 | -slot
 */
public static long close(Object handleArg) {
    Long h = optNum(handleArg);
    if (h == null) {
        return failArg("close.handle", handleArg, "Number");
    }
    SSLEngine engine = ENGINES.remove(h);
    PENDING.remove(h);
    if (h > 0) {
        ERRORS.remove(h);   // handle-scoped errors die with the handle
    }
    if (engine != null) {
        try {
            engine.closeInbound();
        } catch (Exception ignored) {
        }
        try {
            engine.closeOutbound();
        } catch (Exception ignored) {
        }
    }
    return 0;
}
```

(`ERRORS.remove(h)` is guarded by `h > 0` so a nonsensical negative handle can
never consume someone else's error slot.)

### 3.4 Sentinel-value audit (why negative is unambiguous)

| Trap | Valid returns | Negative meaning after change |
|------|---------------|-------------------------------|
| `newClientEngine` / `newServerEngine` | handle ≥ 1 (`NEXT_ENGINE_ID` starts at 1) | error slot |
| `beginHandshake` | `0` | error slot (replaces bare `-1`) |
| `engineStatus` | `0`–`3` | error slot (replaces bare `-1`) |
| `runTasks` | `0` | error slot (replaces bare `-1`) |
| `connectionState` | packed `(version << 32) \| suite`, version/suite ≥ 0 ⇒ packed ≥ 0 | error slot (replaces bare `-1`) |
| `hasPending` | `0`/`1` | error slot |
| `close` | `0` (new return) | error slot |
| `lastError` | `GoString` (never negative-numeric concern) | n/a |

### 3.5 `unwrap` null disambiguation and the "no key" limitation

`unwrap` returns Java `null` in two situations:

1. **Peer closed** (`Status.CLOSED`): legitimate EOF-ish signal — Go's
   `__tlsConnRead` maps it to `io.EOF` today. This path stashes **nothing**.
2. **Exception / type violation**: stashes under the handle, then returns null.

The Go side distinguishes them by consulting the channel only on null:

```go
app := unwrapTrap(c.engineHandle, tmp[:n])
if app == nil {
    if herr := tlsHandleError(c.engineHandle); herr != nil {
        return __tlsConnReadPair{0, herr}
    }
    return __tlsConnReadPair{0, io.EOF}   // unchanged
}
```

Deliberate limitation (documented, not a gap to fix later): when the **handle
argument itself** has the wrong host type in an Object-returning trap, there is
no key to stash under, so the trap returns null with no retrievable detail and
the Go side reports a generic error. This is unreachable from the Go synthetic
code (it always passes its own `int64` field, which D5 boxes as `Long`); the
P1 requirement — no host exception escapes — still holds.

### 3.6 Thread-safety

- `ERRORS` is a `ConcurrentHashMap`; slot allocation is `AtomicLong`. No locks.
- `lastError` consumes via `remove`, which is atomic: exactly one caller ever
  observes a given error.
- Per-handle errors assume the existing single-driver-per-handle discipline
  (R23-6, unchanged): the Go driver issues at most one failing call per handle
  before reading the error. If two failing calls happen without an intervening
  `lastError`, the second message overwrites the first (last-write-wins) —
  this is the documented protocol, not a race to fix.
- No `ThreadLocal` anywhere: trap calls from the same goroutine may migrate
  across host threads under Truffle, so thread-identity-keyed state would be
  unsound. The explicit key-passing design avoids this entirely.

### 3.7 Lifetime / ownership

- **Error slots (negative keys):** owned by the Go call that received the
  negative return. The Go protocol (§3.8) consumes every negative return
  immediately via `lastErrorTrap`, so slots do not accumulate. If Go code ever
  drops a negative return without reading, that slot leaks — bounded by the
  number of failed calls, and a protocol violation the Java unit tests pin
  down (every test that triggers a negative return consumes it).
- **Handle-scoped errors (positive keys):** owned by the handle. Cleared on
  (a) consume-on-read via `lastError`, (b) overwrite by the next error on the
  same handle, (c) `close(handle)` — `ERRORS.remove(h)` in `close`.
- **After close:** `lastError(handle)` returns `""`; `lastError(slot)` for an
  already-consumed slot returns `""`. Both are benign, never throw.
- No background reaper thread: nothing here is hidden machinery, and the
  bounded leak on protocol violation does not justify it (no-bridges rule).

### 3.8 Go synthetic side changes

File: `internal/frontend/synth_crypto_tls.go`. No changes needed in
`dialImpl`, `dialWithDialerImpl`, `listenImpl`, `__listenerAccept` — errors flow
through `Handshake()` / `engineErr` (see below). (`__listenerAccept` returns
the `Server()` conn without handshaking, exactly like gc's `Accept`; a
construction failure surfaces on the first `Handshake`/`Read`/`Write`, which
matches gc's observable shape.)

1. **New trap + helpers** (next to the other trap declarations, ~lines 365–369
   and 689–711):

```go
//go:linkname lastErrorTrap graalgo.tls.lastError
func lastErrorTrap(key int64) string

// tlsErrorFromSlot materializes the Java-side error detail for a negative
// trap return into a Go error, consuming the slot.
func tlsErrorFromSlot(slot int64) error {
    msg := lastErrorTrap(slot)
    if msg == "" {
        msg = "unknown error"
    }
    return errors.New("tls: " + msg)
}

// tlsHandleError returns the stashed per-handle error, if any, consuming it.
// Nil means "no recorded error" (e.g. unwrap's peer-closed path).
func tlsHandleError(handle int64) error {
    if msg := lastErrorTrap(handle); msg != "" {
        return errors.New("tls: " + msg)
    }
    return nil
}
```

2. **`Conn` struct** (~line 313): add field `engineErr error` — construction
   failure recorded here because `Client`/`Server` cannot return errors (gc API
   compatibility). Document that `engineHandle` never holds a negative value.

3. **`Client()`** (~line 322):

```go
c := &Conn{conn: conn, config: config, isClient: true}
if h := newClientEngineTrap(
    config.ServerName,
    config.InsecureSkipVerify,
    int64(config.MinVersion),
    int64(config.MaxVersion),
    encodeCipherSuites(config.CipherSuites),
); h < 0 {
    c.engineErr = tlsErrorFromSlot(h)
} else {
    c.engineHandle = h
}
return c
```

4. **`Server()`** (~line 340): same negative→`engineErr` conversion; additionally,
   the `keyComponents` error path (currently `return c` with handle 0 and the
   error silently dropped, later surfacing as the confusing `"tls: nil engine"`)
   now records `c.engineErr = err`.

5. **`Handshake()`** (~line 723):

```go
func (c *Conn) Handshake() error {
    if c.engineErr != nil {
        return c.engineErr
    }
    if c.engineHandle == 0 {
        return errors.New("tls: nil engine")
    }
    ...
    if rc := beginHandshakeTrap(c.engineHandle); rc != 0 {
        if rc < 0 {
            return tlsErrorFromSlot(rc)
        }
        return fmt.Errorf("tls: handshake failed (beginHandshake code %d)", rc)
    }
    ...
    st := engineStatusTrap(c.engineHandle)
    if st < 0 {
        return tlsErrorFromSlot(st)
    }
    switch st { ... }   // default branch stays as defensive
    ...
    case engineNeedWrap:
        out := wrapTrap(c.engineHandle, nil)
        if out == nil {
            if herr := tlsHandleError(c.engineHandle); herr != nil {
                return herr
            }
        }
        ... // existing empty-out logic unchanged (empty non-nil slice)
    case engineNeedUnwrap:
        ...
        app := unwrapTrap(c.engineHandle, tmp[:n])
        if app == nil {
            if herr := tlsHandleError(c.engineHandle); herr != nil {
                return herr
            }
            // peer closed without detail: keep previous behavior —
            // loop around; the next status check observes the closure.
        }
    case engineNeedTask:
        if rc := runTasksTrap(c.engineHandle); rc < 0 {
            return tlsErrorFromSlot(rc)
        }
    ...
    if hasPendingTrap(c.engineHandle) -> hp; hp < 0 => return tlsErrorFromSlot(hp)
```

   Careful in NEED_WRAP: today `out == nil` cannot happen distinctly from
   empty; after the change, nil ⟺ failure. The existing `len(out) > 0 … else
   sleep` logic operates on non-nil slices and stays as-is.

6. **`verifyPeer()`** (~line 804):

```go
peerDERs := decodeLengthPrefixed(peerCertificatesTrap(c.engineHandle))
if len(peerDERs) == 0 {
    if herr := tlsHandleError(c.engineHandle); herr != nil {
        return herr
    }
    return errors.New("tls: no peer certificates")
}
```

7. **`ConnectionState()`** (~line 840): gc's `ConnectionState` returns no
   error, so a failed trap cannot propagate — consume and zero:

```go
packed := connectionStateTrap(c.engineHandle)
if packed < 0 {
    lastErrorTrap(packed) // consume; nowhere to report (gc API has no error)
    packed = 0
}
```

8. **`__tlsConnRead`** (~line 878):

```go
app := unwrapTrap(c.engineHandle, tmp[:n])
if app == nil {
    if herr := tlsHandleError(c.engineHandle); herr != nil {
        return __tlsConnReadPair{0, herr}
    }
    return __tlsConnReadPair{0, io.EOF}
}
```

9. **`__tlsConnWrite`** (~line 907):

```go
out := wrapTrap(c.engineHandle, b)
if out == nil {
    if herr := tlsHandleError(c.engineHandle); herr != nil {
        return __tlsConnWritePair{0, herr}
    }
    return __tlsConnWritePair{0, errors.New("tls: wrap failed")}
}
```

10. **`Close()`** (~line 926) and trap decl (~line 923):

```go
//go:linkname tlsCloseTrap graalgo.tls.close
func tlsCloseTrap(engineHandle int64) int64

func (c *Conn) Close() error {
    if c.engineHandle != 0 {
        if rc := tlsCloseTrap(c.engineHandle); rc < 0 {
            lastErrorTrap(rc) // consume; Close's error comes from conn.Close
        }
        c.engineHandle = 0
    }
    return c.conn.Close()
}
```

### 3.9 Registration changes

File:
`internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java`,
`registerTlsIntrinsics()` (~lines 715–749):

- Update the javadoc: "the 11 `graalgo.tls.*` intrinsics" → "the 12".
- Add:

```java
Linknames.register("graalgo.tls.lastError",
        args -> TlsRuntime.lastError(args[0]), 1);
```

- Change the close registration to propagate the new return:

```java
Linknames.register("graalgo.tls.close",
        args -> TlsRuntime.close(args[0]), 1);
```

### 3.10 Class-javadoc updates (TlsRuntime)

Document the contract where the Go driver can see it:

- Sentinel contract: negative `long` return ⟺ error slot; `null`/empty from
  Object traps as today; `graalgo.tls.lastError` consume-on-read semantics.
- `catch (Exception)` everywhere; `Error` subclasses still propagate (fatal,
  matches gc).
- Single-driver-per-handle assumption restated (pointer to R23-6's contract).

---

## 4. How `num()`/`str()` are consolidated

`num()`, `str()`, `bytes()` stop throwing and become `optNum()`, `optStr()`,
`optBytes()` returning `null` on type mismatch (§3.2). Every call site performs
an explicit null check and routes to the error channel:

- **In the constructors** (`newClientEngine`, `newServerEngine`): the violation
  is caught by the same `catch (Exception e)`-equivalent path — now an explicit
  `return failArg("<op>.<arg>", badArg, "<expected>")` *before* any handle
  exists, so the negative-slot path (§3.1) carries it. No `try` needed for these;
  genuinely exceptional failures (SNI, cert parse, key build, cipher suites)
  keep going through `catch (Exception e) { return fail("<op>", e); }`.
- **In handle-taking traps**: the handle argument is validated first
  (`optNum` → `failArg("<op>.handle", …)` on mismatch); data arguments
  (`plaintext`, `netData`) are validated inside the try-block and stash
  handle-scoped errors (`failHandleArg`).
- The old messages are preserved verbatim in the channeled text
  (`"expected Number, got <class>"`, `"expected GoString, got <class>"`,
  `"expected GoSlice, got <class>"`), prefixed with `tls.<op>.` — greppability
  of the old strings is retained.

Result: **zero `throw` statements remain in `TlsRuntime`** for `Exception`
types; the only `throw` left in the file should be none (verify with
`grep -n "throw new" TlsRuntime.java` expecting no hits — the audit's
`ExceptionInInitializerError`-style patterns do not exist here).

---

## 5. Edge cases

1. **Concurrent engines:** slots and handle-errors are keyed per call/handle in
   a `ConcurrentHashMap`; two engines failing simultaneously cannot cross
   contaminate. No locks, no thread identity.
2. **Double error without intervening read:** last-write-wins per key; the
   first message is lost. This is the documented Go protocol
   (read-after-failure), not a defect — the Go changes in §3.8 always read
   immediately after observing a sentinel.
3. **Error after handle close:** `close` removes the handle-scoped entry;
   `lastError(handle)` → `""`. A subsequent trap call with the closed handle
   allocates a *fresh* slot (`unknown engine handle`) — no stale detail leaks.
4. **`lastError` called twice / with unknown key / with wrong-typed key:**
   returns `""`, never throws (`optNum` null-check inside `lastError`).
5. **Slot/handle numeric overlap:** impossible by construction (slots negative,
   handles positive); a single map serves both.
6. **`unwrap` peer-closed vs error:** §3.5 — CLOSED stashes nothing, so
   `io.EOF` semantics are preserved exactly.
7. **`Error` (not `Exception`):** e.g. `OutOfMemoryError` still propagates and
   aborts — deliberate, matches gc's fatal behavior; documented in javadoc.
8. **Messages containing newlines or attacker-influenced cert data:** flattened
   to one line and capped at 512 chars (§3.2) — bounded memory, safe to print.
9. **Go `Client()`/`Server()` never store a negative handle:** conversion
   happens immediately at the call site; `engineHandle == 0` checks elsewhere
   are unaffected.
10. **`ConnectionState()` failure:** consumed and dropped (gc API has no error
    return); the handshake/read/write paths remain the primary error surfaces.
11. **R23-7 forward-compat:** if `splitLengthPrefixed` later throws on
    malformed input, it lands inside `newServerEngine`'s try-block and is
    channeled automatically — no further change needed here.
12. **R23-5 (PENDING on CLOSED) is untouched:** this design neither fixes nor
    interferes with it; the CLOSED early-return keeps its exact current shape
    minus nothing.

---

## 6. Regression test plan

### 6.1 Java unit tests (in-JVM, fast — part of `make test-ci`)

New file:
`internal/backend/interop/src/test/java/graalgo/p3tls/TlsErrorChannelTest.java`
(follow the style of `p3os/GoOsErrnoTest.java`). Every test that triggers a
negative return **consumes the slot** via `lastError` (protocol hygiene).

| # | Test | What it proves |
|---|------|----------------|
| 1 | `newClientEngine` with SNI-invalid server name (e.g. `"not a hostname!"`) → negative; `lastError(-ret)` non-empty and contains `IllegalArgumentException`; second `lastError(-ret)` → `""` | constructor failure → slot + detail + consume-on-read; no throw |
| 2 | `newServerEngine` with garbage `chainDER` bytes → negative; message mentions certificate parsing | cert-material failure channeled |
| 3 | `newServerEngine` with `keyAlg = "DSA"` (unsupported) → negative; message contains `unsupported keyAlg` | the line-404 throw becomes a channeled error |
| 4 | `newClientEngine(new Object(), …)` (wrong-typed serverName) → negative, no throw; message contains `expected GoString` | `str()` consolidation |
| 5 | `newClientEngine` with wrong-typed `minVersion` (e.g. a `String`) → negative, no throw; message contains `expected Number` | `num()` consolidation in constructors |
| 6 | `beginHandshake("nope")`, `engineStatus(new Object())`, `hasPending(new Object())`, `close(new Object())` → all negative, none throws | `num()` consolidation in long-returning traps |
| 7 | `wrap(new Object(), …)`, `unwrap(new Object(), …)` → `null`, none throws | `num()` consolidation in Object-returning traps ("no key" limitation documented) |
| 8 | Valid engine handle + `wrap(handle, "not-a-slice")` → `null`; `lastError(handle)` non-empty contains `expected GoSlice`; second call → `""` | handle-scoped error for data-arg violation |
| 9 | `runTasks` on unknown handle → negative with `unknown engine handle` detail, no throw | failure point 6's lookup path; task-throw path covered by code review (deterministic JDK failing-task fixture is out of proportion) |
| 10 | Slot/handle namespace separation: force a construction failure (slot `s<0`), create a valid engine (handle `h>0`), force a handle error on `h` → `lastError(s)` yields the construction message, `lastError(h)` the handle message, in either order | sign-disjoint key spaces, no cross-talk |
| 11 | `close(h)` after a handle error → `lastError(h)` → `""`; `close` returns `0` | handle-scoped errors die with the handle |
| 12 | **No-throw invariant sweep:** reflect over all `public static` methods of `TlsRuntime`; invoke each with hostile args (wrong types, nulls, unknown handles) and assert none throws — only sentinel returns | the P1's core guarantee, mechanically pinned against future regressions |
| 13 | `lastError` with wrong-typed key (`lastError(new Object())`) and unknown key → `""`, no throw | accessor never throws |

How to get a *valid* engine handle in tests 8/10/11 without network: call
`newClientEngine` with valid args (`"", false, 0, 0, empty bytes`) — it only
builds an `SSLEngine`, no I/O. (Precedent: check how existing interop tests
bootstrap; no network needed.)

### 6.2 Go differential corpus (gc vs graalgo parity)

New program `test/corpus/wave3/crypto/tls/02_error_channel.go`, run via
`test/differential/run.sh`. **Assertion strategy:** the test prints only
booleans (`err=true/false`), never messages — messages are intentionally
graalgo-specific. Two assertions:

1. **No-crash (the P1's core):** for every hostile config in the battery, the
   graalgo process exits 0. A host exception escaping would abort the process
   and fail the run.
2. **Error-presence parity:** for the subset of inputs where gc also returns an
   error, graalgo must return an error too.

Battery (implementation lane to verify gc behavior empirically and fix the
parity subset; candidates):

- `tls.Dial` with `Config{ServerName: "not a hostname!", InsecureSkipVerify: true}`
  against the loopback test server from `01_handshake.go` → exercises the SNI
  failure now channeled (graalgo: error; gc behavior TBD empirically).
- `tls.Dial` against a plain-TCP listener that immediately closes → handshake
  failure mid-flight on both (both must print `err=true`).
- `tls.Dial` to a closed port → `net.Dial` error before TLS (both `err=true`;
  guards the pre-existing path).
- Client `Handshake()` twice / `Read` after `Close` → error paths through
  `engineErr`/closed-handle handling (both `err=true` expected).
- Happy-path control: the existing `01_handshake.go` must keep passing
  unchanged (snapshot-check guards drift).

### 6.3 Gates for the implementation lane

- `grep -n "throw new" TlsRuntime.java` → no hits (Exception types).
- `make test-ci` green, including the new `TlsErrorChannelTest`.
- `make snapshot-check` green (no observable behavior drift on the corpus).
- New corpus program green under `test/differential/run.sh` (both backends).
- Independent review before merge (standing order).

---

## 7. Open questions / out-of-scope observations for the implementation lane

1. **gc behavior for hostile configs is TBD empirically** ( §6.2): fix the
   error-presence parity subset by running the battery under real gc first.
   If gc does *not* error on an input where graalgo now does (e.g. invalid SNI
   name — graalgo is stricter because the JDK validates), that is a documented
   strictness difference in the *direction* of failing safe, not drift to
   hide; record it in the test file's header comment.
2. **`insecureArg` is accepted but ignored** by `newClientEngine` (trust is
   always permissive; Go verifies post-handshake). Pre-existing semantic gap,
   out of scope — flagged so the lane doesn't "fix" it here.
3. **`__listenerAccept` returns the `Server()` conn with nil error** even when
   `engineErr` is set; the error surfaces on first `Handshake`/`Read`/`Write`.
   This matches gc's `Accept` shape (no handshake at accept). No change.
4. **`ConnectionState()` drops a consumed error silently** (§3.8.7) — forced by
   gc's error-less API. If a future reviewer wants visibility, the slot could
   be surfaced via a synthetic extension, but that would diverge from gc.
5. The `time.Sleep(10 * time.Second)` in `Handshake()` (line ~750) is
   pre-existing and untouched by this design.

## 8. Files touched by the implementation lane (exact list)

1. `internal/backend/interop/src/main/java/graalgo/p3tls/TlsRuntime.java`
   — error store, `opt*` helpers, `fail*` helpers, `lastError`, per-trap
   reshaping, `close` signature change, javadoc contract.
2. `internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java`
   — register `graalgo.tls.lastError`; update close lambda; javadoc 11 → 12.
3. `internal/frontend/synth_crypto_tls.go`
   — `lastErrorTrap` + `tlsErrorFromSlot` + `tlsHandleError`; `Conn.engineErr`;
   `Client`/`Server`/`Handshake`/`verifyPeer`/`ConnectionState`/
   `__tlsConnRead`/`__tlsConnWrite`/`Close` updates (§3.8).
4. `internal/backend/interop/src/test/java/graalgo/p3tls/TlsErrorChannelTest.java`
   — new (§6.1).
5. `test/corpus/wave3/crypto/tls/02_error_channel.go` — new (§6.2).
