//go:build libsqlite3

package svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"unsafe"
)

// #cgo LDFLAGS: -Wl,--start-group -lsqlite3 -lcrypto -Wl,--end-group
// #define SQLITE_HAS_CODEC
// #include <sqlite3.h>
// #include <stdlib.h>
import "C"

// sqliteDriver is the project's own minimal database/sql driver over
// libsqlite3 (= SQLCipher in this image). mattn/go-sqlite3 cannot
// reopen an encrypted database: its Open runs "PRAGMA synchronous"
// before ConnectHook, and synchronous carries PragFlg_NeedSchema, so
// it reads page 1 unkeyed and fails with SQLITE_NOTADB. The key must
// be applied before any statement; this driver calls sqlite3_key at
// connection create, then the usual pragmas.
type sqliteDriver struct{ hexKey string }

func (d sqliteDriver) Open(name string) (driver.Conn, error) {
	return openConn(name, d.hexKey)
}

// sqliteConnector hands path+key to the driver without a global
// registration: openDB uses sql.OpenDB on a per-call connector.
type sqliteConnector struct {
	path string
	drv  sqliteDriver
}

func (c *sqliteConnector) Connect(context.Context) (driver.Conn, error) {
	return c.drv.Open(c.path)
}

func (c *sqliteConnector) Driver() driver.Driver { return c.drv }

func isHexKey(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func openConn(path, hexKey string) (*sqliteConn, error) {
	if !isHexKey(hexKey) {
		return nil, fmt.Errorf("db key: want 64 lowercase hex chars")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var db *C.sqlite3
	rc := C.sqlite3_open_v2(cpath, &db,
		C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX, nil)
	if rc != C.SQLITE_OK {
		return nil, fmt.Errorf("open %s: %w", path, cErr(db, rc))
	}
	// Raw-key pragma: the 64-hex file IS the 32-byte cipher key (no
	// KDF), matching the format hubctl init provisions.
	for _, q := range []string{
		`PRAGMA key = "x'` + hexKey + `'"`,
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		cs := C.CString(q)
		rc = C.sqlite3_exec(db, cs, nil, nil, nil)
		C.free(unsafe.Pointer(cs))
		if rc != C.SQLITE_OK {
			err := fmt.Errorf("setup %q: %w", q, cErr(db, rc))
			C.sqlite3_close_v2(db)
			return nil, err
		}
	}
	return &sqliteConn{db: db}, nil
}

func cErr(db *C.sqlite3, rc C.int) error {
	if db == nil {
		return fmt.Errorf("sqlite rc=%d", int(rc))
	}
	msg := C.sqlite3_errmsg(db)
	if msg == nil {
		return fmt.Errorf("sqlite rc=%d", int(rc))
	}
	return fmt.Errorf("%s (rc=%d)", C.GoString(msg), int(rc))
}

// sqliteConn is one keyed SQLCipher connection.
type sqliteConn struct{ db *C.sqlite3 }

func (c *sqliteConn) Prepare(q string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), q)
}

func (c *sqliteConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.prepare(ctx, q)
}

func (c *sqliteConn) prepare(ctx context.Context, q string) (*sqliteStmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("prepare: %w", err)
	}
	cs := C.CString(q)
	defer C.free(unsafe.Pointer(cs))
	var st *C.sqlite3_stmt
	var tail *C.char
	rc := C.sqlite3_prepare_v2(c.db, cs, C.int(-1), &st, &tail)
	if rc != C.SQLITE_OK {
		return nil, cErr(c.db, rc)
	}
	t := ""
	if tail != nil {
		t = C.GoString(tail)
	}
	return &sqliteStmt{c: c, st: st, tail: t}, nil
}

func (c *sqliteConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	st, err := c.prepare(ctx, q)
	if err != nil {
		return nil, err
	}
	defer st.Close() //nolint:errcheck // best-effort close
	return st.execAll(ctx, args)
}

func (c *sqliteConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	st, err := c.prepare(ctx, q)
	if err != nil {
		return nil, err
	}
	return st.query(ctx, args)
}

func (c *sqliteConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *sqliteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if opts.Isolation != 0 || opts.ReadOnly {
		return nil, fmt.Errorf("tx options unsupported")
	}
	if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE", nil); err != nil {
		return nil, err
	}
	return &sqliteTx{c: c}, nil
}

func (c *sqliteConn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT 1", nil)
	return err
}

func (c *sqliteConn) ResetSession(context.Context) error { return nil }

func (c *sqliteConn) Close() error {
	if c.db == nil {
		return nil
	}
	rc := C.sqlite3_close_v2(c.db)
	c.db = nil
	if rc != C.SQLITE_OK {
		return fmt.Errorf("close: rc=%d", int(rc))
	}
	return nil
}

type sqliteTx struct{ c *sqliteConn }

func (t *sqliteTx) Commit() error {
	_, err := t.c.ExecContext(context.Background(), "COMMIT", nil)
	return err
}

func (t *sqliteTx) Rollback() error {
	_, err := t.c.ExecContext(context.Background(), "ROLLBACK", nil)
	return err
}

// sqliteStmt is one prepared statement; tail carries the remaining
// SQL of a multi-statement Exec so migration bodies run whole.
type sqliteStmt struct {
	c    *sqliteConn
	st   *C.sqlite3_stmt
	tail string
}

func (s *sqliteStmt) NumInput() int {
	if s.st == nil {
		return 0
	}
	return int(C.sqlite3_bind_parameter_count(s.st))
}

func (s *sqliteStmt) bind(args []driver.NamedValue) error {
	if s.st == nil {
		return nil
	}
	for _, a := range args {
		if err := s.bindOne(a); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteStmt) bindOne(a driver.NamedValue) error {
	var rc C.int
	i := C.int(a.Ordinal)
	switch v := a.Value.(type) {
	case nil:
		rc = C.sqlite3_bind_null(s.st, i)
	case int64:
		rc = C.sqlite3_bind_int64(s.st, i, C.sqlite3_int64(v))
	case float64:
		rc = C.sqlite3_bind_double(s.st, i, C.double(v))
	case bool:
		b := C.int(0)
		if v {
			b = 1
		}
		rc = C.sqlite3_bind_int(s.st, i, b)
	case []byte:
		if len(v) == 0 {
			rc = C.sqlite3_bind_blob(s.st, i, nil, 0, nil)
		} else {
			rc = C.sqlite3_bind_blob(s.st, i, unsafe.Pointer(&v[0]), C.int(len(v)), C.SQLITE_TRANSIENT)
		}
	case string:
		cs := C.CString(v)
		rc = C.sqlite3_bind_text(s.st, i, cs, C.int(len(v)), C.SQLITE_TRANSIENT)
		C.free(unsafe.Pointer(cs))
	default:
		return fmt.Errorf("bind arg %d: unsupported type %T", a.Ordinal, a.Value)
	}
	if rc != C.SQLITE_OK {
		return fmt.Errorf("bind arg %d: %w", a.Ordinal, cErr(s.c.db, rc))
	}
	return nil
}

// stepAll drains a statement; ROW results are discarded (Exec path).
// A nil statement is a whitespace/comment-only prepare — nothing to
// step, and sqlite3_step(NULL) is SQLITE_MISUSE.
func (s *sqliteStmt) stepAll() error {
	if s.st == nil {
		return nil
	}
	for {
		rc := C.sqlite3_step(s.st)
		if rc == C.SQLITE_ROW {
			continue
		}
		if rc == C.SQLITE_DONE {
			return nil
		}
		return cErr(s.c.db, rc)
	}
}

// execAll runs the statement then any tail statements (migrations
// arrive as whole files).
func (s *sqliteStmt) execAll(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.bind(args); err != nil {
		return nil, err
	}
	if err := s.stepAll(); err != nil {
		return nil, err
	}
	rest := s.tail
	for rest != "" {
		st, err := s.c.prepare(ctx, rest)
		if err != nil {
			return nil, err
		}
		err = st.stepAll()
		rest = st.tail
		st.Close() //nolint:errcheck // best-effort close
		if err != nil {
			return nil, err
		}
	}
	return result{
		id:      int64(C.sqlite3_last_insert_rowid(s.c.db)),
		changes: int64(C.sqlite3_changes(s.c.db)),
	}, nil
}

func (s *sqliteStmt) Exec(args []driver.Value) (driver.Result, error) {
	nv := make([]driver.NamedValue, len(args))
	for i, v := range args {
		nv[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.execAll(context.Background(), nv)
}

func (s *sqliteStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	return s.execAll(ctx, args)
}

func (s *sqliteStmt) query(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if err := s.bind(args); err != nil {
		s.Close() //nolint:errcheck // best-effort close
		return nil, err
	}
	return &sqliteRows{s: s}, nil
}

func (s *sqliteStmt) Query(args []driver.Value) (driver.Rows, error) {
	nv := make([]driver.NamedValue, len(args))
	for i, v := range args {
		nv[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.query(context.Background(), nv)
}

func (s *sqliteStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.query(ctx, args)
}

func (s *sqliteStmt) Close() error {
	if s.st == nil {
		return nil
	}
	rc := C.sqlite3_finalize(s.st)
	s.st = nil
	if rc != C.SQLITE_OK {
		return cErr(s.c.db, rc)
	}
	return nil
}

type result struct{ id, changes int64 }

func (r result) LastInsertId() (int64, error) { return r.id, nil }
func (r result) RowsAffected() (int64, error) { return r.changes, nil }

// sqliteRows streams column values; C memory is copied into Go before
// the next step so pointers never outlive the row.
type sqliteRows struct {
	s    *sqliteStmt
	cols []string
	done bool
}

func (r *sqliteRows) Columns() []string {
	if r.cols == nil {
		if r.s.st == nil {
			r.cols = []string{}
			return r.cols
		}
		n := int(C.sqlite3_column_count(r.s.st))
		r.cols = make([]string, n)
		for i := range r.cols {
			r.cols[i] = C.GoString(C.sqlite3_column_name(r.s.st, C.int(i)))
		}
	}
	return r.cols
}

func (r *sqliteRows) Close() error {
	return r.s.Close()
}

func (r *sqliteRows) Next(dest []driver.Value) error {
	if r.done || r.s.st == nil {
		return io.EOF
	}
	rc := C.sqlite3_step(r.s.st)
	switch rc {
	case C.SQLITE_ROW:
		for i := range dest {
			dest[i] = colValue(r.s.st, C.int(i))
		}
		return nil
	case C.SQLITE_DONE:
		r.done = true
		return io.EOF
	default:
		return cErr(r.s.c.db, rc)
	}
}

func colValue(st *C.sqlite3_stmt, i C.int) driver.Value {
	switch C.sqlite3_column_type(st, i) {
	case C.SQLITE_INTEGER:
		return int64(C.sqlite3_column_int64(st, i))
	case C.SQLITE_FLOAT:
		return float64(C.sqlite3_column_double(st, i))
	case C.SQLITE_TEXT:
		n := C.sqlite3_column_bytes(st, i)
		p := C.sqlite3_column_text(st, i)
		if n == 0 || p == nil {
			return ""
		}
		return C.GoStringN((*C.char)(unsafe.Pointer(p)), n)
	case C.SQLITE_BLOB:
		n := C.sqlite3_column_bytes(st, i)
		p := C.sqlite3_column_blob(st, i)
		if n == 0 || p == nil {
			return []byte{}
		}
		return C.GoBytes(p, n)
	default:
		return nil
	}
}

var (
	_ driver.Driver             = sqliteDriver{}
	_ driver.Connector          = (*sqliteConnector)(nil)
	_ driver.Conn               = (*sqliteConn)(nil)
	_ driver.ConnPrepareContext = (*sqliteConn)(nil)
	_ driver.ConnBeginTx        = (*sqliteConn)(nil)
	_ driver.ExecerContext      = (*sqliteConn)(nil)
	_ driver.QueryerContext     = (*sqliteConn)(nil)
	_ driver.Pinger             = (*sqliteConn)(nil)
	_ driver.SessionResetter    = (*sqliteConn)(nil)
	_ driver.Stmt               = (*sqliteStmt)(nil)
	_ driver.StmtExecContext    = (*sqliteStmt)(nil)
	_ driver.StmtQueryContext   = (*sqliteStmt)(nil)
	_ driver.Tx                 = (*sqliteTx)(nil)
	_ driver.Rows               = (*sqliteRows)(nil)
	_ sql.Result                = result{}
)
