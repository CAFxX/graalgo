package main

import "fmt"

type Node interface{ Sum() int }

type Leaf struct{ v int }

func (l Leaf) Sum() int { return l.v }

type Branch struct {
	left, right Node
}

func (b Branch) Sum() int { return b.left.Sum() + b.right.Sum() }

func main() {
	t := Branch{
		left:  Branch{left: Leaf{v: 1}, right: Leaf{v: 2}},
		right: Branch{left: Leaf{v: 3}, right: Leaf{v: 4}},
	}
	fmt.Println(t.Sum())
}
