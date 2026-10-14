package main

import "encoding/json"

// A multi-result call in return position desugars like the assignment
// case: hoist to a temp, return the unpacked fields packed into the
// function's pair.
func marshalIt(x int) ([]byte, error) {
	return json.Marshal(x)
}

// Named results in a multi-result function are defined as locals so
// assignments to them work; the return packs them into the pair.
func namedResults() (sec, nsec int64) {
	sec = int64(123)
	nsec = int64(456)
	return sec, nsec
}

func main() {
	b, err := marshalIt(42)
	if err != nil {
		println("err")
		return
	}
	println("marshal:", string(b))
	s, n := namedResults()
	println("named:", s, n)
}
