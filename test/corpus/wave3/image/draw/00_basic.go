package main

// L11 (REVIEW-2026-11-18-05): image/draw — zero corpus imports
// before this program. Compositing: Draw with Src and Over, DrawMask
// with an alpha mask, and Floyd-Steinberg dithering on a paletted
// image; resulting pixels are printed.

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
)

func px(m *image.RGBA, x, y int) string {
	r, g, b, a := m.At(x, y).RGBA()
	return fmt.Sprintf("%d,%d,%d,%d", r>>8, g>>8, b>>8, a>>8)
}

func main() {
	// Src: opaque red over opaque blue replaces.
	dst := image.NewRGBA(image.Rect(0, 0, 4, 4))
	blue := image.NewUniform(color.RGBA{0, 0, 255, 255})
	draw.Draw(dst, dst.Bounds(), blue, image.Point{}, draw.Src)
	red := image.NewUniform(color.RGBA{255, 0, 0, 255})
	draw.Draw(dst, image.Rect(1, 1, 3, 3), red, image.Point{}, draw.Src)
	fmt.Printf("src corner=%s centre=%s\n", px(dst, 0, 0), px(dst, 1, 1))
	// Over: half-transparent white over the red centre lightens it.
	half := image.NewUniform(color.RGBA{255, 255, 255, 128})
	draw.Draw(dst, image.Rect(1, 1, 3, 3), half, image.Point{}, draw.Over)
	fmt.Printf("over centre=%s\n", px(dst, 1, 1))
	// DrawMask: a gradient alpha mask over a green source.
	mask := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			mask.SetAlpha(x, y, color.Alpha{uint8(x * 85)})
		}
	}
	green := image.NewUniform(color.RGBA{0, 255, 0, 255})
	draw.DrawMask(dst, dst.Bounds(), green, image.Point{}, mask, image.Point{}, draw.Over)
	for x := 0; x < 4; x++ {
		fmt.Printf("mask x=%d => %s\n", x, px(dst, x, 0))
	}
	// Floyd-Steinberg: dither a grey gradient into a 2-colour palette.
	pal := color.Palette{color.Black, color.White}
	pm := image.NewPaletted(image.Rect(0, 0, 4, 1), pal)
	grey := image.NewGray(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		grey.SetGray(x, 0, color.Gray{uint8(x * 64)})
	}
	draw.FloydSteinberg.Draw(pm, pm.Bounds(), grey, image.Point{})
	idx := make([]int, 4)
	for x := 0; x < 4; x++ {
		idx[x] = int(pm.ColorIndexAt(x, 0))
	}
	fmt.Printf("dither=%v\n", idx)
}
