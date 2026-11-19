// Package syscalls is the syscall role table of graalgo's executor
// (docs/arch/toolexec-design.md §5.6, W8a; abi-spike.md §2.3): table.txt
// is the single source of truth; this package parses it, generates the Java
// table from it (`go generate ./syscalls`) and checks it against the
// argument roles derived from the standard library's own call sites (Scan,
// Check) — the R15 gate: a wrong entry is memory-unsafe.
package syscalls

//go:generate go run ./gen

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed table.txt
var tableText string

// Kind is a role's kind.
type Kind int

const (
	Val Kind = iota
	CStr
	Bytes
	// Struct: a pointer to one pointer-free Go value of a named type and a
	// fixed size (an Rlimit, a Timespec): copied in, and back after a
	// successful call when the kernel writes it.
	Struct
)

// Dir is the direction of a buffer.
type Dir int

const (
	In Dir = iota
	Out
)

// Role is what one argument word is.
type Role struct {
	Kind Kind
	// Bytes: the 0-based index of the length argument, the direction and,
	// for Out, whether the kernel fills only r1 bytes.
	LenArg       int
	Dir          Dir
	FilledResult bool
	// Struct: the pointee type as the scan names it ("syscall.Rlimit") and
	// its size in bytes; Dir is the direction.
	Type string
	Size int64
}

func (r Role) String() string {
	switch r.Kind {
	case CStr:
		return "cstr"
	case Struct:
		d := "in"
		if r.Dir == Out {
			d = "out"
		}
		return fmt.Sprintf("struct(%s,%d,%s)", r.Type, r.Size, d)
	case Bytes:
		if r.Dir == In {
			return fmt.Sprintf("bytes(%d,in)", r.LenArg+1)
		}
		f := "all"
		if r.FilledResult {
			f = "result"
		}
		return fmt.Sprintf("bytes(%d,out,%s)", r.LenArg+1, f)
	}
	return "val"
}

// Entry is one system call.
type Entry struct {
	Nr       int
	Name     string
	MayBlock bool
	Roles    []Role
}

// Table is a parsed role table, sorted by number.
type Table struct {
	Entries []Entry
	// Child lists the functions (go/types full names) whose system calls
	// run only after a fork succeeded: their sites are not checked (see
	// table.txt, "afterfork").
	Child []string
}

// Default is the table compiled into this binary.
var Default = mustParse(tableText)

func mustParse(text string) *Table {
	t, err := Parse(text)
	if err != nil {
		panic(err)
	}
	return t
}

// Parse parses the table format of table.txt.
func Parse(text string) (*Table, error) {
	t := &Table{}
	seenNr := map[int]bool{}
	seenName := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		if j := strings.IndexByte(line, '#'); j >= 0 {
			line = line[:j]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		bad := func(format string, a ...any) error {
			return fmt.Errorf("table.txt:%d: %s", i+1, fmt.Sprintf(format, a...))
		}
		if f[0] == "afterfork" {
			if len(f) != 2 {
				return nil, bad("want afterfork FUNC")
			}
			t.Child = append(t.Child, f[1])
			continue
		}
		if len(f) < 3 {
			return nil, bad("want NR NAME BLOCK ROLE...")
		}
		nr, err := strconv.Atoi(f[0])
		if err != nil || nr < 0 {
			return nil, bad("bad number %q", f[0])
		}
		e := Entry{Nr: nr, Name: f[1]}
		switch f[2] {
		case "block":
			e.MayBlock = true
		case "-":
		default:
			return nil, bad("BLOCK is %q, want block or -", f[2])
		}
		if len(f)-3 > 6 {
			return nil, bad("more than six roles")
		}
		for _, r := range f[3:] {
			role, err := parseRole(r)
			if err != nil {
				return nil, bad("%v", err)
			}
			e.Roles = append(e.Roles, role)
		}
		for k, r := range e.Roles {
			if r.Kind != Bytes {
				continue
			}
			if r.LenArg >= len(e.Roles) || r.LenArg == k || e.Roles[r.LenArg].Kind != Val {
				return nil, bad("%s: length argument %d is not a val role of the entry", r, r.LenArg+1)
			}
		}
		if seenNr[nr] || seenName[e.Name] {
			return nil, bad("duplicate entry %d %s", nr, e.Name)
		}
		seenNr[nr], seenName[e.Name] = true, true
		t.Entries = append(t.Entries, e)
	}
	sort.Slice(t.Entries, func(i, j int) bool { return t.Entries[i].Nr < t.Entries[j].Nr })
	return t, nil
}

func parseRole(s string) (Role, error) {
	switch s {
	case "val":
		return Role{Kind: Val}, nil
	case "cstr":
		return Role{Kind: CStr}, nil
	}
	if strings.HasPrefix(s, "struct(") && strings.HasSuffix(s, ")") {
		p := strings.Split(s[len("struct("):len(s)-1], ",")
		if len(p) != 3 || p[0] == "" || strings.ContainsAny(p[0], " ()") {
			return Role{}, fmt.Errorf("%s: want struct(TYPE,SIZE,in) or struct(TYPE,SIZE,out)", s)
		}
		n, err := strconv.ParseInt(p[1], 10, 64)
		if err != nil || n < 1 || n > 4096 {
			return Role{}, fmt.Errorf("%s: bad size", s)
		}
		r := Role{Kind: Struct, Type: p[0], Size: n}
		switch p[2] {
		case "in":
		case "out":
			r.Dir = Out
		default:
			return Role{}, fmt.Errorf("%s: want struct(TYPE,SIZE,in) or struct(TYPE,SIZE,out)", s)
		}
		return r, nil
	}
	if !strings.HasPrefix(s, "bytes(") || !strings.HasSuffix(s, ")") {
		return Role{}, fmt.Errorf("unknown role %q", s)
	}
	p := strings.Split(s[len("bytes("):len(s)-1], ",")
	n, err := strconv.Atoi(p[0])
	if err != nil || n < 1 || n > 6 {
		return Role{}, fmt.Errorf("%s: bad length argument", s)
	}
	r := Role{Kind: Bytes, LenArg: n - 1}
	switch {
	case len(p) == 2 && p[1] == "in":
		r.Dir = In
	case len(p) == 3 && p[1] == "out" && (p[2] == "result" || p[2] == "all"):
		r.Dir = Out
		r.FilledResult = p[2] == "result"
	default:
		return Role{}, fmt.Errorf("%s: want bytes(LEN,in), bytes(LEN,out,result) or bytes(LEN,out,all)", s)
	}
	return r, nil
}
