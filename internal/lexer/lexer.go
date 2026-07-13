package lexer

import (
	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

type Lexer struct {
	file *source.File
	text string
	pos  int

	diags *diag.Reporter
}

func New(file *source.File, diags *diag.Reporter) *Lexer {
	return &Lexer{
		file:  file,
		text:  file.Text,
		diags: diags,
	}
}

func (l *Lexer) LexAll() []token.Token {
	var tokens []token.Token

	for {
		tok := l.Next()

		tokens = append(tokens, tok)

		if tok.Kind == token.EOF {
			break
		}
	}

	return tokens
}

func (l *Lexer) Next() token.Token {
	l.skipWhitespaceAndComments()

	start := l.pos

	if l.isAtEnd() {
		return l.makeToken(token.EOF, start, start)
	}

	ch := l.advance()

	if isIdentStart(ch) {
		if ch == 'c' && l.peek() == '"' {
			return l.lexCString(start)
		}

		return l.lexIdentifier(start)
	}

	if isDigit(ch) {
		return l.lexNumber(start)
	}

	switch ch {
	case '"':
		return l.lexString(start)

	case '\'':
		return l.lexChar(start)

	case '(':
		return l.makeToken(token.LParen, start, l.pos)
	case ')':
		return l.makeToken(token.RParen, start, l.pos)
	case '{':
		return l.makeToken(token.LBrace, start, l.pos)
	case '}':
		return l.makeToken(token.RBrace, start, l.pos)
	case '[':
		return l.makeToken(token.LBracket, start, l.pos)
	case ']':
		return l.makeToken(token.RBracket, start, l.pos)
	case ',':
		return l.makeToken(token.Comma, start, l.pos)
	case ';':
		return l.makeToken(token.Semi, start, l.pos)
	case '?':
		return l.makeToken(token.Question, start, l.pos)
	case '@':
		return l.makeToken(token.At, start, l.pos)
	case '~':
		return l.makeToken(token.Tilde, start, l.pos)
	case '^':
		return l.makeToken(token.Caret, start, l.pos)

	case '.':
		if l.match('.') {
			if l.match('.') {
				return l.makeToken(token.Ellipsis, start, l.pos)
			}

			l.errorAt(start, l.pos, "expected third '.' for ellipsis")
			return l.makeToken(token.Invalid, start, l.pos)
		}

		return l.makeToken(token.Dot, start, l.pos)

	case ':':
		if l.match(':') {
			return l.makeToken(token.ColonColon, start, l.pos)
		}

		if l.match('=') {
			return l.makeToken(token.ColonEq, start, l.pos)
		}

		return l.makeToken(token.Colon, start, l.pos)

	case '=':
		if l.match('=') {
			return l.makeToken(token.EqEq, start, l.pos)
		}

		return l.makeToken(token.Assign, start, l.pos)

	case '!':
		if l.match('=') {
			return l.makeToken(token.NotEq, start, l.pos)
		}

		return l.makeToken(token.Bang, start, l.pos)

	case '<':
		if l.match('=') {
			return l.makeToken(token.LtEq, start, l.pos)
		}

		return l.makeToken(token.Lt, start, l.pos)

	case '>':
		if l.match('=') {
			return l.makeToken(token.GtEq, start, l.pos)
		}

		return l.makeToken(token.Gt, start, l.pos)

	case '+':
		if l.match('=') {
			return l.makeToken(token.PlusEq, start, l.pos)
		}

		return l.makeToken(token.Plus, start, l.pos)

	case '-':
		if l.match('=') {
			return l.makeToken(token.MinusEq, start, l.pos)
		}

		return l.makeToken(token.Minus, start, l.pos)

	case '*':
		if l.match('=') {
			return l.makeToken(token.StarEq, start, l.pos)
		}

		return l.makeToken(token.Star, start, l.pos)

	case '/':
		if l.match('=') {
			return l.makeToken(token.SlashEq, start, l.pos)
		}

		return l.makeToken(token.Slash, start, l.pos)

	case '%':
		if l.match('=') {
			return l.makeToken(token.PercentEq, start, l.pos)
		}

		return l.makeToken(token.Percent, start, l.pos)

	case '&':
		if l.match('&') {
			return l.makeToken(token.AndAnd, start, l.pos)
		}

		return l.makeToken(token.Amp, start, l.pos)

	case '|':
		if l.match('|') {
			return l.makeToken(token.OrOr, start, l.pos)
		}

		return l.makeToken(token.Pipe, start, l.pos)
	}

	l.errorAt(start, l.pos, "unexpected character")
	return l.makeToken(token.Invalid, start, l.pos)
}

func (l *Lexer) lexIdentifier(start int) token.Token {
	for !l.isAtEnd() && isIdentContinue(l.peek()) {
		l.advance()
	}

	text := l.text[start:l.pos]
	kind := token.LookupIdent(text)

	return l.makeToken(kind, start, l.pos)
}

func (l *Lexer) lexNumber(start int) token.Token {
	// Hexadecimal integer:
	//
	//     0x80
	//     0XFF
	//     0x10_FFFF
	//
	// The initial `0` has already been consumed by Next().
	if l.text[start] == '0' &&
		!l.isAtEnd() &&
		(l.peek() == 'x' || l.peek() == 'X') {
		l.advance() // Consume x or X.

		hasDigit := false
		previousUnderscore := false

		for !l.isAtEnd() {
			ch := l.peek()

			switch {
			case isHexDigit(ch):
				hasDigit = true
				previousUnderscore = false
				l.advance()

			case ch == '_':
				if !hasDigit || previousUnderscore {
					l.errorAt(
						start,
						l.pos+1,
						"invalid underscore in hexadecimal literal",
					)
				}

				previousUnderscore = true
				l.advance()

			default:
				goto hexadecimalDone
			}
		}

	hexadecimalDone:
		if !hasDigit {
			l.errorAt(
				start,
				l.pos,
				"expected hexadecimal digit after '0x'",
			)

			return l.makeToken(
				token.Invalid,
				start,
				l.pos,
			)
		}

		if previousUnderscore {
			l.errorAt(
				start,
				l.pos,
				"hexadecimal literal cannot end with underscore",
			)

			return l.makeToken(
				token.Invalid,
				start,
				l.pos,
			)
		}

		return l.makeToken(
			token.IntLit,
			start,
			l.pos,
		)
	}

	kind := token.IntLit

	for !l.isAtEnd() &&
		isDigitOrUnderscore(l.peek()) {
		l.advance()
	}

	if !l.isAtEnd() &&
		l.peek() == '.' &&
		l.peekNextIsDigit() {
		kind = token.FloatLit
		l.advance()

		for !l.isAtEnd() &&
			isDigitOrUnderscore(l.peek()) {
			l.advance()
		}
	}

	if !l.isAtEnd() &&
		(l.peek() == 'e' || l.peek() == 'E') {
		kind = token.FloatLit
		l.advance()

		if !l.isAtEnd() &&
			(l.peek() == '+' || l.peek() == '-') {
			l.advance()
		}

		if l.isAtEnd() || !isDigit(l.peek()) {
			l.errorAt(
				start,
				l.pos,
				"expected digit after exponent",
			)

			return l.makeToken(
				token.Invalid,
				start,
				l.pos,
			)
		}

		for !l.isAtEnd() &&
			isDigitOrUnderscore(l.peek()) {
			l.advance()
		}
	}

	return l.makeToken(
		kind,
		start,
		l.pos,
	)
}

func (l *Lexer) lexString(start int) token.Token {
	for !l.isAtEnd() {
		ch := l.advance()

		if ch == '"' {
			return l.makeToken(token.StringLit, start, l.pos)
		}

		if ch == '\n' {
			l.errorAt(start, l.pos, "unterminated string literal")
			return l.makeToken(token.Invalid, start, l.pos)
		}

		if ch == '\\' && !l.isAtEnd() {
			l.advance()
		}
	}

	l.errorAt(start, l.pos, "unterminated string literal")
	return l.makeToken(token.Invalid, start, l.pos)
}

func (l *Lexer) lexCString(start int) token.Token {
	// We already consumed the leading `c`.
	// Current character must be the opening `"`.
	if l.isAtEnd() || l.peek() != '"' {
		return l.makeToken(token.Ident, start, l.pos)
	}

	l.advance()

	for !l.isAtEnd() {
		ch := l.advance()

		if ch == '"' {
			return l.makeToken(token.CStringLit, start, l.pos)
		}

		if ch == '\n' {
			l.errorAt(start, l.pos, "unterminated cstring literal")
			return l.makeToken(token.Invalid, start, l.pos)
		}

		if ch == '\\' && !l.isAtEnd() {
			l.advance()
		}
	}

	l.errorAt(start, l.pos, "unterminated cstring literal")
	return l.makeToken(token.Invalid, start, l.pos)
}

func (l *Lexer) lexChar(start int) token.Token {
	for !l.isAtEnd() {
		ch := l.advance()

		if ch == '\'' {
			return l.makeToken(token.CharLit, start, l.pos)
		}

		if ch == '\n' {
			l.errorAt(start, l.pos, "unterminated char literal")
			return l.makeToken(token.Invalid, start, l.pos)
		}

		if ch == '\\' && !l.isAtEnd() {
			l.advance()
		}
	}

	l.errorAt(start, l.pos, "unterminated char literal")
	return l.makeToken(token.Invalid, start, l.pos)
}

func (l *Lexer) skipWhitespaceAndComments() {
	for {
		if l.isAtEnd() {
			return
		}

		switch l.peek() {
		case ' ', '\r', '\t', '\n':
			l.advance()
			continue

		case '/':
			if l.peekNext() == '/' {
				l.advance()
				l.advance()

				for !l.isAtEnd() && l.peek() != '\n' {
					l.advance()
				}

				continue
			}

			if l.peekNext() == '*' {
				l.skipBlockComment()
				continue
			}
		}

		return
	}
}

func (l *Lexer) skipBlockComment() {
	start := l.pos

	l.advance()
	l.advance()

	depth := 1

	for !l.isAtEnd() {
		if l.peek() == '/' && l.peekNext() == '*' {
			l.advance()
			l.advance()
			depth++
			continue
		}

		if l.peek() == '*' && l.peekNext() == '/' {
			l.advance()
			l.advance()
			depth--

			if depth == 0 {
				return
			}

			continue
		}

		l.advance()
	}

	l.errorAt(start, l.pos, "unterminated block comment")
}

func (l *Lexer) makeToken(kind token.Kind, start int, end int) token.Token {
	return token.Token{
		Kind:   kind,
		Lexeme: l.text[start:end],
		Span:   source.NewSpan(l.file, start, end),
	}
}

func (l *Lexer) errorAt(start int, end int, message string) {
	l.diags.Add(source.NewSpan(l.file, start, end), message)
}

func (l *Lexer) isAtEnd() bool {
	return l.pos >= len(l.text)
}

func (l *Lexer) advance() byte {
	ch := l.text[l.pos]
	l.pos++
	return ch
}

func (l *Lexer) peek() byte {
	if l.isAtEnd() {
		return 0
	}

	return l.text[l.pos]
}

func (l *Lexer) peekNext() byte {
	if l.pos+1 >= len(l.text) {
		return 0
	}

	return l.text[l.pos+1]
}

func (l *Lexer) peekNextIsDigit() bool {
	return l.pos+1 < len(l.text) && isDigit(l.text[l.pos+1])
}

func (l *Lexer) match(expected byte) bool {
	if l.isAtEnd() {
		return false
	}

	if l.text[l.pos] != expected {
		return false
	}

	l.pos++
	return true
}

func isIdentStart(ch byte) bool {
	return ch == '_' ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z')
}

func isIdentContinue(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch)
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isHexDigit(ch byte) bool {
	return isDigit(ch) ||
		(ch >= 'a' && ch <= 'f') ||
		(ch >= 'A' && ch <= 'F')
}

func isDigitOrUnderscore(ch byte) bool {
	return isDigit(ch) || ch == '_'
}
