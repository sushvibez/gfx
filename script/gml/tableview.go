package gml

// Qt 6 TableView with the declarative TableModel from Qt.labs.qmlmodels:
// TableModel declares columns (TableModelColumn { display: "key" }) over a
// `rows:` array of JS objects, and TableView instantiates its delegate once
// per visible CELL (row-major), injecting row/column/display — plus
// selected/current, which are static false here (ItemSelectionModel is a
// stub; selection rendering is a TODO). HorizontalHeaderView follows its
// syncView's columns. Cells are full per-row/column component instances via
// enterRow, so delegate ids and properties scope per cell like any delegate.

import (
	"fmt"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// tableSource is what the table controls need from any model: the
// declarative TableModel below, or an object-backed model (a Go
// NativeObject / QAbstractTableModel stand-in) exposing the table contract:
// props "table" ([][]cell display values, the VISIBLE view — proxies serve
// sorted/filtered rows here) and "headers" ([]column titles).
type tableSource interface {
	tableDims() (rows, cols int)
	cellDisplay(row, col int) any
	headerDisplay(col int) any
}

// tableWriter is a tableSource whose cells can be edited in place — what
// TableView.editDelegate commits into. Object-backed models don't implement
// it (their rows live behind a Go backend's own API).
type tableWriter interface {
	setCell(row, col int, v any) bool
}

// tableModel is Qt.labs.qmlmodels' TableModel: named display roles per
// column over rows of JS objects.
type tableModel struct {
	columns []string // display role per column, in declaration order
	rows    []map[string]any
}

func (m *tableModel) tableDims() (int, int) { return len(m.rows), len(m.columns) }
func (m *tableModel) cellDisplay(r, c int) any {
	if r < len(m.rows) && c < len(m.columns) {
		return m.rows[r][m.columns[c]]
	}
	return nil
}

// setCell writes an edited value back into the model's row (the display
// role of that column), so bindings reading it re-evaluate next frame.
func (m *tableModel) setCell(r, c int, v any) bool {
	if r >= len(m.rows) || c >= len(m.columns) || m.rows[r] == nil {
		return false
	}
	m.rows[r][m.columns[c]] = v
	core.BumpRevision() // rows are data outside the property system
	return true
}

func (m *tableModel) headerDisplay(c int) any {
	if c < len(m.columns) {
		return m.columns[c]
	}
	return nil
}

// objTable adapts an object exposing the table contract (props "table" +
// "headers", either plain values or Refs) to tableSource. Reads go through
// the props map every time, so Go-side mutations show on the next frame.
type objTable struct{ props map[string]any }

func (t *objTable) get(k string) any {
	v := t.props[k]
	if r, ok := v.(Ref); ok {
		return r.get()
	}
	return v
}

func (t *objTable) rows() []any {
	arr, _ := t.get("table").([]any)
	return arr
}

func (t *objTable) tableDims() (int, int) {
	rows := t.rows()
	headers, _ := t.get("headers").([]any)
	cols := len(headers)
	if cols == 0 && len(rows) > 0 {
		if r0, ok := rows[0].([]any); ok {
			cols = len(r0)
		}
	}
	return len(rows), cols
}

func (t *objTable) cellDisplay(r, c int) any {
	rows := t.rows()
	if r < len(rows) {
		if row, ok := rows[r].([]any); ok && c < len(row) {
			return row[c]
		}
	}
	return nil
}

func (t *objTable) headerDisplay(c int) any {
	headers, _ := t.get("headers").([]any)
	if c < len(headers) {
		return headers[c]
	}
	return nil
}

// registerTableModel evaluates a TableModel's columns and rows once and
// binds it under its id (declaration pass, like ListModel).
func registerTableModel(n *node, env *Env, doc *docState) error {
	id := nodeID(n)
	if id == "" {
		return fmt.Errorf("gml:%d: TableModel needs an id", n.line)
	}
	m := &tableModel{}
	for _, c := range n.children {
		if c.typeName != "TableModelColumn" {
			return fmt.Errorf("gml:%d: TableModel contains only TableModelColumn, got %q", c.line, c.typeName)
		}
		for _, a := range c.attrs {
			if a.name == "display" && a.expr != nil {
				v, err := a.expr.eval(env)
				if err != nil {
					return fmt.Errorf("gml:%d: TableModelColumn display: %v", a.line, err)
				}
				m.columns = append(m.columns, str(v))
			}
		}
	}
	for _, a := range n.attrs {
		if a.name == "rows" && a.expr != nil {
			v, err := a.expr.eval(env)
			if err != nil {
				return fmt.Errorf("gml:%d: TableModel rows: %v", a.line, err)
			}
			arr, ok := v.([]any)
			if !ok {
				return fmt.Errorf("gml:%d: TableModel rows wants an array", a.line)
			}
			m.rows = nil
			for _, item := range arr {
				if row, ok := item.(map[string]any); ok {
					m.rows = append(m.rows, row)
				}
			}
		}
	}
	// Nil-guarded: module.go's singleton scratch docState is a bare literal.
	if doc.tableModels == nil {
		doc.tableModels = map[string]*tableModel{}
	}
	doc.tableModels[id] = m
	env.vars[id] = m
	return nil
}

// call dispatches TableModel's runtime-mutation methods (EditableTableCard:
// appendRow/removeRow/setRow, plus getRow for reads).
func (m *tableModel) call(name string, argExprs []expr, env *Env, line int) (any, error) {
	args, err := evalArgs(argExprs, env)
	if err != nil {
		return nil, err
	}
	rowArg := func(i int) map[string]any {
		if i < len(args) {
			if r, ok := args[i].(map[string]any); ok {
				return r
			}
		}
		return nil
	}
	switch name {
	case "appendRow":
		if r := rowArg(0); r != nil {
			m.rows = append(m.rows, r)
		}
		core.BumpRevision()
		return nil, nil
	case "removeRow":
		if len(args) >= 1 {
			if i, err := toInt(args[0]); err == nil && i >= 0 && i < len(m.rows) {
				m.rows = append(m.rows[:i], m.rows[i+1:]...)
			}
		}
		core.BumpRevision()
		return nil, nil
	case "setRow":
		if len(args) >= 2 {
			if i, err := toInt(args[0]); err == nil && i >= 0 && i < len(m.rows) {
				if r := rowArg(1); r != nil {
					m.rows[i] = r
				}
			}
		}
		core.BumpRevision()
		return nil, nil
	case "getRow":
		if len(args) >= 1 {
			if i, err := toInt(args[0]); err == nil && i >= 0 && i < len(m.rows) {
				return m.rows[i], nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("gml:%d: TableModel has no method %q", line, name)
}

// resolveTableModel reads a TableView's model: attribute to a tableSource —
// a declarative TableModel, or any object exposing the "table" contract
// (Go-registered models and proxies).
func resolveTableModel(a attr, env *Env) (tableSource, error) {
	v, err := resolveAny(a.expr, env)
	if err != nil {
		return nil, err
	}
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	switch m := v.(type) {
	case *tableModel:
		return m, nil
	case *elemObject:
		if m.props != nil {
			if _, has := m.props["table"]; has {
				return &objTable{props: m.props}, nil
			}
		}
	case *NativeObject:
		if _, has := m.Props["table"]; has {
			return &objTable{props: m.Props}, nil
		}
	}
	return nil, fmt.Errorf("gml:%d: TableView model wants a TableModel or an object with table/headers props", a.line)
}

// tableColumnWidths evaluates columnWidthProvider (a JS function of the
// column index) per column, defaulting to 120 where absent or failing.
func tableColumnWidths(provider any, cols int, env *Env, line int) []float32 {
	widths := make([]float32, cols)
	for c := range widths {
		widths[c] = 120
		if provider == nil {
			continue
		}
		v, err := callValue(provider, []expr{&litExpr{float64(c)}}, env, line, "columnWidthProvider")
		if err != nil {
			continue
		}
		if f, err := toFloat32(v); err == nil && f > 0 {
			widths[c] = f
		}
	}
	return widths
}

// chooseDelegate resolves Qt.labs.qmlmodels' DelegateChooser for one cell:
// the first DelegateChoice whose column/row criteria match wins; a choice
// with no criteria is the fallback. A plain delegate passes through.
func chooseDelegate(delegate *node, row, col int, env *Env) *node {
	if delegate == nil || delegate.typeName != "DelegateChooser" {
		return delegate
	}
	for _, ch := range delegate.children {
		if ch.typeName != "DelegateChoice" {
			continue
		}
		match := true
		for _, a := range ch.attrs {
			want := -1
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if f, err := toFloat32(v); err == nil {
						want = int(f)
					}
				}
			}
			switch a.name {
			case "column":
				if want != col {
					match = false
				}
			case "row":
				if want != row {
					match = false
				}
			}
		}
		if !match {
			continue
		}
		// The choice's delegate: attribute (element-valued) or single child.
		for _, a := range ch.attrs {
			if a.name == "delegate" && a.elem != nil {
				return a.elem
			}
		}
		for _, c := range ch.children {
			if !isNonVisual(c) {
				return c
			}
		}
	}
	return nil
}

// buildTableView renders the cell grid: one delegate instance per cell,
// scrollable, with fixed column widths from columnWidthProvider.
func buildTableView(n *node, env *Env, doc *docState) (ui.Element, error) {
	var model tableSource
	var provider any
	var height float32
	selID := "" // ItemSelectionModel element id (selectionModel: <id>)
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "model":
			m, err := resolveTableModel(a, env)
			if err != nil {
				return nil, err
			}
			model = m
		case "columnWidthProvider":
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					provider = v
				}
			}
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			height = f
		case "selectionModel":
			// `selectionModel: <id>` names a declared ItemSelectionModel;
			// an inline `ItemSelectionModel { }` element enables selection
			// without exposing state anywhere.
			if id, ok := a.expr.(*identExpr); ok {
				selID = id.name
			} else if a.elem != nil {
				selID = nodeID(a.elem)
			}
		case "delegate":
			// consumed by delegateChild (element-valued)
		case "width", "clip", "boundsBehavior", "interactive", "reuseItems",
			"selectionBehavior", "selectionMode",
			"editTriggers", "rowSpacing", "columnSpacing", "alternatingRows",
			"columnWidthProviderInvalidated", "syncDirection":
			// accepted, not modeled (cell editing is TODO)
		default:
			return nil, fmt.Errorf("gml:%d: TableView has no attribute %q", a.line, a.name)
		}
	}
	// An inline ItemSelectionModel child also enables selection.
	if selID == "" {
		for _, c := range n.children {
			if c.typeName == "ItemSelectionModel" {
				selID = nodeID(c)
				break
			}
		}
	}
	if model == nil {
		return nil, fmt.Errorf("gml:%d: TableView needs a model", n.line)
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	rows, cols := model.tableDims()
	doc.tableInfo[nodeID(n)] = tableInfo{model: model, provider: provider}
	doc.exposeIDProp(n, "rows", float64(rows))
	doc.exposeIDProp(n, "columns", float64(cols))
	widths := tableColumnWidths(provider, cols, env, n.line)

	viewKey := doc.key(n)
	if doc.tables == nil {
		doc.tables = map[string]*ui.TableState{}
	}
	st := doc.tables[viewKey]
	if st == nil {
		st = &ui.TableState{}
		doc.tables[viewKey] = st
	}
	// Row selection (SelectRows semantics — the corpus default): a tap on
	// any cell selects its row, delegates see live `selected`/`current`,
	// and the linked ItemSelectionModel reflects it (hasSelection,
	// selectedIndexes[i].row, clearSelection()).
	selRow := st.Selected()
	if selRow >= rows {
		selRow = -1 // ui.TableView drops it too
	}
	if selID != "" {
		if obj := doc.elemObjs.get(doc.intern(selID)); obj != nil {
			if obj.props == nil {
				obj.props = map[string]any{}
			}
			obj.setProp("hasSelection", selRow >= 0)
			idx := []any{}
			if selRow >= 0 {
				idx = append(idx, map[string]any{"row": float64(selRow), "column": float64(0)})
			}
			obj.setProp("selectedIndexes", idx)
			if obj.methods == nil {
				obj.methods = map[string]any{
					"clearSelection":  jsFunc(func([]any) (any, error) { st.Select(-1); return nil, nil }),
					"select":          jsFunc(func([]any) (any, error) { return nil, nil }),
					"setCurrentIndex": jsFunc(func([]any) (any, error) { return nil, nil }),
				}
			}
		}
	}
	// The table itself is ui.TableView's; each cell is its delegate (chosen
	// per cell) in the cell's own scope. In-place editing is
	// TableView.editDelegate: QML's default edit trigger is a double tap;
	// with single clicks only, a click on the selected row's cell starts
	// the edit — the same two-step gesture. The editor builds inside the
	// cell's scope and covers the cell.
	var editorEl ui.Element
	o := ui.TableOpts{Rows: rows, Cols: cols, ColWidths: widths, H: height, Selectable: selID != ""}
	o.Cell = func(r, c int, selected, editing bool) ui.Element {
		cellDelegate := chooseDelegate(delegate, r, c, env)
		if cellDelegate == nil {
			return nil
		}
		editor := attachedElem(cellDelegate, "TableView.editDelegate")
		editing = editing && editor != nil
		cell := map[string]any{
			"row": float64(r), "column": float64(c),
			"display":  model.cellDisplay(r, c),
			"selected": selected, "current": selected, "editing": editing,
		}
		restore := enterRow(viewKey, cellDelegate, env, doc, cell, r*cols+c)
		defer restore()
		el, err := build(cellDelegate, env, doc)
		if err == nil && editing {
			editorEl, err = buildCellEditor(editor, model, r, c, env, doc, viewKey, st.StopEditing)
		}
		if err != nil {
			el = ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		return el
	}
	o.CanEdit = func(r, c int) bool {
		d := chooseDelegate(delegate, r, c, env)
		return d != nil && attachedElem(d, "TableView.editDelegate") != nil
	}
	o.Editor = func(int, int) ui.Element { return editorEl }
	return ui.TableView(st, o), nil
}

// tableInfo is what a HorizontalHeaderView needs from its syncView.
type tableInfo struct {
	model    tableSource
	provider any
}

// buildHeaderView renders HorizontalHeaderView: the delegate once per column
// of the synced TableView, at that table's column widths. The table registers
// its info during declaration-independent build; syncView resolves through
// the declared TableView node registry so header-before-table order works.
func buildHeaderView(n *node, env *Env, doc *docState) (ui.Element, error) {
	syncID := ""
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "syncView":
			if id, ok := a.expr.(*identExpr); ok {
				syncID = id.name
			}
		case "delegate":
			// consumed by delegateChild (element-valued)
		case "width", "height", "clip", "boundsBehavior", "interactive":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: HorizontalHeaderView has no attribute %q", a.line, a.name)
		}
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	info, ok := doc.tableInfo[syncID]
	if !ok {
		// The synced table hasn't built yet (header declared first): read its
		// model/provider straight off the registered TableView node.
		tn := doc.tableNodes[syncID]
		if tn == nil {
			return nil, fmt.Errorf("gml:%d: HorizontalHeaderView syncView %q is not a TableView", n.line, syncID)
		}
		for _, a := range tn.attrs {
			switch a.name {
			case "model":
				if m, err := resolveTableModel(a, env); err == nil {
					info.model = m
				}
			case "columnWidthProvider":
				if a.expr != nil {
					if v, err := a.expr.eval(env); err == nil {
						info.provider = v
					}
				}
			}
		}
		if info.model == nil {
			return nil, fmt.Errorf("gml:%d: syncView %q has no resolvable model", n.line, syncID)
		}
	}
	_, cols := info.model.tableDims()
	widths := tableColumnWidths(info.provider, cols, env, n.line)
	// The strip is ui.HeaderView's, at the table's column widths; each cell
	// is the delegate in its column's scope.
	viewKey := doc.key(n)
	return ui.HeaderView(cols, widths, nil, func(c int) ui.Element {
		cell := map[string]any{"column": float64(c), "display": info.model.headerDisplay(c)}
		restore := enterRow(viewKey, delegate, env, doc, cell, c)
		defer restore()
		el, err := build(delegate, env, doc)
		if err != nil {
			return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		return el
	}), nil
}

// attachedElem returns an element-valued attached attribute of a node
// (`TableView.editDelegate: TextField { ... }`), or nil.
func attachedElem(n *node, name string) *node {
	for _, a := range n.attrs {
		if a.name == name {
			return a.elem
		}
	}
	return nil
}

// buildCellEditor builds a cell's TableView.editDelegate in the cell's row
// scope, with the QML editing contract: `edit` is a writable property
// holding the pending value, and `TableView.commit()` writes it into the
// model and closes the editor (closeEditor just closes).
func buildCellEditor(editor *node, model tableSource, r, c int, env *Env, doc *docState,
	viewKey string, stop func()) (ui.Element, error) {
	key := fmt.Sprintf("%s|edit:%d:%d", viewKey, r, c)
	editRef, ok := doc.synthRefs[key]
	if !ok {
		var err error
		editRef, err = newDeclaredProp("var", model.cellDisplay(r, c))
		if err != nil {
			return nil, err
		}
		doc.synthRefs[key] = editRef
	}
	close := func() {
		stop()
		delete(doc.synthRefs, key) // next edit starts from the model again
	}
	commit := func() {
		if w, ok := model.(tableWriter); ok {
			w.setCell(r, c, editRef.get())
		}
		close()
	}
	// `TableView` inside the editor is the attached object: the enum
	// namespace plus the editing methods.
	tv := map[string]any{
		"commit":      jsFunc(func([]any) (any, error) { commit(); return nil, nil }),
		"closeEditor": jsFunc(func([]any) (any, error) { close(); return nil, nil }),
	}
	if ns, ok := env.vars["TableView"].(map[string]any); ok {
		for k, v := range ns {
			if _, taken := tv[k]; !taken {
				tv[k] = v
			}
		}
	}
	prevTV, hadTV := env.vars["TableView"]
	prevEdit, hadEdit := env.vars["edit"]
	env.vars["TableView"] = tv
	env.vars["edit"] = editRef
	defer func() {
		if hadTV {
			env.vars["TableView"] = prevTV
		} else {
			delete(env.vars, "TableView")
		}
		if hadEdit {
			env.vars["edit"] = prevEdit
		} else {
			delete(env.vars, "edit")
		}
	}()
	return build(editor, env, doc)
}
