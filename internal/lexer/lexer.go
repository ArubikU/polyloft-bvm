package lexer

import (
	"fmt"
	"strconv"
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
		case ',':
			lx.addToken(token.Comma, startLine, startCol)
		case ':':
			lx.addToken(token.Colon, startLine, startCol)
		case '.':
			lx.addToken(token.Dot, startLine, startCol)
		case '@':
			lx.addToken(token.At, startLine, startCol)
		case '+':
			lx.addToken(token.Plus, startLine, startCol)
		case '-':
			if lx.match('>') {
				lx.addToken(token.Arrow, startLine, startCol)
			} else {
				lx.addToken(token.Minus, startLine, startCol)
			}
		case '*':
			lx.addToken(token.Star, startLine, startCol)
		case '/':
			if lx.match('/') {
				for !lx.isAtEnd() && lx.peek() != '\n' {
					lx.advance()
				}
				continue
			}
			lx.addToken(token.Slash, startLine, startCol)
		case '!':
			if lx.match('=') {
				lx.addToken(token.BangEqual, startLine, startCol)
			} else {
				lx.addToken(token.Bang, startLine, startCol)
			}
		case '=':
			if lx.match('=') {
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
	for !lx.isAtEnd() && lx.peek() != '"' {
		if lx.peek() == '\n' {
			return fmt.Errorf("line %d:%d: unterminated string", line, col)
		}
		lx.advance()
	}
	if lx.isAtEnd() {
		return fmt.Errorf("line %d:%d: unterminated string", line, col)
	}
	lx.advance()
	literal := string(lx.source[lx.start+1 : lx.cur-1])
	lx.items = append(lx.items, token.Token{Type: token.String, Lexeme: literal, Line: line, Column: col})
	return nil
}

func (lx *Lexer) scanNumber(line, col int) error {
	for unicode.IsDigit(lx.peek()) {
		lx.advance()
	}
	if lx.peek() == '.' && unicode.IsDigit(lx.peekNext()) {
		lx.advance()
		for unicode.IsDigit(lx.peek()) {
			lx.advance()
		}
	}
	literal := string(lx.source[lx.start:lx.cur])
	if _, err := strconv.ParseFloat(literal, 64); err != nil {
		return fmt.Errorf("line %d:%d: invalid number %q", line, col, literal)
	}
	lx.items = append(lx.items, token.Token{Type: token.Number, Lexeme: literal, Line: line, Column: col})
	return nil
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
