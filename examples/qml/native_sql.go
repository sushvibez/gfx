//go:build (linux || windows) && !js

package main

// SqlQueryModel and the DatabaseManager's table models, backed by a REAL
// in-memory SQLite database (modernc.org/sqlite — pure Go, no cgo). The
// corpus' database page runs actual SQL: GROUP BY, ROUND(AVG(...)), joins
// against a products table, and a CRUD model with unsaved-change tracking.
// Running it for real is both simpler and more honest than faking result
// sets per query.

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/script/gml"

	_ "modernc.org/sqlite"
)

var (
	sqlOnce sync.Once
	sqlDB   *sql.DB
	sqlErr  error
)

// exampleDB opens (once) the shared in-memory database the whole page uses
// and seeds the tables the corpus queries. "cache=shared" keeps every
// connection in the pool looking at the same data.
func exampleDB() (*sql.DB, error) {
	sqlOnce.Do(func() {
		db, err := sql.Open("sqlite", "file:qmlexamples?mode=memory&cache=shared")
		if err != nil {
			sqlErr = err
			return
		}
		// A single connection keeps the shared in-memory database alive for
		// the process' lifetime.
		db.SetMaxOpenConns(1)
		schema := []string{
			`CREATE TABLE IF NOT EXISTS employees (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				department TEXT NOT NULL,
				salary REAL NOT NULL,
				hire_date TEXT NOT NULL,
				active INTEGER NOT NULL DEFAULT 1)`,
			`CREATE TABLE IF NOT EXISTS products (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				category TEXT NOT NULL,
				price REAL NOT NULL,
				stock INTEGER NOT NULL)`,
		}
		for _, stmt := range schema {
			if _, err := db.Exec(stmt); err != nil {
				sqlErr = err
				return
			}
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM employees`).Scan(&n); err == nil && n == 0 {
			seedEmployees(db)
			seedProducts(db)
		}
		sqlDB = db
	})
	return sqlDB, sqlErr
}

func seedEmployees(db *sql.DB) {
	rows := [][]any{
		{"Ada Lovelace", "Engineering", 98000.0, "2019-03-04", 1},
		{"Grace Hopper", "Engineering", 105000.0, "2018-07-21", 1},
		{"Radia Perlman", "Networking", 99000.0, "2020-01-15", 1},
		{"Susan Kare", "Design", 87000.0, "2021-06-01", 1},
		{"Barbara Liskov", "Engineering", 112000.0, "2017-11-30", 1},
		{"Katherine Johnson", "Management", 120000.0, "2016-02-09", 1},
		{"Margaret Hamilton", "Engineering", 108000.0, "2018-05-14", 1},
		{"Hedy Lamarr", "Marketing", 76000.0, "2022-09-19", 0},
		{"Anita Borg", "Management", 118000.0, "2019-10-02", 1},
		{"Jean Bartik", "Networking", 91000.0, "2021-04-27", 1},
	}
	for _, r := range rows {
		_, _ = db.Exec(`INSERT INTO employees (name, department, salary, hire_date, active)
			VALUES (?, ?, ?, ?, ?)`, r...)
	}
}

func seedProducts(db *sql.DB) {
	rows := [][]any{
		{"Keyboard", "Peripherals", 89.0, 42},
		{"Mouse", "Peripherals", 45.0, 7},
		{"Monitor", "Displays", 320.0, 3},
		{"Docking station", "Peripherals", 210.0, 12},
		{"Webcam", "Video", 130.0, 2},
		{"Headset", "Audio", 99.0, 25},
	}
	for _, r := range rows {
		_, _ = db.Exec(`INSERT INTO products (name, category, price, stock) VALUES (?, ?, ?, ?)`, r...)
	}
}

// queryRows runs a query and returns its column names plus the rows as
// role maps (column name -> value), which is what markup delegates read.
func queryRows(query string, args ...any) (cols []string, rows []any, err error) {
	db, err := exampleDB()
	if err != nil {
		return nil, nil, err
	}
	rs, err := db.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rs.Close()
	cols, err = rs.Columns()
	if err != nil {
		return nil, nil, err
	}
	for rs.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = sqlValue(cells[i])
		}
		rows = append(rows, row)
	}
	return cols, rows, rs.Err()
}

// sqlValue converts a scanned SQL value into the JS value space markup sees
// (numbers as float64, everything else as a string).
func sqlValue(v any) any {
	switch t := v.(type) {
	case nil:
		return ""
	case int64:
		return float64(t)
	case float64:
		return t
	case bool:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

// registerSqlQueryModel backs QSqlQueryModel: run a query, then read the
// result set by row and column. `table`/`headers` also expose it through the
// TableView model contract, so a TableView can bind it directly.
func registerSqlQueryModel() {
	gml.RegisterType("SqlQueryModel", func(emit gml.EmitFunc) *gml.NativeObject {
		rowCount := &core.Property[float32]{}
		colCount := &core.Property[float32]{}
		lastError := &core.Property[string]{}
		rowsProp := gml.NewVarProp([]any{})
		table := gml.NewVarProp([]any{})
		headers := gml.NewVarProp([]any{})
		var cols []string
		var rows []any

		return &gml.NativeObject{
			Props: map[string]any{
				"rowCount":    gml.BindFloat(rowCount),
				"columnCount": gml.BindFloat(colCount),
				"lastError":   gml.BindString(lastError),
				"rows":        gml.BindVar(rowsProp),
				"table":       gml.BindVar(table),
				"headers":     gml.BindVar(headers),
			},
			Methods: map[string]any{
				// execQuery(sql, connectionName) — the connection name is
				// accepted for API parity; one process-wide database backs
				// every connection here.
				"execQuery": func(args []any) (any, error) {
					q := argStr(args, 0)
					c, r, err := queryRows(q)
					if err != nil {
						lastError.Set(err.Error())
						rowCount.Set(0)
						colCount.Set(0)
						rowsProp.Set([]any{})
						table.Set([]any{})
						emit("queryFailed", err.Error())
						return nil, nil
					}
					cols, rows = c, r
					lastError.Set("")
					rowCount.Set(float32(len(rows)))
					colCount.Set(float32(len(cols)))
					rowsProp.Set(rows)
					hs := make([]any, len(cols))
					for i, name := range cols {
						hs[i] = name
					}
					headers.Set(hs)
					grid := make([]any, 0, len(rows))
					for _, row := range rows {
						m, _ := row.(map[string]any)
						cells := make([]any, len(cols))
						for i, name := range cols {
							cells[i] = m[name]
						}
						grid = append(grid, cells)
					}
					table.Set(grid)
					emit("queryFinished", float64(len(rows)))
					return nil, nil
				},
				"getRow": func(args []any) (any, error) {
					i := int(num(argAt(args, 0), -1))
					if i < 0 || i >= len(rows) {
						return map[string]any{}, nil
					}
					return rows[i], nil
				},
				"getValue": func(args []any) (any, error) {
					i := int(num(argAt(args, 0), -1))
					c := int(num(argAt(args, 1), -1))
					if i < 0 || i >= len(rows) || c < 0 || c >= len(cols) {
						return "", nil
					}
					m, _ := rows[i].(map[string]any)
					return m[cols[c]], nil
				},
				"headerName": func(args []any) (any, error) {
					c := int(num(argAt(args, 0), -1))
					if c < 0 || c >= len(cols) {
						return "", nil
					}
					return cols[c], nil
				},
			},
		}
	})
}

// sqlTableModel is DatabaseManager.createTableModel()'s result: an editable
// view of one table with QSqlTableModel's OnManualSubmit behaviour — edits
// and deletes stay pending until save(), and revertChanges() drops them.
type sqlTableModel struct {
	table   string
	filter  string
	rows    []any            // committed rows, as role maps
	pending map[int]bool     // row ids deleted but not yet saved
	added   []map[string]any // rows added but not yet saved
}

// registerDatabaseTableModels extends DatabaseManager with the connection
// name and the table-model factory the CRUD card uses.
func registerDatabaseTableModels() {
	gml.RegisterType("DatabaseManager", func(emit gml.EmitFunc) *gml.NativeObject {
		isOpen := &core.Property[bool]{}
		return &gml.NativeObject{
			Props: map[string]any{
				"connectionName": "qml_examples_memory",
				"isOpen":         gml.BindBool(isOpen),
			},
			Methods: map[string]any{
				// The page opens the database from Component.onCompleted and
				// builds its models when isOpen turns true.
				"openDatabase": func([]any) (any, error) {
					if _, err := exampleDB(); err != nil {
						emit("errorOccurred", err.Error())
						return nil, nil
					}
					isOpen.Set(true)
					emit("isOpenChanged")
					return true, nil
				},
				"closeDatabase": func([]any) (any, error) {
					isOpen.Set(false)
					emit("isOpenChanged")
					return nil, nil
				},
				"tableNames": func([]any) (any, error) {
					_, rows, err := queryRows(
						`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
					if err != nil {
						return []any{}, nil
					}
					out := make([]any, 0, len(rows))
					for _, r := range rows {
						if m, ok := r.(map[string]any); ok {
							out = append(out, m["name"])
						}
					}
					return out, nil
				},
				"createTableModel": func(args []any) (any, error) {
					return newTableModelObject(argStr(args, 0)), nil
				},
				"queryEmployees": func([]any) (any, error) {
					_, rows, err := queryRows(`SELECT id, name, department AS role, salary
						FROM employees ORDER BY id`)
					if err != nil {
						return []any{}, nil
					}
					return rows, nil
				},
				"addEmployee": func(args []any) (any, error) {
					db, err := exampleDB()
					if err != nil {
						return nil, err
					}
					_, _ = db.Exec(`INSERT INTO employees (name, department, salary, hire_date, active)
						VALUES (?, ?, ?, ?, 1)`,
						argStr(args, 0), argStr(args, 1), num(argAt(args, 2), 0), "2026-01-01")
					_, rows, _ := queryRows(`SELECT id, name, department AS role, salary
						FROM employees ORDER BY id`)
					emit("employeeAdded", argStr(args, 0))
					return rows, nil
				},
			},
		}
	})
}

// newTableModelObject builds the live object markup binds a ListView to: its
// `rows` property is the current view of the table, and the methods mutate
// it with manual-submit semantics.
func newTableModelObject(table string) map[string]any {
	m := &sqlTableModel{table: table, pending: map[int]bool{}}
	rowsProp := gml.NewVarProp([]any{})
	rowCount := &core.Property[float32]{}
	hasChanges := &core.Property[bool]{}

	refresh := func() {
		where := ""
		if m.filter != "" {
			where = " WHERE " + m.filter
		}
		_, rows, err := queryRows(`SELECT * FROM ` + m.table + where + ` ORDER BY id`)
		if err != nil {
			rows = nil
		}
		// Committed rows minus the pending deletions, plus pending inserts —
		// what QSqlTableModel shows before a submit.
		view := make([]any, 0, len(rows)+len(m.added))
		for _, r := range rows {
			if rm, ok := r.(map[string]any); ok {
				if id := int(num(rm["id"], -1)); m.pending[id] {
					continue
				}
			}
			view = append(view, r)
		}
		for _, a := range m.added {
			view = append(view, a)
		}
		m.rows = view
		rowsProp.Set(view)
		rowCount.Set(float32(len(view)))
		hasChanges.Set(len(m.pending) > 0 || len(m.added) > 0)
	}
	refresh()

	return map[string]any{
		"rows":       gml.BindVar(rowsProp),
		"rowCount":   gml.BindFloat(rowCount),
		"hasChanges": gml.BindBool(hasChanges),
		"tableName":  table,
		"setFilterString": func(args []any) (any, error) {
			m.filter = argStr(args, 0)
			refresh()
			return nil, nil
		},
		"addRecord": func(args []any) (any, error) {
			row := map[string]any{
				"id":         float64(-len(m.added) - 1), // negative until submitted
				"name":       firstNonEmpty(argStr(args, 0), "New employee"),
				"department": firstNonEmpty(argStr(args, 1), "Engineering"),
				"salary":     num(argAt(args, 2), 50000),
				"hire_date":  "2026-01-01",
				"active":     float64(1),
			}
			m.added = append(m.added, row)
			refresh()
			return nil, nil
		},
		"deleteRecord": func(args []any) (any, error) {
			i := int(num(argAt(args, 0), -1))
			if i < 0 || i >= len(m.rows) {
				return nil, nil
			}
			rm, _ := m.rows[i].(map[string]any)
			id := int(num(rm["id"], 0))
			if id < 0 { // an unsaved insert: just drop it
				for j, a := range m.added {
					if int(num(a["id"], 0)) == id {
						m.added = append(m.added[:j], m.added[j+1:]...)
						break
					}
				}
			} else {
				m.pending[id] = true
			}
			refresh()
			return nil, nil
		},
		"revertChanges": func([]any) (any, error) {
			m.pending = map[int]bool{}
			m.added = nil
			refresh()
			return nil, nil
		},
		"save": func([]any) (any, error) {
			db, err := exampleDB()
			if err != nil {
				return nil, err
			}
			for id := range m.pending {
				_, _ = db.Exec(`DELETE FROM `+m.table+` WHERE id = ?`, id)
			}
			for _, a := range m.added {
				_, _ = db.Exec(`INSERT INTO `+m.table+
					` (name, department, salary, hire_date, active) VALUES (?, ?, ?, ?, ?)`,
					str(a["name"], ""), str(a["department"], ""),
					num(a["salary"], 0), str(a["hire_date"], ""), 1)
			}
			m.pending = map[int]bool{}
			m.added = nil
			refresh()
			return nil, nil
		},
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
