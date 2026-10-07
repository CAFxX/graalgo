package reflectlite

import "reflect"

type Type = reflect.Type
type Value = reflect.Value

func TypeOf(i any) Type { return reflect.TypeOf(i) }

func ValueOf(i any) Value { return reflect.ValueOf(i) }

func Swapper(slice any) func(i, j int) { return reflect.Swapper(slice) }
