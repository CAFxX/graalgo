package main
import "reflect"
type T struct { A int; b string }
func main() {
	t := reflect.TypeOf(T{})
	println(t.String())
	v := reflect.ValueOf(42)
	println(v.Int())
}
