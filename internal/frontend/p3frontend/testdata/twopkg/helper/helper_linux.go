//go:build linux

package helper

// Platform reports the build OS (linux variant).
func Platform() string {
	return "linux"
}
