package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Requirement is one registry record: requirements/GG-XXXX.yaml.
type Requirement struct {
	ID               string   `yaml:"id"`
	Title            string   `yaml:"title"`
	Statement        string   `yaml:"statement"`
	Rationale        string   `yaml:"rationale"`
	Parent           string   `yaml:"parent"`
	Status           string   `yaml:"status"`
	SupersededBy     string   `yaml:"superseded_by"`
	VerificationPlan string   `yaml:"verification_plan"`
	History          []string `yaml:"history"`

	file string // source YAML path; not serialized
}

// idBody matches the variable part of a requirement ID: the classic numeric
// series (\d{4,}) or a namespaced series ([A-Z]+-\d{4}, e.g. SCHED-0001).
// Shared by registry.go and scan.go so validation and scanning agree.
const idBody = `[A-Z]+-\d{4}|\d{4,}`

var (
	idPattern  = regexp.MustCompile(`^GG-(?:` + idBody + `)$`)
	yamlNameRe = regexp.MustCompile(`^(GG-(?:` + idBody + `))\.yaml$`)
)

var (
	// idSplitRe decomposes a validated ID into namespace ("" for the classic
	// numeric series) and sequence number.
	idSplitRe = regexp.MustCompile(`^GG-(?:([A-Z]+)-)?(\d{4,})$`)
	nsValidRe = regexp.MustCompile(`^[A-Z]+$`)
)

func splitID(id string) (ns string, seq int, ok bool) {
	if !idPattern.MatchString(id) {
		return "", 0, false
	}
	m := idSplitRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

var validStatuses = map[string]bool{
	"draft": true, "locked": true, "active": true, "retired": true, "superseded": true,
}

// Requirement lifecycle: draft (being written, editable in place) -> locked
// (SPEC-accepted, immutable except via supersede/retire) -> active
// (implementation underway; rule (c) requires >=1 verifies: annotation) ->
// retired/superseded. Locking is the Phase-0->1 gate (req lock); activation
// happens as Phase-1+ work starts against a requirement (req activate).

// workCtx carries resolved directories for a command invocation.
type workCtx struct {
	root    string // repo root
	reqDir  string // requirements dir holding GG-*.yaml
	scanDir string // tree scanned for annotations
}

// findRoot walks up from cwd to the directory containing "requirements/".
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "requirements")); err == nil && st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no requirements/ directory found walking up from cwd (use --root)")
		}
		dir = parent
	}
}

func resolveCtx(rootFlag, reqDirFlag, scanDirFlag string) (*workCtx, error) {
	root := rootFlag
	if root == "" {
		switch {
		case scanDirFlag != "":
			abs, err := filepath.Abs(scanDirFlag)
			if err != nil {
				return nil, err
			}
			root = abs
		case reqDirFlag != "":
			// registry dir itself as root: rel paths resolve against it;
			// pass --root or --scan-dir for a wider scan.
			abs, err := filepath.Abs(reqDirFlag)
			if err != nil {
				return nil, err
			}
			root = abs
		default:
			var err error
			root, err = findRoot()
			if err != nil {
				return nil, err
			}
		}
	}
	c := &workCtx{root: root}
	c.reqDir = reqDirFlag
	if c.reqDir == "" {
		c.reqDir = filepath.Join(root, "requirements")
	}
	c.scanDir = scanDirFlag
	if c.scanDir == "" {
		c.scanDir = root
	}
	// Absolutize all three: scan hits are repo-relative paths anchored at
	// root (absRefPath), and historicalRef/scanAll compute filepath.Rel
	// between them — Rel errors on mixed relative/absolute pairs, which
	// silently disabled the rule-(b) historical exemption for relative
	// --req-dir/--root/--scan-dir.
	for _, p := range []*string{&c.root, &c.reqDir, &c.scanDir} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return nil, err
		}
		*p = abs
	}
	if st, err := os.Stat(c.reqDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("requirements dir not found: %s", c.reqDir)
	}
	return c, nil
}

// loadRegistry reads every GG-*.yaml directly inside reqDir (non-recursive).
func loadRegistry(reqDir string) (map[string]*Requirement, error) {
	entries, err := os.ReadDir(reqDir)
	if err != nil {
		return nil, err
	}
	reg := map[string]*Requirement{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := yamlNameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		path := filepath.Join(reqDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r Requirement
		if err := yaml.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		r.file = path
		if r.ID != m[1] {
			return nil, fmt.Errorf("%s: filename ID %s != record id %q", path, m[1], r.ID)
		}
		if !idPattern.MatchString(r.ID) {
			return nil, fmt.Errorf("%s: malformed id %q", path, r.ID)
		}
		if !validStatuses[r.Status] {
			return nil, fmt.Errorf("%s: invalid status %q", path, r.Status)
		}
		if _, dup := reg[r.ID]; dup {
			return nil, fmt.Errorf("duplicate requirement id %s", r.ID)
		}
		reg[r.ID] = &r
	}
	return reg, nil
}

// save writes the requirement back to its YAML file.
func (r *Requirement) save() error {
	out, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(r.file, out, 0644)
}

// mintID returns the next unused ID in the given namespace above the current
// maximum for that namespace. The classic numeric series uses namespace "".
// IDs are monotonic per namespace and never reused, which keeps "never reuse"
// trivially auditable.
func mintID(reg map[string]*Requirement, namespace string) (string, error) {
	if namespace != "" && !nsValidRe.MatchString(namespace) {
		return "", fmt.Errorf("invalid namespace %q: must match [A-Z]+", namespace)
	}
	max := 0
	for id := range reg {
		ns, n, ok := splitID(id)
		if !ok || ns != namespace {
			continue
		}
		if n > max {
			max = n
		}
	}
	if namespace == "" {
		return fmt.Sprintf("GG-%04d", max+1), nil
	}
	return fmt.Sprintf("GG-%s-%04d", namespace, max+1), nil
}

func sortedIDs(reg map[string]*Requirement) []string {
	ids := make([]string, 0, len(reg))
	for id := range reg {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func today() string { return time.Now().Format("2006-01-02") }
