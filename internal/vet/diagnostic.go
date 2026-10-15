// Package vet implements static analyzers for `graalgo vet`.
//
// Each analyzer inspects the parsed AST (and type information where
// available) and reports diagnostics in the same style as `go vet`.
package vet

import (
	"go/token"
	"sort"
)

// Diagnostic is a single vet finding.
type Diagnostic struct {
	Pos     token.Position // source position
	Message string          // e.g. "possible misuse of fmt.Printf"
}

// ByPosition sorts diagnostics for stable output.
type ByPosition []Diagnostic

func (d ByPosition) Len() int      { return len(d) }
func (d ByPosition) Swap(i, j int) { d[i], d[j] = d[j], d[i] }
func (d ByPosition) Less(i, j int) bool {
	if d[i].Pos.Filename != d[j].Pos.Filename {
		return d[i].Pos.Filename < d[j].Pos.Filename
	}
	if d[i].Pos.Line != d[j].Pos.Line {
		return d[i].Pos.Line < d[j].Pos.Line
	}
	if d[i].Pos.Column != d[j].Pos.Column {
		return d[i].Pos.Column < d[j].Pos.Column
	}
	return d[i].Message < d[j].Pos.String()
}

// SortDiagnostics returns diagnostics in stable source order.
func SortDiagnostics(ds []Diagnostic) []Diagnostic {
	sort.Sort(ByPosition(ds))
	return ds
}
