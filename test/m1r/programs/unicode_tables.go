package main

import "unicode"

func main() {
	for _, r := range "aZ9 _\tÄßπЖ中🎉́ ·ǅ٣" {
		println(r, unicode.IsLetter(r), unicode.IsUpper(r), unicode.IsLower(r), unicode.IsDigit(r), unicode.IsNumber(r),
			unicode.IsSpace(r), unicode.IsPunct(r), unicode.IsSymbol(r), unicode.IsMark(r), unicode.IsControl(r),
			unicode.IsPrint(r), unicode.IsGraphic(r), unicode.IsTitle(r),
			unicode.ToUpper(r), unicode.ToLower(r), unicode.ToTitle(r), unicode.SimpleFold(r))
	}
	println(unicode.Is(unicode.Han, '中'), unicode.Is(unicode.Greek, 'π'), unicode.In('Ж', unicode.Latin, unicode.Cyrillic),
		unicode.IsOneOf([]*unicode.RangeTable{unicode.Letter, unicode.Digit}, '7'))
	println(unicode.TurkishCase.ToUpper('i'), unicode.TurkishCase.ToLower('I'), unicode.To(unicode.UpperCase, 'ß'))
	n := 0
	for name, t := range unicode.Scripts {
		if unicode.Is(t, 'a') {
			println("script of a:", name)
		}
		n += len(t.R16) + len(t.R32)
	}
	println(len(unicode.Scripts), len(unicode.Categories), len(unicode.Properties), n)
	println(unicode.MaxRune, unicode.ReplacementChar, unicode.Version)
}
