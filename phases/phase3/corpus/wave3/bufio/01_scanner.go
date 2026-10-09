package main

import (
	"bytes"
	"fmt"
	"bufio"
)

func main() {
	// Scanner: ScanLines.
	input := "line1\nline2\nline3"
	s := bufio.NewScanner(bytes.NewReader([]byte(input)))
	for s.Scan() {
		fmt.Println(s.Text())
	}
	fmt.Println(s.Err())
	// Scanner: ScanWords.
	s2 := bufio.NewScanner(bytes.NewReader([]byte("a b c d")))
	s2.Split(bufio.ScanWords)
	for s2.Scan() {
		fmt.Println(s2.Text())
	}
	// Scanner: Bytes.
	s3 := bufio.NewScanner(bytes.NewReader([]byte("x\ny")))
	for s3.Scan() {
		fmt.Printf("%s\n", s3.Bytes())
	}
	fmt.Println("scanner ok")
}
