package pkgrt

//go:linkname wbLocal runtime.writeBarrier
var wbLocal struct{ enabled bool }
