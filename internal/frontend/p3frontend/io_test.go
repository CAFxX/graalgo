package p3frontend

import (
	"testing"
)

// TestIoMirrorMatchesPhase1 is the drift test for the synthetic io
// mirror: the mirror must match the live phase-1 syntheticIoSource
// (internal/frontend/synth_io.go) in scope and in the method sets of
// every named type (including the 22 interfaces, whose method sets
// come from embedding). The mirror covers the full gc 1.27.1 public
// API of the synthetic source; any divergence fails fast here instead
// of silently diverging.
func TestIoMirrorMatchesPhase1(t *testing.T) {
	mirror := syntheticIoPackage()
	// The io source imports errors (synthetic mirror) and sync (real);
	// syntheticStdImporter resolves both.
	phase1 := phase1SyntheticPackageForTest(t,
		"../synth_io.go", "syntheticIoSource", "io", syntheticStdImporter(t))
	assertScopeMatchesPhase1(t, "io", mirror, phase1)
	for _, name := range []string{"Reader", "Writer", "Closer", "Seeker",
		"ReadWriter", "ReadCloser", "WriteCloser", "ReadWriteCloser",
		"ReadSeeker", "ReadSeekCloser", "WriteSeeker", "ReadWriteSeeker",
		"ReaderFrom", "WriterTo", "ReaderAt", "WriterAt",
		"ByteReader", "ByteScanner", "ByteWriter", "RuneReader",
		"RuneScanner", "StringWriter",
		"LimitedReader", "SectionReader", "OffsetWriter",
		"PipeReader", "PipeWriter"} {
		compareNamedMethodSets(t, "io."+name, mirror, phase1, name, nil)
	}
}
