package main

// Exercises the full v1 straight-line scope: var declarations with and
// without explicit type, all integer binary operators, unary operators,
// parentheses, op-assignment, ++, and bare return (statements after return
// must not execute).
func main() {
	var a int = 20
	var b int
	b = a + 22
	b++
	b += 1
	c := (b - 2) * 2
	d := -c
	e := d / -4
	f := e % 3
	g := f & 7
	h := g | 8
	i := h ^ 3
	j := i << 2
	k := j >> 1
	m := ^k
	n := k &^ 15
	p := +n
	q := p
	q -= 1
	sum := a + b + c + d + e + f + g + h + i + j + k + m + n + p + q
	sum = sum // read sum (M2 observability idiom; see straight01.go)
	return
	sum = -1 // unreachable: must not execute (ReturnSignal check)
}
