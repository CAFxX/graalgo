package vet

import (
	"strings"
	"testing"
)

// Tests for the `composites` analyzer (runComposites in composites.go).

func TestCompositesAnalyzerUnkeyedImported(t *testing.T) {
	// image.Point has exported fields, so an unkeyed literal type-checks
	// and must be flagged for an imported type.
	src := `package p

import "image"

var _ = image.Point{1, 2}
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	if !strings.Contains(joined, "composite literal uses unkeyed fields") {
		t.Errorf("unkeyed imported composite literal not flagged, got:\n%s", joined)
	}
}

func TestCompositesAnalyzerKeyedImported(t *testing.T) {
	src := `package p

import "image"

var _ = image.Point{X: 1, Y: 2}
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	if strings.Contains(joined, "composite literal uses unkeyed fields") {
		t.Errorf("keyed composite literal flagged:\n%s", joined)
	}
}

func TestCompositesAnalyzerUnkeyedLocal(t *testing.T) {
	// Unkeyed literals of the package's own types are not flagged
	// (matching go vet's default).
	src := `package p

type T struct {
	X int
	Y int
}

var _ = T{1, 2}
`
	joined := runVetOnModule(t, "", map[string]string{"p.go": src})
	if strings.Contains(joined, "composite literal uses unkeyed fields") {
		t.Errorf("unkeyed local composite literal flagged:\n%s", joined)
	}
}
