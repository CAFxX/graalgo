// Command mutation runs curated source-level mutants against the test suite.
// For each mutant it builds an overlay (Java) or a rebuilt binary (Go),
// runs the full suite, and reports KILLED (suite red) or SURVIVED (suite green).
//
// The Java side runs the truffle module's differential test runner
// (graalgo.test.Runner) with the same classpath and flags as the
// truffle Makefile `test` target; the Go side rebuilds the gofrontend
// binary from internal/frontend. See runTestSuite / runJavaMutant.
//
// Toolchain discovery (javac/java/go): GRAALGO_JAVA, GRAALGO_JAVA_RUN,
// GRAALGO_GO / GO, $JAVA_HOME, then PATH, then ~/tools/... — see
// javaBin, javaRunBin, goBin. A second copy of the report goes to
// MUTATION_REPORT_DIR when set.
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

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: mutation <repo> <mutants.json>\n")
		os.Exit(2)
	}
	repo := os.Args[1]
	specPath := os.Args[2]

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
	baselineVerdict, baselineDetail := runTestSuite(repo, workdir, nil)
	if baselineVerdict != "SURVIVED" {
		fatal(fmt.Errorf("baseline not green: %s (%s)", baselineVerdict, baselineDetail))
	}
	fmt.Printf("baseline: GREEN\n")

	for _, m := range spec.Mutants {
		start := time.Now()
		verdict, detail := runMutant(repo, workdir, m)
		results = append(results, Result{
			ID:          m.ID,
			Description: m.Description,
			Verdict:     verdict,
			Detail:      detail,
			DurationSec: int64(time.Since(start).Seconds()),
		})
		fmt.Printf("[%s] %s: %s (%s)\n", m.ID, verdict, m.Description, detail)
	}

	// Write report to the workdir (the durable copy: the workdir is a
	// temp dir, so a caller that needs the report after the run sets
	// MUTATION_REPORT_DIR to have a second copy written there). The
	// old second copy went to phases/phase1/hidden_files/mutation —
	// that tree was deleted 2026-10-14, and hidden_files/ must never
	// reappear (repo AGENTS.md §5).
	report, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(filepath.Join(workdir, "report.json"), report, 0644)
	fmt.Printf("\nreport: %s/report.json\n", workdir)

	if p, err := persistReport(report); err != nil {
		fmt.Fprintf(os.Stderr, "warning: persist failed: %v\n", err)
	} else if p != "" {
		fmt.Printf("persisted: %s\n", p)
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

func runMutant(repo, workdir string, m Mutant) (string, string) {
	mdir := filepath.Join(workdir, m.ID)
	if err := os.MkdirAll(mdir, 0755); err != nil {
		return "ERROR", err.Error()
	}

	// Read original file
	srcPath := filepath.Join(repo, m.File)
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
		return runJavaMutant(repo, mdir, m, mutated)
	}
	return runGoMutant(repo, mdir, m, mutated)
}

func runJavaMutant(repo, mdir string, m Mutant, mutated string) (string, string) {
	// Write mutated file to overlay source tree (mirror repo-relative path)
	overlaySrc := filepath.Join(mdir, "src", m.File)
	if err := os.MkdirAll(filepath.Dir(overlaySrc), 0755); err != nil {
		return "ERROR", err.Error()
	}
	if err := os.WriteFile(overlaySrc, []byte(mutated), 0644); err != nil {
		return "ERROR", err.Error()
	}

	// Compile single file against the backend classes. The mutated
	// file is a truffle-module main source (it compiles in the
	// runtime module's joint round — see
	// internal/backend/truffle/Makefile); compiling it standalone
	// against the already-built runtime + truffle classes resolves
	// the same references, and the overlay dir goes first on the
	// test classpath below, so the mutated class shadows the built
	// one exactly as the old phase1 overlay did.
	overlayClasses := filepath.Join(mdir, "classes")
	os.MkdirAll(overlayClasses, 0755)
	cp := javacClasspath(repo)
	cmd := exec.Command(javaBin(),
		"-Xlint:deprecation", "-Werror",
		"-cp", cp,
		"-d", overlayClasses,
		overlaySrc)
	cmd.Dir = truffleDir(repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("javac: %v\n%s", err, out)
	}

	// Run test suite with overlay first in classpath
	return runTestSuite(repo, mdir, map[string]string{
		"CLASSPATH_OVERLAY": overlayClasses,
	})
}

func runGoMutant(repo, mdir string, m Mutant, mutated string) (string, string) {
	// For Go exporter: copy the frontend dir, apply mutation, build
	frontSrc := filepath.Join(repo, "internal", "frontend")
	frontDst := filepath.Join(mdir, "frontend")
	if err := copyDir(frontSrc, frontDst); err != nil {
		return "ERROR", fmt.Sprintf("copy: %v", err)
	}
	mutPath := filepath.Join(frontDst, filepath.Base(m.File))
	// m.File is internal/frontend/export.go, so base is export.go
	if err := os.WriteFile(mutPath, []byte(mutated), 0644); err != nil {
		return "ERROR", err.Error()
	}

	// Build gofrontend exactly as the product does: ./build.sh build
	// applies the vendored go/* overlay (internal/frontend/build.sh).
	// A plain `go build .` would silently use the toolchain's GOROOT
	// go/* sources instead of the vendored tree, so a mutant binary
	// built that way would not be the product binary with one
	// mutation — it would be a different build. build.sh needs `go`
	// (from goBin) and python3 on PATH.
	bin := filepath.Join(frontDst, "gofrontend")
	cmd := exec.Command("/bin/sh", "./build.sh", "build")
	cmd.Dir = frontDst
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(goBin())+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("build.sh: %v\n%s", err, out)
	}

	// Run test suite with GRAALGO_GOFRONTEND pointing to mutated binary
	return runTestSuite(repo, mdir, map[string]string{
		"GRAALGO_GOFRONTEND": bin,
	})
}

func runTestSuite(repo, mdir string, extraEnv map[string]string) (string, string) {
	// Mirror the truffle Makefile `test` target: the differential
	// test runner graalgo.test.Runner with the test classpath above,
	// JAVA_FLAGS (--enable-native-access=ALL-UNNAMED -Xss32m; the
	// -Xss32m is R1#5, for the deep-recursion test), and
	// -Dgraalgo.repo.dir (the corpus differential tests locate the
	// corpus through it). The suite runs from the truffle module
	// dir, as the Makefile does.
	overlay := extraEnv["CLASSPATH_OVERLAY"]
	cp := testClasspath(repo, overlay)

	// Per-run temp dirs under mdir (never /tmp: a full /tmp silently
	// yields empty output with exit 0 — see graalgo-sync-run).
	javaTmp := filepath.Join(mdir, "java-tmp")
	os.MkdirAll(javaTmp, 0755)
	cmd := exec.Command(javaRunBin(),
		"--enable-native-access=ALL-UNNAMED", "-Xss32m",
		"-Djava.io.tmpdir="+javaTmp,
		"-Dgraalgo.repo.dir="+repo,
		"-cp", cp, "graalgo.test.Runner")
	cmd.Dir = truffleDir(repo)
	cmd.Env = os.Environ()
	tmpDir := filepath.Join(mdir, "tmp")
	os.MkdirAll(tmpDir, 0755)
	cmd.Env = append(cmd.Env, "TMPDIR="+tmpDir, "GOTMPDIR="+tmpDir)
	for k, v := range extraEnv {
		if k == "CLASSPATH_OVERLAY" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Log output
	logPath := filepath.Join(mdir, "test.log")
	logFile, _ := os.Create(logPath)
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

// Backend layout (Phase-4 tree). These are the paths the Java side
// of the tool must know; they are pinned by main_test.go against the
// truffle Makefile `test` target, so a future move fails there
// instead of silently running the suite against a stale classpath.
func truffleDir(repo string) string {
	return filepath.Join(repo, "internal", "backend", "truffle")
}

func runtimeClasses(repo string) string {
	return filepath.Join(repo, "internal", "backend", "runtime", "out", "classes")
}

func truffleClasses(repo string) string {
	return filepath.Join(truffleDir(repo), "out", "classes")
}

func truffleTestClasses(repo string) string {
	return filepath.Join(truffleDir(repo), "out", "test-classes")
}

// jarCP matches the backend Makefiles' CP: the Truffle jars in
// internal/backend/truffle/lib/.
func jarCP(repo string) string {
	jars, _ := filepath.Glob(filepath.Join(truffleDir(repo), "lib", "*.jar"))
	return strings.Join(jars, ":")
}

// javacClasspath is the compile classpath for a single mutated main
// source: the built runtime + truffle classes and the Truffle jars.
func javacClasspath(repo string) string {
	return strings.Join([]string{runtimeClasses(repo), truffleClasses(repo), jarCP(repo)}, ":")
}

// testClasspath mirrors the truffle Makefile `test` target:
// test-classes : runtime/classes : truffle/classes : jars, with the
// overlay (mutated classes) first so it shadows the built ones.
func testClasspath(repo, overlay string) string {
	parts := []string{}
	if overlay != "" {
		parts = append(parts, overlay)
	}
	parts = append(parts, truffleTestClasses(repo), runtimeClasses(repo), truffleClasses(repo), jarCP(repo))
	return strings.Join(parts, ":")
}

// Toolchain discovery: an explicit per-tool env var wins, then the
// shared convention ($JAVA_HOME / $GO), then PATH, then the pinned
// local install as a last resort. Nothing is hardcoded without an
// override — the old absolute /home/hatch/tools/... paths made the
// tool unrunnable anywhere else. The Go order matches
// tools/stdlib-excl/validate.go:goBinary (tool-specific var, $GO,
// PATH, $HOME fallback).
func javaBin() string {
	if p := os.Getenv("GRAALGO_JAVA"); p != "" {
		return p
	}
	if p := os.Getenv("JAVA_HOME"); p != "" {
		return filepath.Join(p, "bin", "javac")
	}
	if p, err := exec.LookPath("javac"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), "tools", "graalvm-jdk-25", "bin", "javac")
}

func javaRunBin() string {
	if p := os.Getenv("GRAALGO_JAVA_RUN"); p != "" {
		return p
	}
	// java sits alongside javac: a GRAALGO_JAVA override names its sibling.
	if p := os.Getenv("GRAALGO_JAVA"); p != "" {
		return filepath.Join(filepath.Dir(p), "java")
	}
	if p := os.Getenv("JAVA_HOME"); p != "" {
		return filepath.Join(p, "bin", "java")
	}
	if p, err := exec.LookPath("java"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), "tools", "graalvm-jdk-25", "bin", "java")
}

func goBin() string {
	if p := os.Getenv("GRAALGO_GO"); p != "" {
		return p
	}
	if p := os.Getenv("GO"); p != "" {
		return p
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), "tools", "go", "bin", "go")
}

// persistReport writes a second copy of the report when the caller
// asks for one via MUTATION_REPORT_DIR. With the variable unset
// there is no second copy — the workdir report is the only one.
func persistReport(report []byte) (string, error) {
	dir := os.Getenv("MUTATION_REPORT_DIR")
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ts := time.Now().Format("20060102-150405")
	p := filepath.Join(dir, "report-"+ts+".json")
	if err := os.WriteFile(p, report, 0644); err != nil {
		return "", err
	}
	return p, nil
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
