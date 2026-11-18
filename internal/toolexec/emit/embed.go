package emit

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"graalgo-toolexec/ir"
)

// EmbedConfig is cmd/compile's -embedcfg file: pattern -> matched files
// (slash-separated, relative to the package directory), file -> path on
// disk.
type EmbedConfig struct {
	Patterns map[string][]string
	Files    map[string]string
}

// embedVar is one //go:embed variable.
type embedVar struct {
	v        *types.Var
	pos      token.Pos
	patterns []string
}

// scanEmbeds collects the //go:embed directives of package-level variable
// declarations (the directive lines immediately preceding the spec, as
// cmd/compile attaches pragmas).
func (e *emitter) scanEmbeds() []embedVar {
	var out []embedVar
	for _, f := range e.cfg.Files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				doc := vs.Doc
				if doc == nil && !gd.Lparen.IsValid() {
					doc = gd.Doc
				}
				if doc == nil {
					continue
				}
				var patterns []string
				for _, c := range doc.List {
					text, ok := strings.CutPrefix(c.Text, "//go:embed")
					if !ok || text != "" && !unicode.IsSpace(rune(text[0])) {
						continue
					}
					args, err := parseGoEmbed(text)
					if err != nil {
						e.unsupportedf(c.Pos(), "%v", err)
					}
					patterns = append(patterns, args...)
				}
				if len(patterns) == 0 {
					continue
				}
				if len(vs.Names) != 1 || len(vs.Values) != 0 {
					e.unsupportedf(vs.Pos(), "go:embed on a declaration gc rejects")
				}
				v, _ := e.cfg.Info.Defs[vs.Names[0]].(*types.Var)
				if v == nil {
					continue
				}
				out = append(out, embedVar{v: v, pos: vs.Names[0].Pos(), patterns: patterns})
			}
		}
	}
	return out
}

// parseGoEmbed is cmd/compile's (noder) argument parser.
func parseGoEmbed(args string) ([]string, error) {
	var list []string
	for args = strings.TrimSpace(args); args != ""; args = strings.TrimSpace(args) {
		var p string
	Switch:
		switch args[0] {
		default:
			i := len(args)
			for j, c := range args {
				if unicode.IsSpace(c) {
					i = j
					break
				}
			}
			p, args = args[:i], args[i:]
		case '`':
			i := strings.Index(args[1:], "`")
			if i < 0 {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
			p, args = args[1:1+i], args[1+i+1:]
		case '"':
			i := 1
			for ; i < len(args); i++ {
				if args[i] == '\\' {
					i++
					continue
				}
				if args[i] == '"' {
					q, err := strconv.Unquote(args[:i+1])
					if err != nil {
						return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args[:i+1])
					}
					p, args = q, args[i+1:]
					break Switch
				}
			}
			if i >= len(args) {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
		}
		if args != "" {
			if r, _ := utf8.DecodeRuneInString(args); !unicode.IsSpace(r) {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
		}
		list = append(list, p)
	}
	return list, nil
}

const (
	embedString = iota
	embedBytes
	embedFiles
)

// embedKind classifies the variable's type as cmd/compile's embedKind.
func embedKind(t types.Type) (int, bool) {
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "embed" && n.Obj().Name() == "FS" {
		return embedFiles, true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.String {
			return embedString, true
		}
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
			return embedBytes, true
		}
	}
	return 0, false
}

// embedFileList is cmd/compile's (staticdata) file list: the matched files
// (and, for embed.FS, every parent directory as "dir/"), sorted by
// (directory, element).
func embedFileList(cfg *EmbedConfig, patterns []string, kind int) ([]string, error) {
	have := map[string]bool{}
	var list []string
	for _, pattern := range patterns {
		files, ok := cfg.Patterns[pattern]
		if !ok {
			return nil, fmt.Errorf("invalid go:embed: build system did not map pattern: %s", pattern)
		}
		for _, file := range files {
			if cfg.Files[file] == "" {
				return nil, fmt.Errorf("invalid go:embed: build system did not map file: %s", file)
			}
			if !have[file] {
				have[file] = true
				list = append(list, file)
			}
			if kind == embedFiles {
				for dir := path.Dir(file); dir != "." && !have[dir]; dir = path.Dir(dir) {
					have[dir] = true
					list = append(list, dir+"/")
				}
			}
		}
	}
	sort.Slice(list, func(i, j int) bool { return embedFileLess(list[i], list[j]) })
	if kind != embedFiles && len(list) > 1 {
		return nil, fmt.Errorf("invalid go:embed: multiple files for type string or []byte")
	}
	return list, nil
}

func embedFileNameSplit(name string) (dir, elem string) {
	name, _ = strings.CutSuffix(name, "/")
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return ".", name
	}
	return name[:i], name[i+1:]
}

func embedFileLess(x, y string) bool {
	xdir, xelem := embedFileNameSplit(x)
	ydir, yelem := embedFileNameSplit(y)
	return xdir < ydir || xdir == ydir && xelem < yelem
}

// embedHash is the 16-byte content hash gc stores in embed.FS entries
// (cmd/internal/hash: Sum32 for files up to 1 KiB, New32 above).
func embedHash(data []byte) [16]byte {
	var sum [32]byte
	if len(data) <= 1024 {
		sum = sha256.Sum256(data)
		sum[0] ^= 0xff
	} else {
		h := sha256.New()
		h.Write([]byte{1})
		h.Write(data)
		copy(sum[:], h.Sum(nil))
	}
	var out [16]byte
	copy(out[:], sum[:])
	return out
}

// dataRef appends the contents of file (once per file) to DATA.
func (e *emitter) dataRef(file string) (off, n uint64, data []byte, err error) {
	if r, ok := e.dataOff[file]; ok {
		return r[0], r[1], e.file.Data[r[0] : r[0]+r[1]], nil
	}
	data, err = os.ReadFile(file)
	if err != nil {
		return 0, 0, nil, err
	}
	off, n = uint64(len(e.file.Data)), uint64(len(data))
	e.file.Data = append(e.file.Data, data...)
	e.dataOff[file] = [2]uint64{off, n}
	return off, n, data, nil
}

// embedInits returns the initializers of the //go:embed variables: they
// run first in init·vars (gc initializes them statically, before any
// initializer code).
func (fs *fnState) embedInits(vars []embedVar) []ir.Stmt {
	e := fs.e
	var out []ir.Stmt
	for _, ev := range vars {
		if e.cfg.Embed == nil {
			e.unsupportedf(ev.pos, "go:embed without an -embedcfg")
		}
		kind, ok := embedKind(ev.v.Type())
		if !ok {
			e.unsupportedf(ev.pos, "go:embed cannot apply to var of type %s", ev.v.Type())
		}
		list, err := embedFileList(e.cfg.Embed, ev.patterns, kind)
		if err != nil {
			e.unsupportedf(ev.pos, "%v", err)
		}
		pos := fs.posAt(ev.pos, len(ev.v.Name()))
		strT := types.Typ[types.String]
		var rhs ir.Expr
		switch kind {
		case embedString, embedBytes:
			var data ir.Expr = &ir.ConstExpr{Type: e.typeID(strT), Pos: pos, Value: ir.ConstValue{Kind: ir.ConstKindString}}
			if len(list) == 1 {
				off, n, _, err := e.dataRef(e.cfg.Embed.Files[list[0]])
				if err != nil {
					e.unsupportedf(ev.pos, "go:embed: %v", err)
				}
				data = &ir.DataExpr{Type: e.typeID(strT), Pos: pos, Off: off, Len: n}
			}
			if kind == embedString {
				if !types.Identical(ev.v.Type(), strT) {
					data = &ir.ConvertExpr{Type: e.typeID(ev.v.Type()), Pos: pos, X: data}
				}
				rhs = data
			} else {
				rhs = &ir.ConvertExpr{Type: e.typeID(ev.v.Type()), Pos: pos, X: data}
			}
		case embedFiles:
			rhs = fs.embedFS(ev, list, pos)
		}
		out = append(out, &ir.AssignStmt{Pos: pos, Op: ir.AssignOpPlain, Lhs: []ir.Expr{fs.globalRef(ev.v, ev.pos)}, Rhs: []ir.Expr{rhs}})
	}
	return out
}

// embedFS builds embed.FS{files: &[]file{{name, data, hash}, ...}} with
// embed's own (unexported) struct types, as gc lays out its static data.
func (fs *fnState) embedFS(ev embedVar, list []string, pos ir.PosID) ir.Expr {
	e := fs.e
	fsT := ev.v.Type()
	filesPtr := fsT.Underlying().(*types.Struct).Field(0).Type() // *[]file
	filesSlice := filesPtr.(*types.Pointer).Elem()               // []file
	fileT := filesSlice.(*types.Slice).Elem()                    // file
	hashT := fileT.Underlying().(*types.Struct).Field(2).Type()  // [16]byte
	byteT := hashT.Underlying().(*types.Array).Elem()
	strT := types.Typ[types.String]
	lit := &ir.ArrayLitExpr{Type: e.typeID(filesSlice), Pos: pos}
	for i, name := range list {
		ent := &ir.StructLitExpr{Type: e.typeID(fileT), Pos: pos, Fields: []ir.FieldInit{
			{Path: []uint64{0}, Value: &ir.ConstExpr{Type: e.typeID(strT), Pos: pos, Value: ir.ConstValue{Kind: ir.ConstKindString, Text: name}}},
		}}
		if !strings.HasSuffix(name, "/") {
			off, n, data, err := e.dataRef(e.cfg.Embed.Files[name])
			if err != nil {
				e.unsupportedf(ev.pos, "go:embed: %v", err)
			}
			hash := embedHash(data)
			h := &ir.ArrayLitExpr{Type: e.typeID(hashT), Pos: pos}
			for k, b := range hash {
				h.Elems = append(h.Elems, ir.Elem{Index: uint64(k), Value: &ir.ConstExpr{Type: e.typeID(byteT), Pos: pos, Value: ir.ConstValue{Kind: ir.ConstKindInt, Text: strconv.Itoa(int(b))}}})
			}
			ent.Fields = append(ent.Fields,
				ir.FieldInit{Path: []uint64{1}, Value: &ir.DataExpr{Type: e.typeID(strT), Pos: pos, Off: off, Len: n}},
				ir.FieldInit{Path: []uint64{2}, Value: h})
		}
		lit.Elems = append(lit.Elems, ir.Elem{Index: uint64(i), Value: ent})
	}
	files := &ir.AddrExpr{Type: e.typeID(filesPtr), Pos: pos, X: lit}
	return &ir.StructLitExpr{Type: e.typeID(fsT), Pos: pos, Fields: []ir.FieldInit{{Path: []uint64{0}, Value: files}}}
}
