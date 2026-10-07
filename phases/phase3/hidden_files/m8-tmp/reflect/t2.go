package main
type I interface { M(int) string; m2() }
type T struct { A int }
func (t T) M(x int) string { return "" }
func (t *T) m2() {}
func main() {
	var m map[string][]*T
	var ch chan<- int
	var f func(int, ...string) (bool, error)
	var arr [4]T
	var i I
	var p *int
	_, _, _, _, _, _, _ = m, ch, f, arr, i, p, 0
}
