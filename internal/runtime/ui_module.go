package runtime

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArubikU/giocss"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type uiNode struct {
	Kind     string
	Props    map[string]value.Value
	Children []*uiNode
}

type uiWindow struct {
	Title             string
	Width             int
	Height            int
	Root              *uiNode
	Visible           bool
	Callbacks         map[string]value.Value
	app               *uiApp
	lastTree          *uiNode
	lastPatch         []map[string]any
	lastFx            []map[string]any
	mu                sync.Mutex
	gioRuntime        *giocss.WindowRuntime
	headless          bool
	debug             bool
	debugProfile      map[string]bool
	debugProfilerPath string
}

func (w *uiWindow) nativeRuntime() *giocss.WindowRuntime {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gioRuntime
}

func (w *uiWindow) setNativeRuntime(gr *giocss.WindowRuntime) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.gioRuntime = gr
	w.mu.Unlock()
}

func (w *uiWindow) closeNativeWindow() {
	if gr := w.nativeRuntime(); gr != nil {
		gr.Close()
	}
}

func (w *uiWindow) invalidateNativeWindow() {
	if gr := w.nativeRuntime(); gr != nil {
		gr.Invalidate()
	}
}

func (w *uiWindow) applyDebugConfig() {
	if w == nil {
		return
	}
	gr := w.nativeRuntime()
	if gr == nil {
		return
	}
	w.mu.Lock()
	debug := w.debug
	profile := make(map[string]bool, len(w.debugProfile))
	for key, enabled := range w.debugProfile {
		profile[key] = enabled
	}
	profilerPath := w.debugProfilerPath
	w.mu.Unlock()
	gr.SetDebug(debug)
	gr.SetDebugProfile(profile, profilerPath)
	gr.Invalidate()
}

type uiTask struct {
	id       int64
	seq      int64
	priority int
	callback value.Value
	payload  value.Value
	created  int64
}

type uiApp struct {
	mu          sync.Mutex
	backend     string
	windows     []*uiWindow
	defaultName string
	defaultIcon string
	fontPath    string
	permissions map[string]bool
	eventSink   *channelHandle
	channels    map[string]*channelHandle
	tasks       []*uiTask
	nextTaskID  int64
	nextTaskSeq int64
	loopCancel  chan struct{}
	loopRunning bool
	stylesheet  *styleSheet
}

type styleSheet = giocss.StyleSheet

func newUIApp(backend string) *uiApp {
	if strings.TrimSpace(backend) == "" {
		backend = "headless"
	}
	app := &uiApp{
		backend:     backend,
		windows:     make([]*uiWindow, 0, 2),
		defaultName: "Polyloft UI",
		permissions: map[string]bool{
			"window":       true,
			"process_exec": false,
			"clipboard":    false,
			"file_dialog":  false,
			"network":      false,
			"background":   true,
		},
		channels:   make(map[string]*channelHandle),
		tasks:      make([]*uiTask, 0, 16),
		loopCancel: make(chan struct{}),
		stylesheet: giocss.NewStyleSheet(),
	}
	if backend == "tk" || backend == "go" || backend == "native" {
		app.permissions["process_exec"] = false
	}
	return app
}

func currentUIPlatform() string {
	return strings.ToLower(goruntime.GOOS) + "/" + strings.ToLower(goruntime.GOARCH)
}

func (app *uiApp) snapshotWindows() []*uiWindow {
	if app == nil {
		return nil
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	return append([]*uiWindow(nil), app.windows...)
}

func (app *uiApp) quit() {
	for _, win := range app.snapshotWindows() {
		if win != nil {
			win.closeNativeWindow()
		}
	}
}

func (app *uiApp) setName(name string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "Polyloft UI"
	}
	app.mu.Lock()
	app.defaultName = trimmed
	app.mu.Unlock()
	for _, window := range app.snapshotWindows() {
		if window == nil {
			continue
		}
		window.Title = trimmed
		if gr := window.nativeRuntime(); gr != nil {
			gr.SetTitle(trimmed)
		}
	}
}

func (app *uiApp) hasPermission(name string) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.permissions[name]
}

func (app *uiApp) allowProfile(profile string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "desktop":
		app.permissions["window"] = true
		app.permissions["process_exec"] = true
		app.permissions["clipboard"] = true
		app.permissions["file_dialog"] = true
		app.permissions["background"] = true
	case "mobile":
		app.permissions["window"] = true
		app.permissions["process_exec"] = false
		app.permissions["clipboard"] = true
		app.permissions["file_dialog"] = false
		app.permissions["background"] = true
	case "sandbox":
		for key := range app.permissions {
			app.permissions[key] = false
		}
		app.permissions["window"] = true
	}
}

func (app *uiApp) permissionSnapshot() map[string]value.Value {
	app.mu.Lock()
	defer app.mu.Unlock()
	out := make(map[string]value.Value, len(app.permissions))
	for key, allowed := range app.permissions {
		out[key] = value.BoolValue(allowed)
	}
	return out
}

func (app *uiApp) ensureEventSink(capacity int) *channelHandle {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.eventSink == nil {
		app.eventSink = newChannelHandle(capacity)
	}
	return app.eventSink
}

func (app *uiApp) emitEvent(name string, payload value.Value) bool {
	app.mu.Lock()
	sink := app.eventSink
	app.mu.Unlock()
	if sink == nil {
		return false
	}
	message := map[string]value.Value{
		"name":      value.StringValue(name),
		"payload":   value.DeepCopy(payload),
		"timestamp": value.IntValue(time.Now().UnixMilli()),
	}
	_ = sink.send(value.ObjectValue(&value.Map{Entries: message}))
	return true
}

func (app *uiApp) getOrCreateChannel(name string, capacity int) *channelHandle {
	key := strings.TrimSpace(strings.ToLower(name))
	if key == "" {
		key = "default"
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if existing, ok := app.channels[key]; ok {
		return existing
	}
	created := newChannelHandle(capacity)
	app.channels[key] = created
	return created
}

func (app *uiApp) getChannel(name string) *channelHandle {
	key := strings.TrimSpace(strings.ToLower(name))
	if key == "" {
		key = "default"
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.channels[key]
}

func (app *uiApp) closeChannel(name string) bool {
	key := strings.TrimSpace(strings.ToLower(name))
	if key == "" {
		key = "default"
	}
	app.mu.Lock()
	channel := app.channels[key]
	if channel == nil {
		app.mu.Unlock()
		return false
	}
	delete(app.channels, key)
	app.mu.Unlock()
	return channel.close()
}

func uiIsCallable(candidate value.Value) bool {
	if _, ok := candidate.AsClosure(); ok {
		return true
	}
	if _, ok := candidate.AsFunction(); ok {
		return true
	}
	if _, ok := candidate.AsBuiltin(); ok {
		return true
	}
	return false
}

func cloneUINode(node *uiNode) *uiNode {
	if node == nil {
		return nil
	}
	copyNode := &uiNode{Kind: node.Kind, Props: make(map[string]value.Value, len(node.Props)), Children: make([]*uiNode, 0, len(node.Children))}
	for key, candidate := range node.Props {
		copyNode.Props[key] = value.DeepCopy(candidate)
	}
	for _, child := range node.Children {
		copyNode.Children = append(copyNode.Children, cloneUINode(child))
	}
	return copyNode
}

func reconcileTrees(oldRoot *uiNode, newRoot *uiNode) ([]map[string]any, []map[string]any) {
	return giocss.ReconcileTrees(toGiocssNode(oldRoot), toGiocssNode(newRoot))
}

func layoutNodeToNative(node *uiNode, width int, height int, ss *styleSheet) map[string]any {
	return giocss.LayoutNodeToNative(toGiocssNode(node), width, height, ss)
}

func (app *uiApp) post(callback value.Value, payload value.Value, priority int) (int64, error) {
	if !uiIsCallable(callback) {
		return 0, fmt.Errorf("post expects callable")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	app.nextTaskID++
	app.nextTaskSeq++
	task := &uiTask{
		id:       app.nextTaskID,
		seq:      app.nextTaskSeq,
		priority: priority,
		callback: value.DeepCopy(callback),
		payload:  value.DeepCopy(payload),
		created:  time.Now().UnixMilli(),
	}
	app.tasks = append(app.tasks, task)
	return task.id, nil
}

func (app *uiApp) cancelTask(id int64) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	for i, task := range app.tasks {
		if task != nil && task.id == id {
			app.tasks = append(app.tasks[:i], app.tasks[i+1:]...)
			return true
		}
	}
	return false
}

func (app *uiApp) runOnUIThread(maxTasks int) int {
	app.mu.Lock()
	if len(app.tasks) == 0 {
		app.mu.Unlock()
		return 0
	}
	tasks := append([]*uiTask(nil), app.tasks...)
	app.tasks = app.tasks[:0]
	app.mu.Unlock()

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].priority == tasks[j].priority {
			return tasks[i].seq < tasks[j].seq
		}
		return tasks[i].priority > tasks[j].priority
	})
	if maxTasks > 0 && maxTasks < len(tasks) {
		remaining := append([]*uiTask(nil), tasks[maxTasks:]...)
		app.mu.Lock()
		app.tasks = append(app.tasks, remaining...)
		app.mu.Unlock()
		tasks = tasks[:maxTasks]
	}

	executed := 0
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if GlobalVMProxy == nil {
			app.emitEvent("ui.task.error", value.StringValue("VM context missing"))
			continue
		}
		result, err := GlobalVMProxy.CallClosure(task.callback, []value.Value{task.payload})
		if err != nil {
			app.emitEvent("ui.task.error", value.StringValue(err.Error()))
			continue
		}
		executed++
		app.emitEvent("ui.task.done", result)
	}
	return executed
}

func (app *uiApp) startUILoop(timeSliceMs int, maxTasksPerTick int) bool {
	if timeSliceMs < 1 {
		timeSliceMs = 16
	}
	if maxTasksPerTick < 1 {
		maxTasksPerTick = 16
	}
	app.mu.Lock()
	if app.loopRunning {
		app.mu.Unlock()
		return false
	}
	stop := make(chan struct{})
	app.loopCancel = stop
	app.loopRunning = true
	app.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Duration(timeSliceMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = app.runOnUIThread(maxTasksPerTick)
			}
		}
	}()
	return true
}

func (app *uiApp) stopUILoop() bool {
	app.mu.Lock()
	if !app.loopRunning {
		app.mu.Unlock()
		return false
	}
	stop := app.loopCancel
	app.loopRunning = false
	app.loopCancel = make(chan struct{})
	app.mu.Unlock()
	close(stop)
	return true
}

func (app *uiApp) isUILoopRunning() bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.loopRunning
}

func newUINode(kind string) *uiNode {
	return &uiNode{Kind: kind, Props: make(map[string]value.Value), Children: make([]*uiNode, 0, 4)}
}

func uiValueToNative(candidate value.Value) any {
	switch candidate.Kind {
	case value.Nil:
		return nil
	case value.Bool:
		return candidate.Bool
	case value.Number:
		if candidate.NumberKind == value.NumberInt {
			return candidate.Int
		}
		return candidate.Num
	case value.Char, value.String:
		return candidate.Str
	case value.Object:
		if mapped, ok := candidate.AsMap(); ok {
			entries := make(map[string]any, len(mapped.Entries))
			for key, val := range mapped.Entries {
				entries[key] = uiValueToNative(val)
			}
			return entries
		}
		if arr, ok := candidate.AsArray(); ok {
			items := make([]any, len(arr.Elements))
			for i, item := range arr.Elements {
				items[i] = uiValueToNative(item)
			}
			return items
		}
	}
	return candidate.String()
}

func resolveNodeStyle(node *uiNode, ss *styleSheet, viewportW int) map[string]string {
	if node == nil {
		return map[string]string{}
	}
	if ss == nil {
		return giocss.ResolveNodeStyle(toGiocssNode(node), nil, viewportW)
	}
	return giocss.ResolveNodeStyle(toGiocssNode(node), ss, viewportW)
}

func toGiocssNode(node *uiNode) *giocss.Node {
	if node == nil {
		return nil
	}
	gNode := giocss.NewNode(node.Kind)
	for key, candidate := range node.Props {
		gNode.SetProp(key, uiValueToNative(candidate))
	}
	for _, child := range node.Children {
		gChild := toGiocssNode(child)
		if gChild != nil {
			gNode.AddChild(gChild)
		}
	}
	return gNode
}

func anyToMap(candidate any) map[string]any {
	if mapped, ok := candidate.(map[string]any); ok {
		return mapped
	}
	return map[string]any{}
}

func anyToSlice(candidate any) []any {
	if values, ok := candidate.([]any); ok {
		return values
	}
	return nil
}

func anyToStringSlice(candidate any) []string {
	values := anyToSlice(candidate)
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, anyToString(item, ""))
	}
	return result
}

func anyToInt(candidate any, fallback int) int {
	switch typed := candidate.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func anyToString(candidate any, fallback string) string {
	if typed, ok := candidate.(string); ok {
		normalized := strings.ToValidUTF8(typed, "")
		trimmed := strings.TrimSpace(normalized)
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func drawDebugRectStroke(img *image.NRGBA, x int, y int, w int, h int, col color.NRGBA) {
	if img == nil || w <= 1 || h <= 1 {
		return
	}
	b := img.Bounds()
	x0 := max(b.Min.X, x)
	y0 := max(b.Min.Y, y)
	x1 := min(b.Max.X-1, x+w-1)
	y1 := min(b.Max.Y-1, y+h-1)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for px := x0; px <= x1; px++ {
		img.SetNRGBA(px, y0, col)
		img.SetNRGBA(px, y1, col)
	}
	for py := y0; py <= y1; py++ {
		img.SetNRGBA(x0, py, col)
		img.SetNRGBA(x1, py, col)
	}
}

func debugNodeColor(kind string) color.NRGBA {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "text", "label":
		return color.NRGBA{R: 88, G: 166, B: 255, A: 255}
	case "button":
		return color.NRGBA{R: 46, G: 204, B: 113, A: 255}
	case "input", "native":
		return color.NRGBA{R: 241, G: 196, B: 15, A: 255}
	default:
		return color.NRGBA{R: 155, G: 89, B: 182, A: 255}
	}
}

func drawDebugLayoutNode(img *image.NRGBA, node map[string]any) {
	if img == nil || node == nil {
		return
	}
	kind := anyToString(node["kind"], "view")
	box := anyToMap(node["layout"])
	x := anyToInt(box["x"], 0)
	y := anyToInt(box["y"], 0)
	w := anyToInt(box["width"], 0)
	h := anyToInt(box["height"], 0)
	col := debugNodeColor(kind)
	drawDebugRectStroke(img, x, y, w, h, col)
	for _, child := range anyToSlice(node["children"]) {
		drawDebugLayoutNode(img, anyToMap(child))
	}
}

func saveWindowLayoutPNG(window *uiWindow, outPath string) error {
	if window == nil {
		return fmt.Errorf("window is nil")
	}
	if window.Root == nil {
		return fmt.Errorf("window has no root node")
	}
	if strings.TrimSpace(outPath) == "" {
		return fmt.Errorf("output path is empty")
	}
	ss := window.app.stylesheet
	layout := layoutNodeToNative(window.Root, window.Width, window.Height, ss)
	rootBox := anyToMap(layout["layout"])
	w := max(1, anyToInt(rootBox["width"], window.Width))
	h := max(1, anyToInt(rootBox["height"], window.Height))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.NRGBA{R: 15, G: 23, B: 42, A: 255}}, image.Point{}, draw.Src)
	drawDebugLayoutNode(img, layout)

	clean := filepath.Clean(strings.TrimSpace(outPath))
	dir := filepath.Dir(clean)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(clean)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func dispatchWindowEvent(window *uiWindow, eventName string, payload value.Value) error {
	if window == nil {
		return fmt.Errorf("window is nil")
	}
	if callback, ok := window.Callbacks[eventName]; ok && GlobalVMProxy != nil {
		if _, err := GlobalVMProxy.CallClosure(callback, []value.Value{value.DeepCopy(payload)}); err != nil {
			return err
		}
	}
	return nil
}

func valueMapToNativeMap(mapped *value.Map) map[string]any {
	if mapped == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(mapped.Entries))
	for key, entry := range mapped.Entries {
		result[key] = uiValueToNative(entry)
	}
	return result
}

func ensureWindowRuntime(window *uiWindow) (*giocss.WindowRuntime, error) {
	if window == nil {
		return nil, fmt.Errorf("window is nil")
	}
	if existing := window.nativeRuntime(); existing != nil {
		return existing, nil
	}

	window.mu.Lock()
	if window.gioRuntime != nil {
		runtime := window.gioRuntime
		window.mu.Unlock()
		return runtime, nil
	}
	window.mu.Unlock()

	hooks := giocss.WindowRuntimeHooks{
		Snapshot: func(size image.Point) giocss.WindowRuntimeSnapshot {
			ss := giocss.NewStyleSheet()
			if window.app != nil && window.app.stylesheet != nil {
				ss = window.app.stylesheet
			}
			window.mu.Lock()
			root := window.Root
			windowW := window.Width
			windowH := window.Height
			window.mu.Unlock()
			if windowW <= 0 {
				windowW = size.X
			}
			if windowH <= 0 {
				windowH = size.Y
			}
			if root == nil {
				return giocss.WindowRuntimeSnapshot{
					StyleSheet:   ss,
					ScreenWidth:  windowW,
					ScreenHeight: windowH,
				}
			}
			layout := layoutNodeToNative(root, windowW, windowH, ss)
			style := resolveNodeStyle(root, ss, windowW)
			return giocss.WindowRuntimeSnapshot{
				RootLayout:   layout,
				RootCSS:      style,
				StyleSheet:   ss,
				ScreenWidth:  windowW,
				ScreenHeight: windowH,
			}
		},
		DispatchEvent: func(eventName string, payload map[string]any) error {
			return dispatchWindowEvent(window, eventName, nativeToValue(payload))
		},
		EmitRuntimeError: func(err error) {
			_ = dispatchWindowEvent(window, "error", value.StringValue(err.Error()))
		},
		OnClose: func() {
			window.mu.Lock()
			window.gioRuntime = nil
			window.mu.Unlock()
		},
	}

	title := strings.TrimSpace(window.Title)
	if title == "" {
		title = "Polyloft UI"
	}
	width := window.Width
	height := window.Height
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 768
	}

	runtime := giocss.NewWindowRuntime(giocss.WindowOptions{Title: title, Width: width, Height: height}, hooks)
	window.mu.Lock()
	if window.gioRuntime == nil {
		window.gioRuntime = runtime
		window.mu.Unlock()
		window.applyDebugConfig()
		return runtime, nil
	}
	existing := window.gioRuntime
	window.mu.Unlock()
	return existing, nil
}

func rerenderWindowRuntime(window *uiWindow) error {
	runtime, err := ensureWindowRuntime(window)
	if err != nil {
		return err
	}
	runtime.Invalidate()
	return nil
}

func openWindowRuntime(window *uiWindow) error {
	runtime, err := ensureWindowRuntime(window)
	if err != nil {
		return err
	}
	runtime.Run()
	return nil
}

func BuildUiModule() *RuntimeModule {
	builder := NewModuleBuilder("Ui")

	builder.AddTypedFunction("app_new", []string{TypeString}, TypeAny, true, func(args []value.Value) (value.Value, error) {
		backend := "headless"
		if len(args) > 0 {
			backend = strings.ToLower(strings.TrimSpace(args[0].String()))
		}
		return value.ObjectValue(newUIApp(backend)), nil
	})

	builder.AddTypedFunction("app_set_permission", []string{TypeAny, TypeString, TypeBool}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_set_permission expects app handle")
		}
		name := strings.TrimSpace(args[1].String())
		if name == "" {
			return value.BoolValue(false), nil
		}
		app.mu.Lock()
		if _, exists := app.permissions[name]; !exists {
			app.mu.Unlock()
			return value.BoolValue(false), nil
		}
		app.permissions[name] = args[2].Bool
		app.mu.Unlock()
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("app_has_permission", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_has_permission expects app handle")
		}
		return value.BoolValue(app.hasPermission(args[1].String())), nil
	})

	builder.AddTypedFunction("app_permissions", []string{TypeAny}, TypeMap, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_permissions expects app handle")
		}
		return value.ObjectValue(&value.Map{Entries: app.permissionSnapshot()}), nil
	})

	builder.AddTypedFunction("app_allow_profile", []string{TypeAny, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_allow_profile expects app handle")
		}
		app.allowProfile(args[1].String())
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("app_set_name", []string{TypeAny, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_set_name expects app handle")
		}
		app.setName(args[1].String())
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("app_set_icon", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_set_icon expects app handle")
		}
		path := strings.TrimSpace(args[1].String())
		if path == "" {
			return value.NilValue(), fmt.Errorf("icon path is empty")
		}
		app.mu.Lock()
		app.defaultIcon = path
		app.mu.Unlock()
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("app_load_font", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_load_font expects app handle")
		}
		app.mu.Lock()
		app.fontPath = strings.TrimSpace(args[1].String())
		app.mu.Unlock()
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("app_platform", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		if _, ok := args[0].Object.(*uiApp); !ok {
			return value.NilValue(), fmt.Errorf("app_platform expects app handle")
		}
		return value.StringValue(currentUIPlatform()), nil
	})

	builder.AddTypedFunction("app_event_channel", []string{TypeAny, TypeInt}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_event_channel expects app handle")
		}
		return value.ObjectValue(app.ensureEventSink(int(args[1].Num))), nil
	})

	builder.AddTypedFunction("app_emit_event", []string{TypeAny, TypeString, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_emit_event expects app handle")
		}
		return value.BoolValue(app.emitEvent(args[1].String(), args[2])), nil
	})

	builder.AddTypedFunction("app_channel_new", []string{TypeAny, TypeString, TypeInt}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_channel_new expects app handle")
		}
		channel := app.getOrCreateChannel(args[1].String(), int(args[2].Num))
		return value.ObjectValue(channel), nil
	})

	builder.AddTypedFunction("app_channel_send", []string{TypeAny, TypeString, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_channel_send expects app handle")
		}
		channel := app.getChannel(args[1].String())
		if channel == nil {
			return value.BoolValue(false), nil
		}
		if err := channel.send(args[2]); err != nil {
			return value.NilValue(), err
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("app_channel_receive", []string{TypeAny, TypeString}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_channel_receive expects app handle")
		}
		channel := app.getChannel(args[1].String())
		if channel == nil {
			return value.NilValue(), nil
		}
		return channel.receive(), nil
	})

	builder.AddTypedFunction("app_channel_close", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_channel_close expects app handle")
		}
		return value.BoolValue(app.closeChannel(args[1].String())), nil
	})

	builder.AddTypedFunction("app_dispatch", []string{TypeAny, TypeFunction, TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_dispatch expects app handle")
		}
		if !uiIsCallable(args[1]) {
			return value.NilValue(), fmt.Errorf("app_dispatch expects callable")
		}
		if GlobalVMProxy == nil {
			return value.NilValue(), fmt.Errorf("VM context is missing for app_dispatch")
		}
		result, err := GlobalVMProxy.CallClosure(args[1], []value.Value{value.DeepCopy(args[2])})
		if err != nil {
			return value.NilValue(), err
		}
		app.emitEvent("dispatch.done", result)
		return result, nil
	})

	builder.AddTypedFunction("app_dispatch_async", []string{TypeAny, TypeFunction, TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_dispatch_async expects app handle")
		}
		if !uiIsCallable(args[1]) {
			return value.NilValue(), fmt.Errorf("app_dispatch_async expects callable")
		}
		future := newFutureHandle()
		if !app.hasPermission("background") {
			future.fail(fmt.Errorf("background permission denied"))
			return value.ObjectValue(future), nil
		}
		if GlobalVMProxy == nil {
			future.fail(fmt.Errorf("VM context is missing for async execution"))
			return value.ObjectValue(future), nil
		}
		callback := value.DeepCopy(args[1])
		payload := value.DeepCopy(args[2])
		go func() {
			result, err := GlobalVMProxy.CallClosureIsolated(callback, []value.Value{payload})
			if err != nil {
				future.fail(err)
				app.emitEvent("dispatch.error", value.StringValue(err.Error()))
				return
			}
			future.complete(result)
			app.emitEvent("dispatch.done", result)
		}()
		return value.ObjectValue(future), nil
	})

	builder.AddTypedFunction("app_post", []string{TypeAny, TypeFunction, TypeAny, TypeInt}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_post expects app handle")
		}
		id, err := app.post(args[1], args[2], int(args[3].Num))
		if err != nil {
			return value.NilValue(), err
		}
		return value.IntValue(id), nil
	})

	builder.AddTypedFunction("app_cancel_task", []string{TypeAny, TypeInt}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_cancel_task expects app handle")
		}
		return value.BoolValue(app.cancelTask(args[1].Int)), nil
	})

	builder.AddTypedFunction("app_run_on_ui_thread", []string{TypeAny, TypeInt}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_run_on_ui_thread expects app handle")
		}
		return value.IntValue(int64(app.runOnUIThread(int(args[1].Num)))), nil
	})

	builder.AddTypedFunction("app_start_ui_loop", []string{TypeAny, TypeInt, TypeInt}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_start_ui_loop expects app handle")
		}
		started := app.startUILoop(int(args[1].Num), int(args[2].Num))
		return value.BoolValue(started), nil
	})

	builder.AddTypedFunction("app_stop_ui_loop", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_stop_ui_loop expects app handle")
		}
		return value.BoolValue(app.stopUILoop()), nil
	})

	builder.AddTypedFunction("app_ui_loop_running", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_ui_loop_running expects app handle")
		}
		return value.BoolValue(app.isUILoopRunning()), nil
	})

	builder.AddTypedFunction("node_new", []string{TypeString}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(newUINode(args[0].String())), nil
	})

	builder.AddTypedFunction("node_set_prop", []string{TypeAny, TypeString, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		node, ok := args[0].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("node_set_prop expects node handle")
		}
		node.Props[args[1].String()] = value.DeepCopy(args[2])
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("node_set_style", []string{TypeAny, TypeString, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		node, ok := args[0].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("node_set_style expects node handle")
		}
		node.Props["style."+args[1].String()] = value.DeepCopy(args[2])
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("node_add_child", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		parent, ok := args[0].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("node_add_child expects parent node")
		}
		child, ok := args[1].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("node_add_child expects child node")
		}
		parent.Children = append(parent.Children, child)
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("window_new", []string{TypeAny, TypeString, TypeInt, TypeInt}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_new expects app handle")
		}
		title := strings.TrimSpace(args[1].String())
		if title == "" {
			title = app.defaultName
		}
		window := &uiWindow{
			Title:        title,
			Width:        int(args[2].Num),
			Height:       int(args[3].Num),
			Callbacks:    make(map[string]value.Value),
			debugProfile: make(map[string]bool),
			app:          app,
		}
		app.mu.Lock()
		app.windows = append(app.windows, window)
		app.mu.Unlock()
		return value.ObjectValue(window), nil
	})

	builder.AddTypedFunction("window_set_title", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_set_title expects window handle")
		}
		title := strings.TrimSpace(args[1].String())
		if title == "" {
			return value.BoolValue(false), nil
		}
		window.Title = title
		if gr := window.nativeRuntime(); gr != nil {
			gr.SetTitle(title)
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_set_icon", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_set_icon expects window handle")
		}
		path := strings.TrimSpace(args[1].String())
		if path == "" {
			return value.BoolValue(false), nil
		}
		if window.app != nil {
			window.app.mu.Lock()
			window.app.defaultIcon = path
			window.app.mu.Unlock()
			return value.BoolValue(true), nil
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_set_root", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_set_root expects window handle")
		}
		node, ok := args[1].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_set_root expects node handle")
		}
		window.Root = node
		if err := rerenderWindowRuntime(window); err != nil {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("window_bind", []string{TypeAny, TypeString, TypeFunction}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_bind expects window handle")
		}
		window.Callbacks[args[1].String()] = value.DeepCopy(args[2])
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_trigger", []string{TypeAny, TypeString, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_trigger expects window handle")
		}
		eventName := args[1].String()
		_, ok = window.Callbacks[eventName]
		if !ok {
			return value.BoolValue(false), nil
		}
		if err := dispatchWindowEvent(window, eventName, args[2]); err != nil {
			return value.NilValue(), err
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_to_json", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_to_json expects window handle")
		}
		if window.Root == nil {
			return value.StringValue("{}"), nil
		}
		payload := map[string]any{
			"title":  window.Title,
			"width":  window.Width,
			"height": window.Height,
			"root":   layoutNodeToNative(window.Root, window.Width, window.Height, window.app.stylesheet),
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(b)), nil
	})

	builder.AddTypedFunction("window_layout_json", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_layout_json expects window handle")
		}
		if window.Root == nil {
			return value.StringValue("{}"), nil
		}
		layout := layoutNodeToNative(window.Root, window.Width, window.Height, window.app.stylesheet)
		b, err := json.Marshal(layout)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(b)), nil
	})

	builder.AddTypedFunction("window_reconcile", []string{TypeAny, TypeAny}, TypeMap, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_reconcile expects window handle")
		}
		node, ok := args[1].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_reconcile expects node handle")
		}
		patches, transitions := reconcileTrees(window.Root, node)
		window.lastPatch = patches
		window.lastFx = transitions
		window.lastTree = cloneUINode(window.Root)
		window.Root = node
		if err := rerenderWindowRuntime(window); err != nil {
			return value.NilValue(), err
		}
		result := map[string]value.Value{
			"changed": value.BoolValue(len(patches) > 0),
			"count":   value.IntValue(int64(len(patches))),
			"moves":   value.IntValue(int64(len(transitions))),
		}
		return value.ObjectValue(&value.Map{Entries: result}), nil
	})

	builder.AddTypedFunction("window_last_patch_json", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_last_patch_json expects window handle")
		}
		if len(window.lastPatch) == 0 {
			return value.StringValue("[]"), nil
		}
		b, err := json.Marshal(window.lastPatch)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(b)), nil
	})

	builder.AddTypedFunction("window_last_transition_json", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_last_transition_json expects window handle")
		}
		if len(window.lastFx) == 0 {
			return value.StringValue("[]"), nil
		}
		b, err := json.Marshal(window.lastFx)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(b)), nil
	})

	builder.AddTypedFunction("window_show", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_show expects window handle")
		}
		if window.app == nil {
			return value.NilValue(), fmt.Errorf("window has no app")
		}
		if !window.app.hasPermission("window") {
			return value.NilValue(), fmt.Errorf("window permission denied")
		}
		window.Visible = true
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_print", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_print expects window handle")
		}
		path := strings.TrimSpace(args[1].String())
		if err := saveWindowLayoutPNG(window, path); err != nil {
			return value.NilValue(), err
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("app_run", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_run expects app handle")
		}
		if app.backend == "headless" {
			app.runOnUIThread(0)
			return value.NilValue(), nil
		}
		// Gio backend
		app.runOnUIThread(0)
		windows := app.snapshotWindows()
		var wg sync.WaitGroup
		for _, window := range windows {
			if !window.Visible {
				continue
			}
			wg.Add(1)
			go func(win *uiWindow) {
				defer wg.Done()
				if err := openWindowRuntime(win); err != nil {
					_ = dispatchWindowEvent(win, "error", value.StringValue(err.Error()))
				}
			}(window)
		}
		wg.Wait()
		return value.NilValue(), nil
	})

	// â”€â”€â”€ StyleSheet natives â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

	builder.AddTypedFunction("stylesheet_new", []string{}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(giocss.NewStyleSheet()), nil
	})

	builder.AddTypedFunction("stylesheet_parse", []string{TypeAny, TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_parse expects stylesheet handle")
		}
		n := ss.ParseCSSText(args[1].String())
		return value.IntValue(int64(n)), nil
	})

	builder.AddTypedFunction("stylesheet_load_file", []string{TypeAny, TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_load_file expects stylesheet handle")
		}
		n, err := ss.LoadFile(args[1].String())
		if err != nil {
			return value.NilValue(), err
		}
		return value.IntValue(int64(n)), nil
	})

	builder.AddTypedFunction("stylesheet_set", []string{TypeAny, TypeString, TypeString, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_set expects stylesheet handle")
		}
		ss.SetRule(args[1].String(), args[2].String(), args[3].String())
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("stylesheet_get", []string{TypeAny, TypeString, TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_get expects stylesheet handle")
		}
		v, _ := ss.GetRule(args[1].String(), args[2].String())
		return value.StringValue(v), nil
	})

	builder.AddTypedFunction("app_attach_stylesheet", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_attach_stylesheet expects app handle")
		}
		ss, ok := args[1].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_attach_stylesheet expects stylesheet handle")
		}
		app.mu.Lock()
		app.stylesheet = ss
		app.mu.Unlock()
		for _, win := range app.snapshotWindows() {
			_ = rerenderWindowRuntime(win)
		}
		return value.NilValue(), nil
	})

	// â”€â”€â”€ App quit / window close â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

	builder.AddTypedFunction("app_quit", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_quit expects app handle")
		}
		app.quit()
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("window_close", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_close expects window handle")
		}
		window.Visible = false
		window.closeNativeWindow()
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_set_headless", []string{TypeAny, TypeBool}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_set_headless expects window handle")
		}
		window.headless = args[1].Bool

		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_debug", []string{TypeAny, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_debug expects window handle")
		}
		window.mu.Lock()
		switch args[1].Kind {
		case value.Bool:
			window.debug = args[1].Bool
		case value.Object:
			flagsMap, ok := args[1].AsMap()
			if !ok || flagsMap == nil {
				window.mu.Unlock()
				return value.NilValue(), fmt.Errorf("window_debug expects bool or map<string, any>")
			}
			profile, profilerPath := giocss.ParseDebugProfileConfig(valueMapToNativeMap(flagsMap))
			window.debugProfile = profile
			window.debugProfilerPath = profilerPath
		default:
			window.mu.Unlock()
			return value.NilValue(), fmt.Errorf("window_debug expects bool or map<string, any>")
		}
		window.mu.Unlock()
		window.applyDebugConfig()
		if err := rerenderWindowRuntime(window); err != nil {
			return value.NilValue(), err
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_debug_profile", []string{TypeAny, TypeMap}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_debug_profile expects window handle")
		}
		flagsMap, ok := args[1].AsMap()
		if !ok || flagsMap == nil {
			return value.NilValue(), fmt.Errorf("window_debug_profile expects map<string, any>")
		}
		profile, profilerPath := giocss.ParseDebugProfileConfig(valueMapToNativeMap(flagsMap))

		window.mu.Lock()
		window.debugProfile = profile
		window.debugProfilerPath = profilerPath
		window.mu.Unlock()
		window.applyDebugConfig()
		if err := rerenderWindowRuntime(window); err != nil {
			return value.NilValue(), err
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("node_set_class", []string{TypeAny, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		node, ok := args[0].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("node_set_class expects node handle")
		}
		node.Props["class"] = value.StringValue(strings.TrimSpace(args[1].String()))
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("node_add_class", []string{TypeAny, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		node, ok := args[0].Object.(*uiNode)
		if !ok {
			return value.NilValue(), fmt.Errorf("node_add_class expects node handle")
		}
		newCls := strings.TrimSpace(args[1].String())
		if existing, ok2 := node.Props["class"]; ok2 {
			cur := strings.TrimSpace(existing.String())
			if cur != "" {
				node.Props["class"] = value.StringValue(cur + " " + newCls)
				return value.NilValue(), nil
			}
		}
		node.Props["class"] = value.StringValue(newCls)
		return value.NilValue(), nil
	})

	return builder.Build()
}
