package runtime

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// styleSheet stores CSS-like class rules.
// Supports both .css file loading and programmatic rule setting via setRule().
//
// Two ways to use from Polyloft script:
//
//	// Method 1 – CSS text / file
//	var ss = UI.stylesheet()
//	ss.loadFile("styles.css")        // .title { color: #fff; font-size: 20px }
//	app.attachStylesheet(ss)
//
//	// Method 2 – programmatic
//	var ss = UI.stylesheet()
//	ss.set(".title", "color", "#e8e8e8")
//	ss.set(".title", "text-align", "center")
//	app.attachStylesheet(ss)
//
//	// Applying to a node
//	title.setClass("title")           // or: title.set("class", "title header")
//	title.style("font-size", "24px")  // inline override (highest priority)
type styleSheet struct {
	mu      sync.RWMutex
	classes map[string]map[string]string // normalizedClassName → property → value
}

func newStyleSheet() *styleSheet {
	return &styleSheet{classes: make(map[string]map[string]string)}
}

// normalizeCSSClass strips a leading dot and trims whitespace.
func normalizeCSSClass(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "."))
}

// setRule stores a CSS declaration for className.
// className may optionally start with ".".
func (ss *styleSheet) setRule(className, property, val string) {
	cls := normalizeCSSClass(className)
	if cls == "" {
		return
	}
	prop := strings.ToLower(strings.TrimSpace(property))
	v := strings.TrimSpace(val)
	if prop == "" {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.classes[cls] == nil {
		ss.classes[cls] = make(map[string]string, 4)
	}
	ss.classes[cls][prop] = v
}

// getRule retrieves a single CSS property for a class.
func (ss *styleSheet) getRule(className, property string) (string, bool) {
	cls := normalizeCSSClass(className)
	prop := strings.ToLower(strings.TrimSpace(property))
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if m, ok := ss.classes[cls]; ok {
		if v, ok2 := m[prop]; ok2 {
			return v, true
		}
	}
	return "", false
}

// getClassProps returns a snapshot copy of all declarations for className.
func (ss *styleSheet) getClassProps(className string) map[string]string {
	cls := normalizeCSSClass(className)
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if m, ok := ss.classes[cls]; ok {
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return nil
}

// parseCSSText parses a CSS string into the stylesheet and returns the number
// of declarations successfully loaded.
//
// Supported syntax:
//
//	.classname { property: value; property: value }
//	.a, .b   { property: value }
//	/* block comments */
//	// line comments
func (ss *styleSheet) parseCSSText(text string) int {
	// Strip /* ... */ block comments.
	for {
		si := strings.Index(text, "/*")
		if si < 0 {
			break
		}
		ei := strings.Index(text[si:], "*/")
		if ei < 0 {
			text = text[:si]
			break
		}
		text = text[:si] + " " + text[si+ei+2:]
	}
	// Strip // line comments.
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if ci := strings.Index(l, "//"); ci >= 0 {
			lines[i] = l[:ci]
		}
	}
	text = strings.Join(lines, "\n")

	count := 0
	rest := text
	for {
		dotIdx := strings.IndexByte(rest, '.')
		if dotIdx < 0 {
			break
		}
		tail := rest[dotIdx:]
		openIdx := strings.IndexByte(tail, '{')
		if openIdx < 0 {
			break
		}
		closeIdx := strings.IndexByte(tail[openIdx:], '}')
		if closeIdx < 0 {
			break
		}
		selectorRaw := tail[:openIdx]
		body := tail[openIdx+1 : openIdx+closeIdx]
		rest = tail[openIdx+closeIdx+1:]

		// Support comma-separated selectors: .a, .b { ... }
		for _, sel := range strings.Split(selectorRaw, ",") {
			cls := normalizeCSSClass(sel)
			if cls == "" {
				continue
			}
			for _, decl := range strings.Split(body, ";") {
				parts := strings.SplitN(strings.TrimSpace(decl), ":", 2)
				if len(parts) == 2 {
					prop := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					if prop != "" {
						ss.setRule(cls, prop, val)
						count++
					}
				}
			}
		}
	}
	return count
}

// loadFile reads a CSS file from disk and parses it.
func (ss *styleSheet) loadFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return ss.parseCSSText(string(data)), nil
}

// ─── Style resolution ─────────────────────────────────────────────────────────

// resolveStyle builds a flat map of canonical CSS property → value string from:
//  1. Class-level rules (left-to-right from the "class" prop, later overrides earlier).
//  2. Inline style.* node props (highest priority, override class rules).
//
// The returned map uses standard CSS property names:
// "color", "background-color", "background", "font-size", "font-weight",
// "font-style", "text-align", "padding", "padding-top", etc.
func resolveStyle(props map[string]any, appSS *styleSheet) map[string]string {
	resolved := make(map[string]string, 12)

	// 1. Class styles (e.g. node.set("class", "title hero"))
	if appSS != nil {
		classStr := anyToString(props["class"], "")
		if classStr != "" {
			for _, cls := range strings.Fields(strings.ReplaceAll(classStr, ",", " ")) {
				cls = strings.TrimPrefix(strings.TrimSpace(cls), ".")
				if cls == "" {
					continue
				}
				if m := appSS.getClassProps(cls); m != nil {
					for k, v := range m {
						resolved[k] = v
					}
				}
			}
		}
	}

	// 2. Inline style.* overrides (e.g. node.style("color", "#fff"))
	for k, v := range props {
		if !strings.HasPrefix(k, "style.") {
			continue
		}
		prop := strings.ToLower(strings.TrimPrefix(k, "style."))
		if s := anyToString(v, ""); s != "" {
			resolved[prop] = s
		}
	}

	return resolved
}

// ─── CSS property accessors ───────────────────────────────────────────────────

// cssGetColor returns the CSS property value for prop, or fallback if absent/empty.
func cssGetColor(css map[string]string, prop, fallback string) string {
	if v := css[prop]; v != "" {
		return v
	}
	return fallback
}

// cssBackground returns the background color from "background-color" or "background".
func cssBackground(css map[string]string) string {
	if v := css["background-color"]; v != "" {
		return v
	}
	return css["background"]
}

// cssFontSize parses "font-size" from resolved map (supports px suffix).
func cssFontSize(css map[string]string, fallback float32) float32 {
	v := css["font-size"]
	if v == "" {
		return fallback
	}
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(v), "px"), "em")
	if f, err := strconv.ParseFloat(s, 32); err == nil && f > 0 {
		return float32(f)
	}
	return fallback
}

// cssLength parses a CSS length property (bare number or px/em suffix) to int.
func cssLength(css map[string]string, prop string, fallback int) int {
	v := css[prop]
	if v == "" {
		return fallback
	}
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(v), "px"), "em")
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f)
	}
	return fallback
}

// cssBold returns true when font-weight indicates bold.
func cssBold(css map[string]string) bool {
	v := strings.ToLower(strings.TrimSpace(css["font-weight"]))
	return v == "bold" || v == "700" || v == "800" || v == "900" || v == "bolder"
}

// cssItalic returns true when font-style indicates italic/oblique.
func cssItalic(css map[string]string) bool {
	v := strings.ToLower(strings.TrimSpace(css["font-style"]))
	return strings.Contains(v, "italic") || strings.Contains(v, "oblique")
}
