package helper

// Platform reports the build OS (windows variant). The _windows filename
// suffix excludes this file on linux; if it were wrongly included, the
// duplicate Platform declaration would fail the build.
func Platform() string {
	return "windows"
}
