// Command mutation runs curated source-level mutants against the test suite.
// For each mutant it builds an overlay (Java) or a rebuilt binary (Go),
// runs the full suite, and reports KILLED (suite red) or SURVIVED (suite green).
//
// Layout (post Phase-4 restructure): the Java backend lives in
// internal/backend/{truffle,runtime,interop}. The differential test runner is
// graalgo.test.Runner in the truffle module; the tool uses the same classpath
// the truffle Makefile's `test` target uses:
//
//	out/test-classes : runtime/out/classes : out/classes : lib/*.jar
//
// (the runtime module compiles the joint round, so runtime/out/classes holds
// every non-stub class; the overlay dir goes first to shadow the mutant.)
// The corpus is located via -Dgraalgo.repo.dir=<repo> (tests resolve
// test/corpus/phase1/... under it) and the gofrontend binary via the
// GRAALGO_GOFRONTEND env var, exactly like the Makefile.
//
// Reports always land in the run's temp workdir; a timestamped copy is also
// kept in the durable report dir from mutationReportsDir() (env
// GRAALGO_MUTATION_REPORTS, default ~/workspace/graalgo-mutation-reports —
// outside the repo tree, never hidden_files/).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Mutant struct {
	ID          string `json:"id"`
	Lang        string `json:"lang"` // "java" or "go"
	File        string `json:"file"` // repo-relative
	Old         string `json:"old"`
	New         string `json:"new"`
	Description string `json:"description"`
}

type Result struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Verdict     string `json:"verdict"` // KILLED, SURVIVED, ERROR
	Detail      string `json:"detail"`
	DurationSec int64  `json:"duration_sec"`
}

// layout resolves the repo-relative backend paths once per run.
type layout struct {
	repo       string
	truffle    string   // <repo>/internal/backend/truffle
	runtime    string   // <repo>/internal/backend/runtime
	frontend   string   // <repo>/internal/frontend
	gofrontend string   // <repo>/internal/frontend/gofrontend
	jars       []string // <repo>/internal/backend/truffle/lib/*.jar
	persistDir string   // durable report dir (env-overridable, outside the repo)
}

func newLayout(repo string) (layout, error) {
	l := layout{
		repo:       repo,
		truffle:    filepath.Join(repo, "internal", "backend", "truffle"),
		runtime:    filepath.Join(repo, "internal", "backend", "runtime"),
		frontend:   filepath.Join(repo, "internal", "frontend"),
		persistDir: mutationReportsDir(),
	}
	l.gofrontend = filepath.Join(l.frontend, "gofrontend")
	jars, err := filepath.Glob(filepath.Join(l.truffle, "lib", "*.jar"))
	if err != nil {
		return l, err
	}
	l.jars = jars
	return l, nil
}

// mutationReportsDir is the durable report location. Reports are per-run
// evidence: they must not land in the repo tree (no-debris rule, AGENTS.md
// section 5 — the old phases/phase1/hidden_files/mutation location died
// with the phases/ deletion, and hidden_files/ must never reappear).
// GRAALGO_MUTATION_REPORTS overrides the location; the default is outside
// the repo so no gitignore entry is needed.
func mutationReportsDir() string {
	if p := os.Getenv("GRAALGO_MUTATION_REPORTS"); p != "" {
		return p
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "workspace", "graalgo-mutation-reports")
	}
	return filepath.Join(os.TempDir(), "graalgo-mutation-reports")
}

// testClasspath mirrors the truffle module Makefile `test` target:
// out/test-classes : runtime/out/classes : out/classes : lib/*.jar,
// with the mutant overlay (if any) first so it shadows the real classes.
func (l layout) testClasspath(overlay string) string {
	parts := []string{}
	if overlay != "" {
		parts = append(parts, overlay)
	}
	parts = append(parts,
		filepath.Join(l.truffle, "out", "test-classes"),
		filepath.Join(l.runtime, "out", "classes"),
		filepath.Join(l.truffle, "out", "classes"),
	)
	parts = append(parts, l.jars...)
	return strings.Join(parts, ":")
}

// overlayClasspath is the javac classpath for compiling a single mutated
// Java file: the runtime module's joint-round classes first (they hold every
// non-stub class), then the truffle stubs, then the Truffle jars.
func (l layout) overlayClasspath() string {
	parts := []string{
		filepath.Join(l.runtime, "out", "classes"),
		filepath.Join(l.truffle, "out", "classes"),
	}
	parts = append(parts, l.jars...)
	return strings.Join(parts, ":")
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: mutation <repo> <mutants.json>\n")
		os.Exit(2)
	}
	repo := os.Args[1]
	specPath := os.Args[2]

	l, err := newLayout(repo)
	if err != nil {
		fatal(err)
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		fatal(err)
	}
	var spec struct {
		Mutants []Mutant `json:"mutants"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		fatal(err)
	}

	workdir, err := os.MkdirTemp("", "mutation-*")
	if err != nil {
		fatal(err)
	}
	fmt.Printf("workdir: %s\n", workdir)

	var results []Result

	// R1#5, R2#6: baseline control — the clean tree must be green,
	// otherwise every mutant is trivially "killed".
	fmt.Printf("running baseline control...\n")
	baselineVerdict, baselineDetail := runTestSuite(l, workdir, nil)
	if baselineVerdict != "SURVIVED" {
		fatal(fmt.Errorf("baseline not green: %s (%s)", baselineVerdict, baselineDetail))
	}
	fmt.Printf("baseline: GREEN\n")

	for _, m := range spec.Mutants {
		start := time.Now()
		verdict, detail := runMutant(l, workdir, m)
		results = append(results, Result{
			ID:          m.ID,
			Description: m.Description,
			Verdict:     verdict,
			Detail:      detail,
			DurationSec: int64(time.Since(start).Seconds()),
		})
		fmt.Printf("[%s] %s: %s (%s)\n", m.ID, verdict, m.Description, detail)
	}

	// Write report to the run workdir.
	report, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fatal(fmt.Errorf("marshal report: %v", err))
	}
	if err := os.WriteFile(filepath.Join(workdir, "report.json"), report, 0644); err != nil {
		fatal(fmt.Errorf("write report: %v", err))
	}
	fmt.Printf("\nreport: %s/report.json\n", workdir)

	// Keep a timestamped copy in the durable report dir (outside the repo).
	// Never under hidden_files/ (no-debris rule). A failed persist is only
	// a warning: the primary report above already landed.
	if err := os.MkdirAll(l.persistDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create %s: %v\n", l.persistDir, err)
	} else {
		ts := time.Now().Format("20060102-150405")
		persistPath := filepath.Join(l.persistDir, "report-"+ts+".json")
		if err := os.WriteFile(persistPath, report, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot persist report: %v\n", err)
		} else {
			fmt.Printf("persisted: %s\n", persistPath)
		}
	}

	// Summary
	killed, survived, errors := 0, 0, 0
	for _, r := range results {
		switch r.Verdict {
		case "KILLED":
			killed++
		case "SURVIVED":
			survived++
		default:
			errors++
		}
	}
	fmt.Printf("killed=%d survived=%d errors=%d\n", killed, survived, errors)
}

func runMutant(l layout, workdir string, m Mutant) (string, string) {
	mdir := filepath.Join(workdir, m.ID)
	if err := os.MkdirAll(mdir, 0755); err != nil {
		return "ERROR", err.Error()
	}

	// Read original file
	srcPath := filepath.Join(l.repo, filepath.FromSlash(m.File))
	orig, err := os.ReadFile(srcPath)
	if err != nil {
		return "ERROR", fmt.Sprintf("read: %v", err)
	}
	if !strings.Contains(string(orig), m.Old) {
		return "ERROR", "old string not found"
	}
	// R2#8: warn if the pattern occurs multiple times (we mutate only the first).
	if n := strings.Count(string(orig), m.Old); n > 1 {
		fmt.Fprintf(os.Stderr, "warning: %s: pattern occurs %d times, mutating first only\n", m.ID, n)
	}
	mutated := strings.Replace(string(orig), m.Old, m.New, 1)

	if m.Lang == "java" {
		return runJavaMutant(l, mdir, m, mutated)
	}
	return runGoMutant(l, mdir, m, mutated)
}

func runJavaMutant(l layout, mdir string, m Mutant, mutated string) (string, string) {
	// Write mutated file to overlay source tree (mirror repo-relative path)
	overlaySrc := filepath.Join(mdir, "src", filepath.FromSlash(m.File))
	if err := os.MkdirAll(filepath.Dir(overlaySrc), 0755); err != nil {
		return "ERROR", err.Error()
	}
	if err := os.WriteFile(overlaySrc, []byte(mutated), 0644); err != nil {
		return "ERROR", err.Error()
	}

	// Compile single file against the backend classes.
	overlayClasses := filepath.Join(mdir, "classes")
	if err := os.MkdirAll(overlayClasses, 0755); err != nil {
		return "ERROR", fmt.Sprintf("overlay classes dir: %v", err)
	}
	cmd := exec.Command(javaBin(),
		"-Xlint:deprecation", "-Werror",
		"-cp", l.overlayClasspath(),
		"-d", overlayClasses,
		overlaySrc)
	cmd.Dir = l.truffle
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("javac: %v\n%s", err, out)
	}

	// Run test suite with overlay first in classpath
	return runTestSuite(l, mdir, map[string]string{
		"CLASSPATH_OVERLAY": overlayClasses,
	})
}

func runGoMutant(l layout, mdir string, m Mutant, mutated string) (string, string) {
	// For the Go exporter: copy the frontend dir, apply mutation, rebuild.
	frontDst := filepath.Join(mdir, "frontend")
	if err := copyDir(l.frontend, frontDst); err != nil {
		return "ERROR", fmt.Sprintf("copy: %v", err)
	}
	// m.File is repo-relative (e.g. internal/frontend/export.go); resolve it
	// inside the copied tree and refuse anything that escapes it.
	rel, err := filepath.Rel(filepath.Join("internal", "frontend"), filepath.FromSlash(m.File))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "ERROR", fmt.Sprintf("mutant file outside internal/frontend: %q", m.File)
	}
	mutPath := filepath.Join(frontDst, rel)
	if err := os.WriteFile(mutPath, []byte(mutated), 0644); err != nil {
		return "ERROR", err.Error()
	}

	// Rebuild gofrontend with the vendored-overlay build (build.sh), NOT a
	// plain `go build`: the binary must embed the vendored go/* sources via
	// -overlay, else it silently tests the wrong sources (P3-M7 trap).
	// build.sh loses its exec bit in the copy, so invoke it via sh.
	cmd := exec.Command("sh", "build.sh", "build")
	cmd.Dir = frontDst
	cmd.Env = setEnvVar(os.Environ(), "PATH",
		filepath.Dir(goBin())+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("build.sh: %v\n%s", err, out)
	}

	// Run test suite with GRAALGO_GOFRONTEND pointing to mutated binary
	return runTestSuite(l, mdir, map[string]string{
		"GRAALGO_GOFRONTEND": filepath.Join(frontDst, "gofrontend"),
	})
}

func runTestSuite(l layout, mdir string, extraEnv map[string]string) (string, string) {
	// Build classpath: overlay (if any) : out/test-classes :
	// runtime/out/classes : out/classes : jars (Makefile `test` order).
	cp := l.testClasspath(extraEnv["CLASSPATH_OVERLAY"])

	// gofrontend: the Makefile exports <repo>/internal/frontend/gofrontend;
	// a Go mutant overrides it with its rebuilt binary.
	gofrontend := l.gofrontend
	if v, ok := extraEnv["GRAALGO_GOFRONTEND"]; ok {
		gofrontend = v
	}

	// R1#5: pass the Makefile's JAVA_FLAGS (-Xss32m for the deep-recursion test).
	// -Dgraalgo.repo.dir lets the corpus tests find test/corpus/phase1/...
	cmd := exec.Command(javaRunBin(),
		"--enable-native-access=ALL-UNNAMED", "-Xss32m",
		"-Dgraalgo.repo.dir="+l.repo,
		"-cp", cp, "graalgo.test.Runner")
	cmd.Dir = l.truffle
	env := os.Environ()
	for k, v := range extraEnv {
		if k == "CLASSPATH_OVERLAY" {
			continue
		}
		env = setEnvVar(env, k, v)
	}
	env = setEnvVar(env, "GRAALGO_GOFRONTEND", gofrontend)
	cmd.Env = env
	// Log output. A missing log costs the FAIL-line attribution, so fail
	// the mutant loudly instead of silently degrading (or panicking on a
	// nil Close).
	logPath := filepath.Join(mdir, "test.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return "ERROR", fmt.Sprintf("log: %v", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		logFile.Close()
		// Suite failed (non-zero exit) = mutant KILLED.
		// R1#5: attribute the kill to the failing test(s).
		killers := findFailingTests(logPath)
		return "KILLED", fmt.Sprintf("killed by %v (see %s)", killers, logPath)
	}
	logFile.Close()
	return "SURVIVED", "suite green"
}

// findFailingTests extracts FAIL lines from the test log.
func findFailingTests(logPath string) []string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return []string{"(unreadable log)"}
	}
	var killers []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "FAIL ") {
			// "FAIL Class.testName: ..." -> "Class.testName"
			rest := strings.TrimPrefix(line, "FAIL ")
			if i := strings.Index(rest, ":"); i > 0 {
				rest = rest[:i]
			}
			killers = append(killers, rest)
		}
	}
	if len(killers) == 0 {
		return []string{"(no FAIL lines, non-zero exit)"}
	}
	return killers
}

// setEnvVar sets key=value in env, replacing any existing entry for key.
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func javaBin() string {
	if p := os.Getenv("GRAALGO_JAVA"); p != "" {
		return p
	}
	// javac is alongside java in the GraalVM JDK. $HOME-relative, never a
	// hardcoded username; fall back to PATH lookup.
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "tools", "graalvm-jdk-25", "bin", "javac")
	}
	return "javac"
}

func javaRunBin() string {
	if p := os.Getenv("GRAALGO_JAVA_RUN"); p != "" {
		return p
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "tools", "graalvm-jdk-25", "bin", "java")
	}
	return "java"
}

func goBin() string {
	if p := os.Getenv("GRAALGO_GO"); p != "" {
		return p
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "tools", "go", "bin", "go")
	}
	return "go"
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
