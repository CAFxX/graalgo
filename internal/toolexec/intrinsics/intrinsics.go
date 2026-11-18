// Package intrinsics is the intrinsic registry of the graalgo backend
// (docs/arch/toolexec-design.md D9, §5.3): the table of Go symbols that
// are implemented in Java. registry.txt is the single source of truth; this
// package parses it for the linker (package link) and generates the Java
// dispatch table from it (`go generate ./intrinsics`), and a test fails
// when the checked-in Java file is stale.
package intrinsics

//go:generate go run ./gen

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed registry.txt
var registryText string

// Entry is one registered symbol.
type Entry struct {
	// Symbol is gc's symbol name: import path "." name.
	Symbol string
	// Class is the Java class (package graalgo.go.intrinsics) whose static
	// method implements the symbol.
	Class string
	// Sig is the canonical type key of the function type.
	Sig string
	// Replace: the symbol has a Go body that the intrinsic takes the place
	// of (gc's compiler intrinsics).
	Replace bool
}

// Table is a parsed registry.
type Table struct {
	Version int
	// Hash identifies the registry content (the first 8 bytes of SHA-256
	// over the canonical form of the entries).
	Hash    [8]byte
	Entries []Entry // sorted by symbol
	bySym   map[string]int
}

// Registry is the registry compiled into this binary.
var Registry = mustParse(registryText)

func mustParse(text string) *Table {
	t, err := Parse(text)
	if err != nil {
		panic(err)
	}
	return t
}

// Lookup returns the entry registered under symbol.
func (t *Table) Lookup(symbol string) (Entry, bool) {
	i, ok := t.bySym[symbol]
	if !ok {
		return Entry{}, false
	}
	return t.Entries[i], true
}

// Parse parses a registry file.
func Parse(text string) (*Table, error) {
	t := &Table{bySym: map[string]int{}}
	methods := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		if j := strings.IndexByte(line, '#'); j >= 0 {
			line = line[:j]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		fail := func(format string, a ...any) error {
			return fmt.Errorf("registry.txt:%d: %s", i+1, fmt.Sprintf(format, a...))
		}
		switch f[0] {
		case "version":
			if len(f) != 2 || t.Version != 0 {
				return nil, fail("want exactly one `version N`")
			}
			v, err := strconv.Atoi(f[1])
			if err != nil || v < 1 {
				return nil, fail("bad version %q", f[1])
			}
			t.Version = v
		case "intrinsic", "replace":
			if len(f) < 4 {
				return nil, fail("want `%s SYMBOL CLASS SIGNATURE`", f[0])
			}
			e := Entry{Symbol: f[1], Class: f[2], Sig: strings.Join(f[3:], " "), Replace: f[0] == "replace"}
			if !strings.Contains(e.Symbol, ".") {
				return nil, fail("symbol %q has no package", e.Symbol)
			}
			if !isJavaIdent(e.Class) {
				return nil, fail("class %q is not a Java identifier", e.Class)
			}
			if _, _, err := SplitSig(e.Sig); err != nil {
				return nil, fail("%v", err)
			}
			if _, dup := t.bySym[e.Symbol]; dup {
				return nil, fail("symbol %s registered twice", e.Symbol)
			}
			m := e.Class + "." + MethodName(e.Symbol)
			if other, dup := methods[m]; dup {
				return nil, fail("symbols %s and %s map to the same Java method %s", other, e.Symbol, m)
			}
			methods[m] = e.Symbol
			t.bySym[e.Symbol] = -1
			t.Entries = append(t.Entries, e)
		default:
			return nil, fail("unknown declaration %q", f[0])
		}
	}
	if t.Version == 0 {
		return nil, fmt.Errorf("registry.txt: no version")
	}
	sort.Slice(t.Entries, func(i, j int) bool { return t.Entries[i].Symbol < t.Entries[j].Symbol })
	h := sha256.New()
	fmt.Fprintf(h, "version %d\n", t.Version)
	for i, e := range t.Entries {
		t.bySym[e.Symbol] = i
		fmt.Fprintf(h, "%q %q %q %v\n", e.Symbol, e.Class, e.Sig, e.Replace)
	}
	copy(t.Hash[:], h.Sum(nil))
	return t, nil
}

func isJavaIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		letter := r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// MethodName is the Java method implementing symbol: every character
// outside [A-Za-z0-9] becomes '_'.
func MethodName(symbol string) string {
	b := []byte(symbol)
	for i, c := range b {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
			b[i] = '_'
		}
	}
	return string(b)
}

// SplitSig splits a function type key "func(P, ...) R" or
// "func(P, ...) (R, ...)" into its parameter and result type keys.
func SplitSig(sig string) (params, results []string, err error) {
	rest, ok := strings.CutPrefix(sig, "func(")
	if !ok {
		return nil, nil, fmt.Errorf("signature %q is not a function type key", sig)
	}
	end := matching(rest)
	if end < 0 {
		return nil, nil, fmt.Errorf("signature %q: unbalanced parentheses", sig)
	}
	params = splitList(rest[:end])
	res := strings.TrimSpace(rest[end+1:])
	switch {
	case res == "":
	case strings.HasPrefix(res, "(") && matching(res[1:]) == len(res)-2:
		results = splitList(res[1 : len(res)-1])
	default:
		results = []string{res}
	}
	return params, results, nil
}

// matching returns the index in s of the ')' that closes a parenthesis
// opened just before s, or -1.
func matching(s string) int {
	depth := 0
	for i, c := range s {
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if c == ')' {
					return i
				}
				return -1
			}
			depth--
		}
	}
	return -1
}

// splitList splits a comma-separated list of type keys at nesting depth 0.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}
