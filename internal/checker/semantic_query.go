package checker

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"seal/internal/ast"
	"seal/internal/source"
)

/*
ExprAt returns the smallest checked expression containing offset.

The first pass uses the conventional half-open span:

	[Start, End)

A second inclusive pass handles editor cursors positioned exactly at the end
of an expression.
*/
func (s SemanticInfo) ExprAt(
	file *source.File,
	offset int,
) ast.Expr {
	if file == nil {
		return nil
	}

	offset = clampSourceOffset(
		file,
		offset,
	)

	if expr :=
		s.exprAt(
			file,
			offset,
			false,
		); expr != nil {
		return expr
	}

	return s.exprAt(
		file,
		offset,
		true,
	)
}

func (s SemanticInfo) exprAt(
	file *source.File,
	offset int,
	includeEnd bool,
) ast.Expr {
	var best ast.Expr
	bestLength := int(^uint(0) >> 1)
	bestStart := -1

	for expr := range s.ExprTypes {
		if expr == nil {
			continue
		}

		span := normalizedQuerySpan(
			expr.Span(),
		)

		if !sameSourceFile(
			span.File,
			file,
		) {
			continue
		}

		contains := false

		if includeEnd {
			contains =
				offset >= span.Start &&
					offset <= span.End
		} else {
			contains =
				offset >= span.Start &&
					offset < span.End
		}

		if !contains {
			continue
		}

		length :=
			span.End -
				span.Start

		/*
			Prefer the narrowest expression. When lengths are equal, prefer the
			expression beginning later because it is generally more deeply
			nested.
		*/
		if best == nil ||
			length < bestLength ||
			(length == bestLength &&
				span.Start > bestStart) {
			best = expr
			bestLength = length
			bestStart = span.Start
		}
	}

	return best
}

/*
ExprEndingAtOrBefore returns the checked expression whose end is closest to,
but not after, offset.

This is primarily useful for incomplete selector completion:

	value.
	value.fi
	call().
	items[index].

The caller should verify that only whitespace exists between the selected
expression and the dot. That prevents an expression from a previous statement
from being selected accidentally.
*/
func (s SemanticInfo) ExprEndingAtOrBefore(
	file *source.File,
	offset int,
) ast.Expr {
	if file == nil {
		return nil
	}

	offset = clampSourceOffset(
		file,
		offset,
	)

	var best ast.Expr
	bestEnd := -1
	bestLength := int(^uint(0) >> 1)

	for expr := range s.ExprTypes {
		if expr == nil {
			continue
		}

		span := normalizedQuerySpan(
			expr.Span(),
		)

		if !sameSourceFile(
			span.File,
			file,
		) {
			continue
		}

		if span.End > offset {
			continue
		}

		length :=
			span.End -
				span.Start

		if best == nil ||
			span.End > bestEnd ||
			(span.End == bestEnd &&
				length < bestLength) {
			best = expr
			bestEnd = span.End
			bestLength = length
		}
	}

	return best
}

/*
TypeAt returns the checker-resolved type and expression at offset.
*/
func (s SemanticInfo) TypeAt(
	file *source.File,
	offset int,
) (
	*Type,
	ast.Expr,
	bool,
) {
	expr :=
		s.ExprAt(
			file,
			offset,
		)

	if expr == nil {
		return nil,
			nil,
			false
	}

	typ, found :=
		s.ExprTypes[expr]

	if !found ||
		typ == nil {
		return nil,
			expr,
			false
	}

	return typ,
		expr,
		true
}

/*
TypeEndingAtOrBefore is the typed form of ExprEndingAtOrBefore.
*/
func (s SemanticInfo) TypeEndingAtOrBefore(
	file *source.File,
	offset int,
) (
	*Type,
	ast.Expr,
	bool,
) {
	expr :=
		s.ExprEndingAtOrBefore(
			file,
			offset,
		)

	if expr == nil {
		return nil,
			nil,
			false
	}

	typ, found :=
		s.ExprTypes[expr]

	if !found ||
		typ == nil {
		return nil,
			expr,
			false
	}

	return typ,
		expr,
		true
}

/*
VisibleSymbols returns every symbol visible from this scope.

Nearest declarations win when a name appears in multiple parent scopes.
The returned slice is alphabetically ordered for deterministic consumers.
*/
func (s *Scope) VisibleSymbols() []*Symbol {
	if s == nil {
		return nil
	}

	seen :=
		map[string]bool{}

	var symbols []*Symbol

	for current :=
		s; current != nil; current = current.Parent {
		var names []string

		for name := range current.Symbols {
			names = append(
				names,
				name,
			)
		}

		sort.Strings(names)

		for _, name := range names {
			if seen[name] {
				continue
			}

			symbol :=
				current.Symbols[name]

			if symbol == nil {
				continue
			}

			seen[name] = true

			symbols = append(
				symbols,
				symbol,
			)
		}
	}

	sort.SliceStable(
		symbols,
		func(
			left int,
			right int,
		) bool {
			return symbols[left].Name <
				symbols[right].Name
		},
	)

	return symbols
}

/*
FindSymbolBySpan searches this scope and all descendant scopes for an exact
declaration span.

This is the bridge between resolver navigation and checker types:

	resolver use
	    -> resolver definition span
	    -> checker FindSymbolBySpan
	    -> checker type/signature
*/
func (s *Scope) FindSymbolBySpan(
	span source.Span,
) *Symbol {
	if s == nil ||
		span.File == nil {
		return nil
	}

	for _, symbol := range s.Symbols {
		if symbol == nil {
			continue
		}

		if sameQuerySpan(
			symbol.Span,
			span,
		) {
			return symbol
		}
	}

	for _, child := range s.Children {
		if symbol :=
			child.FindSymbolBySpan(
				span,
			); symbol != nil {
			return symbol
		}
	}

	return nil
}

/*
SymbolAt returns the declaration symbol whose declaration span contains offset.

It searches all descendant scopes and chooses the narrowest declaration span.
This operates on declaration positions. Arbitrary symbol uses should first be
resolved to their definition span through resolver.SemanticInfo.
*/
func (s *Scope) SymbolAt(
	file *source.File,
	offset int,
) *Symbol {
	if s == nil ||
		file == nil {
		return nil
	}

	offset = clampSourceOffset(
		file,
		offset,
	)

	if symbol :=
		s.symbolAt(
			file,
			offset,
			false,
		); symbol != nil {
		return symbol
	}

	return s.symbolAt(
		file,
		offset,
		true,
	)
}

func (s *Scope) symbolAt(
	file *source.File,
	offset int,
	includeEnd bool,
) *Symbol {
	if s == nil {
		return nil
	}

	var best *Symbol
	bestLength := int(^uint(0) >> 1)
	bestStart := -1

	var visit func(*Scope)

	visit =
		func(scope *Scope) {
			if scope == nil {
				return
			}

			for _, symbol := range scope.Symbols {
				if symbol == nil {
					continue
				}

				span :=
					normalizedQuerySpan(
						symbol.Span,
					)

				if !sameSourceFile(
					span.File,
					file,
				) {
					continue
				}

				contains := false

				if includeEnd {
					contains =
						offset >= span.Start &&
							offset <= span.End
				} else {
					contains =
						offset >= span.Start &&
							offset < span.End
				}

				if !contains {
					continue
				}

				length :=
					span.End -
						span.Start

				if best == nil ||
					length < bestLength ||
					(length == bestLength &&
						span.Start > bestStart) {
					best = symbol
					bestLength = length
					bestStart = span.Start
				}
			}

			for _, child := range scope.Children {
				visit(child)
			}
		}

	visit(s)

	return best
}

func normalizedQuerySpan(
	span source.Span,
) source.Span {
	if span.Start < 0 {
		span.Start = 0
	}

	if span.End < span.Start {
		span.End = span.Start
	}

	if span.File != nil {
		if span.Start >
			len(span.File.Text) {
			span.Start =
				len(span.File.Text)
		}

		if span.End >
			len(span.File.Text) {
			span.End =
				len(span.File.Text)
		}
	}

	return span
}

func clampSourceOffset(
	file *source.File,
	offset int,
) int {
	if file == nil {
		return 0
	}

	if offset < 0 {
		return 0
	}

	if offset >
		len(file.Text) {
		return len(file.Text)
	}

	return offset
}

func sameQuerySpan(
	left source.Span,
	right source.Span,
) bool {
	left =
		normalizedQuerySpan(
			left,
		)

	right =
		normalizedQuerySpan(
			right,
		)

	return sameSourceFile(
		left.File,
		right.File,
	) &&
		left.Start == right.Start &&
		left.End == right.End
}

func sameSourceFile(
	left *source.File,
	right *source.File,
) bool {
	if left == right {
		return left != nil
	}

	if left == nil ||
		right == nil {
		return false
	}

	if left.Path == "" ||
		right.Path == "" {
		return false
	}

	leftPath :=
		filepath.Clean(
			left.Path,
		)

	rightPath :=
		filepath.Clean(
			right.Path,
		)

	if runtime.GOOS ==
		"windows" {
		return strings.EqualFold(
			leftPath,
			rightPath,
		)
	}

	return leftPath ==
		rightPath
}

/*
ExpectedTypeFor returns the contextual type supplied while checking expr.
*/
func (s SemanticInfo) ExpectedTypeFor(
	expr ast.Expr,
) (*Type, bool) {
	if expr == nil {
		return nil, false
	}

	typ, found := s.ExpectedExprTypes[expr]

	return typ, found && typ != nil
}

/*
ExpectedTypeAt returns the contextual type belonging to the smallest expression
containing offset.

This is used for contextual enum completion:

	state Status = .
	               ^
*/
func (s SemanticInfo) ExpectedTypeAt(
	file *source.File,
	offset int,
) (
	*Type,
	ast.Expr,
	bool,
) {
	if file == nil {
		return nil, nil, false
	}

	offset = clampSourceOffset(
		file,
		offset,
	)

	if typ, expr, found :=
		s.expectedTypeAt(
			file,
			offset,
			false,
		); found {
		return typ, expr, true
	}

	return s.expectedTypeAt(
		file,
		offset,
		true,
	)
}

func (s SemanticInfo) expectedTypeAt(
	file *source.File,
	offset int,
	includeEnd bool,
) (
	*Type,
	ast.Expr,
	bool,
) {
	var bestExpr ast.Expr
	var bestType *Type

	bestLength := int(^uint(0) >> 1)
	bestStart := -1

	for expr, typ := range s.ExpectedExprTypes {
		if expr == nil ||
			typ == nil {
			continue
		}

		span := normalizedQuerySpan(
			expr.Span(),
		)

		if !sameSourceFile(
			span.File,
			file,
		) {
			continue
		}

		contains := false

		if includeEnd {
			contains =
				offset >= span.Start &&
					offset <= span.End
		} else {
			contains =
				offset >= span.Start &&
					offset < span.End
		}

		if !contains {
			continue
		}

		length :=
			span.End -
				span.Start

		if bestExpr == nil ||
			length < bestLength ||
			(length == bestLength &&
				span.Start > bestStart) {
			bestExpr = expr
			bestType = typ
			bestLength = length
			bestStart = span.Start
		}
	}

	if bestExpr == nil ||
		bestType == nil {
		return nil, nil, false
	}

	return bestType,
		bestExpr,
		true
}

/*
SelectorAt returns the smallest selector whose selected name contains offset.

For:

	value.field
	      ^^^^^

this returns the SelectorExpr only while the cursor is on field, not anywhere
inside the receiver expression.
*/
func (s SemanticInfo) SelectorAt(
	file *source.File,
	offset int,
) *ast.SelectorExpr {
	if file == nil {
		return nil
	}

	offset = clampSourceOffset(
		file,
		offset,
	)

	if selector :=
		s.selectorAt(
			file,
			offset,
			false,
		); selector != nil {
		return selector
	}

	return s.selectorAt(
		file,
		offset,
		true,
	)
}

func (s SemanticInfo) selectorAt(
	file *source.File,
	offset int,
	includeEnd bool,
) *ast.SelectorExpr {
	var best *ast.SelectorExpr
	bestLength := int(^uint(0) >> 1)

	for expr := range s.ExprTypes {
		selector, ok :=
			expr.(*ast.SelectorExpr)

		if !ok ||
			selector == nil {
			continue
		}

		nameSpan :=
			normalizedQuerySpan(
				selector.Name.Span(),
			)

		if !sameSourceFile(
			nameSpan.File,
			file,
		) {
			continue
		}

		contains := false

		if includeEnd {
			contains =
				offset >= nameSpan.Start &&
					offset <= nameSpan.End
		} else {
			contains =
				offset >= nameSpan.Start &&
					offset < nameSpan.End
		}

		if !contains {
			continue
		}

		selectorSpan :=
			normalizedQuerySpan(
				selector.Span(),
			)

		length :=
			selectorSpan.End -
				selectorSpan.Start

		if best == nil ||
			length < bestLength {
			best = selector
			bestLength = length
		}
	}

	return best
}

func (k SymbolKind) String() string {
	switch k {
	case SymbolConst:
		return "constant"

	case SymbolVar:
		return "variable"

	case SymbolParam:
		return "parameter"

	case SymbolType:
		return "type"

	case SymbolTask:
		return "task"

	case SymbolOverload:
		return "overload"

	case SymbolForeignTaskABI:
		return "foreign task ABI"

	case SymbolPackage:
		return "package"

	default:
		return "symbol"
	}
}
