package main

// L11 (REVIEW-2026-11-18-05): go/constant — zero corpus imports before
// this program. Constant values and operations: construction, kind,
// exact values, binary operations, comparisons, and conversions.

import (
	"fmt"
	"go/constant"
	"go/token"
)

func main() {
	i := constant.MakeInt64(42)
	j := constant.MakeInt64(7)
	s := constant.MakeString("hello")
	b := constant.MakeBool(true)
	u := constant.MakeUnknown()
	fmt.Printf("kinds int=%v str=%v bool=%v unknown=%v\n", i.Kind(), s.Kind(), b.Kind(), u.Kind())
	fmt.Printf("int=%v str=%q bool=%v\n", i, constant.StringVal(s), b)
	v, ok := constant.Int64Val(i)
	fmt.Printf("int64val=%d ok=%v\n", v, ok)
	sv := constant.StringVal(s)
	fmt.Printf("stringval=%q\n", sv)
	fmt.Printf("boolval=%v\n", constant.BoolVal(b))
	ops := []struct {
		op   token.Token
		x, y constant.Value
	}{
		{token.ADD, i, j},
		{token.SUB, i, j},
		{token.MUL, i, j},
		{token.QUO, i, j},
		{token.REM, i, j},
		{token.AND, i, j},
		{token.OR, i, j},
		{token.XOR, i, j},
	}
	for _, o := range ops {
		fmt.Printf("binop %v %v %v=%v\n", o.x, o.op, o.y, constant.BinaryOp(o.x, o.op, o.y))
	}
	fmt.Printf("unary-neg=%v not=%v\n",
		constant.UnaryOp(token.SUB, i, 0), constant.UnaryOp(token.NOT, b, 0))
	fmt.Printf("shift-left=%v shift-right=%v\n",
		constant.Shift(i, token.SHL, 2), constant.Shift(i, token.SHR, 2))
	fmt.Printf("cmp 42 vs 7=%v\n", constant.Compare(i, token.GTR, j))
	fmt.Printf("cmp 42 vs 42=%v\n", constant.Compare(i, token.EQL, i))
	fmt.Printf("cmp str=%v\n", constant.Compare(s, token.EQL, constant.MakeString("hello")))
	f := constant.MakeFloat64(2.5)
	fmt.Printf("float=%v kind=%v\n", f, f.Kind())
	fv, ok := constant.Float64Val(f)
	fmt.Printf("float64val=%v ok=%v\n", fv, ok)
	fmt.Printf("sign int=%d str=%d\n", constant.Sign(i), constant.Sign(constant.MakeInt64(-3)))
	fmt.Printf("int64of-float=%v\n", constant.Val(constant.MakeFromLiteral("100", token.INT, 0)))
}
