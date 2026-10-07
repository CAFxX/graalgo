package os

import _ "unsafe" // for go:linkname
import "errors"
import "io"
import "io/fs"
import "syscall"
import "time"

// ---------------------------------------------------------------------------
// Linkname intrinsics (ADR-0004): symbols graalgo.os.<name>, bound by
// graalgo.p3os.GoOs. All params/results are string/int64/[]byte (the D5
// shapes); int64 results are negative = -errno. The local name must equal
// the declared name (exporter scope-checks this, mirroring gc).
// ---------------------------------------------------------------------------

//go:linkname osOpen graalgo.os.open
func osOpen(path string, flags int64, perm int64) int64

//go:linkname osClose graalgo.os.close
func osClose(h int64) int64

//go:linkname osRead graalgo.os.read
func osRead(h int64, b []byte) int64

//go:linkname osWrite graalgo.os.write
func osWrite(h int64, b []byte) int64

//go:linkname osReadAt graalgo.os.readAt
func osReadAt(h int64, b []byte, pos int64) int64

//go:linkname osWriteAt graalgo.os.writeAt
func osWriteAt(h int64, b []byte, pos int64) int64

//go:linkname osSeek graalgo.os.seek
func osSeek(h int64, off int64, whence int64) int64

//go:linkname osTruncate graalgo.os.truncate
func osTruncate(h int64, size int64) int64

//go:linkname osSync graalgo.os.sync
func osSync(h int64) int64

//go:linkname osPipe graalgo.os.pipe
func osPipe() int64

//go:linkname osStdio graalgo.os.stdio
func osStdio(fd int64) int64

//go:linkname osHandleKind graalgo.os.handleKind
func osHandleKind(h int64) int64

//go:linkname osStatMode graalgo.os.statMode
func osStatMode(path string, follow int64) int64

//go:linkname osStatSize graalgo.os.statSize
func osStatSize(path string, follow int64) int64

//go:linkname osStatMtimeNs graalgo.os.statMtimeNs
func osStatMtimeNs(path string, follow int64) int64

//go:linkname osStatDev graalgo.os.statDev
func osStatDev(path string, follow int64) int64

//go:linkname osStatIno graalgo.os.statIno
func osStatIno(path string, follow int64) int64

//go:linkname osDirNext graalgo.os.dirNext
func osDirNext(h int64, max int64) string

//go:linkname osMkdir graalgo.os.mkdir
func osMkdir(path string, perm int64) int64

//go:linkname osRemove graalgo.os.remove
func osRemove(path string) int64

//go:linkname osRename graalgo.os.rename
func osRename(oldpath string, newpath string) int64

//go:linkname osSymlink graalgo.os.symlink
func osSymlink(oldname string, newname string) int64

//go:linkname osReadlink graalgo.os.readlink
func osReadlink(path string, b []byte) int64

//go:linkname osLink graalgo.os.link
func osLink(oldname string, newname string) int64

//go:linkname osChmod graalgo.os.chmod
func osChmod(path string, perm int64) int64

//go:linkname osChtimes graalgo.os.chtimes
func osChtimes(path string, atimeNs int64, mtimeNs int64) int64

//go:linkname osTruncatePath graalgo.os.truncatePath
func osTruncatePath(path string, size int64) int64

//go:linkname osGetenvFound graalgo.os.getenvFound
func osGetenvFound(key string) int64

//go:linkname osGetenv graalgo.os.getenv
func osGetenv(key string) string

//go:linkname osSetenv graalgo.os.setenv
func osSetenv(key string, val string) int64

//go:linkname osUnsetenv graalgo.os.unsetenv
func osUnsetenv(key string) int64

//go:linkname osClearenv graalgo.os.clearenv
func osClearenv() int64

//go:linkname osEnviron graalgo.os.environ
func osEnviron() string

//go:linkname osGetpid graalgo.os.getpid
func osGetpid() int64

//go:linkname osGetppid graalgo.os.getppid
func osGetppid() int64

//go:linkname osGetuid graalgo.os.getuid
func osGetuid() int64

//go:linkname osGeteuid graalgo.os.geteuid
func osGeteuid() int64

//go:linkname osGetgid graalgo.os.getgid
func osGetgid() int64

//go:linkname osGetegid graalgo.os.getegid
func osGetegid() int64

//go:linkname osGetgroups graalgo.os.getgroups
func osGetgroups() string

//go:linkname osHostname graalgo.os.hostname
func osHostname(b []byte) int64

//go:linkname osExit graalgo.os.exit
func osExit(code int64) int64

//go:linkname osArgc graalgo.os.argc
func osArgc() int64

//go:linkname osArgv graalgo.os.argv
func osArgv(i int64) string

//go:linkname osGetcwd graalgo.os.getcwd
func osGetcwd() string

//go:linkname osChdir graalgo.os.chdir
func osChdir(path string) int64

// runtime.fastrand is registered by the P3-M4 linkname table
// (LinknameIntrinsics); the os worker reuses it for CreateTemp names
// (M4 audit work item 3 sanctions this reuse).
//go:linkname fastrand runtime.fastrand
func fastrand() uint32

// ---------------------------------------------------------------------------
// Constants.
// ---------------------------------------------------------------------------

const (
	O_RDONLY int = 0
	O_WRONLY int = 1
	O_RDWR   int = 2
	O_APPEND int = 1024
	O_CREATE int = 64
	O_EXCL   int = 128
	O_SYNC   int = 1052672
	O_TRUNC  int = 512
)

const (
	SEEK_SET int = 0
	SEEK_CUR int = 1
	SEEK_END int = 2
)

// Handle kinds cached in File.kind (os.md §2.2).
const (
	kindRegular int64 = 1
	kindDir     int64 = 2
	kindPipe    int64 = 3
	kindStdio   int64 = 4
)

// FileMode bit aliases (gc os re-exports fs's constants).
const (
	ModeDir        = fs.ModeDir
	ModeAppend     = fs.ModeAppend
	ModeExclusive  = fs.ModeExclusive
	ModeTemporary  = fs.ModeTemporary
	ModeSymlink    = fs.ModeSymlink
	ModeDevice     = fs.ModeDevice
	ModeNamedPipe  = fs.ModeNamedPipe
	ModeSocket     = fs.ModeSocket
	ModeSetuid     = fs.ModeSetuid
	ModeSetgid     = fs.ModeSetgid
	ModeCharDevice = fs.ModeCharDevice
	ModeSticky     = fs.ModeSticky
	ModeIrregular  = fs.ModeIrregular
	ModeType       = fs.ModeType
	ModePerm       = fs.ModePerm
)

// ---------------------------------------------------------------------------
// Package-level vars.
// ---------------------------------------------------------------------------

// Args holds the command-line arguments: Args[0] is the input-file path
// (the documented analog of go run's temp-binary path, R6); the rest are
// the launcher arguments after --. Initialized from the argc/argv
// intrinsics; GoOs.init(argv) runs before Go init (staged P3SyncMain
// patch — the ordering is load-bearing).
var Args = __initArgs()

func __initArgs() []string {
	n := osArgc()
	args := make([]string, 0, n)
	for i := int64(0); i < n; i++ {
		args = append(args, osArgv(i))
	}
	return args
}

// Stdin, Stdout and Stderr are open Files pointing to the standard input,
// standard output, and standard error file descriptors.
var (
	Stdin  = NewFile(0, "/dev/stdin")
	Stdout = NewFile(1, "/dev/stdout")
	Stderr = NewFile(2, "/dev/stderr")
)

// DevNull is the name of the operating system's "null device."
const DevNull = "/dev/null"

// ---------------------------------------------------------------------------
// Error types and predicates (gc os/error.go, error_errno.go, ported).
// ---------------------------------------------------------------------------

// Portable analogs of some common system call errors.
var (
	// ErrInvalid indicates an invalid argument.
	// Methods on File will return this error when the receiver is nil.
	ErrInvalid = fs.ErrInvalid // "invalid argument"

	ErrPermission = fs.ErrPermission // "permission denied"
	ErrExist      = fs.ErrExist      // "file already exists"
	ErrNotExist   = fs.ErrNotExist   // "file does not exist"
	ErrClosed     = fs.ErrClosed     // "file already closed"

	ErrNoDeadline       = errNoDeadline()       // "file type does not support deadline"
	ErrDeadlineExceeded = errDeadlineExceeded() // "i/o timeout"
)

func errNoDeadline() error       { return errors.New("file type does not support deadline") }
func errDeadlineExceeded() error { return errors.New("i/o timeout") }

type timeout interface {
	Timeout() bool
}

// PathError records an error and the operation and file path that caused it.
type PathError = fs.PathError

// FileInfo describes a file and is returned by Stat and Lstat.
type FileInfo = fs.FileInfo

// FileMode represents a file's mode and permission bits.
type FileMode = fs.FileMode

// DirEntry is an entry read from a directory.
type DirEntry = fs.DirEntry

// SyscallError records an error from a specific system call.
type SyscallError struct {
	Syscall string
	Err     error
}

func (e *SyscallError) Error() string { return e.Syscall + ": " + e.Err.Error() }

func (e *SyscallError) Unwrap() error { return e.Err }

// Timeout reports whether this error represents a timeout.
func (e *SyscallError) Timeout() bool {
	t, ok := e.Err.(timeout)
	return ok && t.Timeout()
}

// NewSyscallError returns, as an error, a new SyscallError
// with the given system call name and error details.
// As a convenience, if err is nil, NewSyscallError returns nil.
func NewSyscallError(syscall string, err error) error {
	if err == nil {
		return nil
	}
	return &SyscallError{syscall, err}
}

// LinkError records an error during a link or symlink system call and
// the old and new paths that caused it.
type LinkError struct {
	Op  string
	Old string
	New string
	Err error
}

func (e *LinkError) Error() string { return e.Op + " " + e.Old + " " + e.New + ": " + e.Err.Error() }

func (e *LinkError) Unwrap() error { return e.Err }

// syscallErrorType is the error type the Is* predicates look through,
// exactly like gc (os/error_errno.go).
type syscallErrorType = syscall.Errno

// IsExist returns a boolean indicating whether its argument is known to report
// that a file or directory already exists.
func IsExist(err error) bool {
	return underlyingErrorIs(err, ErrExist)
}

// IsNotExist returns a boolean indicating whether its argument is known to
// report that a file or directory does not exist.
func IsNotExist(err error) bool {
	return underlyingErrorIs(err, ErrNotExist)
}

// IsPermission returns a boolean indicating whether its argument is known to
// report that permission is denied.
func IsPermission(err error) bool {
	return underlyingErrorIs(err, ErrPermission)
}

// IsTimeout returns a boolean indicating whether its argument is known
// to report that a timeout occurred.
func IsTimeout(err error) bool {
	terr, ok := underlyingError(err).(timeout)
	return ok && terr.Timeout()
}

func underlyingErrorIs(err, target error) bool {
	// Note that this function is not errors.Is:
	// underlyingError only unwraps the specific error-wrapping types
	// that it historically did, not all errors implementing Unwrap().
	err = underlyingError(err)
	if err == target {
		return true
	}
	// To preserve prior behavior, only examine syscall errors.
	e, ok := err.(syscallErrorType)
	return ok && e.Is(target)
}

// underlyingError returns the underlying error for known os error types.
func underlyingError(err error) error {
	switch err := err.(type) {
	case *PathError:
		return err.Err
	case *LinkError:
		return err.Err
	case *SyscallError:
		return err.Err
	}
	return err
}

// newPathError builds the *PathError for a failed intrinsic call: negErrno
// is the intrinsic's negative -errno return.
func newPathError(op, path string, negErrno int64) error {
	return &PathError{Op: op, Path: path, Err: syscall.Errno(-negErrno)}
}

// newLinkError builds the *LinkError for a failed link-family intrinsic.
func newLinkError(op, oldname, newname string, negErrno int64) error {
	return &LinkError{Op: op, Old: oldname, New: newname, Err: syscall.Errno(-negErrno)}
}

var errNegativeOffset = errors.New("negative offset")

// errSetuidBits is the descriptive error for chmod/open modes carrying
// setuid/setgid/sticky bits (os.md §6.3): the JVM has no numeric chmod(2);
// masking silently is forbidden. Re-test: Panama FFI numeric-chmod.
var errSetuidBits = errors.New("setuid/setgid/sticky bits not supported (need real chmod(2); re-test: Panama FFI)")

// ---------------------------------------------------------------------------
// File.
// ---------------------------------------------------------------------------

// File represents an open file descriptor (a GoOs cookie handle, §2.2 —
// not an OS fd; see Fd).
type File struct {
	handle int64  // GoOs cookie
	name   string // user-supplied name (PathError.Path keeps this, R3)
	kind   int64  // kindRegular/kindDir/kindPipe/kindStdio/kindUnknown, cached at open
	closed bool   // set by Close; second Close reports ErrClosed like gc
	// appendMode records O_APPEND at open: WriteAt on such files fails
	// with errWriteAtInAppendMode, like gc.
	appendMode bool
	// dirIterStarted records whether a directory read has begun: gc
	// returns EISDIR for non-rewind seeks once iteration started
	// (verified go1.27.1: pre-iteration dir seeks succeed).
	dirIterStarted bool
}

// kindUnknown covers NewFile on a foreign-but-nonnegative fd: gc returns
// a non-nil *File (nil only for negative fds — verified go1.27.1, against
// the doc comment); ops on it fail with EBADF at the intrinsic, like gc.
const kindUnknown int64 = 0

func newFile(handle int64, name string, kind int64, appendMode bool) *File {
	return &File{handle: handle, name: name, kind: kind, appendMode: appendMode}
}

// checkValid mirrors gc's (f *File) checkValid: nil receiver -> ErrInvalid.
func checkValidFile(f *File) error {
	if f == nil {
		return ErrInvalid
	}
	return nil
}

// wrapFileErr mirrors gc's (f *File) wrapErr: nil/ok passthrough, else
// *PathError with the user-supplied name.
func wrapFileErr(f *File, op string, err error) error {
	if err == nil {
		return nil
	}
	return &PathError{Op: op, Path: f.name, Err: err}
}

// Name returns the name of the file as presented to Open.
func (f *File) Name() string { return f.name }

// Fd returns the file descriptor (here: the GoOs cookie, §2.2).
// It is NOT an OS fd; it round-trips with NewFile within the GoOs domain.
func (f *File) Fd() uintptr { return uintptr(f.handle) }

// ---------------------------------------------------------------------------
// Pair/triple structs for the Appendix-M desugar (single-result
// __-helpers; field order matches the multi-result signature order).
// ---------------------------------------------------------------------------

type openPair struct {
	f   *File
	err error
}

type statPair struct {
	fi  FileInfo
	err error
}

type readFilePair struct {
	b   []byte
	err error
}

type readDirPair struct {
	de  []DirEntry
	err error
}

type mkdirTempPair struct {
	s   string
	err error
}

type readlinkPair struct {
	s   string
	err error
}

type getwdPair struct {
	s   string
	err error
}

type pipeTriple struct {
	r   *File
	w   *File
	err error
}

type hostnamePair struct {
	s   string
	err error
}

type lookupEnvPair struct {
	s  string
	ok bool
}

type stringErrPair struct {
	s   string
	err error
}

type readPair struct {
	n   int
	err error
}

type writePair struct {
	n   int
	err error
}

type seekPair struct {
	off int64
	err error
}

type readdirPair struct {
	fi  []FileInfo
	err error
}

type readdirnamesPair struct {
	names []string
	err   error
}

type readFromPair struct {
	n   int64
	err error
}

type writeToPair struct {
	n   int64
	err error
}

type infoPair struct {
	fi  FileInfo
	err error
}

// ---------------------------------------------------------------------------
// Small path helpers (no path/filepath synthetic package in M8).
// ---------------------------------------------------------------------------

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	if IsPathSeparator(dir[len(dir)-1]) {
		return dir + name
	}
	return dir + string('/') + name
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if IsPathSeparator(path[i]) {
			return path[i+1:]
		}
	}
	return path
}

// IsPathSeparator reports whether c is a directory separator character.
func IsPathSeparator(c uint8) bool {
	return c == '/'
}

// ---------------------------------------------------------------------------
// Open / Create / OpenFile.
// ---------------------------------------------------------------------------

// Open opens the named file for reading. If successful, methods on
// the returned file can be used for reading; the associated file
// descriptor has mode O_RDONLY.
func Open(name string) (*File, error) {
	panic("os.Open: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __open(name string) openPair {
	return __openFile(name, O_RDONLY, 0)
}

// Create creates or truncates the named file. If the file already exists,
// it is truncated.
func Create(name string) (*File, error) {
	panic("os.Create: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __create(name string) openPair {
	return __openFile(name, O_RDWR|O_CREATE|O_TRUNC, 0666)
}

// OpenFile is the generalized open call.
func OpenFile(name string, flag int, perm FileMode) (*File, error) {
	panic("os.OpenFile: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __openFile(name string, flag int, perm FileMode) openPair {
	if name == "" {
		return openPair{nil, &PathError{Op: "open", Path: name, Err: syscall.ENOENT}}
	}
	if perm&(ModeSetuid|ModeSetgid|ModeSticky) != 0 {
		// §6.3 analog for open: loud descriptive error, never mask.
		return openPair{nil, &PathError{Op: "open", Path: name, Err: errSetuidBits}}
	}
	h := osOpen(name, int64(flag), int64(perm.Perm()))
	if h < 0 {
		return openPair{nil, newPathError("open", name, h)}
	}
	kind := osHandleKind(h)
	if kind < 0 {
		osClose(h)
		return openPair{nil, newPathError("open", name, kind)}
	}
	return openPair{newFile(h, name, kind, flag&O_APPEND != 0), nil}
}

// NewFile returns a new File with the given file descriptor and name.
//
// M8 §6.9 correction (verified go1.27.1 2026-10-07, against the doc
// comment): gc returns nil ONLY for negative fds; a foreign
// non-negative fd yields a non-nil *File whose ops fail with EBADF.
// The analysis §6.9 "(gc-faithful)" annotation was wrong; this matches
// gc. It never panics.
func NewFile(fd uintptr, name string) *File {
	fdi := int64(fd)
	if fdi < 0 {
		return nil
	}
	if fd <= 2 {
		h := osStdio(fdi)
		if h < 0 {
			return nil
		}
		return newFile(h, name, kindStdio, false)
	}
	kind := osHandleKind(fdi)
	if kind < 0 {
		kind = kindUnknown
	}
	return newFile(fdi, name, kind, false)
}

// ---------------------------------------------------------------------------
// Stat / Lstat.
// ---------------------------------------------------------------------------

// Stat returns the FileInfo structure describing file.
func Stat(name string) (FileInfo, error) {
	panic("os.Stat: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __stat(name string) statPair {
	return __statFollow(name, 1, "stat")
}

// Lstat returns the FileInfo structure describing the named file.
// If the file is a symbolic link, the returned FileInfo describes the
// symbolic link.
func Lstat(name string) (FileInfo, error) {
	panic("os.Lstat: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __lstat(name string) statPair {
	return __statFollow(name, 0, "lstat")
}

func __statFollow(name string, follow int64, op string) statPair {
	if name == "" {
		return statPair{nil, &PathError{Op: op, Path: name, Err: syscall.ENOENT}}
	}
	mode := osStatMode(name, follow)
	if mode < 0 {
		return statPair{nil, newPathError(op, name, mode)}
	}
	size := osStatSize(name, follow)
	if size < 0 {
		return statPair{nil, newPathError(op, name, size)}
	}
	mtimeNs := osStatMtimeNs(name, follow)
	if mtimeNs < 0 {
		return statPair{nil, newPathError(op, name, mtimeNs)}
	}
	dev := osStatDev(name, follow)
	if dev < 0 {
		return statPair{nil, newPathError(op, name, dev)}
	}
	ino := osStatIno(name, follow)
	if ino < 0 {
		return statPair{nil, newPathError(op, name, ino)}
	}
	return statPair{&fileStat{name: name, size: size, mode: __modeFromStat(mode), modNs: mtimeNs, dev: dev, ino: ino}, nil}
}

// fileStat implements FileInfo from scalar stat intrinsics.
type fileStat struct {
	name  string
	size  int64
	mode  FileMode
	modNs int64
	dev   int64
	ino   int64
}

func (fs *fileStat) Name() string       { return baseName(fs.name) }
func (fs *fileStat) Size() int64        { return fs.size }
func (fs *fileStat) Mode() FileMode    { return fs.mode }
func (fs *fileStat) ModTime() time.Time { return __nanosToTime(fs.modNs) }
func (fs *fileStat) IsDir() bool       { return fs.mode.IsDir() }
func (fs *fileStat) Sys() any          { return nil }

// __modeFromStat converts a full st_mode (type + suid/sgid/sticky + 0777)
// to FileMode bits.
func __modeFromStat(stMode int64) FileMode {
	var m FileMode
	switch stMode & 0170000 {
	case 0040000:
		m |= ModeDir
	case 0120000:
		m |= ModeSymlink
	case 0010000:
		m |= ModeNamedPipe
	case 0140000:
		m |= ModeSocket
	case 0060000:
		m |= ModeDevice | ModeCharDevice
	case 0020000:
		m |= ModeDevice
	}
	if stMode&04000 != 0 {
		m |= ModeSetuid
	}
	if stMode&02000 != 0 {
		m |= ModeSetgid
	}
	if stMode&01000 != 0 {
		m |= ModeSticky
	}
	m |= FileMode(stMode & 0777)
	return m
}

// __nanosToTime converts unix nanos to a time.Time via civil-date
// arithmetic (the synthetic time package has no time.Unix).
func __nanosToTime(ns int64) time.Time {
	secs := ns / 1e9
	nsec := ns % 1e9
	if nsec < 0 {
		nsec += 1e9
		secs--
	}
	days := secs / 86400
	rem := secs % 86400
	if rem < 0 {
		rem += 86400
		days--
	}
	// civil_from_days (Howard Hinnant's algorithm), days since 1970-01-01.
	z := days + 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era--
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	mo := mp + 3
	if mo > 12 {
		mo -= 12
		y++
	}
	year := int(y)
	month := time.Month(mo)
	day := int(d)
	hour := int(rem / 3600)
	min := int((rem % 3600) / 60)
	sec := int(rem % 60)
	return time.Date(year, month, day, hour, min, sec, int(nsec), time.UTC)
}

// SameFile reports whether fi1 and fi2 describe the same file.
// Like gc, identity is dev+ino carried in the FileInfo.
func SameFile(fi1, fi2 FileInfo) bool {
	fs1, ok1 := fi1.(*fileStat)
	fs2, ok2 := fi2.(*fileStat)
	if !ok1 || !ok2 {
		return false
	}
	return fs1.dev == fs2.dev && fs1.ino == fs2.ino
}

// ---------------------------------------------------------------------------
// File I/O methods.
// ---------------------------------------------------------------------------

// Read reads up to len(b) bytes from the File and stores them in b.
// It returns the number of bytes read and any error encountered.
// At end of file, Read returns 0, io.EOF.
func (f *File) Read(b []byte) (n int, err error) {
	panic("os.File.Read: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// __fileRead performs exactly one intrinsic read (R2): no Go-side
// looping, so partial reads and EOF surface exactly like gc.
func __fileRead(f *File, b []byte) readPair {
	if err := checkValidFile(f); err != nil {
		return readPair{0, err}
	}
	if f.closed {
		return readPair{0, &PathError{Op: "read", Path: f.name, Err: ErrClosed}}
	}
	if f.kind == kindDir {
		// gc: read on a directory -> (0, *PathError{Op:"read", Err:EISDIR}).
		return readPair{0, &PathError{Op: "read", Path: f.name, Err: syscall.EISDIR}}
	}
	r := osRead(f.handle, b)
	if r < 0 {
		return readPair{0, wrapFileErr(f, "read", syscall.Errno(-r))}
	}
	n := int(r - 1) // n+1 encoding (§2.3); 0 = EOF
	if r == 0 {
		return readPair{0, io.EOF}
	}
	return readPair{n, nil}
}

// ReadAt reads len(b) bytes from the File starting at byte offset off.
func (f *File) ReadAt(b []byte, off int64) (n int, err error) {
	panic("os.File.ReadAt: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileReadAt(f *File, b []byte, off int64) readPair {
	if err := checkValidFile(f); err != nil {
		return readPair{0, err}
	}
	if f.closed {
		return readPair{0, &PathError{Op: "read", Path: f.name, Err: ErrClosed}}
	}
	if off < 0 {
		return readPair{0, &PathError{Op: "readat", Path: f.name, Err: errNegativeOffset}}
	}
	if f.kind != kindRegular {
		return readPair{0, &PathError{Op: "read", Path: f.name, Err: syscall.ESPIPE}}
	}
	r := osReadAt(f.handle, b, off)
	if r < 0 {
		return readPair{0, wrapFileErr(f, "read", syscall.Errno(-r))}
	}
	n := int(r - 1)
	if r == 0 {
		return readPair{0, io.EOF}
	}
	return readPair{n, nil}
}

// Write writes len(b) bytes to the File.
func (f *File) Write(b []byte) (n int, err error) {
	panic("os.File.Write: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// __fileWrite performs exactly one intrinsic write (R2); a short write
// yields io.ErrShortWrite, exactly like gc.
func __fileWrite(f *File, b []byte) writePair {
	if err := checkValidFile(f); err != nil {
		return writePair{0, err}
	}
	if f.closed {
		return writePair{0, &PathError{Op: "write", Path: f.name, Err: ErrClosed}}
	}
	w := osWrite(f.handle, b)
	if w < 0 {
		// EPIPE: GoOs has already raised a real SIGPIPE (R7) if the
		// signal path was available; if we are still alive the EPIPE
		// error surfaces here (loud fallback — the SIGPIPE corpus
		// program fails loudly instead of drifting).
		return writePair{0, wrapFileErr(f, "write", syscall.Errno(-w))}
	}
	n := int(w - 1)
	var err error
	if n != len(b) {
		err = io.ErrShortWrite
	}
	return writePair{n, err}
}

// WriteAt writes len(b) bytes to the File starting at byte offset off.
func (f *File) WriteAt(b []byte, off int64) (n int, err error) {
	panic("os.File.WriteAt: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

var errWriteAtInAppendMode = errors.New("os: invalid use of WriteAt on file opened with O_APPEND")

func __fileWriteAt(f *File, b []byte, off int64) writePair {
	if err := checkValidFile(f); err != nil {
		return writePair{0, err}
	}
	if f.closed {
		return writePair{0, &PathError{Op: "write", Path: f.name, Err: ErrClosed}}
	}
	if f.appendMode {
		return writePair{0, errWriteAtInAppendMode}
	}
	if off < 0 {
		return writePair{0, &PathError{Op: "writeat", Path: f.name, Err: errNegativeOffset}}
	}
	if f.kind != kindRegular {
		return writePair{0, &PathError{Op: "write", Path: f.name, Err: syscall.ESPIPE}}
	}
	w := osWriteAt(f.handle, b, off)
	if w < 0 {
		return writePair{0, wrapFileErr(f, "write", syscall.Errno(-w))}
	}
	n := int(w - 1)
	var err error
	if n != len(b) {
		err = io.ErrShortWrite
	}
	return writePair{n, err}
}

// WriteString is like Write, but writes the contents of string s rather
// than a slice of bytes.
func (f *File) WriteString(s string) (n int, err error) {
	panic("os.File.WriteString: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileWriteString(f *File, s string) writePair {
	return __fileWrite(f, []byte(s))
}

// Seek sets the offset for the next Read or Write on file to offset,
// interpreted according to whence.
func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	panic("os.File.Seek: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileSeek(f *File, offset int64, whence int) seekPair {
	if err := checkValidFile(f); err != nil {
		return seekPair{0, err}
	}
	if f.closed {
		return seekPair{0, &PathError{Op: "seek", Path: f.name, Err: ErrClosed}}
	}
	if f.kind == kindDir {
		// gc-faithful (verified go1.27.1): pre-iteration dir seeks
		// succeed (the lseek result); Seek(0, SEEK_SET) rewinds
		// iteration; once iteration started, any non-rewind seek
		// yields EISDIR. (The analysis §6.10 probe only covered
		// (0,0) pre-iteration.)
		if offset == 0 && whence == SEEK_SET {
			e := osSeek(f.handle, 0, 0)
			if e < 0 {
				return seekPair{0, wrapFileErr(f, "seek", syscall.Errno(-e))}
			}
			f.dirIterStarted = false
			return seekPair{0, nil}
		}
		if f.dirIterStarted {
			return seekPair{0, &PathError{Op: "seek", Path: f.name, Err: syscall.EISDIR}}
		}
		// Pre-iteration: gc returns the lseek result. SEEK_SET ->
		// offset; SEEK_CUR -> 0+offset (fresh position is 0);
		// SEEK_END -> dirsize+offset (intrinsic stats the dir).
		e := osSeek(f.handle, offset, int64(whence))
		if e < 0 {
			return seekPair{0, wrapFileErr(f, "seek", syscall.Errno(-e))}
		}
		return seekPair{e, nil}
	}
	if f.kind == kindPipe || f.kind == kindStdio {
		return seekPair{0, &PathError{Op: "seek", Path: f.name, Err: syscall.ESPIPE}}
	}
	e := osSeek(f.handle, offset, int64(whence))
	if e < 0 {
		return seekPair{0, wrapFileErr(f, "seek", syscall.Errno(-e))}
	}
	return seekPair{e, nil}
}

// Close closes the File, rendering it unusable for I/O.
func (f *File) Close() error {
	p := __close(f)
	return p.err
}

type closePair struct {
	err error
}

func __close(f *File) closePair {
	if err := checkValidFile(f); err != nil {
		return closePair{err}
	}
	if f.closed {
		return closePair{&PathError{Op: "close", Path: f.name, Err: ErrClosed}}
	}
	f.closed = true
	e := osClose(f.handle)
	if e < 0 {
		return closePair{newPathError("close", f.name, e)}
	}
	return closePair{nil}
}

// Stat returns the FileInfo structure describing file.
func (f *File) Stat() (FileInfo, error) {
	panic("os.File.Stat: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileStat(f *File) statPair {
	if err := checkValidFile(f); err != nil {
		return statPair{nil, err}
	}
	if f.closed {
		return statPair{nil, &PathError{Op: "stat", Path: f.name, Err: ErrClosed}}
	}
	switch f.kind {
	case kindUnknown:
		// gc: fstat on an invalid fd -> EBADF.
		return statPair{nil, &PathError{Op: "stat", Path: f.name, Err: syscall.EBADF}}
	case kindRegular:
		// D2: the JVM has no fstat-on-channel; stat the stored open path.
		return __statFollow(f.name, 1, "stat")
	case kindDir:
		return __statFollow(f.name, 1, "stat")
	default:
		// D2: pipe/stdio modes are synthesized from the handle kind
		// (S_IFIFO/S_IFCHR, size 0). dev=1/ino=cookie keeps SameFile
		// coherent within the GoOs domain.
		var mode FileMode
		if f.kind == kindPipe {
			mode = ModeNamedPipe
		} else {
			mode = ModeDevice | ModeCharDevice
		}
		return statPair{&fileStat{name: f.name, size: 0, mode: mode, modNs: 0, dev: 1, ino: f.handle}, nil}
	}
}

// Sync commits the current contents of the file to stable storage.
func (f *File) Sync() error {
	if err := checkValidFile(f); err != nil {
		return err
	}
	if f.closed {
		return &PathError{Op: "sync", Path: f.name, Err: ErrClosed}
	}
	if f.kind == kindUnknown {
		return &PathError{Op: "sync", Path: f.name, Err: syscall.EBADF}
	}
	if f.kind != kindRegular {
		// gc: fsync on pipes/special files -> EINVAL.
		return &PathError{Op: "sync", Path: f.name, Err: syscall.EINVAL}
	}
	e := osSync(f.handle)
	if e < 0 {
		return newPathError("sync", f.name, e)
	}
	return nil
}

// Truncate changes the size of the file.
func (f *File) Truncate(size int64) error {
	if err := checkValidFile(f); err != nil {
		return err
	}
	if f.closed {
		return &PathError{Op: "truncate", Path: f.name, Err: ErrClosed}
	}
	if f.kind == kindUnknown {
		return &PathError{Op: "truncate", Path: f.name, Err: syscall.EBADF}
	}
	if f.kind != kindRegular {
		return &PathError{Op: "truncate", Path: f.name, Err: syscall.EINVAL}
	}
	e := osTruncate(f.handle, size)
	if e < 0 {
		return newPathError("truncate", f.name, e)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ReadFile / WriteFile.
// ---------------------------------------------------------------------------

// ReadFile reads the named file and returns the contents.
func ReadFile(name string) ([]byte, error) {
	panic("os.ReadFile: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __readFile(name string) readFilePair {
	p := __open(name)
	if p.err != nil {
		return readFilePair{nil, p.err}
	}
	f := p.f
	var b []byte
	buf := make([]byte, 32768)
	for {
		rp := __fileRead(f, buf)
		if rp.n > 0 {
			b = append(b, buf[:rp.n]...)
		}
		if rp.err != nil {
			if rp.err == io.EOF {
				cp := __close(f)
				return readFilePair{b, cp.err}
			}
			__close(f)
			return readFilePair{b, rp.err}
		}
	}
}

// WriteFile writes data to the named file, creating it if necessary.
func WriteFile(name string, data []byte, perm FileMode) error {
	p := __openFile(name, O_WRONLY|O_CREATE|O_TRUNC, perm)
	if p.err != nil {
		return p.err
	}
	f := p.f
	wp := __writeAll(f, data)
	if wp.err != nil {
		__close(f)
		return wp.err
	}
	return __close(f).err
}

// __writeAll writes all of b (used by WriteFile).
func __writeAll(f *File, b []byte) writePair {
	total := 0
	for total < len(b) {
		wp := __fileWrite(f, b[total:])
		total += wp.n
		if wp.err != nil {
			return writePair{total, wp.err}
		}
		if wp.n == 0 {
			return writePair{total, io.ErrShortWrite}
		}
	}
	return writePair{total, nil}
}

// ---------------------------------------------------------------------------
// Pipe.
// ---------------------------------------------------------------------------

// Pipe returns a connected pair of Files; reads from r return bytes
// written to w.
func Pipe() (r *File, w *File, err error) {
	panic("os.Pipe: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __pipe() pipeTriple {
	h := osPipe()
	if h < 0 {
		return pipeTriple{nil, nil, newPathError("pipe", "", h)}
	}
	// Consecutive cookies (r, r+1) — one intrinsic, no packing (§2.2).
	r := newFile(h, "|0", kindPipe, false)
	w := newFile(h+1, "|1", kindPipe, false)
	return pipeTriple{r, w, nil}
}

// ---------------------------------------------------------------------------
// Directory reading.
// ---------------------------------------------------------------------------

// dirEntry implements DirEntry from dirNext batches.
type dirEntry struct {
	dir  string // containing directory, for Info
	name string
	typ  FileMode
}

func (d *dirEntry) Name() string   { return d.name }
func (d *dirEntry) IsDir() bool    { return d.typ.IsDir() }
func (d *dirEntry) Type() FileMode { return d.typ }
func (d *dirEntry) Info() (FileInfo, error) {
	panic("os.dirEntry.Info: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// __dirEntryInfo is the desugar helper for DirEntry.Info (the interface
// method is declared in io/fs; the helper lives in os with a helperPkg
// override in the Appendix-M table).
func __dirEntryInfo(d DirEntry) infoPair {
	if de, ok := d.(*dirEntry); ok {
		p := __lstat(joinPath(de.dir, de.name))
		return infoPair{p.fi, p.err}
	}
	return infoPair{nil, errors.New("os: internal error: unknown DirEntry implementation")}
}

type dirRawEntry struct {
	name string
	typ  FileMode
}

type dirBatchPair struct {
	entries []dirRawEntry
	err     error
}

// __typeCharMode maps a dirNext type char to FileMode type bits.
func __typeCharMode(tc byte) FileMode {
	switch tc {
	case 'd':
		return ModeDir
	case 'f':
		return 0
	case 'l':
		return ModeSymlink
	case 'p':
		return ModeNamedPipe
	case 's':
		return ModeSocket
	case 'b':
		return ModeDevice
	case 'c':
		return ModeDevice | ModeCharDevice
	}
	return ModeIrregular
}

// __dirNextBatch fetches one dirNext batch. The Java side signals an
// iteration error with an empty-name/NUL-type sentinel (a real entry
// can never have an empty name).
func __dirNextBatch(f *File) dirBatchPair {
	s := osDirNext(f.handle, 64)
	var out []dirRawEntry
	i := 0
	for i < len(s) {
		j := i
		for j < len(s) && s[j] != 0 {
			j++
		}
		if j+1 >= len(s) {
			return dirBatchPair{nil, &PathError{Op: "readdir", Path: f.name, Err: errors.New("os: malformed dirNext encoding")}}
		}
		name := s[i:j]
		tc := s[j+1]
		if name == "" && tc == 0 {
			return dirBatchPair{nil, &PathError{Op: "readdir", Path: f.name, Err: syscall.EIO}}
		}
		out = append(out, dirRawEntry{name, __typeCharMode(tc)})
		i = j + 2
	}
	return dirBatchPair{out, nil}
}

// Readdirnames reads directory names, like Readdir, but returns only
// the names.
func (f *File) Readdirnames(n int) ([]string, error) {
	panic("os.File.Readdirnames: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileReaddirnames(f *File, n int) readdirnamesPair {
	if err := checkValidFile(f); err != nil {
		return readdirnamesPair{nil, err}
	}
	if f.closed {
		return readdirnamesPair{nil, &PathError{Op: "readdir", Path: f.name, Err: ErrClosed}}
	}
	if f.kind == kindUnknown {
		return readdirnamesPair{nil, &PathError{Op: "readdir", Path: f.name, Err: syscall.EBADF}}
	}
	if f.kind != kindDir {
		return readdirnamesPair{nil, &PathError{Op: "readdir", Path: f.name, Err: syscall.ENOTDIR}}
	}
	f.dirIterStarted = true
	var names []string
	want := n
	if want <= 0 {
		want = -1 // all
	}
	for want == -1 || len(names) < want {
		bp := __dirNextBatch(f)
		if bp.err != nil {
			return readdirnamesPair{names, bp.err}
		}
		if len(bp.entries) == 0 {
			break
		}
		for _, e := range bp.entries {
			names = append(names, e.name)
			if want != -1 && len(names) == want {
				break
			}
		}
	}
	if n > 0 && len(names) == 0 {
		return readdirnamesPair{nil, io.EOF}
	}
	return readdirnamesPair{names, nil}
}

// Readdir reads the contents of the directory associated with file.
func (f *File) Readdir(n int) ([]FileInfo, error) {
	panic("os.File.Readdir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileReaddir(f *File, n int) readdirPair {
	np := __fileReaddirnames(f, n)
	if np.err != nil {
		return readdirPair{nil, np.err}
	}
	infos := make([]FileInfo, 0, len(np.names))
	for _, filename := range np.names {
		sp := __lstat(joinPath(f.name, filename))
		if sp.err != nil {
			return readdirPair{infos, sp.err}
		}
		infos = append(infos, sp.fi)
	}
	return readdirPair{infos, nil}
}

// ReadDir reads the named directory, returning all its directory entries
// sorted by filename.
func ReadDir(name string) ([]DirEntry, error) {
	panic("os.ReadDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __readDir(name string) readDirPair {
	p := __open(name)
	if p.err != nil {
		return readDirPair{nil, p.err}
	}
	f := p.f
	if f.kind != kindDir {
		__close(f)
		return readDirPair{nil, &PathError{Op: "readdir", Path: name, Err: syscall.ENOTDIR}}
	}
	dp := __fileReadDir(f, -1)
	__close(f)
	if dp.err != nil {
		return readDirPair{nil, dp.err}
	}
	__sortDirEntries(dp.de)
	return readDirPair{dp.de, nil}
}

// ReadDir reads the contents of the directory associated with the file f
// and returns a slice of DirEntry values in directory order.
func (f *File) ReadDir(n int) ([]DirEntry, error) {
	panic("os.File.ReadDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileReadDir(f *File, n int) readDirPair {
	if err := checkValidFile(f); err != nil {
		return readDirPair{nil, err}
	}
	if f.closed {
		return readDirPair{nil, &PathError{Op: "readdir", Path: f.name, Err: ErrClosed}}
	}
	if f.kind == kindUnknown {
		return readDirPair{nil, &PathError{Op: "readdir", Path: f.name, Err: syscall.EBADF}}
	}
	if f.kind != kindDir {
		return readDirPair{nil, &PathError{Op: "readdir", Path: f.name, Err: syscall.ENOTDIR}}
	}
	f.dirIterStarted = true
	var out []DirEntry
	want := n
	if want <= 0 {
		want = -1 // all
	}
	for want == -1 || len(out) < want {
		bp := __dirNextBatch(f)
		if bp.err != nil {
			return readDirPair{out, bp.err}
		}
		if len(bp.entries) == 0 {
			break
		}
		for _, e := range bp.entries {
			out = append(out, &dirEntry{dir: f.name, name: e.name, typ: e.typ})
			if want != -1 && len(out) == want {
				break
			}
		}
	}
	if n > 0 && len(out) == 0 {
		return readDirPair{nil, io.EOF}
	}
	return readDirPair{out, nil}
}

// __sortDirEntries sorts entries by filename (insertion sort; directory
// listings are small, and this avoids depending on synthetic sort's
// exact API surface).
func __sortDirEntries(de []DirEntry) {
	for i := 1; i < len(de); i++ {
		for j := i; j > 0 && de[j].Name() < de[j-1].Name(); j-- {
			de[j], de[j-1] = de[j-1], de[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Chmod / Chown / Chtimes / Truncate.
// ---------------------------------------------------------------------------

// Chmod changes the mode of the named file to mode.
func Chmod(name string, mode FileMode) error {
	if mode&(ModeSetuid|ModeSetgid|ModeSticky) != 0 {
		// §6.3: descriptive error, never mask.
		return &PathError{Op: "chmod", Path: name, Err: errSetuidBits}
	}
	if name == "" {
		return &PathError{Op: "chmod", Path: name, Err: syscall.ENOENT}
	}
	e := osChmod(name, int64(mode.Perm()))
	if e < 0 {
		return newPathError("chmod", name, e)
	}
	return nil
}

// Chmod changes the mode of the file to mode.
func (f *File) Chmod(mode FileMode) error {
	if err := checkValidFile(f); err != nil {
		return err
	}
	// D2: no fchmod on a channel; chmod the stored open path.
	return Chmod(f.name, mode)
}

// Chown changes the numeric user and group IDs of the named file.
// M8: fail-fast (§6.2 — NIO has no numeric chown(2)).
func Chown(name string, uid, gid int) error {
	panic("os.Chown: not in M8 scope (needs numeric chown(2); re-test: Panama FFI numeric-chown intrinsic)")
}

// Lchown changes the numeric user and group IDs of the named file.
// M8: fail-fast (§6.2).
func Lchown(name string, uid, gid int) error {
	panic("os.Lchown: not in M8 scope (needs numeric chown(2); re-test: Panama FFI numeric-chown intrinsic)")
}

// Chown changes the numeric user and group IDs of the named file.
// M8: fail-fast (§6.2).
func (f *File) Chown(uid, gid int) error {
	panic("os.File.Chown: not in M8 scope (needs numeric chown(2); re-test: Panama FFI numeric-chown intrinsic)")
}

// Chtimes changes the access and modification times of the named file.
func Chtimes(name string, atime time.Time, mtime time.Time) error {
	if name == "" {
		return &PathError{Op: "chtimes", Path: name, Err: syscall.ENOENT}
	}
	e := osChtimes(name, atime.UnixNano(), mtime.UnixNano())
	if e < 0 {
		return newPathError("chtimes", name, e)
	}
	return nil
}

// Truncate changes the size of the named file.
func Truncate(name string, size int64) error {
	if name == "" {
		return &PathError{Op: "truncate", Path: name, Err: syscall.ENOENT}
	}
	e := osTruncatePath(name, size)
	if e < 0 {
		return newPathError("truncate", name, e)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Deadlines.
// ---------------------------------------------------------------------------

// SetDeadline sets the read and write deadlines for a File.
// Regular files correctly return ErrNoDeadline (like gc); pipes fail
// fast (§6.5 — needs timer-gated pipe I/O).
func (f *File) SetDeadline(t time.Time) error {
	if err := checkValidFile(f); err != nil {
		return err
	}
	if f.kind == kindPipe {
		panic("os.File.SetDeadline: not in M8 scope for pipes (needs timer-gated pipe I/O; re-test: netpoller/timer integration)")
	}
	return ErrNoDeadline
}

// SetReadDeadline sets the read deadline for a File.
func (f *File) SetReadDeadline(t time.Time) error {
	return f.SetDeadline(t)
}

// SetWriteDeadline sets the write deadline for a File.
func (f *File) SetWriteDeadline(t time.Time) error {
	return f.SetDeadline(t)
}

// ---------------------------------------------------------------------------
// Mkdir / MkdirAll / Remove / RemoveAll.
// ---------------------------------------------------------------------------

// Mkdir creates a new directory with the specified name and permission
// bits (before umask — D1: the JVM cannot read umask, so the requested
// 0777 bits are applied exactly).
func Mkdir(name string, perm FileMode) error {
	if name == "" {
		return &PathError{Op: "mkdir", Path: name, Err: syscall.ENOENT}
	}
	e := osMkdir(name, int64(perm.Perm()))
	if e < 0 {
		return newPathError("mkdir", name, e)
	}
	return nil
}

// MkdirAll creates a directory named path, along with any necessary
// parents, and returns nil, or else returns an error.
// (gc os/path.go, ported; Stat calls go through the __-helper since
// Stat is multi-result.)
func MkdirAll(path string, perm FileMode) error {
	// Fast path: if we can tell whether path is a directory or file,
	// stop with success or error.
	sp := __stat(path)
	if sp.err == nil {
		if sp.fi.IsDir() {
			return nil
		}
		return &PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	}

	// Slow path: make sure parent exists and then call Mkdir for path.
	i := len(path) - 1
	for i >= 0 && IsPathSeparator(path[i]) {
		i--
	}
	for i >= 0 && !IsPathSeparator(path[i]) {
		i--
	}
	if i < 0 {
		i = 0
	}

	if parent := path[:i]; len(parent) > 0 {
		if err := MkdirAll(parent, perm); err != nil {
			return err
		}
	}

	// Parent now exists; invoke Mkdir and use its result.
	if err := Mkdir(path, perm); err != nil {
		// Handle arguments like "foo/." by double-checking that the
		// directory doesn't exist.
		lp := __lstat(path)
		if lp.err == nil && lp.fi.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// Remove removes the named file or (empty) directory.
func Remove(name string) error {
	if name == "" {
		return &PathError{Op: "remove", Path: name, Err: syscall.ENOENT}
	}
	e := osRemove(name)
	if e < 0 {
		return newPathError("remove", name, e)
	}
	return nil
}

// endsWithDot reports whether the final component of path is ".".
func endsWithDot(path string) bool {
	if path == "." {
		return true
	}
	if len(path) >= 2 && path[len(path)-1] == '.' && IsPathSeparator(path[len(path)-2]) {
		return true
	}
	return false
}

// RemoveAll removes path and any children it contains.
// (gc removeall_at.go, ported over this package's own primitives; the
// *at-fd dance is unnecessary with the GoOs handle domain.)
func RemoveAll(path string) error {
	if path == "" {
		// fail silently to retain compatibility with previous behavior
		// of RemoveAll (gc issue 28830).
		return nil
	}

	// The rmdir system call does not permit removing ".",
	// so we don't permit it either.
	if endsWithDot(path) {
		return &PathError{Op: "RemoveAll", Path: path, Err: syscall.EINVAL}
	}

	// Simple case: if Remove works, we're done.
	if err := Remove(path); err == nil || IsNotExist(err) {
		return nil
	}

	lp := __lstat(path)
	if lp.err != nil {
		// IsNotExist-ish: gc returns nil when the parent does not exist.
		return nil
	}
	if !lp.fi.IsDir() {
		return Remove(path)
	}

	// Remove contents, then the directory itself.
	dp := __readDir(path)
	if dp.err != nil {
		return dp.err
	}
	for _, e := range dp.de {
		if err := RemoveAll(joinPath(path, e.Name())); err != nil {
			return err
		}
	}
	if err := Remove(path); err == nil || IsNotExist(err) {
		return nil
	} else {
		return err
	}
}

// ---------------------------------------------------------------------------
// Rename / Symlink / Readlink / Link.
// ---------------------------------------------------------------------------

// Rename renames (moves) oldname to newname.
func Rename(oldname, newname string) error {
	if oldname == "" || newname == "" {
		return &LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.ENOENT}
	}
	e := osRename(oldname, newname)
	if e < 0 {
		return newLinkError("rename", oldname, newname, e)
	}
	return nil
}

// Symlink creates newname as a symbolic link to oldname.
func Symlink(oldname, newname string) error {
	if newname == "" {
		return &LinkError{Op: "symlink", Old: oldname, New: newname, Err: syscall.ENOENT}
	}
	e := osSymlink(oldname, newname)
	if e < 0 {
		return newLinkError("symlink", oldname, newname, e)
	}
	return nil
}

// Readlink returns the destination of the named symbolic link.
func Readlink(name string) (string, error) {
	panic("os.Readlink: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __readlink(name string) readlinkPair {
	if name == "" {
		return readlinkPair{"", &PathError{Op: "readlink", Path: name, Err: syscall.ENOENT}}
	}
	for bufsize := 512; ; bufsize *= 2 {
		buf := make([]byte, bufsize)
		r := osReadlink(name, buf)
		if r < 0 {
			return readlinkPair{"", newPathError("readlink", name, r)}
		}
		n := int(r - 1)
		if n < bufsize {
			return readlinkPair{string(buf[:n]), nil}
		}
	}
}

// Link creates newname as a hard link to the oldname file.
func Link(oldname, newname string) error {
	if oldname == "" || newname == "" {
		return &LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.ENOENT}
	}
	e := osLink(oldname, newname)
	if e < 0 {
		return newLinkError("link", oldname, newname, e)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Getwd / Chdir.
// ---------------------------------------------------------------------------

// Getwd returns a rooted path name corresponding to the current directory.
func Getwd() (string, error) {
	panic("os.Getwd: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __getwd() getwdPair {
	// $PWD kludge (gc getwd.go): if $PWD names the current directory
	// (dev+ino identity), return it; otherwise the physical path.
	if pwd := osGetenv("PWD"); pwd != "" && len(pwd) > 0 && pwd[0] == '/' {
		dev1 := osStatDev(pwd, 1)
		ino1 := osStatIno(pwd, 1)
		dev2 := osStatDev(".", 1)
		ino2 := osStatIno(".", 1)
		if dev1 >= 0 && ino1 >= 0 && dev2 >= 0 && ino2 >= 0 && dev1 == dev2 && ino1 == ino2 {
			return getwdPair{pwd, nil}
		}
	}
	return getwdPair{osGetcwd(), nil}
}

// Chdir changes the current working directory to the named directory.
func Chdir(dir string) error {
	if dir == "" {
		return &PathError{Op: "chdir", Path: dir, Err: syscall.ENOENT}
	}
	e := osChdir(dir)
	if e < 0 {
		return newPathError("chdir", dir, e)
	}
	return nil
}

// Chdir changes the current working directory to the file's directory.
func (f *File) Chdir() error {
	if err := checkValidFile(f); err != nil {
		return err
	}
	return Chdir(f.name)
}

// ---------------------------------------------------------------------------
// Environment.
// ---------------------------------------------------------------------------

// Getenv retrieves the value of the environment variable named by the key.
func Getenv(key string) string {
	p := __lookupEnv(key)
	return p.s
}

// LookupEnv retrieves the value of the environment variable named by
// the key.
func LookupEnv(key string) (string, bool) {
	panic("os.LookupEnv: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __lookupEnv(key string) lookupEnvPair {
	if osGetenvFound(key) == 0 {
		return lookupEnvPair{"", false}
	}
	return lookupEnvPair{osGetenv(key), true}
}

// __validEnvKey mirrors syscall.Setenv's key validation.
func __validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '=' || key[i] == 0 {
			return false
		}
	}
	return true
}

// Setenv sets the value of the environment variable named by the key.
func Setenv(key, value string) error {
	if !__validEnvKey(key) {
		return syscall.EINVAL
	}
	e := osSetenv(key, value)
	if e < 0 {
		return syscall.Errno(-e)
	}
	return nil
}

// Unsetenv unsets a single environment variable.
func Unsetenv(key string) error {
	if !__validEnvKey(key) {
		return syscall.EINVAL
	}
	e := osUnsetenv(key)
	if e < 0 {
		return syscall.Errno(-e)
	}
	return nil
}

// Clearenv deletes all environment variables.
func Clearenv() {
	osClearenv()
}

// Environ returns a copy of strings representing the environment,
// in the form "key=value".
func Environ() []string {
	s := osEnviron()
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// Expand replaces ${var} or $var in the string based on the mapping function.
func Expand(s string, mapping func(string) string) string {
	var buf []byte
	// ${} is all ASCII, so bytes are fine for this operation.
	i := 0
	for j := 0; j < len(s); j++ {
		if s[j] == '$' && j+1 < len(s) {
			if buf == nil {
				buf = make([]byte, 0, 2*len(s))
			}
			buf = append(buf, s[i:j]...)
			name, w := getShellName(s[j+1:])
			if name == "" && w > 0 {
				// Encountered invalid syntax; eat the characters.
			} else if name == "" {
				// Valid syntax, but $ was not followed by a name.
				// Leave the dollar character untouched.
				buf = append(buf, s[j])
			} else {
				buf = append(buf, mapping(name)...)
			}
			j += w
			i = j + 1
		}
	}
	if buf == nil {
		return s
	}
	return string(buf) + s[i:]
}

// getShellName returns the name of the shell variable for s.
func getShellName(s string) (string, int) {
	switch {
	case s[0] == '{':
		if len(s) > 2 && isShellSpecialVar(s[1]) && s[2] == '}' {
			return s[1:2], 3
		}
		// Scan to closing brace
		for i := 1; i < len(s); i++ {
			if s[i] == '}' {
				if i == 1 {
					return "", 2 // Bad syntax; eat "${}"
				}
				return s[1:i], i + 1
			}
		}
		return "", 1 // Bad syntax; eat "${"
	case isShellSpecialVar(s[0]):
		return s[0:1], 1
	}
	// Scan alphanumerics.
	var i int
	for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
	}
	return s[:i], i
}

func isShellSpecialVar(c uint8) bool {
	switch c {
	case '*', '#', '$', '@', '!', '?', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func isAlphaNum(c uint8) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// ExpandEnv replaces ${var} or $var in the string according to the values
// of the current environment variables.
func ExpandEnv(s string) string {
	return Expand(s, Getenv)
}

// ---------------------------------------------------------------------------
// Identity / process.
// ---------------------------------------------------------------------------

// Getpid returns the process id of the caller.
func Getpid() int { return int(osGetpid()) }

// Getppid returns the process id of the caller's parent.
func Getppid() int { return int(osGetppid()) }

// Getuid returns the numeric user id of the caller.
func Getuid() int { return int(osGetuid()) }

// Geteuid returns the numeric effective user id of the caller.
func Geteuid() int { return int(osGeteuid()) }

// Getgid returns the numeric group id of the caller.
func Getgid() int { return int(osGetgid()) }

// Getegid returns the numeric effective group id of the caller.
func Getegid() int { return int(osGetegid()) }

// Getgroups returns a list of the numeric ids of groups that the caller
// belongs to.
func Getgroups() ([]int, error) {
	s := osGetgroups()
	if s == "" {
		return nil, nil
	}
	var out []int
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == 0 {
			n, ok := __parseUint(s[start:i])
			if !ok {
				return nil, &PathError{Op: "getgroups", Path: "", Err: syscall.EINVAL}
			}
			out = append(out, int(n))
			start = i + 1
		}
	}
	return out, nil
}

// __parseUint parses a decimal uint64 (avoids depending on synthetic
// strconv's desugar surface from inside os).
func __parseUint(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + uint64(s[i]-'0')
	}
	return n, true
}

// Hostname returns the host name reported by the kernel.
func Hostname() (string, error) {
	panic("os.Hostname: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __hostname() hostnamePair {
	buf := make([]byte, 256)
	r := osHostname(buf)
	if r >= 0 {
		return hostnamePair{string(buf[:r-1]), nil}
	}
	// Go-side fallback mirroring gc's sys_linux.go:
	// /proc/sys/kernel/hostname.
	p := __readFile("/proc/sys/kernel/hostname")
	if p.err != nil {
		return hostnamePair{"", newPathError("hostname", "", r)}
	}
	s := string(p.b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == 0) {
		s = s[:len(s)-1]
	}
	if s == "" {
		return hostnamePair{"", newPathError("hostname", "", r)}
	}
	return hostnamePair{s, nil}
}

// Getpagesize returns the underlying system's memory page size.
func Getpagesize() int { return 4096 }

// Exit causes the current program to exit with the given status code.
// Conventionally, code zero indicates success, non-zero an error.
// The program terminates immediately; deferred functions are not run.
func Exit(code int) {
	osExit(int64(code))
	// Unreachable: the intrinsic exits the process. (If it ever
	// returned, fall off the end — there is nothing sane to do.)
}

// TempDir returns the default directory to use for temporary files.
func TempDir() string {
	dir := Getenv("TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return dir
}

// UserHomeDir returns the current user's home directory.
func UserHomeDir() (string, error) {
	panic("os.UserHomeDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __userHomeDir() stringErrPair {
	if p := __lookupEnv("HOME"); p.ok && p.s != "" {
		return stringErrPair{p.s, nil}
	}
	return stringErrPair{"", errors.New("$HOME is not defined")}
}

// UserConfigDir returns the default root directory to use for
// user-specific configuration data.
func UserConfigDir() (string, error) {
	panic("os.UserConfigDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __userConfigDir() stringErrPair {
	var dir string
	if p := __lookupEnv("XDG_CONFIG_HOME"); p.ok && p.s != "" {
		dir = p.s
		if len(dir) == 0 || dir[0] != '/' {
			return stringErrPair{"", errors.New("path in $XDG_CONFIG_HOME is relative")}
		}
		return stringErrPair{dir, nil}
	}
	if p := __lookupEnv("HOME"); p.ok && p.s != "" {
		return stringErrPair{p.s + "/.config", nil}
	}
	return stringErrPair{"", errors.New("neither $XDG_CONFIG_HOME nor $HOME are defined")}
}

// UserCacheDir returns the default root directory to use for
// user-specific cached data.
func UserCacheDir() (string, error) {
	panic("os.UserCacheDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __userCacheDir() stringErrPair {
	var dir string
	if p := __lookupEnv("XDG_CACHE_HOME"); p.ok && p.s != "" {
		dir = p.s
		if len(dir) == 0 || dir[0] != '/' {
			return stringErrPair{"", errors.New("path in $XDG_CACHE_HOME is relative")}
		}
		return stringErrPair{dir, nil}
	}
	if p := __lookupEnv("HOME"); p.ok && p.s != "" {
		return stringErrPair{p.s + "/.cache", nil}
	}
	return stringErrPair{"", errors.New("neither $XDG_CACHE_HOME nor $HOME are defined")}
}

// ---------------------------------------------------------------------------
// CreateTemp / MkdirTemp (gc os/tempfile.go, ported).
// ---------------------------------------------------------------------------

var errPatternHasSeparator = errors.New("pattern contains path separator")

type prefixSuffixPair struct {
	prefix string
	suffix string
	err    error
}

// __prefixAndSuffix splits pattern by the last wildcard "*".
func __prefixAndSuffix(pattern string) prefixSuffixPair {
	for i := 0; i < len(pattern); i++ {
		if IsPathSeparator(pattern[i]) {
			return prefixSuffixPair{"", "", errPatternHasSeparator}
		}
	}
	pos := -1
	for i := len(pattern) - 1; i >= 0; i-- {
		if pattern[i] == '*' {
			pos = i
			break
		}
	}
	if pos != -1 {
		return prefixSuffixPair{pattern[:pos], pattern[pos+1:], nil}
	}
	return prefixSuffixPair{pattern, "", nil}
}

func nextRandom() string {
	return __formatUint(uint64(fastrand()))
}

// __formatUint formats a uint64 decimally (avoids depending on synthetic
// strconv's desugar surface from inside os).
func __formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// CreateTemp creates a new temporary file in the directory dir.
func CreateTemp(dir, pattern string) (*File, error) {
	panic("os.CreateTemp: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __createTemp(dir, pattern string) openPair {
	if dir == "" {
		dir = TempDir()
	}
	ps := __prefixAndSuffix(pattern)
	if ps.err != nil {
		return openPair{nil, &PathError{Op: "createtemp", Path: pattern, Err: ps.err}}
	}
	prefix := joinPath(dir, ps.prefix)

	try := 0
	for {
		name := prefix + nextRandom() + ps.suffix
		p := __openFile(name, O_RDWR|O_CREATE|O_EXCL, 0600)
		if IsExist(p.err) {
			try++
			if try < 10000 {
				continue
			}
			return openPair{nil, &PathError{Op: "createtemp", Path: prefix + "*" + ps.suffix, Err: ErrExist}}
		}
		return p
	}
}

// MkdirTemp creates a new temporary directory in the directory dir.
func MkdirTemp(dir, pattern string) (string, error) {
	panic("os.MkdirTemp: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __mkdirTemp(dir, pattern string) mkdirTempPair {
	if dir == "" {
		dir = TempDir()
	}
	ps := __prefixAndSuffix(pattern)
	if ps.err != nil {
		return mkdirTempPair{"", &PathError{Op: "mkdirtemp", Path: pattern, Err: ps.err}}
	}
	prefix := joinPath(dir, ps.prefix)

	try := 0
	for {
		name := prefix + nextRandom() + ps.suffix
		err := Mkdir(name, 0700)
		if err == nil {
			return mkdirTempPair{name, nil}
		}
		if IsExist(err) {
			try++
			if try < 10000 {
				continue
			}
			return mkdirTempPair{"", &PathError{Op: "mkdirtemp", Path: prefix + "*" + ps.suffix, Err: ErrExist}}
		}
		if IsNotExist(err) {
			sp := __stat(dir)
			if IsNotExist(sp.err) {
				return mkdirTempPair{"", sp.err}
			}
		}
		return mkdirTempPair{"", err}
	}
}

// ---------------------------------------------------------------------------
// ReadFrom / WriteTo (D4: see the header).
// ---------------------------------------------------------------------------

// ReadFrom implements io.ReaderFrom.
func (f *File) ReadFrom(r io.Reader) (n int64, err error) {
	panic("os.File.ReadFrom: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileReadFrom(f *File, r io.Reader) readFromPair {
	if err := checkValidFile(f); err != nil {
		return readFromPair{0, err}
	}
	if f.closed {
		return readFromPair{0, &PathError{Op: "read", Path: f.name, Err: ErrClosed}}
	}
	// D4: io.ReadAll + a concrete write. (A streaming loop needs io
	// interface-method desugars; re-test when the io worker's land.)
	// Partial input on read error is still written, like the generic loop.
	b, err := io.ReadAll(r)
	wp := __writeAll(f, b)
	if err != nil {
		return readFromPair{int64(wp.n), err}
	}
	return readFromPair{int64(wp.n), wp.err}
}

// WriteTo implements io.WriterTo.
func (f *File) WriteTo(w io.Writer) (n int64, err error) {
	panic("os.File.WriteTo: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fileWriteTo(f *File, w io.Writer) writeToPair {
	if err := checkValidFile(f); err != nil {
		return writeToPair{0, err}
	}
	if f.closed {
		return writeToPair{0, &PathError{Op: "write", Path: f.name, Err: ErrClosed}}
	}
	// D4: concrete read-all + io.WriteString (see ReadFrom).
	bp := __fileReadAll(f)
	n, err := io.WriteString(w, string(bp.b))
	if bp.err != nil {
		return writeToPair{int64(n), bp.err}
	}
	return writeToPair{int64(n), err}
}

// __fileReadAll reads the whole file through the concrete helper.
func __fileReadAll(f *File) readFilePair {
	var b []byte
	buf := make([]byte, 32768)
	for {
		rp := __fileRead(f, buf)
		if rp.n > 0 {
			b = append(b, buf[:rp.n]...)
		}
		if rp.err != nil {
			if rp.err == io.EOF {
				return readFilePair{b, nil}
			}
			return readFilePair{b, rp.err}
		}
	}
}

// ---------------------------------------------------------------------------
// Fail-fast: SyscallConn, Root, Executable, CopyFS/DirFS (§6.4, §6.6–§6.8).
// ---------------------------------------------------------------------------

// SyscallConn returns a raw file. This function is not implemented in M8
// (§6.4 — needs syscall.RawConn + a real fd).
func (f *File) SyscallConn() (syscall.RawConn, error) {
	panic("os.File.SyscallConn: not in M8 scope (needs syscall.RawConn + a real fd; re-test: fd-domain unification, Phase 4/Panama)")
}

// Root represents a directory that acts as the root of a file tree.
// M8: fail-fast (§6.6 — confining opens need openat2(2); a Go-level
// lexical emulation has TOCTOU holes).
type Root struct{}

// OpenRoot opens the named directory as a Root.
func OpenRoot(name string) (*Root, error) {
	panic("os.OpenRoot: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}

func (r *Root) Name() string { return "" }
func (r *Root) Close() error {
	panic("os.Root.Close: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Open(name string) (*File, error) {
	panic("os.Root.Open: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Create(name string) (*File, error) {
	panic("os.Root.Create: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) OpenFile(name string, flag int, perm FileMode) (*File, error) {
	panic("os.Root.OpenFile: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Stat(name string) (FileInfo, error) {
	panic("os.Root.Stat: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Lstat(name string) (FileInfo, error) {
	panic("os.Root.Lstat: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Mkdir(name string, perm FileMode) error {
	panic("os.Root.Mkdir: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Remove(name string) error {
	panic("os.Root.Remove: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) Rename(oldname, newname string) error {
	panic("os.Root.Rename: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) ReadFile(name string) ([]byte, error) {
	panic("os.Root.ReadFile: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) WriteFile(name string, data []byte, perm FileMode) error {
	panic("os.Root.WriteFile: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}
func (r *Root) ReadDir(name string) ([]DirEntry, error) {
	panic("os.Root.ReadDir: not in M8 scope (needs openat2(2); re-test: Panama openat2 or a vetted root-emulation design)")
}

// Executable returns the path name for the executable that started the
// current process. M8: fail-fast (§6.7 — /proc/self/exe is the JVM
// binary, which is the wrong answer; inventing one is worse).
func Executable() (string, error) {
	panic("os.Executable: not in M8 scope (/proc/self/exe is the JVM binary — wrong answer; re-test: launcher defines the executable concept)")
}

// DirFS returns a file system rooted at dir. M8: fail-fast (§6.8 —
// needs the io/fs FS machinery).
func DirFS(dir string) fs.FS {
	panic("os.DirFS: not in M8 scope (needs io/fs FS machinery; re-test: synthetic io/fs FS surface)")
}

// CopyFS copies the file system rooted at fsys to dir. M8: fail-fast (§6.8).
func CopyFS(dir string, fsys fs.FS) error {
	panic("os.CopyFS: not in M8 scope (needs io/fs FS machinery; re-test: synthetic io/fs FS surface)")
}

// ---------------------------------------------------------------------------
// Fail-fast: the whole Process family (§6.1).
// ---------------------------------------------------------------------------

// Signal represents an operating system signal.
type Signal interface {
	String() string
	Signal() // to distinguish from other Stringers
}

type interruptSignal struct{}

func (interruptSignal) String() string { return "interrupt" }
func (interruptSignal) Signal()        {}

type killSignal struct{}

func (killSignal) String() string { return "killed" }
func (killSignal) Signal()        {}

// Interrupt and Kill are the SIGINT/SIGKILL analogs. (M8: local signal
// values — only delivery is JVM-owned, per the R8 inventory amendment.)
var (
	Interrupt Signal = interruptSignal{}
	Kill      Signal = killSignal{}
)

// ProcAttr holds the attributes for StartProcess. M8: Sys is any —
// syscall.SysProcAttr is not in the M8 syscall vocabulary (re-test when
// it lands); nothing in M8 can spawn processes anyway.
type ProcAttr struct {
	Dir   string
	Env   []string
	Files []*File
	Sys   any
}

// Process is a process created by StartProcess. M8: the whole family
// fail-fasts (§6.1 — needs fd extraction for stdio redirect + wait4).
type Process struct {
	Pid int
}

// StartProcess starts a new process. M8: fail-fast (§6.1).
func StartProcess(name string, argv []string, attr *ProcAttr) (*Process, error) {
	panic("os.StartProcess: not in M8 scope (needs fd extraction for stdio redirect + wait4 rusage; re-test: process-spawning milestone / os/exec bring-up)")
}

// FindProcess looks for a running process by pid. M8: fail-fast (§6.1).
func FindProcess(pid int) (*Process, error) {
	panic("os.FindProcess: not in M8 scope (re-test: process-spawning milestone)")
}

// Pid returns the process id. (Pure field access — no syscall involved.)
func (p *Process) Pid() int { return p.Pid }

// Signal sends a signal to the Process. M8: fail-fast (§6.1).
func (p *Process) Signal(sig Signal) error {
	panic("os.Process.Signal: not in M8 scope (re-test: process-spawning milestone)")
}

// Kill causes the Process to exit immediately. M8: fail-fast (§6.1).
func (p *Process) Kill() error {
	panic("os.Process.Kill: not in M8 scope (re-test: process-spawning milestone)")
}

// Wait waits until the Process exits. M8: fail-fast (§6.1).
func (p *Process) Wait() (*ProcessState, error) {
	panic("os.Process.Wait: not in M8 scope (re-test: process-spawning milestone)")
}

// Release releases any resources associated with the Process.
func (p *Process) Release() error {
	panic("os.Process.Release: not in M8 scope (re-test: process-spawning milestone)")
}

// ProcessState describes a finished process. M8: fail-fast (§6.1).
type ProcessState struct{}

// String returns a string representation of the process state.
func (p *ProcessState) String() string {
	panic("os.ProcessState.String: not in M8 scope (re-test: process-spawning milestone)")
}

// Exited reports whether the program has exited.
func (p *ProcessState) Exited() bool {
	panic("os.ProcessState.Exited: not in M8 scope (re-test: process-spawning milestone)")
}

// Success reports whether the program exited successfully.
func (p *ProcessState) Success() bool {
	panic("os.ProcessState.Success: not in M8 scope (re-test: process-spawning milestone)")
}

// Sys returns system-specific exit information. (M8: any —
// syscall.WaitStatus/Rusage are not in the M8 syscall vocabulary.)
func (p *ProcessState) Sys() any {
	panic("os.ProcessState.Sys: not in M8 scope (re-test: process-spawning milestone)")
}

// SysUsage returns system-specific resource usage information.
func (p *ProcessState) SysUsage() any {
	panic("os.ProcessState.SysUsage: not in M8 scope (re-test: process-spawning milestone)")
}

// Pid returns the process id.
func (p *ProcessState) Pid() int {
	panic("os.ProcessState.Pid: not in M8 scope (re-test: process-spawning milestone)")
}

// ExitCode returns the exit code of the exited process.
func (p *ProcessState) ExitCode() int {
	panic("os.ProcessState.ExitCode: not in M8 scope (re-test: process-spawning milestone)")
}

// UserTime returns the user CPU time of the exited process.
func (p *ProcessState) UserTime() time.Duration {
	panic("os.ProcessState.UserTime: not in M8 scope (re-test: process-spawning milestone)")
}

// SystemTime returns the system CPU time of the exited process.
func (p *ProcessState) SystemTime() time.Duration {
	panic("os.ProcessState.SystemTime: not in M8 scope (re-test: process-spawning milestone)")
}
