package runtime

import (
	"strconv"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func splitSpaceOutsideParens(input string) []string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil
	}
	parts := make([]string, 0, 8)
	depth := 0
	start := -1
	for i, r := range input {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if start < 0 && !strings.ContainsRune(" \t\n\r", r) {
			start = i
		}
		if start >= 0 && depth == 0 && strings.ContainsRune(" \t\n\r", r) {
			part := strings.TrimSpace(input[start:i])
			if part != "" {
				parts = append(parts, part)
			}
			start = -1
		}
	}
	if start >= 0 {
		part := strings.TrimSpace(input[start:])
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func nodeStyleProps(node *uiNode) map[string]any {
	props := make(map[string]any, len(node.Props))
	for key, candidate := range node.Props {
		props[key] = uiValueToNative(candidate)
	}
	return props
}

func resolveNodeStyle(node *uiNode, ss *styleSheet, viewportW int) map[string]string {
	if node == nil {
		return map[string]string{}
	}
	return resolveStyle(nodeStyleProps(node), ss, viewportW)
}

func mergeInheritedTextCSS(css map[string]string, parent map[string]string) map[string]string {
	if css == nil {
		css = map[string]string{}
	}
	if parent == nil {
		return css
	}
	inheritable := []string{
		"color",
		"font-family",
		"font-size",
		"font-style",
		"font-weight",
		"line-height",
		"letter-spacing",
		"text-align",
		"text-transform",
		"white-space",
	}
	for _, k := range inheritable {
		v := strings.TrimSpace(css[k])
		if v == "" || strings.EqualFold(v, "inherit") {
			if pv := strings.TrimSpace(parent[k]); pv != "" {
				css[k] = pv
			}
		}
	}
	return css
}

func nodeLayoutString(node *uiNode, css map[string]string, propName string, cssName string, fallback string) string {
	if node != nil {
		if value := strings.TrimSpace(uiNodePropString(node, propName, "")); value != "" {
			return strings.ToLower(value)
		}
	}
	if css != nil {
		if value := strings.TrimSpace(css[cssName]); value != "" {
			return strings.ToLower(value)
		}
	}
	return fallback
}

func nodeLayoutLength(node *uiNode, css map[string]string, propName string, cssName string, basis int, viewportW int, viewportH int, fallback int) int {
	if node != nil && node.Props != nil {
		if candidate, ok := node.Props[propName]; ok {
			if candidate.Kind == value.Number {
				if candidate.NumberKind == value.NumberInt {
					return int(candidate.Int)
				}
				return int(candidate.Num)
			}
			if candidate.Kind == value.String || candidate.Kind == value.Char {
				return cssLengthValue(candidate.String(), fallback, basis, viewportW, viewportH)
			}
		}
	}
	if css != nil {
		if value := css[cssName]; value != "" {
			return cssLengthValue(value, fallback, basis, viewportW, viewportH)
		}
	}
	return fallback
}

func nodeLayoutFloat(node *uiNode, css map[string]string, propName string, cssName string, fallback float64) float64 {
	if node != nil && node.Props != nil {
		if candidate, ok := node.Props[propName]; ok {
			if candidate.Kind == value.Number {
				if candidate.NumberKind == value.NumberInt {
					return float64(candidate.Int)
				}
				return candidate.Num
			}
			if candidate.Kind == value.String || candidate.Kind == value.Char {
				return cssFloatValue(candidate.String(), fallback)
			}
		}
	}
	if css != nil {
		if value := css[cssName]; value != "" {
			return cssFloatValue(value, fallback)
		}
	}
	return fallback
}

func parseGridTrackSpec(spec string, total int, viewportW int, viewportH int) []int {
	trimmed := strings.TrimSpace(strings.ToLower(spec))
	if trimmed == "" || total <= 0 {
		return []int{max(1, total)}
	}
	if strings.HasPrefix(trimmed, "repeat(auto-fit,") || strings.HasPrefix(trimmed, "repeat(auto-fill,") {
		start := strings.Index(trimmed, "minmax(")
		end := strings.LastIndex(trimmed, "))")
		if start > 0 && end > start {
			inner := trimmed[start+len("minmax(") : end]
			parts := strings.SplitN(inner, ",", 2)
			if len(parts) == 2 {
				minTrack := cssLengthValue(strings.TrimSpace(parts[0]), 160, total, viewportW, viewportH)
				maxTrackRaw := strings.TrimSpace(parts[1])
				if minTrack <= 0 {
					minTrack = 160
				}
				count := max(1, total/max(1, minTrack))
				if count < 1 {
					count = 1
				}
				tracks := make([]int, count)
				if strings.HasSuffix(maxTrackRaw, "fr") {
					base := max(1, total/count)
					for i := range tracks {
						tracks[i] = max(minTrack, base)
					}
					return tracks
				}
				maxTrack := cssLengthValue(maxTrackRaw, minTrack, total, viewportW, viewportH)
				for i := range tracks {
					tracks[i] = max(minTrack, maxTrack)
				}
				return tracks
			}
		}
	}
	if strings.HasPrefix(trimmed, "repeat(") && strings.HasSuffix(trimmed, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "repeat("), ")")
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			count, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err == nil && count > 0 {
				unit := strings.TrimSpace(parts[1])
				items := make([]string, count)
				for i := 0; i < count; i++ {
					items[i] = unit
				}
				trimmed = strings.Join(items, " ")
			}
		}
	}
	tokens := splitSpaceOutsideParens(trimmed)
	if len(tokens) == 0 {
		return []int{max(1, total)}
	}
	tracks := make([]int, len(tokens))
	frIndexes := make([]int, 0, len(tokens))
	frMin := make(map[int]int)
	frTotal := 0.0
	fixed := 0
	for i, token := range tokens {
		if strings.HasPrefix(token, "minmax(") && strings.HasSuffix(token, ")") {
			inner := strings.TrimSuffix(strings.TrimPrefix(token, "minmax("), ")")
			parts := strings.SplitN(inner, ",", 2)
			if len(parts) == 2 {
				basis := total
				if basis <= 0 {
					basis = max(viewportW, viewportH)
				}
				minTrack := cssLengthValue(strings.TrimSpace(parts[0]), 1, basis, viewportW, viewportH)
				if minTrack < 1 {
					minTrack = 1
				}
				maxRaw := strings.TrimSpace(parts[1])
				if strings.HasSuffix(maxRaw, "fr") {
					f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(maxRaw, "fr")), 64)
					if err != nil || f <= 0 {
						f = 1
					}
					frIndexes = append(frIndexes, i)
					frTotal += f
					frMin[i] = minTrack
					continue
				}
				maxTrack := cssLengthValue(maxRaw, minTrack, basis, viewportW, viewportH)
				if maxTrack < minTrack {
					maxTrack = minTrack
				}
				tracks[i] = maxTrack
				fixed += maxTrack
				continue
			}
		}
		if strings.HasSuffix(token, "fr") {
			f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(token, "fr")), 64)
			if err != nil || f <= 0 {
				f = 1
			}
			frIndexes = append(frIndexes, i)
			frTotal += f
			continue
		}
		basis := total
		if basis <= 0 {
			basis = max(viewportW, viewportH)
		}
		v := cssLengthValue(token, 0, basis, viewportW, viewportH)
		if v <= 0 {
			v = 1
		}
		tracks[i] = v
		fixed += v
	}
	remaining := total - fixed
	if remaining < 0 {
		remaining = 0
	}
	if len(frIndexes) > 0 {
		if frTotal <= 0 {
			frTotal = float64(len(frIndexes))
		}
		for _, idx := range frIndexes {
			token := tokens[idx]
			f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(token, "fr")), 64)
			if err != nil || f <= 0 {
				f = 1
			}
			share := max(1, int((f/frTotal)*float64(remaining)))
			if minTrack, ok := frMin[idx]; ok {
				share = max(share, minTrack)
			}
			tracks[idx] = share
		}
	}
	return tracks
}

func cssGridSpan(value string) int {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return 1
	}
	if strings.HasPrefix(v, "span") {
		parts := strings.Fields(v)
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 {
				return n
			}
		}
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return 1
}
