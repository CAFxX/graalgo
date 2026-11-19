// Targets recover() visibility rules: recover only works when called
// directly by a deferred function; recover in a helper called from the
// deferred func returns nil; recover outside panicking returns nil;
// recover twice; recover in nested defer frames; deferred method recover.
package main

func helperRecover() any { return recover() }

func viaHelper() (got string) {
	defer func() {
		if r := recover(); r != nil {
			got += "|outer:" + r.(string)
		}
	}()
	defer func() {
		r := helperRecover()
		if r == nil {
			got += "helper-nil"
		} else {
			got += "helper-got"
		}
	}()
	panic("p1")
}

func directDeferRecover() (got string) {
	defer func() {
		got = "after"
	}()
	defer recoverDirect(&got)
	panic("p2")
}

func recoverDirect(s *string) {
	r := recover()
	if r != nil {
		*s = "direct:" + r.(string)
	}
}

func recoverTwice() (a, b bool) {
	defer func() {
		a = recover() != nil
		b = recover() != nil
	}()
	panic("p3")
}

func recoverNilOutside() bool {
	r := recover()
	defer func() {}()
	return r == nil
}

type T struct{ msg string }

func (t *T) rec() {
	if r := recover(); r != nil {
		t.msg = "method:" + r.(string)
	}
}

func methodRecover() string {
	t := &T{}
	func() {
		defer t.rec()
		panic("p4")
	}()
	return t.msg
}

func nestedFuncLit() (s string) {
	defer func() {
		func() {
			if r := recover(); r != nil {
				s = "nested-got"
			} else {
				s = "nested-nil"
			}
		}()
		if r := recover(); r != nil {
			s += "|outer-got:" + r.(string)
		}
	}()
	panic("p5")
}

func deferRecoverBuiltin() (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "outer-recovered:" + r.(string)
		} else {
			s = "outer-nil"
		}
	}()
	defer recover()
	panic("p6")
}

func main() {
	println(viaHelper())
	println(directDeferRecover())
	a, b := recoverTwice()
	println("twice", a, b)
	println("recoverNilOutside", recoverNilOutside())
	println(methodRecover())
	println(nestedFuncLit())
	println(deferRecoverBuiltin())
}
