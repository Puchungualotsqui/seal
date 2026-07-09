package parser

import (
	"fmt"
	"strings"

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

type declModifiers struct {
	Pure        bool
	Test        bool
	Intrinsic   bool
	TrustedPure bool
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

	var name ast.Ident
	var declHead ast.Type
	operatorName := ""

	if nameTok.Kind == token.Ident {
		p.advance()

		name = ast.Ident{Name: nameTok.Lexeme, Loc: nameTok.Span}
		declHead = &ast.NamedType{
			Parts: []ast.Ident{name},
			Loc:   name.Span(),
		}

		if p.at(token.Lt) {
			declHead = p.parseGenericTypeSuffix(declHead, start)
		}
	} else {
		p.advance()
		operatorName = nameTok.Lexeme
	}

	if !p.expect(token.ColonColon, "expected '::' after declaration name") {
		return nil
	}

	mods := declModifiers{}

	if p.match(token.At) {
		dir := p.expectIdent("expected directive name after '@'")
		if dir.Name == "" {
			return nil
		}

		switch dir.Name {
		case "rawUnion":
			if !p.expect(token.KeywordUnion, "expected 'union' after @rawUnion") {
				return nil
			}

			if operatorName != "" {
				p.errorHere("@rawUnion cannot be used with operator declaration")
				return nil
			}

			simpleName, ok := isSimpleNamedType(declHead)
			if !ok {
				p.errorHere("@rawUnion declaration name cannot be generic")
				return nil
			}

			return p.parseUnionDecl(simpleName, start, true)

		case "trusted_pure":
			mods.TrustedPure = true

		default:
			if operatorName != "" {
				p.errorHere("directive declaration name cannot be an operator")
				return nil
			}

			simpleName, ok := isSimpleNamedType(declHead)
			if !ok {
				p.errorHere("directive declaration name cannot be generic")
				return nil
			}

			return p.parseDirectiveDecl(simpleName, dir, start)
		}
	}

	for {
		switch {
		case p.match(token.KeywordPure):
			mods.Pure = true

		case p.match(token.KeywordIntrinsic):
			mods.Intrinsic = true

		case p.match(token.KeywordTest):
			mods.Test = true

		default:
			goto doneModifiers
		}
	}

doneModifiers:

	if mods.Test && mods.Pure {
		p.errorHere("test task cannot be marked pure")
		return nil
	}

	if mods.Test && mods.Intrinsic {
		p.errorHere("test task cannot be intrinsic")
		return nil
	}

	if p.at(token.Ident) && p.peek().Lexeme == "extern" {
		p.advance()

		if mods.Intrinsic {
			p.errorHere("extern task cannot be intrinsic")
			return nil
		}

		if operatorName != "" {
			p.errorHere("extern task name cannot be an operator")
			return nil
		}

		simpleName, ok := isSimpleNamedType(declHead)
		if !ok {
			p.errorHere("extern task name cannot be generic")
			return nil
		}

		return p.parseExternTaskDecl(simpleName, start, mods)
	}

	if mods.TrustedPure {
		p.errorHere("@trusted_pure can only be used before extern")
		return nil
	}

	if operatorName != "" {
		if mods.Pure {
			if !p.expect(token.KeywordTask, "expected 'task' after 'pure'") {
				return nil
			}

			return p.parseTaskDecl(ast.Ident{Name: operatorName, Loc: nameTok.Span}, start, true, false)
		}

		if p.match(token.KeywordOverload) {
			return p.parseOverloadDecl(operatorName, start)
		}

		p.errorHere("operator declaration must be overload or pure task")
		return nil
	}

	simpleName, simple := isSimpleNamedType(declHead)

	if mods.Intrinsic {
		if !simple {
			p.errorHere("intrinsic declaration name cannot be generic")
			return nil
		}

		switch {
		case p.match(token.KeywordTask):
			return p.parseIntrinsicTaskDecl(simpleName, start, mods)

		case p.match(token.KeywordStruct):
			return p.parseStructDecl(simpleName, start, true)

		default:
			p.errorHere("expected 'task' or 'struct' after intrinsic")
			return nil
		}
	}

	if mods.Test {
		if !simple {
			p.errorHere("test task name cannot be generic")
			return nil
		}

		if !p.expect(token.KeywordTask, "expected 'task' after 'test'") {
			return nil
		}

		return p.parseTaskDecl(simpleName, start, false, true)
	}

	if mods.Pure {
		if !simple {
			p.errorHere("pure task name cannot be generic")
			return nil
		}

		if !p.expect(token.KeywordTask, "expected 'task' after 'pure'") {
			return nil
		}

		return p.parseTaskDecl(simpleName, start, true, false)
	}

	switch {
	case p.match(token.KeywordDistinct):
		if !simple {
			p.errorHere("distinct declaration name cannot be generic")
			return nil
		}

		return p.parseDistinctDecl(simpleName, start)

	case p.match(token.KeywordTask):
		if !simple {
			p.errorHere("task declaration name cannot be generic")
			return nil
		}

		return p.parseTaskDecl(simpleName, start, false, false)

	case p.match(token.KeywordStruct):
		if !simple {
			p.errorHere("struct declaration name cannot be generic")
			return nil
		}

		return p.parseStructDecl(simpleName, start, false)

	case p.match(token.KeywordEnum):
		if !simple {
			p.errorHere("enum declaration name cannot be generic")
			return nil
		}

		return p.parseEnumDecl(simpleName, start)

	case p.match(token.KeywordUnion):
		if !simple {
			p.errorHere("union declaration name cannot be generic")
			return nil
		}

		return p.parseUnionDecl(simpleName, start, false)

	case p.match(token.KeywordDyn):
		if !simple {
			p.errorHere("dyn interface declaration name cannot be generic")
			return nil
		}

		if !p.expect(token.KeywordInterface, "expected 'interface' after 'dyn'") {
			return nil
		}

		return p.parseInterfaceDecl(simpleName, start, true)

	case p.match(token.KeywordInterface):
		if !simple {
			p.errorHere("interface declaration name cannot be generic")
			return nil
		}

		return p.parseInterfaceDecl(simpleName, start, false)

	case p.match(token.KeywordImpl):
		return p.parseImplDecl(declHead, start)

	case p.match(token.KeywordOverload):
		if !simple {
			p.errorHere("overload declaration name cannot be generic")
			return nil
		}

		return p.parseOverloadDecl(simpleName.Name, start)

	default:
		if !simple {
			p.errorHere("constant declaration name cannot be generic")
			return nil
		}

		value := p.parseExpr(0)
		if value == nil {
			return nil
		}

		return &ast.ConstDecl{
			Name:  simpleName,
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

func (p *Parser) parseStructDecl(name ast.Ident, start int, intrinsic bool) ast.Decl {
	genericParams := p.parseGenericParamsIfPresent()

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
		Name:          name,
		GenericParams: genericParams,
		Fields:        fields,
		IsIntrinsic:   intrinsic,
		Loc:           p.span(start, endTok.Span.End),
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

func (p *Parser) parseInterfaceDecl(name ast.Ident, start int, isDyn bool) ast.Decl {
	genericParams := p.parseGenericParamsIfPresent()

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
		Name:          name,
		GenericParams: genericParams,
		IsDyn:         isDyn,
		Requirements:  requirements,
		Loc:           p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseImplDecl(interfaceType ast.Type, start int) ast.Decl {
	if !p.expect(token.LBrace, "expected '{' after impl declaration") {
		return nil
	}

	var entries []ast.ImplEntry

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		entryStart := p.peek().Span.Start

		name := p.expectIdent("expected impl entry name")
		if name.Name == "" {
			p.synchronizeDeclBody()
			continue
		}

		if !p.expect(token.ColonColon, "expected '::' after impl entry name") {
			p.synchronizeDeclBody()
			continue
		}

		if p.match(token.KeywordTask) {
			params := p.parseParamList()
			results := p.parseResultTypesUntilBodyOrDeclEnd()
			body := p.parseBlock()
			if body == nil {
				p.synchronizeDeclBody()
				continue
			}

			task := &ast.TaskDecl{
				Name:    name,
				Params:  params,
				Results: results,
				Body:    body,
				Loc:     p.span(entryStart, body.Span().End),
			}

			entries = append(entries, ast.ImplEntry{
				Name: name,
				Task: task,
				Loc:  task.Span(),
			})

			continue
		}

		alias := p.parseExpr(0)
		if alias == nil {
			p.errorHere("expected task alias or inline task implementation")
			p.synchronizeDeclBody()
			continue
		}

		entries = append(entries, ast.ImplEntry{
			Name:  name,
			Alias: alias,
			Loc:   p.span(entryStart, alias.Span().End),
		})

		p.match(token.Comma)
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after impl block")

	return &ast.ImplDecl{
		Interface: interfaceType,
		Entries:   entries,
		Loc:       p.span(start, endTok.Span.End),
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

func (p *Parser) parseExternTaskDecl(name ast.Ident, start int, mods declModifiers) ast.Decl {
	if !p.expect(token.LParen, "expected '(' after extern") {
		return nil
	}

	cNameTok := p.expectToken(token.StringLit, "expected C symbol name string in extern")
	if cNameTok.Kind != token.StringLit {
		return nil
	}

	if !p.expect(token.RParen, "expected ')' after extern name") {
		return nil
	}

	if !p.expect(token.KeywordTask, "expected 'task' after extern(...)") {
		return nil
	}

	params := p.parseParamList()
	results := p.parseExternResultTypes()

	end := p.previous().Span.End

	return &ast.TaskDecl{
		Name:          name,
		IsPure:        mods.Pure || mods.TrustedPure,
		IsExtern:      true,
		IsTrustedPure: mods.TrustedPure,
		ExternName:    strings.Trim(cNameTok.Lexeme, `"`),
		Params:        params,
		Results:       results,
		Body:          nil,
		Loc:           p.span(start, end),
	}
}

func (p *Parser) parseIntrinsicTaskDecl(name ast.Ident, start int, mods declModifiers) ast.Decl {
	genericParams := p.parseGenericParamsIfPresent()
	params := p.parseParamList()
	results := p.parseExternResultTypes()

	end := p.previous().Span.End

	return &ast.TaskDecl{
		Name:          name,
		GenericParams: genericParams,
		IsPure:        mods.Pure,
		IsIntrinsic:   true,
		Params:        params,
		Results:       results,
		Body:          nil,
		Loc:           p.span(start, end),
	}
}

func (p *Parser) parseDistinctDecl(name ast.Ident, start int) ast.Decl {
	underlying := p.parseType()
	if underlying == nil {
		p.errorHere("expected underlying type after distinct")
		return nil
	}

	return &ast.DistinctDecl{
		Name:       name,
		Underlying: underlying,
		Loc:        p.span(start, underlying.Span().End),
	}
}

func (p *Parser) parseTaskDecl(name ast.Ident, start int, isPure bool, isTest bool) ast.Decl {
	genericParams := p.parseGenericParamsIfPresent()
	params := p.parseParamList()
	results := p.parseResultTypesUntilBodyOrDeclEnd()

	body := p.parseBlock()
	if body == nil {
		return nil
	}

	return &ast.TaskDecl{
		Name:          name,
		GenericParams: genericParams,
		IsPure:        isPure,
		IsTest:        isTest,
		Params:        params,
		Results:       results,
		Body:          body,
		Loc:           p.span(start, body.Span().End),
	}
}

func (p *Parser) parseGenericParamsIfPresent() []ast.GenericParam {
	if !p.match(token.Lt) {
		return nil
	}

	var params []ast.GenericParam

	for !p.at(token.Gt) && !p.at(token.EOF) {
		param := p.parseGenericParam()
		if param.Name.Name != "" {
			params = append(params, param)
		}

		if !p.match(token.Comma) {
			break
		}
	}

	p.expect(token.Gt, "expected '>' after generic parameters")
	return params
}

func (p *Parser) parseGenericParam() ast.GenericParam {
	start := p.peek().Span.Start

	name := p.expectIdent("expected generic parameter name")
	if name.Name == "" {
		p.synchronizeUntil(token.Comma, token.Gt)
		return ast.GenericParam{}
	}

	category := ast.GenericParamInvalid
	var paramType ast.Type

	switch {
	case p.match(token.KeywordType):
		category = ast.GenericParamType

	case p.match(token.KeywordEnum):
		category = ast.GenericParamEnum

	case p.match(token.KeywordUnion):
		category = ast.GenericParamUnion

	case p.match(token.KeywordTask):
		category = ast.GenericParamTask

	case p.at(token.Ident) && p.peek().Lexeme == "int":
		p.advance()
		category = ast.GenericParamInt

	case p.at(token.Ident) && p.peek().Lexeme == "bool":
		p.advance()
		category = ast.GenericParamBool

	case p.at(token.Ident) && p.peek().Lexeme == "string":
		p.advance()
		category = ast.GenericParamString

	default:
		paramType = p.parseType()
		if paramType == nil {
			p.errorHere("expected generic parameter category or comptime value type")
			p.synchronizeUntil(token.Comma, token.Gt)
			return ast.GenericParam{}
		}

		category = ast.GenericParamValue
	}

	constraints := p.parseGenericConstraintsIfPresent(category)

	end := name.Span().End
	if len(constraints) > 0 {
		end = constraints[len(constraints)-1].Span().End
	} else if paramType != nil {
		end = paramType.Span().End
	} else if p.pos > 0 {
		end = p.previous().Span.End
	}

	return ast.GenericParam{
		Name:        name,
		Category:    category,
		Type:        paramType,
		Constraints: constraints,
		Loc:         p.span(start, end),
	}
}

func (p *Parser) parseGenericConstraintsIfPresent(category ast.GenericParamCategory) []ast.GenericConstraint {
	if !p.match(token.LBracket) {
		return nil
	}

	var constraints []ast.GenericConstraint

	switch category {
	case ast.GenericParamType:
		constraints = p.parseTypeGenericConstraints()

	case ast.GenericParamEnum:
		constraints = p.parseEnumGenericConstraints()

	case ast.GenericParamUnion:
		constraints = p.parseUnionGenericConstraints()

	case ast.GenericParamTask:
		constraint := p.parseTaskGenericConstraint()
		if constraint != nil {
			constraints = append(constraints, constraint)
		}

	case ast.GenericParamInt,
		ast.GenericParamBool,
		ast.GenericParamString,
		ast.GenericParamValue:
		constraints = p.parseExprGenericConstraints()

	default:
		p.errorHere("invalid generic constraint category")
		p.synchronizeUntil(token.RBracket)
	}

	p.expect(token.RBracket, "expected ']' after generic constraints")
	return constraints
}

func (p *Parser) parseExprGenericConstraints() []ast.GenericConstraint {
	var constraints []ast.GenericConstraint

	for !p.at(token.RBracket) && !p.at(token.EOF) {
		start := p.peek().Span.Start
		expr := p.parseExprUntil(0, token.Comma, token.RBracket)
		if expr == nil {
			break
		}

		constraints = append(constraints, &ast.GenericExprConstraint{
			Expr: expr,
			Loc:  p.span(start, expr.Span().End),
		})

		if !p.match(token.Comma) {
			break
		}
	}

	return constraints
}

func (p *Parser) parseTypeGenericConstraints() []ast.GenericConstraint {
	var constraints []ast.GenericConstraint

	for !p.at(token.RBracket) && !p.at(token.EOF) {
		start := p.peek().Span.Start

		name := p.expectIdent("expected field or interface requirement")
		if name.Name == "" {
			break
		}

		if p.at(token.Lt) {
			base := &ast.NamedType{
				Parts: []ast.Ident{name},
				Loc:   name.Span(),
			}

			iface := p.parseGenericTypeSuffix(base, start)

			if p.expect(token.LParen, "expected '(' after interface requirement") {
				p.expect(token.RParen, "expected ')' after interface requirement")
			}

			constraints = append(constraints, &ast.GenericImplConstraint{
				Interface: iface,
				Loc:       p.span(start, p.previous().Span.End),
			})
		} else if p.match(token.LParen) {
			p.expect(token.RParen, "expected ')' after interface requirement")

			iface := &ast.NamedType{
				Parts: []ast.Ident{name},
				Loc:   name.Span(),
			}

			constraints = append(constraints, &ast.GenericImplConstraint{
				Interface: iface,
				Loc:       p.span(start, p.previous().Span.End),
			})
		} else {
			hasType := false
			var fieldType ast.Type
			end := name.Span().End

			if !p.at(token.Comma) && !p.at(token.RBracket) {
				fieldType = p.parseType()
				if fieldType != nil {
					hasType = true
					end = fieldType.Span().End
				}
			}

			constraints = append(constraints, &ast.GenericFieldConstraint{
				Name:    name,
				Type:    fieldType,
				HasType: hasType,
				Loc:     p.span(start, end),
			})
		}

		if !p.match(token.Comma) {
			break
		}
	}

	return constraints
}

func (p *Parser) parseEnumGenericConstraints() []ast.GenericConstraint {
	var constraints []ast.GenericConstraint

	for !p.at(token.RBracket) && !p.at(token.EOF) {
		name := p.expectIdent("expected enum variant requirement")
		if name.Name == "" {
			break
		}

		constraints = append(constraints, &ast.GenericEnumVariantConstraint{
			Name: name,
			Loc:  name.Span(),
		})

		if !p.match(token.Comma) {
			break
		}
	}

	return constraints
}

func (p *Parser) parseUnionGenericConstraints() []ast.GenericConstraint {
	var constraints []ast.GenericConstraint

	for !p.at(token.RBracket) && !p.at(token.EOF) {
		start := p.peek().Span.Start

		member := p.parseType()
		if member == nil {
			p.errorHere("expected union member type requirement")
			break
		}

		constraints = append(constraints, &ast.GenericUnionMemberConstraint{
			Member: member,
			Loc:    p.span(start, member.Span().End),
		})

		if !p.match(token.Comma) {
			break
		}
	}

	return constraints
}

func (p *Parser) parseTaskGenericConstraint() ast.GenericConstraint {
	start := p.peek().Span.Start

	if !p.expect(token.LParen, "expected '(' before task constraint parameters") {
		return nil
	}

	var params []ast.Type

	for !p.at(token.RParen) && !p.at(token.EOF) {
		param := p.parseType()
		if param == nil {
			p.errorHere("expected task constraint parameter type")
			break
		}

		params = append(params, param)

		if !p.match(token.Comma) {
			break
		}
	}

	p.expect(token.RParen, "expected ')' after task constraint parameters")

	var results []ast.Type

	for !p.at(token.RBracket) && !p.at(token.EOF) {
		result := p.parseType()
		if result == nil {
			break
		}

		results = append(results, result)

		if !p.match(token.Comma) {
			break
		}
	}

	return &ast.GenericTaskConstraint{
		Params:  params,
		Results: results,
		Loc:     p.span(start, p.previous().Span.End),
	}
}

func (p *Parser) parseGenericArgsUntil(end token.Kind) []ast.GenericArg {
	var args []ast.GenericArg

	for !p.at(end) && !p.at(token.EOF) {
		arg := p.parseGenericArg()
		if arg.Kind != ast.GenericArgInvalid {
			args = append(args, arg)
		}

		if !p.match(token.Comma) {
			break
		}
	}

	return args
}

func (p *Parser) parseGenericArg() ast.GenericArg {
	start := p.peek().Span.Start

	switch p.peek().Kind {
	case token.Star, token.LBracket:
		t := p.parseType()
		if t == nil {
			return ast.GenericArg{}
		}

		return ast.GenericArg{
			Kind: ast.GenericArgType,
			Type: t,
			Loc:  p.span(start, t.Span().End),
		}

	case token.Ident:
		// Builtin type arguments:
		//
		//     Box<int>
		//
		// User-defined generic names are intentionally parsed as expressions:
		//
		//     Box<Pair<int, string>>
		//     UseTask<Identity<int>>
		//
		// The checker later decides whether the expression is a type
		// specialization or a task specialization from the expected generic
		// parameter category.
		if isBuiltinGenericTypeName(p.peek().Lexeme) {
			t := p.parseType()
			if t == nil {
				return ast.GenericArg{}
			}

			return ast.GenericArg{
				Kind: ast.GenericArgType,
				Type: t,
				Loc:  p.span(start, t.Span().End),
			}
		}

		if p.genericArgHasGenericSuffix() {
			expr := p.parseGenericArgNameExpr()
			if expr == nil {
				return ast.GenericArg{}
			}

			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: expr,
				Loc:  p.span(start, expr.Span().End),
			}
		}
	}

	expr := p.parseExprUntil(0, token.Comma, token.Gt)
	if expr == nil {
		p.errorHere("expected generic argument")
		return ast.GenericArg{}
	}

	return ast.GenericArg{
		Kind: ast.GenericArgExpr,
		Expr: expr,
		Loc:  p.span(start, expr.Span().End),
	}
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

			nextName := p.expectIdent("expected parameter name")
			names = append(names, nextName)
		}

		isVariadic := false
		if p.match(token.Ellipsis) {
			isVariadic = true
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

		for i, name := range names {
			paramIsVariadic := isVariadic && i == len(names)-1

			params = append(params, ast.Param{
				Name:       name,
				Type:       paramType,
				IsVariadic: paramIsVariadic,
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

func (p *Parser) parseExternResultTypes() []ast.Type {
	var results []ast.Type

	for !p.at(token.RBrace) &&
		!p.at(token.EOF) {
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

func (p *Parser) parseType() ast.Type {
	start := p.peek().Span.Start

	var t ast.Type

	switch {
	case p.match(token.Star):
		elem := p.parseType()
		if elem == nil {
			p.errorHere("expected type after '*'")
			return nil
		}

		t = &ast.PointerType{
			Elem: elem,
			Loc:  p.span(start, elem.Span().End),
		}

	case p.match(token.LBracket):
		inferred := false
		var length ast.Expr

		if p.match(token.RBracket) {
			inferred = true
		} else {
			length = p.parseExpr(0)
			if length == nil {
				p.errorHere("expected array length or ']'")
			}

			p.expect(token.RBracket, "expected ']' after array length")
		}

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

	case p.at(token.Ident):
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

	default:
		return nil
	}

	if p.at(token.Lt) {
		t = p.parseGenericTypeSuffix(t, start)
	}

	return t
}

func (p *Parser) parseGenericTypeSuffix(base ast.Type, start int) ast.Type {
	p.expect(token.Lt, "expected '<' before generic arguments")

	args := p.parseGenericArgsUntil(token.Gt)

	gt := p.expectToken(token.Gt, "expected '>' after generic arguments")

	return &ast.GenericType{
		Base: base,
		Args: args,
		Loc:  p.span(start, gt.Span.End),
	}
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

	if p.at(token.Ident) && p.peekNext().Kind == token.ColonColon {
		decl := p.parseDecl()
		if decl == nil {
			return nil
		}

		return &ast.DeclStmt{
			Decl: decl,
			Loc:  decl.Span(),
		}
	}

	if p.at(token.LBrace) {
		return p.parseBlock()
	}

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

	case p.match(token.At):
		dir := p.expectIdent("expected directive name after '@'")
		if dir.Name != "partial" {
			p.errorHere("expected 'partial' directive before switch")
			return nil
		}

		if !p.expect(token.KeywordSwitch, "expected 'switch' after @partial") {
			return nil
		}

		return p.parseSwitchStmt(start, true)

	case p.match(token.KeywordSwitch):
		return p.parseSwitchStmt(start, false)
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

func (p *Parser) parseSwitchStmt(start int, isPartial bool) ast.Stmt {
	first := p.parseSwitchHeadExpr()
	if first == nil {
		p.errorHere("expected expression after switch")
		return nil
	}

	isUnionSwitch := false
	isTypeSwitch := false
	var bindName ast.Ident
	target := first

	if id, ok := first.(*ast.IdentExpr); ok {
		if p.at(token.Ident) && p.peek().Lexeme == "in" {
			p.advance()

			isUnionSwitch = true
			bindName = id.Name

			target = p.parseSwitchHeadExpr()
			if target == nil {
				p.errorHere("expected union expression after 'in'")
				return nil
			}
		}
	}

	if !isUnionSwitch && (p.at(token.KeywordType) || (p.at(token.Ident) && p.peek().Lexeme == "type")) {
		p.advance()
		isTypeSwitch = true
	}

	if !p.expect(token.LBrace, "expected '{' after switch expression") {
		return nil
	}

	var cases []ast.SwitchCase

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		if p.match(token.KeywordCase) {
			cases = append(cases, p.parseSwitchCase(isUnionSwitch, isTypeSwitch))
			continue
		}

		if p.match(token.KeywordDefault) {
			cases = append(cases, p.parseDefaultSwitchCase())
			continue
		}

		p.errorHere("expected switch case or default")
		p.synchronizeSwitchCase()
	}

	endTok := p.expectToken(token.RBrace, "expected '}' after switch")

	return &ast.SwitchStmt{
		BindName:      bindName,
		Target:        target,
		IsUnionSwitch: isUnionSwitch,
		IsTypeSwitch:  isTypeSwitch,
		IsPartial:     isPartial,
		Cases:         cases,
		Loc:           p.span(start, endTok.Span.End),
	}
}

func (p *Parser) parseSwitchCase(isUnionSwitch bool, isTypeSwitch bool) ast.SwitchCase {
	start := p.previous().Span.Start

	var c ast.SwitchCase

	if p.match(token.KeywordNil) {
		c.Kind = ast.SwitchCaseNil
	} else if p.match(token.Dot) {
		name := p.expectIdent("expected enum variant after '.'")
		c.Kind = ast.SwitchCaseEnumVariant
		c.EnumVariant = name
	} else if isUnionSwitch || isTypeSwitch {
		member := p.parseType()
		if member == nil {
			p.errorHere("expected type in switch case")
		}

		c.Kind = ast.SwitchCaseUnionMember
		c.UnionMember = member
	} else {
		expr := p.parseExpr(0)
		if expr == nil {
			p.errorHere("expected case expression")
		}

		c.Kind = ast.SwitchCaseExpr
		c.Expr = expr
	}

	p.expect(token.Colon, "expected ':' after switch case")

	c.Body = p.parseSwitchCaseBody()

	end := p.previous().Span.End
	if len(c.Body) > 0 {
		end = c.Body[len(c.Body)-1].Span().End
	}

	c.Loc = p.span(start, end)
	return c
}

func (p *Parser) parseDefaultSwitchCase() ast.SwitchCase {
	start := p.previous().Span.Start

	p.expect(token.Colon, "expected ':' after default")

	body := p.parseSwitchCaseBody()

	end := p.previous().Span.End
	if len(body) > 0 {
		end = body[len(body)-1].Span().End
	}

	return ast.SwitchCase{
		Kind: ast.SwitchCaseDefault,
		Body: body,
		Loc:  p.span(start, end),
	}
}

func (p *Parser) parseSwitchCaseBody() []ast.Stmt {
	var stmts []ast.Stmt

	for !p.at(token.KeywordCase) &&
		!p.at(token.KeywordDefault) &&
		!p.at(token.RBrace) &&
		!p.at(token.EOF) {
		stmt := p.parseStmt()
		if stmt != nil {
			stmts = append(stmts, stmt)
			continue
		}

		p.synchronizeStmt()
	}

	return stmts
}

func (p *Parser) synchronizeSwitchCase() {
	for !p.at(token.EOF) &&
		!p.at(token.KeywordCase) &&
		!p.at(token.KeywordDefault) &&
		!p.at(token.RBrace) {
		p.advance()
	}
}

func (p *Parser) looksLikeMultiVarDeclStmt() bool {
	if !p.at(token.Ident) {
		return false
	}

	i := p.pos + 1
	seenComma := false

	for i < len(p.tokens) && p.tokens[i].Kind == token.Comma {
		seenComma = true
		i++

		if i >= len(p.tokens) || p.tokens[i].Kind != token.Ident {
			return false
		}

		i++
	}

	return seenComma && i < len(p.tokens) && p.tokens[i].Kind == token.ColonEq
}

func (p *Parser) parseSimpleStmt() ast.Stmt {
	start := p.peek().Span.Start

	if p.looksLikeMultiVarDeclStmt() {
		var names []ast.Ident

		first := p.expectIdent("expected variable name")
		names = append(names, first)

		for p.match(token.Comma) {
			name := p.expectIdent("expected variable name after ','")
			if name.Name == "" {
				return nil
			}

			names = append(names, name)
		}

		p.expect(token.ColonEq, "expected ':='")

		value := p.parseExpr(0)
		if value == nil {
			p.errorHere("expected value after ':='")
			return nil
		}

		return &ast.MultiVarDeclStmt{
			Names: names,
			Value: value,
			Loc:   p.span(start, value.Span().End),
		}
	}

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
	return p.parseExprUntil(minPrec)
}

func (p *Parser) parseExprUntil(minPrec int, stops ...token.Kind) ast.Expr {
	left := p.parsePrefix()
	if left == nil {
		return nil
	}

	for {
		if p.atAny(stops...) {
			break
		}

		if p.at(token.Lt) && p.looksLikeGenericExpr(left) {
			left = p.parseGenericExpr(left)
			continue
		}

		// `{` is only postfix when the left side can become a type.
		// Otherwise it belongs to an if/for/switch/block.
		if p.at(token.LBrace) {
			if p.typeFromExprForLiteral(left) == nil {
				break
			}

			left = p.parsePostfix(left)
			continue
		}

		if p.isPostfixStart(p.peek().Kind) {
			left = p.parsePostfix(left)
			continue
		}

		prec := p.binaryPrecedence(p.peek().Kind)
		if prec < minPrec {
			break
		}

		op := p.advance()
		right := p.parseExprUntil(prec+1, stops...)
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

func (p *Parser) atAny(kinds ...token.Kind) bool {
	for _, kind := range kinds {
		if p.at(kind) {
			return true
		}
	}

	return false
}

func (p *Parser) parsePrefix() ast.Expr {
	start := p.peek().Span.Start

	switch {
	case p.match(token.Ident):
		id := ast.Ident{
			Name: p.previous().Lexeme,
			Loc:  p.previous().Span,
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

	case p.match(token.CStringLit):
		return &ast.CStringLitExpr{
			Value: p.previous().Lexeme,
			Loc:   p.previous().Span,
		}

	case p.match(token.CharLit):
		return &ast.CharLitExpr{
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

func (p *Parser) looksLikeGenericExpr(left ast.Expr) bool {
	switch left.(type) {
	case *ast.IdentExpr, *ast.SelectorExpr:
	default:
		return false
	}

	if !p.at(token.Lt) {
		return false
	}

	angleDepth := 0
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for i := p.pos; i < len(p.tokens); i++ {
		kind := p.tokens[i].Kind

		switch kind {
		case token.Lt:
			angleDepth++

		case token.Gt:
			if parenDepth > 0 || bracketDepth > 0 || braceDepth > 0 {
				continue
			}

			angleDepth--
			if angleDepth == 0 {
				if i+1 < len(p.tokens) &&
					(p.tokens[i+1].Kind == token.LParen || p.tokens[i+1].Kind == token.LBrace) {
					return true
				}

				return false
			}

		case token.LParen:
			parenDepth++

		case token.RParen:
			if parenDepth == 0 {
				return false
			}
			parenDepth--

		case token.LBracket:
			bracketDepth++

		case token.RBracket:
			if bracketDepth == 0 {
				return false
			}
			bracketDepth--

		case token.LBrace:
			braceDepth++

		case token.RBrace:
			if braceDepth == 0 {
				return false
			}
			braceDepth--

		case token.EOF:
			return false
		}
	}

	return false
}

func (p *Parser) genericArgHasGenericSuffix() bool {
	if !p.at(token.Ident) {
		return false
	}

	i := p.pos + 1

	for i+1 < len(p.tokens) &&
		p.tokens[i].Kind == token.Dot &&
		p.tokens[i+1].Kind == token.Ident {
		i += 2
	}

	return i < len(p.tokens) && p.tokens[i].Kind == token.Lt
}

func (p *Parser) parseGenericArgNameExpr() ast.Expr {
	start := p.peek().Span.Start

	name := p.expectIdent("expected generic argument name")
	if name.Name == "" {
		return nil
	}

	var expr ast.Expr = &ast.IdentExpr{Name: name}

	for p.match(token.Dot) {
		part := p.expectIdent("expected name after '.'")
		if part.Name == "" {
			return expr
		}

		expr = &ast.SelectorExpr{
			Left: expr,
			Name: part,
			Loc:  p.span(start, part.Span().End),
		}
	}

	if p.at(token.Lt) {
		expr = p.parseGenericExpr(expr)
	}

	for {
		switch p.peek().Kind {
		case token.LParen, token.Dot, token.LBracket:
			expr = p.parsePostfix(expr)

		default:
			return expr
		}
	}
}

func (p *Parser) parseGenericExpr(base ast.Expr) ast.Expr {
	start := base.Span().Start

	p.expect(token.Lt, "expected '<' before generic arguments")

	args := p.parseGenericArgsUntil(token.Gt)

	gt := p.expectToken(token.Gt, "expected '>' after generic arguments")

	return &ast.GenericExpr{
		Base: base,
		Args: args,
		Loc:  p.span(start, gt.Span.End),
	}
}

func (p *Parser) parseCallArgs() []ast.Expr {
	var args []ast.Expr

	for !p.at(token.RParen) && !p.at(token.EOF) {
		arg := p.parseExpr(0)
		if arg == nil {
			break
		}

		if p.match(token.Ellipsis) {
			arg = &ast.SpreadExpr{
				Expr: arg,
				Loc:  p.span(arg.Span().Start, p.previous().Span.End),
			}
		}

		args = append(args, arg)

		if !p.match(token.Comma) {
			break
		}
	}

	return args
}

func (p *Parser) parsePostfix(left ast.Expr) ast.Expr {
	start := left.Span().Start

	switch {
	case p.match(token.LParen):
		args := p.parseCallArgs()
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

	case p.match(token.LBrace):
		t := p.typeFromExprForLiteral(left)
		if t == nil {
			p.errorHere("compound literal requires a type name")
			return left
		}

		return p.parseCompoundLiteralAfterLBrace(t, start)

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

func (p *Parser) typeFromExprForLiteral(expr ast.Expr) ast.Type {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return &ast.NamedType{
			Parts: []ast.Ident{e.Name},
			Loc:   e.Name.Span(),
		}

	case *ast.SelectorExpr:
		var parts []ast.Ident

		current := expr
		for {
			switch x := current.(type) {
			case *ast.SelectorExpr:
				parts = append([]ast.Ident{x.Name}, parts...)
				current = x.Left

			case *ast.IdentExpr:
				parts = append([]ast.Ident{x.Name}, parts...)
				return &ast.NamedType{
					Parts: parts,
					Loc:   p.span(expr.Span().Start, expr.Span().End),
				}

			default:
				return nil
			}
		}

	case *ast.GenericExpr:
		base := p.typeFromExprForLiteral(e.Base)
		if base == nil {
			return nil
		}

		return &ast.GenericType{
			Base: base,
			Args: e.Args,
			Loc:  e.Loc,
		}
	}

	return nil
}

func (p *Parser) parseCompoundLiteralAfterLBrace(t ast.Type, start int) ast.Expr {
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

func (p *Parser) parseCompoundLiteral(t ast.Type, start int) ast.Expr {
	p.expect(token.LBrace, "expected '{'")
	return p.parseCompoundLiteralAfterLBrace(t, start)
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

func isBuiltinGenericTypeName(name string) bool {
	switch name {
	case "bool",
		"int", "uint",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f32", "f64",
		"char",
		"rawptr",
		"any",
		"string",
		"cstring":
		return true

	default:
		return false
	}
}

func isSimpleNamedType(t ast.Type) (ast.Ident, bool) {
	named, ok := t.(*ast.NamedType)
	if !ok || len(named.Parts) != 1 {
		return ast.Ident{}, false
	}

	return named.Parts[0], true
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

func (p *Parser) parseSwitchHeadExpr() ast.Expr {
	start := p.peek().Span.Start

	// Special case:
	//
	//     switch err {
	//
	// The normal expression parser sees `err { ... }` and thinks this is a
	// compound literal. In a switch head, `{` starts the switch body, not a
	// literal.
	if p.at(token.Ident) {
		id := ast.Ident{
			Name: p.peek().Lexeme,
			Loc:  p.peek().Span,
		}
		p.advance()

		var expr ast.Expr = &ast.IdentExpr{Name: id}

		for {
			if p.at(token.Lt) && p.looksLikeGenericExpr(expr) {
				expr = p.parseGenericExpr(expr)
				continue
			}

			switch p.peek().Kind {
			case token.LParen, token.Dot, token.LBracket:
				expr = p.parsePostfix(expr)

			default:
				return expr
			}
		}
	}

	// Non-ident expressions can use the normal parser.
	// This supports things like:
	//
	//     switch GetError() { ... }
	//     switch 1 + 2 { ... }
	//
	// The important ambiguous case is bare Ident followed by `{`.
	expr := p.parseExpr(0)
	if expr == nil {
		p.diags.Add(source.NewSpan(p.peek().Span.File, start, p.peek().Span.End), "expected switch expression")
	}

	return expr
}

func DebugSummary(file *ast.File) string {
	return fmt.Sprintf("decls=%d", len(file.Decls))
}
