# `database/sql` wave-3 corpus

Differential corpus for the database/sql (+database/sql/driver) package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Minimal in-memory driver (Driver/Conn/Stmt/Result/Rows, transactions) exercising Exec/Query/Prepare/QueryRow, LastInsertId/RowsAffected, ErrNoRows, commit + rollback. Pure Go, no cgo. No .scopelimited programs. Only the driver's in-memory table is exercised; real database drivers and connection pooling behaviour are out of scope.

## Programs

- `00_driver.go` — In-memory driver: Exec/Query/Prepare/QueryRow, transactions commit+rollback.

## Retest trigger

database/sql scanning/conversion semantics change.
