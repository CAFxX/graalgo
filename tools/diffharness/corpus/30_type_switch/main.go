package main

import "fmt"

func describe(v any) string {
	switch t := v.(type) {
	case nil:
		return "nil"
	case int:
		return fmt.Sprintf("int %d", t)
	case string:
		return fmt.Sprintf("string %q", t)
	case []int:
		return fmt.Sprintf("slice len %d", len(t))
	case fmt.Stringer:
		return "stringer: " + t.String()
	default:
		return fmt.Sprintf("other %T", t)
	}
}

func main() {
	fmt.Println(describe(nil))
	fmt.Println(describe(7))
	fmt.Println(describe("hi"))
	fmt.Println(describe([]int{1, 2}))
	fmt.Println(describe(1.5))
}
