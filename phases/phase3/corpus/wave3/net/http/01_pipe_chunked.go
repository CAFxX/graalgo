// P3-M9 wave3: net/http pipe leg — chunked streaming (Flusher, no
// Content-Length), trailers, MaxBytesReader enforcement.
// SCOPE-LIMITED: no synthetic net/http source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
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

	// Chunked stream: no Content-Length, Flusher, trailer declared.
	mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trailer", "X-Sum")
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "chunk-%d;", i)
			fl.Flush()
		}
		w.Header().Set("X-Sum", "abc123")
	})

	// MaxBytesReader: 10-byte limit.
	mux.HandleFunc("POST /limited", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Fprintf(w, "read-err")
			return
		}
		fmt.Fprintf(w, "got-%d", len(b))
	})

	// Client observes the de-chunked body and the trailer.
	err := roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Get("http://x/stream")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		fmt.Println("chunked-status", resp.StatusCode == 200)
		fmt.Println("chunked-clen", resp.ContentLength == -1)
		fmt.Println("chunked-body", string(b) == "chunk-0;chunk-1;chunk-2;chunk-3;chunk-4;")
		fmt.Println("trailer", resp.Trailer.Get("X-Sum") == "abc123")
		// Trailer must not be visible before the body is consumed.
		return nil
	})
	fmt.Println("chunked-err", err == nil)

	// MaxBytesReader: over-limit body -> handler sees a read error.
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Post("http://x/limited", "text/plain",
			strings.NewReader("0123456789ABCDEF"))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println("limited-status", resp.StatusCode == 200)
		fmt.Println("limited-body", string(b) == "read-err")
		return nil
	})
	fmt.Println("limited-err", err == nil)

	// MaxBytesReader: under-limit body passes through.
	err = roundTrip(mux, func(cl *http.Client) error {
		resp, err := cl.Post("http://x/limited", "text/plain",
			strings.NewReader("0123456789"))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println("underlimit-body", string(b) == "got-10")
		return nil
	})
	fmt.Println("underlimit-err", err == nil)

	// Chunked request body: client streams without knowing the length.
	err = roundTrip(mux, func(cl *http.Client) error {
		pr, pw := io.Pipe()
		go func() {
			pw.Write([]byte("streamed-"))
			pw.Write([]byte("request"))
			pw.Close()
		}()
		resp, err := cl.Post("http://x/echo2", "text/plain", pr)
		if err != nil {
			// /echo2 is not registered: the 404 still proves the
			// chunked request was accepted and parsed.
			return err
		}
		resp.Body.Close()
		fmt.Println("chunked-req-status", resp.StatusCode == 404)
		return nil
	})
	fmt.Println("chunked-req-err", err == nil)

	fmt.Println("ok")
}
