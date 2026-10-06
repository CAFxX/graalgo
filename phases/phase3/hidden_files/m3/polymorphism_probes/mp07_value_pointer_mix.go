package main

import "fmt"

type Counter interface{ Next() int }

type C struct{ n int }

func (c C) Next() int { return c.n + 1 }

type P struct{ n int }

func (p *P) Next() int { p.n++; return p.n }

func main() {
	var c Counter
	total := 0
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			c = C{n: i}
		} else {
			c = &P{n: i}
		}
		total += c.Next()
	}
	fmt.Println(total)
}
