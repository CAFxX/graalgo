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
	"regexp"
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
}

// FilterStderr drops every line of s matching any of the backend's
// StderrFilters. An invalid expression is a programming error and panics.
func (b *Backend) FilterStderr(s string) string {
	if len(b.StderrFilters) == 0 {
		return s
	}
	res := make([]*regexp.Regexp, len(b.StderrFilters))
	for i, expr := range b.StderrFilters {
		res[i] = regexp.MustCompile(expr)
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
		// Expand ${ENVVAR} in Vars values so toolchain paths stay portable
		// across checkouts; an empty expansion falls back to PATH lookup.
		for k, v := range b.Vars {
			ev := os.ExpandEnv(v)
			if strings.TrimSpace(ev) == "" {
				ev = strings.ToLower(k) // e.g. GO -> "go" resolved via PATH
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
