package main

import (
	"container/list"
	"container/ring"
	"image/color"
	"image/color/palette"
)

func main() {
	l := list.New()
	e4 := l.PushBack(4)
	e1 := l.PushFront(1)
	l.InsertBefore(3, e4)
	l.InsertAfter(2, e1)
	l.MoveToFront(e4)
	for e := l.Front(); e != nil; e = e.Next() {
		print(e.Value.(int), " ")
	}
	println(l.Len())
	l2 := list.New()
	l2.PushBack("x")
	l2.PushBackList(l)
	l.Remove(e1)
	for e := l2.Back(); e != nil; e = e.Prev() {
		switch v := e.Value.(type) {
		case int:
			print(v, " ")
		case string:
			print(v, " ")
		}
	}
	println(l2.Len(), l.Len())
	r := ring.New(5)
	for i := 0; i < r.Len(); i++ {
		r.Value = i * i
		r = r.Next()
	}
	sum := 0
	r.Do(func(v any) { sum += v.(int) })
	r2 := r.Move(2).Unlink(2)
	println(sum, r.Len(), r2.Len(), r.Value.(int), r.Prev().Value.(int))

	c := color.RGBA{200, 100, 50, 255}
	rr, g, b, a := c.RGBA()
	println(rr, g, b, a)
	gray := color.GrayModel.Convert(c).(color.Gray)
	y, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
	println(gray.Y, y, cb, cr)
	n := color.NRGBAModel.Convert(color.RGBA{100, 50, 25, 128}).(color.NRGBA)
	println(n.R, n.G, n.B, n.A)
	cc, mm, yy, kk := color.RGBToCMYK(10, 200, 30)
	println(cc, mm, yy, kk, len(palette.Plan9), len(palette.WebSafe))
	p := color.Palette(palette.WebSafe)
	println(p.Index(c), p.Index(color.Black), p.Index(color.White))
	var col color.Color = p.Convert(color.RGBA{250, 5, 130, 255})
	r3, g3, b3, _ := col.RGBA()
	println(r3>>8, g3>>8, b3>>8, color.Opaque.A, color.Gray16Model.Convert(color.White).(color.Gray16).Y)
}
