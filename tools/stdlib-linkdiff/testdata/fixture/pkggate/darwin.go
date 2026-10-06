//go:build darwin

package pkggate

//go:linkname darwinLocal runtime.fastrand
func darwinLocal() uint32
