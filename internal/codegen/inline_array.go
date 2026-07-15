package cgen

import (
	"fmt"
	"seal/internal/ast"
	"seal/internal/token"
	"strings"
)

func (g *Generator) cInlineArrayTypeFromAst(
	t *ast.InlineArrayType,
	subst map[string]ast.GenericArg,
) CType {
	if t == nil {
		return CInvalid
	}

	elem := CInvalid

	if subst != nil {
		elem = g.cTypeFromAstWithGenericArgs(
			t.Elem,
			subst,
		)
	} else {
		elem = g.cTypeFromAstInContext(
			t.Elem,
		)
	}

	length, ok :=
		g.inlineArrayLengthFromExpr(
			t.Length,
			subst,
		)

	if !ok {
		span := t.Span()
		if t.Length != nil {
			span = t.Length.Span()
		}

		g.error(
			span,
			"@inline_array length must be a non-negative compile-time integer",
		)

		length = 0
	}

	out := CType{
		SealName: fmt.Sprintf(
			"@inline_array<%s,%d>",
			elem.SealName,
			length,
		),
		IsInlineArray: true,
		InlineLength:  length,
		Elem:          &elem,
	}

	out.Name = out.TypeName()

	return out
}

func (g *Generator) inlineArrayLengthFromExpr(
	expr ast.Expr,
	subst map[string]ast.GenericArg,
) (uint64, bool) {
	value, ok :=
		g.evalInlineArrayConstInt(
			expr,
			subst,
			map[string]bool{},
		)

	if !ok || value < 0 {
		return 0, false
	}

	return uint64(value), true
}

func (g *Generator) evalInlineArrayConstInt(
	expr ast.Expr,
	subst map[string]ast.GenericArg,
	visiting map[string]bool,
) (int64, bool) {
	if expr == nil {
		return 0, false
	}

	switch e := expr.(type) {
	case *ast.IntLitExpr:
		value, ok :=
			parseSealIntegerLiteralForCGen(
				e.Value,
			)

		if !ok || !value.IsInt64() {
			return 0, false
		}

		return value.Int64(), true

	case *ast.IdentExpr:
		name := e.Name.Name

		if subst != nil {
			if arg, ok := subst[name]; ok &&
				arg.Kind == ast.GenericArgExpr &&
				arg.Expr != nil {
				if genericArgIsSingleNameForCGen(
					arg,
					name,
				) {
					return 0, false
				}

				return g.evalInlineArrayConstInt(
					arg.Expr,
					subst,
					visiting,
				)
			}
		}

		if subst == nil && g.genericSubst != nil {
			if arg, ok := g.genericSubst[name]; ok &&
				arg.Kind == ast.GenericArgExpr &&
				arg.Expr != nil {
				if genericArgIsSingleNameForCGen(
					arg,
					name,
				) {
					return 0, false
				}

				return g.evalInlineArrayConstInt(
					arg.Expr,
					g.genericSubst,
					visiting,
				)
			}
		}

		decl := g.constDecls[name]
		if decl == nil {
			return 0, false
		}

		if visiting[name] {
			return 0, false
		}

		visiting[name] = true

		value, ok :=
			g.evalInlineArrayConstInt(
				decl.Value,
				subst,
				visiting,
			)

		visiting[name] = false

		return value, ok

	case *ast.UnaryExpr:
		value, ok :=
			g.evalInlineArrayConstInt(
				e.Expr,
				subst,
				visiting,
			)

		if !ok {
			return 0, false
		}

		switch e.Op {
		case token.Plus:
			return value, true

		case token.Minus:
			return -value, true

		case token.Tilde:
			return ^value, true
		}

		return 0, false

	case *ast.BinaryExpr:
		left, ok :=
			g.evalInlineArrayConstInt(
				e.Left,
				subst,
				visiting,
			)

		if !ok {
			return 0, false
		}

		right, ok :=
			g.evalInlineArrayConstInt(
				e.Right,
				subst,
				visiting,
			)

		if !ok {
			return 0, false
		}

		switch e.Op {
		case token.Plus:
			return left + right, true

		case token.Minus:
			return left - right, true

		case token.Star:
			return left * right, true

		case token.Slash:
			if right == 0 {
				return 0, false
			}

			return left / right, true

		case token.Percent:
			if right == 0 {
				return 0, false
			}

			return left % right, true

		case token.Amp:
			return left & right, true

		case token.Pipe:
			return left | right, true

		case token.Caret:
			return left ^ right, true
		}

		return 0, false

	case *ast.CallExpr:
		// Accept cast<int>(N)-style constant wrappers.
		gen, ok := e.Callee.(*ast.GenericExpr)
		if !ok || len(e.Args) != 1 {
			return 0, false
		}

		id, ok := gen.Base.(*ast.IdentExpr)
		if !ok || id.Name.Name != "cast" {
			return 0, false
		}

		return g.evalInlineArrayConstInt(
			e.Args[0],
			subst,
			visiting,
		)
	}

	return 0, false
}

func (g *Generator) emitInlineArrayInitializer(
	e *ast.InlineArrayExpr,
	typ CType,
) string {
	if e == nil {
		return "{0}"
	}

	if !typ.IsInlineArray || typ.Elem == nil {
		g.error(
			e.Span(),
			"@inline_array initializer has no inline-array C type",
		)

		return "{0}"
	}

	if len(e.Values) == 0 {
		return "{0}"
	}

	elemType := *typ.Elem
	values := make(
		[]string,
		0,
		len(e.Values),
	)

	for _, value := range e.Values {
		if elemType.IsInlineArray {
			nested, ok :=
				value.(*ast.InlineArrayExpr)

			if !ok {
				g.error(
					value.Span(),
					"nested @inline_array element must be initialized with an @inline_array literal",
				)

				values = append(values, "{0}")
				continue
			}

			values = append(
				values,
				g.emitInlineArrayInitializer(
					nested,
					elemType,
				),
			)

			continue
		}

		values = append(
			values,
			g.emitExpr(
				value,
				&elemType,
			),
		)
	}

	return fmt.Sprintf(
		"{%s}",
		strings.Join(values, ", "),
	)
}

func (g *Generator) emitInlineArrayCompoundLiteral(
	e *ast.InlineArrayExpr,
	typ CType,
) string {
	return fmt.Sprintf(
		"((%s)%s)",
		typ.TypeName(),
		g.emitInlineArrayInitializer(
			e,
			typ,
		),
	)
}

func (g *Generator) emitStructLiteralFieldValue(
	value ast.Expr,
	fieldType CType,
) string {
	if inline, ok :=
		value.(*ast.InlineArrayExpr); ok &&
		fieldType.IsInlineArray {
		return g.emitInlineArrayInitializer(
			inline,
			fieldType,
		)
	}

	if fieldType.IsInlineArray {
		g.error(
			value.Span(),
			"cannot initialize @inline_array field from a non-@inline_array expression",
		)

		return "{0}"
	}

	if isInvalidCType(fieldType) {
		return g.emitExpr(value, nil)
	}

	return g.emitExpr(
		value,
		&fieldType,
	)
}
