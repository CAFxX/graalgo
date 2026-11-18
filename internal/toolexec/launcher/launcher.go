// Package launcher computes what the graalgo launcher executes
// (docs/arch/toolexec-design.md §1, decision D1): the stock go binary of
// graalgo's GOROOT, with the original argv except for -toolexec and -tags,
// and an environment that injects graalgo-tool through GOFLAGS.
//
// The launcher reimplements no go command. The only argv it interprets are
// -toolexec and -tags of the subcommands that define build flags; every
// other argument reaches cmd/go untouched.
package launcher

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"graalgo-toolexec/argv"
	"graalgo-toolexec/release"
)

// Layout of an installed graalgo (GRAALGO_HOME):
//
//	bin/graalgo              this launcher
//	libexec/compile          graalgo-tool, under a tool basename so that
//	                         cmd/go uses response files (design §1)
//	libexec/graalgo-stub     the launcher stub of linked programs
//	goroot/                  the GOROOT (stock distribution + fork, §5.1)
const (
	ToolRel   = "libexec/compile"
	GorootRel = "goroot"
)

// Request is the launcher's input.
type Request struct {
	Home string   // GRAALGO_HOME
	Args []string // argv without argv[0]
	Env  []string // the launcher's environment
}

// Plan is what the launcher executes.
type Plan struct {
	Go   string   // path of the stock go binary
	Argv []string // full argv, argv[0] = "go"
	Env  []string
	// AppendVersion is set for a bare `go version`: the launcher runs go as
	// a child and appends graalgo's version to its single output line.
	AppendVersion bool
}

// HomeFromExecutable derives GRAALGO_HOME from the launcher's own path:
// the parent of a "bin" directory, else the directory itself.
func HomeFromExecutable(exe string) string {
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir)
	}
	return dir
}

// Compose computes the Plan for r.
func Compose(r Request) (*Plan, error) {
	if r.Home == "" {
		return nil, errors.New("GRAALGO_HOME is not set and cannot be derived")
	}
	home, err := filepath.Abs(r.Home)
	if err != nil {
		return nil, err
	}
	goroot := filepath.Join(home, GorootRel)
	goBin := filepath.Join(goroot, "bin", "go")
	if err := checkGoroot(goroot, goBin); err != nil {
		return nil, err
	}
	tool := filepath.Join(home, ToolRel)
	if fi, err := os.Stat(tool); err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("graalgo-tool missing at %s (incomplete GRAALGO_HOME %s)", tool, home)
	}

	env := newEnv(r.Env)
	settings := userSettings(env, goroot)
	purego := env.get("GRAALGO_PUREGO") != "0"

	env.set("GRAALGO_HOME", home)
	env.set("GOROOT", goroot)
	if _, ok := settings["GOTOOLCHAIN"]; !ok {
		env.set("GOTOOLCHAIN", "local")
	}
	if _, ok := settings["CGO_ENABLED"]; !ok {
		env.set("CGO_ENABLED", "0")
	}
	goflags, err := composeGOFLAGS(settings["GOFLAGS"], tool, purego)
	if err != nil {
		return nil, fmt.Errorf("GOFLAGS: %v", err)
	}
	env.set("GOFLAGS", goflags)

	args, err := rewriteArgs(r.Args, tool, purego)
	if err != nil {
		return nil, err
	}
	if err := verifyInjected(goflags, tool); err != nil {
		return nil, err
	}
	return &Plan{
		Go:            goBin,
		Argv:          append([]string{"go"}, args...),
		Env:           env.list(),
		AppendVersion: len(r.Args) == 1 && r.Args[0] == "version",
	}, nil
}

// checkGoroot refuses a GOROOT that is missing or of another release.
func checkGoroot(goroot, goBin string) error {
	if _, err := os.Stat(goBin); err != nil {
		return fmt.Errorf("no go command at %s: %v", goBin, err)
	}
	data, err := os.ReadFile(filepath.Join(goroot, "VERSION"))
	if err != nil {
		return fmt.Errorf("GOROOT %s: %v", goroot, err)
	}
	v, _, _ := strings.Cut(string(data), "\n")
	if v = strings.TrimSpace(v); v != release.GoVersion {
		return fmt.Errorf("GOROOT %s is %s; this graalgo tracks %s", goroot, v, release.GoVersion)
	}
	return nil
}

// env is an ordered environment with last-wins lookup.
type env struct{ kv []string }

func newEnv(list []string) *env { return &env{kv: slices.Clone(list)} }

func (e *env) lookup(k string) (string, bool) {
	for i := len(e.kv) - 1; i >= 0; i-- {
		if name, v, ok := strings.Cut(e.kv[i], "="); ok && name == k {
			return v, true
		}
	}
	return "", false
}

func (e *env) get(k string) string { v, _ := e.lookup(k); return v }

func (e *env) set(k, v string) {
	e.kv = slices.DeleteFunc(e.kv, func(s string) bool {
		name, _, _ := strings.Cut(s, "=")
		return name == k
	})
	e.kv = append(e.kv, k+"="+v)
}

func (e *env) list() []string { return e.kv }

// userSettings returns the go environment variables the user has set,
// with cmd/go's precedence: the process environment, then the user's go
// env file ($GOENV or <UserConfigDir>/go/env, written by `go env -w`).
// GOROOT/go.env holds distribution defaults (GOTOOLCHAIN=auto, GOPROXY),
// which are not user settings and which the launcher's defaults override.
// GOFLAGS is the exception: a GOFLAGS in GOROOT/go.env is kept, because
// setting GOFLAGS in the environment hides it.
func userSettings(e *env, goroot string) map[string]string {
	keys := []string{"GOFLAGS", "GOTOOLCHAIN", "CGO_ENABLED"}
	out := map[string]string{}
	// cmd/go's cfg.Getenv: an empty variable is unset (it falls through to
	// the go env file), and an empty GOENV selects the default file.
	var files []string
	if f := e.get("GOENV"); f != "" {
		if f != "off" {
			files = append(files, f)
		}
	} else if dir, err := userConfigDir(e); err == nil {
		files = append(files, filepath.Join(dir, "go", "env"))
	}
	fileVals := map[string]string{}
	for _, f := range files {
		for k, v := range readGoEnvFile(f) {
			fileVals[k] = v
		}
	}
	for _, k := range keys {
		if v := e.get(k); v != "" {
			out[k] = v
		} else if v := fileVals[k]; v != "" {
			out[k] = v
		}
	}
	if _, ok := out["GOFLAGS"]; !ok {
		if v := readGoEnvFile(filepath.Join(goroot, "go.env"))["GOFLAGS"]; v != "" {
			out["GOFLAGS"] = v
		}
	}
	return out
}

// userConfigDir is os.UserConfigDir evaluated against e rather than the
// launcher's own environment.
func userConfigDir(e *env) (string, error) {
	if d := e.get("XDG_CONFIG_HOME"); d != "" {
		return d, nil
	}
	if h := e.get("HOME"); h != "" {
		return filepath.Join(h, ".config"), nil
	}
	return "", errors.New("no HOME")
}

// readGoEnvFile parses a go env file (KEY=VALUE lines, '#' comments), as
// cmd/go/internal/cfg does. A missing file is empty.
func readGoEnvFile(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out
}

// toolexecValue renders graalgo-tool's -toolexec value, chaining the
// user's own -toolexec value when there is one.
func toolexecValue(tool, user string, hasUser bool) (string, error) {
	fields := []string{tool}
	if hasUser {
		if userFields, err := argv.SplitQuoted(user); err != nil {
			return "", fmt.Errorf("-toolexec %q: %v", user, err)
		} else if len(userFields) > 0 {
			fields = append(fields, argv.ChainFlag, argv.EncodeChain(user))
		}
	}
	return argv.JoinQuoted(fields)
}

// mergeTags adds purego to a -tags value, with cmd/go's tagsFlag parsing
// (comma list, or the pre-1.13 quoted space-separated form).
func mergeTags(v string) (string, error) {
	var tags []string
	if strings.ContainsAny(v, " '") {
		f, err := argv.SplitQuoted(v)
		if err != nil {
			return "", fmt.Errorf("-tags %q: %v", v, err)
		}
		tags = f
	} else {
		for t := range strings.SplitSeq(v, ",") {
			if t != "" {
				tags = append(tags, t)
			}
		}
	}
	if !slices.Contains(tags, "purego") {
		tags = append(tags, "purego")
	}
	return strings.Join(tags, ","), nil
}

// flagName splits "-name=value", "--name=value", "-name" into its parts.
func flagName(tok string) (name, value string, hasValue, ok bool) {
	if !strings.HasPrefix(tok, "-") || tok == "-" || tok == "--" {
		return "", "", false, false
	}
	s := strings.TrimPrefix(strings.TrimPrefix(tok, "-"), "-")
	name, value, hasValue = strings.Cut(s, "=")
	return name, value, hasValue, true
}

// composeGOFLAGS rewrites the user's GOFLAGS: an existing -toolexec is
// chained, an existing -tags gains purego, and whatever is missing is
// prepended (user entries keep stock precedence because they come later).
func composeGOFLAGS(user, tool string, purego bool) (string, error) {
	fields, err := argv.SplitQuoted(user)
	if err != nil {
		return "", err
	}
	sawToolexec, sawTags := false, false
	for i, f := range fields {
		name, value, hasValue, ok := flagName(f)
		if !ok || !hasValue {
			continue
		}
		switch name {
		case "toolexec":
			v, err := toolexecValue(tool, value, true)
			if err != nil {
				return "", err
			}
			fields[i], sawToolexec = "-toolexec="+v, true
		case "tags":
			if purego {
				v, err := mergeTags(value)
				if err != nil {
					return "", err
				}
				fields[i] = "-tags=" + v
			}
			sawTags = true
		}
	}
	var pre []string
	if !sawToolexec {
		v, err := toolexecValue(tool, "", false)
		if err != nil {
			return "", err
		}
		pre = append(pre, "-toolexec="+v)
	}
	if purego && !sawTags {
		pre = append(pre, "-tags=purego")
	}
	return argv.JoinQuoted(append(pre, fields...))
}

// buildFlagCommands are the go subcommands that define the shared build
// flags (and so -toolexec and -tags).
var buildFlagCommands = map[string]bool{
	"build": true, "clean": true, "fix": true, "generate": true, "get": true,
	"install": true, "list": true, "run": true, "test": true, "vet": true,
}

// valueFlags are the cmd/go flags (build, test, list, vet, run) that take
// their value as a separate argument, so that the scan does not mistake a
// value for a flag or a package.
var valueFlags = map[string]bool{
	"C": true, "o": true, "p": true, "asmflags": true, "buildmode": true,
	"compiler": true, "gccgoflags": true, "gcflags": true, "installsuffix": true,
	"ldflags": true, "mod": true, "modfile": true, "overlay": true, "pgo": true,
	"pkgdir": true, "tags": true, "toolexec": true, "covermode": true,
	"coverpkg": true, "coverprofile": true, "exec": true, "bench": true,
	"benchtime": true, "blockprofile": true, "blockprofilerate": true,
	"count": true, "cpu": true, "cpuprofile": true, "fuzz": true,
	"fuzzminimizetime": true, "fuzztime": true, "list": true, "memprofile": true,
	"memprofilerate": true, "mutexprofile": true, "mutexprofilefraction": true,
	"outputdir": true, "parallel": true, "run": true, "shuffle": true,
	"skip": true, "timeout": true, "trace": true, "vet": true, "f": true,
	"reuse": true, "debug-actiongraph": true, "debug-runtime-trace": true,
	"debug-trace": true,
}

// rewriteArgs applies the -toolexec chaining and the -tags merge to the
// command line of a build-flag subcommand. Scanning stops at "--", at
// "-args" (go test), and for go run at the first non-flag argument, after
// which arguments belong to the program.
func rewriteArgs(args []string, tool string, purego bool) ([]string, error) {
	out := slices.Clone(args)
	i := 0
	if i < len(out) && (out[i] == "-C" || out[i] == "--C") {
		i += 2
	} else if i < len(out) && (strings.HasPrefix(out[i], "-C=") || strings.HasPrefix(out[i], "--C=")) {
		i++
	}
	if i >= len(out) || !buildFlagCommands[out[i]] {
		return out, nil
	}
	cmd := out[i]
	for i++; i < len(out); i++ {
		tok := out[i]
		if tok == "--" || (cmd == "test" && (tok == "-args" || tok == "--args")) {
			break
		}
		name, value, hasValue, ok := flagName(tok)
		if !ok {
			if cmd == "run" {
				break
			}
			continue
		}
		if (name == "toolexec" || name == "tags") && !hasValue {
			if i+1 >= len(out) {
				return nil, fmt.Errorf("flag needs an argument: -%s", name)
			}
			value = out[i+1]
			out = slices.Delete(out, i+1, i+2)
		}
		switch {
		case name == "toolexec":
			v, err := toolexecValue(tool, value, true)
			if err != nil {
				return nil, err
			}
			out[i] = "-toolexec=" + v
		case name == "tags":
			if purego {
				v, err := mergeTags(value)
				if err != nil {
					return nil, err
				}
				out[i] = "-tags=" + v
			} else {
				out[i] = "-tags=" + value
			}
		case valueFlags[name] && !hasValue:
			i++
		}
	}
	return out, nil
}

// verifyInjected is the R8 guard of the design: the effective -toolexec of
// GOFLAGS (the last one) must start with graalgo-tool. rewriteArgs rewrites
// every -toolexec of the command line it scans, so together they ensure
// that no build runs plain gc silently. A failure here is a launcher bug.
func verifyInjected(goflags, tool string) error {
	fields, err := argv.SplitQuoted(goflags)
	if err != nil {
		return fmt.Errorf("internal: composed GOFLAGS %q: %v", goflags, err)
	}
	last, found := "", false
	for _, f := range fields {
		if name, v, has, ok := flagName(f); ok && has && name == "toolexec" {
			last, found = v, true
		}
	}
	f, err := argv.SplitQuoted(last)
	if !found || err != nil || len(f) == 0 || f[0] != tool {
		return fmt.Errorf("internal: effective -toolexec in GOFLAGS is %q, not graalgo-tool", last)
	}
	return nil
}
