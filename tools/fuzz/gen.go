// Package main implements the M8 differential fuzzer for graalgo Phase 1.
//
// gen.go — template-based Go program generator. The generator is seeded and
// deterministic: generate(seed) always yields the same program. Every emitted
// program is (intended to be) inside the frontend/v1 scope
// (docs/frontend-v1.md §7) and deterministic at runtime: no map-iteration
// order in output, no time, no addresses, no unseeded randomness.
//
// If a generated program is rejected by gc it is a generator bug; the driver
// labels it GC_INVALID (not counted as a divergence).
package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

// Edge values chosen to stress wraparound, sign extension, and growth paths.
var intEdges = []string{
	"0", "1", "-1", "2", "3", "7", "42", "127", "128", "255", "256",
	"1000", "-1000", "1<<30", "-(1<<30)", "(1<<62)-1", "-(1<<62)",
}

var intNonZero = []string{
	"1", "-1", "2", "3", "7", "42", "127", "128", "255", "1000", "-1000", "1<<30",
}

var uint8Edges = []string{"0", "1", "127", "128", "255"}

var floatEdges = []string{
	"0.0", "1.5", "-2.25", "1e10", "1e-10", "3.14159", "-0.0",
}

var stringEdges = []string{
	`""`, `"a"`, `"hello"`, `"hello world"`, `"héllo"`, `"日本語"`,
	`"\x00\x01\x02"`, `"\xff\xfe"`, `"a\nb\tc"`,
}

// generator holds the RNG and the program under construction.
type generator struct {
	rng     *rand.Rand
	sb      strings.Builder
	stmts   []string // main-body statements, one entry per statement() call
	varSeq  int
	gotoSeq int
	pkgDecl []string // package-level declarations (const/var)
	helpers []string // helper functions emitted before main
	needGid bool     // generic identity helper
	needGmax bool    // generic max helper (comparable)
	needPair bool    // pair struct type
	needAcc  bool    // accumulator struct type with methods
}

func (g *generator) v() string {
	g.varSeq++
	return "v" + strconv.Itoa(g.varSeq)
}

func (g *generator) p(format string, args ...interface{}) {
	fmt.Fprintf(&g.sb, format+"\n", args...)
}

func (g *generator) pick(ss []string) string { return ss[g.rng.Intn(len(ss))] }

func (g *generator) intLit() string  { return g.pick(intEdges) }
func (g *generator) nzLit() string   { return g.pick(intNonZero) }
func (g *generator) u8Lit() string   { return g.pick(uint8Edges) }
func (g *generator) fLit() string    { return g.pick(floatEdges) }
func (g *generator) strLit() string  { return g.pick(stringEdges) }
func (g *generator) boolLit() string { return g.pick([]string{"true", "false"}) }

// generate builds one complete program for the given seed.
func generate(seed int64) string {
	pre, stmts, post := generateParts(seed)
	var out strings.Builder
	out.WriteString(pre)
	for _, s := range stmts {
		out.WriteString(s)
	}
	out.WriteString(post)
	return out.String()
}

// generateParts splits the program into the fixed prefix, the removable
// main-body statements, and the closing brace. Helpers and package decls are
// kept whole: unused functions/vars are legal Go, and every statement is
// self-contained (fresh variable names), so removing statements cannot break
// references.
func generateParts(seed int64) (pre string, stmts []string, post string) {
	g := &generator{rng: rand.New(rand.NewSource(seed))}
	n := 12 + g.rng.Intn(18) // statements in main
	for i := 0; i < n; i++ {
		mark := g.sb.Len()
		g.statement()
		g.stmts = append(g.stmts, g.sb.String()[mark:])
	}
	var out strings.Builder
	out.WriteString("package main\n\nimport \"fmt\"\n\n")
	for _, d := range g.pkgDecl {
		out.WriteString(d + "\n")
	}
	if len(g.pkgDecl) > 0 {
		out.WriteByte('\n')
	}
	for _, h := range g.helpers {
		out.WriteString(h + "\n")
	}
	if len(g.helpers) > 0 {
		out.WriteByte('\n')
	}
	out.WriteString("func main() {\n")
	return out.String(), g.stmts, "}\n"
}

func (g *generator) statement() {
	switch g.rng.Intn(24) {
	case 0:
		g.sArith()
	case 1:
		g.sCmp()
	case 2:
		g.sIf()
	case 3:
		g.sFor3()
	case 4:
		g.sForRange()
	case 5:
		g.sSwitchExpr()
	case 6:
		g.sSwitchType()
	case 7:
		g.sSlice()
	case 8:
		g.sMap()
	case 9:
		g.sStruct()
	case 10:
		g.sString()
	case 11:
		g.sClosure()
	case 12:
		g.sDefer()
	case 13:
		g.sPanicRecover()
	case 14:
		g.sGeneric()
	case 15:
		g.sMethod()
	case 16:
		g.sAssert()
	case 17:
		g.sFmtVerbs()
	case 18:
		g.sShadow()
	case 19:
		g.sGoto()
	case 20:
		g.sNamedRet()
	case 21:
		g.sBuiltin()
	case 22:
		g.sArray()
	case 23:
		g.sFloat()
	}
}
