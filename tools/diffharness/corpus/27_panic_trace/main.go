package main

func level3() { panic("boom") }
func level2() { level3() }
func level1() { level2() }

func main() { level1() }
