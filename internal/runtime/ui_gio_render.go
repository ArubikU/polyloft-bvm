package runtime

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"reflect"
	stdruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	gioapp "gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

// pointerTag is a stable heap-allocated identifier for Gio event routing.
type pointerTag struct {
	path string
}

type scrollHitInfo struct {
	rect   image.Rectangle
	hasV   bool
	hasH   bool
	trackV image.Rectangle
	thumbV image.Rectangle
	trackH image.Rectangle
	thumbH image.Rectangle
	maxX   int
	maxY   int
}

type debugComponentStat struct {
	Path          string
	Kind          string
	ID            string
	ClassName     string
	Component     string
	Tag           string
	Role          string
	StyleHint     string
	Attrs         map[string]string
	Samples       int64
	TotalMS       float64
	SelfTotalMS   float64
	MaxMS         float64
	SelfMaxMS     float64
	EstMemBytes   int64
	MaxMemBytes   int64
	LastRenderMS  float64
	LastSeenFrame int64
}

type resolvedCSSCacheEntry struct {
	hovered   bool
	active    bool
	viewW     int
	lowPower  bool
	parentSig string
	lastSeen  int64
	css       map[string]string
}

type zChildrenHintCacheEntry struct {
	count    int
	sig      uint64
	needs    bool
	lastSeen int64
}

type backgroundFilterFactors struct {
	brightness float64
	contrast   float64
	saturate   float64
	grayscale  float64
	invert     float64
}

var (
	renderCfgOnce    sync.Once
	renderGPUEnabled = true
	renderLowPower   = false
)

var (
	gradientStopsCacheMu    sync.RWMutex
	gradientStopsCache      = map[string][]gradientStop{}
	repeatingPxStopsCacheMu sync.RWMutex
	repeatingPxStopsCache   = map[string]struct {
		stops []gradientStopPx
		ok    bool
	}{}
	backgroundLayersCacheMu sync.RWMutex
	backgroundLayersCache   = map[string][]string{}
)

func cloneGradientStops(in []gradientStop) []gradientStop {
	if len(in) == 0 {
		return nil
	}
	out := make([]gradientStop, len(in))
	copy(out, in)
	return out
}

func cloneGradientStopsPx(in []gradientStopPx) []gradientStopPx {
	if len(in) == 0 {
		return nil
	}
	out := make([]gradientStopPx, len(in))
	copy(out, in)
	return out
}

func backgroundLayers(raw string) []string {
	backgroundLayersCacheMu.RLock()
	if cached, ok := backgroundLayersCache[raw]; ok {
		backgroundLayersCacheMu.RUnlock()
		out := make([]string, len(cached))
		copy(out, cached)
		return out
	}
	backgroundLayersCacheMu.RUnlock()

	layers := splitCommaOutsideParens(raw)
	if len(layers) == 0 {
		layers = []string{raw}
	}
	clean := make([]string, 0, len(layers))
	for _, l := range layers {
		lt := strings.TrimSpace(l)
		if lt != "" {
			clean = append(clean, lt)
		}
	}

	backgroundLayersCacheMu.Lock()
	if len(backgroundLayersCache) > 1024 {
		backgroundLayersCache = map[string][]string{}
	}
	copyClean := make([]string, len(clean))
	copy(copyClean, clean)
	backgroundLayersCache[raw] = copyClean
	backgroundLayersCacheMu.Unlock()

	out := make([]string, len(clean))
	copy(out, clean)
	return out
}

func parseBackgroundFilterFactors(css map[string]string) backgroundFilterFactors {
	ff := backgroundFilterFactors{brightness: 1.0, contrast: 1.0, saturate: 1.0, grayscale: 0.0, invert: 0.0}
	if rawBright := strings.TrimSpace(css["__brightness_factor"]); rawBright != "" {
		if f, err := strconv.ParseFloat(rawBright, 64); err == nil {
			ff.brightness = f
		}
	}
	if rawContrast := strings.TrimSpace(css["__contrast_factor"]); rawContrast != "" {
		if f, err := strconv.ParseFloat(rawContrast, 64); err == nil {
			ff.contrast = f
		}
	}
	if rawSaturate := strings.TrimSpace(css["__saturate_factor"]); rawSaturate != "" {
		if f, err := strconv.ParseFloat(rawSaturate, 64); err == nil {
			ff.saturate = f
		}
	}
	if rawGrayscale := strings.TrimSpace(css["__grayscale_factor"]); rawGrayscale != "" {
		if f, err := strconv.ParseFloat(rawGrayscale, 64); err == nil {
			ff.grayscale = f
		}
	}
	if rawInvert := strings.TrimSpace(css["__invert_factor"]); rawInvert != "" {
		if f, err := strconv.ParseFloat(rawInvert, 64); err == nil {
			ff.invert = f
		}
	}
	return ff
}

func initRenderConfig() {
	renderCfgOnce.Do(func() {
		gpuEnv := strings.TrimSpace(strings.ToLower(os.Getenv("POLYLOFT_GIO_GPU")))
		if gpuEnv == "0" || gpuEnv == "false" || gpuEnv == "off" {
			renderGPUEnabled = false
		}
		lowEnv := strings.TrimSpace(strings.ToLower(os.Getenv("POLYLOFT_GIO_LOW_POWER")))
		if lowEnv == "1" || lowEnv == "true" || lowEnv == "on" {
			renderLowPower = true
		}
	})
}

// gioWindowState holds per-window Gio rendering state that persists between frames.
type gioWindowState struct {
	shaper            *text.Shaper
	tags              map[string]*pointerTag
	handlers          map[string]func()
	propsForPath      map[string]map[string]any
	cssForPath        map[string]map[string]string
	scrollOffsets     map[string]image.Point // per-element scroll position, persists across frames
	scrollCarry       map[string]f32.Point   // fractional scroll accumulation for high-res devices
	scrollHits        map[string]scrollHitInfo
	scrollFocusPath   string
	scrollDragPath    string
	scrollDragAxis    string
	scrollDragGrab    int
	boundsForPath     map[string]image.Rectangle
	lastPointer       map[string]image.Point
	pointerPos        image.Point
	pointerKnown      bool
	editors           map[string]*widget.Editor
	inputValues       map[string]string
	inputExternal     map[string]string
	inputScrollX      map[string]int
	inputContentW     map[string]int
	inputVisibleW     map[string]int
	sliderValues      map[string]float64
	boolValues        map[string]bool
	inputFocused      map[string]bool // Track if input path has focus
	focusedInputPath  string
	pickerModalOpen   string // path of open picker, empty if none
	pickerType        string // "date" or "time"
	pickerValue       string // current selected value in picker
	frameViewW        int
	frameViewH        int
	frameViewportRect image.Rectangle
	frameViewport48   image.Rectangle
	frameViewport96   image.Rectangle
	frameHoverState   map[string]bool
	frameActiveState  map[string]bool
	frameDebug        bool
	frameStyleSheet   *styleSheet
	frameNumber       int64
	profileSampleFrame bool
	frameCursorPath   string
	frameCursorValue  string
	profileComponents bool
	profileFull       bool
	resolvedCSS       map[string]resolvedCSSCacheEntry
	zChildrenHint     map[string]zChildrenHintCacheEntry
}

func newGioWindowState() *gioWindowState {
	return &gioWindowState{
		shaper:            text.NewShaper(text.WithCollection(gofont.Collection())),
		tags:              make(map[string]*pointerTag),
		handlers:          make(map[string]func()),
		propsForPath:      make(map[string]map[string]any),
		cssForPath:        make(map[string]map[string]string),
		scrollOffsets:     make(map[string]image.Point),
		scrollCarry:       make(map[string]f32.Point),
		scrollHits:        make(map[string]scrollHitInfo),
		boundsForPath:     make(map[string]image.Rectangle),
		lastPointer:       make(map[string]image.Point),
		pointerPos:        image.Point{},
		pointerKnown:      false,
		editors:           make(map[string]*widget.Editor),
		inputValues:       make(map[string]string),
		inputExternal:     make(map[string]string),
		inputScrollX:      make(map[string]int),
		inputContentW:     make(map[string]int),
		inputVisibleW:     make(map[string]int),
		sliderValues:      make(map[string]float64),
		boolValues:        make(map[string]bool),
		inputFocused:      make(map[string]bool),
		focusedInputPath:  "",
		pickerModalOpen:   "",
		pickerType:        "",
		pickerValue:       "",
		frameViewW:        0,
		frameViewH:        0,
		frameViewportRect: image.Rectangle{},
		frameViewport48:   image.Rectangle{},
		frameViewport96:   image.Rectangle{},
		frameHoverState:   make(map[string]bool),
		frameActiveState:  make(map[string]bool),
		frameDebug:        false,
		frameStyleSheet:   nil,
		frameNumber:       0,
		profileSampleFrame: false,
		frameCursorPath:   "",
		frameCursorValue:  "",
		profileComponents: false,
		profileFull:       false,
		resolvedCSS:       make(map[string]resolvedCSSCacheEntry),
		zChildrenHint:     make(map[string]zChildrenHintCacheEntry),
	}
}

func childSliceSignature(children []any) uint64 {
	if len(children) == 0 {
		return 0
	}
	var sig uint64 = 1469598103934665603
	const prime uint64 = 1099511628211
	for _, child := range children {
		cm := anyToMap(child)
		if len(cm) == 0 {
			sig ^= 0x9e3779b97f4a7c15
			sig *= prime
			continue
		}
		ptr := uint64(reflect.ValueOf(cm).Pointer())
		sig ^= ptr + 0x9e3779b97f4a7c15 + (sig << 6) + (sig >> 2)
		sig *= prime
	}
	return sig
}

func (gs *gioWindowState) purgeStaleCaches(frame int64) {
	if frame <= 0 {
		return
	}
	const ttl = int64(240)
	for k, v := range gs.resolvedCSS {
		if frame-v.lastSeen > ttl {
			delete(gs.resolvedCSS, k)
		}
	}
	for k, v := range gs.zChildrenHint {
		if frame-v.lastSeen > ttl {
			delete(gs.zChildrenHint, k)
		}
	}
}

func refreshBoolMap(dst map[string]bool, src map[string]bool) map[string]bool {
	if len(src) == 0 {
		if dst != nil {
			clear(dst)
		}
		return dst
	}
	if dst == nil {
		dst = make(map[string]bool, len(src))
	} else {
		clear(dst)
	}
	for k, v := range src {
		if strings.TrimSpace(k) == "" {
			continue
		}
		dst[k] = v
	}
	return dst
}

func childPath(parent string, idx int) string {
	return parent + "/" + strconv.Itoa(idx)
}

func lowerASCIIIfNeeded(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			return strings.ToLower(s)
		}
	}
	return s
}

func inheritedTextCSSSignature(parent map[string]string) string {
	if len(parent) == 0 {
		return ""
	}
	keys := []string{"color", "font-family", "font-size", "font-style", "font-weight", "line-height", "letter-spacing", "text-align", "text-transform", "white-space"}
	var b strings.Builder
	b.Grow(128)
	for _, k := range keys {
		v := strings.TrimSpace(parent[k])
		if v == "" {
			continue
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte(';')
	}
	return b.String()
}

func (gs *gioWindowState) getTag(path string) *pointerTag {
	if t, ok := gs.tags[path]; ok {
		return t
	}
	t := &pointerTag{path: path}
	gs.tags[path] = t
	return t
}

// clearFrame resets per-frame state (handlers), but keeps tags (stable pointers).
func (gs *gioWindowState) clearFrame() {
	clear(gs.handlers)
	clear(gs.propsForPath)
	clear(gs.cssForPath)
	clear(gs.scrollHits)
	clear(gs.boundsForPath)
	gs.frameCursorPath = ""
	gs.frameCursorValue = ""
}

func expandedViewportRect(viewW, viewH, margin int) image.Rectangle {
	return image.Rect(-margin, -margin, viewW+margin, viewH+margin)
}

func nodeRenderRect(x, y, w, h int, tx, ty float64, scale float64) image.Rectangle {
	r := image.Rect(x+int(tx), y+int(ty), x+w+int(tx), y+h+int(ty))
	if scale <= 0 || mathAbs(scale-1.0) < 0.001 {
		return r
	}
	cx := float64(r.Min.X+r.Max.X) / 2.0
	cy := float64(r.Min.Y+r.Max.Y) / 2.0
	hw := float64(r.Dx()) * scale / 2.0
	hh := float64(r.Dy()) * scale / 2.0
	return image.Rect(int(cx-hw), int(cy-hh), int(cx+hw), int(cy+hh))
}

func pointInRect(p image.Point, r image.Rectangle) bool {
	return p.X >= r.Min.X && p.X < r.Max.X && p.Y >= r.Min.Y && p.Y < r.Max.Y
}

func nodeHasExplicitZIndex(props map[string]any) bool {
	if props == nil {
		return false
	}
	for _, key := range []string{"z-index", "zIndex"} {
		if raw, ok := props[key]; ok {
			v := strings.TrimSpace(anyToString(raw, ""))
			if v != "" && v != "0" && v != "auto" && v != "Auto" && v != "AUTO" {
				return true
			}
		}
	}
	styleRaw := strings.ToLower(strings.TrimSpace(anyToString(props["style"], "")))
	if strings.Contains(styleRaw, "z-index") {
		return true
	}
	return false
}

func parseZIndexFromPropsFast(props map[string]any) int {
	if len(props) == 0 {
		return 0
	}
	for _, key := range []string{"z-index", "zIndex"} {
		if raw, ok := props[key]; ok {
			v := strings.TrimSpace(anyToString(raw, ""))
			if v == "" || v == "auto" || v == "Auto" || v == "AUTO" {
				continue
			}
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	if styleRaw, ok := props["style"]; ok {
		style := strings.ToLower(anyToString(styleRaw, ""))
		if idx := strings.Index(style, "z-index:"); idx >= 0 {
			rest := style[idx+len("z-index:"):]
			if semi := strings.Index(rest, ";"); semi >= 0 {
				rest = rest[:semi]
			}
			rest = strings.TrimSpace(rest)
			if n, err := strconv.Atoi(rest); err == nil {
				return n
			}
		}
	}
	return 0
}

func mayContainOutOfFlowChildren(children []any) bool {
	for _, child := range children {
		cmap := anyToMap(child)
		props := anyToMap(cmap["props"])
		if len(props) == 0 {
			continue
		}
		pos := strings.ToLower(strings.TrimSpace(anyToString(props["position"], "")))
		if pos == "absolute" || pos == "fixed" || pos == "sticky" {
			return true
		}
		if _, ok := props["top"]; ok {
			return true
		}
		if _, ok := props["left"]; ok {
			return true
		}
		if _, ok := props["right"]; ok {
			return true
		}
		if _, ok := props["bottom"]; ok {
			return true
		}
		if styleRaw, ok := props["style"]; ok {
			style := strings.ToLower(anyToString(styleRaw, ""))
			if strings.Contains(style, "position:") || strings.Contains(style, "transform:") {
				return true
			}
		}
	}
	return false
}

func inputValueString(candidate any) string {
	switch v := candidate.(type) {
	case string:
		return strings.ToValidUTF8(v, "")
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ""
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		fv := float64(v)
		if math.IsNaN(fv) || math.IsInf(fv, 0) {
			return ""
		}
		return strconv.FormatFloat(fv, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func inputExternalValue(props map[string]any) string {
	if props == nil {
		return ""
	}
	if raw, ok := props["text"]; ok {
		if s := strings.TrimSpace(inputValueString(raw)); s != "" {
			return s
		}
	}
	if raw, ok := props["value"]; ok {
		return strings.TrimSpace(inputValueString(raw))
	}
	return ""
}

func inputPropFloat(props map[string]any, key string) (float64, bool) {
	if props == nil {
		return 0, false
	}
	raw, ok := props[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case float32:
		fv := float64(v)
		if math.IsNaN(fv) || math.IsInf(fv, 0) {
			return 0, false
		}
		return fv, true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func numberStepFromProps(props map[string]any) (float64, bool) {
	if props == nil {
		return 0, false
	}
	raw, ok := props["step"]
	if !ok {
		return 0, false
	}
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" || s == "any" {
			return 0, false
		}
	}
	step, has := inputPropFloat(props, "step")
	if !has || step <= 0 {
		return 0, false
	}
	return step, true
}

func sanitizeNumberLive(raw string) string {
	raw = strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	hasSign := false
	hasDot := false
	for i, r := range raw {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case (r == '+' || r == '-') && i == 0 && !hasSign:
			hasSign = true
			b.WriteRune(r)
		case r == '.' && !hasDot:
			hasDot = true
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeNumberInput(raw string, props map[string]any, finalize bool) string {
	clean := sanitizeNumberLive(raw)
	if clean == "" {
		return ""
	}
	if !finalize {
		switch clean {
		case "+", "-", ".", "+.", "-.":
			return clean
		}
		return clean
	}
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	if minV, ok := inputPropFloat(props, "min"); ok && v < minV {
		v = minV
	}
	if maxV, ok := inputPropFloat(props, "max"); ok && v > maxV {
		v = maxV
	}
	if step, ok := numberStepFromProps(props); ok {
		base := 0.0
		if minV, hasMin := inputPropFloat(props, "min"); hasMin {
			base = minV
		}
		steps := math.Round((v - base) / step)
		v = base + steps*step
		if minV, ok := inputPropFloat(props, "min"); ok && v < minV {
			v = minV
		}
		if maxV, ok := inputPropFloat(props, "max"); ok && v > maxV {
			v = maxV
		}
	}
	if math.Abs(v) < 1e-12 {
		v = 0
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func sanitizeDateLive(raw string) string {
	raw = strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(min(len(raw), 10))
	for _, r := range raw {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == '/' || r == '-' {
			b.WriteRune('-')
		}
		if b.Len() >= 10 {
			break
		}
	}
	return b.String()
}

func normalizeDateInput(raw string, finalize bool) string {
	clean := sanitizeDateLive(raw)
	if clean == "" {
		return ""
	}
	if !finalize {
		return clean
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02", "02-01-2006", "02/01/2006"} {
		if parsed, err := time.Parse(layout, clean); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func sanitizeTimeLive(raw string) string {
	raw = strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(min(len(raw), 8))
	for _, r := range raw {
		if unicode.IsDigit(r) || r == ':' {
			b.WriteRune(r)
		}
		if b.Len() >= 8 {
			break
		}
	}
	return b.String()
}

func normalizeTimeInput(raw string, props map[string]any, finalize bool) string {
	clean := sanitizeTimeLive(raw)
	if clean == "" {
		return ""
	}
	if !finalize {
		return clean
	}
	step, hasStep := numberStepFromProps(props)
	wantSeconds := strings.Count(clean, ":") == 2 || (hasStep && step < 60)
	for _, layout := range []string{"15:04", "15:04:05"} {
		if parsed, err := time.Parse(layout, clean); err == nil {
			if wantSeconds {
				return parsed.Format("15:04:05")
			}
			return parsed.Format("15:04")
		}
	}
	return ""
}

func normalizeTypedInputValue(inputType, raw string, props map[string]any, finalize bool) string {
	switch strings.ToLower(strings.TrimSpace(inputType)) {
	case "number":
		return normalizeNumberInput(raw, props, finalize)
	case "date":
		return normalizeDateInput(raw, finalize)
	case "time":
		return normalizeTimeInput(raw, props, finalize)
	default:
		return strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	}
}

func stepNumberInputValue(current string, props map[string]any, delta int) string {
	if delta == 0 {
		return normalizeNumberInput(current, props, true)
	}
	step := 1.0
	if customStep, ok := numberStepFromProps(props); ok {
		step = customStep
	}
	cur := 0.0
	normalizedCurrent := normalizeNumberInput(current, props, true)
	if normalizedCurrent != "" {
		if parsed, err := strconv.ParseFloat(normalizedCurrent, 64); err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
			cur = parsed
		}
	} else if minV, ok := inputPropFloat(props, "min"); ok {
		cur = minV
	}
	cur += float64(delta) * step
	return normalizeNumberInput(strconv.FormatFloat(cur, 'f', -1, 64), props, true)
}

func dispatchWindowEventAny(window *uiWindow, eventName string, payload map[string]any) error {
	return dispatchWindowEvent(window, eventName, nativeToValue(payload))
}

func cssCursorToGio(raw string) pointer.Cursor {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "auto", "default", "initial", "inherit", "unset":
		return pointer.CursorDefault
	case "none":
		return pointer.CursorNone
	case "pointer":
		return pointer.CursorPointer
	case "text":
		return pointer.CursorText
	case "vertical-text":
		return pointer.CursorVerticalText
	case "crosshair":
		return pointer.CursorCrosshair
	case "move", "all-scroll":
		return pointer.CursorAllScroll
	case "grab":
		return pointer.CursorGrab
	case "grabbing":
		return pointer.CursorGrabbing
	case "not-allowed", "no-drop":
		return pointer.CursorNotAllowed
	case "wait":
		return pointer.CursorWait
	case "progress":
		return pointer.CursorProgress
	case "col-resize":
		return pointer.CursorColResize
	case "row-resize":
		return pointer.CursorRowResize
	case "n-resize":
		return pointer.CursorNorthResize
	case "s-resize":
		return pointer.CursorSouthResize
	case "e-resize":
		return pointer.CursorEastResize
	case "w-resize":
		return pointer.CursorWestResize
	case "ns-resize":
		return pointer.CursorNorthSouthResize
	case "ew-resize":
		return pointer.CursorEastWestResize
	case "ne-resize":
		return pointer.CursorNorthEastResize
	case "nw-resize":
		return pointer.CursorNorthWestResize
	case "se-resize":
		return pointer.CursorSouthEastResize
	case "sw-resize":
		return pointer.CursorSouthWestResize
	case "nesw-resize":
		return pointer.CursorNorthEastSouthWestResize
	case "nwse-resize":
		return pointer.CursorNorthWestSouthEastResize
	default:
		return pointer.CursorDefault
	}
}

func scrollTargetForPoint(state *gioWindowState, preferredPath string, p image.Point) string {
	if hit, ok := state.scrollHits[preferredPath]; ok && (hit.hasV || hit.hasH) {
		return preferredPath
	}
	bestPath := ""
	bestArea := int(^uint(0) >> 1)
	for path, hit := range state.scrollHits {
		if !(hit.hasV || hit.hasH) {
			continue
		}
		if !pointInRect(p, hit.rect) {
			continue
		}
		area := hit.rect.Dx() * hit.rect.Dy()
		if area < bestArea {
			bestArea = area
			bestPath = path
		}
	}
	return bestPath
}

func clampScrollForPath(path string, state *gioWindowState) bool {
	hit, ok := state.scrollHits[path]
	if !ok {
		return false
	}
	cur := state.scrollOffsets[path]
	old := cur
	if cur.X < 0 {
		cur.X = 0
	}
	if cur.Y < 0 {
		cur.Y = 0
	}
	if cur.X > hit.maxX {
		cur.X = hit.maxX
	}
	if cur.Y > hit.maxY {
		cur.Y = hit.maxY
	}
	if cur != old {
		state.scrollOffsets[path] = cur
		return true
	}
	return false
}

func applyKeyboardScroll(path string, kev key.Event, state *gioWindowState) bool {
	hit, ok := state.scrollHits[path]
	if !ok {
		return false
	}
	cur := state.scrollOffsets[path]
	old := cur
	lineY := 36
	lineX := 36
	pageY := max(1, hit.rect.Dy()-24)

	switch kev.Name {
	case key.NameDownArrow:
		cur.Y += lineY
	case key.NameUpArrow:
		cur.Y -= lineY
	case key.NameRightArrow:
		cur.X += lineX
	case key.NameLeftArrow:
		cur.X -= lineX
	case key.NamePageDown:
		cur.Y += pageY
	case key.NamePageUp:
		cur.Y -= pageY
	case key.NameHome:
		cur.Y = 0
		cur.X = 0
	case key.NameEnd:
		cur.Y = hit.maxY
	}

	state.scrollOffsets[path] = cur
	changed := clampScrollForPath(path, state)
	if !changed && state.scrollOffsets[path] != old {
		changed = true
	}
	return changed
}

func updateScrollByThumb(path string, p image.Point, state *gioWindowState, axis string) bool {
	hit, ok := state.scrollHits[path]
	if !ok {
		return false
	}
	cur := state.scrollOffsets[path]
	changed := false
	if axis == "y" && hit.hasV {
		track := hit.trackV
		thumb := hit.thumbV
		usable := max(1, track.Dy()-thumb.Dy())
		pos := p.Y - track.Min.Y - state.scrollDragGrab
		if pos < 0 {
			pos = 0
		}
		if pos > usable {
			pos = usable
		}
		next := 0
		if hit.maxY > 0 {
			next = int(float64(pos) * float64(hit.maxY) / float64(usable))
		}
		if next != cur.Y {
			cur.Y = next
			changed = true
		}
	}
	if axis == "x" && hit.hasH {
		track := hit.trackH
		thumb := hit.thumbH
		usable := max(1, track.Dx()-thumb.Dx())
		pos := p.X - track.Min.X - state.scrollDragGrab
		if pos < 0 {
			pos = 0
		}
		if pos > usable {
			pos = usable
		}
		next := 0
		if hit.maxX > 0 {
			next = int(float64(pos) * float64(hit.maxX) / float64(usable))
		}
		if next != cur.X {
			cur.X = next
			changed = true
		}
	}
	if changed {
		state.scrollOffsets[path] = cur
	}
	return changed
}

func handleScrollbarPress(path string, p image.Point, state *gioWindowState) (handled bool, changed bool) {
	hit, ok := state.scrollHits[path]
	if !ok {
		return false, false
	}
	if hit.hasV && pointInRect(p, hit.thumbV) {
		state.scrollDragPath = path
		state.scrollDragAxis = "y"
		state.scrollDragGrab = p.Y - hit.thumbV.Min.Y
		return true, false
	}
	if hit.hasH && pointInRect(p, hit.thumbH) {
		state.scrollDragPath = path
		state.scrollDragAxis = "x"
		state.scrollDragGrab = p.X - hit.thumbH.Min.X
		return true, false
	}
	if hit.hasV && pointInRect(p, hit.trackV) {
		state.scrollDragPath = path
		state.scrollDragAxis = "y"
		state.scrollDragGrab = hit.thumbV.Dy() / 2
		return true, updateScrollByThumb(path, p, state, "y")
	}
	if hit.hasH && pointInRect(p, hit.trackH) {
		state.scrollDragPath = path
		state.scrollDragAxis = "x"
		state.scrollDragGrab = hit.thumbH.Dx() / 2
		return true, updateScrollByThumb(path, p, state, "x")
	}
	return false, false
}

// processGioEvents reads all pending pointer events for registered tags.
// Returns true if a re-render is needed.
func processGioEvents(gtx layout.Context, window *uiWindow, state *gioWindowState, gw *gioapp.Window, frameTime time.Time) bool {
	needInvalidate := false
	for path, tag := range state.tags {
		for {
			ev, ok := gtx.Source.Event(pointer.Filter{
				Target:  tag,
				Kinds:   pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll,
				ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			state.pointerPos = image.Pt(int(pe.Position.X), int(pe.Position.Y))
			state.pointerKnown = true
			switch pe.Kind {
			case pointer.Enter:
				if !isPathHovered(window, path) {
					window.mu.Lock()
					if window.hoverState == nil {
						window.hoverState = make(map[string]bool)
					}
					window.hoverState[path] = true
					window.mu.Unlock()
					needInvalidate = true
					if gw != nil {
						gw.Invalidate()
					}
				}
			case pointer.Leave:
				if isPathHovered(window, path) {
					window.mu.Lock()
					if window.hoverState != nil {
						delete(window.hoverState, path)
					}
					window.mu.Unlock()
					needInvalidate = true
					if gw != nil {
						gw.Invalidate()
					}
				}
			case pointer.Press:
				state.lastPointer[path] = image.Pt(int(pe.Position.X), int(pe.Position.Y))
				if handled, changed := handleScrollbarPress(path, state.lastPointer[path], state); handled {
					if changed {
						needInvalidate = true
						if gw != nil {
							gw.Invalidate()
						}
					}
					continue
				}
				// Track focus transitions for input elements.
				nextFocusedInput := ""
				if tag.path != "" {
					candidatePath := tag.path
					if strings.HasSuffix(candidatePath, "__spin") || strings.HasSuffix(candidatePath, "__picker") {
						candidatePath = strings.TrimSuffix(strings.TrimSuffix(candidatePath, "__spin"), "__picker")
					}
					if inputProps, ok := state.propsForPath[candidatePath]; ok {
						inputType := strings.ToLower(anyToString(inputProps["type"], anyToString(inputProps["inputtype"], "")))
						if inputType != "" || anyToString(inputProps["tag"], "") == "input" {
							nextFocusedInput = candidatePath
						}
					}
				}
				if nextFocusedInput != state.focusedInputPath {
					previousFocusedInput := state.focusedInputPath
					for k := range state.inputFocused {
						state.inputFocused[k] = false
					}
					if nextFocusedInput != "" {
						state.inputFocused[nextFocusedInput] = true
					}
					state.focusedInputPath = nextFocusedInput

					if previousFocusedInput != "" {
						if prevProps, ok := state.propsForPath[previousFocusedInput]; ok {
							evBlur := strings.TrimSpace(anyToString(prevProps["onblur"], ""))
							if evBlur != "" {
								blurPayload := map[string]any{
									"source":    previousFocusedInput,
									"component": strings.ToLower(anyToString(prevProps["type"], anyToString(prevProps["inputtype"], "input"))),
									"value":     state.inputValues[previousFocusedInput],
									"focused":   false,
									"timestamp": frameTime.UnixMilli(),
								}
								_ = dispatchWindowEventAny(window, evBlur, blurPayload)
							}
						}
					}

					if nextFocusedInput != "" {
						if nextProps, ok := state.propsForPath[nextFocusedInput]; ok {
							evFocus := strings.TrimSpace(anyToString(nextProps["onfocus"], ""))
							if evFocus != "" {
								focusPayload := map[string]any{
									"source":    nextFocusedInput,
									"component": strings.ToLower(anyToString(nextProps["type"], anyToString(nextProps["inputtype"], "input"))),
									"value":     state.inputValues[nextFocusedInput],
									"focused":   true,
									"timestamp": frameTime.UnixMilli(),
								}
								_ = dispatchWindowEventAny(window, evFocus, focusPayload)
							}
						}
					}
					needInvalidate = true
				}
				if h, ok := state.handlers[path]; ok {
					// Check active CSS selector
					props := state.propsForPath[path]
					if props != nil {
						var appSS *styleSheet
						if window.app != nil {
							window.app.mu.Lock()
							appSS = window.app.stylesheet
							window.app.mu.Unlock()
						}
						if hasActiveSelector(props, appSS) {
							window.mu.Lock()
							if window.activeState == nil {
								window.activeState = make(map[string]bool)
							}
							window.activeState[path] = true
							window.mu.Unlock()
							capturedPath := path
							capturedWin := window
							time.AfterFunc(140*time.Millisecond, func() {
								capturedWin.mu.Lock()
								if capturedWin.activeState != nil {
									delete(capturedWin.activeState, capturedPath)
								}
								capturedWin.mu.Unlock()
								capturedWin.mu.Lock()
								gw := capturedWin.gioWin
								capturedWin.mu.Unlock()
								if gw != nil {
									gw.Invalidate()
								}
							})
							needInvalidate = true
						}
					}
					h()
				}
			case pointer.Drag:
				state.lastPointer[path] = image.Pt(int(pe.Position.X), int(pe.Position.Y))
				if state.scrollDragPath == path && (state.scrollDragAxis == "x" || state.scrollDragAxis == "y") {
					if updateScrollByThumb(path, state.lastPointer[path], state, state.scrollDragAxis) {
						needInvalidate = true
						if gw != nil {
							gw.Invalidate()
						}
					}
					continue
				}
				if h, ok := state.handlers[path]; ok {
					props := state.propsForPath[path]
					if props != nil {
						t := strings.ToLower(anyToString(props["__interactive"], ""))
						if t == "slider" || t == "range" {
							h()
						}
					}
				}
			case pointer.Release:
				if state.scrollDragPath == path {
					state.scrollDragPath = ""
					state.scrollDragAxis = ""
					state.scrollDragGrab = 0
				}
			case pointer.Scroll:
				if ed := state.editors[path]; ed != nil && ed.SingleLine {
					contentW := state.inputContentW[path]
					visibleW := state.inputVisibleW[path]
					maxScroll := max(0, contentW-visibleW)
					if maxScroll > 0 {
						axis := pe.Scroll.X
						if math.Abs(float64(pe.Scroll.Y)) > math.Abs(float64(axis)) {
							axis = pe.Scroll.Y
						}
						cur := state.inputScrollX[path]
						next := cur + int(-axis*12)
						if next < 0 {
							next = 0
						}
						if next > maxScroll {
							next = maxScroll
						}
						if next != cur {
							state.inputScrollX[path] = next
							needInvalidate = true
							window.mu.Lock()
							gw := window.gioWin
							window.mu.Unlock()
							if gw != nil {
								gw.Invalidate()
							}
						}
						continue
					}
				}
				pointerPos := image.Pt(int(pe.Position.X), int(pe.Position.Y))
				targetPath := scrollTargetForPoint(state, path, pointerPos)
				if targetPath == "" {
					continue
				}
				state.scrollFocusPath = targetPath
				css := state.cssForPath[targetPath]
				overflow := strings.ToLower(strings.TrimSpace(css["overflow"]))
				overflowX := strings.ToLower(strings.TrimSpace(css["overflow-x"]))
				overflowY := strings.ToLower(strings.TrimSpace(css["overflow-y"]))
				if overflowX == "" {
					overflowX = overflow
				}
				if overflowY == "" {
					overflowY = overflow
				}
				if overflowX == "auto" || overflowX == "scroll" || overflowY == "auto" || overflowY == "scroll" {
					cur := state.scrollOffsets[targetPath]
					carry := state.scrollCarry[targetPath]

					dx := carry.X + pe.Scroll.X
					dy := carry.Y + pe.Scroll.Y

					stepX := 0
					for dx >= 1 {
						stepX++
						dx -= 1
					}
					for dx <= -1 {
						stepX--
						dx += 1
					}

					stepY := 0
					for dy >= 1 {
						stepY++
						dy -= 1
					}
					for dy <= -1 {
						stepY--
						dy += 1
					}

					state.scrollCarry[targetPath] = f32.Pt(dx, dy)
					cur.X += stepX
					cur.Y += stepY
					state.scrollOffsets[targetPath] = cur
					_ = clampScrollForPath(targetPath, state)
					needInvalidate = true
					window.mu.Lock()
					gw := window.gioWin
					window.mu.Unlock()
					if gw != nil {
						gw.Invalidate()
					}
				}
			}
		}

		if state.scrollFocusPath == path {
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameDownArrow})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameUpArrow})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameRightArrow})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameLeftArrow})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NamePageDown})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NamePageUp})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameHome})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
			for {
				ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameEnd})
				if !ok {
					break
				}
				if kev, ok := ev.(key.Event); ok && kev.State == key.Press {
					if applyKeyboardScroll(path, kev, state) {
						needInvalidate = true
					}
				}
			}
		}
	}
	return needInvalidate
}

// toNRGBA converts any color.Color to color.NRGBA.
func toNRGBA(c color.Color) color.NRGBA {
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// drawGioRect draws a solid colored rectangle at absolute position (x,y) with size (w,h).
func drawGioRect(ops *op.Ops, x, y, w, h int, col color.NRGBA) {
	if col.A == 0 || w <= 0 || h <= 0 {
		return
	}
	defer clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops).Pop()
	paint.ColorOp{Color: col}.Add(ops)
	paint.PaintOp{}.Add(ops)
}

type cornerRadii struct {
	nw int
	ne int
	se int
	sw int
}

func (r cornerRadii) max() int {
	m := r.nw
	if r.ne > m {
		m = r.ne
	}
	if r.se > m {
		m = r.se
	}
	if r.sw > m {
		m = r.sw
	}
	return m
}

func (r cornerRadii) allEqual() bool {
	return r.nw == r.ne && r.ne == r.se && r.se == r.sw
}

func drawGioRRectCorners(ops *op.Ops, x, y, w, h int, radii cornerRadii, col color.NRGBA) {
	if col.A == 0 || w <= 0 || h <= 0 {
		return
	}
	if radii.max() <= 0 {
		drawGioRect(ops, x, y, w, h, col)
		return
	}
	maxR := min(w, h) / 2
	nw := min(max(0, radii.nw), maxR)
	ne := min(max(0, radii.ne), maxR)
	se := min(max(0, radii.se), maxR)
	sw := min(max(0, radii.sw), maxR)
	defer clip.RRect{
		Rect: image.Rect(x, y, x+w, y+h),
		NW:   nw, NE: ne, SE: se, SW: sw,
	}.Push(ops).Pop()
	paint.ColorOp{Color: col}.Add(ops)
	paint.PaintOp{}.Add(ops)
}

// drawGioRRect draws a solid rounded rectangle.
func drawGioRRect(ops *op.Ops, x, y, w, h, radius int, col color.NRGBA) {
	drawGioRRectCorners(ops, x, y, w, h, cornerRadii{nw: radius, ne: radius, se: radius, sw: radius}, col)
}

// drawGioBorder draws a border outline. Supports border-radius via a stroked rounded-rect path.
func drawGioBorder(ops *op.Ops, x, y, w, h, radius, borderW int, col color.NRGBA) {
	if col.A == 0 || borderW <= 0 || w <= 0 || h <= 0 {
		return
	}
	if radius <= 0 {
		// Top
		drawGioRect(ops, x, y, w, borderW, col)
		// Bottom
		drawGioRect(ops, x, y+h-borderW, w, borderW, col)
		// Left (inner height)
		if h > 2*borderW {
			drawGioRect(ops, x, y+borderW, borderW, h-2*borderW, col)
			// Right
			drawGioRect(ops, x+w-borderW, y+borderW, borderW, h-2*borderW, col)
		}
		return
	}
	// Rounded border: stroke a rounded-rect path with cubic Bezier corner approximation (k ~= 0.5523).
	maxR := float32(min(w, h)) / 2
	r := float32(radius)
	if r > maxR {
		r = maxR
	}
	const k = float32(0.5523)
	hw := float32(borderW) / 2
	x0 := float32(x) + hw
	y0 := float32(y) + hw
	w0 := float32(w) - 2*hw
	h0 := float32(h) - 2*hw
	if r > w0/2 {
		r = w0 / 2
	}
	if r > h0/2 {
		r = h0 / 2
	}
	var p clip.Path
	p.Begin(ops)
	p.Move(f32.Pt(x0+r, y0))
	p.Line(f32.Pt(w0-2*r, 0))
	p.Cube(f32.Pt(k*r, 0), f32.Pt(r, r-k*r), f32.Pt(r, r))
	p.Line(f32.Pt(0, h0-2*r))
	p.Cube(f32.Pt(0, k*r), f32.Pt(-(r-k*r), r), f32.Pt(-r, r))
	p.Line(f32.Pt(-(w0 - 2*r), 0))
	p.Cube(f32.Pt(-k*r, 0), f32.Pt(-r, -(r-k*r)), f32.Pt(-r, -r))
	p.Line(f32.Pt(0, -(h0 - 2*r)))
	p.Cube(f32.Pt(0, -k*r), f32.Pt(r-k*r, -r), f32.Pt(r, -r))
	p.Close()
	cs := clip.Stroke{Path: p.End(), Width: float32(borderW)}.Op().Push(ops)
	paint.ColorOp{Color: col}.Add(ops)
	paint.PaintOp{}.Add(ops)
	cs.Pop()
}

func drawGioDashedBorder(ops *op.Ops, x, y, w, h, radius, borderW int, col color.NRGBA, dotted bool) {
	if col.A == 0 || borderW <= 0 || w <= 0 || h <= 0 {
		return
	}
	var cs clip.Stack
	if radius > 0 {
		r := min(radius, min(w, h)/2)
		cs = clip.RRect{Rect: image.Rect(x, y, x+w, y+h), NW: r, NE: r, SE: r, SW: r}.Push(ops)
	} else {
		cs = clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops)
	}

	dashLen := max(1, borderW*3)
	gapLen := max(1, borderW*2)
	if dotted {
		dashLen = max(1, borderW)
		gapLen = max(1, borderW*2)
	}

	// Top + bottom
	for cx := x; cx < x+w; cx += dashLen + gapLen {
		dw := min(dashLen, x+w-cx)
		drawGioRect(ops, cx, y, dw, min(borderW, h), col)
		if h > borderW {
			drawGioRect(ops, cx, y+h-borderW, dw, min(borderW, h), col)
		}
	}

	// Left + right
	for cy := y; cy < y+h; cy += dashLen + gapLen {
		dh := min(dashLen, y+h-cy)
		drawGioRect(ops, x, cy, min(borderW, w), dh, col)
		if w > borderW {
			drawGioRect(ops, x+w-borderW, cy, min(borderW, w), dh, col)
		}
	}

	cs.Pop()
}

// drawGioElementBorder renders borders respecting per-side declarations
// (border-top, border-right, border-bottom, border-left) as well as the
// uniform `border-width` / `border-color` fallback from the `border` shorthand.
// When all active sides are identical, it delegates to drawGioBorder which
// supports rounded corners via clip.Stroke.
func drawGioElementBorder(ops *op.Ops, x, y, w, h int, radii cornerRadii, css map[string]string) {
	type side struct {
		width int
		color color.NRGBA
		style string
	}
	getSide := func(name string) side {
		wStr := css["border-"+name+"-width"]
		if wStr == "" {
			wStr = css["border-width"] // fallback: set only by `border` shorthand
		}
		cStr := css["border-"+name+"-color"]
		if cStr == "" {
			cStr = css["border-color"]
		}
		sStyle := strings.ToLower(strings.TrimSpace(css["border-"+name+"-style"]))
		if sStyle == "" {
			sStyle = strings.ToLower(strings.TrimSpace(css["border-style"]))
		}
		bw := cssLengthValue(wStr, 0, max(w, h), w, h)
		bc := toNRGBA(parseHexColor(cStr, color.NRGBA{A: 255}))
		if sStyle == "none" || sStyle == "hidden" {
			bw = 0
		}
		return side{bw, bc, sStyle}
	}

	top := getSide("top")
	right := getSide("right")
	bottom := getSide("bottom")
	left := getSide("left")

	// Quick exit if nothing to draw.
	if top.width == 0 && right.width == 0 && bottom.width == 0 && left.width == 0 {
		return
	}

	// If all four sides are identical, use a single-path renderer.
	if top.width == right.width && right.width == bottom.width && bottom.width == left.width &&
		top.color == right.color && top.style == right.style && right.style == bottom.style && bottom.style == left.style {
		style := strings.ToLower(strings.TrimSpace(top.style))
		if style == "dashed" || style == "dotted" {
			drawGioDashedBorder(ops, x, y, w, h, radii.max(), top.width, top.color, style == "dotted")
		} else {
			if radii.allEqual() {
				drawGioBorder(ops, x, y, w, h, radii.nw, top.width, top.color)
			} else {
				var cs clip.Stack
				cs = clip.RRect{Rect: image.Rect(x, y, x+w, y+h), NW: radii.nw, NE: radii.ne, SE: radii.se, SW: radii.sw}.Push(ops)
				drawGioRect(ops, x, y, w, top.width, top.color)
				drawGioRect(ops, x, y+h-bottom.width, w, bottom.width, bottom.color)
				innerTop := top.width
				innerBot := bottom.width
				innerH := h - innerTop - innerBot
				if innerH > 0 {
					drawGioRect(ops, x, y+innerTop, left.width, innerH, left.color)
					drawGioRect(ops, x+w-right.width, y+innerTop, right.width, innerH, right.color)
				}
				cs.Pop()
			}
		}
		return
	}

	// Per-side rendering with individual rectangles clipped by rounded outer shape.
	var cs clip.Stack
	useClip := radii.max() > 0
	if useClip {
		cs = clip.RRect{Rect: image.Rect(x, y, x+w, y+h), NW: radii.nw, NE: radii.ne, SE: radii.se, SW: radii.sw}.Push(ops)
	}
	if top.width > 0 && top.color.A > 0 {
		drawGioRect(ops, x, y, w, top.width, top.color)
	}
	if bottom.width > 0 && bottom.color.A > 0 {
		drawGioRect(ops, x, y+h-bottom.width, w, bottom.width, bottom.color)
	}
	innerTop := top.width
	innerBot := bottom.width
	innerH := h - innerTop - innerBot
	if innerH > 0 {
		if left.width > 0 && left.color.A > 0 {
			drawGioRect(ops, x, y+innerTop, left.width, innerH, left.color)
		}
		if right.width > 0 && right.color.A > 0 {
			drawGioRect(ops, x+w-right.width, y+innerTop, right.width, innerH, right.color)
		}
	}
	if useClip {
		cs.Pop()
	}
}

// drawGioBackground draws the CSS background (background-color + border-radius).
func drawGioBackground(ops *op.Ops, x, y, w, h int, css map[string]string) {
	bgRaw := strings.TrimSpace(cssBackground(css))
	if bgRaw == "" {
		return
	}
	radii := cssBorderRadiiValues(css, w, h)
	layers := backgroundLayers(bgRaw)
	filters := parseBackgroundFilterFactors(css)
	// Paint from back to front: last layer first, first layer on top.
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if layer == "" {
			continue
		}
		drawSingleBackgroundLayer(ops, x, y, w, h, radii, layer, css, filters)
	}
}

func drawSingleBackgroundLayer(ops *op.Ops, x, y, w, h int, radii cornerRadii, layer string, css map[string]string, filters backgroundFilterFactors) {
	initRenderConfig()
	if renderLowPower {
		lower := strings.ToLower(strings.TrimSpace(layer))
		if strings.Contains(lower, "gradient(") {
			fallback := toNRGBA(parseHexColor(layer, color.NRGBA{R: 0x1E, G: 0x29, B: 0x3B, A: 0xFF}))
			drawGioRRectCorners(ops, x, y, w, h, radii, fallback)
			return
		}
	}

	lower := strings.ToLower(strings.TrimSpace(layer))
	isLinear := strings.HasPrefix(lower, "linear-gradient(") || strings.HasPrefix(lower, "repeating-linear-gradient(")
	isRadial := strings.HasPrefix(lower, "radial-gradient(") || strings.HasPrefix(lower, "repeating-radial-gradient(")
	isRepeating := strings.HasPrefix(lower, "repeating-linear-gradient(") || strings.HasPrefix(lower, "repeating-radial-gradient(")
	if isLinear && isRepeating {
		if pxStops, ok := parseRepeatingLinearPxStops(layer); ok {
			drawGioRepeatingLinearGradientPx(ops, x, y, w, h, radii, layer, pxStops)
			return
		}
	}
	if isLinear || isRadial {
		stops := gradientColorStops(layer)
		if len(stops) >= 2 {
			// Apply all filters to gradient stops
			if filters.brightness != 1.0 || filters.contrast != 1.0 || filters.saturate != 1.0 || filters.grayscale > 0.001 || filters.invert > 0.001 {
				for i := range stops {
					col := stops[i].col
					if filters.brightness != 1.0 {
						col = applyBrightnessToColor(col, filters.brightness)
					}
					if filters.contrast != 1.0 {
						col = applyContrastToColor(col, filters.contrast)
					}
					if filters.saturate != 1.0 {
						col = applySaturationToColor(col, filters.saturate)
					}
					if filters.grayscale > 0.001 {
						col = applyGrayscaleToColor(col, filters.grayscale)
					}
					if filters.invert > 0.001 {
						col = applyInvertToColor(col, filters.invert)
					}
					stops[i].col = col
				}
			}
			if isRadial {
				drawGioRadialGradientApprox(ops, x, y, w, h, radii, layer, stops, isRepeating)
			} else {
				drawGioLinearGradientApprox(ops, x, y, w, h, radii, layer, stops, isRepeating)
			}
			return
		}
	}
	raw := parseHexColor(layer, color.Transparent)
	col := toNRGBA(applyCSSOpacity(raw, css))
	if col.A == 0 {
		return
	}
	// Apply all filters to solid color
	if filters.brightness != 1.0 || filters.contrast != 1.0 || filters.saturate != 1.0 || filters.grayscale > 0.001 || filters.invert > 0.001 {
		if filters.brightness != 1.0 {
			col = applyBrightnessToColor(col, filters.brightness)
		}
		if filters.contrast != 1.0 {
			col = applyContrastToColor(col, filters.contrast)
		}
		if filters.saturate != 1.0 {
			col = applySaturationToColor(col, filters.saturate)
		}
		if filters.grayscale > 0.001 {
			col = applyGrayscaleToColor(col, filters.grayscale)
		}
		if filters.invert > 0.001 {
			col = applyInvertToColor(col, filters.invert)
		}
	}
	drawGioRRectCorners(ops, x, y, w, h, radii, col)
}

type gradientStop struct {
	col color.NRGBA
	pos float64 // normalized [0..1]
}

type gradientRasterKey struct {
	kind     string
	w        int
	h        int
	nw       int
	ne       int
	se       int
	sw       int
	raw      string
	stopsSig string
}

var (
	gradientRasterCacheMu sync.RWMutex
	gradientRasterCache   = map[gradientRasterKey]*image.NRGBA{}
)

func gradientStopsSignature(stops []gradientStop) string {
	if len(stops) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(stops) * 32)
	for _, s := range stops {
		b.WriteString(strconv.FormatFloat(s.pos, 'f', 4, 64))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(int(s.col.R)))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(int(s.col.G)))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(int(s.col.B)))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(int(s.col.A)))
		b.WriteByte(';')
	}
	return b.String()
}

func gradientStopsPxSignature(stops []gradientStopPx) string {
	if len(stops) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(stops) * 32)
	for _, s := range stops {
		b.WriteString(strconv.FormatFloat(s.pos, 'f', 2, 64))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(int(s.col.R)))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(int(s.col.G)))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(int(s.col.B)))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(int(s.col.A)))
		b.WriteByte(';')
	}
	return b.String()
}

func getCachedGradientRaster(key gradientRasterKey, build func() *image.NRGBA) *image.NRGBA {
	gradientRasterCacheMu.RLock()
	if img, ok := gradientRasterCache[key]; ok && img != nil {
		gradientRasterCacheMu.RUnlock()
		return img
	}
	gradientRasterCacheMu.RUnlock()

	img := build()
	if img == nil {
		return nil
	}

	gradientRasterCacheMu.Lock()
	if len(gradientRasterCache) > 512 {
		gradientRasterCache = map[gradientRasterKey]*image.NRGBA{}
	}
	gradientRasterCache[key] = img
	gradientRasterCacheMu.Unlock()
	return img
}

func gradientColorStops(raw string) []gradientStop {
	gradientStopsCacheMu.RLock()
	if cached, ok := gradientStopsCache[raw]; ok {
		gradientStopsCacheMu.RUnlock()
		return cloneGradientStops(cached)
	}
	gradientStopsCacheMu.RUnlock()

	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end <= start {
		return nil
	}
	inner := raw[start+1 : end]
	parts := splitCommaOutsideParens(inner)
	type rawStop struct {
		col    color.NRGBA
		pos    float64
		hasPos bool
	}
	st := make([]rawStop, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		lp := strings.ToLower(p)
		if strings.Contains(lp, "deg") || strings.HasPrefix(lp, "to ") || strings.HasPrefix(lp, "circle") || strings.HasPrefix(lp, "ellipse") {
			continue
		}
		c := parseHexColor(p, nil)
		hasPos := false
		pos := 0.0
		if c == nil {
			fields := strings.Fields(p)
			if len(fields) > 0 {
				c = parseHexColor(fields[0], nil)
				if len(fields) > 1 {
					last := strings.TrimSpace(fields[len(fields)-1])
					if strings.HasSuffix(last, "%") {
						if f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(last, "%")), 64); err == nil {
							hasPos = true
							pos = f / 100.0
						}
					}
				}
			}
		} else {
			fields := strings.Fields(p)
			if len(fields) > 1 {
				last := strings.TrimSpace(fields[len(fields)-1])
				if strings.HasSuffix(last, "%") {
					if f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(last, "%")), 64); err == nil {
						hasPos = true
						pos = f / 100.0
					}
				}
			}
		}
		if c != nil {
			st = append(st, rawStop{col: toNRGBA(c), pos: pos, hasPos: hasPos})
		}
	}
	if len(st) == 0 {
		return nil
	}
	if !st[0].hasPos {
		st[0].pos = 0
		st[0].hasPos = true
	}
	if !st[len(st)-1].hasPos {
		st[len(st)-1].pos = 1
		st[len(st)-1].hasPos = true
	}
	for i := 1; i < len(st)-1; {
		if st[i].hasPos {
			i++
			continue
		}
		j := i
		for j < len(st)-1 && !st[j].hasPos {
			j++
		}
		left := st[i-1].pos
		right := st[j].pos
		count := j - i + 1
		for k := i; k < j; k++ {
			st[k].pos = left + (right-left)*float64(k-i+1)/float64(count)
			st[k].hasPos = true
		}
		i = j + 1
	}
	out := make([]gradientStop, 0, len(st))
	prev := -1.0
	for _, s := range st {
		p := s.pos
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		if p < prev {
			p = prev
		}
		out = append(out, gradientStop{col: s.col, pos: p})
		prev = p
	}
	gradientStopsCacheMu.Lock()
	if len(gradientStopsCache) > 1024 {
		gradientStopsCache = map[string][]gradientStop{}
	}
	gradientStopsCache[raw] = cloneGradientStops(out)
	gradientStopsCacheMu.Unlock()
	return out
}

func gradientParts(raw string) []string {
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end <= start {
		return nil
	}
	inner := raw[start+1 : end]
	return splitCommaOutsideParens(inner)
}

func parseLinearGradientDirection(raw string) (float64, float64) {
	// CSS default: to bottom.
	dx, dy := 0.0, 1.0
	parts := gradientParts(raw)
	if len(parts) == 0 {
		return dx, dy
	}
	tok := strings.ToLower(strings.TrimSpace(parts[0]))
	if tok == "" {
		return dx, dy
	}
	if c := parseHexColor(tok, nil); c != nil {
		return dx, dy
	}
	if strings.HasPrefix(tok, "rgb(") || strings.HasPrefix(tok, "rgba(") || strings.HasPrefix(tok, "hsl(") || strings.HasPrefix(tok, "hsla(") || strings.HasPrefix(tok, "cmyk(") {
		return dx, dy
	}
	if strings.HasPrefix(tok, "to ") {
		dx, dy = 0, 0
		for _, f := range strings.Fields(strings.TrimPrefix(tok, "to ")) {
			switch f {
			case "left":
				dx -= 1
			case "right":
				dx += 1
			case "top":
				dy -= 1
			case "bottom":
				dy += 1
			}
		}
		if dx == 0 && dy == 0 {
			return 0, 1
		}
		m := math.Hypot(dx, dy)
		if m <= 0 {
			return 0, 1
		}
		return dx / m, dy / m
	}
	if strings.Contains(tok, "deg") || strings.Contains(tok, "grad") || strings.Contains(tok, "rad") || strings.Contains(tok, "turn") {
		if ang, ok := parseCSSAngleToDegrees(tok); ok {
			rad := ang * math.Pi / 180.0
			// CSS angle: 0deg points up; 90deg points right.
			dx = math.Sin(rad)
			dy = -math.Cos(rad)
			m := math.Hypot(dx, dy)
			if m > 0 {
				dx /= m
				dy /= m
			}
			return dx, dy
		}
	}
	return dx, dy
}

func parseCSSAngleToDegrees(raw string) (float64, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return 0, false
	}
	if strings.HasSuffix(v, "deg") {
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "deg")), 64)
		return f, err == nil
	}
	if strings.HasSuffix(v, "grad") {
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "grad")), 64)
		if err != nil {
			return 0, false
		}
		return f * 0.9, true
	}
	if strings.HasSuffix(v, "turn") {
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "turn")), 64)
		if err != nil {
			return 0, false
		}
		return f * 360.0, true
	}
	if strings.HasSuffix(v, "rad") {
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "rad")), 64)
		if err != nil {
			return 0, false
		}
		return f * 180.0 / math.Pi, true
	}
	return 0, false
}

func parseRadialGradientCenter(raw string, x, y, w, h int) (int, int) {
	cx, cy := x+w/2, y+h/2
	parts := gradientParts(raw)
	if len(parts) == 0 {
		return cx, cy
	}
	tok := strings.ToLower(strings.TrimSpace(parts[0]))
	idx := strings.Index(tok, " at ")
	if idx < 0 {
		return cx, cy
	}
	pos := strings.TrimSpace(tok[idx+4:])
	if pos == "" {
		return cx, cy
	}
	parseAxis := func(v string, basis int, isX bool, fallback int) int {
		switch strings.TrimSpace(v) {
		case "left", "top":
			return 0
		case "right", "bottom":
			return basis
		case "center":
			return basis / 2
		default:
			return cssLengthValue(v, fallback, basis, w, h)
		}
	}
	fields := strings.Fields(pos)
	if len(fields) == 1 {
		f := fields[0]
		switch f {
		case "top", "bottom":
			cy = y + parseAxis(f, h, false, h/2)
		default:
			cx = x + parseAxis(f, w, true, w/2)
		}
		return cx, cy
	}
	cx = x + parseAxis(fields[0], w, true, w/2)
	cy = y + parseAxis(fields[1], h, false, h/2)
	return cx, cy
}

func mixNRGBA(a color.NRGBA, b color.NRGBA, t float64) color.NRGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	lerp := func(x, y float64) float64 {
		return x + (y-x)*t
	}
	clamp255 := func(v float64) uint8 {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v + 0.5)
	}

	// Web-like blending for gradients: interpolate in premultiplied sRGB.
	aA := float64(a.A) / 255.0
	bA := float64(b.A) / 255.0
	outA := lerp(aA, bA)
	if outA <= 0 {
		return color.NRGBA{}
	}

	aR := (float64(a.R) / 255.0) * aA
	aG := (float64(a.G) / 255.0) * aA
	aB := (float64(a.B) / 255.0) * aA
	bR := (float64(b.R) / 255.0) * bA
	bG := (float64(b.G) / 255.0) * bA
	bB := (float64(b.B) / 255.0) * bA

	outR := lerp(aR, bR) / outA
	outG := lerp(aG, bG) / outA
	outB := lerp(aB, bB) / outA

	return color.NRGBA{
		R: clamp255(outR * 255.0),
		G: clamp255(outG * 255.0),
		B: clamp255(outB * 255.0),
		A: clamp255(outA * 255.0),
	}
}

func gradientPaletteColor(stops []gradientStop, t float64) color.NRGBA {
	if len(stops) == 0 {
		return color.NRGBA{}
	}
	if len(stops) == 1 {
		return stops[0].col
	}
	if t <= stops[0].pos {
		return stops[0].col
	}
	if t >= stops[len(stops)-1].pos {
		return stops[len(stops)-1].col
	}
	for i := 0; i < len(stops)-1; i++ {
		l := stops[i]
		r := stops[i+1]
		if t < l.pos || t > r.pos {
			continue
		}
		if r.pos <= l.pos {
			return r.col
		}
		local := (t - l.pos) / (r.pos - l.pos)
		return mixNRGBA(l.col, r.col, local)
	}
	return stops[len(stops)-1].col
}

func gradientPaletteColorRepeating(stops []gradientStop, t float64) color.NRGBA {
	if len(stops) == 0 {
		return color.NRGBA{}
	}
	if len(stops) == 1 {
		return stops[0].col
	}
	start := stops[0].pos
	end := stops[len(stops)-1].pos
	span := end - start
	if span <= 0 {
		span = 1
		start = 0
	}
	wrapped := start + math.Mod(t-start, span)
	if wrapped < start {
		wrapped += span
	}
	return gradientPaletteColor(stops, wrapped)
}

type gradientStopPx struct {
	col color.NRGBA
	pos float64
}

func parseRepeatingLinearPxStops(raw string) ([]gradientStopPx, bool) {
	repeatingPxStopsCacheMu.RLock()
	if cached, ok := repeatingPxStopsCache[raw]; ok {
		repeatingPxStopsCacheMu.RUnlock()
		return cloneGradientStopsPx(cached.stops), cached.ok
	}
	repeatingPxStopsCacheMu.RUnlock()

	parts := gradientParts(raw)
	if len(parts) == 0 {
		repeatingPxStopsCacheMu.Lock()
		repeatingPxStopsCache[raw] = struct {
			stops []gradientStopPx
			ok    bool
		}{stops: nil, ok: false}
		repeatingPxStopsCacheMu.Unlock()
		return nil, false
	}
	out := make([]gradientStopPx, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		lp := strings.ToLower(p)
		if strings.Contains(lp, "deg") || strings.HasPrefix(lp, "to ") {
			continue
		}
		fields := strings.Fields(p)
		if len(fields) < 2 {
			continue
		}
		c := parseHexColor(fields[0], nil)
		if c == nil {
			continue
		}
		last := strings.ToLower(strings.TrimSpace(fields[len(fields)-1]))
		if !strings.HasSuffix(last, "px") {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(last, "px")), 64)
		if err != nil {
			continue
		}
		out = append(out, gradientStopPx{col: toNRGBA(c), pos: f})
	}
	if len(out) < 2 {
		repeatingPxStopsCacheMu.Lock()
		repeatingPxStopsCache[raw] = struct {
			stops []gradientStopPx
			ok    bool
		}{stops: nil, ok: false}
		repeatingPxStopsCacheMu.Unlock()
		return nil, false
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pos < out[j].pos })
	repeatingPxStopsCacheMu.Lock()
	if len(repeatingPxStopsCache) > 1024 {
		repeatingPxStopsCache = map[string]struct {
			stops []gradientStopPx
			ok    bool
		}{}
	}
	repeatingPxStopsCache[raw] = struct {
		stops []gradientStopPx
		ok    bool
	}{stops: cloneGradientStopsPx(out), ok: true}
	repeatingPxStopsCacheMu.Unlock()
	return out, true
}

func gradientPaletteColorPx(stops []gradientStopPx, t float64) color.NRGBA {
	if len(stops) == 0 {
		return color.NRGBA{}
	}
	if len(stops) == 1 {
		return stops[0].col
	}
	if t <= stops[0].pos {
		return stops[0].col
	}
	if t >= stops[len(stops)-1].pos {
		return stops[len(stops)-1].col
	}
	for i := 0; i < len(stops)-1; i++ {
		l := stops[i]
		r := stops[i+1]
		if t < l.pos || t > r.pos {
			continue
		}
		if r.pos <= l.pos {
			return r.col
		}
		local := (t - l.pos) / (r.pos - l.pos)
		return mixNRGBA(l.col, r.col, local)
	}
	return stops[len(stops)-1].col
}

func drawGioRepeatingLinearGradientPx(ops *op.Ops, x, y, w, h int, radii cornerRadii, raw string, stops []gradientStopPx) {
	if w <= 0 || h <= 0 || len(stops) < 2 {
		return
	}
	var cs clip.Stack
	if radii.max() > 0 {
		maxR := min(w, h) / 2
		cs = clip.RRect{
			Rect: image.Rect(x, y, x+w, y+h),
			NW:   min(max(0, radii.nw), maxR),
			NE:   min(max(0, radii.ne), maxR),
			SE:   min(max(0, radii.se), maxR),
			SW:   min(max(0, radii.sw), maxR),
		}.Push(ops)
	} else {
		cs = clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops)
	}

	dx, dy := parseLinearGradientDirection(raw)
	start := stops[0].pos
	end := stops[len(stops)-1].pos
	span := end - start
	if span <= 0 {
		span = 1
	}
	key := gradientRasterKey{
		kind:     "repeating-linear-px",
		w:        w,
		h:        h,
		nw:       radii.nw,
		ne:       radii.ne,
		se:       radii.se,
		sw:       radii.sw,
		raw:      raw,
		stopsSig: gradientStopsPxSignature(stops),
	}
	img := getCachedGradientRaster(key, func() *image.NRGBA {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		for py := 0; py < h; py++ {
			for px := 0; px < w; px++ {
				p := float64(px)*dx + float64(py)*dy
				wrapped := start + math.Mod(p-start, span)
				if wrapped < start {
					wrapped += span
				}
				out.SetNRGBA(px, py, gradientPaletteColorPx(stops, wrapped))
			}
		}
		return out
	})
	if img == nil {
		cs.Pop()
		return
	}
	tr := op.Offset(image.Pt(x, y)).Push(ops)
	paint.NewImageOp(img).Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
	cs.Pop()
}

func drawGioLinearGradientApprox(ops *op.Ops, x, y, w, h int, radii cornerRadii, raw string, stops []gradientStop, repeating bool) {
	if w <= 0 || h <= 0 {
		return
	}
	var cs clip.Stack
	if radii.max() > 0 {
		maxR := min(w, h) / 2
		cs = clip.RRect{
			Rect: image.Rect(x, y, x+w, y+h),
			NW:   min(max(0, radii.nw), maxR),
			NE:   min(max(0, radii.ne), maxR),
			SE:   min(max(0, radii.se), maxR),
			SW:   min(max(0, radii.sw), maxR),
		}.Push(ops)
	} else {
		cs = clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops)
	}

	dx, dy := parseLinearGradientDirection(raw)
	proj := func(px, py float64) float64 {
		return px*dx + py*dy
	}
	minP := proj(0, 0)
	maxP := minP
	for _, pt := range [][2]float64{{float64(w - 1), 0}, {0, float64(h - 1)}, {float64(w - 1), float64(h - 1)}} {
		p := proj(pt[0], pt[1])
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}
	den := maxP - minP
	if den <= 0 {
		drawGioRect(ops, x, y, w, h, stops[0].col)
		cs.Pop()
		return
	}

	key := gradientRasterKey{
		kind:     "linear",
		w:        w,
		h:        h,
		nw:       radii.nw,
		ne:       radii.ne,
		se:       radii.se,
		sw:       radii.sw,
		raw:      raw + "|rep:" + strconv.FormatBool(repeating),
		stopsSig: gradientStopsSignature(stops),
	}
	img := getCachedGradientRaster(key, func() *image.NRGBA {
		// Render 2D to preserve diagonal angles (e.g. 45deg) and multi-stop richness.
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		for py := 0; py < h; py++ {
			for px := 0; px < w; px++ {
				p := proj(float64(px), float64(py))
				t := (p - minP) / den
				col := gradientPaletteColor(stops, t)
				if repeating {
					col = gradientPaletteColorRepeating(stops, t)
				}
				out.SetNRGBA(px, py, col)
			}
		}
		return out
	})
	if img == nil {
		cs.Pop()
		return
	}

	tr := op.Offset(image.Pt(x, y)).Push(ops)
	imgOp := paint.NewImageOp(img)
	imgOp.Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
	cs.Pop()
}

func drawGioRadialGradientApprox(ops *op.Ops, x, y, w, h int, radii cornerRadii, raw string, stops []gradientStop, repeating bool) {
	if w <= 0 || h <= 0 {
		return
	}
	var cs clip.Stack
	if radii.max() > 0 {
		maxR := min(w, h) / 2
		cs = clip.RRect{
			Rect: image.Rect(x, y, x+w, y+h),
			NW:   min(max(0, radii.nw), maxR),
			NE:   min(max(0, radii.ne), maxR),
			SE:   min(max(0, radii.se), maxR),
			SW:   min(max(0, radii.sw), maxR),
		}.Push(ops)
	} else {
		cs = clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops)
	}
	cx, cy := parseRadialGradientCenter(raw, 0, 0, w, h)
	maxRadius := math.Max(
		math.Hypot(float64(cx), float64(cy)),
		math.Max(
			math.Hypot(float64(cx-w), float64(cy)),
			math.Max(
				math.Hypot(float64(cx), float64(cy-h)),
				math.Hypot(float64(cx-w), float64(cy-h)),
			),
		),
	)
	if maxRadius <= 0 {
		cs.Pop()
		return
	}
	key := gradientRasterKey{
		kind:     "radial",
		w:        w,
		h:        h,
		nw:       radii.nw,
		ne:       radii.ne,
		se:       radii.se,
		sw:       radii.sw,
		raw:      raw + "|rep:" + strconv.FormatBool(repeating),
		stopsSig: gradientStopsSignature(stops),
	}
	img := getCachedGradientRaster(key, func() *image.NRGBA {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		for py := 0; py < h; py++ {
			for px := 0; px < w; px++ {
				d := math.Hypot(float64(px-cx), float64(py-cy))
				t := d / maxRadius
				col := gradientPaletteColor(stops, t)
				if repeating {
					col = gradientPaletteColorRepeating(stops, t)
				}
				out.SetNRGBA(px, py, col)
			}
		}
		return out
	})
	if img == nil {
		cs.Pop()
		return
	}
	tr := op.Offset(image.Pt(x, y)).Push(ops)
	paint.NewImageOp(img).Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
	cs.Pop()
}

func cssBorderRadiiValues(css map[string]string, w int, h int) cornerRadii {
	basis := max(w, h)
	parse := func(raw string, fallback int) int {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fallback
		}
		if idx := strings.Index(raw, "/"); idx >= 0 {
			raw = strings.TrimSpace(raw[:idx])
		}
		parts := strings.Fields(raw)
		if len(parts) == 0 {
			return fallback
		}
		return max(0, cssLengthValue(parts[0], fallback, basis, w, h))
	}
	r := cornerRadii{}
	if raw := strings.TrimSpace(css["border-radius"]); raw != "" {
		main := raw
		if idx := strings.Index(main, "/"); idx >= 0 {
			main = strings.TrimSpace(main[:idx])
		}
		parts := strings.Fields(main)
		if len(parts) == 1 {
			v := parse(parts[0], 0)
			r = cornerRadii{nw: v, ne: v, se: v, sw: v}
		} else if len(parts) == 2 {
			v1 := parse(parts[0], 0)
			v2 := parse(parts[1], 0)
			r = cornerRadii{nw: v1, ne: v2, se: v1, sw: v2}
		} else if len(parts) == 3 {
			v1 := parse(parts[0], 0)
			v2 := parse(parts[1], 0)
			v3 := parse(parts[2], 0)
			r = cornerRadii{nw: v1, ne: v2, se: v3, sw: v2}
		} else if len(parts) >= 4 {
			r = cornerRadii{
				nw: parse(parts[0], 0),
				ne: parse(parts[1], 0),
				se: parse(parts[2], 0),
				sw: parse(parts[3], 0),
			}
		}
	}
	r.nw = parse(css["border-top-left-radius"], r.nw)
	r.ne = parse(css["border-top-right-radius"], r.ne)
	r.se = parse(css["border-bottom-right-radius"], r.se)
	r.sw = parse(css["border-bottom-left-radius"], r.sw)
	maxR := min(w, h) / 2
	r.nw = min(r.nw, maxR)
	r.ne = min(r.ne, maxR)
	r.se = min(r.se, maxR)
	r.sw = min(r.sw, maxR)
	return r
}

func cssBorderRadiusValue(css map[string]string, w int, h int) int {
	r := cssBorderRadiiValues(css, w, h)
	if !r.allEqual() {
		return r.max()
	}
	return r.nw
}

type boxShadowLayer struct {
	offsetX int
	offsetY int
	blur    int
	spread  int
	color   color.NRGBA
	inset   bool
}

type boxShadowLayersKey struct {
	raw string
	w   int
	h   int
}

var (
	boxShadowLayersCacheMu sync.RWMutex
	boxShadowLayersCache   = map[boxShadowLayersKey][]boxShadowLayer{}
)

func cloneBoxShadowLayers(in []boxShadowLayer) []boxShadowLayer {
	if len(in) == 0 {
		return nil
	}
	out := make([]boxShadowLayer, len(in))
	copy(out, in)
	return out
}

func parseBoxShadowLayersCached(raw string, basisW int, basisH int) []boxShadowLayer {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") {
		return nil
	}
	key := boxShadowLayersKey{raw: raw, w: basisW, h: basisH}
	boxShadowLayersCacheMu.RLock()
	if cached, ok := boxShadowLayersCache[key]; ok {
		boxShadowLayersCacheMu.RUnlock()
		return cloneBoxShadowLayers(cached)
	}
	boxShadowLayersCacheMu.RUnlock()

	parsed := parseBoxShadowLayers(raw, basisW, basisH)
	boxShadowLayersCacheMu.Lock()
	if len(boxShadowLayersCache) > 384 {
		boxShadowLayersCache = map[boxShadowLayersKey][]boxShadowLayer{}
	}
	boxShadowLayersCache[key] = cloneBoxShadowLayers(parsed)
	boxShadowLayersCacheMu.Unlock()
	return parsed
}

func splitCommaOutsideParens(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	depth := 0
	start := 0
	for i, r := range input {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				chunk := strings.TrimSpace(input[start:i])
				if chunk != "" {
					parts = append(parts, chunk)
				}
				start = i + 1
			}
		}
	}
	last := strings.TrimSpace(input[start:])
	if last != "" {
		parts = append(parts, last)
	}
	return parts
}

func parseBoxShadowLayers(raw string, basisW int, basisH int) []boxShadowLayer {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") {
		return nil
	}
	items := splitCommaOutsideParens(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]boxShadowLayer, 0, len(items))
	for _, item := range items {
		cleanItem := item
		colorToken := ""
		for _, fn := range []string{"rgba(", "rgb(", "hsla(", "hsl(", "cmyk("} {
			if idx := strings.Index(strings.ToLower(cleanItem), fn); idx >= 0 {
				end := strings.Index(cleanItem[idx:], ")")
				if end > 0 {
					colorToken = strings.TrimSpace(cleanItem[idx : idx+end+1])
					cleanItem = strings.TrimSpace(cleanItem[:idx] + " " + cleanItem[idx+end+1:])
					break
				}
			}
		}
		tokens := strings.Fields(cleanItem)
		if len(tokens) < 2 {
			continue
		}
		layer := boxShadowLayer{}
		vals := make([]int, 0, 4)
		for _, tok := range tokens {
			ltok := strings.ToLower(strings.TrimSpace(tok))
			if ltok == "inset" {
				layer.inset = true
				continue
			}
			if strings.HasPrefix(ltok, "#") || strings.HasPrefix(ltok, "rgb(") || strings.HasPrefix(ltok, "rgba(") || strings.HasPrefix(ltok, "hsl(") || strings.HasPrefix(ltok, "hsla(") || strings.HasPrefix(ltok, "cmyk(") || ltok == "transparent" || ltok == "black" || ltok == "white" || ltok == "gray" || ltok == "grey" || ltok == "red" || ltok == "green" || ltok == "blue" || ltok == "yellow" || ltok == "orange" || ltok == "purple" {
				colorToken = tok
				continue
			}
			v := cssLengthValue(tok, 0, max(basisW, basisH), basisW, basisH)
			vals = append(vals, v)
		}
		if len(vals) < 2 {
			continue
		}
		layer.offsetX = vals[0]
		layer.offsetY = vals[1]
		if len(vals) >= 3 {
			layer.blur = max(0, vals[2])
		}
		if len(vals) >= 4 {
			layer.spread = vals[3]
		}
		if colorToken == "" {
			colorToken = "rgba(0,0,0,0.35)"
		}
		layer.color = toNRGBA(parseHexColor(colorToken, color.NRGBA{R: 0, G: 0, B: 0, A: 90}))
		out = append(out, layer)
	}
	return out
}

type shadowRasterKey struct {
	w       int
	h       int
	radius  int
	offsetX int
	offsetY int
	blur    int
	spread  int
	color   uint32
	inset   bool
}

type shadowRaster struct {
	img *image.NRGBA
	dx  int
	dy  int
}

type shadowTemplateKey struct {
	radius  int
	offsetX int
	offsetY int
	blur    int
	spread  int
	color   uint32
}

type shadowTemplate struct {
	raster shadowRaster
	coreW  int
	coreH  int
}

var (
	shadowRasterCacheMu sync.RWMutex
	shadowRasterCache   = map[shadowRasterKey]shadowRaster{}
	shadowTemplateMu    sync.RWMutex
	shadowTemplateCache = map[shadowTemplateKey]shadowTemplate{}
)

func shadowColorKey(c color.NRGBA) uint32 {
	return (uint32(c.R) << 24) | (uint32(c.G) << 16) | (uint32(c.B) << 8) | uint32(c.A)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func signedDistanceRoundedRect(px, py, left, top, width, height, radius float64) float64 {
	if width <= 0 || height <= 0 {
		return 1e9
	}
	hx := width / 2
	hy := height / 2
	r := radius
	if r < 0 {
		r = 0
	}
	if r > hx {
		r = hx
	}
	if r > hy {
		r = hy
	}
	cx := left + hx
	cy := top + hy
	qx := math.Abs(px-cx) - (hx - r)
	qy := math.Abs(py-cy) - (hy - r)
	ax := math.Max(qx, 0)
	ay := math.Max(qy, 0)
	outside := math.Hypot(ax, ay)
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - r
}

func shadowAlphaOuter(d float64, blur int, baseA float64) float64 {
	if blur <= 0 {
		if d <= 0 {
			return baseA
		}
		return 0
	}
	if d <= 0 {
		return baseA
	}
	sigma := math.Max(0.75, float64(blur)*0.6)
	a := baseA * math.Exp(-(d*d)/(2*sigma*sigma))
	if a < 0.5 {
		return 0
	}
	return a
}

func shadowAlphaInset(d float64, blur int, baseA float64) float64 {
	if blur <= 0 {
		if d >= 0 {
			return baseA
		}
		return 0
	}
	if d >= 0 {
		return baseA
	}
	dist := -d
	sigma := math.Max(0.75, float64(blur)*0.6)
	a := baseA * math.Exp(-(dist*dist)/(2*sigma*sigma))
	if a < 0.5 {
		return 0
	}
	return a
}

func buildOuterShadowRaster(layer boxShadowLayer, w int, h int, radius int) (shadowRaster, bool) {
	if w <= 0 || h <= 0 || layer.color.A == 0 {
		return shadowRaster{}, false
	}
	pad := max(2, layer.blur*3+max(0, layer.spread)+absInt(layer.offsetX)+absInt(layer.offsetY)+2)
	imgW := w + 2*pad
	imgH := h + 2*pad
	if imgW <= 0 || imgH <= 0 {
		return shadowRaster{}, false
	}
	img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))
	rectLeft := float64(-layer.spread)
	rectTop := float64(-layer.spread)
	rectW := float64(w + 2*layer.spread)
	rectH := float64(h + 2*layer.spread)
	if rectW <= 0 || rectH <= 0 {
		return shadowRaster{}, false
	}
	rad := float64(max(0, radius+layer.spread))
	baseA := float64(layer.color.A)
	for py := 0; py < imgH; py++ {
		ry := float64(py-pad) - float64(layer.offsetY)
		for px := 0; px < imgW; px++ {
			rx := float64(px-pad) - float64(layer.offsetX)
			d := signedDistanceRoundedRect(rx+0.5, ry+0.5, rectLeft, rectTop, rectW, rectH, rad)
			a := shadowAlphaOuter(d, layer.blur, baseA)
			if a <= 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{R: layer.color.R, G: layer.color.G, B: layer.color.B, A: clampUint8(a)})
		}
	}
	return shadowRaster{img: img, dx: -pad, dy: -pad}, true
}

func buildInsetShadowRaster(layer boxShadowLayer, w int, h int, radius int) (shadowRaster, bool) {
	if w <= 0 || h <= 0 || layer.color.A == 0 {
		return shadowRaster{}, false
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	baseA := float64(layer.color.A)
	for py := 0; py < h; py++ {
		y := float64(py) + 0.5
		for px := 0; px < w; px++ {
			x := float64(px) + 0.5
			d0 := signedDistanceRoundedRect(x, y, 0, 0, float64(w), float64(h), float64(radius))
			if d0 > 0 {
				continue
			}
			sx := x + float64(layer.offsetX)
			sy := y + float64(layer.offsetY)
			d := signedDistanceRoundedRect(sx, sy, 0, 0, float64(w), float64(h), float64(radius))
			d -= float64(layer.spread)
			a := shadowAlphaInset(d, layer.blur, baseA)
			if a <= 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{R: layer.color.R, G: layer.color.G, B: layer.color.B, A: clampUint8(a)})
		}
	}
	return shadowRaster{img: img, dx: 0, dy: 0}, true
}

func shadowRasterForLayer(layer boxShadowLayer, w int, h int, radius int) (shadowRaster, bool) {
	key := shadowRasterKey{
		w:       w,
		h:       h,
		radius:  radius,
		offsetX: layer.offsetX,
		offsetY: layer.offsetY,
		blur:    layer.blur,
		spread:  layer.spread,
		color:   shadowColorKey(layer.color),
		inset:   layer.inset,
	}
	shadowRasterCacheMu.RLock()
	if cached, ok := shadowRasterCache[key]; ok {
		shadowRasterCacheMu.RUnlock()
		return cached, true
	}
	shadowRasterCacheMu.RUnlock()

	var raster shadowRaster
	var ok bool
	if layer.inset {
		raster, ok = buildInsetShadowRaster(layer, w, h, radius)
	} else {
		raster, ok = buildOuterShadowRaster(layer, w, h, radius)
	}
	if !ok {
		return shadowRaster{}, false
	}

	shadowRasterCacheMu.Lock()
	if len(shadowRasterCache) > 384 {
		shadowRasterCache = map[shadowRasterKey]shadowRaster{}
	}
	shadowRasterCache[key] = raster
	shadowRasterCacheMu.Unlock()
	return raster, true
}

func drawShadowRaster(ops *op.Ops, x int, y int, raster shadowRaster) {
	if raster.img == nil {
		return
	}
	tr := op.Offset(image.Pt(x+raster.dx, y+raster.dy)).Push(ops)
	imgOp := paint.NewImageOp(raster.img)
	imgOp.Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
}

func drawShadowRasterSlice(ops *op.Ops, img *image.NRGBA, src image.Rectangle, dst image.Rectangle) {
	if img == nil || src.Empty() || dst.Empty() {
		return
	}
	if src.Dx() <= 0 || src.Dy() <= 0 || dst.Dx() <= 0 || dst.Dy() <= 0 {
		return
	}
	part, ok := img.SubImage(src).(image.Image)
	if !ok || part == nil {
		return
	}
	clipStack := clip.Rect(dst).Push(ops)
	tr := op.Offset(image.Pt(dst.Min.X, dst.Min.Y)).Push(ops)
	sx := float32(dst.Dx()) / float32(src.Dx())
	sy := float32(dst.Dy()) / float32(src.Dy())
	op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sx, sy))).Add(ops)
	paint.NewImageOp(part).Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
	clipStack.Pop()
}

func shadowTemplateForLayer(layer boxShadowLayer, radius int) (shadowTemplate, bool) {
	if layer.inset {
		return shadowTemplate{}, false
	}
	key := shadowTemplateKey{
		radius:  radius,
		offsetX: layer.offsetX,
		offsetY: layer.offsetY,
		blur:    layer.blur,
		spread:  layer.spread,
		color:   shadowColorKey(layer.color),
	}
	shadowTemplateMu.RLock()
	if cached, ok := shadowTemplateCache[key]; ok {
		shadowTemplateMu.RUnlock()
		return cached, true
	}
	shadowTemplateMu.RUnlock()

	coreW := max(4, 2*max(1, radius)+4)
	coreH := max(4, 2*max(1, radius)+4)
	raster, ok := shadowRasterForLayer(layer, coreW, coreH, radius)
	if !ok || raster.img == nil {
		return shadowTemplate{}, false
	}
	tpl := shadowTemplate{raster: raster, coreW: coreW, coreH: coreH}

	shadowTemplateMu.Lock()
	if len(shadowTemplateCache) > 256 {
		shadowTemplateCache = map[shadowTemplateKey]shadowTemplate{}
	}
	shadowTemplateCache[key] = tpl
	shadowTemplateMu.Unlock()
	return tpl, true
}

func drawShadowTemplateNineSlice(ops *op.Ops, x int, y int, w int, h int, tpl shadowTemplate) bool {
	img := tpl.raster.img
	if img == nil || w <= 0 || h <= 0 {
		return false
	}
	full := img.Bounds()
	left := max(0, -tpl.raster.dx)
	top := max(0, -tpl.raster.dy)
	right := min(full.Max.X, left+tpl.coreW)
	bottom := min(full.Max.Y, top+tpl.coreH)
	if left <= full.Min.X || top <= full.Min.Y || right >= full.Max.X || bottom >= full.Max.Y {
		return false
	}

	outL := left - full.Min.X
	outT := top - full.Min.Y
	outR := full.Max.X - right
	outB := full.Max.Y - bottom

	x0 := x - outL
	x1 := x
	x2 := x + w
	x3 := x + w + outR
	y0 := y - outT
	y1 := y
	y2 := y + h
	y3 := y + h + outB

	if x1 < x0 || x2 < x1 || x3 < x2 || y1 < y0 || y2 < y1 || y3 < y2 {
		return false
	}

	sx := [4]int{full.Min.X, left, right, full.Max.X}
	sy := [4]int{full.Min.Y, top, bottom, full.Max.Y}
	dx := [4]int{x0, x1, x2, x3}
	dy := [4]int{y0, y1, y2, y3}

	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			src := image.Rect(sx[col], sy[row], sx[col+1], sy[row+1])
			dst := image.Rect(dx[col], dy[row], dx[col+1], dy[row+1])
			drawShadowRasterSlice(ops, img, src, dst)
		}
	}
	return true
}

func drawGioBoxShadow(ops *op.Ops, x int, y int, w int, h int, radius int, css map[string]string) {
	initRenderConfig()
	if renderLowPower {
		return
	}
	shadowRaw := strings.TrimSpace(css["box-shadow"])
	if shadowRaw == "" || strings.EqualFold(shadowRaw, "none") {
		return
	}
	layers := parseBoxShadowLayersCached(shadowRaw, w, h)
	if len(layers) == 0 {
		return
	}
	for _, layer := range layers {
		if layer.inset {
			continue
		}
		if tpl, ok := shadowTemplateForLayer(layer, radius); ok {
			if drawShadowTemplateNineSlice(ops, x, y, w, h, tpl) {
				continue
			}
		}
		raster, ok := shadowRasterForLayer(layer, w, h, radius)
		if !ok {
			continue
		}
		drawShadowRaster(ops, x, y, raster)
	}
}

func drawGioInsetBoxShadow(ops *op.Ops, x int, y int, w int, h int, radius int, css map[string]string) {
	initRenderConfig()
	if renderLowPower {
		return
	}
	shadowRaw := strings.TrimSpace(css["box-shadow"])
	if shadowRaw == "" || strings.EqualFold(shadowRaw, "none") {
		return
	}
	layers := parseBoxShadowLayersCached(shadowRaw, w, h)
	if len(layers) == 0 {
		return
	}
	for _, layer := range layers {
		if !layer.inset {
			continue
		}
		raster, ok := shadowRasterForLayer(layer, w, h, radius)
		if !ok {
			continue
		}
		drawShadowRaster(ops, x, y, raster)
	}
}

func roundedFromProps(props map[string]any, css map[string]string, w int, h int) int {
	radius := cssBorderRadiusValue(css, w, h)
	if radius > 0 {
		return radius
	}
	if props == nil {
		return 0
	}
	if v, ok := props["rounded"]; ok {
		s := anyToString(v, "")
		if s != "" {
			return max(0, cssLengthValue(s, 0, max(w, h), w, h))
		}
		switch n := v.(type) {
		case int:
			return max(0, n)
		case int64:
			return max(0, int(n))
		case float64:
			return max(0, int(n))
		}
	}
	if v, ok := props["radius"]; ok {
		s := anyToString(v, "")
		if s != "" {
			return max(0, cssLengthValue(s, 0, max(w, h), w, h))
		}
		switch n := v.(type) {
		case int:
			return max(0, n)
		case int64:
			return max(0, int(n))
		case float64:
			return max(0, int(n))
		}
	}
	return 0
}

func scrollbarThickness(css map[string]string, w int, h int) int {
	v := strings.ToLower(strings.TrimSpace(css["scrollbar-width"]))
	switch v {
	case "none":
		return 0
	case "thin":
		return 8
	case "auto", "":
		return 12
	default:
		return max(4, cssLengthValue(v, 12, max(w, h), w, h))
	}
}

func scrollbarColors(css map[string]string) (color.NRGBA, color.NRGBA) {
	thumb := color.NRGBA{R: 0x8E, G: 0x9C, B: 0xC9, A: 0xF0}
	track := color.NRGBA{R: 0x2B, G: 0x33, B: 0x47, A: 0xD8}
	if raw := strings.TrimSpace(css["scrollbar-color"]); raw != "" {
		parts := strings.Fields(raw)
		if len(parts) >= 1 {
			thumb = toNRGBA(parseHexColor(parts[0], thumb))
		}
		if len(parts) >= 2 {
			track = toNRGBA(parseHexColor(parts[1], track))
		}
	}
	if raw := strings.TrimSpace(css["scrollbar-thumb-color"]); raw != "" {
		thumb = toNRGBA(parseHexColor(raw, thumb))
	}
	if raw := strings.TrimSpace(css["scrollbar-track-color"]); raw != "" {
		track = toNRGBA(parseHexColor(raw, track))
	}
	return thumb, track
}

func childrenContentBounds(children []any, fallbackRight int, fallbackBottom int) (int, int) {
	maxRight := fallbackRight
	maxBottom := fallbackBottom
	for _, child := range children {
		cm := anyToMap(child)
		lb := anyToMap(cm["layout"])
		x := anyToInt(lb["x"], 0)
		y := anyToInt(lb["y"], 0)
		cw := anyToInt(lb["width"], 0)
		ch := anyToInt(lb["height"], 0)
		if x+cw > maxRight {
			maxRight = x + cw
		}
		if y+ch > maxBottom {
			maxBottom = y + ch
		}
	}
	return maxRight, maxBottom
}

func drawScrollbars(gtx layout.Context, x int, y int, w int, h int, contentRight int, contentBottom int, css map[string]string, radius int, scrollX int, scrollY int) scrollHitInfo {
	info := scrollHitInfo{rect: image.Rect(x, y, x+w, y+h)}
	overflow := strings.ToLower(strings.TrimSpace(css["overflow"]))
	overflowX := strings.ToLower(strings.TrimSpace(css["overflow-x"]))
	overflowY := strings.ToLower(strings.TrimSpace(css["overflow-y"]))
	if overflowX == "" {
		overflowX = overflow
	}
	if overflowY == "" {
		overflowY = overflow
	}
	if (overflowX != "auto" && overflowX != "scroll") && (overflowY != "auto" && overflowY != "scroll") {
		return info
	}
	th := scrollbarThickness(css, w, h)
	if th <= 0 {
		return info
	}
	thumbCol, trackCol := scrollbarColors(css)
	sr := max(2, cssLengthValue(css["scrollbar-radius"], th/2, max(w, h), w, h))
	if sr > th {
		sr = th
	}

	contentW := max(w, contentRight-x)
	contentH := max(h, contentBottom-y)

	showV := overflowY == "scroll" || (overflowY == "auto" && contentH > h)
	showH := overflowX == "scroll" || (overflowX == "auto" && contentW > w)
	info.hasV = showV
	info.hasH = showH
	info.maxX = max(0, contentW-w)
	info.maxY = max(0, contentH-h)

	if showV {
		trackX := x + w - th - 1
		trackY := y + 1
		trackH := h - 2
		if showH {
			trackH -= th
		}
		if trackH > 0 {
			info.trackV = image.Rect(trackX, trackY, trackX+th, trackY+trackH)
			drawGioRRect(gtx.Ops, trackX, trackY, th, trackH, sr, trackCol)
			ratio := float64(h) / float64(max(h, contentH))
			thumbH := max(th, int(float64(trackH)*ratio))
			thumbY := trackY
			if contentH > h {
				thumbY = trackY + int(float64(trackH-thumbH)*float64(scrollY)/float64(max(1, contentH-h)))
			}
			info.thumbV = image.Rect(trackX, thumbY, trackX+th, thumbY+thumbH)
			drawGioRRect(gtx.Ops, trackX, thumbY, th, thumbH, sr, thumbCol)
		}
	}
	if showH {
		trackX := x + 1
		trackY := y + h - th - 1
		trackW := w - 2
		if showV {
			trackW -= th
		}
		if trackW > 0 {
			info.trackH = image.Rect(trackX, trackY, trackX+trackW, trackY+th)
			drawGioRRect(gtx.Ops, trackX, trackY, trackW, th, sr, trackCol)
			ratio := float64(w) / float64(max(w, contentW))
			thumbW := max(th, int(float64(trackW)*ratio))
			thumbX := trackX
			if contentW > w {
				thumbX = trackX + int(float64(trackW-thumbW)*float64(scrollX)/float64(max(1, contentW-w)))
			}
			info.thumbH = image.Rect(thumbX, trackY, thumbX+thumbW, trackY+th)
			drawGioRRect(gtx.Ops, thumbX, trackY, thumbW, th, sr, thumbCol)
		}
	}
	_ = radius
	return info
}

// drawGioText renders text at absolute position (x,y) with size (w,h).
func drawGioText(gtx layout.Context, state *gioWindowState, x, y, w, h int,
	txt string, col color.NRGBA, fontSize float32, bold, italic, mono bool,
	align text.Alignment, maxLines int, wrapText bool) {
	if txt == "" || w <= 0 || h <= 0 {
		return
	}
	// Record the color for text material
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	colorCall := macro.Stop()

	w2 := font.Normal
	if bold {
		w2 = font.Bold
	}
	style := font.Regular
	if italic {
		style = font.Italic
	}
	// Use word wrapping as the default CSS-like behavior; heuristic wrapping can
	// split short tokens (e.g. "50%") in tight boxes and cause visual clipping.
	wrapPolicy := text.WrapWords
	label := widget.Label{
		Alignment:  align,
		MaxLines:   maxLines,
		WrapPolicy: wrapPolicy,
		Truncator:  "",
	}

	// Browser-like centering for one-line text: measure shaped bounds first,
	// then place exact text box in the center of the available area.
	runeCount := len([]rune(strings.TrimSpace(txt)))
	shortToken := !strings.Contains(txt, " ") && runeCount > 0 && runeCount <= 8
	lineLike := !strings.Contains(txt, "\n") && (maxLines == 1 || h <= max(18, int(float64(fontSize)*1.9)) || shortToken)
	if lineLike {
		th := max(1, min(h, int(float64(fontSize)*1.35)+1))
		ty := y + max(0, (h-th)/2)

		tr := op.Offset(image.Pt(x, ty)).Push(gtx.Ops)
		gtx2 := gtx
		gtx2.Constraints = layout.Exact(image.Pt(w, th))
		widget.Label{
			Alignment:  align,
			MaxLines:   1,
			WrapPolicy: wrapPolicy,
			Truncator:  "",
		}.Layout(gtx2, state.shaper, font.Font{Weight: w2, Style: style}, unit.Sp(fontSize), txt, colorCall)
		tr.Pop()
		return
	}

	// Translate to position
	tr := op.Offset(image.Pt(x, y)).Push(gtx.Ops)

	// Set exact constraints
	gtx2 := gtx
	gtx2.Constraints = layout.Exact(image.Pt(w, h))

	label.Layout(gtx2, state.shaper, font.Font{Weight: w2, Style: style}, unit.Sp(fontSize), txt, colorCall)

	tr.Pop()
}

func drawDecorationLine(ops *op.Ops, x, y, w, thickness int, col color.NRGBA, style string) {
	if w <= 0 || thickness <= 0 || col.A == 0 {
		return
	}
	style = strings.ToLower(strings.TrimSpace(style))
	switch style {
	case "dotted":
		dot := max(1, thickness)
		gap := max(1, thickness)
		for cx := x; cx < x+w; cx += dot + gap {
			dw := min(dot, x+w-cx)
			drawGioRRect(ops, cx, y, dw, thickness, thickness/2, col)
		}
	case "dashed":
		dash := max(2, thickness*3)
		gap := max(1, thickness*2)
		for cx := x; cx < x+w; cx += dash + gap {
			dw := min(dash, x+w-cx)
			drawGioRect(ops, cx, y, dw, thickness, col)
		}
	default:
		drawGioRect(ops, x, y, w, thickness, col)
	}
}

func drawGioTextDecorations(gtx layout.Context, x, y, w, h int, fontSize float32, align text.Alignment, txt string, base color.NRGBA, deco textDecorationInfo) {
	line := strings.ToLower(strings.TrimSpace(deco.line))
	if line == "" || line == "none" {
		return
	}

	thickness := cssLengthValue(strings.TrimSpace(deco.thickness), max(1, int(fontSize*0.08)+1), max(w, h), w, h)
	if thickness <= 0 {
		thickness = 1
	}
	lineColor := base
	if strings.TrimSpace(deco.color) != "" {
		lineColor = toNRGBA(parseHexColor(deco.color, base))
	}
	lineStyle := strings.ToLower(strings.TrimSpace(deco.style))

	firstLine := txt
	if idx := strings.Index(firstLine, "\n"); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return
	}
	runeCount := len([]rune(firstLine))
	if runeCount <= 0 {
		return
	}
	avgCharW := float64(fontSize) * 0.56
	lineW := int(float64(runeCount)*avgCharW) + max(2, int(float64(fontSize)*0.22))
	lineW = min(w, max(1, lineW))
	lineX := x
	switch align {
	case text.Middle:
		lineX = x + max(0, (w-lineW)/2)
	case text.End:
		lineX = x + max(0, w-lineW)
	}

	lineH := max(1, int(float64(fontSize)*1.28))
	if lineH > h {
		lineH = h
	}
	lineTop := y + max(0, (h-lineH)/2)

	parts := strings.Fields(strings.ReplaceAll(line, ",", " "))
	for _, p := range parts {
		switch p {
		case "underline":
			uy := min(y+h-thickness, lineTop+lineH-max(1, int(fontSize*0.10)))
			drawDecorationLine(gtx.Ops, lineX, uy, lineW, thickness, lineColor, lineStyle)
			if lineStyle == "double" {
				drawDecorationLine(gtx.Ops, lineX, min(y+h-thickness, uy+thickness+1), lineW, thickness, lineColor, "solid")
			}
		case "overline":
			oy := lineTop + max(0, int(fontSize*0.06))
			drawDecorationLine(gtx.Ops, lineX, oy, lineW, thickness, lineColor, lineStyle)
			if lineStyle == "double" {
				drawDecorationLine(gtx.Ops, lineX, oy+thickness+1, lineW, thickness, lineColor, "solid")
			}
		case "line-through":
			sy := min(y+h-thickness, lineTop+int(float32(lineH)*0.50))
			drawDecorationLine(gtx.Ops, lineX, sy, lineW, thickness, lineColor, lineStyle)
			if lineStyle == "double" {
				drawDecorationLine(gtx.Ops, lineX, sy+thickness+1, lineW, thickness, lineColor, "solid")
			}
		}
	}
}

// drawGioImage renders a raster image at absolute position with scaling.
func drawGioImage(ops *op.Ops, x, y, w, h int, img image.Image) {
	if img == nil || w <= 0 || h <= 0 {
		return
	}
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW <= 0 || imgH <= 0 {
		return
	}

	// Clip to destination
	cs := clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops)

	tr := op.Offset(image.Pt(x, y)).Push(ops)

	sx := float32(w) / float32(imgW)
	sy := float32(h) / float32(imgH)
	aff := f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sx, sy))
	op.Affine(aff).Add(ops)

	imgOp := paint.NewImageOp(img)
	imgOp.Add(ops)
	paint.PaintOp{}.Add(ops)

	tr.Pop()
	cs.Pop()
}

// registerGioEventArea registers a clip area with stable tag for pointer events.
func registerGioEventArea(ops *op.Ops, x, y, w, h int, tag *pointerTag) {
	area := clip.Rect(image.Rect(x, y, x+w, y+h)).Push(ops)
	event.Op(ops, tag)
	area.Pop()
}

// applyFrameCursorOverride keeps cursor style stable while pointer is stationary.
func applyFrameCursorOverride(ops *op.Ops, state *gioWindowState) {
	if state == nil {
		return
	}
	if state.frameCursorValue != "" {
		cssCursorToGio(state.frameCursorValue).Add(ops)
		return
	}
	if len(state.frameHoverState) == 0 || len(state.cssForPath) == 0 {
		return
	}
	bestPathLen := -1
	bestCursor := ""
	for path, hovered := range state.frameHoverState {
		if !hovered {
			continue
		}
		css, ok := state.cssForPath[path]
		if !ok || css == nil {
			continue
		}
		cursorRaw := parseCursor(css)
		if cursorRaw == "" || cursorRaw == "default" || cursorRaw == "auto" {
			continue
		}
		if len(path) > bestPathLen {
			bestPathLen = len(path)
			bestCursor = cursorRaw
		}
	}
	if bestCursor != "" {
		cssCursorToGio(bestCursor).Add(ops)
	}
}

func drawDebugOutline(ops *op.Ops, x, y, w, h, stroke int, col color.NRGBA) {
	if stroke <= 0 || w <= 0 || h <= 0 || col.A == 0 {
		return
	}
	drawGioRect(ops, x, y, w, min(stroke, h), col)
	if h > stroke {
		drawGioRect(ops, x, y+h-stroke, w, min(stroke, h), col)
	}
	innerH := h - 2*stroke
	if innerH > 0 {
		drawGioRect(ops, x, y+stroke, min(stroke, w), innerH, col)
		if w > stroke {
			drawGioRect(ops, x+w-stroke, y+stroke, min(stroke, w), innerH, col)
		}
	}
}

func drawGioDebugOverlay(gtx layout.Context, state *gioWindowState, window *uiWindow, path string, kind string, x, y, w, h int, css map[string]string, clipChildren bool, contentRight int, contentBottom int) {
	if window == nil || state == nil || !state.frameDebug {
		return
	}

	baseCol := color.NRGBA{R: 255, G: 173, B: 51, A: 190}
	interactiveCol := color.NRGBA{R: 255, G: 64, B: 129, A: 190}
	scrollCol := color.NRGBA{R: 0, G: 220, B: 255, A: 200}
	hoverCol := color.NRGBA{R: 140, G: 255, B: 140, A: 190}
	activeCol := color.NRGBA{R: 255, G: 90, B: 90, A: 200}

	outlineCol := baseCol
	if isPathActive(window, path) {
		outlineCol = activeCol
	} else if isPathHovered(window, path) {
		outlineCol = hoverCol
	}
	drawDebugOutline(gtx.Ops, x, y, w, h, 1, outlineCol)

	interactive := false
	if kind == "button" || kind == "input" {
		interactive = true
	}
	if nativeKind := strings.ToLower(anyToString(state.propsForPath[path]["component"], anyToString(state.propsForPath[path]["native"], ""))); nativeKind == "slider" || nativeKind == "checkbox" || nativeKind == "radio" || nativeKind == "select" || nativeKind == "dropdown" {
		interactive = true
	}
	if _, ok := state.handlers[path]; ok && interactive {
		drawDebugOutline(gtx.Ops, x+2, y+2, max(1, w-4), max(1, h-4), 1, interactiveCol)
	}

	if clipChildren {
		drawDebugOutline(gtx.Ops, x+1, y+1, max(1, w-2), max(1, h-2), 1, scrollCol)
		if contentRight > x+w || contentBottom > y+h {
			cw := max(1, contentRight-x)
			ch := max(1, contentBottom-y)
			drawDebugOutline(gtx.Ops, x, y, cw, ch, 1, color.NRGBA{R: 0, G: 255, B: 255, A: 90})
		}
	}

	label := kind
	if label == "native" {
		if comp := strings.ToLower(anyToString(state.propsForPath[path]["component"], anyToString(state.propsForPath[path]["native"], ""))); comp != "" {
			label = label + ":" + comp
		}
	}
	if label != "" {
		drawGioText(gtx, state, x+3, y+2, max(1, w-6), min(14, h), label, outlineCol, 9, false, false, true, text.Start, 1, false)
	}
	_ = css
}

func updateDebugFrameMetrics(window *uiWindow, frameTime time.Time) {
	if window == nil {
		return
	}
	window.mu.Lock()
	window.debugFrames++
	window.debugFPSFrames++
	if window.debugFPSLastAt.IsZero() {
		window.debugFPSLastAt = frameTime
	}
	dt := frameTime.Sub(window.debugFPSLastAt)
	if dt >= time.Second {
		if dt > 0 {
			window.debugFPS = float64(window.debugFPSFrames) / dt.Seconds()
		}
		window.debugFPSFrames = 0
		window.debugFPSLastAt = frameTime
	}
	window.mu.Unlock()
}

func updateDebugRenderMetrics(window *uiWindow, renderDur time.Duration, frameTime time.Time) {
	if window == nil {
		return
	}
	ms := float64(renderDur) / float64(time.Millisecond)

	window.mu.Lock()
	window.debugFrameRenderMS = ms
	if window.debugFrames <= 1 {
		window.debugFrameRenderAvgMS = ms
	} else {
		n := float64(window.debugFrames)
		window.debugFrameRenderAvgMS += (ms - window.debugFrameRenderAvgMS) / n
	}
	if ms > window.debugFrameRenderMaxMS {
		window.debugFrameRenderMaxMS = ms
	}
	if ms > 16.7 {
		window.debugSlowFrames++
	}
	if window.debugFPSLastAt.IsZero() || frameTime.Sub(window.debugFPSLastAt) >= time.Second {
		var mem stdruntime.MemStats
		stdruntime.ReadMemStats(&mem)
		window.debugHeapMB = float64(mem.Alloc) / (1024.0 * 1024.0)
	}
	window.mu.Unlock()
}

func summarizeComponentProps(kind string, props map[string]any) (string, string, string, string, string, map[string]string) {
	if len(props) == 0 {
		return "", "", "", "", "", nil
	}
	id := strings.TrimSpace(anyToString(props["id"], ""))
	className := strings.TrimSpace(anyToString(props["class"], anyToString(props["className"], "")))
	component := strings.TrimSpace(anyToString(props["component"], anyToString(props["native"], "")))
	tag := strings.TrimSpace(anyToString(props["tag"], ""))
	if tag == "" {
		tag = kind
	}
	role := strings.TrimSpace(anyToString(props["role"], ""))
	styleHint := strings.TrimSpace(anyToString(props["style"], ""))
	if len(styleHint) > 220 {
		styleHint = styleHint[:220]
	}

	keys := []string{"name", "type", "event", "oninput", "onchange", "onsubmit", "src", "href", "direction", "placeholder", "value"}
	attrs := make(map[string]string)
	for _, key := range keys {
		if raw, ok := props[key]; ok {
			val := strings.TrimSpace(anyToString(raw, ""))
			if val == "" {
				continue
			}
			if len(val) > 120 {
				val = val[:120]
			}
			attrs[key] = val
		}
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	return id, className, component, tag, role, attrs
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func recordDebugComponentMetrics(window *uiWindow, path string, kind string, props map[string]any, w int, h int, totalDur time.Duration, selfDur time.Duration) {
	if window == nil || path == "" {
		return
	}
	totalMS := float64(totalDur) / float64(time.Millisecond)
	selfMS := float64(selfDur) / float64(time.Millisecond)
	if selfMS < 0 {
		selfMS = 0
	}
	estBytes := int64(max(1, w)) * int64(max(1, h)) * 4
	id, className, component, tag, role, attrs := summarizeComponentProps(kind, props)
	styleHint := strings.TrimSpace(anyToString(props["style"], ""))
	if len(styleHint) > 220 {
		styleHint = styleHint[:220]
	}

	window.mu.Lock()
	if window.debugComponents == nil {
		window.debugComponents = make(map[string]*debugComponentStat)
	}
	stat := window.debugComponents[path]
	if stat == nil {
		stat = &debugComponentStat{Path: path}
		window.debugComponents[path] = stat
	}
	stat.Kind = kind
	if id != "" {
		stat.ID = id
	}
	if className != "" {
		stat.ClassName = className
	}
	if component != "" {
		stat.Component = component
	}
	if tag != "" {
		stat.Tag = tag
	}
	if role != "" {
		stat.Role = role
	}
	if styleHint != "" {
		stat.StyleHint = styleHint
	}
	if attrs != nil {
		stat.Attrs = cloneStringMap(attrs)
	}
	stat.Samples++
	stat.TotalMS += totalMS
	stat.SelfTotalMS += selfMS
	if totalMS > stat.MaxMS {
		stat.MaxMS = totalMS
	}
	if selfMS > stat.SelfMaxMS {
		stat.SelfMaxMS = selfMS
	}
	stat.LastRenderMS = selfMS
	stat.EstMemBytes += estBytes
	if estBytes > stat.MaxMemBytes {
		stat.MaxMemBytes = estBytes
	}
	stat.LastSeenFrame = window.debugFrames
	window.mu.Unlock()
}

func buildTopComponentStats(stats map[string]*debugComponentStat, byMem bool, limit int) []map[string]any {
	if len(stats) == 0 || limit <= 0 {
		return nil
	}
	items := make([]*debugComponentStat, 0, len(stats))
	for _, st := range stats {
		if st == nil {
			continue
		}
		items = append(items, st)
	}
	if len(items) == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		a := items[i]
		b := items[j]
		if byMem {
			if a.EstMemBytes == b.EstMemBytes {
				return a.SelfTotalMS > b.SelfTotalMS
			}
			return a.EstMemBytes > b.EstMemBytes
		}
		if a.SelfTotalMS == b.SelfTotalMS {
			return a.SelfMaxMS > b.SelfMaxMS
		}
		return a.SelfTotalMS > b.SelfTotalMS
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, st := range items {
		avg := 0.0
		selfAvg := 0.0
		if st.Samples > 0 {
			avg = st.TotalMS / float64(st.Samples)
			selfAvg = st.SelfTotalMS / float64(st.Samples)
		}
		out = append(out, map[string]any{
			"path":              st.Path,
			"kind":              st.Kind,
			"id":                st.ID,
			"class_name":        st.ClassName,
			"component":         st.Component,
			"tag":               st.Tag,
			"role":              st.Role,
			"style_hint":        st.StyleHint,
			"attrs":             cloneStringMap(st.Attrs),
			"samples":           st.Samples,
			"total_ms":          st.TotalMS,
			"self_total_ms":     st.SelfTotalMS,
			"avg_ms":            avg,
			"self_avg_ms":       selfAvg,
			"max_ms":            st.MaxMS,
			"self_max_ms":       st.SelfMaxMS,
			"estimated_mem_mb":  float64(st.EstMemBytes) / (1024.0 * 1024.0),
			"estimated_peak_mb": float64(st.MaxMemBytes) / (1024.0 * 1024.0),
		})
	}
	return out
}

func writeDebugProfileDump(window *uiWindow) {
	if window == nil {
		return
	}
	window.mu.Lock()
	path := strings.TrimSpace(window.debugProfilerPath)
	if path == "" {
		window.mu.Unlock()
		return
	}
	flags := make([]string, 0, len(window.debugProfile))
	for k, enabled := range window.debugProfile {
		if enabled {
			flags = append(flags, k)
		}
	}
	componentStats := make(map[string]*debugComponentStat, len(window.debugComponents))
	for k, v := range window.debugComponents {
		if v == nil {
			continue
		}
		copyV := *v
		componentStats[k] = &copyV
	}
	data := map[string]any{
		"timestamp":     time.Now().Format(time.RFC3339Nano),
		"frames":        window.debugFrames,
		"fps":           window.debugFPS,
		"render_ms":     window.debugFrameRenderMS,
		"render_avg_ms": window.debugFrameRenderAvgMS,
		"render_max_ms": window.debugFrameRenderMaxMS,
		"slow_frames":   window.debugSlowFrames,
		"heap_mb":       window.debugHeapMB,
		"gpu_enabled":   renderGPUEnabled,
		"low_power":     renderLowPower,
		"profile_flags": flags,
		"window_title":  window.Title,
		"window_width":  window.Width,
		"window_height": window.Height,
	}
	if len(componentStats) > 0 {
		data["components_profiled"] = len(componentStats)
		data["top_components_cpu"] = buildTopComponentStats(componentStats, false, 12)
		data["top_components_mem"] = buildTopComponentStats(componentStats, true, 12)
	}
	window.mu.Unlock()

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Printf("[gio] profiler dump mkdir failed: %v\n", err)
			return
		}
	}
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("[gio] profiler dump marshal failed: %v\n", err)
		return
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		fmt.Printf("[gio] profiler dump write failed: %v\n", err)
		return
	}
	fmt.Printf("[gio] profiler dump written: %s\n", path)
}

func drawGioProfilerOverlay(gtx layout.Context, state *gioWindowState, window *uiWindow, screenW int, screenH int) {
	if window == nil {
		return
	}
	window.mu.Lock()
	flags := map[string]bool{}
	for k, v := range window.debugProfile {
		flags[strings.ToLower(strings.TrimSpace(k))] = v
	}
	frames := window.debugFrames
	fps := window.debugFPS
	renderMS := window.debugFrameRenderMS
	renderAvgMS := window.debugFrameRenderAvgMS
	renderMaxMS := window.debugFrameRenderMaxMS
	slowFrames := window.debugSlowFrames
	heapMB := window.debugHeapMB
	componentCount := len(window.debugComponents)
	window.mu.Unlock()

	showFrames := flags["frames"]
	showFPS := flags["fpscounter"] || flags["fps"]
	showRender := flags["render"] || flags["render_ms"]
	showCPU := flags["cpu"]
	showGPU := flags["gpu"]
	showMem := flags["mem"] || flags["memory"]
	showSlow := flags["slow"] || flags["slowframes"]
	showComponents := flags["components"] || flags["component_cpu"] || flags["component_mem"]
	if !showFrames && !showFPS && !showRender && !showCPU && !showGPU && !showMem && !showSlow && !showComponents {
		return
	}

	lines := make([]string, 0, 8)
	if showFrames {
		lines = append(lines, fmt.Sprintf("frames: %d", frames))
	}
	if showFPS {
		lines = append(lines, fmt.Sprintf("fps: %.1f", fps))
	}
	if showRender {
		lines = append(lines, fmt.Sprintf("render ms: %.2f (avg %.2f / max %.2f)", renderMS, renderAvgMS, renderMaxMS))
	}
	if showCPU {
		cpuApprox := (renderAvgMS / 16.7) * 100.0
		if cpuApprox < 0 {
			cpuApprox = 0
		}
		lines = append(lines, fmt.Sprintf("cpu est: %.0f%% of 60fps budget", cpuApprox))
	}
	if showGPU {
		mode := "on"
		if !renderGPUEnabled {
			mode = "off"
		}
		power := "normal"
		if renderLowPower {
			power = "low"
		}
		lines = append(lines, fmt.Sprintf("gpu: %s | power: %s", mode, power))
	}
	if showMem {
		lines = append(lines, fmt.Sprintf("heap: %.1f MB", heapMB))
	}
	if showSlow {
		lines = append(lines, fmt.Sprintf("slow frames (>16.7ms): %d", slowFrames))
	}
	if showComponents {
		lines = append(lines, fmt.Sprintf("components tracked: %d", componentCount))
	}
	if len(lines) == 0 {
		return
	}

	pad := 8
	lineH := 16
	boxW := 340
	boxH := pad*2 + lineH*len(lines)
	boxX := max(8, screenW-boxW-12)
	boxY := 12

	drawGioRRect(gtx.Ops, boxX, boxY, boxW, boxH, 8, color.NRGBA{R: 12, G: 18, B: 28, A: 220})
	drawGioBorder(gtx.Ops, boxX, boxY, boxW, boxH, 8, 1, color.NRGBA{R: 71, G: 85, B: 105, A: 230})

	for i, line := range lines {
		ty := boxY + pad + i*lineH
		drawGioText(gtx, state, boxX+10, ty, boxW-20, lineH, line, color.NRGBA{R: 226, G: 232, B: 240, A: 255}, 12, false, false, false, text.Start, 1, false)
	}
	_ = screenH
}

// drawGioTree traverses a computed layout tree (from layoutNodeToNative) and draws each element.
func drawGioTree(gtx layout.Context, node map[string]any, window *uiWindow, path string, state *gioWindowState, parentClass string, containingBlock image.Rectangle, parentCSS map[string]string, inheritedShiftX int, inheritedShiftY int, frameTime time.Time) {
	if node == nil {
		return
	}
	_ = parentClass
	initRenderConfig()

	kind := lowerASCIIIfNeeded(anyToString(node["kind"], "view"))
	props := anyToMap(node["props"])
	box := anyToMap(node["layout"])
	x := anyToInt(box["x"], 0)
	y := anyToInt(box["y"], 0)
	w := max(1, anyToInt(box["width"], 100))
	h := max(1, anyToInt(box["height"], 30))
	hovered := state.frameHoverState != nil && state.frameHoverState[path]
	active := state.frameActiveState != nil && state.frameActiveState[path]
	viewW := state.frameViewW
	viewH := state.frameViewH
	if viewW <= 0 || viewH <= 0 {
		return
	}
	childrenSlice := anyToSlice(node["children"])

	quickRect := image.Rect(x+inheritedShiftX, y+inheritedShiftY, x+w+inheritedShiftX, y+h+inheritedShiftY)
	quickViewport := state.frameViewport96
	if quickViewport.Empty() {
		quickViewport = expandedViewportRect(viewW, viewH, 96)
	}
	if path != "root" && !quickRect.Overlaps(quickViewport) && len(childrenSlice) == 0 && kind != "input" && kind != "button" {
		return
	}

	profileThisNode := false
	if state != nil && state.profileComponents {
		profileThisNode = state.profileFull || (path != "root" && state.profileSampleFrame)
	}
	var childrenDur time.Duration
	var nodeStart time.Time
	if profileThisNode {
		nodeStart = frameTime
		defer func() {
			totalDur := time.Since(nodeStart)
			selfDur := totalDur - childrenDur
			if selfDur < 0 {
				selfDur = 0
			}
			recordDebugComponentMetrics(window, path, kind, props, w, h, totalDur, selfDur)
		}()
	}

	// Resolve CSS using per-frame stylesheet snapshot to avoid per-node locking.
	appSS := state.frameStyleSheet
	parentSig := inheritedTextCSSSignature(parentCSS)
	var css map[string]string
	cssWritable := false
	ensureCSSWritable := func() {
		if cssWritable {
			return
		}
		css = cloneStringMap(css)
		cssWritable = true
	}
	if cached, ok := state.resolvedCSS[path]; ok && cached.hovered == hovered && cached.active == active && cached.viewW == viewW && cached.lowPower == renderLowPower && cached.parentSig == parentSig {
		cached.lastSeen = state.frameNumber
		state.resolvedCSS[path] = cached
		css = cached.css
	} else {
		css = resolveStyle(props, appSS, viewW)
		css = mergeInheritedTextCSS(css, parentCSS)
		applyHoverStyles(css, props, appSS, hovered)
		applyActiveStyles(css, props, appSS, active)
		cssWritable = true
		state.resolvedCSS[path] = resolvedCSSCacheEntry{
			hovered:   hovered,
			active:    active,
			viewW:     viewW,
			lowPower:  renderLowPower,
			parentSig: parentSig,
			lastSeen:  state.frameNumber,
			css:       cloneStringMap(css),
		}
	}
	if renderLowPower {
		ensureCSSWritable()
		css["filter"] = ""
		css["transition"] = "none"
	} else {
		applyRuntimeTransition(window, path, css, x, y, w, h)
	}

	// CSS transform values are applied as rendering transforms later,
	// so descendants (like text children) inherit the same motion/scale.
	scale := cssScale(css)
	filterRaw := strings.TrimSpace(css["filter"])
	hasFilter := filterRaw != "" && filterRaw != "none" && filterRaw != "None" && filterRaw != "NONE"

	// Store brightness filter for later use during painting
	if !renderLowPower && hasFilter {
		filterParams := parseCSSFilterChain(filterRaw)
		brightnessFactor := filterParams.brightness
		if window != nil && window.gioWin != nil && mathAbs(brightnessFactor-1.0) > 0.001 {
			// brightness is stored in CSS for access during paint operations
			ensureCSSWritable()
			css["__brightness_factor"] = strconv.FormatFloat(brightnessFactor, 'f', 3, 64)
		}
		if mathAbs(filterParams.contrast-1.0) > 0.001 {
			ensureCSSWritable()
			css["__contrast_factor"] = strconv.FormatFloat(filterParams.contrast, 'f', 3, 64)
		}
		if mathAbs(filterParams.saturate-1.0) > 0.001 {
			ensureCSSWritable()
			css["__saturate_factor"] = strconv.FormatFloat(filterParams.saturate, 'f', 3, 64)
		}
		if filterParams.grayscale > 0.001 {
			ensureCSSWritable()
			css["__grayscale_factor"] = strconv.FormatFloat(filterParams.grayscale, 'f', 3, 64)
		}
		if filterParams.invert > 0.001 {
			ensureCSSWritable()
			css["__invert_factor"] = strconv.FormatFloat(filterParams.invert, 'f', 3, 64)
		}
	}

	// Apply position: absolute/relative/fixed offsets (convert to pixel adjustments)
	posInfo := parsePositionAndOffset(css, w, h)
	switch posInfo.position {
	case "relative", "sticky":
		if posInfo.hasLeft {
			x += int(posInfo.offsetX)
		} else if posInfo.hasRight {
			x -= int(posInfo.offsetX)
		}
		if posInfo.hasTop {
			y += int(posInfo.offsetY)
		} else if posInfo.hasBottom {
			y -= int(posInfo.offsetY)
		}
	case "absolute":
		if posInfo.hasLeft {
			x = containingBlock.Min.X + int(posInfo.offsetX)
		} else if posInfo.hasRight {
			x = containingBlock.Max.X - w - int(posInfo.offsetX)
		}
		if posInfo.hasTop {
			y = containingBlock.Min.Y + int(posInfo.offsetY)
		} else if posInfo.hasBottom {
			y = containingBlock.Max.Y - h - int(posInfo.offsetY)
		}
	case "fixed":
		viewport := state.frameViewportRect
		if viewport.Empty() {
			viewport = image.Rect(0, 0, viewW, viewH)
		}
		if posInfo.hasLeft {
			x = viewport.Min.X + int(posInfo.offsetX)
		} else if posInfo.hasRight {
			x = viewport.Max.X - w - int(posInfo.offsetX)
		}
		if posInfo.hasTop {
			y = viewport.Min.Y + int(posInfo.offsetY)
		} else if posInfo.hasBottom {
			y = viewport.Max.Y - h - int(posInfo.offsetY)
		}
	}

	// Parse CSS transform translate; applied as render transform later.
	translateX, translateY := parseTransformTranslate(css)

	// Apply width/height CSS properties
	cssW, cssH, hasWidth, hasHeight := parseWidthHeightCSS(css, w, h)
	if hasWidth {
		w = cssW
	}
	if hasHeight {
		h = cssH
	}

	displayVal := strings.TrimSpace(css["display"])
	if displayVal == "none" || displayVal == "None" || displayVal == "NONE" {
		return
	}

	nodeRect := image.Rect(x, y, x+w, y+h)
	childContainingBlock := containingBlock
	if posInfo.position != "static" {
		childContainingBlock = nodeRect
	}

	visibilityVal := strings.TrimSpace(css["visibility"])
	visible := !(visibilityVal == "hidden" || visibilityVal == "Hidden" || visibilityVal == "HIDDEN")
	overflowRaw := strings.TrimSpace(css["overflow"])
	overflowMode := lowerASCIIIfNeeded(overflowRaw)
	overflowXRaw := strings.TrimSpace(css["overflow-x"])
	overflowX := lowerASCIIIfNeeded(overflowXRaw)
	overflowYRaw := strings.TrimSpace(css["overflow-y"])
	overflowY := lowerASCIIIfNeeded(overflowYRaw)
	if overflowX == "" {
		overflowX = overflowMode
	}
	if overflowY == "" {
		overflowY = overflowMode
	}
	clipX := overflowX == "hidden" || overflowX == "auto" || overflowX == "scroll" || overflowX == "clip"
	clipY := overflowY == "hidden" || overflowY == "auto" || overflowY == "scroll" || overflowY == "clip"
	clipChildren := clipX || clipY
	viewportRect := state.frameViewport48
	if viewportRect.Empty() {
		viewportRect = expandedViewportRect(viewW, viewH, 48)
	}
	renderRect := nodeRenderRect(x+inheritedShiftX, y+inheritedShiftY, w, h, translateX, translateY, scale)
	isOnScreen := renderRect.Overlaps(viewportRect) || path == "root"
	if !isOnScreen && len(childrenSlice) == 0 && kind != "input" && kind != "button" {
		return
	}

	var nodeTranslateTransform op.TransformStack
	hasNodeTranslateTransform := false
	var nodeScaleTransform op.TransformStack
	hasNodeScaleTransform := false
	if translateX != 0 || translateY != 0 {
		nodeTranslateTransform = op.Offset(image.Pt(int(translateX), int(translateY))).Push(gtx.Ops)
		hasNodeTranslateTransform = true
	}
	if scale > 0 && mathAbs(scale-1.0) > 0.001 {
		cx := float32(x) + float32(w)/2
		cy := float32(y) + float32(h)/2
		aff := f32.Affine2D{}.Scale(f32.Pt(cx, cy), f32.Pt(float32(scale), float32(scale)))
		nodeScaleTransform = op.Affine(aff).Push(gtx.Ops)
		hasNodeScaleTransform = true
	}

	if visible && isOnScreen {
		cursorRaw := parseCursor(css)
		if cursorRaw != "" && cursorRaw != "default" && cursorRaw != "auto" {
			if state.pointerKnown && pointInRect(state.pointerPos, renderRect) && len(path) >= len(state.frameCursorPath) {
				state.frameCursorPath = path
				state.frameCursorValue = cursorRaw
			}
			cursorClip := clip.Rect(nodeRect).Push(gtx.Ops)
			cssCursorToGio(cursorRaw).Add(gtx.Ops)
			cursorClip.Pop()
		}

		radius := roundedFromProps(props, css, w, h)
		radii := cssBorderRadiiValues(css, w, h)

		// Draw box-shadow first so it appears beneath the element.
		drawGioBoxShadow(gtx.Ops, x, y, w, h, radius, css)

		// Draw background
		drawGioBackground(gtx.Ops, x, y, w, h, css)

		// Draw inset shadow over background, clipped to element interior.
		drawGioInsetBoxShadow(gtx.Ops, x, y, w, h, radius, css)

		// Draw border
		drawGioElementBorder(gtx.Ops, x, y, w, h, radii, css)

		// Draw node content
		switch kind {
		case "text", "label":
			fgHex := cssGetColor(css, "color", anyToString(props["fg"], "#e8e8e8"))
			fg := toNRGBA(applyCSSOpacity(parseHexColor(fgHex, color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}), css))

			textValue := cssTextTransform(anyToString(props["text"], ""), css)
			textValue = cssApplyLetterSpacing(textValue, css)
			textValue = sanitizeRenderText(textValue)
			fontSize := cssFontSize(css, 14)
			wrapAllowed := cssAllowsWrap(css)
			if cssShouldClipText(css) {
				textValue = fitTextToWidth(textValue, fontSize, css, w)
				wrapAllowed = false
			}

			fontFamily := strings.ToLower(strings.TrimSpace(css["font-family"]))
			maxLines := 0
			if !wrapAllowed {
				maxLines = 1
			}
			drawGioText(gtx, state, x, y, w, h, textValue, fg, fontSize,
				cssBold(css), cssItalic(css), strings.Contains(fontFamily, "mono"),
				cssTextAlign(css), maxLines, wrapAllowed)
			drawGioTextDecorations(gtx, x, y, w, h, fontSize, cssTextAlign(css), textValue, fg, parseTextDecoration(css))

		case "button":
			label := ""
			if raw, ok := props["text"].(string); ok {
				label = sanitizeRenderText(strings.ToValidUTF8(raw, ""))
			} else {
				label = sanitizeRenderText(anyToString(props["text"], "Button"))
			}

			// Button text
			fgHex := cssGetColor(css, "color", anyToString(props["fg"], "#e8e8e8"))
			fg := toNRGBA(applyCSSOpacity(parseHexColor(fgHex, color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}), css))
			labelProcessed := cssTextTransform(label, css)
			fontSize := cssFontSize(css, 13)
			padL := cssLengthValue(css["padding-left"], 0, w, w, h)
			padR := cssLengthValue(css["padding-right"], 0, w, w, h)
			padT := cssLengthValue(css["padding-top"], 0, h, w, h)
			padB := cssLengthValue(css["padding-bottom"], 0, h, w, h)
			textX := x + padL
			textY := y + padT
			textW := max(1, w-padL-padR)
			textH := max(1, h-padT-padB)
			drawGioText(gtx, state, textX, textY, textW, textH, labelProcessed, fg, fontSize,
				cssBold(css), cssItalic(css), false, text.Middle, 1, false)
			drawGioTextDecorations(gtx, textX, textY, textW, textH, fontSize, text.Middle, labelProcessed, fg, parseTextDecoration(css))

			// Register click event
			eventName := anyToString(props["event"], "click")
			capturedWindow := window
			capturedPath := path
			capturedLabel := label
			state.handlers[path] = func() {
				payloadMap := map[string]value.Value{
					"source":    value.StringValue(capturedPath),
					"component": value.StringValue("button"),
					"text":      value.StringValue(capturedLabel),
					"timestamp": value.IntValue(time.Now().UnixMilli()),
				}
				if err := dispatchWindowEvent(capturedWindow, eventName, value.ObjectValue(&value.Map{Entries: payloadMap})); err != nil && capturedWindow.app != nil {
					capturedWindow.app.emitEvent("ui.event.error", value.StringValue(err.Error()))
				}
			}
			state.propsForPath[path] = props
			state.cssForPath[path] = css
			registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

		case "input":
			if _, hasTag := props["tag"]; !hasTag {
				props["tag"] = "input"
			}
			inputType := strings.ToLower(anyToString(props["type"], anyToString(props["inputtype"], "text")))
			padX, padY := 6, 4
			fontSize := cssFontSize(css, 13)
			fgHex := cssGetColor(css, "color", "#e2e8f0")
			textColor := toNRGBA(parseHexColor(fgHex, color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF}))
			phColor := toNRGBA(parseHexColor(cssGetColor(css, "placeholder-color", "#64748b"), color.NRGBA{R: 0x64, G: 0x74, B: 0x8B, A: 0xCC}))
			accentCol := toNRGBA(parseHexColor(cssGetColor(css, "accent-color", "#4ade80"), color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0xFF}))
			borderCol := toNRGBA(parseHexColor(cssGetColor(css, "border-color", "#475569"), color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}))

			switch inputType {
			case "hidden":
				// No visual output — skip rendering.

			case "checkbox":
				checked := state.boolValues[path]
				if _, seen := state.inputValues[path+"__init"]; !seen {
					if anyToString(props["checked"], "") == "true" {
						checked = true
						state.boolValues[path] = true
					}
					state.inputValues[path+"__init"] = "1"
				}
				boxSize := min(w, h) - 4
				bx := x + (w-boxSize)/2
				by := y + (h-boxSize)/2
				bg := color.NRGBA{R: 30, G: 41, B: 59, A: 255}
				if checked {
					bg = accentCol
				}
				drawGioRRect(gtx.Ops, bx, by, boxSize, boxSize, 3, bg)
				drawGioBorder(gtx.Ops, bx, by, boxSize, boxSize, 3, 2, borderCol)
				if checked {
					drawGioText(gtx, state, bx, by, boxSize, boxSize, "✓",
						color.NRGBA{R: 15, G: 23, B: 25, A: 255}, fontSize, true, false, false, text.Middle, 1, false)
				}
				capturedWindow := window
				capturedPath := path
				eventName := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					state.boolValues[capturedPath] = !state.boolValues[capturedPath]
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedPath),
						"component": value.StringValue("checkbox"),
						"checked":   value.BoolValue(state.boolValues[capturedPath]),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedWindow, eventName, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedWindow.gioWin != nil {
						capturedWindow.gioWin.Invalidate()
					}
				}
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "radio":
				groupName := anyToString(props["name"], path)
				groupKey := "radio:" + groupName
				myValue := anyToString(props["value"], path)
				if _, seen := state.inputValues[path+"__init"]; !seen {
					if anyToString(props["checked"], "") == "true" {
						state.inputValues[groupKey] = myValue
					}
					state.inputValues[path+"__init"] = "1"
				}
				selected := state.inputValues[groupKey] == myValue
				rSize := min(w, h) - 4
				bx := x + (w-rSize)/2
				by := y + (h-rSize)/2
				bgRad := color.NRGBA{R: 30, G: 41, B: 59, A: 255}
				drawGioRRect(gtx.Ops, bx, by, rSize, rSize, rSize/2, bgRad)
				drawGioBorder(gtx.Ops, bx, by, rSize, rSize, rSize/2, 2, borderCol)
				if selected {
					inner := rSize / 2
					drawGioRRect(gtx.Ops, bx+(rSize-inner)/2, by+(rSize-inner)/2, inner, inner, inner/2, accentCol)
				}
				capturedWindow2 := window
				capturedPath2 := path
				capturedGroupKey := groupKey
				capturedMyValue := myValue
				eventName2 := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					state.inputValues[capturedGroupKey] = capturedMyValue
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedPath2),
						"component": value.StringValue("radio"),
						"value":     value.StringValue(capturedMyValue),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedWindow2, eventName2, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedWindow2.gioWin != nil {
						capturedWindow2.gioWin.Invalidate()
					}
				}
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "color":
				colValue := anyToString(props["value"], anyToString(props["text"], "#4ade80"))
				swatchCol := toNRGBA(parseHexColor(colValue, color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0xFF}))
				sw := w - 8
				sh := h - 8
				if sw < 4 {
					sw = 4
				}
				if sh < 4 {
					sh = 4
				}
				drawGioRRect(gtx.Ops, x+4, y+4, sw, sh, 4, swatchCol)
				drawGioBorder(gtx.Ops, x+4, y+4, sw, sh, 4, 2, borderCol)

			case "range":
				minV := float64(anyToInt(props["min"], 0))
				maxV := float64(anyToInt(props["max"], 100))
				if maxV <= minV {
					maxV = minV + 100
				}
				if _, seen := state.sliderValues[path]; !seen {
					initV := 0.0
					switch v := props["value"].(type) {
					case float64:
						initV = v
					case int:
						initV = float64(v)
					case int64:
						initV = float64(v)
					default:
						initV = minV + (maxV-minV)/2
					}
					state.sliderValues[path] = initV
				}
				val := state.sliderValues[path]
				pct := (val - minV) / (maxV - minV)
				if pct < 0 {
					pct = 0
				}
				if pct > 1 {
					pct = 1
				}
				trackCol := color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}
				drawGioRRect(gtx.Ops, x, y+h/2-3, w, 6, 3, trackCol)
				fillW := int(float64(w) * pct)
				if fillW > 0 {
					drawGioRRect(gtx.Ops, x, y+h/2-3, fillW, 6, 3, accentCol)
				}
				thumbX := x + fillW
				thumbR := min(h/3, 10)
				if thumbR < 5 {
					thumbR = 5
				}
				drawGioRRect(gtx.Ops, thumbX-thumbR, y+h/2-thumbR, thumbR*2, thumbR*2, thumbR, accentCol)
				state.boundsForPath[path] = image.Rect(x, y, x+w, y+h)
				capturedWindow3 := window
				capturedPath3 := path
				capturedMinV := minV
				capturedMaxV := maxV
				eventName3 := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					bounds := state.boundsForPath[capturedPath3]
					bw := bounds.Max.X - bounds.Min.X
					if bw <= 0 {
						return
					}
					px := state.lastPointer[capturedPath3].X
					newPct := float64(px-bounds.Min.X) / float64(bw)
					if newPct < 0 {
						newPct = 0
					}
					if newPct > 1 {
						newPct = 1
					}
					newVal := capturedMinV + newPct*(capturedMaxV-capturedMinV)
					state.sliderValues[capturedPath3] = newVal
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedPath3),
						"component": value.StringValue("range"),
						"value":     value.FloatValue(newVal),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedWindow3, eventName3, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedWindow3.gioWin != nil {
						capturedWindow3.gioWin.Invalidate()
					}
				}
				props["__interactive"] = "slider"
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "submit", "reset", "button":
				label := sanitizeRenderText(anyToString(props["text"], anyToString(props["value"], inputType)))
				fg := toNRGBA(parseHexColor(cssGetColor(css, "color", "#e8e8e8"), color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}))
				drawGioText(gtx, state, x, y, w, h, label, fg, fontSize, cssBold(css), cssItalic(css), false, text.Middle, 1, false)
				capturedWindow4 := window
				eventName4 := anyToString(props["event"], "click")
				capturedPath4 := path
				capturedInputType4 := inputType
				capturedLabel4 := label
				state.handlers[path] = func() {
					payloadMap4 := map[string]value.Value{
						"source":    value.StringValue(capturedPath4),
						"component": value.StringValue(capturedInputType4),
						"text":      value.StringValue(capturedLabel4),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedWindow4, eventName4, value.ObjectValue(&value.Map{Entries: payloadMap4}))
				}
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			default:
				// text, password, email, number, tel, url, search,
				// date, datetime-local, time, month, week, textarea
				isTextarea := inputType == "textarea"
				isPassword := inputType == "password"
				isNumberInput := inputType == "number"
				isPickerInput := inputType == "date" || inputType == "time"
				externalRaw := inputExternalValue(props)
				externalVal := normalizeTypedInputValue(inputType, externalRaw, props, true)
				ed := state.editors[path]
				if ed == nil {
					ed = new(widget.Editor)
					ed.SingleLine = !isTextarea
					if isTextarea {
						ed.WrapPolicy = text.WrapHeuristically
					} else {
						ed.WrapPolicy = text.WrapWords
					}
					ed.MaxLen = 0
					if inputType == "date" {
						ed.MaxLen = 10
					} else if inputType == "time" {
						ed.MaxLen = 8
					}
					if isPassword {
						ed.Mask = '•'
					}
					if externalVal != "" {
						ed.SetText(externalVal)
						state.inputValues[path] = externalVal
					}
					state.inputExternal[path] = externalVal
					state.editors[path] = ed
				}
				// Defensive: ensure SingleLine matches the input type (guards against stale state).
				if ed.SingleLine != !isTextarea {
					ed.SingleLine = !isTextarea
				}
				ed.MaxLen = 0
				if inputType == "date" {
					ed.MaxLen = 10
				} else if inputType == "time" {
					ed.MaxLen = 8
				}
				if isTextarea {
					ed.WrapPolicy = text.WrapHeuristically
				} else {
					ed.WrapPolicy = text.WrapWords
				}
				// Controlled input sync: if external PF value changes, update the editor.
				if prevExternal, seen := state.inputExternal[path]; !seen || externalVal != prevExternal {
					ed.SetText(externalVal)
					state.inputValues[path] = externalVal
					state.inputExternal[path] = externalVal
				}
				// Process editor events (ChangeEvent, SubmitEvent)
				for {
					e, ok := ed.Update(gtx)
					if !ok {
						break
					}
					switch e.(type) {
					case widget.ChangeEvent:
						newTextRaw := ed.Text()
						newText := normalizeTypedInputValue(inputType, newTextRaw, props, false)
						if newText != newTextRaw {
							ed.SetText(newText)
						}
						state.inputValues[path] = newText
						evName := anyToString(props["oninput"], anyToString(props["event"], "input"))
						payloadMap := map[string]any{
							"source":    path,
							"component": inputType,
							"value":     newText,
							"focused":   true,
							"timestamp": time.Now().UnixMilli(),
						}
						_ = dispatchWindowEventAny(window, evName, payloadMap)
					case widget.SubmitEvent:
						finalText := normalizeTypedInputValue(inputType, ed.Text(), props, true)
						ed.SetText(finalText)
						state.inputValues[path] = finalText
						evSubmit := anyToString(props["onsubmit"], anyToString(props["event"], "submit"))
						payloadMap2 := map[string]any{
							"source":    path,
							"component": inputType,
							"value":     finalText,
							"focused":   state.inputFocused[path],
							"timestamp": time.Now().UnixMilli(),
						}
						_ = dispatchWindowEventAny(window, evSubmit, payloadMap2)
						evChange := strings.TrimSpace(anyToString(props["onchange"], ""))
						if evChange != "" && evChange != evSubmit {
							_ = dispatchWindowEventAny(window, evChange, payloadMap2)
						}
					}
				}
				// Read CSS padding (cssExpandBoxShorthand already expanded shorthand).
				cssPadL := max(0, cssLengthValue(css["padding-left"], padX, w, w, h))
				cssPadR := max(0, cssLengthValue(css["padding-right"], padX, w, w, h))
				cssPadT := max(0, cssLengthValue(css["padding-top"], padY, h, w, h))
				cssPadB := max(0, cssLengthValue(css["padding-bottom"], padY, h, w, h))
				spinnerW := 0
				if isNumberInput && !isTextarea {
					spinnerW = min(24, max(16, w/5))
				}
				pickerW := 0
				if isPickerInput && !isTextarea {
					pickerW = min(24, max(16, w/5))
				}
				controlW := max(spinnerW, pickerW)
				contentPadR := cssPadR + controlW
				// Draw placeholder if editor is empty
				ph := anyToString(props["placeholder"], "")
				if ph != "" && ed.Text() == "" {
					drawGioText(gtx, state, x+cssPadL, y, max(1, w-cssPadL-contentPadR), h, ph, phColor,
						fontSize, false, false, false, text.Start, 1, false)
				}
				// Build paint materials for the editor
				textRec := op.Record(gtx.Ops)
				paint.ColorOp{Color: textColor}.Add(gtx.Ops)
				textMat := textRec.Stop()
				selRec := op.Record(gtx.Ops)
				paint.ColorOp{Color: color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0x60}}.Add(gtx.Ops)
				selMat := selRec.Stop()
				// Layout editor at (x+padX, y+padY) constrained to input content box.
				edW := max(1, w-cssPadL-contentPadR)
				var edH, edOffY int
				contentW := edW
				offX := x + cssPadL
				if isTextarea {
					edH = max(1, h-cssPadT-cssPadB)
					edOffY = cssPadT
				} else {
					// Single-line: keep one visual line, then horizontally pan by caret position.
					edH = max(1, h-cssPadT-cssPadB)
					edOffY = cssPadT
					fullText := ed.Text()
					fullW, _ := estimateTextBox(fullText, fontSize, css)
					if fullW < edW {
						fullW = edW
					}
					_, caretCol := ed.CaretPos()
					runes := []rune(fullText)
					if caretCol < 0 {
						caretCol = 0
					}
					if caretCol > len(runes) {
						caretCol = len(runes)
					}
					caretPrefix := string(runes[:caretCol])
					caretW, _ := estimateTextBox(caretPrefix, fontSize, css)
					sx := state.inputScrollX[path]
					rightPad := 8
					if caretW-sx > edW-rightPad {
						sx = caretW - (edW - rightPad)
					}
					if caretW-sx < 0 {
						sx = caretW - 2
					}
					maxSX := max(0, fullW-edW)
					if sx < 0 {
						sx = 0
					}
					if sx > maxSX {
						sx = maxSX
					}
					state.inputScrollX[path] = sx
					state.inputContentW[path] = fullW
					state.inputVisibleW[path] = edW
					contentW = max(edW, fullW+2)
					offX = x + cssPadL - sx
				}
				edClip := clip.Rect(image.Rect(x, y, x+w, y+h)).Push(gtx.Ops)
				offs := op.Offset(image.Pt(offX, y+edOffY)).Push(gtx.Ops)
				edGtx := gtx
				edGtx.Constraints = layout.Exact(image.Pt(contentW, edH))
				ed.Layout(edGtx, state.shaper, font.Font{}, unit.Sp(float32(fontSize)), textMat, selMat)
				offs.Pop()
				edClip.Pop()

				if !isTextarea {
					totalW := state.inputContentW[path]
					if totalW > edW {
						trackX := x + cssPadL
						trackW := max(1, edW)
						trackH := 2
						trackY := y + h - max(2, cssPadB/2) - trackH
						drawGioRRect(gtx.Ops, trackX, trackY, trackW, trackH, 1, color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xB0})
						sx := state.inputScrollX[path]

						thumbW := max(12, int(float64(trackW)*float64(trackW)/float64(totalW)))
						maxThumbX := max(0, trackW-thumbW)
						thumbX := 0
						if totalW > edW {
							thumbX = int(float64(maxThumbX) * (float64(sx) / float64(totalW-edW)))
						}
						if thumbX < 0 {
							thumbX = 0
						}
						if thumbX > maxThumbX {
							thumbX = maxThumbX
						}
						drawGioRRect(gtx.Ops, trackX+thumbX, trackY, thumbW, trackH, 1, color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0xE0})
					}
				}

				inputHasFocus := state.inputFocused[path]

				// Only show spinner if input is focused
				if spinnerW > 0 && inputHasFocus {
					spinX := x + w - spinnerW
					spinBorder := toNRGBA(parseHexColor(cssGetColor(css, "border-color", "#475569"), color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}))
					spinFg := toNRGBA(parseHexColor(cssGetColor(css, "color", "#e8e8e8"), color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}))
					drawGioRect(gtx.Ops, spinX, y, 1, h, spinBorder)
					drawGioRect(gtx.Ops, spinX, y+h/2, spinnerW, 1, spinBorder)
					drawGioText(gtx, state, spinX, y, spinnerW, h/2, "▲", spinFg, max(9, fontSize-2), false, false, false, text.Middle, 1, false)
					drawGioText(gtx, state, spinX, y+h/2, spinnerW, h-h/2, "▼", spinFg, max(9, fontSize-2), false, false, false, text.Middle, 1, false)

					spinPath := path + "__spin"
					capturedSpinPath := path
					capturedSpinX := spinX
					capturedSpinY := y
					capturedSpinH := h
					capturedSpinWin := window
					capturedSpinProps := props
					capturedSpinEditor := ed
					state.handlers[spinPath] = func() {
						py := state.lastPointer[spinPath].Y
						delta := 1
						if py >= capturedSpinY+capturedSpinH/2 {
							delta = -1
						}
						nextVal := stepNumberInputValue(capturedSpinEditor.Text(), capturedSpinProps, delta)
						if nextVal == "" {
							return
						}
						capturedSpinEditor.SetText(nextVal)
						state.inputValues[capturedSpinPath] = nextVal
						evName := anyToString(capturedSpinProps["oninput"], anyToString(capturedSpinProps["event"], "input"))
						payload := map[string]any{
							"source":    capturedSpinPath,
							"component": "number",
							"value":     nextVal,
							"focused":   true,
							"timestamp": time.Now().UnixMilli(),
						}
						_ = dispatchWindowEventAny(capturedSpinWin, evName, payload)
						evChange := strings.TrimSpace(anyToString(capturedSpinProps["onchange"], ""))
						if evChange != "" && evChange != evName {
							_ = dispatchWindowEventAny(capturedSpinWin, evChange, payload)
						}
						if capturedSpinWin.gioWin != nil {
							capturedSpinWin.gioWin.Invalidate()
						}
					}
					state.propsForPath[spinPath] = props
					state.cssForPath[spinPath] = css
					registerGioEventArea(gtx.Ops, capturedSpinX, capturedSpinY, spinnerW, capturedSpinH, state.getTag(spinPath))
				}

				// Get direction property for date/time inputs
				pickerDirection := strings.TrimSpace(strings.ToLower(anyToString(props["direction"], "left")))

				// Only show picker icon if input is focused
				if pickerW > 0 && inputHasFocus {
					pickerX := x + w - pickerW
					// Allow direction property to position icon on left instead of right
					if pickerDirection == "left" {
						pickerX = x
					}
					pickerBorder := toNRGBA(parseHexColor(cssGetColor(css, "border-color", "#475569"), color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}))
					pickerFg := toNRGBA(parseHexColor(cssGetColor(css, "color", "#e8e8e8"), color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}))
					drawGioRect(gtx.Ops, pickerX, y, 1, h, pickerBorder)
					drawGioText(gtx, state, pickerX, y, pickerW, h, "▼", pickerFg, max(10, fontSize-1), false, false, false, text.Middle, 1, false)

					pickerPath := path + "__picker"
					capturedPickerPath := path
					capturedPickerWin := window
					capturedPickerProps := props
					capturedPickerType := inputType
					capturedPickerEditor := ed
					// Also open the modal picker
					state.handlers[pickerPath] = func() {
						// Open modal picker
						state.pickerModalOpen = capturedPickerPath
						state.pickerType = capturedPickerType
						state.pickerValue = normalizeTypedInputValue(capturedPickerType, capturedPickerEditor.Text(), capturedPickerProps, true)

						// Dispatch event
						showPickerEvent := strings.TrimSpace(anyToString(capturedPickerProps["onshowpicker"], ""))
						if showPickerEvent == "" {
							showPickerEvent = "showpicker"
						}
						payload := map[string]any{
							"source":    capturedPickerPath,
							"component": capturedPickerType,
							"value":     state.pickerValue,
							"focused":   true,
							"timestamp": time.Now().UnixMilli(),
						}
						_ = dispatchWindowEventAny(capturedPickerWin, showPickerEvent, payload)
						if capturedPickerWin.gioWin != nil {
							capturedPickerWin.gioWin.Invalidate()
						}
					}
					state.propsForPath[pickerPath] = props
					state.cssForPath[pickerPath] = css
					registerGioEventArea(gtx.Ops, pickerX, y, pickerW, h, state.getTag(pickerPath))
				}

				// Register the base input area so pointer-based focus transitions
				// work consistently for text/number/date/time inputs.
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))
			}
			_ = padX
			_ = padY

		case "native":
			component := strings.ToLower(anyToString(props["component"], anyToString(props["native"], "label")))
			switch component {
			case "image", "img", "svg":
				src := anyToString(props["src"], anyToString(props["path"], ""))
				if src != "" {
					img, err := loadRasterImage(src)
					if err == nil {
						filtered := applyImageFilters(img, strings.TrimSpace(css["filter"]), css, w, h)
						drawGioImage(gtx.Ops, x, y, w, h, filtered)
					} else {
						drawGioRect(gtx.Ops, x, y, w, h, color.NRGBA{R: 60, G: 30, B: 30, A: 200})
						drawGioText(gtx, state, x, y, w, h, "[img]",
							color.NRGBA{R: 200, G: 100, B: 100, A: 255}, 11,
							false, false, false, text.Middle, 1, false)
					}
				}

			case "progress", "progressbar":
				val := 0.0
				switch v := props["value"].(type) {
				case float64:
					val = v
				case int:
					val = float64(v) / 100.0
				case int64:
					val = float64(v) / 100.0
				}
				if val < 0 {
					val = 0
				}
				if val > 1 {
					val = 1
				}
				trackCol := toNRGBA(parseHexColor(anyToString(props["track"], "#444444"), color.NRGBA{R: 68, G: 68, B: 68, A: 255}))
				fillCol := toNRGBA(parseHexColor(anyToString(props["fill"], "#4caf50"), color.NRGBA{R: 76, G: 175, B: 80, A: 255}))
				drawGioRRect(gtx.Ops, x, y, w, h, radius, trackCol)
				fillW := int(float64(w) * val)
				if fillW > 0 {
					drawGioRRect(gtx.Ops, x, y, fillW, h, radius, fillCol)
				}

			case "slider":
				minV := float64(anyToInt(props["min"], 0))
				maxV := float64(anyToInt(props["max"], 100))
				if maxV <= minV {
					maxV = minV + 100
				}
				if _, seen := state.sliderValues[path]; !seen {
					initV := 0.0
					switch v := props["value"].(type) {
					case float64:
						initV = v
					case int:
						initV = float64(v)
					case int64:
						initV = float64(v)
					default:
						initV = minV + (maxV-minV)/2
					}
					state.sliderValues[path] = initV
				}
				val := state.sliderValues[path]
				pct := (val - minV) / (maxV - minV)
				if pct < 0 {
					pct = 0
				}
				if pct > 1 {
					pct = 1
				}
				accentSlider := toNRGBA(parseHexColor(cssGetColor(css, "accent-color", "#4ade80"), color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0xFF}))
				trackCol := color.NRGBA{R: 68, G: 68, B: 68, A: 255}
				drawGioRRect(gtx.Ops, x, y+h/2-3, w, 6, 3, trackCol)
				fillW := int(float64(w) * pct)
				if fillW > 0 {
					drawGioRRect(gtx.Ops, x, y+h/2-3, fillW, 6, 3, accentSlider)
				}
				thumbX := x + fillW
				thumbR := min(h/3, 10)
				if thumbR < 4 {
					thumbR = 4
				}
				drawGioRRect(gtx.Ops, thumbX-thumbR, y+h/2-thumbR, thumbR*2, thumbR*2, thumbR, accentSlider)
				// Register drag interaction
				state.boundsForPath[path] = image.Rect(x, y, x+w, y+h)
				capturedSliderPath := path
				capturedSliderMin := minV
				capturedSliderMax := maxV
				capturedSliderWin := window
				eventNameSlider := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					bounds := state.boundsForPath[capturedSliderPath]
					bw := bounds.Max.X - bounds.Min.X
					if bw <= 0 {
						return
					}
					px := state.lastPointer[capturedSliderPath].X
					newPct := float64(px-bounds.Min.X) / float64(bw)
					if newPct < 0 {
						newPct = 0
					}
					if newPct > 1 {
						newPct = 1
					}
					newVal := capturedSliderMin + newPct*(capturedSliderMax-capturedSliderMin)
					state.sliderValues[capturedSliderPath] = newVal
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedSliderPath),
						"component": value.StringValue("slider"),
						"value":     value.FloatValue(newVal),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedSliderWin, eventNameSlider, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedSliderWin.gioWin != nil {
						capturedSliderWin.gioWin.Invalidate()
					}
				}
				props["__interactive"] = "slider"
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "checkbox":
				checked := state.boolValues[path]
				if _, seen := state.inputValues[path+"__init"]; !seen {
					if anyToString(props["checked"], "") == "true" {
						checked = true
						state.boolValues[path] = true
					}
					state.inputValues[path+"__init"] = "1"
				}
				accentChk := toNRGBA(parseHexColor(cssGetColor(css, "accent-color", "#4ade80"), color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0xFF}))
				borderChk := toNRGBA(parseHexColor(cssGetColor(css, "border-color", "#475569"), color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}))
				boxSize := min(w, h) - 4
				bx := x + (w-boxSize)/2
				by := y + (h-boxSize)/2
				bgChk := color.NRGBA{R: 30, G: 41, B: 59, A: 255}
				if checked {
					bgChk = accentChk
				}
				drawGioRRect(gtx.Ops, bx, by, boxSize, boxSize, 3, bgChk)
				drawGioBorder(gtx.Ops, bx, by, boxSize, boxSize, 3, 2, borderChk)
				if checked {
					drawGioText(gtx, state, bx, by, boxSize, boxSize, "✓",
						color.NRGBA{R: 15, G: 23, B: 25, A: 255}, cssFontSize(css, 13), true, false, false, text.Middle, 1, false)
				}
				capturedChkWin := window
				capturedChkPath := path
				chkEvent := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					state.boolValues[capturedChkPath] = !state.boolValues[capturedChkPath]
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedChkPath),
						"component": value.StringValue("checkbox"),
						"checked":   value.BoolValue(state.boolValues[capturedChkPath]),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedChkWin, chkEvent, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedChkWin.gioWin != nil {
						capturedChkWin.gioWin.Invalidate()
					}
				}
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "radio":
				groupNameN := anyToString(props["name"], path)
				groupKeyN := "radio:" + groupNameN
				myValueN := anyToString(props["value"], path)
				if _, seen := state.inputValues[path+"__init"]; !seen {
					if anyToString(props["checked"], "") == "true" {
						state.inputValues[groupKeyN] = myValueN
					}
					state.inputValues[path+"__init"] = "1"
				}
				selectedN := state.inputValues[groupKeyN] == myValueN
				accentRad := toNRGBA(parseHexColor(cssGetColor(css, "accent-color", "#4ade80"), color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0xFF}))
				borderRad := toNRGBA(parseHexColor(cssGetColor(css, "border-color", "#475569"), color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}))
				rSizeN := min(w, h) - 4
				bxN := x + (w-rSizeN)/2
				byN := y + (h-rSizeN)/2
				drawGioRRect(gtx.Ops, bxN, byN, rSizeN, rSizeN, rSizeN/2, color.NRGBA{R: 30, G: 41, B: 59, A: 255})
				drawGioBorder(gtx.Ops, bxN, byN, rSizeN, rSizeN, rSizeN/2, 2, borderRad)
				if selectedN {
					innerN := rSizeN / 2
					drawGioRRect(gtx.Ops, bxN+(rSizeN-innerN)/2, byN+(rSizeN-innerN)/2, innerN, innerN, innerN/2, accentRad)
				}
				capturedRadWin := window
				capturedRadPath := path
				capturedRadGroupKey := groupKeyN
				capturedRadValue := myValueN
				radEvent := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					state.inputValues[capturedRadGroupKey] = capturedRadValue
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedRadPath),
						"component": value.StringValue("radio"),
						"value":     value.StringValue(capturedRadValue),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedRadWin, radEvent, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedRadWin.gioWin != nil {
						capturedRadWin.gioWin.Invalidate()
					}
				}
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "select", "dropdown":
				// Get options as []any or comma-separated string
				var selOptions []string
				switch opts := props["options"].(type) {
				case []any:
					for _, o := range opts {
						selOptions = append(selOptions, anyToString(o, ""))
					}
				case string:
					for _, o := range strings.Split(opts, ",") {
						selOptions = append(selOptions, strings.TrimSpace(o))
					}
				}
				if len(selOptions) == 0 {
					selOptions = []string{"Option 1", "Option 2", "Option 3"}
				}
				// Get or init selected index
				selKey := "sel:" + path
				if _, seen := state.inputValues[path+"__init"]; !seen {
					initSel := anyToString(props["value"], anyToString(props["selected"], ""))
					for i, o := range selOptions {
						if o == initSel {
							state.inputValues[selKey] = strconv.Itoa(i)
							break
						}
					}
					state.inputValues[path+"__init"] = "1"
				}
				selIdx := 0
				if idxStr := state.inputValues[selKey]; idxStr != "" {
					if n, err := strconv.Atoi(idxStr); err == nil {
						selIdx = n
					}
				}
				if selIdx < 0 {
					selIdx = 0
				}
				if selIdx >= len(selOptions) {
					selIdx = len(selOptions) - 1
				}
				selectedLabel := selOptions[selIdx]
				fgSel := toNRGBA(parseHexColor(cssGetColor(css, "color", "#e2e8f0"), color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF}))
				// Draw selected value + arrow indicator
				arrowW := 20
				drawGioText(gtx, state, x+6, y, max(1, w-arrowW-6), h, selectedLabel, fgSel, cssFontSize(css, 13),
					false, false, false, text.Start, 1, false)
				drawGioText(gtx, state, x+w-arrowW, y, arrowW, h, "▾", fgSel, cssFontSize(css, 11),
					false, false, false, text.Middle, 1, false)
				// Click cycles to next option
				capturedSelWin := window
				capturedSelPath := path
				capturedSelKey := selKey
				capturedSelOpts := selOptions
				selEvent := anyToString(props["event"], "change")
				state.handlers[path] = func() {
					curStr := state.inputValues[capturedSelKey]
					cur := 0
					if curStr != "" {
						if n, err := strconv.Atoi(curStr); err == nil {
							cur = n
						}
					}
					next := (cur + 1) % len(capturedSelOpts)
					state.inputValues[capturedSelKey] = strconv.Itoa(next)
					payloadMap := map[string]value.Value{
						"source":    value.StringValue(capturedSelPath),
						"component": value.StringValue("select"),
						"value":     value.StringValue(capturedSelOpts[next]),
						"index":     value.IntValue(int64(next)),
						"timestamp": value.IntValue(time.Now().UnixMilli()),
					}
					_ = dispatchWindowEvent(capturedSelWin, selEvent, value.ObjectValue(&value.Map{Entries: payloadMap}))
					if capturedSelWin.gioWin != nil {
						capturedSelWin.gioWin.Invalidate()
					}
				}
				state.propsForPath[path] = props
				state.cssForPath[path] = css
				registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))

			case "textarea":
				externalTA := anyToString(props["text"], anyToString(props["value"], ""))
				edTA := state.editors[path]
				if edTA == nil {
					edTA = new(widget.Editor)
					edTA.SingleLine = false
					if externalTA != "" {
						edTA.SetText(externalTA)
						state.inputValues[path] = externalTA
					}
					state.inputExternal[path] = externalTA
					state.editors[path] = edTA
				}
				if prevExternal, seen := state.inputExternal[path]; !seen || externalTA != prevExternal {
					edTA.SetText(externalTA)
					state.inputValues[path] = externalTA
					state.inputExternal[path] = externalTA
				}
				for {
					e, ok := edTA.Update(gtx)
					if !ok {
						break
					}
					if _, isChange := e.(widget.ChangeEvent); isChange {
						state.inputValues[path] = edTA.Text()
					}
				}
				fgTA := toNRGBA(parseHexColor(cssGetColor(css, "color", "#e2e8f0"), color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF}))
				taRec := op.Record(gtx.Ops)
				paint.ColorOp{Color: fgTA}.Add(gtx.Ops)
				taMat := taRec.Stop()
				taSelRec := op.Record(gtx.Ops)
				paint.ColorOp{Color: color.NRGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0x60}}.Add(gtx.Ops)
				taSelMat := taSelRec.Stop()
				edW2 := max(1, w-12)
				edH2 := max(1, h-8)
				edClip2 := clip.Rect(image.Rect(x, y, x+w, y+h)).Push(gtx.Ops)
				offs2 := op.Offset(image.Pt(x+6, y+4)).Push(gtx.Ops)
				edGtx2 := gtx
				edGtx2.Constraints = layout.Exact(image.Pt(edW2, edH2))
				edTA.Layout(edGtx2, state.shaper, font.Font{}, unit.Sp(float32(cssFontSize(css, 13))), taMat, taSelMat)
				offs2.Pop()
				edClip2.Pop()

			default:
				labelText := sanitizeRenderText(anyToString(props["text"], component))
				fgHex := cssGetColor(css, "color", "#e8e8e8")
				fg := toNRGBA(parseHexColor(fgHex, color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}))
				drawGioText(gtx, state, x, y, w, h, labelText, fg, cssFontSize(css, 13),
					false, false, false, text.Start, 0, true)
			}

		default:
			// view/container: background already drawn — no extra content
		}

		// Register event area only when needed (hover/active selectors or scroll containers)
		// to avoid intercepting pointer focus from child widget.Editor inputs.
		if kind != "button" && kind != "input" &&
			(hasHoverSelector(props, appSS) || hasActiveSelector(props, appSS) ||
				overflowX == "auto" || overflowX == "scroll" || overflowY == "auto" || overflowY == "scroll") {
			state.propsForPath[path] = props
			state.cssForPath[path] = css
			registerGioEventArea(gtx.Ops, x, y, w, h, state.getTag(path))
		}
	}

	var childClip clip.Stack
	var scrollTrStack op.TransformStack
	var hasScrollTr bool
	childShiftX := inheritedShiftX + int(translateX)
	childShiftY := inheritedShiftY + int(translateY)
	if visible && isOnScreen && clipChildren {
		childClip = clip.Rect(image.Rect(x, y, x+w, y+h)).Push(gtx.Ops)
		so := state.scrollOffsets[path]
		if so.X != 0 || so.Y != 0 {
			scrollTrStack = op.Offset(image.Pt(-so.X, -so.Y)).Push(gtx.Ops)
			hasScrollTr = true
			childShiftX -= so.X
			childShiftY -= so.Y
		}
	}

	if isOnScreen {
		needsZSort := false
		childSig := childSliceSignature(childrenSlice)
		if hint, ok := state.zChildrenHint[path]; ok && hint.count == len(childrenSlice) && hint.sig == childSig {
			needsZSort = hint.needs
			hint.lastSeen = state.frameNumber
			state.zChildrenHint[path] = hint
		} else {
			for _, child := range childrenSlice {
				if nodeHasExplicitZIndex(anyToMap(anyToMap(child)["props"])) {
					needsZSort = true
					break
				}
			}
			state.zChildrenHint[path] = zChildrenHintCacheEntry{
				count:    len(childrenSlice),
				sig:      childSig,
				needs:    needsZSort,
				lastSeen: state.frameNumber,
			}
		}

		if needsZSort {
			// Recurse into children using CSS z-index ordering while preserving stable child paths.
			type childRenderItem struct {
				idx    int
				child  map[string]any
				zIndex int
			}
			renderOrder := make([]childRenderItem, 0, len(childrenSlice))
			for i, child := range childrenSlice {
				childMap := anyToMap(child)
				childProps := anyToMap(childMap["props"])
				renderOrder = append(renderOrder, childRenderItem{
					idx:    i,
					child:  childMap,
					zIndex: parseZIndexFromPropsFast(childProps),
				})
			}
			sort.SliceStable(renderOrder, func(i, j int) bool {
				if renderOrder[i].zIndex == renderOrder[j].zIndex {
					return renderOrder[i].idx < renderOrder[j].idx
				}
				return renderOrder[i].zIndex < renderOrder[j].zIndex
			})
			for _, item := range renderOrder {
				if profileThisNode {
					drawGioTree(gtx, item.child, window, childPath(path, item.idx), state, "", childContainingBlock, css, childShiftX, childShiftY, frameTime)
					// Note: childrenDur calculation omitted; frameTime measurement eliminates the syscall
				} else {
					drawGioTree(gtx, item.child, window, childPath(path, item.idx), state, "", childContainingBlock, css, childShiftX, childShiftY, frameTime)
				}
			}
		} else {
			for i, child := range childrenSlice {
				if profileThisNode {
					drawGioTree(gtx, anyToMap(child), window, childPath(path, i), state, "", childContainingBlock, css, childShiftX, childShiftY, frameTime)
				} else {
					drawGioTree(gtx, anyToMap(child), window, childPath(path, i), state, "", childContainingBlock, css, childShiftX, childShiftY, frameTime)
				}
			}
		}
	} else if mayContainOutOfFlowChildren(childrenSlice) {
		for i, child := range childrenSlice {
			if profileThisNode {
				drawGioTree(gtx, anyToMap(child), window, childPath(path, i), state, "", childContainingBlock, css, childShiftX, childShiftY, frameTime)
			} else {
				drawGioTree(gtx, anyToMap(child), window, childPath(path, i), state, "", childContainingBlock, css, childShiftX, childShiftY, frameTime)
			}
		}
	}

	if visible && isOnScreen && clipChildren {
		if hasScrollTr {
			scrollTrStack.Pop()
		}
		childClip.Pop()
	}

	if visible && isOnScreen && clipChildren {
		radius := roundedFromProps(props, css, w, h)
		contentRight, contentBottom := childrenContentBounds(childrenSlice, x+w, y+h)
		so := state.scrollOffsets[path]
		maxScrollX := max(0, contentRight-(x+w))
		maxScrollY := max(0, contentBottom-(y+h))
		if so.X > maxScrollX {
			so.X = maxScrollX
		}
		if so.Y > maxScrollY {
			so.Y = maxScrollY
		}
		if so.X < 0 {
			so.X = 0
		}
		if so.Y < 0 {
			so.Y = 0
		}
		state.scrollOffsets[path] = so
		hitInfo := drawScrollbars(gtx, x, y, w, h, contentRight, contentBottom, css, radius, so.X, so.Y)
		if hitInfo.hasV || hitInfo.hasH {
			state.scrollHits[path] = hitInfo
		}
	}

	if visible && isOnScreen && state.frameDebug {
		contentRight, contentBottom := x+w, y+h
		if clipChildren {
			contentRight, contentBottom = childrenContentBounds(childrenSlice, x+w, y+h)
		}
		drawGioDebugOverlay(gtx, state, window, path, kind, x, y, w, h, css, clipChildren, contentRight, contentBottom)
	}

	if hasNodeScaleTransform {
		nodeScaleTransform.Pop()
	}
	if hasNodeTranslateTransform {
		nodeTranslateTransform.Pop()
	}
}

// openGioWindow creates and runs a Gio native window for the given uiWindow.
// Blocks until the window is closed.
func openGioWindow(win *uiWindow) {
	initRenderConfig()
	gw := &gioapp.Window{}
	gw.Option(
		gioapp.Title(win.Title),
		gioapp.Size(unit.Dp(float32(max(320, win.Width))), unit.Dp(float32(max(240, win.Height)))),
		gioapp.CustomRenderer(!renderGPUEnabled),
	)
	win.mu.Lock()
	win.gioWin = gw
	win.mu.Unlock()

	state := newGioWindowState()
	var ops op.Ops
	var cachedRoot *uiNode
	var cachedSS *styleSheet
	var cachedLayout map[string]any
	var cachedW int
	var cachedH int
	var cachedLayoutAge int

	for {
		e := gw.Event()
		switch e := e.(type) {
		case gioapp.DestroyEvent:
			writeDebugProfileDump(win)
			win.mu.Lock()
			win.Visible = false
			win.gioWin = nil
			win.mu.Unlock()
			_ = dispatchWindowEvent(win, "close", value.NilValue())
			return

		case gioapp.FrameEvent:
			gtx := gioapp.NewContext(&ops, e)
			frameTime := time.Now()
			updateDebugFrameMetrics(win, frameTime)

			// Snapshot gioWin once to avoid repeated locks
			win.mu.Lock()
			gw := win.gioWin
			win.mu.Unlock()

			// Process pointer events queued from the previous frame
			layoutDirty := processGioEvents(gtx, win, state, gw, frameTime)
			if layoutDirty && gw != nil {
				gw.Invalidate()
			}

			// Clear per-frame handler registrations
			state.clearFrame()

			// Snapshot root and dimensions under lock
			win.mu.Lock()
			rootSnap := win.Root
			var ss *styleSheet
			if win.app != nil {
				ss = win.app.stylesheet
			}
			win.Width = e.Size.X
			win.Height = e.Size.Y
			state.frameViewW = e.Size.X
			state.frameViewH = e.Size.Y
			state.frameViewportRect = image.Rect(0, 0, e.Size.X, e.Size.Y)
			state.frameViewport48 = expandedViewportRect(e.Size.X, e.Size.Y, 48)
			state.frameViewport96 = expandedViewportRect(e.Size.X, e.Size.Y, 96)
			state.frameNumber = win.debugFrames
			state.profileSampleFrame = state.frameNumber%30 == 0
			if state.frameNumber%120 == 0 {
				state.purgeStaleCaches(state.frameNumber)
			}
			state.frameHoverState = refreshBoolMap(state.frameHoverState, win.hoverState)
			state.frameActiveState = refreshBoolMap(state.frameActiveState, win.activeState)
			state.frameDebug = win.debug
			state.frameStyleSheet = ss
			state.profileComponents = win.debugProfile["components"] || win.debugProfile["component_cpu"] || win.debugProfile["component_mem"]
			state.profileFull = win.debugProfile["components_full"]
			win.mu.Unlock()

			// Render
			renderStart := time.Now()
			if rootSnap != nil {
				needLayout := layoutDirty || cachedLayout == nil || cachedRoot != rootSnap || cachedSS != ss || cachedW != e.Size.X || cachedH != e.Size.Y || cachedLayoutAge >= 300
				if needLayout {
					cachedLayout = layoutNodeToNative(rootSnap, e.Size.X, e.Size.Y, ss)
					cachedRoot = rootSnap
					cachedSS = ss
					cachedW = e.Size.X
					cachedH = e.Size.Y
					cachedLayoutAge = 0
				} else {
					cachedLayoutAge++
				}
				rootLayout := cachedLayout

				// Draw viewport background from root CSS (supports colors, gradients, filters, etc)
				rootLayoutProps := anyToMap(rootLayout["props"])
				rootCSS := resolveStyle(rootLayoutProps, ss, e.Size.X)
				drawGioBackground(gtx.Ops, 0, 0, e.Size.X, e.Size.Y, rootCSS)

				drawGioTree(gtx, rootLayout, win, "root", state, "", image.Rect(0, 0, e.Size.X, e.Size.Y), nil, 0, 0, frameTime)
				applyFrameCursorOverride(gtx.Ops, state)
			}
			drawGioProfilerOverlay(gtx, state, win, e.Size.X, e.Size.Y)
			updateDebugRenderMetrics(win, time.Since(renderStart), frameTime)
			finalizeRenderState(win)

			// Invalidate was already done above if needed, but ensure one final invalidate for continuous rendering
			if gw != nil {
				gw.Invalidate()
			}

			e.Frame(gtx.Ops)
		}
	}
}

// drawPickerModal renders a date/time picker modal overlay that appears on top of all content
func drawPickerModal(gtx layout.Context, state *gioWindowState, screenW, screenH int) {
	if state.pickerModalOpen == "" {
		return
	}

	// Semi-transparent background overlay
	overlayColor := color.NRGBA{R: 0, G: 0, B: 0, A: 200}
	drawGioRect(gtx.Ops, 0, 0, screenW, screenH, overlayColor)

	// Modal box dimensions
	modalW := min(400, screenW-40)
	modalH := 300
	modalX := (screenW - modalW) / 2
	modalY := (screenH - modalH) / 2

	// Modal background
	modalBg := color.NRGBA{R: 0x1F, G: 0x29, B: 0x37, A: 0xFF}
	drawGioRRect(gtx.Ops, modalX, modalY, modalW, modalH, 12, modalBg)

	// Modal border
	borderColor := color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}
	drawGioRect(gtx.Ops, modalX, modalY, modalW, 1, borderColor)
	drawGioRect(gtx.Ops, modalX, modalY+modalH-1, modalW, 1, borderColor)
	drawGioRect(gtx.Ops, modalX, modalY, 1, modalH, borderColor)
	drawGioRect(gtx.Ops, modalX+modalW-1, modalY, 1, modalH, borderColor)

	// Title
	titleY := modalY + 15
	titleColor := color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF}
	title := "Select " + state.pickerType
	drawGioText(gtx, state, modalX+10, titleY, modalW-20, 30, title, titleColor, 16, true, false, false, text.Start, 1, false)

	// Separator line under title
	sepY := titleY + 35
	drawGioRect(gtx.Ops, modalX+10, sepY, modalW-20, 1, borderColor)

	// Picker content area (simplified: show current value and buttons)
	contentY := sepY + 20

	// Display current value
	valueText := state.pickerValue
	if valueText == "" {
		valueText = "(no value)"
	}
	valueColor := color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0xFF}
	drawGioText(gtx, state, modalX+20, contentY, modalW-40, 40, valueText, valueColor, 14, false, false, false, text.Middle, 1, false)

	// Increment/Decrement buttons
	btnW := 50
	btnH := 30
	btnY := contentY + 50
	decX := modalX + 20
	incX := modalX + modalW - btnW - 20

	decColor := color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}
	drawGioRRect(gtx.Ops, decX, btnY, btnW, btnH, 6, decColor)
	drawGioText(gtx, state, decX, btnY, btnW, btnH, "−", titleColor, 16, true, false, false, text.Middle, 1, false)
	decPath := state.pickerModalOpen + "__modal_dec"
	state.handlers[decPath] = func() {}
	registerGioEventArea(gtx.Ops, decX, btnY, btnW, btnH, state.getTag(decPath))

	incColor := color.NRGBA{R: 0x22, G: 0xC5, B: 0x5E, A: 0xFF}
	drawGioRRect(gtx.Ops, incX, btnY, btnW, btnH, 6, incColor)
	drawGioText(gtx, state, incX, btnY, btnW, btnH, "+", titleColor, 16, true, false, false, text.Middle, 1, false)
	incPath := state.pickerModalOpen + "__modal_inc"
	state.handlers[incPath] = func() {}
	registerGioEventArea(gtx.Ops, incX, btnY, btnW, btnH, state.getTag(incPath))

	// Close button
	closeY := modalY + modalH - 40
	closeW := 80
	closeH := 30
	closeX := modalX + (modalW-closeW)/2

	closeColor := color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xFF}
	drawGioRRect(gtx.Ops, closeX, closeY, closeW, closeH, 6, closeColor)
	drawGioText(gtx, state, closeX, closeY, closeW, closeH, "Close", titleColor, 12, false, false, false, text.Middle, 1, false)

	// Register close button hit area
	closePath := state.pickerModalOpen + "__modal_close"
	state.handlers[closePath] = func() {
		state.pickerModalOpen = ""
		state.pickerType = ""
		state.pickerValue = ""
	}
	registerGioEventArea(gtx.Ops, closeX, closeY, closeW, closeH, state.getTag(closePath))
}

// rerenderGioWindow requests a new frame for an open Gio window.
func rerenderGioWindow(window *uiWindow) error {
	if window == nil {
		return nil
	}
	window.mu.Lock()
	gw := window.gioWin
	window.mu.Unlock()
	if gw != nil {
		gw.Invalidate()
	}
	return nil
}
