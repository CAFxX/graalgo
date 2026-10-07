package main
type T struct { A int; b string `json:"b"` }
type U struct { T; C float64 }
func main() { var x U; _ = x }
