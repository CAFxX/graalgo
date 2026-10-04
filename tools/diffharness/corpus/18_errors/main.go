package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func lookup(m map[string]int, k string) (int, error) {
	v, ok := m[k]
	if !ok {
		return 0, fmt.Errorf("lookup %q: %w", k, ErrNotFound)
	}
	return v, nil
}

func main() {
	m := map[string]int{"a": 1}
	if v, err := lookup(m, "a"); err != nil {
		fmt.Println("err:", err)
	} else {
		fmt.Println("ok:", v)
	}
	_, err := lookup(m, "zzz")
	fmt.Println(errors.Is(err, ErrNotFound))
	var target *customErr
	fmt.Println(errors.As(err, &target))
	fmt.Println(errors.Unwrap(err))
}

type customErr struct{ msg string }

func (e *customErr) Error() string { return e.msg }
