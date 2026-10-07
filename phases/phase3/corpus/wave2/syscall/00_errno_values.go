package main

import (
	"fmt"
	"syscall"
)

// sc00: errno / SYS_* / O_* / signal constant values + Getpagesize.
// Constant drift vs gc: every value must match stock gc 1.27.1
// linux/amd64 exactly.
func main() {
	fmt.Println(int(syscall.ENOENT), int(syscall.EACCES), int(syscall.EEXIST),
		int(syscall.EINVAL), int(syscall.EAGAIN), int(syscall.EPERM))
	fmt.Println(int(syscall.EINTR), int(syscall.ENOTEMPTY), int(syscall.ENOSYS),
		int(syscall.EBADF), int(syscall.ECHILD))
	fmt.Println(int(syscall.SYS_GETPID), int(syscall.SYS_READ), int(syscall.SYS_WRITE),
		int(syscall.SYS_OPENAT))
	fmt.Println(syscall.Getpagesize())
	fmt.Println(int(syscall.O_RDONLY), int(syscall.O_WRONLY), int(syscall.O_RDWR),
		int(syscall.O_CREAT), int(syscall.O_TRUNC), int(syscall.O_APPEND))
	fmt.Println(int(syscall.SIGINT), int(syscall.SIGKILL), int(syscall.SIGTERM))
	fmt.Println(int(syscall.S_IFREG), int(syscall.S_IFDIR), int(syscall.S_IFLNK))
	fmt.Println(int(syscall.DT_REG), int(syscall.DT_DIR), int(syscall.DT_LNK))
}
