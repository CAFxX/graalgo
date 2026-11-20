package main

// L11 (REVIEW-2026-11-18-05): image/png — zero corpus imports before
// this program. Encoding and decoding PNGs: an RGBA gradient
// round-trip with exact pixels, compression-level byte counts, and
// the error paths on truncated and non-PNG data.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

func gradient() *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			m.SetRGBA(x, y, color.RGBA{uint8(x * 32), uint8(y * 32), 128, 255})
		}
	}
	return m
}

func main() {
	m := gradient()
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		fmt.Printf("encode-err=%v\n", err != nil)
		return
	}
	fmt.Printf("encoded-bytes=%d\n", buf.Len())
	encoded := append([]byte(nil), buf.Bytes()...)
	dec, err := png.Decode(&buf)
	if err != nil {
		fmt.Printf("decode-err=%v\n", err != nil)
		return
	}
	fmt.Printf("decoded-bounds=%v model=%T\n", dec.Bounds(), dec.ColorModel())
	match := true
	for y := 0; y < 8 && match; y++ {
		for x := 0; x < 8; x++ {
			if dec.At(x, y) != m.At(x, y) {
				match = false
			}
		}
	}
	fmt.Printf("pixels-match=%v\n", match)
	// Compression levels change the byte count deterministically.
	for _, level := range []png.CompressionLevel{png.DefaultCompression, png.NoCompression, png.BestSpeed, png.BestCompression} {
		var b bytes.Buffer
		enc := &png.Encoder{CompressionLevel: level}
		if err := enc.Encode(&b, m); err != nil {
			fmt.Printf("level %d err=%v\n", level, err != nil)
			continue
		}
		fmt.Printf("level %d bytes=%d\n", level, b.Len())
	}
	// Error paths.
	_, err = png.Decode(bytes.NewReader([]byte("not a png")))
	fmt.Printf("nonpng-err=%v\n", err != nil)
	_, err = png.Decode(bytes.NewReader(encoded[:len(encoded)/2]))
	fmt.Printf("truncated-err=%v\n", err != nil)
	// DecodeConfig reads the header without the pixels.
	cfg, err := png.DecodeConfig(bytes.NewReader(encoded))
	fmt.Printf("config-err=%v w=%d h=%d model=%T\n", err != nil, cfg.Width, cfg.Height, cfg.ColorModel)
}
