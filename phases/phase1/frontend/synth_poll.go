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
// Error identity: ErrDeadlineExceeded is a dedicated type with
// Timeout()==true (gc: poll.deadlineExceededError). The synthetic net
// package converts it to its own timeoutError (which additionally
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
type FD struct {
	Sysfd uintptr
	pd    pollDesc

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
func (fd *FD) Init() error {
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

// pollTimeoutError is the synthetic internal/poll deadline-exceeded
// error (gc: poll.deadlineExceededError). Timeout()==true, like gc. The
// synthetic net package converts it to its own timeoutError (which
// Is-matches os.ErrDeadlineExceeded) before wrapping in *net.OpError.
type pollTimeoutError struct{}

func (pollTimeoutError) Error() string   { return "i/o timeout" }
func (pollTimeoutError) Timeout() bool   { return true }
func (pollTimeoutError) Temporary() bool { return true }

// ErrDeadlineExceeded is returned when a poll wait exceeds its deadline.
var ErrDeadlineExceeded error = pollTimeoutError{}
`
