package runtime

import (
	"image/color"
	"strconv"
	"strings"
	"time"
)

func captureRenderState(window *uiWindow, path string, css map[string]string, x int, y int, w int, h int) uiRenderState {
	state := uiRenderState{CSS: map[string]string{}, Layout: map[string]int{"x": x, "y": y, "width": w, "height": h}}
	for key, value := range css {
		state.CSS[key] = value
	}
	if window != nil {
		if window.nextRenderState == nil {
			window.nextRenderState = make(map[string]uiRenderState)
		}
		window.nextRenderState[path] = state
	}
	return state
}

func finalizeRenderState(window *uiWindow) {
	if window == nil {
		return
	}
	if window.nextRenderState != nil {
		window.renderState = window.nextRenderState
	}
	window.nextRenderState = make(map[string]uiRenderState)
}

func applyRuntimeTransition(window *uiWindow, path string, css map[string]string, x int, y int, w int, h int) {
	if window == nil {
		return
	}
	rawTransition := strings.TrimSpace(css["transition"])
	if rawTransition == "" || strings.EqualFold(rawTransition, "none") {
		return
	}
	prev, ok := window.renderState[path]
	captureRenderState(window, path, css, x, y, w, h)
	if !ok {
		return
	}
	if !renderStateChanged(prev, css, x, y, w, h) {
		return
	}
	_, props := parseTransition(css)
	if len(props) == 0 {
		return
	}
	_ = rerenderGioWindow(window)
}

func renderStateChanged(prev uiRenderState, css map[string]string, x int, y int, w int, h int) bool {
	if prev.Layout["x"] != x || prev.Layout["y"] != y || prev.Layout["width"] != w || prev.Layout["height"] != h {
		return true
	}
	if len(prev.CSS) != len(css) {
		return true
	}
	for k, v := range css {
		if pv, ok := prev.CSS[k]; !ok || pv != v {
			return true
		}
	}
	return false
}

func parseTransition(css map[string]string) (time.Duration, []string) {
	raw := strings.TrimSpace(css["transition"])
	if raw == "" {
		return 0, nil
	}
	// CSS transition: comma-separated entries, each "property [duration] [timing] [delay]".
	entries := splitCommaOutsideParens(raw)
	if len(entries) == 0 {
		return 0, nil
	}
	props := make([]string, 0, len(entries))
	maxDuration := time.Duration(0)
	for _, entry := range entries {
		parts := strings.Fields(strings.ToLower(strings.TrimSpace(entry)))
		if len(parts) == 0 {
			continue
		}
		prop := strings.TrimRight(parts[0], ",")
		if prop == "" {
			continue
		}
		props = append(props, prop)
		for _, tok := range parts[1:] {
			if strings.HasSuffix(tok, "ms") {
				if n, err := strconv.Atoi(strings.TrimSuffix(tok, "ms")); err == nil {
					if d := time.Duration(n) * time.Millisecond; d > maxDuration {
						maxDuration = d
					}
					break
				}
			}
			if strings.HasSuffix(tok, "s") {
				if f, err := strconv.ParseFloat(strings.TrimSuffix(tok, "s"), 64); err == nil {
					if d := time.Duration(f * float64(time.Second)); d > maxDuration {
						maxDuration = d
					}
					break
				}
			}
		}
	}
	if maxDuration <= 0 {
		maxDuration = 180 * time.Millisecond
	}
	if len(props) == 0 {
		props = []string{"all"}
	}
	return maxDuration, props
}

func hasTransitionProp(props []string, name string) bool {
	for _, prop := range props {
		if prop == name {
			return true
		}
	}
	return false
}

func cssScale(css map[string]string) float64 {
	if raw := strings.TrimSpace(css["scale"]); raw != "" {
		if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 0 {
			return f
		}
	}
	if raw := strings.TrimSpace(css["transform"]); strings.Contains(raw, "scale(") {
		start := strings.Index(raw, "scale(")
		end := strings.Index(raw[start:], ")")
		if start >= 0 && end > 0 {
			inner := raw[start+len("scale(") : start+end]
			if f, err := strconv.ParseFloat(strings.TrimSpace(inner), 64); err == nil && f > 0 {
				return f
			}
		}
	}
	return 1.0
}

// parseTransformTranslate extracts translateX and translateY from CSS transform property
// Supports: translate(x, y), translateX(x), translateY(y), and combinations
func parseTransformTranslate(css map[string]string) (translateX, translateY float64) {
	raw := strings.TrimSpace(css["transform"])
	if raw == "" {
		return 0, 0
	}

	// Extract value from function like "translateX(10px)" or "translate(10px, 20px)"
	extractFunc := func(funcName string) float64 {
		start := strings.Index(raw, funcName+"(")
		if start < 0 {
			return 0
		}
		start += len(funcName)
		end := strings.Index(raw[start:], ")")
		if end < 0 {
			return 0
		}
		inner := strings.TrimSpace(raw[start+1 : start+end])
		// Remove units like px, em, etc. - keep digits, dot, and minus sign
		inner = strings.TrimSpace(strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || r == '.' || r == '-' {
				return r
			}
			return -1
		}, strings.TrimSpace(inner)))
		if v, err := strconv.ParseFloat(inner, 64); err == nil {
			return v
		}
		return 0
	}

	if strings.Contains(raw, "translate(") {
		// translate(x, y) or translate(x)
		start := strings.Index(raw, "translate(")
		if start >= 0 {
			end := strings.Index(raw[start:], ")")
			if end > 0 {
				inner := strings.TrimSpace(raw[start+len("translate(") : start+end])
				parts := strings.Split(inner, ",")
				if len(parts) >= 1 {
					// Extract first value (translateX)
					xStr := strings.TrimSpace(strings.Map(func(r rune) rune {
						if (r >= '0' && r <= '9') || r == '.' || r == '-' {
							return r
						}
						return -1
					}, strings.TrimSpace(parts[0])))
					if v, err := strconv.ParseFloat(xStr, 64); err == nil {
						translateX = v
					}
				}
				if len(parts) >= 2 {
					// Extract second value (translateY)
					yStr := strings.TrimSpace(strings.Map(func(r rune) rune {
						if (r >= '0' && r <= '9') || r == '.' || r == '-' {
							return r
						}
						return -1
					}, strings.TrimSpace(parts[1])))
					if v, err := strconv.ParseFloat(yStr, 64); err == nil {
						translateY = v
					}
				}
			}
		}
	} else {
		translateX = extractFunc("translateX")
		translateY = extractFunc("translateY")
	}

	return translateX, translateY
}

// parseFilterBrightness extracts brightness value from filter property
// Supports: brightness(value) where value is typically 0.0 to 2.0 or 0% to 200%
func parseFilterBrightness(css map[string]string) float64 {
	raw := strings.TrimSpace(css["filter"])
	if raw == "" {
		return 1.0 // default: no change
	}

	start := strings.Index(raw, "brightness(")
	if start < 0 {
		return 1.0
	}

	start += len("brightness(")
	end := strings.Index(raw[start:], ")")
	if end < 0 {
		return 1.0
	}

	inner := strings.TrimSpace(raw[start : start+end])

	// Handle percentage (e.g., "100%")
	if strings.HasSuffix(inner, "%") {
		inner = strings.TrimSuffix(inner, "%")
		if v, err := strconv.ParseFloat(inner, 64); err == nil {
			return v / 100.0 // Convert percentage to decimal
		}
	}

	// Handle decimal (e.g., "1.0")
	if v, err := strconv.ParseFloat(inner, 64); err == nil {
		return v
	}

	return 1.0
}

func animateFrames(duration time.Duration, frame func(t float64)) {
	go func() {
		steps := 10
		if duration < 100*time.Millisecond {
			steps = 6
		}
		for i := 0; i <= steps; i++ {
			frame(float64(i) / float64(steps))
			time.Sleep(duration / time.Duration(steps))
		}
	}()
}

func mixColor(from color.Color, to color.Color, t float64) color.NRGBA {
	fr, fg, fb, fa := from.RGBA()
	tr, tg, tb, ta := to.RGBA()
	lerp := func(a uint32, b uint32) uint8 {
		v := float64(a>>8) + (float64(b>>8)-float64(a>>8))*t
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v)
	}
	return color.NRGBA{R: lerp(fr, tr), G: lerp(fg, tg), B: lerp(fb, tb), A: lerp(fa, ta)}
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// parsePositionAndOffset extracts position type and offset values (top, left, right, bottom)
// Returns: (position string, offsetX float64, offsetY float64)
type positionInfo struct {
	position  string  // "static", "relative", "absolute", "fixed"
	offsetX   float64 // left/right offset
	offsetY   float64 // top/bottom offset
	isLeft    bool    // true if using left, false if using right
	isTop     bool    // true if using top, false if using bottom
	hasLeft   bool
	hasRight  bool
	hasTop    bool
	hasBottom bool
}

func parsePositionAndOffset(css map[string]string, elemW, elemH int) positionInfo {
	info := positionInfo{position: "static"}
	posRaw := css["position"]
	if posRaw == "" && css["top"] == "" && css["bottom"] == "" && css["left"] == "" && css["right"] == "" {
		return info
	}
	pos := strings.ToLower(strings.TrimSpace(posRaw))
	if pos == "relative" || pos == "absolute" || pos == "fixed" || pos == "sticky" {
		info.position = pos
	}

	// Parse offsets based on position type
	if info.position != "static" {
		// Extract top/bottom
		topVal := strings.TrimSpace(css["top"])
		bottomVal := strings.TrimSpace(css["bottom"])

		if topVal != "" && topVal != "auto" {
			info.offsetY = float64(cssLengthValue(topVal, 0, elemH, elemW, elemH))
			info.isTop = true
			info.hasTop = true
		} else if bottomVal != "" && bottomVal != "auto" {
			info.offsetY = float64(cssLengthValue(bottomVal, 0, elemH, elemW, elemH))
			info.isTop = false
			info.hasBottom = true
		}

		// Extract left/right
		leftVal := strings.TrimSpace(css["left"])
		rightVal := strings.TrimSpace(css["right"])

		if leftVal != "" && leftVal != "auto" {
			info.offsetX = float64(cssLengthValue(leftVal, 0, elemW, elemW, elemH))
			info.isLeft = true
			info.hasLeft = true
		} else if rightVal != "" && rightVal != "auto" {
			info.offsetX = float64(cssLengthValue(rightVal, 0, elemW, elemW, elemH))
			info.isLeft = false
			info.hasRight = true
		}
	}

	return info
}

// parseZIndex extracts z-index value from CSS
func parseZIndex(css map[string]string) int {
	raw := css["z-index"]
	if raw == "" {
		return 0
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "auto") {
		return 0
	}
	if v, err := strconv.Atoi(raw); err == nil {
		return v
	}
	return 0
}

// parseWidthHeightCSS extracts width and height from CSS properties
// Returns: (width int, height int, hasWidth bool, hasHeight bool)
func parseWidthHeightCSS(css map[string]string, elemW, elemH int) (int, int, bool, bool) {
	width := elemW
	height := elemH
	hasWidth := false
	hasHeight := false
	if css["width"] == "" && css["height"] == "" && css["min-width"] == "" && css["max-width"] == "" && css["min-height"] == "" && css["max-height"] == "" {
		return width, height, hasWidth, hasHeight
	}

	if raw := strings.TrimSpace(css["width"]); raw != "" && !strings.EqualFold(raw, "auto") {
		width = cssLengthValue(raw, elemW, elemW, elemW, elemH)
		hasWidth = true
	}

	if raw := strings.TrimSpace(css["height"]); raw != "" && !strings.EqualFold(raw, "auto") {
		height = cssLengthValue(raw, elemH, elemH, elemW, elemH)
		hasHeight = true
	}

	// Apply min/max constraints
	if raw := strings.TrimSpace(css["min-width"]); raw != "" {
		minW := cssLengthValue(raw, 0, elemW, elemW, elemH)
		if width < minW {
			width = minW
		}
	}

	if raw := strings.TrimSpace(css["max-width"]); raw != "" {
		maxW := cssLengthValue(raw, elemW, elemW, elemW, elemH)
		if width > maxW {
			width = maxW
		}
	}

	if raw := strings.TrimSpace(css["min-height"]); raw != "" {
		minH := cssLengthValue(raw, 0, elemH, elemW, elemH)
		if height < minH {
			height = minH
		}
	}

	if raw := strings.TrimSpace(css["max-height"]); raw != "" {
		maxH := cssLengthValue(raw, elemH, elemH, elemW, elemH)
		if height > maxH {
			height = maxH
		}
	}

	return width, height, hasWidth, hasHeight
}

// parseFilterDropShadow extracts drop-shadow value from filter property
// Format: drop-shadow(offset-x offset-y blur-radius color)
type shadowParams struct {
	offsetX float64
	offsetY float64
	blur    float64
	color   string
}

func parseFilterDropShadow(css map[string]string) shadowParams {
	raw := strings.TrimSpace(css["filter"])
	if raw == "" {
		return shadowParams{}
	}

	start := strings.Index(raw, "drop-shadow(")
	if start < 0 {
		return shadowParams{}
	}

	start += len("drop-shadow(")
	depth := 1
	end := start
	for end < len(raw) && depth > 0 {
		if raw[end] == '(' {
			depth++
		} else if raw[end] == ')' {
			depth--
		}
		end++
	}

	if depth != 0 {
		return shadowParams{}
	}

	inner := strings.TrimSpace(raw[start : end-1])
	parts := strings.Fields(inner)
	if len(parts) < 3 {
		return shadowParams{}
	}

	shadow := shadowParams{}
	if v, err := strconv.ParseFloat(strings.TrimRight(strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return -1
	}, parts[0]), "-"), 64); err == nil {
		shadow.offsetX = v
	}

	if v, err := strconv.ParseFloat(strings.TrimRight(strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return -1
	}, parts[1]), "-"), 64); err == nil {
		shadow.offsetY = v
	}

	if v, err := strconv.ParseFloat(strings.TrimRight(strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return -1
	}, parts[2]), "-"), 64); err == nil {
		shadow.blur = v
	}

	if len(parts) >= 4 {
		shadow.color = strings.Join(parts[3:], " ")
	}

	return shadow
}

// parseFilterContrast extracts contrast value from filter property
func parseFilterContrast(css map[string]string) float64 {
	raw := strings.TrimSpace(css["filter"])
	if raw == "" {
		return 1.0
	}

	start := strings.Index(raw, "contrast(")
	if start < 0 {
		return 1.0
	}

	start += len("contrast(")
	end := strings.Index(raw[start:], ")")
	if end < 0 {
		return 1.0
	}

	inner := strings.TrimSpace(raw[start : start+end])

	if strings.HasSuffix(inner, "%") {
		inner = strings.TrimSuffix(inner, "%")
		if v, err := strconv.ParseFloat(inner, 64); err == nil {
			return v / 100.0
		}
	}

	if v, err := strconv.ParseFloat(inner, 64); err == nil {
		return v
	}

	return 1.0
}

// parseFilterSaturate extracts saturate value from filter property
func parseFilterSaturate(css map[string]string) float64 {
	raw := strings.TrimSpace(css["filter"])
	if raw == "" {
		return 1.0
	}

	start := strings.Index(raw, "saturate(")
	if start < 0 {
		return 1.0
	}

	start += len("saturate(")
	end := strings.Index(raw[start:], ")")
	if end < 0 {
		return 1.0
	}

	inner := strings.TrimSpace(raw[start : start+end])

	if strings.HasSuffix(inner, "%") {
		inner = strings.TrimSuffix(inner, "%")
		if v, err := strconv.ParseFloat(inner, 64); err == nil {
			return v / 100.0
		}
	}

	if v, err := strconv.ParseFloat(inner, 64); err == nil {
		return v
	}

	return 1.0
}

// parseFilterGrayscale extracts grayscale value from filter property
func parseFilterGrayscale(css map[string]string) float64 {
	raw := strings.TrimSpace(css["filter"])
	if raw == "" {
		return 0.0
	}

	start := strings.Index(raw, "grayscale(")
	if start < 0 {
		return 0.0
	}

	start += len("grayscale(")
	end := strings.Index(raw[start:], ")")
	if end < 0 {
		return 0.0
	}

	inner := strings.TrimSpace(raw[start : start+end])

	if strings.HasSuffix(inner, "%") {
		inner = strings.TrimSuffix(inner, "%")
		if v, err := strconv.ParseFloat(inner, 64); err == nil {
			return v / 100.0
		}
	}

	if v, err := strconv.ParseFloat(inner, 64); err == nil {
		return v
	}

	return 0.0
}

// parseFilterInvert extracts invert value from filter property
func parseFilterInvert(css map[string]string) float64 {
	raw := strings.TrimSpace(css["filter"])
	if raw == "" {
		return 0.0
	}

	start := strings.Index(raw, "invert(")
	if start < 0 {
		return 0.0
	}

	start += len("invert(")
	end := strings.Index(raw[start:], ")")
	if end < 0 {
		return 0.0
	}

	inner := strings.TrimSpace(raw[start : start+end])

	if strings.HasSuffix(inner, "%") {
		inner = strings.TrimSuffix(inner, "%")
		if v, err := strconv.ParseFloat(inner, 64); err == nil {
			return v / 100.0
		}
	}

	if v, err := strconv.ParseFloat(inner, 64); err == nil {
		return v
	}

	return 0.0
}

// parseTextDecoration extracts text-decoration properties
type textDecorationInfo struct {
	line      string // "underline", "overline", "line-through", "none"
	style     string // "solid", "double", "dotted", "dashed", "wavy"
	color     string // color hex/name
	thickness string // "auto", px value
}

func parseTextDecoration(css map[string]string) textDecorationInfo {
	info := textDecorationInfo{line: "none"}

	if raw := strings.TrimSpace(css["text-decoration"]); raw != "" {
		parts := strings.Fields(raw)
		if len(parts) > 0 {
			info.line = strings.ToLower(parts[0])
		}
		if len(parts) > 1 {
			info.style = strings.ToLower(parts[1])
		}
		if len(parts) > 2 {
			info.color = parts[2]
		}
	}

	if raw := strings.TrimSpace(css["text-decoration-line"]); raw != "" {
		info.line = strings.ToLower(raw)
	}
	if raw := strings.TrimSpace(css["text-decoration-style"]); raw != "" {
		info.style = strings.ToLower(raw)
	}
	if raw := strings.TrimSpace(css["text-decoration-color"]); raw != "" {
		info.color = raw
	}
	if raw := strings.TrimSpace(css["text-decoration-thickness"]); raw != "" {
		info.thickness = raw
	}

	return info
}

// parseCursor extracts cursor property value
func parseCursor(css map[string]string) string {
	if raw := strings.TrimSpace(css["cursor"]); raw != "" {
		return strings.ToLower(raw)
	}
	return "default"
}

// parseAspectRatio extracts aspect-ratio property
// Returns: (width:height ratio, hasAspectRatio bool)
func parseAspectRatio(css map[string]string) (float64, bool) {
	raw := strings.TrimSpace(css["aspect-ratio"])
	if raw == "" || strings.EqualFold(raw, "auto") {
		return 0, false
	}

	// Parse "16 / 9" or "16/9" or "1.777"
	parts := strings.Split(raw, "/")
	if len(parts) >= 2 {
		widthStr := strings.TrimSpace(parts[0])
		heightStr := strings.TrimSpace(parts[1])
		if w, err1 := strconv.ParseFloat(widthStr, 64); err1 == nil {
			if h, err2 := strconv.ParseFloat(heightStr, 64); err2 == nil && h != 0 {
				return w / h, true
			}
		}
	} else if len(parts) == 1 {
		if ratio, err := strconv.ParseFloat(parts[0], 64); err == nil && ratio > 0 {
			return ratio, true
		}
	}

	return 0, false
}
