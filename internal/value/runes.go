package value

import (
	"unicode/utf8"
	"unsafe"
)

// asciiPrefixLen returns the length of the longest all-ASCII prefix of s. It
// checks eight bytes per step, which makes ASCII-only strings (the common case)
// cheap to classify without allocating or decoding.
func asciiPrefixLen(s string) int {
	n := len(s)
	i := 0
	if n >= 8 {
		p := unsafe.Pointer(unsafe.StringData(s))
		for i+8 <= n {
			if *(*uint64)(unsafe.Add(p, i))&0x8080808080808080 != 0 {
				break
			}
			i += 8
		}
	}
	for i < n && s[i] < utf8.RuneSelf {
		i++
	}
	return i
}

// RuneLen returns the number of runes in s (the language-level length of a
// string) without converting it to []rune; ASCII prefixes are counted by length.
func RuneLen(s string) int {
	p := asciiPrefixLen(s)
	if p == len(s) {
		return p
	}
	return p + utf8.RuneCountInString(s[p:])
}

// RuneAt returns the idx-th rune of s, or false when idx is out of range.
func RuneAt(s string, idx int) (rune, bool) {
	if idx < 0 {
		return 0, false
	}
	if idx < len(s) && asciiPrefixLen(s[:idx+1]) == idx+1 {
		return rune(s[idx]), true
	}
	i := 0
	for _, r := range s {
		if i == idx {
			return r, true
		}
		i++
	}
	return 0, false
}

// RuneSlice returns runes [start, end] (inclusive) of s as a string using the
// language's slice bounds rules: it reports ok=false for out-of-range bounds and
// returns "" for start > end.
func RuneSlice(s string, start, end int) (string, bool) {
	n := RuneLen(s)
	if start < 0 || end < 0 || start > n || end >= n {
		return "", false
	}
	if start > end {
		return "", true
	}
	if asciiPrefixLen(s) == len(s) {
		return s[start : end+1], true
	}
	return string([]rune(s)[start : end+1]), true
}
