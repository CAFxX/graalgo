// Command toolexec-log is the -toolexec interception harness for graalgo
// spike 5 (cmd/go coupling), testing the study's recommended driver
// architecture: stock go distribution with the backend swapped behind the
// existing -toolexec seam.
//
// For every tool invocation the driver prefixes with the wrapper, it logs a
// record (timestamp, pid, tool path, TOOLEXEC_IMPORTPATH, working directory,
// full argv with @response-file expansion) to $GRAALGO_WRAPPER_LOG, then
// execs the real tool with argv and environment unchanged. Observable
// behavior (stdout/stderr/exit code) is therefore identical to stock; the
// log is the only side channel.
//
// The log is append-only with an exclusive flock around each record so
// parallel driver actions do not interleave. The lock is released before
// exec, so tool execution itself is never serialized by the harness.
//
// Version history:
//
//	v1: initial wrapper (no PATH lookup for bare tool names).
//	v2: v1 + exec.LookPath for slash-less argv[1]. The driver probes the
//	    host C compiler as bare `gcc -### ...` *through* the wrapper
//	    (cmd/go/internal/work/buildid.go:221); syscall.Exec does no PATH
//	    lookup, so without this the probe fails (the driver degrades
//	    deterministically, hashing "CC ID ERROR=..." — correct but with
//	    different cache keys than stock).
//	v3: v2 + @response-file expansion. The driver rewrites long tool
//	    command lines to `tool @file` (passLongArgsInResponseFiles,
//	    cmd/go/internal/work/exec.go) — but useResponseFile keys off the
//	    *wrapper's* basename, so the wrapper binary must be named (or
//	    reachable under a name) like one of compile/link/cgo/asm/cover/pack
//	    for the driver to use response files at all (else: E2BIG,
//	    see spike finding F6). When invoked as [wrapper @file], the wrapper
//	    reads the file to discover the real tool, then execs
//	    [tool @file] so the tool expands it itself (preserving the
//	    driver's intent; the file is deleted by the driver after we exit).
//	    ParseArgs below is copied verbatim from go1.27.1's
//	    cmd/internal/objabi/flag.go — it is the exact parser the tools use
//	    and is documented there as the inverse of cmd/go's encodeArg.
//	v4: v3 + in-place @file rewrite dropping the duplicated tool path.
//	    The driver's response file contains cmd.Args[1:] = [tool, args...];
//	    passing it through unchanged makes the tool see the tool path as a
//	    spurious extra argv[1]. v4 rewrites the file to [args...] (using a
//	    verbatim copy of the driver's encodeArg) before exec'ing
//	    [tool @file].
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "toolexec-log: no tool argv\n")
		os.Exit(2)
	}
	raw := os.Args[1:]
	tool, toolArgs, expandNote := resolveTool(raw)
	logInvocation(raw, tool, toolArgs, expandNote)
	// The driver may pass a bare tool name (no slash): buildid.go:221
	// probes the host C compiler as `gcc -### ...` *through* the wrapper.
	// exec.Command would resolve it via PATH lookup; syscall.Exec does
	// not, so replicate the lookup here.
	if !strings.Contains(tool, "/") {
		if resolved, err := exec.LookPath(tool); err == nil {
			tool = resolved
		}
	}
	// Replace ourselves with the real tool: argv, env, cwd, stdio and
	// exit status are exactly what the driver would have gotten without
	// the wrapper.
	argv := append([]string{tool}, toolArgs...)
	if err := syscall.Exec(tool, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "toolexec-log: exec %s: %v\n", tool, err)
		os.Exit(1)
	}
}

// resolveTool maps the wrapper's argv to the real tool invocation.
// Normally argv is [tool args...]. When the driver used a response file,
// argv is [@file]: the file is read (with the tools' own response-file
// parser) to discover the real tool.
//
// CRITICAL SUBTLETY (spike finding F6): the driver writes cmd.Args[1:] into
// the response file, and with -toolexec set, cmd.Args[1] is the TOOL PATH
// itself — so the file contains [tool, args...]. If the wrapper execs
// [tool @file] unchanged, the tool expands @file and sees the tool path as
// a spurious extra argv[1] (for cgo: it tries to process the tool binary as
// a Go source file). The wrapper therefore rewrites the file IN PLACE,
// dropping the tool path, so the tool sees exactly [toolBinary, args...].
// In-place rewrite is clean: the driver deletes the file after the wrapper
// (and the tool it becomes) exits.
func resolveTool(argv []string) (tool string, toolArgs []string, note string) {
	if !strings.HasPrefix(argv[0], "@") || len(argv[0]) == 1 {
		return argv[0], argv[1:], ""
	}
	path := argv[0][1:]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "toolexec-log: cannot read response file %s: %v\n", path, err)
		os.Exit(1)
	}
	expanded := ParseArgs(data)
	// Handle (unlikely) nesting: a response file whose first arg is
	// another @file.
	for depth := 0; depth < 4 && len(expanded) > 0 && strings.HasPrefix(expanded[0], "@") && len(expanded[0]) > 1; depth++ {
		data, err = os.ReadFile(expanded[0][1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "toolexec-log: cannot read nested response file %s: %v\n", expanded[0][1:], err)
			os.Exit(1)
		}
		expanded = ParseArgs(data)
	}
	if len(expanded) == 0 {
		fmt.Fprintf(os.Stderr, "toolexec-log: empty response file %s\n", path)
		os.Exit(1)
	}
	tool = expanded[0]
	rest := expanded[1:]
	// Rewrite the response file without the tool path, VERBATIM for the
	// remaining lines (go1.26 format is one arg per line; re-encoding would
	// risk changing arg boundaries).
	idx := strings.IndexByte(string(data), '\n')
	if idx < 0 {
		fmt.Fprintf(os.Stderr, "toolexec-log: response file %s has only the tool path\n", path)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data[idx+1:], 0644); err != nil {
		fmt.Fprintf(os.Stderr, "toolexec-log: cannot rewrite response file %s: %v\n", path, err)
		os.Exit(1)
	}
	note = fmt.Sprintf("response file %s: %d bytes sha256=%x, tool=%q, %d args; rewrote file without tool path, passing @file through",
		path, len(data), sha256.Sum256(data), tool, len(rest))
	// Pass the (rewritten) @file through: the tool expands it itself.
	// This preserves the driver's intent (avoiding E2BIG) — the expanded
	// argv may contain a single arg larger than MAX_ARG_STRLEN.
	return tool, []string{argv[0]}, note
}

// encodeArg is copied verbatim from go1.27.1 cmd/go/internal/work/exec.go.
// It is the inverse of ParseArgs for driver-generated files.
func encodeArg(arg string) string {
	// 1.26 response-file format (verbatim from go1.26.8 cmd/go/internal/work/exec.go):
	// escape backslash and newline only; one arg per line.
	if !strings.ContainsAny(arg, "\\\n") {
		return arg
	}
	var b strings.Builder
	for _, r := range arg {
		switch r {
		case '\\':
			b.WriteByte('\\')
			b.WriteByte('\\')
		case '\n':
			b.WriteByte('\\')
			b.WriteByte('n')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DecodeArg is copied verbatim from go1.26.8 cmd/internal/objabi/flag.go.
func DecodeArg(arg string) string {
	if !strings.ContainsAny(arg, "\\\n") {
		return arg
	}
	var b strings.Builder
	var wasBS bool
	for _, r := range arg {
		if wasBS {
			switch r {
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			default:
				panic("badly formatted input")
			}
		} else if r == '\\' {
			wasBS = true
			continue
		} else {
			b.WriteRune(r)
		}
		wasBS = false
	}
	return b.String()
}

func logInvocation(raw []string, tool string, toolArgs []string, expandNote string) {
	logPath := os.Getenv("GRAALGO_WRAPPER_LOG")
	if logPath == "" {
		return
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	defer f.Close()

	cwd, _ := os.Getwd()
	fmt.Fprintf(f, "=== %s pid=%d tool=%s importpath=%q cwd=%q argc=%d\n",
		time.Now().UTC().Format(time.RFC3339Nano), os.Getpid(),
		tool, os.Getenv("TOOLEXEC_IMPORTPATH"), cwd, len(toolArgs))
	if expandNote != "" {
		fmt.Fprintf(f, "  [response-file] %s\n", expandNote)
		fmt.Fprintf(f, "  raw argv[0]=%q\n", raw[0])
	}
	for i, a := range toolArgs {
		if strings.HasPrefix(a, "@") && len(a) > 1 {
			data, err := os.ReadFile(a[1:])
			if err != nil {
				fmt.Fprintf(f, "  argv[%d]=@%q (response file unreadable: %v)\n", i, a[1:], err)
			} else {
				fmt.Fprintf(f, "  argv[%d]=@%q (response file: %d bytes sha256=%x)\n", i, a[1:], len(data), sha256.Sum256(data))
			}
			continue
		}
		fmt.Fprintf(f, "  argv[%d]=%q\n", i, a)
	}
}

// ParseArgs parses response file content in the go1.26 response-file format:
// one argument per line (CR stripped), each decoded with DecodeArg.
// (Verbatim port of go1.26.8 cmd/internal/objabi.expandArgs's splitting.)
func ParseArgs(s []byte) []string {
	text := strings.TrimSpace(strings.ReplaceAll(string(s), "\r", ""))
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	args := make([]string, len(lines))
	for i, line := range lines {
		args[i] = DecodeArg(line)
	}
	return args
}
