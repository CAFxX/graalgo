// Package argv holds the command-line plumbing shared by the graalgo
// launcher and graalgo-tool: response files (docs/arch/toolexec-design.md
// §2.2 step 1), cmd/go's quoted-field splitting, and the composition of the
// -toolexec and -tags values that the launcher injects (§1).
//
// Everything here reproduces a cmd/go or cmd/internal/objabi algorithm of
// the tracked release; the tests pin the behaviour against the stock tools
// where that is observable.
package argv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxResponseDepth bounds nested "@file" expansion. objabi.expandArgs has
// no bound (a self-including response file recurses until the stack is
// exhausted); graalgo-tool fails cleanly instead.
const maxResponseDepth = 32

// ParseArgs splits response-file content into arguments with the
// GCC-compatible rules of cmd/internal/objabi.ParseArgs (go1.27):
// whitespace separates arguments; single quotes are literal; double quotes
// understand \\ \" \$ \` and backslash-newline; outside quotes a backslash
// escapes the next byte and backslash-newline (LF or CRLF) is a line
// continuation; empty quotes produce an empty argument.
func ParseArgs(s []byte) []string {
	var args []string
	var arg strings.Builder
	hasArg := false
	inSingle, inDouble := false, false
	for i := 0; i < len(s); {
		c := s[i]
		if inSingle {
			if c == '\'' {
				inSingle = false
			} else {
				arg.WriteByte(c)
			}
			i++
			continue
		}
		if inDouble {
			if c == '\\' && i+1 < len(s) {
				switch next := s[i+1]; next {
				case '\\', '"', '$', '`':
					arg.WriteByte(next)
					i += 2
				case '\n':
					i += 2
				case '\r':
					if i+2 < len(s) && s[i+2] == '\n' {
						i += 3
					} else {
						arg.WriteByte(c)
						i++
					}
				default:
					arg.WriteByte(c)
					i++
				}
			} else if c == '"' {
				inDouble = false
				i++
			} else {
				arg.WriteByte(c)
				i++
			}
			continue
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			if arg.Len() > 0 || hasArg {
				args = append(args, arg.String())
				arg.Reset()
				hasArg = false
			}
		case '\'':
			inSingle, hasArg = true, true
		case '"':
			inDouble, hasArg = true, true
		case '\\':
			if i+1 < len(s) {
				next := s[i+1]
				if next == '\n' {
					i += 2
					continue
				}
				if next == '\r' && i+2 < len(s) && s[i+2] == '\n' {
					i += 3
					continue
				}
				arg.WriteByte(next)
				hasArg = true
				i += 2
				continue
			}
			i++
			continue
		default:
			arg.WriteByte(c)
		}
		i++
	}
	if arg.Len() > 0 || hasArg {
		args = append(args, arg.String())
	}
	return args
}

// EncodeArg quotes one argument for a response file exactly as cmd/go's
// encodeArg (cmd/go/internal/work/exec.go) does, so that a rewritten
// response file is byte-identical to one cmd/go would have written.
func EncodeArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\n\r'\"\\$`") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range arg {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '$':
			b.WriteString(`\$`)
		case '`':
			b.WriteString("\\`")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// EncodeArgs renders args as response-file content (one encoded argument
// per line, as cmd/go writes it).
func EncodeArgs(args []string) []byte {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(EncodeArg(a))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// Expand replaces every "@file" argument by the arguments the file holds,
// recursively, as objabi.expandArgs does for the stock tools. It returns
// args itself when there is nothing to expand.
func Expand(args []string) ([]string, error) {
	return expand(args, 0)
}

func expand(in []string, depth int) ([]string, error) {
	var out []string
	for i, s := range in {
		if !strings.HasPrefix(s, "@") {
			if out != nil {
				out = append(out, s)
			}
			continue
		}
		if depth >= maxResponseDepth {
			return nil, fmt.Errorf("response files nested deeper than %d (%s)", maxResponseDepth, s)
		}
		if out == nil {
			out = append(make([]string, 0, 2*len(in)), in[:i]...)
		}
		data, err := os.ReadFile(s[1:])
		if err != nil {
			return nil, fmt.Errorf("response file: %w", err)
		}
		sub, err := expand(ParseArgs(data), depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, sub...)
	}
	if out == nil {
		return in, nil
	}
	return out, nil
}

// ApplyRewrites applies a compile -trimpath value ("prefix=>replacement"
// rules separated by ';'; a rule without "=>" strips its prefix) to a file
// name: cmd/internal/objabi.ApplyRewrites, verbatim. The first rule whose
// prefix is a path prefix of file wins; rewritten reports whether one did
// (even if the result equals the input).
func ApplyRewrites(file, rewrites string) (string, bool) {
	start := 0
	for i := 0; i <= len(rewrites); i++ {
		if i == len(rewrites) || rewrites[i] == ';' {
			if out, ok := applyRewrite(file, rewrites[start:i]); ok {
				return out, true
			}
			start = i + 1
		}
	}
	return file, false
}

func applyRewrite(path, rewrite string) (string, bool) {
	prefix, replace := rewrite, ""
	if j := strings.LastIndex(rewrite, "=>"); j >= 0 {
		prefix, replace = rewrite[:j], rewrite[j+len("=>"):]
	}
	if prefix == "" || !hasPathPrefix(path, prefix) {
		return path, false
	}
	if len(path) == len(prefix) {
		return replace, true
	}
	if replace == "" {
		return path[len(prefix)+1:], true
	}
	return replace + path[len(prefix):], true
}

// hasPathPrefix is objabi's: t is a prefix of s ending at a separator,
// ignoring ASCII case and treating '\\' as '/'.
func hasPathPrefix(s, t string) bool {
	if len(t) > len(s) {
		return false
	}
	var i int
	for i = 0; i < len(t); i++ {
		cs, ct := int(s[i]), int(t[i])
		if 'A' <= cs && cs <= 'Z' {
			cs += 'a' - 'A'
		}
		if 'A' <= ct && ct <= 'Z' {
			ct += 'a' - 'A'
		}
		if cs == '\\' {
			cs = '/'
		}
		if ct == '\\' {
			ct = '/'
		}
		if cs != ct {
			return false
		}
	}
	return i >= len(s) || s[i] == '/' || s[i] == '\\'
}

// AbsFile is cmd/internal/objabi.AbsFile for a linux target: the name gc
// records for file in positions, given the compile's working directory,
// its -trimpath value and GOROOT.
func AbsFile(dir, file, rewrites, goroot string) string {
	abs := file
	if dir != "" && !filepath.IsAbs(file) {
		abs = filepath.Join(dir, file)
	}
	abs, rewritten := ApplyRewrites(abs, rewrites)
	if !rewritten && goroot != "" && hasPathPrefix(abs, goroot) {
		abs = "$GOROOT" + abs[len(goroot):]
	}
	if abs == "" {
		abs = "??"
	}
	return abs
}
