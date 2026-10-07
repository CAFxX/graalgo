#!/usr/bin/env python3
"""Generate phases/phase1/frontend/synth_syscall.go.

The P3-M8 synthetic `syscall` package (linux/amd64): vocabulary + scalar
services only (R2/R3). Mechanical parts are copied verbatim from the
vendored go1.27.1 stdlib:
  - zerrors_linux_amd64.go : all consts + errors/signals tables
  - zsysnum_linux_amd64.go : SYS_* consts + trap-number->name map
  - ztypes_linux_amd64.go  : selected struct types (verbatim layouts)
Hand-authored: Errno/Signal/WaitStatus + methods, env family, scalar
wrappers binding graalgo.os.* via //go:linkname, trap fail-fast, and the
fail-fast I/O/proc/socket/mmap wrappers with exact gc signatures.

Output is gofmt'd before embedding.
"""
import os
import re
import subprocess
import sys

STD = os.path.expanduser("~/workspace/graalgo-stdlib/go1.27.1/syscall")
OUT = os.path.expanduser("~/workspace/graalgo-m8-pipe/phases/phase1/frontend/synth_syscall.go")


def read(name):
    with open(os.path.join(STD, name)) as f:
        return f.read()


def extract_blocks(text, start_markers):
    """Extract top-level const/var blocks beginning with any of the markers."""
    lines = text.split("\n")
    out = []
    i = 0
    n = len(lines)
    while i < n:
        hit = False
        for m in start_markers:
            if lines[i].startswith(m):
                hit = True
                break
        if hit:
            depth = 0
            while i < n:
                out.append(lines[i])
                for ch in lines[i]:
                    if ch in "({":
                        depth += 1
                    elif ch in ")}":
                        depth -= 1
                i += 1
                if depth <= 0 and out[-1].strip() in (")", "}"):
                    break
            out.append("")
        else:
            i += 1
    return "\n".join(out)


# --- zerrors: 3 const blocks + errors table + signals table -----------------
zerrors = read("zerrors_linux_amd64.go")
consts_block = extract_blocks(zerrors, ["const ("])
tables = extract_blocks(zerrors, ["var errors =", "var signals ="])

# --- zsysnum: const block + trap name map ------------------------------------
zsysnum = read("zsysnum_linux_amd64.go")
sysnum_consts = extract_blocks(zsysnum, ["const ("])

trap_names = []  # (number, name)
seen_nums = {}
for m in re.finditer(r"(?m)^\t(SYS_[A-Za-z0-9_]+)\s*=\s*(0x[0-9a-fA-F]+|\d+)", zsysnum):
    name, val = m.group(1), int(m.group(2), 0)
    if val in seen_nums:
        continue  # duplicate number: keep first (message use only)
    seen_nums[val] = name
    trap_names.append((val, name))
trap_names.sort()
trap_map_entries = "\n".join('\t%d: "%s",' % (v, n) for v, n in trap_names)

# --- ztypes: selected struct types, verbatim ----------------------------------
ztypes = read("ztypes_linux_amd64.go")
WANT_TYPES = [
    "Timespec", "Timeval", "Time_t", "Tms", "Utimbuf", "Rusage", "Rlimit",
    "Stat_t", "Statfs_t", "Fsid", "Dirent", "Utsname", "EpollEvent",
    "InotifyEvent",
]


def extract_type(text, name):
    # matches "type Name struct {" ... "}" or "type Name <underlying>"
    m = re.search(r"(?m)^type %s (struct \{.*?\n\}|[^\n]+)" % re.escape(name), text,
                  re.DOTALL)
    if not m:
        raise SystemExit("type %s not found" % name)
    return m.group(0)


type_block = "\n\n".join(extract_type(ztypes, t) for t in WANT_TYPES)

# --- fail-fast wrapper inventory ----------------------------------------------
# (name, params, results[(name,type)...], category)
# categories: os | proc | net | mmap | demand
MSGS = {
    "os": "no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama",
    "proc": "no graalgo intrinsic; re-test: os/exec milestone",
    "net": "no graalgo intrinsic; re-test: net package bring-up",
    "mmap": "no graalgo intrinsic; re-test: first real consumer",
    "demand": "no graalgo intrinsic; re-test: per-consumer demand",
}

# multi-result fail-fast: (name, params, results, category, pairtype, pairfields)
MR = [
    ("Open", "path string, mode int, perm uint32",
     [("fd", "int"), ("err", "error")], "os", "intErrorPair", ["v", "err"]),
    ("Read", "fd int, p []byte",
     [("n", "int"), ("err", "error")], "os", "intErrorPair", ["v", "err"]),
    ("Write", "fd int, p []byte",
     [("n", "int"), ("err", "error")], "os", "intErrorPair", ["v", "err"]),
    ("Pread", "fd int, p []byte, offset int64",
     [("n", "int"), ("err", "error")], "os", "intErrorPair", ["v", "err"]),
    ("Pwrite", "fd int, p []byte, offset int64",
     [("n", "int"), ("err", "error")], "os", "intErrorPair", ["v", "err"]),
    ("Seek", "fd int, offset int64, whence int",
     [("off", "int64"), ("err", "error")], "os", "int64ErrorPair", ["v", "err"]),
    ("Readlink", "path string, buf []byte",
     [("n", "int"), ("err", "error")], "os", "intErrorPair", ["v", "err"]),
    ("Mmap", "fd int, offset int64, length int, prot int, flags int",
     [("data", "[]byte"), ("err", "error")], "mmap", "bytesErrorPair", ["v", "err"]),
    ("StartProcess", "argv0 string, argv []string, attr *ProcAttr",
     [("pid", "int"), ("handle", "uintptr"), ("err", "error")],
     "proc", "startProcessTriple", ["pid", "handle", "err"]),
    ("ForkExec", "argv0 string, argv []string, attr *ProcAttr",
     [("pid", "int"), ("err", "error")], "proc", "intErrorPair", ["v", "err"]),
    ("Wait4", "pid int, wstatus *WaitStatus, options int, rusage *Rusage",
     [("wpid", "int"), ("err", "error")], "proc", "intErrorPair", ["v", "err"]),
    ("Socket", "domain, typ, proto int",
     [("fd", "int"), ("err", "error")], "net", "intErrorPair", ["v", "err"]),
    ("Accept", "fd int",
     [("nfd", "int"), ("sa", "Sockaddr"), ("err", "error")],
     "net", "acceptTriple", ["fd", "sa", "err"]),
    ("GetsockoptInt", "fd, level, opt int",
     [("value", "int"), ("err", "error")], "net", "intErrorPair", ["v", "err"]),
    ("Recvmsg", "fd int, p, oob []byte, flags int",
     [("n", "int"), ("oobn", "int"), ("recvflags", "int"),
      ("from", "Sockaddr"), ("err", "error")],
     "net", "recvmsgTuple", ["n", "oobn", "recvflags", "from", "err"]),
    ("EpollCreate", "size int",
     [("fd", "int"), ("err", "error")], "demand", "intErrorPair", ["v", "err"]),
    ("EpollCreate1", "flag int",
     [("fd", "int"), ("err", "error")], "demand", "intErrorPair", ["v", "err"]),
    ("EpollWait", "epfd int, events []EpollEvent, msec int",
     [("n", "int"), ("err", "error")], "demand", "intErrorPair", ["v", "err"]),
    ("InotifyInit", "",
     [("fd", "int"), ("err", "error")], "demand", "intErrorPair", ["v", "err"]),
    ("InotifyAddWatch", "fd int, pathname string, mask uint32",
     [("watchdesc", "int"), ("err", "error")], "demand", "intErrorPair", ["v", "err"]),
    ("InotifyRmWatch", "fd int, watchdesc uint32",
     [("success", "int"), ("err", "error")], "demand", "intErrorPair", ["v", "err"]),
]

# single-result (or zero-result) fail-fast: (name, params, results, category)
SR = [
    ("Close", "fd int", "err error", "os"),
    ("Stat", "path string, stat *Stat_t", "err error", "os"),
    ("Lstat", "path string, stat *Stat_t", "err error", "os"),
    ("Fstat", "fd int, stat *Stat_t", "err error", "os"),
    ("Mkdir", "path string, mode uint32", "err error", "os"),
    ("Chdir", "path string", "err error", "os"),
    ("Chmod", "path string, mode uint32", "err error", "os"),
    ("Chown", "path string, uid int, gid int", "err error", "os"),
    ("Lchown", "path string, uid int, gid int", "err error", "os"),
    ("Link", "oldpath string, newpath string", "err error", "os"),
    ("Symlink", "oldpath string, newpath string", "err error", "os"),
    ("Rename", "oldpath string, newpath string", "err error", "os"),
    ("Rmdir", "path string", "error", "os"),
    ("Unlink", "path string", "error", "os"),
    ("Truncate", "path string, length int64", "err error", "os"),
    ("UtimesNano", "path string, ts []Timespec", "error", "os"),
    ("Uname", "buf *Utsname", "err error", "os"),
    ("Kill", "pid int, sig Signal", "err error", "proc"),
    ("SetNonblock", "fd int, nonblocking bool", "error", "os"),
    ("CloseOnExec", "fd int", "", "os"),
    ("Pipe2", "p []int, flags int", "error", "os"),
    ("Munmap", "b []byte", "err error", "mmap"),
    ("Mprotect", "b []byte, prot int", "err error", "mmap"),
    ("Madvise", "b []byte, advice int", "err error", "mmap"),
    ("Flock", "fd int, how int", "err error", "demand"),
    ("Getrlimit", "resource int, rlim *Rlimit", "err error", "demand"),
    ("Setrlimit", "resource int, rlim *Rlimit", "error", "demand"),
    ("Setuid", "uid int", "err error", "demand"),
    ("Setgid", "gid int", "err error", "demand"),
    ("Bind", "fd int, sa Sockaddr", "err error", "net"),
    ("Connect", "fd int, sa Sockaddr", "err error", "net"),
    ("Listen", "s int, n int", "err error", "net"),
    ("SetsockoptInt", "fd, level, opt int, value int", "err error", "net"),
    ("Sendmsg", "fd int, p, oob []byte, to Sockaddr, flags int", "err error", "net"),
    ("Shutdown", "fd int, how int", "err error", "net"),
    ("EpollCtl", "epfd int, op int, fd int, event *EpollEvent", "err error", "demand"),
]


def const_name(name):
    return "fail" + name


def sig_str(params, results):
    if isinstance(results, list):
        r = "(" + ", ".join("%s %s" % t for t in results) + ")"
    else:
        r = results
        if r:
            r = "(" + r + ")"
    return "(%s) %s" % (params, r) if r else "(%s)" % params


fail_sec = []
for name, params, results, cat, pairtype, pairfields in MR:
    cn = const_name(name)
    msg = "syscall.%s: %s" % (name, MSGS[cat])
    fail_sec.append('const %s = "%s"' % (cn, msg))
    fail_sec.append("")
    rs = "(" + ", ".join("%s %s" % t for t in results) + ")"
    fail_sec.append("func %s(%s) %s {" % (name, params, rs))
    fail_sec.append("\tpanic(%s)" % cn)
    fail_sec.append("}")
    fail_sec.append("")
    fail_sec.append("func __%s(%s) %s {" % (name.lower(), params, pairtype))
    fail_sec.append("\tpanic(%s)" % cn)
    fail_sec.append("}")
    fail_sec.append("")
for name, params, results, cat in SR:
    cn = const_name(name)
    msg = "syscall.%s: %s" % (name, MSGS[cat])
    fail_sec.append('const %s = "%s"' % (cn, msg))
    fail_sec.append("")
    if results:
        fail_sec.append("func %s(%s) (%s) {" % (name, params, results))
    else:
        fail_sec.append("func %s(%s) {" % (name, params))
    fail_sec.append("\tpanic(%s)" % cn)
    fail_sec.append("}")
    fail_sec.append("")
fail_fast_src = "\n".join(fail_sec)

# desugar table entries for the staged mrdesugar_m5.go patch
desugar_entries = []
for name, params, results, cat, pairtype, pairfields in MR:
    fields = ", ".join('"%s"' % f for f in pairfields)
    desugar_entries.append(
        '\t{"syscall", "%s", "", "__%s", "%s", []string{%s}},'
        % (name, name.lower(), pairtype, fields))
desugar_entries.append('\t{"syscall", "Getenv", "", "__getenv", "getenvPair", []string{"value", "found"}},')
desugar_entries.append('\t{"syscall", "Getgroups", "", "__getgroups", "gidsErrorPair", []string{"gids", "err"}},')
desugar_entries.append('\t{"syscall", "Getwd", "", "__getwd", "stringErrorPair", []string{"v", "err"}},')
for tname in ["Syscall", "RawSyscall", "Syscall6", "RawSyscall6"]:
    desugar_entries.append(
        '\t{"syscall", "%s", "", "__%s", "syscallTriple", []string{"r1", "r2", "err"}},'
        % (tname, tname.lower()))

DESUGAR_PATCH_BODY = "\n".join(desugar_entries)

# --- linkname decls -------------------------------------------------------------
LINKNAMES = [
    ("graalgoOsGetenvFound", "graalgo.os.getenvFound", "key string", "int64"),
    ("graalgoOsGetenv", "graalgo.os.getenv", "key string", "string"),
    ("graalgoOsSetenv", "graalgo.os.setenv", "key, value string", "int64"),
    ("graalgoOsUnsetenv", "graalgo.os.unsetenv", "key string", "int64"),
    ("graalgoOsClearenv", "graalgo.os.clearenv", "", "int64"),
    ("graalgoOsEnviron", "graalgo.os.environ", "", "string"),
    ("graalgoOsGetpid", "graalgo.os.getpid", "", "int64"),
    ("graalgoOsGetppid", "graalgo.os.getppid", "", "int64"),
    ("graalgoOsGetuid", "graalgo.os.getuid", "", "int64"),
    ("graalgoOsGeteuid", "graalgo.os.geteuid", "", "int64"),
    ("graalgoOsGetgid", "graalgo.os.getgid", "", "int64"),
    ("graalgoOsGetegid", "graalgo.os.getegid", "", "int64"),
    ("graalgoOsGetgroups", "graalgo.os.getgroups", "", "string"),
    ("graalgoOsGetcwd", "graalgo.os.getcwd", "", "string"),
    ("graalgoOsExit", "graalgo.os.exit", "code int64", ""),
]
linkname_src = "\n".join(
    "//go:linkname %s %s\nfunc %s(%s)%s\n" % (
        ln, sym, ln, params, " " + res if res else "")
    for ln, sym, params, res in LINKNAMES)

SYMBOLS_CONSUMED = sorted(sym for _, sym, _, _ in LINKNAMES)

# --- assemble ------------------------------------------------------------------
SRC = '''package syscall

import (
	errorspkg "errors"
	"io/fs"
	"strconv"

	_ "unsafe"
)

// An Errno is an unsigned number describing an error condition.
// It implements the error interface. The zero Errno is by convention
// a non-error, so code to convert from Errno to error should use:
//
//	err = nil
//	if errno != 0 {
//		err = errno
//	}
//
// Errno values can be tested against error values using [errors.Is].
type Errno uintptr

func (e Errno) Error() string {
	if 0 <= int(e) && int(e) < len(errors) {
		s := errors[e]
		if s != "" {
			return s
		}
	}
	return "errno " + strconv.Itoa(int(e))
}

// Is reports whether e matches target. The mapping is gc's exact
// linux/amd64 mapping (syscall_unix.go): the fs.* sentinels are the
// synthetic io/fs package's (shared instance with synthetic os, so
// errors.Is(err, fs.ErrNotExist) and os.IsNotExist work across the
// package boundary), errors.ErrUnsupported is the synthetic errors
// package's.
func (e Errno) Is(target error) bool {
	switch target {
	case fs.ErrPermission:
		return e == EACCES || e == EPERM
	case fs.ErrExist:
		return e == EEXIST || e == ENOTEMPTY
	case fs.ErrNotExist:
		return e == ENOENT
	case errorspkg.ErrUnsupported:
		return e == ENOSYS || e == ENOTSUP || e == EOPNOTSUPP
	}
	return false
}

func (e Errno) Temporary() bool {
	return e == EINTR || e == EMFILE || e == ENFILE || e.Timeout()
}

func (e Errno) Timeout() bool {
	return e == EAGAIN || e == EWOULDBLOCK || e == ETIMEDOUT
}

// Do the interface allocations only once for common
// Errno values.
var (
	errEAGAIN error = EAGAIN
	errEINVAL error = EINVAL
	errENOENT error = ENOENT
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e Errno) error {
	switch e {
	case 0:
		return nil
	case EAGAIN:
		return errEAGAIN
	case EINVAL:
		return errEINVAL
	case ENOENT:
		return errENOENT
	}
	return e
}

// A Signal is a number describing a process signal.
// It implements the [os.Signal] interface.
type Signal int

func (s Signal) Signal() {}

func (s Signal) String() string {
	if 0 <= s && int(s) < len(signals) {
		str := signals[s]
		if str != "" {
			return str
		}
	}
	return "signal " + strconv.Itoa(int(s))
}

var (
	Stdin  = 0
	Stdout = 1
	Stderr = 2
)

const ImplementsGetwd = true

__ZERRORS_CONSTS__

__ZSYSNUM_CONSTS__

__ZERRORS_TABLES__

__ZTYPES__

type WaitStatus uint32

// Wait status is 7 bits at bottom, either 0 (exited),
// 0x7F (stopped), or a signal number that caused an exit.
// The 0x80 bit is whether there was a core dump.
// An extra number (exit code, signal causing a stop)
// is in the high bits. At least that's the idea.
// There are various irregularities. For example, the
// "continued" status is 0xFFFF, distinguishing itself
// from stopped via the core dump bit.

const (
	wsMask    = 0x7F
	wsCore    = 0x80
	wsExited  = 0x00
	wsStopped = 0x7F
	wsShift   = 8
)

func (w WaitStatus) Exited() bool { return w&wsMask == wsExited }

func (w WaitStatus) Signaled() bool { return w&wsMask != wsStopped && w&wsMask != wsExited }

func (w WaitStatus) Stopped() bool { return w&0xFF == wsStopped }

func (w WaitStatus) Continued() bool { return w == 0xFFFF }

func (w WaitStatus) CoreDump() bool { return w.Signaled() && w&wsCore != 0 }

func (w WaitStatus) ExitStatus() int {
	if !w.Exited() {
		return -1
	}
	return int(w>>wsShift) & 0xFF
}

func (w WaitStatus) Signal() Signal {
	if !w.Signaled() {
		return -1
	}
	return Signal(w & wsMask)
}

func (w WaitStatus) StopSignal() Signal {
	if !w.Stopped() {
		return -1
	}
	return Signal(w>>wsShift) & 0xFF
}

func (w WaitStatus) TrapCause() int {
	if w.StopSignal() != SIGTRAP {
		return -1
	}
	return int(w>>wsShift) >> 8
}

// Credential holds user and group identities for a new process.
type Credential struct {
	Uid         uint32   // User ID.
	Gid         uint32   // Group ID.
	Groups      []uint32 // Supplementary group IDs.
	NoSetGroups bool     // If true, don't set supplementary groups
}

// ProcAttr holds attributes that will be applied to a new process started
// by [StartProcess].
type ProcAttr struct {
	Dir   string   // Current working directory.
	Env   []string // Environment.
	Files []uintptr
	Sys   *SysProcAttr
}

// SysProcAttr holds attributes for Linux process creation.
// (Subset of gc's exec_linux.go SysProcAttr: the fields are vocabulary;
// StartProcess itself is fail-fast in M8.)
type SysProcAttr struct {
	Chroot     string
	Credential *Credential
	Ptrace     bool
	Setsid     bool
	Setpgid    bool
	Setctty    bool
	Noctty     bool
	Ctty       int
	Foreground bool
	Pgid       int
	Pdeathsig  Signal
	Cloneflags uintptr
}

// Sockaddr is gc's socket-address interface. The real method set needs
// unsafe (sockaddr() (unsafe.Pointer, _Socklen, error)); the synthetic
// package keeps the empty interface so call sites type-check — every
// socket operation fail-fasts at runtime (re-test: net package
// bring-up), so no method set is observable.
type Sockaddr interface{}

// SockaddrInet4/SockaddrInet6/SockaddrUnix are the public address forms
// (fields only; methods need unsafe and live with the fail-fast socket
// scope).
type SockaddrInet4 struct {
	Port int
	Addr [4]byte
}

type SockaddrInet6 struct {
	Port   int
	ZoneId uint32
	Addr   [16]byte
}

type SockaddrUnix struct {
	Name string
}

// RawConn/Conn mirror gc's syscall.Conn API (used by os.NewFile's
// SyscallConn, which fail-fasts in M8).
type RawConn interface {
	Control(f func(fd uintptr)) error
	Read(f func(fd uintptr) bool) error
	Write(f func(fd uintptr) bool) error
}

type Conn interface {
	SyscallConn() (RawConn, error)
}

func setTimespec(sec, nsec int64) Timespec {
	return Timespec{Sec: sec, Nsec: nsec}
}

func setTimeval(sec, usec int64) Timeval {
	return Timeval{Sec: sec, Usec: usec}
}

// Unix returns the time stored in ts as seconds plus nanoseconds.
func (ts *Timespec) Unix() (sec int64, nsec int64) {
	return int64(ts.Sec), int64(ts.Nsec)
}

// Unix returns the time stored in tv as seconds plus nanoseconds.
func (tv *Timeval) Unix() (sec int64, nsec int64) {
	return int64(tv.Sec), int64(tv.Usec) * 1000
}

// Nano returns the time stored in ts as nanoseconds.
func (ts *Timespec) Nano() int64 {
	return int64(ts.Sec)*1e9 + int64(ts.Nsec)
}

// Nano returns the time stored in tv as nanoseconds.
func (tv *Timeval) Nano() int64 {
	return int64(tv.Sec)*1e9 + int64(tv.Usec)*1000
}

// TimespecToNsec returns the time stored in ts as nanoseconds.
func TimespecToNsec(ts Timespec) int64 { return ts.Nano() }

// NsecToTimespec converts a number of nanoseconds into a [Timespec].
func NsecToTimespec(nsec int64) Timespec {
	sec := nsec / 1e9
	nsec = nsec % 1e9
	if nsec < 0 {
		nsec += 1e9
		sec--
	}
	return setTimespec(sec, nsec)
}

// TimevalToNsec returns the time stored in tv as nanoseconds.
func TimevalToNsec(tv Timeval) int64 { return tv.Nano() }

// NsecToTimeval converts a number of nanoseconds into a [Timeval].
func NsecToTimeval(nsec int64) Timeval {
	nsec += 999 // round up to microsecond
	usec := nsec % 1e9 / 1e3
	sec := nsec / 1e9
	if usec < 0 {
		usec += 1e6
		sec--
	}
	return setTimeval(sec, usec)
}

// StringByteSlice converts s to a NUL-terminated []byte.
// If s contains a NUL byte, it panics (gc 1.27.1:
// "syscall: string with NUL passed to StringByteSlice").
func StringByteSlice(s string) []byte {
	a, err := ByteSliceFromString(s)
	if err != nil {
		panic("syscall: string with NUL passed to StringByteSlice")
	}
	return a
}

// ByteSliceFromString returns a NUL-terminated []byte with the text of
// s. If s contains a NUL byte, it returns (nil, [EINVAL]).
func ByteSliceFromString(s string) ([]byte, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return nil, EINVAL
		}
	}
	a := make([]byte, len(s)+1)
	copy(a, s)
	return a, nil
}

// StringBytePtr returns a pointer to a NUL-terminated array of bytes.
// If s contains a NUL byte this function panics instead of returning
// an error.
//
// Deprecated: Use [BytePtrFromString] instead.
func StringBytePtr(s string) *byte { return &StringByteSlice(s)[0] }

// BytePtrFromString returns a pointer to a NUL-terminated array of
// bytes containing the text of s. If s contains a NUL byte at any
// location, it returns (nil, [EINVAL]).
func BytePtrFromString(s string) (*byte, error) {
	a, err := ByteSliceFromString(s)
	if err != nil {
		return nil, err
	}
	return &a[0], nil
}

// entersyscall/exitsyscall are no-ops on graalgo: blocking intrinsics
// run on virtual threads, which unmount the carrier automatically
// (SPEC §3.5 netpoller-deleted precedent). No P is ever released.
func runtime_entersyscall() {}
func runtime_exitsyscall() {}

// Fork/exec hooks pushed for the runtime's pull linknames. Fork and
// exec do not exist in M8 scope (StartProcess/ForkExec fail-fast), so
// these are no-ops. Re-test: process-spawning milestone.
func runtime_BeforeFork()      {}
func runtime_AfterFork()       {}
func runtime_AfterForkInChild() {}
func runtime_BeforeExec()      {}
func runtime_AfterExec()       {}

__LINKNAMES__

// errnoFromLong converts a graalgo.os intrinsic result (0 ok,
// negative -errno) to an error.
func errnoFromLong(n int64) error {
	if n == 0 {
		return nil
	}
	return Errno(-n)
}

// runtime_envs is pulled by the runtime (gc: //go:linkname in
// runtime). Provided here over the graalgo.os.environ intrinsic
// (NUL-joined "k=v").
func runtime_envs() []string {
	return splitNUL(graalgoOsEnviron())
}

func splitNUL(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if out == nil {
		return []string{}
	}
	return out
}

// Getpagesize returns the OS page size (4096 on linux/amd64;
// differential-pinned vs gc).
func Getpagesize() int { return 4096 }

// Exit causes the current program to exit with the given status code.
// Like gc's, it runs no deferred calls.
func Exit(code int) { graalgoOsExit(int64(code)) }

// runtimeSetenv/runtimeUnsetenv/runtimeClearenv are pulled by the
// runtime. They go through the same GoOs env overlay as
// Setenv/Unsetenv/Clearenv (documented divergence: the real process
// environ is not mutated — invisible to Go code; only native/cgo code
// could observe it, and cgo is excluded). Re-test: Panama FFI.
func runtimeSetenv(k, v string) { _ = graalgoOsSetenv(k, v) }
func runtimeUnsetenv(k string)  { _ = graalgoOsUnsetenv(k) }
func runtimeClearenv(env map[string]int) {
	// No Go-side env copy exists in M8 (the overlay is the source of
	// truth), so env is vestigial.
	_ = env
	_ = graalgoOsClearenv()
}

// Getenv retrieves the value of the environment variable named by key.
// Multi-result: call sites desugar to __getenv via Appendix M (staged
// mrdesugar_m5.go entry); the body below runs only on the gc reference
// leg and is unreachable on graalgo.
func Getenv(key string) (value string, found bool) {
	p := __getenv(key)
	return p.value, p.found
}

type getenvPair struct {
	value string
	found bool
}

func __getenv(key string) getenvPair {
	if len(key) == 0 {
		return getenvPair{}
	}
	if graalgoOsGetenvFound(key) == 0 {
		return getenvPair{}
	}
	return getenvPair{value: graalgoOsGetenv(key), found: true}
}

// Setenv sets the value of the environment variable named by key.
// Key validation mirrors gc (empty/'='/NUL key or NUL in value ->
// EINVAL); the mutation lands in the GoOs overlay.
func Setenv(key, value string) error {
	if len(key) == 0 {
		return EINVAL
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '=' || key[i] == 0 {
			return EINVAL
		}
	}
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return EINVAL
		}
	}
	return errnoFromLong(graalgoOsSetenv(key, value))
}

// Unsetenv unsets a single environment variable.
func Unsetenv(key string) error {
	return errnoFromLong(graalgoOsUnsetenv(key))
}

// Clearenv deletes all environment variables.
func Clearenv() {
	_ = graalgoOsClearenv()
}

// Environ returns a copy of strings representing the environment,
// in the form "key=value".
func Environ() []string {
	return splitNUL(graalgoOsEnviron())
}

// Getpid returns the process id of the caller.
func Getpid() (pid int) { return int(graalgoOsGetpid()) }

// Getppid returns the process id of the caller's parent.
func Getppid() (ppid int) { return int(graalgoOsGetppid()) }

// Getuid returns the numeric user id of the caller.
func Getuid() (uid int) { return int(graalgoOsGetuid()) }

// Geteuid returns the numeric effective user id of the caller.
func Geteuid() (euid int) { return int(graalgoOsGeteuid()) }

// Getgid returns the numeric group id of the caller.
func Getgid() (gid int) { return int(graalgoOsGetgid()) }

// Getegid returns the numeric effective group id of the caller.
func Getegid() (egid int) { return int(graalgoOsGetegid()) }

// Getgroups returns the numeric ids of the caller's groups.
// Multi-result: call sites desugar to __getgroups via Appendix M.
func Getgroups() (gids []int, err error) {
	p := __getgroups()
	return p.gids, p.err
}

type gidsErrorPair struct {
	gids []int
	err  error
}

func __getgroups() gidsErrorPair {
	// graalgo.os.getgroups returns NUL-joined decimal gids.
	s := graalgoOsGetgroups()
	if s == "" {
		return gidsErrorPair{}
	}
	parts := splitNUL(s)
	gids := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		v, ok := parseUint(p)
		if !ok {
			return gidsErrorPair{err: EINVAL}
		}
		gids = append(gids, int(v))
	}
	return gidsErrorPair{gids: gids}
}

// parseUint parses a decimal string; ok=false on any non-digit.
func parseUint(s string) (v uint64, ok bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + uint64(c-'0')
	}
	return v, true
}

// Getwd returns the current working directory.
// Multi-result: call sites desugar to __getwd via Appendix M.
// (Shape assumption, flagged: graalgo.os.getcwd returns the path as a
// GoString with no error channel; the intrinsic never returns "" in
// practice — the virtual cwd is always set — so the ENOENT arm is
// defensive only.)
func Getwd() (wd string, err error) {
	p := __getwd()
	return p.v, p.err
}

type stringErrorPair struct {
	v   string
	err error
}

func __getwd() stringErrorPair {
	wd := graalgoOsGetcwd()
	if wd == "" {
		return stringErrorPair{err: ENOENT}
	}
	return stringErrorPair{v: wd}
}

// syscallTrapNames maps trap numbers to SYS_* names for the fail-fast
// messages (generated from zsysnum_linux_amd64.go).
var syscallTrapNames = map[uintptr]string{
__TRAP_MAP__
}

func syscallTrapMsg(name string, trap uintptr) string {
	num := strconv.FormatUint(uint64(trap), 10)
	sname, ok := syscallTrapNames[trap]
	if !ok {
		sname = "unknown"
	}
	return "syscall." + name + ": trap " + num + " (" + sname + ") has no graalgo intrinsic; re-test: Panama FFI (Phase 4)"
}

// Syscall/RawSyscall/Syscall6/RawSyscall6 with arbitrary trap numbers
// are inexpressible under the handle-table value model (SPEC §7.3: a
// uintptr arriving at a trap site is a handle token, not a native
// address; no generic dispatcher can marshal pointer args without
// per-trap type knowledge). They fail fast with the trap number and
// name. Multi-result: call sites desugar via Appendix M; the public
// bodies below run only on the gc reference leg.
func Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("Syscall", trap))
}

func __syscall(trap, a1, a2, a3 uintptr) syscallTriple {
	panic(syscallTrapMsg("Syscall", trap))
}

func RawSyscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("RawSyscall", trap))
}

func __rawsyscall(trap, a1, a2, a3 uintptr) syscallTriple {
	panic(syscallTrapMsg("RawSyscall", trap))
}

func Syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("Syscall6", trap))
}

func __syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) syscallTriple {
	panic(syscallTrapMsg("Syscall6", trap))
}

func RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("RawSyscall6", trap))
}

func __rawsyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) syscallTriple {
	panic(syscallTrapMsg("RawSyscall6", trap))
}

type syscallTriple struct {
	r1  uintptr
	r2  uintptr
	err Errno
}

type intErrorPair struct {
	v   int
	err error
}

type int64ErrorPair struct {
	v   int64
	err error
}

type bytesErrorPair struct {
	v   []byte
	err error
}

type startProcessTriple struct {
	pid    int
	handle uintptr
	err    error
}

type acceptTriple struct {
	fd  int
	sa  Sockaddr
	err error
}

type recvmsgTuple struct {
	n         int
	oobn      int
	recvflags int
	from      Sockaddr
	err       error
}

__FAIL_FAST__
'''

src = SRC.replace("__ZERRORS_CONSTS__", consts_block.strip())
src = src.replace("__ZSYSNUM_CONSTS__", sysnum_consts.strip())
src = src.replace("__ZERRORS_TABLES__", tables.strip())
src = src.replace("__ZTYPES__", type_block.strip())
src = src.replace("__LINKNAMES__", linkname_src.strip())
src = src.replace("__TRAP_MAP__", trap_map_entries)
src = src.replace("__FAIL_FAST__", fail_fast_src.strip())

# gofmt the source before embedding
tmp = os.path.join(os.environ.get("TMPDIR", "/tmp"), "synth_syscall_gofmt.go")
with open(tmp, "w") as f:
    f.write(src)
r = subprocess.run(["gofmt", tmp], capture_output=True, text=True)
if r.returncode != 0:
    print("GOFMT FAILED:")
    print(r.stderr[:4000])
    sys.exit(1)
src = r.stdout
os.unlink(tmp)

HEADER = '''// P3-M8 (wave 2): synthetic `syscall` package (linux/amd64).
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer. Per R1/R2/R3: vocabulary + scalar services ONLY.
//   - IN: errno/SYS_*/O_*/S_*/DT_* constants (copied verbatim from
//     zerrors_linux_amd64.go / zsysnum_linux_amd64.go), struct types
//     (verbatim from ztypes_linux_amd64.go), Errno/Signal/WaitStatus
//     + methods, errnoErr, the env family, entersyscall/exitsyscall
//     no-ops, and scalar wrappers (Getenv/Getpid/Getuid/Exit/...)
//     binding the FROZEN graalgo.os.* table (os.md §2.4) via
//     //go:linkname (ADR-0004). The os worker implements the Java
//     side in GoOs.java; one Linknames registration serves both
//     synthetic packages.
//   - FAIL-FAST with exact trap-style messages naming re-test
//     triggers: Syscall/RawSyscall/Syscall6/RawSyscall6 (arbitrary
//     traps), the I/O wrappers (Open/Read/Write/Close/Stat/Mkdir/...),
//     StartProcess/ForkExec/Wait4, sockets/netlink, Mmap, Flock/
//     rlimit/setuid/epoll/inotify. The zsyscall wrappers are NOT reused
//     verbatim (they funnel untyped uintptrs through Syscall);
//     everything is re-authored as typed Go.
// Multi-result members keep exact gc signatures with fail-fast bodies;
// the real implementations (Getenv/Getgroups/Getwd) and the fail-fast
// helpers live in single-result __-helpers returning pair structs, and
// 2/3-target call sites desugar via Appendix M (staged mrdesugar_m5.go
// entries). Every other position fails fast (WS D).
package main

// syntheticSyscallSource is the P3-M8 synthetic `syscall` package.
const syntheticSyscallSource = `'''

with open(OUT, "w") as f:
    f.write(HEADER)
    f.write(src)
    f.write("`\n")

print("wrote", OUT, "(%d bytes)" % os.path.getsize(OUT))
print("trap names:", len(trap_names))
print("symbols consumed:")
for s in SYMBOLS_CONSUMED:
    print("  ", s)

with open(os.path.expanduser("~/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-staging/syscall/DESUGAR_ENTRIES.txt"), "w") as f:
    f.write("// Staged Appendix-M desugar entries for mrdesugar_m5.go\n")
    f.write("// (m7MultiResultFuncs table; applied by the coordinator at rebase).\n")
    f.write(DESUGAR_PATCH_BODY + "\n")
print("wrote DESUGAR_ENTRIES.txt")
