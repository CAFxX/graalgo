package main

import (
	"errors"
	"sort"
)

// er06: combination — errors + sort in one program. Collect failures as
// errors, Join them, sort their messages, binary-search the sorted
// messages. Exercises both synthetic packages together.

func failOp(name string, fail bool) error {
	if fail {
		return errors.New("op " + name + " failed")
	}
	return nil
}

func main() {
	var errs []error
	ops := []string{"alpha", "beta", "gamma", "delta"}
	fails := []bool{true, false, true, true}
	for i, name := range ops {
		if err := failOp(name, fails[i]); err != nil {
			errs = append(errs, err)
		}
	}
	joined := errors.Join(errs...)
	println("[" + joined.Error() + "]")
	// Sort the messages and search.
	msgs := []string{}
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	sort.Strings(msgs)
	for _, m := range msgs {
		println(m)
	}
	println(sort.StringsAreSorted(msgs)) // true
	i := sort.SearchStrings(msgs, "op gamma failed")
	println(i, msgs[i] == "op gamma failed")
	// The joined error Is-matches each member.
	println(errors.Is(joined, errs[0]))
	println(errors.Is(joined, errs[2]))
	println(errors.Is(joined, errors.New("op beta failed"))) // false: beta ok
}
