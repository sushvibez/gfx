//go:build linux || windows

package main

// Go implementations of the QML-Examples-Collection corpus' C++-registered
// types (gml.RegisterType — the qmlRegisterType equivalent). These are the
// dogfooding proof that .qml talks to Go backends: properties are live
// core.Property bindings, methods are invokable from markup, and signals
// fired from Go goroutines drive the page's on<Signal> handlers.

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/script/gml"
)

func registerNativeTypes() {
	registerEthernetController()
	registerThreadPipeline()
	registerTheoryParser()
	registerFileSystemTreeModel()
	registerOrganizationTreeModel()
	registerDatabaseManager()
	registerSqlQueryModel()
	registerDatabaseTableModels() // extends DatabaseManager with real SQL
	registerSettingsManager()
	registerPaintedTypes()
	registerBridges()
	registerAsyncTask()
	registerAsyncComputer()
	registerEmployeeModel()
	registerEmployeeProxyModel()
	// WebSocketClient (examples/websocket): a simulated echo server —
	// connectToServer/sendMessage/disconnectFromServer methods, live
	// connected/statusText properties, and messageSent/messageReceived/
	// errorOccurred signals. The echo reply arrives from a goroutine,
	// exercising the queued cross-thread emission path.
	gml.RegisterType("WebSocketClient", func(emit gml.EmitFunc) *gml.NativeObject {
		connected := &core.Property[bool]{}
		statusText := &core.Property[string]{}
		statusText.Set("Disconnected")
		msgCount := 0
		return &gml.NativeObject{
			Props: map[string]any{
				"connected":  gml.BindBool(connected),
				"statusText": gml.BindString(statusText),
			},
			Methods: map[string]any{
				"connectToServer": func() {
					connected.Set(true)
					statusText.Set("Connected (Go echo backend)")
					emit("connected")
					emit("messageReceived", "Welcome! This echo server is a Go backend (gml.RegisterType).")
				},
				"disconnectFromServer": func() {
					connected.Set(false)
					statusText.Set("Disconnected")
					emit("disconnected")
				},
				"sendMessage": func(msg string) {
					if !connected.Get() {
						emit("errorOccurred", "not connected")
						return
					}
					msgCount++
					emit("messageSent", msg)
					n := msgCount
					go func() { // echo replies arrive asynchronously
						time.Sleep(150 * time.Millisecond)
						emit("messageReceived", fmt.Sprintf("echo #%d: %s", n, msg))
					}()
				},
			},
		}
	})
}

// EthernetController (examples/ethernet): a simulated STANAG-4586-ish
// point-to-point link. Sending loops a message back to itself with a
// short delay, incrementing the counters the ConnectionCard displays.
func registerEthernetController() {
	gml.RegisterType("EthernetController", func(emit gml.EmitFunc) *gml.NativeObject {
		bound := &core.Property[bool]{}
		statusText := &core.Property[string]{}
		statusText.Set("Disconnected")
		sent := &core.Property[float32]{}
		received := &core.Property[float32]{}
		errors := &core.Property[float32]{}
		listenPort := &core.Property[float32]{}
		listenPort.Set(4586)
		sendPort := &core.Property[float32]{}
		sendPort.Set(4587)

		return &gml.NativeObject{
			Props: map[string]any{
				"bound":         gml.BindBool(bound),
				"statusText":    gml.BindString(statusText),
				"sentCount":     gml.BindFloat(sent),
				"receivedCount": gml.BindFloat(received),
				"errorCount":    gml.BindFloat(errors),
				"listenPort":    gml.BindFloat(listenPort),
				"sendPort":      gml.BindFloat(sendPort),
			},
			Methods: map[string]any{
				"getFieldDefinitions": func([]any) (any, error) {
					// STANAG 4586-ish bitmask fields (presenceMask bit order).
					names := []string{"Latitude", "Longitude", "Altitude", "Heading",
						"Speed", "Timestamp", "VehicleID", "PayloadStatus"}
					out := make([]any, len(names))
					for i, name := range names {
						out[i] = map[string]any{"name": name}
					}
					return out, nil
				},
				"startListening": func() {
					bound.Set(true)
					statusText.Set(fmt.Sprintf("Listening on %d (Go backend)", int(listenPort.Get())))
				},
				"stopListening": func() {
					bound.Set(false)
					statusText.Set("Disconnected")
				},
				"sendMessage": func(args []any) (any, error) {
					if !bound.Get() {
						emit("protocolError", nowStr(), "not listening", "")
						return nil, nil
					}
					msgID, presence, fields := 0.0, 0.0, []any(nil)
					if len(args) > 0 {
						msgID, _ = args[0].(float64)
					}
					if len(args) > 1 {
						presence, _ = args[1].(float64)
					}
					if len(args) > 2 {
						fields, _ = args[2].([]any)
					}
					hex := fmt.Sprintf("%04X%04X%02X", int(msgID), int(presence), len(fields))
					sent.Set(sent.Get() + 1)
					emit("messageSent", nowStr(), hex, msgID, float64(len(fields)), float64(len(hex)/2))
					n := sent.Get()
					go func() { // loopback arrives asynchronously, like real UDP
						time.Sleep(120 * time.Millisecond)
						received.Set(received.Get() + 1)
						emit("messageReceived", nowStr(), hex, msgID, float64(sendPort.Get()),
							float64(len(fields)), float64(len(hex)/2), true)
						emit("fieldsDecoded", fields)
						_ = n
					}()
					return nil, nil
				},
			},
		}
	})
}

func nowStr() string { return time.Now().Format("15:04:05.000") }

// ThreadPipeline (examples/threads): a simulated 3-stage worker pipeline
// (generator -> filter -> collector) driven by a ticker goroutine, standing
// in for the corpus' QThread + moveToThread demo.
func registerThreadPipeline() {
	gml.RegisterType("ThreadPipeline", func(emit gml.EmitFunc) *gml.NativeObject {
		running := &core.Property[bool]{}
		generated := &core.Property[float32]{}
		processed := &core.Property[float32]{}
		matched := &core.Property[float32]{}
		interval := &core.Property[float32]{}
		interval.Set(200)
		pattern := &core.Property[string]{}
		pattern.Set("DEADBEEF")
		stop := make(chan struct{})

		runLoop := func() {
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for {
				ms := int(interval.Get())
				if ms <= 0 {
					ms = 200
				}
				select {
				case <-stop:
					return
				case <-time.After(time.Duration(ms) * time.Millisecond):
				}
				buf := make([]byte, 8)
				rng.Read(buf)
				generated.Set(generated.Get() + 1)
				processed.Set(processed.Get() + 1)
				hex := fmt.Sprintf("%X", buf)
				if strings.Contains(hex, pattern.Get()[:min(4, len(pattern.Get()))]) || rng.Intn(6) == 0 {
					matched.Set(matched.Get() + 1)
					emit("recordAdded", nowStr(), hex, float64(len(buf)))
				}
			}
		}

		return &gml.NativeObject{
			Props: map[string]any{
				"running":            gml.BindBool(running),
				"generatedCount":     gml.BindFloat(generated),
				"processedCount":     gml.BindFloat(processed),
				"matchedCount":       gml.BindFloat(matched),
				"generationInterval": gml.BindFloat(interval),
				"filterPatternHex":   gml.BindString(pattern),
			},
			Methods: map[string]any{
				"start": func() {
					if running.Get() {
						return
					}
					running.Set(true)
					stop = make(chan struct{})
					go runLoop()
				},
				"stop": func() {
					if !running.Get() {
						return
					}
					running.Set(false)
					close(stop)
				},
				"clear": func() {
					generated.Set(0)
					processed.Set(0)
					matched.Set(0)
				},
				"setFilterPattern": func(p string) { pattern.Set(p) },
			},
		}
	})
}

// TheoryParser (examples/theorycpp): a simulated documentation source —
// a fixed chapter/topic tree plus canned explanation HTML and code
// sections, standing in for the corpus' C++ markdown/code parser.
func registerTheoryParser() {
	gml.RegisterType("TheoryParser", func(emit gml.EmitFunc) *gml.NativeObject {
		chapters := []any{
			map[string]any{"name": "properties", "displayName": "Properties & Signals", "topics": []any{
				map[string]any{"file": "intro", "displayName": "Property basics"},
				map[string]any{"file": "bindings", "displayName": "Dependency tracking"},
			}},
			map[string]any{"name": "rendering", "displayName": "Rendering", "topics": []any{
				map[string]any{"file": "sdf", "displayName": "SDF quads"},
				map[string]any{"file": "batching", "displayName": "Instanced batching"},
			}},
			map[string]any{"name": "gml", "displayName": "gml / QML compat", "topics": []any{
				map[string]any{"file": "native", "displayName": "Go-registered types"},
			}},
		}
		return &gml.NativeObject{
			Props: map[string]any{"chapters": chapters},
			Methods: map[string]any{
				"getExplanationHtml": func(args []any) (any, error) {
					chapterDir, topicFile := argStr(args, 0), argStr(args, 1)
					return fmt.Sprintf(
						"This is the %s / %s topic, served by a Go-registered "+
							"NativeObject (gml.RegisterType) standing in for the "+
							"corpus' C++ TheoryParser. No native plugin required.",
						chapterDir, topicFile), nil
				},
				"getCodeSections": func(args []any) (any, error) {
					chapterDir, topicFile := argStr(args, 0), argStr(args, 1)
					return []any{
						map[string]any{
							"title":  "Registering a type",
							"code":   "gml.RegisterType(\"" + chapterDir + "\", factory)",
							"result": "Instantiable from markup: " + chapterDir + " { id: x }",
						},
						map[string]any{
							"title":  "Reading this topic",
							"code":   "parser.getExplanationHtml(\"" + chapterDir + "\", \"" + topicFile + "\", ...)",
							"result": "Called from ContentPanel.loadContent()",
						},
					}, nil
				},
			},
		}
	})
}

func argStr(args []any, i int) string {
	if i < len(args) {
		if s, ok := args[i].(string); ok {
			return s
		}
	}
	return ""
}

// FileSystemTreeModel (examples/treeview): a Go directory lister backing
// a file browser. The corpus page itself binds this to a real TreeView
// (not yet implemented — see script/gml/check/QML-EXAMPLES.md), so this
// is proven via examples/qml/filesystem.gml's ListView-based browser
// instead of the gated corpus card.
// fsTreeNode builds the nested tree the TreeView control consumes
// ({roles..., children: [...]}) from a real directory, capped so a huge
// checkout doesn't explode the model. Roles match the corpus delegate:
// fileName, isFolder, fileSize.
func fsTreeNode(path string, depth int) []any {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	const maxEntries = 12
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		node := map[string]any{
			"fileName": e.Name(),
			"isFolder": e.IsDir(),
			"fileSize": "",
		}
		if e.IsDir() {
			if depth < 3 {
				node["children"] = fsTreeNode(filepath.Join(path, e.Name()), depth+1)
			}
		} else if info, err := e.Info(); err == nil {
			node["fileSize"] = fmt.Sprintf("%.1f KB", float64(info.Size())/1024)
		}
		out = append(out, node)
	}
	return out
}

func countTreeNodes(nodes []any) int {
	n := 0
	for _, item := range nodes {
		if node, ok := item.(map[string]any); ok {
			n++
			if children, ok := node["children"].([]any); ok {
				n += countTreeNodes(children)
			}
		}
	}
	return n
}

func registerFileSystemTreeModel() {
	gml.RegisterType("FileSystemTreeModel", func(emit gml.EmitFunc) *gml.NativeObject {
		tree := fsTreeNode(".", 0)
		return &gml.NativeObject{
			Props: map[string]any{
				"tree":       tree,
				"totalNodes": float64(countTreeNodes(tree)),
			},
			Methods: map[string]any{
				"listDir": func(args []any) (any, error) {
					path := argStr(args, 0)
					if path == "" {
						path = "."
					}
					entries, err := os.ReadDir(path)
					if err != nil {
						return nil, err
					}
					out := make([]any, 0, len(entries))
					for _, e := range entries {
						size := float64(0)
						if info, err := e.Info(); err == nil {
							size = float64(info.Size())
						}
						out = append(out, map[string]any{
							"name":  e.Name(),
							"isDir": e.IsDir(),
							"size":  size,
							"path":  filepath.Join(path, e.Name()),
						})
					}
					return out, nil
				},
				"parentDir": func(args []any) (any, error) {
					return filepath.Dir(argStr(args, 0)), nil
				},
			},
		}
	})
}

// registerOrganizationTreeModel stands in for the corpus' C++
// OrganizationTreeModel (treeview page): a static org chart with the
// delegate's roles (name, title, department) as a nested tree.
func registerOrganizationTreeModel() {
	gml.RegisterType("OrganizationTreeModel", func(emit gml.EmitFunc) *gml.NativeObject {
		person := func(name, title, dept string, children ...any) map[string]any {
			n := map[string]any{"name": name, "title": title, "department": dept}
			if len(children) > 0 {
				n["children"] = children
			}
			return n
		}
		tree := []any{
			person("Dana Reyes", "CEO", "Executive",
				person("Miguel Ortiz", "VP Engineering", "Engineering",
					person("Ada Lovelace", "Staff Engineer", "Engineering"),
					person("Grace Hopper", "Compiler Lead", "Engineering"),
					person("Radia Perlman", "Network Architect", "Engineering")),
				person("Priya Shah", "VP Design", "Design",
					person("Saul Bass", "Brand Designer", "Design"),
					person("Susan Kare", "Icon Designer", "Design")),
				person("Tom Muller", "CFO", "Finance",
					person("Ken Iverson", "Analyst", "Finance"))),
		}
		return &gml.NativeObject{
			Props: map[string]any{
				"tree":      tree,
				"nodeCount": float64(countTreeNodes(tree)),
			},
		}
	})
}

// employee is DatabaseManager's in-memory row shape — a plain Go slice
// standing in for the corpus' sqlite-backed model (swapping in
// modernc.org/sqlite, pure Go and already used elsewhere on this stack, is a
// mechanical change once a real TableView control exists to justify it).
type employee struct {
	id         int
	name, role string
	salary     float64
}

var employees = []employee{
	{1, "Ada Lovelace", "Engineer", 128000},
	{2, "Grace Hopper", "Engineer", 132000},
	{3, "Radia Perlman", "Architect", 145000},
}

// DatabaseManager (examples/database, examples/tableview): a query/insert
// API over the employees table. Gated the same way as
// FileSystemTreeModel — the corpus pages bind this to TableView (not
// implemented); examples/qml/database.gml proves it via ListView.
func registerDatabaseManager() {
	gml.RegisterType("DatabaseManager", func(emit gml.EmitFunc) *gml.NativeObject {
		rowMaps := func() []any {
			out := make([]any, len(employees))
			for i, e := range employees {
				out[i] = map[string]any{
					"id": float64(e.id), "name": e.name,
					"role": e.role, "salary": e.salary,
				}
			}
			return out
		}
		return &gml.NativeObject{
			Methods: map[string]any{
				"queryEmployees": func([]any) (any, error) { return rowMaps(), nil },
				"addEmployee": func(args []any) (any, error) {
					name, role := argStr(args, 0), argStr(args, 1)
					salary := 0.0
					if len(args) > 2 {
						if f, ok := args[2].(float64); ok {
							salary = f
						}
					}
					id := len(employees) + 1
					employees = append(employees, employee{id, name, role, salary})
					emit("employeeAdded", float64(id), name)
					return rowMaps(), nil
				},
			},
		}
	})
}

// ---- EmployeeModel / EmployeeProxyModel (examples/tableview) -------------

// employeeTable is the shared backing store for EmployeeModel and
// EmployeeProxyModel — the corpus wires the proxy's sourceModel to the
// model in QML, but both Go stand-ins are registered here so they simply
// share state (a page instantiates one of each). refresh fns re-derive
// every registered view's "table" prop after a mutation.
type employeeTable struct {
	rows       [][]any // id, name, department, salary, fulltime
	nextID     int
	refreshers []func()
}

var empStore = &employeeTable{
	rows: [][]any{
		{float64(1), "Ada Lovelace", "Engineering", float64(128000), true},
		{float64(2), "Grace Hopper", "Engineering", float64(132000), true},
		{float64(3), "Radia Perlman", "Networking", float64(145000), true},
		{float64(4), "Susan Kare", "Design", float64(98000), false},
		{float64(5), "Ken Iverson", "Finance", float64(87000), true},
		{float64(6), "Saul Bass", "Design", float64(91000), false},
		{float64(7), "Dana Reyes", "Executive", float64(210000), true},
		{float64(8), "Miguel Ortiz", "Engineering", float64(158000), true},
	},
	nextID: 9,
}

var empHeaders = []any{"ID", "Name", "Department", "Salary", "Full-time"}

func (t *employeeTable) mutate(fn func()) {
	fn()
	for _, r := range t.refreshers {
		r()
	}
	core.BumpRevision()
}

func (t *employeeTable) asAny() []any {
	out := make([]any, len(t.rows))
	for i, r := range t.rows {
		out[i] = append([]any(nil), r...)
	}
	return out
}

// registerEmployeeModel is the QAbstractTableModel stand-in: the raw table
// plus addEmployee/removeEmployee invokables and a live count.
func registerEmployeeModel() {
	gml.RegisterType("EmployeeModel", func(emit gml.EmitFunc) *gml.NativeObject {
		obj := &gml.NativeObject{
			Props: map[string]any{
				"headers": empHeaders,
			},
			Methods: map[string]any{},
		}
		refresh := func() {
			obj.Props["table"] = empStore.asAny()
			obj.Props["count"] = float64(len(empStore.rows))
		}
		refresh()
		empStore.refreshers = append(empStore.refreshers, refresh)
		obj.Methods["addEmployee"] = func(args []any) (any, error) {
			name, dept := argStr(args, 0), argStr(args, 1)
			salary, fulltime := 100000.0, true
			if len(args) > 2 {
				if f, ok := args[2].(float64); ok {
					salary = f
				}
			}
			if len(args) > 3 {
				if b, ok := args[3].(bool); ok {
					fulltime = b
				}
			}
			empStore.mutate(func() {
				empStore.rows = append(empStore.rows,
					[]any{float64(empStore.nextID), name, dept, salary, fulltime})
				empStore.nextID++
			})
			emit("employeeAdded", name)
			return nil, nil
		}
		obj.Methods["removeEmployee"] = func(row float64) {
			i := int(row)
			empStore.mutate(func() {
				if i >= 0 && i < len(empStore.rows) {
					empStore.rows = append(empStore.rows[:i], empStore.rows[i+1:]...)
				}
			})
		}
		return obj
	})
}

// registerEmployeeProxyModel is the QSortFilterProxyModel stand-in: its
// "table" is the source rows filtered by filterText (name or department,
// case-insensitive) and sorted by toggleSort's column/order.
func registerEmployeeProxyModel() {
	gml.RegisterType("EmployeeProxyModel", func(emit gml.EmitFunc) *gml.NativeObject {
		var filter core.Property[string]
		sortCol, ascending := -1, true
		obj := &gml.NativeObject{
			Props: map[string]any{
				"headers":    empHeaders,
				"filterText": gml.BindString(&filter),
			},
			Methods: map[string]any{},
		}
		refresh := func() {
			rows := empStore.asAny()
			if q := strings.ToLower(strings.TrimSpace(filter.Get())); q != "" {
				kept := rows[:0]
				for _, r := range rows {
					row := r.([]any)
					if strings.Contains(strings.ToLower(row[1].(string)), q) ||
						strings.Contains(strings.ToLower(row[2].(string)), q) {
						kept = append(kept, r)
					}
				}
				rows = kept
			}
			if sortCol >= 0 {
				sort.SliceStable(rows, func(i, j int) bool {
					a, b := rows[i].([]any)[sortCol], rows[j].([]any)[sortCol]
					less := false
					switch av := a.(type) {
					case float64:
						bv, _ := b.(float64)
						less = av < bv
					case string:
						bv, _ := b.(string)
						less = strings.ToLower(av) < strings.ToLower(bv)
					case bool:
						bv, _ := b.(bool)
						less = !av && bv
					}
					if !ascending {
						return !less
					}
					return less
				})
			}
			obj.Props["table"] = rows
			obj.Props["sortColumn"] = float64(sortCol)
			// Qt::AscendingOrder = 0, DescendingOrder = 1.
			order := float64(0)
			if !ascending {
				order = 1
			}
			obj.Props["currentSortOrder"] = order
			obj.Props["count"] = float64(len(rows))
		}
		refresh()
		empStore.refreshers = append(empStore.refreshers, refresh)
		filter.Changed().Connect(func(string) { refresh() })
		obj.Methods["toggleSort"] = func(col float64) {
			c := int(col)
			if c == sortCol {
				ascending = !ascending
			} else {
				sortCol, ascending = c, true
			}
			refresh()
			core.BumpRevision()
		}
		return obj
	})
}
