package main

import "fmt"

type Stringer interface{ String() string }

type MyInt int

func (m MyInt) String() string { return fmt.Sprint("int:", int(m)) }

type MyStr string

func (m MyStr) String() string { return "str:" + string(m) }

type MySlice []int

func (m MySlice) String() string { return fmt.Sprint("slice:", len(m)) }

func main() {
	vals := []Stringer{MyInt(7), MyStr("hi"), MySlice{1, 2, 3}}
	for _, v := range vals {
		fmt.Println(v.String())
	}
}
