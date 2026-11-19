package main

// L11 (REVIEW-2026-11-18-05): image/jpeg — zero corpus imports before
// this program. Encode a generated gradient, decode it back, compare
// bounds and sample pixels with a tolerance (JPEG is lossy; exact
// pixel values are not pinned). No external files.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
)

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func main() {
	const w, h = 32, 24
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / (w - 1)),
				G: uint8(y * 255 / (h - 1)),
				B: 128,
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		fmt.Printf("encode-err=%v\n", err)
		return
	}
	fmt.Printf("encoded-bytes=%d\n", buf.Len())

	cfg, format, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		fmt.Printf("config-err=%v\n", err)
		return
	}
	fmt.Printf("config-format=%q size=%dx%d\n", format, cfg.Width, cfg.Height)

	dec, err := jpeg.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		fmt.Printf("decode-err=%v\n", err)
		return
	}
	fmt.Printf("bounds=%v\n", dec.Bounds())

	// Sample pixels: within tolerance 12 per 16-bit channel.
	const tol = 12 * 0x101
	points := [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}, {w / 2, h / 2}}
	allOK := true
	for _, p := range points {
		r1, g1, b1, _ := img.At(p[0], p[1]).RGBA()
		r2, g2, b2, _ := dec.At(p[0], p[1]).RGBA()
		ok := absDiff(r1, r2) <= tol && absDiff(g1, g2) <= tol && absDiff(b1, b2) <= tol
		fmt.Printf("px-%d-%d-ok=%v\n", p[0], p[1], ok)
		allOK = allOK && ok
	}
	fmt.Printf("roundtrip-ok=%v\n", allOK)
}
