package vet

import (
	"strings"
	"testing"
)

// countDiag runs vet on src and counts diagnostics containing substr.
func countDiag(t *testing.T, src, substr string) int {
	t.Helper()
	joined := runVetOnSrc(t, src)
	return strings.Count(joined, substr)
}

// TestUnsafePtrRoundTrip (R05-V-07): the safe uintptr round-trip and its
// arithmetic forms must be quiet, while genuinely unsafe conversions,
// aliased-import conversions, and reflect-header misuse must be flagged.
func TestUnsafePtrRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int // expected count of "possible misuse" diagnostics
	}{
		{
			name: "plain round-trip",
			src: `package main
import "unsafe"
func f(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p))
}
`,
			want: 0,
		},
		{
			name: "round-trip with addition",
			src: `package main
import "unsafe"
func f(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + 8)
}
`,
			want: 0,
		},
		{
			name: "round-trip with subtraction",
			src: `package main
import "unsafe"
func f(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) - 8)
}
`,
			want: 0,
		},
		{
			name: "round-trip with AND-NOT alignment",
			src: `package main
import "unsafe"
func f(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) &^ 7)
}
`,
			want: 0,
		},
		{
			name: "parenthesised round-trip",
			src: `package main
import "unsafe"
func f(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer((uintptr(p) + 8))
}
`,
			want: 0,
		},
		{
			name: "reflect Value.Pointer",
			src: `package main
import (
	"reflect"
	"unsafe"
)
func f(v reflect.Value) unsafe.Pointer {
	return unsafe.Pointer(v.Pointer())
}
`,
			want: 0,
		},
		{
			name: "reflect Value.UnsafeAddr",
			src: `package main
import (
	"reflect"
	"unsafe"
)
func f(v reflect.Value) unsafe.Pointer {
	return unsafe.Pointer(v.UnsafeAddr())
}
`,
			want: 0,
		},
		{
			name: "reflect ValueOf Pointer",
			src: `package main
import (
	"reflect"
	"unsafe"
)
func f(x int) unsafe.Pointer {
	return unsafe.Pointer(reflect.ValueOf(x).Pointer())
}
`,
			want: 0,
		},
		{
			name: "SliceHeader Data via pointer",
			src: `package main
import (
	"reflect"
	"unsafe"
)
func f(h *reflect.SliceHeader) unsafe.Pointer {
	return unsafe.Pointer(h.Data)
}
`,
			want: 0,
		},
		{
			name: "StringHeader Data via pointer",
			src: `package main
import (
	"reflect"
	"unsafe"
)
func f(h *reflect.StringHeader) unsafe.Pointer {
	return unsafe.Pointer(h.Data)
}
`,
			want: 0,
		},
		{
			name: "aliased unsafe import, safe round-trip",
			src: `package main
import u "unsafe"
func f(p u.Pointer) u.Pointer {
	return u.Pointer(uintptr(p) + 8)
}
`,
			want: 0,
		},
		{
			name: "plain uintptr variable",
			src: `package main
import "unsafe"
func f(x uintptr) unsafe.Pointer {
	return unsafe.Pointer(x)
}
`,
			want: 1,
		},
		{
			name: "non-reflect Pointer method",
			src: `package main
import "unsafe"
type myVal struct{}
func (myVal) Pointer() uintptr { return 0 }
func f(v myVal) unsafe.Pointer {
	return unsafe.Pointer(v.Pointer())
}
`,
			want: 1,
		},
		{
			name: "SliceHeader Data via value",
			src: `package main
import (
	"reflect"
	"unsafe"
)
func f(h reflect.SliceHeader) unsafe.Pointer {
	return unsafe.Pointer(h.Data)
}
`,
			want: 1,
		},
		{
			name: "sum of two uintptr conversions",
			src: `package main
import "unsafe"
func f(p, q unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + uintptr(q))
}
`,
			want: 1,
		},
		{
			name: "aliased unsafe import, plain uintptr",
			src: `package main
import u "unsafe"
func f(x uintptr) u.Pointer {
	return u.Pointer(x)
}
`,
			want: 1,
		},
		{
			name: "dereference SliceHeader",
			src: `package main
import "reflect"
func f(h *reflect.SliceHeader) reflect.SliceHeader {
	return *h
}
`,
			want: 1,
		},
		{
			name: "address of SliceHeader",
			src: `package main
import "reflect"
func f(h reflect.SliceHeader) *reflect.SliceHeader {
	return &h
}
`,
			want: 1,
		},
		{
			name: "dereference StringHeader",
			src: `package main
import "reflect"
func f(h *reflect.StringHeader) reflect.StringHeader {
	return *h
}
`,
			want: 1,
		},
		{
			name: "address of StringHeader",
			src: `package main
import "reflect"
func f(h reflect.StringHeader) *reflect.StringHeader {
	return &h
}
`,
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countDiag(t, tc.src, "possible misuse"); got != tc.want {
				t.Errorf("expected %d \"possible misuse\" diagnostics, got %d", tc.want, got)
			}
		})
	}
}

// TestSigChanyzerScope (R05-V-10): the channel must be resolved to its
// defining object (scope-aware), and var declarations must be recognised.
func TestSigChanyzerScope(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int // expected count of "should be buffered" diagnostics
	}{
		{
			name: "same name c, different buffering in two functions",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
}
func g() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
}
`,
			want: 1,
		},
		{
			name: "ValueSpec unbuffered",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	var c = make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
}
`,
			want: 1,
		},
		{
			name: "ValueSpec buffered",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	var c = make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
}
`,
			want: 0,
		},
		{
			name: "shadowing inner block",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	c := make(chan os.Signal)
	func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
	}()
	signal.Notify(c, os.Interrupt)
}
`,
			want: 1,
		},
		{
			name: "direct inline make",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	signal.Notify(make(chan os.Signal), os.Interrupt)
}
`,
			want: 1,
		},
		{
			name: "cap zero",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	c := make(chan os.Signal, 0)
	signal.Notify(c, os.Interrupt)
}
`,
			want: 1,
		},
		{
			name: "declared without make",
			src: `package main
import (
	"os"
	"os/signal"
)
func f(c chan os.Signal) {
	signal.Notify(c, os.Interrupt)
}
`,
			want: 0,
		},
		{
			name: "both unbuffered in two functions",
			src: `package main
import (
	"os"
	"os/signal"
)
func f() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
}
func g() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
}
`,
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countDiag(t, tc.src, "should be buffered"); got != tc.want {
				t.Errorf("expected %d \"should be buffered\" diagnostics, got %d", tc.want, got)
			}
		})
	}
}

// TestHostPortUnix (R05-V-11): unix-family networks have no port by design.
func TestHostPortUnix(t *testing.T) {
	for _, network := range []string{"unix", "unixgram", "unixpacket"} {
		src := `package main
import "net"
func f() {
	_, _ = net.Dial("` + network + `", "/tmp/sock")
}
`
		if got := countDiag(t, src, "missing port"); got != 0 {
			t.Errorf("network %q: expected no missing-port diagnostic, got %d", network, got)
		}
	}
	// A tcp address without a port is still flagged.
	src := `package main
import "net"
func f() {
	_, _ = net.Dial("tcp", "localhost")
}
`
	if got := countDiag(t, src, "missing port"); got != 1 {
		t.Errorf("tcp: expected 1 missing-port diagnostic, got %d", got)
	}
}

// TestHostPortSprintf (R05-V-11): fmt.Sprintf("%s:%d"/"%s:%s", host, port)
// passed to Dial is an IPv6 hazard, directly or through a variable.
func TestHostPortSprintf(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int // expected count of "does not work with IPv6" diagnostics
	}{
		{
			name: "direct Sprintf %s:%d",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host string, port int) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
}
`,
			want: 1,
		},
		{
			name: "direct Sprintf %s:%s",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host, port string) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%s", host, port))
}
`,
			want: 1,
		},
		{
			name: "through variable :=",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host string, port int) {
	addr := fmt.Sprintf("%s:%d", host, port)
	_, _ = net.Dial("tcp", addr)
}
`,
			want: 1,
		},
		{
			name: "through variable var",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host, port string) {
	var addr = fmt.Sprintf("%s:%s", host, port)
	_, _ = net.Dial("tcp", addr)
}
`,
			want: 1,
		},
		{
			name: "other format",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host string, port int) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s-%d", host, port))
}
`,
			want: 0,
		},
		{
			name: "Sprintf with two args",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host string) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host))
}
`,
			want: 0,
		},
		{
			name: "DialTimeout direct Sprintf",
			src: `package main
import (
	"fmt"
	"net"
	"time"
)
func f(host string, port int) {
	_, _ = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), time.Second)
}
`,
			want: 1,
		},
		{
			name: "Dialer.Dial through variable",
			src: `package main
import (
	"fmt"
	"net"
)
func f(host string, port int) {
	d := net.Dialer{}
	addr := fmt.Sprintf("%s:%d", host, port)
	_, _ = d.Dial("tcp", addr)
}
`,
			want: 1,
		},
		{
			name: "Dialer.Dial unix",
			src: `package main
import "net"
func f() {
	d := net.Dialer{}
	_, _ = d.Dial("unix", "/tmp/sock")
}
`,
			want: 0,
		},
		{
			name: "constant-folded concatenation",
			src: `package main
import "net"
func f() {
	_, _ = net.Dial("tcp", "local" + "host:80")
}
`,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countDiag(t, tc.src, "does not work with IPv6"); got != tc.want {
				t.Errorf("expected %d IPv6 diagnostics, got %d", tc.want, got)
			}
		})
	}
	// Non-constant concatenation is still flagged.
	src := `package main
import "net"
func f(host string) {
	_, _ = net.Dial("tcp", host+":80")
}
`
	if got := countDiag(t, src, "JoinHostPort"); got != 1 {
		t.Errorf("expected 1 JoinHostPort diagnostic, got %d", got)
	}
	// Dialer.Dial with a port-less address still reports missing port.
	src = `package main
import "net"
func f() {
	d := net.Dialer{}
	_, _ = d.Dial("tcp", "localhost")
}
`
	if got := countDiag(t, src, "missing port"); got != 1 {
		t.Errorf("expected 1 missing-port diagnostic, got %d", got)
	}
}
