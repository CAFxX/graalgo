package main

import (
	"encoding/json"
	"fmt"
)

type Item struct {
	Name  string         `json:"name"`
	Price float64        `json:"price"`
	Tags  []string       `json:"tags,omitempty"`
	Meta  map[string]int `json:"meta"`
}

func main() {
	it := Item{Name: "widget", Price: 9.99, Meta: map[string]int{"b": 2, "a": 1}}
	data, err := json.Marshal(it)
	fmt.Println(string(data), err)
	var back Item
	fmt.Println(json.Unmarshal(data, &back), back.Name, back.Meta["a"])
	fmt.Println(json.Marshal(map[string]int{"z": 1, "m": 2})) // keys sorted
}
