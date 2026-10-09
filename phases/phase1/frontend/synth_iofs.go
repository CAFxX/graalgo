// P3-M8 (wave 2): synthetic `io/fs` core.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (imports: errors, time — both synthetic). The
// resulting *types.Package is what overlayImporter.Import("io/fs")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures for the MINIMAL core the os
// worker needs (R5): PathError, FileInfo, DirEntry, FileMode + the bit
// constants (+ the String/IsDir/IsRegular/Perm/Type methods, pure Go),
// the Err* sentinels, the File/ReadDirFile interfaces, and the FS
// interface stub. Everything else in io/fs (Glob, ReadFile, WalkDir,
// ReadDirFS, StatFS, Sub, ...) is OUT of M8 scope: it rides on the
// synthetic os package once that lands (re-test trigger recorded in
// phases/phase3/hidden_files/m8-analysis/os.md §6.8).
//
// Only frontend/v1-supported constructs (no generics, no reflect, no
// unsafe). All functions are single-result, so no Appendix-M desugar
// entries are needed for this package.
package main

// syntheticIoFsSource is the P3-M8 synthetic `io/fs` core package:
// exact gc 1.27.1 signatures for PathError, FileInfo, DirEntry,
// FileMode (+ constants and methods), the Err* sentinels, File,
// ReadDirFile, and the FS interface stub.
const syntheticIoFsSource = `package fs

import (
	"errors"
	"io"
	"time"
)

// PathError records an error and the operation and file path that caused it.
type PathError struct {
	Op   string
	Path string
	Err  error
}

func (e *PathError) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }

func (e *PathError) Unwrap() error { return e.Err }

// FileInfo describes a file and is returned by Stat and Lstat.
type FileInfo interface {
	Name() string       // base name of the file
	Size() int64        // length in bytes for regular files; system-dependent for others
	Mode() FileMode     // file mode bits
	ModTime() time.Time // modification time
	IsDir() bool        // abbreviation for Mode().IsDir()
	Sys() any           // underlying data source (can return nil)
}

// A FileMode represents a file's mode and permission bits.
// The bits have the same definition on all systems, so that
// information about files can be moved from one system
// to another portably. Not all bits apply to all systems.
// The only required bit is ModeDir for directories.
type FileMode uint32

// The defined file mode bits are the most significant bits of the FileMode.
// The nine least-significant bits are the standard Unix rwxrwxrwx permissions.
// The values of these bits should be considered part of the public API and
// may be used in wire protocols or disk representations: they must not be
// changed, although new bits might be added.
const (
	// The single letters are the abbreviations
	// used by the String method's formatting.
	ModeDir        FileMode = 1 << (32 - 1 - iota) // d: is a directory
	ModeAppend                                     // a: append-only
	ModeExclusive                                  // l: exclusive use
	ModeTemporary                                  // T: temporary file; Plan 9 only
	ModeSymlink                                    // L: symbolic link
	ModeDevice                                     // D: device file
	ModeNamedPipe                                  // p: named pipe (FIFO)
	ModeSocket                                     // S: Unix domain socket
	ModeSetuid                                     // u: setuid
	ModeSetgid                                     // g: setgid
	ModeCharDevice                                 // c: Unix character device, when ModeDevice is set
	ModeSticky                                     // t: sticky
	ModeIrregular                                  // ?: non-regular file; nothing else is known about this file

	// Mask for the type bits. For regular files, none will be set.
	ModeType = ModeDir | ModeSymlink | ModeNamedPipe | ModeSocket | ModeDevice | ModeCharDevice | ModeIrregular

	ModePerm FileMode = 0777 // Unix permission bits
)

func (m FileMode) String() string {
	const str = "dalTLDpSugct?"
	var buf [32]byte // Mode is uint32.
	w := 0
	for i, c := range str {
		if m&(1<<uint(32-1-i)) != 0 {
			buf[w] = byte(c)
			w++
		}
	}
	if w == 0 {
		return "----------"
	}
	const rwx = "rwxrwxrwx"
	for i, c := range rwx {
		if m&(1<<uint(8-i)) != 0 {
			buf[w] = byte(c)
		} else {
			buf[w] = '-'
		}
		w++
	}
	return string(buf[:w])
}

// IsDir reports whether m describes a directory.
func (m FileMode) IsDir() bool { return m&ModeDir != 0 }

// IsRegular reports whether m describes a regular file.
func (m FileMode) IsRegular() bool { return m&ModeType == 0 }

// Perm returns the Unix permission bits in m (low 9 bits).
func (m FileMode) Perm() FileMode { return m & ModePerm }

// Type returns type bits in m (ModeType).
func (m FileMode) Type() FileMode { return m & ModeType }

// DirEntry is an entry read from a directory.
type DirEntry interface {
	// Name returns the name of the file or subdirectory described by the entry.
	Name() string
	// IsDir reports whether the entry describes a directory.
	IsDir() bool
	// Type returns the type bits for the entry.
	Type() FileMode
	// Info returns the FileInfo for the file or subdirectory described by the entry.
	Info() (FileInfo, error)
}

// ReadDirFile is the interface implemented by a file that can be used
// as a directory.
type ReadDirFile interface {
	File
	// ReadDir reads the contents of the directory and returns
	// a slice of up to n DirEntry values in directory order.
	ReadDir(n int) ([]DirEntry, error)
}

// File is the interface implemented by *os.File (declared here so the
// FS interface can name it without importing os — gc's exact shape).
type File interface {
	Stat() (FileInfo, error)
	Read([]byte) (int, error)
	Close() error
}

// An FS provides access to a hierarchical file system.
type FS interface {
	Open(name string) (File, error)
}

// Generic file system errors.
// Errors returned by file systems can be tested against these errors
// using errors.Is.
var (
	ErrInvalid    = errors.New("invalid argument")
	ErrPermission = errors.New("permission denied")
	ErrExist      = errors.New("file already exists")
	ErrNotExist   = errors.New("file does not exist")
)

// ErrClosed is returned when operations are attempted on a file that
// has already been closed, including when Close is called more than once.
var ErrClosed = errors.New("file already closed")

// ---------------------------------------------------------------------------
// P3-M9 (wave 3): io/fs file-system machinery — ReadFile, ReadDir, Stat,
// Glob, WalkDir, Sub (+ the FS extension interfaces, ValidPath, the
// DirEntry helpers). Ports of go1.27.1 io/fs/{fs,readfile,readdir,stat,
// glob,walk,sub}.go, adapted to frontend/v1 constraints:
//
//   - internal/bytealg, internal/oserror, unicode/utf8, and path are not
//     synthetic in M9: ValidPath carries a minimal UTF-8 validator
//     (validUTF8, same accept table as unicode/utf8), and the glob
//     machinery carries unexported ports of path.Match/Join/Split
//     (fsMatch, fsPathJoin, fsPathSplit).
//   - No defer in synthetic sources: file.Close is explicit on every path
//     (gc's deferred Close ignores the close error; ours does too).
//   - Every multi-result function keeps its exact gc signature with a
//     fail-fast body; the real implementation lives in a single-result
//     __-helper returning a pair struct, desugared via the Appendix-M
//     table (mrdesugar_m5.go). Interface-typed call sites (fsys.Open,
//     file.Stat, ...) dispatch through the itab to the concrete type's
//     pair helper (m8IfaceMultiResultMethods, matched by method name).
// ---------------------------------------------------------------------------

// errFsBadPattern is the fs-local bad-pattern error. gc reports
// path.ErrBadPattern here; the path package is not synthetic in M9, so
// the identical message is carried locally (error strings compare
// equal; errors.Is across the path boundary is a documented gap).
var errFsBadPattern = errors.New("syntax error in pattern")

// --- minimal UTF-8 machinery (port of unicode/utf8, accept-table only) ---

const (
	fsRuneSelf = 0x80
	fsLocb     = 0x80
	fsHicb     = 0xBF

	fsXx = 0xF1
	fsAs = 0xF0
	fsS1 = 0x02
	fsS2 = 0x13
	fsS3 = 0x03
	fsS4 = 0x23
	fsS5 = 0x34
	fsS6 = 0x04
	fsS7 = 0x44
)

type fsAcceptRange struct {
	lo uint8
	hi uint8
}

var fsFirst = [256]uint8{
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs, fsAs,
	fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx,
	fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx,
	fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx,
	fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx,
	fsXx, fsXx, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1,
	fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1, fsS1,
	fsS2, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS3, fsS4, fsS3, fsS3,
	fsS5, fsS6, fsS6, fsS6, fsS7, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx, fsXx,
}

var fsAcceptRanges = [16]fsAcceptRange{
	0: {fsLocb, fsHicb},
	1: {0xA0, fsHicb},
	2: {fsLocb, 0x9F},
	3: {0x90, fsHicb},
	4: {fsLocb, 0x8F},
}

// validUTF8 reports whether s is a valid UTF-8 encoding (port of
// unicode/utf8.ValidString, same accept table).
func validUTF8(s string) bool {
	for i := 0; i < len(s); {
		si := s[i]
		if si < fsRuneSelf {
			i++
			continue
		}
		x := fsFirst[si]
		if x == fsXx {
			return false
		}
		size := int(x & 7)
		if i+size > len(s) {
			return false
		}
		accept := fsAcceptRanges[x>>4]
		if c := s[i+1]; c < accept.lo || accept.hi < c {
			return false
		} else if size == 2 {
		} else if c := s[i+2]; c < fsLocb || fsHicb < c {
			return false
		} else if size == 3 {
		} else if c := s[i+3]; c < fsLocb || fsHicb < c {
			return false
		}
		i += size
	}
	return true
}

// ValidPath reports whether the given path name is valid for use in a call
// to Open. Path names must be UTF-8, unrooted, slash-separated, with no
// ".", "..", or empty elements (except "." for the root).
func ValidPath(path string) bool {
	if !validUTF8(path) {
		return false
	}
	if path == "." {
		return true
	}
	for {
		i := 0
		for i < len(path) && path[i] != '/' {
			i++
		}
		elem := path[:i]
		if elem == "" || elem == "." || elem == ".." {
			return false
		}
		if i == len(path) {
			return true
		}
		path = path[i+1:]
	}
}

// SkipDir is used as a return value from WalkDirFunc to indicate that the
// directory named in the call is to be skipped.
var SkipDir = errors.New("skip this directory")

// SkipAll is used as a return value from WalkDirFunc to indicate that all
// remaining files and directories are to be skipped.
var SkipAll = errors.New("skip everything and stop the walk")

// WalkDirFunc is the type of the function called by WalkDir to visit each
// file or directory.
type WalkDirFunc func(path string, d DirEntry, err error) error

// FS extension interfaces (optimized-implementation hooks, gc-exact).
type ReadDirFS interface {
	FS
	ReadDir(name string) ([]DirEntry, error)
}

type StatFS interface {
	FS
	Stat(name string) (FileInfo, error)
}

type ReadFileFS interface {
	FS
	ReadFile(name string) ([]byte, error)
}

type GlobFS interface {
	FS
	Glob(pattern string) ([]string, error)
}

type SubFS interface {
	FS
	Sub(dir string) (FS, error)
}

// Pair structs for the Appendix-M desugar (field order matches the
// multi-result signature order positionally).
type fsReadFilePair struct {
	b   []byte
	err error
}

type fsReadDirPair struct {
	de  []DirEntry
	err error
}

type fsStatPair struct {
	fi  FileInfo
	err error
}

type fsGlobPair struct {
	matches []string
	err     error
}

type fsSubPair struct {
	fsys FS
	err  error
}

type fsOpenPair struct {
	f   File
	err error
}

// dirInfo is a DirEntry based on a FileInfo (gc readdir.go).
type dirInfo struct {
	fileInfo FileInfo
}

func (di dirInfo) Name() string       { return di.fileInfo.Name() }
func (di dirInfo) IsDir() bool        { return di.fileInfo.IsDir() }
func (di dirInfo) Type() FileMode     { return di.fileInfo.Mode().Type() }
func (di dirInfo) Info() (FileInfo, error) {
	panic("fs.dirInfo.Info: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __dirInfoInfo(di dirInfo) fsStatPair { return fsStatPair{di.fileInfo, nil} }

// FileInfoToDirEntry returns a DirEntry that returns information from info.
// If info is nil, FileInfoToDirEntry returns nil.
func FileInfoToDirEntry(info FileInfo) DirEntry {
	if info == nil {
		return nil
	}
	return dirInfo{fileInfo: info}
}

// ---------------------------------------------------------------------------
// ReadFile.
// ---------------------------------------------------------------------------

// ReadFile reads the named file from the file system fsys and returns its
// contents. If fsys implements ReadFileFS, ReadFile calls fsys.ReadFile;
// otherwise it Opens the file and uses Read and Close.
func ReadFile(fsys FS, name string) ([]byte, error) {
	panic("fs.ReadFile: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fsReadFile(fsys FS, name string) fsReadFilePair {
	if rfFS, ok := fsys.(ReadFileFS); ok {
		b, err := rfFS.ReadFile(name)
		return fsReadFilePair{b, err}
	}
	file, err := fsys.Open(name)
	if err != nil {
		return fsReadFilePair{nil, err}
	}
	var size int
	if info, err := file.Stat(); err == nil {
		size64 := info.Size()
		if int64(int(size64)) == size64 {
			size = int(size64)
		}
	}
	data := make([]byte, 0, size+1)
	for {
		if len(data) >= cap(data) {
			d := append(data[:cap(data)], 0)
			data = d[:len(data)]
		}
		n, err := file.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			file.Close()
			return fsReadFilePair{data, err}
		}
	}
}

// ---------------------------------------------------------------------------
// ReadDir.
// ---------------------------------------------------------------------------

// ReadDir reads the named directory and returns a list of directory
// entries sorted by filename. If fsys implements ReadDirFS, ReadDir calls
// fsys.ReadDir; otherwise it Opens the directory and uses ReadDirFile.
func ReadDir(fsys FS, name string) ([]DirEntry, error) {
	panic("fs.ReadDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fsReadDir(fsys FS, name string) fsReadDirPair {
	if rdFS, ok := fsys.(ReadDirFS); ok {
		de, err := rdFS.ReadDir(name)
		return fsReadDirPair{de, err}
	}
	file, err := fsys.Open(name)
	if err != nil {
		return fsReadDirPair{nil, err}
	}
	dir, ok := file.(ReadDirFile)
	if !ok {
		file.Close()
		return fsReadDirPair{nil, &PathError{Op: "readdir", Path: name, Err: errors.New("not implemented")}}
	}
	list, err := dir.ReadDir(-1)
	__sortFsDirEntries(list)
	file.Close()
	return fsReadDirPair{list, err}
}

// __sortFsDirEntries sorts entries by filename (insertion sort; the sort
// package's exact API surface is not needed here).
func __sortFsDirEntries(de []DirEntry) {
	for i := 1; i < len(de); i++ {
		for j := i; j > 0 && de[j].Name() < de[j-1].Name(); j-- {
			de[j], de[j-1] = de[j-1], de[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Stat.
// ---------------------------------------------------------------------------

// Stat returns a FileInfo describing the named file from the file system.
// If fsys implements StatFS, Stat calls fsys.Stat; otherwise it opens the
// file and stats it.
func Stat(fsys FS, name string) (FileInfo, error) {
	panic("fs.Stat: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fsStat(fsys FS, name string) fsStatPair {
	if stFS, ok := fsys.(StatFS); ok {
		fi, err := stFS.Stat(name)
		return fsStatPair{fi, err}
	}
	file, err := fsys.Open(name)
	if err != nil {
		return fsStatPair{nil, err}
	}
	fi, err := file.Stat()
	file.Close()
	return fsStatPair{fi, err}
}

// ---------------------------------------------------------------------------
// path.Match / path.Split / path.Join ports (the path package is not
// synthetic in M9; these are unexported ports of go1.27.1 path/match.go,
// used by the glob machinery below).
// ---------------------------------------------------------------------------

// fsMatchResult carries fsMatch's two results (v1 single-result rule).
type fsMatchResult struct {
	matched bool
	err     error
}

type fsScanChunkResult struct {
	star  bool
	chunk string
	rest  string
}

type fsMatchChunkResult struct {
	rest string
	ok   bool
	err  error
}

type fsGetEscResult struct {
	r      rune
	nchunk string
	err    error
}

type fsDecodeRuneResult struct {
	r rune
	n int
}

func fsMatch(pattern, name string) fsMatchResult {
Pattern:
	for len(pattern) > 0 {
		var star bool
		var chunk string
		sc := fsScanChunk(pattern)
		star, chunk, pattern = sc.star, sc.chunk, sc.rest
		if star && chunk == "" {
			return fsMatchResult{fsIndexByte(name, '/') < 0, nil}
		}
		mc := fsMatchChunk(chunk, name)
		t, ok, err := mc.rest, mc.ok, mc.err
		if ok && (len(t) == 0 || len(pattern) > 0) {
			name = t
			continue
		}
		if err != nil {
			return fsMatchResult{false, err}
		}
		if star {
			for i := 0; i < len(name) && name[i] != '/'; i++ {
				mc := fsMatchChunk(chunk, name[i+1:])
				t, ok, err := mc.rest, mc.ok, mc.err
				if ok {
					if len(pattern) == 0 && len(t) > 0 {
						continue
					}
					name = t
					continue Pattern
				}
				if err != nil {
					return fsMatchResult{false, err}
				}
			}
		}
		for len(pattern) > 0 {
			sc := fsScanChunk(pattern)
			_, chunk, pattern = sc.star, sc.chunk, sc.rest
			if mc := fsMatchChunk(chunk, ""); mc.err != nil {
				return fsMatchResult{false, mc.err}
			}
		}
		return fsMatchResult{false, nil}
	}
	return fsMatchResult{len(name) == 0, nil}
}

func fsScanChunk(pattern string) fsScanChunkResult {
	var star bool
	for len(pattern) > 0 && pattern[0] == '*' {
		pattern = pattern[1:]
		star = true
	}
	inrange := false
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
			}
		case '[':
			inrange = true
		case ']':
			inrange = false
		case '*':
			if !inrange {
				return fsScanChunkResult{star, pattern[:i], pattern[i:]}
			}
		}
	}
	return fsScanChunkResult{star, pattern, ""}
}

func fsMatchChunk(chunk, s string) fsMatchChunkResult {
	failed := false
	for len(chunk) > 0 {
		failed = failed || len(s) == 0
		switch chunk[0] {
		case '[':
			var r rune
			if !failed {
				dr := fsDecodeRune(s)
				r, s = dr.r, s[dr.n:]
			}
			chunk = chunk[1:]
			negated := false
			if len(chunk) > 0 && chunk[0] == '^' {
				negated = true
				chunk = chunk[1:]
			}
			match := false
			nrange := 0
			for {
				if len(chunk) > 0 && chunk[0] == ']' && nrange > 0 {
					chunk = chunk[1:]
					break
				}
				ge := fsGetEsc(chunk)
				if ge.err != nil {
					return fsMatchChunkResult{rest: "", ok: false, err: ge.err}
				}
				lo, chunk := ge.r, ge.nchunk
				hi := lo
				if chunk[0] == '-' {
					ge := fsGetEsc(chunk[1:])
					if ge.err != nil {
						return fsMatchChunkResult{rest: "", ok: false, err: ge.err}
					}
					hi, chunk = ge.r, ge.nchunk
				}
				match = match || lo <= r && r <= hi
				nrange++
			}
			failed = failed || match == negated
		case '?':
			if !failed {
				failed = s[0] == '/'
				dr := fsDecodeRune(s)
				s = s[dr.n:]
			}
			chunk = chunk[1:]
		case '\\':
			chunk = chunk[1:]
			if len(chunk) == 0 {
				return fsMatchChunkResult{rest: "", ok: false, err: errFsBadPattern}
			}
			fallthrough
		default:
			if !failed {
				failed = chunk[0] != s[0]
				s = s[1:]
			}
			chunk = chunk[1:]
		}
	}
	if failed {
		return fsMatchChunkResult{rest: "", ok: false, err: nil}
	}
	return fsMatchChunkResult{rest: s, ok: true, err: nil}
}

func fsGetEsc(chunk string) fsGetEscResult {
	if len(chunk) == 0 || chunk[0] == '-' || chunk[0] == ']' {
		return fsGetEscResult{0, "", errFsBadPattern}
	}
	if chunk[0] == '\\' {
		chunk = chunk[1:]
		if len(chunk) == 0 {
			return fsGetEscResult{0, "", errFsBadPattern}
		}
	}
	dr := fsDecodeRune(chunk)
	r, n := dr.r, dr.n
	if r == 0xFFFD && n == 1 {
		return fsGetEscResult{0, "", errFsBadPattern}
	}
	nchunk := chunk[n:]
	if len(nchunk) == 0 {
		return fsGetEscResult{0, "", errFsBadPattern}
	}
	return fsGetEscResult{r: r, nchunk: nchunk, err: nil}
}

// fsDecodeRune decodes the first rune of s (port of the rune-decoding core
// of unicode/utf8.DecodeRuneInString, same accept table as validUTF8).
func fsDecodeRune(s string) fsDecodeRuneResult {
	if len(s) == 0 {
		return fsDecodeRuneResult{r: 0xFFFD, n: 0}
	}
	c := s[0]
	if c < fsRuneSelf {
		return fsDecodeRuneResult{r: rune(c), n: 1}
	}
	x := fsFirst[c]
	if x == fsXx {
		return fsDecodeRuneResult{r: 0xFFFD, n: 1}
	}
	size := int(x & 7)
	if len(s) < size {
		return fsDecodeRuneResult{r: 0xFFFD, n: 1}
	}
	accept := fsAcceptRanges[x>>4]
	if c1 := s[1]; c1 < accept.lo || accept.hi < c1 {
		return fsDecodeRuneResult{r: 0xFFFD, n: 1}
	} else if size == 2 {
		r := rune(c&0x1F)<<6 | rune(s[1]&0x3F)
		return fsDecodeRuneResult{r: r, n: 2}
	} else if c2 := s[2]; c2 < fsLocb || fsHicb < c2 {
		return fsDecodeRuneResult{r: 0xFFFD, n: 1}
	} else if size == 3 {
		r := rune(c&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F)
		return fsDecodeRuneResult{r: r, n: 3}
	} else if c3 := s[3]; c3 < fsLocb || fsHicb < c3 {
		return fsDecodeRuneResult{r: 0xFFFD, n: 1}
	}
	r := rune(c&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F)
	return fsDecodeRuneResult{r: r, n: 4}
}

func fsIndexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// fsPathSplit splits path immediately following the final slash.
type fsPathSplitResult struct {
	dir  string
	file string
}

func fsPathSplit(path string) fsPathSplitResult {
	i := len(path) - 1
	for i >= 0 && path[i] != '/' {
		i--
	}
	return fsPathSplitResult{path[:i+1], path[i+1:]}
}

// fsPathJoin joins two path elements with a slash, cleaning the result.
// (Two-argument form: frontend/v1 cannot pack variadic call arguments,
// so the variadic path.Join shape is not used for internal calls.)
func fsPathJoin(a, b string) string {
	if a == "" {
		if b == "" {
			return ""
		}
		return fsPathClean(b)
	}
	if b == "" {
		return fsPathClean(a + "/")
	}
	return fsPathClean(a + "/" + b)
}

// fsPathClean is the slash-only Clean used by the glob join (no volume
// names; fs paths are slash-separated by definition).
func fsPathClean(path string) string {
	if path == "" {
		return "."
	}
	rooted := path[0] == '/'
	n := len(path)
	var out []byte
	r, dotdot := 0, 0
	if rooted {
		out = append(out, '/')
		r, dotdot = 1, 1
	}
	for r < n {
		switch {
		case path[r] == '/':
			r++
		case path[r] == '.' && (r+1 == n || path[r+1] == '/'):
			r++
		case path[r] == '.' && r+1 < n && path[r+1] == '.' && (r+2 == n || path[r+2] == '/'):
			r += 2
			if len(out) > dotdot {
				lenOut := len(out) - 1
				for lenOut > dotdot && out[lenOut] != '/' {
					lenOut--
				}
				out = out[:lenOut]
			} else if !rooted {
				if len(out) > 0 {
					out = append(out, '/')
				}
				out = append(out, '.', '.')
				dotdot = len(out)
			}
		default:
			if rooted && len(out) != 1 || !rooted && len(out) != 0 {
				out = append(out, '/')
			}
			for ; r < n && path[r] != '/'; r++ {
				out = append(out, path[r])
			}
		}
	}
	if len(out) == 0 {
		return "."
	}
	return string(out)
}

// fsHasMeta reports whether path contains any of the magic characters
// recognized by fsMatch.
func fsHasMeta(path string) bool {
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '*', '?', '[', '\\':
			return true
		}
	}
	return false
}

// fsCleanGlobPath prepares a glob dir for matching (chop trailing slash).
func fsCleanGlobPath(path string) string {
	if path == "" {
		return "."
	}
	return path[:len(path)-1]
}

// ---------------------------------------------------------------------------
// Glob.
// ---------------------------------------------------------------------------

// Glob returns the names of all files matching pattern or nil if there is
// no matching file. The syntax of patterns is the same as in path.Match.
// Glob ignores file system errors such as I/O errors reading directories;
// the only possible returned error reports a malformed pattern.
func Glob(fsys FS, pattern string) ([]string, error) {
	panic("fs.Glob: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fsGlob(fsys FS, pattern string) fsGlobPair {
	return __fsGlobWithLimit(fsys, pattern, 0)
}

func __fsGlobWithLimit(fsys FS, pattern string, depth int) fsGlobPair {
	const pathSeparatorsLimit = 10000
	if depth > pathSeparatorsLimit {
		return fsGlobPair{nil, errFsBadPattern}
	}
	if gfFS, ok := fsys.(GlobFS); ok {
		m, err := gfFS.Glob(pattern)
		return fsGlobPair{m, err}
	}
	if mr := fsMatch(pattern, ""); mr.err != nil {
		return fsGlobPair{nil, mr.err}
	}
	if !fsHasMeta(pattern) {
		sp := __fsStat(fsys, pattern)
		if sp.err != nil {
			return fsGlobPair{nil, nil}
		}
		return fsGlobPair{[]string{pattern}, nil}
	}
	sps := fsPathSplit(pattern)
	dir, file := sps.dir, sps.file
	dir = fsCleanGlobPath(dir)
	if !fsHasMeta(dir) {
		return __fsGlobDir(fsys, dir, file, nil)
	}
	if dir == pattern {
		return fsGlobPair{nil, errFsBadPattern}
	}
	dp := __fsGlobWithLimit(fsys, dir, depth+1)
	if dp.err != nil {
		return fsGlobPair{nil, dp.err}
	}
	matches := dp.matches
	for _, d := range dp.matches {
		gp := __fsGlobDir(fsys, d, file, matches)
		if gp.err != nil {
			return fsGlobPair{gp.matches, gp.err}
		}
		matches = gp.matches
	}
	return fsGlobPair{matches, nil}
}

func __fsGlobDir(fsys FS, dir, pattern string, matches []string) fsGlobPair {
	rdp := __fsReadDir(fsys, dir)
	if rdp.err != nil {
		return fsGlobPair{matches, nil}
	}
	m := matches
	for _, de := range rdp.de {
		n := de.Name()
		mr := fsMatch(pattern, n)
		if mr.err != nil {
			return fsGlobPair{m, mr.err}
		}
		matched := mr.matched
		if matched {
			m = append(m, fsPathJoin(dir, n))
		}
	}
	return fsGlobPair{m, nil}
}

// ---------------------------------------------------------------------------
// WalkDir.
// ---------------------------------------------------------------------------

// WalkDir walks the file tree rooted at root, calling fn for each file or
// directory in the tree, including root. All errors that arise visiting
// files and directories are filtered by fn.
func WalkDir(fsys FS, root string, fn WalkDirFunc) error {
	sp := __fsStat(fsys, root)
	var err error
	if sp.err != nil {
		err = fn(root, nil, sp.err)
	} else {
		err = __fsWalkDir(fsys, root, FileInfoToDirEntry(sp.fi), fn)
	}
	if err == SkipDir || err == SkipAll {
		return nil
	}
	return err
}

func __fsWalkDir(fsys FS, name string, d DirEntry, walkDirFn WalkDirFunc) error {
	if err := walkDirFn(name, d, nil); err != nil || !d.IsDir() {
		if err == SkipDir && d.IsDir() {
			err = nil
		}
		return err
	}
	rdp := __fsReadDir(fsys, name)
	if rdp.err != nil {
		err := walkDirFn(name, d, rdp.err)
		if err != nil {
			if err == SkipDir && d.IsDir() {
				err = nil
			}
			return err
		}
	}
	// Index-based loop: the frontend's range-variable handling across
	// recursive calls is suspect (walk stops after first subtree).
	for i := 0; i < len(rdp.de); i++ {
		d1 := rdp.de[i]
		name1 := fsPathJoin(name, d1.Name())
		if err := __fsWalkDir(fsys, name1, d1, walkDirFn); err != nil {
			if err == SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sub.
// ---------------------------------------------------------------------------

// Sub returns an FS corresponding to the subtree rooted at dir. If dir is
// ".", Sub returns fsys unchanged. If fsys implements SubFS, Sub calls
// fsys.Sub.
func Sub(fsys FS, dir string) (FS, error) {
	panic("fs.Sub: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __fsSub(fsys FS, dir string) fsSubPair {
	if !ValidPath(dir) {
		return fsSubPair{nil, &PathError{Op: "sub", Path: dir, Err: ErrInvalid}}
	}
	if dir == "." {
		return fsSubPair{fsys, nil}
	}
	if sbFS, ok := fsys.(SubFS); ok {
		s, err := sbFS.Sub(dir)
		return fsSubPair{s, err}
	}
	return fsSubPair{&subFS{fsys: fsys, dir: dir}, nil}
}

type subFS struct {
	fsys FS
	dir  string
}

type subFullNamePair struct {
	s   string
	err error
}

func __subFSFullName(f *subFS, op string, name string) subFullNamePair {
	if !ValidPath(name) {
		return subFullNamePair{"", &PathError{Op: op, Path: name, Err: ErrInvalid}}
	}
	return subFullNamePair{fsPathJoin(f.dir, name), nil}
}

type subShortenResult struct {
	rel string
	ok  bool
}

func __subFSShorten(f *subFS, name string) subShortenResult {
	if name == f.dir {
		return subShortenResult{".", true}
	}
	if len(name) >= len(f.dir)+2 && name[len(f.dir)] == '/' && name[:len(f.dir)] == f.dir {
		return subShortenResult{name[len(f.dir)+1:], true}
	}
	return subShortenResult{"", false}
}

func __subFSFixErr(f *subFS, err error) error {
	if e, ok := err.(*PathError); ok {
		if sr := __subFSShorten(f, e.Path); sr.ok {
			e.Path = sr.rel
		}
	}
	return err
}

func (f *subFS) Open(name string) (File, error) {
	panic("fs.subFS.Open: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __subFSOpen(f *subFS, name string) fsOpenPair {
	fnp := __subFSFullName(f, "open", name)
	if fnp.err != nil {
		return fsOpenPair{nil, fnp.err}
	}
	file, err := f.fsys.Open(fnp.s)
	return fsOpenPair{file, __subFSFixErr(f, err)}
}

func (f *subFS) ReadDir(name string) ([]DirEntry, error) {
	panic("fs.subFS.ReadDir: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __subFSReadDir(f *subFS, name string) fsReadDirPair {
	fnp := __subFSFullName(f, "read", name)
	if fnp.err != nil {
		return fsReadDirPair{nil, fnp.err}
	}
	rdp := __fsReadDir(f.fsys, fnp.s)
	return fsReadDirPair{rdp.de, __subFSFixErr(f, rdp.err)}
}

func (f *subFS) ReadFile(name string) ([]byte, error) {
	panic("fs.subFS.ReadFile: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __subFSReadFile(f *subFS, name string) fsReadFilePair {
	fnp := __subFSFullName(f, "read", name)
	if fnp.err != nil {
		return fsReadFilePair{nil, fnp.err}
	}
	rfp := __fsReadFile(f.fsys, fnp.s)
	return fsReadFilePair{rfp.b, __subFSFixErr(f, rfp.err)}
}

func (f *subFS) Glob(pattern string) ([]string, error) {
	panic("fs.subFS.Glob: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __subFSGlob(f *subFS, pattern string) fsGlobPair {
	if mr := fsMatch(pattern, ""); mr.err != nil {
		return fsGlobPair{nil, mr.err}
	}
	if pattern == "." {
		return fsGlobPair{[]string{"."}, nil}
	}
	full := f.dir + "/" + pattern
	gp := __fsGlob(f.fsys, full)
	list := gp.matches
	for i, name := range list {
		sr := __subFSShorten(f, name)
		if !sr.ok {
			return fsGlobPair{nil, errors.New("invalid result from inner fsys Glob: " + name + " not in " + f.dir)}
		}
		list[i] = sr.rel
	}
	return fsGlobPair{list, __subFSFixErr(f, gp.err)}
}

func (f *subFS) Sub(dir string) (FS, error) {
	panic("fs.subFS.Sub: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __subFSSub(f *subFS, dir string) fsSubPair {
	if dir == "." {
		return fsSubPair{f, nil}
	}
	fnp := __subFSFullName(f, "sub", dir)
	if fnp.err != nil {
		return fsSubPair{nil, fnp.err}
	}
	return fsSubPair{&subFS{fsys: f.fsys, dir: fnp.s}, nil}
}
`

