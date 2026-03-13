package token

type Type string

const (
	EOF         Type = "EOF"
	Newline     Type = "NEWLINE"
	Identifier  Type = "IDENTIFIER"
	IntNumber   Type = "INT_NUMBER"
	FloatNumber Type = "FLOAT_NUMBER"
	Char        Type = "CHAR"
	String      Type = "STRING"

	LeftParen    Type = "("
	RightParen   Type = ")"
	LeftBracket  Type = "["
	RightBracket Type = "]"
	LeftBrace    Type = "{"
	RightBrace   Type = "}"
	Comma        Type = ","
	Colon        Type = ":"
	Dot          Type = "."
	Question     Type = "?"
	Ampersand    Type = "&"
	Pipe         Type = "|"
	At           Type = "@"
	Arrow        Type = "->"
	FatArrow     Type = "=>"

	Plus         Type = "+"
	PlusEqual    Type = "+="
	Minus        Type = "-"
	MinusEqual   Type = "-="
	Star         Type = "*"
	StarStar     Type = "**"
	StarEqual    Type = "*="
	Slash        Type = "/"
	SlashEqual   Type = "/="
	Percent      Type = "%"
	Caret        Type = "^"
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
	Enum       Type = "enum"
	Def        Type = "def"
	Native     Type = "native"
	Import     Type = "import"
	Record     Type = "record"
	Abstract   Type = "abstract"
	Sealed     Type = "sealed"
	Interface  Type = "interface"
	Class      Type = "class"
	Extends    Type = "extends"
	Implements Type = "implements"
	Static     Type = "static"
	Public     Type = "public"
	Private    Type = "private"
	Protected  Type = "protected"
	New        Type = "new"
	This       Type = "this"
	Super      Type = "super"
	If         Type = "if"
	Else       Type = "else"
	Elif       Type = "elif"
	Switch     Type = "switch"
	Case       Type = "case"
	Default    Type = "default"
	End        Type = "end"
	Return     Type = "return"
	Try        Type = "try"
	Catch      Type = "catch"
	Throw      Type = "throw"
	For        Type = "for"
	LoopKw     Type = "loop"
	DoKw       Type = "do"
	BreakKw    Type = "break"
	ContinueKw Type = "continue"
	In         Type = "in"
	TypeKw     Type = "type"
	Where      Type = "where"
	Instanceof Type = "instanceof"
	True       Type = "true"
	False      Type = "false"
	Nil        Type = "nil"
	ThreadKw   Type = "thread"
	SpawnKw    Type = "spawn"
	Ellipsis   Type = "..."
)

var keywords = map[string]Type{
	"let":        Let,
	"var":        Var,
	"const":      Const,
	"final":      Final,
	"enum":       Enum,
	"def":        Def,
	"native":     Native,
	"import":     Import,
	"record":     Record,
	"abstract":   Abstract,
	"sealed":     Sealed,
	"interface":  Interface,
	"class":      Class,
	"extends":    Extends,
	"implements": Implements,
	"static":     Static,
	"public":     Public,
	"pub":        Public,
	"private":    Private,
	"priv":       Private,
	"protected":  Protected,
	"prot":       Protected,
	"new":        New,
	"this":       This,
	"super":      Super,
	"if":         If,
	"else":       Else,
	"elif":       Elif,
	"switch":     Switch,
	"case":       Case,
	"default":    Default,
	"end":        End,
	"return":     Return,
	"try":        Try,
	"catch":      Catch,
	"throw":      Throw,
	"for":        For,
	"loop":       LoopKw,
	"do":         DoKw,
	"break":      BreakKw,
	"continue":   ContinueKw,
	"in":         In,
	"type":       TypeKw,
	"where":      Where,
	"instanceof": Instanceof,
	"true":       True,
	"false":      False,
	"nil":        Nil,
	"thread":     ThreadKw,
	"spawn":      SpawnKw,
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
