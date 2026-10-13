package main

// P3-M9 (tier-5: net/http): synthetic `net/http` package.
//
// Real-Go sources: curated from gc go1.27.1 ($GOROOT/src/net/http/*.go).
//
// DESIGN (per program-owner directive 2026-10-10): prefer delegating to
// Java's HTTP facilities where semantics match, rather than reimplementing
// in Go. Only Go code where Java delegation isn't semantically compatible.
//
// Delegated to Java (graalgo.http.client.do via java.net.http.HttpClient):
//   - Client.Do / Get / Post / Head (the HTTP client transaction)
//
// Implemented in Go (semantics don't match Java or need Go-specific API):
//   - Types: Header, Request, Response, etc. (Go API surface)
//   - ReadRequest / ReadResponse (parsing; needed for pipe leg + public API)
//   - Server (needs net.Pipe support; Java HttpServer is TCP-only)
//   - ServeMux, ResponseRecorder (Go-specific)
//   - DetectContentType, MaxBytesReader, Error, helpers
//
// Adaptations for the v1 frontend (no multi-result functions, WS D):
// same-package multi-result funcs use the __-form explicitly (pair structs);
// public API keeps exact gc signatures.
//
// Only frontend/v1-supported constructs (no generics, no reflect, no unsafe).
const syntheticNetHttpSource = `// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:linkname httpClientDoIntrinsic graalgo.http.client.do
// P3-M10: returns the response as a []byte wire encoding (int64 BE status,
// int64 BE headerCount, per header: int64 BE keyLen + key + int64 BE valLen
// + val, int64 BE bodyLen + body) — a concrete []byte needs no interface
// boxing on the linkname path, unlike []interface{}.
func httpClientDoIntrinsic(method string, url string, headers []string, body []byte) []byte

// __httpWireReader decodes the graalgo.http.client.do []byte wire format.
type __httpWireReader struct {
	b []byte
	o int
	ok bool
}

func (r *__httpWireReader) i64() int64 {
	if !r.ok || r.o+8 > len(r.b) {
		r.ok = false
		return 0
	}
	v := int64(r.b[r.o])<<56 | int64(r.b[r.o+1])<<48 | int64(r.b[r.o+2])<<40 | int64(r.b[r.o+3])<<32 |
		int64(r.b[r.o+4])<<24 | int64(r.b[r.o+5])<<16 | int64(r.b[r.o+6])<<8 | int64(r.b[r.o+7])
	r.o += 8
	return v
}

func (r *__httpWireReader) bytes() []byte {
	n := r.i64()
	if !r.ok || n < 0 || r.o+int(n) > len(r.b) {
		r.ok = false
		return nil
	}
	v := r.b[r.o : r.o+int(n)]
	r.o += int(n)
	return v
}

// ---------------------------------------------------------------------------
// Status codes
// ---------------------------------------------------------------------------

const (
	StatusContinue           = 100
	StatusSwitchingProtocols = 101
	StatusProcessing         = 102
	StatusEarlyHints         = 103

	StatusOK                   = 200
	StatusCreated              = 201
	StatusAccepted             = 202
	StatusNonAuthoritativeInfo = 203
	StatusNoContent            = 204
	StatusResetContent         = 205
	StatusPartialContent       = 206
	StatusMultiStatus          = 207
	StatusAlreadyReported      = 208
	StatusIMUsed               = 226

	StatusMultipleChoices  = 300
	StatusMovedPermanently = 301
	StatusFound            = 302
	StatusSeeOther         = 303
	StatusNotModified      = 304
	StatusUseProxy         = 305

	StatusTemporaryRedirect = 307
	StatusPermanentRedirect = 308

	StatusBadRequest                   = 400
	StatusUnauthorized                 = 401
	StatusPaymentRequired              = 402
	StatusForbidden                    = 403
	StatusNotFound                     = 404
	StatusMethodNotAllowed             = 405
	StatusNotAcceptable                = 406
	StatusProxyAuthRequired            = 407
	StatusRequestTimeout               = 408
	StatusConflict                     = 409
	StatusGone                         = 410
	StatusLengthRequired               = 411
	StatusPreconditionFailed           = 412
	StatusRequestEntityTooLarge        = 413
	StatusRequestURITooLong            = 414
	StatusUnsupportedMediaType         = 415
	StatusRequestedRangeNotSatisfiable = 416
	StatusExpectationFailed            = 417
	StatusTeapot                       = 418
	StatusMisdirectedRequest           = 421
	StatusUnprocessableEntity          = 422
	StatusLocked                       = 423
	StatusFailedDependency             = 424
	StatusTooEarly                     = 425
	StatusUpgradeRequired              = 426
	StatusPreconditionRequired         = 428
	StatusTooManyRequests              = 429
	StatusRequestHeaderFieldsTooLarge   = 431
	StatusUnavailableForLegalReasons   = 451

	StatusInternalServerError           = 500
	StatusNotImplemented                = 501
	StatusBadGateway                    = 502
	StatusServiceUnavailable            = 503
	StatusGatewayTimeout                = 504
	StatusHTTPVersionNotSupported       = 505
	StatusVariantAlsoNegotiates        = 506
	StatusInsufficientStorage          = 507
	StatusLoopDetected                 = 508
	StatusNotExtended                  = 510
	StatusNetworkAuthenticationRequired = 511
)

var statusText = map[int]string{
	StatusContinue:           "Continue",
	StatusSwitchingProtocols: "Switching Protocols",
	StatusProcessing:         "Processing",
	StatusEarlyHints:         "Early Hints",
	StatusOK:                   "OK",
	StatusCreated:              "Created",
	StatusAccepted:             "Accepted",
	StatusNonAuthoritativeInfo: "Non-Authoritative Information",
	StatusNoContent:            "No Content",
	StatusResetContent:         "Reset Content",
	StatusPartialContent:       "Partial Content",
	StatusMultiStatus:          "Multi-Status",
	StatusAlreadyReported:      "Already Reported",
	StatusIMUsed:               "IM Used",
	StatusMultipleChoices:  "Multiple Choices",
	StatusMovedPermanently: "Moved Permanently",
	StatusFound:            "Found",
	StatusSeeOther:         "See Other",
	StatusNotModified:      "Not Modified",
	StatusUseProxy:         "Use Proxy",
	StatusTemporaryRedirect: "Temporary Redirect",
	StatusPermanentRedirect: "Permanent Redirect",
	StatusBadRequest:                   "Bad Request",
	StatusUnauthorized:                 "Unauthorized",
	StatusPaymentRequired:              "Payment Required",
	StatusForbidden:                    "Forbidden",
	StatusNotFound:                     "Not Found",
	StatusMethodNotAllowed:             "Method Not Allowed",
	StatusNotAcceptable:                "Not Acceptable",
	StatusProxyAuthRequired:            "Proxy Authentication Required",
	StatusRequestTimeout:               "Request Timeout",
	StatusConflict:                     "Conflict",
	StatusGone:                         "Gone",
	StatusLengthRequired:               "Length Required",
	StatusPreconditionFailed:           "Precondition Failed",
	StatusRequestEntityTooLarge:        "Request Entity Too Large",
	StatusRequestURITooLong:            "Request URI Too Long",
	StatusUnsupportedMediaType:         "Unsupported Media Type",
	StatusRequestedRangeNotSatisfiable: "Requested Range Not Satisfiable",
	StatusExpectationFailed:            "Expectation Failed",
	StatusTeapot:                       "I'm a teapot",
	StatusMisdirectedRequest:           "Misdirected Request",
	StatusUnprocessableEntity:          "Unprocessable Entity",
	StatusLocked:                       "Locked",
	StatusFailedDependency:             "Failed Dependency",
	StatusTooEarly:                     "Too Early",
	StatusUpgradeRequired:              "Upgrade Required",
	StatusPreconditionRequired:         "Precondition Required",
	StatusTooManyRequests:              "Too Many Requests",
	StatusRequestHeaderFieldsTooLarge:   "Request Header Fields Too Large",
	StatusUnavailableForLegalReasons:   "Unavailable For Legal Reasons",
	StatusInternalServerError:           "Internal Server Error",
	StatusNotImplemented:                "Not Implemented",
	StatusBadGateway:                    "Bad Gateway",
	StatusServiceUnavailable:            "Service Unavailable",
	StatusGatewayTimeout:                "Gateway Timeout",
	StatusHTTPVersionNotSupported:       "HTTP Version Not Supported",
	StatusVariantAlsoNegotiates:         "Variant Also Negotiates",
	StatusInsufficientStorage:           "Insufficient Storage",
	StatusLoopDetected:                  "Loop Detected",
	StatusNotExtended:                   "Not Extended",
	StatusNetworkAuthenticationRequired: "Network Authentication Required",
}

// StatusText returns a text for the HTTP status code.
func StatusText(code int) string { return statusText[code] }

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

// A Header represents the key-value pairs in an HTTP header.
type Header map[string][]string

func (h Header) Add(key, value string) { textproto.MIMEHeader(h).Add(key, value) }
func (h Header) Set(key, value string) { textproto.MIMEHeader(h).Set(key, value) }
func (h Header) Get(key string) string { return textproto.MIMEHeader(h).Get(key) }
func (h Header) Values(key string) []string { return textproto.MIMEHeader(h).Values(key) }
func (h Header) Del(key string) { textproto.MIMEHeader(h).Del(key) }

func (h Header) Clone() Header {
	if h == nil {
		return nil
	}
	h2 := make(Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

// ---------------------------------------------------------------------------
// Request
// ---------------------------------------------------------------------------

type Request struct {
	Method           string
	URL              *url.URL
	Proto            string
	ProtoMajor       int
	ProtoMinor       int
	Header           Header
	Body             io.ReadCloser
	ContentLength    int64
	TransferEncoding []string
	Host             string
	Form             url.Values
	PostForm         url.Values
	Trailer          Header
	RemoteAddr       string
	RequestURI       string
}

func NewRequest(method, urlStr string, body io.Reader) (*Request, error) {
	__tmp := __newRequest(method, urlStr, body)
	return __tmp.req, __tmp.err
}

type __newRequestPair struct {
	req *Request
	err error
}

func __newRequest(method, urlStr string, body io.Reader) __newRequestPair {
	u, err := url.Parse(urlStr)
	if err != nil {
		return __newRequestPair{nil, err}
	}
	req := &Request{
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(Header),
		Host:       u.Host,
	}
	if body != nil {
		switch v := body.(type) {
		case *bytes.Buffer:
			req.ContentLength = int64(v.Len())
		case *bytes.Reader:
			req.ContentLength = int64(v.Len())
		case *strings.Reader:
			req.ContentLength = int64(v.Len())
		}
		req.Body = io.NopCloser(body)
	}
	return __newRequestPair{req, nil}
}

// ---------------------------------------------------------------------------
// Request reading (Go-implemented: needed for pipe leg + public API)
// ---------------------------------------------------------------------------

func ReadRequest(b *bufio.Reader) (*Request, error) {
	__tmp := __readRequest(b)
	return __tmp.req, __tmp.err
}

type __readRequestPair struct {
	req *Request
	err error
}

func __readRequest(b *bufio.Reader) __readRequestPair {
	line, err := readLine(b)
	if err != nil {
		return __readRequestPair{nil, err}
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return __readRequestPair{nil, errors.New("malformed HTTP request line")}
	}
	method, requestURI, proto := parts[0], parts[1], parts[2]
	if !strings.HasPrefix(proto, "HTTP/") {
		return __readRequestPair{nil, errors.New("malformed HTTP version")}
	}
	pv := strings.SplitN(proto[5:], ".", 2)
	if len(pv) != 2 {
		return __readRequestPair{nil, errors.New("malformed HTTP version")}
	}
	protoMajor, err1 := strconv.Atoi(pv[0])
	protoMinor, err2 := strconv.Atoi(pv[1])
	if err1 != nil || err2 != nil {
		return __readRequestPair{nil, errors.New("malformed HTTP version")}
	}
	mimeHeader, err := readMIMEHeader(b)
	if err != nil {
		return __readRequestPair{nil, err}
	}
	header := Header(mimeHeader)
	u, err := url.ParseRequestURI(requestURI)
	if err != nil {
		u, err = url.Parse(requestURI)
		if err != nil {
			return __readRequestPair{nil, err}
		}
	}
	req := &Request{
		Method: method, URL: u, Proto: proto,
		ProtoMajor: protoMajor, ProtoMinor: protoMinor,
		Header: header, Host: header.Get("Host"), RequestURI: requestURI,
	}
	if req.Host == "" {
		req.Host = u.Host
	}
	if te := header.Get("Transfer-Encoding"); te != "" {
		for _, enc := range strings.Split(te, ",") {
			if enc = strings.TrimSpace(strings.ToLower(enc)); enc != "" {
				req.TransferEncoding = append(req.TransferEncoding, enc)
			}
		}
	}
	if cl := header.Get("Content-Length"); cl != "" {
		n, err := strconv.ParseInt(strings.TrimSpace(cl), 10, 64)
		if err != nil || n < 0 {
			return __readRequestPair{nil, errors.New("bad Content-Length")}
		}
		req.ContentLength = n
	}
	chunked := false
	for _, enc := range req.TransferEncoding {
		if enc == "chunked" {
			chunked = true
		}
	}
	if chunked {
		req.Body = &chunkedReader{br: b, hdr: &req.Trailer}
	} else if req.ContentLength > 0 {
		req.Body = &limitedReadCloser{R: io.LimitReader(b, req.ContentLength)}
	} else {
		req.Body = io.NopCloser(bytes.NewReader(nil))
	}
	return __readRequestPair{req, nil}
}

func readLine(b *bufio.Reader) (string, error) {
	__tmp := __readLine(b)
	return __tmp.line, __tmp.err
}

type __readLinePair struct {
	line string
	err  error
}

func __readLine(b *bufio.Reader) __readLinePair {
	line, err := b.ReadString('\n')
	if err != nil {
		return __readLinePair{"", err}
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	return __readLinePair{line, nil}
}

func readMIMEHeader(b *bufio.Reader) (textproto.MIMEHeader, error) {
	__tmp := __readMIMEHeader(b)
	return __tmp.hdr, __tmp.err
}

type __readMIMEHeaderPair struct {
	hdr textproto.MIMEHeader
	err error
}

func __readMIMEHeader(b *bufio.Reader) __readMIMEHeaderPair {
	hdr := make(textproto.MIMEHeader)
	for {
		line, err := b.ReadString('\n')
		if err != nil {
			return __readMIMEHeaderPair{nil, err}
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			break
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return __readMIMEHeaderPair{nil, errors.New("malformed MIME header line")}
		}
		hdr.Add(strings.TrimSpace(line[:colon]), strings.TrimSpace(line[colon+1:]))
	}
	return __readMIMEHeaderPair{hdr, nil}
}

type chunkedReader struct {
	br   *bufio.Reader
	hdr  *Header
	n    int64
	done bool
}

func (cr *chunkedReader) Read(p []byte) (int, error) {
	__tmp := __chunkedRead(cr, p)
	return __tmp.n, __tmp.err
}

type __chunkedReadPair struct {
	n   int
	err error
}

func __chunkedRead(cr *chunkedReader, p []byte) __chunkedReadPair {
	if cr.done {
		return __chunkedReadPair{0, io.EOF}
	}
	if cr.n == 0 {
		line, err := readLine(cr.br)
		if err != nil {
			return __chunkedReadPair{0, err}
		}
		if i := strings.Index(line, ";"); i >= 0 {
			line = line[:i]
		}
		n, err := strconv.ParseInt(strings.TrimSpace(line), 16, 64)
		if err != nil || n < 0 {
			return __chunkedReadPair{0, errors.New("bad chunk length")}
		}
		if n == 0 {
			trailer, err := readMIMEHeader(cr.br)
			if err != nil {
				return __chunkedReadPair{0, err}
			}
			if cr.hdr != nil {
				*cr.hdr = Header(trailer)
			}
			cr.done = true
			return __chunkedReadPair{0, io.EOF}
		}
		cr.n = n
	}
	if int64(len(p)) > cr.n {
		p = p[:cr.n]
	}
	n, err := cr.br.Read(p)
	cr.n -= int64(n)
	if cr.n == 0 {
		readLine(cr.br)
	}
	return __chunkedReadPair{n, err}
}

func (cr *chunkedReader) Close() error { return nil }

type limitedReadCloser struct {
	R       io.Reader
	closeFn func() error
}

func (l *limitedReadCloser) Read(p []byte) (int, error) { return l.R.Read(p) }

// P3-M10: __-helper for the m7 desugar table. The interface call
// l.R.Read desugars through the mrIfaceMethod path (itab lookup on the
// concrete dynamic type).
type __limitedReadCloserReadPair struct {
	n   int
	err error
}

func __limitedReadCloserRead(l *limitedReadCloser, p []byte) __limitedReadCloserReadPair {
	n, err := l.R.Read(p)
	return __limitedReadCloserReadPair{n, err}
}
func (l *limitedReadCloser) Close() error {
	if l.closeFn != nil {
		return l.closeFn()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Response
// ---------------------------------------------------------------------------

type Response struct {
	Status           string
	StatusCode       int
	Proto            string
	ProtoMajor       int
	ProtoMinor       int
	Header           Header
	Body             io.ReadCloser
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Trailer          Header
	Request          *Request
}

func ReadResponse(b *bufio.Reader, req *Request) (*Response, error) {
	__tmp := __readResponse(b, req)
	return __tmp.resp, __tmp.err
}

type __readResponsePair struct {
	resp *Response
	err  error
}

func __readResponse(b *bufio.Reader, req *Request) __readResponsePair {
	line, err := readLine(b)
	if err != nil {
		return __readResponsePair{nil, err}
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return __readResponsePair{nil, errors.New("malformed HTTP response line")}
	}
	proto := parts[0]
	pv := strings.SplitN(proto[5:], ".", 2)
	protoMajor, _ := strconv.Atoi(pv[0])
	protoMinor := 0
	if len(pv) == 2 {
		protoMinor, _ = strconv.Atoi(pv[1])
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return __readResponsePair{nil, errors.New("malformed HTTP status code")}
	}
	status := StatusText(code)
	if len(parts) == 3 {
		status = parts[2]
	}
	mimeHeader, err := readMIMEHeader(b)
	if err != nil {
		return __readResponsePair{nil, err}
	}
	header := Header(mimeHeader)
	resp := &Response{
		Status: strconv.Itoa(code) + " " + status, StatusCode: code,
		Proto: proto, ProtoMajor: protoMajor, ProtoMinor: protoMinor,
		Header: header, Request: req,
	}
	if te := header.Get("Transfer-Encoding"); te != "" {
		for _, enc := range strings.Split(te, ",") {
			if enc = strings.TrimSpace(strings.ToLower(enc)); enc != "" {
				resp.TransferEncoding = append(resp.TransferEncoding, enc)
			}
		}
	}
	chunked := false
	for _, enc := range resp.TransferEncoding {
		if enc == "chunked" {
			chunked = true
		}
	}
	noBody := (req != nil && req.Method == "HEAD") ||
		code == StatusNoContent || code == StatusNotModified ||
		(code >= 100 && code < 200)
	var body io.ReadCloser
	if noBody {
		body = io.NopCloser(bytes.NewReader(nil))
		resp.ContentLength = 0
	} else if chunked {
		body = &chunkedReader{br: b, hdr: &resp.Trailer}
		resp.ContentLength = -1
	} else if cl := header.Get("Content-Length"); cl != "" {
		n, err := strconv.ParseInt(strings.TrimSpace(cl), 10, 64)
		if err != nil || n < 0 {
			return __readResponsePair{nil, errors.New("bad Content-Length")}
		}
		resp.ContentLength = n
		if n == 0 {
			body = io.NopCloser(bytes.NewReader(nil))
		} else {
			body = &limitedReadCloser{R: io.LimitReader(b, n)}
		}
	} else {
		resp.ContentLength = -1
		body = &limitedReadCloser{R: b}
	}
	resp.Body = body
	return __readResponsePair{resp, nil}
}

// ---------------------------------------------------------------------------
// ResponseWriter, Flusher, Hijacker
// ---------------------------------------------------------------------------

type ResponseWriter interface {
	Header() Header
	Write([]byte) (int, error)
	WriteHeader(statusCode int)
}

type Flusher interface {
	Flush()
}

type Hijacker interface {
	Hijack() (net.Conn, *bufio.ReadWriter, error)
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

type HandlerFunc func(ResponseWriter, *Request)

func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) { f(w, r) }

// ---------------------------------------------------------------------------
// ServeMux
// ---------------------------------------------------------------------------

type ServeMux struct {
	m map[string]muxEntry
}

// P3-M10: the v1 frontend (M2 scope) rejects sync primitives as struct
// fields, so ServeMux/Server synchronization uses package-level mutexes
// (cf. synth_mathrand.go's globalRandMu). Coarser than gc's per-mux
// RWMutex, but semantically equivalent mutual exclusion.
var serveMuxMu sync.Mutex

type muxEntry struct {
	h       Handler
	pattern string
}

func NewServeMux() *ServeMux { return &ServeMux{m: make(map[string]muxEntry)} }

var DefaultServeMux = NewServeMux()

func (mux *ServeMux) Handle(pattern string, handler Handler) {
	serveMuxMu.Lock()
	defer serveMuxMu.Unlock()
	mux.m[pattern] = muxEntry{h: handler, pattern: pattern}
}

func (mux *ServeMux) HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	mux.Handle(pattern, HandlerFunc(handler))
}

func (mux *ServeMux) ServeHTTP(w ResponseWriter, r *Request) {
	h, _ := mux.Handler(r)
	h.ServeHTTP(w, r)
}

func (mux *ServeMux) Handler(r *Request) (Handler, string) {
	__tmp := __muxHandler(mux, r)
	return __tmp.h, __tmp.pattern
}

type __muxHandlerPair struct {
	h       Handler
	pattern string
}

func __muxHandler(mux *ServeMux, r *Request) __muxHandlerPair {
	serveMuxMu.Lock()
	defer serveMuxMu.Unlock()
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	if e, ok := mux.m[path]; ok {
		return __muxHandlerPair{e.h, e.pattern}
	}
	var best *muxEntry
	bestLen := -1
	for pattern, e := range mux.m {
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(path, pattern) && len(pattern) > bestLen {
			ec := e
			best = &ec
			bestLen = len(pattern)
		}
	}
	if best != nil {
		return __muxHandlerPair{best.h, best.pattern}
	}
	return __muxHandlerPair{NotFoundHandler(), ""}
}

func Handle(pattern string, handler Handler) { DefaultServeMux.Handle(pattern, handler) }

func HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	DefaultServeMux.HandleFunc(pattern, handler)
}

func NotFoundHandler() Handler { return HandlerFunc(NotFound) }

func NotFound(w ResponseWriter, r *Request) { Error(w, "404 page not found", StatusNotFound) }

// ---------------------------------------------------------------------------
// Client — delegates to java.net.http.HttpClient via intrinsic
// ---------------------------------------------------------------------------

type Client struct {
	Transport RoundTripper
	Timeout   time.Duration
}

type RoundTripper interface {
	RoundTrip(*Request) (*Response, error)
}

var DefaultClient = &Client{}

func (c *Client) Do(req *Request) (*Response, error) {
	__tmp := __clientDo(c, req)
	return __tmp.resp, __tmp.err
}

type __clientDoPair struct {
	resp *Response
	err  error
}

func __clientDo(c *Client, req *Request) __clientDoPair {
	rt := c.Transport
	if rt == nil {
		rt = DefaultTransport
	}
	resp, err := rt.RoundTrip(req)
	return __clientDoPair{resp, err}
}

func Get(urlStr string) (*Response, error) {
	__tmp := __httpGet(urlStr)
	return __tmp.resp, __tmp.err
}

func __httpGet(urlStr string) __clientDoPair {
	req, err := NewRequest("GET", urlStr, nil)
	if err != nil {
		return __clientDoPair{nil, err}
	}
	return __clientDo(DefaultClient, req)
}

func Head(urlStr string) (*Response, error) {
	__tmp := __httpHead(urlStr)
	return __tmp.resp, __tmp.err
}

func __httpHead(urlStr string) __clientDoPair {
	req, err := NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return __clientDoPair{nil, err}
	}
	return __clientDo(DefaultClient, req)
}

func Post(urlStr, contentType string, body io.Reader) (*Response, error) {
	__tmp := __httpPost(urlStr, contentType, body)
	return __tmp.resp, __tmp.err
}

func __httpPost(urlStr, contentType string, body io.Reader) __clientDoPair {
	req, err := NewRequest("POST", urlStr, body)
	if err != nil {
		return __clientDoPair{nil, err}
	}
	req.Header.Set("Content-Type", contentType)
	return __clientDo(DefaultClient, req)
}

func PostForm(urlStr string, data url.Values) (*Response, error) {
	__tmp := __httpPostForm(urlStr, data)
	return __tmp.resp, __tmp.err
}

func __httpPostForm(urlStr string, data url.Values) __clientDoPair {
	return __httpPost(urlStr, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
}

// ---------------------------------------------------------------------------
// Transport — RoundTrip delegates to the Java intrinsic
// ---------------------------------------------------------------------------

type Transport struct {
	DialContext func(ctx interface{}, network, addr string) (net.Conn, error)
}

var DefaultTransport RoundTripper = &Transport{}

func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	__tmp := __transportRoundTrip(t, req)
	return __tmp.resp, __tmp.err
}

func __transportRoundTrip(t *Transport, req *Request) __clientDoPair {
	var hkv []string
	for k, vv := range req.Header {
		for _, v := range vv {
			hkv = append(hkv, k, v)
		}
	}
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
	}
	urlStr := req.URL.String()
	if urlStr == "" || req.URL.Host == "" {
		urlStr = "http://" + req.Host + req.URL.RequestURI()
	}
	result := httpClientDoIntrinsic(req.Method, urlStr, hkv, bodyBytes)
	rd := &__httpWireReader{b: result, ok: true}
	statusCode := rd.i64()
	if !rd.ok {
		return __clientDoPair{nil, errors.New("http: client intrinsic failed")}
	}
	if statusCode == 0 {
		return __clientDoPair{nil, errors.New("http: transport error")}
	}
	resp := &Response{
		StatusCode: int(statusCode), Proto: "HTTP/1.1",
		ProtoMajor: 1, ProtoMinor: 1,
		Header: make(Header), Request: req,
	}
	resp.Status = strconv.Itoa(resp.StatusCode) + " " + StatusText(resp.StatusCode)
	nhead := rd.i64()
	for i := int64(0); i < nhead; i++ {
		k := rd.bytes()
		v := rd.bytes()
		if !rd.ok {
			return __clientDoPair{nil, errors.New("http: client intrinsic failed")}
		}
		resp.Header.Add(string(k), string(v))
	}
	bodyOut := rd.bytes()
	if !rd.ok {
		return __clientDoPair{nil, errors.New("http: client intrinsic failed")}
	}
	resp.Body = io.NopCloser(bytes.NewReader(bodyOut))
	resp.ContentLength = int64(len(bodyOut))
	return __clientDoPair{resp, nil}
}

// ---------------------------------------------------------------------------
// Server — Go-implemented (needs net.Pipe support; Java HttpServer is TCP-only)
// ---------------------------------------------------------------------------

type Server struct {
	Addr           string
	Handler        Handler
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxHeaderBytes int
	listeners      map[net.Listener]struct{}
	activeConn     map[net.Conn]struct{}
}

// P3-M10: see the serveMuxMu note above — the Server's listener/conn
// bookkeeping maps are guarded by this package-level mutex instead of a
// per-Server sync.Mutex field (out of M2 scope; and user code builds
// Server values with composite literals, so a *sync.Mutex field could
// not be constructor-initialized).
var httpServerMu sync.Mutex

func (srv *Server) ListenAndServe() error {
	__tmp := __serverListenAndServe(srv)
	return __tmp.err
}

type __serverListenAndServePair struct {
	err error
}

func __serverListenAndServe(srv *Server) __serverListenAndServePair {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return __serverListenAndServePair{err}
	}
	__tmp := __serverServe(srv, l)
	return __serverListenAndServePair{__tmp.err}
}

func (srv *Server) Serve(l net.Listener) error {
	__tmp := __serverServe(srv, l)
	return __tmp.err
}

type __serverServePair struct {
	err error
}

func __serverServe(srv *Server, l net.Listener) __serverServePair {
	httpServerMu.Lock()
	if srv.listeners == nil {
		srv.listeners = make(map[net.Listener]struct{})
	}
	srv.listeners[l] = struct{}{}
	httpServerMu.Unlock()
	defer func() {
		httpServerMu.Lock()
		delete(srv.listeners, l)
		httpServerMu.Unlock()
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			return __serverServePair{err}
		}
		httpServerMu.Lock()
		if srv.activeConn == nil {
			srv.activeConn = make(map[net.Conn]struct{})
		}
		srv.activeConn[c] = struct{}{}
		httpServerMu.Unlock()
		go srv.serveConn(c)
	}
}

func (srv *Server) serveConn(c net.Conn) {
	defer func() {
		c.Close()
		httpServerMu.Lock()
		delete(srv.activeConn, c)
		httpServerMu.Unlock()
	}()
	br := bufio.NewReader(c)
	for {
		req, err := ReadRequest(br)
		if err != nil {
			if err != io.EOF {
				writeSimpleResponse(c, 400, "Bad Request\n")
			}
			return
		}
		req.RemoteAddr = c.RemoteAddr().String()
		handler := srv.Handler
		if handler == nil {
			handler = DefaultServeMux
		}
		w := &responseWriter{conn: c, header: make(Header)}
		handler.ServeHTTP(w, req)
		w.finish()
		return
	}
}

func (srv *Server) Close() error {
	httpServerMu.Lock()
	defer httpServerMu.Unlock()
	for l := range srv.listeners {
		l.Close()
	}
	for c := range srv.activeConn {
		c.Close()
	}
	return nil
}

type responseWriter struct {
	conn        net.Conn
	header      Header
	status      int
	wroteHeader bool
	sentHeader  bool
	body        bytes.Buffer
}

func (w *responseWriter) Header() Header { return w.header }

func (w *responseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = statusCode
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(200)
	}
	return w.body.Write(p)
}

// P3-M10: __-helper for the m7 desugar table (v1 cannot emit the
// multi-result Write method; the body.Write call desugars through the
// bytes.Buffer.Write table entry).
type __responseWriterWritePair struct {
	n   int
	err error
}

func __responseWriterWrite(w *responseWriter, p []byte) __responseWriterWritePair {
	if !w.wroteHeader {
		w.WriteHeader(200)
	}
	n, err := w.body.Write(p)
	return __responseWriterWritePair{n, err}
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(200)
	}
	w.sendHeader()
	if w.body.Len() > 0 {
		w.conn.Write(w.body.Bytes())
		w.body.Reset()
	}
}

func (w *responseWriter) sendHeader() {
	if w.sentHeader {
		return
	}
	w.sentHeader = true
	var buf bytes.Buffer
	text := StatusText(w.status)
	if text == "" {
		text = "status " + strconv.Itoa(w.status)
	}
	buf.WriteString("HTTP/1.1 " + strconv.Itoa(w.status) + " " + text + "\r\n")
	for k, vv := range w.header {
		for _, v := range vv {
			buf.WriteString(k + ": " + v + "\r\n")
		}
	}
	buf.WriteString("Content-Length: " + strconv.Itoa(w.body.Len()) + "\r\n")
	buf.WriteString("Connection: close\r\n")
	buf.WriteString("\r\n")
	w.conn.Write(buf.Bytes())
}

func (w *responseWriter) finish() {
	if !w.wroteHeader {
		w.status = 200
		w.wroteHeader = true
	}
	w.sendHeader()
	if w.body.Len() > 0 {
		w.conn.Write(w.body.Bytes())
		w.body.Reset()
	}
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	__tmp := __rwHijack(w)
	return __tmp.c, __tmp.rw, __tmp.err
}

type __rwHijackPair struct {
	c   net.Conn
	rw  *bufio.ReadWriter
	err error
}

func __rwHijack(w *responseWriter) __rwHijackPair {
	if !w.wroteHeader {
		w.WriteHeader(200)
	}
	w.sendHeader()
	if w.body.Len() > 0 {
		w.conn.Write(w.body.Bytes())
		w.body.Reset()
	}
	rw := bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn))
	return __rwHijackPair{w.conn, rw, nil}
}

func writeSimpleResponse(w io.Writer, status int, body string) {
	text := StatusText(status)
	var buf bytes.Buffer
	buf.WriteString("HTTP/1.1 " + strconv.Itoa(status) + " " + text + "\r\n")
	buf.WriteString("Content-Length: " + strconv.Itoa(len(body)) + "\r\n")
	buf.WriteString("Connection: close\r\n\r\n")
	buf.WriteString(body)
	w.Write(buf.Bytes())
}

func Serve(l net.Listener, handler Handler) error {
	srv := &Server{Handler: handler}
	__tmp := __serverServe(srv, l)
	return __tmp.err
}

func ListenAndServe(addr string, handler Handler) error {
	srv := &Server{Addr: addr, Handler: handler}
	__tmp := __serverListenAndServe(srv)
	return __tmp.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func Error(w ResponseWriter, error string, code int) {
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	w.Write([]byte(error + "\n"))
}

func MaxBytesReader(w ResponseWriter, r io.ReadCloser, n int64) io.ReadCloser {
	// P3-M10: method values on interfaces are out of v1 scope (P3-M3);
	// the closure below calls the interface method directly instead.
	return &limitedReadCloser{R: io.LimitReader(r, n), closeFn: func() error { return r.Close() }}
}

func DetectContentType(data []byte) string {
	if len(data) > 512 {
		data = data[:512]
	}
	if isHTML(data) {
		return "text/html; charset=utf-8"
	}
	if isText(data) {
		return "text/plain; charset=utf-8"
	}
	if len(data) >= 4 {
		if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
			return "image/png"
		}
		if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
			return "image/jpeg"
		}
		if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
			return "image/gif"
		}
		if len(data) >= 4 && data[0] == 0x25 && data[1] == 0x50 && data[2] == 0x44 && data[3] == 0x46 {
			return "application/pdf"
		}
	}
	return "application/octet-stream"
}

func isText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	for _, b := range data {
		if b < 0x09 || b == 0x0B || (b > 0x0D && b < 0x20) || b == 0x7F {
			return false
		}
	}
	return true
}

func isHTML(data []byte) bool {
	lower := bytes.ToLower(data)
	trimmed := bytes.TrimSpace(lower)
	for _, tag := range []string{
		"<!doctype html", "<html", "<head", "<script", "<iframe",
		"<h1", "<div", "<font", "<table", "<a", "<style",
		"<title", "<b", "<body", "<br", "<p", "<!--",
	} {
		if strings.HasPrefix(string(trimmed), tag) {
			return true
		}
	}
	return false
}

func Redirect(w ResponseWriter, r *Request, urlStr string, code int) {
	h := w.Header()
	h.Set("Location", urlStr)
	w.WriteHeader(code)
}

func (r *Request) ParseForm() error {
	__tmp := __parseForm(r)
	return __tmp.err
}

type __parseFormPair struct {
	err error
}

func __parseForm(r *Request) __parseFormPair {
	if r.Form != nil {
		return __parseFormPair{nil}
	}
	r.Form = make(url.Values)
	if r.URL != nil {
		for k, vv := range r.URL.Query() {
			for _, v := range vv {
				r.Form.Add(k, v)
			}
		}
	}
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return __parseFormPair{err}
			}
			vals, err := url.ParseQuery(string(body))
			if err != nil {
				return __parseFormPair{err}
			}
			for k, vv := range vals {
				for _, v := range vv {
					r.Form.Add(k, v)
				}
			}
			r.PostForm = vals
		}
	}
	return __parseFormPair{nil}
}

func (r *Request) FormValue(key string) string {
	r.ParseForm()
	if r.Form == nil {
		return ""
	}
	return r.Form.Get(key)
}
`
