# Worker netpoller checkpoint (append-only) — RISK-M9-1 + RISK-M9-2

## 2026-10-09 ~07:30 CEST — session start, design

Task: `runtime_poll*` linknames + GMP integration (RISK-M9-1), socket trap
surface + fd-domain unification (RISK-M9-2). Tree `~/workspace/graalgo-m9`,
branch `m9`.

### Design decisions (recorded before code)

1. **Symbol namespace**: `internal/poll.runtime_poll*` for the 8 poll
   linknames (+ `internal/poll.runtime_isPollServerDescriptor`), matching
   gc's `runtime/netpoll.go` linkname targets and the linkname-trial JSON's
   MISSING entries. `runtime.nanotime` already satisfied. Socket traps under
   new `graalgo.net.*` namespace (NOT extending the frozen `graalgo.os.*`
   table; RISK-M9-2's open question decided: separate namespace, mechanical
   rename possible later).
2. **Single-result adaptation** (frontend/v1 constraint): exporter rejects
   >1 results even for linkname decls (`exportLinknameDecl`). So the fork's
   `internal/poll` declares `func runtime_pollOpen(fd uintptr) uintptr`
   (ctx cookie, 0 = failure) + `func runtime_pollOpenErrno() int`
   (ThreadLocal errno, gc's errno value). All other poll linknames are
   already single-result/void in gc. Package worker adapts `pollDesc.init`.
3. **Poller = one dedicated daemon platform thread + JDK Selector (epoll)**.
   NOT a GMP carrier (answers experiment 1's design half: no carrier
   carve-out; parked waiters release their in-flight slot via PARKED).
   Registration via queue + wakeup + future (register() from another thread
   while select() blocks is unsafe).
4. **Wait = SchedulerCore.parkCurrent + unpark via drainer WAKE lane.**
   Exactly-once unpark via CAS-winner discipline: the waiter holds
   `AtomicReference<Integer> result` (null = pending); readiness (poller
   thread), deadline (timer thread), close (any thread) CAS it; ONLY the
   CAS winner unparks. This structurally prevents the drainer's
   `unparkPending` stale-entry leak (a stale UNPARK would poison the
   goroutine's NEXT park with a spurious immediate wakeup).
5. **Deadlines via TimerHeap.addCallback** (not addSleep — addSleep unparks
   without CAS). State (`readDeadlineNanos`) persists across waits like
   gc's `pd.rd`; timer is one-shot per arming. d>0 arm, d==0 disarm, d<0
   expire-now + wake waiter with TIMEOUT.
6. **fd-domain unification**: sockets are cookies in GoOs's HANDLES map
   (new `KIND_SOCKET=5`), sharing NEXT_COOKIE space (no collision).
   `graalgo.os.read/write/close` work on socket cookies (SocketChannel
   implements Readable/WritableByteChannel). `GoOs.close` on KIND_SOCKET
   first evicts poll state (`GoNetpoll.evictForClose`) — one close path.
   Read/write EAGAIN mapping: GoOs.read returns n+1 (0=EOF); 0 bytes on a
   nonblocking socket → 1 → fork maps to EAGAIN when len(buf)>0.
7. **Socket model (eager channels)**: `socket()` creates the NIO channel
   immediately (SOCK_STREAM→SocketChannel, SOCK_DGRAM→DatagramChannel,
   both nonblocking). `bind()` binds. `listen()` swaps SocketChannel→
   ServerSocketChannel (binds recorded addr or wildcard). `connect()`
   nonblocking; EINPROGRESS → SockState.CONNECTING, OP_CONNECT in poller
   → finishConnect; failure stashed, surfaced via getsockopt(SO_ERROR)
   (gc parity).
8. **Addr wire format** (GoSlice bytes): [family, ...]; AF_INET:
   fam=2, port BE u16, 4 addr bytes; AF_INET6: fam=10, port BE u16,
   16 addr bytes, scope-id BE u32; AF_UNIX: fam=1, path bytes (deferred:
   -EAFNOSUPPORT for now? — decide in code; low priority).
9. **GG-0018 vs netpoller**: the Phase-2 deletion gate scans
   `phases/phase2/src` only; new code lives in `phases/phase3/src`
   (`graalgo.p3net`), whose Makefile has no NIO gate. `Netpoll.java`
   (deleted surface) untouched. The M9 risk register explicitly supersedes
   GG-0018 for the net track.
10. **ParkReason.NETPOLL**: additive enum constant for JFR fidelity
    (IO means slot-release; netpoll parks are scheduler parks).

### Experiments (to run, per task)
- E1: single-socket loopback echo vs 38µs cold-drainer-wakeup budget.
- E2: deadline churn (reset-per-read/write) vs timer wheel.
- E3: wakeup latency at 1k+ connections vs accepted band.

### Files to write
- `phases/phase3/src/main/java/graalgo/p3net/GoNet.java` (socket traps)
- `phases/phase3/src/main/java/graalgo/p3net/GoNetpoll.java` (poll linknames + poller)
- patch `p3os/GoOs.java` (KIND_SOCKET, register/resolve, poll-aware close)
- patch `p3link/LinknameIntrinsics.java` (symbol registration)
- patch `graalgo/jfr/ParkReason.java` (NETPOLL)
- `phases/phase3/src/test/java/graalgo/p3net/GoNetpollTest.java` + runner registration

## 2026-10-09 ~08:30 CEST — implementation complete, tests at 104/106

### Landed
- `phases/phase3/src/main/java/graalgo/p3net/GoNet.java` — 13 socket traps
  (`graalgo.net.*`): socket/bind/listen/accept/connect/setsockopt/
  getsockopt/getsockname/getpeername/shutdown/setnonblock/sendto/recvfrom.
  Addr wire format documented in javadoc. AF_UNIX → -EAFNOSUPPORT (scope).
  IPV6_V6ONLY: v=0 no-op success (dual-stack by construction), v=1 loud
  -ENOPROTOOPT (no JDK API).
- `phases/phase3/src/main/java/graalgo/p3net/GoNetpoll.java` — 10 poll
  linknames (`internal/poll.*`): single dedicated epoll Selector thread
  (not a GMP carrier), parkCurrent/unpark via drainer WAKE lane,
  CAS-winner exactly-once wakeup, deadlines via TimerHeap.addCallback
  with stale-timer id validation.
- `p3os/GoOs.java`: KIND_SOCKET=5, registerSocketChannel/socketChannel/
  replaceSocketChannel, close() evicts poll state for KIND_SOCKET.
- `p3link/LinknameIntrinsics.java`: registerNetIntrinsics() (10 poll + 13
  socket symbols, arities documented).
- `graalgo/jfr/ParkReason.java`: +NETPOLL (additive).
- `phases/phase3/src/test/java/graalgo/p3net/GoNetpollTest.java`: 11 tests,
  registered in IfaceTestRunner.

### Test results (phase-3 suite): 104/106, then fixes below
Failures found and fixed (red-green):
1. Test passed `char` mode; D5 delivers `Long` for Go int — test bug, fixed
   to `(long) 'r'` (implementation correctly strict per D5 contract).
2. `InetSocketAddress(InetAddress,int,int)` doesn't exist — fixed via
   `Inet6Address.getByAddress`.
3. `StandardSocketOptions.IPV6_V6ONLY` doesn't exist — handled explicitly.
4. Stale-timer race: a fired timer's callback could cancel a re-armed timer
   and wake spuriously — fixed with callback id validation.
5. Tests drove pollWait on the main thread — wrapped in goroutines (prod
   always calls on goroutines; fail-fast is correct).

### Environmental findings (not code bugs)
- **Sandbox blocks unconnected sendto(2) with EPERM** (verified at syscall
  level with python; connected send works). `sockErrno` maps
  "Operation not permitted" → EPERM (not EIO). UDP test uses connected
  datagrams; sendto positive path untestable here — noted for the fork's
  WriteToUDP (works on real hosts).
- **VM load ~28** makes wall-clock latency measurements noisy (negative
  wakeup timings = main-thread descheduling between write() and the
  timestamp). Added poller-side hook `lastWakeNanosForTest` (CAS-win
  nanotime) so tests measure the owned component without waiter-side
  carrier noise. Committed tests gate generously (hang catchers); the
  quiet-machine E1/E3 numbers go here when measured.

## Experiments E1/E2/E3 (2026-10-09 ~09:00 CEST, VM 2 vCPU, load 18-30 — noisy)

E1 wakeup path (dedicated bench, 600 iters, single loopback connection):
- poller-side (write-done -> CAS-win): minPositive=32us, p50=2.9ms (noise-dominated)
- drainer-side (CAS-win -> waiter vthread resume): min=41us, p50=7ms (noise-dominated)
- Verdict: drainer-side structural floor 41us ~= 38us cold-drainer-wakeup budget
  (same VM class as the Phase-2 number; min-over-600 filters descheduling noise).
  Poller adds ~32us floor. Re-measure trigger: quiet/server hardware for a clean
  p50/p99 verdict; committed test gates are hang-catchers (p99<1s), not budget gates.

E2 deadline churn (20k cycles, loaded VM — upper bounds):
- arm+disarm: 71us/cycle; re-arm (cancel+add): 19.5us/cycle (delta is noise, identical op counts)
- arm+fire(1ms): 4.96ms/cycle (dominated by the 2ms sleep in the bench)
- Correctness: no leaked timers, pollerError=null, descCount()==0.
- Note: TimerHeap.cancel is lazy; cancelled entries accumulate until they reach the
  heap head (reaped in deadline order) — heap size bounded by churn_rate x horizon.
  http-style SetDeadline-per-request churn is fine; a future timer-modify would cut it.

E3 1k burst (committed test, load-noisy): poller-side wake latency p50=18.5ms,
p99=52ms, max=52ms — dominated by the poller thread being descheduled during the
burst on this VM (all 1000 CAS-wins land in one batch after reschedule), NOT the
per-key path. Correctness: all 1000 waiters woke with result 0. E1's 32us floor
is the per-key structural cost; burst drain throughput re-measure on quiet HW.

## Self-review race fixes (2026-10-09 ~09:30 CEST, red-green)

R1 stale-timer clobber (correctness): `onDeadline` did check-then-clear of
`readTimerId`/`writeTimerId` (volatile long). A callback that fired just as
`pollSetDeadline` re-armed could wipe the NEW timer's id and deliver a
spurious TIMEOUT. Fix: AtomicLong ids; `onDeadline` claims via
`compareAndSet(myId, -1)`; `setTimerId` uses getAndSet+cancel. Either the
stale callback wins (its deadline genuinely expired first) or it loses —
never clobbers. Verified by inspection; existing deadline tests green.

R2 close-vs-wait race (SEVERE — hang): `pollUnblock` landing between the
pre-wait `checkErr` and the slot claim set closing=true but found an empty
slot (no wakeup), then the waiter claimed the slot and parked FOREVER.
Fix (gc's netpollblock re-check, adapted): after the slot claim +
setInterest, re-run checkErr; on failure, withdraw via
`w.result.compareAndSet(null, WITHDRAWN)` — exactly one of
withdraw/wakeup wins the result CAS, so no stale UNPARK poisons the next
park and no wakeup is lost. If a waker won the race, the waiter parks and
consumes its wakeup (spurious-but-correct).
Red-green: new `testUnblockBeforeClaimReturnsClosing` uses a
`preClaimHookForTest` to force the interleaving deterministically —
FAILS without the fix ("waiter parked forever", 30s timeout), PASSES with it.

Error-class hunt (check-then-act on shared waiter/timer state):
`pollWaitCanceled` funnels through `pollWaitRaw` (covered);
`pollUnblock`/`pollClose`/`evictForClose` mutate closing under `synchronized (pd)`;
`wakeWith` never touches the slot reference (no cross-waiter talk).
No other instances in the netpoller.
