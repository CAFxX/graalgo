// Targets the unrecovered panic printout of non-string basic kinds: a named
// float type (gc prints it type-qualified, main.Celsius(...)), after a
// recovered chain containing complex, bool and uint values; exit 2.
package main

type Celsius float64

func conv(f float64) Celsius { return Celsius((f - 32) * 5 / 9) }

func main() {
	defer func() {
		r := recover()
		println("first recovered is complex:", r.(complex128) == complex(1.5, -2))
		defer func() {
			r2 := recover()
			println("second recovered bool:", r2.(bool))
			defer func() {
				r3 := recover()
				println("third recovered uint8:", r3.(uint8))
				panic(conv(99.5))
			}()
			panic(uint8(255))
		}()
		panic(true)
	}()
	panic(complex(1.5, -2))
}
