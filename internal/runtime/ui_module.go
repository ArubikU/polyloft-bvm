package runtime

import (
	"encoding/json"
	"fmt"
	"image/color"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	fyne "fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type uiNode struct {
	Kind     string
	Props    map[string]value.Value
	Children []*uiNode
}

type uiWindow struct {
	Title     string
	Width     int
	Height    int
	Root      *uiNode
	Visible   bool
	Callbacks map[string]value.Value
	app       *uiApp
	lastTree  *uiNode
	lastPatch []map[string]any
	lastFx    []map[string]any
	nativeWin fyne.Window
	headless  bool
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
	permissions map[string]bool
	eventSink   *channelHandle
	channels    map[string]*channelHandle
	tasks       []*uiTask
	nextTaskID  int64
	nextTaskSeq int64
	loopCancel  chan struct{}
	loopRunning bool
	nativeApp   fyne.App
	stylesheet  *styleSheet
}

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
		stylesheet: newStyleSheet(),
	}
	if backend == "tk" || backend == "go" || backend == "native" {
		app.permissions["process_exec"] = false
	}
	return app
}

func currentUIPlatform() string {
	return strings.ToLower(goruntime.GOOS) + "/" + strings.ToLower(goruntime.GOARCH)
}

func (app *uiApp) setName(name string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "Polyloft UI"
	}
	app.mu.Lock()
	app.defaultName = trimmed
	windows := append([]*uiWindow(nil), app.windows...)
	app.mu.Unlock()
	for _, window := range windows {
		if window == nil {
			continue
		}
		window.Title = trimmed
		if window.nativeWin != nil {
			window.nativeWin.SetTitle(trimmed)
		}
	}
}

func (app *uiApp) setIcon(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("icon path is empty")
	}
	res, err := fyne.LoadResourceFromPath(trimmed)
	if err != nil {
		return err
	}
	app.mu.Lock()
	app.defaultIcon = trimmed
	windows := append([]*uiWindow(nil), app.windows...)
	app.mu.Unlock()
	for _, window := range windows {
		if window != nil && window.nativeWin != nil {
			window.nativeWin.SetIcon(res)
		}
	}
	if app.nativeApp != nil {
		app.nativeApp.SetIcon(res)
	}
	return nil
}

func (app *uiApp) setPermission(name string, allowed bool) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	if _, ok := app.permissions[name]; !ok {
		return false
	}
	app.permissions[name] = allowed
	return true
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

func uiNodePropString(node *uiNode, name string, fallback string) string {
	if node == nil || node.Props == nil {
		return fallback
	}
	if candidate, ok := node.Props[name]; ok {
		text := strings.TrimSpace(candidate.String())
		if text != "" {
			return text
		}
	}
	return fallback
}

func uiNodePropInt(node *uiNode, name string, fallback int) int {
	if node == nil || node.Props == nil {
		return fallback
	}
	if candidate, ok := node.Props[name]; ok {
		if candidate.Kind == value.Number {
			if candidate.NumberKind == value.NumberInt {
				return int(candidate.Int)
			}
			return int(candidate.Num)
		}
	}
	return fallback
}

func uiNodePropFloat(node *uiNode, name string, fallback float64) float64 {
	if node == nil || node.Props == nil {
		return fallback
	}
	if candidate, ok := node.Props[name]; ok {
		if candidate.Kind == value.Number {
			if candidate.NumberKind == value.NumberInt {
				return float64(candidate.Int)
			}
			return candidate.Num
		}
	}
	return fallback
}

func uiNodeKey(node *uiNode, path string) string {
	if node == nil {
		return path
	}
	if key := strings.TrimSpace(uiNodePropString(node, "key", "")); key != "" {
		return key
	}
	if id := strings.TrimSpace(uiNodePropString(node, "id", "")); id != "" {
		return id
	}
	return path
}

func uiNodeSignature(node *uiNode) string {
	if node == nil {
		return "nil"
	}
	payload := map[string]any{
		"kind":     node.Kind,
		"props":    uiValueToNative(value.ObjectValue(&value.Map{Entries: node.Props})),
		"children": len(node.Children),
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func uiNodeSelfSignature(node *uiNode) string {
	if node == nil {
		return "nil"
	}
	props := make(map[string]any, len(node.Props))
	for key, candidate := range node.Props {
		props[key] = uiValueToNative(candidate)
	}
	payload := map[string]any{
		"kind":  node.Kind,
		"props": props,
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func uiChildStableKey(node *uiNode, index int) string {
	if node == nil {
		return fmt.Sprintf("#%d", index)
	}
	if key := strings.TrimSpace(uiNodePropString(node, "key", "")); key != "" {
		return key
	}
	if id := strings.TrimSpace(uiNodePropString(node, "id", "")); id != "" {
		return id
	}
	return fmt.Sprintf("#%d", index)
}

func flattenUINodes(node *uiNode, path string, out map[string]string, kinds map[string]string) {
	if node == nil {
		return
	}
	key := uiNodeKey(node, path)
	out[key] = uiNodeSignature(node)
	kinds[key] = node.Kind
	for index, child := range node.Children {
		flattenUINodes(child, fmt.Sprintf("%s/%d", key, index), out, kinds)
	}
}

func reconcileTrees(oldRoot *uiNode, newRoot *uiNode) ([]map[string]any, []map[string]any) {
	patches := make([]map[string]any, 0, 16)
	fx := make([]map[string]any, 0, 16)
	var walk func(oldNode *uiNode, newNode *uiNode, path string)
	walk = func(oldNode *uiNode, newNode *uiNode, path string) {
		if oldNode == nil && newNode == nil {
			return
		}
		if oldNode == nil {
			patches = append(patches, map[string]any{"op": "add", "path": path, "kind": newNode.Kind})
			fx = append(fx, map[string]any{"type": "fade-in", "path": path, "durationMs": 140})
			return
		}
		if newNode == nil {
			patches = append(patches, map[string]any{"op": "remove", "path": path, "kind": oldNode.Kind})
			fx = append(fx, map[string]any{"type": "fade-out", "path": path, "durationMs": 120})
			return
		}

		if oldNode.Kind != newNode.Kind || uiNodeSelfSignature(oldNode) != uiNodeSelfSignature(newNode) {
			patches = append(patches, map[string]any{"op": "update", "path": path, "kind": newNode.Kind})
			fx = append(fx, map[string]any{"type": "morph", "path": path, "durationMs": 120})
		}

		oldByKey := make(map[string]*uiNode, len(oldNode.Children))
		newByKey := make(map[string]*uiNode, len(newNode.Children))
		oldIndex := make(map[string]int, len(oldNode.Children))
		newIndex := make(map[string]int, len(newNode.Children))
		for i, child := range oldNode.Children {
			key := uiChildStableKey(child, i)
			oldByKey[key] = child
			oldIndex[key] = i
		}
		for i, child := range newNode.Children {
			key := uiChildStableKey(child, i)
			newByKey[key] = child
			newIndex[key] = i
		}

		for i, child := range newNode.Children {
			key := uiChildStableKey(child, i)
			nextPath := path + "/" + key
			oldChild := oldByKey[key]
			if oldChild == nil {
				walk(nil, child, nextPath)
				continue
			}
			if oldIndex[key] != i {
				patches = append(patches, map[string]any{
					"op":     "move",
					"path":   nextPath,
					"kind":   child.Kind,
					"from":   oldIndex[key],
					"to":     i,
					"parent": path,
				})
				fx = append(fx, map[string]any{"type": "move", "path": nextPath, "from": oldIndex[key], "to": i, "durationMs": 180})
			}
			walk(oldChild, child, nextPath)
		}

		for i, child := range oldNode.Children {
			key := uiChildStableKey(child, i)
			if _, ok := newByKey[key]; !ok {
				walk(child, nil, path+"/"+key)
			}
		}
	}

	walk(oldRoot, newRoot, "root")
	return patches, fx
}

func layoutNodeToNative(node *uiNode, width int, height int) map[string]any {
	if node == nil {
		return map[string]any{}
	}
	if width <= 0 {
		width = uiNodePropInt(node, "width", 800)
	}
	if height <= 0 {
		height = uiNodePropInt(node, "height", 600)
	}
	return layoutNodeToNativeWithBox(node, 0, 0, width, height)
}

func layoutNodeToNativeWithBox(node *uiNode, x int, y int, width int, height int) map[string]any {
	if node == nil {
		return map[string]any{}
	}
	paddingX := uiNodePropInt(node, "padx", uiNodePropInt(node, "padding", 0))
	paddingY := uiNodePropInt(node, "pady", uiNodePropInt(node, "padding", 0))
	direction := strings.ToLower(uiNodePropString(node, "direction", uiNodePropString(node, "layout", "column")))
	if direction != "row" {
		direction = "column"
	}
	justify := strings.ToLower(uiNodePropString(node, "justify", "start"))
	align := strings.ToLower(uiNodePropString(node, "align", "start"))
	gap := uiNodePropInt(node, "gap", 0)

	contentWidth := width - (paddingX * 2)
	contentHeight := height - (paddingY * 2)
	if contentWidth < 0 {
		contentWidth = 0
	}
	if contentHeight < 0 {
		contentHeight = 0
	}

	children := make([]any, 0, len(node.Children))
	if len(node.Children) > 0 {
		mainSize := contentHeight
		crossSize := contentWidth
		if direction == "row" {
			mainSize = contentWidth
			crossSize = contentHeight
		}

		mainLens := make([]int, len(node.Children))
		crossLens := make([]int, len(node.Children))
		fixedMain := 0
		totalFlex := 0.0
		for i, child := range node.Children {
			flex := uiNodePropFloat(child, "flex", 0)
			totalFlex += flex
			if direction == "row" {
				cw := uiNodePropInt(child, "width", -1)
				if cw >= 0 {
					mainLens[i] = cw
					fixedMain += cw
				}
				ch := uiNodePropInt(child, "height", -1)
				if ch >= 0 {
					crossLens[i] = ch
				}
			} else {
				ch := uiNodePropInt(child, "height", -1)
				if ch >= 0 {
					mainLens[i] = ch
					fixedMain += ch
				}
				cw := uiNodePropInt(child, "width", -1)
				if cw >= 0 {
					crossLens[i] = cw
				}
			}
		}

		remaining := mainSize - fixedMain - (max(0, len(node.Children)-1) * gap)
		if remaining < 0 {
			remaining = 0
		}
		if totalFlex > 0 {
			for i, child := range node.Children {
				if mainLens[i] > 0 {
					continue
				}
				flex := uiNodePropFloat(child, "flex", 0)
				if flex > 0 {
					mainLens[i] = int((float64(remaining) * flex) / totalFlex)
				}
			}
		}

		totalMainUsed := 0
		for _, size := range mainLens {
			totalMainUsed += size
		}
		totalMainUsed += max(0, len(node.Children)-1) * gap
		extra := mainSize - totalMainUsed
		if extra < 0 {
			extra = 0
		}

		cursor := 0
		effectiveGap := gap
		if justify == "center" {
			cursor = extra / 2
		} else if justify == "end" {
			cursor = extra
		} else if justify == "space-between" && len(node.Children) > 1 {
			effectiveGap = gap + (extra / (len(node.Children) - 1))
		} else if justify == "space-around" && len(node.Children) > 0 {
			effectiveGap = gap + (extra / len(node.Children))
			cursor = effectiveGap / 2
		}

		for i, child := range node.Children {
			mainLen := mainLens[i]
			if mainLen <= 0 {
				if direction == "row" {
					mainLen = 120
				} else {
					mainLen = 32
				}
			}
			crossLen := crossLens[i]
			if crossLen <= 0 {
				if align == "stretch" {
					crossLen = crossSize
				} else {
					if direction == "column" {
						crossLen = crossSize
					} else {
						crossLen = min(140, crossSize)
					}
				}
			}
			crossPos := 0
			if align == "center" {
				crossPos = (crossSize - crossLen) / 2
			} else if align == "end" {
				crossPos = crossSize - crossLen
			}
			if crossPos < 0 {
				crossPos = 0
			}

			childX := x + paddingX
			childY := y + paddingY
			childW := crossLen
			childH := mainLen
			if direction == "row" {
				childX += cursor
				childY += crossPos
				childW = mainLen
				childH = crossLen
			} else {
				childX += crossPos
				childY += cursor
				childW = crossLen
				childH = mainLen
			}
			children = append(children, layoutNodeToNativeWithBox(child, childX, childY, childW, childH))
			cursor += mainLen + effectiveGap
		}
	}

	props := make(map[string]any, len(node.Props))
	for key, candidate := range node.Props {
		props[key] = uiValueToNative(candidate)
	}
	return map[string]any{
		"kind":  node.Kind,
		"props": props,
		"layout": map[string]any{
			"x":      x,
			"y":      y,
			"width":  width,
			"height": height,
		},
		"children": children,
	}
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

func nodeToNative(node *uiNode) map[string]any {
	children := make([]any, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, nodeToNative(child))
	}
	props := make(map[string]any, len(node.Props))
	for key, candidate := range node.Props {
		props[key] = uiValueToNative(candidate)
	}
	return map[string]any{
		"kind":     node.Kind,
		"props":    props,
		"children": children,
	}
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
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func parseHexColor(input string, fallback color.Color) color.Color {
	text := strings.TrimSpace(strings.TrimPrefix(input, "#"))
	if len(text) == 0 {
		return fallback
	}
	if len(text) == 3 {
		text = string([]byte{text[0], text[0], text[1], text[1], text[2], text[2]})
	}
	if len(text) != 6 {
		return fallback
	}
	r, errR := strconv.ParseUint(text[0:2], 16, 8)
	g, errG := strconv.ParseUint(text[2:4], 16, 8)
	b, errB := strconv.ParseUint(text[4:6], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return fallback
	}
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xFF}
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
	if window.app != nil {
		window.app.emitEvent(eventName, payload)
	}
	return nil
}

// cssTextAlignFyne converts a CSS text-align string to a fyne.TextAlign constant.
func cssTextAlignFyne(css map[string]string) fyne.TextAlign {
	switch strings.ToLower(strings.TrimSpace(css["text-align"])) {
	case "center":
		return fyne.TextAlignCenter
	case "right", "end":
		return fyne.TextAlignTrailing
	default:
		return fyne.TextAlignLeading
	}
}

func appendFyneLayoutNode(root *fyne.Container, node map[string]any, window *uiWindow, path string) {
	kind := strings.ToLower(anyToString(node["kind"], "view"))
	props := anyToMap(node["props"])
	box := anyToMap(node["layout"])
	x := anyToInt(box["x"], 0)
	y := anyToInt(box["y"], 0)
	w := max(1, anyToInt(box["width"], 100))
	h := max(1, anyToInt(box["height"], 30))

	// Resolve CSS: class rules + inline style.* (highest priority).
	var appSS *styleSheet
	if window != nil && window.app != nil {
		appSS = window.app.stylesheet
	}
	css := resolveStyle(props, appSS)

	var object fyne.CanvasObject
	switch kind {
	case "text", "label":
		// color: CSS "color" > legacy "fg" prop > default
		fgHex := cssGetColor(css, "color", anyToString(props["fg"], "#e8e8e8"))
		fg := parseHexColor(fgHex, color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF})
		label := canvas.NewText(anyToString(props["text"], ""), fg)
		label.Alignment = cssTextAlignFyne(css)
		label.TextSize = cssFontSize(css, label.TextSize)
		label.TextStyle = fyne.TextStyle{Bold: cssBold(css), Italic: cssItalic(css)}
		object = label

	case "button":
		label := anyToString(props["text"], "Button")
		eventName := anyToString(props["event"], "click")
		payloadMap := map[string]value.Value{
			"source":    value.StringValue(path),
			"component": value.StringValue("button"),
			"text":      value.StringValue(label),
			"timestamp": value.IntValue(time.Now().UnixMilli()),
		}
		btn := widget.NewButton(label, func() {
			if window == nil {
				return
			}
			if err := dispatchWindowEvent(window, eventName, value.ObjectValue(&value.Map{Entries: payloadMap})); err != nil && window.app != nil {
				window.app.emitEvent("ui.event.error", value.StringValue(err.Error()))
			}
		})
		// Apply background-color / background from CSS if present.
		bgHex := cssBackground(css)
		if bgHex == "" {
			bgHex = anyToString(props["bg"], "")
		}
		if bgHex != "" {
			// Wrap button in a rectangle for background, then overlay button.
			bg := canvas.NewRectangle(parseHexColor(bgHex, color.Transparent))
			bg.Move(fyne.NewPos(float32(x), float32(y)))
			bg.Resize(fyne.NewSize(float32(w), float32(h)))
			root.Add(bg)
		}
		object = btn

	case "input":
		entry := widget.NewEntry()
		entry.SetText(anyToString(props["text"], ""))
		object = entry

	case "native":
		component := strings.ToLower(anyToString(props["component"], anyToString(props["native"], "label")))
		switch component {
		case "check", "checkbox":
			checked := false
			if raw, ok := props["checked"].(bool); ok {
				checked = raw
			}
			check := widget.NewCheck(anyToString(props["text"], ""), nil)
			check.SetChecked(checked)
			object = check

		case "slider":
			minV := float64(anyToInt(props["min"], 0))
			maxV := float64(anyToInt(props["max"], 100))
			if minV >= maxV {
				maxV = minV + 100
			}
			slider := widget.NewSlider(minV, maxV)
			if v, ok := props["value"].(float64); ok {
				slider.SetValue(v)
			}
			// Wire OnChanged → dispatchWindowEvent with value + percent.
			eventName := anyToString(props["event"], "change")
			capturedWindow := window
			capturedPath := path
			slider.OnChanged = func(v float64) {
				if capturedWindow == nil {
					return
				}
				pct := 0.0
				if maxV > minV {
					pct = (v - minV) / (maxV - minV) * 100.0
				}
				payload := map[string]value.Value{
					"source":    value.StringValue(capturedPath),
					"component": value.StringValue("slider"),
					"value":     value.FloatValue(v),
					"percent":   value.FloatValue(pct),
					"min":       value.FloatValue(minV),
					"max":       value.FloatValue(maxV),
					"timestamp": value.IntValue(time.Now().UnixMilli()),
				}
				_ = dispatchWindowEvent(capturedWindow, eventName, value.ObjectValue(&value.Map{Entries: payload}))
			}
			object = slider

		case "progress", "progressbar":
			progress := widget.NewProgressBar()
			if v, ok := props["value"].(float64); ok {
				progress.SetValue(v)
			}
			object = progress

		case "select", "dropdown":
			options := anyToStringSlice(props["options"])
			if len(options) == 0 {
				options = []string{"Option"}
			}
			selectWidget := widget.NewSelect(options, nil)
			selected := anyToString(props["selected"], "")
			if selected != "" {
				selectWidget.SetSelected(selected)
			}
			object = selectWidget

		default:
			object = widget.NewLabel(anyToString(props["text"], component))
		}

	default:
		// View / container: render background rectangle.
		bgHex := cssBackground(css)
		if bgHex == "" {
			bgHex = anyToString(props["bg"], "")
		}
		bg := parseHexColor(bgHex, color.Transparent)
		object = canvas.NewRectangle(bg)
	}

	object.Move(fyne.NewPos(float32(x), float32(y)))
	object.Resize(fyne.NewSize(float32(w), float32(h)))
	root.Add(object)

	for i, child := range anyToSlice(node["children"]) {
		appendFyneLayoutNode(root, anyToMap(child), window, fmt.Sprintf("%s/%d", path, i))
	}
}

func renderGoWindow(window *uiWindow) error {
	if window.Root == nil {
		return fmt.Errorf("window has no root node")
	}
	if window.app == nil {
		return fmt.Errorf("window has no app")
	}
	rootLayout := layoutNodeToNative(window.Root, window.Width, window.Height)
	window.app.mu.Lock()
	if window.app.nativeApp == nil {
		window.app.nativeApp = fyneapp.New()
	}
	ui := window.app.nativeApp
	window.app.mu.Unlock()
	w := ui.NewWindow(window.Title)
	window.nativeWin = w
	if window.app.defaultIcon != "" {
		if res, err := fyne.LoadResourceFromPath(window.app.defaultIcon); err == nil {
			ui.SetIcon(res)
			w.SetIcon(res)
		}
	}
	w.Resize(fyne.NewSize(float32(max(320, window.Width)), float32(max(240, window.Height))))
	if window.headless {
		w.SetFixedSize(true)
		w.SetPadded(false)
	}
	root := container.NewWithoutLayout()
	appendFyneLayoutNode(root, rootLayout, window, "root")
	w.SetContent(root)
	w.ShowAndRun()
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
		return value.BoolValue(app.setPermission(args[1].String(), args[2].Bool)), nil
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
		if err := app.setIcon(args[1].String()); err != nil {
			return value.NilValue(), err
		}
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
			Title:     title,
			Width:     int(args[2].Num),
			Height:    int(args[3].Num),
			Callbacks: make(map[string]value.Value),
			app:       app,
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
		if window.nativeWin != nil {
			window.nativeWin.SetTitle(title)
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
			if err := window.app.setIcon(path); err != nil {
				return value.NilValue(), err
			}
			return value.BoolValue(true), nil
		}
		res, err := fyne.LoadResourceFromPath(path)
		if err != nil {
			return value.NilValue(), err
		}
		if window.nativeWin != nil {
			window.nativeWin.SetIcon(res)
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
			"root":   layoutNodeToNative(window.Root, window.Width, window.Height),
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
		layout := layoutNodeToNative(window.Root, window.Width, window.Height)
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

	builder.AddTypedFunction("app_run", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_run expects app handle")
		}
		if app.backend == "headless" {
			app.runOnUIThread(0)
			return value.NilValue(), nil
		}
		if app.backend == "tk" || app.backend == "go" || app.backend == "native" {
			app.runOnUIThread(0)
			app.mu.Lock()
			windows := append([]*uiWindow(nil), app.windows...)
			app.mu.Unlock()
			for _, window := range windows {
				if !window.Visible {
					continue
				}
				if err := renderGoWindow(window); err != nil {
					return value.NilValue(), err
				}
				break
			}
		}
		return value.NilValue(), nil
	})

	// ─── StyleSheet natives ───────────────────────────────────────────────────

	builder.AddTypedFunction("stylesheet_new", []string{}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(newStyleSheet()), nil
	})

	builder.AddTypedFunction("stylesheet_parse", []string{TypeAny, TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_parse expects stylesheet handle")
		}
		n := ss.parseCSSText(args[1].String())
		return value.IntValue(int64(n)), nil
	})

	builder.AddTypedFunction("stylesheet_load_file", []string{TypeAny, TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_load_file expects stylesheet handle")
		}
		n, err := ss.loadFile(args[1].String())
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
		ss.setRule(args[1].String(), args[2].String(), args[3].String())
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("stylesheet_get", []string{TypeAny, TypeString, TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		ss, ok := args[0].Object.(*styleSheet)
		if !ok {
			return value.NilValue(), fmt.Errorf("stylesheet_get expects stylesheet handle")
		}
		v, _ := ss.getRule(args[1].String(), args[2].String())
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
		return value.NilValue(), nil
	})

	// ─── App quit / window close ──────────────────────────────────────────────

	builder.AddTypedFunction("app_quit", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_quit expects app handle")
		}
		app.mu.Lock()
		na := app.nativeApp
		windows := append([]*uiWindow(nil), app.windows...)
		app.mu.Unlock()
		for _, win := range windows {
			if win != nil && win.nativeWin != nil {
				win.nativeWin.Close()
			}
		}
		if na != nil {
			na.Quit()
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("window_close", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_close expects window handle")
		}
		window.Visible = false
		if window.nativeWin != nil {
			window.nativeWin.Close()
		}
		return value.BoolValue(true), nil
	})

	builder.AddTypedFunction("window_set_headless", []string{TypeAny, TypeBool}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_set_headless expects window handle")
		}
		window.headless = args[1].Bool
		// If already open, apply immediately.
		if window.nativeWin != nil {
			window.nativeWin.SetFixedSize(args[1].Bool)
			window.nativeWin.SetPadded(!args[1].Bool)
		}
		return value.BoolValue(true), nil
	})

	// ─── Node class helpers ───────────────────────────────────────────────────

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
