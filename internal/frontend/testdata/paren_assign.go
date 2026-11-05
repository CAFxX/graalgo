package main

// Parenthesized assignment targets (review-1105-slog-parenassign):
// parentheses are transparent in Go, so every parenthesized form below
// must export as its operative lhs shape. Covers the plain-assign path
// (exportAssignable) and the multi-result desugar path (desugarExtract),
// including the log/slog closeGroup shape (*s.prefix) = (*s.prefix)[:...].
import "strconv"

type S struct {
	p *int
	a []int
}

func assignForms(s *S, x int) int {
	(x) = 3
	(x) = 4
	(s.a[0]) = 5
	(*(s.p)) = 6
	*(s.p) = 7
	(x)++
	(_) = 8
	_ = x
	return s.a[0] + *s.p
}

func mrForm(a []int, s string) (int, error) {
	var err error
	(a[0]), err = strconv.Atoi(s)
	return a[0], err
}

type T struct {
	prefix *[]string
}

func (t *T) closeGroup() {
	(*t.prefix) = (*t.prefix)[:len(*t.prefix)-1]
}

func main() {
	v := 0
	s := &S{p: &v, a: []int{0}}
	_ = assignForms(s, 0)
	a := []int{0}
	_, _ = mrForm(a, "42")
	xs := []string{"a", "b"}
	t := &T{prefix: &xs}
	t.closeGroup()
}
