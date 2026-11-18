package fork

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Apply applies a unified diff of one file to orig and returns the patched
// content and the diff's added and deleted line counts. It is strict:
// every hunk must match at exactly the line its header names, context and
// deleted lines byte for byte. A patch that needs an offset or fuzz is a
// patch against another upstream version and must be regenerated, not
// tolerated.
func Apply(orig, patch []byte) (out []byte, added, deleted int, err error) {
	src := bytes.SplitAfter(orig, []byte("\n"))
	if n := len(src); n > 0 && len(src[n-1]) == 0 {
		src = src[:n-1]
	}
	lines := bytes.SplitAfter(patch, []byte("\n"))
	pos := 0 // next unconsumed line of src
	hunks := 0
	i := 0
	for i < len(lines) {
		line := lines[i]
		m := hunkHeader.FindSubmatch(line)
		if m == nil {
			if hunks > 0 && len(bytes.TrimSpace(line)) > 0 {
				return nil, 0, 0, fmt.Errorf("patch line %d: unexpected text after a hunk", i+1)
			}
			i++ // file headers ("--- a/x", "+++ b/x", "diff ...") and blank tail
			continue
		}
		hunks++
		start, _ := strconv.Atoi(string(m[1]))
		oldN, newN := 1, 1
		if len(m[2]) > 0 {
			oldN, _ = strconv.Atoi(string(m[2]))
		}
		if len(m[4]) > 0 {
			newN, _ = strconv.Atoi(string(m[4]))
		}
		at := start - 1
		if oldN == 0 {
			at = start // a pure insertion names the line it follows
		}
		if at < pos || at > len(src) {
			return nil, 0, 0, fmt.Errorf("patch line %d: hunk at line %d is out of order or past the end", i+1, start)
		}
		out = append(out, bytes.Join(src[pos:at], nil)...)
		pos = at
		i++
		for oldN > 0 || newN > 0 {
			if i >= len(lines) || len(lines[i]) == 0 {
				return nil, 0, 0, fmt.Errorf("patch line %d: hunk is truncated", i+1)
			}
			l := lines[i]
			text := l[1:]
			noNewline := i+1 < len(lines) && bytes.HasPrefix(lines[i+1], []byte(`\ No newline at end of file`))
			if noNewline {
				text = bytes.TrimSuffix(text, []byte("\n"))
			}
			switch l[0] {
			case ' ', '-':
				if pos >= len(src) || !bytes.Equal(src[pos], text) {
					return nil, 0, 0, fmt.Errorf("patch line %d: does not match upstream line %d", i+1, pos+1)
				}
				if l[0] == ' ' {
					out = append(out, text...)
					newN--
				} else {
					deleted++
				}
				pos++
				oldN--
			case '+':
				out = append(out, text...)
				added++
				newN--
			default:
				return nil, 0, 0, fmt.Errorf("patch line %d: not a hunk line", i+1)
			}
			if oldN < 0 || newN < 0 {
				return nil, 0, 0, fmt.Errorf("patch line %d: hunk is longer than its header says", i+1)
			}
			i++
			if noNewline {
				i++
			}
		}
	}
	if hunks == 0 {
		return nil, 0, 0, fmt.Errorf("no hunks")
	}
	out = append(out, bytes.Join(src[pos:], nil)...)
	return out, added, deleted, nil
}
