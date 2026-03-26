package runtime

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"gioui.org/text"
)

type textMeasureKey struct {
	text          string
	fontSizeMilli int
	letterSpacing string
	lineHeight    string
	mode          byte
}

type textMeasureValue struct {
	w int
	h int
}

var (
	textMeasureCacheMu sync.RWMutex
	textMeasureCache   = map[textMeasureKey]textMeasureValue{}
)

func textMeasureKeyFor(text string, fontSize float32, css map[string]string, mode byte) (textMeasureKey, bool) {
	if len(text) > 256 {
		return textMeasureKey{}, false
	}
	return textMeasureKey{
		text:          text,
		fontSizeMilli: int(fontSize * 1000),
		letterSpacing: strings.TrimSpace(css["letter-spacing"]),
		lineHeight:    strings.TrimSpace(css["line-height"]),
		mode:          mode,
	}, true
}

func getCachedTextMeasure(key textMeasureKey) (textMeasureValue, bool) {
	textMeasureCacheMu.RLock()
	v, ok := textMeasureCache[key]
	textMeasureCacheMu.RUnlock()
	return v, ok
}

func putCachedTextMeasure(key textMeasureKey, v textMeasureValue) {
	textMeasureCacheMu.Lock()
	if len(textMeasureCache) > 2048 {
		textMeasureCache = map[textMeasureKey]textMeasureValue{}
	}
	textMeasureCache[key] = v
	textMeasureCacheMu.Unlock()
}

func parseHexColor(input string, fallback color.Color) color.Color {
	text := strings.TrimSpace(strings.TrimPrefix(input, "#"))
	if len(text) == 0 {
		return fallback
	}
	lowerInput := strings.ToLower(strings.TrimSpace(input))
	if strings.HasPrefix(lowerInput, "linear-gradient(") || strings.HasPrefix(lowerInput, "radial-gradient(") {
		start := strings.Index(lowerInput, "(")
		end := strings.LastIndex(lowerInput, ")")
		if start >= 0 && end > start {
			inner := input[start+1 : end]
			parts := splitColorArgs(inner)
			for _, p := range parts {
				cand := strings.TrimSpace(p)
				if cand == "" {
					continue
				}
				// Skip gradient-direction tokens until first parseable color stop.
				if strings.Contains(cand, "deg") || strings.HasPrefix(strings.ToLower(cand), "to ") || strings.HasPrefix(strings.ToLower(cand), "circle") || strings.HasPrefix(strings.ToLower(cand), "ellipse") {
					continue
				}
				if c := parseHexColor(cand, nil); c != nil {
					return c
				}
				fields := strings.Fields(cand)
				if len(fields) > 0 {
					if c := parseHexColor(fields[0], nil); c != nil {
						return c
					}
				}
			}
		}
		return fallback
	}
	if strings.HasPrefix(lowerInput, "rgb(") || strings.HasPrefix(lowerInput, "rgba(") {
		return parseRGBColor(input, fallback)
	}
	if strings.HasPrefix(lowerInput, "hsl(") || strings.HasPrefix(lowerInput, "hsla(") {
		return parseHSLColor(input, fallback)
	}
	if strings.HasPrefix(lowerInput, "cmyk(") {
		return parseCMYKColor(input, fallback)
	}
	if named := parseNamedColor(input); named != nil {
		return named
	}
	if len(text) == 4 {
		text = string([]byte{text[0], text[0], text[1], text[1], text[2], text[2], text[3], text[3]})
	}
	if len(text) == 3 {
		text = string([]byte{text[0], text[0], text[1], text[1], text[2], text[2]})
	}
	if len(text) == 8 {
		r, errR := strconv.ParseUint(text[0:2], 16, 8)
		g, errG := strconv.ParseUint(text[2:4], 16, 8)
		b, errB := strconv.ParseUint(text[4:6], 16, 8)
		a, errA := strconv.ParseUint(text[6:8], 16, 8)
		if errR != nil || errG != nil || errB != nil || errA != nil {
			return fallback
		}
		return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}
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

func splitColorArgs(input string) []string {
	text := strings.TrimSpace(input)
	if text == "" {
		return nil
	}
	if strings.Contains(text, ",") {
		parts := make([]string, 0, 6)
		depth := 0
		start := 0
		for i, r := range text {
			switch r {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case ',':
				if depth == 0 {
					chunk := strings.TrimSpace(text[start:i])
					if chunk != "" {
						parts = append(parts, chunk)
					}
					start = i + 1
				}
			}
		}
		last := strings.TrimSpace(text[start:])
		if last != "" {
			parts = append(parts, last)
		}
		if len(parts) > 0 {
			return parts
		}
	}
	text = strings.ReplaceAll(text, "/", " ")
	return strings.Fields(text)
}

func parseRGBColor(input string, fallback color.Color) color.Color {
	text := strings.ToLower(strings.TrimSpace(input))
	start := strings.Index(text, "(")
	end := strings.LastIndex(text, ")")
	if start < 0 || end <= start {
		return fallback
	}
	parts := splitColorArgs(text[start+1 : end])
	if len(parts) < 3 {
		return fallback
	}
	parseChannel := func(raw string) (uint8, bool) {
		v := strings.TrimSpace(raw)
		if strings.HasSuffix(v, "%") {
			f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			if err != nil {
				return 0, false
			}
			if f < 0 {
				f = 0
			}
			if f > 100 {
				f = 100
			}
			return uint8((f / 100.0) * 255.0), true
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return uint8(n), true
	}
	r, okR := parseChannel(parts[0])
	g, okG := parseChannel(parts[1])
	b, okB := parseChannel(parts[2])
	if !okR || !okG || !okB {
		return fallback
	}
	a := uint8(255)
	if len(parts) >= 4 {
		af, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil {
			return fallback
		}
		if af < 0 {
			af = 0
		}
		if af > 1 {
			af = 1
		}
		a = uint8(af * 255.0)
	}
	return color.NRGBA{R: r, G: g, B: b, A: a}
}

func parseHSLColor(input string, fallback color.Color) color.Color {
	text := strings.ToLower(strings.TrimSpace(input))
	start := strings.Index(text, "(")
	end := strings.LastIndex(text, ")")
	if start < 0 || end <= start {
		return fallback
	}
	parts := splitColorArgs(text[start+1 : end])
	if len(parts) < 3 {
		return fallback
	}
	parseHue := func(raw string) (float64, bool) {
		v := strings.TrimSpace(strings.TrimSuffix(raw, "deg"))
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		f = math.Mod(f, 360.0)
		if f < 0 {
			f += 360.0
		}
		return f, true
	}
	parsePct := func(raw string) (float64, bool) {
		v := strings.TrimSpace(raw)
		if !strings.HasSuffix(v, "%") {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		if err != nil {
			return 0, false
		}
		if f < 0 {
			f = 0
		}
		if f > 100 {
			f = 100
		}
		return f / 100.0, true
	}
	h, okH := parseHue(parts[0])
	s, okS := parsePct(parts[1])
	l, okL := parsePct(parts[2])
	if !okH || !okS || !okL {
		return fallback
	}
	a := 1.0
	if len(parts) >= 4 {
		v := strings.TrimSpace(parts[3])
		if strings.HasSuffix(v, "%") {
			f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			if err != nil {
				return fallback
			}
			a = f / 100.0
		} else {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fallback
			}
			a = f
		}
		if a < 0 {
			a = 0
		}
		if a > 1 {
			a = 1
		}
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60.0, 2)-1))
	m := l - c/2
	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	to8 := func(v float64) uint8 {
		p := (v + m) * 255.0
		if p < 0 {
			p = 0
		}
		if p > 255 {
			p = 255
		}
		return uint8(p + 0.5)
	}
	return color.NRGBA{R: to8(r1), G: to8(g1), B: to8(b1), A: uint8(a*255 + 0.5)}
}

func parseCMYKColor(input string, fallback color.Color) color.Color {
	text := strings.ToLower(strings.TrimSpace(input))
	start := strings.Index(text, "(")
	end := strings.LastIndex(text, ")")
	if start < 0 || end <= start {
		return fallback
	}
	parts := splitColorArgs(text[start+1 : end])
	if len(parts) < 4 {
		return fallback
	}
	parseC := func(raw string) (float64, bool) {
		v := strings.TrimSpace(raw)
		if strings.HasSuffix(v, "%") {
			f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			if err != nil {
				return 0, false
			}
			if f < 0 {
				f = 0
			}
			if f > 100 {
				f = 100
			}
			return f / 100.0, true
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return f, true
	}
	c, okC := parseC(parts[0])
	m, okM := parseC(parts[1])
	y, okY := parseC(parts[2])
	k, okK := parseC(parts[3])
	if !okC || !okM || !okY || !okK {
		return fallback
	}
	a := 1.0
	if len(parts) >= 5 {
		f, err := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		if err == nil {
			a = f
			if a < 0 {
				a = 0
			}
			if a > 1 {
				a = 1
			}
		}
	}
	r := 255.0 * (1 - c) * (1 - k)
	g := 255.0 * (1 - m) * (1 - k)
	b := 255.0 * (1 - y) * (1 - k)
	clamp := func(v float64) uint8 {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v + 0.5)
	}
	return color.NRGBA{R: clamp(r), G: clamp(g), B: clamp(b), A: uint8(a*255 + 0.5)}
}

func parseNamedColor(input string) color.Color {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "white":
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case "black":
		return color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	case "red":
		return color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	case "green":
		return color.NRGBA{R: 0, G: 128, B: 0, A: 255}
	case "blue":
		return color.NRGBA{R: 0, G: 0, B: 255, A: 255}
	case "yellow":
		return color.NRGBA{R: 255, G: 255, B: 0, A: 255}
	case "gray", "grey":
		return color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	case "silver":
		return color.NRGBA{R: 192, G: 192, B: 192, A: 255}
	case "maroon":
		return color.NRGBA{R: 128, G: 0, B: 0, A: 255}
	case "navy":
		return color.NRGBA{R: 0, G: 0, B: 128, A: 255}
	case "teal":
		return color.NRGBA{R: 0, G: 128, B: 128, A: 255}
	case "olive":
		return color.NRGBA{R: 128, G: 128, B: 0, A: 255}
	case "lime":
		return color.NRGBA{R: 0, G: 255, B: 0, A: 255}
	case "aqua", "cyan":
		return color.NRGBA{R: 0, G: 255, B: 255, A: 255}
	case "magenta", "fuchsia":
		return color.NRGBA{R: 255, G: 0, B: 255, A: 255}
	case "orange":
		return color.NRGBA{R: 255, G: 165, B: 0, A: 255}
	case "purple":
		return color.NRGBA{R: 128, G: 0, B: 128, A: 255}
	case "transparent":
		return color.NRGBA{R: 0, G: 0, B: 0, A: 0}
	}
	return nil
}

func applyCSSOpacity(c color.Color, css map[string]string) color.Color {
	v := strings.TrimSpace(css["opacity"])
	if v == "" {
		return c
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return c
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	r, g, b, a := c.RGBA()
	n := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	n.A = uint8(float64(n.A) * f)
	return n
}

func cssTextTransform(text string, css map[string]string) string {
	switch strings.ToLower(strings.TrimSpace(css["text-transform"])) {
	case "uppercase":
		return strings.ToUpper(text)
	case "lowercase":
		return strings.ToLower(text)
	case "capitalize":
		parts := strings.Fields(text)
		for i, p := range parts {
			if len(p) == 0 {
				continue
			}
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
		return strings.Join(parts, " ")
	default:
		return text
	}
}

func cssApplyLetterSpacing(text string, css map[string]string) string {
	v := strings.TrimSpace(css["letter-spacing"])
	if v == "" || text == "" {
		return text
	}
	if strings.Contains(v, "0.") {
		// Subpixel letter-spacing cannot be emulated by inserting whole spaces.
		// Keep original text to avoid broken words.
		return text
	}
	steps := cssLengthValue(v, 0, 0, 0, 0)
	if steps <= 0 {
		return text
	}
	pad := strings.Repeat(" ", steps)
	runes := []rune(text)
	if len(runes) < 2 {
		return text
	}
	var b strings.Builder
	for i, r := range runes {
		b.WriteRune(r)
		if i < len(runes)-1 {
			b.WriteString(pad)
		}
	}
	return b.String()
}

func cssLineHeightPx(css map[string]string, fontSize float32) int {
	v := strings.TrimSpace(css["line-height"])
	if v == "" {
		return max(1, int(float32(fontSize)*1.35))
	}
	if strings.HasSuffix(v, "%") {
		pct, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		if err == nil && pct > 0 {
			return max(1, int((pct/100.0)*float64(fontSize)))
		}
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
		if !strings.Contains(v, "px") && !strings.Contains(v, "vh") && !strings.Contains(v, "vw") && !strings.Contains(v, "%") {
			return max(1, int(f*float64(fontSize)))
		}
	}
	return max(1, cssLengthValue(v, int(float32(fontSize)*1.35), 0, 0, 0))
}

func estimateTextBox(text string, fontSize float32, css map[string]string) (int, int) {
	if key, ok := textMeasureKeyFor(text, fontSize, css, 0); ok {
		if cached, hit := getCachedTextMeasure(key); hit {
			return cached.w, cached.h
		}
		w, h := estimateTextBoxUncached(text, fontSize, css)
		putCachedTextMeasure(key, textMeasureValue{w: w, h: h})
		return w, h
	}
	return estimateTextBoxUncached(text, fontSize, css)
}

func estimateTextBoxUncached(text string, fontSize float32, css map[string]string) (int, int) {
	if text == "" {
		return max(8, int(fontSize)*2/3), cssLineHeightPx(css, fontSize)
	}
	lines := strings.Split(text, "\n")
	maxChars := 0
	hasWideSymbol := false
	for _, line := range lines {
		count := len([]rune(line))
		if count > maxChars {
			maxChars = count
		}
		for _, r := range line {
			switch r {
			case '%', '&', '@', '#', '$', 'W', 'M':
				hasWideSymbol = true
			}
		}
	}
	// Use a conservative glyph estimate plus guard pixels so one-line labels
	// in tight flex/grid slots don't lose trailing characters.
	charWidth := float64(fontSize) * 0.78
	width := int(float64(maxChars) * charWidth)
	width += cssLengthValue(css["letter-spacing"], 0, 0, 0, 0) * max(0, maxChars-1)
	width += max(6, int(float64(fontSize)*0.5))
	if hasWideSymbol {
		width += max(2, int(float64(fontSize)*0.25))
	}
	height := len(lines) * cssLineHeightPx(css, fontSize)
	// Keep only a tiny guard pixel budget; large guards distort flex centering
	// because text nodes become taller than their visual line box.
	height += max(1, int(float64(fontSize)*0.06))
	return max(8, width), max(1, height)
}

func estimateTextLayoutBox(text string, fontSize float32, css map[string]string) (int, int) {
	if key, ok := textMeasureKeyFor(text, fontSize, css, 1); ok {
		if cached, hit := getCachedTextMeasure(key); hit {
			return cached.w, cached.h
		}
		w, h := estimateTextLayoutBoxUncached(text, fontSize, css)
		putCachedTextMeasure(key, textMeasureValue{w: w, h: h})
		return w, h
	}
	return estimateTextLayoutBoxUncached(text, fontSize, css)
}

func estimateTextLayoutBoxUncached(text string, fontSize float32, css map[string]string) (int, int) {
	if text == "" {
		return max(4, int(fontSize)*2/3), cssLineHeightPx(css, fontSize)
	}
	lines := strings.Split(text, "\n")
	maxChars := 0
	for _, line := range lines {
		count := len([]rune(line))
		if count > maxChars {
			maxChars = count
		}
	}
	charWidth := float64(fontSize) * 0.72
	width := int(float64(maxChars) * charWidth)
	width += cssLengthValue(css["letter-spacing"], 0, 0, 0, 0) * max(0, maxChars-1)
	width += max(4, int(float64(fontSize)*0.28))
	height := len(lines)*cssLineHeightPx(css, fontSize) + 1
	return max(4, width), max(1, height)
}

func fitTextToWidth(text string, fontSize float32, css map[string]string, width int) string {
	if width <= 0 || text == "" {
		return text
	}
	availableChars := int(float64(width) / max(1.0, float64(fontSize)*0.62))
	if availableChars <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= availableChars {
		return text
	}
	if availableChars <= 1 {
		return string(runes[:1])
	}
	if strings.ToLower(strings.TrimSpace(css["text-overflow"])) != "ellipsis" {
		return string(runes[:availableChars])
	}
	return string(runes[:availableChars-1]) + "…"
}

func textCharsForWidth(fontSize float32, width int) int {
	if width <= 0 {
		return 0
	}
	chars := int(float64(width) / max(1.0, float64(fontSize)*0.62))
	if chars < 1 {
		return 1
	}
	return chars
}

func wrapTextToWidth(text string, fontSize float32, width int) string {
	if text == "" || width <= 0 {
		return text
	}
	maxChars := textCharsForWidth(fontSize, width)
	if maxChars <= 1 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	lines := make([]string, 0, 4)
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		candidate := line + " " + word
		if len([]rune(candidate)) <= maxChars {
			line = candidate
			continue
		}
		lines = append(lines, line)
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func sanitizeRenderText(text string) string {
	if text == "" {
		return text
	}
	var b strings.Builder
	lastWasSpace := false
	for _, r := range text {
		switch r {
		case '\uFFFD':
			if !lastWasSpace {
				b.WriteRune(' ')
				lastWasSpace = true
			}
		case '\t', '\r':
			if !lastWasSpace {
				b.WriteRune(' ')
				lastWasSpace = true
			}
		case '\n':
			b.WriteRune('\n')
			lastWasSpace = false
		default:
			if r < 32 {
				continue
			}
			if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
				if !lastWasSpace {
					b.WriteRune(' ')
					lastWasSpace = true
				}
				continue
			}
			if !unicode.IsPrint(r) {
				continue
			}
			b.WriteRune(r)
			lastWasSpace = false
		}
	}
	return b.String()
}

func cssAllowsWrap(css map[string]string) bool {
	whiteSpace := strings.ToLower(strings.TrimSpace(css["white-space"]))
	if whiteSpace == "nowrap" {
		return false
	}
	overflow := strings.ToLower(strings.TrimSpace(css["overflow"]))
	if overflow == "hidden" && strings.ToLower(strings.TrimSpace(css["text-overflow"])) == "ellipsis" {
		return false
	}
	return true
}

func intrinsicNodeSize(node *uiNode, ss *styleSheet, viewportW int, viewportH int) (int, int) {
	return intrinsicNodeSizeWithInherited(node, ss, viewportW, viewportH, nil)
}

func intrinsicNodeSizeWithInherited(node *uiNode, ss *styleSheet, viewportW int, viewportH int, parentCSS map[string]string) (int, int) {
	if node == nil {
		return 0, 0
	}
	css := resolveNodeStyle(node, ss, viewportW)
	css = mergeInheritedTextCSS(css, parentCSS)
	if strings.EqualFold(strings.TrimSpace(css["display"]), "none") {
		return 0, 0
	}
	hasExplicitWidth := strings.TrimSpace(css["width"]) != ""
	hasExplicitHeight := strings.TrimSpace(css["height"]) != ""
	width := nodeLayoutLength(node, css, "width", "width", viewportW, viewportW, viewportH, -1)
	height := nodeLayoutLength(node, css, "height", "height", viewportH, viewportW, viewportH, -1)
	paddingTop := nodeLayoutLength(node, css, "paddingTop", "padding-top", viewportH, viewportW, viewportH, uiNodePropInt(node, "pady", uiNodePropInt(node, "padding", 0)))
	paddingRight := nodeLayoutLength(node, css, "paddingRight", "padding-right", viewportW, viewportW, viewportH, uiNodePropInt(node, "padx", uiNodePropInt(node, "padding", 0)))
	paddingBottom := nodeLayoutLength(node, css, "paddingBottom", "padding-bottom", viewportH, viewportW, viewportH, uiNodePropInt(node, "pady", uiNodePropInt(node, "padding", 0)))
	paddingLeft := nodeLayoutLength(node, css, "paddingLeft", "padding-left", viewportW, viewportW, viewportH, uiNodePropInt(node, "padx", uiNodePropInt(node, "padding", 0)))
	gap := nodeLayoutLength(node, css, "gap", "gap", max(viewportW, viewportH), viewportW, viewportH, 0)
	kind := strings.ToLower(strings.TrimSpace(node.Kind))

	if width >= 0 && height >= 0 {
		return width, height
	}

	switch kind {
	case "text", "label":
		fontSize := cssFontSize(css, 14)
		text := cssApplyLetterSpacing(cssTextTransform(uiNodePropString(node, "text", ""), css), css)
		if width > 0 && cssAllowsWrap(css) && !cssShouldClipText(css) {
			inner := max(1, width-paddingLeft-paddingRight)
			text = wrapTextToWidth(text, fontSize, inner)
		}
		w, h := estimateTextLayoutBox(text, fontSize, css)
		if width < 0 {
			width = w + paddingLeft + paddingRight
		}
		if height < 0 {
			height = h + paddingTop + paddingBottom
		}
	case "button":
		fontSize := cssFontSize(css, 13)
		text := cssApplyLetterSpacing(cssTextTransform(uiNodePropString(node, "text", ""), css), css)
		w, h := estimateTextLayoutBox(text, fontSize, css)
		iconExtra := 0
		if uiNodePropString(node, "icon", "") != "" {
			iconExtra = max(18, int(fontSize)+4)
		}
		if width < 0 {
			width = w + iconExtra + paddingLeft + paddingRight + 28
		}
		if height < 0 {
			height = max(30, h+paddingTop+paddingBottom+10)
		}
	case "input":
		inputType := strings.ToLower(uiNodePropString(node, "type", "text"))
		if width < 0 {
			if inputType == "checkbox" || inputType == "check" {
				width = 120
			} else if inputType == "range" || inputType == "slider" {
				width = 220
			} else {
				width = 180
			}
		}
		if height < 0 {
			if inputType == "textarea" || inputType == "multiline" {
				height = 92
			} else {
				height = 36
			}
		}
	case "native":
		component := strings.ToLower(uiNodePropString(node, "component", uiNodePropString(node, "native", "label")))
		if width < 0 {
			switch component {
			case "image", "img", "svg":
				width = 180
			case "icon":
				width = 18
			case "slider":
				width = 220
			case "progress", "progressbar":
				width = 220
			case "select", "dropdown":
				width = 180
			default:
				width = 120
			}
		}
		if height < 0 {
			switch component {
			case "image", "img", "svg":
				height = 120
			case "icon":
				height = 18
			case "progress", "progressbar":
				height = 14
			default:
				height = 36
			}
		}
	default:
		direction := nodeLayoutString(node, css, "direction", "direction", strings.ToLower(uiNodePropString(node, "layout", "column")))
		if direction != "row" {
			direction = "column"
		}
		contentW := 0
		contentH := 0
		for _, child := range node.Children {
			cw, ch := intrinsicNodeSizeWithInherited(child, ss, viewportW, viewportH, css)
			childCSS := resolveNodeStyle(child, ss, viewportW)
			childCSS = mergeInheritedTextCSS(childCSS, css)
			mw := nodeLayoutLength(child, childCSS, "marginLeft", "margin-left", viewportW, viewportW, viewportH, 0) + nodeLayoutLength(child, childCSS, "marginRight", "margin-right", viewportW, viewportW, viewportH, 0)
			mh := nodeLayoutLength(child, childCSS, "marginTop", "margin-top", viewportH, viewportW, viewportH, 0) + nodeLayoutLength(child, childCSS, "marginBottom", "margin-bottom", viewportH, viewportW, viewportH, 0)
			if direction == "row" {
				contentW += cw + mw
				contentH = max(contentH, ch+mh)
			} else {
				contentH += ch + mh
				contentW = max(contentW, cw+mw)
			}
		}
		visibleChildren := len(node.Children)
		if visibleChildren > 1 {
			if direction == "row" {
				contentW += gap * (visibleChildren - 1)
			} else {
				contentH += gap * (visibleChildren - 1)
			}
		}
		if width < 0 {
			width = contentW + paddingLeft + paddingRight
		}
		if height < 0 {
			height = contentH + paddingTop + paddingBottom
		}
	}

	if ar, ok := parseAspectRatio(css); ok && ar > 0.001 {
		switch {
		case width > 0 && !hasExplicitHeight:
			height = max(1, int(float64(width)/ar))
		case height > 0 && !hasExplicitWidth:
			width = max(1, int(float64(height)*ar))
		case width <= 0 && height <= 0:
			baseW := max(96, paddingLeft+paddingRight+96)
			width = baseW
			height = max(1, int(float64(baseW)/ar))
		}
	}

	return max(1, width), max(1, height)
}

func cssTextAlign(css map[string]string) text.Alignment {
	switch strings.ToLower(strings.TrimSpace(css["text-align"])) {
	case "center":
		return text.Middle
	case "right", "end":
		return text.End
	default:
		return text.Start
	}
}

func uiClassTokens(raw string) []string {
	if raw == "" {
		return nil
	}
	clean := strings.ReplaceAll(raw, ",", " ")
	parts := strings.Fields(clean)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		cls := strings.TrimSpace(strings.TrimPrefix(p, "."))
		if cls != "" {
			out = append(out, cls)
		}
	}
	return out
}

func applyHoverStyles(css map[string]string, props map[string]any, ss *styleSheet, hovered bool) {
	if !hovered || ss == nil {
		return
	}
	for _, cls := range uiClassTokens(anyToString(props["class"], "")) {
		if m := ss.getClassProps(cls + ":hover"); m != nil {
			// If the hover class overrides a box shorthand, clear per-side values first
			// so the re-expansion below takes full effect.
			if _, ok := m["padding"]; ok {
				delete(css, "padding-top")
				delete(css, "padding-right")
				delete(css, "padding-bottom")
				delete(css, "padding-left")
			}
			if _, ok := m["margin"]; ok {
				delete(css, "margin-top")
				delete(css, "margin-right")
				delete(css, "margin-bottom")
				delete(css, "margin-left")
			}
			for k, v := range m {
				css[cssCanonicalName(k)] = v
			}
		}
	}
	cssExpandBoxShorthand(css, "padding")
	cssExpandBoxShorthand(css, "margin")
	cssExpandBorderShorthand(css)
	cssResolveVariables(css)
}

func applyActiveStyles(css map[string]string, props map[string]any, ss *styleSheet, active bool) {
	if !active || ss == nil {
		return
	}
	for _, cls := range uiClassTokens(anyToString(props["class"], "")) {
		if m := ss.getClassProps(cls + ":active"); m != nil {
			if _, ok := m["padding"]; ok {
				delete(css, "padding-top")
				delete(css, "padding-right")
				delete(css, "padding-bottom")
				delete(css, "padding-left")
			}
			if _, ok := m["margin"]; ok {
				delete(css, "margin-top")
				delete(css, "margin-right")
				delete(css, "margin-bottom")
				delete(css, "margin-left")
			}
			for k, v := range m {
				css[cssCanonicalName(k)] = v
			}
		}
	}
	cssExpandBoxShorthand(css, "padding")
	cssExpandBoxShorthand(css, "margin")
	cssExpandBorderShorthand(css)
	cssResolveVariables(css)
}

func hasHoverSelector(props map[string]any, ss *styleSheet) bool {
	if ss == nil {
		return false
	}
	for _, cls := range uiClassTokens(anyToString(props["class"], "")) {
		if m := ss.getClassProps(cls + ":hover"); len(m) > 0 {
			return true
		}
	}
	return false
}

func hasActiveSelector(props map[string]any, ss *styleSheet) bool {
	if ss == nil {
		return false
	}
	for _, cls := range uiClassTokens(anyToString(props["class"], "")) {
		if m := ss.getClassProps(cls + ":active"); len(m) > 0 {
			return true
		}
	}
	return false
}

func isPathHovered(window *uiWindow, path string) bool {
	if window == nil || window.hoverState == nil {
		return false
	}
	return window.hoverState[path]
}

func isPathActive(window *uiWindow, path string) bool {
	if window == nil || window.activeState == nil {
		return false
	}
	return window.activeState[path]
}

func setPathHovered(window *uiWindow, path string, hovered bool) {
	if window == nil {
		return
	}
	if window.hoverState == nil {
		window.hoverState = make(map[string]bool)
	}
	current := window.hoverState[path]
	if current == hovered {
		return
	}
	if hovered {
		window.hoverState[path] = true
	} else {
		delete(window.hoverState, path)
	}
	_ = rerenderGioWindow(window)
}

func setPathActive(window *uiWindow, path string, active bool) {
	if window == nil {
		return
	}
	if window.activeState == nil {
		window.activeState = make(map[string]bool)
	}
	current := window.activeState[path]
	if current == active {
		return
	}
	if active {
		window.activeState[path] = true
	} else {
		delete(window.activeState, path)
	}
	_ = rerenderGioWindow(window)
}

func cssShouldClipText(css map[string]string) bool {
	overflow := strings.ToLower(strings.TrimSpace(css["overflow"]))
	textOverflow := strings.ToLower(strings.TrimSpace(css["text-overflow"]))
	whiteSpace := strings.ToLower(strings.TrimSpace(css["white-space"]))
	if textOverflow == "ellipsis" {
		return true
	}
	if overflow == "hidden" && whiteSpace == "nowrap" {
		return true
	}
	return false
}

// applyBrightnessToColor multiplies RGBA channels by brightness factor
// brightness 1.0 = no change, 0.5 = 50% darker, 1.5 = 50% brighter
func applyBrightnessToColor(col color.NRGBA, brightness float64) color.NRGBA {
	if brightness < 0 {
		brightness = 0
	}
	return color.NRGBA{
		R: clampUint8(float64(col.R) * brightness),
		G: clampUint8(float64(col.G) * brightness),
		B: clampUint8(float64(col.B) * brightness),
		A: col.A, // alpha channel not affected by brightness
	}
}

func clampUint8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// applyContrastToColor adjusts contrast: values < 1.0 reduce contrast, > 1.0 increase it
func applyContrastToColor(col color.NRGBA, contrast float64) color.NRGBA {
	if contrast < 0 {
		contrast = 0
	}
	mid := 128.0
	return color.NRGBA{
		R: clampUint8(mid + (float64(col.R)-mid)*contrast),
		G: clampUint8(mid + (float64(col.G)-mid)*contrast),
		B: clampUint8(mid + (float64(col.B)-mid)*contrast),
		A: col.A,
	}
}

// applySaturationToColor adjusts color saturation
// 0.0 = grayscale, 1.0 = no change, 2.0 = double saturation
func applySaturationToColor(col color.NRGBA, saturation float64) color.NRGBA {
	if saturation < 0 {
		saturation = 0
	}
	r := float64(col.R)
	g := float64(col.G)
	b := float64(col.B)
	// CSS Filter Effects (saturate) color matrix in sRGB.
	r2 := (0.213+0.787*saturation)*r + (0.715-0.715*saturation)*g + (0.072-0.072*saturation)*b
	g2 := (0.213-0.213*saturation)*r + (0.715+0.285*saturation)*g + (0.072-0.072*saturation)*b
	b2 := (0.213-0.213*saturation)*r + (0.715-0.715*saturation)*g + (0.072+0.928*saturation)*b
	return color.NRGBA{
		R: clampUint8(r2),
		G: clampUint8(g2),
		B: clampUint8(b2),
		A: col.A,
	}
}

// applyGrayscaleToColor converts to grayscale: 0.0 = full color, 1.0 = full grayscale
func applyGrayscaleToColor(col color.NRGBA, grayscale float64) color.NRGBA {
	if grayscale < 0 {
		grayscale = 0
	}
	if grayscale > 1.0 {
		grayscale = 1.0
	}
	// CSS grayscale(amount) is equivalent to saturate(1-amount) matrix in sRGB.
	r := float64(col.R)
	g := float64(col.G)
	b := float64(col.B)
	s := 1.0 - grayscale
	r2 := (0.2126+0.7874*s)*r + (0.7152-0.7152*s)*g + (0.0722-0.0722*s)*b
	g2 := (0.2126-0.2126*s)*r + (0.7152+0.2848*s)*g + (0.0722-0.0722*s)*b
	b2 := (0.2126-0.2126*s)*r + (0.7152-0.7152*s)*g + (0.0722+0.9278*s)*b
	return color.NRGBA{
		R: clampUint8(r2),
		G: clampUint8(g2),
		B: clampUint8(b2),
		A: col.A,
	}
}

// applyInvertToColor inverts colors: 0.0 = no inversion, 1.0 = full inversion
func applyInvertToColor(col color.NRGBA, invert float64) color.NRGBA {
	if invert < 0 {
		invert = 0
	}
	if invert > 1.0 {
		invert = 1.0
	}
	inverted := color.NRGBA{
		R: 255 - col.R,
		G: 255 - col.G,
		B: 255 - col.B,
		A: col.A,
	}
	// Blend original color with inverted
	return color.NRGBA{
		R: clampUint8(float64(col.R)*(1.0-invert) + float64(inverted.R)*invert),
		G: clampUint8(float64(col.G)*(1.0-invert) + float64(inverted.G)*invert),
		B: clampUint8(float64(col.B)*(1.0-invert) + float64(inverted.B)*invert),
		A: col.A,
	}
}
