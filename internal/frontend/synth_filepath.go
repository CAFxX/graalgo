// SYNTHETIC (Principle 4) — path/filepath: real go1.27.1 source uses constructs the graalgo frontend cannot compile; ported with the documented adaptations below.
// RETIRES WHEN: frontend gains the underlying capability and the real GOROOT source compiles unchanged — then delete this file.
// RE-TEST TRIGGER: the capability named in this file's adaptation notes lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (wave 3, tier-2): synthetic `path/filepath` package (linux-only).
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (imports: errors, io/fs, os, strings, syscall — all
// synthetic). Same synthetic-source/exporter/desugar contract as the
// other P3-M8/M9 packages (see synth_os.go header).
//
// Port of go1.27.1 path/filepath/{path,path_unix,match,symlink}.go plus
// the unix branch of internal/filepathlite (Clean, Base, Dir, Ext,
// Split, VolumeName, IsAbs, ToSlash, FromSlash, IsLocal, Localize).
// Windows/Plan9 branches are out of scope (Linux-first per the Phase-3
// plan; GG-0023 defers the other platforms with re-test triggers).
// internal/bytealg, internal/filepathlite, internal/stringslite, and
// unicode/utf8 are not synthetic in M9: their used fragments are ported
// inline (fpCountSeparator, fpDecodeRune, the lazybuf Clean engine).
//
// Multi-result API (Abs, Rel, EvalSymlinks, Localize, Glob) keeps exact
// gc signatures with fail-fast bodies; the real implementations live in
// single-result __-helpers returning pair structs, desugared at call
// sites via the Appendix-M table (mrdesugar_m5.go, "path/filepath"
// entries). Walk/WalkDir are single-result and emitted directly; they
// call os.Lstat/os.ReadDir/os.Open (concrete, already desugared) and
// fs.FileInfoToDirEntry.
package main

// syntheticFilepathSource is the P3-M9 synthetic `path/filepath` package.
const syntheticFilepathSource = `package filepath

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// Separator and ListSeparator: gc derives these from os.PathSeparator /
// os.PathListSeparator.
const (
	Separator     = os.PathSeparator
	ListSeparator = os.PathListSeparator
)

// ErrBadPattern indicates a malformed glob pattern.
var ErrBadPattern = errors.New("syntax error in pattern")

// SkipDir and SkipAll alias the io/fs sentinels (gc: var SkipDir error =
// fs.SkipDir).
var SkipDir error = fs.SkipDir
var SkipAll error = fs.SkipAll

// WalkFunc is the type of the function called by Walk to visit each file
// or directory.
type WalkFunc func(path string, info fs.FileInfo, err error) error

// Pair structs for the Appendix-M desugar.
type filepathPair struct {
	s   string
	err error
}

type filepathGlobPair struct {
	matches []string
	err     error
}

type filepathReadDirNamesPair struct {
	names []string
	err   error
}

// ---------------------------------------------------------------------------
// filepathlite unix port (Clean engine + helpers).
// ---------------------------------------------------------------------------

type fpLazybuf struct {
	path       string
	buf        []byte
	w          int
	volAndPath string
	volLen     int
}

func (b *fpLazybuf) fpIndex(i int) byte {
	if b.buf != nil {
		return b.buf[i]
	}
	return b.path[i]
}

func (b *fpLazybuf) fpAppend(c byte) {
	if b.buf == nil {
		if b.w < len(b.path) && b.path[b.w] == c {
			b.w++
			return
		}
		b.buf = make([]byte, len(b.path))
		copy(b.buf, b.path[:b.w])
	}
	b.buf[b.w] = c
	b.w++
}

func (b *fpLazybuf) fpString() string {
	if b.buf == nil {
		return b.volAndPath[:b.volLen+b.w]
	}
	return b.volAndPath[:b.volLen] + string(b.buf[:b.w])
}

// Clean returns the canonical path (port of filepathlite.Clean, unix:
// volume length 0, postClean no-op, FromSlash identity).
func Clean(path string) string {
	originalPath := path
	if path == "" {
		return originalPath + "."
	}
	rooted := path[0] == Separator
	n := len(path)
	out := &fpLazybuf{path: path, volAndPath: originalPath, volLen: 0}
	r, dotdot := 0, 0
	if rooted {
		out.fpAppend(Separator)
		r, dotdot = 1, 1
	}
	for r < n {
		switch {
		case path[r] == Separator:
			r++
		case path[r] == '.' && (r+1 == n || path[r+1] == Separator):
			r++
		case path[r] == '.' && r+1 < n && path[r+1] == '.' && (r+2 == n || path[r+2] == Separator):
			r += 2
			switch {
			case out.w > dotdot:
				out.w--
				for out.w > dotdot && out.fpIndex(out.w) != Separator {
					out.w--
				}
			case !rooted:
				if out.w > 0 {
					out.fpAppend(Separator)
				}
				out.fpAppend('.')
				out.fpAppend('.')
				dotdot = out.w
			}
		default:
			if rooted && out.w != 1 || !rooted && out.w != 0 {
				out.fpAppend(Separator)
			}
			for ; r < n && path[r] != Separator; r++ {
				out.fpAppend(path[r])
			}
		}
	}
	if out.w == 0 {
		out.fpAppend('.')
	}
	return out.fpString()
}

// IsAbs reports whether the path is absolute (unix: leading slash).
func IsAbs(path string) bool {
	return strings.HasPrefix(path, "/")
}

// VolumeName returns the leading volume name ("" on unix).
func VolumeName(path string) string { return "" }

// Split splits path immediately following the final Separator.
func Split(path string) (dir, file string) {
	panic("filepath.Split: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

type filepathSplitPair struct {
	dir  string
	file string
}

func __filepathSplit(path string) filepathSplitPair {
	i := len(path) - 1
	for i >= 0 && path[i] != Separator {
		i--
	}
	return filepathSplitPair{path[:i+1], path[i+1:]}
}

// Join joins any number of path elements into a single path, separating
// them with Separator and cleaning the result.
func Join(elem ...string) string {
	return join(elem)
}

func join(elem []string) string {
	for i, e := range elem {
		if e != "" {
			joined := e
			for _, e2 := range elem[i+1:] {
				joined += string(Separator) + e2
			}
			return Clean(joined)
		}
	}
	return ""
}

// Ext returns the file name extension used by path.
func Ext(path string) string {
	for i := len(path) - 1; i >= 0 && path[i] != Separator; i-- {
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}

// Base returns the last element of path.
func Base(path string) string {
	if path == "" {
		return "."
	}
	for len(path) > 0 && path[len(path)-1] == Separator {
		path = path[0 : len(path)-1]
	}
	i := len(path) - 1
	for i >= 0 && path[i] != Separator {
		i--
	}
	if i >= 0 {
		path = path[i+1:]
	}
	if path == "" {
		return string(Separator)
	}
	return path
}

// Dir returns all but the last element of path.
func Dir(path string) string {
	i := len(path) - 1
	for i >= 0 && path[i] != Separator {
		i--
	}
	dir := Clean(path[:i+1])
	return dir
}

// ToSlash returns path with all Separator replaced by '/' (identity on
// unix).
func ToSlash(path string) string { return path }

// FromSlash returns path with all '/' replaced by Separator (identity on
// unix).
func FromSlash(path string) string { return path }

// HasPrefix reports whether p begins with prefix (unix: strings.HasPrefix;
// the windows volume-aware comparison arrives with GG-0023).
func HasPrefix(p, prefix string) bool {
	return strings.HasPrefix(p, prefix)
}

// SplitList splits a list of paths joined by ListSeparator.
func SplitList(path string) []string {
	return splitList(path)
}

func splitList(path string) []string {
	if path == "" {
		return []string{}
	}
	return strings.Split(path, string(ListSeparator))
}

// IsLocal reports whether path is local to the current directory
// (port of filepathlite unixIsLocal).
func IsLocal(path string) bool {
	if IsAbs(path) || path == "" {
		return false
	}
	hasDots := false
	for p := path; p != ""; {
		cr := fpCut(p)
		part := cr.before
		p = cr.after
		if part == "." || part == ".." {
			hasDots = true
			break
		}
	}
	if hasDots {
		path = Clean(path)
	}
	if path == ".." || strings.HasPrefix(path, "../") {
		return false
	}
	return true
}

// fpCut cuts p at the first '/' (minimal stringslite.Cut port).
type fpCutResult struct {
	before string
	after  string
}

func fpCut(p string) fpCutResult {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return fpCutResult{p[:i], p[i+1:]}
	}
	return fpCutResult{p, ""}
}

// Localize returns path if it is a valid, local fs path.
func Localize(path string) (string, error) {
	panic("filepath.Localize: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __filepathLocalize(path string) filepathPair {
	if !fs.ValidPath(path) {
		return filepathPair{"", errors.New("invalid path")}
	}
	for i := 0; i < len(path); i++ {
		if path[i] == 0 {
			return filepathPair{"", errors.New("invalid path")}
		}
	}
	return filepathPair{path, nil}
}

// ---------------------------------------------------------------------------
// Abs / Rel.
// ---------------------------------------------------------------------------

// Abs returns an absolute representation of path.
func Abs(path string) (string, error) {
	panic("filepath.Abs: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __filepathAbs(path string) filepathPair {
	if IsAbs(path) {
		return filepathPair{Clean(path), nil}
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepathPair{"", err}
	}
	return filepathPair{join([]string{wd, path}), nil}
}

// Rel returns a relative path that is lexically equivalent to targPath
// when joined to basePath.
func Rel(basePath, targPath string) (string, error) {
	panic("filepath.Rel: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __filepathRel(basePath, targPath string) filepathPair {
	baseVol := VolumeName(basePath)
	targVol := VolumeName(targPath)
	base := Clean(basePath)
	targ := Clean(targPath)
	if sameWord(targ, base) {
		return filepathPair{".", nil}
	}
	base = base[len(baseVol):]
	targ = targ[len(targVol):]
	if base == "." {
		base = ""
	}
	baseSlashed := len(base) > 0 && base[0] == Separator
	targSlashed := len(targ) > 0 && targ[0] == Separator
	if baseSlashed != targSlashed || !sameWord(baseVol, targVol) {
		return filepathPair{"", errors.New("Rel: can't make " + targPath + " relative to " + basePath)}
	}
	bl := len(base)
	tl := len(targ)
	var b0, bi, t0, ti int
	for {
		for bi < bl && base[bi] != Separator {
			bi++
		}
		for ti < tl && targ[ti] != Separator {
			ti++
		}
		if !sameWord(targ[t0:ti], base[b0:bi]) {
			break
		}
		if bi < bl {
			bi++
		}
		if ti < tl {
			ti++
		}
		b0 = bi
		t0 = ti
	}
	if base[b0:bi] == ".." {
		return filepathPair{"", errors.New("Rel: can't make " + targPath + " relative to " + basePath)}
	}
	if b0 != bl {
		seps := fpCountSeparator(base[b0:bl])
		size := 2 + seps*3
		if tl != t0 {
			size += 1 + tl - t0
		}
		buf := make([]byte, size)
		n := copy(buf, "..")
		for i := 0; i < seps; i++ {
			buf[n] = Separator
			copy(buf[n+1:], "..")
			n += 3
		}
		if t0 != tl {
			buf[n] = Separator
			copy(buf[n+1:], targ[t0:])
		}
		return filepathPair{Clean(string(buf)), nil}
	}
	return filepathPair{targ[t0:], nil}
}

// sameWord compares path elements for Rel (case-insensitive on windows;
// exact on unix).
func sameWord(a, b string) bool { return a == b }

// fpCountSeparator counts Separator bytes (internal/bytealg port).
func fpCountSeparator(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == Separator {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// EvalSymlinks.
// ---------------------------------------------------------------------------

// EvalSymlinks returns the path name after the evaluation of any symbolic
// links.
func EvalSymlinks(path string) (string, error) {
	panic("filepath.EvalSymlinks: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __filepathEvalSymlinks(path string) filepathPair {
	return walkSymlinks(path)
}

func walkSymlinks(path string) filepathPair {
	volLen := 0
	pathSeparator := string(Separator)
	if volLen < len(path) && os.IsPathSeparator(path[volLen]) {
		volLen++
	}
	vol := path[:volLen]
	dest := vol
	linksWalked := 0
	for start, end := volLen, volLen; start < len(path); start = end {
		for start < len(path) && os.IsPathSeparator(path[start]) {
			start++
		}
		end = start
		for end < len(path) && !os.IsPathSeparator(path[end]) {
			end++
		}
		if end == start {
			break
		} else if path[start:end] == "." {
			continue
		} else if path[start:end] == ".." {
			var r int
			for r = len(dest) - 1; r >= volLen; r-- {
				if os.IsPathSeparator(dest[r]) {
					break
				}
			}
			if r < volLen || dest[r+1:] == ".." {
				if len(dest) > volLen {
					dest += pathSeparator
				}
				dest += ".."
			} else {
				dest = dest[:r]
			}
			continue
		}
		if len(dest) > 0 && !os.IsPathSeparator(dest[len(dest)-1]) {
			dest += pathSeparator
		}
		dest += path[start:end]
		fi, err := os.Lstat(dest)
		if err != nil {
			return filepathPair{"", err}
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			if !fi.Mode().IsDir() && end < len(path) {
				return filepathPair{"", syscall.ENOTDIR}
			}
			continue
		}
		linksWalked++
		if linksWalked > 255 {
			return filepathPair{"", errors.New("EvalSymlinks: too many links")}
		}
		link, err := os.Readlink(dest)
		if err != nil {
			return filepathPair{"", err}
		}
		path = link + path[end:]
		v := 0
		if v > 0 {
			if v < len(link) && os.IsPathSeparator(link[v]) {
				v++
			}
			vol = link[:v]
			dest = vol
			end = len(vol)
		} else if len(link) > 0 && os.IsPathSeparator(link[0]) {
			dest = link[:1]
			end = 1
			vol = link[:1]
			volLen = 1
		} else {
			var r int
			for r = len(dest) - 1; r >= volLen; r-- {
				if os.IsPathSeparator(dest[r]) {
					break
				}
			}
			if r < volLen {
				dest = vol
			} else {
				dest = dest[:r]
			}
			end = 0
		}
	}
	return filepathPair{Clean(dest), nil}
}

// ---------------------------------------------------------------------------
// Match (port of go1.27.1 path/filepath/match.go, unix: '\\' escapes,
// Separator-scoped '*').
// ---------------------------------------------------------------------------

// Match reports whether name matches the shell file name pattern.
func Match(pattern, name string) (matched bool, err error) {
	panic("filepath.Match: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

type filepathMatchPair struct {
	matched bool
	err     error
}

func __filepathMatch(pattern, name string) filepathMatchPair {
	mp := fpMatch(pattern, name)
	return filepathMatchPair{mp.matched, mp.err}
}

type fpMatchResult struct {
	matched bool
	err     error
}

type fpScanChunkResult struct {
	star  bool
	chunk string
	rest  string
}

type fpMatchChunkResult struct {
	rest string
	ok   bool
	err  error
}

type fpGetEscResult struct {
	r      rune
	nchunk string
	err    error
}

type fpDecodeRuneResult struct {
	r rune
	n int
}

func fpMatch(pattern, name string) fpMatchResult {
Pattern:
	for len(pattern) > 0 {
		var star bool
		var chunk string
		sc := fpScanChunk(pattern)
		star, chunk, pattern = sc.star, sc.chunk, sc.rest
		if star && chunk == "" {
			return fpMatchResult{!strings.Contains(name, string(Separator)), nil}
		}
		mc := fpMatchChunk(chunk, name)
		t, ok, err := mc.rest, mc.ok, mc.err
		if ok && (len(t) == 0 || len(pattern) > 0) {
			name = t
			continue
		}
		if err != nil {
			return fpMatchResult{false, err}
		}
		if star {
			for i := 0; i < len(name) && name[i] != Separator; i++ {
				mc := fpMatchChunk(chunk, name[i+1:])
				t, ok, err := mc.rest, mc.ok, mc.err
				if ok {
					if len(pattern) == 0 && len(t) > 0 {
						continue
					}
					name = t
					continue Pattern
				}
				if err != nil {
					return fpMatchResult{false, err}
				}
			}
		}
		return fpMatchResult{false, nil}
	}
	return fpMatchResult{len(name) == 0, nil}
}

func fpScanChunk(pattern string) fpScanChunkResult {
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
				return fpScanChunkResult{star, pattern[:i], pattern[i:]}
			}
		}
	}
	return fpScanChunkResult{star, pattern, ""}
}

func fpMatchChunk(chunk, s string) fpMatchChunkResult {
	failed := false
	for len(chunk) > 0 {
		failed = failed || len(s) == 0
		switch chunk[0] {
		case '[':
			var r rune
			if !failed {
				// ASCII-only (scope-limit: see fpGetEsc).
				r = rune(s[0])
				s = s[1:]
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
				ge := fpGetEsc(chunk)
				if ge.err != nil {
					return fpMatchChunkResult{rest: "", ok: false, err: ge.err}
				}
				lo, chunk := ge.r, ge.nchunk
				hi := lo
				if chunk[0] == '-' {
					ge := fpGetEsc(chunk[1:])
					if ge.err != nil {
						return fpMatchChunkResult{rest: "", ok: false, err: ge.err}
					}
					hi, chunk = ge.r, ge.nchunk
				}
				match = match || lo <= r && r <= hi
				nrange++
			}
			failed = failed || match == negated
		case '?':
			if !failed {
				failed = s[0] == Separator
				// ASCII-only (scope-limit: see fpGetEsc).
				s = s[1:]
			}
			chunk = chunk[1:]
		case '\\':
			chunk = chunk[1:]
			if len(chunk) == 0 {
				return fpMatchChunkResult{rest: "", ok: false, err: ErrBadPattern}
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
		return fpMatchChunkResult{rest: "", ok: false, err: nil}
	}
	return fpMatchChunkResult{rest: s, ok: true, err: nil}
}

func fpGetEsc(chunk string) fpGetEscResult {
	if len(chunk) == 0 || chunk[0] == '-' || chunk[0] == ']' {
		return fpGetEscResult{r: 0, nchunk: "", err: ErrBadPattern}
	}
	if chunk[0] == '\\' {
		chunk = chunk[1:]
		if len(chunk) == 0 {
			return fpGetEscResult{r: 0, nchunk: "", err: ErrBadPattern}
		}
	}
	// ASCII-only decode (scope-limit: the frontend miscompiles the
	// struct-returning fpDecodeRune helper; non-ASCII bytes in character
	// classes decode as single bytes, not runes. Retest trigger: frontend
	// struct-literal bug fixed).
	r := rune(chunk[0])
	nchunk := chunk[1:]
	if len(nchunk) == 0 {
		return fpGetEscResult{r: 0, nchunk: "", err: ErrBadPattern}
	}
	return fpGetEscResult{r: r, nchunk: nchunk, err: nil}
}

// fpDecodeRune decodes the first rune of s (0xFFFD is
// unicode/utf8.RuneError; overlong/surrogate/out-of-range rejected).
func fpDecodeRune(s string) fpDecodeRuneResult {
	if len(s) == 0 {
		return fpDecodeRuneResult{r: 0xFFFD, n: 0}
	}
	c := s[0]
	if c < 0x80 {
		var result fpDecodeRuneResult
		result.r = rune(c)
		result.n = 1
		return result
	}
	var size int
	var acceptLo, acceptHi uint8 = 0x80, 0xBF
	switch {
	case c&0xE0 == 0xC0:
		size = 2
		if c < 0xC2 {
			return fpDecodeRuneResult{r: 0xFFFD, n: 1}
		}
	case c&0xF0 == 0xE0:
		size = 3
		if c == 0xE0 {
			acceptLo = 0xA0
		} else if c == 0xED {
			acceptHi = 0x9F
		}
	case c&0xF8 == 0xF0:
		size = 4
		if c > 0xF4 {
			return fpDecodeRuneResult{r: 0xFFFD, n: 1}
		}
		if c == 0xF0 {
			acceptLo = 0x90
		} else if c == 0xF4 {
			acceptHi = 0x8F
		}
	default:
		return fpDecodeRuneResult{r: 0xFFFD, n: 1}
	}
	if len(s) < size {
		return fpDecodeRuneResult{r: 0xFFFD, n: 1}
	}
	if c1 := s[1]; c1 < acceptLo || acceptHi < c1 {
		return fpDecodeRuneResult{r: 0xFFFD, n: 1}
	}
	for i := 2; i < size; i++ {
		if c := s[i]; c < 0x80 || 0xBF < c {
			return fpDecodeRuneResult{r: 0xFFFD, n: 1}
		}
	}
	switch size {
	case 2:
		return fpDecodeRuneResult{r: rune(c&0x1F)<<6 | rune(s[1]&0x3F), n: 2}
	case 3:
		return fpDecodeRuneResult{r: rune(c&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), n: 3}
	}
	return fpDecodeRuneResult{r: rune(c&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), n: 4}
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Glob.
// ---------------------------------------------------------------------------

// Glob returns the names of all files matching pattern or nil if there is
// no matching file. The syntax of patterns is the same as in Match.
// Glob ignores I/O errors reading directories; the only possible returned
// error is ErrBadPattern.
func Glob(pattern string) (matches []string, err error) {
	panic("filepath.Glob: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

func __filepathGlob(pattern string) filepathGlobPair {
	return __filepathGlobWithLimit(pattern, 0)
}

func __filepathGlobWithLimit(pattern string, depth int) filepathGlobPair {
	const pathSeparatorsLimit = 10000
	if depth == pathSeparatorsLimit {
		return filepathGlobPair{nil, ErrBadPattern}
	}
	if mp := __filepathMatch(pattern, ""); mp.err != nil {
		return filepathGlobPair{nil, mp.err}
	}
	if !hasMeta(pattern) {
		if _, err := os.Lstat(pattern); err != nil {
			return filepathGlobPair{nil, nil}
		}
		return filepathGlobPair{[]string{pattern}, nil}
	}
	sp := __filepathSplit(pattern)
	dir, file := sp.dir, sp.file
	dir = cleanGlobPath(dir)
	if !hasMeta(dir) {
		return __filepathGlobDir(dir, file, nil)
	}
	if dir == pattern {
		return filepathGlobPair{nil, ErrBadPattern}
	}
	dp := __filepathGlobWithLimit(dir, depth+1)
	if dp.err != nil {
		return filepathGlobPair{nil, dp.err}
	}
	matches := dp.matches
	for _, d := range dp.matches {
		gp := __filepathGlobDir(d, file, matches)
		if gp.err != nil {
			return filepathGlobPair{gp.matches, gp.err}
		}
		matches = gp.matches
	}
	return filepathGlobPair{matches, nil}
}

func cleanGlobPath(path string) string {
	switch path {
	case "":
		return "."
	case string(Separator):
		return path
	default:
		return path[0 : len(path)-1]
	}
}

func __filepathGlobDir(dir, pattern string, matches []string) filepathGlobPair {
	rdp := readDirNames(dir)
	if rdp.err != nil {
		return filepathGlobPair{matches, nil}
	}
	m := matches
	for _, n := range rdp.names {
		mp := __filepathMatch(pattern, n)
		if mp.err != nil {
			return filepathGlobPair{m, mp.err}
		}
		if mp.matched {
			m = append(m, join([]string{dir, n}))
		}
	}
	return filepathGlobPair{m, nil}
}

func hasMeta(path string) bool {
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '*', '?', '[', '\\':
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Walk / WalkDir.
// ---------------------------------------------------------------------------

// readDirNames reads the directory named by dirname and returns a sorted
// list of directory entry names.
func readDirNames(dirname string) filepathReadDirNamesPair {
	f, err := os.Open(dirname)
	if err != nil {
		return filepathReadDirNamesPair{nil, err}
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return filepathReadDirNamesPair{nil, err}
	}
	__sortStrings(names)
	return filepathReadDirNamesPair{names, nil}
}

func __sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Walk walks the file tree rooted at root, calling fn for each file or
// directory in the tree, including root. Walk does not follow symbolic
// links.
func Walk(root string, fn WalkFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walk(root, info, fn)
	}
	if err == SkipDir || err == SkipAll {
		return nil
	}
	return err
}

func walk(path string, info fs.FileInfo, walkFn WalkFunc) error {
	if !info.IsDir() {
		return walkFn(path, info, nil)
	}
	rdp := readDirNames(path)
	err1 := walkFn(path, info, rdp.err)
	if rdp.err != nil || err1 != nil {
		return err1
	}
	// Index-based loop: the frontend's range-variable handling across
	// recursive calls is suspect (walk stops after first subtree).
	for i := 0; i < len(rdp.names); i++ {
		name := rdp.names[i]
		filename := join([]string{path, name})
		fileInfo, err := os.Lstat(filename)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != SkipDir {
				return err
			}
		} else {
			err = walk(filename, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

// WalkDir walks the file tree rooted at root, calling fn for each file or
// directory in the tree, including root. WalkDir does not follow symbolic
// links. Paths use the OS separator (unlike io/fs.WalkDir).
func WalkDir(root string, fn fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDir(root, fs.FileInfoToDirEntry(info), fn)
	}
	if err == SkipDir || err == SkipAll {
		return nil
	}
	return err
}

func walkDir(path string, d fs.DirEntry, walkDirFn fs.WalkDirFunc) error {
	if err := walkDirFn(path, d, nil); err != nil || !d.IsDir() {
		if err == SkipDir && d.IsDir() {
			err = nil
		}
		return err
	}
	rd, err := os.ReadDir(path)
	if err != nil {
		// Second call, to report ReadDir error.
		err = walkDirFn(path, d, err)
		if err != nil {
			if err == SkipDir && d.IsDir() {
				err = nil
			}
			return err
		}
	}

	// Index-based loop: the frontend's range-variable handling across
	// recursive calls is suspect (walk stops after first subtree).
	for i := 0; i < len(rd); i++ {
		d1 := rd[i]
		path1 := join([]string{path, d1.Name()})
		if err := walkDir(path1, d1, walkDirFn); err != nil {
			if err == SkipDir {
				break
			}
			return err
		}
	}
	return nil
}
`
