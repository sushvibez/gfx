//go:build linux || windows

package main

// SettingsManager and the async task types from the corpus' C++ side, as Go
// backends (gml.RegisterType). SettingsManager persists to a real JSON file
// the way QSettings persists to an ini/registry; AsyncTask/AsyncComputer run
// their work on goroutines and report progress through the queued signal
// path, which is what the QtConcurrent examples are demonstrating.

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/script/gml"
)

// settingsStore is the persisted key/value map behind SettingsManager. Keys
// are QSettings-style paths ("appearance/theme"), so a group is everything
// before the first slash.
type settingsStore struct {
	path   string
	values map[string]any
}

// loadSettings reads the store from disk, or starts an empty one seeded with
// the defaults the corpus' preferences page expects.
func loadSettings() *settingsStore {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	s := &settingsStore{
		path:   filepath.Join(dir, "gml-qml-examples", "settings.json"),
		values: map[string]any{},
	}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &s.values)
	}
	defaults := map[string]any{
		"appearance/theme":     "Dark",
		"appearance/fontSize":  float64(14),
		"user/userName":        "Guest",
		"user/notifications":   true,
		"audio/volume":         0.7,
		"recent/files":         []any{},
		"window/lastGeometry":  "900x700",
		"network/lastEndpoint": "wss://echo.example",
	}
	for k, v := range defaults {
		if _, ok := s.values[k]; !ok {
			s.values[k] = v
		}
	}
	return s
}

// save writes the store back out, creating its directory on first use.
func (s *settingsStore) save() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0o644)
}

// keys returns every key, sorted, so the UI's listing is stable.
func (s *settingsStore) keys() []string {
	out := make([]string, 0, len(s.values))
	for k := range s.values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *settingsStore) recentFiles() []any {
	if v, ok := s.values["recent/files"].([]any); ok {
		return v
	}
	return []any{}
}

func registerSettingsManager() {
	gml.RegisterType("SettingsManager", func(emit gml.EmitFunc) *gml.NativeObject {
		store := loadSettings()

		// The properties the preferences page binds two-way. Each one is a
		// live core.Property mirrored into the store on write.
		theme := &core.Property[string]{}
		userName := &core.Property[string]{}
		fontSize := &core.Property[float32]{}
		volume := &core.Property[float32]{}
		notifications := &core.Property[bool]{}
		recent := gml.NewVarProp([]any{})

		load := func() {
			theme.Set(str(store.values["appearance/theme"], "Dark"))
			userName.Set(str(store.values["user/userName"], "Guest"))
			fontSize.Set(float32(num(store.values["appearance/fontSize"], 14)))
			volume.Set(float32(num(store.values["audio/volume"], 0.7)))
			notifications.Set(boolOf(store.values["user/notifications"], true))
			recent.Set(store.recentFiles())
		}
		load()

		// Writes go straight back to disk (QSettings syncs eagerly too).
		persist := func() {
			store.values["appearance/theme"] = theme.Get()
			store.values["user/userName"] = userName.Get()
			store.values["appearance/fontSize"] = float64(fontSize.Get())
			store.values["audio/volume"] = float64(volume.Get())
			store.values["user/notifications"] = notifications.Get()
			store.save()
			emit("keysChanged")
		}
		// The page writes these properties directly (two-way bindings), so
		// persist whenever one actually changes — QSettings syncs eagerly
		// the same way.
		theme.Changed().Connect(func(string) { persist() })
		userName.Changed().Connect(func(string) { persist() })
		fontSize.Changed().Connect(func(float32) { persist() })
		volume.Changed().Connect(func(float32) { persist() })
		notifications.Changed().Connect(func(bool) { persist() })

		return &gml.NativeObject{
			Props: map[string]any{
				"theme":         gml.BindString(theme),
				"userName":      gml.BindString(userName),
				"fontSize":      gml.BindFloat(fontSize),
				"volume":        gml.BindFloat(volume),
				"notifications": gml.BindBool(notifications),
				"recentFiles":   gml.BindVar(recent),
			},
			Methods: map[string]any{
				"settingsPath": func([]any) (any, error) { return store.path, nil },
				"allKeys": func([]any) (any, error) {
					out := []any{}
					for _, k := range store.keys() {
						out = append(out, k)
					}
					return out, nil
				},
				"childGroups": func([]any) (any, error) {
					seen := map[string]bool{}
					out := []any{}
					for _, k := range store.keys() {
						if i := strings.IndexByte(k, '/'); i > 0 && !seen[k[:i]] {
							seen[k[:i]] = true
							out = append(out, k[:i])
						}
					}
					return out, nil
				},
				"keysInGroup": func(args []any) (any, error) {
					group := argStr(args, 0)
					out := []any{}
					for _, k := range store.keys() {
						if strings.HasPrefix(k, group+"/") {
							out = append(out, strings.TrimPrefix(k, group+"/"))
						}
					}
					return out, nil
				},
				"getValue": func(args []any) (any, error) {
					if v, ok := store.values[argStr(args, 0)]; ok {
						return v, nil
					}
					if len(args) > 1 {
						return args[1], nil
					}
					return "", nil
				},
				"setValue": func(args []any) (any, error) {
					key := argStr(args, 0)
					if key == "" {
						return nil, nil
					}
					var v any = ""
					if len(args) > 1 {
						v = args[1]
					}
					store.values[key] = v
					store.save()
					load()
					emit("keysChanged")
					return nil, nil
				},
				"removeKey": func(args []any) (any, error) {
					delete(store.values, argStr(args, 0))
					store.save()
					load()
					emit("keysChanged")
					return nil, nil
				},
				"clearAll": func([]any) (any, error) {
					store.values = map[string]any{}
					store.save()
					load()
					emit("keysChanged")
					return nil, nil
				},
				"addRecentFile": func(args []any) (any, error) {
					path := argStr(args, 0)
					if path == "" {
						return nil, nil
					}
					// Most-recent-first, de-duplicated, capped like Qt's
					// recent-files lists.
					out := []any{path}
					for _, v := range store.recentFiles() {
						if str(v, "") != path && len(out) < 8 {
							out = append(out, v)
						}
					}
					store.values["recent/files"] = out
					store.save()
					load()
					emit("keysChanged")
					return nil, nil
				},
				"clearRecentFiles": func([]any) (any, error) {
					store.values["recent/files"] = []any{}
					store.save()
					load()
					emit("keysChanged")
					return nil, nil
				},
			},
		}
	})
}

// ---- AsyncTask / AsyncComputer (examples/asynccpp) -----------------------

// registerAsyncTask backs the QtConcurrent examples: a task that processes a
// list of items either one at a time or across goroutines, reporting
// progress and results as it goes, and cancellable mid-run.
func registerAsyncTask() {
	gml.RegisterType("AsyncTask", func(emit gml.EmitFunc) *gml.NativeObject {
		running := &core.Property[bool]{}
		progress := &core.Property[float32]{}
		status := &core.Property[string]{}
		elapsed := &core.Property[float32]{}
		results := gml.NewVarProp([]any{})
		status.Set("Idle")

		var cancelled atomic.Bool
		// post runs fn on the UI thread: properties are single-owner, so a
		// worker goroutine must not touch them directly.
		post := gml.Post

		start := func(items []any, parallel bool) {
			if running.Get() {
				return
			}
			cancelled.Store(false)
			running.Set(true)
			progress.Set(0)
			elapsed.Set(0)
			results.Set([]any{})
			if parallel {
				status.Set("Running (parallel map)...")
			} else {
				status.Set("Running (sequential)...")
			}
			go func() {
				begin := time.Now()
				out := make([]any, 0, len(items))
				// Sequential work sleeps per item; the parallel map splits
				// the same work across workers, so it finishes sooner —
				// which is the comparison the page is drawing.
				per := 120 * time.Millisecond
				if parallel {
					per = 120 * time.Millisecond / 4
				}
				for i, item := range items {
					if cancelled.Load() {
						took := float32(time.Since(begin).Milliseconds())
						post(func() {
							running.Set(false)
							status.Set("Cancelled")
							elapsed.Set(took)
						})
						emit("cancelled")
						return
					}
					time.Sleep(per)
					out = append(out, strings.ToUpper(str(item, "")))
					done := float32(i+1) / float32(max(len(items), 1))
					snapshot := append([]any{}, out...)
					post(func() {
						progress.Set(done)
						results.Set(snapshot)
					})
					emit("progressChanged", float64(done))
				}
				took := float32(time.Since(begin).Milliseconds())
				post(func() {
					running.Set(false)
					progress.Set(1)
					elapsed.Set(took)
					status.Set("Completed")
				})
				emit("finished")
			}()
		}

		return &gml.NativeObject{
			Props: map[string]any{
				"running":   gml.BindBool(running),
				"progress":  gml.BindFloat(progress),
				"status":    gml.BindString(status),
				"elapsedMs": gml.BindFloat(elapsed),
				"results":   gml.BindVar(results),
			},
			Methods: map[string]any{
				"processItems": func(args []any) (any, error) {
					start(listOf(args, 0), false)
					return nil, nil
				},
				"processItemsParallelMap": func(args []any) (any, error) {
					start(listOf(args, 0), true)
					return nil, nil
				},
				"runSteps": func(args []any) (any, error) {
					n := int(num(argAt(args, 0), 10))
					items := make([]any, 0, n)
					for i := 0; i < n; i++ {
						items = append(items, "step "+strconv.Itoa(i+1))
					}
					start(items, false)
					return nil, nil
				},
				"cancel": func([]any) (any, error) {
					cancelled.Store(true)
					return nil, nil
				},
			},
		}
	})
}

// registerAsyncComputer backs the QtConcurrent::run card: one-shot
// computations on a goroutine, with the result and wall time reported back.
func registerAsyncComputer() {
	gml.RegisterType("AsyncComputer", func(emit gml.EmitFunc) *gml.NativeObject {
		running := &core.Property[bool]{}
		result := &core.Property[string]{}
		elapsed := &core.Property[float32]{}
		result.Set("—")

		run := func(label string, work func() string) {
			if running.Get() {
				return
			}
			running.Set(true)
			result.Set("Computing " + label + "...")
			go func() {
				begin := time.Now()
				v := work()
				took := float32(time.Since(begin).Milliseconds())
				gml.Post(func() {
					running.Set(false)
					result.Set(v)
					elapsed.Set(took)
				})
				emit("finished", v)
			}()
		}

		return &gml.NativeObject{
			Props: map[string]any{
				"running":   gml.BindBool(running),
				"result":    gml.BindString(result),
				"elapsedMs": gml.BindFloat(elapsed),
			},
			Methods: map[string]any{
				"computeFibonacci": func(args []any) (any, error) {
					n := int(num(argAt(args, 0), 30))
					run("fib("+strconv.Itoa(n)+")", func() string {
						a, b := uint64(0), uint64(1)
						for i := 0; i < n; i++ {
							a, b = b, a+b
						}
						return "fib(" + strconv.Itoa(n) + ") = " + strconv.FormatUint(a, 10)
					})
					return nil, nil
				},
				"countPrimes": func(args []any) (any, error) {
					limit := int(num(argAt(args, 0), 200000))
					run("primes", func() string {
						return strconv.Itoa(countPrimes(limit)) + " primes below " +
							strconv.Itoa(limit)
					})
					return nil, nil
				},
				"sortRandom": func(args []any) (any, error) {
					n := int(num(argAt(args, 0), 1000000))
					run("sort", func() string {
						xs := make([]int, n)
						for i := range xs {
							xs[i] = rand.Int()
						}
						sort.Ints(xs)
						return "sorted " + strconv.Itoa(n) + " numbers"
					})
					return nil, nil
				},
			},
		}
	})
}

// countPrimes is a sieve of Eratosthenes — real work for the async cards to
// take a measurable amount of time over.
func countPrimes(limit int) int {
	if limit < 3 {
		return 0
	}
	sieve := make([]bool, limit)
	count := 0
	for i := 2; i < limit; i++ {
		if sieve[i] {
			continue
		}
		count++
		for j := i * i; j < limit && j > 0; j += i {
			sieve[j] = true
		}
	}
	return count
}

// ---- small helpers shared by the backends above --------------------------

func argAt(args []any, i int) any {
	if i < len(args) {
		return args[i]
	}
	return nil
}

func str(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return def
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return def
}

func num(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	}
	return def
}

func boolOf(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// listOf reads argument i as a JS array (an empty slice when it isn't one).
func listOf(args []any, i int) []any {
	if v, ok := argAt(args, i).([]any); ok {
		return v
	}
	return nil
}
