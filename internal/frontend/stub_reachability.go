package main

// P1-3 (panic-stub surface): the honest equivalent of gc's link error
// for unimplemented (bodyless, provider-less) declarations.
//
// gc fails the BUILD when a program references a function whose
// implementation is missing: an assembly body that is not there, or a
// //go:linkname target with no provider, is a link error — not a
// program that builds cleanly and dies at runtime. graalgo used to
// emit every such declaration as a panic stub (fields["unimplemented"])
// and fail only if the stub was actually invoked, so programs calling
// a stubbed function compiled cleanly and died at runtime with a
// message that blamed assembly even for non-assembly shapes.
//
// checkStubReachability runs at the end of ExportFiles, over the
// emitted wire program (the program graalgo actually runs), and fails
// the export when an unimplemented stub is referenced from reachable
// code. A stub that is not referenced from reachable code keeps the
// stub form: that is what keeps genuinely unprovidable-but-uninvoked
// declarations building — internal/cpu.cpuid is the standing example
// (its only caller is internal/cpu.doinit, reached only from
// internal/cpu.Initialize, which only gc's runtime calls; graalgo's
// synthetic runtime never calls it, so the stub is unreachable and
// the feature flags stay zero, forcing callers onto generic paths).
//
// Reachability, precisely:
//   - Roots are func main (by object id, so a method named main can
//     never be mistaken for the entry point) and every package-level
//     global initializer, including init() calls (emitted as
//     side-effect-only globals with Obj == -1).
//   - Edges are direct object references in a reachable body: ident,
//     methodref, and methodvalue nodes all carry the target's object
//     id, and funclit bodies are walked as part of their enclosing
//     body (a closure literal's code is linked when its enclosing
//     function is linked, whether or not the closure is invoked).
//   - Interface boxings are edges too, but only precisely: a boxing
//     site in reachable code (an explicit convert node, a vardecl or
//     assignment into an interface-typed variable, a package-level
//     global of interface type, or a return of interface type) whose
//     interface's method set contains a stub method's name, where the
//     boxed value's type carries that stub method in its method table,
//     counts as a reference (the backend builds the itab from that
//     method table, so dispatch through the interface can reach the
//     stub).
//
// Known approximations, documented rather than hidden:
//   - A stub method reached ONLY by passing a concrete value as an
//     interface argument at a call site is not flagged: resolving
//     callee signatures (including methodref receiver offsets and
//     variadic packing) from the wire format is not reliable enough
//     to fail builds on. Neither is a stub reached only via reflect
//     (MethodByName / Value.Call, which carry names, not object ids).
//     Such a stub still panics if invoked, but now with its accurate
//     reason (see exportLinknameDecl), never the old assembly blame.
//   - A blank-identifier interface assignment (var _ I = T{}) is NOT
//     flagged: the exporter elides the conversion entirely (the global
//     initializer is just the composite literal), so no itab is built
//     and the stub is genuinely unreachable in the emitted program.

import (
	"fmt"
)

// stubInfo describes one emitted unimplemented funcdecl.
type stubInfo struct {
	obj    int
	name   string // pkgpath.name, for diagnostics
	reason string
}

// checkStubReachability fails the export when any unimplemented stub
// in doc is referenced from code reachable from mainObj (func main)
// or from a package-level global initializer. mainObj is -1 when the
// exporter found no main (ExportFiles already fails that case first,
// so -1 only ever arises in direct unit tests of this function).
func checkStubReachability(doc *Doc, mainObj int) error {
	// Index funcdecls by object id.
	declByObj := make(map[int]*Node)
	declName := make(map[int]string)
	stubs := make(map[int]*stubInfo)
	for _, f := range doc.Funcs {
		if f == nil || f.Kind != "funcdecl" {
			continue
		}
		obj, ok := intField(f.Fields, "obj")
		if !ok {
			continue
		}
		declByObj[obj] = f
		name, _ := f.Fields["name"].(string)
		pkg, _ := f.Fields["pkgpath"].(string)
		declName[obj] = pkg + "." + name
		if unimp, _ := f.Fields["unimplemented"].(bool); unimp {
			reason, _ := f.Fields["unimplementedReason"].(string)
			stubs[obj] = &stubInfo{obj: obj, name: pkg + "." + name, reason: reason}
		}
	}
	if len(stubs) == 0 {
		return nil
	}

	// Adjacency: funcdecl object id -> referenced funcdecl object ids,
	// plus the display name of the referencing function (for the
	// error) and the interface-conversion edges.
	refs := make(map[int]map[int]bool)
	referrer := make(map[int]string) // stub obj -> first referencing func
	addEdge := func(from int, fromName string, to int) {
		if _, ok := declByObj[to]; !ok {
			return // not a funcdecl (a var, a param, ...): not an edge
		}
		if refs[from] == nil {
			refs[from] = make(map[int]bool)
		}
		if !refs[from][to] {
			refs[from][to] = true
			if _, ok := stubs[to]; ok && referrer[to] == "" {
				referrer[to] = fromName
			}
		}
	}
	// Iterate in document order so the reported referrer is
	// deterministic when several functions reference one stub.
	for _, f := range doc.Funcs {
		if f == nil || f.Kind != "funcdecl" {
			continue
		}
		obj, ok := intField(f.Fields, "obj")
		if !ok {
			continue
		}
		body, _ := f.Fields["body"].(*Node)
		if body == nil {
			continue // stub or bodiless: no outgoing edges
		}
		for _, to := range collectObjRefs(body) {
			addEdge(obj, declName[obj], to)
		}
		for _, to := range collectBoxingStubRefs(body, doc, funcdeclResultTypes(f, doc)) {
			addEdge(obj, declName[obj], to)
		}
	}

	// Roots: func main, and every global initializer.
	visited := make(map[int]bool)
	queue := []int{}
	if mainObj >= 0 {
		if _, ok := declByObj[mainObj]; ok {
			visited[mainObj] = true
			queue = append(queue, mainObj)
		}
	}
	for _, g := range doc.Globals {
		if g.Init == nil {
			continue
		}
		for _, to := range collectObjRefs(g.Init) {
			if _, ok := declByObj[to]; ok && !visited[to] {
				visited[to] = true
				queue = append(queue, to)
				if _, ok := stubs[to]; ok && referrer[to] == "" {
					referrer[to] = "<package initialization>"
				}
			}
		}
		for _, to := range collectBoxingStubRefs(g.Init, doc, nil) {
			if _, ok := declByObj[to]; ok && !visited[to] {
				visited[to] = true
				queue = append(queue, to)
				if _, ok := stubs[to]; ok && referrer[to] == "" {
					referrer[to] = "<package initialization>"
				}
			}
		}
		// The global's own declared type boxes its initializer: a
		// package-level `var i I = T{}` builds the itab for (T, I)
		// when the initializer runs. Blank globals (Obj == -1) are
		// excluded: their initializer is evaluated unboxed.
		if g.Obj >= 0 && g.Obj < len(doc.Objects) {
			if valueType, ok := intField(g.Init.Fields, "type"); ok {
				for _, to := range convertStubTargets(doc.Objects[g.Obj].Type, valueType, doc) {
					if _, ok := declByObj[to]; ok && !visited[to] {
						visited[to] = true
						queue = append(queue, to)
						if _, ok := stubs[to]; ok && referrer[to] == "" {
							referrer[to] = "<package initialization>"
						}
					}
				}
			}
		}
	}
	// Breadth-first traversal of the reachable call graph.
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for to := range refs[cur] {
			if !visited[to] {
				visited[to] = true
				queue = append(queue, to)
			}
		}
	}

	// Report the first reachable stub in document order (deterministic).
	for _, f := range doc.Funcs {
		if f == nil || f.Kind != "funcdecl" {
			continue
		}
		obj, ok := intField(f.Fields, "obj")
		if !ok {
			continue
		}
		stub, ok := stubs[obj]
		if !ok || !visited[obj] {
			continue
		}
		from := referrer[obj]
		if from == "" {
			from = "reachable code"
		}
		reason := stub.reason
		if reason == "" {
			reason = "no implementation is available in graalgo"
		}
		return fmt.Errorf("gofrontend: %s is unimplemented (%s) but is referenced from reachable code (%s); graalgo has no implementation for it — the equivalent of gc's link error for a missing body",
			stub.name, reason, from)
	}
	return nil
}

// intField reads an int-valued field (wire fields are ints pre-marshal;
// be liberal: a float64 survives a JSON round-trip in tests).
func intField(fields map[string]any, key string) (int, bool) {
	switch v := fields[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// collectObjRefs returns every object id referenced by "obj" fields
// anywhere in the node tree (ident, methodref, methodvalue, and the
// init-call idents). Var/param ids are returned too; the caller
// intersects with funcdecl ids.
func collectObjRefs(n *Node) []int {
	var out []int
	walkNode(n, func(fields map[string]any) {
		if obj, ok := intField(fields, "obj"); ok {
			out = append(out, obj)
		}
	})
	return out
}

// collectBoxingStubRefs returns the object ids of stub methods that
// an interface boxing in the tree can dispatch to. A boxing site pairs
// a value type with an interface type; a stub method in the value
// type's method table whose name is in the interface's method set
// counts (the backend builds the itab from that method table, so
// dispatch through the interface can reach the stub). Boxing sites:
//   - convert nodes (explicit conversions): target type + the
//     converted value's own type (the "type" field of the "x" child);
//   - vardecl nodes: each declared variable's type (from the object
//     table entry for its obj id) + its initializer's type;
//   - assign nodes: each identifier target's declared type + the
//     corresponding right-hand side's type (positional; skipped when
//     the arities differ, e.g. multi-valued calls);
//   - return nodes: each returned value's type + the corresponding
//     result type of the enclosing function (resultTypes, or the
//     funclit's own results when walking inside a funclit).
//
// Conversions to the empty interface, or to an interface not
// containing the stub method, do not count. Call arguments are NOT
// covered: passing a concrete value to an interface parameter boxes
// it, but resolving callee signatures (including methodref receiver
// offsets and variadic packing) from the wire format is not reliable
// enough to fail builds on — that shape stays a documented
// approximation (see the file header).
func collectBoxingStubRefs(n *Node, doc *Doc, resultTypes []int) []int {
	var out []int
	seen := make(map[int]bool)
	add := func(ifaceType, valueType int) {
		for _, stubObj := range convertStubTargets(ifaceType, valueType, doc) {
			if !seen[stubObj] {
				seen[stubObj] = true
				out = append(out, stubObj)
			}
		}
	}
	walkBoxing(n, doc, resultTypes, add)
	return out
}

// funcdeclResultTypes returns the result type ids of a funcdecl's
// signature (the pair-transformed signature for multi-result funcs,
// which pairs positionally with the transformed return values).
func funcdeclResultTypes(f *Node, doc *Doc) []int {
	sigID, ok := intField(f.Fields, "sig")
	if !ok {
		return nil
	}
	ent := typeEntry(doc, sigID)
	if ent == nil || ent.Kind != "signature" {
		return nil
	}
	var out []int
	results, ok := ent.Detail["results"].([]any)
	if !ok {
		return nil
	}
	for _, r := range results {
		if rm, ok := r.(map[string]any); ok {
			if tid, ok := intField(rm, "type"); ok {
				out = append(out, tid)
			}
		}
	}
	return out
}

// walkBoxing visits the tree, calling add for every (interface type,
// value type) boxing pair it finds. resultTypes is the enclosing
// function's result types (for return nodes); funclit nodes replace
// it with their own results while their body is walked.
func walkBoxing(n *Node, doc *Doc, resultTypes []int, add func(ifaceType, valueType int)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "convert":
		if target, ok := intField(n.Fields, "type"); ok {
			if x, ok := n.Fields["x"].(*Node); ok && x != nil {
				if source, ok := intField(x.Fields, "type"); ok {
					add(target, source)
				}
			}
		}
	case "vardecl":
		objs := intListField(n.Fields, "objs")
		values, _ := n.Fields["values"].([]*Node)
		for i, value := range values {
			if i >= len(objs) || value == nil {
				continue
			}
			if objs[i] < 0 || objs[i] >= len(doc.Objects) {
				continue
			}
			if valueType, ok := intField(value.Fields, "type"); ok {
				add(doc.Objects[objs[i]].Type, valueType)
			}
		}
	case "assign":
		lhs, _ := n.Fields["lhs"].([]*Node)
		rhs, _ := n.Fields["rhs"].([]*Node)
		if len(lhs) == len(rhs) {
			for i, l := range lhs {
				if l == nil || l.Kind != "ident" {
					continue
				}
				obj, ok := intField(l.Fields, "obj")
				if !ok || obj < 0 || obj >= len(doc.Objects) {
					continue
				}
				if rhs[i] == nil {
					continue
				}
				if valueType, ok := intField(rhs[i].Fields, "type"); ok {
					add(doc.Objects[obj].Type, valueType)
				}
			}
		}
	case "return":
		// Return nodes carry their values under "results".
		if vals, ok := n.Fields["results"].([]*Node); ok && len(vals) == len(resultTypes) {
			for i, v := range vals {
				if v == nil {
					continue
				}
				if valueType, ok := intField(v.Fields, "type"); ok {
					add(resultTypes[i], valueType)
				}
			}
		}
	case "funclit":
		// The funclit's own results replace the enclosing function's
		// while its body is walked (a bare return inside the literal
		// returns from the literal, not the outer function).
		results, _ := n.Fields["results"].([]*Node)
		var litResults []int
		for _, r := range results {
			if r == nil {
				continue
			}
			if tid, ok := intField(r.Fields, "type"); ok {
				litResults = append(litResults, tid)
			}
		}
		resultTypes = litResults
	}
	for _, v := range n.Fields {
		walkBoxingValue(v, doc, resultTypes, add)
	}
}

func walkBoxingValue(v any, doc *Doc, resultTypes []int, add func(ifaceType, valueType int)) {
	switch t := v.(type) {
	case *Node:
		walkBoxing(t, doc, resultTypes, add)
	case []*Node:
		for _, c := range t {
			walkBoxing(c, doc, resultTypes, add)
		}
	case []any:
		for _, c := range t {
			walkBoxingValue(c, doc, resultTypes, add)
		}
	case map[string]any:
		for _, c := range t {
			walkBoxingValue(c, doc, resultTypes, add)
		}
	}
}

// intListField reads an int-list field (objs is []int pre-marshal).
func intListField(fields map[string]any, key string) []int {
	switch v := fields[key].(type) {
	case []int:
		return v
	case []any:
		var out []int
		for _, e := range v {
			switch n := e.(type) {
			case int:
				out = append(out, n)
			case float64:
				out = append(out, int(n))
			}
		}
		return out
	default:
		return nil
	}
}

// walkNode visits every node's fields map in the tree (depth-first).
func walkNode(n *Node, fn func(map[string]any)) {
	if n == nil {
		return
	}
	fn(n.Fields)
	walkValue(n.Fields, fn)
}

// walkValue recurses into arbitrary wire values.
func walkValue(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case *Node:
		walkNode(t, fn)
	case []*Node:
		for _, c := range t {
			walkNode(c, fn)
		}
	case []any:
		for _, c := range t {
			walkValue(c, fn)
		}
	case map[string]any:
		for _, c := range t {
			walkValue(c, fn)
		}
	}
}

// convertStubTargets resolves a single conversion (target interface
// type id, source value type id) to the stub method object ids it can
// dispatch to.
func convertStubTargets(targetType, sourceType int, doc *Doc) []int {
	if targetType == 0 || sourceType == 0 {
		return nil
	}
	ifaceMethods := interfaceMethodNames(targetType, doc, make(map[int]bool), 0)
	if len(ifaceMethods) == 0 {
		return nil
	}
	var out []int
	for _, ent := range resolveTypeChain(sourceType, doc) {
		methods, ok := ent["methods"].([]any)
		if !ok {
			continue
		}
		for _, m := range methods {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			name, _ := mm["name"].(string)
			if !ifaceMethods[name] {
				continue
			}
			obj, ok := intField(mm, "obj")
			if !ok || obj < 0 {
				continue
			}
			out = append(out, obj)
		}
	}
	return out
}

// interfaceMethodNames returns the method-name set of the interface
// denoted by type id (following named/alias indirection and embedded
// interfaces), or an empty set when it is not an interface.
func interfaceMethodNames(typeID int, doc *Doc, visited map[int]bool, depth int) map[string]bool {
	names := make(map[string]bool)
	if depth > 16 || visited[typeID] {
		return names
	}
	visited[typeID] = true
	ent := typeEntry(doc, typeID)
	if ent == nil {
		return names
	}
	switch ent.Kind {
	case "interface":
		for _, m := range detailMethods(ent) {
			if name, ok := m["name"].(string); ok {
				names[name] = true
			}
		}
		for _, emb := range detailTypeIDs(ent, "embedded") {
			for n := range interfaceMethodNames(emb, doc, visited, depth+1) {
				names[n] = true
			}
		}
	case "named":
		if under, ok := detailTypeID(ent, "underlying"); ok {
			for n := range interfaceMethodNames(under, doc, visited, depth+1) {
				names[n] = true
			}
		}
	case "alias":
		if rhs, ok := detailTypeID(ent, "rhs"); ok {
			for n := range interfaceMethodNames(rhs, doc, visited, depth+1) {
				names[n] = true
			}
		}
	}
	return names
}

// resolveTypeChain follows named/pointer/alias indirection for a
// source type, returning every type-table entry on the chain that
// carries a method table (named and pointer entries do).
func resolveTypeChain(typeID int, doc *Doc) []map[string]any {
	var out []map[string]any
	visited := make(map[int]bool)
	for depth := 0; depth < 16; depth++ {
		if visited[typeID] {
			break
		}
		visited[typeID] = true
		ent := typeEntry(doc, typeID)
		if ent == nil {
			break
		}
		if _, ok := ent.Detail["methods"]; ok {
			out = append(out, ent.Detail)
		}
		next := 0
		switch ent.Kind {
		case "named":
			next, _ = detailTypeID(ent, "underlying")
		case "pointer":
			next, _ = detailTypeID(ent, "elem")
		case "alias":
			next, _ = detailTypeID(ent, "rhs")
		}
		if next == 0 {
			break
		}
		// A named entry's underlying struct has no method table; the
		// pointer entry's elem (the named type) does, so keep walking
		// one step, but stop at non-method-table kinds afterwards.
		typeID = next
		if ent.Kind == "named" {
			// The named entry itself was already collected above; its
			// underlying is a struct/interface, not a method table.
			break
		}
	}
	return out
}

func typeEntry(doc *Doc, id int) *TypeEnt {
	if id < 0 || id >= len(doc.Types) {
		return nil
	}
	return &doc.Types[id]
}

func detailMethods(ent *TypeEnt) []map[string]any {
	var out []map[string]any
	methods, ok := ent.Detail["methods"].([]any)
	if !ok {
		return out
	}
	for _, m := range methods {
		if mm, ok := m.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

func detailTypeID(ent *TypeEnt, key string) (int, bool) {
	return intField(ent.Detail, key)
}

func detailTypeIDs(ent *TypeEnt, key string) []int {
	var out []int
	ids, ok := ent.Detail[key].([]any)
	if !ok {
		return out
	}
	for _, id := range ids {
		switch v := id.(type) {
		case int:
			out = append(out, v)
		case float64:
			out = append(out, int(v))
		}
	}
	return out
}
