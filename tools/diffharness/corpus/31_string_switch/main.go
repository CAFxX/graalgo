package main

import "fmt"

func grade(s string) string {
	switch s {
	case "a", "A":
		return "excellent"
	case "b":
		return "good"
	case "c", "d":
		return "pass"
	case "":
		return "empty"
	default:
		return "fail"
	}
}

func main() {
	for _, g := range []string{"a", "B", "c", "", "z"} {
		fmt.Println(g, grade(g))
	}
}
