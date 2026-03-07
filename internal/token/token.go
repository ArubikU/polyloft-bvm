package token

type Type string

const (
	EOF        Type = "EOF"
	Newline    Type = "NEWLINE"
	Identifier Type = "IDENTIFIER"
	Number     Type = "NUMBER"
	String     Type = "STRING"

	LeftParen  Type = "("
	RightParen Type = ")"
	Comma      Type = ","
	Colon      Type = ":"
	Dot        Type = "."
	At         Type = "@"
	Arrow      Type = "->"

	Plus         Type = "+"
	Minus        Type = "-"
	Star         Type = "*"
	Slash        Type = "/"
	Percent      Type = "%"
	Bang         Type = "!"
	Equal        Type = "="
	EqualEqual   Type = "=="
	BangEqual    Type = "!="
	AndAnd       Type = "&&"
	OrOr         Type = "||"
	Greater      Type = ">"
	GreaterEqual Type = ">="
	Less         Type = "<"
	LessEqual    Type = "<="

	Let        Type = "let"
	Var        Type = "var"
	Const      Type = "const"
	Final      Type = "final"
	Def        Type = "def"
	Class      Type = "class"
	Implements Type = "implements"
	Static     Type = "static"
	New        Type = "new"
	This       Type = "this"
	Super      Type = "super"
	If         Type = "if"
	Else       Type = "else"
	End        Type = "end"
	Return     Type = "return"
	For        Type = "for"
	In         Type = "in"
	Where      Type = "where"
	True       Type = "true"
	False      Type = "false"
	Nil        Type = "nil"
)

var keywords = map[string]Type{
	"let":        Let,
	"var":        Var,
	"const":      Const,
	"final":      Final,
	"def":        Def,
	"class":      Class,
	"implements": Implements,
	"static":     Static,
	"new":        New,
	"this":       This,
	"super":      Super,
	"if":         If,
	"else":       Else,
	"end":        End,
	"return":     Return,
	"for":        For,
	"in":         In,
	"where":      Where,
	"true":       True,
	"false":      False,
	"nil":        Nil,
}

type Token struct {
	Type   Type
	Lexeme string
	Line   int
	Column int
}

func LookupIdentifier(lit string) Type {
	if tokenType, ok := keywords[lit]; ok {
		return tokenType
	}
	return Identifier
}
