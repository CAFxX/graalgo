package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Refusal sentinels — tests match these with errors.Is.
var (
	ErrTargetExists    = errors.New("target directory already exists and is non-empty (use --force)")
	ErrVersionMismatch = errors.New("GOROOT version differs from pinned version (use --allow-version-drift)")
)

var goVersionRe = regexp.MustCompile(`^go\d+\.\d+(\.\d+)?$`)

// manifestVersion is bumped whenever the on-disk manifest schema changes.
const manifestVersion = 1

// fileEntry is one vendored file: path relative to the tree root.
type fileEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"` // permission bits (e.g. 0644 == 420)
}

// manifest is the in-repo provenance record. It is byte-deterministic:
// no timestamps, sorted entries, fixed JSON encoding — so two vendor
// runs over identical sources produce byte-identical manifests.
type manifest struct {
	ManifestVersion int         `json:"manifestVersion"`
	GoVersion       string      `json:"goVersion"`
	SourceGoroot    string      `json:"sourceGoroot"`
	TargetDir       string      `json:"targetDir"`
	VersionDrift    bool        `json:"versionDrift"`
	Files           []fileEntry `json:"files"`
}

type vendorOptions struct {
	Goroot            string // GOROOT; "" => `go env GOROOT`
	Target            string // vendored tree dir; "" => $HOME/workspace/graalgo-stdlib/<ver>/
	ManifestPath      string // in-repo manifest; "" => <repo>/phases/phase3/stdlib-fork/vendor-manifest.json
	PinPath           string // in-repo pin; "" => <repo>/.../stdlib-fork/pinned-version.txt
	Repo              string // explicit repo root; "" => discover from CWD
	Force             bool
	AllowVersionDrift bool
}

// vendorTree copies $Goroot/src into Target and writes the manifest and pin.
// It returns sentinel errors ErrTargetExists / ErrVersionMismatch on the
// two refusal paths.
func vendorTree(o vendorOptions, log io.Writer) error {
	goroot, err := resolveGoroot(o.Goroot)
	if err != nil {
		return err
	}
	version, err := readGoVersion(goroot)
	if err != nil {
		return err
	}

	repo, manifestPath, pinPath, err := o.resolveRepoPaths()
	if err != nil {
		return err
	}
	_ = repo

	pin, pinExists, err := readPin(pinPath)
	if err != nil {
		return err
	}
	if pinExists && pin != version && !o.AllowVersionDrift {
		return fmt.Errorf("%w: source is %s, pinned is %s",
			ErrVersionMismatch, version, pin)
	}

	target := o.Target
	if target == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home dir for default target: %w", err)
		}
		target = filepath.Join(home, "workspace", "graalgo-stdlib", version)
	}
	target = expandHome(target)
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}

	if !o.Force {
		nonEmpty, err := dirNonEmpty(target)
		if err != nil {
			return err
		}
		if nonEmpty {
			return fmt.Errorf("%w: %s", ErrTargetExists, target)
		}
	}

	srcDir := filepath.Join(goroot, "src")
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return fmt.Errorf("source tree %q is not a directory", srcDir)
	}

	entries, totalBytes, err := copyTree(srcDir, target, log)
	if err != nil {
		return err
	}

	m := manifest{
		ManifestVersion: manifestVersion,
		GoVersion:       version,
		SourceGoroot:    goroot,
		TargetDir:       target,
		VersionDrift:    o.AllowVersionDrift && pinExists && pin != version,
		Files:           entries,
	}
	if err := writeManifest(manifestPath, &m); err != nil {
		return err
	}

	if !pinExists {
		if err := writePin(pinPath, version); err != nil {
			return err
		}
		fmt.Fprintf(log, "pinned new Go version %s -> %s\n", version, pinPath)
	} else if pin != version {
		fmt.Fprintf(log, "warning: vendored %s under --allow-version-drift; pin stays %s (update it deliberately per VENDORING.md)\n", version, pin)
	}

	fmt.Fprintf(log, "vendored %s (%d files, %d bytes) -> %s\n", version, len(entries), totalBytes, target)
	fmt.Fprintf(log, "manifest: %s\n", manifestPath)
	return nil
}

// resolveRepoPaths fills in manifest/pin paths. Explicitly-passed paths
// win; otherwise the repo root is discovered (from --repo or CWD) and the
// in-repo defaults apply.
func (o vendorOptions) resolveRepoPaths() (repo, manifestPath, pinPath string, err error) {
	manifestPath, pinPath = o.ManifestPath, o.PinPath
	if manifestPath != "" && pinPath != "" {
		return "", manifestPath, pinPath, nil
	}
	repo = o.Repo
	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", "", err
		}
		repo, err = repoRoot(cwd)
		if err != nil {
			return "", "", "", err
		}
	}
	if manifestPath == "" {
		manifestPath = defaultManifestPath(repo)
	}
	if pinPath == "" {
		pinPath = defaultPinPath(repo)
	}
	return repo, manifestPath, pinPath, nil
}

// resolveGoroot returns the flag value or the toolchain's `go env GOROOT`.
func resolveGoroot(flagVal string) (string, error) {
	if flagVal != "" {
		return filepath.Abs(expandHome(flagVal))
	}
	goroot, err := goEnv("GOROOT")
	if err != nil {
		return "", err
	}
	goroot = strings.TrimSpace(goroot)
	if goroot == "" {
		return "", errors.New("`go env GOROOT` returned empty; pass --goroot explicitly")
	}
	return goroot, nil
}

// readGoVersion reads the first line of $GOROOT/VERSION (e.g. "go1.27.1").
func readGoVersion(goroot string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(goroot, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("cannot read %s/VERSION: %w", goroot, err)
	}
	ver := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	if !goVersionRe.MatchString(ver) {
		return "", fmt.Errorf("malformed Go version %q in %s/VERSION", ver, goroot)
	}
	return ver, nil
}

// readPin reads the pinned version; ok=false when the pin file is absent
// (first vendor run — the pin is created then).
func readPin(pinPath string) (pin string, ok bool, err error) {
	raw, err := os.ReadFile(pinPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	pin = strings.TrimSpace(string(raw))
	if !goVersionRe.MatchString(pin) {
		return "", false, fmt.Errorf("malformed pinned version %q in %s", pin, pinPath)
	}
	return pin, true, nil
}

func writePin(pinPath, version string) error {
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(pinPath, []byte(version+"\n"), 0o644)
}

// dirNonEmpty reports whether dir exists and contains at least one entry.
func dirNonEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	return err == nil, err
}

// copyTree copies src into dst (creating dst), hashing every regular file.
// It returns the sorted manifest entries and total byte count.
func copyTree(src, dst string, log io.Writer) ([]fileEntry, int64, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, 0, err
	}
	var entries []fileEntry
	var total int64
	var count int
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(dstPath, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("refusing to vendor symlink %q (source tree must be plain files)", rel)
		case info.Mode().IsRegular():
			sum, n, err := copyAndHash(path, dstPath, info.Mode().Perm())
			if err != nil {
				return err
			}
			entries = append(entries, fileEntry{
				Path:   filepath.ToSlash(rel),
				SHA256: sum,
				Size:   n,
				Mode:   uint32(info.Mode().Perm()),
			})
			total += n
			count++
			if log != nil && count%2000 == 0 {
				fmt.Fprintf(log, "  ... %d files\n", count)
			}
			return nil
		default:
			return fmt.Errorf("refusing to vendor non-regular file %q (mode %v)", rel, info.Mode())
		}
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, total, nil
}

// copyAndHash copies src to dst (preserving permission bits) and returns
// the hex SHA-256 of the file contents and its size.
func copyAndHash(src, dst string, perm fs.FileMode) (string, int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	cerr := out.Close()
	if err != nil {
		return "", 0, err
	}
	if cerr != nil {
		return "", 0, cerr
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// writeManifest marshals m deterministically (sorted entries, fixed
// indent, no timestamps) and writes it atomically.
func writeManifest(path string, m *manifest) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func runVendorCmd(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vendor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	gorootFlag := fs.String("goroot", "", "GOROOT to vendor from (default: `go env GOROOT`)")
	targetFlag := fs.String("target", "", "target directory (default: $HOME/workspace/graalgo-stdlib/<goVersion>/)")
	manifestFlag := fs.String("manifest", "", "manifest output path (default: <repo>/phases/phase3/stdlib-fork/vendor-manifest.json)")
	repoFlag := fs.String("repo", "", "repo root (default: discovered from CWD)")
	forceFlag := fs.Bool("force", false, "vendor into an existing non-empty target directory")
	driftFlag := fs.Bool("allow-version-drift", false, "proceed when GOROOT version differs from the pin")
	if err := fs.Parse(argv); err != nil {
		return &usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return &usageError{fmt.Sprintf("unexpected arguments: %v", fs.Args())}
	}
	var o vendorOptions
	o.Goroot = *gorootFlag
	o.Target = *targetFlag
	o.Force = *forceFlag
	o.AllowVersionDrift = *driftFlag
	if *manifestFlag != "" {
		o.ManifestPath = *manifestFlag
		o.PinPath = filepath.Join(filepath.Dir(*manifestFlag), "pinned-version.txt")
	} else if *repoFlag != "" {
		o.Repo = *repoFlag
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repo, err := repoRoot(cwd)
		if err != nil {
			return err
		}
		o.Repo = repo
	}
	return vendorTree(o, stdout)
}
