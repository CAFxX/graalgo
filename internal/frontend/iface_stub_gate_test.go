package main

// P1-1 (review-2026-11-04) regression: interface dispatch to a
// stub-only synthetic method must fail at compile time. Before the fix,
// every "reject" case below exported successfully (frontend exit 0)
// while the method table wired the call to a fail-fast panic stub, so
// the accepted program could only fail at run time. After the fix, the
// exporter rejects them with a named P1-1 scope error naming the
// stub-only method(s) in the implementer set.
//
// Deliberately NOT pinned here:
//   - zip/multipart interface shapes: the baseline frontend crashes at
//     import for those packages (pre-existing defects, unrelated to
//     this gate), so their interface shapes are currently unreachable.
//   - the collision program (base32 imported, but only bytes.Buffer
//     used via io.Reader) is in the ACCEPT set: reachability requires
//     a stub value to actually flow from user code, not mere import.
//   - the over-approximation residue: a program that constructs a stub
//     value anywhere AND calls the same interface method on a different,
//     real implementer elsewhere is still rejected (no per-variable flow
//     analysis). That case is documented in iface_stub_gate.go and is
//     follow-up work, not pinned either way here.
//   - single-result stub dispatch (e.g. encoder.Close via io.Closer)
//     and library-mediated dispatch (e.g. io.Copy): out of this gate's
//     multi-result scope; see iface_stub_gate.go.

import (
	"strings"
	"testing"
)

func TestIfaceStubGateRejects(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // required substring of the rejection
	}{
		{
			"base32 encoder via io.Writer, 2-target (p1 shape)",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func main() {
	var buf bytes.Buffer
	var w io.Writer = base32.NewEncoder(base32.StdEncoding, &buf)
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"base32 decoder via io.Reader",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func main() {
	var r io.Reader = base32.NewDecoder(base32.StdEncoding, bytes.NewReader([]byte("hello")))
	_, _ = r.Read(make([]byte, 8))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"base32 encoder via io.Writer, go-statement",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func main() {
	var buf bytes.Buffer
	var w io.Writer = base32.NewEncoder(base32.StdEncoding, &buf)
	go w.Write([]byte("hello"))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"base32 encoder via io.Writer, return position",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func f(w io.Writer, p []byte) (int, error) {
	return w.Write(p)
}

func main() {
	var buf bytes.Buffer
	var w io.Writer = base32.NewEncoder(base32.StdEncoding, &buf)
	_, _ = f(w, []byte("hello"))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"base32 encoder via io.Writer, argument position",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func g(n int, err error) {}

func main() {
	var buf bytes.Buffer
	var w io.Writer = base32.NewEncoder(base32.StdEncoding, &buf)
	g(w.Write([]byte("hello")))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"base32 encoder via io.WriteCloser",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func main() {
	var buf bytes.Buffer
	var w io.WriteCloser = base32.NewEncoder(base32.StdEncoding, &buf)
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"io SectionReader via io.Reader",
			`package main

import (
	"io"
	"strings"
)

func main() {
	var r io.Reader = io.NewSectionReader(strings.NewReader("hello"), 0, 5)
	_, _ = r.Read(make([]byte, 8))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"io TeeReader via io.Reader",
			`package main

import (
	"bytes"
	"io"
	"strings"
)

func main() {
	var r io.Reader = io.TeeReader(strings.NewReader("hello"), &bytes.Buffer{})
	_, _ = r.Read(make([]byte, 8))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"io MultiReader via io.Reader",
			`package main

import (
	"io"
	"strings"
)

func main() {
	var r io.Reader = io.MultiReader(strings.NewReader("hello"), strings.NewReader("world"))
	_, _ = r.Read(make([]byte, 8))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"io MultiWriter via io.Writer",
			`package main

import (
	"bytes"
	"io"
)

func main() {
	var w io.Writer = io.MultiWriter(&bytes.Buffer{}, &bytes.Buffer{})
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"io OffsetWriter via io.Writer",
			`package main

import (
	"io"
)

type wa struct{}

func (wa) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

func main() {
	var w io.Writer = io.NewOffsetWriter(wa{}, 0)
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
			"P1-1",
		},
		{
			"interface method value (pre-existing P3-M3 rejection, pinned)",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func main() {
	var buf bytes.Buffer
	var w io.Writer = base32.NewEncoder(base32.StdEncoding, &buf)
	f := w.Write
	_, _ = f([]byte("hello"))
	_ = io.Discard
}
`,
			"P3-M3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeIfaceProg(t, tc.src)
			_, err := ExportFile(p)
			if err == nil {
				t.Fatalf("expected rejection, got success")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestIfaceStubGateAccepts(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			"bytes.Buffer.Read return position (p4 control)",
			`package main

import "bytes"

func f(b *bytes.Buffer, p []byte) (int, error) {
	return b.Read(p)
}

func main() {
	_, _ = f(bytes.NewBuffer([]byte("hello")), make([]byte, 8))
}
`,
		},
		{
			"os.Executable 2-target (p6 control)",
			`package main

import "os"

func main() {
	_, _ = os.Executable()
}
`,
		},
		{
			"gzip writer via io.Writer (real __-helper; NOT a stub)",
			`package main

import (
	"bytes"
	"compress/gzip"
	"io"
)

func main() {
	var buf bytes.Buffer
	var w io.Writer = gzip.NewWriter(&buf)
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
		},
		{
			"base64 encoder via io.Writer (real __-helper; NOT a stub)",
			`package main

import (
	"bytes"
	"encoding/base64"
	"io"
)

func main() {
	var buf bytes.Buffer
	var w io.Writer = base64.NewEncoder(base64.StdEncoding, &buf)
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
		},
		{
			"tar reader via io.Reader (real __-helper; NOT a stub)",
			`package main

import (
	"archive/tar"
	"bytes"
	"io"
)

func main() {
	var r io.Reader = tar.NewReader(bytes.NewReader(nil))
	_, _ = r.Read(make([]byte, 8))
	_ = io.Discard
}
`,
		},
		{
			"sha256 digest Write via hash.Hash (real __-helper; NOT a stub)",
			`package main

import (
	"crypto/sha256"
	"hash"
)

func main() {
	var h hash.Hash = sha256.New()
	_, _ = h.Write([]byte("hello"))
}
`,
		},
		{
			"bytes.Buffer via io.Reader (whitelist path, real implementer)",
			`package main

import (
	"bytes"
	"io"
)

func main() {
	var r io.Reader = bytes.NewBuffer([]byte("hello"))
	_, _ = r.Read(make([]byte, 8))
	var w io.Writer = bytes.NewBuffer(nil)
	_, _ = w.Write([]byte("hello"))
	_ = io.Discard
}
`,
		},
		{
			"user-defined interface with user implementer",
			`package main

type R interface {
	Read(p []byte) (int, error)
}

type B struct{}

func (B) Read(p []byte) (int, error) { return len(p), nil }

func main() {
	var r R = B{}
	_, _ = r.Read([]byte("hello"))
}
`,
		},
		{
			// Regression guard for the first full-suite run: context
			// ctx Deadline methods have single-panic bodies but a live
			// mrDeadline desugar (__deadlineOf), so they are NOT
			// stub-only. The gate must not fire (TestDeadlineCallableP4
			// pins the same path from the wire side).
			"context.Background().Deadline() (live mrDeadline path)",
			`package main

import "context"

func main() {
	ctx := context.Background()
	_, _ = ctx.Deadline()
}
`,
		},
		{
			"collision: base32 imported but only bytes.Buffer via io.Reader",
			`package main

import (
	"bytes"
	"encoding/base32"
	"io"
)

func main() {
	s := base32.StdEncoding.EncodeToString([]byte("hello"))
	_ = s
	var r io.Reader = bytes.NewBuffer([]byte("hello"))
	_, _ = r.Read(make([]byte, 8))
	_ = io.Discard
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeIfaceProg(t, tc.src)
			if _, err := ExportFile(p); err != nil {
				t.Fatalf("expected success, got error: %v", err)
			}
		})
	}
}
