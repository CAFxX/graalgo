package main

func main() {
	wasNil := true
	// probe
	defer func() {
		r := recover()
		wasNil = r == nil
	}()
	panic(nil)
	_ = wasNil
}

// observe: wasNil
