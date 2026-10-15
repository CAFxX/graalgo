package main

// P3-M9 (tier-4: net): synthetic `internal/poll` package.
//
// Real-Go source: adapted from gc go1.27.1
// ($GOROOT/src/internal/poll/fd_poll_runtime.go + errno_unix.go), per
// phases/phase3/hidden_files/m9-tmp/net-fork-contract.md:
//
//   - fd_poll_runtime.go logic kept unmodified; only the declaration block
//     is replaced with the single-result //go:linkname declarations (the
//     v1 frontend rejects multi-result declarations, even for linknames).
//   - pollDesc.init adapted to the cookie model: runtime_pollOpen takes the
//     socket cookie (uintptr) and returns a pollDesc cookie (0 = failure);
//     the errno comes from runtime_pollOpenErrno (thread-local).
//   - The gc FD type (fd_unix.go) is replaced by a minimal synthetic FD
//     carrying Sysfd + pollDesc + a closed flag. The synthetic net package
//     drives it through the exported methods below (Init/Close/WaitRead/
//     WaitWrite/Evict/SetDeadline/SetReadDeadline/SetWriteDeadline);
//     fd_poll_runtime.go's own methods stay unexported, exactly as in gc.
//   - runtime_pollSetDeadline semantics (contract): d is relative
//     nanoseconds (setDeadlineImpl already computes time.Until); d > 0
//     arms, d == 0 disarms, d < 0 expires immediately. Mode 'r'/'w'/'r'+'w'.
//
// Result codes for runtime_pollWait/runtime_pollReset (contract, matching
// fd_poll_runtime.go): 0 = no error, 1 = closing, 2 = timeout,
// 3 = not pollable.
//
// Error identity: ErrDeadlineExceeded is &DeadlineExceededError{} exactly
// as in gc 1.27.1 internal/poll/fd.go, with Timeout()==true. The synthetic
// net package converts it to its own timeoutError (which additionally
// Is-matches os.ErrDeadlineExceeded) before wrapping in *net.OpError.
//
// Only frontend/v1-supported constructs (no generics, no reflect, no
// unsafe beyond the blank import required by //go:linkname).
const syntheticPollSource = `package poll

import _ "unsafe" // for go:linkname
import "errors"
import "sync"
import "syscall"
import "time"

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// String is required by the real net package (rawconn.go).
// Verbatim from gc 1.27.1 internal/poll/fd.go.
type String string

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Hook variables required by the real net package (rawconn.go).
// SEMANTIC DIVERGENCE from gc 1.27.1 internal/poll/fd.go (NOT verbatim):
// the synthetic syscall.Close/Accept/Accept4 these point at are fail-fast
// panics until the ASM backend provides real fd syscalls. The variables
// exist only for source compatibility with net/rawconn.go — calling through
// them panics. Do not treat them as working hooks.
var CloseFunc func(int) error = syscall.Close
var AcceptFunc func(int) (int, syscall.Sockaddr, error) = syscall.Accept
var Accept4Func func(int, int) (int, syscall.Sockaddr, error) = syscall.Accept4

// DeadlineExceededError is returned by poll waits that exceed their
// deadline. Verbatim from gc 1.27.1 internal/poll/fd.go; backs
// ErrDeadlineExceeded exactly as in gc.
type DeadlineExceededError struct{}

func (e *DeadlineExceededError) Error() string   { return "i/o timeout" }
func (e *DeadlineExceededError) Timeout() bool   { return true }
func (e *DeadlineExceededError) Temporary() bool { return true }

var TestHookDidWritev = func(wrote int) {}
var TestHookDidSendFile = func(dstFD *FD, src uintptr, written int64, err error, handled bool) {}

const failPollSpliceSendfile = "poll.Splice/SendFile: temporary until ASM backend (his 2026-10-15 order)"

func Splice(dst, src *FD, remain int64) (written int64, handled bool, err error) {
	panic(failPollSpliceSendfile)
}

func SendFile(dstFD *FD, src uintptr, size int64) (n int64, err error, handled bool) {
	panic(failPollSpliceSendfile)
}

// ---------------------------------------------------------------------------
// Linkname intrinsics (net-fork-contract.md): symbols internal/poll.*,
// bound by graalgo.p3net.GoNetpoll. All params/results are int64/uintptr
// (the D5 shapes); int64 results are negative = -errno.
// The local name must equal the declared name (exporter scope-checks
// this, mirroring gc).
// ---------------------------------------------------------------------------

//go:linkname runtime_pollServerInit internal/poll.runtime_pollServerInit
func runtime_pollServerInit()

//go:linkname runtime_pollOpen internal/poll.runtime_pollOpen
func runtime_pollOpen(fd uintptr) uintptr // pollDesc cookie; 0 = failure

//go:linkname runtime_pollOpenErrno internal/poll.runtime_pollOpenErrno
func runtime_pollOpenErrno() int // errno of the last failed pollOpen (this goroutine)

//go:linkname runtime_pollClose internal/poll.runtime_pollClose
func runtime_pollClose(ctx uintptr)

//go:linkname runtime_pollWait internal/poll.runtime_pollWait
func runtime_pollWait(ctx uintptr, mode int) int // 0/1/2/3, see consts below

//go:linkname runtime_pollWaitCanceled internal/poll.runtime_pollWaitCanceled
func runtime_pollWaitCanceled(ctx uintptr, mode int)

//go:linkname runtime_pollReset internal/poll.runtime_pollReset
func runtime_pollReset(ctx uintptr, mode int) int // 0/1/2/3

//go:linkname runtime_pollSetDeadline internal/poll.runtime_pollSetDeadline
func runtime_pollSetDeadline(ctx uintptr, d int64, mode int)

//go:linkname runtime_pollUnblock internal/poll.runtime_pollUnblock
func runtime_pollUnblock(ctx uintptr)

//go:linkname runtime_isPollServerDescriptor internal/poll.runtime_isPollServerDescriptor
func runtime_isPollServerDescriptor(fd uintptr) bool // always false

//go:linkname runtimeNano runtime.nanotime
func runtimeNano() int64

// ---------------------------------------------------------------------------
// pollDesc + methods: verbatim from gc 1.27.1 fd_poll_runtime.go, except
// init (cookie model, per contract) and the declaration block above.
// ---------------------------------------------------------------------------

type pollDesc struct {
	runtimeCtx uintptr
}

var serverInit sync.Once

func (pd *pollDesc) init(fd *FD) error {
	serverInit.Do(runtime_pollServerInit)
	ctx := runtime_pollOpen(uintptr(fd.Sysfd))
	if ctx == 0 {
		return errnoErr(syscall.Errno(runtime_pollOpenErrno()))
	}
	pd.runtimeCtx = ctx
	return nil
}

func (pd *pollDesc) close() {
	if pd.runtimeCtx == 0 {
		return
	}
	runtime_pollClose(pd.runtimeCtx)
	pd.runtimeCtx = 0
}

// Evict evicts fd from the pending list, unblocking any I/O running on fd.
func (pd *pollDesc) evict() {
	if pd.runtimeCtx == 0 {
		return
	}
	runtime_pollUnblock(pd.runtimeCtx)
}

func (pd *pollDesc) prepare(mode int, isFile bool) error {
	if pd.runtimeCtx == 0 {
		return nil
	}
	res := runtime_pollReset(pd.runtimeCtx, mode)
	return convertErr(res, isFile)
}

func (pd *pollDesc) prepareRead(isFile bool) error {
	return pd.prepare('r', isFile)
}

func (pd *pollDesc) prepareWrite(isFile bool) error {
	return pd.prepare('w', isFile)
}

func (pd *pollDesc) wait(mode int, isFile bool) error {
	if pd.runtimeCtx == 0 {
		return errors.New("waiting for unsupported file type")
	}
	res := runtime_pollWait(pd.runtimeCtx, mode)
	return convertErr(res, isFile)
}

func (pd *pollDesc) waitRead(isFile bool) error {
	return pd.wait('r', isFile)
}

func (pd *pollDesc) waitWrite(isFile bool) error {
	return pd.wait('w', isFile)
}

func (pd *pollDesc) waitCanceled(mode int) {
	if pd.runtimeCtx == 0 {
		return
	}
	runtime_pollWaitCanceled(pd.runtimeCtx, mode)
}

func (pd *pollDesc) pollable() bool {
	return pd.runtimeCtx != 0
}

// Error values returned by runtime_pollReset and runtime_pollWait.
// These must match the values in runtime/netpoll.go.
const (
	pollNoError        = 0
	pollErrClosing     = 1
	pollErrTimeout     = 2
	pollErrNotPollable = 3
)

func convertErr(res int, isFile bool) error {
	switch res {
	case pollNoError:
		return nil
	case pollErrClosing:
		return errClosing(isFile)
	case pollErrTimeout:
		return ErrDeadlineExceeded
	case pollErrNotPollable:
		return ErrNotPollable
	}
	println("unreachable: ", res)
	panic("unreachable")
}

// SetDeadline sets the read and write deadlines associated with fd.
func (fd *FD) SetDeadline(t time.Time) error {
	return setDeadlineImpl(fd, t, 'r'+'w')
}

// SetReadDeadline sets the read deadline associated with fd.
func (fd *FD) SetReadDeadline(t time.Time) error {
	return setDeadlineImpl(fd, t, 'r')
}

// SetWriteDeadline sets the write deadline associated with fd.
func (fd *FD) SetWriteDeadline(t time.Time) error {
	return setDeadlineImpl(fd, t, 'w')
}

func setDeadlineImpl(fd *FD, t time.Time, mode int) error {
	var d int64
	if !t.IsZero() {
		d = int64(time.Until(t))
		if d == 0 {
			d = -1 // don't confuse deadline right now with no deadline
		}
	}
	if err := fd.incref(); err != nil {
		return err
	}
	defer fd.decref()

	if fd.pd.runtimeCtx == 0 {
		return ErrNoDeadline
	}
	runtime_pollSetDeadline(fd.pd.runtimeCtx, d, mode)
	return nil
}

// IsPollDescriptor reports whether fd is the descriptor being used by the poller.
// (Kept for gc API parity; the runtime always answers false.)
func IsPollDescriptor(fd uintptr) bool {
	return runtime_isPollServerDescriptor(fd)
}

// ---------------------------------------------------------------------------
// Minimal synthetic FD (replaces gc's fd_unix.go FD for the net fork).
// The synthetic net package owns the socket lifecycle; FD carries the
// cookie (Sysfd), the pollDesc, and a closed flag so SetDeadline after
// Close fails like gc (errClosing) instead of touching a dead pollDesc.
// ---------------------------------------------------------------------------

// FD is the synthetic poll file descriptor: a socket cookie plus its
// pollDesc. Only Sysfd is set by the owner before Init.
// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Sysfd changed from uintptr to int, and IsStream/ZeroReadIsEOF added,
// to match the real poll.FD layout (gc 1.27.1 internal/poll/fd_unix.go).
// The real net package sets these fields in struct literals.
type FD struct {
	Sysfd int
	pd    pollDesc

	IsStream      bool
	ZeroReadIsEOF bool

	mu     sync.Mutex
	closed bool
}

func (fd *FD) incref() error {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	if fd.closed {
		return errClosing(false)
	}
	return nil
}

func (fd *FD) decref() {}

// Init registers the socket cookie with the netpoller. The fork calls
// this before bind/listen/connect (net-fork-contract.md call order).
// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Signature changed to match the real poll.FD.Init(net string, pollable bool)
// (gc 1.27.1). The net/pollable args are accepted for API compatibility.
func (fd *FD) Init(net string, pollable bool) error {
	return fd.pd.init(fd)
}

// Evict unblocks any I/O currently waiting on fd (close path).
func (fd *FD) Evict() {
	fd.pd.evict()
}

// WaitRead waits for the socket to become readable.
func (fd *FD) WaitRead() error {
	return fd.pd.waitRead(false)
}

// WaitWrite waits for the socket to become writable.
func (fd *FD) WaitWrite() error {
	return fd.pd.waitWrite(false)
}

// ClosePoll releases the pollDesc. Called after the socket cookie itself
// is closed (graalgo.os.close evicts netpoller state); idempotent.
func (fd *FD) ClosePoll() {
	fd.mu.Lock()
	fd.closed = true
	fd.mu.Unlock()
	fd.pd.close()
}

// ---------------------------------------------------------------------------
// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Real-net compatibility: poll.FD methods required by the real net package
// (fd_posix.go, sock_posix.go, etc.). These are stubs: the real net's
// socket operations go through these methods. Calling them panics with a
// clear "temporary until ASM" message.
// ---------------------------------------------------------------------------

const failPollFD = "poll.FD method: temporary until ASM backend (his 2026-10-15 order)"

func (fd *FD) Close() error { panic(failPollFD) }
func (fd *FD) Read(p []byte) (int, error) { panic(failPollFD) }
func (fd *FD) Write(p []byte) (int, error) { panic(failPollFD) }
func (fd *FD) Accept() (int, syscall.Sockaddr, string, error) { panic(failPollFD) }
func (fd *FD) Dup() (int, string, error) { panic(failPollFD) }
func (fd *FD) Shutdown(how int) error { panic(failPollFD) }

// ---------------------------------------------------------------------------
// Errors and errno helpers (from gc errno_unix.go / fd.go, unix half).
// ---------------------------------------------------------------------------

var (
	errEAGAIN error = syscall.EAGAIN
	errEINVAL error = syscall.EINVAL
	errENOENT error = syscall.ENOENT
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case syscall.EAGAIN:
		return errEAGAIN
	case syscall.EINVAL:
		return errEINVAL
	case syscall.ENOENT:
		return errENOENT
	}
	return e
}

func errClosing(isFile bool) error {
	if isFile {
		return ErrFileClosing
	}
	return ErrNetClosing
}

var (
	ErrNetClosing  = errors.New("use of closed network connection")
	ErrFileClosing = errors.New("file already closed")
	ErrNoDeadline  = errors.New("file type does not support deadline")
	ErrNotPollable = errors.New("not pollable")
)

// ErrDeadlineExceeded is returned when a poll wait exceeds its deadline.
// Exactly gc 1.27.1 internal/poll/fd.go:
//
//	var ErrDeadlineExceeded error = &DeadlineExceededError{}
//
// (The synthetic net package converts it to its own timeoutError, which
// additionally Is-matches os.ErrDeadlineExceeded, before wrapping in
// *net.OpError.) A single error type — no synthetic duplicate.
var ErrDeadlineExceeded error = &DeadlineExceededError{}

// DupCloseOnExec (gc 1.27.1, src/internal/poll/fd_unix.go:658):
// duplicates fd with close-on-exec flag set. Not yet implemented as a
// graalgo intrinsic; panics honestly if called (re-test when the fd-domain
// unification lands, Phase 4/Panama).
func DupCloseOnExec(fd int) (int, string, error) {
	panic("poll.DupCloseOnExec: no graalgo intrinsic; re-test: fd-domain unification, Phase 4/Panama")
}
`
