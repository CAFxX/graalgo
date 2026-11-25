package build

// Stamp has no initializer; when is unexported; Derived is initializer code.
var Stamp string
var when = "never"
var Derived = Stamp + "+" + when

func When() string { return when }

//go:noinline
func Name() string { return "build" }
