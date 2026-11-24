package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Program is one corpus program: a directory containing main.go.
type Program struct {
	Name string // directory base name, e.g. "01_hello"
	Dir  string
	Main string // path to main.go
	// StdoutRules are the opt-in stdout normalization rules declared in
	// the program's NORMALIZE file (see NormalizeStdout); usually none.
	StdoutRules []string
	// Args are the program's command-line arguments (os.Args[1:]); usually
	// none. GOROOT/test declares them on the action line ("// run a b").
	Args []string
	// RunDir, when set, is the working directory of the run step instead
	// of a fresh empty one (GOROOT/test programs run in GOROOT/test, as
	// gc's own test driver runs them, and may read files there).
	RunDir string
	// TimeoutScale multiplies the config's per-program run timeout
	// (0 = 1). For programs that do real work in child processes (e.g.
	// invoking the go tool) and so are slow on a loaded host.
	TimeoutScale int
}

// DiscoverCorpus finds all corpus programs: immediate subdirectories of
// root containing a main.go file, sorted by name for deterministic order.
// root is resolved to an absolute path: backend run steps execute in a
// fresh empty working directory, so program paths must not be relative.
func DiscoverCorpus(root string) ([]Program, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve corpus dir: %w", err)
	}
	root = abs
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read corpus dir: %w", err)
	}
	var progs []Program
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		main := filepath.Join(root, e.Name(), "main.go")
		if st, err := os.Stat(main); err != nil || st.IsDir() {
			continue
		}
		p := Program{Name: e.Name(), Dir: filepath.Join(root, e.Name()), Main: main}
		if data, err := os.ReadFile(filepath.Join(p.Dir, "NORMALIZE")); err == nil {
			rules, err := ParseNormalizeFile(string(data))
			if err != nil {
				return nil, fmt.Errorf("%s/NORMALIZE: %w", p.Name, err)
			}
			p.StdoutRules = rules
		}
		progs = append(progs, p)
	}
	sort.Slice(progs, func(i, j int) bool { return progs[i].Name < progs[j].Name })
	if len(progs) == 0 {
		return nil, fmt.Errorf("no corpus programs found in %s", root)
	}
	return progs, nil
}

// ApplyExclusions drops excluded programs, returning the kept set and a
// record of what was skipped (for the report).
func ApplyExclusions(progs []Program, cfg *Config) (kept []Program, skipped []Exclusion) {
	for _, p := range progs {
		if reason, ok := cfg.Excluded(p.Name); ok {
			skipped = append(skipped, Exclusion{Program: p.Name, Reason: reason})
			continue
		}
		kept = append(kept, p)
	}
	return kept, skipped
}
