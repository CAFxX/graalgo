package main

type Stringer interface {
	String() string
}

type myint int

func (m myint) String() string {
	if m == 1 {
		return "one"
	}
	return "many"
}

// Stringify calls a bound method on the type parameter T: the exporter
// devirtualizes v.String() via the method set of the substituted receiver.
func Stringify[T Stringer](s []T) string {
	r := ""
	for _, v := range s {
		r += v.String()
	}
	return r
}

func main() {
	a := 0
	if Stringify[myint]([]myint{1, 2}) == "onemany" {
		a = 1
	}
	b := 0
	if Stringify[myint]([]myint{2, 1, 2}) == "manyonemany" {
		b = 1
	}
	a = a
	b = b
}

// verifies: GG-0014
// observe: a b
