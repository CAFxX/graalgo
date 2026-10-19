// Command mutation runs curated source-level mutants against the test suite.
// For each mutant it builds an overlay (Java) or a rebuilt binary (Go),
// runs the full suite, and reports KILLED (suite red) or SURVIVED (suite green).
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

	// Write report to the run workdir only. Per repo AGENTS.md §5,
	// hidden_files/ must never reappear and per-run evidence belongs in
	// commit messages — the report path is printed so the caller can
	// copy or quote it where it matters.
	report, _ := json.MarshalIndent(results, "", "  ")
	reportPath := filepath.Join(workdir, "report.json")
	os.WriteFile(reportPath, report, 0644)
	fmt.Printf("\nreport: %s\n", reportPath)

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

	// Compile the single mutated file against the built module classes.
	// Post phases/ removal the language core (incl. NodeFactory) compiles
	// in the runtime module's joint round, so the compile classpath is
	// runtime/out/classes : truffle/out/classes : truffle/lib jars —
	// the same split the truffle Makefile's main/test targets use.
	overlayClasses := filepath.Join(mdir, "classes")
	os.MkdirAll(overlayClasses, 0755)
	truffle := filepath.Join(repo, "internal", "backend", "truffle")
	runtime := filepath.Join(repo, "internal", "backend", "runtime")
	cp := fmt.Sprintf("%s:%s:%s",
		filepath.Join(runtime, "out", "classes"),
		filepath.Join(truffle, "out", "classes"),
		jarCP(truffle))
	cmd := exec.Command(javaBin(),
		"-Xlint:deprecation", "-Werror",
		"-cp", cp,
		"-d", overlayClasses,
		overlaySrc)
	cmd.Dir = truffle
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

	// Build gofrontend
	bin := filepath.Join(mdir, "gofrontend")
	cmd := exec.Command(goBin(), "build", "-o", bin, ".")
	cmd.Dir = frontDst
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "ERROR", fmt.Sprintf("go build: %v\n%s", err, out)
	}

	// Run test suite with GRAALGO_GOFRONTEND pointing to mutated binary
	return runTestSuite(repo, mdir, map[string]string{
		"GRAALGO_GOFRONTEND": bin,
	})
}

func runTestSuite(repo, mdir string, extraEnv map[string]string) (string, string) {
	truffle := filepath.Join(repo, "internal", "backend", "truffle")
	runtime := filepath.Join(repo, "internal", "backend", "runtime")
	// Build classpath: overlay (if any) : truffle test-classes :
	// runtime classes (joint round) : truffle classes (stubs) : jars.
	// Mirrors the truffle Makefile's `test` target exactly, with the
	// mutant overlay prepended so it shadows the baseline class.
	cp := ""
	if ov, ok := extraEnv["CLASSPATH_OVERLAY"]; ok {
		cp = ov + ":"
	}
	cp += fmt.Sprintf("%s:%s:%s:%s",
		filepath.Join(truffle, "out", "test-classes"),
		filepath.Join(runtime, "out", "classes"),
		filepath.Join(truffle, "out", "classes"),
		jarCP(truffle))

	// R1#5: pass the Makefile's JAVA_FLAGS (-Xss32m for the deep-recursion test),
	// plus the repo-dir property and test tmpdir the Makefile's `test` target sets.
	cmd := exec.Command(javaRunBin(),
		"--enable-native-access=ALL-UNNAMED", "-Xss32m",
		"-Dgraalgo.repo.dir="+repo,
		"-Djava.io.tmpdir="+filepath.Join(truffle, "out", "test-tmp"),
		"-cp", cp, "graalgo.test.Runner")
	cmd.Dir = truffle
	cmd.Env = os.Environ()
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

func jarCP(moduleDir string) string {
	// Match the Makefile's lib/*.jar classpath (truffle module).
	jars, _ := filepath.Glob(filepath.Join(moduleDir, "lib", "*.jar"))
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
