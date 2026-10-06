package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type verifyOptions struct {
	Target       string // vendored tree dir; "" => <manifest>.TargetDir
	ManifestPath string // in-repo manifest; "" => <repo>/phases/phase3/stdlib-fork/vendor-manifest.json
	Repo         string // explicit repo root; "" => discover from CWD
}

// verifyReport accumulates verification findings.
type verifyReport struct {
	checked  int
	missing  []string
	extra    []string
	modified []string
}

func (r *verifyReport) ok() bool {
	return len(r.missing) == 0 && len(r.extra) == 0 && len(r.modified) == 0
}

func (r *verifyReport) writeTo(w io.Writer) {
	fmt.Fprintf(w, "checked %d files\n", r.checked)
	for _, p := range r.missing {
		fmt.Fprintf(w, "MISSING  %s\n", p)
	}
	for _, p := range r.extra {
		fmt.Fprintf(w, "EXTRA    %s\n", p)
	}
	for _, p := range r.modified {
		fmt.Fprintf(w, "MODIFIED %s\n", p)
	}
	if r.ok() {
		fmt.Fprintf(w, "VERIFY OK\n")
	} else {
		fmt.Fprintf(w, "VERIFY FAILED\n")
	}
}

// verifyTree re-hashes the vendored tree against the manifest. It reports
// missing, extra, and modified files.
func verifyTree(o verifyOptions, out io.Writer) (*verifyReport, error) {
	manifestPath := o.ManifestPath
	if manifestPath == "" {
		repo := o.Repo
		if repo == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			repo, err = repoRoot(cwd)
			if err != nil {
				return nil, err
			}
		}
		manifestPath = defaultManifestPath(repo)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read manifest %q: %w", manifestPath, err)
	}
	var m manifest
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("malformed manifest %q: %w", manifestPath, err)
	}
	if m.ManifestVersion != manifestVersion {
		return nil, fmt.Errorf("manifest version %d not supported by this tool (tool supports %d)",
			m.ManifestVersion, manifestVersion)
	}

	target := o.Target
	if target == "" {
		target = m.TargetDir
	}
	target, err = filepath.Abs(expandHome(target))
	if err != nil {
		return nil, err
	}

	rep := &verifyReport{}
	seen := make(map[string]bool, len(m.Files))
	for _, e := range m.Files {
		rep.checked++
		seen[e.Path] = true
		p := filepath.Join(target, filepath.FromSlash(e.Path))
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() {
			rep.missing = append(rep.missing, e.Path)
			continue
		}
		if st.Size() != e.Size {
			rep.modified = append(rep.modified, e.Path)
			continue
		}
		sum, err := hashFile(p)
		if err != nil {
			return nil, err
		}
		if sum != e.SHA256 {
			rep.modified = append(rep.modified, e.Path)
		}
	}
	// Extra files: walk the target and flag anything not in the manifest.
	err = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			rep.extra = append(rep.extra, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot walk target %q: %w", target, err)
	}
	sort.Strings(rep.missing)
	sort.Strings(rep.extra)
	sort.Strings(rep.modified)
	rep.writeTo(out)
	if !rep.ok() {
		return rep, fmt.Errorf("verification failed: %d missing, %d extra, %d modified",
			len(rep.missing), len(rep.extra), len(rep.modified))
	}
	return rep, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func runVerifyCmd(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targetFlag := fs.String("target", "", "vendored tree directory (default: the TargetDir recorded in the manifest)")
	manifestFlag := fs.String("manifest", "", "manifest path (default: <repo>/phases/phase3/stdlib-fork/vendor-manifest.json)")
	repoFlag := fs.String("repo", "", "repo root (default: discovered from CWD)")
	if err := fs.Parse(argv); err != nil {
		return &usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return &usageError{fmt.Sprintf("unexpected arguments: %v", fs.Args())}
	}
	var o verifyOptions
	o.Target = *targetFlag
	o.ManifestPath = *manifestFlag
	o.Repo = *repoFlag
	_, err := verifyTree(o, stdout)
	return err
}
