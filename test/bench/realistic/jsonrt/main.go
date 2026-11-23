// jsonrt: encoding/json round trips of an API-shaped payload (structs with
// nested slices, maps and pointers): Marshal, then Unmarshal into fresh
// values. Reflection-heavy (descriptor images, reflect's call path).
package main

import (
	"encoding/json"
	"strconv"

	"graalgo/bench/h"
)

type User struct {
	Login string `json:"login"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

type Item struct {
	ID     int               `json:"id"`
	Name   string            `json:"name"`
	Tags   []string          `json:"tags"`
	Price  float64           `json:"price"`
	Attrs  map[string]string `json:"attrs"`
	Active bool              `json:"active"`
	Owner  *User             `json:"owner,omitempty"`
}

var items []Item

func init() {
	seed := uint32(7)
	rnd := func() int { seed = seed*1664525 + 1013904223; return int(seed >> 8) }
	for i := 0; i < 400; i++ {
		it := Item{ID: i, Name: "item-" + strconv.Itoa(rnd()%100000), Price: float64(rnd()%100000) / 100,
			Active: rnd()%2 == 0, Attrs: map[string]string{}}
		for t := 0; t < 1+rnd()%5; t++ {
			it.Tags = append(it.Tags, "tag"+strconv.Itoa(rnd()%50))
		}
		for a := 0; a < rnd()%4; a++ {
			it.Attrs["k"+strconv.Itoa(a)] = "v" + strconv.Itoa(rnd()%1000)
		}
		if rnd()%3 != 0 {
			it.Owner = &User{Login: "u" + strconv.Itoa(rnd()%1000), Email: "x@example.com", Age: 18 + rnd()%60}
		}
		items = append(items, it)
	}
}

func work() int {
	sum := 0
	for r := 0; r < 4; r++ {
		b, err := json.Marshal(items)
		if err != nil {
			panic(err)
		}
		var back []Item
		if err := json.Unmarshal(b, &back); err != nil {
			panic(err)
		}
		sum += len(b)
		for i := range back {
			sum += back[i].ID + len(back[i].Tags) + len(back[i].Attrs)
			if back[i].Owner != nil {
				sum += back[i].Owner.Age
			}
		}
	}
	return sum
}

func main() { h.Run("jsonrt", 20, work) }
