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
import "io"
import "sync"
import "syscall"
import "time"

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// String is required by the real net package (rawconn.go).
// Verbatim from gc 1.27.1 internal/poll/fd.go.
type String string

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Hook variables and error types required by the real net package.
// Verbatim from gc 1.27.1 internal/poll.
var CloseFunc func(int) error = syscall.Close
var AcceptFunc func(int) (int, syscall.Sockaddr, error) = syscall.Accept
var Accept4Func func(int, int) (int, syscall.Sockaddr, error) = syscall.Accept4

type DeadlineExceededError struct{}

func (e *DeadlineExceededError) Error() string   { return "i/o timeout" }
func (e *DeadlineExceededError) Timeout() bool   { return true }
func (e *DeadlineExceededError) Temporary() bool { return true }

var TestHookDidWritev = func(wrote int) {}
var TestHookDidSendFile = func(dstFD *FD, src uintptr, written int64, err error, handled bool) {}

// Splice (gc 1.27.1 internal/poll/sendfile_linux.go): the JVM has no
// splice(2), so the socket pair is never spliced in-kernel. Returning
// handled=false is the contract-faithful fallback: gc's own
// splice_linux.go returns handled=false on EINVAL (unsupported), and
// net's sendFile falls back to the generic copy when handled is false.
// The caller (net.sendFile) then copies with the same observable bytes.
func Splice(dst, src *FD, remain int64) (written int64, handled bool, err error) {
	return 0, false, nil
}

// SendFile (gc 1.27.1 internal/poll/sendfile_linux.go): same fallback
// as Splice — the JVM has no sendfile(2). The TestHookDidSendFile call
// (deferred first, exactly like gc) reports the not-handled outcome;
// net's sendFile falls back to the generic copy. Locks and prepare
// match gc: a closed fd reports errClosing with handled=false.
func SendFile(dstFD *FD, src uintptr, size int64) (n int64, err error, handled bool) {
	defer func() { TestHookDidSendFile(dstFD, src, n, err, handled) }()
	if err := dstFD.writeLock(); err != nil {
		return 0, err, false
	}
	defer dstFD.writeUnlock()
	if err := dstFD.pd.prepareWrite(false); err != nil {
		return 0, err, false
	}
	return 0, nil, false
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

// ---------------------------------------------------------------------------
// Socket-trap linknames (O-P1-4): the FD I/O surface drives the JVM
// socket channel through the same graalgo.os.* / graalgo.net.* traps
// the synthetic net package uses (netSockRead etc. in synth_net.go).
// Poll-local names keep the poll package self-contained; duplicate
// targets are the established pattern (osRead/netSockRead both bind
// graalgo.os.read). int64 results are negative = -errno, except the
// read/write traps, which return n+1 (0 = EOF on read).
// ---------------------------------------------------------------------------

//go:linkname pollSockRead graalgo.os.read
func pollSockRead(h int64, b []byte) int64 // n+1 (0=EOF) or -errno

//go:linkname pollSockWrite graalgo.os.write
func pollSockWrite(h int64, b []byte) int64 // n+1 or -errno

//go:linkname pollSockClose graalgo.os.close
func pollSockClose(h int64) int64 // 0 or -errno

//go:linkname pollAccept graalgo.net.accept
func pollAccept(cookie int64) int64 // new cookie or -errno (incl. -EAGAIN)

//go:linkname pollShutdown graalgo.net.shutdown
func pollShutdown(cookie int64, how int64) int64 // 0 or -errno

//go:linkname pollGetpeername graalgo.net.getpeername
func pollGetpeername(cookie int64, buf []byte) int64 // n+1 or -errno

//go:linkname pollDup graalgo.os.dup
func pollDup(cookie int64) int64 // new cookie or -errno

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

	// I/O operations in progress (gc fdMutex shape, mutex form):
	// readLock/writeLock serialize concurrent Read/Write calls
	// (gc fdMutex rwlock), incref/decref count in-flight operations
	// so Close can wait for them to drain before destroying the fd.
	// A dup'd alias shares the channel (graalgo.os.dup refcounting)
	// but has its own FD, refs and locks — exactly like gc, where a
	// dup'd fd is an independent file description over the same
	// underlying socket.
	refs int
	// done is closed by destroy when the last ref drops after Close;
	// nil until the first Close (created lazily, under mu).
	done chan struct{}
	// gone is set when destroy has run (so the final decref destroys
	// exactly once).
	gone bool
	// destroyErr stashes the cookie-close error for Close to return
	// (gc fd_unix.go: Close returns destroy's error).
	destroyErr error
	// rmu serializes Read calls; wmu serializes Write calls.
	rmu sync.Mutex
	wmu sync.Mutex

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
	fd.refs++
	return nil
}

func (fd *FD) decref() {
	fd.mu.Lock()
	fd.refs--
	doDestroy := fd.refs == 0 && fd.closed && !fd.gone
	fd.mu.Unlock()
	if doDestroy {
		fd.destroy()
	}
}

// destroy releases the pollDesc and the socket cookie. Called exactly
// once, by the final decref after Close (gc fd_unix.go destroy).
// The cookie close error is stashed for Close to return.
func (fd *FD) destroy() {
	fd.mu.Lock()
	if fd.gone {
		fd.mu.Unlock()
		return
	}
	fd.gone = true
	done := fd.done
	fd.mu.Unlock()
	fd.pd.close()
	if fd.Sysfd >= 0 {
		if r := pollSockClose(int64(fd.Sysfd)); r < 0 {
			fd.mu.Lock()
			fd.destroyErr = errnoErr(syscall.Errno(-r))
			fd.mu.Unlock()
		}
		fd.mu.Lock()
		fd.Sysfd = -1
		fd.mu.Unlock()
	}
	if done != nil {
		close(done)
	}
}

// readLock serializes Read calls (gc fdMutex rwlock, mutex form) and
// takes a ref so Close waits for the read to drain.
func (fd *FD) readLock() error {
	fd.rmu.Lock()
	fd.mu.Lock()
	if fd.closed {
		fd.mu.Unlock()
		fd.rmu.Unlock()
		return errClosing(false)
	}
	fd.refs++
	fd.mu.Unlock()
	return nil
}

func (fd *FD) readUnlock() {
	fd.rmu.Unlock()
	fd.decref()
}

// writeLock serializes Write calls (gc fdMutex rwlock, mutex form) and
// takes a ref so Close waits for the write to drain.
func (fd *FD) writeLock() error {
	fd.wmu.Lock()
	fd.mu.Lock()
	if fd.closed {
		fd.mu.Unlock()
		fd.wmu.Unlock()
		return errClosing(false)
	}
	fd.refs++
	fd.mu.Unlock()
	return nil
}

func (fd *FD) writeUnlock() {
	fd.wmu.Unlock()
	fd.decref()
}

// eofError maps a zero-byte read to EOF only when ZeroReadIsEOF
// (gc fd_unix.go eofError).
func (fd *FD) eofError(n int, err error) error {
	if n == 0 && err == nil && fd.ZeroReadIsEOF {
		return io.EOF
	}
	return err
}

// maxRW is the largest read/write chunk (gc fd_unix.go).
const maxRW = 1 << 30

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

// ---------------------------------------------------------------------------
// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Real-net compatibility: poll.FD methods required by the real net package
// (fd_posix.go, sock_posix.go, etc.). O-P1-4 implemented: real I/O through
// the graalgo.os.* / graalgo.net.* traps (pollSockRead etc. below), with
// gc's fdMutex refcounting (incref/decref, read/write locks) — see the
// FD struct fields. ClosePoll was deleted: it had no callers, and
// graalgo.os.close already evicts netpoller state (GoNetpoll.evictForClose),
// so there is no second close path to keep in sync.
// ---------------------------------------------------------------------------

// Close (gc 1.27.1 internal/poll/fd_unix.go): marks the fd closed,
// unblocks in-flight I/O, waits for it to drain, then destroys the
// pollDesc and the socket cookie. A second Close fails with errClosing.
func (fd *FD) Close() error {
	if err := fd.incref(); err != nil {
		return err
	}
	fd.mu.Lock()
	if fd.closed {
		// Unreachable in practice (incref already failed on closed),
		// but keep the gc shape: never close twice.
		fd.mu.Unlock()
		fd.decref()
		return errClosing(false)
	}
	fd.closed = true
	fd.done = make(chan struct{})
	fd.mu.Unlock()
	fd.pd.evict()
	fd.decref()
	<-fd.done
	fd.mu.Lock()
	err := fd.destroyErr
	fd.mu.Unlock()
	return err
}

// Read (gc 1.27.1 internal/poll/fd_unix.go, socket half): one
// pollSockRead per loop iteration. Trap contract (netSockRead):
// r == 0 is EOF, r < 0 is -errno, r-1 is the byte count. A 0-byte
// read (r == 1) is the JVM's EAGAIN (nonblocking channel, nothing
// yet): wait on the poller and retry, exactly like gc on EAGAIN.
func (fd *FD) Read(p []byte) (int, error) {
	if err := fd.readLock(); err != nil {
		return 0, err
	}
	defer fd.readUnlock()
	if len(p) == 0 {
		return 0, nil
	}
	if err := fd.pd.prepareRead(false); err != nil {
		return 0, err
	}
	if fd.IsStream && len(p) > maxRW {
		p = p[:maxRW]
	}
	for {
		r := pollSockRead(int64(fd.Sysfd), p)
		if r == 0 {
			return 0, fd.eofError(0, nil)
		}
		if r < 0 {
			err := errnoErr(syscall.Errno(-r))
			if err == syscall.EAGAIN && fd.pd.pollable() {
				if err := fd.pd.waitRead(false); err == nil {
					continue
				}
				return 0, err
			}
			return 0, fd.eofError(0, err)
		}
		n := int(r - 1)
		if n == 0 {
			// JVM 0-byte read = kernel EAGAIN: wait and retry.
			if err := fd.pd.waitRead(false); err != nil {
				return 0, err
			}
			continue
		}
		return n, fd.eofError(n, nil)
	}
}

// Write (gc 1.27.1 internal/poll/fd_unix.go, socket half): accumulates
// bytes across EAGAIN waits. Trap contract: r-1 is the byte count;
// a 0-byte write (r == 1) is the JVM's EAGAIN (send buffer full):
// wait on the poller and retry — it is NOT io.ErrUnexpectedEOF,
// which gc only returns for a 0-byte write with no error on a
// blocking fd, unreachable here.
func (fd *FD) Write(p []byte) (int, error) {
	if err := fd.writeLock(); err != nil {
		return 0, err
	}
	defer fd.writeUnlock()
	if err := fd.pd.prepareWrite(false); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	nn := 0
	for {
		max := len(p) - nn
		if fd.IsStream && max > maxRW {
			max = maxRW
		}
		r := pollSockWrite(int64(fd.Sysfd), p[nn:nn+max])
		if r < 0 {
			err := errnoErr(syscall.Errno(-r))
			if err == syscall.EAGAIN && fd.pd.pollable() {
				if err := fd.pd.waitWrite(false); err == nil {
					continue
				}
				return nn, err
			}
			return nn, err
		}
		n := int(r - 1)
		if n == 0 {
			// JVM 0-byte write = kernel EAGAIN: wait and retry.
			if err := fd.pd.waitWrite(false); err != nil {
				return nn, err
			}
			continue
		}
		nn += n
		if nn == len(p) {
			return nn, nil
		}
	}
}

// Accept (gc 1.27.1 internal/poll/fd_unix.go accept + fd_posix.go
// accept): one pollAccept per loop iteration; EAGAIN waits on the
// poller, EINTR/ECONNABORTED retry. The accepted cookie's peer
// address comes from pollGetpeername (GoNet.renderAddr wire format).
func (fd *FD) Accept() (int, syscall.Sockaddr, string, error) {
	if err := fd.readLock(); err != nil {
		return -1, nil, "", err
	}
	defer fd.readUnlock()
	if err := fd.pd.prepareRead(false); err != nil {
		return -1, nil, "", err
	}
	for {
		nc := pollAccept(int64(fd.Sysfd))
		if nc < 0 {
			errnoVal := syscall.Errno(-nc)
			if errnoVal == syscall.EINTR || errnoVal == syscall.ECONNABORTED {
				continue
			}
			if errnoVal == syscall.EAGAIN && fd.pd.pollable() {
				if err := fd.pd.waitRead(false); err != nil {
					return -1, nil, "accept4", err
				}
				continue
			}
			return -1, nil, "accept4", errnoVal
		}
		sa := fd.peerSockaddr(nc)
		return int(nc), sa, "", nil
	}
}

// peerSockaddr decodes the GoNet.renderAddr wire format
// ([family, port-hi, port-lo, addr...]) into a syscall.Sockaddr.
// A short/failed getpeername yields nil (gc: accept returns a nil
// Sockaddr when the peer address is unavailable, not an error).
func (fd *FD) peerSockaddr(cookie int64) syscall.Sockaddr {
	buf := make([]byte, 23)
	r := pollGetpeername(cookie, buf)
	if r <= 0 {
		return nil
	}
	n := int(r - 1)
	if n < 7 {
		return nil
	}
	port := int(buf[1])<<8 | int(buf[2])
	switch buf[0] {
	case 2:
		sa := &syscall.SockaddrInet4{Port: port}
		copy(sa.Addr[:], buf[3:7])
		return sa
	case 10:
		if n < 19 {
			return nil
		}
		sa := &syscall.SockaddrInet6{Port: port}
		copy(sa.Addr[:], buf[3:19])
		return sa
	}
	return nil
}

// Dup (gc 1.27.1 internal/poll/fd_unix.go): duplicates the socket
// cookie with close-on-exec semantics (DupCloseOnExec).
func (fd *FD) Dup() (int, string, error) {
	if err := fd.incref(); err != nil {
		return -1, "", err
	}
	defer fd.decref()
	return DupCloseOnExec(fd.Sysfd)
}

// Shutdown (gc 1.27.1 internal/poll/fd_unix.go): shuts down the
// socket in one direction (SHUT_RD/SHUT_WR/SHUT_RDWR).
func (fd *FD) Shutdown(how int) error {
	if err := fd.incref(); err != nil {
		return err
	}
	defer fd.decref()
	r := pollShutdown(int64(fd.Sysfd), int64(how))
	if r < 0 {
		return errnoErr(syscall.Errno(-r))
	}
	return nil
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

// DupCloseOnExec (gc 1.27.1, src/internal/poll/fd_unix.go:658):
// duplicates fd with close-on-exec flag set. The flag is vacuous
// here: the guest has no exec path (os.StartProcess panics, "not in
// M8 scope"; the only ProcessBuilder uses are host-side launcher
// tooling), so no fd can ever leak across an exec. The dup itself
// is real: graalgo.os.dup aliases the channel and refcounts it,
// so the original and the dup share the socket until both close.
func DupCloseOnExec(fd int) (int, string, error) {
	nc := pollDup(int64(fd))
	if nc < 0 {
		return -1, "dup", errnoErr(syscall.Errno(-nc))
	}
	return int(nc), "", nil
}
`
