package token

import "seal/internal/source"

type Token struct {
	Kind   Kind
	Lexeme string
	Span   source.Span
}
