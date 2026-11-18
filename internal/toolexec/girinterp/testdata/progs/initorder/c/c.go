package c

var C = mk()

func mk() string { println("c var"); return "c" }

func init() { println("c init") }
