package main

func main() {
	var mx int64 = 9223372036854775807
	o1 := mx + 1
	var mn int64 = -9223372036854775808
	o2 := mn - 1
	o3 := mn / -1
	o4 := mx * 2
	s := o1 + o2 + o3 + o4
	var umx uint64 = 18446744073709551615
	o5 := umx + 1
	o6 := umx * umx
	t := o5 + o6
	s = s
	t = t
}

// observe: o1 o2 o3 o4 s o5 o6 t
