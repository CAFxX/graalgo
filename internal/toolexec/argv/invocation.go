package argv

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ChainFlag is graalgo-tool's own flag: "-chain X" names a user -toolexec
// command X that graalgo-tool runs every stock tool under (design §1 step
// 4). X is one argument, the user's -toolexec value percent-encoded with
// [EncodeChain] and split with [SplitQuoted] as cmd/go splits a -toolexec
// value. The encoding keeps X free of spaces and quotes: the launcher
// nests it inside a -toolexec value that may itself sit in a quoted GOFLAGS
// field, and quoted.Split has no escapes, so a quoted X could not be
// nested.
const ChainFlag = "-chain"

// EncodeChain percent-encodes every byte of s outside a conservative set
// that needs no quoting anywhere.
func EncodeChain(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("/._-:=,+@", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// DecodeChain inverts EncodeChain.
func DecodeChain(s string) (string, error) {
	v, err := url.PathUnescape(s)
	if err != nil {
		return "", fmt.Errorf("-chain %q: %v", s, err)
	}
	return v, nil
}

// Invocation is one graalgo-tool command line, decoded.
type Invocation struct {
	// Chain is the user's -toolexec command, split; empty when none.
	Chain []string
	// Tool is the tool path cmd/go asked for (argv[1] of a stock -toolexec
	// invocation).
	Tool string
	// Args are the tool's arguments exactly as they must reach the stock
	// tool: either the original argv or a single "@file" whose file has
	// been rewritten to hold only these arguments.
	Args []string
	// Expanded are the tool's arguments with every response file
	// expanded; what graalgo-tool itself interprets.
	Expanded []string
	// ResponseFile is the rewritten response file, or "" when cmd/go did
	// not use one.
	ResponseFile string
}

// ParseInvocation decodes graalgo-tool's argv (without argv[0]).
//
// cmd/go decides whether to pass a response file from the basename of the
// -toolexec program (design §1, spike 5 F6a). When it does, the file holds
// everything after argv[0] — graalgo-tool's own -chain flag, the tool path
// and the tool's arguments. ParseInvocation then rewrites the file in place
// without the graalgo-tool prefix, so that the stock tool can be given the
// same "@file" (spike 5 F6b); nested response files inside it are left for
// the stock tool to expand and are expanded only into Expanded.
func ParseInvocation(args []string) (*Invocation, error) {
	inv := &Invocation{}
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		file := args[0][1:]
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("response file: %w", err)
		}
		inner := ParseArgs(data)
		rest, err := inv.takePrefix(inner)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(file, EncodeArgs(rest), 0o644); err != nil {
			return nil, fmt.Errorf("rewriting response file: %w", err)
		}
		inv.ResponseFile = file
		inv.Args = []string{"@" + file}
		inv.Expanded, err = Expand(rest)
		if err != nil {
			return nil, err
		}
		return inv, nil
	}
	rest, err := inv.takePrefix(args)
	if err != nil {
		return nil, err
	}
	inv.Args = rest
	inv.Expanded, err = Expand(rest)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// takePrefix consumes [-chain X] tool and returns the tool's arguments.
func (inv *Invocation) takePrefix(args []string) ([]string, error) {
	if len(args) > 0 && args[0] == ChainFlag {
		if len(args) < 2 {
			return nil, errors.New("-chain needs a value")
		}
		x, err := DecodeChain(args[1])
		if err != nil {
			return nil, err
		}
		chain, err := SplitQuoted(x)
		if err != nil {
			return nil, fmt.Errorf("-chain %q: %v", args[1], err)
		}
		inv.Chain = chain
		args = args[2:]
	}
	if len(args) == 0 {
		return nil, errors.New("no tool named (usage: graalgo-tool [-chain CMD] TOOL ARGS...)")
	}
	inv.Tool = args[0]
	return args[1:], nil
}

// Command returns the argv that runs the stock tool: the chain (if any),
// the tool, and Args.
func (inv *Invocation) Command() []string {
	c := append([]string{}, inv.Chain...)
	c = append(c, inv.Tool)
	return append(c, inv.Args...)
}
