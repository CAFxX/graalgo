package syscalls

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
)

var (
	scanOnce  sync.Once
	scanSites []Site
	scanErr   error
)

// stdSites scans the standard library of the go command on PATH (the
// tracked release; the module's toolchain line pins it) once per test run.
func stdSites(t *testing.T) []Site {
	t.Helper()
	scanOnce.Do(func() {
		goCmd, err := exec.LookPath("go")
		if err != nil {
			scanErr = err
			return
		}
		scanSites, scanErr = Scan(goCmd)
	})
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	return scanSites
}

// TestTableMatchesSource is the R15 gate: the checked-in table agrees with
// the roles derived from the standard library's call sites.
func TestTableMatchesSource(t *testing.T) {
	sites := stdSites(t)
	if len(sites) < 100 {
		t.Fatalf("scan found %d raw system call sites; expected std's hundreds (did the scan break?)", len(sites))
	}
	for _, p := range Check(Default, sites) {
		t.Error(p)
	}
}

// TestCheckCatchesPlantedErrors: every planted wrong entry turns the gate
// red, with a message naming the defect.
func TestCheckCatchesPlantedErrors(t *testing.T) {
	sites := stdSites(t)
	planted := []struct{ name, line, want string }{
		{"pointer declared val", "0 read block val val val", "passes buf:byte at a2, declared val"},
		{"buffer length from the wrong argument", "0 read block val bytes(1,out,result) val", "is not a1 = uintptr(len(p))"},
		{"cstr declared bytes", "257 openat block val bytes(3,in) val val", "passes cstr at a2, declared bytes(3,in)"},
		{"value declared cstr", "8 lseek - cstr val val", "passes val at a1, declared cstr"},
		{"wrong number for the name", "1 read block val bytes(3,out,result) val", "as SYS_write, not read"},
		{"undeclared pointer", "257 openat block val", "passes cstr at a2, which the entry does not declare"},
		{"number std never issues", "335 nosuchcall -", "no call site"},
		{"stale afterfork exemption", "afterfork syscall.nosuchfunc", "stale exemption"},
		{"fork entry beside afterfork exemptions", "56 clone - val\nafterfork syscall.forkAndExecInChild1", "the exempted sites become reachable"},
		{"afterfork sites are checked without the exemption", "1 write block val bytes(3,in) val", "passes ptr:syscall.Errno at a2"},
		{"struct of the wrong size", "302 prlimit64 - val val struct(syscall.Rlimit,8,in) struct(syscall.Rlimit,16,out)", "syscall.Rlimit at a3 is 16 bytes, declared 8"},
		{"struct of the wrong type", "302 prlimit64 - val val struct(syscall.Timespec,16,in) struct(syscall.Rlimit,16,out)", "passes ptr:syscall.Rlimit at a3, declared struct(syscall.Timespec,16,in)"},
		{"struct declared val", "302 prlimit64 - val val val struct(syscall.Rlimit,16,out)", "passes ptr:syscall.Rlimit at a3, declared val"},
		{"struct pointer undeclared", "302 prlimit64 - val val struct(syscall.Rlimit,16,in)", "passes ptr:syscall.Rlimit at a4, which the entry does not declare"},
		{"selector passed a pointer", "0 read block val sel(1,2) val", "passes buf:byte at a2, declared sel(1,2)"},
		{"trap constant of another call", "262 unlinkat - val cstr struct(syscall.Stat_t,144,out) val", "as SYS_fstatattrap, not unlinkat"},
		{"struct with pointers", "46 sendmsg - val struct(syscall.Msghdr,56,in) val", "syscall.Msghdr at a2 holds pointers"},
	}
	for _, p := range planted {
		tab, err := Parse(p.line)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		problems := Check(tab, sites)
		found := false
		for _, pr := range problems {
			if strings.Contains(pr, p.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: planted %q, want a problem containing %q, got %q", p.name, p.line, p.want, problems)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, line := range []string{
		"x read block val",
		"0 read maybe val",
		"0 read block val bytes(2,out,result)",      // the length argument is the buffer itself
		"0 read block val bytes(7,in) val",          // out of range
		"0 read block val bytes(3,sideways) val",    // bad direction
		"0 read block val val val val val val val",  // seven roles
		"0 read block struct(syscall.Stat_t)",       // no size, no direction
		"0 read block struct(syscall.Rlimit,0,in)",  // bad size
		"0 read block struct(syscall.Rlimit,16,rw)", // bad direction
		"0 read block struct(,16,in)",               // no type
		"0 read block\n0 read block",                // duplicate
	} {
		if _, err := Parse(line); err == nil {
			t.Errorf("Parse(%q) accepted", line)
		}
	}
}

func TestJavaUpToDate(t *testing.T) {
	got, err := readJava("..")
	if err != nil {
		t.Fatal(err)
	}
	if got != Default.Java() {
		t.Fatalf("%s is stale: run `go generate ./syscalls` in internal/toolexec", JavaPath)
	}
}

// TestDumpSites writes every scanned site, sorted by number, to the file
// SYSCALLS_DUMP names (skipped without it): the source-derived roles a new
// table entry is written from.
func TestDumpSites(t *testing.T) {
	out := os.Getenv("SYSCALLS_DUMP")
	if out == "" {
		t.Skip("SYSCALLS_DUMP not set")
	}
	sites := append([]Site(nil), stdSites(t)...)
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].Nr < sites[j].Nr })
	var b strings.Builder
	for _, s := range sites {
		fmt.Fprintf(&b, "%d %s %s %s roles=%v buf=%q len=%q size=%v\n", s.Nr, s.Name, s.Func, s.Pos, s.Roles, s.Buf, s.Len, s.Size)
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
