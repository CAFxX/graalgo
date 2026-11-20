// Targets the unrecovered panic printout of a value of a named string type
// (no Error/String methods): gc prints the type-qualified form
// `panic: main.State("half\nopen")` with escaped content; exit 2.
package main

type State string

func (s State) next() State {
	switch s {
	case "closed":
		return "open"
	case "open":
		return "half\nopen"
	}
	return s
}

func main() {
	s := State("closed")
	for i := 0; i < 2; i++ {
		s = s.next()
		println("state len", len(s))
	}
	panic(s)
}
