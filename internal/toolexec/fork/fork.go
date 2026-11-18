// Package fork applies graalgo's standard-library fork to an upstream Go
// source tree (docs/arch/toolexec-design.md D8, §5.1).
//
// The fork is data, stored in the repository's stdlib directory:
//
//	stdlib/fork/<import path>/      a replaced package: the complete
//	                                directory that takes the place of
//	                                upstream's (facade packages)
//	stdlib/patches/<import path>/<file>.patch
//	                                a patched file: a unified diff against
//	                                upstream's <file> of that package
//
// Nothing here names a package: which packages are replaced or patched is
// what the two directories contain. Upstream is never modified; the result
// is either a `go build -overlay` file (development and tests) or an
// assembled GOROOT (the distribution).
package fork

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Fork is the loaded, validated fork.
type Fork struct {
	// Upstream is the GOROOT the fork applies to.
	Upstream string
	// Replaced are the replaced packages, sorted by import path.
	Replaced []Replaced
	// Patched are the patched files, sorted by upstream-relative path.
	Patched []Patched
}

// Replaced is one replaced package.
type Replaced struct {
	ImportPath string
	// Dir is the fork's directory; Files its regular files (base names).
	Dir   string
	Files []string
	// Removed are the base names of upstream's files in the package
	// directory, all of which the fork hides.
	Removed []string
}

// Patched is one patched upstream file.
type Patched struct {
	// Rel is the file's path under GOROOT/src, slash-separated.
	Rel string
	// Patch is the diff file; Content the patched file's bytes.
	Patch   string
	Content []byte
	// Added and Deleted count the diff's + and - lines (fork-size audit,
	// GG-0044).
	Added, Deleted int
}

// Load reads the fork under stdlibDir (the directory holding fork/ and
// patches/) and validates it against the upstream GOROOT: its VERSION must
// be goVersion, every replaced package and patched file must exist
// upstream, every patch must apply exactly (no fuzz, no offset), and no
// package may be both replaced and patched.
func Load(stdlibDir, upstream, goVersion string) (*Fork, error) {
	v, err := os.ReadFile(filepath.Join(upstream, "VERSION"))
	if err != nil {
		return nil, fmt.Errorf("fork: upstream GOROOT: %v", err)
	}
	if line, _, _ := strings.Cut(string(v), "\n"); strings.TrimSpace(line) != goVersion {
		return nil, fmt.Errorf("fork: upstream GOROOT %s is %s; the fork is against %s", upstream, strings.TrimSpace(line), goVersion)
	}
	f := &Fork{Upstream: upstream}
	src := filepath.Join(upstream, "src")

	forkRoot := filepath.Join(stdlibDir, "fork")
	err = walkDirs(forkRoot, func(dir, rel string, files []string) error {
		hasGo := false
		for _, name := range files {
			if strings.HasSuffix(name, ".patch") {
				return fmt.Errorf("fork: %s: a patch in a replaced package (patches belong under stdlib/patches)", filepath.Join(dir, name))
			}
			hasGo = hasGo || strings.HasSuffix(name, ".go")
		}
		if !hasGo {
			if len(files) > 0 {
				return fmt.Errorf("fork: %s: files but no Go source", dir)
			}
			return nil
		}
		up := filepath.Join(src, filepath.FromSlash(rel))
		removed, err := regularFiles(up)
		if err != nil {
			return fmt.Errorf("fork: replaced package %s does not exist upstream: %v", rel, err)
		}
		f.Replaced = append(f.Replaced, Replaced{ImportPath: rel, Dir: dir, Files: files, Removed: removed})
		return nil
	})
	if err != nil {
		return nil, err
	}

	patchRoot := filepath.Join(stdlibDir, "patches")
	err = walkDirs(patchRoot, func(dir, rel string, files []string) error {
		for _, name := range files {
			if name == ".gitkeep" {
				continue
			}
			base, ok := strings.CutSuffix(name, ".patch")
			if !ok {
				return fmt.Errorf("fork: %s: not a .patch file (stdlib/patches holds diffs, never copies of upstream)", filepath.Join(dir, name))
			}
			for _, r := range f.Replaced {
				if r.ImportPath == rel {
					return fmt.Errorf("fork: package %s is both replaced and patched", rel)
				}
			}
			patchFile := filepath.Join(dir, name)
			patch, err := os.ReadFile(patchFile)
			if err != nil {
				return err
			}
			relFile := rel + "/" + base
			orig, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(relFile)))
			if err != nil {
				return fmt.Errorf("fork: %s patches a file that does not exist upstream: %v", patchFile, err)
			}
			content, added, deleted, err := Apply(orig, patch)
			if err != nil {
				return fmt.Errorf("fork: %s does not apply to %s %s: %v", patchFile, goVersion, relFile, err)
			}
			if bytes.Equal(content, orig) {
				return fmt.Errorf("fork: %s changes nothing", patchFile)
			}
			f.Patched = append(f.Patched, Patched{Rel: relFile, Patch: patchFile, Content: content, Added: added, Deleted: deleted})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(f.Replaced, func(i, j int) bool { return f.Replaced[i].ImportPath < f.Replaced[j].ImportPath })
	sort.Slice(f.Patched, func(i, j int) bool { return f.Patched[i].Rel < f.Patched[j].Rel })
	return f, nil
}

// walkDirs calls fn for every directory under root (root excluded; a
// missing root is an empty fork) with its slash-separated relative path
// and the sorted base names of its regular files. Anything that is neither
// a directory nor a regular file is an error.
func walkDirs(root string, fn func(dir, rel string, files []string) error) error {
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if !d.Type().IsRegular() {
				return fmt.Errorf("fork: %s: not a regular file", path)
			}
			return nil
		}
		files, err := regularFiles(path)
		if err != nil {
			return err
		}
		if path == root {
			for _, name := range files {
				if name != ".gitkeep" {
					return fmt.Errorf("fork: %s: file outside a package directory", filepath.Join(path, name))
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(path, filepath.ToSlash(rel), files)
	})
}

func regularFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// Overlay returns the `go build -overlay` JSON that applies the fork to
// the upstream tree in place: upstream's files of a replaced package map
// to "" (deleted), the fork's files are added, and each patched file maps
// to its patched copy, which Overlay writes under workDir.
func (f *Fork) Overlay(workDir string) ([]byte, error) {
	src := filepath.Join(f.Upstream, "src")
	replace := map[string]string{}
	for _, r := range f.Replaced {
		up := filepath.Join(src, filepath.FromSlash(r.ImportPath))
		for _, name := range r.Removed {
			replace[filepath.Join(up, name)] = ""
		}
		for _, name := range r.Files {
			abs, err := filepath.Abs(filepath.Join(r.Dir, name))
			if err != nil {
				return nil, err
			}
			replace[filepath.Join(up, name)] = abs
		}
	}
	for _, p := range f.Patched {
		out := filepath.Join(workDir, filepath.FromSlash(p.Rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(out, p.Content, 0o644); err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(out)
		if err != nil {
			return nil, err
		}
		replace[filepath.Join(src, filepath.FromSlash(p.Rel))] = abs
	}
	// encoding/json sorts map keys: the output is deterministic.
	return json.MarshalIndent(struct{ Replace map[string]string }{replace}, "", "\t")
}

// Assemble writes a GOROOT for graalgo at dir, which must not exist:
// everything but src is a symbolic link into the upstream distribution
// (never modified, AGENTS §4); src is upstream's with the fork applied.
//
// With shallow false, src is a full copy: the tree a distribution ships.
// With shallow true, every source directory the fork does not touch is a
// symbolic link to upstream's: cheap enough to build per test, and
// equivalent for building named packages, but `std` and `...` patterns do
// not descend into linked directories — development and tests only.
func (f *Fork) Assemble(dir string, shallow bool) error {
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("fork: %s exists; remove it first", dir)
	}
	up, err := filepath.Abs(f.Upstream)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(up)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == "src" {
			continue
		}
		if err := os.Symlink(filepath.Join(up, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	replaced := map[string]*Replaced{}
	// touched: the directories that differ from upstream's or contain one
	// that does ("." is src itself).
	touched := map[string]bool{".": true}
	touch := func(rel string) {
		for ; rel != "." && !touched[rel]; rel = pathDir(rel) {
			touched[rel] = true
		}
	}
	for i := range f.Replaced {
		replaced[f.Replaced[i].ImportPath] = &f.Replaced[i]
		touch(f.Replaced[i].ImportPath)
	}
	patched := map[string]*Patched{}
	for i := range f.Patched {
		patched[f.Patched[i].Rel] = &f.Patched[i]
		touch(pathDir(f.Patched[i].Rel))
	}
	upSrc := filepath.Join(up, "src")
	outSrc := filepath.Join(dir, "src")
	return filepath.WalkDir(upSrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(upSrc, path)
		if err != nil {
			return err
		}
		out := filepath.Join(outSrc, rel)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if shallow && !touched[rel] {
				if err := os.Symlink(path, out); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			if r := replaced[rel]; r != nil {
				for _, name := range r.Files {
					if err := copyFile(filepath.Join(r.Dir, name), filepath.Join(out, name)); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, out)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("fork: upstream %s: not a regular file", path)
		}
		if replaced[pathDir(rel)] != nil {
			return nil // hidden by the replacement
		}
		if p := patched[rel]; p != nil {
			return os.WriteFile(out, p.Content, 0o644)
		}
		return copyFile(path, out)
	})
}

// pathDir is path.Dir for a slash-separated relative path ("." at the top).
func pathDir(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return "."
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	st, err := os.Stat(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, st.Mode().Perm())
}

// Audit is the fork-size report of GG-0009/GG-0044: one line per replaced
// package and per patched file, then totals.
func (f *Fork) Audit() (string, error) {
	var b strings.Builder
	files, lines := 0, 0
	for _, r := range f.Replaced {
		n := 0
		for _, name := range r.Files {
			data, err := os.ReadFile(filepath.Join(r.Dir, name))
			if err != nil {
				return "", err
			}
			n += bytes.Count(data, []byte("\n"))
		}
		fmt.Fprintf(&b, "replaced %-40s %3d files %6d lines (hides %d upstream files)\n", r.ImportPath, len(r.Files), n, len(r.Removed))
		files += len(r.Files)
		lines += n
	}
	added, deleted := 0, 0
	for _, p := range f.Patched {
		fmt.Fprintf(&b, "patched  %-40s +%d -%d\n", p.Rel, p.Added, p.Deleted)
		added += p.Added
		deleted += p.Deleted
	}
	fmt.Fprintf(&b, "total: %d replaced packages (%d files, %d lines), %d patched files (+%d -%d)\n",
		len(f.Replaced), files, lines, len(f.Patched), added, deleted)
	return b.String(), nil
}
