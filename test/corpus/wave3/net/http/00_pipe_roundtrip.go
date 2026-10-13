// P3-M9 wave3: net/http pipe leg — full client+server over net.Pipe,
// no sockets. Pins: GET/POST, status codes, method routing, header
// propagation, body round-trip 0/1/64KB.
// SCOPE-LIMITED: no synthetic net/http source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
)

var errNoMoreConns = errors.New("oneShotListener: no more connections")

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

// roundTrip runs one client<->server exchange over a fresh pipe pair.
func roundTrip(handler http.Handler, do func(*http.Client) error) error {
	c, s := net.Pipe()
	l := newOneShotListener(s)
	srv := &http.Server{Handler: handler, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan struct{})
	go func() { srv.Serve(l); close(done) }()
	defer func() { l.Close(); <-done }()

	cl := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c, nil
		},
	}}
	err := do(cl)
	c.Close()
	return err
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-UA", r.UserAgent())
		w.Header().Set("X-Custom-In", r.Header.Get("X-Custom"))
		fmt.Fprint(w, "hello-body")
	})
	mux.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Got-Bytes", fmt.Sprint(len(b)))
		w.Write(b)
	})
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	// GET with custom headers; default User-Agent propagation.
	err := roundTrip(mux, func(cl *http.Client) error {
		req, _ := http.NewRequest("GET", "http://x/hello", nil)
		req.Header.Set("X-Custom", "cval")
		resp, err := cl.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println("get-status", resp.StatusCode == 200)
		fmt.Println("get-body", string(b) == "hello-body")
		fmt.Println("get-ua", resp.Header.Get("X-Echo-UA") == "Go-http-client/1.1")
		fmt.Println("get-custom", resp.Header.Get("X-Custom-In") == "cval")
		return nil
	})
	fmt.Println("get-err", err == nil)

	// POST echo with 64KB body.
	big := bytes.Repeat([]byte("abcdef"), 10923) // 65538 bytes
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Post("http://x/echo", "text/plain", bytes.NewReader(big))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println("post-status", resp.StatusCode == 200)
		fmt.Println("post-len", resp.Header.Get("X-Got-Bytes") == "65538")
		fmt.Println("post-body", bytes.Equal(b, big))
		return nil
	})
	fmt.Println("post-err", err == nil)

	// POST with empty body (0-byte round-trip).
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Post("http://x/echo", "text/plain", bytes.NewReader(nil))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println("post0-status", resp.StatusCode == 200)
		fmt.Println("post0-body", len(b) == 0)
		return nil
	})
	fmt.Println("post0-err", err == nil)

	// POST with 1-byte body.
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Post("http://x/echo", "text/plain", bytes.NewReader([]byte("Z")))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println("post1-body", string(b) == "Z")
		return nil
	})
	fmt.Println("post1-err", err == nil)

	// 404 for unregistered path; 500 from handler.
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Get("http://x/missing")
		if err != nil {
			return err
		}
		resp.Body.Close()
		fmt.Println("status-404", resp.StatusCode == 404)
		resp2, err := cl.Get("http://x/fail")
		if err != nil {
			return err
		}
		defer resp2.Body.Close()
		b, _ := io.ReadAll(resp2.Body)
		fmt.Println("status-500", resp2.StatusCode == 500, string(b) == "boom\n")
		return nil
	})
	fmt.Println("status-err", err == nil)

	// Method routing: POST to a GET-only path -> 405.
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Post("http://x/hello", "text/plain", nil)
		if err != nil {
			return err
		}
		resp.Body.Close()
		fmt.Println("method-405", resp.StatusCode == 405)
		return nil
	})
	fmt.Println("method-err", err == nil)

	fmt.Println("ok")
}
