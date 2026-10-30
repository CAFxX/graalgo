# Fork contract: `internal/poll` + `syscall` socket surface (for the M9 package worker)

Implemented in `graalgo.p3net` (branch `m9`). This is the exact Go-side
surface the adapted fork must declare. All symbols are registered in
`LinknameIntrinsics.registerNetIntrinsics`.

## internal/poll linkname declarations

The fork keeps `fd_poll_runtime.go` logic unmodified but replaces the
declaration block with single-result `//go:linkname` declarations (the
v1 frontend rejects multi-result declarations, even for linknames):

```go
//go:linkname runtime_pollServerInit internal/poll.runtime_pollServerInit
func runtime_pollServerInit()

//go:linkname runtime_pollOpen internal/poll.runtime_pollOpen
func runtime_pollOpen(fd uintptr) uintptr // pollDesc cookie; 0 = failure

//go:linkname runtime_pollOpenErrno internal/poll.runtime_pollOpenErrno
func runtime_pollOpenErrno() int // errno of the last failed pollOpen (this goroutine)

//go:linkname runtime_pollClose internal/poll.runtime_pollClose
func runtime_pollClose(ctx uintptr)

//go:linkname runtime_pollWait internal/poll.runtime_pollWait
func runtime_pollWait(ctx uintptr, mode int) int // 0/1/2/3, see below

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
```

Result codes for `runtime_pollWait`/`runtime_pollReset` (match
`fd_poll_runtime.go`): 0 = no error, 1 = closing, 2 = timeout,
3 = not pollable.

`pollDesc.init` adaptation:

```go
func (pd *pollDesc) init(fd *FD) error {
	serverInit.Do(runtime_pollServerInit)
	ctx := runtime_pollOpen(uintptr(fd.Sysfd))
	if ctx == 0 {
		return errnoErr(syscall.Errno(runtime_pollOpenErrno()))
	}
	pd.runtimeCtx = ctx
	return nil
}
```

`runtime_pollSetDeadline` semantics: `d` is relative nanoseconds
(`fd_poll_runtime.go` already computes `d = int64(time.Until(t))`);
d > 0 arms, d == 0 disarms, d < 0 expires immediately. Mode is
`'r'`, `'w'`, or `'r'+'w'` (233).

## syscall socket traps (graalgo.net.*)

```go
//go:linkname socketTrap graalgo.net.socket
func socketTrap(domain, typ, proto int64) int64 // cookie or -errno

//go:linkname bindTrap graalgo.net.bind
func bindTrap(cookie int64, addr []byte) int64 // 0 or -errno

//go:linkname listenTrap graalgo.net.listen
func listenTrap(cookie int64, backlog int64) int64 // 0 or -errno

//go:linkname acceptTrap graalgo.net.accept
func acceptTrap(cookie int64) int64 // new cookie or -errno (incl. -EAGAIN)

//go:linkname connectTrap graalgo.net.connect
func connectTrap(cookie int64, addr []byte) int64 // 0, -EINPROGRESS, or -errno

//go:linkname setsockoptTrap graalgo.net.setsockopt
func setsockoptTrap(cookie, level, opt, value int64) int64

//go:linkname getsockoptTrap graalgo.net.getsockopt
func getsockoptTrap(cookie, level, opt int64) int64 // value or -errno

//go:linkname getsocknameTrap graalgo.net.getsockname
func getsocknameTrap(cookie int64, buf []byte) int64 // n+1 or -errno

//go:linkname getpeernameTrap graalgo.net.getpeername
func getpeernameTrap(cookie int64, buf []byte) int64 // n+1 or -errno

//go:linkname shutdownTrap graalgo.net.shutdown
func shutdownTrap(cookie, how int64) int64

//go:linkname setnonblockTrap graalgo.net.setnonblock
func setnonblockTrap(cookie, nonblocking int64) int64

//go:linkname sendtoTrap graalgo.net.sendto
func sendtoTrap(cookie int64, buf []byte, addr []byte) int64 // n+1 or -errno

//go:linkname recvfromTrap graalgo.net.recvfrom
func recvfromTrap(cookie int64, buf []byte, addr []byte) int64 // n+1 or -errno
```

Conventions:
- All `int64` returns: negative = `-errno` (Linux amd64 numbers),
  non-negative = value. `sendto`/`recvfrom`/`getsockname`/`getpeername`
  return `n+1` (a zero-length datagram returns 1).
- Stream read/write go through the existing `graalgo.os.read`/`write`
  on the same cookie (fd-domain unification). `graalgo.os.read` returns
  `n+1` (0 = EOF): a 0-byte read on a nonblocking socket returns 1 —
  the fork maps that to EAGAIN when `len(buf) > 0`.
- Socket close goes through `graalgo.os.close` (unified); it evicts
  netpoller state automatically.
- Sockaddr wire format (`[]byte`): byte 0 = family; AF_INET (2):
  `[2, portHi, portLo, a0, a1, a2, a3]`; AF_INET6 (10):
  `[10, portHi, portLo, a0..a15, scopeHi..scopeLo]` (23 bytes).
  `recvfrom` writes the peer address in this format into its third arg
  (must have room for 23 bytes).
- `syscall.Socket` returns the cookie (not a kernel fd). `SetNonblock`
  is a recorded no-op (channels are always nonblocking).
- After `connect` returns `-EINPROGRESS`, completion is driven by
  `poll.Wait`('w') as usual; failure surfaces via
  `getsockopt(SO_ERROR)` (gc parity), not via the wait result.
- `sendmsg`/`recvmsg`/`sendfile` are NOT provided (fork fail-fasts them;
  basic UDP works via sendto/recvfrom + connected write).
- AF_UNIX → `-EAFNOSUPPORT` (M9 scope: loopback TCP/UDP).
- Sandbox note: this VM blocks unconnected sendto(2) with EPERM;
  connected datagram write works. `WriteToUDP` needs a real host.

## Errno numbers used (Linux amd64)

EAGAIN=11, ENOTCONN=107, EISCONN=106, EALREADY=114, EINPROGRESS=115,
EADDRINUSE=98, ECONNREFUSED=111, EHOSTUNREACH=113, ENOTSOCK=88,
EAFNOSUPPORT=97, ENOPROTOOPT=92, EOPNOTSUPP=95, EMSGSIZE=90,
EDESTADDRREQ=89, EPERM=1. Plus GoOs's file set (EBADF=9, EINVAL=22).
