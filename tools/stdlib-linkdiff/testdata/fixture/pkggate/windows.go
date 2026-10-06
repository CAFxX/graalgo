//go:build windows

package pkggate

//go:linkname windowsLocal runtime.fastrand
func windowsLocal() uint32
