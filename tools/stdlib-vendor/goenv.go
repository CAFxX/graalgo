package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// goEnv runs `go env <key>` and returns the value. The go binary is found
// via PATH, falling back to the workspace-pinned toolchain location.
func goEnv(key string) (string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		goBin = filepath.Join(home, "tools", "go", "bin", "go")
	}
	out, err := exec.Command(goBin, "env", key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
