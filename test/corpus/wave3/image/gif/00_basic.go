package main

// L11 (REVIEW-2026-11-18-05): image/gif — zero corpus imports before
// this program. Encoding and decoding GIFs: a single-frame round-trip
// with exact pixels, an animated EncodeAll with delays and disposal,
// and the error paths on truncated and non-GIF data.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
)

func checkerboard() *image.Paletted {
	pal := color.Palette{color.Black, color.White, color.RGBA{255, 0, 0, 255}}
	m := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			m.SetColorIndex(x, y, uint8((x+y)%3))
		}
	}
	return m
}

func main() {
	m := checkerboard()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, m, nil); err != nil {
		fmt.Printf("encode-err=%v\n", err != nil)
		return
	}
	fmt.Printf("encoded-bytes=%d\n", buf.Len())
	encoded := append([]byte(nil), buf.Bytes()...)
	dec, err := gif.Decode(&buf)
	if err != nil {
		fmt.Printf("decode-err=%v\n", err != nil)
		return
	}
	fmt.Printf("decoded-bounds=%v\n", dec.Bounds())
	match := true
	for y := 0; y < 4 && match; y++ {
		for x := 0; x < 4; x++ {
			if dec.At(x, y) != m.At(x, y) {
				match = false
			}
		}
	}
	fmt.Printf("pixels-match=%v\n", match)
	// Animated: two frames, delays, disposal.
	var buf2 bytes.Buffer
	g := &gif.GIF{
		Image:     []*image.Paletted{m, checkerboard()},
		Delay:     []int{10, 25},
		Disposal:  []byte{gif.DisposalNone, gif.DisposalBackground},
		LoopCount: 3,
	}
	if err := gif.EncodeAll(&buf2, g); err != nil {
		fmt.Printf("encodeall-err=%v\n", err != nil)
		return
	}
	dec2, err := gif.DecodeAll(&buf2)
	if err != nil {
		fmt.Printf("decodeall-err=%v\n", err != nil)
		return
	}
	fmt.Printf("frames=%d delays=%v loop=%d\n", len(dec2.Image), dec2.Delay, dec2.LoopCount)
	// Error paths.
	_, err = gif.Decode(bytes.NewReader([]byte("not a gif")))
	fmt.Printf("nongif-err=%v\n", err != nil)
	_, err = gif.Decode(bytes.NewReader(encoded[:len(encoded)/2]))
	fmt.Printf("truncated-err=%v\n", err != nil)
	// Options: quantizer choice is honoured via NumColors.
	var buf3 bytes.Buffer
	if err := gif.Encode(&buf3, m, &gif.Options{NumColors: 2}); err != nil {
		fmt.Printf("encode-opt-err=%v\n", err != nil)
		return
	}
	fmt.Printf("numcolors2-bytes=%d\n", buf3.Len())
}
