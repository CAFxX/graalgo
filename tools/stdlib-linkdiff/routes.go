package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Route is one explicit linkname-target routing: the stdlib target is
// deliberately NOT provided by graalgo; it is routed to a replacement
// with a recorded reason.
type Route struct {
	Target      string // normalized linkname target, e.g. "runtime.writeBarrier"
	Replacement string // what graalgo routes it to, e.g. "const:false"
	Reason      string // why the route is correct
}

// loadRoutes parses a routes file. Format, one route per line:
//
//	target -> replacement # reason
//
// Blank lines and lines starting with '#' are ignored. The '# reason'
// part is mandatory: a route without a recorded reason is rejected.
func loadRoutes(path string) ([]Route, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var routes []Route
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineno := 0
	for sc.Scan() {
		lineno++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		arrow := strings.Index(line, "->")
		if arrow < 0 {
			return nil, fmt.Errorf("%s:%d: route without '->'", path, lineno)
		}
		target := strings.TrimSpace(line[:arrow])
		rest := strings.TrimSpace(line[arrow+2:])
		replacement, reason := rest, ""
		if i := strings.Index(rest, "#"); i >= 0 {
			replacement = strings.TrimSpace(rest[:i])
			reason = strings.TrimSpace(rest[i+1:])
		}
		if target == "" || replacement == "" {
			return nil, fmt.Errorf("%s:%d: route with empty target or replacement", path, lineno)
		}
		if reason == "" {
			return nil, fmt.Errorf("%s:%d: route %q has no recorded reason", path, lineno, target)
		}
		if prev, dup := seen[target]; dup {
			return nil, fmt.Errorf("%s:%d: duplicate route for %q (first at line %d)", path, lineno, target, prev)
		}
		seen[target] = lineno
		routes = append(routes, Route{Target: target, Replacement: replacement, Reason: reason})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return routes, nil
}
