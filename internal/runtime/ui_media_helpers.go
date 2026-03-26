package runtime

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	remoteImageCacheMu sync.RWMutex
	remoteImageCache   = map[string]image.Image{}
)

func loadRasterImage(path string) (image.Image, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(path)), "http://") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(path)), "https://") {
		remoteImageCacheMu.RLock()
		cached, ok := remoteImageCache[path]
		remoteImageCacheMu.RUnlock()
		if ok && cached != nil {
			return cached, nil
		}

		client := &http.Client{Timeout: 8 * time.Second}
		resp, err := client.Get(path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("image request failed: %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		remoteImageCacheMu.Lock()
		if len(remoteImageCache) > 256 {
			remoteImageCache = map[string]image.Image{}
		}
		remoteImageCache[path] = img
		remoteImageCacheMu.Unlock()
		return img, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func applyImageFilters(img image.Image, filter string, css map[string]string, width int, height int) image.Image {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	params := parseCSSFilterChain(filter)
	radii := cssBorderRadiiValues(css, width, height)
	if cssOpacity := strings.TrimSpace(css["opacity"]); cssOpacity != "" {
		if f, err := strconv.ParseFloat(cssOpacity, 64); err == nil {
			if f >= 0 && f <= 1 {
				params.opacity *= f
			}
		}
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			r := float64(uint8(r16 >> 8))
			g := float64(uint8(g16 >> 8))
			b := float64(uint8(b16 >> 8))
			a := float64(uint8(a16 >> 8))

			if params.grayscale > 0 {
				gray := 0.2126*r + 0.7152*g + 0.0722*b
				r = r*(1.0-params.grayscale) + gray*params.grayscale
				g = g*(1.0-params.grayscale) + gray*params.grayscale
				b = b*(1.0-params.grayscale) + gray*params.grayscale
			}
			if params.sepia > 0 {
				aSep := params.sepia
				ir := 1.0 - aSep
				r2 := r*(ir+0.393*aSep) + g*(0.769*aSep) + b*(0.189*aSep)
				g2 := r*(0.349*aSep) + g*(ir+0.686*aSep) + b*(0.168*aSep)
				b2 := r*(0.272*aSep) + g*(0.534*aSep) + b*(ir+0.131*aSep)
				r, g, b = r2, g2, b2
			}
			if params.saturate != 1.0 {
				s := params.saturate
				r2 := (0.213+0.787*s)*r + (0.715-0.715*s)*g + (0.072-0.072*s)*b
				g2 := (0.213-0.213*s)*r + (0.715+0.285*s)*g + (0.072-0.072*s)*b
				b2 := (0.213-0.213*s)*r + (0.715-0.715*s)*g + (0.072+0.928*s)*b
				r, g, b = r2, g2, b2
			}
			if params.hueRotateDeg != 0 {
				r, g, b = applyHueRotateRGB(r, g, b, params.hueRotateDeg)
			}
			if params.invert > 0 {
				r = r*(1.0-params.invert) + (255.0-r)*params.invert
				g = g*(1.0-params.invert) + (255.0-g)*params.invert
				b = b*(1.0-params.invert) + (255.0-b)*params.invert
			}
			r = clamp255(((r-128)*params.contrast)+128) * params.brightness
			g = clamp255(((g-128)*params.contrast)+128) * params.brightness
			b = clamp255(((b-128)*params.contrast)+128) * params.brightness
			a = clamp255(a * params.opacity)

			// Radius mask is applied after filter passes (including blur).

			out.SetNRGBA(x, y, color.NRGBA{R: uint8(clamp255(r)), G: uint8(clamp255(g)), B: uint8(clamp255(b)), A: uint8(clamp255(a))})
		}
	}
	if params.blurPx > 0 {
		out = boxBlurNRGBA(out, params.blurPx)
	}
	if radii.max() > 0 {
		applyRadiiMask(out, radii)
	}
	return out
}

type cssFilterParams struct {
	grayscale    float64
	invert       float64
	sepia        float64
	saturate     float64
	brightness   float64
	contrast     float64
	opacity      float64
	hueRotateDeg float64
	blurPx       int
}

func parseCSSFilterChain(filter string) cssFilterParams {
	p := cssFilterParams{
		saturate:   1.0,
		brightness: 1.0,
		contrast:   1.0,
		opacity:    1.0,
	}
	chain := strings.TrimSpace(filter)
	if chain == "" {
		return p
	}
	parts := splitFilterFunctions(chain)
	for _, fn := range parts {
		lower := strings.ToLower(strings.TrimSpace(fn))
		s := strings.Index(lower, "(")
		e := strings.LastIndex(lower, ")")
		if s < 0 || e <= s {
			continue
		}
		name := strings.TrimSpace(lower[:s])
		arg := strings.TrimSpace(lower[s+1 : e])
		switch name {
		case "grayscale":
			p.grayscale = clamp01(parseFilterAmount(arg, 1.0))
		case "invert":
			p.invert = clamp01(parseFilterAmount(arg, 1.0))
		case "sepia":
			p.sepia = clamp01(parseFilterAmount(arg, 1.0))
		case "saturate":
			p.saturate = maxFloat(0.0, parseFilterAmount(arg, 1.0))
		case "brightness":
			p.brightness = maxFloat(0.0, parseFilterAmount(arg, 1.0))
		case "contrast":
			p.contrast = maxFloat(0.0, parseFilterAmount(arg, 1.0))
		case "opacity":
			p.opacity = clamp01(parseFilterAmount(arg, 1.0))
		case "hue-rotate":
			p.hueRotateDeg = parseAngleToDeg(arg)
		case "blur":
			px := parseLengthPx(arg)
			if px > 0 {
				p.blurPx = px
			}
		}
	}
	return p
}

func splitFilterFunctions(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	out := make([]string, 0, 8)
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
		case ' ', '\t', '\n':
			if depth == 0 {
				part := strings.TrimSpace(input[start:i])
				if part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	last := strings.TrimSpace(input[start:])
	if last != "" {
		out = append(out, last)
	}
	return out
}

func parseFilterAmount(raw string, fallback float64) float64 {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fallback
	}
	if strings.HasSuffix(v, "%") {
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "%")), 64)
		if err != nil {
			return fallback
		}
		return f / 100.0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func parseLengthPx(raw string) int {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0
	}
	if strings.HasSuffix(v, "px") {
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "px")), 64)
		if err == nil {
			return max(0, int(math.Round(f)))
		}
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return max(0, int(math.Round(f)))
}

func parseAngleToDeg(raw string) float64 {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return 0
	}
	if strings.HasSuffix(v, "deg") {
		f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "deg")), 64)
		return f
	}
	if strings.HasSuffix(v, "grad") {
		f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "grad")), 64)
		return f * 0.9
	}
	if strings.HasSuffix(v, "turn") {
		f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "turn")), 64)
		return f * 360.0
	}
	if strings.HasSuffix(v, "rad") {
		f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "rad")), 64)
		return f * 180.0 / math.Pi
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func applyHueRotateRGB(r, g, b, deg float64) (float64, float64, float64) {
	rad := deg * math.Pi / 180.0
	cosA := math.Cos(rad)
	sinA := math.Sin(rad)
	r2 := (0.213+cosA*0.787-sinA*0.213)*r + (0.715-cosA*0.715-sinA*0.715)*g + (0.072-cosA*0.072+sinA*0.928)*b
	g2 := (0.213-cosA*0.213+sinA*0.143)*r + (0.715+cosA*0.285+sinA*0.140)*g + (0.072-cosA*0.072-sinA*0.283)*b
	b2 := (0.213-cosA*0.213-sinA*0.787)*r + (0.715-cosA*0.715+sinA*0.715)*g + (0.072+cosA*0.928+sinA*0.072)*b
	return r2, g2, b2
}

func boxBlurNRGBA(src *image.NRGBA, radius int) *image.NRGBA {
	if src == nil || radius <= 0 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 1 || h <= 1 {
		return src
	}
	radius = min(radius, 24)
	tmp := image.NewNRGBA(b)
	out := image.NewNRGBA(b)
	// Horizontal pass.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa, c int
			for k := -radius; k <= radius; k++ {
				xx := x + k
				if xx < 0 {
					xx = 0
				} else if xx >= w {
					xx = w - 1
				}
				off := src.PixOffset(xx+b.Min.X, y+b.Min.Y)
				sr += int(src.Pix[off+0])
				sg += int(src.Pix[off+1])
				sb += int(src.Pix[off+2])
				sa += int(src.Pix[off+3])
				c++
			}
			off := tmp.PixOffset(x+b.Min.X, y+b.Min.Y)
			tmp.Pix[off+0] = uint8(sr / c)
			tmp.Pix[off+1] = uint8(sg / c)
			tmp.Pix[off+2] = uint8(sb / c)
			tmp.Pix[off+3] = uint8(sa / c)
		}
	}
	// Vertical pass.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa, c int
			for k := -radius; k <= radius; k++ {
				yy := y + k
				if yy < 0 {
					yy = 0
				} else if yy >= h {
					yy = h - 1
				}
				off := tmp.PixOffset(x+b.Min.X, yy+b.Min.Y)
				sr += int(tmp.Pix[off+0])
				sg += int(tmp.Pix[off+1])
				sb += int(tmp.Pix[off+2])
				sa += int(tmp.Pix[off+3])
				c++
			}
			off := out.PixOffset(x+b.Min.X, y+b.Min.Y)
			out.Pix[off+0] = uint8(sr / c)
			out.Pix[off+1] = uint8(sg / c)
			out.Pix[off+2] = uint8(sb / c)
			out.Pix[off+3] = uint8(sa / c)
		}
	}
	return out
}

func applyRadiiMask(img *image.NRGBA, radii cornerRadii) {
	if img == nil || radii.max() <= 0 {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if roundedCornerAlphaCorners(x, y, w, h, radii) == 0 {
				off := img.PixOffset(x+b.Min.X, y+b.Min.Y)
				img.Pix[off+3] = 0
			}
		}
	}
}

func roundedCornerAlphaCorners(x int, y int, width int, height int, radii cornerRadii) uint8 {
	if width <= 0 || height <= 0 || radii.max() <= 0 {
		return 255
	}
	// Top-left
	if radii.nw > 0 && x < radii.nw && y < radii.nw {
		cx := float64(radii.nw - 1)
		cy := float64(radii.nw - 1)
		dx := float64(x) - cx
		dy := float64(y) - cy
		if dx*dx+dy*dy > float64(radii.nw*radii.nw) {
			return 0
		}
	}
	// Top-right
	if radii.ne > 0 && x >= width-radii.ne && y < radii.ne {
		cx := float64(width - radii.ne)
		cy := float64(radii.ne - 1)
		dx := float64(x) - cx
		dy := float64(y) - cy
		if dx*dx+dy*dy > float64(radii.ne*radii.ne) {
			return 0
		}
	}
	// Bottom-right
	if radii.se > 0 && x >= width-radii.se && y >= height-radii.se {
		cx := float64(width - radii.se)
		cy := float64(height - radii.se)
		dx := float64(x) - cx
		dy := float64(y) - cy
		if dx*dx+dy*dy > float64(radii.se*radii.se) {
			return 0
		}
	}
	// Bottom-left
	if radii.sw > 0 && x < radii.sw && y >= height-radii.sw {
		cx := float64(radii.sw - 1)
		cy := float64(height - radii.sw)
		dx := float64(x) - cx
		dy := float64(y) - cy
		if dx*dx+dy*dy > float64(radii.sw*radii.sw) {
			return 0
		}
	}
	return 255
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func parseFilterPercent(raw string) float64 {
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end <= start {
		return 1.0
	}
	v := strings.TrimSpace(raw[start+1 : end])
	if strings.HasSuffix(v, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		if err == nil {
			return f / 100.0
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 1.0
	}
	return f
}

func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func roundedCornerAlpha(x int, y int, width int, height int, radius int) uint8 {
	if radius <= 0 {
		return 255
	}
	left := x < radius
	right := x >= width-radius
	top := y < radius
	bottom := y >= height-radius
	if !(left || right) || !(top || bottom) {
		return 255
	}
	cx := float64(radius)
	cy := float64(radius)
	px := float64(x)
	py := float64(y)
	if right {
		cx = float64(width - radius - 1)
	}
	if bottom {
		cy = float64(height - radius - 1)
	}
	dx := px - cx
	dy := py - cy
	if math.Sqrt(dx*dx+dy*dy) > float64(radius) {
		return 0
	}
	return 255
}
