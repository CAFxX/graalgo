package main

// gen_stmts.go — statement templates for the M8 fuzzer generator.
//
// Every template prints its observable values (more output = more differential
// signal) and is written to be valid Go by construction. Fresh variable names
// come from g.v() so there are no redeclaration collisions.

func (g *generator) sArith() {
	a, b, c := g.v(), g.v(), g.v()
	// literals go through variables first: constant expressions would be
	// compile-time evaluated (and overflow is a compile error), while
	// runtime values wrap — and wraparound is what we want to fuzz.
	t1, t2 := g.v(), g.v()
	op := g.pick([]string{"+", "-", "*", "/", "%"})
	d2 := g.intLit()
	if op == "/" || op == "%" {
		d2 = g.nzLit() // never divide by zero
	}
	g.p("%s := %s", t1, g.intLit())
	g.p("%s := %s", t2, d2)
	g.p("%s := %s %s %s", a, t1, op, t2)
	g.p("%s = %s", t1, g.intLit())
	g.p("%s = %s", t2, g.intLit())
	g.p("%s := %s * %s + %s", b, a, t1, t2)
	g.p("%s = %s", t2, g.nzLit())
	g.p("%s := (%s - %s) * (%s + 1)", c, b, a, t2)
	g.p("fmt.Println(%s, %s, %s)", a, b, c)
}

func (g *generator) sFloat() {
	a, b := g.v(), g.v()
	g.p("%s := %s", a, g.fLit())
	g.p("%s := %s + %s * %s", b, a, g.fLit(), g.fLit())
	g.p("fmt.Println(%s, %s)", a, b)
	// float64 -> int conversion truncates toward zero
	c := g.v()
	g.p("%s := int(%s)", c, b)
	g.p("fmt.Println(%s)", c)
}

func (g *generator) sCmp() {
	a := g.v()
	ops := []string{"==", "!=", "<", "<=", ">", ">="}
	g.p("%s := %s", a, g.boolLit())
	for i := 0; i < 3; i++ {
		x := g.v()
		g.p("%s := (%s %s %s) == %s", x, g.intLit(), g.pick(ops), g.intLit(), g.boolLit())
		g.p("fmt.Println(%s)", x)
	}
	g.p("%s = (%s && %s) || !(%s)", a, g.boolLit(), g.boolLit(), g.boolLit())
	g.p("fmt.Println(%s)", a)
}

func (g *generator) sIf() {
	c := g.v()
	g.p("%s := %s", c, g.intLit())
	g.p("if %s > 0 {", c)
	g.p("fmt.Println(%s * 2)", c)
	g.p("} else if %s < 0 {", c)
	g.p("fmt.Println(%s * -3)", c)
	g.p("} else {")
	g.p(`fmt.Println("zero")`)
	g.p("}")
	// if with init statement
	x := g.v()
	g.p("if %s := %s %% 7; %s == 0 {", x, g.intLit(), x)
	g.p(`fmt.Println("div7")`)
	g.p("} else {")
	g.p("fmt.Println(%s)", x)
	g.p("}")
}

func (g *generator) sFor3() {
	acc, i := g.v(), g.v()
	lim := g.pick([]string{"5", "10", "17"})
	g.p("%s := 0", acc)
	g.p("for %s := 0; %s < %s; %s++ {", i, i, lim, i)
	g.p("%s += %s * %s", acc, i, g.nzLit())
	// break/continue exercise
	g.p("if %s %% 11 == 0 { continue }", i)
	g.p("if %s > 1000000 { break }", acc)
	g.p("}")
	g.p("fmt.Println(%s)", acc)
}

func (g *generator) sForRange() {
	s, acc := g.v(), g.v()
	kind := g.rng.Intn(4)
	switch kind {
	case 0: // slice with index+value
		g.p("%s := []int{%s, %s, %s, %s}", s, g.intLit(), g.intLit(), g.intLit(), g.intLit())
		g.p("%s := 0", acc)
		i, x := g.v(), g.v()
		g.p("for %s, %s := range %s { %s += %s * %s }", i, x, s, acc, i, x)
		g.p("fmt.Println(%s)", acc)
	case 1: // array, value only
		g.p("%s := [3]int64{%s, %s, %s}", s, g.intLit(), g.intLit(), g.intLit())
		g.p("%s := int64(0)", acc)
		x := g.v()
		g.p("for _, %s := range %s { %s += %s }", x, s, acc, x)
		g.p("fmt.Println(%s)", acc)
	case 2: // string: index + rune
		g.p("%s := %s", s, g.strLit())
		g.p("%s := 0", acc)
		i, r := g.v(), g.v()
		g.p("for %s, %s := range %s { %s += %s + int(%s) }", i, r, s, acc, i, r)
		g.p("fmt.Println(%s)", acc)
	case 3: // map: order-independent aggregation only (fixed distinct keys)
		g.p("%s := map[string]int{%s, %s, %s}", s,
			`"ka": `+g.intLit(), `"kb": `+g.intLit(), `"kc": `+g.intLit())
		g.p("%s := 0", acc)
		x := g.v()
		g.p("for _, %s := range %s { %s += %s }", x, s, acc, x)
		g.p("fmt.Println(len(%s), %s)", s, acc)
	}
}

func (g *generator) sSwitchExpr() {
	a, r := g.v(), g.v()
	g.p("%s := %s", a, g.intLit())
	g.p("switch %s {", a)
	g.p("case 0:")
	g.p(`fmt.Println("zero")`)
	g.p("case 1, 2, 3:")
	g.p("fmt.Println(%s * 10)", a)
	// fallthrough exercise
	g.p("case 42:")
	g.p(`fmt.Println("pre-fall")`)
	g.p("fallthrough")
	g.p("case 43:")
	g.p(`fmt.Println("fell")`)
	g.p("default:")
	g.p("fmt.Println(%s)", a)
	g.p("}")
	// switch with init + no tag
	g.p("switch %s := %s %% 3; {", r, a)
	g.p("case %s < 0:", r)
	g.p(`fmt.Println("neg")`)
	g.p("case %s == 0:", r)
	g.p(`fmt.Println("mod0")`)
	g.p("default:")
	g.p("fmt.Println(%s)", r)
	g.p("}")
}

func (g *generator) sSwitchType() {
	a := g.v()
	choice := g.rng.Intn(4)
	var val string
	switch choice {
	case 0:
		val = g.intLit()
	case 1:
		val = g.strLit()
	case 2:
		val = "[]int{" + g.intLit() + "}"
	default:
		val = "nil"
	}
	g.p("var %s any = %s", a, val)
	g.p("switch t := %s.(type) {", a)
	g.p("case nil:")
	g.p(`fmt.Println("nil")`)
	g.p("case int:")
	g.p("fmt.Println(t * 2)")
	g.p("case string:")
	g.p("fmt.Println(len(t))")
	g.p("case []int:")
	g.p("fmt.Println(len(t))")
	g.p("default:")
	g.p(`fmt.Println("other")`)
	g.p("}")
}

func (g *generator) sSlice() {
	s := g.v()
	switch g.rng.Intn(6) {
	case 0: // append growth + spread
		g.p("%s := []int{%s}", s, g.intLit())
		for i := 0; i < 3; i++ {
			g.p("%s = append(%s, %s, %s)", s, s, g.intLit(), g.intLit())
		}
		t := g.v()
		g.p("%s := []int{%s, %s}", t, g.intLit(), g.intLit())
		g.p("%s = append(%s, %s...)", s, s, t)
		g.p("fmt.Println(%s, len(%s), cap(%s))", s, s, s)
	case 1: // overlap: append(s[:2], s...) — the R2 P0-1 regression shape
		g.p("%s := []int{0, 1, 2, 3, 4, 5, 6, 7}", s)
		g.p("%s = append(%s[:2], %s...)", s, s, s)
		g.p("fmt.Println(%s)", s)
	case 2: // slicing 2- and 3-index
		g.p("%s := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}", s)
		a, b := g.v(), g.v()
		g.p("%s := %s[2:7]", a, s)
		g.p("%s := %s[1:8:9]", b, s)
		g.p("fmt.Println(%s, %s, len(%s), cap(%s))", a, b, b, b)
	case 3: // copy
		g.p("%s := []int{1, 2, 3, 4, 5}", s)
		d, n := g.v(), g.v()
		g.p("%s := make([]int, 3)", d)
		g.p("%s := copy(%s, %s)", n, d, s)
		g.p("fmt.Println(%s, %s)", d, n)
	case 4: // zero-arg append identity (R1 P1-2 regression shape)
		g.p("var %s []int", s)
		t := g.v()
		g.p("%s := append(%s)", t, s)
		g.p("fmt.Println(%s == nil, len(%s))", t, t)
	case 5: // nil handling
		g.p("var %s []int", s)
		g.p("fmt.Println(%s == nil, len(%s), cap(%s))", s, s, s)
	}
}

func (g *generator) sArray() {
	a := g.v()
	depth := g.rng.Intn(3) // 0..2 -> up to [2][2][2]int (R2 P0-2 regression shape)
	switch depth {
	case 0:
		g.p("%s := [4]int{%s, %s, %s, %s}", a, g.intLit(), g.intLit(), g.intLit(), g.intLit())
		g.p("fmt.Println(%s, %s[2])", a, a)
	case 1:
		g.p("%s := [2][2]int{{%s, %s}, {%s, %s}}", a, g.intLit(), g.intLit(), g.intLit(), g.intLit())
		g.p("fmt.Println(%s, %s[1][0])", a, a)
	case 2:
		g.p("%s := [2][2][2]int{{{1, 2}, {3, 4}}, {{5, 6}, {7, 8}}}", a)
		g.p("fmt.Println(%s, %s[1][0][1])", a, a)
	}
}

func (g *generator) sMap() {
	m := g.v()
	g.p("%s := map[string]int{%s, %s}", m, `"mk1": `+g.intLit(), `"mk2": `+g.intLit())
	x, ok := g.v(), g.v()
	g.p("%s, %s := %s[%s]", x, ok, m, g.strLit())
	g.p("fmt.Println(%s, %s, len(%s))", x, ok, m)
	g.p("delete(%s, %s)", m, g.strLit())
	g.p("fmt.Println(len(%s))", m)
	// nested map
	n := g.v()
	g.p("%s := map[string]map[string]int{%s: {%s: %s}}", n, g.strLit(), g.strLit(), g.intLit())
	g.p("fmt.Println(%s)", n)
}

func (g *generator) sStruct() {
	if !g.needPair {
		g.needPair = true
		g.helpers = append(g.helpers, "type fzPair struct {\n\tA int\n\tB string\n}\n")
	}
	p := g.v()
	style := g.rng.Intn(3)
	switch style {
	case 0:
		g.p("%s := fzPair{%s, %s}", p, g.intLit(), g.strLit())
	case 1:
		g.p("%s := fzPair{A: %s, B: %s}", p, g.intLit(), g.strLit())
	default:
		g.p("%s := &fzPair{%s, %s}", p, g.intLit(), g.strLit())
	}
	g.p("fmt.Println(%s, %s.A, %s.B)", p, p, p)
	g.p("%s.A += %s", p, g.nzLit())
	g.p("fmt.Println(%s.A)", p)
}

func (g *generator) sString() {
	s := g.v()
	switch g.rng.Intn(5) {
	case 0:
		g.p("%s := %s", s, g.strLit())
		g.p("fmt.Println(len(%s))", s)
		i := g.v()
		g.p("if len(%s) > 0 { %s := %s[0]; fmt.Println(%s) }", s, i, s, i)
	case 1:
		g.p("%s := %s + %s", s, g.strLit(), g.strLit())
		g.p("fmt.Println(%s)", s)
	case 2: // []byte <-> string round trip
		g.p("%s := []byte(%s)", s, g.strLit())
		t := g.v()
		g.p("%s := string(%s)", t, s)
		g.p("fmt.Println(%s, len(%s))", t, t)
	case 3: // []rune incl. invalid -> U+FFFD (R1 P1-3 regression shape)
		g.p("%s := []rune{%s, 0xD800, 0x110000, 65}", s, g.pick([]string{"65", "128", "8364", "0", "-5"}))
		t := g.v()
		g.p("%s := string(%s)", t, s)
		g.p("fmt.Println(%s)", t)
	case 4: // string slicing
		g.p("%s := %s", s, g.strLit())
		t := g.v()
		g.p("if len(%s) >= 2 { %s := %s[1:]; fmt.Println(%s) }", s, t, s, t)
	}
}

func (g *generator) sClosure() {
	// Loop capture via immediately-invoked closures (the M5 loopvar model).
	// Calling through a func-typed variable is WS D (out of scope), so
	// closures are invoked immediately; each iteration must still capture
	// its own loop variable.
	res := g.v()
	g.p("%s := []int{}", res)
	g.p("for i := 0; i < 4; i++ {")
	g.p("%s = append(%s, func() int { return i * %s }())", res, res, g.nzLit())
	g.p("}")
	g.p("fmt.Println(%s)", res)
	// nested closure + transitive capture, immediately invoked
	r := g.v()
	x := g.intLit()
	g.p("%s := func() int {", r)
	g.p("y := %s", x)
	g.p("return func() int { return y * %s }()", g.nzLit())
	g.p("}()")
	g.p("fmt.Println(%s)", r)
	// closure mutating captured variable
	c, out := g.v(), g.v()
	g.p("%s := %s", c, g.intLit())
	g.p("%s := func() int { %s += %s; return %s }()", out, c, g.nzLit(), c)
	g.p("fmt.Println(%s, %s)", c, out)
}

func (g *generator) sDefer() {
	// LIFO order + arg evaluation at defer time
	g.p("for i := 0; i < 3; i++ {")
	g.p("defer fmt.Println(%s)", `"defer"`)
	g.p("defer fmt.Println(i)")
	g.p("}")
	x := g.v()
	g.p("%s := %s", x, g.intLit())
	g.p("defer func() { fmt.Println(%s * 2) }()", x)
	g.p("fmt.Println(%s)", x)
}

func (g *generator) sPanicRecover() {
	// recover in deferred closure; program continues
	g.p("func() {")
	g.p("defer func() {")
	g.p("if r := recover(); r != nil {")
	g.p("fmt.Println(%s, r)", `"recovered"`)
	g.p("}")
	g.p("}()")
	v := g.intLit()
	g.p("if %s > 100 { panic(%s) }", v, g.strLit())
	g.p(`fmt.Println("no panic")`)
	g.p("}()")
}

func (g *generator) sGeneric() {
	if !g.needGid {
		g.needGid = true
		g.helpers = append(g.helpers,
			"func fzIdent[T any](x T) T { return x }\n")
	}
	if !g.needGmax {
		g.needGmax = true
		g.helpers = append(g.helpers,
			"func fzMax[T comparable](a, b T) T {\n\tif a == b {\n\t\treturn a\n\t}\n\treturn b\n}\n")
	}
	a, b, c := g.v(), g.v(), g.v()
	// multiple instantiations incl. slice type
	g.p("%s := fzIdent(%s)", a, g.intLit())
	g.p("%s := fzIdent(%s)", b, g.strLit())
	g.p("%s := fzIdent([]int{%s, %s})", c, g.intLit(), g.intLit())
	g.p("fmt.Println(%s, %s, %s)", a, b, c)
	d := g.v()
	g.p("%s := fzMax(%s, %s)", d, g.intLit(), g.intLit())
	g.p("fmt.Println(%s)", d)
}

func (g *generator) sMethod() {
	if !g.needAcc {
		g.needAcc = true
		g.helpers = append(g.helpers,
			"type fzAcc struct{ N int }\n",
			"func (a fzAcc) Get() int { return a.N }\n",
			"func (a fzAcc) Add(d int) int { return a.N + d }\n")
	}
	a, r := g.v(), g.v()
	g.p("%s := fzAcc{%s}", a, g.intLit())
	g.p("%s := %s.Add(%s)", r, a, g.intLit())
	g.p("fmt.Println(%s.Get(), %s)", a, r)
}

func (g *generator) sAssert() {
	a := g.v()
	choice := g.rng.Intn(3)
	var val string
	switch choice {
	case 0:
		val = g.intLit()
	case 1:
		val = g.strLit()
	default:
		val = "[]int{" + g.intLit() + "}"
	}
	g.p("var %s any = %s", a, val)
	x, ok := g.v(), g.v()
	g.p("%s, %s := %s.(int)", x, ok, a)
	g.p("fmt.Println(%s, %s)", x, ok)
	// comma-ok on the other candidate types (never panics)
	z, ok2 := g.v(), g.v()
	g.p("%s, %s := %s.(string)", z, ok2, a)
	g.p("fmt.Println(%s, %s)", z, ok2)
	w, ok3 := g.v(), g.v()
	g.p("%s, %s := %s.([]int)", w, ok3, a)
	g.p("fmt.Println(%s, %s)", w, ok3)
}

func (g *generator) sFmtVerbs() {
	g.p("fmt.Printf(%s, %s, %s, %s, %s)", `"%d %s %v %x\n"`, g.intLit(), g.strLit(), g.intLit(), g.intLit())
	g.p("fmt.Printf(%s, %s, %s, %s)", `"%f %t %q\n"`, g.fLit(), g.boolLit(), g.strLit())
	g.p("fmt.Println(fmt.Sprint(%s, %s))", g.intLit(), g.strLit())
	g.p("fmt.Println(fmt.Sprintf(%s, %s, %s))", `"%d-%d"`, g.intLit(), g.intLit())
}

func (g *generator) sShadow() {
	x := g.v()
	g.p("%s := %s", x, g.intLit())
	g.p("{")
	y := g.v()
	g.p("%s := %s", y, g.strLit())
	g.p("fmt.Println(%s, %s)", x, y)
	g.p("%s := %s", x, g.intLit()) // shadows in inner block
	g.p("fmt.Println(%s)", x)
	g.p("}")
	g.p("fmt.Println(%s)", x)
	_ = y
}

func (g *generator) sGoto() {
	// forward goto only, no declarations crossed; dedicated counter so
	// back-to-back gotos get unique labels
	g.gotoSeq++
	n := g.gotoSeq
	g.p("goto fzEnd%d", n)
	g.p(`fmt.Println("skipped")`)
	g.p("fzEnd%d:", n)
	g.p(`fmt.Println("landed")`)
}

func (g *generator) sNamedRet() {
	// named returns modified by defer
	g.p("func() {")
	g.p("f := func() (n int) {")
	g.p("defer func() { n += %s }()", g.nzLit())
	g.p("n = %s", g.intLit())
	g.p("return")
	g.p("}()")
	g.p("fmt.Println(f)")
	g.p("}()")
}

func (g *generator) sBuiltin() {
	// Only builtins in the v1 surface (freeze-stub.md "Builtin nodes").
	// clear/min/max are WS G scope errors — the generator stays in scope;
	// the scope-error path is covered by MainTest.testCheckRejectsClearBuiltin.
	// R1#7: `*p = x` (deref-assign) is WS D — use only in-scope `*p` reads.
	switch g.rng.Intn(3) {
	case 0: // new
		p := g.v()
		g.p("%s := new(int)", p)
		g.p("fmt.Println(*%s)", p)
	case 1: // make map + len
		m := g.v()
		g.p("%s := make(map[int]string, 4)", m)
		g.p("%s[%s] = %s", m, g.intLit(), g.strLit())
		g.p("fmt.Println(len(%s))", m)
	case 2: // make slice + cap
		s := g.v()
		g.p("%s := make([]int, %s, %s)", s, g.pick([]string{"0", "3"}), g.pick([]string{"4", "8"}))
		g.p("fmt.Println(len(%s), cap(%s))", s, s)
	}
}
