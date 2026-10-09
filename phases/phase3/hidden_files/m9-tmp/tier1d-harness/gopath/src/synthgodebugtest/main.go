package main

import (
	"fmt"
	"synthgodebug"
)

// SCOPE-LIMITED + NOGC: external programs cannot import internal/* (go
// tool restriction). No synthetic adapter exists: the package needs the
// generated internal/godebugs table plus runtime linknamed hooks
// (setUpdate/registerMetric/setNewIncNonDefault); there is no in-tree
// consumer. API-surface pin, verified by building against the real package
// (Value() and String() are not called: they panic for names not listed
// in the godebugs table, by design.)
func main() {
	s := godebug.New("testsetting")
	fmt.Println(s.Name())
	fmt.Println(s.Undocumented())
	s2 := godebug.New("#undocumented")
	fmt.Println(s2.Undocumented())
	fmt.Println(s2.Name())
	fmt.Println("ok")
}
