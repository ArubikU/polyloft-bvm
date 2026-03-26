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

	gioapp "gioui.org/app"
	"gioui.org/io/system"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type uiNode struct {
	Kind     string
	Props    map[string]value.Value
	Children []*uiNode
}

type uiWindow struct {
	Title                 string
	Width                 int
	Height                int
	Root                  *uiNode
	Visible               bool
	Callbacks             map[string]value.Value
	app                   *uiApp
	lastTree              *uiNode
	lastPatch             []map[string]any
	lastFx                []map[string]any
	mu                    sync.Mutex
	gioWin                *gioapp.Window
	headless              bool
	debug                 bool
	debugProfile          map[string]bool
	debugProfilerPath     string
	debugFrames           int64
	debugFPS              float64
	debugFPSFrames        int64
	debugFPSLastAt        time.Time
	debugFrameRenderMS    float64
	debugFrameRenderAvgMS float64
	debugFrameRenderMaxMS float64
	debugSlowFrames       int64
	debugHeapMB           float64
	debugComponents       map[string]*debugComponentStat
	hoverState            map[string]bool
	activeState           map[string]bool
	renderState           map[string]uiRenderState
	nextRenderState       map[string]uiRenderState
}

func parseWindowDebugProfile(flagsMap *value.Map) (map[string]bool, string) {
	profile := make(map[string]bool, len(flagsMap.Entries))
	profilerPath := ""
	for key, v := range flagsMap.Entries {
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		switch name {
		case "profiler_path", "profilerpath", "profile_path", "dump_path":
			if v.Kind == value.String || v.Kind == value.Char {
				profilerPath = strings.TrimSpace(v.String())
			}
		default:
			profile[name] = v.IsTruthy()
		}
	}
	return profile, profilerPath
}

type uiRenderState struct {
	CSS    map[string]string
	Layout map[string]int
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
		window.mu.Lock()
		gw := window.gioWin
		window.mu.Unlock()
		if gw != nil {
			gw.Option(gioapp.Title(trimmed))
		}
	}
}

func (app *uiApp) setIcon(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("icon path is empty")
	}
	app.mu.Lock()
	app.defaultIcon = trimmed
	app.mu.Unlock()
	return nil
}

func (app *uiApp) loadFont(pathOrURL string) error {
	app.mu.Lock()
	app.fontPath = strings.TrimSpace(pathOrURL)
	app.mu.Unlock()
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

func layoutNodeToNative(node *uiNode, width int, height int, ss *styleSheet) map[string]any {
	if node == nil {
		return map[string]any{}
	}
	rootCSS := resolveNodeStyle(node, ss, width)
	if width <= 0 {
		width = nodeLayoutLength(node, rootCSS, "width", "width", 800, 800, 600, 800)
	}
	if height <= 0 {
		height = nodeLayoutLength(node, rootCSS, "height", "height", 600, width, 600, 600)
	}
	return layoutNodeToNativeWithBox(node, 0, 0, width, height, width, height, ss, nil)
}

func layoutNodeToNativeWithBox(node *uiNode, x int, y int, width int, height int, viewportW int, viewportH int, ss *styleSheet, parentCSS map[string]string) map[string]any {
	if node == nil {
		return map[string]any{}
	}
	css := resolveNodeStyle(node, ss, viewportW)
	css = mergeInheritedTextCSS(css, parentCSS)
	paddingX := uiNodePropInt(node, "padx", uiNodePropInt(node, "padding", 0))
	paddingY := uiNodePropInt(node, "pady", uiNodePropInt(node, "padding", 0))
	paddingTop := nodeLayoutLength(node, css, "paddingTop", "padding-top", height, viewportW, viewportH, paddingY)
	paddingRight := nodeLayoutLength(node, css, "paddingRight", "padding-right", width, viewportW, viewportH, paddingX)
	paddingBottom := nodeLayoutLength(node, css, "paddingBottom", "padding-bottom", height, viewportW, viewportH, paddingY)
	paddingLeft := nodeLayoutLength(node, css, "paddingLeft", "padding-left", width, viewportW, viewportH, paddingX)
	direction := nodeLayoutString(node, css, "direction", "direction", strings.ToLower(uiNodePropString(node, "layout", "column")))
	if direction != "row" {
		direction = "column"
	}
	justify := nodeLayoutString(node, css, "justify", "justify", "start")
	align := nodeLayoutString(node, css, "align", "align", "stretch")
	gap := nodeLayoutLength(node, css, "gap", "gap", max(width, height), viewportW, viewportH, 0)
	rowGap := nodeLayoutLength(node, css, "rowGap", "row-gap", max(width, height), viewportW, viewportH, gap)
	columnGap := nodeLayoutLength(node, css, "columnGap", "column-gap", max(width, height), viewportW, viewportH, gap)

	contentWidth := width - paddingLeft - paddingRight
	contentHeight := height - paddingTop - paddingBottom
	if contentWidth < 0 {
		contentWidth = 0
	}
	if contentHeight < 0 {
		contentHeight = 0
	}

	children := make([]any, 0, len(node.Children))
	if len(node.Children) > 0 {
		displayMode := strings.ToLower(strings.TrimSpace(css["display"]))
		if displayMode == "grid" {
			if strings.TrimSpace(css["justify"]) == "" {
				justify = "stretch"
			}
			if strings.TrimSpace(css["align"]) == "" {
				align = "stretch"
			}
			containerJustify := strings.ToLower(strings.TrimSpace(css["justify-content"]))
			if containerJustify == "" {
				containerJustify = strings.ToLower(strings.TrimSpace(css["justify"]))
			}
			if containerJustify == "" {
				containerJustify = "start"
			}
			itemJustifyDefault := strings.ToLower(strings.TrimSpace(css["justify-items"]))
			if itemJustifyDefault == "" {
				itemJustifyDefault = "stretch"
			}
			if place := strings.Fields(strings.ToLower(strings.TrimSpace(css["place-items"]))); len(place) > 0 {
				align = place[0]
				if len(place) > 1 {
					itemJustifyDefault = place[1]
				} else {
					itemJustifyDefault = place[0]
				}
			}
			if itemJustifyDefault == "start" {
				itemJustifyDefault = "left"
			}
			if itemJustifyDefault == "end" {
				itemJustifyDefault = "right"
			}
			gridColGap := columnGap
			gridRowGap := rowGap
			colTracks := parseGridTrackSpec(css["grid-template-columns"], contentWidth, viewportW, viewportH)
			autoRows := strings.TrimSpace(css["grid-template-rows"]) == ""
			rowTracks := parseGridTrackSpec(css["grid-template-rows"], contentHeight, viewportW, viewportH)
			// Subtract column gap space before distributing fr tracks so that
			// sum(tracks) + (numCols-1)*gap = contentWidth (no overflow).
			{
				hint := len(colTracks)
				if hint > 1 {
					adj := max(1, contentWidth-(hint-1)*gridColGap)
					colTracks = parseGridTrackSpec(css["grid-template-columns"], adj, viewportW, viewportH)
				}
			}
			if autoRows {
				rowTracks = make([]int, 0)
			}
			colStarts := make([]int, len(colTracks))
			cursorX := x + paddingLeft
			for i, cw := range colTracks {
				colStarts[i] = cursorX
				cursorX += cw + gridColGap
			}
			gridTracksWidth := 0
			for _, cw := range colTracks {
				gridTracksWidth += cw
			}
			if len(colTracks) > 1 {
				gridTracksWidth += (len(colTracks) - 1) * gridColGap
			}
			if gridTracksWidth < contentWidth {
				extra := contentWidth - gridTracksWidth
				offX := 0
				if containerJustify == "center" {
					offX = extra / 2
				} else if containerJustify == "end" || containerJustify == "right" {
					offX = extra
				}
				if offX > 0 {
					for i := range colStarts {
						colStarts[i] += offX
					}
				}
			}
			rowCursor := y + paddingTop
			rowIndex := 0
			colIndex := 0
			if len(rowTracks) == 0 {
				if autoRows {
					rowTracks = append(rowTracks, 0)
				} else {
					rowTracks = append(rowTracks, max(48, contentHeight))
				}
			}
			for _, child := range node.Children {
				childCSS := resolveNodeStyle(child, ss, viewportW)
				childCSS = mergeInheritedTextCSS(childCSS, css)
				if strings.EqualFold(strings.TrimSpace(childCSS["display"]), "none") {
					continue
				}
				intrinsicW, intrinsicH := intrinsicNodeSizeWithInherited(child, ss, viewportW, viewportH, css)
				span := cssGridSpan(childCSS["grid-column"])
				if span < 1 {
					span = 1
				}
				if span > len(colTracks) {
					span = len(colTracks)
				}
				if colIndex+span > len(colTracks) {
					rowIndex++
					colIndex = 0
				}
				if rowIndex > 0 && rowIndex > len(rowTracks)-1 {
					if autoRows {
						rowTracks = append(rowTracks, 0)
					} else {
						rowTracks = append(rowTracks, max(48, contentHeight/max(1, (len(node.Children)+len(colTracks)-1)/len(colTracks))))
					}
				}
				childX := colStarts[colIndex]
				childW := 0
				for ci := 0; ci < span && colIndex+ci < len(colTracks); ci++ {
					childW += colTracks[colIndex+ci]
					if ci < span-1 {
						childW += gridColGap
					}
				}
				childH := rowTracks[rowIndex]
				if autoRows {
					if intrinsicH > 0 {
						childH = max(childH, intrinsicH)
					}
					if childH <= 0 {
						childH = 48
					}
					rowTracks[rowIndex] = childH
				} else if childH <= 0 {
					childH = intrinsicH
				}
				if childW <= 0 {
					childW = intrinsicW
				}
				childY := rowCursor
				for ri := 0; ri < rowIndex; ri++ {
					childY += rowTracks[ri] + gridRowGap
				}
				slotW := childW
				slotH := childH
				itemJustify := strings.ToLower(strings.TrimSpace(childCSS["justify-self"]))
				if itemJustify == "" || itemJustify == "auto" {
					itemJustify = itemJustifyDefault
				}
				itemAlign := strings.ToLower(strings.TrimSpace(childCSS["align-self"]))
				if itemAlign == "" || itemAlign == "auto" {
					itemAlign = align
				}
				renderW := slotW
				renderH := slotH
				explicitW := nodeLayoutLength(child, childCSS, "width", "width", slotW, viewportW, viewportH, -1)
				explicitH := nodeLayoutLength(child, childCSS, "height", "height", slotH, viewportW, viewportH, -1)
				if itemJustify != "stretch" && intrinsicW > 0 {
					renderW = min(slotW, intrinsicW)
				}
				if itemAlign != "stretch" && intrinsicH > 0 {
					renderH = min(slotH, intrinsicH)
				}
				if itemJustify == "stretch" && explicitW > 0 && explicitW < slotW {
					renderW = explicitW
				}
				if itemAlign == "stretch" && explicitH > 0 && explicitH < slotH {
					renderH = explicitH
				}
				renderX := childX
				renderY := childY
				if itemJustify == "center" {
					renderX += max(0, (slotW-renderW)/2)
				} else if itemJustify == "right" || itemJustify == "end" {
					renderX += max(0, slotW-renderW)
				} else if itemJustify == "stretch" && renderW < slotW {
					renderX += max(0, (slotW-renderW)/2)
				}
				if itemAlign == "center" {
					renderY += max(0, (slotH-renderH)/2)
				} else if itemAlign == "end" || itemAlign == "bottom" {
					renderY += max(0, slotH-renderH)
				} else if itemAlign == "stretch" && renderH < slotH {
					renderY += max(0, (slotH-renderH)/2)
				}
				children = append(children, layoutNodeToNativeWithBox(child, renderX, renderY, renderW, renderH, viewportW, viewportH, ss, css))
				colIndex += span
				if colIndex >= len(colTracks) {
					rowIndex++
					colIndex = 0
				}
			}
			props := make(map[string]any, len(node.Props))
			for key, candidate := range node.Props {
				props[key] = uiValueToNative(candidate)
			}
			return map[string]any{
				"kind":     node.Kind,
				"props":    props,
				"layout":   map[string]any{"x": x, "y": y, "width": width, "height": height},
				"children": children,
			}
		}

		mainSize := contentHeight
		crossSize := contentWidth
		mainGap := rowGap
		crossGap := columnGap
		if direction == "row" {
			mainSize = contentWidth
			crossSize = contentHeight
			mainGap = columnGap
			crossGap = rowGap
		}

		mainLens := make([]int, len(node.Children))
		crossLens := make([]int, len(node.Children))
		mainMarginBefore := make([]int, len(node.Children))
		mainMarginAfter := make([]int, len(node.Children))
		crossMarginBefore := make([]int, len(node.Children))
		crossMarginAfter := make([]int, len(node.Children))
		fixedMain := 0
		totalFlex := 0.0
		for i, child := range node.Children {
			childCSS := resolveNodeStyle(child, ss, viewportW)
			childCSS = mergeInheritedTextCSS(childCSS, css)
			if strings.EqualFold(strings.TrimSpace(childCSS["display"]), "none") {
				continue
			}
			intrinsicW, intrinsicH := intrinsicNodeSizeWithInherited(child, ss, viewportW, viewportH, css)
			itemAlignPref := strings.ToLower(strings.TrimSpace(childCSS["align-self"]))
			if itemAlignPref == "" || itemAlignPref == "auto" {
				itemAlignPref = align
			}
			if direction == "row" {
				mainMarginBefore[i] = nodeLayoutLength(child, childCSS, "marginLeft", "margin-left", mainSize, viewportW, viewportH, 0)
				mainMarginAfter[i] = nodeLayoutLength(child, childCSS, "marginRight", "margin-right", mainSize, viewportW, viewportH, 0)
				crossMarginBefore[i] = nodeLayoutLength(child, childCSS, "marginTop", "margin-top", crossSize, viewportW, viewportH, 0)
				crossMarginAfter[i] = nodeLayoutLength(child, childCSS, "marginBottom", "margin-bottom", crossSize, viewportW, viewportH, 0)
			} else {
				mainMarginBefore[i] = nodeLayoutLength(child, childCSS, "marginTop", "margin-top", mainSize, viewportW, viewportH, 0)
				mainMarginAfter[i] = nodeLayoutLength(child, childCSS, "marginBottom", "margin-bottom", mainSize, viewportW, viewportH, 0)
				crossMarginBefore[i] = nodeLayoutLength(child, childCSS, "marginLeft", "margin-left", crossSize, viewportW, viewportH, 0)
				crossMarginAfter[i] = nodeLayoutLength(child, childCSS, "marginRight", "margin-right", crossSize, viewportW, viewportH, 0)
			}
			flex := nodeLayoutFloat(child, childCSS, "flex", "flex", 0)
			totalFlex += flex
			if direction == "row" {
				cw := nodeLayoutLength(child, childCSS, "width", "width", mainSize, viewportW, viewportH, -1)
				hasExplicitMain := cw >= 0
				if cw < 0 && flex <= 0 {
					cw = intrinsicW
				}
				if cw >= 0 && (hasExplicitMain || flex <= 0) {
					minW := nodeLayoutLength(child, childCSS, "minWidth", "min-width", mainSize, viewportW, viewportH, -1)
					maxW := nodeLayoutLength(child, childCSS, "maxWidth", "max-width", mainSize, viewportW, viewportH, -1)
					if minW >= 0 && cw < minW {
						cw = minW
					}
					if maxW >= 0 && cw > maxW {
						cw = maxW
					}
					mainLens[i] = cw
					fixedMain += cw + mainMarginBefore[i] + mainMarginAfter[i]
				}
				ch := nodeLayoutLength(child, childCSS, "height", "height", crossSize, viewportW, viewportH, -1)
				if ch < 0 && itemAlignPref != "stretch" {
					ch = intrinsicH
				}
				if ch >= 0 {
					minH := nodeLayoutLength(child, childCSS, "minHeight", "min-height", crossSize, viewportW, viewportH, -1)
					maxH := nodeLayoutLength(child, childCSS, "maxHeight", "max-height", crossSize, viewportW, viewportH, -1)
					if minH >= 0 && ch < minH {
						ch = minH
					}
					if maxH >= 0 && ch > maxH {
						ch = maxH
					}
					crossLens[i] = ch
				}
			} else {
				ch := nodeLayoutLength(child, childCSS, "height", "height", mainSize, viewportW, viewportH, -1)
				hasExplicitMain := ch >= 0
				if ch < 0 && flex <= 0 {
					ch = intrinsicH
				}
				if ch >= 0 && (hasExplicitMain || flex <= 0) {
					minH := nodeLayoutLength(child, childCSS, "minHeight", "min-height", mainSize, viewportW, viewportH, -1)
					maxH := nodeLayoutLength(child, childCSS, "maxHeight", "max-height", mainSize, viewportW, viewportH, -1)
					if minH >= 0 && ch < minH {
						ch = minH
					}
					if maxH >= 0 && ch > maxH {
						ch = maxH
					}
					mainLens[i] = ch
					fixedMain += ch + mainMarginBefore[i] + mainMarginAfter[i]
				}
				cw := nodeLayoutLength(child, childCSS, "width", "width", crossSize, viewportW, viewportH, -1)
				if cw < 0 && itemAlignPref != "stretch" {
					cw = intrinsicW
				}
				if cw >= 0 {
					minW := nodeLayoutLength(child, childCSS, "minWidth", "min-width", crossSize, viewportW, viewportH, -1)
					maxW := nodeLayoutLength(child, childCSS, "maxWidth", "max-width", crossSize, viewportW, viewportH, -1)
					if minW >= 0 && cw < minW {
						cw = minW
					}
					if maxW >= 0 && cw > maxW {
						cw = maxW
					}
					crossLens[i] = cw
				}
			}
		}

		remaining := mainSize - fixedMain - (max(0, len(node.Children)-1) * mainGap)
		if remaining < 0 {
			remaining = 0
		}
		if totalFlex > 0 {
			for i, child := range node.Children {
				if mainLens[i] > 0 {
					continue
				}
				childCSS := resolveNodeStyle(child, ss, viewportW)
				flex := nodeLayoutFloat(child, childCSS, "flex", "flex", 0)
				if flex > 0 {
					mainLens[i] = int((float64(remaining) * flex) / totalFlex)
				}
			}
		}

		totalMainUsed := 0
		for i, size := range mainLens {
			totalMainUsed += size + mainMarginBefore[i] + mainMarginAfter[i]
		}
		totalMainUsed += max(0, len(node.Children)-1) * mainGap
		extra := mainSize - totalMainUsed
		if extra < 0 {
			extra = 0
		}

		cursor := 0
		crossLineOffset := 0
		lineCrossMax := 0
		effectiveGap := mainGap
		wrap := strings.EqualFold(strings.TrimSpace(css["flex-wrap"]), "wrap")
		if justify == "center" {
			cursor = extra / 2
		} else if justify == "end" {
			cursor = extra
		} else if justify == "space-between" && len(node.Children) > 1 {
			effectiveGap = mainGap + (extra / (len(node.Children) - 1))
		} else if justify == "space-around" && len(node.Children) > 0 {
			effectiveGap = mainGap + (extra / len(node.Children))
			cursor = effectiveGap / 2
		}

		for i, child := range node.Children {
			childCSS := resolveNodeStyle(child, ss, viewportW)
			childCSS = mergeInheritedTextCSS(childCSS, css)
			if strings.EqualFold(strings.TrimSpace(childCSS["display"]), "none") {
				continue
			}
			intrinsicW, intrinsicH := intrinsicNodeSizeWithInherited(child, ss, viewportW, viewportH, css)
			aspectRatio, hasAspectRatio := parseAspectRatio(childCSS)
			hasExplicitWidth := strings.TrimSpace(childCSS["width"]) != ""
			hasExplicitHeight := strings.TrimSpace(childCSS["height"]) != ""
			mainLen := mainLens[i]
			if mainLen <= 0 {
				if direction == "row" {
					mainLen = max(120, intrinsicW)
				} else {
					mainLen = max(32, intrinsicH)
				}
			}
			if direction == "row" {
				minW := nodeLayoutLength(child, childCSS, "minWidth", "min-width", mainSize, viewportW, viewportH, -1)
				maxW := nodeLayoutLength(child, childCSS, "maxWidth", "max-width", mainSize, viewportW, viewportH, -1)
				if minW >= 0 && mainLen < minW {
					mainLen = minW
				}
				if maxW >= 0 && mainLen > maxW {
					mainLen = maxW
				}
			} else {
				minH := nodeLayoutLength(child, childCSS, "minHeight", "min-height", mainSize, viewportW, viewportH, -1)
				maxH := nodeLayoutLength(child, childCSS, "maxHeight", "max-height", mainSize, viewportW, viewportH, -1)
				if minH >= 0 && mainLen < minH {
					mainLen = minH
				}
				if maxH >= 0 && mainLen > maxH {
					mainLen = maxH
				}
			}
			crossLen := crossLens[i]
			if crossLen <= 0 {
				if align == "stretch" {
					crossLen = crossSize
				} else if direction == "column" {
					crossLen = min(crossSize, max(intrinsicW, 32))
				} else {
					crossLen = max(intrinsicH, min(140, crossSize))
				}
			}
			if direction == "row" {
				minH := nodeLayoutLength(child, childCSS, "minHeight", "min-height", crossSize, viewportW, viewportH, -1)
				maxH := nodeLayoutLength(child, childCSS, "maxHeight", "max-height", crossSize, viewportW, viewportH, -1)
				if minH >= 0 && crossLen < minH {
					crossLen = minH
				}
				if maxH >= 0 && crossLen > maxH {
					crossLen = maxH
				}
			} else {
				minW := nodeLayoutLength(child, childCSS, "minWidth", "min-width", crossSize, viewportW, viewportH, -1)
				maxW := nodeLayoutLength(child, childCSS, "maxWidth", "max-width", crossSize, viewportW, viewportH, -1)
				if minW >= 0 && crossLen < minW {
					crossLen = minW
				}
				if maxW >= 0 && crossLen > maxW {
					crossLen = maxW
				}
			}
			itemAlign := strings.ToLower(strings.TrimSpace(childCSS["align-self"]))
			if itemAlign == "" || itemAlign == "auto" {
				itemAlign = align
			}
			if hasAspectRatio && aspectRatio > 0.001 {
				if direction == "row" {
					if !hasExplicitHeight && itemAlign != "stretch" {
						crossLen = max(1, int(float64(mainLen)/aspectRatio))
					}
				} else {
					if !hasExplicitWidth && itemAlign != "stretch" {
						crossLen = max(1, int(float64(mainLen)*aspectRatio))
					}
				}
			}
			crossLen = max(1, crossLen-crossMarginBefore[i]-crossMarginAfter[i])
			crossPos := 0
			if itemAlign == "center" {
				crossPos = (crossSize - crossLen) / 2
			} else if itemAlign == "end" || itemAlign == "bottom" {
				crossPos = crossSize - crossLen
			}
			if crossPos < 0 {
				crossPos = 0
			}

			childX := x + paddingLeft
			childY := y + paddingTop
			childW := crossLen
			childH := mainLen
			if wrap && direction == "row" && cursor+mainMarginBefore[i]+mainLen+mainMarginAfter[i] > mainSize && cursor > 0 {
				cursor = 0
				crossLineOffset += lineCrossMax + crossGap
				lineCrossMax = 0
			}
			if wrap && direction == "column" && cursor+mainMarginBefore[i]+mainLen+mainMarginAfter[i] > mainSize && cursor > 0 {
				cursor = 0
				crossLineOffset += lineCrossMax + crossGap
				lineCrossMax = 0
			}
			if direction == "row" {
				childX += cursor + mainMarginBefore[i]
				childY += crossPos + crossMarginBefore[i] + crossLineOffset
				childW = mainLen
				childH = crossLen
			} else {
				childX += crossPos + crossMarginBefore[i] + crossLineOffset
				childY += cursor + mainMarginBefore[i]
				childW = crossLen
				childH = mainLen
			}
			children = append(children, layoutNodeToNativeWithBox(child, childX, childY, childW, childH, viewportW, viewportH, ss, css))
			if direction == "row" {
				lineCrossMax = max(lineCrossMax, childH+crossMarginBefore[i]+crossMarginAfter[i])
			} else {
				lineCrossMax = max(lineCrossMax, childW+crossMarginBefore[i]+crossMarginAfter[i])
			}
			cursor += mainMarginBefore[i] + mainLen + mainMarginAfter[i] + effectiveGap
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

	builder.AddTypedFunction("app_load_font", []string{TypeAny, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_load_font expects app handle")
		}
		if err := app.loadFont(args[1].String()); err != nil {
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
			Title:           title,
			Width:           int(args[2].Num),
			Height:          int(args[3].Num),
			Callbacks:       make(map[string]value.Value),
			debugProfile:    make(map[string]bool),
			hoverState:      make(map[string]bool),
			activeState:     make(map[string]bool),
			renderState:     make(map[string]uiRenderState),
			nextRenderState: make(map[string]uiRenderState),
			app:             app,
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
		window.mu.Lock()
		gw2 := window.gioWin
		window.mu.Unlock()
		if gw2 != nil {
			gw2.Option(gioapp.Title(title))
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
		if err := rerenderGioWindow(window); err != nil {
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
		if err := rerenderGioWindow(window); err != nil {
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
		app.mu.Lock()
		windows := append([]*uiWindow(nil), app.windows...)
		app.mu.Unlock()
		var wg sync.WaitGroup
		for _, window := range windows {
			if !window.Visible {
				continue
			}
			wg.Add(1)
			go func(win *uiWindow) {
				defer wg.Done()
				openGioWindow(win)
			}(window)
		}
		wg.Wait()
		return value.NilValue(), nil
	})

	// â”€â”€â”€ StyleSheet natives â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

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
		windows := append([]*uiWindow(nil), app.windows...)
		app.mu.Unlock()
		for _, win := range windows {
			_ = rerenderGioWindow(win)
		}
		return value.NilValue(), nil
	})

	// â”€â”€â”€ App quit / window close â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

	builder.AddTypedFunction("app_quit", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		app, ok := args[0].Object.(*uiApp)
		if !ok {
			return value.NilValue(), fmt.Errorf("app_quit expects app handle")
		}
		app.mu.Lock()
		windows := append([]*uiWindow(nil), app.windows...)
		app.mu.Unlock()
		for _, win := range windows {
			if win != nil {
				win.mu.Lock()
				gw := win.gioWin
				win.mu.Unlock()
				if gw != nil {
					gw.Perform(system.ActionClose)
				}
			}
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("window_close", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		window, ok := args[0].Object.(*uiWindow)
		if !ok {
			return value.NilValue(), fmt.Errorf("window_close expects window handle")
		}
		window.Visible = false
		window.mu.Lock()
		gw := window.gioWin
		window.mu.Unlock()
		if gw != nil {
			gw.Perform(system.ActionClose)
		}
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
			profile, profilerPath := parseWindowDebugProfile(flagsMap)
			window.debugProfile = profile
			window.debugProfilerPath = profilerPath
		default:
			window.mu.Unlock()
			return value.NilValue(), fmt.Errorf("window_debug expects bool or map<string, any>")
		}
		window.mu.Unlock()
		if err := rerenderGioWindow(window); err != nil {
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
		profile, profilerPath := parseWindowDebugProfile(flagsMap)

		window.mu.Lock()
		window.debugProfile = profile
		window.debugProfilerPath = profilerPath
		window.mu.Unlock()
		if err := rerenderGioWindow(window); err != nil {
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
