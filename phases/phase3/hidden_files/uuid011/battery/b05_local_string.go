package main

// Local named type with String(): regression guard (must keep working).
import "fmt"

type MyInt int

func (m MyInt) String() string { return fmt.Sprintf("MyInt(%d)", int(m)) }

type MyErr string

func (m MyErr) Error() string { return "boom:" + string(m) }

type MyPtr int

func (m *MyPtr) String() string { return fmt.Sprintf("MyPtr(%d)", int(*m)) }

type Stringer interface{ String() string }

func main() {
	var m MyInt = 42
	fmt.Println(m)
	var s Stringer = m
	fmt.Println(s)
	var e error = MyErr("x")
	fmt.Println(e)
	fmt.Printf("%T\n", m)
	var pp MyPtr = 7
	fmt.Println(&pp) // pointer receiver on pointer: want MyPtr(7)
	fmt.Println(pp)  // value, no String in method set: want 7
}
