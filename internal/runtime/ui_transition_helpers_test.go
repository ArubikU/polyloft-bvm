package runtime

import (
	"testing"
)

func TestParseTransformTranslate(t *testing.T) {
	tests := []struct {
		name  string
		css   map[string]string
		wantX float64
		wantY float64
	}{
		{
			name:  "translate with two values",
			css:   map[string]string{"transform": "translate(10px, 20px)"},
			wantX: 10,
			wantY: 20,
		},
		{
			name:  "translate with one value",
			css:   map[string]string{"transform": "translate(15px)"},
			wantX: 15,
			wantY: 0,
		},
		{
			name:  "translateX only",
			css:   map[string]string{"transform": "translateX(25px)"},
			wantX: 25,
			wantY: 0,
		},
		{
			name:  "translateY only",
			css:   map[string]string{"transform": "translateY(30px)"},
			wantX: 0,
			wantY: 30,
		},
		{
			name:  "translateX and translateY combined",
			css:   map[string]string{"transform": "translateX(10px) translateY(20px)"},
			wantX: 10,
			wantY: 20,
		},
		{
			name:  "negative values",
			css:   map[string]string{"transform": "translate(-5px, -10px)"},
			wantX: -5,
			wantY: -10,
		},
		{
			name:  "empty transform",
			css:   map[string]string{"transform": ""},
			wantX: 0,
			wantY: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := parseTransformTranslate(tt.css)
			if x != tt.wantX || y != tt.wantY {
				t.Errorf("parseTransformTranslate() = (%v, %v), want (%v, %v)", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestParseFilterBrightness(t *testing.T) {
	tests := []struct {
		name string
		css  map[string]string
		want float64
	}{
		{
			name: "brightness decimal",
			css:  map[string]string{"filter": "brightness(1.5)"},
			want: 1.5,
		},
		{
			name: "brightness percentage",
			css:  map[string]string{"filter": "brightness(150%)"},
			want: 1.5,
		},
		{
			name: "brightness 100%",
			css:  map[string]string{"filter": "brightness(100%)"},
			want: 1.0,
		},
		{
			name: "brightness 50%",
			css:  map[string]string{"filter": "brightness(50%)"},
			want: 0.5,
		},
		{
			name: "brightness 0.8",
			css:  map[string]string{"filter": "brightness(0.8)"},
			want: 0.8,
		},
		{
			name: "empty filter",
			css:  map[string]string{"filter": ""},
			want: 1.0,
		},
		{
			name: "no brightness in filter",
			css:  map[string]string{"filter": "blur(5px)"},
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFilterBrightness(tt.css)
			if got != tt.want {
				t.Errorf("parseFilterBrightness() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCssScale(t *testing.T) {
	tests := []struct {
		name string
		css  map[string]string
		want float64
	}{
		{
			name: "scale with decimal",
			css:  map[string]string{"transform": "scale(0.75)"},
			want: 0.75,
		},
		{
			name: "scale 1.0",
			css:  map[string]string{"transform": "scale(1.0)"},
			want: 1.0,
		},
		{
			name: "scale 1.25",
			css:  map[string]string{"transform": "scale(1.25)"},
			want: 1.25,
		},
		{
			name: "scale integer",
			css:  map[string]string{"transform": "scale(2)"},
			want: 2.0,
		},
		{
			name: "scale property directly",
			css:  map[string]string{"scale": "0.5"},
			want: 0.5,
		},
		{
			name: "empty transform",
			css:  map[string]string{"transform": ""},
			want: 1.0,
		},
		{
			name: "no scale in transform",
			css:  map[string]string{"transform": "translate(10px, 20px)"},
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cssScale(tt.css)
			if got != tt.want {
				t.Errorf("cssScale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParsePositionAndOffset(t *testing.T) {
	tests := []struct {
		name      string
		css       map[string]string
		wantPos   string
		wantLeft  bool
		wantRight bool
		wantTop   bool
		wantBot   bool
		wantX     float64
		wantY     float64
	}{
		{
			name:     "absolute left top",
			css:      map[string]string{"position": "absolute", "left": "10px", "top": "20px"},
			wantPos:  "absolute",
			wantLeft: true,
			wantTop:  true,
			wantX:    10,
			wantY:    20,
		},
		{
			name:      "fixed right bottom",
			css:       map[string]string{"position": "fixed", "right": "12px", "bottom": "8px"},
			wantPos:   "fixed",
			wantRight: true,
			wantBot:   true,
			wantX:     12,
			wantY:     8,
		},
		{
			name:    "static ignores offsets",
			css:     map[string]string{"position": "static", "left": "99px", "top": "99px"},
			wantPos: "static",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePositionAndOffset(tt.css, 100, 100)
			if got.position != tt.wantPos {
				t.Fatalf("position = %q, want %q", got.position, tt.wantPos)
			}
			if got.hasLeft != tt.wantLeft || got.hasRight != tt.wantRight || got.hasTop != tt.wantTop || got.hasBottom != tt.wantBot {
				t.Fatalf("flags = left:%v right:%v top:%v bottom:%v", got.hasLeft, got.hasRight, got.hasTop, got.hasBottom)
			}
			if got.offsetX != tt.wantX || got.offsetY != tt.wantY {
				t.Fatalf("offsets = (%v,%v), want (%v,%v)", got.offsetX, got.offsetY, tt.wantX, tt.wantY)
			}
		})
	}
}
