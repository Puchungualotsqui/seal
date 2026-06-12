package checker

import (
	"fmt"
	"strings"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

type TypeKind int

const (
	TypeInvalid TypeKind = iota
	TypeVoid
	TypeBool
	TypeInt
	TypeF32
	TypeF64
	TypeString
	TypeNil
	TypeEnumLiteral

	TypeUntypedInt
	TypeUntypedFloat

	TypePointer
	TypeArray
	TypeStruct
	TypeEnum
	TypeUnion
	TypeInterface
	TypeTask
	TypePackage
	TypeTypeParam
	TypeValueParam
)

type Type struct {
	Kind TypeKind
	Name string

	Elem *Type

	Len      int
	Inferred bool

	Fields   []FieldInfo
	Variants []EnumVariantInfo
	Members  []*Type

	Params  []*Type
	Results []*Type
}

type EnumVariantInfo struct {
	Name string
	Span source.Span
}

type FieldInfo struct {
	Name string
	Type *Type
	Span source.Span
}

func (t *Type) String() string {
	if t == nil {
		return "<nil>"
	}

	switch t.Kind {
	case TypeInvalid:
		return "<invalid>"
	case TypeVoid:
		return "void"
	case TypeBool:
		return "bool"
	case TypeInt:
		return "int"
	case TypeF32:
		return "f32"
	case TypeF64:
		return "f64"
	case TypeString:
		return "string"
	case TypeUntypedInt:
		return "untyped int"
	case TypeUntypedFloat:
		return "untyped float"
	case TypePointer:
		return "*" + t.Elem.String()
	case TypeArray:
		if t.Inferred {
			return "[?]" + t.Elem.String()
		}

		return fmt.Sprintf("[%d]%s", t.Len, t.Elem.String())
	case TypeStruct, TypeEnum, TypeUnion, TypeInterface, TypeTypeParam, TypeValueParam:
		return t.Name
	case TypeTask:
		var params []string
		for _, p := range t.Params {
			params = append(params, p.String())
		}

		var results []string
		for _, r := range t.Results {
			results = append(results, r.String())
		}

		if len(results) == 0 {
			return fmt.Sprintf("task(%s)", strings.Join(params, ", "))
		}

		return fmt.Sprintf("task(%s) %s", strings.Join(params, ", "), strings.Join(results, ", "))
	case TypePackage:
		return "package " + t.Name
	case TypeNil:
		return "nil"
	case TypeEnumLiteral:
		return "." + t.Name
	default:
		return "<unknown>"
	}
}

var (
	InvalidType      = &Type{Kind: TypeInvalid, Name: "<invalid>"}
	VoidType         = &Type{Kind: TypeVoid, Name: "void"}
	BoolType         = &Type{Kind: TypeBool, Name: "bool"}
	IntType          = &Type{Kind: TypeInt, Name: "int"}
	F32Type          = &Type{Kind: TypeF32, Name: "f32"}
	F64Type          = &Type{Kind: TypeF64, Name: "f64"}
	StringType       = &Type{Kind: TypeString, Name: "string"}
	NilType          = &Type{Kind: TypeNil, Name: "nil"}
	UntypedIntType   = &Type{Kind: TypeUntypedInt, Name: "untyped int"}
	UntypedFloatType = &Type{Kind: TypeUntypedFloat, Name: "untyped float"}
)

type SymbolKind int

const (
	SymbolInvalid SymbolKind = iota
	SymbolConst
	SymbolVar
	SymbolParam
	SymbolType
	SymbolTask
	SymbolPackage
)

type Symbol struct {
	Name string
	Kind SymbolKind
	Type *Type
	Span source.Span
	Node ast.Node
}

type Scope struct {
	Parent  *Scope
	Symbols map[string]*Symbol
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Parent:  parent,
		Symbols: map[string]*Symbol{},
	}
}

func (s *Scope) LookupLocal(name string) *Symbol {
	return s.Symbols[name]
}

func (s *Scope) Lookup(name string) *Symbol {
	for scope := s; scope != nil; scope = scope.Parent {
		if sym := scope.LookupLocal(name); sym != nil {
			return sym
		}
	}

	return nil
}

func (s *Scope) Declare(sym *Symbol) {
	s.Symbols[sym.Name] = sym
}

type Checker struct {
	diags *diag.Reporter

	global *Scope

	currentResults []*Type
}

func New(diags *diag.Reporter) *Checker {
	c := &Checker{
		diags: diags,
	}

	c.global = NewScope(nil)
	c.declareBuiltins(c.global)

	return c
}

func (c *Checker) CheckFile(file *ast.File) *Scope {
	for _, decl := range file.Decls {
		c.declareDecl(c.global, decl)
	}

	for _, decl := range file.Decls {
		c.prepareDecl(c.global, decl)
	}

	for _, decl := range file.Decls {
		c.checkDecl(c.global, decl)
	}

	return c.global
}

func (c *Checker) declareBuiltins(scope *Scope) {
	builtinTypes := map[string]*Type{
		"void":   VoidType,
		"bool":   BoolType,
		"int":    IntType,
		"f32":    F32Type,
		"f64":    F64Type,
		"string": StringType,
	}

	for name, typ := range builtinTypes {
		scope.Declare(&Symbol{
			Name: name,
			Kind: SymbolType,
			Type: typ,
		})
	}

	scope.Declare(&Symbol{
		Name: "Assert",
		Kind: SymbolTask,
		Type: &Type{
			Kind:    TypeTask,
			Name:    "Assert",
			Params:  []*Type{BoolType},
			Results: nil,
		},
	})

	scope.Declare(&Symbol{
		Name: "Print",
		Kind: SymbolTask,
		Type: &Type{
			Kind:    TypeTask,
			Name:    "Print",
			Params:  nil,
			Results: nil,
		},
	})
}

func (c *Checker) declareDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolConst,
			Type: InvalidType,
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.StructDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolType,
			Type: &Type{
				Kind: TypeStruct,
				Name: d.Name.Name,
			},
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.EnumDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolType,
			Type: &Type{
				Kind: TypeEnum,
				Name: d.Name.Name,
			},
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.UnionDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolType,
			Type: &Type{
				Kind: TypeUnion,
				Name: d.Name.Name,
			},
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.InterfaceDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolType,
			Type: &Type{
				Kind: TypeInterface,
				Name: d.Name.Name,
			},
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.TaskDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolTask,
			Type: InvalidType,
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.DirectiveDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolPackage,
			Type: &Type{
				Kind: TypePackage,
				Name: d.Name.Name,
			},
			Span: d.Name.Span(),
			Node: d,
		})

	case *ast.ImplDecl:
		return

	case *ast.OverloadDecl:
		// Later phase.
		return
	}
}

func (c *Checker) prepareDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.StructDecl:
		c.prepareStructDecl(scope, d)

	case *ast.TaskDecl:
		sym := scope.LookupLocal(d.Name.Name)
		if sym != nil {
			sym.Type = c.taskTypeFromDecl(scope, d)
		}

	case *ast.InterfaceDecl:
		c.prepareInterfaceDecl(scope, d)

	case *ast.UnionDecl:
		for _, member := range d.Members {
			c.typeFromAst(scope, member)
		}
	}
}

func (c *Checker) prepareStructDecl(parent *Scope, d *ast.StructDecl) {
	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil {
		return
	}

	scope := NewScope(parent)
	c.declareGenericParams(scope, d.Params)

	var fields []FieldInfo

	for _, field := range d.Fields {
		fieldType := c.typeFromAst(scope, field.Type)

		fields = append(fields, FieldInfo{
			Name: field.Name.Name,
			Type: fieldType,
			Span: field.Name.Span(),
		})
	}

	sym.Type.Fields = fields
}

func (c *Checker) prepareInterfaceDecl(parent *Scope, d *ast.InterfaceDecl) {
	scope := NewScope(parent)

	scope.Declare(&Symbol{
		Name: "T",
		Kind: SymbolType,
		Type: &Type{
			Kind: TypeTypeParam,
			Name: "T",
		},
		Span: d.Name.Span(),
	})

	for _, req := range d.Requirements {
		for _, param := range req.Params {
			c.typeFromAst(scope, param.Type)
		}

		for _, result := range req.Results {
			c.typeFromAst(scope, result)
		}
	}
}

func (c *Checker) declareGenericParams(scope *Scope, params []ast.GenericParam) {
	for _, param := range params {
		switch param.Kind {
		case ast.GenericTypeParam:
			scope.Declare(&Symbol{
				Name: param.Name.Name,
				Kind: SymbolType,
				Type: &Type{
					Kind: TypeTypeParam,
					Name: param.Name.Name,
				},
				Span: param.Name.Span(),
			})

		case ast.GenericValueParam:
			scope.Declare(&Symbol{
				Name: param.Name.Name,
				Kind: SymbolConst,
				Type: &Type{
					Kind: TypeValueParam,
					Name: param.Name.Name,
				},
				Span: param.Name.Span(),
			})
		}
	}
}

func (c *Checker) checkDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		typ := c.defaultType(c.checkExpr(scope, d.Value))
		if sym := scope.LookupLocal(d.Name.Name); sym != nil {
			sym.Type = typ
		}

	case *ast.StructDecl:
		// Already prepared.

	case *ast.EnumDecl:
		// Later phase.

	case *ast.UnionDecl:
		// Later phase.

	case *ast.InterfaceDecl:
		// Later phase.

	case *ast.ImplDecl:
		// Later phase.

	case *ast.OverloadDecl:
		// Later phase.

	case *ast.DirectiveDecl:
		return

	case *ast.TaskDecl:
		c.checkTaskDecl(scope, d)
	}
}

func (c *Checker) checkTaskDecl(parent *Scope, d *ast.TaskDecl) {
	taskSym := parent.LookupLocal(d.Name.Name)
	if taskSym == nil {
		return
	}

	taskType := taskSym.Type
	if taskType == nil || taskType.Kind != TypeTask {
		taskType = c.taskTypeFromDecl(parent, d)
		taskSym.Type = taskType
	}

	taskScope := NewScope(parent)

	for i, param := range d.Params {
		paramType := InvalidType
		if i < len(taskType.Params) {
			paramType = taskType.Params[i]
		}

		taskScope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolParam,
			Type: paramType,
			Span: param.Name.Span(),
			Node: d,
		})

		if param.HasDefault {
			defaultType := c.checkExpr(parent, param.Default)
			c.checkAssignable(paramType, defaultType, param.Default.Span())
		}
	}

	oldResults := c.currentResults
	c.currentResults = taskType.Results

	c.checkBlockInScope(taskScope, d.Body, false)

	c.currentResults = oldResults
}

func (c *Checker) taskTypeFromDecl(scope *Scope, d *ast.TaskDecl) *Type {
	var params []*Type
	for _, param := range d.Params {
		params = append(params, c.typeFromAst(scope, param.Type))
	}

	var results []*Type
	for _, result := range d.Results {
		results = append(results, c.typeFromAst(scope, result))
	}

	return &Type{
		Kind:    TypeTask,
		Name:    d.Name.Name,
		Params:  params,
		Results: results,
	}
}

func (c *Checker) checkBlockInScope(scope *Scope, block *ast.BlockStmt, createChild bool) {
	if block == nil {
		return
	}

	blockScope := scope
	if createChild {
		blockScope = NewScope(scope)
	}

	for _, stmt := range block.Stmts {
		c.checkStmt(blockScope, stmt)
	}
}

func (c *Checker) checkStmt(scope *Scope, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		c.declareDecl(scope, s.Decl)
		c.prepareDecl(scope, s.Decl)
		c.checkDecl(scope, s.Decl)

	case *ast.BlockStmt:
		c.checkBlockInScope(scope, s, true)

	case *ast.ReturnStmt:
		c.checkReturnStmt(scope, s)

	case *ast.DeferStmt:
		c.checkExpr(scope, s.Call)

	case *ast.SealStmt:
		c.checkExpr(scope, s.Target)

	case *ast.ExprStmt:
		c.checkExpr(scope, s.Expr)

	case *ast.AssignStmt:
		c.checkAssignStmt(scope, s)

	case *ast.VarDeclStmt:
		c.checkVarDeclStmt(scope, s)

	case *ast.IfStmt:
		cond := c.checkExpr(scope, s.Cond)
		c.checkBoolCondition(cond, s.Cond.Span(), "if condition must be bool")

		c.checkBlockInScope(scope, s.Then, true)

		if s.Else != nil {
			c.checkStmt(scope, s.Else)
		}

	case *ast.ForStmt:
		forScope := NewScope(scope)

		if s.Init != nil {
			c.checkStmt(forScope, s.Init)
		}

		if s.Cond != nil {
			cond := c.checkExpr(forScope, s.Cond)
			c.checkBoolCondition(cond, s.Cond.Span(), "for condition must be bool")
		}

		if s.Post != nil {
			c.checkStmt(forScope, s.Post)
		}

		c.checkBlockInScope(forScope, s.Body, true)

	case *ast.SwitchStmt:
		c.checkSwitchStmt(scope, s)
	}
}

func (c *Checker) checkReturnStmt(scope *Scope, s *ast.ReturnStmt) {
	expected := c.currentResults

	if len(s.Values) != len(expected) {
		c.diags.Add(
			s.Span(),
			fmt.Sprintf("return count mismatch: expected %d value(s), got %d", len(expected), len(s.Values)),
		)

		for _, value := range s.Values {
			c.checkExpr(scope, value)
		}

		return
	}

	for i, value := range s.Values {
		got := c.checkExpr(scope, value)
		c.checkAssignable(expected[i], got, value.Span())
	}
}

func (c *Checker) checkAssignStmt(scope *Scope, s *ast.AssignStmt) {
	if id, ok := s.Left.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)

		if sym != nil {
			if sym.Kind == SymbolParam {
				c.diags.Add(id.Span(), fmt.Sprintf("cannot reassign parameter %q", id.Name.Name))
			}

			if sym.Kind == SymbolConst {
				c.diags.Add(id.Span(), fmt.Sprintf("cannot assign to constant %q", id.Name.Name))
			}

			if sym.Kind == SymbolTask || sym.Kind == SymbolType || sym.Kind == SymbolPackage {
				c.diags.Add(id.Span(), fmt.Sprintf("cannot assign to %q", id.Name.Name))
			}
		}
	}

	leftType := c.checkExpr(scope, s.Left)
	rightType := c.checkExpr(scope, s.Right)

	c.checkAssignable(leftType, rightType, s.Right.Span())
}

func (c *Checker) checkVarDeclStmt(scope *Scope, s *ast.VarDeclStmt) {
	var varType *Type

	if s.HasType {
		varType = c.typeFromAst(scope, s.Type)
	}

	if s.HasValue {
		valueType := c.checkExpr(scope, s.Value)

		if s.HasType {
			c.checkAssignable(varType, valueType, s.Value.Span())
		} else {
			if valueType.Kind == TypeEnumLiteral {
				c.diags.Add(s.Value.Span(), fmt.Sprintf("enum literal .%s needs explicit type", valueType.Name))
				varType = InvalidType
			} else if valueType.Kind == TypeNil {
				c.diags.Add(s.Value.Span(), "nil needs explicit type")
				varType = InvalidType
			} else {
				varType = c.defaultType(valueType)
			}
		}
	}

	if varType == nil {
		varType = InvalidType
	}

	scope.Declare(&Symbol{
		Name: s.Name.Name,
		Kind: SymbolVar,
		Type: varType,
		Span: s.Name.Span(),
		Node: s,
	})
}

func (c *Checker) checkExpr(scope *Scope, expr ast.Expr) *Type {
	if expr == nil {
		return InvalidType
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(e.Name.Name)
		if sym == nil {
			c.diags.Add(e.Span(), fmt.Sprintf("undefined symbol %q", e.Name.Name))
			return InvalidType
		}

		return sym.Type

	case *ast.DotIdentExpr:
		return &Type{
			Kind: TypeEnumLiteral,
			Name: e.Name.Name,
		}

	case *ast.IntLitExpr:
		return UntypedIntType

	case *ast.FloatLitExpr:
		return UntypedFloatType

	case *ast.StringLitExpr:
		return StringType

	case *ast.BoolLitExpr:
		return BoolType

	case *ast.NilLitExpr:
		return NilType

	case *ast.UnaryExpr:
		return c.checkUnaryExpr(scope, e)

	case *ast.BinaryExpr:
		return c.checkBinaryExpr(scope, e)

	case *ast.CallExpr:
		return c.checkCallExpr(scope, e)

	case *ast.SelectorExpr:
		return c.checkSelectorExpr(scope, e)

	case *ast.IndexExpr:
		return c.checkIndexExpr(scope, e)

	case *ast.ArrayLiteralExpr:
		return c.checkArrayLiteralExpr(scope, e)

	case *ast.CompoundLiteralExpr:
		return c.checkCompoundLiteralExpr(scope, e)
	}

	return InvalidType
}

func (c *Checker) checkUnaryExpr(scope *Scope, e *ast.UnaryExpr) *Type {
	typ := c.checkExpr(scope, e.Expr)

	switch e.Op {
	case token.Minus:
		if !c.isNumeric(typ) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator '-' requires numeric type, got %s", typ.String()))
			return InvalidType
		}

		return typ

	case token.Bang:
		if !c.sameType(typ, BoolType) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator '!' requires bool, got %s", typ.String()))
			return InvalidType
		}

		return BoolType

	case token.Amp:
		return &Type{
			Kind: TypePointer,
			Elem: typ,
		}

	case token.Star:
		if typ.Kind != TypePointer {
			c.diags.Add(e.Span(), fmt.Sprintf("cannot dereference non-pointer type %s", typ.String()))
			return InvalidType
		}

		return typ.Elem

	case token.Tilde:
		if !c.isIntegerLike(typ) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator '~' requires integer type, got %s", typ.String()))
			return InvalidType
		}

		return typ
	}

	return InvalidType
}

func (c *Checker) checkBinaryExpr(scope *Scope, e *ast.BinaryExpr) *Type {
	left := c.checkExpr(scope, e.Left)
	right := c.checkExpr(scope, e.Right)

	switch e.Op {
	case token.Plus, token.Minus, token.Star, token.Slash:
		if !c.isNumeric(left) || !c.isNumeric(right) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires numeric operands", e.Op.String()))
			return InvalidType
		}

		result, ok := c.numericResultType(left, right)
		if !ok {
			c.diags.Add(e.Span(), fmt.Sprintf("mismatched numeric operands: %s and %s", left.String(), right.String()))
			return InvalidType
		}

		return result

	case token.EqEq, token.NotEq:
		if !c.assignableEitherWay(left, right) {
			c.diags.Add(e.Span(), fmt.Sprintf("cannot compare %s and %s", left.String(), right.String()))
			return InvalidType
		}

		return BoolType

	case token.Lt, token.Gt, token.LtEq, token.GtEq:
		if !c.isNumeric(left) || !c.isNumeric(right) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires numeric operands", e.Op.String()))
			return InvalidType
		}

		_, ok := c.numericResultType(left, right)
		if !ok {
			c.diags.Add(e.Span(), fmt.Sprintf("mismatched numeric operands: %s and %s", left.String(), right.String()))
			return InvalidType
		}

		return BoolType

	case token.AndAnd, token.OrOr:
		if !c.sameType(left, BoolType) || !c.sameType(right, BoolType) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires bool operands", e.Op.String()))
			return InvalidType
		}

		return BoolType
	}

	return InvalidType
}

func (c *Checker) checkCallExpr(scope *Scope, e *ast.CallExpr) *Type {
	calleeType := c.checkExpr(scope, e.Callee)

	if calleeType.Kind != TypeTask {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("cannot call non-task type %s", calleeType.String()))

		for _, arg := range e.Args {
			c.checkExpr(scope, arg)
		}

		return InvalidType
	}

	minArgs := 0
	for _, param := range calleeType.Params {
		if param != nil {
			minArgs++
		}
	}

	// Default parameters are parsed, but their full metadata is not stored in Type yet.
	// For now, require exact count.
	if len(e.Args) != len(calleeType.Params) {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf("task call argument count mismatch: expected %d, got %d", len(calleeType.Params), len(e.Args)),
		)
	}

	count := len(e.Args)
	if len(calleeType.Params) < count {
		count = len(calleeType.Params)
	}

	for i := 0; i < count; i++ {
		argType := c.checkExpr(scope, e.Args[i])
		c.checkAssignable(calleeType.Params[i], argType, e.Args[i].Span())
	}

	for i := count; i < len(e.Args); i++ {
		c.checkExpr(scope, e.Args[i])
	}

	if len(calleeType.Results) == 0 {
		return VoidType
	}

	if len(calleeType.Results) == 1 {
		return calleeType.Results[0]
	}

	c.diags.Add(e.Span(), "multi-result task call cannot be used as a single expression yet")
	return InvalidType
}

func (c *Checker) checkSelectorExpr(scope *Scope, e *ast.SelectorExpr) *Type {
	leftType := c.checkExpr(scope, e.Left)

	if leftType.Kind == TypePointer {
		leftType = leftType.Elem
	}

	if leftType.Kind != TypeStruct {
		c.diags.Add(e.Span(), fmt.Sprintf("cannot access field %q on non-struct type %s", e.Name.Name, leftType.String()))
		return InvalidType
	}

	for _, field := range leftType.Fields {
		if field.Name == e.Name.Name {
			return field.Type
		}
	}

	c.diags.Add(e.Name.Span(), fmt.Sprintf("type %s has no field %q", leftType.String(), e.Name.Name))
	return InvalidType
}

func (c *Checker) checkIndexExpr(scope *Scope, e *ast.IndexExpr) *Type {
	leftType := c.checkExpr(scope, e.Left)
	indexType := c.checkExpr(scope, e.Index)

	if !c.assignable(IntType, indexType) {
		c.diags.Add(e.Index.Span(), fmt.Sprintf("array index must be int, got %s", indexType.String()))
	}

	if leftType.Kind != TypeArray {
		c.diags.Add(e.Left.Span(), fmt.Sprintf("cannot index non-array type %s", leftType.String()))
		return InvalidType
	}

	return leftType.Elem
}

func (c *Checker) checkArrayLiteralExpr(scope *Scope, e *ast.ArrayLiteralExpr) *Type {
	if len(e.Values) == 0 {
		return &Type{
			Kind: TypeArray,
			Len:  0,
			Elem: InvalidType,
		}
	}

	firstType := c.defaultType(c.checkExpr(scope, e.Values[0]))

	for i := 1; i < len(e.Values); i++ {
		itemType := c.checkExpr(scope, e.Values[i])
		c.checkAssignable(firstType, itemType, e.Values[i].Span())
	}

	return &Type{
		Kind: TypeArray,
		Len:  len(e.Values),
		Elem: firstType,
	}
}

func (c *Checker) checkCompoundLiteralExpr(scope *Scope, e *ast.CompoundLiteralExpr) *Type {
	litType := c.typeFromAst(scope, e.Type)

	if litType.Kind != TypeStruct {
		for _, field := range e.Fields {
			c.checkExpr(scope, field.Value)
		}

		for _, value := range e.Values {
			c.checkExpr(scope, value)
		}

		return litType
	}

	if len(e.Fields) > 0 {
		for _, field := range e.Fields {
			fieldType := c.lookupField(litType, field.Name.Name)
			if fieldType == nil {
				c.diags.Add(field.Name.Span(), fmt.Sprintf("type %s has no field %q", litType.String(), field.Name.Name))
				c.checkExpr(scope, field.Value)
				continue
			}

			valueType := c.checkExpr(scope, field.Value)
			c.checkAssignable(fieldType, valueType, field.Value.Span())
		}
	}

	if len(e.Values) > 0 {
		if len(e.Values) > len(litType.Fields) {
			c.diags.Add(e.Span(), fmt.Sprintf("too many values in %s literal", litType.String()))
		}

		count := len(e.Values)
		if count > len(litType.Fields) {
			count = len(litType.Fields)
		}

		for i := 0; i < count; i++ {
			valueType := c.checkExpr(scope, e.Values[i])
			c.checkAssignable(litType.Fields[i].Type, valueType, e.Values[i].Span())
		}
	}

	return litType
}

func (c *Checker) lookupField(typ *Type, name string) *Type {
	if typ == nil {
		return nil
	}

	for _, field := range typ.Fields {
		if field.Name == name {
			return field.Type
		}
	}

	return nil
}

func (c *Checker) typeFromAst(scope *Scope, typ ast.Type) *Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 0 {
			return InvalidType
		}

		if len(t.Parts) > 1 {
			// Package-qualified types later.
			first := t.Parts[0]
			sym := scope.Lookup(first.Name)
			if sym == nil {
				c.diags.Add(first.Span(), fmt.Sprintf("undefined type or package %q", first.Name))
				return InvalidType
			}

			return InvalidType
		}

		name := t.Parts[0]
		sym := scope.Lookup(name.Name)
		if sym == nil {
			c.diags.Add(name.Span(), fmt.Sprintf("undefined type %q", name.Name))
			return InvalidType
		}

		if sym.Kind != SymbolType {
			c.diags.Add(name.Span(), fmt.Sprintf("%q is not a type", name.Name))
			return InvalidType
		}

		return sym.Type

	case *ast.PointerType:
		return &Type{
			Kind: TypePointer,
			Elem: c.typeFromAst(scope, t.Elem),
		}

	case *ast.ArrayType:
		lenValue := -1

		if !t.Inferred && t.Len != nil {
			if lit, ok := t.Len.(*ast.IntLitExpr); ok {
				var parsed int
				_, err := fmt.Sscanf(lit.Value, "%d", &parsed)
				if err == nil {
					lenValue = parsed
				}
			}

			lenType := c.checkExpr(scope, t.Len)
			if !c.assignable(IntType, lenType) {
				c.diags.Add(t.Len.Span(), fmt.Sprintf("array length must be int, got %s", lenType.String()))
			}
		}

		return &Type{
			Kind:     TypeArray,
			Len:      lenValue,
			Inferred: t.Inferred,
			Elem:     c.typeFromAst(scope, t.Elem),
		}

	case *ast.GenericType:
		// Full generic type instantiation comes in a later phase.
		return c.typeFromAst(scope, t.Base)
	}

	return InvalidType
}

func (c *Checker) checkAssignable(dst *Type, src *Type, span source.Span) {
	if dst == nil || src == nil {
		return
	}

	if dst.Kind == TypeInvalid || src.Kind == TypeInvalid {
		return
	}

	if dst.Kind == TypeEnum && src.Kind == TypeEnumLiteral {
		if !c.enumHasVariant(dst, src.Name) {
			c.diags.Add(span, fmt.Sprintf("enum %s has no variant .%s", dst.String(), src.Name))
		}
		return
	}

	if src.Kind == TypeEnumLiteral {
		c.diags.Add(span, fmt.Sprintf("enum literal .%s needs contextual enum type", src.Name))
		return
	}

	if dst.Kind == TypeUnion {
		if src.Kind == TypeNil {
			return
		}

		if c.unionHasMember(dst, src) {
			return
		}

		c.diags.Add(span, fmt.Sprintf("cannot assign %s to union %s", src.String(), dst.String()))
		return
	}

	if src.Kind == TypeNil {
		if dst.Kind == TypePointer {
			return
		}

		c.diags.Add(span, fmt.Sprintf("cannot assign nil to %s", dst.String()))
		return
	}

	if !c.assignable(dst, src) {
		c.diags.Add(span, fmt.Sprintf("cannot assign %s to %s", src.String(), dst.String()))
	}
}

func (c *Checker) assignable(dst *Type, src *Type) bool {
	if dst == nil || src == nil {
		return true
	}

	if dst.Kind == TypeInvalid || src.Kind == TypeInvalid {
		return true
	}

	if dst.Kind == TypeEnum && src.Kind == TypeEnumLiteral {
		return c.enumHasVariant(dst, src.Name)
	}

	if dst.Kind == TypeUnion {
		return src.Kind == TypeNil || c.unionHasMember(dst, src)
	}

	if src.Kind == TypeNil {
		return dst.Kind == TypePointer || dst.Kind == TypeUnion
	}

	if c.sameType(dst, src) {
		return true
	}

	if src.Kind == TypeUntypedInt {
		return dst.Kind == TypeInt || dst.Kind == TypeF32 || dst.Kind == TypeF64
	}

	if src.Kind == TypeUntypedFloat {
		return dst.Kind == TypeF32 || dst.Kind == TypeF64
	}

	if dst.Kind == TypeArray && src.Kind == TypeArray {
		if !dst.Inferred && src.Len >= 0 && dst.Len >= 0 && dst.Len != src.Len {
			return false
		}

		return c.assignable(dst.Elem, src.Elem)
	}

	return false
}

func (c *Checker) assignableEitherWay(a *Type, b *Type) bool {
	return c.assignable(a, b) || c.assignable(b, a)
}

func (c *Checker) sameType(a *Type, b *Type) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind == TypeInvalid || b.Kind == TypeInvalid {
		return true
	}

	if a.Kind != b.Kind {
		return false
	}

	switch a.Kind {
	case TypePointer:
		return c.sameType(a.Elem, b.Elem)

	case TypeArray:
		if a.Inferred || b.Inferred {
			return c.sameType(a.Elem, b.Elem)
		}

		return a.Len == b.Len && c.sameType(a.Elem, b.Elem)

	case TypeStruct, TypeEnum, TypeUnion, TypeInterface, TypeTypeParam, TypeValueParam:
		return a.Name == b.Name

	case TypeTask:
		if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
			return false
		}

		for i := range a.Params {
			if !c.sameType(a.Params[i], b.Params[i]) {
				return false
			}
		}

		for i := range a.Results {
			if !c.sameType(a.Results[i], b.Results[i]) {
				return false
			}
		}

		return true

	default:
		return true
	}
}

func (c *Checker) defaultType(t *Type) *Type {
	if t == nil {
		return InvalidType
	}

	switch t.Kind {
	case TypeUntypedInt:
		return IntType
	case TypeUntypedFloat:
		return F64Type
	case TypeEnumLiteral:
		return InvalidType
	case TypeNil:
		return InvalidType
	default:
		return t
	}
}

func (c *Checker) isNumeric(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeInt, TypeF32, TypeF64, TypeUntypedInt, TypeUntypedFloat:
		return true
	default:
		return false
	}
}

func (c *Checker) isIntegerLike(t *Type) bool {
	if t == nil {
		return false
	}

	return t.Kind == TypeInt || t.Kind == TypeUntypedInt
}

func (c *Checker) numericResultType(a *Type, b *Type) (*Type, bool) {
	if a.Kind == TypeInvalid || b.Kind == TypeInvalid {
		return InvalidType, true
	}

	if a.Kind == TypeUntypedInt && b.Kind == TypeUntypedInt {
		return UntypedIntType, true
	}

	if a.Kind == TypeUntypedFloat && b.Kind == TypeUntypedFloat {
		return UntypedFloatType, true
	}

	if a.Kind == TypeUntypedInt && c.isNumeric(b) {
		return b, true
	}

	if b.Kind == TypeUntypedInt && c.isNumeric(a) {
		return a, true
	}

	if a.Kind == TypeUntypedFloat && (b.Kind == TypeF32 || b.Kind == TypeF64) {
		return b, true
	}

	if b.Kind == TypeUntypedFloat && (a.Kind == TypeF32 || a.Kind == TypeF64) {
		return a, true
	}

	if c.sameType(a, b) {
		return a, true
	}

	return InvalidType, false
}

func (c *Checker) checkBoolCondition(t *Type, span source.Span, message string) {
	if t == nil || t.Kind == TypeInvalid {
		return
	}

	if !c.sameType(t, BoolType) {
		c.diags.Add(span, fmt.Sprintf("%s, got %s", message, t.String()))
	}
}

func (c *Checker) prepareEnumDecl(parent *Scope, d *ast.EnumDecl) {
	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil {
		return
	}

	var variants []EnumVariantInfo

	for _, variant := range d.Variants {
		variants = append(variants, EnumVariantInfo{
			Name: variant.Name,
			Span: variant.Span(),
		})
	}

	sym.Type.Variants = variants
}

func (c *Checker) prepareUnionDecl(parent *Scope, d *ast.UnionDecl) {
	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil {
		return
	}

	var members []*Type

	for _, member := range d.Members {
		memberType := c.typeFromAst(parent, member)
		members = append(members, memberType)
	}

	sym.Type.Members = members
}

func (c *Checker) enumHasVariant(enumType *Type, name string) bool {
	if enumType == nil || enumType.Kind != TypeEnum {
		return false
	}

	for _, variant := range enumType.Variants {
		if variant.Name == name {
			return true
		}
	}

	return false
}

func (c *Checker) unionHasMember(unionType *Type, memberType *Type) bool {
	if unionType == nil || unionType.Kind != TypeUnion {
		return false
	}

	for _, member := range unionType.Members {
		if c.sameType(member, memberType) {
			return true
		}
	}

	return false
}

func (c *Checker) unionMemberByName(unionType *Type, name string) *Type {
	if unionType == nil || unionType.Kind != TypeUnion {
		return nil
	}

	for _, member := range unionType.Members {
		if member.Name == name {
			return member
		}
	}

	return nil
}

func (c *Checker) checkSwitchStmt(scope *Scope, s *ast.SwitchStmt) {
	targetType := c.checkExpr(scope, s.Target)

	if s.IsUnionSwitch {
		c.checkUnionSwitch(scope, s, targetType)
		return
	}

	c.checkNormalSwitch(scope, s, targetType)
}

func (c *Checker) checkNormalSwitch(scope *Scope, s *ast.SwitchStmt, targetType *Type) {
	if targetType.Kind != TypeEnum {
		c.diags.Add(s.Target.Span(), fmt.Sprintf("switch target must be enum for now, got %s", targetType.String()))
	}

	for _, swCase := range s.Cases {
		caseScope := NewScope(scope)

		switch swCase.Kind {
		case ast.SwitchCaseEnumVariant:
			if targetType.Kind == TypeEnum && !c.enumHasVariant(targetType, swCase.EnumVariant.Name) {
				c.diags.Add(
					swCase.EnumVariant.Span(),
					fmt.Sprintf("enum %s has no variant .%s", targetType.String(), swCase.EnumVariant.Name),
				)
			}

		case ast.SwitchCaseDefault:
			// ok

		case ast.SwitchCaseExpr:
			caseType := c.checkExpr(scope, swCase.Expr)
			c.checkAssignable(targetType, caseType, swCase.Expr.Span())

		case ast.SwitchCaseNil:
			c.diags.Add(swCase.Loc, "nil case is only valid in union switch")

		case ast.SwitchCaseUnionMember:
			c.diags.Add(swCase.Loc, "type case is only valid in union switch")
		}

		for _, stmt := range swCase.Body {
			c.checkStmt(caseScope, stmt)
		}
	}
}

func (c *Checker) checkUnionSwitch(scope *Scope, s *ast.SwitchStmt, targetType *Type) {
	if targetType.Kind != TypeUnion {
		c.diags.Add(s.Target.Span(), fmt.Sprintf("union switch target must be union, got %s", targetType.String()))
	}

	for _, swCase := range s.Cases {
		caseScope := NewScope(scope)

		switch swCase.Kind {
		case ast.SwitchCaseUnionMember:
			memberType := c.typeFromAst(scope, swCase.UnionMember)

			if targetType.Kind == TypeUnion && !c.unionHasMember(targetType, memberType) {
				c.diags.Add(
					swCase.UnionMember.Span(),
					fmt.Sprintf("union %s has no member %s", targetType.String(), memberType.String()),
				)
			}

			if s.BindName.Name != "" {
				caseScope.Declare(&Symbol{
					Name: s.BindName.Name,
					Kind: SymbolVar,
					Type: memberType,
					Span: s.BindName.Span(),
					Node: s,
				})
			}

		case ast.SwitchCaseNil:
			if s.BindName.Name != "" {
				caseScope.Declare(&Symbol{
					Name: s.BindName.Name,
					Kind: SymbolVar,
					Type: NilType,
					Span: s.BindName.Span(),
					Node: s,
				})
			}

		case ast.SwitchCaseDefault:
			// No narrowing.

		case ast.SwitchCaseEnumVariant:
			c.diags.Add(swCase.EnumVariant.Span(), "enum case is not valid in union switch")

		case ast.SwitchCaseExpr:
			c.diags.Add(swCase.Expr.Span(), "expression case is not valid in union switch")
		}

		for _, stmt := range swCase.Body {
			c.checkStmt(caseScope, stmt)
		}
	}
}

func DebugSummary(scope *Scope) string {
	count := 0

	for _, sym := range scope.Symbols {
		switch sym.Name {
		case "void", "bool", "int", "f32", "f64", "string", "Assert", "Print":
			continue
		}

		count++
	}

	return fmt.Sprintf("checked_symbols=%d", count)
}
