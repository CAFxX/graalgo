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
		progs = append(progs, Program{Name: e.Name(), Dir: filepath.Join(root, e.Name()), Main: main})
	}
	sort.Slice(progs, func(i, j int) bool { return progs[i].Name < progs[j].Name })
	if len(progs) == 0 {
		return nil, fmt.Errorf("no corpus programs found in %s", root)
	}
	return progs, nil
}

// ApplyExclusions drops excluded programs, returning the kept set and a
// record of what was skipped (for the report). The skipped record carries
// the config's full exclusion entry — owner and retriage trigger included —
// not just program+reason: reconstructing it from the reason alone used to
// silently drop the ownership metadata on every report and snapshot record.
func ApplyExclusions(progs []Program, cfg *Config) (kept []Program, skipped []Exclusion) {
	byProgram := map[string]Exclusion{}
	for _, e := range cfg.Exclusions {
		byProgram[e.Program] = e
	}
	for _, p := range progs {
		if e, ok := byProgram[p.Name]; ok {
			skipped = append(skipped, e)
			continue
		}
		kept = append(kept, p)
	}
	return kept, skipped
}
