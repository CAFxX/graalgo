// Package release names the Go release graalgo tracks and graalgo's own
// version string. docs/arch/toolexec-design.md D10: graalgo-tool and the
// launcher are built by the tracked toolchain, and the go/parser + go/types
// they link are that release's; TestBuiltByTrackedToolchain asserts it.
package release

// GoVersion is the tracked Go release. The launcher refuses a GOROOT of any
// other version, and graalgo-tool refuses tools of any other version (a
// GOTOOLCHAIN switch, design §1).
const GoVersion = "go1.27.1"

// Version is graalgo's own version, appended to `go version` by the
// launcher. Release builds set it with
// -ldflags=-X=graalgo-toolexec/release.Version=<v>.
var Version = "devel"
