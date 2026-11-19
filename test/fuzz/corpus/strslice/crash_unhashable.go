// Ends with an UNRECOVERED panic (exit 2): inserting an interface key whose
// dynamic type is unhashable, reached through nested calls with deferred
// prints that must run (in LIFO order) before the crash message is printed.
package main

import (
	"strconv"
	"strings"
)

type keyed struct {
	id   int
	name string
	data []byte
}

var registry = map[any]int{}

func register(k any, v int) {
	defer println("register defer", v)
	registry[k] = v
}

func stage(n int) {
	defer println("stage defer", n)
	if n == 0 {
		register(keyed{id: 1, name: "x", data: []byte("boom")}, 99)
		return
	}
	stage(n - 1)
}

func main() {
	keys := []any{1, "one", 1.5, [2]string{"a", "b"}, struct{ a, b int }{1, 2}, true, int8(-1), 'r'}
	for i, k := range keys {
		register(k, i)
	}
	println("registered", len(registry))
	total := 0
	for _, v := range registry {
		total += v
	}
	println("total", total)
	// recovered variant first, so both paths execute
	func() {
		defer func() {
			r := recover()
			println("recovered:", r.(interface{ Error() string }).Error())
		}()
		register([]string{"x"}, -1)
	}()
	println("len after recovered", len(registry))
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		sb.WriteString(strconv.Itoa(i * i))
		sb.WriteByte(';')
	}
	println("pre-crash", sb.String())
	defer println("main defer runs before crash")
	stage(3)
	println("unreachable")
}
