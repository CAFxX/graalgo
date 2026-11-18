// Package fixture names the syscall argument types the P2 prototype
// marshals. descview's typetab turns it into the layout table the Java
// executor reads, exactly as for the reflect prototype: the layouts are
// go/types' view of package syscall, not hand-written offsets.
package fixture

import "syscall"

var Roots = []any{
	syscall.Stat_t{},
	syscall.Timespec{},
	syscall.EpollEvent{},
	[4]syscall.EpollEvent{},
	syscall.Iovec{},
	[2]syscall.Iovec{},
	syscall.Msghdr{},
	[2]int32{},
}
