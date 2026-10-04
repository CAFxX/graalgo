package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------- tiny flag parsing ----------

type flags struct {
	pos  []string
	kv   map[string]string
	seen map[string]bool
}

func parseFlags(args []string) *flags {
	f := &flags{kv: map[string]string{}, seen: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			f.pos = append(f.pos, a)
			continue
		}
		a = a[2:]
		var k, v string
		if eq := strings.Index(a, "="); eq >= 0 {
			k, v = a[:eq], a[eq+1:]
		} else {
			k = a
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				v = args[i]
			} else {
				v = "true"
			}
		}
		f.kv[k] = v
		f.seen[k] = true
	}
	return f
}

func (f *flags) str(key string) string { return f.kv[key] }

func (f *flags) ctx() (*workCtx, error) {
	return resolveCtx(f.str("root"), f.str("req-dir"), f.str("scan-dir"))
}

// isTerminal is a variable (not a plain function) so tests can stub the
// interactive-prompt gate without a real TTY. Production behavior is
// unchanged: true only when stdin is a character device.
var isTerminal = func() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// ---------- req add ----------

func cmdAdd(args []string) error {
	f := parseFlags(args)
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	title, stmt := f.str("title"), f.str("statement")
	if title == "" || stmt == "" {
		return fmt.Errorf("req add requires --title and --statement")
	}
	parent := f.str("parent")
	if parent != "" {
		if _, ok := reg[parent]; !ok {
			return fmt.Errorf("parent %s not in registry", parent)
		}
	}
	id, err := mintID(reg, f.str("namespace"))
	if err != nil {
		return err
	}
	r := &Requirement{
		ID:               id,
		Title:            title,
		Statement:        stmt,
		Rationale:        f.str("rationale"),
		Parent:           parent,
		Status:           "draft",
		VerificationPlan: f.str("verification-plan"),
		History:          []string{today() + ": created (draft)"},
	}
	r.file = filepath.Join(c.reqDir, id+".yaml")
	if err := r.save(); err != nil {
		return err
	}
	fmt.Printf("minted %s -> %s\n", id, r.file)
	return nil
}

// ---------- req lock ----------

// cmdLock transitions draft requirements to locked (SPEC-accepted: immutable
// except via supersede/retire). Locking is the Phase-0->1 gate. Atomic: every
// ID is validated before any file is written.
func cmdLock(args []string) error {
	f := parseFlags(args)
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	var ids []string
	if f.seen["all"] {
		ids = sortedIDs(reg)
	} else {
		ids = f.pos
	}
	if len(ids) == 0 {
		return fmt.Errorf("usage: req lock GG-XXXX [GG-YYYY ...] | req lock --all")
	}
	for _, id := range ids {
		rec, ok := reg[id]
		if !ok {
			return fmt.Errorf("unknown requirement id %s", id)
		}
		switch rec.Status {
		case "draft":
			// ok
		case "locked":
			return fmt.Errorf("%s is already locked", id)
		default:
			return fmt.Errorf("%s is not draft (status %q): only draft requirements can be locked", id, rec.Status)
		}
	}
	for _, id := range ids {
		rec := reg[id]
		rec.Status = "locked"
		rec.History = append(rec.History, today()+": locked")
		if err := rec.save(); err != nil {
			return err
		}
		fmt.Printf("locked %s\n", id)
	}
	return nil
}

// ---------- req activate ----------

// cmdActivate transitions locked requirements to active (implementation has
// started; rule (c) then requires >=1 verifies: annotation). Atomic: every
// ID is validated before any file is written.
func cmdActivate(args []string) error {
	f := parseFlags(args)
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	var ids []string
	if f.seen["all"] {
		ids = sortedIDs(reg)
	} else {
		ids = f.pos
	}
	if len(ids) == 0 {
		return fmt.Errorf("usage: req activate GG-XXXX [GG-YYYY ...] | req activate --all")
	}
	for _, id := range ids {
		rec, ok := reg[id]
		if !ok {
			return fmt.Errorf("unknown requirement id %s", id)
		}
		switch rec.Status {
		case "locked":
			// ok
		case "active":
			return fmt.Errorf("%s is already active", id)
		default:
			return fmt.Errorf("%s is not locked (status %q): only locked requirements can be activated", id, rec.Status)
		}
	}
	for _, id := range ids {
		rec := reg[id]
		rec.Status = "active"
		rec.History = append(rec.History, today()+": activated (implementation started)")
		if err := rec.save(); err != nil {
			return err
		}
		fmt.Printf("activated %s\n", id)
	}
	return nil
}

// ---------- req lint ----------

// historicalRef reports whether a reference to a retired/superseded ID is
// allowed to keep pointing at the old ID: inside the registry directory
// itself (the registry's own history: superseded_by chains, parent links,
// history entries) and in the root ESCAPES.md (the escape log is a
// historical record).
func (c *workCtx) historicalRef(rel string) bool {
	if rel == "ESCAPES.md" {
		return true
	}
	relReq, err := filepath.Rel(c.reqDir, filepath.Join(c.root, rel))
	if err != nil {
		return false
	}
	return relReq != ".." && !strings.HasPrefix(relReq, ".."+string(filepath.Separator))
}

func cmdLint(args []string) error {
	f := parseFlags(args)
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	refs, fmts, err := scanAll(c)
	if err != nil {
		return err
	}
	var problems []string

	// Rule (a): every referenced ID resolves.
	// Rule (b): no refs to retired/superseded IDs outside historical records.
	for _, r := range refs {
		rec, ok := reg[r.ID]
		if !ok {
			problems = append(problems,
				fmt.Sprintf("%s:%d: rule (a): unknown requirement id %s", r.File, r.Line, r.ID))
			continue
		}
		if (rec.Status == "retired" || rec.Status == "superseded") && !c.historicalRef(r.File) {
			problems = append(problems,
				fmt.Sprintf("%s:%d: rule (b): reference to %s %s outside historical record",
					r.File, r.Line, rec.Status, r.ID))
		}
	}

	// Rule (c): every active requirement is the target of >=1 verifies: annotation.
	// Locked requirements are exempt until activated (implementation starts);
	// see the lifecycle note on validStatuses.
	verified := map[string]bool{}
	for _, r := range refs {
		if r.Kind == KindVerifies {
			verified[r.ID] = true
		}
	}
	for _, id := range sortedIDs(reg) {
		if reg[id].Status == "active" && !verified[id] {
			problems = append(problems,
				fmt.Sprintf("rule (c): active requirement %s has no `verifies:` annotation", id))
		}
	}

	// Rule (d): canonical annotation format.
	for _, msg := range fmts {
		problems = append(problems, "rule (d): "+msg)
	}

	// Registry self-consistency: parent and superseded_by resolve; superseded
	// requirements name a live successor.
	for _, id := range sortedIDs(reg) {
		rec := reg[id]
		if rec.Parent != "" {
			if _, ok := reg[rec.Parent]; !ok {
				problems = append(problems,
					fmt.Sprintf("%s: parent %s not in registry", rec.file, rec.Parent))
			}
		}
		if rec.Status == "superseded" {
			// The superseded_by chain must terminate: no dangles, no
			// cycles. Intermediate links being themselves superseded
			// is normal (A -> B -> C); the chain is the history.
			seen := map[string]bool{id: true}
			cur := rec.SupersededBy
			for cur != "" {
				succ, ok := reg[cur]
				if !ok {
					problems = append(problems,
						fmt.Sprintf("%s: superseded_by chain dangles at %s", rec.file, cur))
					break
				}
				if seen[cur] {
					problems = append(problems,
						fmt.Sprintf("%s: superseded_by chain cycles at %s", rec.file, cur))
					break
				}
				seen[cur] = true
				if succ.Status != "superseded" {
					break
				}
				if succ.SupersededBy == "" {
					problems = append(problems,
						fmt.Sprintf("%s: chain reaches %s which is superseded with no successor", rec.file, cur))
					break
				}
				cur = succ.SupersededBy
			}
			if rec.SupersededBy == "" {
				problems = append(problems,
					fmt.Sprintf("%s: superseded with empty superseded_by", rec.file))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			fmt.Println("LINT:", p)
		}
		return fmt.Errorf("%d lint problem(s)", len(problems))
	}
	fmt.Printf("lint ok: %d requirement(s), %d reference(s)\n", len(reg), len(refs))
	return nil
}

// ---------- req trace ----------

func cmdTrace(args []string) error {
	f := parseFlags(args)
	if len(f.pos) != 1 {
		return fmt.Errorf("usage: req trace GG-XXXX")
	}
	id := f.pos[0]
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	rec, ok := reg[id]
	if !ok {
		return fmt.Errorf("unknown requirement id %s", id)
	}
	fmt.Printf("%s [%s]\n  title: %s\n", rec.ID, rec.Status, rec.Title)
	if rec.Parent != "" {
		fmt.Printf("  parent: %s\n", rec.Parent)
	}
	if rec.SupersededBy != "" {
		fmt.Printf("  superseded_by: %s\n", rec.SupersededBy)
	}
	// predecessors: requirements this one supersedes
	var preds []string
	for _, other := range sortedIDs(reg) {
		if reg[other].SupersededBy == id {
			preds = append(preds, other)
		}
	}
	if len(preds) > 0 {
		fmt.Printf("  supersedes: %s\n", strings.Join(preds, ", "))
	}
	fmt.Printf("  statement: %s\n", rec.Statement)
	if rec.Rationale != "" {
		fmt.Printf("  rationale: %s\n", rec.Rationale)
	}
	if rec.VerificationPlan != "" {
		fmt.Printf("  verification_plan: %s\n", rec.VerificationPlan)
	}
	for _, h := range rec.History {
		fmt.Printf("  history: %s\n", h)
	}

	refs, _, err := scanAll(c)
	if err != nil {
		return err
	}
	fmt.Println("  code sites:")
	n := 0
	for _, r := range refs {
		if r.ID == id && r.Kind == KindRef && !c.historicalRef(r.File) {
			fmt.Printf("    %s:%d: %s\n", r.File, r.Line, r.Text)
			n++
		}
	}
	if n == 0 {
		fmt.Println("    (none)")
	}
	fmt.Println("  tests:")
	n = 0
	for _, r := range refs {
		if r.ID == id && r.Kind == KindVerifies {
			fmt.Printf("    %s:%d: %s\n", r.File, r.Line, r.Text)
			n++
		}
	}
	if n == 0 {
		fmt.Println("    (none)")
	}
	return nil
}

// ---------- req tree ----------

func cmdTree(args []string) error {
	f := parseFlags(args)
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	children := map[string][]string{}
	var roots []string
	for _, id := range sortedIDs(reg) {
		p := reg[id].Parent
		if p == "" {
			roots = append(roots, id)
			continue
		}
		if _, ok := reg[p]; !ok {
			fmt.Printf("warning: %s has dangling parent %s\n", id, p)
			roots = append(roots, id)
			continue
		}
		children[p] = append(children[p], id)
	}
	var walk func(id string, depth int, stack map[string]bool)
	walk = func(id string, depth int, stack map[string]bool) {
		if stack[id] {
			fmt.Printf("%s%s [CYCLE]\n", strings.Repeat("  ", depth), id)
			return
		}
		stack[id] = true
		rec := reg[id]
		status := rec.Status
		if rec.SupersededBy != "" {
			status += " -> " + rec.SupersededBy
		}
		fmt.Printf("%s%s [%s] %s\n", strings.Repeat("  ", depth), id, status, rec.Title)
		for _, ch := range children[id] {
			walk(ch, depth+1, stack)
		}
		delete(stack, id)
	}
	for _, r := range roots {
		walk(r, 0, map[string]bool{})
	}
	return nil
}

// ---------- req supersede / req retire ----------

// disposition of one codebase hit during supersede/retire.
type disposition struct {
	ref    Ref
	action byte // 'u' update token, 'd' delete line, 'a' abort
}

func codeHits(c *workCtx, refs []Ref, oldID string) []Ref {
	var hits []Ref
	for _, r := range refs {
		if r.ID == oldID && !c.historicalRef(r.File) {
			hits = append(hits, r)
		}
	}
	return hits
}

func promptDispositions(hits []Ref, oldID, newID string, retire bool) ([]disposition, error) {
	if !isTerminal() {
		return nil, fmt.Errorf("refusing non-interactive supersede/retire with %d un-dispositioned hit(s); run in a terminal", len(hits))
	}
	in := bufio.NewReader(os.Stdin)
	var out []disposition
	for _, h := range hits {
		fmt.Printf("\n%s:%d [%s]\n  %s\n", h.File, h.Line, h.Kind, h.Text)
		if h.Kind == KindVerifies {
			fmt.Printf("  note: re-pointing a `verifies:` annotation asserts this test verifies the new requirement\n")
		}
		for {
			if retire {
				fmt.Printf("  [d]elete line / [a]bort (requirement %s is being retired, references must go): ", oldID)
			} else {
				fmt.Printf("  [u]pdate to %s / [d]elete line / [a]bort: ", newID)
			}
			line, err := in.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("aborted: %v", err)
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "u":
				if retire {
					fmt.Println("  update not applicable on retire; delete the line or abort and re-point it manually")
					continue
				}
				out = append(out, disposition{h, 'u'})
			case "d":
				out = append(out, disposition{h, 'd'})
			case "a", "":
				return nil, fmt.Errorf("aborted by user; no changes written")
			default:
				fmt.Println("  unknown choice")
				continue
			}
			break
		}
	}
	return out, nil
}

// absRefPath anchors a repo-relative scan path at the work root. Scan hits
// are stored relative to the root, but file mutations must resolve against
// the root regardless of the process working directory.
func absRefPath(c *workCtx, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(c.root, rel)
}

// pendingWrite is one file mutation staged in memory before the atomic commit.
type pendingWrite struct {
	path    string // absolute path
	data    []byte // new file content
	existed bool   // false when the file is brand new
	orig    []byte // original content, valid when existed
}

// planFileEdits applies dispositions to in-memory copies of the hit files.
// Nothing is written: the caller commits the returned writes atomically, so
// a planning failure (unreadable file, stale line number) leaves the tree
// untouched.
func planFileEdits(c *workCtx, ds []disposition, oldID, newID string) ([]pendingWrite, error) {
	byFile := map[string][]disposition{}
	for _, d := range ds {
		p := absRefPath(c, d.ref.File)
		byFile[p] = append(byFile[p], d)
	}
	var out []pendingWrite
	for path, list := range byFile {
		// Descending line order keeps 1-based line numbers valid while
		// deleting lines above not-yet-processed hits.
		sort.Slice(list, func(i, j int) bool { return list[i].ref.Line > list[j].ref.Line })
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(data), "\n")
		for _, d := range list {
			idx := d.ref.Line - 1
			if idx < 0 || idx >= len(lines) {
				return nil, fmt.Errorf("%s:%d: line out of range (file changed during supersede?)", d.ref.File, d.ref.Line)
			}
			switch d.action {
			case 'u':
				lines[idx] = strings.ReplaceAll(lines[idx], oldID, newID)
			case 'd':
				lines = append(lines[:idx], lines[idx+1:]...)
			}
		}
		out = append(out, pendingWrite{path: path, data: []byte(strings.Join(lines, "\n")), existed: true, orig: data})
		fmt.Printf("  staged %s (%d hit(s))\n", path, len(list))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// commitWrites performs every staged write and reports the first failure.
// On failure it rolls back the writes already performed — restoring original
// contents and removing brand-new files — so the tree is left exactly as it
// was before the commit started. Rollback itself is best-effort: there is
// nothing further to roll back to.
func commitWrites(ws []pendingWrite) error {
	var done []pendingWrite
	for _, w := range ws {
		if err := os.WriteFile(w.path, w.data, 0644); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				d := done[i]
				if d.existed {
					_ = os.WriteFile(d.path, d.orig, 0644)
				} else {
					_ = os.Remove(d.path)
				}
			}
			return err
		}
		done = append(done, w)
	}
	return nil
}

func cmdSupersede(args []string) error {
	return cmdRetireSupersede(args, false)
}

func cmdRetire(args []string) error {
	return cmdRetireSupersede(args, true)
}

func cmdRetireSupersede(args []string, retire bool) error {
	op := "supersede"
	if retire {
		op = "retire"
	}
	f := parseFlags(args)
	if len(f.pos) != 1 {
		return fmt.Errorf("usage: req %s GG-XXXX [--title ... --statement ...]", op)
	}
	oldID := f.pos[0]
	c, err := f.ctx()
	if err != nil {
		return err
	}
	// resolveCtx defaults reqDir to <root>/requirements; supersede/retire
	// operate on the real registry there.
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	old, ok := reg[oldID]
	if !ok {
		return fmt.Errorf("unknown requirement id %s", oldID)
	}
	if old.Status == "retired" || old.Status == "superseded" {
		return fmt.Errorf("%s is already %s", oldID, old.Status)
	}
	if old.Status == "draft" {
		return fmt.Errorf("%s is still draft: edit requirements/%s.yaml in place instead of %sing it", oldID, oldID, op)
	}

	var newRec *Requirement
	newID := ""
	if !retire {
		title, stmt := f.str("title"), f.str("statement")
		if title == "" || stmt == "" {
			return fmt.Errorf("req supersede requires --title and --statement for the new requirement")
		}
		// Superseding IDs stay in the old ID's namespace.
		ons, _, _ := splitID(oldID)
		newID, err = mintID(reg, ons)
		if err != nil {
			return err
		}
		newRec = &Requirement{
			ID:               newID,
			Title:            title,
			Statement:        stmt,
			Rationale:        f.str("rationale"),
			Parent:           f.str("parent"),
			Status:           "draft",
			VerificationPlan: f.str("verification-plan"),
			History:          []string{today() + ": created (draft), supersedes " + oldID},
		}
		if newRec.Parent != "" {
			if _, ok := reg[newRec.Parent]; !ok {
				return fmt.Errorf("parent %s not in registry", newRec.Parent)
			}
		}
		newRec.file = filepath.Join(c.reqDir, newID+".yaml")
	}

	refs, _, err := scanAll(c)
	if err != nil {
		return err
	}
	hits := codeHits(c, refs, oldID)
	var kept []Ref
	for _, r := range refs {
		if r.ID == oldID && c.historicalRef(r.File) {
			kept = append(kept, r)
		}
	}
	fmt.Printf("%sing %s: %d codebase hit(s), %d historical reference(s) kept as-is\n",
		op, oldID, len(hits), len(kept))
	for _, r := range kept {
		fmt.Printf("  kept (historical): %s:%d\n", r.File, r.Line)
	}

	// Disposition first; registry writes last. Dispositions are planned
	// against in-memory copies and every mutation — code files and registry
	// YAMLs alike — goes through one atomic commit with rollback, so any
	// failure before the commit returns leaves the tree byte-identical.
	var writes []pendingWrite
	if len(hits) > 0 {
		ds, err := promptDispositions(hits, oldID, newID, retire)
		if err != nil {
			return err
		}
		writes, err = planFileEdits(c, ds, oldID, newID)
		if err != nil {
			return err
		}
	}

	oldOrig, err := os.ReadFile(old.file)
	if err != nil {
		return err
	}
	if !retire {
		newData, err := yaml.Marshal(newRec)
		if err != nil {
			return err
		}
		writes = append(writes, pendingWrite{path: newRec.file, data: newData})
		old.Status = "superseded"
		old.SupersededBy = newID
		old.History = append(old.History, today()+": superseded by "+newID)
	} else {
		old.Status = "retired"
		reason := f.str("reason")
		if reason == "" {
			reason = "retired"
		}
		old.History = append(old.History, today()+": retired ("+reason+")")
	}
	oldData, err := yaml.Marshal(old)
	if err != nil {
		return err
	}
	writes = append(writes, pendingWrite{path: old.file, data: oldData, existed: true, orig: oldOrig})
	if err := commitWrites(writes); err != nil {
		return fmt.Errorf("%s: atomic commit failed, rolled back: %v", op, err)
	}

	fmt.Printf("\n%s complete. Review the diff, run `req lint`, then commit atomically:\n", op)
	if retire {
		fmt.Printf("  git add -A && git commit -m \"req(%s): retired\"\n", oldID)
	} else {
		fmt.Printf("  git add -A && git commit -m \"req(%s): superseded by %s\"\n", oldID, newID)
	}
	return nil
}
