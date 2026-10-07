# `syscall` (linux/amd64) — P3-M8 synthetic bring-up analysis

**Verdict:** synthetic package, following the `synth_errors.go` pattern (real Go source in
`phases/phase1/frontend/synth_syscall.go`, registered in `syntheticSources`). All pure-Go
content (constants, types, `Errno`/`Signal` methods, env copy, wrappers' *logic*) is reused;
the trap itself becomes ~30 small **typed Java intrinsics** (exporter-lowered wire nodes,
the `time.Now` precedent in `synth_machinery.go`). `Syscall`/`RawSyscall`/`Syscall6`/
`RawSyscall6` with arbitrary trap numbers are **fail-fast** — this is forced by the value
model (SPEC §7.3: `unsafe.Pointer` is handle-table emulation, so a `uintptr` arriving at a
trap site is a handle token, not a native address; no generic trap dispatcher can marshal
pointer args without per-trap type knowledge). No JNI/JNA mechanism exists in the project
(`PinAudit.java` documents "no JNI/FFM on goroutine paths" as a verified invariant), and
none is needed: the intrinsics use host JVM APIs (`java.nio`, `ProcessHandle`,
`java.time`), not raw traps. Panama FFI (SPEC §3.7) is the recorded re-test trigger for
widening coverage, not the M8 mechanism.

## 1. linux/amd64 file set (via `go list -e ./syscall`, go1.27.1)

27 Go files + 1 asm file. (`*_test.go` excluded throughout.)

| file | kind | disposition |
|---|---|---|
| `syscall.go` | pure Go | reuse: `Timeval`/`Timespec` methods, `StringByteSlice`/`ByteSliceFromString`/`BytePtrFromString`; `Getpagesize`/`Exit` → intrinsics (§4) |
| `syscall_linux.go` | pure Go + `//sys` decls + linknames | reuse hand wrappers (`Open`, `Pipe`, `Mkdir`, `WaitStatus` methods…), retargeted to intrinsics; linknames triaged in §2; `//sys` decls NOT mechanically reused (see `zsyscall` row) |
| `syscall_linux_amd64.go` | pure Go + `//sys` decls + `gettimeofday` linkname | reuse consts; `Gettimeofday`/`Time` → time intrinsic |
| `syscall_unix.go` | pure Go | reuse `errnoErr`, `Read`/`Write`/`Pread`/`Pwrite` logic **minus** the `internal/asan` guards (no-op when disabled; drop import); `Sockaddr` machinery deferred (no socket consumer in M8) |
| `zsyscall_linux_amd64.go` | generated, pure Go | **do NOT reuse verbatim**: all 121 call sites funnel untyped `uintptr(unsafe.Pointer(...))` through `Syscall`/`Syscall6`. Re-author the ~35 wrappers `os` needs as *typed* Go (`path string`, `p []byte`, `stat *Stat_t`) calling typed intrinsics — this is what makes the package `unsafe`-free and marshalable |
| `zerrors_linux_amd64.go` | generated consts | reuse verbatim (errno values) |
| `zsysnum_linux_amd64.go` | generated consts | reuse verbatim (`SYS_*` numbers kept for source compat; traps are never issued) |
| `ztypes_linux_amd64.go`, `types_linux.go` | generated + hand types | reuse, but layouts are differential-verified byte-for-byte (risk §6.2) |
| `env_unix.go` | pure Go | reuse with intrinsic `runtime_envs`; `runtimeSetenv/Unsetenv/Clearenv` → Go-copy-only intrinsics (documented divergence §4) |
| `dirent.go` | pure Go wrapper | reuse logic over a `getdents64` intrinsic (binary-layout risk §6.2) |
| `timestruct.go`, `time_nofake.go` | pure Go | reuse verbatim |
| `exec_linux.go`, `exec_unix.go`, `forkpipe2.go` | pure Go (`ForkExec`/`StartProcess`) | **defer**: fail-fast; with them `rawVforkSyscall` and the `os_checkClonePidfd` push edge disappear |
| `flock_linux.go`, `setuidgid_linux.go`, `rlimit.go`, `rlimit_stub.go` | pure Go wrappers | defer (no `os` consumer on linux/amd64); fail-fast |
| `net.go`, `sockcmsg_unix.go`, `sockcmsg_linux.go`, `sockcmsg_unix_other.go`, `netlink_linux.go`, `lsf_linux.go` | pure Go | defer (socket/netlink consumer is `net`, not M8 `os`); fail-fast |
| `linkname_unix.go` (`mmap` push), `badlinkname_unix.go` (`getsockopt`/`setsockopt` push) | linkname shims for x/sys | defer (no x/sys in M8) |
| `asm_linux_amd64.s` | asm: `rawVforkSyscall`, `rawSyscallNoError`, `gettimeofday` | inventory category **unsupported**: "syscall trampoline asm; graalgo's syscall package is adapted to Panama FFI (SPEC §3.7) in the wave-2 bring-up — per-file strategy then (**re-test trigger: syscall wave**)". Implication: never assembled; each Go-visible service is re-provided as an intrinsic. `rawVforkSyscall` dies with deferred `ForkExec`; `rawSyscallNoError`/`gettimeofday` become intrinsics |
| (transitive) `internal/runtime/syscall/linux/asm_linux_amd64.s` (`Syscall6` trap) | asm | inventory category **evaporate** (runtime-core: no Go stacks on Truffle; Go-visible services re-provided as intrinsics). The synthetic `syscall` package does **not** import `internal/runtime/syscall/linux` at all — the intrinsic replaces the trap |

## 2. Linkname triage

### 2a. Symbols `syscall` NEEDS (outward; all currently MISSING per `linkname-trial-go1.27.1.md`)

| symbol | declared in | trial status | disposition |
|---|---|---|---|
| `runtime.entersyscall` | `syscall_linux.go:29` | MISSING (skip-list) | **route to no-op intrinsic.** Rationale: on gc it releases the P around a blocking trap; on graalgo, blocking intrinsics run on virtual threads which unmount the carrier automatically (SPEC §3.5 netpoller-deleted precedent). No-op is behavior-preserving, not a shortcut. Re-test trigger: none expected; re-open only if a future Panama downcall needs carrier-pin accounting (SPEC §3.7 budgets pinning per GG-0057) |
| `runtime.exitsyscall` | `syscall_linux.go:32` | MISSING | same as above |
| `internal/asan` (import, `syscall_unix.go`) | `Read`/`Write`/`Pread`/`Pwrite` guards | n/a (import, not linkname) | **avoid: drop the import and the `if asan.Enabled` guards.** Pure-Go alternative exists trivially — the guards are no-ops when ASan is off, and graalgo has no ASan. No re-test trigger |
| `os.checkClonePidfd` | — | n/a | **direction correction:** this is not a need. `syscall/exec_linux.go:774` *pushes* `os_checkClonePidfd` (gc object-symbol alias) for `os/pidfd_linux.go`'s pull linkname, used only by `StartProcess` pidfd logic. Deferring `StartProcess`/`ForkExec` removes both sides |

### 2b. Symbols `syscall` PROVIDES (others linkname into it — synthetic `syscall` must keep these resolvable)

| symbol | pulled by | disposition |
|---|---|---|
| `syscall.runtime_BeforeFork` / `AfterFork` / `AfterForkInChild` / `BeforeExec` / `AfterExec` | `runtime/proc.go` | provide as no-op funcs (fork/exec don't exist in M8 scope). Re-test: process-spawning milestone |
| `syscall.runtime_envs` | `runtime/runtime.go:121` (and `env_unix.go` internally) | provide via `__graalgo_runtime_envs` intrinsic (`System.getenv` → `k=v` strings) |
| `syscall.Getpagesize` | `runtime/runtime.go:124` | intrinsic returning constant `4096` (differential-pin vs gc on linux/amd64) |
| `syscall.Exit` | `runtime/runtime.go:130` | intrinsic → `System.exit(code)` (note: runs JVM shutdown hooks; gc's `Exit` runs no deferred Go funcs — closest honest mapping; document) |
| `syscall.runtimeSetenv` / `runtimeUnsetenv` / `runtimeClearenv` | `runtime/runtime.go:201`, `env_unix.go` | Go-copy-only intrinsics (divergence: real process environ not mutated — invisible to Go code and to `os/exec` children, which receive explicit env; only native/cgo code could observe it, and cgo is excluded). Re-test trigger: Panama FFI |
| `syscall.RawSyscall` / `Syscall` / `…6` (nameless linknames), `syscall.gettimeofday`, `syscall.mmap`, `syscall.prlimit`, `syscall.getsockopt`/`setsockopt` | x/sys/unix (wild) | keep the *names* resolvable: trap funcs exist with fail-fast bodies (§4); `mmap`/`prlimit`/socket shims deferred with the socket/mmap scope |
| `syscall.cgocaller`, `syscall.cgo_libc_*` | `runtime/cgo` | dead with excluded cgo; omit |

## 3. Design question: how do real syscalls happen on the JVM?

`Syscall(trap, a1, a2, a3)` on gc = `entersyscall` + raw `SYSCALL` instruction + `exitsyscall`.
On graalgo there is no `SYSCALL` instruction and `uintptr` is a handle-table token (SPEC §7.3),
not a native address. Options:

- **(a) Typed per-operation intrinsics + fail-fast on arbitrary traps (RECOMMENDED).** Each
  wrapper `os` needs becomes typed Go calling a private intrinsic
  (e.g. `__sys_openat(dirfd int, path string, flags int, mode uint32) (int, Errno)`,
  panic-bodied, exporter-lowered like `time.Now`). The Java side implements it with host APIs
  (`java.nio.file`/`channels`, `ProcessHandle`, `java.time`, fd table — §4). Per-trap typing
  is *required*, not optional: only the typed Go layer knows which args are pointers, their
  lengths, and struct layouts, so only it can marshal across the handle-table boundary.
  A generic trap dispatcher cannot be written correctly. ~30 small intrinsics; no new runtime
  capability; no FFM enablement flags; works in all three build modes (§3.9).
- **(b) Panama downcall to libc `syscall(2)`.** Attractive (one generic path, real traps, real
  errno) but **rejected for M8**: (i) no FFM usage exists anywhere in the runtime yet — it is
  SPEC §3.7 Phase-4 work, unbuilt; (ii) it does not solve the actual problem — pointer args
  arrive as handle tokens, so each trap still needs per-trap marshaling descriptors, i.e. it
  degenerates into (a) plus FFM complexity; (iii) `SYS_*` numbers are Linux-version-dependent
  while `java.nio` semantics are stable; (iv) r2 (`rdx`) is not preserved through libc's
  `syscall()` wrapper, breaking `RawSyscall`'s return convention subtly. **Re-test trigger:
  Phase-4 Panama FFI** — revisit then for widening (sockets, `mmap`, exotic traps).
- **(c) JNI/JNA escape hatch.** Does not exist (verified by grep; `PinAudit.java` asserts
  no-JNI/FFM-on-goroutine-paths as an invariant). Building one would add a native toolchain
  dependency the program deliberately avoids (SPEC §3.7 keeps host gcc only for the C side
  of cgo). Rejected; do not build.
- **(c2) Raw trap via a native helper process.** Incoherent with the value model; rejected.

**Recommendation: (a).** It is the only option expressible in the current architecture, it
matches the inventory's "evaporate" categorization of the `Syscall6` trap asm, and its
per-intrinsic cost is small and differential-testable. The fail-fast on arbitrary traps is a
principled boundary (inexpressible in the value model), not a scope shortcut.

## 4. Proposed scope

### Implemented — real

- **Constants:** all errno values (`zerrors_linux_amd64.go`); all `SYS_*` trap numbers
  (`zsysnum_linux_amd64.go`, source-compat only); `O_*`, `S_*`, `DT_*`, `AT_*`, `F_*`,
  `EPOLL_*`, signal numbers; `Stdin`/`Stdout`/`Stderr`.
- **Types + methods:** `Errno` (`Error`/`Is`/`Timeout`/`Temporary`), `Signal`, `Stat_t`,
  `Statfs_t`, `Timespec`/`Timeval`/`Time_t` (+`Nano`/`Nanoseconds`), `Rusage`, `WaitStatus`
  (+`Exited`/`Signal`/`ExitStatus`…), `Dirent`, `Utsname`. Layouts byte-verified vs gc (§6.2).
- **Pure-Go funcs:** `BytePtrFromString`/`ByteSliceFromString`, `NsecToTimespec`,
  `TimespecToNsec`, `errnoErr` (with the `errEAGAIN`/`errEINVAL`/`errENOENT` identity aliases),
  full env family (`Environ`/`Getenv`/`Setenv`/`Unsetenv`/`Clearenv` over intrinsic
  `runtime_envs`; `runtimeSetenv` et al. Go-copy-only — divergence documented in §2b).
- **Intrinsics (Java-implemented, fd table with lowest-free allocation, `CLOEXEC` tracked):**
  `openat`, `close`, `read`, `write`, `pread`, `pwrite`, `lseek`, `pipe2`, `fcntl`
  (`F_GETFL`/`F_SETFL` for `SetNonblock`, `F_DUPFD_CLOEXEC` for `CloseOnExec`),
  `newfstatat`/`fstat`/`lstat`, `getdents64`, `mkdir`, `rmdir`, `unlink`/`unlinkat`,
  `rename`/`renameat`, `symlink`, `readlink`, `chmod`/`fchmodat`, `chown`/`lchown`,
  `truncate`, `utimesNano`, `getcwd`, `chdir`, `getpid`/`getppid`/`getuid`/`geteuid`/
  `getgid`/`getegid`, `kill` (via `ProcessHandle`), `uname` (hostname via
  `InetAddress.getLocalHost`), `getpagesize` (=4096), `exit`, `gettimeofday`.
- **Linkname surface:** `runtime.entersyscall`/`exitsyscall` → no-ops; provided symbols
  in §2b table.

### Fail-fast (every one names its re-test trigger in the panic message)

1. `Syscall`/`RawSyscall`/`Syscall6`/`RawSyscall6` — arbitrary traps inexpressible under the
   handle-table value model. Re-test: Panama FFI (Phase 4) or per-trap intrinsic demand.
2. `StartProcess`/`ForkExec` (+`Wait4`/`Waitmsg`, `rawVforkSyscall`, `os_checkClonePidfd`
   push) — re-test: process-spawning milestone (`os/exec` bring-up; design: `ProcessBuilder`
   intrinsic).
3. Sockets/netlink (`Socket`, `Bind`, `Connect`, `Sendmsg`/`Recvmsg`…, `NetlinkRIB`,
   `LsfSocket`, `Sockaddr` methods) and `SyscallConn` — re-test: `net` package bring-up.
4. `Mmap`/`Munmap`/`Mprotect`/`Madvise` — re-test: first real consumer.
5. `Flock`, `Getrlimit`/`Setrlimit`/`Prlimit`, `Setuid`/`Setgid`/…​,
   `Ustat`, `SyncFileRange`, `Splice`, `Sendfile`, `Epoll*`, `Inotify*`, `Mknod`,
   `Ptracing` — re-test: per-consumer demand (each panic names the consumer that asked).
6. `runtimeSetenv` real-environ mutation — Go-copy-only; re-test: Panama FFI.

## 5. Contract with synthetic `os` (sibling worker)

Measured from the linux/amd64 `os` file set (`go list -e ./os`; `os` never calls
`syscall.Syscall`/`RawSyscall` or `SYS_*` constants directly on this platform — the whole
contract is wrappers + constants + types):

- **Funcs:** `Chdir`, `Chmod`, `Chown`, `Clearenv`, `Close`, `CloseOnExec`, `Environ`, `Exit`,
  `Fstat`, `Getegid`, `Getenv`, `Geteuid`, `Getgid`, `Getpagesize`, `Getpid`, `Getppid`,
  `Getuid`, `Getwd`, `Kill`, `Lchown`, `Link`, `Lstat`, `Mkdir`, `Open`, `Pipe2`, `Readlink`,
  `Rename`, `Rmdir`, `SetNonblock`, `Setenv`, `Stat`, `Symlink`, `Truncate`, `Uname`,
  `Unlink`, `Unsetenv`, `UtimesNano`, `NsecToTimespec`, `StartProcess` (→ fail-fast per §4),
  `RawConn` (type only; `File.SyscallConn` → fail-fast per §4).
- **Types:** `Dirent`, `Errno`, `ProcAttr`, `Rusage`, `Signal`, `Stat_t`, `SysProcAttr`,
  `Timespec`, `Utsname`, `WaitStatus`, `Stdin`/`Stdout`/`Stderr`.
- **Constants:** errno set (`EACCES`, `ECHILD`, `EEXIST`, `EINTR`, `EINVAL`, `EISDIR`,
  `ELOOP`, `EMLINK`, `ENAMETOOLONG`, `ENOMEM`, `ENOSYS`, `ENOTDIR`, `ENOTEMPTY`, `ENOTSUP`,
  `EOPNOTSUPP`, `EPERM`, `EPIPE`, `ERANGE`, `ESPIPE`, `ESRCH`, `EPLAN9`, `EWINDOWS`),
  `O_*`, `S_*` (+`S_ISUID`/`S_ISGID`/`S_ISVTX`), `DT_*`, `F_DUPFD_CLOEXEC`/`F_GETFL`,
  `SIGINT`/`SIGKILL`/`SIGTRAP`, `WEXITED`/`WNOWAIT`.
- **Notes for the `os` worker:** (i) `os` path errors surface as `*PathError` wrapping our
  `Errno` — identity through `errnoErr` aliases must hold (§6.1); (ii) `os.Getwd` uses
  `ImplementsGetwd`+`Getwd` — both real; (iii) `File.Read` EINTR-retry logic in `os`
  assumes raw `read(2)` short-read/EINTR semantics from our intrinsic — do not "helpfully"
  loop inside the intrinsic; (iv) `os.Hostname()` needs our `Uname` intrinsic.

## 6. Three hardest behavioral risks

1. **Exception→errno fidelity at the Java boundary.** JVM APIs don't speak errno: every
   intrinsic hand-maps failures to exact Linux numbers. The peril is misclassification, not
   missing cases — e.g. `Files.readAttributes` on a dangling symlink vs missing parent
   (`ENOENT` vs `ENOTDIR`), over-long paths (`ENAMETOOLONG` vs `ENOENT`), symlink loops
   (`ELOOP`), permission shadows (`EACCES` vs `EPERM`). Each intrinsic's mapping table is
   hand-authored and must be differential-tested per failure mode, not just the happy path.
   Downstream, `Errno.Is`/`Timeout`/`Temporary` and the `errnoErr` identity aliases
   (`errEAGAIN` etc.) must make `os.IsNotExist`/`os.IsPermission`/`errors.Is(err,
   syscall.EAGAIN)` behave exactly — one `==` on the wrong dynamic type breaks the chain
   silently.
2. **Kernel-struct layout fidelity.** `Stat_t` (via `newfstatat`), `linux_dirent64` (via
   `getdents64`), `Utsname` must be byte-exact: same field offsets, sizes, padding, and
   NUL-termination semantics as gc's `ztypes`. The §3.7 C-ABI layout engine doesn't exist
   until Phase 4, so M8 hand-authors layouts — every field differential-tested
   (`stat.Size`, `stat.Mode` bits incl. `S_IFMT` extraction, `d_reclen`/`d_type` walking in
   `ReadDir`). `getdents64` is the single hardest intrinsic: variable-length records the
   Go side must parse exactly like gc's `dirent.go`.
3. **Arbitrary-trap fail-fast as an API break.** Any stdlib/user code path reaching
   `Syscall` with a non-intrinsic trap panics where gc would issue the trap. The risk isn't
   the panic — it's *late discovery* (only surfaces when the path executes). Mitigations:
   fail-fast message includes the trap number (`syscall.Syscall: trap 39 (SYS_GETPID) has
   no graalgo intrinsic; re-test: Panama FFI (Phase 4)`); corpus program `09` pins the
   message; the M8 wave greps the synthetic `os`/`time` trees for direct `Syscall` uses
   (none found on linux/amd64 — §5) so the blast radius is user code only.

## 7. Differential corpus ideas (`corpus/wave1/syscall/`, conventions: `corpus/wave1/README.md`)

All hermetic (own temp dir via `os.MkdirTemp`, unique names), deterministic (print booleans,
counts, and `==` comparisons — never pids, paths, or times), `package main`/`func main`,
`NN_name.go` ascending. gc leg: stock gc 1.27.1; graalgo leg: `P3SyncMain`; byte-compare
stdout + exit code (+ first stderr line for `.panic` programs).

1. `00_errno_values.go` — print `int(syscall.ENOENT)`, `EACCES`, `EEXIST`, `EINVAL`,
   `EAGAIN`, `EPERM` as a comma list (constant fidelity vs gc).
2. `01_errno_methods.go` — `Errno.Is`, `Timeout` (`EINTR`→true), `Temporary`
   (`EAGAIN`→true), plus `errors.Is(err, syscall.ENOENT)` where `err` comes from
   `syscall.Open` of a missing file (identity through `errnoErr`).
3. `02_open_write_read.go` — `Open(O_CREAT|O_RDWR)`, `Write`, `Seek(0,0)`, `Read` back,
   compare bytes, `Close`, `Unlink`; assert first `Open` returned fd `3` (fd-table
   discipline).
4. `03_open_missing.go` — `Open` nonexistent → print `err == syscall.ENOENT` and
   `os.IsNotExist(err)` (both true); `os.Open` same path → `*PathError` chain.
5. `04_ids.go` — print `syscall.Getpid()==os.Getpid()`, `Getuid()==os.Getuid()`,
   `Getgid()==os.Getgid()` (all true; no raw values printed).
6. `05_stat.go` — create, `Chmod(0640)`, `Stat`: print `Size`, `Mode().Perm()==0640`,
   `Mode().IsRegular()`; `Lstat` on a symlink: `Mode().IsSymlink()` true, `Stat`
   follows it.
7. `06_mkdir_rename_remove.go` — `Mkdir`, second `Mkdir` → `EEXIST`, `Rename`,
   `Rmdir`, `Unlink` missing → `ENOENT`; print each errno predicate.
8. `07_pipe.go` — `Pipe`, write one end / read other (short message + 1MB for short-read
   behavior), close write end → read returns `(0, nil)` (EOF, not error); read closed fd
   → `EBADF`.
9. `08_dir_read.go` — create 3 files, `Open` dir, `ReadDirent`/`Getdents` loop via
   `os.ReadDir`: print sorted names joined (deterministic order by sorting in-program).
10. `09_syscall_failfast.go` + `09_syscall_failfast.panic` — call
    `syscall.Syscall(syscall.SYS_GETPID, 0, 0, 0)`; expect first stderr line
    `panic: syscall.Syscall: trap 39 (SYS_GETPID) has no graalgo intrinsic; re-test:
    Panama FFI (Phase 4)` (byte-compared by the runner).

(Plus `10_env.go` if budget allows: `Setenv`/`Getenv`/`Unsetenv`/`Environ` round-trip on a
unique key, incl. `Setenv("","x")` → `EINVAL` and NUL-in-value → `EINVAL`.)

## 8. Open questions / handoff notes

1. **Sibling (`os` worker):** confirm M8 `os` does not need `StartProcess` — if it does,
   §4 item 2's deferral is wrong and a `ProcessBuilder` intrinsic design is needed instead.
2. **Parent:** v1 frontend `unsafe` status — this design avoids `unsafe` entirely in the
   wrapper layer (typed intrinsics take Go values), but `Sockaddr`/`anyToSockaddr` need it
   if sockets ever come into scope; confirm the plan is still "defer with `net`".
3. **Parent:** the M6 inventory's "adapted to Panama FFI (SPEC §3.7)" line for
   `syscall/asm_linux_amd64.s` predates this analysis; recommend amending the per-file
   strategy to "typed Java intrinsics in M8; Panama FFI is the Phase-4 widening trigger"
   so the inventory doesn't imply FFM is an M8 dependency.
4. `os.File`'s EINTR-retry and nonblocking-poll paths assume `read(2)`/`poll(2)` semantics;
   `SetNonblock` on our fd table is a flag today (no real poller until `net`) — document
   the divergence if any `os` path depends on `EAGAIN` from nonblocking regular files
   (regular files never return `EAGAIN` on Linux, so this is safe; pipes are the edge).
