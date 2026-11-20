// Package harness implements the differential-vs-gc smoke layer specified in
// FEASIBILITY.md §7(1): run the same corpus programs under two backends,
// compare observables (stdout, stderr, exit code, panic traces), and report
// divergences. The two backends are declared in a JSON config; a future
// graalgo backend plugs in as a second backend entry with zero harness
// changes (see README.md "Backend seam").
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Backend describes one execution backend as command templates. Tokens in
// Build/Run may reference {VAR} entries from Vars, plus the per-run {SRC}
// (program source file) and {BIN} (output binary path) substituted by the
// runner. Env entries are applied as overrides: for builds over the ambient
// environment, for runs over a minimal deterministic environment
// (PATH, HOME, plus Env).
type Backend struct {
	Name  string            `json:"name"`
	Vars  map[string]string `json:"vars"`
	Build []string          `json:"build"`
	Run   []string          `json:"run"`
	Env   map[string]string `json:"env"`
	// StderrFilters, when non-empty, holds RE2 regular expressions naming
	// stderr lines to drop after normalization (one line per expression
	// match). It exists for backend-emitted noise that is not a program
	// observable — e.g. the HotSpot Truffle runtime's
	// sun.misc.Unsafe terminal-deprecation warnings on the graalgo backend.
	// Filters are backend-specific by construction: they never apply to
	// the reference backend's output.
	StderrFilters []string `json:"stderrFilters,omitempty"`
	// NoiseSets names built-in, harness-owned filter sets (see NoiseSets)
	// applied like StderrFilters. Structural: every JVM-hosted backend
	// declares "jvm" and gets the same, single definition of JVM noise,
	// instead of each config copying (and forgetting) regexes.
	NoiseSets []string `json:"noiseSets,omitempty"`
	// Batch: the launcher accepts --batch=<file> (the phase-1 graalgo-run
	// does; the product launcher does not), letting the typeparam/srctest
	// adapters amortize one JVM over many files.
	Batch bool `json:"batch,omitempty"`
	// ArgsPrefix is inserted between the run command and a program's
	// arguments when it has any (Program.Args): a launcher that takes the
	// source file as an operand needs a separator ("--") so the program's
	// arguments are not read as more source files. A gc binary needs none.
	ArgsPrefix []string `json:"argsPrefix,omitempty"`
	// LazyBuild: a check (snapshot check) runs the program without the
	// build step first (RunSkippingBuild) and pays for the separate build
	// only when that run diverges from the reference — then the verdict
	// comes from the full build+run, exactly as without LazyBuild, and a
	// full run that matches where the lazy one diverged is reported as a
	// flake. For a launcher whose build step is a whole second run of the
	// frontend and the JVM (graalgo-sync-run --check), a matching program
	// costs one JVM instead of two.
	LazyBuild bool `json:"lazyBuild,omitempty"`
}

// NoiseSets are the named stderr-noise filter sets a backend may opt into.
// Each line pattern is anchored and matches only text a host runtime (not
// the Go program) prints.
var NoiseSets = map[string][]string{
	"jvm": {
		// JDK 25 sun.misc.Unsafe terminal-deprecation warnings (Truffle).
		`^WARNING: A terminally deprecated method in sun\.misc\.Unsafe has been called$`,
		`^WARNING: sun\.misc\.Unsafe::.*$`,
		`^WARNING: Please consider reporting this to the maintainers.*$`,
		// HotSpot VM warnings, e.g. the hsperfdata "... locked" warning
		// two concurrent JVMs sharing a temp dir can print (the launchers
		// also pass -XX:-UsePerfData; this is the belt to that brace),
		// and its continuation lines.
		`^(Java HotSpot\(TM\)|OpenJDK) 64-Bit Server VM warning:.*$`,
		`^   \S*hsperfdata\S*$`,
		`^Try using the -Djava\.io\.tmpdir= option to select an alternate temp location\.$`,
	},
}

// FilterStderr drops every line of s matching any of the backend's
// StderrFilters. An invalid expression is a programming error and panics.
func (b *Backend) FilterStderr(s string) string {
	res := b.filterRegexps()
	if len(res) == 0 {
		return s
	}
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		drop := false
		for _, re := range res {
			if re.MatchString(line) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func (b *Backend) filterRegexps() []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, set := range b.NoiseSets {
		for _, expr := range NoiseSets[set] {
			res = append(res, regexp.MustCompile(expr))
		}
	}
	for _, expr := range b.StderrFilters {
		res = append(res, regexp.MustCompile(expr))
	}
	return res
}

// Exclusion removes a corpus program from runs with a recorded reason, per
// FEASIBILITY.md §7(2): the exclusion list may only grow through review,
// never silently. Reason codes: nondeterministic-output, backend-specific,
// flaky, other:<free text>.
type Exclusion struct {
	Program string `json:"program"`
	Reason  string `json:"reason"`
}

// Config is the on-disk harness configuration.
type Config struct {
	Backends   []Backend   `json:"backends"`
	Exclusions []Exclusion `json:"exclusions"`
	TimeoutSec int         `json:"timeoutSec"`
}

// LoadConfig reads and validates a JSON config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(c.Backends) < 1 {
		return nil, fmt.Errorf("config must declare at least one backend")
	}
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = 15
	}
	seen := map[string]bool{}
	for i := range c.Backends {
		b := &c.Backends[i]
		if b.Name == "" {
			return nil, fmt.Errorf("backend %d has empty name", i)
		}
		if seen[b.Name] {
			return nil, fmt.Errorf("duplicate backend name %q", b.Name)
		}
		seen[b.Name] = true
		if len(b.Build) == 0 {
			return nil, fmt.Errorf("backend %q has empty build template", b.Name)
		}
		if len(b.Run) == 0 {
			return nil, fmt.Errorf("backend %q has empty run template", b.Name)
		}
		for _, set := range b.NoiseSets {
			if _, ok := NoiseSets[set]; !ok {
				return nil, fmt.Errorf("backend %q: unknown noise set %q", b.Name, set)
			}
		}
		for _, expr := range b.StderrFilters {
			if _, err := regexp.Compile(expr); err != nil {
				return nil, fmt.Errorf("backend %q: bad stderr filter %q: %v", b.Name, expr, err)
			}
		}
		// Expand ${ENVVAR} in Vars values so toolchain paths stay portable
		// across checkouts; an empty expansion falls back to PATH lookup.
		for k, v := range b.Vars {
			ev := os.ExpandEnv(v)
			if strings.TrimSpace(ev) == "" {
				if k == "GO" {
					// GO is used as {GO}/bin/go in templates; resolve the
					// actual binary via PATH and derive the root.
					if p, err := exec.LookPath("go"); err == nil {
						if ap, err := filepath.Abs(p); err == nil {
							p = ap
						}
						ev = filepath.Dir(filepath.Dir(p))
					}
				}
				if ev == "" {
					ev = strings.ToLower(k) // e.g. FOO -> "foo" via PATH
				}
			}
			b.Vars[k] = ev
		}
	}
	return &c, nil
}

// render substitutes {VAR} placeholders from Vars into template tokens.
// {SRC} and {BIN} are left for the runner to substitute per program.
func (b *Backend) render(tokens []string) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		for k, v := range b.Vars {
			t = strings.ReplaceAll(t, "{"+k+"}", v)
		}
		out[i] = t
	}
	return out
}

// RenderBuild returns the build command with {SRC}/{BIN} substituted.
func (b *Backend) RenderBuild(src, bin string) []string {
	return substitute(b.render(b.Build), map[string]string{"SRC": src, "BIN": bin})
}

// RenderRun returns the run command with {SRC}/{BIN} substituted.
func (b *Backend) RenderRun(src, bin string) []string {
	return substitute(b.render(b.Run), map[string]string{"SRC": src, "BIN": bin})
}

// RenderRunArgs is RenderRun followed by the program arguments (behind
// ArgsPrefix when there are any).
func (b *Backend) RenderRunArgs(src, bin string, args []string) []string {
	cmd := b.RenderRun(src, bin)
	if len(args) == 0 {
		return cmd
	}
	cmd = append(cmd, b.ArgsPrefix...)
	return append(cmd, args...)
}

// Unrender replaces this backend's resolved variable values in s with
// their {VAR} placeholders (longest value first), so text that echoes a
// rendered command or a toolchain path is the same on every machine.
func (b *Backend) Unrender(s string) string {
	keys := make([]string, 0, len(b.Vars))
	for k, v := range b.Vars {
		if len(v) > 1 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if li, lj := len(b.Vars[keys[i]]), len(b.Vars[keys[j]]); li != lj {
			return li > lj
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		s = strings.ReplaceAll(s, b.Vars[k], "{"+k+"}")
	}
	return s
}

func substitute(tokens []string, vars map[string]string) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		for k, v := range vars {
			t = strings.ReplaceAll(t, "{"+k+"}", v)
		}
		out[i] = t
	}
	return out
}

// Excluded reports whether program is on the exclusion list and why.
func (c *Config) Excluded(program string) (string, bool) {
	for _, e := range c.Exclusions {
		if e.Program == program {
			return e.Reason, true
		}
	}
	return "", false
}
