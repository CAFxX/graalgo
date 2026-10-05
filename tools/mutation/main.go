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

	// Write report
	report, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(filepath.Join(workdir, "report.json"), report, 0644)
	fmt.Printf("\nreport: %s/report.json\n", workdir)

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
	if strings.Count(string(orig), m.Old) > 1 {
		// Use first occurrence only; warn in detail
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

	// Compile single file against out/classes
	overlayClasses := filepath.Join(mdir, "classes")
	os.MkdirAll(overlayClasses, 0755)
	phase1 := filepath.Join(repo, "phases/phase1")
	cp := fmt.Sprintf("%s/out/classes:%s", phase1, jarCP(phase1))
	cmd := exec.Command(javaBin(),
		"-Xlint:deprecation", "-Werror",
		"-cp", cp,
		"-d", overlayClasses,
		overlaySrc)
	cmd.Dir = phase1
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
	phase1 := filepath.Join(repo, "phases/phase1")
	frontSrc := filepath.Join(phase1, "frontend")
	frontDst := filepath.Join(mdir, "frontend")
	if err := copyDir(frontSrc, frontDst); err != nil {
		return "ERROR", fmt.Sprintf("copy: %v", err)
	}
	mutPath := filepath.Join(frontDst, filepath.Base(m.File))
	// m.File is phases/phase1/frontend/export.go, so base is export.go
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
	phase1 := filepath.Join(repo, "phases/phase1")
	// Build classpath: overlay (if any) : out/test : out/classes : jars
	cp := ""
	if ov, ok := extraEnv["CLASSPATH_OVERLAY"]; ok {
		cp = ov + ":"
	}
	cp += fmt.Sprintf("%s/out/test:%s/out/classes:%s", phase1, phase1, jarCP(phase1))

	cmd := exec.Command(javaRunBin(), "-cp", cp, "graalgo.test.Runner")
	cmd.Dir = phase1
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
		// Suite failed (non-zero exit) = mutant KILLED
		return "KILLED", fmt.Sprintf("suite red (see %s)", logPath)
	}
	logFile.Close()
	return "SURVIVED", "suite green"
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

func jarCP(phase1 string) string {
	// Match Makefile CP: lib/*.jar
	jars, _ := filepath.Glob(filepath.Join(phase1, "lib", "*.jar"))
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
