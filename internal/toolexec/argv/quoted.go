package argv

import (
	"fmt"
	"slices"
	"strings"
)

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// SplitQuoted splits s into fields the way cmd/internal/quoted.Split does:
// fields are separated by spaces, and a field that starts with ' or " runs
// to the matching quote with no escapes inside. cmd/go uses it for GOFLAGS
// and for the -toolexec value.
func SplitQuoted(s string) ([]string, error) {
	var f []string
	for len(s) > 0 {
		for len(s) > 0 && isSpaceByte(s[0]) {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		if s[0] == '"' || s[0] == '\'' {
			quote := s[0]
			s = s[1:]
			i := strings.IndexByte(s, quote)
			if i < 0 {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			f = append(f, s[:i])
			s = s[i+1:]
			continue
		}
		i := 0
		for i < len(s) && !isSpaceByte(s[i]) {
			i++
		}
		f = append(f, s[:i])
		s = s[i:]
	}
	return f, nil
}

// JoinQuoted is the inverse of SplitQuoted (cmd/internal/quoted.Join, plus
// a pair of single quotes for an empty field, which quoted.Join would
// drop). It fails when a
// field cannot be represented, and it verifies the round trip, so a value
// it returns always splits back into exactly fields.
func JoinQuoted(fields []string) (string, error) {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(' ')
		}
		space := strings.ContainsAny(f, " \t\n\r")
		single := strings.Contains(f, "'")
		double := strings.Contains(f, `"`)
		switch {
		case f == "":
			b.WriteString("''")
		case !space && !single && !double:
			b.WriteString(f)
		case !single:
			b.WriteString("'" + f + "'")
		case !double:
			b.WriteString(`"` + f + `"`)
		default:
			return "", fmt.Errorf("argument %q contains both single and double quotes and cannot be quoted", f)
		}
	}
	s := b.String()
	back, err := SplitQuoted(s)
	if err != nil || !slices.Equal(back, fields) {
		return "", fmt.Errorf("cannot quote %q unambiguously", fields)
	}
	return s, nil
}
