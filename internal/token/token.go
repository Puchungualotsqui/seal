package token

import "seal/internal/source"

type Token struct {
	Kind   Kind
	Lexeme string
	Span   source.Span
}

type Kind int

const (
	Invalid Kind = iota
	EOF

	Ident
	IntLit
	FloatLit
	StringLit
	CStringLit
	CharLit

	// Keywords
	KeywordTask
	KeywordPure
	KeywordIntrinsic
	KeywordTest
	KeywordStruct
	KeywordInterface
	KeywordUnion
	KeywordEnum
	KeywordBitSet
	KeywordImpl
	KeywordOverload
	KeywordImport
	KeywordSwitch
	KeywordCase
	KeywordDefault
	KeywordIf
	KeywordElse
	KeywordFor
	KeywordReturn
	KeywordDefer
	KeywordDyn
	KeywordType
	KeywordSeal
	KeywordConst
	KeywordDistinct
	KeywordNil
	KeywordTrue
	KeywordFalse

	// Punctuation
	LParen   // (
	RParen   // )
	LBrace   // {
	RBrace   // }
	LBracket // [
	RBracket // ]
	Comma    // ,
	Dot      // .
	Semi     // ;
	Colon    // :
	Question // ?
	At       // @

	// Operators
	ColonColon // ::
	ColonEq    // :=
	Ellipsis   // ...
	Assign     // =
	EqEq       // ==
	NotEq      // !=
	Lt         // <
	Gt         // >
	LtEq       // <=
	GtEq       // >=
	Plus       // +
	Minus      // -
	Star       // *
	Slash      // /
	Percent    // %
	Bang       // !
	Amp        // &
	Pipe       // |
	Caret      // ^
	Tilde      // ~
	AndAnd     // &&
	OrOr       // ||
	PlusEq     // +=
	MinusEq    // -=
	StarEq     // *=
	SlashEq    // /=
	PercentEq  // %=
)

var keywordKinds = map[string]Kind{
	"task":      KeywordTask,
	"pure":      KeywordPure,
	"intrinsic": KeywordIntrinsic,
	"test":      KeywordTest,
	"struct":    KeywordStruct,
	"distinct":  KeywordDistinct,
	"dyn":       KeywordDyn,
	"interface": KeywordInterface,
	"union":     KeywordUnion,
	"enum":      KeywordEnum,
	"bit_set":   KeywordBitSet,
	"impl":      KeywordImpl,
	"overload":  KeywordOverload,
	"import":    KeywordImport,
	"switch":    KeywordSwitch,
	"case":      KeywordCase,
	"default":   KeywordDefault,
	"if":        KeywordIf,
	"else":      KeywordElse,
	"for":       KeywordFor,
	"return":    KeywordReturn,
	"defer":     KeywordDefer,
	"seal":      KeywordSeal,
	"const":     KeywordConst,
	"type":      KeywordType,
	"nil":       KeywordNil,
	"true":      KeywordTrue,
	"false":     KeywordFalse,
}

func LookupIdent(text string) Kind {
	if kind, ok := keywordKinds[text]; ok {
		return kind
	}

	return Ident
}

func (k Kind) String() string {
	switch k {
	case Invalid:
		return "Invalid"
	case EOF:
		return "EOF"
	case Ident:
		return "Ident"
	case IntLit:
		return "IntLit"
	case FloatLit:
		return "FloatLit"
	case StringLit:
		return "StringLit"
	case CStringLit:
		return "CStringLit"
	case CharLit:
		return "CharLit"

	case KeywordDyn:
		return "dyn"
	case KeywordType:
		return "type"
	case KeywordTask:
		return "task"
	case KeywordPure:
		return "pure"
	case KeywordIntrinsic:
		return "intrinsic"
	case KeywordTest:
		return "test"
	case KeywordStruct:
		return "struct"
	case KeywordInterface:
		return "interface"
	case KeywordUnion:
		return "union"
	case KeywordEnum:
		return "enum"
	case KeywordBitSet:
		return "bit_set"
	case KeywordImpl:
		return "impl"
	case KeywordOverload:
		return "overload"
	case KeywordImport:
		return "import"
	case KeywordSwitch:
		return "switch"
	case KeywordCase:
		return "case"
	case KeywordDefault:
		return "default"
	case KeywordIf:
		return "if"
	case KeywordElse:
		return "else"
	case KeywordFor:
		return "for"
	case KeywordReturn:
		return "return"
	case KeywordDefer:
		return "defer"
	case KeywordSeal:
		return "seal"
	case KeywordConst:
		return "const"
	case KeywordDistinct:
		return "distinct"
	case KeywordNil:
		return "nil"
	case KeywordTrue:
		return "true"
	case KeywordFalse:
		return "false"

	case LParen:
		return "("
	case RParen:
		return ")"
	case LBrace:
		return "{"
	case RBrace:
		return "}"
	case LBracket:
		return "["
	case RBracket:
		return "]"
	case Comma:
		return ","
	case Dot:
		return "."
	case Semi:
		return ";"
	case Colon:
		return ":"
	case Question:
		return "?"
	case At:
		return "@"

	case ColonColon:
		return "::"
	case ColonEq:
		return ":="
	case Ellipsis:
		return "..."
	case Assign:
		return "="
	case EqEq:
		return "=="
	case NotEq:
		return "!="
	case Lt:
		return "<"
	case Gt:
		return ">"
	case LtEq:
		return "<="
	case GtEq:
		return ">="
	case Plus:
		return "+"
	case Minus:
		return "-"
	case Star:
		return "*"
	case Slash:
		return "/"
	case Percent:
		return "%"
	case Bang:
		return "!"
	case Amp:
		return "&"
	case Pipe:
		return "|"
	case Caret:
		return "^"
	case Tilde:
		return "~"
	case AndAnd:
		return "&&"
	case OrOr:
		return "||"
	case PlusEq:
		return "+="
	case MinusEq:
		return "-="
	case StarEq:
		return "*="
	case SlashEq:
		return "/="
	case PercentEq:
		return "%="
	default:
		return "<unknown>"
	}
}
