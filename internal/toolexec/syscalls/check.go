package syscalls

import (
	"fmt"
	"strconv"
)

// Check verifies t against the standard library's call sites (Scan) and
// returns one line per problem (none: the table is consistent with the
// source). The rules, each guarding a memory-safety property of the
// executor (risk R15):
//
//   - every entry has at least one site with its number, and every such
//     site names the entry's SYS_ constant (a wrong number would apply the
//     roles to another system call);
//   - at every argument position of every site, the site passes what the
//     entry declares: val for val (a word declared as a value but passed
//     a pointer would reach the kernel as a handle integer), cstr for cstr,
//     a byte buffer for bytes;
//   - a bytes role's length argument is uintptr(len(b)) of the very slice
//     the pointer points into (a length taken from elsewhere could run past
//     the Go object);
//   - a site's argument beyond the entry's roles is a value;
//   - sites inside the table's afterfork functions are skipped, which is sound
//     only while the table has no fork entry (checked), and every afterfork
//     exemption still has sites (checked: no stale exemption).
func Check(t *Table, sites []Site) []string {
	var problems []string
	child := map[string]int{}
	for _, f := range t.Child {
		child[f] = 0
	}
	byNr := map[int64][]Site{}
	for _, s := range sites {
		if n, ok := child[s.Func]; ok {
			child[s.Func] = n + 1
			continue
		}
		byNr[s.Nr] = append(byNr[s.Nr], s)
	}
	for _, f := range t.Child {
		if child[f] == 0 {
			problems = append(problems, fmt.Sprintf("afterfork %s: no raw system call site in that function (stale exemption)", f))
		}
	}
	if len(t.Child) > 0 {
		// The exemption is sound only while no process can be forked: a
		// child function's sites are reached after clone/vfork, which then
		// fail loudly as numbers missing from the table.
		for _, e := range t.Entries {
			switch e.Name {
			case "clone", "clone3", "vfork", "fork":
				problems = append(problems, fmt.Sprintf("%d %s: a fork entry with afterfork exemptions in the table: the exempted sites become reachable", e.Nr, e.Name))
			}
		}
	}
	bad := func(e Entry, format string, a ...any) {
		problems = append(problems, fmt.Sprintf("%d %s: %s", e.Nr, e.Name, fmt.Sprintf(format, a...)))
	}
	for _, e := range t.Entries {
		ss := byNr[int64(e.Nr)]
		if len(ss) == 0 {
			bad(e, "no call site in the standard library issues system call %d: the entry is not derived from source", e.Nr)
			continue
		}
		for _, s := range ss {
			if s.Name != e.Name && s.Name != e.Name+"trap" { // internal/syscall/unix's fooTrap constants
				bad(e, "%s issues %d as SYS_%s, not %s", s.Pos, e.Nr, s.Name, e.Name)
			}
			for i, got := range s.Roles {
				arg := "a" + strconv.Itoa(i+1)
				if i >= len(e.Roles) {
					if got != "val" {
						bad(e, "%s passes %s at %s, which the entry does not declare", s.Pos, got, arg)
					}
					continue
				}
				r := e.Roles[i]
				switch r.Kind {
				case Val:
					if got != "val" {
						bad(e, "%s passes %s at %s, declared val", s.Pos, got, arg)
					}
				case CStr:
					if got != "cstr" {
						bad(e, "%s passes %s at %s, declared cstr", s.Pos, got, arg)
					}
				case Bytes:
					if got == "byteptr-param" {
						// A caller's *byte with a caller's length (readlen): the static tie is
						// not derivable; the length word must be a value, and the executor
						// bounds it by the Go object at run time.
						if r.LenArg >= len(s.Roles) || s.Roles[r.LenArg] != "val" {
							bad(e, "%s: the length of the *byte buffer at %s is not a value", s.Pos, arg)
						}
						continue
					}
					if got != "buf:byte" && got != "buf:uint8" {
						bad(e, "%s passes %s at %s, declared %s", s.Pos, got, arg, r)
						continue
					}
					if r.LenArg >= len(s.Len) || s.Buf[i] == "" || s.Len[r.LenArg] != s.Buf[i] {
						l := ""
						if r.LenArg < len(s.Len) {
							l = s.Len[r.LenArg]
						}
						bad(e, "%s: the length of the buffer %s at %s is not a%d = uintptr(len(%s)) (a%d is len(%s))",
							s.Pos, s.Buf[i], arg, r.LenArg+1, s.Buf[i], r.LenArg+1, l)
					}
				}
			}
			if len(s.Roles) < len(e.Roles) {
				bad(e, "%s passes %d arguments, the entry declares %d", s.Pos, len(s.Roles), len(e.Roles))
			}
		}
	}
	return problems
}
