// Package release names the Go release graalgo tracks and graalgo's own
// version string. docs/arch/toolexec-design.md D10: graalgo-tool and the
// launcher are built by the tracked toolchain, and the go/parser + go/types
// they link are that release's; TestBuiltByTrackedToolchain asserts it.
package release

import "fmt"

// GoVersion is the tracked Go release. The launcher refuses a GOROOT of any
// other version, and graalgo-tool refuses tools of any other version (a
// GOTOOLCHAIN switch, design §1).
const GoVersion = "go1.27.1"

// Version is graalgo's own version, appended to `go version` by the
// launcher. Release builds set it with
// -ldflags=-X=graalgo-toolexec/release.Version=<v>.
var Version = "devel"

// CheckBuiltBy is GG-0069's refusal: a binary whose frontend (go/parser,
// go/types) was linked from any release but the tracked one must not run.
// builtBy is runtime.Version() of the calling binary; the error names both
// versions.
func CheckBuiltBy(builtBy string) error {
	if builtBy != GoVersion {
		return fmt.Errorf("built by %s, but this graalgo tracks %s: its go/parser and go/types must be the tracked release's (GG-0069); rebuild it with %s", builtBy, GoVersion, GoVersion)
	}
	return nil
}
