package main

import (
    "fmt"
)

type GenS1 struct { F0 uint64; F1 int16 }
type GenS2 struct { F0 uint64; F1 uint8; F2 uint16 }
func genFn0(p0 uint16, p1 uint32, p2 int64) int {
    var v7 bool = true
    for fi8 := 0; fi8 < 11; fi8++ {
        v9 := []int{}
        _ = fi8
        _ = v9
    }
    _ = p0
    _ = p1
    _ = p2
    _ = v7
    return 42
}
func genFn1(p0 uint32) uint16 {
    var v10 uint = 255
    for fi11 := 0; fi11 < 4; fi11++ {
        p0 += p0
        p0 = 1
        var v12 string = "a"
        _ = fi11
        _ = v12
    }
    _ = p0
    _ = v10
    return 945
}

func main() {
    var v1 uint8 = 248
    var v2 string = "hello"
    var v3 int32 = 266
    var v4 uint32 = 317
    v5 := make(map[string]uint32)
    var v6 int8 = 1
    switch ("foo bar" + ("hello" + "foo bar")) {
    case "hello":
        fmt.Println(v1)
    case "":
        switch (("" + "\n\t") + (v2 + "")) {
        case "\n\t":
            var v13 int16 = -954
            _ = v13
        }
    case "\n\t":
        var v14 int64 = -1
        _ = v14
    default:
        var v15 uint32 = v4
        _ = v15
    }
    v16 := GenS1{F0: 807, F1: -1}
    var v17 uint16 = 255
    fmt.Println(v17, v3)
    if ((((1e10 * -2.25) / (1.5 + ((0.1 / 0.1) - 3.14159))) + 0.1) == 1e10) {
        v18 := GenS1{F0: 42, F1: 296}
        v19 := GenS1{F0: 2, F1: -32768}
        _ = v18
        _ = v19
    } else {
        for fi20 := 0; fi20 < 4; fi20++ {
            var v21 float32 = -2.25
            _ = fi20
            _ = v21
        }
        v2 = "foo bar"
        for fi22 := 0; fi22 < 1; fi22++ {
            v23 := GenS1{F0: 18446744073709551615, F1: -32768}
            var v24 int = fi22
            _ = fi22
            _ = v23
            _ = v24
        }
    }
    v25 := GenS1{F0: 42, F1: -1}
    fmt.Println(v2)
    v3 = v3
    v5[v2] = v4
    if true {
        var v26 uint32 = (v4 & (v4 ^ 127))
        if (true || (!true)) {
            v27 := []int{32767}
            _ = v27
        }
        _ = v26
    } else {
        _ = len(v5)
        var v28 int32 = (v3 * v3)
        _ = v28
    }
    v29 := GenS1{F0: 1, F1: 276}
    var v30 int32 = (v3 * v3)
    for fi31 := 0; fi31 < 14; fi31++ {
        if (genFn1(325) <= (v17 * (v17 | v17))) {
            fmt.Println(v2)
            v1 += 1
            delete(v5, v2)
        }
        _ = len(v5)
        v32 := GenS2{F0: 1, F1: 1, F2: v17}
        _ = fi31
        _ = v32
    }
    var v33 bool = false
    v34 := []int{-1, -316, 32767}
    var v35 bool = (((1.5 + 1e10) + float64(1e10)) != (0.0 / (0.1 * 1.5)))
    fmt.Println("v1:", v1)
    fmt.Println("v17:", v17)
    fmt.Println("v2:", v2)
    fmt.Println("v3:", v3)
    fmt.Println("v30:", v30)
    fmt.Println("v33:", v33)
    fmt.Println("slicelen v34:", len(v34), v34)
    fmt.Println("v35:", v35)
    fmt.Println("v4:", v4)
    fmt.Println("maplen v5:", len(v5))
    fmt.Println("v6:", v6)
    _ = v1
    _ = v16
    _ = v17
    _ = v2
    _ = v25
    _ = v29
    _ = v3
    _ = v30
    _ = v33
    _ = v34
    _ = v35
    _ = v4
    _ = v5
    _ = v6
}
