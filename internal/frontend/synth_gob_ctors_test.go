package main

import "testing"

// P1-2 (review-1105-gob-ctors): encoding/gob's canonical constructors.
// io landed in P3-M8 but NewEncoder/NewDecoder were never added, so any
// program written against gc's gob API failed at export with
// "undefined: gob.NewEncoder". The stream constructors must export with
// gc's exact signatures, and the M7 buffer constructors must keep
// working alongside them. (Behavioral coverage of the streaming design
// — per-Encode flush, message-unit reads, write/read error surfacing —
// lives in the lane's gc-level run of the extracted synthetic source;
// the frontend suite pins the export surface here.)
func TestSyntheticGobCtors(t *testing.T) {
	src := "package main\n" +
		"import (\n\"bytes\"\n\"encoding/gob\"\n)\n" +
		"func main() {\n" +
		"var buf bytes.Buffer\n" +
		"enc := gob.NewEncoder(&buf)\n" +
		"_ = enc\n" +
		"dec := gob.NewDecoder(&buf)\n" +
		"_ = dec\n" +
		"enc2 := gob.NewBufferEncoder()\n" +
		"_ = enc2\n" +
		"dec2 := gob.NewBufferDecoder(nil)\n" +
		"_ = dec2\n" +
		"}\n"
	out := exportEnccoreSrc(t, src)
	names := enccoreFuncdecls(t, out)
	for _, want := range []string{"NewEncoder", "NewDecoder", "NewBufferEncoder", "NewBufferDecoder", "Encode", "Decode"} {
		if !names[want] {
			t.Errorf("missing funcdecl %q", want)
		}
	}
}

// TestSyntheticGobCtorsStreamCalls pins that a program actually calling
// Encode/Decode through the stream constructors exports: this drives
// the new synth bodies (writeFull/writeMsg flush, fill/readMsg refill,
// including their 2-target Read/Write call sites) through the exporter.
func TestSyntheticGobCtorsStreamCalls(t *testing.T) {
	src := "package main\n" +
		"import (\n\"bytes\"\n\"encoding/gob\"\n\"fmt\"\n)\n" +
		"func main() {\n" +
		"var buf bytes.Buffer\n" +
		"enc := gob.NewEncoder(&buf)\n" +
		"if err := enc.Encode(42); err != nil {\n" +
		"panic(err)\n" +
		"}\n" +
		"dec := gob.NewDecoder(&buf)\n" +
		"var i int\n" +
		"if err := dec.Decode(&i); err != nil {\n" +
		"panic(err)\n" +
		"}\n" +
		"fmt.Println(i)\n" +
		"}\n"
	out := exportEnccoreSrc(t, src)
	names := enccoreFuncdecls(t, out)
	for _, want := range []string{"NewEncoder", "NewDecoder", "Encode", "Decode"} {
		if !names[want] {
			t.Errorf("missing funcdecl %q", want)
		}
	}
}
