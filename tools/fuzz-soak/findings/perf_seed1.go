package main

import (
    "fmt"
)

type GenS1 struct { F0 uint8; F1 uint32; F2 string }
type GenS2 struct { F0 int; F1 int32; F2 string }
type GenS3 struct { F0 bool; F1 int8 }
type GenS4 struct { F0 bool; F1 bool }
type GenS5 struct { F0 int8 }
type GenS6 struct { F0 bool; F1 int16; F2 uint64 }
type GenS7 struct { F0 uint8 }
func genFn0() uint32 {
    v5 := []int{42, -148, 9223372036854775807, 9223372036854775807}
    v6 := GenS1{F0: 42, F1: 2147483648, F2: ("hello" + "\n\t")}
    var v7 int16 = 0
    _ = v5
    _ = v6
    _ = v7
    return 629
}
func genFn1(p0 string, p1 int16, p2 int16) uint32 {
    v8 := GenS1{F0: 180, F1: 0, F2: "\n\t"}
    v9 := []int{-15, -154, 2147483647, -2147483648}
    var v10 int32 = 632
    _ = p0
    _ = p1
    _ = p2
    _ = v10
    _ = v8
    _ = v9
    return genFn0()
}

func main() {
    var v1 string = (("" + "foo bar") + "")
    var v2 int64 = 846
    v3 := []int{}
    var v4 int = 32767
    var v11 bool = true
    v3 = append(v3, (v4 % ((v4) | 1)))
    switch (((v1 + ((((("\n\t" + v1) + ((v1 + (v1 + v1)) + (("\n\t" + (v1 + ((((v1 + v1) + "foo bar") + (v1 + v1)) + "a"))) + v1))) + ("\n\t" + v1)) + v1) + "hello")) + v1) + v1) {
    case "hello":
        fmt.Println(v1, v11, v4)
    case "":
        v3 = append(v3, v4)
    default:
        v3 = append(v3, (-(v4)))
    }
    if v11 {
        v11 = v11
        var v12 int = 32767
        v1 = v1
        _ = v12
    } else {
        if len(v3) >= 2 {
            v3 = v3[1:len(v3)]
        }
    }
    v2 += v2
    var v13 string = "a"
    if v11 {
        fmt.Println(v2, v4)
        var v14 uint64 = 1
        if true {
            for fi15 := 0; fi15 < 10; fi15++ {
                if (177 < 2) {
                    v14 += v14
                    if len(v3) > 0 {
                        v3[0] = v4
                    }
                }
                switch v13 {
                case "hello":
                    for fi16 := 0; fi16 < 18; fi16++ {
                        var v17 bool = v11
                        v3 = append(v3, fi15)
                        _ = fi16
                        _ = v17
                    }
                case "foo bar":
                    for fi18 := 0; fi18 < 6; fi18++ {
                        var v19 uint16 = 255
                        fmt.Println(v11, v11)
                        v14 -= (^(v14))
                        _ = fi18
                        _ = v19
                    }
                case "\n\t":
                    for fi20 := 0; fi20 < 9; fi20++ {
                        var v21 int8 = 100
                        if len(v3) >= 2 {
                            v3 = v3[1:len(v3)]
                        }
                        switch "foo bar" {
                        case "":
                            fmt.Println(v21, fi15, v4)
                        case "a":
                            v2 = (v2 & (v2 + (^(v2))))
                        case "\n\t":
                            fmt.Println(v2, fi20, v11)
                        }
                        _ = fi20
                        _ = v21
                    }
                default:
                    if len(v3) >= 2 {
                        v3 = v3[1:len(v3)]
                    }
                }
                _ = fi15
            }
        } else {
            v22 := GenS1{F0: 2, F1: 4, F2: v1}
            for fi23 := 0; fi23 < 20; fi23++ {
                fmt.Println(v2)
                _ = fi23
            }
            v13 = v13
            _ = v22
        }
        _ = v14
    }
    if v11 {
        if v11 {
            v1 = (("a" + v13) + "hello")
        } else {
            switch v13 {
            case "foo bar":
                if true {
                    for fi24 := 0; fi24 < 17; fi24++ {
                        v25 := GenS1{F0: 1, F1: 2147483648, F2: ("" + "hello")}
                        for fi26 := 0; fi26 < 14; fi26++ {
                            for fi27 := 0; fi27 < 13; fi27++ {
                                fmt.Println(v1)
                                v4 -= fi26
                                _ = fi27
                            }
                            if (4294967295 == 2) {
                                var v28 float32 = float32(1.5)
                                v4 += 349
                                v13 = ("\n\t" + "a")
                                _ = v28
                            } else {
                                var v29 string = "a"
                                v13 = v29
                                _ = v29
                            }
                            if v11 {
                                var v30 float64 = 1e10
                                _ = v30
                            } else {
                                var v31 int64 = (v2 * v2)
                                switch v13 {
                                case "":
                                    v32 := GenS2{F0: 974, F1: -1, F2: ""}
                                    _ = v32
                                case "a":
                                    if (1000 != -324) {
                                        for fi33 := 0; fi33 < 7; fi33++ {
                                            var v34 string = v13
                                            _ = fi33
                                            _ = v34
                                        }
                                        fmt.Println(v11, fi26)
                                    } else {
                                        var v35 int = fi26
                                        for fi36 := 0; fi36 < 19; fi36++ {
                                            v1 = ""
                                            for fi37 := 0; fi37 < 17; fi37++ {
                                                var v38 int64 = (v2 / (((v2 % (((v31 % (((v2 + 9223372036854775807)) | 1))) | 1))) | 1))
                                                _ = fi37
                                                _ = v38
                                            }
                                            _ = fi36
                                        }
                                        v39 := GenS2{F0: (^(v4)), F1: 540, F2: v1}
                                        _ = v35
                                        _ = v39
                                    }
                                default:
                                    v40 := GenS1{F0: 255, F1: 41, F2: v1}
                                    _ = v40
                                }
                                fmt.Println(v31, v1, v13)
                                _ = v31
                            }
                            _ = fi26
                        }
                        _ = fi24
                        _ = v25
                    }
                    var v41 string = v13
                    _ = v41
                } else {
                    v2 += 42
                    fmt.Println(v13, v2, v4)
                }
            }
            if v11 {
                if len(v3) >= 2 {
                    v3 = v3[1:len(v3)]
                }
                if len(v3) >= 2 {
                    v3 = v3[1:len(v3)]
                }
                var v42 uint = 255
                _ = v42
            }
        }
        switch ("\n\t" + v13) {
        case "\n\t":
            var v43 int64 = 670
            _ = v43
        case "":
            v44 := GenS1{F0: 255, F1: 764, F2: "hello"}
            _ = v44
        default:
            if (v11 && v11) {
                for fi45 := 0; fi45 < 4; fi45++ {
                    switch v4 {
                    case -555:
                        switch (fi45 >> ((435) & 63)) {
                        case 2147483647:
                            v46 := GenS3{F0: v11, F1: 42}
                            _ = v46
                        case 32768:
                            var v47 uint64 = 0
                            _ = v47
                        default:
                            v48 := GenS4{F0: (v11 || ((v11 || v11) || (!(0 < 42)))), F1: false}
                            _ = v48
                        }
                    }
                    if ((944 < 32767) && (972 <= 2)) {
                        v49 := GenS5{F0: 0}
                        _ = v49
                    } else {
                        var v50 int = 125
                        var v51 uint8 = 52
                        if len(v3) >= 2 {
                            v3 = v3[1:len(v3)]
                        }
                        _ = v50
                        _ = v51
                    }
                    switch 386 {
                    case 32767:
                        var v52 uint16 = 42
                        _ = v52
                    case 32768:
                        var v53 uint8 = 2
                        _ = v53
                    case 2147483647:
                        v11 = false
                    }
                    _ = fi45
                }
                var v54 string = "hello"
                var v55 float32 = 0.0
                _ = v54
                _ = v55
            } else {
                v2 -= v2
                for fi56 := 0; fi56 < 20; fi56++ {
                    for fi57 := 0; fi57 < 20; fi57++ {
                        var v58 uint8 = 2
                        v2 -= int64(v58)
                        if len(v3) > 0 {
                            v3[0] = fi57
                        }
                        _ = fi57
                        _ = v58
                    }
                    _ = fi56
                }
            }
        }
    } else {
        v11 = (1000 >= 0)
    }
    v2 += (v2 + (v2 / ((v2) | 1)))
    var v59 uint = 2
    switch v4 {
    case 127:
        for fi60 := 0; fi60 < 12; fi60++ {
            var v61 uint8 = 0
            _ = fi60
            _ = v61
        }
    case 128:
        v62 := GenS6{F0: (42 >= 26), F1: 1, F2: 281}
        _ = v62
    }
    v4 += (v4 - 2147483647)
    fmt.Println(v13, v59, v13)
    var v63 int64 = (v2 - v2)
    v4 = -170
    var v64 uint64 = 18446744073709551615
    v3 = append(v3, (^(v4)))
    if (v11 || (851 == 255)) {
        for fi65 := 0; fi65 < 19; fi65++ {
            v64 = (^(v64))
            for fi66 := 0; fi66 < 12; fi66++ {
                var v67 int = (fi65 & 32767)
                _ = fi66
                _ = v67
            }
            if v11 {
                for fi68 := 0; fi68 < 5; fi68++ {
                    v2 = (^(v2))
                    _ = fi68
                }
                v69 := GenS7{F0: 1}
                var v70 uint = (v59 / (((v59 / ((4294967295) | 1))) | 1))
                _ = v69
                _ = v70
            } else {
                switch (fi65 << ((v59) & 63)) {
                case 0:
                    v71 := GenS6{F0: (!(42 <= 255)), F1: 0, F2: 18446744073709551615}
                    _ = v71
                case -2147483648:
                    var v72 uint32 = 4294967295
                    _ = v72
                }
            }
            _ = fi65
        }
        _ = len(v3)
    }
    fmt.Println("v1:", v1)
    fmt.Println("v11:", v11)
    fmt.Println("v13:", v13)
    fmt.Println("v2:", v2)
    fmt.Println("slicelen v3:", len(v3), v3)
    fmt.Println("v4:", v4)
    fmt.Println("v59:", v59)
    fmt.Println("v63:", v63)
    fmt.Println("v64:", v64)
    _ = v1
    _ = v11
    _ = v13
    _ = v2
    _ = v3
    _ = v4
    _ = v59
    _ = v63
    _ = v64
}
