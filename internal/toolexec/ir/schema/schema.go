// Package schema parses the GIR schema (../schema.txt) and generates the
// Go and Java codecs from it. It is the generator behind `go generate
// ./ir`; TestGeneratedUpToDate fails when a checked-in generated file
// differs from what the schema produces.
package schema

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Schema is a parsed schema.txt.
type Schema struct {
	Version  int
	MaxDepth int
	Hash     [8]byte // first 8 bytes of SHA-256 over the normalized schema
	Enums    []*Enum
	Records  []*Record
	Unions   []*Union
	Sections []*Section
	byName   map[string]any
}

type Enum struct {
	Name   string
	Values []EnumValue
}

type EnumValue struct {
	Value   int
	Name    string
	Comment string
}

type Record struct {
	Name   string
	Scope  bool
	Fields []*Field
}

type Union struct {
	Name    string
	Members []*Member
}

type Member struct {
	Kind   int
	Name   string
	Fields []*Field
}

type Section struct {
	ID    int
	Name  string // wire name (STRINGS, PKG, ...)
	Field string // field name in the File struct
	Raw   bool
	Type  *Type // nil when Raw
}

type Field struct {
	Name string
	Type *Type
}

// TypeKind classifies a field type.
type TypeKind int

const (
	Prim      TypeKind = iota // uint int bool bytes str
	Ref                       // type pos sym obj fn local label
	OptRef                    // sym? obj? fn? local? label?
	EnumT                     // enum
	RecordT                   // record inline
	OptRecord                 // ?record
	UnionT                    // union
	OptUnion                  // ?union
	List                      // []T
)

type Type struct {
	Kind TypeKind
	Name string // prim/ref name, enum/record/union name
	Elem *Type  // List
}

var prims = map[string]bool{"uint": true, "int": true, "bool": true, "bytes": true, "str": true}
var refs = map[string]bool{"type": true, "pos": true, "sym": true, "obj": true, "fn": true, "local": true, "label": true}

// RefSections maps the file-level reference kinds to the section whose
// list they index.
var RefSections = map[string]string{"type": "TYPES", "sym": "SYMREFS", "obj": "OBJECTS", "fn": "FUNCINDEX"}

// Parse parses schema source.
func Parse(src []byte) (*Schema, error) {
	s := &Schema{byName: map[string]any{}}
	var norm strings.Builder
	var curEnum *Enum
	var curUnion *Union
	sc := bufio.NewScanner(strings.NewReader(string(src)))
	sc.Buffer(nil, 1<<20)
	lineNo := 0
	fail := func(format string, a ...any) error {
		return fmt.Errorf("schema.txt:%d: %s", lineNo, fmt.Sprintf(format, a...))
	}
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		comment := ""
		if i := strings.Index(raw, "#"); i >= 0 {
			comment = strings.TrimSpace(raw[i+1:])
			raw = raw[:i]
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indented := raw[0] == ' ' || raw[0] == '\t'
		f := strings.Fields(raw)
		norm.WriteString(strings.Join(f, " "))
		norm.WriteByte('\n')
		if indented {
			kind, err := strconv.Atoi(f[0])
			if err != nil || len(f) < 2 {
				return nil, fail("want KIND NAME")
			}
			switch {
			case curEnum != nil:
				if len(f) != 2 {
					return nil, fail("enum value takes no fields")
				}
				curEnum.Values = append(curEnum.Values, EnumValue{kind, f[1], comment})
			case curUnion != nil:
				m := &Member{Kind: kind, Name: f[1]}
				for _, fs := range f[2:] {
					fld, err := parseField(fs)
					if err != nil {
						return nil, fail("%v", err)
					}
					m.Fields = append(m.Fields, fld)
				}
				curUnion.Members = append(curUnion.Members, m)
			default:
				return nil, fail("indented line outside enum/union")
			}
			continue
		}
		curEnum, curUnion = nil, nil
		switch f[0] {
		case "version":
			v, err := strconv.Atoi(f[1])
			if err != nil {
				return nil, fail("bad version")
			}
			s.Version = v
		case "maxdepth":
			v, err := strconv.Atoi(f[1])
			if err != nil || v < 1 {
				return nil, fail("bad maxdepth")
			}
			s.MaxDepth = v
		case "enum":
			curEnum = &Enum{Name: f[1]}
			s.Enums = append(s.Enums, curEnum)
			if err := s.define(f[1], curEnum); err != nil {
				return nil, fail("%v", err)
			}
		case "union":
			curUnion = &Union{Name: f[1]}
			s.Unions = append(s.Unions, curUnion)
			if err := s.define(f[1], curUnion); err != nil {
				return nil, fail("%v", err)
			}
		case "record":
			r := &Record{Name: f[1]}
			rest := f[2:]
			if len(rest) > 0 && rest[0] == "scope" {
				r.Scope, rest = true, rest[1:]
			}
			for _, fs := range rest {
				fld, err := parseField(fs)
				if err != nil {
					return nil, fail("%v", err)
				}
				r.Fields = append(r.Fields, fld)
			}
			s.Records = append(s.Records, r)
			if err := s.define(f[1], r); err != nil {
				return nil, fail("%v", err)
			}
		case "section":
			if len(f) != 5 {
				return nil, fail("want: section ID NAME field TYPE|raw")
			}
			id, err := strconv.Atoi(f[1])
			if err != nil {
				return nil, fail("bad section id")
			}
			sec := &Section{ID: id, Name: f[2], Field: f[3]}
			if f[4] == "raw" {
				sec.Raw = true
			} else if sec.Type, err = parseType(f[4]); err != nil {
				return nil, fail("%v", err)
			}
			s.Sections = append(s.Sections, sec)
		default:
			return nil, fail("unknown declaration %q", f[0])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(norm.String()))
	copy(s.Hash[:], sum[:8])
	return s, s.check()
}

func (s *Schema) define(name string, v any) error {
	if _, dup := s.byName[name]; dup {
		return fmt.Errorf("%s declared twice", name)
	}
	s.byName[name] = v
	return nil
}

func parseField(fs string) (*Field, error) {
	name, typ, ok := strings.Cut(fs, ":")
	if !ok || name == "" {
		return nil, fmt.Errorf("field %q: want name:type", fs)
	}
	t, err := parseType(typ)
	if err != nil {
		return nil, fmt.Errorf("field %q: %v", fs, err)
	}
	return &Field{Name: name, Type: t}, nil
}

func parseType(t string) (*Type, error) {
	switch {
	case strings.HasPrefix(t, "[]"):
		e, err := parseType(t[2:])
		if err != nil {
			return nil, err
		}
		return &Type{Kind: List, Elem: e}, nil
	case prims[t]:
		return &Type{Kind: Prim, Name: t}, nil
	case refs[t]:
		return &Type{Kind: Ref, Name: t}, nil
	case strings.HasSuffix(t, "?") && refs[strings.TrimSuffix(t, "?")]:
		n := strings.TrimSuffix(t, "?")
		if n == "type" || n == "pos" {
			return nil, fmt.Errorf("%s is already optional (0 = none)", t)
		}
		return &Type{Kind: OptRef, Name: n}, nil
	case strings.HasPrefix(t, "?"):
		return &Type{Kind: OptRecord, Name: t[1:]}, nil // resolved in check
	case t != "" && unicode.IsUpper(rune(t[0])):
		return &Type{Kind: RecordT, Name: t}, nil // resolved in check
	}
	return nil, fmt.Errorf("unknown type %q", t)
}

// check resolves named types and validates the schema.
func (s *Schema) check() error {
	var resolve func(t *Type) error
	resolve = func(t *Type) error {
		switch t.Kind {
		case List:
			return resolve(t.Elem)
		case RecordT, OptRecord:
			switch s.byName[t.Name].(type) {
			case *Record:
			case *Union:
				if t.Kind == RecordT {
					t.Kind = UnionT
				} else {
					t.Kind = OptUnion
				}
			case *Enum:
				if t.Kind == OptRecord {
					return fmt.Errorf("optional enum %s", t.Name)
				}
				t.Kind = EnumT
			default:
				return fmt.Errorf("undefined type %s", t.Name)
			}
		}
		return nil
	}
	for _, r := range s.Records {
		for _, f := range r.Fields {
			if err := resolve(f.Type); err != nil {
				return fmt.Errorf("record %s: %v", r.Name, err)
			}
		}
	}
	for _, u := range s.Unions {
		seen := map[int]bool{}
		for _, m := range u.Members {
			if m.Kind < 1 || seen[m.Kind] {
				return fmt.Errorf("union %s: bad or duplicate kind %d", u.Name, m.Kind)
			}
			seen[m.Kind] = true
			for _, f := range m.Fields {
				if f.Name == "unionTag" {
					return fmt.Errorf("union %s.%s: field name unionTag is reserved", u.Name, m.Name)
				}
				if err := resolve(f.Type); err != nil {
					return fmt.Errorf("union %s.%s: %v", u.Name, m.Name, err)
				}
			}
		}
	}
	for _, e := range s.Enums {
		seen := map[int]bool{}
		for _, v := range e.Values {
			if v.Value < 1 || seen[v.Value] {
				return fmt.Errorf("enum %s: bad or duplicate value %d", e.Name, v.Value)
			}
			seen[v.Value] = true
		}
	}
	last := 0
	secs := map[string]*Section{}
	for _, sec := range s.Sections {
		if sec.ID <= last {
			return fmt.Errorf("section %s: ids must ascend", sec.Name)
		}
		last = sec.ID
		secs[sec.Name] = sec
		if sec.Type != nil {
			if err := resolve(sec.Type); err != nil {
				return fmt.Errorf("section %s: %v", sec.Name, err)
			}
		}
	}
	for _, name := range []string{"STRINGS", "BODIES", "DATA"} {
		if sec := secs[name]; sec == nil || !sec.Raw {
			return fmt.Errorf("raw section %s missing", name)
		}
	}
	for ref, name := range RefSections {
		if sec := secs[name]; sec == nil || sec.Type == nil || sec.Type.Kind != List {
			return fmt.Errorf("section %s (target of %s refs) must be a list", name, ref)
		}
	}
	if s.MaxDepth == 0 {
		return fmt.Errorf("maxdepth missing")
	}
	if _, ok := s.byName["FuncBody"].(*Record); !ok {
		return fmt.Errorf("record FuncBody (the BODIES blob) missing")
	}
	return nil
}

// Record returns the named record.
func (s *Schema) Record(name string) *Record { r, _ := s.byName[name].(*Record); return r }

// Section returns the named section.
func (s *Schema) Section(name string) *Section {
	for _, sec := range s.Sections {
		if sec.Name == name {
			return sec
		}
	}
	return nil
}

// exported converts a schema identifier to an exported Go identifier.
func exported(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

// upperSnake converts CamelCase to UPPER_SNAKE for Java constants.
func upperSnake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(rune(name[i-1])) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}
