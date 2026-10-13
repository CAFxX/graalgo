package main

import (
	"encoding/csv"
	"fmt"
)

func main() {
	// In-scope constructors: NewReaderString / NewBufferWriter.
	// (gc's NewReader/NewWriter take io.Reader/io.Writer; io is out of
	// M7 scope by design.)

	// Reader construction smoke test. NOTE: r.Read / r.ReadAll are
	// 2-target calls whose desugar was removed from mrdesugar_m5.go
	// (review §2) — they fail fast today. Re-test trigger: when the csv
	// desugar is re-implemented, extend this program with Read/ReadAll
	// differential coverage (quoted fields, embedded newlines, bare
	// quotes, field-count mismatch).
	r := csv.NewReaderString("a,b,c\n1,2,3\n")
	fmt.Println("InputOffset:", r.InputOffset())

	// Writer: basic Write + Flush + Bytes + Error.
	w := csv.NewBufferWriter()
	w.Write([]string{"a", "b", "c"})
	w.Write([]string{"1", "2", "3"})
	w.Flush()
	fmt.Printf("Write: %q err=%v\n", w.Bytes(), w.Error())

	// Writer: WriteAll in one shot.
	w2 := csv.NewBufferWriter()
	w2.WriteAll([][]string{{"x", "y"}, {"1", "2"}, {"3", "4"}})
	fmt.Printf("WriteAll: %q err=%v\n", w2.Bytes(), w2.Error())

	// Adversarial: fields needing quotes (comma, quote, newline),
	// empty fields.
	w3 := csv.NewBufferWriter()
	w3.Write([]string{"a,b", `c"d`, "e\nf", "", "g"})
	w3.Flush()
	fmt.Printf("Quoted: %q err=%v\n", w3.Bytes(), w3.Error())

	// Custom delimiter + CRLF line endings (exported fields).
	w4 := csv.NewBufferWriter()
	w4.Comma = ';'
	w4.UseCRLF = true
	w4.Write([]string{"a", "b"})
	w4.Write([]string{"1", "2"})
	w4.Flush()
	fmt.Printf("SemiCRLF: %q err=%v\n", w4.Bytes(), w4.Error())

	// Error is initially nil; empty writer emits nothing.
	w5 := csv.NewBufferWriter()
	fmt.Println("EmptyErrorNil:", w5.Error() == nil)
	fmt.Printf("Empty: %q\n", w5.Bytes())
}
