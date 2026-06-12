package checker

import (
	"fmt"
	"strconv"
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
	TypeChar
	TypeString
	TypeCstring
	TypeU8
	TypeUsize
	TypeRawptr
	TypeAny
	TypeNil
	TypeEnumLiteral

	TypeUntypedInt
	TypeUntypedFloat

	TypePointer
	TypeArray
	TypeVariadic
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

	Params          []*Type
	Results         []*Type
	RequiredParams  int
	ParamDefaults   []ast.Expr
	ParamHasDefault []bool
	ParamIsVariadic []bool
	IsVariadic      bool
	IsExtern        bool
	ExternName      string
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
	case TypeChar:
		return "char"
	case TypeString:
		return "string"
	case TypeCstring:
		return "cstring"
	case TypeU8:
		return "u8"
	case TypeUntypedInt:
		return "untyped int"
	case TypeUntypedFloat:
		return "untyped float"
	case TypePointer:
		return "*" + t.Elem.String()
	case TypeUsize:
		return "usize"
	case TypeRawptr:
		return "rawptr"
	case TypeAny:
		return "any"
	case TypeArray:
		if t.Inferred {
			return "[?]" + t.Elem.String()
		}

		return fmt.Sprintf("[%d]%s", t.Len, t.Elem.String())
	case TypeVariadic:
		if t.Elem == nil {
			return "...<invalid>"
		}

		return "..." + t.Elem.String()
	case TypeStruct, TypeEnum, TypeUnion, TypeInterface, TypeTypeParam, TypeValueParam:
		return t.Name
	case TypeTask:
		var params []string
		for i, p := range t.Params {
			if i < len(t.ParamIsVariadic) && t.ParamIsVariadic[i] {
				params = append(params, "..."+p.String())
			} else {
				params = append(params, p.String())
			}
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
	CharType         = &Type{Kind: TypeChar, Name: "char"}
	StringType       = &Type{Kind: TypeString, Name: "string"}
	CstringType      = &Type{Kind: TypeCstring, Name: "cstring"}
	U8Type           = &Type{Kind: TypeU8, Name: "u8"}
	UsizeType        = &Type{Kind: TypeUsize, Name: "usize"}
	RawptrType       = &Type{Kind: TypeRawptr, Name: "rawptr"}
	AnyType          = &Type{Kind: TypeAny, Name: "any"}
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
	SymbolOverload
	SymbolPackage
)

type Symbol struct {
	Name     string
	Kind     SymbolKind
	Type     *Type
	Span     source.Span
	Node     ast.Node
	Overload *OverloadInfo

	Package *PackageInfo
}

type PackageInfo struct {
	Name    string
	Symbols map[string]*Symbol
}

type OverloadInfo struct {
	Name       string
	IsOperator bool
	Candidates []*Symbol
	Span       source.Span
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

	global   *Scope
	packages map[string]*PackageInfo

	currentResults []*Type
}

func New(diags *diag.Reporter) *Checker {
	return NewWithPackages(diags, nil)
}

func NewWithPackages(diags *diag.Reporter, packages map[string]*PackageInfo) *Checker {
	c := &Checker{
		diags:    diags,
		packages: packages,
	}

	c.global = NewScope(nil)
	c.declareBuiltins(c.global)
	c.declarePackages(c.global)

	return c
}

func (c *Checker) CheckFile(file *ast.File) *Scope {
	for _, decl := range file.Decls {
		c.declareDecl(c.global, decl)
	}

	// Prepare all non-overload symbols first so overload declarations can
	// refer to tasks declared later in the same global scope.
	for _, decl := range file.Decls {
		if _, ok := decl.(*ast.OverloadDecl); ok {
			continue
		}

		c.prepareDecl(c.global, decl)
	}

	for _, decl := range file.Decls {
		if _, ok := decl.(*ast.OverloadDecl); !ok {
			continue
		}

		c.prepareDecl(c.global, decl)
	}

	for _, decl := range file.Decls {
		c.checkDecl(c.global, decl)
	}

	return c.global
}

type overloadResolution struct {
	Candidate *Symbol
	Score     int
	Matched   bool
	Ambiguous bool
}

func (c *Checker) resolveOverload(info *OverloadInfo, argTypes []*Type) overloadResolution {
	best := overloadResolution{
		Score: 1 << 30,
	}

	for _, candidate := range info.Candidates {
		if candidate.Type == nil || candidate.Type.Kind != TypeTask {
			continue
		}

		score, ok := c.callScore(candidate.Type, argTypes)
		if !ok {
			continue
		}

		if !best.Matched || score < best.Score {
			best = overloadResolution{
				Candidate: candidate,
				Score:     score,
				Matched:   true,
			}
			continue
		}

		if score == best.Score {
			best.Ambiguous = true
		}
	}

	return best
}

func (c *Checker) callScore(taskType *Type, argTypes []*Type) (int, bool) {
	if taskType == nil || taskType.Kind != TypeTask {
		return 0, false
	}

	required := taskType.RequiredParams
	total := len(taskType.Params)

	if taskType.IsVariadic {
		fixedCount := total - 1
		variadicElem := InvalidType
		if total > 0 {
			variadicElem = taskType.Params[total-1]
		}

		if len(argTypes) < required {
			return 0, false
		}

		score := 0
		count := len(argTypes)
		if count > fixedCount {
			count = fixedCount
		}

		for i := 0; i < count; i++ {
			itemScore, ok := c.conversionScore(taskType.Params[i], argTypes[i])
			if !ok {
				return 0, false
			}

			score += itemScore
		}

		for i := fixedCount; i < len(argTypes); i++ {
			itemScore, ok := c.conversionScore(variadicElem, argTypes[i])
			if !ok {
				return 0, false
			}

			score += itemScore + 10
		}

		return score, true
	}

	if required == 0 && total > 0 && len(taskType.ParamHasDefault) == 0 {
		required = total
	}

	if len(argTypes) < required || len(argTypes) > total {
		return 0, false
	}

	score := 0

	for i := range argTypes {
		itemScore, ok := c.conversionScore(taskType.Params[i], argTypes[i])
		if !ok {
			return 0, false
		}

		score += itemScore
	}

	// Prefer candidates that do not need defaults when both match.
	missingDefaults := total - len(argTypes)
	score += missingDefaults * 5

	return score, true
}

func (c *Checker) conversionScore(dst *Type, src *Type) (int, bool) {
	if dst == nil || src == nil {
		return 100, true
	}

	if dst.Kind == TypeInvalid || src.Kind == TypeInvalid {
		return 100, true
	}

	if c.sameType(dst, src) {
		return 0, true
	}

	if dst.Kind == TypeAny {
		return 20, true
	}

	if dst.Kind == TypeEnum && src.Kind == TypeEnumLiteral {
		if c.enumHasVariant(dst, src.Name) {
			return 1, true
		}

		return 0, false
	}

	if dst.Kind == TypeUnion {
		if src.Kind == TypeNil || c.unionHasMember(dst, src) {
			return 1, true
		}

		return 0, false
	}

	if src.Kind == TypeNil {
		if dst.Kind == TypePointer || dst.Kind == TypeUnion {
			return 1, true
		}

		return 0, false
	}

	if src.Kind == TypeUntypedInt {
		switch dst.Kind {
		case TypeInt, TypeUsize:
			return 1, true
		case TypeF32, TypeF64:
			return 2, true
		}
	}

	if src.Kind == TypeUntypedFloat {
		switch dst.Kind {
		case TypeF64:
			return 1, true
		case TypeF32:
			return 2, true
		}
	}

	if dst.Kind == TypeArray && src.Kind == TypeArray {
		if !dst.Inferred && src.Len >= 0 && dst.Len >= 0 && dst.Len != src.Len {
			return 0, false
		}

		score, ok := c.conversionScore(dst.Elem, src.Elem)
		if !ok {
			return 0, false
		}

		return score + 1, true
	}

	if c.assignable(dst, src) {
		return 10, true
	}

	return 0, false
}

func (c *Checker) formatTypes(types []*Type) string {
	var parts []string

	for _, t := range types {
		if t == nil {
			parts = append(parts, "<nil>")
		} else {
			parts = append(parts, t.String())
		}
	}

	return strings.Join(parts, ", ")
}

func (c *Checker) resultTypeFromCall(taskType *Type, span source.Span) *Type {
	if taskType == nil || taskType.Kind != TypeTask {
		return InvalidType
	}

	if len(taskType.Results) == 0 {
		return VoidType
	}

	if len(taskType.Results) == 1 {
		return taskType.Results[0]
	}

	c.diags.Add(span, "multi-result task call cannot be used as a single expression yet")
	return InvalidType
}

func (c *Checker) declareBuiltins(scope *Scope) {
	builtinTypes := map[string]*Type{
		"void":    VoidType,
		"bool":    BoolType,
		"int":     IntType,
		"u8":      U8Type,
		"usize":   UsizeType,
		"rawptr":  RawptrType,
		"any":     AnyType,
		"f32":     F32Type,
		"f64":     F64Type,
		"char":    CharType,
		"string":  StringType,
		"cstring": CstringType,
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
}

func (c *Checker) declarePackages(scope *Scope) {
	for name, pkg := range c.packages {
		if name == "" || pkg == nil {
			continue
		}

		scope.Declare(&Symbol{
			Name: name,
			Kind: SymbolPackage,
			Type: &Type{
				Kind: TypePackage,
				Name: name,
			},
			Package: pkg,
		})
	}
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
		scope.Declare(&Symbol{
			Name: d.Name,
			Kind: SymbolOverload,
			Type: InvalidType,
			Span: d.Span(),
			Node: d,
			Overload: &OverloadInfo{
				Name:       d.Name,
				IsOperator: isOperatorOverloadName(d.Name),
				Span:       d.Span(),
			},
		})
	}
}

func (c *Checker) prepareDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.StructDecl:
		c.prepareStructDecl(scope, d)

	case *ast.EnumDecl:
		c.prepareEnumDecl(scope, d)

	case *ast.UnionDecl:
		c.prepareUnionDecl(scope, d)

	case *ast.TaskDecl:
		sym := scope.LookupLocal(d.Name.Name)
		if sym != nil {
			sym.Type = c.taskTypeFromDecl(scope, d)
		}

	case *ast.InterfaceDecl:
		c.prepareInterfaceDecl(scope, d)

	case *ast.OverloadDecl:
		c.prepareOverloadDecl(scope, d)
	}
}

func (c *Checker) prepareOverloadDecl(scope *Scope, d *ast.OverloadDecl) {
	sym := scope.LookupLocal(d.Name)
	if sym == nil || sym.Overload == nil {
		return
	}

	var candidates []*Symbol

	for _, name := range d.Names {
		candidate := scope.Lookup(name.Name)
		if candidate == nil {
			c.diags.Add(name.Span(), fmt.Sprintf("undefined overload candidate %q", name.Name))
			continue
		}

		if candidate.Kind != SymbolTask {
			c.diags.Add(name.Span(), fmt.Sprintf("overload candidate %q is not a task", name.Name))
			continue
		}

		if candidate.Type == nil || candidate.Type.Kind != TypeTask {
			c.diags.Add(name.Span(), fmt.Sprintf("overload candidate %q has invalid task type", name.Name))
			continue
		}

		candidates = append(candidates, candidate)
	}

	sym.Overload.Candidates = candidates
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
		c.checkInterfaceDecl(scope, d)

	case *ast.ImplDecl:
	// Later phase.

	case *ast.OverloadDecl:
		c.checkOverloadDecl(scope, d)

	case *ast.DirectiveDecl:
		return

	case *ast.TaskDecl:
		c.checkTaskDecl(scope, d)
	}
}

func (c *Checker) checkOverloadDecl(scope *Scope, d *ast.OverloadDecl) {
	sym := scope.LookupLocal(d.Name)
	if sym == nil || sym.Overload == nil {
		return
	}

	info := sym.Overload

	for _, candidate := range info.Candidates {
		taskDecl, _ := candidate.Node.(*ast.TaskDecl)
		taskType := candidate.Type

		if taskType == nil || taskType.Kind != TypeTask {
			continue
		}

		if info.IsOperator {
			if taskDecl == nil || !taskDecl.IsPure {
				c.diags.Add(candidate.Span, fmt.Sprintf("operator overload %q requires pure task candidate %q", d.Name, candidate.Name))
			}

			if c.taskHasDefaultParameters(taskType) {
				c.diags.Add(candidate.Span, fmt.Sprintf("operator overload %q candidate %q cannot have default parameters", d.Name, candidate.Name))
			}

			if len(taskType.Params) != 2 {
				c.diags.Add(candidate.Span, fmt.Sprintf("operator overload %q candidate %q must have exactly 2 parameters", d.Name, candidate.Name))
			}

			if len(taskType.Results) != 1 {
				c.diags.Add(candidate.Span, fmt.Sprintf("operator overload %q candidate %q must return exactly 1 value", d.Name, candidate.Name))
			}

			if isComparisonOperatorName(d.Name) && len(taskType.Results) == 1 && !c.sameType(taskType.Results[0], BoolType) {
				c.diags.Add(candidate.Span, fmt.Sprintf("comparison operator overload %q candidate %q must return bool", d.Name, candidate.Name))
			}

			if len(taskType.Params) == 2 &&
				c.isBuiltinPrimitiveForOperator(taskType.Params[0]) &&
				c.isBuiltinPrimitiveForOperator(taskType.Params[1]) {
				c.diags.Add(candidate.Span, fmt.Sprintf("operator overload %q cannot replace built-in primitive operator behavior", d.Name))
			}
		}
	}

	c.checkDuplicateOverloadSignatures(info)
}

func (c *Checker) checkDuplicateOverloadSignatures(info *OverloadInfo) {
	for i := 0; i < len(info.Candidates); i++ {
		a := info.Candidates[i]
		if a.Type == nil || a.Type.Kind != TypeTask {
			continue
		}

		for j := i + 1; j < len(info.Candidates); j++ {
			b := info.Candidates[j]
			if b.Type == nil || b.Type.Kind != TypeTask {
				continue
			}

			if sameParamSignature(c, a.Type, b.Type) {
				c.diags.Add(b.Span, fmt.Sprintf("duplicate overload signature for %q", info.Name))
			}
		}
	}
}

func sameParamSignature(c *Checker, a *Type, b *Type) bool {
	if len(a.Params) != len(b.Params) {
		return false
	}

	for i := range a.Params {
		if !c.sameType(a.Params[i], b.Params[i]) {
			return false
		}
	}

	return true
}

func (c *Checker) checkInterfaceDecl(scope *Scope, d *ast.InterfaceDecl) {
	for _, req := range d.Requirements {
		for _, param := range req.Params {
			if param.HasDefault {
				c.diags.Add(
					param.Name.Span(),
					fmt.Sprintf("interface requirement parameter %q cannot have a default value", param.Name.Name),
				)
			}
		}
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

	c.checkTaskDefaultParameters(parent, d, taskType)

	if d.IsExtern {
		if d.Body != nil {
			c.diags.Add(d.Body.Span(), fmt.Sprintf("extern task %q cannot have a body", d.Name.Name))
		}
		return
	}

	taskScope := NewScope(parent)

	for i, param := range d.Params {
		paramType := InvalidType
		if i < len(taskType.Params) {
			paramType = taskType.Params[i]
		}

		if param.IsVariadic {
			paramType = &Type{
				Kind: TypeVariadic,
				Elem: paramType,
				Name: "..." + paramType.String(),
			}
		}

		taskScope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolParam,
			Type: paramType,
			Span: param.Name.Span(),
			Node: d,
		})
	}

	oldResults := c.currentResults
	c.currentResults = taskType.Results

	c.checkBlockInScope(taskScope, d.Body, false)

	c.currentResults = oldResults
}

func (c *Checker) taskTypeFromDecl(scope *Scope, d *ast.TaskDecl) *Type {
	var params []*Type
	var paramDefaults []ast.Expr
	var paramHasDefault []bool
	var paramIsVariadic []bool

	requiredParams := len(d.Params)
	isVariadic := false

	for i, param := range d.Params {
		params = append(params, c.typeFromAst(scope, param.Type))
		paramDefaults = append(paramDefaults, param.Default)
		paramHasDefault = append(paramHasDefault, param.HasDefault)
		paramIsVariadic = append(paramIsVariadic, param.IsVariadic)

		if param.IsVariadic {
			isVariadic = true
			if requiredParams == len(d.Params) {
				requiredParams = i
			}
		}

		if param.HasDefault && requiredParams == len(d.Params) {
			requiredParams = i
		}
	}

	var results []*Type
	for _, result := range d.Results {
		results = append(results, c.typeFromAst(scope, result))
	}

	return &Type{
		Kind:            TypeTask,
		Name:            d.Name.Name,
		Params:          params,
		Results:         results,
		RequiredParams:  requiredParams,
		ParamDefaults:   paramDefaults,
		ParamHasDefault: paramHasDefault,
		ParamIsVariadic: paramIsVariadic,
		IsVariadic:      isVariadic,
		IsExtern:        d.IsExtern,
		ExternName:      d.ExternName,
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

func (c *Checker) checkTaskDefaultParameters(parent *Scope, d *ast.TaskDecl, taskType *Type) {
	seenDefault := false
	seenVariadic := false

	for i, param := range d.Params {
		if param.IsVariadic {
			if seenVariadic {
				c.diags.Add(param.Name.Span(), "task can have only one variadic parameter")
			}

			seenVariadic = true

			if i != len(d.Params)-1 {
				c.diags.Add(param.Name.Span(), "variadic parameter must be the last parameter")
			}

			if param.HasDefault {
				c.diags.Add(param.Name.Span(), "variadic parameter cannot have a default value")
			}

			// C varargs are not typed, so Seal extern variadics use ...any.
			if d.IsExtern && i < len(taskType.Params) && !c.sameType(taskType.Params[i], AnyType) {
				c.diags.Add(param.Name.Span(), fmt.Sprintf("extern variadic parameter %q must have type any", param.Name.Name))
			}

			if d.IsExtern && i == 0 {
				c.diags.Add(param.Name.Span(), "extern variadic task must have at least one fixed parameter before ...any")
			}
		}

		if d.IsExtern && param.HasDefault {
			c.diags.Add(param.Name.Span(), fmt.Sprintf("extern task parameter %q cannot have a default value", param.Name.Name))
		}

		if param.HasDefault {
			seenDefault = true

			defaultType := c.checkExpr(parent, param.Default)

			if i < len(taskType.Params) {
				c.checkAssignable(taskType.Params[i], defaultType, param.Default.Span())
			}

			continue
		}

		if seenDefault && !param.IsVariadic {
			c.diags.Add(
				param.Name.Span(),
				fmt.Sprintf("parameter %q must have a default value because a previous parameter has a default", param.Name.Name),
			)
		}
	}
}

func (c *Checker) taskHasDefaultParameters(taskType *Type) bool {
	if taskType == nil {
		return false
	}

	for _, hasDefault := range taskType.ParamHasDefault {
		if hasDefault {
			return true
		}
	}

	return false
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

	if index, ok := s.Left.(*ast.IndexExpr); ok {
		containerType := c.checkExpr(scope, index.Left)

		if containerType.Kind == TypeString {
			c.diags.Add(index.Span(), "cannot assign to string index because strings are immutable")
		}

		if containerType.Kind == TypeCstring {
			c.diags.Add(index.Span(), "cannot assign to cstring index")
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
		var valueType *Type

		if s.HasType {
			valueType = c.checkExprWithExpected(scope, s.Value, varType)
			c.checkAssignable(varType, valueType, s.Value.Span())
		} else {
			valueType = c.checkExpr(scope, s.Value)

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

	case *ast.GenericExpr:
		c.diags.Add(e.Span(), "generic expression cannot be used as a value")
		return InvalidType

	case *ast.IntLitExpr:
		return UntypedIntType

	case *ast.FloatLitExpr:
		return UntypedFloatType

	case *ast.StringLitExpr:
		return StringType

	case *ast.CStringLitExpr:
		return CstringType

	case *ast.CharLitExpr:
		value, err := strconv.Unquote(e.Value)
		if err != nil {
			c.diags.Add(e.Span(), fmt.Sprintf("invalid char literal: %v", err))
			return CharType
		}

		if len([]rune(value)) != 1 {
			c.diags.Add(e.Span(), "char literal must contain exactly one Unicode scalar value")
		}

		return CharType

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

func (c *Checker) checkExprWithExpected(scope *Scope, expr ast.Expr, expected *Type) *Type {
	if expected == nil || expected.Kind == TypeInvalid {
		return c.checkExpr(scope, expr)
	}

	switch e := expr.(type) {
	case *ast.ArrayLiteralExpr:
		return c.checkArrayLiteralExprWithExpected(scope, e, expected)

	default:
		got := c.checkExpr(scope, expr)
		c.checkAssignable(expected, got, expr.Span())
		return expected
	}
}

func (c *Checker) checkArrayLiteralExprWithExpected(scope *Scope, e *ast.ArrayLiteralExpr, expected *Type) *Type {
	if expected == nil || expected.Kind != TypeArray || expected.Elem == nil {
		return c.checkArrayLiteralExpr(scope, e)
	}

	for _, value := range e.Values {
		valueType := c.checkExpr(scope, value)
		c.checkAssignable(expected.Elem, valueType, value.Span())
	}

	length := len(e.Values)
	if !expected.Inferred && expected.Len >= 0 && expected.Len != length {
		c.diags.Add(e.Span(), fmt.Sprintf("array length mismatch: expected %d, got %d", expected.Len, length))
	}

	return &Type{
		Kind:     TypeArray,
		Len:      length,
		Inferred: expected.Inferred,
		Elem:     expected.Elem,
	}
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
		if c.isNumeric(left) && c.isNumeric(right) {
			result, ok := c.numericResultType(left, right)
			if !ok {
				c.diags.Add(e.Span(), fmt.Sprintf("mismatched numeric operands: %s and %s", left.String(), right.String()))
				return InvalidType
			}

			return result
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return result
		}

		c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires numeric operands", e.Op.String()))
		return InvalidType

	case token.EqEq:
		if c.builtinEqualityCompatible(left, right) {
			return BoolType
		}

		if result, ok := c.checkOperatorOverload(scope, "==", []*Type{left, right}, e.Span(), true); ok {
			return result
		}

		c.diags.Add(e.Span(), fmt.Sprintf("cannot compare %s and %s", left.String(), right.String()))
		return InvalidType

	case token.NotEq:
		if c.builtinEqualityCompatible(left, right) {
			return BoolType
		}

		if result, ok := c.checkOperatorOverload(scope, "!=", []*Type{left, right}, e.Span(), false); ok {
			return result
		}

		// Derive != from == if only == exists.
		if result, ok := c.checkOperatorOverload(scope, "==", []*Type{left, right}, e.Span(), false); ok {
			if !c.sameType(result, BoolType) {
				c.diags.Add(e.Span(), "derived != requires == overload to return bool")
				return InvalidType
			}

			return BoolType
		}

		c.diags.Add(e.Span(), fmt.Sprintf("cannot compare %s and %s", left.String(), right.String()))
		return InvalidType

	case token.Lt, token.Gt, token.LtEq, token.GtEq:
		if c.isNumeric(left) && c.isNumeric(right) {
			if c.numericComparable(left, right) {
				return BoolType
			}

			c.diags.Add(e.Span(), fmt.Sprintf("mismatched numeric operands: %s and %s", left.String(), right.String()))
			return InvalidType
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return result
		}

		c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires numeric operands", e.Op.String()))
		return InvalidType

	case token.AndAnd, token.OrOr:
		if !c.sameType(left, BoolType) || !c.sameType(right, BoolType) {
			c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires bool operands", e.Op.String()))
			return InvalidType
		}

		return BoolType
	}

	return InvalidType
}

func (c *Checker) numericComparable(a *Type, b *Type) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind == TypeInvalid || b.Kind == TypeInvalid {
		return true
	}

	if c.sameType(a, b) {
		return true
	}

	if a.Kind == TypeUntypedInt && c.isIntegerLike(b) {
		return true
	}

	if b.Kind == TypeUntypedInt && c.isIntegerLike(a) {
		return true
	}

	if a.Kind == TypeUntypedFloat && (b.Kind == TypeF32 || b.Kind == TypeF64) {
		return true
	}

	if b.Kind == TypeUntypedFloat && (a.Kind == TypeF32 || a.Kind == TypeF64) {
		return true
	}

	if c.isIntegerLike(a) && c.isIntegerLike(b) {
		return true
	}

	if c.isNumeric(a) && c.isNumeric(b) {
		_, ok := c.numericResultType(a, b)
		return ok
	}

	return false
}

func (c *Checker) checkOperatorOverload(scope *Scope, name string, argTypes []*Type, span source.Span, diagnoseMissing bool) (*Type, bool) {
	sym := scope.Lookup(name)
	if sym == nil || sym.Kind != SymbolOverload || sym.Overload == nil {
		return InvalidType, false
	}

	info := sym.Overload
	result := c.resolveOverload(info, argTypes)

	if !result.Matched {
		if diagnoseMissing {
			c.diags.Add(
				span,
				fmt.Sprintf("no operator overload %q matches operand types (%s)", name, c.formatTypes(argTypes)),
			)
		}

		return InvalidType, true
	}

	if result.Ambiguous {
		c.diags.Add(
			span,
			fmt.Sprintf("ambiguous operator overload %q with operand types (%s)", name, c.formatTypes(argTypes)),
		)

		return InvalidType, true
	}

	return c.resultTypeFromCall(result.Candidate.Type, span), true
}

func (c *Checker) builtinEqualityCompatible(a *Type, b *Type) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind == TypeInvalid || b.Kind == TypeInvalid {
		return true
	}

	if c.isNumeric(a) && c.isNumeric(b) {
		_, ok := c.numericResultType(a, b)
		return ok
	}

	if c.sameType(a, BoolType) && c.sameType(b, BoolType) {
		return true
	}

	if c.sameType(a, StringType) && c.sameType(b, StringType) {
		return true
	}

	if a.Kind == TypeEnum && b.Kind == TypeEnum {
		return c.sameType(a, b)
	}

	if a.Kind == TypeEnum && b.Kind == TypeEnumLiteral {
		return c.enumHasVariant(a, b.Name)
	}

	if b.Kind == TypeEnum && a.Kind == TypeEnumLiteral {
		return c.enumHasVariant(b, a.Name)
	}

	if a.Kind == TypePointer && b.Kind == TypeNil {
		return true
	}

	if b.Kind == TypePointer && a.Kind == TypeNil {
		return true
	}

	return false
}

func (c *Checker) checkCallExpr(scope *Scope, e *ast.CallExpr) *Type {
	argTypes := make([]*Type, 0, len(e.Args))
	for _, arg := range e.Args {
		argTypes = append(argTypes, c.checkExpr(scope, arg))
	}

	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		return c.checkGenericIntrinsicCall(scope, gen, argTypes, e.Args, e.Span())
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok && id.Name.Name == "len" {
		return c.checkLenCall(e.Args, argTypes, e.Span())
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym == nil {
			c.diags.Add(id.Span(), fmt.Sprintf("undefined symbol %q", id.Name.Name))
			return InvalidType
		}

		switch sym.Kind {
		case SymbolTask:
			return c.checkDirectTaskCall(sym, argTypes, e.Args, e.Span())

		case SymbolOverload:
			return c.checkOverloadCall(sym, argTypes, e.Args, e.Span())

		default:
			c.diags.Add(id.Span(), fmt.Sprintf("cannot call non-task symbol %q", id.Name.Name))
			return InvalidType
		}
	}

	if selector, ok := e.Callee.(*ast.SelectorExpr); ok {
		if id, ok := selector.Left.(*ast.IdentExpr); ok {
			pkgSym := scope.Lookup(id.Name.Name)
			if pkgSym != nil && pkgSym.Kind == SymbolPackage {
				return c.checkPackageCall(pkgSym, selector, argTypes, e.Args, e.Span())
			}
		}
	}

	calleeType := c.checkExpr(scope, e.Callee)

	if calleeType.Kind != TypeTask {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("cannot call non-task type %s", calleeType.String()))
		return InvalidType
	}

	return c.checkTaskTypeCall(calleeType, argTypes, e.Args, e.Span())
}

func (c *Checker) checkPackageCall(pkgSym *Symbol, selector *ast.SelectorExpr, argTypes []*Type, args []ast.Expr, span source.Span) *Type {
	if pkgSym.Package == nil {
		c.diags.Add(selector.Left.Span(), fmt.Sprintf("package %q has no symbol table", pkgSym.Name))
		return InvalidType
	}

	member := pkgSym.Package.Symbols[selector.Name.Name]
	if member == nil {
		c.diags.Add(selector.Name.Span(), fmt.Sprintf("package %s has no symbol %q", pkgSym.Name, selector.Name.Name))
		return InvalidType
	}

	switch member.Kind {
	case SymbolTask:
		return c.checkDirectTaskCall(member, argTypes, args, span)

	case SymbolOverload:
		return c.checkOverloadCall(member, argTypes, args, span)

	default:
		c.diags.Add(selector.Name.Span(), fmt.Sprintf("package symbol %s.%s is not callable", pkgSym.Name, selector.Name.Name))
		return InvalidType
	}
}

func (c *Checker) checkDirectTaskCall(sym *Symbol, argTypes []*Type, args []ast.Expr, span source.Span) *Type {
	if sym.Type == nil || sym.Type.Kind != TypeTask {
		c.diags.Add(sym.Span, fmt.Sprintf("symbol %q is not a valid task", sym.Name))
		return InvalidType
	}

	return c.checkTaskTypeCall(sym.Type, argTypes, args, span)
}

func (c *Checker) checkTaskTypeCall(taskType *Type, argTypes []*Type, args []ast.Expr, span source.Span) *Type {
	required := taskType.RequiredParams
	total := len(taskType.Params)

	if required == 0 && total > 0 && len(taskType.ParamHasDefault) == 0 && !taskType.IsVariadic {
		required = total
	}

	if taskType.IsVariadic {
		fixedCount := total - 1
		variadicElem := InvalidType
		if total > 0 {
			variadicElem = taskType.Params[total-1]
		}

		if len(argTypes) < required {
			c.diags.Add(
				span,
				fmt.Sprintf("task call argument count mismatch: expected at least %d, got %d", required, len(argTypes)),
			)
		}

		count := len(argTypes)
		if count > fixedCount {
			count = fixedCount
		}

		for i := 0; i < count; i++ {
			c.checkAssignable(taskType.Params[i], argTypes[i], args[i].Span())
		}

		for i := fixedCount; i < len(argTypes); i++ {
			c.checkAssignable(variadicElem, argTypes[i], args[i].Span())
		}

		return c.resultTypeFromCall(taskType, span)
	}

	if len(argTypes) < required || len(argTypes) > total {
		if required == total {
			c.diags.Add(
				span,
				fmt.Sprintf("task call argument count mismatch: expected %d, got %d", total, len(argTypes)),
			)
		} else {
			c.diags.Add(
				span,
				fmt.Sprintf("task call argument count mismatch: expected %d to %d, got %d", required, total, len(argTypes)),
			)
		}
	}

	count := len(argTypes)
	if total < count {
		count = total
	}

	for i := 0; i < count; i++ {
		c.checkAssignable(taskType.Params[i], argTypes[i], args[i].Span())
	}

	return c.resultTypeFromCall(taskType, span)
}

func (c *Checker) checkGenericIntrinsicCall(scope *Scope, gen *ast.GenericExpr, argTypes []*Type, args []ast.Expr, span source.Span) *Type {
	id, ok := gen.Base.(*ast.IdentExpr)
	if !ok {
		c.diags.Add(gen.Base.Span(), "only intrinsic generic calls are supported here")
		return InvalidType
	}

	name := id.Name.Name

	if name != "anyAs" && name != "anyIs" {
		c.diags.Add(id.Span(), fmt.Sprintf("unknown generic intrinsic %q", name))
		return InvalidType
	}

	if len(gen.Args) != 1 {
		c.diags.Add(gen.Span(), fmt.Sprintf("%s expects exactly 1 type argument", name))
		return InvalidType
	}

	targetType := c.typeFromAst(scope, gen.Args[0])

	if len(argTypes) != 1 {
		c.diags.Add(span, fmt.Sprintf("%s expects exactly 1 value argument", name))
		return InvalidType
	}

	if !c.sameType(argTypes[0], AnyType) {
		c.diags.Add(args[0].Span(), fmt.Sprintf("%s expects any, got %s", name, argTypes[0].String()))
	}

	switch name {
	case "anyAs":
		return targetType

	case "anyIs":
		return BoolType
	}

	return InvalidType
}

func (c *Checker) checkLenCall(args []ast.Expr, argTypes []*Type, span source.Span) *Type {
	if len(argTypes) != 1 {
		c.diags.Add(span, fmt.Sprintf("len expects 1 argument, got %d", len(argTypes)))
		return UsizeType
	}

	t := argTypes[0]
	if t == nil || t.Kind == TypeInvalid {
		return UsizeType
	}

	switch t.Kind {
	case TypeArray, TypeVariadic, TypeString:
		return UsizeType

	default:
		c.diags.Add(args[0].Span(), fmt.Sprintf("len does not support %s", t.String()))
		return UsizeType
	}
}

func (c *Checker) checkOverloadCall(sym *Symbol, argTypes []*Type, args []ast.Expr, span source.Span) *Type {
	info := sym.Overload
	if info == nil {
		c.diags.Add(sym.Span, fmt.Sprintf("symbol %q is not a valid overload", sym.Name))
		return InvalidType
	}

	result := c.resolveOverload(info, argTypes)

	if !result.Matched {
		c.diags.Add(
			span,
			fmt.Sprintf("no overload of %q matches argument types (%s)", info.Name, c.formatTypes(argTypes)),
		)
		return InvalidType
	}

	if result.Ambiguous {
		c.diags.Add(
			span,
			fmt.Sprintf("ambiguous overload call %q with argument types (%s)", info.Name, c.formatTypes(argTypes)),
		)
		return InvalidType
	}

	for i, arg := range args {
		c.checkAssignable(result.Candidate.Type.Params[i], argTypes[i], arg.Span())
	}

	return c.resultTypeFromCall(result.Candidate.Type, span)
}

func (c *Checker) checkSelectorExpr(scope *Scope, e *ast.SelectorExpr) *Type {
	if id, ok := e.Left.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym != nil && sym.Kind == SymbolPackage {
			return c.checkPackageSelectorExpr(sym, e)
		}
	}

	leftType := c.checkExpr(scope, e.Left)

	if leftType.Kind == TypeString {
		c.diags.Add(e.Name.Span(), fmt.Sprintf("string has no field %q; use len(s) or s[i]", e.Name.Name))
		return InvalidType
	}

	if leftType.Kind == TypeCstring {
		c.diags.Add(e.Name.Span(), fmt.Sprintf("cstring has no field %q", e.Name.Name))
		return InvalidType
	}

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

	if !c.isIndexType(indexType) {
		c.diags.Add(e.Index.Span(), fmt.Sprintf("index must be int or usize, got %s", indexType.String()))
	}

	switch leftType.Kind {
	case TypeArray:
		return leftType.Elem

	case TypeVariadic:
		return leftType.Elem

	case TypeString:
		return CharType

	case TypeCstring:
		c.diags.Add(e.Left.Span(), "cstring does not support character indexing")
		return InvalidType

	default:
		c.diags.Add(e.Left.Span(), fmt.Sprintf("cannot index type %s", leftType.String()))
		return InvalidType
	}
}

func (c *Checker) checkPackageSelectorExpr(pkgSym *Symbol, e *ast.SelectorExpr) *Type {
	if pkgSym.Package == nil {
		c.diags.Add(e.Left.Span(), fmt.Sprintf("package %q has no symbol table", pkgSym.Name))
		return InvalidType
	}

	member := pkgSym.Package.Symbols[e.Name.Name]
	if member == nil {
		c.diags.Add(e.Name.Span(), fmt.Sprintf("package %s has no symbol %q", pkgSym.Name, e.Name.Name))
		return InvalidType
	}

	return member.Type
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
			first := t.Parts[0]
			member := t.Parts[1]

			sym := scope.Lookup(first.Name)
			if sym == nil {
				c.diags.Add(first.Span(), fmt.Sprintf("undefined type or package %q", first.Name))
				return InvalidType
			}

			if sym.Kind != SymbolPackage {
				c.diags.Add(first.Span(), fmt.Sprintf("%q is not a package", first.Name))
				return InvalidType
			}

			if sym.Package == nil {
				c.diags.Add(first.Span(), fmt.Sprintf("package %q has no symbol table", first.Name))
				return InvalidType
			}

			memberSym := sym.Package.Symbols[member.Name]
			if memberSym == nil {
				c.diags.Add(member.Span(), fmt.Sprintf("package %s has no type %q", first.Name, member.Name))
				return InvalidType
			}

			if memberSym.Kind != SymbolType {
				c.diags.Add(member.Span(), fmt.Sprintf("package symbol %s.%s is not a type", first.Name, member.Name))
				return InvalidType
			}

			return memberSym.Type
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

	if c.sameType(dst, src) {
		return
	}

	if dst.Kind == TypeAny {
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

	if c.sameType(dst, src) {
		return true
	}

	if dst.Kind == TypeAny {
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

	if src.Kind == TypeUntypedInt {
		return dst.Kind == TypeInt ||
			dst.Kind == TypeUsize ||
			dst.Kind == TypeF32 ||
			dst.Kind == TypeF64
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

	case TypeVariadic:
		return c.sameType(a.Elem, b.Elem)

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
	case TypeInt, TypeU8, TypeUsize, TypeChar, TypeF32, TypeF64, TypeUntypedInt, TypeUntypedFloat:
		return true
	default:
		return false
	}
}

func (c *Checker) isIndexType(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeInt, TypeUsize, TypeUntypedInt:
		return true

	default:
		return false
	}
}

func (c *Checker) isIntegerLike(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeInt, TypeU8, TypeUsize, TypeChar, TypeUntypedInt:
		return true
	default:
		return false
	}
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

	if a.Kind == TypeUntypedFloat || b.Kind == TypeUntypedFloat {
		return InvalidType, false
	}

	if a.Kind == TypeF64 || b.Kind == TypeF64 {
		if c.isNumeric(a) && c.isNumeric(b) {
			return F64Type, true
		}
	}

	if a.Kind == TypeF32 || b.Kind == TypeF32 {
		if c.isNumeric(a) && c.isNumeric(b) {
			return F32Type, true
		}
	}

	if c.sameType(a, b) {
		return a, true
	}

	// Allow comparisons between integer-like types, but avoid silently choosing
	// a result type for arithmetic with mixed concrete integer types.
	if c.isIntegerLike(a) && c.isIntegerLike(b) {
		return InvalidType, false
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
	if sym == nil || sym.Type == nil {
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
	if sym == nil || sym.Type == nil {
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
	if enumType == nil {
		return false
	}

	if enumType.Kind != TypeEnum {
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
	if unionType == nil || memberType == nil {
		return false
	}

	if unionType.Kind != TypeUnion {
		return false
	}

	for _, member := range unionType.Members {
		if c.sameType(member, memberType) {
			return true
		}

		if member != nil &&
			member.Name != "" &&
			member.Name == memberType.Name {
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

func (c *Checker) addDuplicateCaseDiagnostic(current source.Span, previous source.Span, label string) {
	c.diags.Add(
		current,
		fmt.Sprintf("duplicate switch case %s, previous case at %s", label, previous.String()),
	)
}

func switchCaseTypeKey(t *Type) string {
	if t == nil {
		return "<nil>"
	}

	return t.String()
}

func (c *Checker) checkSwitchStmt(scope *Scope, s *ast.SwitchStmt) {
	targetType := c.checkExpr(scope, s.Target)

	if s.IsTypeSwitch {
		c.checkTypeSwitch(scope, s, targetType)
		return
	}

	if s.IsUnionSwitch {
		c.checkUnionSwitch(scope, s, targetType)
		return
	}

	c.checkNormalSwitch(scope, s, targetType)
}

func (c *Checker) checkTypeSwitch(scope *Scope, s *ast.SwitchStmt, targetType *Type) {
	if !c.sameType(targetType, AnyType) {
		c.diags.Add(s.Target.Span(), fmt.Sprintf("type switch target must be any, got %s", targetType.String()))
	}

	hasDefault := false
	var previousDefault source.Span
	seenTypes := map[string]source.Span{}

	for _, swCase := range s.Cases {
		caseScope := NewScope(scope)

		switch swCase.Kind {
		case ast.SwitchCaseUnionMember:
			caseType := c.typeFromAst(scope, swCase.UnionMember)
			key := caseType.String()

			if prev, ok := seenTypes[key]; ok {
				c.diags.Add(
					swCase.UnionMember.Span(),
					fmt.Sprintf("duplicate type switch case %s, previous case at %s", key, prev.String()),
				)
			} else {
				seenTypes[key] = swCase.UnionMember.Span()
			}

		case ast.SwitchCaseDefault:
			if hasDefault {
				c.addDuplicateCaseDiagnostic(swCase.Loc, previousDefault, "default")
			} else {
				hasDefault = true
				previousDefault = swCase.Loc
			}

		case ast.SwitchCaseNil:
			c.diags.Add(swCase.Loc, "nil case is not valid in any type switch")

		case ast.SwitchCaseEnumVariant:
			c.diags.Add(swCase.EnumVariant.Span(), "enum case is not valid in any type switch")

		case ast.SwitchCaseExpr:
			c.diags.Add(swCase.Expr.Span(), "expression case is not valid in any type switch")
		}

		for _, stmt := range swCase.Body {
			c.checkStmt(caseScope, stmt)
		}
	}

	if !s.IsPartial && !hasDefault {
		c.diags.Add(s.Span(), "non-partial any type switch requires default")
	}
}

func (c *Checker) checkNormalSwitch(scope *Scope, s *ast.SwitchStmt, targetType *Type) {
	if targetType.Kind != TypeEnum {
		c.diags.Add(s.Target.Span(), fmt.Sprintf("switch target must be enum for now, got %s", targetType.String()))
	}

	seenEnumVariants := map[string]source.Span{}
	seenExprCases := map[string]source.Span{}
	seenDefault := false
	var previousDefault source.Span

	for _, swCase := range s.Cases {
		caseScope := NewScope(scope)

		switch swCase.Kind {
		case ast.SwitchCaseEnumVariant:
			key := swCase.EnumVariant.Name

			if prev, ok := seenEnumVariants[key]; ok {
				c.addDuplicateCaseDiagnostic(swCase.EnumVariant.Span(), prev, "."+key)
			} else {
				seenEnumVariants[key] = swCase.EnumVariant.Span()
			}

			if targetType.Kind == TypeEnum && !c.enumHasVariant(targetType, swCase.EnumVariant.Name) {
				c.diags.Add(
					swCase.EnumVariant.Span(),
					fmt.Sprintf("enum %s has no variant .%s", targetType.String(), swCase.EnumVariant.Name),
				)
			}

		case ast.SwitchCaseDefault:
			if seenDefault {
				c.addDuplicateCaseDiagnostic(swCase.Loc, previousDefault, "default")
			} else {
				seenDefault = true
				previousDefault = swCase.Loc
			}

		case ast.SwitchCaseExpr:
			caseType := c.checkExpr(scope, swCase.Expr)
			c.checkAssignable(targetType, caseType, swCase.Expr.Span())

			key := switchCaseTypeKey(caseType) + ":" + swCase.Expr.Span().String()
			if lit, ok := swCase.Expr.(*ast.IntLitExpr); ok {
				key = "int:" + lit.Value
			} else if lit, ok := swCase.Expr.(*ast.StringLitExpr); ok {
				key = "string:" + lit.Value
			} else if lit, ok := swCase.Expr.(*ast.BoolLitExpr); ok {
				key = "bool:" + fmt.Sprintf("%v", lit.Value)
			} else if dot, ok := swCase.Expr.(*ast.DotIdentExpr); ok {
				key = "enum:" + dot.Name.Name
			}

			if prev, ok := seenExprCases[key]; ok {
				c.addDuplicateCaseDiagnostic(swCase.Expr.Span(), prev, key)
			} else {
				seenExprCases[key] = swCase.Expr.Span()
			}

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

	seenMembers := map[string]source.Span{}
	seenNil := false
	var previousNil source.Span
	seenDefault := false
	var previousDefault source.Span

	for _, swCase := range s.Cases {
		caseScope := NewScope(scope)

		switch swCase.Kind {
		case ast.SwitchCaseUnionMember:
			memberType := c.typeFromAst(scope, swCase.UnionMember)
			key := switchCaseTypeKey(memberType)

			if prev, ok := seenMembers[key]; ok {
				c.addDuplicateCaseDiagnostic(swCase.UnionMember.Span(), prev, key)
			} else {
				seenMembers[key] = swCase.UnionMember.Span()
			}

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
			if seenNil {
				c.addDuplicateCaseDiagnostic(swCase.Loc, previousNil, "nil")
			} else {
				seenNil = true
				previousNil = swCase.Loc
			}

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
			if seenDefault {
				c.addDuplicateCaseDiagnostic(swCase.Loc, previousDefault, "default")
			} else {
				seenDefault = true
				previousDefault = swCase.Loc
			}

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

func isOperatorOverloadName(name string) bool {
	switch name {
	case "+", "-", "*", "/", "%", "==", "!=", "<", ">", "<=", ">=", "&", "|", "^":
		return true
	default:
		return false
	}
}

func isComparisonOperatorName(name string) bool {
	switch name {
	case "==", "!=", "<", ">", "<=", ">=":
		return true
	default:
		return false
	}
}

func (c *Checker) isBuiltinPrimitiveForOperator(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeBool, TypeInt, TypeU8, TypeUsize, TypeChar, TypeF32, TypeF64, TypeString, TypeCstring:
		return true
	default:
		return false
	}
}

func ExportPackage(name string, scope *Scope) *PackageInfo {
	info := &PackageInfo{
		Name:    name,
		Symbols: map[string]*Symbol{},
	}

	if scope == nil {
		return info
	}

	for symbolName, sym := range scope.Symbols {
		if sym == nil {
			continue
		}

		switch sym.Kind {
		case SymbolConst,
			SymbolType,
			SymbolTask,
			SymbolOverload:
			info.Symbols[symbolName] = sym
		}
	}

	return info
}

func DebugSummary(scope *Scope) string {
	count := 0

	for _, sym := range scope.Symbols {
		switch sym.Name {
		case "void", "bool", "int", "u8", "usize", "rawptr", "any", "f32", "f64", "char", "string", "cstring", "Assert":
			continue
		}

		count++
	}

	return fmt.Sprintf("checked_symbols=%d", count)
}
