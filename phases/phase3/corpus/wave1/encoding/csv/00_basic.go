package main

import (
	"encoding/csv"
	"fmt"
	"strings"
)

func main() {
	// Basic Read
	r := csv.NewReader(strings.NewReader("a,b,c\n1,2,3\n"))
	rec, err := r.Read()
	fmt.Printf("Read1: %q err=%v\n", rec, err)
	rec, err = r.Read()
	fmt.Printf("Read2: %q err=%v\n", rec, err)
	_, err = r.Read()
	fmt.Printf("Read3 (EOF): err=%v\n", err)

	// ReadAll
	r2 := csv.NewReader(strings.NewReader("x,y\n1,2\n3,4\n"))
	all, err := r2.ReadAll()
	fmt.Printf("ReadAll: %q err=%v\n", all, err)

	// Writer
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	w.Write([]string{"a", "b", "c"})
	w.Write([]string{"1", "2", "3"})
	w.Flush()
	fmt.Printf("Write: %q err=%v\n", sb.String(), w.Error())

	// Adversarial: quoted fields, embedded newlines
	r3 := csv.NewReader(strings.NewReader("\"a\nb\",\"c\"\"d\",e\n"))
	rec, err = r3.Read()
	fmt.Printf("Quoted: %q err=%v\n", rec, err)
}
