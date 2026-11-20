package main

// L11 (REVIEW-2026-11-18-05): image/color/palette — zero corpus
// imports before this program. The standard palettes: Plan9 and
// WebSafe sizes, selected entries, and palette lookup behaviour.

import (
	"fmt"
	"image/color"
	"image/color/palette"
)

func show(name string, p color.Palette) {
	fmt.Printf("palette %s len=%d\n", name, len(p))
	for _, i := range []int{0, 1, 2, len(p) / 2, len(p) - 1} {
		if i < 0 || i >= len(p) {
			continue
		}
		r, g, b, a := p[i].RGBA()
		fmt.Printf("palette %s [%d] rgba=%d,%d,%d,%d\n", name, i, r>>8, g>>8, b>>8, a>>8)
	}
	// Index lookup: exact colour finds its slot.
	fmt.Printf("palette %s index-of-first=%d\n", name, p.Index(p[0]))
}

func main() {
	show("plan9", palette.Plan9)
	show("websafe", palette.WebSafe)
}
