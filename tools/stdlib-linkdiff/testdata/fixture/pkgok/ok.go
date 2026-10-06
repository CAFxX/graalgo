package pkgok

//go:linkname okLocal runtime.fastrand
func okLocal() uint32

//go:linknamestd is not a directive: this comment must not be counted.
var _ = 0
