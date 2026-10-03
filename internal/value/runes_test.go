package value

import (
	"strings"
	"testing"
)

func TestRuneHelpersMatchRuneSlice(t *testing.T) {
	cases := []string{"", "a", "hello world, this is ascii text", "héllo wörld ñ", "日本語テキスト", "mix日本ascii and ünï", strings.Repeat("abcdefgh", 9) + "é" + "xyz"}
	for _, s := range cases {
		rs := []rune(s)
		if got := RuneLen(s); got != len(rs) {
			t.Fatalf("RuneLen(%q) = %d, want %d", s, got, len(rs))
		}
		for i := -1; i <= len(rs); i++ {
			r, ok := RuneAt(s, i)
			if wantOK := i >= 0 && i < len(rs); ok != wantOK || (ok && r != rs[i]) {
				t.Fatalf("RuneAt(%q,%d) = %q,%v", s, i, r, ok)
			}
		}
		for a := -1; a <= len(rs)+1; a++ {
			for b := -1; b <= len(rs)+1; b++ {
				got, ok := RuneSlice(s, a, b)
				wantOK := !(a < 0 || b < 0 || a > len(rs) || b >= len(rs))
				if ok != wantOK {
					t.Fatalf("RuneSlice(%q,%d,%d) ok=%v want %v", s, a, b, ok, wantOK)
				}
				if ok {
					want := ""
					if a <= b {
						want = string(rs[a : b+1])
					}
					if got != want {
						t.Fatalf("RuneSlice(%q,%d,%d) = %q want %q", s, a, b, got, want)
					}
				}
			}
		}
	}
}
