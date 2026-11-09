package vet

import (
	"strings"
	"testing"
)

// R05-V-03: nilfunc must only flag comparisons where the func side is a
// value known never to be nil — a declared function/method value or a
// func literal. Func variables, parameters, fields, and call results
// can be nil, so their nil checks are legitimate and must stay quiet.
func TestNilFunc(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// want is the nilfunc diagnostic substring expected, or "" if
		// the comparison must stay quiet.
		want string
	}{
		{
			name: "func variable == nil is quiet",
			src: `package main

func main() {
	var f func()
	if f == nil {
		println("nil")
	}
}
`,
			want: "",
		},
		{
			name: "func variable != nil is quiet",
			src: `package main

func main() {
	var f func()
	if f != nil {
		println("nil")
	}
}
`,
			want: "",
		},
		{
			name: "func parameter == nil is quiet",
			src: `package main

func check(f func()) {
	if f == nil {
		println("nil")
	}
}
`,
			want: "",
		},
		{
			name: "func return value == nil is quiet",
			src: `package main

func getF() func() { return nil }

func main() {
	if getF() == nil {
		println("nil")
	}
}
`,
			want: "",
		},
		{
			name: "func field == nil is quiet",
			src: `package main

type S struct {
	f func()
}

func main() {
	var s S
	if s.f == nil {
		println("nil")
	}
}
`,
			want: "",
		},
		{
			name: "declared func == nil fires",
			src: `package main

func helper() {}

func main() {
	if helper == nil {
		println("nil")
	}
}
`,
			want: "comparison of function to nil is always false",
		},
		{
			name: "declared func != nil fires",
			src: `package main

func helper() {}

func main() {
	if helper != nil {
		println("nil")
	}
}
`,
			want: "comparison of function to nil is always true",
		},
		{
			name: "func literal == nil fires",
			src: `package main

func main() {
	if func() {} == nil {
		println("nil")
	}
}
`,
			want: "comparison of function to nil is always false",
		},
		{
			name: "method value == nil fires",
			src: `package main

type T struct{}

func (T) M() {}

func main() {
	var t T
	if t.M == nil {
		println("nil")
	}
}
`,
			want: "comparison of function to nil is always false",
		},
		{
			name: "package-qualified func == nil fires",
			src: `package main

import "strings"

func main() {
	if strings.Contains == nil {
		println("nil")
	}
}
`,
			want: "comparison of function to nil is always false",
		},
		{
			name: "parenthesised declared func == nil fires",
			src: `package main

func helper() {}

func main() {
	if (helper) == nil {
		println("nil")
	}
}
`,
			want: "comparison of function to nil is always false",
		},
		{
			name: "parenthesised func variable == nil is quiet",
			src: `package main

func main() {
	var f func()
	if (f) == nil {
		println("nil")
	}
}
`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			joined := runVetOnSrc(t, tc.src)
			if tc.want == "" && strings.Contains(joined, "comparison of function to nil") {
				t.Errorf("expected nilfunc quiet, got diagnostics:\n%s", joined)
			}
			if tc.want != "" && !strings.Contains(joined, tc.want) {
				t.Errorf("expected diagnostic containing %q, got:\n%s", tc.want, joined)
			}
		})
	}
}
