package intrinsics

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// bugsMD is the bug register temporary entries cite.
const bugsMD = "../../../docs/dev/bugs.md"

// bugRowRE is a row of the register's table: id, then the remaining cells.
var bugRowRE = regexp.MustCompile(`^\|\s*(BUG-[0-9]{4})\s*\|(.*)\|\s*$`)

// fixedRE is a "Retired by" cell of a fixed bug (docs/dev/bugs.md).
var fixedRE = regexp.MustCompile(`(?i)^\s*(fixed|closed)\b`)

// checkTemporary reports every TEMPORARY entry of tab whose bug is not an
// open row of the register text: missing, or marked fixed.
func checkTemporary(tab *Table, register string) []string {
	retired := map[string]string{} // id -> last cell
	for _, line := range strings.Split(register, "\n") {
		if m := bugRowRE.FindStringSubmatch(line); m != nil {
			cells := strings.Split(m[2], "|")
			retired[m[1]] = cells[len(cells)-1]
		}
	}
	var errs []string
	for _, e := range tab.Entries {
		if e.Temporary == "" {
			continue
		}
		last, ok := retired[e.Temporary]
		switch {
		case !ok:
			errs = append(errs, e.Symbol+": TEMPORARY cites "+e.Temporary+", which docs/dev/bugs.md does not list")
		case fixedRE.MatchString(last):
			errs = append(errs, e.Symbol+": TEMPORARY cites "+e.Temporary+", marked fixed in docs/dev/bugs.md — delete the entry (or reopen the bug)")
		}
	}
	return errs
}

// TestTemporaryEntries: every temporary registry entry cites an open bug.
func TestTemporaryEntries(t *testing.T) {
	b, err := os.ReadFile(bugsMD)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range checkTemporary(Registry, string(b)) {
		t.Error(e)
	}
}

// TestTemporaryCheckFails plants each violation the check guards.
func TestTemporaryCheckFails(t *testing.T) {
	reg := "version 1\nreplace a.F C func() # TEMPORARY (BUG-0002)\nreplace a.G C func()\n"
	tab, err := Parse(reg)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := tab.Lookup("a.F"); e.Temporary != "BUG-0002" {
		t.Fatalf("a.F: Temporary = %q", e.Temporary)
	}
	if e, _ := tab.Lookup("a.G"); e.Temporary != "" {
		t.Fatalf("a.G: Temporary = %q", e.Temporary)
	}
	header := "| Id | Bug | Evidence | Retired by |\n|---|---|---|---|\n"
	for register, want := range map[string]int{
		header + "| BUG-0002 | b | e | the fix |\n":                        0,
		header + "| BUG-0001 | b | e | the fix |\n":                        1, // missing
		header + "| BUG-0002 | b | e | fixed by abc123 (2026-11-19) |\n":   1,
		header + "| BUG-0002 | b | e | Closed by an earlier fix |\n":       1,
		header + "| BUG-0002 | b | fixed in evidence only | the fix |\n":   0, // only the last cell counts
		header + "| BUG-0002 | b | e | WP-X, then fixed once it lands |\n": 0,
	} {
		if got := checkTemporary(tab, register); len(got) != want {
			t.Errorf("register %q: %d errors %v, want %d", register, len(got), got, want)
		}
	}
	// A marker must sit on a declaration and be well formed.
	for _, bad := range []string{
		"version 1\n# TEMPORARY (BUG-0002)\nreplace a.F C func()\n",
		"version 1\nreplace a.F C func() # TEMPORARY BUG-0002\n",
		"version 1\nreplace a.F C func() # TEMPORARY (BUG-2)\n",
		"version 1 # TEMPORARY (BUG-0002)\n",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse accepted %q", bad)
		}
	}
	// The marker does not change the registry's identity.
	plain, _ := Parse("version 1\nreplace a.F C func()\nreplace a.G C func()\n")
	if plain.Hash != tab.Hash {
		t.Errorf("the TEMPORARY marker changed the registry hash")
	}
}
