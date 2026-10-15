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

// NOTE (real-net lane, 2026-10-15): String is required by the real net
// package (rawconn.go: Network() poll.String). Verbatim from gc 1.27.1
// internal/poll/fd.go.
type String string

// NOTE (real-net lane, 2026-10-15): hook variables and error types
// required by the real net package. Verbatim from gc 1.27.1
// internal/poll/hook_unix.go, hook_cloexec.go, fd.go, sendfile*.go.
// These are test hooks; the synthetic poll's FD methods don't use them,
// but net references them.
var CloseFunc func(int) error = syscall.Close
var AcceptFunc func(int) (int, syscall.Sockaddr, error) = syscall.Accept
var Accept4Func func(int, int) (int, syscall.Sockaddr, error) = syscall.Accept4

type DeadlineExceededError struct{}

func (e *DeadlineExceededError) Error() string   { return "i/o timeout" }
func (e *DeadlineExceededError) Timeout() bool   { return true }
func (e *DeadlineExceededError) Temporary() bool { return true }

var TestHookDidWritev = func(wrote int) {}
var TestHookDidSendFile = func(dstFD *FD, src uintptr, written int64, err error, handled bool) {}

const failPollSpliceSendfile = "poll.Splice/SendFile: no graalgo intrinsic; splice/sendfile not in net's basic path, Phase 4"

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
// NOTE (real-net lane, 2026-10-15): Sysfd changed from uintptr to int,
// and IsStream/ZeroReadIsEOF added, to match the real poll.FD layout
// (gc 1.27.1 internal/poll/fd_unix.go). The real net package sets these
// fields in struct literals (fd_unix.go).
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
// NOTE (real-net lane, 2026-10-15): signature changed to match the real
// poll.FD.Init(net string, pollable bool) (gc 1.27.1). The net/pollable
// args are accepted for API compatibility; the synthetic poll ignores
// them (cookie model).
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
// Real-net compatibility: poll.FD methods required by the real net package
// (fd_posix.go, sock_posix.go, etc.). These are stubs: the real net's
// socket operations go through these methods, but the synthetic poll's
// FD is cookie-based (for the synthetic net). The real net needs a
// full poll.FD reimplementation backed by GoNet — that is a separate
// backend task (Phase 4). These stubs allow the real net to TYPE-CHECK;
// calling them panics with a clear message.
// NOTE (real-net lane, 2026-10-15).
// ---------------------------------------------------------------------------

const failPollFD = "poll.FD method: not implemented; real net requires GoNet-backed poll.FD, Phase 4"

func (fd *FD) Close() error { panic(failPollFD) }
func (fd *FD) Read(p []byte) (int, error) { panic(failPollFD) }
func (fd *FD) Write(p []byte) (int, error) { panic(failPollFD) }
func (fd *FD) Accept() (int, syscall.Sockaddr, string, error) { panic(failPollFD) }
func (fd *FD) Dup() (int, string, error) { panic(failPollFD) }
func (fd *FD) Shutdown(how int) error { panic(failPollFD) }
func (fd *FD) GetsockoptInt(level, name int) (int, error) { panic(failPollFD) }
func (fd *FD) SetsockoptInt(level, name, value int) error { panic(failPollFD) }
func (fd *FD) SetsockoptLinger(level, name int, l *syscall.Linger) error { panic(failPollFD) }
func (fd *FD) SetsockoptByte(level, name int, value byte) error { panic(failPollFD) }
func (fd *FD) SetsockoptInet4Addr(level, name int, value [4]byte) error { panic(failPollFD) }
func (fd *FD) SetsockoptIPMreq(level, name int, value *syscall.IPMreq) error { panic(failPollFD) }
func (fd *FD) SetsockoptIPMreqn(level, name int, value *syscall.IPMreqn) error { panic(failPollFD) }
func (fd *FD) SetsockoptIPv6Mreq(level, name int, value *syscall.IPv6Mreq) error { panic(failPollFD) }
func (fd *FD) ReadFrom(p []byte) (int, syscall.Sockaddr, error) { panic(failPollFD) }
func (fd *FD) ReadFromInet4(p []byte, from *syscall.SockaddrInet4) (int, error) { panic(failPollFD) }
func (fd *FD) ReadFromInet6(p []byte, from *syscall.SockaddrInet6) (int, error) { panic(failPollFD) }
func (fd *FD) WriteTo(p []byte, sa syscall.Sockaddr) (int, error) { panic(failPollFD) }
func (fd *FD) WriteToInet4(p []byte, sa *syscall.SockaddrInet4) (int, error) { panic(failPollFD) }
func (fd *FD) WriteToInet6(p []byte, sa *syscall.SockaddrInet6) (int, error) { panic(failPollFD) }
func (fd *FD) ReadMsg(p []byte, oob []byte, flags int) (int, int, int, syscall.Sockaddr, error) { panic(failPollFD) }
func (fd *FD) ReadMsgInet4(p []byte, oob []byte, flags int, sa4 *syscall.SockaddrInet4) (int, int, int, error) { panic(failPollFD) }
func (fd *FD) ReadMsgInet6(p []byte, oob []byte, flags int, sa6 *syscall.SockaddrInet6) (int, int, int, error) { panic(failPollFD) }
func (fd *FD) WriteMsg(p []byte, oob []byte, sa syscall.Sockaddr) (int, int, error) { panic(failPollFD) }
func (fd *FD) WriteMsgInet4(p []byte, oob []byte, sa *syscall.SockaddrInet4) (int, int, error) { panic(failPollFD) }
func (fd *FD) WriteMsgInet6(p []byte, oob []byte, sa *syscall.SockaddrInet6) (int, int, error) { panic(failPollFD) }
func (fd *FD) Destroy() error { panic(failPollFD) }
func (fd *FD) RawControl(f func(uintptr)) error { panic(failPollFD) }
func (fd *FD) RawRead(f func(uintptr) bool) error { panic(failPollFD) }
func (fd *FD) RawWrite(f func(uintptr) bool) error { panic(failPollFD) }
func (fd *FD) Writev(v *[][]byte) (int64, error) { panic(failPollFD) }
func (fd *FD) ReadLock() error { panic(failPollFD) }
func (fd *FD) ReadUnlock() { panic(failPollFD) }
func (fd *FD) WriteOnce(p []byte) (int, error) { panic(failPollFD) }

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

// DupCloseOnExec (gc 1.27.1, src/internal/poll/fd_unix.go:658):
// duplicates fd with close-on-exec flag set. Not yet implemented as a
// graalgo intrinsic; panics honestly if called (re-test when the fd-domain
// unification lands, Phase 4/Panama).
func DupCloseOnExec(fd int) (int, string, error) {
	panic("poll.DupCloseOnExec: no graalgo intrinsic; re-test: fd-domain unification, Phase 4/Panama")
}
`
