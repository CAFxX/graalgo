// Targets struct embedding: value and pointer embedding, multi-level field
// and method promotion, shadowing at different depths, promoted methods
// through pointer embeds mutating shared state, and method values of
// promoted methods.
package main

type Base struct {
	ID   int
	Name string
}

func (b Base) Describe() string   { return "Base(" + b.Name + ")" }
func (b *Base) Rename(n string)   { b.Name = n }
func (b Base) Level() int         { return 0 }
func (b *Base) Bump()             { b.ID++ }
func (b Base) IDPlus(k int) int   { return b.ID + k }
func (b Base) Shadowed() string   { return "base-shadowed" }
func (b *Base) PtrShadow() string { return "base-ptr" }

type Mid struct {
	Base
	Name  string // shadows Base.Name
	Extra int
}

func (m Mid) Level() int       { return 1 }
func (m Mid) Shadowed() string { return "mid-shadowed:" + m.Name }

type Top struct {
	*Mid
	Tag int
}

func (t Top) Level() int { return 2 }

type Other struct {
	Count int
}

func (o *Other) Inc() int { o.Count++; return o.Count }

type Multi struct {
	Top
	*Other
	ID int // shadows Base.ID at depth 0
}

type Lvl interface{ Level() int }
type Desc interface{ Describe() string }

//go:noinline
func level(l Lvl) int { return l.Level() }

//go:noinline
func desc(d Desc) string { return d.Describe() }

var gm = &Mid{Base: Base{ID: 10, Name: "inner"}, Name: "outer", Extra: 3}

func main() {
	t := Top{Mid: gm, Tag: 7}
	println(t.Name, t.Base.Name, t.Mid.Name, t.ID, t.Extra, t.Tag)
	println(t.Describe(), t.Shadowed(), t.Base.Shadowed(), t.Level(), t.Mid.Level(), t.Base.Level())

	// mutate through promoted pointer-receiver methods: shared via *Mid
	t.Rename("renamed")
	t.Bump()
	t.Bump()
	println(gm.Base.Name, gm.ID, t.ID, t.Describe())

	// copy of Top shares *Mid
	t2 := t
	t2.Tag = 99
	t2.Extra = 42
	println(t.Tag, t.Extra, t2.Tag, gm.Extra)

	// copy of Mid does not share Base
	mcopy := *gm
	mcopy.Rename("copy")
	mcopy.Bump()
	println(mcopy.Base.Name, mcopy.ID, gm.Base.Name, gm.ID)

	m := Multi{Top: t, Other: &Other{Count: 5}, ID: -1}
	println(m.ID, m.Top.ID, m.Base.ID, m.Inc(), m.Inc(), m.Count, m.Level())
	println(m.IDPlus(100), m.Mid.IDPlus(1), m.PtrShadow())

	// interfaces satisfied through promotion
	println(level(t), level(*gm), level(gm.Base), level(m))
	println(desc(t), desc(gm), desc(m), desc(&m))

	// method values bound at evaluation time (value receiver copies)
	b := Base{ID: 1, Name: "x"}
	dv := b.Describe
	ip := b.IDPlus
	b.Name = "y"
	b.ID = 50
	println(dv(), ip(1), b.Describe(), b.IDPlus(1))

	// method value via promoted pointer path evaluates *Mid at bind time
	mv := t.IDPlus // copies Base value now
	gm.ID = 1000
	println(mv(0), t.IDPlus(0))

	// pointer-receiver method value binds pointer
	pb := &b
	bump := pb.Bump
	bump()
	bump()
	println(b.ID)

	// method expressions
	fe := Base.Describe
	fp := (*Base).Rename
	fp(&b, "viaexpr")
	println(fe(b), (*Base).Describe(&b), Mid.Level(*gm), Top.Level(t))
	fl := (*Mid).Level
	println(fl(gm))

	// embedded nil pointer: field access panics, but method on copy fine
	var tn Top
	println(tn.Tag, tn.Level())
	println(tryName(tn))
	println(tryBump(&tn))

	// slices of structs with embeds
	arr := []Mid{{Base: Base{ID: 1}}, {Base: Base{ID: 2}}, {Base: Base{ID: 3}}}
	for i := range arr {
		arr[i].Bump()
		arr[i].Name = strconvI(arr[i].ID)
	}
	for _, x := range arr {
		x.Bump() // copy
		println(x.ID, x.Name, x.Base.Name)
	}
	println(arr[0].ID, arr[1].ID, arr[2].ID)
}

func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func tryName(t Top) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "recovered: " + r.(interface{ Error() string }).Error()
		}
	}()
	return t.Name
}

func tryBump(t *Top) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "recovered: " + r.(interface{ Error() string }).Error()
		}
	}()
	t.Bump()
	return "no panic"
}
