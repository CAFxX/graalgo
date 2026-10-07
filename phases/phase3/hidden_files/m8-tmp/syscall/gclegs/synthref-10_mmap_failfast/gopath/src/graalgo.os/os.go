// Go stubs for the graalgo.os.* linkname targets, for the gc reference leg
// of .synthetic corpus programs. The synthetic syscall source binds these
// symbols via //go:linkname; under stock gc the bodies below satisfy the
// linker. Panic-program legs never call them.
package os

func getenvFound(key string) int64 { return 0 }
func getenv(key string) string      { return "" }
func setenv(key, value string) int64 { return 0 }
func unsetenv(key string) int64    { return 0 }
func clearenv() int64              { return 0 }
func environ() string              { return "" }
func getpid() int64                { return 1 }
func getppid() int64               { return 0 }
func getuid() int64                { return 0 }
func geteuid() int64               { return 0 }
func getgid() int64                { return 0 }
func getegid() int64               { return 0 }
func getgroups() string            { return "" }
func getcwd() string               { return "" }
func exit(code int64)              {}
