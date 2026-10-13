// P3-M9 wave3: net/http pipe leg — adversarial inputs: malformed request
// bytes over the pipe (server must emit 400), truncated headers,
// bad Content-Length, oversized header line vs MaxHeaderBytes,
// http.ReadRequest on truncated input.
// SCOPE-LIMITED: no synthetic net/http source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var errNoMoreConns = errorString("oneShotListener: no more connections")

type errorString string

func (e errorString) Error() string { return string(e) }

// oneShotListener serves a single pre-connected net.Pipe end. Serve
// calls Accept in a loop, so the second Accept blocks until Close
// (Serve always closes the listener on return, which would otherwise
// tear down the in-flight connection).
type oneShotListener struct {
	c    net.Conn
	used bool
	done chan struct{}
	mu   sync.Mutex
}

func newOneShotListener(c net.Conn) *oneShotListener {
	return &oneShotListener{c: c, done: make(chan struct{})}
}

func (l *oneShotListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.used {
		l.used = true
		c := l.c
		l.mu.Unlock()
		return c, nil
	}
	done := l.done
	l.mu.Unlock()
	<-done
	return nil, errNoMoreConns
}
func (l *oneShotListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return l.c.Close()
}
func (l *oneShotListener) Addr() net.Addr { return l.c.LocalAddr() }

// rawExchange writes raw request bytes to the server end of a pipe and
// returns the response's first status line (or an outcome class).
func rawExchange(srv *http.Server, req string) string {
	c, s := net.Pipe()
	l := newOneShotListener(s)
	done := make(chan struct{})
	go func() { srv.Serve(l); close(done) }()
	defer func() { l.Close(); <-done }()

	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte(req))
	if !strings.HasSuffix(req, "\r\n\r\n") {
		// Client aborts mid-headers: server sees EOF and closes;
		// no response is ever written.
		c.Close()
		l.Close()
		<-done
		return "read-err"
	}
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	c.Close()
	if err != nil {
		return "read-err"
	}
	return strings.TrimRight(line, "\r\n")
}

// rawExchangeAsync is rawExchange with the request pumped from a
// background goroutine while the main goroutine reads the response.
// This is the only deterministic way to observe the server's over-limit
// (431) path over a pipe: a synchronous writer deadlocks against the
// server (documented "undefined behavior" in net/http/server.go — the
// server stops reading to emit 431 while the client is still writing).
func rawExchangeAsync(srv *http.Server, req string) string {
	c, s := net.Pipe()
	l := newOneShotListener(s)
	done := make(chan struct{})
	go func() { srv.Serve(l); close(done) }()
	defer func() { l.Close(); <-done }()

	c.SetDeadline(time.Now().Add(10 * time.Second))
	go func() { c.Write([]byte(req)) }() // write errors are ignored
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	c.Close()
	if err != nil {
		return "read-err"
	}
	return strings.TrimRight(line, "\r\n")
}

func main() {
	newSrv := func() *http.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "ok")
		})
		return &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	}

	// Garbage request line -> 400 Bad Request.
	fmt.Println("garbage", rawExchange(newSrv(), "GARBAGE\r\n\r\n") == "HTTP/1.1 400 Bad Request")

	// Non-numeric Content-Length -> 400.
	fmt.Println("bad-clen", rawExchange(newSrv(),
		"POST / HTTP/1.1\r\nHost: x\r\nContent-Length: abc\r\n\r\n") == "HTTP/1.1 400 Bad Request")

	// Truncated headers (client closes mid-headers): server just closes.
	fmt.Println("truncated", rawExchange(newSrv(),
		"GET / HTTP/1.1\r\nHost: x\r\nX-Half: par") == "read-err")

	// Oversized header vs MaxHeaderBytes=1024: the server answers
	// 431 Request Header Fields Too Large (not 400). The wire limit is
	// MaxHeaderBytes+4096 (bufio slop); the 431 path is only observable
	// deterministically with an async writer (see rawExchangeAsync).
	big := "GET / HTTP/1.1\r\nHost: x\r\nX-Big: " + strings.Repeat("a", 20000) + "\r\n\r\n"
	srv := newSrv()
	srv.MaxHeaderBytes = 1024
	fmt.Println("maxheader", rawExchangeAsync(srv, big) == "HTTP/1.1 431 Request Header Fields Too Large")

	// The same request under the default limit is fine -> 200.
	fmt.Println("maxheader-ok", rawExchange(newSrv(), big) == "HTTP/1.1 200 OK")

	// A 4000-byte header line still fits under MaxHeaderBytes=1024
	// (wire limit is 1024+4096): pins the slop, not a strict byte count.
	ok4k := "GET / HTTP/1.1\r\nHost: x\r\nX-Big: " + strings.Repeat("a", 4000) + "\r\n\r\n"
	fmt.Println("maxheader-4k", rawExchange(srv, ok4k) == "HTTP/1.1 200 OK")

	// http.ReadRequest on truncated input: header parse succeeds, body
	// read hits unexpected EOF (not a panic).
	req, err := http.ReadRequest(bufio.NewReader(
		strings.NewReader("POST / HTTP/1.0\r\nContent-Length: 10\r\n\r\nabc")))
	if err != nil {
		fmt.Println("readreq-trunc-parse-err", err)
	} else {
		_, err = io.ReadAll(req.Body)
		fmt.Println("readreq-trunc-body", err == io.ErrUnexpectedEOF)
	}

	// http.ReadRequest on garbage -> error, not panic.
	_, err = http.ReadRequest(bufio.NewReader(strings.NewReader("GARBAGE\r\n\r\n")))
	fmt.Println("readreq-garbage", err != nil)

	// http.ReadRequest with bad Content-Length -> error.
	_, err = http.ReadRequest(bufio.NewReader(
		strings.NewReader("POST / HTTP/1.1\r\nHost: x\r\nContent-Length: abc\r\n\r\n")))
	fmt.Println("readreq-badclen", err != nil)

	// Absolute-form URI in request line is accepted.
	fmt.Println("absolute-uri", rawExchange(newSrv(),
		"GET http://x/ HTTP/1.1\r\nHost: x\r\n\r\n") == "HTTP/1.1 200 OK")

	// Unknown method with valid framing -> 501 or handler 200? (pins: server
	// routes unknown methods to the handler; no 400).
	fmt.Println("unknown-method", rawExchange(newSrv(),
		"FROBNICATE / HTTP/1.1\r\nHost: x\r\n\r\n") == "HTTP/1.1 200 OK")

	fmt.Println("ok")
}
