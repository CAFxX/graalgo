# P3-M8 `os` bring-up analysis (worker 3/5, 2026-10-07)

Read-only analysis. Tree: `~/workspace/graalgo-m8-pipe` (branch `m8-pipeline`).
Vendored source of truth: `~/workspace/graalgo-stdlib/go1.27.1/os`
(GOROOT layout without the `src/` prefix; linux/amd64 file set resolved
via a symlinked fake GOROOT — see §1).

## 0. Verdict

**`os` is bring-up-able in M8 with ~40 linkname intrinsics, zero exporter
changes, one new Java class, and no real syscalls.** Every operation the
in-scope API needs maps onto a JVM API (`java.nio.file`, `java.nio.channels`,
`java.net.InetAddress`, `java.lang.ProcessHandle`, `jdk.security.auth`).
The two things JVM APIs genuinely cannot do — numeric `chown(2)` and
process signal/spawn semantics — become the fail-fast list (§6).

Hard prerequisites (must land with or before the os worker):

1. **Synthetic `io`** — worker 2/5 scopes FULL (`io.go`+`multi.go`+`pipe.go`):
   os needs `EOF`, `ErrShortWrite`, `Reader/Writer/Seeker/Closer`,
   `Copy`, `ReadAll`, `WriteString`, `SeekStart/Current/End`. Covered.
2. **Synthetic `io/fs` CORE — currently UNOWNED (gap).** os needs it as
   *type aliases* (`PathError`, `FileInfo`, `DirEntry`, `FileMode` +
   mode-bit constants, `FS`) and the `Err*` sentinels
   (`ErrInvalid/ErrPermission/ErrExist/ErrNotExist/ErrClosed`). The io
   worker scoped `io/fs` OUT. Recommendation: the os worker authors the
   minimal `io/fs` subset (interfaces + constants + sentinels; no
   `ReadDirFS`/`WalkDir` machinery — those can ride on os itself later).
   Without it, `*PathError`/`errors.Is` identity cannot be built.
3. **Synthetic `syscall` vocabulary** — sibling worker: `Errno` + `Is`,
   errno constants, `O_*`/`S_*` constants, `Signal`/`Stdin..` consts.
   Pure Go, no intrinsics. os imports it for error identity only (§3).

Proposed scope: the full file/dir/stat/path/env/argv/identity surface
(§5, classes (a)+(b)). Fail-fast: the whole `Process` family, numeric
chown, suid/sgid/sticky chmod, pipe deadlines, `Root`, `Executable`,
`SyscallConn`, `CopyFS`/`DirFS` (until io/fs) — §6, each with a re-test
trigger. `os/exec` stays out of M8 (confirmed: nothing in the in-scope
os surface needs it).

## 1. linux/amd64 file inventory

Resolved with the go1.27.1 toolchain:
`GOOS=linux GOARCH=amd64 go list -f '{{.GoFiles}}' os` → **50 files,
0 asm** (no `.s` files exist anywhere under `src/os`). Classes:

| File(s) | Role | Disposition |
|---|---|---|
| `file.go` | `File` methods, `OpenFile`, `LinkError`, stdio vars | Port logic to synthetic Go; syscalls → intrinsics |
| `file_open_unix.go`, `file_posix.go`, `file_unix.go` | open/read/write/seek/close, `NewFile`, finalizers | Intrinsic-backed; finalizer → `GcInterop.setFinalizer` (M4 REAL) or drop (gc semantics: finalizer only closes on GC; dropping changes nothing observable — see note N1) |
| `dir.go`, `dir_unix.go`, `dirent_linux.go` | `Readdir`/`Readdirnames`/`ReadDir` via getdents64 | Logic ported; dirent parsing REPLACED by a Java iterator intrinsic (no `linux_dirent64` layout — §2.6) |
| `env.go` | env ops (imports `internal/testlog`, `syscall`) | Port; testlog dropped (test-only hook); `syscall.Getenv` family → `graalgo.os.*` intrinsics |
| `error.go`, `error_errno.go` | `*PathError`/`LinkError`/`SyscallError`, `Is*` | Port; `poll.ErrNoDeadline` → local def; `syscall.Errno` → synthetic syscall `Errno` |
| `exec.go`, `exec_unix.go`, `exec_posix.go`, `exec_linux.go`, `pidfd_linux.go` | `Process`, `ProcAttr`, `Signal`, `StartProcess`, wait/kill | Types ported (pure Go); everything executable → fail-fast (§6.1) |
| `stat.go`, `stat_unix.go`, `stat_linux.go`, `statat.go`, `statat_unix.go` | `Stat`/`Lstat`/`File.Stat`, `fillFileStatFromSys` | Port; `syscall.Stat_t` REPLACED by scalar intrinsics via the NIO `unix:` attribute view (no `Stat_t` layout — §2.6) |
| `path.go` | `MkdirAll`, `RemoveAll`, `endsWithDot` | Port logic verbatim over intrinsics |
| `removeall_at.go`, `removeall_unix.go` | `RemoveAll` guts | Port over intrinsics |
| `tempfile.go` | `CreateTemp`/`MkdirTemp` | Port; `runtime_rand` linkname → already-registered `runtime.fastrand` (M4); `strconv` → synthetic |
| `sys.go`, `sys_unix.go`, `sys_linux.go` | `Hostname`, `Getpagesize` | `hostname()` → InetAddress intrinsic + `/proc` fallback in Go (mirrors gc); `Getpagesize` → const 4096 (linux/amd64 ABI) |
| `getwd.go` | `Getwd` (+$PWD kludge) | Port; kludge needs stat-identity intrinsics (§2.5) |
| `types.go`, `types_unix.go` | `File` struct, `fileStat` | Redefined for the handle model (§2.2) |
| `pipe2_unix.go` | `Pipe` | Intrinsic (`java.nio.channels.Pipe`) |
| `proc.go` | `Args`, ids, `Exit` | `Args` via launcher argv (§4); ids → intrinsics; `Exit` → intrinsic |
| `zero_copy_linux.go`, `zero_copy_posix.go` | sendfile `ReadFrom`/`WriteTo` | DROP (optimization); generic `io.Copy` loop in Go — behaviorally identical |
| `rawconn.go` | `SyscallConn` | Fail-fast (§6.4) |
| `root.go`, `root_unix.go`, `root_openat.go`, `root_nonwindows.go` | `Root`/`OpenRoot` | Fail-fast (§6.6) — needs `openat2` |
| `executable.go`, `executable_procfs.go` | `Executable` | Fail-fast (§6.7) — `/proc/self/exe` is the JVM, not the Go program |
| `eloop_other.go`, `sticky_notbsd.go` | consts | Fold into synthetic consts |

N1: gc sets `runtime.SetFinalizer(f.file, (*file).close)` on unix.
Dropping it is observably safe (a test asserting GC-closes-files would
be testing unspecified behavior), but the M4 `GcInterop.setFinalizer`
exists — wiring it is 3 lines; recommend wiring it for fidelity.

## 2. Intrinsic strategy

### 2.1 Mechanism: `//go:linkname`, NOT new wire nodes

The sibling (syscall) analysis proposes exporter-lowered wire nodes
("time.Now precedent") for ~30 intrinsics. I recommend the **ADR-0004
linkname path** instead, for both packages:

1. **It was built for exactly this.** `Linknames` (P3-M4, WS C) exists so
   that "a bodyless Go function carrying `//go:linkname` binds to an
   intrinsic by symbol name" — `LinknameRootNode` + `dispatch` already
   work, arity-checked, fail-fast on unknown symbols. The M4 audit's
   work item 3 sanctions `runtime.fastrand`-style reuse.
2. **Zero exporter changes.** New wire node kinds need per-op exporter
   interception code (`exportTimeIntrinsic` pattern) in the frozen
   exporter; linkname needs none — only additive *data* in the
   Appendix-M table (`mrdesugar_m5.go`, which worker 2 already plans to
   extend; coordinate one patch).
3. **One registration serves two binders.** Both synthetic `os` and
   synthetic `syscall` declare bodyless funcs against the *same symbol*
   (e.g. `graalgo.os.getenv`); the launcher registers the Java target
   once in `graalgo.p3link.LinknameIntrinsics`.
4. **D5 boxing already covers the shapes we need**: `Long` (int64),
   `GoString` (string), `GoSlice` (mutable `byte[]`+offset+len —
   targets MUST respect `offset()`; sub-slices share the array).
   String-returning targets must return `GoString` (raw `Object` flows
   through when the declared result isn't an interface —
   `LinknameRootNode`, boxResult=false).
5. **M8 is bring-up, not optimization.** Wire-node Truffle
   specialization buys nothing for syscall-bound I/O; a
   `ConcurrentHashMap` dispatch per call is noise next to the I/O.

New Java code: **one class, `graalgo.p3os.GoOs`** (new files allowed
per the synthesis), plus registration lines in `LinknameIntrinsics`.
Pinned toolchain per the M7 checklist (GraalVM 25.0.4.1.1, plain javac,
`-Xlint:deprecation -Werror`).

### 2.2 Handle model

Go's `*poll.FD` (int fd) is replaced by a **Java-managed cookie**:

```go
// synthetic os
type File struct {
    handle int64  // GoOs cookie; see below
    name   string
    kind   int64  // cached at open: 1=regular 2=dir 3=pipe 4=stdio
}
```

`GoOs` holds `ConcurrentHashMap<Long, Handle>` + `AtomicLong` allocator
(start at 16; 1/2/3 reserved for stdin/stdout/stderr). `Handle` variants:
`FileChannel` (regular), `DirectoryStream` iterator (dir, guarded by
`synchronized` — the iterator isn't thread-safe), `Pipe` source/sink
channels, stdio (`Channels.newChannel(System.in/out/err)`).

Consequences, all deliberate:

- `File.Fd()` returns the cookie. Documented: **not an OS fd**; it
  round-trips with `NewFile` *within the GoOs domain* (fd 0/1/2 → stdio,
  live cookie → wrap, anything else → `nil`, matching gc's
  "nil if fd is not a valid file descriptor").
- `os.Pipe()` allocates **consecutive** cookies `(r, r+1)` — one
  intrinsic, no packing.
- No fake-fd table, no `dup2` emulation, no getdents parsing (§2.6).

### 2.3 Return/arg conventions (documented once, in `GoOs` javadoc)

- Every intrinsic returns `long`: **negative = `-errno`** (Linux errno
  number), non-negative = value.
- `read` needs an EOF signal distinct from errors: return `n+1` for
  `n ≥ 0` bytes, **`0` = EOF**, negative = `-errno`. (A zero-byte read
  of a non-empty buffer returns 1, never confused with EOF.)
- `readlink`/`hostname` write into a caller `[]byte` (like `read`) —
  avoids the multi-value problem without new machinery.
- Paths arrive as `GoString` → `new String(bytes, UTF_8)`; NUL bytes
  throw `InvalidPathException` (not an `IOException` — catch separately)
  → `EINVAL`, exactly like gc.
- **Virtual cwd** (§2.5): every path-taking intrinsic resolves through
  `GoOs.resolve()` — the single choke point. `PathError.Path` keeps the
  *original* user string (gc preserves it).

### 2.4 The intrinsic table

Symbols live under `graalgo.os.*`. Java APIs named per op.

**Files** (errno: `NoSuchFileException`→ENOENT,
`AccessDeniedException`/`SecurityException`→EACCES,
`FileAlreadyExistsException`→EEXIST, `NotDirectoryException`→ENOTDIR,
`FileSystemLoopException`→ELOOP, `DirectoryNotEmptyException`→ENOTEMPTY,
other `IOException`→EIO):

| Symbol | Java core | Notes |
|---|---|---|
| `open(path, flags, perm)` | stat-kind first, then `FileChannel.open` / `Files.newDirectoryStream` | flags `O_RDONLY/WRONLY/RDWR/APPEND/CREATE/EXCL/SYNC/TRUNC` → `StandardOpenOption`s (`APPEND` ⇒ real `O_APPEND` on the fd ⇒ per-`write(2)` atomic append, §7.2); `O_EXCL` without `O_CREATE` is a no-op like gc |
| `close(h)` | `Handle.close` + map remove | unknown handle → `EBADF` |
| `read(h, b, off, ln)` | `ReadableByteChannel.read(ByteBuffer.wrap(arr, arrOff+off, ln))` | single call (no Go-side-helpful looping — §7.2); dir → `EISDIR`; write-only → `EBADF` |
| `write(h, b, off, ln)` | `WritableByteChannel.write` | single call; gc's short-write/`ErrShortWrite` logic ported verbatim in Go; `EPIPE` → §7.2 SIGPIPE note |
| `readAt/writeAt(h, b, off, ln, pos)` | `FileChannel.read/write(buf, pos)` | non-`FileChannel` → `ESPIPE` |
| `seek(h, off, whence)` | `FileChannel.position` | non-seekable (pipe/stdio) → `ESPIPE`; negative result → `EINVAL`; **dir handle**: `Seek(0, SEEK_SET)` rewinds by reopening the `DirectoryStream` (verified: gc `Seek` on a dir *succeeds*, probe §7 R3); other dir seeks succeed as recorded-position no-ops (documented approximation — gc's nonzero dir seeks yield undefined iteration) |
| `truncate(h, size)` | `FileChannel.truncate` | |
| `sync(h)` | `FileChannel.force(true)` | |
| `pipe()` | `Pipe.open()` | consecutive cookies |
| `stdio(fd)` | `Channels.newChannel(System.in/out/err)` | 0/1/2 → reserved cookies; else `EBADF` |
| `handleKind(h)` | — | cached in `File` at open; drives Seek/ReadDir/deadline decisions |

**Stat** — via the NIO **`unix:` attribute view**
(`Files.getAttribute(p, "unix:mode", NOFOLLOW_LINKS)`), which yields the
*full* `st_mode` (type + suid/sgid/sticky + perm bits), `ino`, `dev`,
`uid`, `gid`. Five scalar intrinsics, no `Stat_t` layout, no getdents:

| Symbol | Returns |
|---|---|
| `statMode(path, follow)` | full `st_mode` or `-errno` |
| `statSize(path, follow)` | `size` (`BasicFileAttributes`) |
| `statMtimeNs(path, follow)` | mtime nanos |
| `statDev(path, follow)` / `statIno(path, follow)` | dev/ino → `SameFile` = `dev==dev && ino==ino` |

`File.Stat()` on an open handle: for `FileChannel` handles the open
path is known (stored in `Handle`) → same intrinsics; for pipe/stdio
handles the mode is *synthesized* from the kind (`S_IFIFO`/`S_IFCHR`,
size 0) — documented approximation (JVM has no fstat-on-channel).

**Dirs**: `open` with kind=dir stores a `DirectoryStream` iterator;
`dirNext(h, max)` returns up to `max` entries encoded
`name\0typeChar` joined (`typeChar` from each entry's `unix:mode`:
`d f l p s b c ?`; `?` = NIO-indistinguishable "other" — invisible in
M8 scope since fifos/sockets can't be created without `syscall`);
empty string = end. `close` reuses `close(h)`. No `linux_dirent64`
parsing anywhere.

**Path ops**: `mkdir` (`Files.createDirectory` + `PosixFileAttributeView`
perm set), `remove` (`Files.delete`), `rename` (same-`FileStore` check
→ `EXDEV`, else `Files.move(REPLACE_EXISTING)`), `symlink`,
`readlink(path, buf, off, ln)`, `link` (`Files.createLink`),
`chmod(path, perm)` (perm bits only — §6.3), `chtimes`
(`BasicFileAttributeView.setTimes`), `truncatePath`.

**Env** (overlay — JVM has no `setenv(2)`): `GoOs` holds
`ConcurrentHashMap<String,String> envOverlay` + `cleared` flag.
`getenvFound(key)` → 0/1, `getenv(key)` → `GoString`,
`setenv/unsetenv/clearenv`, `environ()` → NUL-joined `k=v`.
Lookup order: overlay → `System.getenv` (unless cleared). **Documented
divergence**: mutations are invisible to native code and to exec'd
children (no `os/exec` in M8, so the only observer is Go code itself —
which sees exact gc semantics). Key validation (`=`/NUL/empty → EINVAL)
in synthetic Go, mirroring `syscall.Setenv`.

**Identity/process**: `getpid` (`ProcessHandle.current().pid()`),
`getppid` (parent pid, 0 if unknown), `getuid/geteuid/getgid/getegid`
(`com.sun.security.auth.module.UnixSystem` — public API in module
`jdk.security.auth`; methods `getUid/getEuid/getGid/getEgids`),
`getgroups` (NUL-joined decimal gids), `hostname(buf,off,ln)`
(`InetAddress.getLocalHost().getHostName()`; on failure the *Go* side
falls back to reading `/proc/sys/kernel/hostname` — mirrors gc's
`sys_linux.go` structure), `exit(code)` (`System.exit`; no deferreds,
like gc), `argc()`/`argv(i)` (launcher-provided, §4),
`getcwd()`/`chdir(path)` (§2.5).

**Entropy** (added P3-M8 post-analysis, coordinator): `getrandom(b)` —
fills the GoSlice with `java.security.SecureRandom` bytes; returns the
count. Backs `crypto/rand` (the frozen table had no entropy symbol;
the crypto worker fail-fasted pending this).

`getpagesize` → Go const `4096` (linux/amd64 ABI; drift-tested vs
`syscall.Getpagesize` under gc).

### 2.5 Virtual cwd

The JVM cannot change the OS cwd (`user.dir` is a startup snapshot).
Since **all** Go file ops route through `GoOs.resolve()`, we implement
a process-wide virtual cwd: `volatile Path vcwd` (init = `user.dir`).
`chdir` validates (exists + is-dir, else ENOENT/ENOTDIR/EACCES) and
stores `p.toRealPath()` (physical, symlink-resolved — matches gc's
`Getwd`). `getcwd` returns it. `Getwd`'s `$PWD` kludge ports to
synthetic Go using `statDev/statIno` identity. Risk: any new intrinsic
that forgets `resolve()` — mitigated by the single choke point + a
unit test asserting every path-taking symbol resolves relatively.

### 2.6 What this design DISSOLVES (vs the sibling's fd-table plan)

- **No `Stat_t` layout**: `unix:mode/ino/dev` attributes give every
  field `fillFileStatFromSys` needs (mode bits, size, mtime, dev/ino).
  `syscall.Stat_t` never crosses the boundary.
- **No `linux_dirent64` parsing**: `DirectoryStream` iterator in Java;
  gc's `dirent`/`readdir` name/type loop ports to Go over `dirNext`.
- **No `Utsname` layout**: `InetAddress` + `/proc` fallback.
- EINTR: `FileChannel` never surfaces it (JDK retries); keep gc's
  `ignoringEINTR` wrapper shape in Go as a no-op pass-through for
  structural fidelity.

### 2.7 What genuinely needs real syscalls (hence fail-fast)

`chown(2)` by uid/gid (NIO: names only, no numeric API), process
spawn/wait/signal (need fd extraction for stdio redirect + `wait4`
rusage — `ProcessHandle` gives pid/alive/destroy only), `openat2`
(`Root`), file locking, `flock`/rlimit (no os consumer in M8).

## 3. Coordination with the `syscall` analysis

The sibling (worker 1/5) proposes: synthetic `syscall` with ~30 *typed*
intrinsics (exporter-lowered wire nodes) behind a **JVM-level fd table**,
with `os` calling `syscall.Open/Read/Write/Stat/...`. I recommend a
different cut. The **op inventory is nearly identical** — the
disagreement is mechanism and home:

**Recommendation:**

1. **Mechanism: linkname (ADR-0004) for all M8 OS-touching intrinsics**,
   in both packages — §2.1. One Java registration serves binders from
   both synthetic packages (same symbol, e.g. `graalgo.os.getenv`).
2. **I/O home: `os` binds directly to `graalgo.os.*`; synthetic `syscall`
   does NOT implement I/O wrappers in M8** — `syscall.Open/Read/Write/
   Close/Stat/Mkdir/...` fail fast with trap-style messages
   (`syscall.Open: no graalgo intrinsic; use os; re-test: fd-domain
   unification, Phase 4/Panama`). Reasons:
   - A JVM fake-fd table *plus* GoOs handle cookies = two file domains;
     `Fd()`/`NewFile`/`SyscallConn` across the seam would be incoherent.
     One domain (GoOs cookies, owned by os) is coherent: `Fd()` returns
     the cookie, `NewFile` re-wraps it.
   - The fd table reintroduces the hard problems §2.6 dissolves
     (getdents64 parsing, `Stat_t` layouts, `O_APPEND`/nonblocking fd
     semantics). For a bring-up milestone, dissolving them dominates.
   - Measured fact (sibling §5, independently confirmed here): **zero**
     direct `syscall.Syscall`/`RawSyscall` uses in linux/amd64 `os`, and
     `os` never needs an *untyped* trap — the whole contract is
     wrappers + constants + types.
3. **Shared vocabulary: `syscall` owns `Errno` + `Is` + errno/`O_*`/
   `S_*`/`DT_*` constants + types** (pure Go, zero intrinsics); `os`
   imports it. This is the *only* true coupling: `os.IsNotExist`,
   `errors.Is(err, syscall.ENOENT)`, and `underlyingErrorIs`'s
   `syscallErrorType` assertion all key off it.
4. **Scalar services keep gc's call graph**: where gc's `os` calls
   `syscall.Getenv/Getpid/Getuid/Exit/...`, synthetic `os` calls
   synthetic `syscall.Getenv/...`, and *those* are 3-line real-Go
   wrappers binding the same `graalgo.os.*` symbols. (Fallback: if the
   sibling scopes `Errno` out, `os` carries a private `errno` type with
   identical `Is` semantics + linux/amd64 ABI numbers, and a re-test
   trigger "adopt `syscall.Errno` when it lands".)

**Joint re-test trigger** (both docs): *fd-domain unification* — when
`syscall` needs real fds (Panama FFI, `net`, `os/exec`), decide once:
migrate GoOs to real fds or keep two domains with explicit translation.
Do not build the second domain in M8.

**Divergences from the sibling doc I am explicitly not adopting:**
(a) exporter-lowered wire nodes → linkname (§2.1);
(b) JVM fd table in `syscall` → single GoOs handle domain (§2.2);
(c) `Uname` intrinsic → `InetAddress` + `/proc` fallback (§2.4);
(d) `getdents64` intrinsic → `DirectoryStream` iterator (§2.6).
**Agreements:** no JNI/JNA (PinAudit stands); Panama FFI = Phase-4
trigger; `StartProcess`/`ForkExec`/`Wait4` fail fast (also §6.1 —
confirming the sibling's open question: **M8 os does not need
`StartProcess`**); `os/exec` out of M8.

## 4. Launcher / runtime gaps (needed for `os`, not just nice)

1. **argv passing — no convention exists.** `P3SyncMain` takes launcher
   flags + the input file; nothing reaches Go as `os.Args`. Need:
   launcher flag (e.g. args after `--`) + `GoOs.init(argv)` before Go
   init + the `argc`/`argv(i)` intrinsics. `os.Args[0]` = the input
   file path (documented analog of `go run`'s temp-binary path).
   Corpus `11` (§8) needs a `<base>.args` runner convention (like
   `.timing`/`.panic`) in `run_wave.sh`.
2. **`os.Exit` → `System.exit`.** No deferreds run (matches gc). The
   launcher already `System.exit(run(args))`; the intrinsic exits
   directly. Note: `testlog.PanicOnExit0`/`runtime.beforeExit` hooks are
   test-only — dropped.
3. **stdin must stay open** for `Stdin` reads; corpus programs redirect
   it from files (deterministic).
4. **Env overlay init**: `GoOs` snapshots nothing at startup; overlay +
   `System.getenv` fallback (§2.4) is enough.

## 5. API disposition map

(a) = pure synthetic Go · (b) = new `graalgo.os.*` intrinsic (§2.4) ·
(c) = fail-fast with named re-test trigger (§6).

**Package-level:** `Args`(b §4.1) · `Stdin/Stdout/Stderr`(b: `stdio`) ·
`DevNull`(a const) · `O_*`/`SEEK_*`(a consts, linux ABI, drift-tested) ·
`Expand/ExpandEnv`(a) · `IsPathSeparator`(a) · `NewSyscallError`(a) ·
`IsExist/IsNotExist/IsPermission/IsTimeout`(a — over synthetic
`Errno.Is`) · `SameFile`(a over `statDev/statIno`) · `TempDir`(a) ·
`UserHomeDir/UserConfigDir/UserCacheDir`(a over env) ·
`Getpagesize`(a const) · `Getwd`(b) · `Chdir`(b) · `Mkdir/MkdirAll`(b) ·
`Remove/RemoveAll`(b; `RemoveAll` logic ported verbatim) ·
`Rename/Symlink/Readlink/Link`(b) · `Chmod`(b, 0777 subset; §6.3) ·
`Chtimes`(b) · `Truncate`(b) · `Open/Create/OpenFile`(b) · `Pipe`(b) ·
`Stat/Lstat`(b) · `ReadFile/WriteFile`(a over b) ·
`ReadDir`(b) · `CreateTemp/MkdirTemp`(a over b + `runtime.fastrand`
linkname) · `Getenv/LookupEnv/Setenv/Unsetenv/Clearenv/Environ`(b,
overlay) · `Getpid/Getppid/Getuid/Geteuid/Getgid/Getegid/Getgroups`(b) ·
`Hostname`(b) · `Exit`(b).

**`File` methods:** `Name`(a) · `Fd`(a: returns cookie, §2.2) ·
`Read/ReadAt/Write/WriteAt/WriteString`(b; gc's single-call +
short-write logic verbatim) · `Seek`(b) · `Close`(b) · `Stat`(b, §2.4) ·
`Sync`(b) · `Truncate`(b) · `Chmod`(b, §6.3) · `Chdir`(b) ·
`ReadDir/Readdir/Readdirnames`(b over `dirNext`) ·
`SetDeadline/SetReadDeadline/SetWriteDeadline`(a: `ErrNoDeadline` for
regular files like gc; c for pipes §6.5) · `ReadFrom/WriteTo`(a:
generic `io.Copy` loop once synthetic `io` lands) ·
`SyscallConn`(c §6.4) · `Chown`(c §6.2).

**Errors/types:** `PathError/FileInfo/FileMode/DirEntry`(a: aliases to
synthetic `io/fs` — §0 prerequisite 2) · `LinkError/SyscallError`(a) ·
`Signal`(a: interface only) · `Process/ProcAttr/ProcessState`(a: types
only; behavior c §6.1).

## 6. Fail-fast list (exact; every entry needs its re-test trigger)

1. **Whole `Process` family**: `StartProcess`, `FindProcess`,
   `(*Process).Signal/Kill/Wait/Release/WithHandle`,
   `(*ProcessState).ExitCode/Exited/Pid/String/Success/Sys/SysUsage/
   UserTime/SystemTime`. *Why*: needs fd extraction for stdio redirect
   (`ProcAttr.Files` are GoOs cookies, not OS fds) + `wait4` rusage.
   *Trigger*: process-spawning milestone (`os/exec` bring-up).
2. **`Chown/Lchown/(*File).Chown`**. *Why*: NIO has no numeric-uid
   `chown(2)` (`PosixFileAttributeView.setOwner` takes names).
   *Trigger*: Panama FFI / JNI numeric-chown intrinsic.
3. **`Chmod` with `ModeSetuid/ModeSetgid/ModeSticky` bits**. 0777 subset
   works via NIO. *Why*: same as (2) — setuid bits need real
   `chmod(2)`. *Trigger*: same as (2). (Masking silently is forbidden;
   out-of-0777 modes return a descriptive error.)
4. **`(*File).SyscallConn`**. *Why*: needs `syscall.RawConn` + a real
   fd. *Trigger*: fd-domain unification (§3).
5. **Deadlines on pipes** (`SetDeadline` et al. where kind=pipe).
   Regular files correctly return `ErrNoDeadline` (matches gc).
   *Why*: needs timer-gated pipe I/O. *Trigger*: netpoller/timer
   integration for pipe deadlines.
6. **`OpenRoot/OpenInRoot/(*Root).*`**. *Why*: needs `openat2(2)`;
   Go-level lexical emulation has TOCTOU holes. *Trigger*: Panama
   `openat2` or a vetted root-emulation design.
7. **`Executable`**. *Why*: `/proc/self/exe` is the JVM binary — wrong
   answer, and inventing one is worse than failing. *Trigger*:
   launcher defines the "executable" concept for a `.go` input.
8. **`CopyFS/DirFS`** (need `io/fs.FS` machinery). *Trigger*:
   synthetic `io/fs` FS surface.
9. **`NewFile` with an fd that is neither 0/1/2 nor a live cookie** →
   returns `nil` (gc-faithful, not a panic).
10. **`(*File).Seek` on directory handles** — empirically resolved
    (go1.27.1, linux/amd64 probe 2026-10-07): `Seek` on a dir
    **succeeds** (`pos=0, err=nil`); `Read` on a dir returns
    `n=0, *PathError{Op:"read", Err:EISDIR}` ("is a directory").
    Design: dir-handle `Seek(0, SEEK_SET)` rewinds via stream reopen;
    other dir seeks succeed as no-ops (documented); `read` on kind=dir
    returns `EISDIR` without touching the stream.

## 7. Three hardest behavioral risks

**R1 — Error identity must be exact, and it fails silently.**
`os.IsNotExist`, `errors.Is(err, fs.ErrNotExist)`,
`errors.Is(err, syscall.ENOENT)` all depend on a chain:
Java exception → Linux errno number → synthetic `Errno` →
`Errno.Is(target)` → `*PathError` wrap. The peril is
*misclassification at the Java boundary*, not missing cases:
`NoSuchFileException` vs `AccessDeniedException` ordering (a
permission-denied intermediate dir must be EACCES, not ENOENT),
dangling symlink vs missing parent, `InvalidPathException` (NUL byte
→ EINVAL — it is NOT an `IOException`), over-long paths
(ENAMETOOLONG), symlink loops (ELOOP). One wrong number breaks
`IsNotExist` *without any test failing loudly* — the chain just
returns false. Mitigations: (i) a single `GoOs.errno(Throwable)`
mapper, unit-tested per failure mode (not just happy path);
(ii) `Errno.Is` covering the exact gc mapping
(ENOENT→ErrNotExist, EACCES/EPERM→ErrPermission,
EEXIST→ErrExist); (iii) the corpus `01` program asserts the full
chain on every failing op (§8). This is the #1 differential risk —
it is also the sibling's risk #1; we agree.

**R2 — Read/write call-shape fidelity (partial reads, EOF, O_APPEND,
EPIPE).** gc's `File.Read` performs *exactly one* `read(2)` and
`File.Write` *exactly one* `write(2)` (short write → `ErrShortWrite`).
The intrinsic must do exactly one channel op too — no
"helpful" looping inside Java (the sibling's note (iii) §5 agrees).
EOF encoding (`0` = EOF, `n+1` = bytes, negative = `-errno`, §2.3)
must be exact: `(0, io.EOF)` at end, `(n, nil)` partial.
`O_APPEND` is safe: `StandardOpenOption.APPEND` puts real `O_APPEND`
on the fd, so each single `write(2)` appends atomically — but the Go
side must NOT split one `Write` into two intrinsic calls, or
atomicity breaks. **EPIPE/SIGPIPE**: gc's `epipecheck` *raises SIGPIPE*
on the first EPIPE (the process dies unless it handles the signal).
JVM options: `jdk.unsupported`'s `sun.misc.Signal.raise("PIPE")`
(fragile across JDKs) vs returning the EPIPE error (divergence: gc
dies, we survive). Recommendation: attempt the raise guarded by
availability; if unavailable, return the wrapped EPIPE error — and a
corpus program asserting the *death* (exit 141) differentially, so a
missing raise fails loudly instead of drifting.

**R3 — Virtual-cwd coherence.** Every path intrinsic must resolve
through `GoOs.resolve()`; one that forgets silently uses the JVM
cwd. `PathError.Path` must keep the *user-supplied* string, not the
resolved one. `$PWD`-kludge vs `toRealPath()` physical paths must
agree (probe: symlinked temp dir → `Getwd` returns the physical
path under both gc and graalgo). Resolved empirically (go1.27.1
probe, 2026-10-07): `Seek` on a dir *succeeds* (`pos=0, nil`);
`Read` on a dir → `(0, *PathError{Op:"read", Err:EISDIR})` — both
covered in §2.4/§6.10.

## 8. Differential corpus sketches (`corpus/wave1/os/`)

Conventions per `corpus/wave1/README.md`: deterministic output only,
hermetic (own temp dir via `MkdirTemp`, unique names, `RemoveAll`
cleanup; **skip-if-root** guard for permission tests —
`os.Geteuid()==0` → print fixed `SKIP-root`), `.timing` markers for
the concurrency programs, `.panic` files for fail-fast programs,
`.args` convention needed for program 11 (runner extension, §4.1).

1. `00_roundtrip` — `CreateTemp`→`Write`→`Seek(0,0)`→`Read`→`Close`;
   `WriteFile`/`ReadFile`; `Stat` size; `SameFile` true/false.
2. `01_error_identity` — `Stat`/`Open`/`Remove`/`Mkdir` on missing
   paths: `IsNotExist`, `errors.Is(err, fs.ErrNotExist)`,
   `errors.Is(err, syscall.ENOENT)`, `*PathError` Op/Path fields;
   0-perm file (skip-if-root): `IsPermission`; `Mkdir` existing dir:
   `IsExist`; `errors.Is(err, fs.ErrExist)`.
3. `02_read_edges` — 100-byte file, 7-byte buffer loop: exact bytes,
   final `(0, io.EOF)`; `ReadAt` at/beyond EOF (`io.EOF`, `n<len`);
   negative offset → `*PathError{Op:"readat"}`; empty file read →
   `(0, io.EOF)`; `Read` into zero-len buffer → `(0, nil)`.
4. `03_append_atomic` — `O_APPEND|O_CREATE|O_WRONLY`; 8 goroutines ×
   200 × 64-byte lines; assert 1600 lines, every line intact
   (sort + count; fixed-string output). `.timing`.
5. `04_concurrent_rw` — shared-offset concurrent `Read`/`Write`
   (no-crash + byte totals) and concurrent `ReadAt`/`WriteAt`
   (offset-independent → exact bytes). `.timing`.
6. `05_mkdirall` — 8 goroutines `MkdirAll` same nested path → all nil;
   `MkdirAll` through a *file* → error, not IsNotExist; `Mkdir`
   existing → `IsExist`; `RemoveAll` nested tree incl. read-only
   files; `Remove` non-empty dir → `ENOTEMPTY` (not IsNotExist).
7. `06_env` — `Setenv`/`LookupEnv`/`Getenv` round-trip; `Setenv`
   with `=`/NUL/empty key → error; `Unsetenv` missing → nil;
   `Clearenv` → `Environ` empty → restore marker var; `ExpandEnv`
   `${UNSET}` → `""`.
8. `07_links` — `Symlink`→`Lstat` (`ModeSymlink`) vs `Stat`
   (follows); `Readlink`; `Link`→`SameFile`; `Rename` over existing
   file (replaces), onto empty dir (replaces), missing src
   (`IsNotExist` + `*LinkError`); dangling-symlink `Stat` →
   `IsNotExist`, `Lstat` → ok.
9. `08_tempfile` — 50× `CreateTemp("p*.tmp")` unique names, mode
   `0600` via `Stat`; `MkdirTemp`; bad pattern → error; cleanup.
10. `09_chdir` — `Getwd`; `Chdir(tmp)`; relative
    `Create`/`Open`/`Stat`; `Getwd` == physical tmp (symlink
    component → resolved); `Chdir` to missing → `IsNotExist`;
    `(*File).Chdir` on dir handle. Restore cwd at end.
11. `10_readdir` — `Readdirnames(2)` twice partitions names;
    third call → `io.EOF`; `Readdir(-1)` all `FileInfo`;
    `os.ReadDir` sorted by filename; `DirEntry.Type()`/
    `Info()`; `ReadDir` on a *file* → `ENOTDIR`.
12. `11_args_exit` — print `len(os.Args)` and `os.Args[1:]`
    (`.args` fixture); separate tiny program calls `os.Exit(3)` →
    runner byte-compares exit codes.
13. `12_failfast` — one program per §6 item (or a table-driven one):
    each expects the documented panic line (`.panic` files);
    `StartProcess`, `Chown`, `SyscallConn`, `OpenRoot`,
    `Executable`, pipe `SetDeadline`.

## 9. Dependencies & sequencing

1. Synthetic `io/fs` core (UNOWNED — assign; ~150 lines: `PathError`,
   `FileInfo`, `DirEntry`, `File`, `FileMode` + bit constants,
   `Err*` sentinels, `FS` interface stub) → blocks everything.
2. Synthetic `syscall` vocabulary (sibling) → blocks error identity.
3. Synthetic `io` (worker 2) → blocks `ReadFrom`/`WriteTo`/all tests
   printing via `io`.
4. `synth_os.go` (~2500 lines real Go, modeled on `synth_errors.go`
   header discipline) + `Linknames` registrations + `GoOs.java`.
5. Appendix-M table entries (~40 public 2/3-result members) — stage
   as ONE patch to `mrdesugar_m5.go` coordinated with worker 2's.
6. Launcher argv convention (§4.1) + `run_wave.sh` `.args` support.
7. Drift tests: `O_*`/errno/`Getpagesize` consts vs real `syscall`
   under gc; p3frontend mirror test per the M7 checklist.

Estimated intrinsic count: ~40 symbols (§2.4) + `runtime.fastrand`
(reused). No new wire node kinds. No exporter changes beyond the
Appendix-M data patch.

## 10. Open questions for the coordinator

1. **Mechanism ruling** (§3): linkname vs wire-nodes for M8 — needed
   before the os and syscall workers write code, or they diverge.
2. **Who owns synthetic `io/fs` core** (§0.2) — the io worker scoped it
   out; os cannot start without it.
3. **`os.Args[0]` semantics** (§4.1): input-file path (proposed) vs
   launcher binary path vs empty.
4. **EPIPE/SIGPIPE policy** (§7 R2): guarded `Signal.raise` vs
   error-return — pick one; the corpus pins it either way.
5. **Amend the M6 inventory line** "os/signal (the JVM owns signals)"
   (sibling's open question 4): `os.Signal` the *type* is pure Go and
   ships; only *delivery* is JVM-owned. And the "Panama FFI" line for
   `asm_linux_amd64.s` stays — Panama remains the Phase-4 trigger.
