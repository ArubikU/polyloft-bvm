package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/ArubikU/polyloft-bvm/internal/token"
)

type Lexer struct {
	source []rune
	start  int
	cur    int
	line   int
	col    int
	items  []token.Token
}

func Scan(source string) ([]token.Token, error) {
	lx := &Lexer{
		source: []rune(source),
		line:   1,
		col:    1,
	}
	if err := lx.scanTokens(); err != nil {
		return nil, err
	}
	return lx.items, nil
}

func (lx *Lexer) scanTokens() error {
	for !lx.isAtEnd() {
		lx.start = lx.cur
		startLine := lx.line
		startCol := lx.col
		r := lx.advance()
		switch r {
		case ' ', '\r', '\t':
			continue
		case '\n':
			lx.items = append(lx.items, token.Token{Type: token.Newline, Lexeme: "\\n", Line: startLine, Column: startCol})
		case '(':
			lx.addToken(token.LeftParen, startLine, startCol)
		case ')':
			lx.addToken(token.RightParen, startLine, startCol)
		case '[':
			lx.addToken(token.LeftBracket, startLine, startCol)
		case ']':
			lx.addToken(token.RightBracket, startLine, startCol)
		case '{':
			lx.addToken(token.LeftBrace, startLine, startCol)
		case '}':
			lx.addToken(token.RightBrace, startLine, startCol)
		case ',':
			lx.addToken(token.Comma, startLine, startCol)
		case ':':
			lx.addToken(token.Colon, startLine, startCol)
		case '.':
			if lx.peek() == '.' && lx.peekNext() == '.' {
				lx.advance()
				lx.advance()
				lx.addToken(token.Ellipsis, startLine, startCol)
			} else {
				lx.addToken(token.Dot, startLine, startCol)
			}
		case '?':
			lx.addToken(token.Question, startLine, startCol)
		case '@':
			lx.addToken(token.At, startLine, startCol)
		case '+':
			if lx.match('=') {
				lx.addToken(token.PlusEqual, startLine, startCol)
			} else {
				lx.addToken(token.Plus, startLine, startCol)
			}
		case '-':
			if lx.match('>') {
				lx.addToken(token.Arrow, startLine, startCol)
			} else if lx.match('=') {
				lx.addToken(token.MinusEqual, startLine, startCol)
			} else {
				lx.addToken(token.Minus, startLine, startCol)
			}
		case '*':
			if lx.match('*') {
				lx.addToken(token.StarStar, startLine, startCol)
			} else if lx.match('=') {
				lx.addToken(token.StarEqual, startLine, startCol)
			} else {
				lx.addToken(token.Star, startLine, startCol)
			}
		case '%':
			lx.addToken(token.Percent, startLine, startCol)
		case '^':
			lx.addToken(token.Caret, startLine, startCol)
		case '/':
			if lx.match('/') {
				for !lx.isAtEnd() && lx.peek() != '\n' {
					lx.advance()
				}
				continue
			}
			if lx.match('*') {
				for !lx.isAtEnd() {
					if lx.peek() == '*' && lx.peekNext() == '/' {
						lx.advance() // Consume *
						lx.advance() // Consume /
						break
					}
					lx.advance()
				}
				continue
			}
			if lx.match('=') {
				lx.addToken(token.SlashEqual, startLine, startCol)
			} else {
				lx.addToken(token.Slash, startLine, startCol)
			}
		case '!':
			if lx.match('=') {
				lx.addToken(token.BangEqual, startLine, startCol)
			} else {
				lx.addToken(token.Bang, startLine, startCol)
			}
		case '&':
			if lx.match('&') {
				lx.addToken(token.AndAnd, startLine, startCol)
			} else {
				lx.addToken(token.Ampersand, startLine, startCol)
			}
		case '|':
			if lx.match('|') {
				lx.addToken(token.OrOr, startLine, startCol)
			} else {
				lx.addToken(token.Pipe, startLine, startCol)
			}
		case '=':
			if lx.match('>') {
				lx.addToken(token.FatArrow, startLine, startCol)
			} else if lx.match('=') {
				lx.addToken(token.EqualEqual, startLine, startCol)
			} else {
				lx.addToken(token.Equal, startLine, startCol)
			}
		case '>':
			if lx.match('=') {
				lx.addToken(token.GreaterEqual, startLine, startCol)
			} else {
				lx.addToken(token.Greater, startLine, startCol)
			}
		case '<':
			if lx.match('=') {
				lx.addToken(token.LessEqual, startLine, startCol)
			} else {
				lx.addToken(token.Less, startLine, startCol)
			}
		case '"':
			if err := lx.scanString(startLine, startCol); err != nil {
				return err
			}
		case '\'':
			if err := lx.scanChar(startLine, startCol); err != nil {
				return err
			}
		default:
			if unicode.IsDigit(r) {
				if err := lx.scanNumber(startLine, startCol); err != nil {
					return err
				}
				continue
			}
			if unicode.IsLetter(r) || r == '_' {
				lx.scanIdentifier(startLine, startCol)
				continue
			}
			return fmt.Errorf("line %d:%d: unexpected character %q", startLine, startCol, r)
		}
	}

	lx.items = append(lx.items, token.Token{Type: token.EOF, Lexeme: "", Line: lx.line, Column: lx.col})
	return nil
}

func (lx *Lexer) scanString(line, col int) error {
	// Track brace depth so we can allow `"` inside `#{ }` interpolation blocks.
	interpDepth := 0 // number of unclosed interpolation braces we're inside
	for !lx.isAtEnd() {
		ch := lx.peek()
		// Inside an interpolation block, count { and } to track depth.
		if interpDepth > 0 {
			if ch == '{' {
				interpDepth++
				lx.advance()
				continue
			}
			if ch == '}' {
				interpDepth--
				lx.advance()
				continue
			}
			// While inside #{}, allow any character including '"' — just advance.
			lx.advance()
			continue
		}
		// Outside interpolation blocks:
		if ch == '"' {
			break // end of string
		}
		if ch == '\n' {
			return fmt.Errorf("line %d:%d: unterminated string", line, col)
		}
		// Detect start of `#{` interpolation block.
		if ch == '#' && lx.peekNext() == '{' {
			lx.advance() // consume '#'
			lx.advance() // consume '{'
			interpDepth++
			continue
		}
		// Escaped quote
		if ch == '\\' && lx.peekNext() == '"' {
			lx.advance()
			lx.advance()
			continue
		}
		lx.advance()
	}
	if lx.isAtEnd() {
		return fmt.Errorf("line %d:%d: unterminated string", line, col)
	}
	lx.advance() // consume closing '"'

	raw := string(lx.source[lx.start+1 : lx.cur-1])

	// Unescape escape sequences (but leave #{...} untouched for the VM).
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			switch raw[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteByte('\\')
				b.WriteByte(raw[i])
			}
		} else {
			b.WriteByte(raw[i])
		}
	}

	lx.items = append(lx.items, token.Token{Type: token.String, Lexeme: b.String(), Line: line, Column: col})
	return nil
}

func (lx *Lexer) scanChar(line, col int) error {
	if lx.isAtEnd() || lx.peek() == '\n' {
		return fmt.Errorf("line %d:%d: unterminated char", line, col)
	}
	var value rune
	if lx.peek() == '\\' {
		lx.advance()
		if lx.isAtEnd() {
			return fmt.Errorf("line %d:%d: unterminated char", line, col)
		}
		escaped := lx.advance()
		switch escaped {
		case 'n':
			value = '\n'
		case 'r':
			value = '\r'
		case 't':
			value = '\t'
		case 'b':
			value = '\b'
		case 'f':
			value = '\f'
		case '\\':
			value = '\\'
		case '\'':
			value = '\''
		case '"':
			value = '"'
		case '0':
			value = '\x00'
		default:
			return fmt.Errorf("line %d:%d: unsupported char escape \\%c", line, col, escaped)
		}
	} else {
		value = lx.advance()
	}
	if lx.isAtEnd() || lx.peek() != '\'' {
		return fmt.Errorf("line %d:%d: char literal must contain exactly one character", line, col)
	}
	lx.advance()
	lx.items = append(lx.items, token.Token{Type: token.Char, Lexeme: string(value), Line: line, Column: col})
	return nil
}

func (lx *Lexer) scanNumber(line, col int) error {
	isFloat := false
	if err := lx.consumeNumberDigits(line, col, false); err != nil {
		return err
	}
	if lx.peek() == '.' && isNumberDigitStart(lx.peekNext()) {
		isFloat = true
		lx.advance()
		if err := lx.consumeNumberDigits(line, col, true); err != nil {
			return err
		}
	}
	literal := string(lx.source[lx.start:lx.cur])
	normalized := normalizeNumberLiteral(literal)
	if _, err := strconv.ParseFloat(normalized, 64); err != nil {
		return fmt.Errorf("line %d:%d: invalid number %q", line, col, literal)
	}
	tokenType := token.IntNumber
	if isFloat {
		tokenType = token.FloatNumber
	}
	lx.items = append(lx.items, token.Token{Type: tokenType, Lexeme: literal, Line: line, Column: col})
	return nil
}

func (lx *Lexer) consumeNumberDigits(line, col int, requireLeadingDigit bool) error {
	if requireLeadingDigit && !unicode.IsDigit(lx.peek()) {
		return fmt.Errorf("line %d:%d: invalid number %q", line, col, string(lx.source[lx.start:lx.cur]))
	}
	for {
		if unicode.IsDigit(lx.peek()) {
			lx.advance()
			continue
		}
		if lx.peek() == '_' {
			if !unicode.IsDigit(lx.peekNext()) {
				literal := string(lx.source[lx.start : lx.cur+1])
				return fmt.Errorf("line %d:%d: invalid number %q", line, col, literal)
			}
			lx.advance()
			continue
		}
		break
	}
	if lx.source[lx.cur-1] == '_' {
		literal := string(lx.source[lx.start:lx.cur])
		return fmt.Errorf("line %d:%d: invalid number %q", line, col, literal)
	}
	return nil
}

func isNumberDigitStart(ch rune) bool {
	return unicode.IsDigit(ch)
}

func normalizeNumberLiteral(literal string) string {
	return strings.ReplaceAll(literal, "_", "")
}

func (lx *Lexer) scanIdentifier(line, col int) {
	for unicode.IsLetter(lx.peek()) || unicode.IsDigit(lx.peek()) || lx.peek() == '_' {
		lx.advance()
	}
	literal := string(lx.source[lx.start:lx.cur])
	lx.items = append(lx.items, token.Token{Type: token.LookupIdentifier(literal), Lexeme: literal, Line: line, Column: col})
}

func (lx *Lexer) addToken(kind token.Type, line, col int) {
	lx.items = append(lx.items, token.Token{Type: kind, Lexeme: string(lx.source[lx.start:lx.cur]), Line: line, Column: col})
}

func (lx *Lexer) match(expected rune) bool {
	if lx.isAtEnd() || lx.source[lx.cur] != expected {
		return false
	}
	lx.cur++
	lx.col++
	return true
}

func (lx *Lexer) peek() rune {
	if lx.isAtEnd() {
		return 0
	}
	return lx.source[lx.cur]
}

func (lx *Lexer) peekNext() rune {
	if lx.cur+1 >= len(lx.source) {
		return 0
	}
	return lx.source[lx.cur+1]
}

func (lx *Lexer) advance() rune {
	r := lx.source[lx.cur]
	lx.cur++
	if r == '\n' {
		lx.line++
		lx.col = 1
	} else {
		lx.col++
	}
	return r
}

func (lx *Lexer) isAtEnd() bool {
	return lx.cur >= len(lx.source)
}
