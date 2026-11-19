// Targets image/color: RGBA() of every concrete color type, conversions
// through RGBAModel/NRGBAModel/RGBA64Model/NRGBA64Model/GrayModel/Gray16Model/
// AlphaModel/Alpha16Model/CMYKModel/YCbCrModel/NYCbCrAModel, the functions
// RGBToYCbCr/YCbCrToRGB/RGBToCMYK/CMYKToRGB over sweeps, and Palette.Index.
package main

import "image/color"

var h uint64 = 14695981039346656037

func mix(vs ...uint32) {
	for _, v := range vs {
		h = (h ^ uint64(v)) * 1099511628211
	}
}

func p4(name string, c color.Color) {
	r, g, b, a := c.RGBA()
	println(name, r, g, b, a)
}

var models = []color.Model{
	color.RGBAModel, color.RGBA64Model, color.NRGBAModel, color.NRGBA64Model,
	color.AlphaModel, color.Alpha16Model, color.GrayModel, color.Gray16Model,
	color.CMYKModel, color.YCbCrModel, color.NYCbCrAModel,
}

func main() {
	samples := []color.Color{
		color.RGBA{10, 20, 30, 40}, color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 0, 0},
		color.RGBA64{0xffff, 0x8000, 1, 0xffff}, color.NRGBA{200, 100, 50, 128},
		color.NRGBA{255, 0, 0, 0}, color.NRGBA64{0x1234, 0x5678, 0x9abc, 0x7fff},
		color.Alpha{77}, color.Alpha16{0xfffe}, color.Gray{128}, color.Gray16{0x0101},
		color.CMYK{0, 255, 255, 0}, color.CMYK{12, 34, 56, 78},
		color.YCbCr{235, 128, 128}, color.YCbCr{16, 240, 16}, color.YCbCr{81, 90, 240},
		color.NYCbCrA{YCbCr: color.YCbCr{200, 50, 200}, A: 99},
		color.Black, color.White, color.Transparent, color.Opaque,
	}
	for i, s := range samples {
		p4("src", s)
		for _, m := range models {
			c := m.Convert(s)
			r, g, b, a := c.RGBA()
			mix(r, g, b, a)
			if i%5 == 0 {
				print(r, ",", g, ",", b, ",", a, " ")
			}
		}
		if i%5 == 0 {
			println()
		}
	}
	println("models", h)
	// the concrete type after conversion
	if c, ok := color.CMYKModel.Convert(color.RGBA{255, 128, 0, 255}).(color.CMYK); ok {
		println("cmyk", c.C, c.M, c.Y, c.K)
	}
	if c, ok := color.YCbCrModel.Convert(color.NRGBA{255, 128, 0, 128}).(color.YCbCr); ok {
		println("ycbcr", c.Y, c.Cb, c.Cr)
	}
	if c, ok := color.NYCbCrAModel.Convert(color.RGBA{100, 50, 25, 128}).(color.NYCbCrA); ok {
		println("nycbcra", c.Y, c.Cb, c.Cr, c.A)
	}
	if c, ok := color.NRGBAModel.Convert(color.RGBA{100, 50, 25, 128}).(color.NRGBA); ok {
		println("nrgba", c.R, c.G, c.B, c.A)
	}
	if c, ok := color.GrayModel.Convert(color.RGBA{255, 0, 0, 255}).(color.Gray); ok {
		println("gray", c.Y)
	}

	// RGB <-> YCbCr sweep
	h = 14695981039346656037
	maxErr := 0
	for r := 0; r < 256; r += 3 {
		for g := 0; g < 256; g += 5 {
			for b := 0; b < 256; b += 7 {
				y, cb, cr := color.RGBToYCbCr(uint8(r), uint8(g), uint8(b))
				r2, g2, b2 := color.YCbCrToRGB(y, cb, cr)
				mix(uint32(y)<<16|uint32(cb)<<8|uint32(cr), uint32(r2)<<16|uint32(g2)<<8|uint32(b2))
				for _, d := range []int{int(r2) - r, int(g2) - g, int(b2) - b} {
					if d < 0 {
						d = -d
					}
					if d > maxErr {
						maxErr = d
					}
				}
				c, m, yy, k := color.RGBToCMYK(uint8(r), uint8(g), uint8(b))
				r3, g3, b3 := color.CMYKToRGB(c, m, yy, k)
				mix(uint32(c)<<24|uint32(m)<<16|uint32(yy)<<8|uint32(k), uint32(r3)<<16|uint32(g3)<<8|uint32(b3))
			}
		}
	}
	println("ycc sweep", h, maxErr)
	// full YCbCr cube corner sweep (out-of-gamut clamping)
	h = 14695981039346656037
	for y := 0; y < 256; y += 15 {
		for cb := 0; cb < 256; cb += 15 {
			for cr := 0; cr < 256; cr += 15 {
				r, g, b := color.YCbCrToRGB(uint8(y), uint8(cb), uint8(cr))
				mix(uint32(r)<<16 | uint32(g)<<8 | uint32(b))
				rr, gg, bb, aa := color.YCbCr{uint8(y), uint8(cb), uint8(cr)}.RGBA()
				mix(rr, gg, bb, aa)
				nr, ng, nb, na := color.NYCbCrA{color.YCbCr{uint8(y), uint8(cb), uint8(cr)}, uint8(y ^ cb)}.RGBA()
				mix(nr, ng, nb, na)
			}
		}
	}
	println("ycc cube", h)
	// NRGBA alpha sweep
	h = 14695981039346656037
	for a := 0; a < 256; a++ {
		c := color.NRGBA{uint8(a * 7), uint8(255 - a), uint8(a * 13), uint8(a)}
		r, g, b, al := c.RGBA()
		mix(r, g, b, al)
		back := color.NRGBAModel.Convert(color.RGBAModel.Convert(c)).(color.NRGBA)
		mix(uint32(back.R), uint32(back.G), uint32(back.B), uint32(back.A))
		g16 := color.Gray16Model.Convert(c).(color.Gray16)
		mix(uint32(g16.Y))
	}
	println("alpha sweep", h)
	pal := color.Palette{color.Black, color.White, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}, color.Transparent}
	for _, c := range []color.Color{color.RGBA{200, 10, 10, 255}, color.Gray{100}, color.Gray{200}, color.NRGBA{0, 0, 255, 10}, color.CMYK{255, 0, 255, 0}} {
		print(pal.Index(c), " ")
	}
	println()
	p4("palconv", pal.Convert(color.YCbCr{100, 200, 50}))
	var empty color.Palette
	println(empty.Index(color.Black))
}
