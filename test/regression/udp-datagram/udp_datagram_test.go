// Regression tests for R23-P1-8+9: UDP fork datagram paths in the
// synthetic net package.
//
// The three functions under test below are VERBATIM copies of
// __udpConnRead, __udpConnReadFrom and __udpConnWriteTo from
// internal/frontend/synth_net.go (as fixed for R23-P1-8+9). They cannot be
// imported directly: the real ones call linknamed trap intrinsics
// (graalgo.net.recvfrom / graalgo.net.sendto / netpoller waits) that only
// exist inside the graalgo backend. Here the traps are scripted fakes that
// implement the documented trap contract (see
// internal/backend/interop/src/main/java/graalgo/p3net/GoNet.java):
//
//   - recvfrom: -EAGAIN when no datagram is queued; n+1 when a datagram
//     (possibly zero-length, n == 0) is consumed.
//   - sendto:   n+1 for n bytes sent; 0 (returned as r == 1) on a
//     nonblocking would-block with want > 0.
//
// The tests assert gc/Linux observable behavior at that contract boundary:
// a consumed zero-length datagram is delivered immediately as (0, nil),
// a would-blocked datagram write waits via the netpoller and completes
// with the full length (never a silent zero-byte "success"), and the
// EAGAIN path still waits and retries.
//
// If internal/frontend/synth_net.go changes these functions, update the
// copies below in lockstep (they are delimited by BEGIN/END markers).
package udpdatagram

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Minimal scaffolding mirroring the synthetic net types.
// ---------------------------------------------------------------------------

type errno int64

func (e errno) Error() string { return "errno" }

const errnoEAGAIN = 11

type Addr interface {
	Network() string
	String() string
}

type IP []byte

type UDPAddr struct {
	IP   IP
	Port int
	Zone string
}

func (a *UDPAddr) Network() string { return "udp" }
func (a *UDPAddr) String() string  { return "udp-addr" }

type OpError struct {
	Op   string
	Net  string
	Addr Addr
	Err  error
}

func (e *OpError) Error() string { return "operror: " + e.Op }

type netFD struct {
	cookie int64
	net    string
}

func (fd *netFD) mapPollErr(op string, err error) error {
	return &OpError{Op: op, Net: fd.net, Err: err}
}

type UDPConn struct {
	fd *netFD
}

type readPair struct {
	n   int
	err error
}

type readFromPair struct {
	n    int
	addr Addr
	err  error
}

type writePair struct {
	n   int
	err error
}

// Minimal sockaddr wire codec (family byte 0; AF_INET: [2, portHi,
// portLo, a0..a3]). The fake sendto ignores its input; the fake recvfrom
// writes this format so decodeSockaddr exercises the real path.
type decodeSockaddrPair struct {
	ip   IP
	port int
}

func encodeSockaddr(ip IP, port int) []byte {
	return []byte{2, byte(port >> 8), byte(port), 127, 0, 0, 1}
}

func decodeSockaddr(b []byte) decodeSockaddrPair {
	if len(b) < 7 || b[0] != 2 {
		return decodeSockaddrPair{nil, 0}
	}
	return decodeSockaddrPair{IP{b[3], b[4], b[5], b[6]}, int(b[1])<<8 | int(b[2])}
}

// ---------------------------------------------------------------------------
// Scripted trap fakes. Tests set the scripts before calling the functions.
// ---------------------------------------------------------------------------

var (
	recvfromScript     []int64 // values returned in order; last repeats
	recvfromCalls      int
	pollWaitReadCalls  int
	sendtoScript       []int64
	sendtoCalls        int
	pollWaitWriteCalls int
	pollWaitErr        error // returned by the poll waits (nil = writable/readable)
)

func resetFakes() {
	recvfromScript = nil
	recvfromCalls = 0
	pollWaitReadCalls = 0
	sendtoScript = nil
	sendtoCalls = 0
	pollWaitWriteCalls = 0
	pollWaitErr = nil
}

func scriptNext(script []int64, calls int) int64 {
	if len(script) == 0 {
		return -errnoEAGAIN
	}
	if calls < len(script) {
		return script[calls]
	}
	return script[len(script)-1]
}

// netRecvfrom fake: consumes the next scripted value; on a datagram
// delivery (r >= 0) writes a loopback peer address into addrBuf.
func netRecvfrom(cookie int64, b []byte, addr []byte) int64 {
	r := scriptNext(recvfromScript, recvfromCalls)
	recvfromCalls++
	if r >= 0 && addr != nil {
		copy(addr, []byte{2, 0x13, 0x88, 127, 0, 0, 1}) // 127.0.0.1:5000
	}
	return r
}

func netSendto(cookie int64, buf []byte, addr []byte) int64 {
	r := scriptNext(sendtoScript, sendtoCalls)
	sendtoCalls++
	return r
}

func netPollWaitRead(cookie int64) error {
	pollWaitReadCalls++
	return pollWaitErr
}

func netPollWaitWrite(cookie int64) error {
	pollWaitWriteCalls++
	return pollWaitErr
}

// ---------------------------------------------------------------------------
// BEGIN VERBATIM COPIES (internal/frontend/synth_net.go, R23-P1-8+9 fixed).
// ---------------------------------------------------------------------------

func __udpConnRead(c *UDPConn, b []byte) readPair {
	// Connected UDP read: recvfrom with nil addr.
	for {
		r := netRecvfrom(int64(c.fd.cookie), b, nil)
		if r < 0 {
			errnoVal := errno(-r)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitRead(c.fd.cookie); werr != nil {
					return readPair{0, c.fd.mapPollErr("read", werr)}
				}
				continue
			}
			return readPair{0, &OpError{Op: "read", Net: c.fd.net, Err: errnoVal}}
		}
		n := int(r - 1)
		// r-1 == 0 means a zero-length datagram was consumed (no datagram
		// pending is reported as -EAGAIN above): return it immediately.
		return readPair{n, nil}
	}
}

func __udpConnReadFrom(c *UDPConn, b []byte) readFromPair {
	addrBuf := make([]byte, 32)
	for {
		r := netRecvfrom(int64(c.fd.cookie), b, addrBuf)
		if r < 0 {
			errnoVal := errno(-r)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitRead(c.fd.cookie); werr != nil {
					return readFromPair{0, nil, c.fd.mapPollErr("read", werr)}
				}
				continue
			}
			return readFromPair{0, nil, &OpError{Op: "read", Net: c.fd.net, Err: errnoVal}}
		}
		// The addr buffer is separate; r-1 is the datagram length.
		// Re-query the peer address length via a second call is not
		// possible; decode from addrBuf directly. The trap writes the
		// wire format; decode the family to know the length.
		//
		// n == 0 means a zero-length datagram was consumed (no datagram
		// pending is reported as -EAGAIN above): return it immediately.
		n := int(r - 1)
		var raddr Addr
		if len(addrBuf) >= 7 {
			__tmpSA2 := decodeSockaddr(addrBuf)
			raddr = &UDPAddr{IP: __tmpSA2.ip, Port: __tmpSA2.port}
		}
		return readFromPair{n, raddr, nil}
	}
}

func __udpConnWriteTo(c *UDPConn, b []byte, addr Addr) writePair {
	uaddr, ok := addr.(*UDPAddr)
	if !ok {
		return writePair{0, &OpError{Op: "write", Net: c.fd.net, Err: errors.New("net: WriteTo with non-UDPAddr")}}
	}
	sa := encodeSockaddr(uaddr.IP, uaddr.Port)
	for {
		r := netSendto(int64(c.fd.cookie), b, sa)
		if r < 0 {
			return writePair{0, &OpError{Op: "write", Net: c.fd.net, Addr: addr, Err: errno(-r)}}
		}
		n := int(r - 1)
		if n == 0 && len(b) > 0 {
			// 0-byte send on a nonblocking socket with want > 0: EAGAIN
			// (fork-side contract, mirroring netFD.write). Wait for
			// writability via the netpoller and retry; do not report a
			// successful zero-byte write. len(b)==0 datagrams still
			// complete immediately as genuine zero-byte writes.
			if werr := netPollWaitWrite(c.fd.cookie); werr != nil {
				return writePair{0, c.fd.mapPollErr("write", werr)}
			}
			continue
		}
		return writePair{n, nil}
	}
}

// ---------------------------------------------------------------------------
// END VERBATIM COPIES.
// ---------------------------------------------------------------------------

func testConn() *UDPConn {
	return &UDPConn{fd: &netFD{cookie: 7, net: "udp"}}
}

func testUDPAddr() *UDPAddr {
	return &UDPAddr{IP: IP{127, 0, 0, 1}, Port: 5000}
}

// R23-P1-8+9(b): a consumed zero-length datagram must be delivered
// immediately as (0, nil): no netpoller wait, no swallowing it while
// waiting for the NEXT datagram. Pre-fix this waited and consumed the
// scripted 5-byte datagram instead, returning (5, nil).
func TestUDPConnReadZeroLengthDatagram(t *testing.T) {
	resetFakes()
	recvfromScript = []int64{1, 6} // zero-length datagram, then 5-byte datagram
	got := __udpConnRead(testConn(), make([]byte, 100))
	if got.err != nil {
		t.Fatalf("Read: err = %v, want nil", got.err)
	}
	if got.n != 0 {
		t.Fatalf("Read: n = %d, want 0 (zero-length datagram)", got.n)
	}
	if pollWaitReadCalls != 0 {
		t.Fatalf("Read: netPollWaitRead called %d times, want 0 (must not wait on a consumed datagram)", pollWaitReadCalls)
	}
	if recvfromCalls != 1 {
		t.Fatalf("Read: recvfrom called %d times, want 1 (must not consume the next datagram)", recvfromCalls)
	}
}

// R23-P1-8+9(b), ReadFrom variant: same guarantee, plus the peer address
// of the zero-length datagram is decoded and returned.
func TestUDPConnReadFromZeroLengthDatagram(t *testing.T) {
	resetFakes()
	recvfromScript = []int64{1, 6}
	got := __udpConnReadFrom(testConn(), make([]byte, 100))
	if got.err != nil {
		t.Fatalf("ReadFrom: err = %v, want nil", got.err)
	}
	if got.n != 0 {
		t.Fatalf("ReadFrom: n = %d, want 0 (zero-length datagram)", got.n)
	}
	if got.addr == nil {
		t.Fatalf("ReadFrom: addr = nil, want the datagram's peer address")
	}
	if pollWaitReadCalls != 0 {
		t.Fatalf("ReadFrom: netPollWaitRead called %d times, want 0", pollWaitReadCalls)
	}
	if recvfromCalls != 1 {
		t.Fatalf("ReadFrom: recvfrom called %d times, want 1", recvfromCalls)
	}
}

// R23-P1-8+9(b), no-regression: the EAGAIN path still waits via the
// netpoller and retries until a datagram arrives.
func TestUDPConnReadEagainWaitsAndRetries(t *testing.T) {
	resetFakes()
	recvfromScript = []int64{-errnoEAGAIN, -errnoEAGAIN, 6}
	got := __udpConnRead(testConn(), make([]byte, 100))
	if got.err != nil {
		t.Fatalf("Read: err = %v, want nil", got.err)
	}
	if got.n != 5 {
		t.Fatalf("Read: n = %d, want 5", got.n)
	}
	if pollWaitReadCalls != 2 {
		t.Fatalf("Read: netPollWaitRead called %d times, want 2 (one per EAGAIN)", pollWaitReadCalls)
	}
	if recvfromCalls != 3 {
		t.Fatalf("Read: recvfrom called %d times, want 3", recvfromCalls)
	}
}

// R23-P1-8+9(a): a would-blocked datagram write (trap returns r == 1,
// i.e. n == 0, with want > 0) must block via the netpoller until
// writable and then complete with the full length — never a silent
// zero-byte "success". Pre-fix this returned (0, nil) immediately.
func TestUDPConnWriteToWouldBlockWaitsAndRetries(t *testing.T) {
	resetFakes()
	payload := []byte("hello udp")
	sendtoScript = []int64{1, int64(len(payload) + 1)} // would-block, then full write
	got := __udpConnWriteTo(testConn(), payload, testUDPAddr())
	if got.err != nil {
		t.Fatalf("WriteTo: err = %v, want nil", got.err)
	}
	if got.n != len(payload) {
		t.Fatalf("WriteTo: n = %d, want %d (would-block must not surface as a zero-byte write)", got.n, len(payload))
	}
	if pollWaitWriteCalls != 1 {
		t.Fatalf("WriteTo: netPollWaitWrite called %d times, want 1", pollWaitWriteCalls)
	}
	if sendtoCalls != 2 {
		t.Fatalf("WriteTo: sendto called %d times, want 2 (retry after wait)", sendtoCalls)
	}
}

// R23-P1-8+9(a), guard: a genuine empty datagram (len(b) == 0) still
// completes immediately as a zero-byte write — no netpoller wait.
func TestUDPConnWriteToEmptyDatagramCompletes(t *testing.T) {
	resetFakes()
	sendtoScript = []int64{1} // n == 0 is the genuine empty-datagram result
	got := __udpConnWriteTo(testConn(), []byte{}, testUDPAddr())
	if got.err != nil {
		t.Fatalf("WriteTo: err = %v, want nil", got.err)
	}
	if got.n != 0 {
		t.Fatalf("WriteTo: n = %d, want 0", got.n)
	}
	if pollWaitWriteCalls != 0 {
		t.Fatalf("WriteTo: netPollWaitWrite called %d times, want 0 (empty datagrams never wait)", pollWaitWriteCalls)
	}
}
