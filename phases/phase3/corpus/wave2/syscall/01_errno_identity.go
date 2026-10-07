package main

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
)

// sc01: Errno.Is — gc's exact mapping (syscall_unix.go), including the
// fs sentinel identities shared with synthetic os (errors.Is(err,
// fs.ErrNotExist), os.IsNotExist chains), plus Timeout/Temporary and
// the Error()/Signal String() renderings.
func main() {
	// The exact gc mapping.
	fmt.Println(errors.Is(syscall.ENOENT, fs.ErrNotExist))
	fmt.Println(errors.Is(syscall.EACCES, fs.ErrPermission))
	fmt.Println(errors.Is(syscall.EPERM, fs.ErrPermission))
	fmt.Println(errors.Is(syscall.EEXIST, fs.ErrExist))
	fmt.Println(errors.Is(syscall.ENOTEMPTY, fs.ErrExist))
	fmt.Println(errors.Is(syscall.ENOSYS, errors.ErrUnsupported))
	fmt.Println(errors.Is(syscall.ENOTSUP, errors.ErrUnsupported))
	fmt.Println(errors.Is(syscall.EOPNOTSUPP, errors.ErrUnsupported))
	// Negatives: near-miss errnos must NOT match.
	fmt.Println(errors.Is(syscall.EACCES, fs.ErrNotExist))
	fmt.Println(errors.Is(syscall.EINVAL, errors.ErrUnsupported))
	fmt.Println(errors.Is(syscall.ENOENT, fs.ErrPermission))
	fmt.Println(errors.Is(syscall.EEXIST, fs.ErrNotExist))
	// Direct identity still works.
	fmt.Println(errors.Is(syscall.ENOENT, syscall.ENOENT))
	fmt.Println(errors.Is(syscall.EAGAIN, syscall.EAGAIN))
	// Timeout / Temporary.
	fmt.Println(syscall.EAGAIN.Timeout(), syscall.EWOULDBLOCK.Timeout(), syscall.ETIMEDOUT.Timeout())
	fmt.Println(syscall.EINTR.Timeout())
	fmt.Println(syscall.EINTR.Temporary(), syscall.EAGAIN.Temporary(),
		syscall.EMFILE.Temporary(), syscall.ENFILE.Temporary())
	fmt.Println(syscall.EINVAL.Temporary(), syscall.ENOENT.Temporary())
	// Renderings.
	fmt.Println(syscall.ENOENT.Error())
	fmt.Println(syscall.EACCES.Error())
	fmt.Println(syscall.EAGAIN.Error())
	fmt.Println(syscall.Errno(9999).Error())
	fmt.Println(syscall.SIGINT.String())
	fmt.Println(syscall.SIGKILL.String())
	fmt.Println(syscall.Signal(999).String())
}
