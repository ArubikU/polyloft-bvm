package runtime

import (
	"testing"

	"gioui.org/io/pointer"
)

func TestCssCursorToGio(t *testing.T) {
	tests := []struct {
		in   string
		want pointer.Cursor
	}{
		{in: "", want: pointer.CursorDefault},
		{in: "default", want: pointer.CursorDefault},
		{in: "pointer", want: pointer.CursorPointer},
		{in: "text", want: pointer.CursorText},
		{in: "move", want: pointer.CursorAllScroll},
		{in: "col-resize", want: pointer.CursorColResize},
		{in: "row-resize", want: pointer.CursorRowResize},
		{in: "not-allowed", want: pointer.CursorNotAllowed},
	}

	for _, tt := range tests {
		if got := cssCursorToGio(tt.in); got != tt.want {
			t.Fatalf("cssCursorToGio(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
