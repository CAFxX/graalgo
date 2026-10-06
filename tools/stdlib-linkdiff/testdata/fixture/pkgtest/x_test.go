package pkgtest

//go:linkname testLocal runtime.fastrand
func testLocal() uint32
