package main

// P1-3: an unimplemented stub referenced from reachable code fails the
// export (the equivalent of gc's link error for a missing
// implementation); a stub referenced from nowhere in the emitted program
// keeps the stub form.

import (
	"strings"
	"testing"
)

func TestUnimplementedReachableFails(t *testing.T) {
	cases := map[string]string{
		// Direct method call on a bodyless method.
		"method-called": `package main

type T struct{}

func (T) M() int

func main() { var x T; _ = x.M() }
`,
		// Function value of a multi-result bodyless function.
		"pair-value": `package main

func pair() (int, int)

func main() { _ = pair }
`,
		// Direct multi-result call.
		"pair-called": `package main

func pair() (int, int)

func main() { a, b := pair(); _, _ = a, b }
`,
		// Transitive reachability: main -> a -> b -> stub.
		"transitive": `package main

type T struct{}

func (T) M() int

func b(x T) int { return x.M() }

func a(x T) int { return b(x) }

func main() { var x T; _ = a(x) }
`,
		// Package-level initializer calling a stub.
		"global-init": `package main

type T struct{}

func (T) M() int

var x T

var y = x.M()

func main() { _ = y }
`,
		// init() calling a stub.
		"init-calls": `package main

type T struct{}

func (T) M() int

func init() { var x T; _ = x.M() }

func main() {}
`,
		// Multi-result //go:linkname declaration with no provider,
		// called from main.
		"linkname-called": `package main

//go:linkname pair2 other.missing
func pair2() (int, int)

func main() { a, b := pair2(); _, _ = a, b }
`,
	}
	for name, src := range cases {
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("%s: expected export failure for reachable stub, got success", name)
			continue
		}
		if !strings.Contains(err.Error(), "unimplemented") {
			t.Errorf("%s: error %q does not name the unimplemented stub", name, err)
		}
		if !strings.Contains(err.Error(), "reachable") {
			t.Errorf("%s: error %q does not say reachable", name, err)
		}
	}
}

func TestUnimplementedUnreferencedBuilds(t *testing.T) {
	cases := map[string]string{
		// Declared and never used at all.
		"method-declared-only": `package main

type T struct{}

func (T) M() int

func main() {}
`,
		"pair-declared-only": `package main

func pair() (int, int)

func main() {}
`,
		// Blank-identifier interface assignment: the exporter elides
		// the conversion (the global initializer is just the composite
		// literal), so no itab is built and the stub is genuinely
		// unreachable in the emitted program (documented approximation
		// in stub_reachability.go).
		"method-iface-blank": `package main

type T struct{}

func (T) M() int

var _ interface{ M() int } = T{}

func main() {}
`,
		// Conversion to the empty interface builds no method itab.
		"method-any-conversion": `package main

type T struct{}

func (T) M() int

func main() { var i any = T{}; _ = i }
`,
	}
	for name, src := range cases {
		out, err := ExportFile(writeSyncProg(t, src))
		if err != nil {
			t.Errorf("%s: unreferenced stub must still export, got error: %v", name, err)
			continue
		}
		if !strings.Contains(out, `"unimplemented": true`) {
			t.Errorf("%s: export lost the stub form for the unreferenced declaration", name)
		}
	}
}

func TestUnimplementedInterfaceConversionFails(t *testing.T) {
	// A non-blank conversion to an interface whose method set contains
	// the stub method builds the itab from the type's method table, so
	// the stub is reachable: the export must fail.
	src := `package main

type T struct{}

func (T) M() int

func main() {
	var i interface{ M() int } = T{}
	_ = i.M()
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("interface conversion + dispatch to stub method: expected export failure, got success")
	}
	if !strings.Contains(err.Error(), "unimplemented") || !strings.Contains(err.Error(), "M") {
		t.Fatalf("interface conversion: error %q does not name the unimplemented stub method", err)
	}
}

func TestUnimplementedReasonFields(t *testing.T) {
	cases := map[string]struct {
		src    string
		reason string
	}{
		"method": {
			src: `package main

type T struct{}

func (T) M() int

func main() {}
`,
			reason: "bodyless method",
		},
		"defaulted-multi": {
			src: `package main

func pair() (int, int)

func main() {}
`,
			reason: "default symbol",
		},
		"linkname-multi-no-provider": {
			src: `package main

//go:linkname pair2 other.missing
func pair2() (int, int)

func main() {}
`,
			reason: "no compiled provider",
		},
	}
	for name, tc := range cases {
		out, err := ExportFile(writeSyncProg(t, tc.src))
		if err != nil {
			t.Errorf("%s: expected export success for unreferenced stub, got: %v", name, err)
			continue
		}
		if !strings.Contains(out, `"unimplemented": true`) {
			t.Errorf("%s: export lost the stub form", name)
		}
		if !strings.Contains(out, tc.reason) {
			t.Errorf("%s: export missing reason %q", name, tc.reason)
		}
		// The old message blamed assembly for every shape; the reason
		// field must not carry that claim for non-assembly shapes.
		if name != "method" && strings.Contains(out, "assembly function not implemented") {
			t.Errorf("%s: export still carries the old assembly-blame message", name)
		}
	}
}
