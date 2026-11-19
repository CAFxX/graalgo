package main

// L11 (REVIEW-2026-11-18-05): database/sql + database/sql/driver —
// zero corpus imports before this program. A minimal in-memory
// driver.Driver/Conn/Stmt/Result/Rows (pure Go, no cgo) exercising
// Exec/Query/Prepare/QueryRow and transactions (commit + rollback).
// Table: a single map[string]int64 ("kv").

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type memDriver struct{}

type memConn struct {
	mu   sync.Mutex
	data map[string]int64
	tx   *memTx
}

type memTx struct {
	conn *memConn
	// staged writes, applied on Commit, dropped on Rollback.
	pending map[string]int64
	done    bool
}

type memStmt struct {
	conn  *memConn
	query string
}

type memRows struct {
	cols []string
	vals [][]driver.Value
	pos  int
}

type memResult struct{ lastID, affected int64 }

func (memDriver) Open(name string) (driver.Conn, error) {
	return &memConn{data: map[string]int64{}}, nil
}

func (c *memConn) Prepare(query string) (driver.Stmt, error) {
	return &memStmt{conn: c, query: query}, nil
}

func (c *memConn) Close() error { return nil }

func (c *memConn) Begin() (driver.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tx != nil {
		return nil, fmt.Errorf("tx already active")
	}
	c.tx = &memTx{conn: c, pending: map[string]int64{}}
	return c.tx, nil
}

func (t *memTx) Commit() error {
	t.conn.mu.Lock()
	defer t.conn.mu.Unlock()
	if t.done {
		return fmt.Errorf("tx already finished")
	}
	t.done = true
	for k, v := range t.pending {
		t.conn.data[k] = v
	}
	t.conn.tx = nil
	return nil
}

func (t *memTx) Rollback() error {
	t.conn.mu.Lock()
	defer t.conn.mu.Unlock()
	if t.done {
		return fmt.Errorf("tx already finished")
	}
	t.done = true
	t.conn.tx = nil
	return nil
}

func (c *memConn) put(k string, v int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tx != nil {
		c.tx.pending[k] = v
		return
	}
	c.data[k] = v
}

func (c *memConn) get(k string) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tx != nil {
		if v, ok := c.tx.pending[k]; ok {
			return v, true
		}
	}
	v, ok := c.data[k]
	return v, ok
}

func (c *memConn) all() [][2]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.data))
	for k := range c.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]any, 0, len(keys))
	for _, k := range keys {
		v := c.data[k]
		if c.tx != nil {
			if pv, ok := c.tx.pending[k]; ok {
				v = pv
			}
		}
		out = append(out, [2]any{k, v})
	}
	for k, v := range c.txPending() {
		found := false
		for _, kv := range out {
			if kv[0] == k {
				found = true
			}
		}
		if !found {
			out = append(out, [2]any{k, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0].(string) < out[j][0].(string) })
	return out
}

func (c *memConn) txPending() map[string]int64 {
	if c.tx == nil {
		return nil
	}
	return c.tx.pending
}

func parseKV(args []driver.Value) (string, int64, error) {
	if len(args) != 2 {
		return "", 0, fmt.Errorf("want 2 args, got %d", len(args))
	}
	k, ok := args[0].(string)
	if !ok {
		return "", 0, fmt.Errorf("key must be string")
	}
	var v int64
	switch n := args[1].(type) {
	case int64:
		v = n
	case int:
		v = int64(n)
	case string:
		n64, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return "", 0, err
		}
		v = n64
	default:
		return "", 0, fmt.Errorf("value must be int64")
	}
	return k, v, nil
}

func (s *memStmt) Close() error { return nil }

func (s *memStmt) NumInput() int {
	switch {
	case strings.HasPrefix(s.query, "INSERT"):
		return 2
	case strings.HasPrefix(s.query, "SELECT *"):
		return 0
	case strings.HasPrefix(s.query, "SELECT"):
		return 1
	default:
		return -1
	}
}

func (s *memStmt) Exec(args []driver.Value) (driver.Result, error) {
	if !strings.HasPrefix(s.query, "INSERT") {
		return nil, fmt.Errorf("exec: unsupported %q", s.query)
	}
	k, v, err := parseKV(args)
	if err != nil {
		return nil, err
	}
	s.conn.put(k, v)
	return memResult{lastID: v, affected: 1}, nil
}

func (s *memStmt) Query(args []driver.Value) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(s.query, "SELECT *"):
		rows := s.conn.all()
		vals := make([][]driver.Value, 0, len(rows))
		for _, kv := range rows {
			vals = append(vals, []driver.Value{kv[0].(string), kv[1].(int64)})
		}
		return &memRows{cols: []string{"k", "v"}, vals: vals}, nil
	case strings.HasPrefix(s.query, "SELECT"):
		if len(args) != 1 {
			return nil, fmt.Errorf("select needs 1 arg")
		}
		k, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("key must be string")
		}
		v, found := s.conn.get(k)
		if !found {
			return &memRows{cols: []string{"v"}}, nil
		}
		if strings.HasPrefix(s.query, "SELECT v") {
			return &memRows{cols: []string{"v"},
				vals: [][]driver.Value{{v}}}, nil
		}
		return &memRows{cols: []string{"k", "v"},
			vals: [][]driver.Value{{k, v}}}, nil
	default:
		return nil, fmt.Errorf("query: unsupported %q", s.query)
	}
}

func (r *memRows) Columns() []string { return r.cols }

func (r *memRows) Close() error { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.vals) {
		return io.EOF
	}
	copy(dest, r.vals[r.pos])
	r.pos++
	return nil
}

func (r memResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r memResult) RowsAffected() (int64, error) { return r.affected, nil }

func check(err error) {
	if err != nil {
		fmt.Printf("fatal=%v\n", err)
		panic("abort")
	}
}

func main() {
	sql.Register("mem1805", memDriver{})
	db, err := sql.Open("mem1805", "")
	check(err)
	defer db.Close()
	check(db.Ping())

	// Exec + LastInsertId/RowsAffected.
	res, err := db.Exec("INSERT INTO kv VALUES (?, ?)", "a", 10)
	check(err)
	id, _ := res.LastInsertId()
	aff, _ := res.RowsAffected()
	fmt.Printf("insert-a lastid=%d affected=%d\n", id, aff)
	_, err = db.Exec("INSERT INTO kv VALUES (?, ?)", "b", 20)
	check(err)

	// Prepared query.
	stmt, err := db.Prepare("SELECT v FROM kv WHERE k = ?")
	check(err)
	var v int64
	err = stmt.QueryRow("a").Scan(&v)
	check(err)
	fmt.Printf("select-a=%d\n", v)

	// QueryRow on a missing key -> ErrNoRows.
	err = stmt.QueryRow("missing").Scan(&v)
	fmt.Printf("missing-is-norows=%v\n", err == sql.ErrNoRows)
	stmt.Close()

	// Full scan, sorted by key.
	rows, err := db.Query("SELECT * FROM kv")
	check(err)
	var pairs []string
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			fmt.Printf("scan-err=%v\n", err)
			return
		}
		pairs = append(pairs, fmt.Sprintf("%s=%d", k, n))
	}
	fmt.Printf("all=%v rows-err=%v\n", pairs, rows.Err())
	rows.Close()

	// Transaction commit.
	tx, err := db.Begin()
	check(err)
	_, err = tx.Exec("INSERT INTO kv VALUES (?, ?)", "c", 30)
	check(err)
	check(tx.Commit())
	err = db.QueryRow("SELECT v FROM kv WHERE k = ?", "c").Scan(&v)
	check(err)
	fmt.Printf("after-commit-c=%d\n", v)

	// Transaction rollback.
	tx, err = db.Begin()
	check(err)
	_, err = tx.Exec("INSERT INTO kv VALUES (?, ?)", "d", 40)
	check(err)
	check(tx.Rollback())
	err = db.QueryRow("SELECT v FROM kv WHERE k = ?", "d").Scan(&v)
	fmt.Printf("after-rollback-d-norows=%v\n", err == sql.ErrNoRows)
}
