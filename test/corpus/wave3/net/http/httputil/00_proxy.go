package main

// L11 (REVIEW-2026-11-18-05): net/http/httputil — zero corpus imports
// before this program. ReverseProxy + DumpRequest/DumpResponse against an
// httptest server on local loopback. The server port and the Date header
// vary run to run, so only deterministic facts are printed: status,
// body, header values, and Contains checks on the dumps (never the raw
// dump bytes).

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
)

func main() {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Fixed", "yes")
		fmt.Fprint(w, "hello-backend\n")
	}))
	defer backend.Close()

	// ReverseProxy to the test server.
	target, err := url.Parse(backend.URL)
	if err != nil {
		fmt.Printf("url-err=%v\n", err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxied?q=1", nil)
	proxy.ServeHTTP(rec, req)
	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("proxy-status=%d\n", resp.StatusCode)
	fmt.Printf("proxy-body=%q\n", string(body))
	fmt.Printf("proxy-header=%q\n", resp.Header.Get("X-Fixed"))

	// DumpRequest on a request with a fixed Host (no random port).
	dreq, err := http.NewRequest("GET", "http://example.com/path?q=1", nil)
	if err != nil {
		fmt.Printf("req-err=%v\n", err)
		return
	}
	dreq.Header.Set("X-Test", "dump")
	dump, err := httputil.DumpRequest(dreq, false)
	if err != nil {
		fmt.Printf("dump-req-err=%v\n", err)
		return
	}
	fmt.Printf("dump-req-has-line=%v\n",
		strings.Contains(string(dump), "GET /path?q=1 HTTP/1.1"))
	fmt.Printf("dump-req-has-host=%v\n",
		strings.Contains(string(dump), "Host: example.com"))
	fmt.Printf("dump-req-has-header=%v\n",
		strings.Contains(string(dump), "X-Test: dump"))

	// DumpResponse, with the varying Date header filtered out.
	cresp, err := http.Get(backend.URL + "/direct")
	if err != nil {
		fmt.Printf("get-err=%v\n", err)
		return
	}
	defer cresp.Body.Close()
	rdump, err := httputil.DumpResponse(cresp, true)
	if err != nil {
		fmt.Printf("dump-resp-err=%v\n", err)
		return
	}
	var kept []string
	for _, line := range strings.Split(string(rdump), "\r\n") {
		if strings.HasPrefix(line, "Date:") {
			continue
		}
		kept = append(kept, line)
	}
	fmt.Printf("dump-resp=%q\n", strings.Join(kept, "|"))
}
