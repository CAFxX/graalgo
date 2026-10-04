package main

import (
	"fmt"
	"reflect"
)

type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  bool   `json:"tls,omitempty"`
}

func main() {
	t := reflect.TypeOf(Config{})
	fmt.Println(t.Name(), t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fmt.Println(f.Name, f.Type, f.Tag.Get("json"))
	}
	v := reflect.ValueOf(&Config{Host: "x", Port: 80})
	fmt.Println(v.Elem().FieldByName("Port").Int())
	fmt.Println(reflect.TypeOf(42).Kind())
}
