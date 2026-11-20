package main

// L11 (REVIEW-2026-11-18-05): net/http/httptrace — zero corpus
// imports before this program. Tracing an HTTP client request:
// which hooks fire (booleans only — no timings), and the trace
// context round-trip through the request.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "trace-body")
	}))
	defer srv.Close()
	var gotConn, wroteReq, gotFirstByte, wroteHeaders, dnsDone bool
	trace := &httptrace.ClientTrace{
		GetConn:              func(hostPort string) { gotConn = true },
		GotConn:              func(info httptrace.GotConnInfo) { gotConn = true },
		WroteRequest:         func(info httptrace.WroteRequestInfo) { wroteReq = true },
		GotFirstResponseByte: func() { gotFirstByte = true },
		WroteHeaders:         func() { wroteHeaders = true },
		DNSDone:              func(info httptrace.DNSDoneInfo) { dnsDone = true },
	}
	req, _ := http.NewRequest("GET", srv.URL+"/x", nil)
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("do-err=%v\n", err != nil)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("status=%d body=%q\n", resp.StatusCode, string(body))
	fmt.Printf("hooks gotconn=%v wroteReq=%v firstByte=%v wroteHeaders=%v dns=%v\n",
		gotConn, wroteReq, gotFirstByte, wroteHeaders, dnsDone)
	// A second request through the same trace fires the hooks again.
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("do2-err=%v\n", err != nil)
		return
	}
	io.ReadAll(resp2.Body)
	resp2.Body.Close()
	fmt.Printf("second status=%d hooks-still-set=%v\n", resp2.StatusCode, gotConn && wroteReq && gotFirstByte)
}
