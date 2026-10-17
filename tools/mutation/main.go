// Command mutation runs curated source-level mutants against the test suite.
// For each mutant it builds an overlay (Java) or a rebuilt binary (Go),
// runs the full suite, and reports KILLED (suite red) or SURVIVED (suite green).
//
// Layout (post-migration): the Java backend lives under
// internal/backend/{truffle,runtime}; the test runner is graalgo.test.Runner
// with the shadow-order classpath overlay:test-classes:runtime:out/classes:
// truffle:out/classes:jars (mirroring internal/backend/truffle/Makefile's
// test target).
//
// Prerequisites: the backend must be built first (`make build` at the repo
// root, plus the truffle test classes); the tool checks and fails loudly
// otherwise. The JSON report is printed to stdout — per-run evidence is
// not written into the repo tree.
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

// backendDirs locates the post-migration backend build outputs.
type backendDirs struct {
	truffle     string // internal/backend/truffle
	runtime     string // internal/backend/runtime
	runtimeOut  string // internal/backend/runtime/out/classes
	truffleOut  string // internal/backend/truffle/out/classes
	truffleTest string // internal/backend/truffle/out/test-classes
}

func backendLayout(repo string) backendDirs {
	truffle := filepath.Join(repo, "internal", "backend", "truffle")
	runtime := filepath.Join(repo, "internal", "backend", "runtime")
	return backendDirs{
		truffle:     truffle,
		runtime:     runtime,
		runtimeOut:  filepath.Join(runtime, "out", "classes"),
		truffleOut:  filepath.Join(truffle, "out", "classes"),
		truffleTest: filepath.Join(truffle, "out", "test-classes"),
	}
}

// checkPrereqs fails loudly unless the backend (including test classes) is
// built. Running mutants against a half-built tree would report every
// mutant KILLED for the wrong reason.
func checkPrereqs(bd backendDirs) {
	missing := []string{}
	for _, d := range []string{bd.runtimeOut, bd.truffleOut, bd.truffleTest} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		fatal(fmt.Errorf("backend not built, missing:\n  %s\nbuild it first: `make build` at the repo root, then `make -C internal/backend/truffle test` (compiles out/test-classes as part of its build)",
			strings.Join(missing, "\n  ")))
	}
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

	bd := backendLayout(repo)
	checkPrereqs(bd)

	workdir, err := os.MkdirTemp("", "mutation-*")
	if err != nil {
		fatal(err)
	}
	// Every run copies a full frontend tree here; never leak it.
	defer os.RemoveAll(workdir)
	fmt.Printf("workdir: %s\n", workdir)

	var results []Result

	// R1#5, R2#6: baseline control — the clean tree must be green,
	// otherwise every mutant is trivially "killed".
	fmt.Printf("running baseline control...\n")
	baselineVerdict, baselineDetail := runTestSuite(repo, bd, workdir, nil)
	if baselineVerdict != "SURVIVED" {
		fatal(fmt.Errorf("baseline not green: %s (%s)", baselineVerdict, baselineDetail))
	}
	fmt.Printf("baseline: GREEN\n")

	for _, m := range spec.Mutants {
		start := time.Now()
		verdict, detail := runMutant(repo, bd, workdir, m)
		results = append(results, Result{
			ID:          m.ID,
			Description: m.Description,
			Verdict:     verdict,
			Detail:      detail,
			DurationSec: int64(time.Since(start).Seconds()),
		})
		fmt.Printf("[%s] %s: %s (%s)\n", m.ID, verdict, m.Description, detail)
	}

	// Write report.json into the workdir and print the full JSON report
	// to stdout: the workdir is removed on exit, so stdout is the durable
	// record. (Per-run evidence is never written into the repo tree.)
	report, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fatal(fmt.Errorf("marshal report: %w", err))
	}
	if err := os.WriteFile(filepath.Join(workdir, "report.json"), report, 0644); err != nil {
		fatal(fmt.Errorf("write report: %w", err))
	}
	fmt.Printf("\nreport JSON:\n%s\n", report)

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

func runMutant(repo string, bd backendDirs, workdir string, m Mutant) (string, string) {
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
		return runJavaMutant(repo, bd, mdir, m, mutated)
	}
	return runGoMutant(repo, bd, mdir, m, mutated)
}

func runJavaMutant(repo string, bd backendDirs, mdir string, m Mutant, mutated string) (string, string) {
	// Write mutated file to overlay source tree (mirror repo-relative path)
	overlaySrc := filepath.Join(mdir, "src", m.File)
	if err := os.MkdirAll(filepath.Dir(overlaySrc), 0755); err != nil {
		return "ERROR", err.Error()
	}
	if err := os.WriteFile(overlaySrc, []byte(mutated), 0644); err != nil {
		return "ERROR", err.Error()
	}

	// Compile single file against the built backend classes. Classpath
	// order mirrors the build: runtime classes (joint round, holds the
	// real classes) before the truffle module's classes.
	overlayClasses := filepath.Join(mdir, "classes")
	if err := os.MkdirAll(overlayClasses, 0755); err != nil {
		return "ERROR", err.Error()
	}
	cp := strings.Join([]string{bd.runtimeOut, bd.truffleOut, jarCP(bd.truffle)}, ":")
	cmd := exec.Command(javaBin(),
		"-Xlint:deprecation", "-Werror",
		"-cp", cp,
		"-d", overlayClasses,
		overlaySrc)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("javac: %v\n%s", err, out)
	}

	// Run test suite with overlay first in classpath
	return runTestSuite(repo, bd, mdir, map[string]string{
		"CLASSPATH_OVERLAY": overlayClasses,
	})
}

func runGoMutant(repo string, bd backendDirs, mdir string, m Mutant, mutated string) (string, string) {
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

	// Build gofrontend
	bin := filepath.Join(mdir, "gofrontend")
	cmd := exec.Command(goBin(), "build", "-o", bin, ".")
	cmd.Dir = frontDst
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("go build: %v\n%s", err, out)
	}

	// Run test suite with GRAALGO_GOFRONTEND pointing to mutated binary
	return runTestSuite(repo, bd, mdir, map[string]string{
		"GRAALGO_GOFRONTEND": bin,
	})
}

func runTestSuite(repo string, bd backendDirs, mdir string, extraEnv map[string]string) (string, string) {
	// Classpath mirrors internal/backend/truffle/Makefile's test target:
	// overlay (if any) : test classes : runtime classes (replacements
	// shadow stubs) : truffle classes : jars.
	cp := ""
	if ov, ok := extraEnv["CLASSPATH_OVERLAY"]; ok {
		cp = ov + ":"
	}
	cp += strings.Join([]string{bd.truffleTest, bd.runtimeOut, bd.truffleOut, jarCP(bd.truffle)}, ":")

	// R1#5: pass the Makefile's JAVA_FLAGS (-Xss32m for the deep-recursion test).
	// -Dgraalgo.repo.dir lets the differential tests locate the corpus,
	// as in the Makefile's test target.
	cmd := exec.Command(javaRunBin(),
		"--enable-native-access=ALL-UNNAMED", "-Xss32m",
		"-Dgraalgo.repo.dir="+repo,
		"-cp", cp, "graalgo.test.Runner")
	cmd.Dir = repo
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		if k == "CLASSPATH_OVERLAY" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Log output. If the log cannot be created, stdout/stderr would be
	// silently discarded and kill-attribution destroyed — fail loudly.
	logPath := filepath.Join(mdir, "test.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return "ERROR", fmt.Sprintf("create log %s: %v", logPath, err)
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

func javaBin() string {
	if p := os.Getenv("GRAALGO_JAVA"); p != "" {
		return p
	}
	// javac is alongside java in the GraalVM JDK
	return "/home/hatch/tools/graalvm-jdk-25/bin/javac"
}

func javaRunBin() string {
	if p := os.Getenv("GRAALGO_JAVA_RUN"); p != "" {
		return p
	}
	return "/home/hatch/tools/graalvm-jdk-25/bin/java"
}

func goBin() string {
	return "/home/hatch/tools/go/bin/go"
}

func jarCP(truffleDir string) string {
	// Match the backend Makefiles: lib/*.jar under the truffle module.
	jars, _ := filepath.Glob(filepath.Join(truffleDir, "lib", "*.jar"))
	return strings.Join(jars, ":")
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
