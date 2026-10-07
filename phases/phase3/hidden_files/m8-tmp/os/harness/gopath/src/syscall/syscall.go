package syscall

type Errno uintptr
func (e Errno) Error() string { return "errno" }
func (e Errno) Is(target error) bool {
	t, ok := target.(Errno)
	return ok && e == t
}
type RawConn interface {
	Control(func(fd uintptr)) error
	Read(func(fd uintptr) bool) error
	Write(func(fd uintptr) bool) error
}
const (
	EPERM        = Errno(1)
	ENOENT       = Errno(2)
	EIO          = Errno(5)
	EBADF        = Errno(9)
	EACCES       = Errno(13)
	EEXIST       = Errno(17)
	EXDEV        = Errno(18)
	ENOTDIR      = Errno(20)
	EISDIR       = Errno(21)
	EINVAL       = Errno(22)
	ESPIPE       = Errno(29)
	EPIPE        = Errno(32)
	ENAMETOOLONG = Errno(36)
	ENOTEMPTY    = Errno(39)
	ELOOP        = Errno(40)
)
