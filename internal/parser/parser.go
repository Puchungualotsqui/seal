package parser

import (
	"fmt"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

type Parser struct {
	tokens []token.Token
	pos    int
	diags  *diag.Reporter
}

func New(tokens []token.Token, diags *diag.Reporter) *Parser {
	return &Parser{
		tokens: tokens,
		diags:  diags,
	}
}

func (p *Parser) ParseFile() *ast.File {
	file := &ast.File{}

	for !p.at(token.EOF) {
		decl := p.parseDecl()
		if decl != nil {
			file.Decls = append(file.Decls, decl)
			continue
		}

		p.synchronizeTopLevel()
	}

	return file
}

func (p *Parser) parseDecl() ast.Decl {
	start := p.peek().Span.Start

	nameTok := p.peek()
	if !p.isDeclName(nameTok.Kind) {
		p.errorHere("expected declaration name")
		return nil
	}

	p.advance()

	if !p.expect(token.ColonColon, "expected '::' after declaration name") {
		return nil
	}

	if p.match(token.At) {
		dir := p.expectIdent("expected directive name after '@'")
		if dir.Name == "" {
			return nil
		}

		if dir.Name == "rawUnion" {
			if !p.expect(token.KeywordUnion, "expected 'union' after @rawUnion") {
				return nil
			}

			return p.parseUnionDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start, true)
		}

		return p.parseDirectiveDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, dir, start)
	}

	if p.match(token.KeywordTest) {
		if !p.expect(token.KeywordTask, "expected 'task' after 'test'") {
			return nil
		}

		return p.parseTaskDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start, false, true)
	}

	if p.match(token.KeywordPure) {
		if !p.expect(token.KeywordTask, "expected 'task' after 'pure'") {
			return nil
		}

		return p.parseTaskDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start, true, false)
	}

	switch {
	case p.match(token.KeywordTask):
		return p.parseTaskDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start, false, false)

	case p.match(token.KeywordStruct):
		return p.parseStructDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start)

	case p.match(token.KeywordEnum):
		return p.parseEnumDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start)

	case p.match(token.KeywordUnion):
		return p.parseUnionDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start, false)

	case p.match(token.KeywordInterface):
		return p.parseInterfaceDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start)

	case p.match(token.KeywordImpl):
		return p.parseImplDecl(ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}, start)

	case p.match(token.KeywordOverload):
		return p.parseOverloadDecl(nameTok.Lexeme, start)

	default:
		value := p.parseExpr(0)
		if value == nil {
			return nil
		}

		return &ast.ConstDecl{
			Name:  ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span},
			Value: value,
			Loc:   p.span(start, value.Span().End),
		}
	}
}

func (p *Parser) parseDirectiveDecl(name ast.Ident, dir ast.Ident, start int) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after directive name") {
		return nil
	}

	var body []token.Token
	depth := 1

	for !p.at(token.EOF) && depth > 0 {
		tok := p.advance()

		if tok.Kind == token.LBrace {
			depth++
		}

		if tok.Kind == token.RBrace {
			depth--
			if depth == 0 {
				break
			}
		}

		body = append(body, tok)
	}

	end := p.previous().Span.End

	return &ast.DirectiveDecl{
		Name:      name,
		Directive: dir,
		Body:      body,
		Loc:       p.span(start, end),
	}
}

func (p *Parser) parseStructDecl(name ast.Ident, start int) ast.Decl {
	params := p.parseGenericParamsIfPresent()

	if !p.expect(token.LBrace, "expected '{' after struct declaration") {
		return nil
	}

	var fields []ast.Field

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		fieldName := p.expectIdent("expected field name")
		if fieldName.Name == "" {
			p.synchronizeDeclBody()
			continue
		}

		fieldType := p.parseType()
		if fieldType == nil {
			p.errorHere("expected field type")
			p.synchronizeDeclBody()
			continue
		}

		fields = append(fields, ast.Field{
			Name: fieldName,
			Type: fieldType,
		})
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after struct fields")

	return &ast.StructDecl{
		Name:   name,
		Params: params,
		Fields: fields,
		Loc:    p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseEnumDecl(name ast.Ident, start int) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after enum declaration") {
		return nil
	}

	var variants []ast.Ident

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		variant := p.expectIdent("expected enum variant")
		if variant.Name == "" {
			p.synchronizeDeclBody()
			continue
		}

		variants = append(variants, variant)
		p.match(token.Comma)
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after enum variants")

	return &ast.EnumDecl{
		Name:     name,
		Variants: variants,
		Loc:      p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseUnionDecl(name ast.Ident, start int, raw bool) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after union declaration") {
		return nil
	}

	var members []ast.Type

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		member := p.parseType()
		if member == nil {
			p.errorHere("expected union member type")
			p.synchronizeDeclBody()
			continue
		}

		members = append(members, member)
		p.match(token.Comma)
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after union members")

	return &ast.UnionDecl{
		Name:    name,
		Members: members,
		Raw:     raw,
		Loc:     p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseInterfaceDecl(name ast.Ident, start int) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after interface declaration") {
		return nil
	}

	var requirements []*ast.TaskSignature

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		reqStart := p.peek().Span.Start
		reqName := p.expectIdent("expected interface requirement name")
		if reqName.Name == "" {
			p.synchronizeDeclBody()
			continue
		}

		if !p.expect(token.ColonColon, "expected '::' after requirement name") {
			p.synchronizeDeclBody()
			continue
		}

		if !p.expect(token.KeywordTask, "expected 'task' in interface requirement") {
			p.synchronizeDeclBody()
			continue
		}

		params := p.parseParamList()
		results := p.parseInterfaceResultTypes()

		requirements = append(requirements, &ast.TaskSignature{
			Name:    reqName,
			Params:  params,
			Results: results,
			Loc:     p.span(reqStart, p.previous().Span.End),
		})
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after interface body")

	return &ast.InterfaceDecl{
		Name:         name,
		Requirements: requirements,
		Loc:          p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseImplDecl(typeName ast.Ident, start int) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after impl declaration") {
		return nil
	}

	var interfaces []ast.Type

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		t := p.parseType()
		if t == nil {
			p.errorHere("expected interface name in impl block")
			p.synchronizeDeclBody()
			continue
		}

		interfaces = append(interfaces, t)
		p.match(token.Comma)
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after impl block")

	return &ast.ImplDecl{
		TypeName:   typeName,
		Interfaces: interfaces,
		Loc:        p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseOverloadDecl(name string, start int) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after overload") {
		return nil
	}

	var names []ast.Ident

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		id := p.expectIdent("expected task name in overload")
		if id.Name == "" {
			p.synchronizeDeclBody()
			continue
		}

		names = append(names, id)
		p.match(token.Comma)
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after overload")

	return &ast.OverloadDecl{
		Name:  name,
		Names: names,
		Loc:   p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseTaskDecl(name ast.Ident, start int, isPure bool, isTest bool) ast.Decl {
	params := p.parseParamList()
	results := p.parseResultTypesUntilBodyOrDeclEnd()

	body := p.parseBlock()
	if body == nil {
		return nil
	}

	return &ast.TaskDecl{
		Name:    name,
		IsPure:  isPure,
		IsTest:  isTest,
		Params:  params,
		Results: results,
		Body:    body,
		Loc:     p.span(start, body.Span().End),
	}
}

func (p *Parser) parseGenericParamsIfPresent() []ast.GenericParam {
	if !p.match(token.LParen) {
		return nil
	}

	var params []ast.GenericParam

	for !p.at(token.RParen) && !p.at(token.EOF) {
		kind := ast.GenericTypeParam

		if p.match(token.Dollar) {
			kind = ast.GenericTypeParam
		} else if p.match(token.Hash) {
			kind = ast.GenericValueParam
		} else {
			p.errorHere("expected '$' or '#' in generic parameter")
			p.synchronizeUntil(token.Comma, token.RParen)
			p.match(token.Comma)
			continue
		}

		name := p.expectIdent("expected generic parameter name")
		if name.Name != "" {
			params = append(params, ast.GenericParam{
				Kind: kind,
				Name: name,
			})
		}

		if !p.match(token.Comma) {
			break
		}
	}

	p.expect(token.RParen, "expected ')' after generic parameters")
	return params
}

func (p *Parser) parseParamList() []ast.Param {
	if !p.expect(token.LParen, "expected '(' before parameter list") {
		return nil
	}

	var params []ast.Param

	for !p.at(token.RParen) && !p.at(token.EOF) {
		names := []ast.Ident{}

		firstName := p.expectIdent("expected parameter name")
		if firstName.Name == "" {
			p.synchronizeUntil(token.Comma, token.RParen)
			p.match(token.Comma)
			continue
		}

		names = append(names, firstName)

		for p.match(token.Comma) {
			if !p.at(token.Ident) {
				p.backup()
				break
			}

			// Grouped parameters: a, b int
			nextName := p.expectIdent("expected parameter name")
			names = append(names, nextName)
		}

		paramType := p.parseType()
		if paramType == nil {
			p.errorHere("expected parameter type")
			p.synchronizeUntil(token.Comma, token.RParen)
			p.match(token.Comma)
			continue
		}

		hasDefault := false
		var defaultValue ast.Expr

		if p.match(token.Assign) {
			hasDefault = true
			defaultValue = p.parseExpr(0)
		}

		for _, name := range names {
			params = append(params, ast.Param{
				Name:       name,
				Type:       paramType,
				HasDefault: hasDefault,
				Default:    defaultValue,
			})
		}

		p.match(token.Comma)
	}

	p.expect(token.RParen, "expected ')' after parameter list")
	return params
}

func (p *Parser) parseResultTypesUntilBodyOrDeclEnd() []ast.Type {
	var results []ast.Type

	for !p.at(token.LBrace) &&
		!p.at(token.RBrace) &&
		!p.at(token.EOF) {
		t := p.parseType()
		if t == nil {
			break
		}

		results = append(results, t)

		if !p.match(token.Comma) {
			break
		}
	}

	return results
}

func (p *Parser) parseType() ast.Type {
	start := p.peek().Span.Start

	var t ast.Type

	if p.match(token.Star) {
		elem := p.parseType()
		if elem == nil {
			p.errorHere("expected type after '*'")
			return nil
		}

		t = &ast.PointerType{
			Elem: elem,
			Loc:  p.span(start, elem.Span().End),
		}
	} else if p.match(token.LBracket) {
		inferred := false
		var length ast.Expr

		if p.match(token.Question) {
			inferred = true
		} else {
			length = p.parseExpr(0)
			if length == nil {
				p.errorHere("expected array length")
			}
		}

		p.expect(token.RBracket, "expected ']' after array length")

		elem := p.parseType()
		if elem == nil {
			p.errorHere("expected array element type")
			return nil
		}

		t = &ast.ArrayType{
			Len:      length,
			Inferred: inferred,
			Elem:     elem,
			Loc:      p.span(start, elem.Span().End),
		}
	} else if p.at(token.Dollar) {
		p.advance()
		name := p.expectIdent("expected type parameter name after '$'")
		t = &ast.NamedType{
			Parts: []ast.Ident{name},
			Loc:   p.span(start, name.Span().End),
		}
	} else if p.at(token.Ident) {
		parts := []ast.Ident{p.expectIdent("expected type name")}

		for p.match(token.Dot) {
			part := p.expectIdent("expected name after '.'")
			if part.Name == "" {
				return nil
			}

			parts = append(parts, part)
		}

		t = &ast.NamedType{
			Parts: parts,
			Loc:   p.span(start, parts[len(parts)-1].Span().End),
		}
	} else {
		return nil
	}

	if p.match(token.Lt) {
		var args []ast.Expr

		for !p.at(token.Gt) && !p.at(token.EOF) {
			arg := p.parseExpr(0)
			if arg == nil {
				p.errorHere("expected compile-time argument")
				break
			}

			args = append(args, arg)

			if !p.match(token.Comma) {
				break
			}
		}

		gt := p.expectToken(token.Gt, "expected '>' after compile-time arguments")

		t = &ast.GenericType{
			Base: t,
			Args: args,
			Loc:  p.span(start, gt.Span.End),
		}
	}

	return t
}

func (p *Parser) parseBlock() *ast.BlockStmt {
	startTok := p.expectToken(token.LBrace, "expected '{' before block")
	if startTok.Kind != token.LBrace {
		return nil
	}

	var stmts []ast.Stmt

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		stmt := p.parseStmt()
		if stmt != nil {
			stmts = append(stmts, stmt)
			continue
		}

		p.synchronizeStmt()
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after block")

	return &ast.BlockStmt{
		Stmts: stmts,
		Loc:   p.span(startTok.Span.Start, endTok.Span.End),
	}
}

func (p *Parser) parseStmt() ast.Stmt {
	start := p.peek().Span.Start

	switch {
	case p.match(token.KeywordReturn):
		var values []ast.Expr

		if !p.at(token.RBrace) && !p.at(token.EOF) {
			value := p.parseExpr(0)
			if value != nil {
				values = append(values, value)

				for p.match(token.Comma) {
					next := p.parseExpr(0)
					if next == nil {
						break
					}

					values = append(values, next)
				}
			}
		}

		end := p.previous().Span.End
		return &ast.ReturnStmt{
			Values: values,
			Loc:    p.span(start, end),
		}

	case p.match(token.KeywordDefer):
		call := p.parseExpr(0)
		if call == nil {
			p.errorHere("expected expression after defer")
			return nil
		}

		return &ast.DeferStmt{
			Call: call,
			Loc:  p.span(start, call.Span().End),
		}

	case p.match(token.KeywordSeal):
		target := p.parseExpr(0)
		if target == nil {
			p.errorHere("expected expression after seal")
			return nil
		}

		return &ast.SealStmt{
			Target: target,
			Loc:    p.span(start, target.Span().End),
		}

	case p.match(token.KeywordIf):
		cond := p.parseExpr(0)
		if cond == nil {
			p.errorHere("expected condition after if")
			return nil
		}

		then := p.parseBlock()
		if then == nil {
			return nil
		}

		var elseStmt ast.Stmt
		if p.match(token.KeywordElse) {
			if p.at(token.KeywordIf) {
				elseStmt = p.parseStmt()
			} else {
				elseStmt = p.parseBlock()
			}
		}

		end := then.Span().End
		if elseStmt != nil {
			end = elseStmt.Span().End
		}

		return &ast.IfStmt{
			Cond: cond,
			Then: then,
			Else: elseStmt,
			Loc:  p.span(start, end),
		}

	case p.match(token.KeywordFor):
		return p.parseForStmt(start)
	}

	return p.parseSimpleStmt()
}

func (p *Parser) parseForStmt(start int) ast.Stmt {
	if p.at(token.LBrace) {
		body := p.parseBlock()
		return &ast.ForStmt{
			Body: body,
			Loc:  p.span(start, body.Span().End),
		}
	}

	first := p.parseSimpleStmt()

	if p.match(token.Semi) {
		var cond ast.Expr
		var post ast.Stmt

		if !p.at(token.Semi) {
			cond = p.parseExpr(0)
		}

		p.expect(token.Semi, "expected ';' after for condition")

		if !p.at(token.LBrace) {
			post = p.parseSimpleStmt()
		}

		body := p.parseBlock()
		if body == nil {
			return nil
		}

		return &ast.ForStmt{
			Init: first,
			Cond: cond,
			Post: post,
			Body: body,
			Loc:  p.span(start, body.Span().End),
		}
	}

	condStmt, ok := first.(*ast.ExprStmt)
	if !ok {
		p.errorHere("expected condition or C-like for statement")
		return nil
	}

	body := p.parseBlock()
	if body == nil {
		return nil
	}

	return &ast.ForStmt{
		Cond: condStmt.Expr,
		Body: body,
		Loc:  p.span(start, body.Span().End),
	}
}

func (p *Parser) parseSimpleStmt() ast.Stmt {
	start := p.peek().Span.Start

	if p.at(token.Ident) && p.peekNext().Kind == token.ColonEq {
		name := p.expectIdent("expected variable name")
		p.expect(token.ColonEq, "expected ':='")
		value := p.parseExpr(0)
		if value == nil {
			p.errorHere("expected value after ':='")
			return nil
		}

		return &ast.VarDeclStmt{
			Name:     name,
			Value:    value,
			HasValue: true,
			Loc:      p.span(start, value.Span().End),
		}
	}

	if p.at(token.Ident) && p.peekNext().Kind == token.Colon {
		name := p.expectIdent("expected variable name")
		p.expect(token.Colon, "expected ':'")
		t := p.parseType()
		if t == nil {
			p.errorHere("expected type after ':'")
			return nil
		}

		var value ast.Expr
		hasValue := false
		end := t.Span().End

		if p.match(token.Assign) {
			hasValue = true
			value = p.parseExpr(0)
			if value == nil {
				p.errorHere("expected value after '='")
				return nil
			}

			end = value.Span().End
		}

		return &ast.VarDeclStmt{
			Name:     name,
			Type:     t,
			Value:    value,
			HasType:  true,
			HasValue: hasValue,
			Loc:      p.span(start, end),
		}
	}

	left := p.parseExpr(0)
	if left == nil {
		return nil
	}

	if p.isAssignOp(p.peek().Kind) {
		op := p.advance()
		right := p.parseExpr(0)
		if right == nil {
			p.errorHere("expected expression after assignment operator")
			return nil
		}

		return &ast.AssignStmt{
			Left:  left,
			Op:    op.Kind,
			Right: right,
			Loc:   p.span(start, right.Span().End),
		}
	}

	return &ast.ExprStmt{
		Expr: left,
		Loc:  left.Span(),
	}
}

func (p *Parser) parseExpr(minPrec int) ast.Expr {
	left := p.parsePrefix()
	if left == nil {
		return nil
	}

	for {
		if p.isPostfixStart(p.peek().Kind) {
			left = p.parsePostfix(left)
			continue
		}

		prec := p.binaryPrecedence(p.peek().Kind)
		if prec < minPrec {
			break
		}

		op := p.advance()
		right := p.parseExpr(prec + 1)
		if right == nil {
			p.errorHere("expected expression after binary operator")
			return left
		}

		left = &ast.BinaryExpr{
			Left:  left,
			Op:    op.Kind,
			Right: right,
			Loc:   p.span(left.Span().Start, right.Span().End),
		}
	}

	return left
}

func (p *Parser) parsePrefix() ast.Expr {
	start := p.peek().Span.Start

	switch {
	case p.match(token.Ident):
		id := ast.Ident{
			Name: p.previous().Lexeme,
			Loc:  p.previous().Span,
		}

		baseType := &ast.NamedType{
			Parts: []ast.Ident{id},
			Loc:   id.Span(),
		}

		if p.at(token.LBrace) {
			return p.parseCompoundLiteral(baseType, start)
		}

		return &ast.IdentExpr{Name: id}

	case p.match(token.IntLit):
		return &ast.IntLitExpr{
			Value: p.previous().Lexeme,
			Loc:   p.previous().Span,
		}

	case p.match(token.FloatLit):
		return &ast.FloatLitExpr{
			Value: p.previous().Lexeme,
			Loc:   p.previous().Span,
		}

	case p.match(token.StringLit):
		return &ast.StringLitExpr{
			Value: p.previous().Lexeme,
			Loc:   p.previous().Span,
		}

	case p.match(token.KeywordTrue):
		return &ast.BoolLitExpr{
			Value: true,
			Loc:   p.previous().Span,
		}

	case p.match(token.KeywordFalse):
		return &ast.BoolLitExpr{
			Value: false,
			Loc:   p.previous().Span,
		}

	case p.match(token.KeywordNil):
		return &ast.NilLitExpr{
			Loc: p.previous().Span,
		}

	case p.match(token.Dot):
		name := p.expectIdent("expected enum element after '.'")
		return &ast.DotIdentExpr{
			Name: name,
			Loc:  p.span(start, name.Span().End),
		}

	case p.match(token.LParen):
		expr := p.parseExpr(0)
		p.expect(token.RParen, "expected ')' after expression")
		return expr

	case p.match(token.LBracket):
		return p.parseArrayLiteral(start)

	case p.isUnaryOp(p.peek().Kind):
		op := p.advance()
		expr := p.parseExpr(7)
		if expr == nil {
			p.errorHere("expected expression after unary operator")
			return nil
		}

		return &ast.UnaryExpr{
			Op:   op.Kind,
			Expr: expr,
			Loc:  p.span(start, expr.Span().End),
		}
	}

	p.errorHere("expected expression")
	return nil
}

func (p *Parser) parsePostfix(left ast.Expr) ast.Expr {
	start := left.Span().Start

	switch {
	case p.match(token.LParen):
		var args []ast.Expr

		for !p.at(token.RParen) && !p.at(token.EOF) {
			arg := p.parseExpr(0)
			if arg == nil {
				break
			}

			args = append(args, arg)

			if !p.match(token.Comma) {
				break
			}
		}

		endTok := p.expectToken(token.RParen, "expected ')' after arguments")

		return &ast.CallExpr{
			Callee: left,
			Args:   args,
			Loc:    p.span(start, endTok.Span.End),
		}

	case p.match(token.Dot):
		name := p.expectIdent("expected field name after '.'")

		return &ast.SelectorExpr{
			Left: left,
			Name: name,
			Loc:  p.span(start, name.Span().End),
		}

	case p.match(token.LBracket):
		index := p.parseExpr(0)
		if index == nil {
			p.errorHere("expected index expression")
			return left
		}

		endTok := p.expectToken(token.RBracket, "expected ']' after index")

		return &ast.IndexExpr{
			Left:  left,
			Index: index,
			Loc:   p.span(start, endTok.Span.End),
		}
	}

	return left
}

func (p *Parser) parseArrayLiteral(start int) ast.Expr {
	var values []ast.Expr

	for !p.at(token.RBracket) && !p.at(token.EOF) {
		value := p.parseExpr(0)
		if value == nil {
			break
		}

		values = append(values, value)

		if !p.match(token.Comma) {
			break
		}
	}

	endTok := p.expectToken(token.RBracket, "expected ']' after array literal")

	return &ast.ArrayLiteralExpr{
		Values: values,
		Loc:    p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseCompoundLiteral(t ast.Type, start int) ast.Expr {
	p.expect(token.LBrace, "expected '{'")

	var fields []ast.LiteralField
	var values []ast.Expr

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		if p.at(token.Ident) && p.peekNext().Kind == token.Assign {
			name := p.expectIdent("expected field name")
			p.expect(token.Assign, "expected '=' after field name")

			value := p.parseExpr(0)
			if value == nil {
				break
			}

			fields = append(fields, ast.LiteralField{
				Name:  name,
				Value: value,
			})
		} else {
			value := p.parseExpr(0)
			if value == nil {
				break
			}

			values = append(values, value)
		}

		if !p.match(token.Comma) {
			break
		}
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after literal")

	return &ast.CompoundLiteralExpr{
		Type:   t,
		Fields: fields,
		Values: values,
		Loc:    p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseInterfaceResultTypes() []ast.Type {
	var results []ast.Type

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		// Next interface requirement starts here:
		//
		// Health :: task(...)
		//
		// So the previous requirement has no more result types.
		if p.at(token.Ident) && p.peekNext().Kind == token.ColonColon {
			break
		}

		t := p.parseType()
		if t == nil {
			break
		}

		results = append(results, t)

		if !p.match(token.Comma) {
			break
		}
	}

	return results
}

func (p *Parser) binaryPrecedence(kind token.Kind) int {
	switch kind {
	case token.OrOr:
		return 1
	case token.AndAnd:
		return 2
	case token.EqEq, token.NotEq, token.Lt, token.LtEq, token.Gt, token.GtEq:
		return 3
	case token.Plus, token.Minus, token.Pipe, token.Caret:
		return 4
	case token.Star, token.Slash, token.Percent, token.Amp:
		return 5
	default:
		return -1
	}
}

func (p *Parser) isUnaryOp(kind token.Kind) bool {
	switch kind {
	case token.Minus, token.Bang, token.Tilde, token.Amp, token.Star:
		return true
	default:
		return false
	}
}

func (p *Parser) isPostfixStart(kind token.Kind) bool {
	switch kind {
	case token.LParen, token.Dot, token.LBracket:
		return true
	default:
		return false
	}
}

func (p *Parser) isAssignOp(kind token.Kind) bool {
	switch kind {
	case token.Assign,
		token.PlusEq,
		token.MinusEq,
		token.StarEq,
		token.SlashEq,
		token.PercentEq:
		return true
	default:
		return false
	}
}

func (p *Parser) isDeclName(kind token.Kind) bool {
	if kind == token.Ident {
		return true
	}

	switch kind {
	case token.Plus,
		token.Minus,
		token.Star,
		token.Slash,
		token.Percent,
		token.EqEq,
		token.NotEq,
		token.Lt,
		token.LtEq,
		token.Gt,
		token.GtEq,
		token.Amp,
		token.Pipe,
		token.Caret:
		return true
	default:
		return false
	}
}

func (p *Parser) expectIdent(message string) ast.Ident {
	if !p.at(token.Ident) {
		p.errorHere(message)
		return ast.Ident{}
	}

	tok := p.advance()

	return ast.Ident{
		Name: tok.Lexeme,
		Loc:  tok.Span,
	}
}

func (p *Parser) expect(kind token.Kind, message string) bool {
	if p.at(kind) {
		p.advance()
		return true
	}

	p.errorHere(message)
	return false
}

func (p *Parser) expectToken(kind token.Kind, message string) token.Token {
	if p.at(kind) {
		return p.advance()
	}

	p.errorHere(message)

	return token.Token{
		Kind: kind,
		Span: p.peek().Span,
	}
}

func (p *Parser) match(kind token.Kind) bool {
	if !p.at(kind) {
		return false
	}

	p.advance()
	return true
}

func (p *Parser) at(kind token.Kind) bool {
	return p.peek().Kind == kind
}

func (p *Parser) peek() token.Token {
	if p.pos >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}

	return p.tokens[p.pos]
}

func (p *Parser) peekNext() token.Token {
	if p.pos+1 >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}

	return p.tokens[p.pos+1]
}

func (p *Parser) previous() token.Token {
	if p.pos == 0 {
		return p.peek()
	}

	return p.tokens[p.pos-1]
}

func (p *Parser) advance() token.Token {
	tok := p.peek()

	if p.pos < len(p.tokens) {
		p.pos++
	}

	return tok
}

func (p *Parser) backup() {
	if p.pos > 0 {
		p.pos--
	}
}

func (p *Parser) errorHere(message string) {
	p.diags.Add(p.peek().Span, message)
}

func (p *Parser) span(start int, end int) source.Span {
	file := p.peek().Span.File
	if file == nil && len(p.tokens) > 0 {
		file = p.tokens[0].Span.File
	}

	return source.NewSpan(file, start, end)
}

func (p *Parser) synchronizeTopLevel() {
	for !p.at(token.EOF) {
		if p.at(token.Ident) && p.peekNext().Kind == token.ColonColon {
			return
		}

		p.advance()
	}
}

func (p *Parser) synchronizeDeclBody() {
	for !p.at(token.EOF) && !p.at(token.RBrace) {
		if p.at(token.Ident) {
			return
		}

		p.advance()
	}
}

func (p *Parser) synchronizeStmt() {
	for !p.at(token.EOF) && !p.at(token.RBrace) {
		switch p.peek().Kind {
		case token.KeywordReturn,
			token.KeywordDefer,
			token.KeywordSeal,
			token.KeywordIf,
			token.KeywordFor,
			token.Ident:
			return
		}

		p.advance()
	}
}

func (p *Parser) synchronizeUntil(kinds ...token.Kind) {
	for !p.at(token.EOF) {
		for _, kind := range kinds {
			if p.at(kind) {
				return
			}
		}

		p.advance()
	}
}

func DebugSummary(file *ast.File) string {
	return fmt.Sprintf("decls=%d", len(file.Decls))
}
