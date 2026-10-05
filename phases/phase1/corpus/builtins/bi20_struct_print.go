package main

// Struct %v printing with mixed longs-carried and objects-carried fields.
//
// Regression test (M8 closeout): Fmt.structFieldBoxed passed the carrier
// index (position within longs[]/objects[]) to GoStruct.getLong/getObject,
// which expect the field index (position in the descriptor's field list).
// Any mixed struct (e.g. {int, string}) crashed printing with
// "field A is not an objects-carried field". Pure-longs and pure-objects
// structs passed by accident (carrier index == field index).
import "fmt"

type mixedPair struct {
	A int
	B string
}

type allLongs struct {
	X int
	Y int64
}

type allObjs struct {
	S string
	T string
}

type nestedMixed struct {
	M mixedPair
	N int
}

func main() {
	// The exact crash: mixed struct, direct %v.
	v := mixedPair{128, "a"}
	fmt.Println(v)

	// Via interface boxing (the fuzzer's original repro): boxing must not
	// corrupt the variable, and the boxed value must print.
	v.A = 999
	var x any = v
	fmt.Println(x)
	v.A = 111
	fmt.Println(v.A)
	fmt.Println(v)

	// Pure-longs and pure-objects structs (previously passed by accident).
	fmt.Println(allLongs{1, 2})
	fmt.Println(allObjs{"s", "t"})

	// Nested mixed struct.
	fmt.Println(nestedMixed{mixedPair{7, "seven"}, 8})

	// %+v and %#v spellings.
	fmt.Printf("%+v\n", v)
	fmt.Printf("%#v\n", v)
}
