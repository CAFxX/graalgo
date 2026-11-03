package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L-L F3: verifyTestDriver admits only driver-shaped inputs for the
// package dir's own import set, and fails closed otherwise.

func writeTestDriverFixture(t *testing.T, withTest bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package foo\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if withTest {
		if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package foo\n\nimport (\n\t\"strings\"\n\t\"testing\"\n\t\"internal/bytealg\"\n)\n\nvar _, _ = strings.Contains, testing.T{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeDriver(t *testing.T, body string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "driver.go")
	if err := os.WriteFile(f, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

// driverPrefix is the head of a genuine driver: package clause first,
// then the import block (imports must precede other declarations),
// then the generated shim. Test drivers must be valid Go: the
// verifier parses them, and an import-after-declaration file fails
// parsing before any import-set check runs.
const driverPrefix = "package main\n\n"

const driverShim = "// --- graalgo test shim (generated) ---\ntype graalgoT struct{}\n\n"

func TestVerifyTestDriverAcceptsGenuineShape(t *testing.T) {
	dir := writeTestDriverFixture(t, true)
	driver := writeDriver(t, driverPrefix+`
import (
	"fmt"
	"strings"
	"time"
	"os"
	"internal/bytealg"
)

`+driverShim+`
func main() {
	_, _, _, _, _ = fmt.Sprint, strings.Contains, time.Now, os.Args, bytealg.IndexByteString
}
`)
	if err := verifyTestDriver([]string{driver}, dir); err != nil {
		t.Fatalf("verifyTestDriver(genuine shape): %v", err)
	}
}

func TestVerifyTestDriverRejects(t *testing.T) {
	dir := writeTestDriverFixture(t, true)
	noTestDir := writeTestDriverFixture(t, false)
	cases := []struct {
		name   string
		driver string
		dir    string
		want   string
	}{
		{
			"forged internal import",
			driverPrefix + "import \"internal/cpu\"\n\n" + driverShim + "func main() {}\n",
			dir, "imports \"internal/cpu\"",
		},
		{
			"testing import is never a driver import",
			driverPrefix + "import \"testing\"\n\n" + driverShim + "func main() {}\n",
			dir, "imports \"testing\"",
		},
		{
			"missing shim marker",
			"package main\n\nfunc main() {}\n",
			dir, "lack the generated test shim",
		},
		{
			"non-main package",
			"package foo\n\n// --- graalgo test shim (generated) ---\n\nfunc main() {}\n",
			dir, "package \"foo\"",
		},
		{
			"missing func main",
			driverPrefix + driverShim + "func run() {}\n",
			dir, "func main",
		},
		{
			"dir without test files",
			driverPrefix + driverShim + "func main() {}\n",
			noTestDir, "no _test.go",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driver := writeDriver(t, c.driver)
			err := verifyTestDriver([]string{driver}, c.dir)
			if err == nil {
				t.Fatalf("verifyTestDriver(%s): got nil error", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("verifyTestDriver(%s) error = %q, want it to contain %q", c.name, err, c.want)
			}
		})
	}
	// Missing dir fails closed too.
	if err := verifyTestDriver([]string{writeDriver(t, driverPrefix+"func main() {}\n")}, filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("verifyTestDriver(missing dir): got nil error")
	}
}
