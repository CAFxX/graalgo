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
func DiscoverCorpus(root string) ([]Program, error) {
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
