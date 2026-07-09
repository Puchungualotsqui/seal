package checker

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

type TypeKind int

const (
	TypeInvalid TypeKind = iota

	// Internal only. Not a Seal source type.
	TypeVoid

	TypeBool

	TypeInt
	TypeUint

	TypeI8
	TypeI16
	TypeI32
	TypeI64

	TypeU8
	TypeU16
	TypeU32
	TypeU64

	TypeF32
	TypeF64

	TypeChar
	TypeString
	TypeCstring
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
	TypeDistinct
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

	InterfaceRequirements []InterfaceRequirementInfo
	Implements            []*Type

	Params          []*Type
	Results         []*Type
	RequiredParams  int
	ParamDefaults   []ast.Expr
	ParamHasDefault []bool
	ParamIsVariadic []bool
	IsVariadic      bool
	IsExtern        bool
	ExternName      string

	IsPure        bool
	IsIntrinsic   bool
	IsTrustedPure bool

	Underlying *Type

	GenericParams  []ast.GenericParam
	IsDynInterface bool
}

type EnumVariantInfo struct {
	Name string
	Span source.Span
}

type FieldInfo struct {
	Name    string
	Type    *Type
	TypeAst ast.Type
	Span    source.Span
}

type InterfaceRequirementInfo struct {
	Name    string
	Params  []*Type
	Results []*Type
	Span    source.Span
}

func (t *Type) String() string {
	if t == nil {
		return "<nil>"
	}

	switch t.Kind {
	case TypeVoid:
		return "void"
	case TypeBool:
		return "bool"

	case TypeInt:
		return "int"
	case TypeUint:
		return "uint"

	case TypeI8:
		return "i8"
	case TypeI16:
		return "i16"
	case TypeI32:
		return "i32"
	case TypeI64:
		return "i64"

	case TypeU8:
		return "u8"
	case TypeU16:
		return "u16"
	case TypeU32:
		return "u32"
	case TypeU64:
		return "u64"

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
	case TypeRawptr:
		return "rawptr"
	case TypeAny:
		return "any"
	case TypeInvalid:
		return "<invalid>"

	case TypeDistinct:
		return t.Name

	case TypeUntypedInt:
		return "untyped int"
	case TypeUntypedFloat:
		return "untyped float"
	case TypePointer:
		return "*" + t.Elem.String()

	case TypeArray:
		if t.Inferred {
			return "[]" + t.Elem.String()
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
	InvalidType = &Type{Kind: TypeInvalid, Name: "<invalid>"}

	// Internal only.
	VoidType = &Type{Kind: TypeVoid, Name: "void"}

	BoolType = &Type{Kind: TypeBool, Name: "bool"}

	IntType  = &Type{Kind: TypeInt, Name: "int"}
	UintType = &Type{Kind: TypeUint, Name: "uint"}

	I8Type  = &Type{Kind: TypeI8, Name: "i8"}
	I16Type = &Type{Kind: TypeI16, Name: "i16"}
	I32Type = &Type{Kind: TypeI32, Name: "i32"}
	I64Type = &Type{Kind: TypeI64, Name: "i64"}

	U8Type  = &Type{Kind: TypeU8, Name: "u8"}
	U16Type = &Type{Kind: TypeU16, Name: "u16"}
	U32Type = &Type{Kind: TypeU32, Name: "u32"}
	U64Type = &Type{Kind: TypeU64, Name: "u64"}

	F32Type = &Type{Kind: TypeF32, Name: "f32"}
	F64Type = &Type{Kind: TypeF64, Name: "f64"}

	CharType    = &Type{Kind: TypeChar, Name: "char"}
	StringType  = &Type{Kind: TypeString, Name: "string"}
	CstringType = &Type{Kind: TypeCstring, Name: "cstring"}
	RawptrType  = &Type{Kind: TypeRawptr, Name: "rawptr"}
	AnyType     = &Type{Kind: TypeAny, Name: "any"}

	NilType          = &Type{Kind: TypeNil, Name: "nil"}
	UntypedIntType   = &Type{Kind: TypeUntypedInt, Name: "untyped int"}
	UntypedFloatType = &Type{Kind: TypeUntypedFloat, Name: "untyped float"}
)

var checkerBuiltinTypes = map[string]*Type{
	"bool": BoolType,

	"int":  IntType,
	"uint": UintType,

	"i8":  I8Type,
	"i16": I16Type,
	"i32": I32Type,
	"i64": I64Type,

	"u8":  U8Type,
	"u16": U16Type,
	"u32": U32Type,
	"u64": U64Type,

	"f32": F32Type,
	"f64": F64Type,

	"char":    CharType,
	"string":  StringType,
	"cstring": CstringType,
	"rawptr":  RawptrType,
	"any":     AnyType,
}

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
	Builtin  bool

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

	specializedTypes map[string]*Type

	currentResults []*Type

	preparingTasks map[*ast.TaskDecl]bool
}

func New(diags *diag.Reporter) *Checker {
	return NewWithPackages(diags, nil)
}

func NewWithPackages(diags *diag.Reporter, packages map[string]*PackageInfo) *Checker {
	c := &Checker{
		diags:            diags,
		packages:         packages,
		specializedTypes: map[string]*Type{},
		preparingTasks:   map[*ast.TaskDecl]bool{},
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

	// Check impls before task bodies, so interface assignments inside tasks
	// can see Type.Implements.
	for _, decl := range file.Decls {
		if impl, ok := decl.(*ast.ImplDecl); ok {
			c.checkImplDecl(c.global, impl)
		}
	}

	for _, decl := range file.Decls {
		if _, ok := decl.(*ast.ImplDecl); ok {
			continue
		}

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

func (c *Checker) checkCallArgumentTypes(scope *Scope, args []ast.Expr) ([]*Type, []source.Span) {
	var types []*Type
	var spans []source.Span

	for i, arg := range args {
		spread, ok := arg.(*ast.SpreadExpr)
		if !ok {
			types = append(types, c.checkExpr(scope, arg))
			spans = append(spans, arg.Span())
			continue
		}

		if i != len(args)-1 {
			c.diags.Add(spread.Span(), "spread argument must be the last argument")
		}

		spreadType := c.checkExpr(scope, spread.Expr)

		switch spreadType.Kind {
		case TypeArray:
			if spreadType.Elem == nil {
				types = append(types, InvalidType)
				spans = append(spans, spread.Span())
				continue
			}

			types = append(types, &Type{
				Kind: TypeVariadic,
				Elem: spreadType.Elem,
				Name: "..." + spreadType.Elem.String(),
			})
			spans = append(spans, spread.Span())

		case TypeVariadic:
			if spreadType.Elem == nil {
				types = append(types, InvalidType)
				spans = append(spans, spread.Span())
				continue
			}

			types = append(types, &Type{
				Kind: TypeVariadic,
				Elem: spreadType.Elem,
				Name: "..." + spreadType.Elem.String(),
			})
			spans = append(spans, spread.Span())

		default:
			c.diags.Add(spread.Span(), fmt.Sprintf("cannot spread %s; expected array or variadic value", spreadType.String()))
			types = append(types, InvalidType)
			spans = append(spans, spread.Span())
		}
	}

	return types, spans
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
			if argTypes[i] != nil && argTypes[i].Kind == TypeVariadic {
				return 0, false
			}

			itemScore, ok := c.conversionScore(taskType.Params[i], argTypes[i])
			if !ok {
				return 0, false
			}

			score += itemScore
		}

		for i := fixedCount; i < len(argTypes); i++ {
			got := argTypes[i]
			if got != nil && got.Kind == TypeVariadic {
				got = got.Elem
			}

			itemScore, ok := c.conversionScore(variadicElem, got)
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
		if argTypes[i] != nil && argTypes[i].Kind == TypeVariadic {
			return 0, false
		}

		itemScore, ok := c.conversionScore(taskType.Params[i], argTypes[i])
		if !ok {
			return 0, false
		}

		score += itemScore
	}

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

	if dst.Kind == TypeDistinct {
		if src.Kind == TypeUntypedInt || src.Kind == TypeUntypedFloat {
			if c.assignable(dst.Underlying, src) {
				return 1, true
			}
		}

		return 0, false
	}

	if src.Kind == TypeDistinct {
		return 0, false
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

	if dst.Kind == TypeInterface {
		if src.Kind == TypeNil {
			return 1, true
		}

		if src.Kind == TypePointer && c.typeImplementsInterface(src.Elem, dst) {
			return 5, true
		}

		return 0, false
	}

	if src.Kind == TypeNil {
		if dst.Kind == TypePointer || dst.Kind == TypeRawptr || dst.Kind == TypeUnion || dst.Kind == TypeInterface {
			return 1, true
		}

		return 0, false
	}

	if src.Kind == TypeUntypedInt {
		if c.isIntegerLike(dst) {
			return 1, true
		}

		switch dst.Kind {
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

	c.diags.Add(span, "multi-result task call cannot be used as a single expression; destructure it with a, b :=")
	return InvalidType
}

func (c *Checker) declareBuiltins(scope *Scope) {
	for name, typ := range checkerBuiltinTypes {
		scope.Declare(&Symbol{
			Name:    name,
			Kind:    SymbolType,
			Type:    typ,
			Builtin: true,
		})
	}
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

	case *ast.DistinctDecl:
		scope.Declare(&Symbol{
			Name: d.Name.Name,
			Kind: SymbolType,
			Type: &Type{
				Kind: TypeDistinct,
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
		// c :: @c_import { ... } is codegen metadata, not a visible Seal symbol.
		return

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

func (c *Checker) prepareTaskSymbolType(scope *Scope, sym *Symbol, d *ast.TaskDecl) *Type {
	if sym == nil || d == nil {
		return InvalidType
	}

	if sym.Type != nil && sym.Type.Kind == TypeTask {
		return sym.Type
	}

	if c.preparingTasks[d] {
		if sym.Type != nil {
			return sym.Type
		}

		return InvalidType
	}

	c.preparingTasks[d] = true
	taskType := c.taskTypeFromDecl(scope, d)
	delete(c.preparingTasks, d)

	sym.Type = taskType
	return taskType
}

func (c *Checker) ensureTaskSymbolPrepared(scope *Scope, sym *Symbol) {
	if sym == nil || sym.Kind != SymbolTask {
		return
	}

	if sym.Type != nil && sym.Type.Kind == TypeTask {
		return
	}

	taskDecl, ok := sym.Node.(*ast.TaskDecl)
	if !ok {
		return
	}

	if scope == nil {
		scope = c.global
	}

	c.prepareTaskSymbolType(scope, sym, taskDecl)
}

func (c *Checker) prepareDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.DistinctDecl:
		c.prepareDistinctDecl(scope, d)

	case *ast.StructDecl:
		c.prepareStructDecl(scope, d)

	case *ast.EnumDecl:
		c.prepareEnumDecl(scope, d)

	case *ast.UnionDecl:
		c.prepareUnionDecl(scope, d)

	case *ast.TaskDecl:
		sym := scope.LookupLocal(d.Name.Name)
		if sym != nil {
			c.prepareTaskSymbolType(scope, sym, d)
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

func (c *Checker) prepareDistinctDecl(scope *Scope, d *ast.DistinctDecl) {
	sym := scope.LookupLocal(d.Name.Name)
	if sym == nil || sym.Type == nil {
		return
	}

	underlying := c.typeFromAst(scope, d.Underlying)

	if !c.isValidDistinctUnderlying(underlying) {
		c.diags.Add(
			d.Underlying.Span(),
			fmt.Sprintf("distinct underlying type must be a concrete primitive type, got %s", underlying.String()),
		)
	}

	sym.Type.Underlying = underlying
}

func (c *Checker) prepareStructDecl(parent *Scope, d *ast.StructDecl) {
	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil || sym.Type == nil {
		return
	}

	scope := c.scopeWithGenericParams(parent, d.GenericParams)

	var fields []FieldInfo

	for _, field := range d.Fields {
		fieldType := c.typeFromAst(scope, field.Type)

		fields = append(fields, FieldInfo{
			Name:    field.Name.Name,
			Type:    fieldType,
			TypeAst: field.Type,
			Span:    field.Name.Span(),
		})
	}

	sym.Type.Fields = fields
	sym.Type.GenericParams = d.GenericParams
}

func (c *Checker) prepareInterfaceDecl(parent *Scope, d *ast.InterfaceDecl) {
	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil || sym.Type == nil {
		return
	}

	scope := c.scopeWithGenericParams(parent, d.GenericParams)

	var requirements []InterfaceRequirementInfo

	for _, req := range d.Requirements {
		var params []*Type
		var results []*Type

		for _, param := range req.Params {
			params = append(params, c.typeFromAst(scope, param.Type))
		}

		for _, result := range req.Results {
			results = append(results, c.typeFromAst(scope, result))
		}

		requirements = append(requirements, InterfaceRequirementInfo{
			Name:    req.Name.Name,
			Params:  params,
			Results: results,
			Span:    req.Loc,
		})
	}

	sym.Type.InterfaceRequirements = requirements
	sym.Type.GenericParams = d.GenericParams
	sym.Type.IsDynInterface = d.IsDyn
}

func (c *Checker) scopeWithGenericParams(parent *Scope, params []ast.GenericParam) *Scope {
	if len(params) == 0 {
		return parent
	}

	scope := NewScope(parent)
	c.declareGenericParams(scope, params)
	return scope
}

func (c *Checker) declareGenericParams(scope *Scope, params []ast.GenericParam) {
	for _, param := range params {
		c.declareGenericParamSymbol(scope, param)
	}

	for _, param := range params {
		c.checkGenericParamConstraints(scope, param)
	}
}

func (c *Checker) declareGenericParamSymbol(scope *Scope, param ast.GenericParam) {
	if param.Name.Name == "" {
		return
	}

	switch param.Category {
	case ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion:
		scope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolType,
			Type: &Type{
				Kind:          TypeTypeParam,
				Name:          param.Name.Name,
				GenericParams: nil,
			},
			Span: param.Name.Span(),
		})

	case ast.GenericParamTask:
		scope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolTask,
			Type: c.taskTypeFromGenericTaskParam(scope, param),
			Span: param.Name.Span(),
		})

	case ast.GenericParamInt:
		scope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolConst,
			Type: IntType,
			Span: param.Name.Span(),
		})

	case ast.GenericParamBool:
		scope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolConst,
			Type: BoolType,
			Span: param.Name.Span(),
		})

	case ast.GenericParamString:
		scope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolConst,
			Type: StringType,
			Span: param.Name.Span(),
		})

	case ast.GenericParamValue:
		typ := InvalidType
		if param.Type != nil {
			typ = c.typeFromAst(scope, param.Type)
		}

		scope.Declare(&Symbol{
			Name: param.Name.Name,
			Kind: SymbolConst,
			Type: typ,
			Span: param.Name.Span(),
		})
	}
}

func (c *Checker) taskTypeFromGenericTaskParam(scope *Scope, param ast.GenericParam) *Type {
	taskType := &Type{
		Kind: TypeTask,
		Name: param.Name.Name,
	}

	for _, constraint := range param.Constraints {
		taskConstraint, ok := constraint.(*ast.GenericTaskConstraint)
		if !ok {
			continue
		}

		for _, p := range taskConstraint.Params {
			taskType.Params = append(taskType.Params, c.typeFromAst(scope, p))
		}

		for _, r := range taskConstraint.Results {
			taskType.Results = append(taskType.Results, c.typeFromAst(scope, r))
		}

		taskType.RequiredParams = len(taskType.Params)
		return taskType
	}

	return taskType
}

func (c *Checker) checkGenericParamConstraints(scope *Scope, param ast.GenericParam) {
	if param.Category == ast.GenericParamTask {
		taskConstraints := 0

		for _, constraint := range param.Constraints {
			if _, ok := constraint.(*ast.GenericTaskConstraint); ok {
				taskConstraints++
			}
		}

		if taskConstraints == 0 {
			c.diags.Add(param.Name.Span(), fmt.Sprintf("generic task parameter %q requires a task signature constraint", param.Name.Name))
		}

		if taskConstraints > 1 {
			c.diags.Add(param.Name.Span(), fmt.Sprintf("generic task parameter %q can have only one task signature constraint", param.Name.Name))
		}
	}

	for _, constraint := range param.Constraints {
		switch x := constraint.(type) {
		case *ast.GenericExprConstraint:
			t := c.checkExpr(scope, x.Expr)
			c.checkBoolCondition(t, x.Expr.Span(), "generic constraint must be bool")

		case *ast.GenericFieldConstraint:
			fieldType := InvalidType
			if x.HasType && x.Type != nil {
				fieldType = c.typeFromAst(scope, x.Type)
			}

			if param.Category == ast.GenericParamType {
				c.addFieldConstraintToTypeParam(scope, param, x, fieldType)
			}

		case *ast.GenericImplConstraint:
			iface := c.typeFromAst(scope, x.Interface)
			if iface.Kind != TypeInterface && iface.Kind != TypeTypeParam && iface.Kind != TypeInvalid {
				c.diags.Add(x.Interface.Span(), fmt.Sprintf("generic implementation constraint must name an interface, got %s", iface.String()))
			}

			if param.Category == ast.GenericParamType {
				c.addImplConstraintToTypeParam(scope, param, iface)
			}

		case *ast.GenericEnumVariantConstraint:
			// Name-only constraint. Checked at instantiation.

		case *ast.GenericUnionMemberConstraint:
			c.typeFromAst(scope, x.Member)

		case *ast.GenericTaskConstraint:
			for _, p := range x.Params {
				c.typeFromAst(scope, p)
			}

			for _, r := range x.Results {
				c.typeFromAst(scope, r)
			}
		}
	}
}

func (c *Checker) addFieldConstraintToTypeParam(scope *Scope, param ast.GenericParam, constraint *ast.GenericFieldConstraint, fieldType *Type) {
	sym := scope.LookupLocal(param.Name.Name)
	if sym == nil || sym.Type == nil || sym.Type.Kind != TypeTypeParam {
		return
	}

	for i := range sym.Type.Fields {
		if sym.Type.Fields[i].Name == constraint.Name.Name {
			if constraint.HasType {
				sym.Type.Fields[i].Type = fieldType
			}

			return
		}
	}

	sym.Type.Fields = append(sym.Type.Fields, FieldInfo{
		Name: constraint.Name.Name,
		Type: fieldType,
		Span: constraint.Name.Span(),
	})
}

func (c *Checker) addImplConstraintToTypeParam(scope *Scope, param ast.GenericParam, iface *Type) {
	if iface == nil || iface.Kind == TypeInvalid {
		return
	}

	sym := scope.LookupLocal(param.Name.Name)
	if sym == nil || sym.Type == nil || sym.Type.Kind != TypeTypeParam {
		return
	}

	for _, existing := range sym.Type.Implements {
		if c.sameType(existing, iface) {
			return
		}
	}

	sym.Type.Implements = append(sym.Type.Implements, iface)
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
		c.checkImplDecl(scope, d)

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

func (c *Checker) checkImplDecl(scope *Scope, d *ast.ImplDecl) {
	ifaceType, concreteType, subst, ok := c.implTypesFromDecl(scope, d)
	if !ok {
		return
	}

	if ifaceType.Kind != TypeInterface {
		c.diags.Add(d.Interface.Span(), fmt.Sprintf("%s is not an interface", ifaceType.String()))
		return
	}

	c.checkImplEntries(scope, d, ifaceType, concreteType, subst)

	if !c.typeImplementsInterface(concreteType, ifaceType) {
		concreteType.Implements = append(concreteType.Implements, ifaceType)
	}
}

func (c *Checker) implTypesFromDecl(scope *Scope, d *ast.ImplDecl) (*Type, *Type, map[string]*Type, bool) {
	gen, ok := d.Interface.(*ast.GenericType)
	if !ok {
		c.diags.Add(d.Interface.Span(), "impl must specialize an interface, for example: Drawable<Sprite> :: impl")
		return InvalidType, InvalidType, nil, false
	}

	ifaceType := c.typeFromAst(scope, gen.Base)
	if ifaceType.Kind != TypeInterface {
		c.diags.Add(gen.Base.Span(), fmt.Sprintf("%s is not an interface", ifaceType.String()))
		return InvalidType, InvalidType, nil, false
	}

	if len(ifaceType.GenericParams) == 0 {
		c.diags.Add(gen.Span(), fmt.Sprintf("interface %s has no generic parameters", ifaceType.String()))
		return InvalidType, InvalidType, nil, false
	}

	if len(gen.Args) != len(ifaceType.GenericParams) {
		c.diags.Add(
			gen.Span(),
			fmt.Sprintf("interface %s expects %d generic argument(s), got %d", ifaceType.String(), len(ifaceType.GenericParams), len(gen.Args)),
		)
		return InvalidType, InvalidType, nil, false
	}

	subst := map[string]*Type{}
	var concreteType *Type

	for i, param := range ifaceType.GenericParams {
		argType := c.typeFromGenericArg(scope, gen.Args[i])
		subst[param.Name.Name] = argType

		if i == 0 {
			concreteType = argType
		}
	}

	if concreteType == nil || concreteType.Kind == TypeInvalid {
		return InvalidType, InvalidType, nil, false
	}

	if concreteType.Kind == TypeInterface {
		c.diags.Add(gen.Args[0].Span(), "generic interface implementation target cannot be an interface")
		return InvalidType, InvalidType, nil, false
	}

	return ifaceType, concreteType, subst, true
}

func (c *Checker) checkImplEntries(scope *Scope, d *ast.ImplDecl, ifaceType *Type, concreteType *Type, subst map[string]*Type) {
	entries := map[string]ast.ImplEntry{}

	for _, entry := range d.Entries {
		if _, exists := entries[entry.Name.Name]; exists {
			c.diags.Add(entry.Name.Span(), fmt.Sprintf("duplicate impl entry %q", entry.Name.Name))
			continue
		}

		entries[entry.Name.Name] = entry
	}

	for _, req := range ifaceType.InterfaceRequirements {
		entry, ok := entries[req.Name]
		if !ok {
			c.diags.Add(
				d.Interface.Span(),
				fmt.Sprintf("type %s does not implement %s: missing requirement %q",
					concreteType.String(),
					ifaceType.String(),
					req.Name,
				),
			)
			continue
		}

		expectedParams := make([]*Type, 0, len(req.Params))
		for _, p := range req.Params {
			expectedParams = append(expectedParams, c.substituteGenericTypes(p, subst))
		}

		expectedResults := make([]*Type, 0, len(req.Results))
		for _, r := range req.Results {
			expectedResults = append(expectedResults, c.substituteGenericTypes(r, subst))
		}

		if entry.Task != nil {
			taskType := c.taskTypeFromDecl(scope, entry.Task)

			if !c.taskSignatureMatches(taskType, expectedParams, expectedResults) {
				c.diags.Add(entry.Name.Span(), fmt.Sprintf("impl entry %q has wrong signature; expected %s", entry.Name.Name, c.formatTaskSignature(req.Name, expectedParams, expectedResults)))
				continue
			}

			c.checkInlineImplTask(scope, entry.Task, taskType)
			continue
		}

		if entry.Alias != nil {
			aliasType := c.checkExpr(scope, entry.Alias)

			if !c.taskSignatureMatches(aliasType, expectedParams, expectedResults) {
				c.diags.Add(entry.Alias.Span(), fmt.Sprintf("impl entry %q has wrong signature; expected %s", entry.Name.Name, c.formatTaskSignature(req.Name, expectedParams, expectedResults)))
			}
		}
	}
}

func (c *Checker) checkInlineImplTask(parent *Scope, d *ast.TaskDecl, taskType *Type) {
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
	}

	oldResults := c.currentResults
	c.currentResults = taskType.Results

	c.checkBlockInScope(taskScope, d.Body, false)

	c.currentResults = oldResults
}

func (c *Checker) taskSignatureMatches(taskType *Type, expectedParams []*Type, expectedResults []*Type) bool {
	if taskType == nil || taskType.Kind != TypeTask {
		return false
	}

	if len(taskType.Params) != len(expectedParams) || len(taskType.Results) != len(expectedResults) {
		return false
	}

	for i := range expectedParams {
		if !c.sameType(taskType.Params[i], expectedParams[i]) {
			return false
		}
	}

	for i := range expectedResults {
		if !c.sameType(taskType.Results[i], expectedResults[i]) {
			return false
		}
	}

	return true
}

func (c *Checker) typeImplementsInterface(concreteType *Type, ifaceType *Type) bool {
	if concreteType == nil || ifaceType == nil {
		return false
	}

	for _, implemented := range concreteType.Implements {
		if c.sameType(implemented, ifaceType) {
			return true
		}
	}

	return false
}

func (c *Checker) formatTaskSignature(name string, params []*Type, results []*Type) string {
	var paramParts []string
	for _, param := range params {
		paramParts = append(paramParts, param.String())
	}

	var resultParts []string
	for _, result := range results {
		resultParts = append(resultParts, result.String())
	}

	if len(resultParts) == 0 {
		return fmt.Sprintf("%s :: task(%s)", name, strings.Join(paramParts, ", "))
	}

	return fmt.Sprintf("%s :: task(%s) %s", name, strings.Join(paramParts, ", "), strings.Join(resultParts, ", "))
}

func (c *Checker) checkInterfaceDecl(scope *Scope, d *ast.InterfaceDecl) {
	if len(d.GenericParams) == 0 {
		c.diags.Add(d.Name.Span(), fmt.Sprintf("interface %q must declare its concrete type parameter, for example: interface <T type>", d.Name.Name))
		return
	}

	selfName := d.GenericParams[0].Name.Name

	if d.GenericParams[0].Category != ast.GenericParamType {
		c.diags.Add(d.GenericParams[0].Name.Span(), "first interface generic parameter must be a type parameter")
	}

	for _, req := range d.Requirements {
		if len(req.Params) == 0 {
			c.diags.Add(req.Name.Span(), fmt.Sprintf("interface requirement %q must have a self parameter *%s", req.Name.Name, selfName))
			continue
		}

		if !isInterfaceSelfParam(req.Params[0].Type, selfName) {
			c.diags.Add(req.Params[0].Name.Span(), fmt.Sprintf("first parameter of interface requirement %q must be *%s", req.Name.Name, selfName))
		}

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

func isInterfaceSelfParam(t ast.Type, selfName string) bool {
	ptr, ok := t.(*ast.PointerType)
	if !ok {
		return false
	}

	named, ok := ptr.Elem.(*ast.NamedType)
	if !ok {
		return false
	}

	return len(named.Parts) == 1 && named.Parts[0].Name == selfName
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

	genericScope := c.scopeWithGenericParams(parent, d.GenericParams)

	c.checkTaskDefaultParameters(genericScope, d, taskType)

	if d.IsExtern {
		if d.IsPure && !d.IsTrustedPure {
			c.diags.Add(d.Name.Span(), "extern task cannot be marked pure; use @trusted_pure extern(...) if this C function is safe to treat as pure")
		}

		if d.Body != nil {
			c.diags.Add(d.Body.Span(), fmt.Sprintf("extern task %q cannot have a body", d.Name.Name))
		}

		return
	}

	if d.IsIntrinsic {
		if d.Body != nil {
			c.diags.Add(d.Body.Span(), fmt.Sprintf("intrinsic task %q cannot have a body", d.Name.Name))
		}

		return
	}

	taskScope := NewScope(genericScope)

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

func (c *Checker) taskTypeFromImportedGenericSignature(scope *Scope, packageName string, generic *Type, args []ast.GenericArg, span source.Span) *Type {
	if generic == nil || generic.Kind != TypeTask {
		return &Type{
			Kind:    TypeTask,
			Name:    "<invalid>",
			Results: []*Type{InvalidType},
		}
	}

	if len(args) != len(generic.GenericParams) {
		return &Type{
			Kind:    TypeTask,
			Name:    generic.Name,
			Results: []*Type{InvalidType},
		}
	}

	argSubst := genericArgSubst(generic.GenericParams, args)
	typeSubst := c.genericTypeSubstFromArgs(scope, generic.GenericParams, args)

	var params []*Type
	for _, param := range generic.Params {
		params = append(params, c.substituteImportedGenericSignatureType(scope, packageName, param, typeSubst, argSubst))
	}

	var results []*Type
	for _, result := range generic.Results {
		results = append(results, c.substituteImportedGenericSignatureType(scope, packageName, result, typeSubst, argSubst))
	}

	paramDefaults := make([]ast.Expr, 0, len(generic.ParamDefaults))
	for _, defaultExpr := range generic.ParamDefaults {
		paramDefaults = append(paramDefaults, c.substituteGenericExpr(defaultExpr, argSubst))
	}

	return &Type{
		Kind:            TypeTask,
		Name:            c.specializedTypeName(packageName+"."+generic.Name, args),
		Params:          params,
		Results:         results,
		RequiredParams:  generic.RequiredParams,
		ParamDefaults:   paramDefaults,
		ParamHasDefault: append([]bool(nil), generic.ParamHasDefault...),
		ParamIsVariadic: append([]bool(nil), generic.ParamIsVariadic...),
		IsVariadic:      generic.IsVariadic,
		IsExtern:        generic.IsExtern,
		ExternName:      generic.ExternName,
		IsPure:          generic.IsPure,
		IsIntrinsic:     generic.IsIntrinsic,
		IsTrustedPure:   generic.IsTrustedPure,
	}
}

func (c *Checker) substituteImportedGenericSignatureType(scope *Scope, packageName string, typ *Type, typeSubst map[string]*Type, argSubst map[string]ast.GenericArg) *Type {
	if typ == nil {
		return InvalidType
	}

	switch typ.Kind {
	case TypeTypeParam:
		// Important: substituted caller-provided type arguments must NOT be
		// qualified with the imported package name.
		if replacement := typeSubst[typ.Name]; replacement != nil {
			return replacement
		}

		return typ

	case TypePointer:
		return &Type{
			Kind: TypePointer,
			Elem: c.substituteImportedGenericSignatureType(scope, packageName, typ.Elem, typeSubst, argSubst),
		}

	case TypeArray:
		return &Type{
			Kind:     TypeArray,
			Name:     typ.Name,
			Elem:     c.substituteImportedGenericSignatureType(scope, packageName, typ.Elem, typeSubst, argSubst),
			Len:      typ.Len,
			Inferred: typ.Inferred,
		}

	case TypeVariadic:
		return &Type{
			Kind: TypeVariadic,
			Name: typ.Name,
			Elem: c.substituteImportedGenericSignatureType(scope, packageName, typ.Elem, typeSubst, argSubst),
		}

	case TypeStruct:
		out := *typ
		out.Fields = nil
		out.GenericParams = nil

		if typ.Name != "" {
			out.Name = c.substitutedGenericDisplayName(typ.Name, typeSubst)

			if packageName != "" && !strings.Contains(out.Name, ".") {
				out.Name = packageName + "." + out.Name
			}
		}

		for _, field := range typ.Fields {
			out.Fields = append(out.Fields, FieldInfo{
				Name:    field.Name,
				Type:    c.substituteImportedGenericSignatureType(scope, packageName, field.Type, typeSubst, argSubst),
				TypeAst: field.TypeAst,
				Span:    field.Span,
			})
		}

		return &out

	case TypeUnion:
		out := *typ
		out.Members = nil

		if out.Name != "" && packageName != "" && !strings.Contains(out.Name, ".") {
			out.Name = packageName + "." + out.Name
		}

		for _, member := range typ.Members {
			out.Members = append(out.Members, c.substituteImportedGenericSignatureType(scope, packageName, member, typeSubst, argSubst))
		}

		return &out

	case TypeInterface:
		out := *typ
		out.InterfaceRequirements = nil

		if out.Name != "" && packageName != "" && !strings.Contains(out.Name, ".") {
			out.Name = packageName + "." + out.Name
		}

		for _, req := range typ.InterfaceRequirements {
			cloned := InterfaceRequirementInfo{
				Name: req.Name,
				Span: req.Span,
			}

			for _, param := range req.Params {
				cloned.Params = append(cloned.Params, c.substituteImportedGenericSignatureType(scope, packageName, param, typeSubst, argSubst))
			}

			for _, result := range req.Results {
				cloned.Results = append(cloned.Results, c.substituteImportedGenericSignatureType(scope, packageName, result, typeSubst, argSubst))
			}

			out.InterfaceRequirements = append(out.InterfaceRequirements, cloned)
		}

		return &out

	case TypeTask:
		out := *typ
		out.Params = nil
		out.Results = nil

		for _, param := range typ.Params {
			out.Params = append(out.Params, c.substituteImportedGenericSignatureType(scope, packageName, param, typeSubst, argSubst))
		}

		for _, result := range typ.Results {
			out.Results = append(out.Results, c.substituteImportedGenericSignatureType(scope, packageName, result, typeSubst, argSubst))
		}

		return &out

	default:
		return typ
	}
}

func (c *Checker) taskTypeFromGenericSignature(scope *Scope, generic *Type, args []ast.GenericArg, span source.Span) *Type {
	if generic == nil || generic.Kind != TypeTask {
		return &Type{
			Kind:    TypeTask,
			Name:    "<invalid>",
			Results: []*Type{InvalidType},
		}
	}

	if len(args) != len(generic.GenericParams) {
		return &Type{
			Kind:    TypeTask,
			Name:    generic.Name,
			Results: []*Type{InvalidType},
		}
	}

	argSubst := genericArgSubst(generic.GenericParams, args)
	typeSubst := c.genericTypeSubstFromArgs(scope, generic.GenericParams, args)

	var params []*Type
	for _, param := range generic.Params {
		params = append(params, c.substituteGenericSignatureType(scope, param, typeSubst, argSubst))
	}

	var results []*Type
	for _, result := range generic.Results {
		results = append(results, c.substituteGenericSignatureType(scope, result, typeSubst, argSubst))
	}

	paramDefaults := make([]ast.Expr, 0, len(generic.ParamDefaults))
	for _, defaultExpr := range generic.ParamDefaults {
		paramDefaults = append(paramDefaults, c.substituteGenericExpr(defaultExpr, argSubst))
	}

	return &Type{
		Kind:            TypeTask,
		Name:            c.specializedTypeName(generic.Name, args),
		Params:          params,
		Results:         results,
		RequiredParams:  generic.RequiredParams,
		ParamDefaults:   paramDefaults,
		ParamHasDefault: append([]bool(nil), generic.ParamHasDefault...),
		ParamIsVariadic: append([]bool(nil), generic.ParamIsVariadic...),
		IsVariadic:      generic.IsVariadic,
		IsExtern:        generic.IsExtern,
		ExternName:      generic.ExternName,
		IsPure:          generic.IsPure,
		IsIntrinsic:     generic.IsIntrinsic,
		IsTrustedPure:   generic.IsTrustedPure,
	}
}

func (c *Checker) substituteGenericSignatureType(scope *Scope, typ *Type, typeSubst map[string]*Type, argSubst map[string]ast.GenericArg) *Type {
	if typ == nil {
		return InvalidType
	}

	switch typ.Kind {
	case TypeTypeParam:
		if replacement := typeSubst[typ.Name]; replacement != nil {
			return replacement
		}

		return typ

	case TypePointer:
		return &Type{
			Kind: TypePointer,
			Elem: c.substituteGenericSignatureType(scope, typ.Elem, typeSubst, argSubst),
		}

	case TypeArray:
		return &Type{
			Kind:     TypeArray,
			Name:     typ.Name,
			Elem:     c.substituteGenericSignatureType(scope, typ.Elem, typeSubst, argSubst),
			Len:      typ.Len,
			Inferred: typ.Inferred,
		}

	case TypeVariadic:
		return &Type{
			Kind: TypeVariadic,
			Name: typ.Name,
			Elem: c.substituteGenericSignatureType(scope, typ.Elem, typeSubst, argSubst),
		}

	case TypeStruct:
		out := *typ
		out.Fields = nil
		out.GenericParams = nil

		if typ.Name != "" {
			out.Name = c.substitutedGenericDisplayName(typ.Name, typeSubst)
		}

		for _, field := range typ.Fields {
			out.Fields = append(out.Fields, FieldInfo{
				Name:    field.Name,
				Type:    c.substituteGenericSignatureType(scope, field.Type, typeSubst, argSubst),
				TypeAst: field.TypeAst,
				Span:    field.Span,
			})
		}

		return &out

	case TypeUnion:
		out := *typ
		out.Members = nil

		for _, member := range typ.Members {
			out.Members = append(out.Members, c.substituteGenericSignatureType(scope, member, typeSubst, argSubst))
		}

		return &out

	case TypeInterface:
		out := *typ
		out.InterfaceRequirements = nil

		for _, req := range typ.InterfaceRequirements {
			cloned := InterfaceRequirementInfo{
				Name: req.Name,
				Span: req.Span,
			}

			for _, param := range req.Params {
				cloned.Params = append(cloned.Params, c.substituteGenericSignatureType(scope, param, typeSubst, argSubst))
			}

			for _, result := range req.Results {
				cloned.Results = append(cloned.Results, c.substituteGenericSignatureType(scope, result, typeSubst, argSubst))
			}

			out.InterfaceRequirements = append(out.InterfaceRequirements, cloned)
		}

		return &out

	case TypeTask:
		out := *typ
		out.Params = nil
		out.Results = nil

		for _, param := range typ.Params {
			out.Params = append(out.Params, c.substituteGenericSignatureType(scope, param, typeSubst, argSubst))
		}

		for _, result := range typ.Results {
			out.Results = append(out.Results, c.substituteGenericSignatureType(scope, result, typeSubst, argSubst))
		}

		return &out

	default:
		return typ
	}
}

func (c *Checker) substitutedGenericDisplayName(name string, typeSubst map[string]*Type) string {
	if name == "" || len(typeSubst) == 0 {
		return name
	}

	out := name

	keys := make([]string, 0, len(typeSubst))
	for key := range typeSubst {
		keys = append(keys, key)
	}

	// Longer names first avoids replacing T inside TypeName-like params.
	sort.Strings(keys)
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}

	for _, key := range keys {
		replacement := typeSubst[key]
		if replacement == nil {
			continue
		}

		out = replaceGenericDisplayToken(out, key, replacement.String())
	}

	return out
}

func replaceGenericDisplayToken(input string, token string, replacement string) string {
	if token == "" {
		return input
	}

	var b strings.Builder

	for i := 0; i < len(input); {
		if strings.HasPrefix(input[i:], token) &&
			isGenericDisplayBoundary(input, i-1) &&
			isGenericDisplayBoundary(input, i+len(token)) {
			b.WriteString(replacement)
			i += len(token)
			continue
		}

		b.WriteByte(input[i])
		i++
	}

	return b.String()
}

func isGenericDisplayBoundary(input string, index int) bool {
	if index < 0 || index >= len(input) {
		return true
	}

	ch := input[index]

	if ch >= 'a' && ch <= 'z' ||
		ch >= 'A' && ch <= 'Z' ||
		ch >= '0' && ch <= '9' ||
		ch == '_' {
		return false
	}

	return true
}

func (c *Checker) taskTypeFromGenericCall(scope *Scope, d *ast.TaskDecl, args []ast.GenericArg) *Type {
	if d == nil {
		return &Type{
			Kind:    TypeTask,
			Name:    "<invalid>",
			Results: []*Type{InvalidType},
		}
	}

	if len(args) != len(d.GenericParams) {
		return &Type{
			Kind:    TypeTask,
			Name:    d.Name.Name,
			Results: []*Type{InvalidType},
		}
	}

	subst := genericArgSubst(d.GenericParams, args)

	var params []*Type
	var paramDefaults []ast.Expr
	var paramHasDefault []bool
	var paramIsVariadic []bool

	requiredParams := len(d.Params)
	isVariadic := false

	for i, param := range d.Params {
		params = append(params, c.typeFromAstWithGenericArgs(scope, param.Type, subst))

		defaultExpr := param.Default
		if param.HasDefault {
			defaultExpr = c.substituteGenericExpr(param.Default, subst)
		}

		paramDefaults = append(paramDefaults, defaultExpr)
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
		results = append(results, c.typeFromAstWithGenericArgs(scope, result, subst))
	}

	return &Type{
		Kind:            TypeTask,
		Name:            c.specializedTypeName(d.Name.Name, args),
		Params:          params,
		Results:         results,
		RequiredParams:  requiredParams,
		ParamDefaults:   paramDefaults,
		ParamHasDefault: paramHasDefault,
		ParamIsVariadic: paramIsVariadic,
		IsVariadic:      isVariadic,
		IsExtern:        d.IsExtern,
		ExternName:      d.ExternName,
		IsPure:          d.IsPure,
		IsIntrinsic:     d.IsIntrinsic,
		IsTrustedPure:   d.IsTrustedPure,
	}
}

func (c *Checker) taskTypeFromDecl(scope *Scope, d *ast.TaskDecl) *Type {
	genericScope := c.scopeWithGenericParams(scope, d.GenericParams)

	var params []*Type
	var paramDefaults []ast.Expr
	var paramHasDefault []bool
	var paramIsVariadic []bool

	requiredParams := len(d.Params)
	isVariadic := false

	for i, param := range d.Params {
		params = append(params, c.typeFromAst(genericScope, param.Type))
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
		results = append(results, c.typeFromAst(genericScope, result))
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
		IsPure:          d.IsPure,
		IsIntrinsic:     d.IsIntrinsic,
		IsTrustedPure:   d.IsTrustedPure,
		GenericParams:   d.GenericParams,
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

	case *ast.MultiVarDeclStmt:
		c.checkMultiVarDeclStmt(scope, s)

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

	if len(s.Values) == 1 && len(expected) > 1 {
		if call, ok := s.Values[0].(*ast.CallExpr); ok {
			results := c.checkCallResultTypes(scope, call)

			if len(results) != len(expected) {
				c.diags.Add(
					s.Span(),
					fmt.Sprintf("return count mismatch: expected %d value(s), got %d", len(expected), len(results)),
				)
				return
			}

			for i, result := range results {
				c.checkAssignable(expected[i], result, s.Values[0].Span())
			}

			return
		}
	}

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

		if c.isByteIndexableValue(containerType) && containerType.Kind != TypeRawptr {
			if !c.isAddressableExpr(scope, index.Left) {
				c.diags.Add(index.Left.Span(), "byte-index assignment requires an addressable value")
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
		var valueType *Type

		if s.HasType {
			valueType = c.checkExprWithExpected(scope, s.Value, varType)
			c.checkAssignable(varType, valueType, s.Value.Span())

			// Preserve inferred array length:
			//
			//     a: []int = [1, 2, 3]
			//
			// The declared type starts as []int, but after checking the literal
			// we know its concrete length is 3. Keep that concrete type for later
			// operations like a...
			if varType != nil &&
				valueType != nil &&
				varType.Kind == TypeArray &&
				varType.Inferred &&
				valueType.Kind == TypeArray {
				varType = valueType
			}
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

	if s.Name.Name == "_" {
		return
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

	case *ast.SpreadExpr:
		c.diags.Add(e.Span(), "spread can only be used as a call argument")
		c.checkExpr(scope, e.Expr)
		return InvalidType

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

	case token.Percent:
		if c.isIntegerLike(left) && c.isIntegerLike(right) {
			result, ok := c.numericResultType(left, right)
			if !ok {
				c.diags.Add(e.Span(), fmt.Sprintf("mismatched integer operands: %s and %s", left.String(), right.String()))
				return InvalidType
			}

			return result
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return result
		}

		c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires integer operands", e.Op.String()))
		return InvalidType

	case token.Amp, token.Pipe, token.Caret:
		if c.isIntegerLike(left) && c.isIntegerLike(right) {
			result, ok := c.numericResultType(left, right)
			if !ok {
				c.diags.Add(e.Span(), fmt.Sprintf("mismatched integer operands: %s and %s", left.String(), right.String()))
				return InvalidType
			}

			return result
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return result
		}

		c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires integer operands", e.Op.String()))
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
		if c.distinctComparable(left, right) {
			return BoolType
		}

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

	if a.Kind == TypeRawptr && b.Kind == TypeNil {
		return true
	}

	if b.Kind == TypeRawptr && a.Kind == TypeNil {
		return true
	}

	if a.Kind == TypeDistinct && b.Kind == TypeDistinct && c.sameType(a, b) {
		return true
	}

	return false
}

func (c *Checker) checkInterfaceDispatchCall(scope *Scope, e *ast.CallExpr, argTypes []*Type, argSpans []source.Span) (*Type, bool) {
	results, ok := c.checkInterfaceDispatchCallResultTypes(scope, e, argTypes, argSpans)
	if !ok {
		return InvalidType, false
	}

	if len(results) == 0 {
		return VoidType, true
	}

	if len(results) == 1 {
		return results[0], true
	}

	c.diags.Add(e.Span(), "multi-result interface call cannot be used as a single expression yet")
	return InvalidType, true
}

func (c *Checker) checkInterfaceDispatchCallResultTypes(scope *Scope, e *ast.CallExpr, argTypes []*Type, argSpans []source.Span) ([]*Type, bool) {
	id, ok := e.Callee.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}

	if len(argTypes) == 0 {
		return nil, false
	}

	ifaceType := argTypes[0]
	if ifaceType == nil || ifaceType.Kind != TypeInterface {
		return nil, false
	}

	req := c.lookupInterfaceRequirement(ifaceType, id.Name.Name)
	if req == nil {
		return nil, false
	}

	if len(argTypes) != len(req.Params) {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf("interface call argument count mismatch: expected %d, got %d", len(req.Params), len(argTypes)),
		)
	} else {
		for i := 1; i < len(argTypes); i++ {
			c.checkAssignable(req.Params[i], argTypes[i], argSpans[i])
		}
	}

	return req.Results, true
}

func (c *Checker) lookupInterfaceRequirement(ifaceType *Type, name string) *InterfaceRequirementInfo {
	if ifaceType == nil || ifaceType.Kind != TypeInterface {
		return nil
	}

	for i := range ifaceType.InterfaceRequirements {
		if ifaceType.InterfaceRequirements[i].Name == name {
			return &ifaceType.InterfaceRequirements[i]
		}
	}

	return nil
}

func (c *Checker) checkCallResultTypes(scope *Scope, e *ast.CallExpr) []*Type {
	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		return c.checkGenericCallResultTypes(scope, gen, nil, nil, e.Args, e.Span())
	}

	argTypes, argSpans := c.checkCallArgumentTypes(scope, e.Args)

	if results, ok := c.checkInterfaceDispatchCallResultTypes(scope, e, argTypes, argSpans); ok {
		return results
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if kind, ok := c.primitiveTaskKind(scope, id.Name.Name); ok {
			switch kind {
			case builtin.TaskLen:
				return []*Type{c.checkLenCall(e.Args, argTypes, e.Span())}

			case builtin.TaskSize:
				return []*Type{c.checkSizeCall(e.Args, argTypes, e.Span())}

			case builtin.TaskAssert:
				return []*Type{c.checkAssertCall(e.Args, argTypes, e.Span())}

			case builtin.TaskPanic:
				return []*Type{c.checkPanicCall(e.Args, argTypes, e.Span())}

			case builtin.TaskTrap:
				return []*Type{c.checkNoArgVoidPrimitive("trap", e.Args, argTypes, e.Span())}

			case builtin.TaskUnreachable:
				return []*Type{c.checkNoArgVoidPrimitive("unreachable", e.Args, argTypes, e.Span())}
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym == nil {
			c.diags.Add(id.Span(), fmt.Sprintf("undefined symbol %q", id.Name.Name))
			return []*Type{InvalidType}
		}

		switch sym.Kind {
		case SymbolTask:
			return c.checkDirectTaskCallResultTypes(sym, argTypes, argSpans, e.Span())

		case SymbolOverload:
			return c.checkOverloadCallResultTypes(sym, argTypes, argSpans, e.Span())

		default:
			c.diags.Add(id.Span(), fmt.Sprintf("cannot call non-task symbol %q", id.Name.Name))
			return []*Type{InvalidType}
		}
	}

	if selector, ok := e.Callee.(*ast.SelectorExpr); ok {
		if id, ok := selector.Left.(*ast.IdentExpr); ok {
			pkgSym := scope.Lookup(id.Name.Name)
			if pkgSym != nil && pkgSym.Kind == SymbolPackage {
				return c.checkPackageCallResultTypes(pkgSym, selector, argTypes, argSpans, e.Span())
			}
		}
	}

	calleeType := c.checkExpr(scope, e.Callee)

	if calleeType.Kind != TypeTask {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("cannot call non-task type %s", calleeType.String()))
		return []*Type{InvalidType}
	}

	c.checkTaskTypeCallArguments(calleeType, argTypes, argSpans, e.Span())
	return calleeType.Results
}

func (c *Checker) checkGenericCallExpr(scope *Scope, gen *ast.GenericExpr, argTypes []*Type, argSpans []source.Span, args []ast.Expr, span source.Span) *Type {
	results := c.checkGenericCallResultTypes(scope, gen, argTypes, argSpans, args, span)

	if len(results) == 0 {
		return VoidType
	}

	if len(results) == 1 {
		return results[0]
	}

	c.diags.Add(span, "multi-result generic task call cannot be used as a single expression; destructure it with a, b :=")
	return InvalidType
}

func (c *Checker) checkGenericCallResultTypes(scope *Scope, gen *ast.GenericExpr, argTypes []*Type, argSpans []source.Span, args []ast.Expr, span source.Span) []*Type {
	if id, ok := gen.Base.(*ast.IdentExpr); ok {
		if task, ok := builtin.LookupTask(id.Name.Name); ok && task.Generic {
			actualTypes, _ := c.checkCallArgumentTypes(scope, args)
			result := c.checkGenericIntrinsicCall(scope, gen, actualTypes, args, span)
			return []*Type{result}
		}
	}

	sym := c.taskSymbolFromGenericExprBase(scope, gen.Base)
	if sym == nil {
		return []*Type{InvalidType}
	}

	if sym.Type == nil || sym.Type.Kind != TypeTask {
		c.diags.Add(gen.Base.Span(), "generic callee has invalid task type")
		return []*Type{InvalidType}
	}

	if len(sym.Type.GenericParams) == 0 {
		c.diags.Add(gen.Span(), fmt.Sprintf("task %q is not generic", sym.Name))
		return []*Type{InvalidType}
	}

	c.checkGenericArgsAgainstParams(scope, gen.Args, sym.Type.GenericParams, gen.Span())

	taskDecl, _ := sym.Node.(*ast.TaskDecl)

	var instantiated *Type
	if taskDecl != nil {
		instantiated = c.taskTypeFromGenericCall(scope, taskDecl, gen.Args)
	} else if pkgName, ok := c.packageNameFromGenericExprBase(scope, gen.Base); ok {
		instantiated = c.taskTypeFromImportedGenericSignature(scope, pkgName, sym.Type, gen.Args, gen.Span())
	} else {
		instantiated = c.taskTypeFromGenericSignature(scope, sym.Type, gen.Args, gen.Span())
	}

	c.checkGenericTaskCallArguments(scope, instantiated, args, span)

	return instantiated.Results
}

func (c *Checker) packageNameFromGenericExprBase(scope *Scope, base ast.Expr) (string, bool) {
	selector, ok := base.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	id, ok := selector.Left.(*ast.IdentExpr)
	if !ok {
		return "", false
	}

	pkgSym := scope.Lookup(id.Name.Name)
	if pkgSym == nil || pkgSym.Kind != SymbolPackage {
		return "", false
	}

	return id.Name.Name, true
}

func (c *Checker) checkGenericTaskCallArguments(scope *Scope, taskType *Type, args []ast.Expr, span source.Span) {
	if taskType == nil || taskType.Kind != TypeTask {
		for _, arg := range args {
			c.checkExpr(scope, arg)
		}
		return
	}

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

		if len(args) < required {
			c.diags.Add(
				span,
				fmt.Sprintf("task call argument count mismatch: expected at least %d, got %d", required, len(args)),
			)
		}

		count := len(args)
		if count > fixedCount {
			count = fixedCount
		}

		for i := 0; i < count; i++ {
			if spread, ok := args[i].(*ast.SpreadExpr); ok {
				c.diags.Add(spread.Span(), "spread variadic argument cannot be used for fixed parameter")
				c.checkExpr(scope, spread.Expr)
				continue
			}

			expected := InvalidType
			if i < len(taskType.Params) {
				expected = taskType.Params[i]
			}

			c.checkExprWithExpected(scope, args[i], expected)
		}

		for i := fixedCount; i < len(args); i++ {
			if spread, ok := args[i].(*ast.SpreadExpr); ok {
				c.checkGenericVariadicSpreadArgument(scope, spread, variadicElem)
				continue
			}

			c.checkExprWithExpected(scope, args[i], variadicElem)
		}

		return
	}

	if len(args) < required || len(args) > total {
		if required == total {
			c.diags.Add(
				span,
				fmt.Sprintf("task call argument count mismatch: expected %d, got %d", total, len(args)),
			)
		} else {
			c.diags.Add(
				span,
				fmt.Sprintf("task call argument count mismatch: expected %d to %d, got %d", required, total, len(args)),
			)
		}
	}

	count := len(args)
	if total < count {
		count = total
	}

	for i := 0; i < count; i++ {
		if spread, ok := args[i].(*ast.SpreadExpr); ok {
			c.diags.Add(spread.Span(), "cannot spread variadic argument into non-variadic task")
			c.checkExpr(scope, spread.Expr)
			continue
		}

		c.checkExprWithExpected(scope, args[i], taskType.Params[i])
	}

	for i := count; i < len(args); i++ {
		c.checkExpr(scope, args[i])
	}
}

func (c *Checker) checkGenericVariadicSpreadArgument(scope *Scope, spread *ast.SpreadExpr, expectedElem *Type) {
	spreadType := c.checkExpr(scope, spread.Expr)

	switch spreadType.Kind {
	case TypeArray:
		if spreadType.Elem == nil {
			c.diags.Add(spread.Span(), "cannot spread invalid array")
			return
		}

		c.checkAssignable(expectedElem, spreadType.Elem, spread.Span())

	case TypeVariadic:
		if spreadType.Elem == nil {
			c.diags.Add(spread.Span(), "cannot spread invalid variadic value")
			return
		}

		c.checkAssignable(expectedElem, spreadType.Elem, spread.Span())

	default:
		c.diags.Add(spread.Span(), fmt.Sprintf("cannot spread %s; expected array or variadic value", spreadType.String()))
	}
}

func (c *Checker) checkCallExpr(scope *Scope, e *ast.CallExpr) *Type {
	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		return c.checkGenericCallExpr(scope, gen, nil, nil, e.Args, e.Span())
	}

	argTypes, argSpans := c.checkCallArgumentTypes(scope, e.Args)

	if result, ok := c.checkInterfaceDispatchCall(scope, e, argTypes, argSpans); ok {
		return result
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if kind, ok := c.primitiveTaskKind(scope, id.Name.Name); ok {
			switch kind {
			case builtin.TaskLen:
				return c.checkLenCall(e.Args, argTypes, e.Span())

			case builtin.TaskSize:
				return c.checkSizeCall(e.Args, argTypes, e.Span())

			case builtin.TaskAssert:
				return c.checkAssertCall(e.Args, argTypes, e.Span())

			case builtin.TaskPanic:
				return c.checkPanicCall(e.Args, argTypes, e.Span())

			case builtin.TaskTrap:
				return c.checkNoArgVoidPrimitive("trap", e.Args, argTypes, e.Span())

			case builtin.TaskUnreachable:
				return c.checkNoArgVoidPrimitive("unreachable", e.Args, argTypes, e.Span())
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym == nil {
			c.diags.Add(id.Span(), fmt.Sprintf("undefined symbol %q", id.Name.Name))
			return InvalidType
		}

		switch sym.Kind {
		case SymbolTask:
			return c.checkDirectTaskCall(sym, argTypes, argSpans, e.Span())

		case SymbolOverload:
			return c.checkOverloadCall(sym, argTypes, argSpans, e.Span())

		default:
			c.diags.Add(id.Span(), fmt.Sprintf("cannot call non-task symbol %q", id.Name.Name))
			return InvalidType
		}
	}

	if selector, ok := e.Callee.(*ast.SelectorExpr); ok {
		if id, ok := selector.Left.(*ast.IdentExpr); ok {
			pkgSym := scope.Lookup(id.Name.Name)
			if pkgSym != nil && pkgSym.Kind == SymbolPackage {
				return c.checkPackageCall(pkgSym, selector, argTypes, argSpans, e.Span())
			}
		}
	}

	calleeType := c.checkExpr(scope, e.Callee)

	if calleeType.Kind != TypeTask {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("cannot call non-task type %s", calleeType.String()))
		return InvalidType
	}

	return c.checkTaskTypeCall(calleeType, argTypes, argSpans, e.Span())
}

func (c *Checker) checkNoArgVoidPrimitive(name string, args []ast.Expr, argTypes []*Type, span source.Span) *Type {
	if len(argTypes) != 0 {
		c.diags.Add(span, fmt.Sprintf("%s expects 0 arguments, got %d", name, len(argTypes)))
	}

	return VoidType
}

func (c *Checker) checkPanicCall(args []ast.Expr, argTypes []*Type, span source.Span) *Type {
	if len(argTypes) > 1 {
		c.diags.Add(span, fmt.Sprintf("panic expects 0 or 1 argument, got %d", len(argTypes)))
		return VoidType
	}

	if len(argTypes) == 1 {
		t := argTypes[0]
		if !c.sameType(t, StringType) && !c.sameType(t, CstringType) {
			c.diags.Add(args[0].Span(), fmt.Sprintf("panic expects string or cstring, got %s", t.String()))
		}
	}

	return VoidType
}

func (c *Checker) checkPackageCall(pkgSym *Symbol, selector *ast.SelectorExpr, argTypes []*Type, argSpans []source.Span, span source.Span) *Type {
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
		return c.checkDirectTaskCall(member, argTypes, argSpans, span)

	case SymbolOverload:
		return c.checkOverloadCall(member, argTypes, argSpans, span)

	default:
		c.diags.Add(selector.Name.Span(), fmt.Sprintf("package symbol %s.%s is not callable", pkgSym.Name, selector.Name.Name))
		return InvalidType
	}
}

func (c *Checker) checkDirectTaskCall(sym *Symbol, argTypes []*Type, argSpans []source.Span, span source.Span) *Type {
	c.ensureTaskSymbolPrepared(nil, sym)

	if sym.Type == nil || sym.Type.Kind != TypeTask {
		c.diags.Add(sym.Span, fmt.Sprintf("symbol %q is not a valid task", sym.Name))
		return InvalidType
	}

	if len(sym.Type.GenericParams) > 0 {
		c.diags.Add(span, fmt.Sprintf("generic task %q requires generic arguments", sym.Name))
		return InvalidType
	}

	return c.checkTaskTypeCall(sym.Type, argTypes, argSpans, span)
}

func (c *Checker) checkTaskTypeCall(taskType *Type, argTypes []*Type, argSpans []source.Span, span source.Span) *Type {
	c.checkTaskTypeCallArguments(taskType, argTypes, argSpans, span)
	return c.resultTypeFromCall(taskType, span)
}

func (c *Checker) checkTaskTypeCallArguments(taskType *Type, argTypes []*Type, argSpans []source.Span, span source.Span) {
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
			if argTypes[i] != nil && argTypes[i].Kind == TypeVariadic {
				c.diags.Add(argSpans[i], "spread variadic argument cannot be used for fixed parameter")
				continue
			}

			c.checkAssignable(taskType.Params[i], argTypes[i], argSpans[i])
		}

		for i := fixedCount; i < len(argTypes); i++ {
			got := argTypes[i]
			if got != nil && got.Kind == TypeVariadic {
				got = got.Elem
			}

			c.checkAssignable(variadicElem, got, argSpans[i])
		}

		return
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
		if argTypes[i] != nil && argTypes[i].Kind == TypeVariadic {
			c.diags.Add(argSpans[i], "cannot spread variadic argument into non-variadic task")
			continue
		}

		c.checkAssignable(taskType.Params[i], argTypes[i], argSpans[i])
	}
}

func (c *Checker) checkGenericIntrinsicCall(scope *Scope, gen *ast.GenericExpr, argTypes []*Type, args []ast.Expr, span source.Span) *Type {
	id, ok := gen.Base.(*ast.IdentExpr)
	if !ok {
		c.diags.Add(gen.Base.Span(), "only intrinsic generic calls are supported here")
		return InvalidType
	}

	name := id.Name.Name

	if c.isShadowedPrimitive(scope, name) {
		c.diags.Add(id.Span(), fmt.Sprintf("%q is not an intrinsic generic task in this scope", name))
		return InvalidType
	}

	task, ok := builtin.LookupTask(name)
	if !ok || !task.Generic {
		c.diags.Add(id.Span(), fmt.Sprintf("unknown generic intrinsic %q", name))
		return InvalidType
	}

	if len(gen.Args) != 1 {
		c.diags.Add(gen.Span(), fmt.Sprintf("%s expects exactly 1 type argument", name))
		return InvalidType
	}

	if len(argTypes) != 1 {
		c.diags.Add(span, fmt.Sprintf("%s expects exactly 1 value argument, got %d", name, len(argTypes)))
		return InvalidType
	}

	targetType := c.typeFromGenericArg(scope, gen.Args[0])

	switch task.Kind {
	case builtin.TaskAnyAs:
		if !c.sameType(argTypes[0], AnyType) {
			c.diags.Add(args[0].Span(), fmt.Sprintf("anyAs expects any, got %s", argTypes[0].String()))
		}
		return targetType

	case builtin.TaskAnyIs:
		if !c.sameType(argTypes[0], AnyType) {
			c.diags.Add(args[0].Span(), fmt.Sprintf("anyIs expects any, got %s", argTypes[0].String()))
		}
		return BoolType

	case builtin.TaskCast:
		return targetType

	default:
		c.diags.Add(id.Span(), fmt.Sprintf("unknown generic intrinsic %q", name))
		return InvalidType
	}
}

func (c *Checker) checkSizeCall(args []ast.Expr, argTypes []*Type, span source.Span) *Type {
	if len(argTypes) != 1 {
		c.diags.Add(span, fmt.Sprintf("size expects 1 argument, got %d", len(argTypes)))
		return UintType
	}

	t := argTypes[0]
	if t == nil || t.Kind == TypeInvalid {
		return UintType
	}

	switch t.Kind {
	case TypeVoid,
		TypeNil,
		TypePackage,
		TypeTask,
		TypeEnumLiteral:
		c.diags.Add(args[0].Span(), fmt.Sprintf("size does not support %s", t.String()))
		return UintType
	}

	return UintType
}

func (c *Checker) checkLenCall(args []ast.Expr, argTypes []*Type, span source.Span) *Type {
	if len(argTypes) != 1 {
		c.diags.Add(span, fmt.Sprintf("len expects 1 argument, got %d", len(argTypes)))
		return UintType
	}

	t := argTypes[0]
	if t == nil || t.Kind == TypeInvalid {
		return UintType
	}

	switch t.Kind {
	case TypeArray, TypeVariadic:
		return UintType

	default:
		c.diags.Add(args[0].Span(), fmt.Sprintf("len does not support %s", t.String()))
		return UintType
	}
}

func (c *Checker) checkOverloadCall(sym *Symbol, argTypes []*Type, argSpans []source.Span, span source.Span) *Type {
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

	return c.checkTaskTypeCall(result.Candidate.Type, argTypes, argSpans, span)
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
		c.diags.Add(e.Name.Span(), fmt.Sprintf("string has no field %q; use size(s) or s[i]", e.Name.Name))
		return InvalidType
	}

	if leftType.Kind == TypeCstring {
		c.diags.Add(e.Name.Span(), fmt.Sprintf("cstring has no field %q", e.Name.Name))
		return InvalidType
	}

	if leftType.Kind == TypeInterface {
		c.diags.Add(e.Span(), fmt.Sprintf("interface method syntax is invalid; use %s(value, ...) instead", e.Name.Name))
		return InvalidType
	}

	if leftType.Kind == TypePointer {
		leftType = leftType.Elem
	}

	if leftType.Kind != TypeStruct && leftType.Kind != TypeTypeParam {
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
		c.diags.Add(e.Index.Span(), fmt.Sprintf("index must be integer, got %s", indexType.String()))
	}

	switch leftType.Kind {
	case TypeArray:
		return leftType.Elem

	case TypeVariadic:
		return leftType.Elem

	case TypeString:
		return CharType

	case TypeRawptr:
		return U8Type

	case TypeCstring:
		c.diags.Add(e.Left.Span(), "cstring does not support character indexing")
		return InvalidType

	default:
		if c.isByteIndexableValue(leftType) {
			return U8Type
		}

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

	if litType.Kind == TypeDistinct {
		c.diags.Add(e.Span(), fmt.Sprintf("distinct type %s cannot be constructed with a literal; use cast<%s>(value)", litType.String(), litType.String()))

		for _, field := range e.Fields {
			c.checkExpr(scope, field.Value)
		}

		for _, value := range e.Values {
			c.checkExpr(scope, value)
		}

		return InvalidType
	}

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
			if !c.isIntegerLike(lenType) {
				c.diags.Add(t.Len.Span(), fmt.Sprintf("array length must be integer, got %s", lenType.String()))
			}
		}

		return &Type{
			Kind:     TypeArray,
			Len:      lenValue,
			Inferred: t.Inferred,
			Elem:     c.typeFromAst(scope, t.Elem),
		}

	case *ast.GenericType:
		return c.typeFromGenericTypeAst(scope, t)
	}

	return InvalidType
}

func (c *Checker) typeFromGenericTypeAst(scope *Scope, t *ast.GenericType) *Type {
	baseType := c.typeFromAst(scope, t.Base)

	if baseType.Kind == TypeInvalid {
		return InvalidType
	}

	c.checkGenericArgsAgainstParams(scope, t.Args, baseType.GenericParams, t.Span())

	if len(baseType.GenericParams) == 0 {
		return baseType
	}

	switch baseType.Kind {
	case TypeStruct:
		decl, baseName := c.structDeclForGenericBase(scope, t.Base, baseType)
		return c.specializeStructType(scope, t, baseType, decl, baseName)

	case TypeInterface:
		// Runtime generic interfaces still use the base interface representation
		// for now. The important part of this pass is concrete generic structs.
		return baseType

	default:
		return baseType
	}
}

func (c *Checker) structDeclForGenericBase(scope *Scope, typ ast.Type, baseType *Type) (*ast.StructDecl, string) {
	baseName := ""
	if baseType != nil {
		baseName = baseType.Name
	}

	named, ok := typ.(*ast.NamedType)
	if !ok || len(named.Parts) == 0 {
		return nil, baseName
	}

	if len(named.Parts) == 1 {
		sym := scope.Lookup(named.Parts[0].Name)
		if sym == nil {
			return nil, baseName
		}

		decl, _ := sym.Node.(*ast.StructDecl)
		return decl, baseName
	}

	pkgIdent := named.Parts[0]
	typeIdent := named.Parts[len(named.Parts)-1]

	pkgSym := scope.Lookup(pkgIdent.Name)
	if pkgSym == nil || pkgSym.Kind != SymbolPackage || pkgSym.Package == nil {
		return nil, baseName
	}

	member := pkgSym.Package.Symbols[typeIdent.Name]
	if member == nil || member.Kind != SymbolType {
		return nil, baseName
	}

	decl, _ := member.Node.(*ast.StructDecl)

	return decl, pkgIdent.Name + "." + typeIdent.Name
}

func (c *Checker) specializeStructType(scope *Scope, gen *ast.GenericType, baseType *Type, decl *ast.StructDecl, baseName string) *Type {
	if len(gen.Args) != len(baseType.GenericParams) {
		return InvalidType
	}

	if baseName == "" {
		baseName = baseType.Name
	}

	name := c.specializedTypeName(baseName, gen.Args)

	if cached := c.specializedTypes[name]; cached != nil {
		return cached
	}

	subst := genericArgSubst(baseType.GenericParams, gen.Args)
	typeSubst := c.genericTypeSubstFromArgs(scope, baseType.GenericParams, gen.Args)

	typ := &Type{
		Kind:          TypeStruct,
		Name:          name,
		GenericParams: nil,
	}

	// Store before fields so recursive generic structs do not loop forever.
	c.specializedTypes[name] = typ

	if decl != nil {
		for _, field := range decl.Fields {
			fieldType := c.typeFromAstWithGenericArgs(scope, field.Type, subst)

			typ.Fields = append(typ.Fields, FieldInfo{
				Name:    field.Name.Name,
				Type:    fieldType,
				TypeAst: field.Type,
				Span:    field.Name.Span(),
			})
		}

		return typ
	}

	for _, field := range baseType.Fields {
		fieldType := InvalidType

		if field.TypeAst != nil {
			fieldType = c.typeFromAstWithGenericArgs(scope, field.TypeAst, subst)
		} else {
			fieldType = c.substituteGenericSignatureType(scope, field.Type, typeSubst, subst)
		}

		typ.Fields = append(typ.Fields, FieldInfo{
			Name:    field.Name,
			Type:    fieldType,
			TypeAst: field.TypeAst,
			Span:    field.Span,
		})
	}

	return typ
}

func (c *Checker) genericTypeSubstFromArgs(scope *Scope, params []ast.GenericParam, args []ast.GenericArg) map[string]*Type {
	typeSubst := map[string]*Type{}

	for i, param := range params {
		if i >= len(args) {
			break
		}

		switch param.Category {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			typeSubst[param.Name.Name] = c.typeFromGenericArg(scope, args[i])
		}
	}

	return typeSubst
}

func genericArgSubst(params []ast.GenericParam, args []ast.GenericArg) map[string]ast.GenericArg {
	subst := map[string]ast.GenericArg{}

	for i, param := range params {
		if i >= len(args) {
			break
		}

		subst[param.Name.Name] = args[i]
	}

	return subst
}

func (c *Checker) typeFromAstWithGenericArgs(scope *Scope, typ ast.Type, subst map[string]ast.GenericArg) *Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok := subst[t.Parts[0].Name]; ok {
				return c.typeFromGenericArg(scope, arg)
			}
		}

		return c.typeFromAst(scope, t)

	case *ast.PointerType:
		return &Type{
			Kind: TypePointer,
			Elem: c.typeFromAstWithGenericArgs(scope, t.Elem, subst),
		}

	case *ast.ArrayType:
		lenValue := -1

		if !t.Inferred && t.Len != nil {
			lenExpr := c.substituteGenericExpr(t.Len, subst)

			if lit, ok := lenExpr.(*ast.IntLitExpr); ok {
				parsed, err := strconv.Atoi(lit.Value)
				if err == nil {
					lenValue = parsed
				}
			}

			lenType := c.checkExpr(scope, lenExpr)
			if !c.isIntegerLike(lenType) {
				c.diags.Add(lenExpr.Span(), fmt.Sprintf("array length must be integer, got %s", lenType.String()))
			}
		}

		return &Type{
			Kind:     TypeArray,
			Len:      lenValue,
			Inferred: t.Inferred,
			Elem:     c.typeFromAstWithGenericArgs(scope, t.Elem, subst),
		}

	case *ast.GenericType:
		args := make([]ast.GenericArg, 0, len(t.Args))
		for _, arg := range t.Args {
			args = append(args, c.substituteGenericArg(arg, subst))
		}

		base := c.substituteTypeAst(t.Base, subst)

		return c.typeFromAst(scope, &ast.GenericType{
			Base: base,
			Args: args,
			Loc:  t.Loc,
		})
	}

	return c.typeFromAst(scope, typ)
}

func (c *Checker) substituteGenericArg(arg ast.GenericArg, subst map[string]ast.GenericArg) ast.GenericArg {
	switch arg.Kind {
	case ast.GenericArgType:
		return ast.GenericArg{
			Kind: ast.GenericArgType,
			Type: c.substituteTypeAst(arg.Type, subst),
			Loc:  arg.Loc,
		}

	case ast.GenericArgExpr:
		if id, ok := arg.Expr.(*ast.IdentExpr); ok {
			if replacement, exists := subst[id.Name.Name]; exists {
				return replacement
			}
		}

		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: c.substituteGenericExpr(arg.Expr, subst),
			Loc:  arg.Loc,
		}
	}

	return arg
}

func (c *Checker) substituteTypeAst(typ ast.Type, subst map[string]ast.GenericArg) ast.Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok := subst[t.Parts[0].Name]; ok {
				if argType := genericArgAsTypeAst(arg); argType != nil {
					return argType
				}
			}
		}

		return t

	case *ast.PointerType:
		return &ast.PointerType{
			Elem: c.substituteTypeAst(t.Elem, subst),
			Loc:  t.Loc,
		}

	case *ast.ArrayType:
		return &ast.ArrayType{
			Len:      c.substituteGenericExpr(t.Len, subst),
			Inferred: t.Inferred,
			Elem:     c.substituteTypeAst(t.Elem, subst),
			Loc:      t.Loc,
		}

	case *ast.GenericType:
		args := make([]ast.GenericArg, 0, len(t.Args))
		for _, arg := range t.Args {
			args = append(args, c.substituteGenericArg(arg, subst))
		}

		return &ast.GenericType{
			Base: c.substituteTypeAst(t.Base, subst),
			Args: args,
			Loc:  t.Loc,
		}
	}

	return typ
}

func genericArgAsTypeAst(arg ast.GenericArg) ast.Type {
	switch arg.Kind {
	case ast.GenericArgType:
		return arg.Type

	case ast.GenericArgExpr:
		return typeAstFromExpr(arg.Expr)
	}

	return nil
}

func typeAstFromExpr(expr ast.Expr) ast.Type {
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
					Loc:   expr.Span(),
				}

			default:
				return nil
			}
		}

	case *ast.GenericExpr:
		base := typeAstFromExpr(e.Base)
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

func (c *Checker) substituteGenericExpr(expr ast.Expr, subst map[string]ast.GenericArg) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if arg, ok := subst[e.Name.Name]; ok && arg.Kind == ast.GenericArgExpr && arg.Expr != nil {
			return arg.Expr
		}

		return e

	case *ast.SelectorExpr:
		return &ast.SelectorExpr{
			Left: c.substituteGenericExpr(e.Left, subst),
			Name: e.Name,
			Loc:  e.Loc,
		}

	case *ast.IndexExpr:
		return &ast.IndexExpr{
			Left:  c.substituteGenericExpr(e.Left, subst),
			Index: c.substituteGenericExpr(e.Index, subst),
			Loc:   e.Loc,
		}

	case *ast.SpreadExpr:
		return &ast.SpreadExpr{
			Expr: c.substituteGenericExpr(e.Expr, subst),
			Loc:  e.Loc,
		}

	case *ast.UnaryExpr:
		return &ast.UnaryExpr{
			Op:   e.Op,
			Expr: c.substituteGenericExpr(e.Expr, subst),
			Loc:  e.Loc,
		}

	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Left:  c.substituteGenericExpr(e.Left, subst),
			Op:    e.Op,
			Right: c.substituteGenericExpr(e.Right, subst),
			Loc:   e.Loc,
		}

	case *ast.CallExpr:
		args := make([]ast.Expr, 0, len(e.Args))
		for _, arg := range e.Args {
			args = append(args, c.substituteGenericExpr(arg, subst))
		}

		return &ast.CallExpr{
			Callee: c.substituteGenericExpr(e.Callee, subst),
			Args:   args,
			Loc:    e.Loc,
		}

	case *ast.GenericExpr:
		args := make([]ast.GenericArg, 0, len(e.Args))
		for _, arg := range e.Args {
			args = append(args, c.substituteGenericArg(arg, subst))
		}

		return &ast.GenericExpr{
			Base: c.substituteGenericExpr(e.Base, subst),
			Args: args,
			Loc:  e.Loc,
		}

	case *ast.ArrayLiteralExpr:
		values := make([]ast.Expr, 0, len(e.Values))
		for _, value := range e.Values {
			values = append(values, c.substituteGenericExpr(value, subst))
		}

		return &ast.ArrayLiteralExpr{
			Values: values,
			Loc:    e.Loc,
		}

	case *ast.CompoundLiteralExpr:
		fields := make([]ast.LiteralField, 0, len(e.Fields))
		for _, field := range e.Fields {
			fields = append(fields, ast.LiteralField{
				Name:  field.Name,
				Value: c.substituteGenericExpr(field.Value, subst),
			})
		}

		values := make([]ast.Expr, 0, len(e.Values))
		for _, value := range e.Values {
			values = append(values, c.substituteGenericExpr(value, subst))
		}

		return &ast.CompoundLiteralExpr{
			Type:   c.substituteTypeAst(e.Type, subst),
			Fields: fields,
			Values: values,
			Loc:    e.Loc,
		}
	}

	return expr
}

func (c *Checker) specializedTypeName(base string, args []ast.GenericArg) string {
	var parts []string

	for _, arg := range args {
		parts = append(parts, genericArgDisplay(arg))
	}

	return fmt.Sprintf("%s<%s>", base, strings.Join(parts, ", "))
}

func genericArgDisplay(arg ast.GenericArg) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return typeDisplay(arg.Type)

	case ast.GenericArgExpr:
		return exprDisplay(arg.Expr)
	}

	return "<invalid>"
}

func typeDisplay(typ ast.Type) string {
	switch t := typ.(type) {
	case *ast.NamedType:
		var parts []string
		for _, part := range t.Parts {
			parts = append(parts, part.Name)
		}

		return strings.Join(parts, ".")

	case *ast.PointerType:
		return "*" + typeDisplay(t.Elem)

	case *ast.ArrayType:
		if t.Inferred {
			return "[]" + typeDisplay(t.Elem)
		}

		return "[" + exprDisplay(t.Len) + "]" + typeDisplay(t.Elem)

	case *ast.GenericType:
		var args []string
		for _, arg := range t.Args {
			args = append(args, genericArgDisplay(arg))
		}

		return typeDisplay(t.Base) + "<" + strings.Join(args, ", ") + ">"
	}

	return "<type>"
}

func exprDisplay(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name.Name

	case *ast.SelectorExpr:
		return exprDisplay(e.Left) + "." + e.Name.Name

	case *ast.IntLitExpr:
		return e.Value

	case *ast.FloatLitExpr:
		return e.Value

	case *ast.StringLitExpr:
		return e.Value

	case *ast.CStringLitExpr:
		return e.Value

	case *ast.CharLitExpr:
		return e.Value

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		return "nil"

	case *ast.UnaryExpr:
		return e.Op.String() + exprDisplay(e.Expr)

	case *ast.BinaryExpr:
		return exprDisplay(e.Left) + " " + e.Op.String() + " " + exprDisplay(e.Right)

	case *ast.CallExpr:
		var args []string
		for _, arg := range e.Args {
			args = append(args, exprDisplay(arg))
		}

		return exprDisplay(e.Callee) + "(" + strings.Join(args, ", ") + ")"

	case *ast.GenericExpr:
		var args []string
		for _, arg := range e.Args {
			args = append(args, genericArgDisplay(arg))
		}

		return exprDisplay(e.Base) + "<" + strings.Join(args, ", ") + ">"

	case *ast.CompoundLiteralExpr:
		return typeDisplay(e.Type) + "{...}"
	}

	return "<expr>"
}

func (c *Checker) substituteGenericTypes(t *Type, subst map[string]*Type) *Type {
	if t == nil {
		return InvalidType
	}

	switch t.Kind {
	case TypeTypeParam:
		if replacement := subst[t.Name]; replacement != nil {
			return replacement
		}

		return t

	case TypePointer:
		return &Type{
			Kind: TypePointer,
			Elem: c.substituteGenericTypes(t.Elem, subst),
		}

	case TypeArray:
		return &Type{
			Kind:     TypeArray,
			Name:     t.Name,
			Elem:     c.substituteGenericTypes(t.Elem, subst),
			Len:      t.Len,
			Inferred: t.Inferred,
		}

	case TypeVariadic:
		return &Type{
			Kind: TypeVariadic,
			Name: t.Name,
			Elem: c.substituteGenericTypes(t.Elem, subst),
		}

	default:
		return t
	}
}

func (c *Checker) checkGenericArgsAgainstParams(scope *Scope, args []ast.GenericArg, params []ast.GenericParam, span source.Span) {
	if len(params) == 0 {
		if len(args) > 0 {
			c.diags.Add(span, "non-generic symbol cannot receive generic arguments")
		}
		return
	}

	if len(args) != len(params) {
		c.diags.Add(span, fmt.Sprintf("generic argument count mismatch: expected %d, got %d", len(params), len(args)))
		return
	}

	subst := genericArgSubst(params, args)

	for i := range args {
		c.checkGenericArgAgainstParam(scope, args[i], params[i], subst)
	}

	for i := range args {
		c.checkGenericArgConstraintsAgainstParam(scope, args[i], params[i], subst)
	}
}

func (c *Checker) checkGenericArgAgainstParam(scope *Scope, arg ast.GenericArg, param ast.GenericParam, subst map[string]ast.GenericArg) {
	switch param.Category {
	case ast.GenericParamType:
		argType := c.typeFromGenericArg(scope, arg)

		switch argType.Kind {
		case TypeInvalid:
			return

		case TypeEnum, TypeUnion, TypeInterface, TypeTask, TypePackage:
			c.diags.Add(arg.Span(), fmt.Sprintf("generic parameter %q expects concrete stored data type, got %s", param.Name.Name, argType.String()))

		default:
			return
		}

	case ast.GenericParamEnum:
		argType := c.typeFromGenericArg(scope, arg)
		if argType.Kind != TypeEnum && argType.Kind != TypeInvalid {
			c.diags.Add(arg.Span(), fmt.Sprintf("generic parameter %q expects enum type, got %s", param.Name.Name, argType.String()))
		}

	case ast.GenericParamUnion:
		argType := c.typeFromGenericArg(scope, arg)
		if argType.Kind != TypeUnion && argType.Kind != TypeInvalid {
			c.diags.Add(arg.Span(), fmt.Sprintf("generic parameter %q expects union type, got %s", param.Name.Name, argType.String()))
		}

	case ast.GenericParamTask:
		argType := c.taskFromGenericArg(scope, arg)
		if argType.Kind != TypeTask && argType.Kind != TypeInvalid {
			c.diags.Add(arg.Span(), fmt.Sprintf("generic parameter %q expects task, got %s", param.Name.Name, argType.String()))
			return
		}

		if argType.Kind == TypeInvalid {
			return
		}

		expected := c.taskTypeFromGenericTaskParamWithGenericArgs(scope, param, subst)
		if c.genericTaskParamHasSignatureConstraint(param) {
			c.checkGenericTaskArgumentSignature(arg.Span(), param.Name.Name, expected, argType)
		}

	case ast.GenericParamInt:
		got := c.valueFromGenericArg(scope, arg)
		c.checkAssignable(IntType, got, arg.Span())
		c.checkGenericArgIsCompileTimeValue(scope, arg, param)

	case ast.GenericParamBool:
		got := c.valueFromGenericArg(scope, arg)
		c.checkAssignable(BoolType, got, arg.Span())
		c.checkGenericArgIsCompileTimeValue(scope, arg, param)

	case ast.GenericParamString:
		got := c.valueFromGenericArg(scope, arg)
		c.checkAssignable(StringType, got, arg.Span())
		c.checkGenericArgIsCompileTimeValue(scope, arg, param)

	case ast.GenericParamValue:
		expected := InvalidType
		if param.Type != nil {
			expected = c.typeFromAstWithGenericArgs(scope, param.Type, subst)
		}

		got := c.valueFromGenericArg(scope, arg)
		c.checkAssignable(expected, got, arg.Span())
		c.checkGenericArgIsCompileTimeValue(scope, arg, param)
	}
}

func (c *Checker) checkGenericTaskArgumentSignature(span source.Span, paramName string, expected *Type, actual *Type) {
	if expected == nil || actual == nil {
		return
	}

	if expected.Kind == TypeInvalid || actual.Kind == TypeInvalid {
		return
	}

	if expected.Kind != TypeTask {
		return
	}

	if actual.Kind != TypeTask {
		c.diags.Add(span, fmt.Sprintf("generic task parameter %q expects task, got %s", paramName, actual.String()))
		return
	}

	if actual.IsVariadic || taskTypeHasVariadicParam(actual) {
		c.diags.Add(span, fmt.Sprintf("generic task parameter %q expects non-variadic task, got %s", paramName, actual.String()))
	}

	if len(actual.Params) != len(expected.Params) {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"generic task parameter %q expects task with %d parameter(s), got %d",
				paramName,
				len(expected.Params),
				len(actual.Params),
			),
		)
	} else {
		for i := range expected.Params {
			if typeIsInvalid(expected.Params[i]) || typeIsInvalid(actual.Params[i]) {
				continue
			}

			if !c.sameType(expected.Params[i], actual.Params[i]) {
				c.diags.Add(
					span,
					fmt.Sprintf(
						"generic task parameter %q parameter %d expects %s, got %s",
						paramName,
						i+1,
						expected.Params[i].String(),
						actual.Params[i].String(),
					),
				)
			}
		}
	}

	if len(actual.Results) != len(expected.Results) {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"generic task parameter %q expects task with %d result value(s), got %d",
				paramName,
				len(expected.Results),
				len(actual.Results),
			),
		)
	} else {
		for i := range expected.Results {
			if typeIsInvalid(expected.Results[i]) || typeIsInvalid(actual.Results[i]) {
				continue
			}

			if !c.sameType(expected.Results[i], actual.Results[i]) {
				c.diags.Add(
					span,
					fmt.Sprintf(
						"generic task parameter %q result %d expects %s, got %s",
						paramName,
						i+1,
						expected.Results[i].String(),
						actual.Results[i].String(),
					),
				)
			}
		}
	}
}

func taskTypeHasVariadicParam(taskType *Type) bool {
	if taskType == nil {
		return false
	}

	for _, isVariadic := range taskType.ParamIsVariadic {
		if isVariadic {
			return true
		}
	}

	return false
}

func typeIsInvalid(t *Type) bool {
	return t == nil || t.Kind == TypeInvalid
}

func (c *Checker) exprFromGenericArg(scope *Scope, arg ast.GenericArg) *Type {
	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		c.diags.Add(arg.Span(), "expected compile-time value argument")
		return InvalidType
	}

	return c.checkExpr(scope, arg.Expr)
}

func (c *Checker) genericTaskParamHasSignatureConstraint(param ast.GenericParam) bool {
	for _, constraint := range param.Constraints {
		if _, ok := constraint.(*ast.GenericTaskConstraint); ok {
			return true
		}
	}

	return false
}

func (c *Checker) taskTypeFromGenericTaskParamWithGenericArgs(scope *Scope, param ast.GenericParam, subst map[string]ast.GenericArg) *Type {
	taskType := &Type{
		Kind: TypeTask,
		Name: param.Name.Name,
	}

	for _, constraint := range param.Constraints {
		taskConstraint, ok := constraint.(*ast.GenericTaskConstraint)
		if !ok {
			continue
		}

		for _, p := range taskConstraint.Params {
			taskType.Params = append(taskType.Params, c.typeFromAstWithGenericArgs(scope, p, subst))
		}

		for _, r := range taskConstraint.Results {
			taskType.Results = append(taskType.Results, c.typeFromAstWithGenericArgs(scope, r, subst))
		}

		taskType.RequiredParams = len(taskType.Params)
		return taskType
	}

	return taskType
}

func (c *Checker) checkGenericArgConstraintsAgainstParam(scope *Scope, arg ast.GenericArg, param ast.GenericParam, subst map[string]ast.GenericArg) {
	for _, constraint := range param.Constraints {
		switch x := constraint.(type) {
		case *ast.GenericExprConstraint:
			expr := c.substituteGenericExpr(x.Expr, subst)
			typ := c.checkExpr(scope, expr)
			c.checkBoolCondition(typ, expr.Span(), "generic constraint must be bool")

			if typ == nil || typ.Kind == TypeInvalid {
				continue
			}

			value, ok := c.evalGenericConstBool(scope, expr)
			if !ok {
				c.diags.Add(expr.Span(), "generic constraint must be evaluable at compile time")
				continue
			}

			if !value {
				c.diags.Add(arg.Span(), fmt.Sprintf("generic constraint failed: %s", exprDisplay(expr)))
			}

		case *ast.GenericFieldConstraint:
			actual := c.typeFromGenericArg(scope, arg)
			if actual == nil || actual.Kind == TypeInvalid {
				continue
			}

			fieldType := c.lookupField(actual, x.Name.Name)
			if fieldType == nil {
				c.diags.Add(arg.Span(), fmt.Sprintf("generic argument %s for %q must have field %q", actual.String(), param.Name.Name, x.Name.Name))
				continue
			}

			if x.HasType && x.Type != nil {
				expected := c.typeFromAstWithGenericArgs(scope, x.Type, subst)
				if !c.sameType(fieldType, expected) {
					c.diags.Add(
						arg.Span(),
						fmt.Sprintf("generic argument %s field %q must have type %s, got %s", actual.String(), x.Name.Name, expected.String(), fieldType.String()),
					)
				}
			}

		case *ast.GenericImplConstraint:
			actual := c.typeFromGenericArg(scope, arg)
			if actual == nil || actual.Kind == TypeInvalid {
				continue
			}

			iface := c.typeFromAstWithGenericArgs(scope, x.Interface, subst)
			if iface == nil || iface.Kind == TypeInvalid {
				continue
			}

			if iface.Kind != TypeInterface && iface.Kind != TypeTypeParam {
				c.diags.Add(x.Interface.Span(), fmt.Sprintf("generic implementation constraint must name an interface, got %s", iface.String()))
				continue
			}

			if !c.typeImplementsInterface(actual, iface) {
				c.diags.Add(arg.Span(), fmt.Sprintf("generic argument %s for %q must implement %s", actual.String(), param.Name.Name, iface.String()))
			}

		case *ast.GenericEnumVariantConstraint:
			actual := c.typeFromGenericArg(scope, arg)
			if actual == nil || actual.Kind == TypeInvalid || actual.Kind == TypeTypeParam {
				continue
			}

			if actual.Kind != TypeEnum {
				continue
			}

			if !c.enumHasVariant(actual, x.Name.Name) {
				c.diags.Add(arg.Span(), fmt.Sprintf("generic enum argument %s for %q must contain variant .%s", actual.String(), param.Name.Name, x.Name.Name))
			}

		case *ast.GenericUnionMemberConstraint:
			actual := c.typeFromGenericArg(scope, arg)
			if actual == nil || actual.Kind == TypeInvalid || actual.Kind == TypeTypeParam {
				continue
			}

			if actual.Kind != TypeUnion {
				continue
			}

			member := c.typeFromAstWithGenericArgs(scope, x.Member, subst)
			if member == nil || member.Kind == TypeInvalid {
				continue
			}

			if !c.unionHasMember(actual, member) {
				c.diags.Add(arg.Span(), fmt.Sprintf("generic union argument %s for %q must contain member %s", actual.String(), param.Name.Name, member.String()))
			}

		case *ast.GenericTaskConstraint:
			// Checked in checkGenericArgAgainstParam, because task signature
			// constraints are part of category compatibility.
		}
	}
}

func (c *Checker) typeFromGenericArg(scope *Scope, arg ast.GenericArg) *Type {
	switch arg.Kind {
	case ast.GenericArgType:
		if arg.Type == nil {
			return InvalidType
		}

		return c.typeFromAst(scope, arg.Type)

	case ast.GenericArgExpr:
		switch e := arg.Expr.(type) {
		case *ast.IdentExpr:
			sym := scope.Lookup(e.Name.Name)
			if sym == nil {
				c.diags.Add(e.Span(), fmt.Sprintf("undefined generic argument %q", e.Name.Name))
				return InvalidType
			}

			if sym.Kind == SymbolType {
				return sym.Type
			}

			c.diags.Add(e.Span(), fmt.Sprintf("expected type argument, got value %q", e.Name.Name))
			return InvalidType

		case *ast.SelectorExpr:
			if id, ok := e.Left.(*ast.IdentExpr); ok {
				pkgSym := scope.Lookup(id.Name.Name)
				if pkgSym != nil && pkgSym.Kind == SymbolPackage {
					if pkgSym.Package == nil {
						c.diags.Add(id.Span(), fmt.Sprintf("package %q has no symbol table", id.Name.Name))
						return InvalidType
					}

					member := pkgSym.Package.Symbols[e.Name.Name]
					if member == nil {
						c.diags.Add(e.Name.Span(), fmt.Sprintf("package %s has no symbol %q", id.Name.Name, e.Name.Name))
						return InvalidType
					}

					if member.Kind == SymbolType {
						return member.Type
					}

					c.diags.Add(e.Name.Span(), fmt.Sprintf("package symbol %s.%s is not a type", id.Name.Name, e.Name.Name))
					return InvalidType
				}
			}

			c.diags.Add(e.Span(), "expected type argument")
			return InvalidType

		case *ast.GenericExpr:
			typ := typeAstFromExpr(e)
			if typ == nil {
				c.diags.Add(e.Span(), "expected type argument")
				return InvalidType
			}

			return c.typeFromAst(scope, typ)

		default:
			c.diags.Add(arg.Span(), "expected type argument")
			return InvalidType
		}
	}

	return InvalidType
}

func (c *Checker) taskFromGenericArg(scope *Scope, arg ast.GenericArg) *Type {
	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		c.diags.Add(arg.Span(), "expected task argument")
		return InvalidType
	}

	switch e := arg.Expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(e.Name.Name)
		if sym == nil {
			c.diags.Add(e.Span(), fmt.Sprintf("undefined task argument %q", e.Name.Name))
			return InvalidType
		}

		if sym.Kind != SymbolTask {
			c.diags.Add(e.Span(), fmt.Sprintf("expected task argument, got %q", e.Name.Name))
			return InvalidType
		}

		if sym.Type == nil {
			return InvalidType
		}

		if len(sym.Type.GenericParams) > 0 {
			c.diags.Add(e.Span(), fmt.Sprintf("generic task argument %q requires specialization", e.Name.Name))
			return InvalidType
		}

		return sym.Type

	case *ast.SelectorExpr:
		sym := c.packageTaskSymbolFromSelector(scope, e)
		if sym == nil {
			return InvalidType
		}

		if sym.Type == nil {
			return InvalidType
		}

		if len(sym.Type.GenericParams) > 0 {
			c.diags.Add(e.Span(), fmt.Sprintf("generic task argument %q requires specialization", e.Name.Name))
			return InvalidType
		}

		return sym.Type

	case *ast.GenericExpr:
		sym := c.taskSymbolFromGenericExprBase(scope, e.Base)
		if sym == nil {
			return InvalidType
		}

		if sym.Type == nil || sym.Type.Kind != TypeTask {
			c.diags.Add(e.Base.Span(), "generic task argument has invalid task type")
			return InvalidType
		}

		if len(sym.Type.GenericParams) == 0 {
			c.diags.Add(e.Span(), fmt.Sprintf("task %q is not generic", sym.Name))
			return InvalidType
		}

		c.checkGenericArgsAgainstParams(scope, e.Args, sym.Type.GenericParams, e.Span())

		taskDecl, _ := sym.Node.(*ast.TaskDecl)
		if taskDecl != nil {
			return c.taskTypeFromGenericCall(scope, taskDecl, e.Args)
		}

		if pkgName, ok := c.packageNameFromGenericExprBase(scope, e.Base); ok {
			return c.taskTypeFromImportedGenericSignature(scope, pkgName, sym.Type, e.Args, e.Span())
		}

		return c.taskTypeFromGenericSignature(scope, sym.Type, e.Args, e.Span())
	}

	c.diags.Add(arg.Span(), "expected task argument")
	return InvalidType
}

func (c *Checker) packageTaskSymbolFromSelector(scope *Scope, e *ast.SelectorExpr) *Symbol {
	id, ok := e.Left.(*ast.IdentExpr)
	if !ok {
		c.diags.Add(e.Span(), "expected task argument")
		return nil
	}

	pkgSym := scope.Lookup(id.Name.Name)
	if pkgSym == nil {
		c.diags.Add(id.Span(), fmt.Sprintf("undefined package %q", id.Name.Name))
		return nil
	}

	if pkgSym.Kind != SymbolPackage {
		c.diags.Add(id.Span(), fmt.Sprintf("%q is not a package", id.Name.Name))
		return nil
	}

	if pkgSym.Package == nil {
		c.diags.Add(id.Span(), fmt.Sprintf("package %q has no symbol table", id.Name.Name))
		return nil
	}

	member := pkgSym.Package.Symbols[e.Name.Name]
	if member == nil {
		c.diags.Add(e.Name.Span(), fmt.Sprintf("package %s has no symbol %q", id.Name.Name, e.Name.Name))
		return nil
	}

	if member.Kind != SymbolTask {
		c.diags.Add(e.Name.Span(), fmt.Sprintf("package symbol %s.%s is not a task", id.Name.Name, e.Name.Name))
		return nil
	}

	return member
}

func (c *Checker) taskSymbolFromGenericExprBase(scope *Scope, base ast.Expr) *Symbol {
	switch b := base.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(b.Name.Name)
		if sym == nil {
			c.diags.Add(b.Span(), fmt.Sprintf("undefined task argument %q", b.Name.Name))
			return nil
		}

		if sym.Kind != SymbolTask {
			c.diags.Add(b.Span(), fmt.Sprintf("expected task argument, got %q", b.Name.Name))
			return nil
		}

		return sym

	case *ast.SelectorExpr:
		return c.packageTaskSymbolFromSelector(scope, b)

	default:
		c.diags.Add(base.Span(), "expected generic task name")
		return nil
	}
}

func (c *Checker) checkGenericArgIsCompileTimeValue(scope *Scope, arg ast.GenericArg, param ast.GenericParam) {
	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		return
	}

	if _, ok := c.evalGenericConstExpr(scope, arg.Expr); ok {
		return
	}

	if !c.isCompileTimeGenericExpr(scope, arg.Expr) {
		c.diags.Add(arg.Span(), fmt.Sprintf("generic parameter %q requires a compile-time value argument", param.Name.Name))
	}
}

func (c *Checker) isCompileTimeGenericExpr(scope *Scope, expr ast.Expr) bool {
	if expr == nil {
		return false
	}

	switch e := expr.(type) {
	case *ast.IntLitExpr,
		*ast.FloatLitExpr,
		*ast.StringLitExpr,
		*ast.CStringLitExpr,
		*ast.CharLitExpr,
		*ast.BoolLitExpr,
		*ast.NilLitExpr,
		*ast.DotIdentExpr:
		return true

	case *ast.IdentExpr:
		sym := scope.Lookup(e.Name.Name)
		return sym != nil && sym.Kind == SymbolConst

	case *ast.SelectorExpr:
		if id, ok := e.Left.(*ast.IdentExpr); ok {
			pkgSym := scope.Lookup(id.Name.Name)
			if pkgSym != nil && pkgSym.Kind == SymbolPackage && pkgSym.Package != nil {
				member := pkgSym.Package.Symbols[e.Name.Name]
				return member != nil && member.Kind == SymbolConst
			}
		}

		return false

	case *ast.UnaryExpr:
		return c.isCompileTimeGenericExpr(scope, e.Expr)

	case *ast.BinaryExpr:
		return c.isCompileTimeGenericExpr(scope, e.Left) &&
			c.isCompileTimeGenericExpr(scope, e.Right)

	case *ast.CallExpr:
		gen, ok := e.Callee.(*ast.GenericExpr)
		if !ok {
			return false
		}

		id, ok := gen.Base.(*ast.IdentExpr)
		if !ok || id.Name.Name != "cast" {
			return false
		}

		return len(e.Args) == 1 && c.isCompileTimeGenericExpr(scope, e.Args[0])

	case *ast.ArrayLiteralExpr:
		for _, value := range e.Values {
			if !c.isCompileTimeGenericExpr(scope, value) {
				return false
			}
		}

		return true

	case *ast.CompoundLiteralExpr:
		for _, field := range e.Fields {
			if !c.isCompileTimeGenericExpr(scope, field.Value) {
				return false
			}
		}

		for _, value := range e.Values {
			if !c.isCompileTimeGenericExpr(scope, value) {
				return false
			}
		}

		return true
	}

	return false
}

type genericConstKind int

const (
	genericConstInvalid genericConstKind = iota
	genericConstBool
	genericConstInt
	genericConstString
)

type genericConstValue struct {
	Kind        genericConstKind
	BoolValue   bool
	IntValue    int64
	StringValue string
}

func (c *Checker) evalGenericConstBool(scope *Scope, expr ast.Expr) (bool, bool) {
	value, ok := c.evalGenericConstExpr(scope, expr)
	if !ok || value.Kind != genericConstBool {
		return false, false
	}

	return value.BoolValue, true
}

func (c *Checker) evalGenericConstExpr(scope *Scope, expr ast.Expr) (genericConstValue, bool) {
	return c.evalGenericConstExprWithEnv(scope, expr, nil)
}

func (c *Checker) evalGenericConstExprWithEnv(scope *Scope, expr ast.Expr, env map[string]genericConstValue) (genericConstValue, bool) {
	if expr == nil {
		return genericConstValue{}, false
	}

	switch e := expr.(type) {
	case *ast.BoolLitExpr:
		return genericConstValue{Kind: genericConstBool, BoolValue: e.Value}, true

	case *ast.IntLitExpr:
		value, err := strconv.ParseInt(e.Value, 10, 64)
		if err != nil {
			return genericConstValue{}, false
		}

		return genericConstValue{Kind: genericConstInt, IntValue: value}, true

	case *ast.StringLitExpr:
		value, err := strconv.Unquote(e.Value)
		if err != nil {
			return genericConstValue{}, false
		}

		return genericConstValue{Kind: genericConstString, StringValue: value}, true

	case *ast.IdentExpr:
		if env != nil {
			if value, ok := env[e.Name.Name]; ok {
				return value, true
			}
		}

		sym := scope.Lookup(e.Name.Name)
		if sym == nil || sym.Kind != SymbolConst {
			return genericConstValue{}, false
		}

		decl, ok := sym.Node.(*ast.ConstDecl)
		if !ok || decl.Value == nil {
			return genericConstValue{}, false
		}

		return c.evalGenericConstExprWithEnv(scope, decl.Value, env)

	case *ast.SelectorExpr:
		return c.evalGenericConstSelectorWithEnv(scope, e.Left, e.Name, env)

	case *ast.UnaryExpr:
		value, ok := c.evalGenericConstExprWithEnv(scope, e.Expr, env)
		if !ok {
			return genericConstValue{}, false
		}

		switch e.Op {
		case token.Bang:
			if value.Kind != genericConstBool {
				return genericConstValue{}, false
			}

			return genericConstValue{Kind: genericConstBool, BoolValue: !value.BoolValue}, true

		case token.Minus:
			if value.Kind != genericConstInt {
				return genericConstValue{}, false
			}

			return genericConstValue{Kind: genericConstInt, IntValue: -value.IntValue}, true
		}

		return genericConstValue{}, false

	case *ast.BinaryExpr:
		left, ok := c.evalGenericConstExprWithEnv(scope, e.Left, env)
		if !ok {
			return genericConstValue{}, false
		}

		right, ok := c.evalGenericConstExprWithEnv(scope, e.Right, env)
		if !ok {
			return genericConstValue{}, false
		}

		return evalGenericConstBinary(e.Op, left, right)

	case *ast.CallExpr:
		return c.evalGenericConstCall(scope, e, env)
	}

	return genericConstValue{}, false
}

func (c *Checker) evalGenericConstSelector(scope *Scope, receiver ast.Expr, field ast.Ident) (genericConstValue, bool) {
	return c.evalGenericConstSelectorWithEnv(scope, receiver, field, nil)
}

func (c *Checker) evalGenericConstSelectorWithEnv(scope *Scope, receiver ast.Expr, field ast.Ident, env map[string]genericConstValue) (genericConstValue, bool) {
	if receiver == nil {
		return genericConstValue{}, false
	}

	switch r := receiver.(type) {
	case *ast.CompoundLiteralExpr:
		for _, literalField := range r.Fields {
			if literalField.Name.Name == field.Name {
				return c.evalGenericConstExprWithEnv(scope, literalField.Value, env)
			}
		}

		litType := c.typeFromAst(scope, r.Type)
		if litType != nil && litType.Kind == TypeStruct {
			for i, structField := range litType.Fields {
				if structField.Name == field.Name && i < len(r.Values) {
					return c.evalGenericConstExprWithEnv(scope, r.Values[i], env)
				}
			}
		}

		return genericConstValue{}, false

	case *ast.IdentExpr:
		if env != nil {
			if _, ok := env[r.Name.Name]; ok {
				return genericConstValue{}, false
			}
		}

		sym := scope.Lookup(r.Name.Name)
		if sym == nil {
			return genericConstValue{}, false
		}

		if sym.Kind == SymbolConst {
			decl, ok := sym.Node.(*ast.ConstDecl)
			if !ok || decl.Value == nil {
				return genericConstValue{}, false
			}

			return c.evalGenericConstSelectorWithEnv(scope, decl.Value, field, env)
		}

		if sym.Kind == SymbolPackage && sym.Package != nil {
			member := sym.Package.Symbols[field.Name]
			if member == nil || member.Kind != SymbolConst {
				return genericConstValue{}, false
			}

			decl, ok := member.Node.(*ast.ConstDecl)
			if !ok || decl.Value == nil {
				return genericConstValue{}, false
			}

			return c.evalGenericConstExprWithEnv(scope, decl.Value, env)
		}

		return genericConstValue{}, false

	case *ast.SelectorExpr:
		value, ok := c.evalGenericConstSelectorWithEnv(scope, r.Left, r.Name, env)
		if !ok {
			return genericConstValue{}, false
		}

		_ = value
		return genericConstValue{}, false
	}

	return genericConstValue{}, false
}

func (c *Checker) evalGenericConstCall(scope *Scope, e *ast.CallExpr, env map[string]genericConstValue) (genericConstValue, bool) {
	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		if id, ok := gen.Base.(*ast.IdentExpr); ok && id.Name.Name == "cast" && len(e.Args) == 1 {
			return c.evalGenericConstExprWithEnv(scope, e.Args[0], env)
		}

		return genericConstValue{}, false
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok && id.Name.Name == "size" && len(e.Args) == 1 {
		value, ok := c.evalGenericConstExprWithEnv(scope, e.Args[0], env)
		if !ok || value.Kind != genericConstString {
			return genericConstValue{}, false
		}

		return genericConstValue{Kind: genericConstInt, IntValue: int64(len(value.StringValue))}, true
	}

	sym := c.taskSymbolFromConstCallCallee(scope, e.Callee)
	if sym == nil {
		return genericConstValue{}, false
	}

	c.ensureTaskSymbolPrepared(scope, sym)

	if sym.Type == nil || sym.Type.Kind != TypeTask {
		return genericConstValue{}, false
	}

	if !sym.Type.IsPure && !sym.Type.IsTrustedPure {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("generic constraint call %q must be pure", sym.Name))
		return genericConstValue{}, false
	}

	if len(sym.Type.Results) != 1 {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("generic constraint call %q must return exactly 1 value", sym.Name))
		return genericConstValue{}, false
	}

	var argValues []genericConstValue
	for _, arg := range e.Args {
		value, ok := c.evalGenericConstExprWithEnv(scope, arg, env)
		if !ok {
			return genericConstValue{}, false
		}

		argValues = append(argValues, value)
	}

	taskDecl, ok := sym.Node.(*ast.TaskDecl)
	if !ok || taskDecl.Body == nil {
		c.diags.Add(e.Callee.Span(), fmt.Sprintf("generic constraint call %q cannot be evaluated because its body is unavailable", sym.Name))
		return genericConstValue{}, false
	}

	return c.evalPureTaskConstBody(scope, taskDecl, argValues)
}

func (c *Checker) taskSymbolFromConstCallCallee(scope *Scope, callee ast.Expr) *Symbol {
	switch x := callee.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(x.Name.Name)
		if sym == nil || sym.Kind != SymbolTask {
			return nil
		}

		return sym

	case *ast.SelectorExpr:
		id, ok := x.Left.(*ast.IdentExpr)
		if !ok {
			return nil
		}

		pkgSym := scope.Lookup(id.Name.Name)
		if pkgSym == nil || pkgSym.Kind != SymbolPackage || pkgSym.Package == nil {
			return nil
		}

		member := pkgSym.Package.Symbols[x.Name.Name]
		if member == nil || member.Kind != SymbolTask {
			return nil
		}

		return member
	}

	return nil
}

func (c *Checker) evalPureTaskConstBody(scope *Scope, d *ast.TaskDecl, args []genericConstValue) (genericConstValue, bool) {
	if d == nil || d.Body == nil {
		return genericConstValue{}, false
	}

	if len(args) != len(d.Params) {
		return genericConstValue{}, false
	}

	env := map[string]genericConstValue{}

	for i, param := range d.Params {
		env[param.Name.Name] = args[i]
	}

	for _, stmt := range d.Body.Stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if len(s.Values) != 1 {
				return genericConstValue{}, false
			}

			return c.evalGenericConstExprWithEnv(scope, s.Values[0], env)

		case *ast.DeclStmt:
			constDecl, ok := s.Decl.(*ast.ConstDecl)
			if !ok {
				return genericConstValue{}, false
			}

			value, ok := c.evalGenericConstExprWithEnv(scope, constDecl.Value, env)
			if !ok {
				return genericConstValue{}, false
			}

			env[constDecl.Name.Name] = value

		default:
			return genericConstValue{}, false
		}
	}

	return genericConstValue{}, false
}

func evalGenericConstBinary(op token.Kind, left genericConstValue, right genericConstValue) (genericConstValue, bool) {
	switch op {
	case token.AndAnd:
		if left.Kind == genericConstBool && right.Kind == genericConstBool {
			return genericConstValue{Kind: genericConstBool, BoolValue: left.BoolValue && right.BoolValue}, true
		}

	case token.OrOr:
		if left.Kind == genericConstBool && right.Kind == genericConstBool {
			return genericConstValue{Kind: genericConstBool, BoolValue: left.BoolValue || right.BoolValue}, true
		}

	case token.EqEq, token.NotEq:
		value, ok := genericConstEqual(left, right)
		if !ok {
			return genericConstValue{}, false
		}

		if op == token.NotEq {
			value = !value
		}

		return genericConstValue{Kind: genericConstBool, BoolValue: value}, true

	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		if left.Kind == genericConstInt && right.Kind == genericConstInt {
			switch op {
			case token.Lt:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.IntValue < right.IntValue}, true
			case token.LtEq:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.IntValue <= right.IntValue}, true
			case token.Gt:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.IntValue > right.IntValue}, true
			case token.GtEq:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.IntValue >= right.IntValue}, true
			}
		}

		if left.Kind == genericConstString && right.Kind == genericConstString {
			switch op {
			case token.Lt:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.StringValue < right.StringValue}, true
			case token.LtEq:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.StringValue <= right.StringValue}, true
			case token.Gt:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.StringValue > right.StringValue}, true
			case token.GtEq:
				return genericConstValue{Kind: genericConstBool, BoolValue: left.StringValue >= right.StringValue}, true
			}
		}

	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent:
		if left.Kind != genericConstInt || right.Kind != genericConstInt {
			return genericConstValue{}, false
		}

		switch op {
		case token.Plus:
			return genericConstValue{Kind: genericConstInt, IntValue: left.IntValue + right.IntValue}, true
		case token.Minus:
			return genericConstValue{Kind: genericConstInt, IntValue: left.IntValue - right.IntValue}, true
		case token.Star:
			return genericConstValue{Kind: genericConstInt, IntValue: left.IntValue * right.IntValue}, true
		case token.Slash:
			if right.IntValue == 0 {
				return genericConstValue{}, false
			}

			return genericConstValue{Kind: genericConstInt, IntValue: left.IntValue / right.IntValue}, true
		case token.Percent:
			if right.IntValue == 0 {
				return genericConstValue{}, false
			}

			return genericConstValue{Kind: genericConstInt, IntValue: left.IntValue % right.IntValue}, true
		}
	}

	return genericConstValue{}, false
}

func genericConstEqual(left genericConstValue, right genericConstValue) (bool, bool) {
	if left.Kind != right.Kind {
		return false, false
	}

	switch left.Kind {
	case genericConstBool:
		return left.BoolValue == right.BoolValue, true

	case genericConstInt:
		return left.IntValue == right.IntValue, true

	case genericConstString:
		return left.StringValue == right.StringValue, true
	}

	return false, false
}

func (c *Checker) valueFromGenericArg(scope *Scope, arg ast.GenericArg) *Type {
	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		c.diags.Add(arg.Span(), "expected compile-time value argument")
		return InvalidType
	}

	return c.checkExpr(scope, arg.Expr)
}

func (c *Checker) checkMultiVarDeclStmt(scope *Scope, s *ast.MultiVarDeclStmt) {
	call, ok := s.Value.(*ast.CallExpr)
	if !ok {
		c.diags.Add(s.Value.Span(), "multi-value declaration requires a task call")
		c.checkExpr(scope, s.Value)
		return
	}

	resultTypes := c.checkCallResultTypes(scope, call)

	if len(resultTypes) != len(s.Names) {
		c.diags.Add(
			s.Span(),
			fmt.Sprintf("multi-value declaration mismatch: expected %d name(s), got %d result value(s)", len(s.Names), len(resultTypes)),
		)
	}

	count := len(s.Names)
	if len(resultTypes) < count {
		count = len(resultTypes)
	}

	for i := 0; i < count; i++ {
		name := s.Names[i]
		if name.Name == "_" {
			continue
		}

		varType := c.defaultType(resultTypes[i])
		if varType.Kind == TypeEnumLiteral {
			c.diags.Add(name.Span(), fmt.Sprintf("enum literal .%s needs explicit type", varType.Name))
			varType = InvalidType
		}

		if varType.Kind == TypeNil {
			c.diags.Add(name.Span(), "nil needs explicit type")
			varType = InvalidType
		}

		scope.Declare(&Symbol{
			Name: name.Name,
			Kind: SymbolVar,
			Type: varType,
			Span: name.Span(),
			Node: s,
		})
	}
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

	if dst.Kind == TypeInterface {
		if src.Kind == TypeNil {
			return
		}

		if src.Kind == TypePointer && c.typeImplementsInterface(src.Elem, dst) {
			return
		}

		c.diags.Add(span, fmt.Sprintf("cannot assign %s to interface %s", src.String(), dst.String()))
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
		if dst.Kind == TypePointer || dst.Kind == TypeRawptr || dst.Kind == TypeInterface {
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

	if dst.Kind == TypeDistinct {
		if src.Kind == TypeUntypedInt || src.Kind == TypeUntypedFloat {
			return c.assignable(dst.Underlying, src)
		}

		return false
	}

	if src.Kind == TypeDistinct {
		return false
	}

	if dst.Kind == TypeEnum && src.Kind == TypeEnumLiteral {
		return c.enumHasVariant(dst, src.Name)
	}

	if dst.Kind == TypeInterface {
		return src.Kind == TypeNil ||
			(src.Kind == TypePointer && c.typeImplementsInterface(src.Elem, dst))
	}

	if dst.Kind == TypeUnion {
		return src.Kind == TypeNil || c.unionHasMember(dst, src)
	}

	if src.Kind == TypeNil {
		return dst.Kind == TypePointer || dst.Kind == TypeRawptr || dst.Kind == TypeUnion || dst.Kind == TypeInterface
	}

	if src.Kind == TypeUntypedInt {
		return c.isIntegerLike(dst) || dst.Kind == TypeF32 || dst.Kind == TypeF64
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

	case TypeStruct,
		TypeDistinct,
		TypeEnum,
		TypeUnion,
		TypeInterface,
		TypeTypeParam,
		TypeValueParam:
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

func (c *Checker) isSignedInteger(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeInt, TypeI8, TypeI16, TypeI32, TypeI64:
		return true
	default:
		return false
	}
}

func (c *Checker) isUnsignedInteger(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeUint, TypeU8, TypeU16, TypeU32, TypeU64:
		return true
	default:
		return false
	}
}

func (c *Checker) isIntegerLike(t *Type) bool {
	if t == nil {
		return false
	}

	if t.Kind == TypeUntypedInt {
		return true
	}

	if t.Kind == TypeChar {
		return true
	}

	return c.isSignedInteger(t) || c.isUnsignedInteger(t)
}

func (c *Checker) isFloat(t *Type) bool {
	if t == nil {
		return false
	}

	return t.Kind == TypeF32 || t.Kind == TypeF64 || t.Kind == TypeUntypedFloat
}

func (c *Checker) isNumeric(t *Type) bool {
	return c.isIntegerLike(t) || c.isFloat(t)
}

func (c *Checker) isIndexType(t *Type) bool {
	return c.isIntegerLike(t)
}

func (c *Checker) isByteIndexableValue(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeInvalid,
		TypeVoid,
		TypeNil,
		TypePackage,
		TypeTask,
		TypeEnumLiteral,
		TypeArray,
		TypeVariadic,
		TypeString,
		TypeCstring:
		return false

	default:
		return true
	}
}

func (c *Checker) isAddressableExpr(scope *Scope, expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(e.Name.Name)
		return sym != nil && sym.Kind == SymbolVar

	case *ast.SelectorExpr:
		return c.isAddressableExpr(scope, e.Left)

	case *ast.IndexExpr:
		leftType := c.checkExpr(scope, e.Left)
		return leftType.Kind == TypeArray ||
			leftType.Kind == TypeVariadic ||
			leftType.Kind == TypeRawptr ||
			c.isAddressableExpr(scope, e.Left)

	case *ast.UnaryExpr:
		return e.Op == token.Star
	}

	return false
}

func (c *Checker) isValidDistinctUnderlying(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeBool,
		TypeInt,
		TypeUint,
		TypeI8,
		TypeI16,
		TypeI32,
		TypeI64,
		TypeU8,
		TypeU16,
		TypeU32,
		TypeU64,
		TypeF32,
		TypeF64,
		TypeChar,
		TypeString,
		TypeCstring,
		TypeRawptr:
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

	// Do not silently choose a concrete result for mixed integer arithmetic.
	// Users should cast explicitly.
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

func (c *Checker) distinctComparable(a *Type, b *Type) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind != TypeDistinct || b.Kind != TypeDistinct {
		return false
	}

	if !c.sameType(a, b) {
		return false
	}

	return c.isNumeric(a.Underlying) || c.isIntegerLike(a.Underlying)
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

func (c *Checker) checkAssertCall(args []ast.Expr, argTypes []*Type, span source.Span) *Type {
	if len(argTypes) != 1 {
		c.diags.Add(span, fmt.Sprintf("assert expects 1 argument, got %d", len(argTypes)))
		return VoidType
	}

	t := argTypes[0]
	if t == nil || t.Kind == TypeInvalid {
		return VoidType
	}

	if !c.sameType(t, BoolType) {
		c.diags.Add(args[0].Span(), fmt.Sprintf("assert expects bool, got %s", t.String()))
	}

	return VoidType
}

func (c *Checker) checkDirectTaskCallResultTypes(sym *Symbol, argTypes []*Type, argSpans []source.Span, span source.Span) []*Type {
	c.ensureTaskSymbolPrepared(nil, sym)

	if sym.Type == nil || sym.Type.Kind != TypeTask {
		c.diags.Add(sym.Span, fmt.Sprintf("symbol %q is not a valid task", sym.Name))
		return []*Type{InvalidType}
	}

	if len(sym.Type.GenericParams) > 0 {
		c.diags.Add(span, fmt.Sprintf("generic task %q requires generic arguments", sym.Name))
		return []*Type{InvalidType}
	}

	c.checkTaskTypeCallArguments(sym.Type, argTypes, argSpans, span)
	return sym.Type.Results
}

func (c *Checker) checkPackageCallResultTypes(pkgSym *Symbol, selector *ast.SelectorExpr, argTypes []*Type, argSpans []source.Span, span source.Span) []*Type {
	if pkgSym.Package == nil {
		c.diags.Add(selector.Left.Span(), fmt.Sprintf("package %q has no symbol table", pkgSym.Name))
		return []*Type{InvalidType}
	}

	member := pkgSym.Package.Symbols[selector.Name.Name]
	if member == nil {
		c.diags.Add(selector.Name.Span(), fmt.Sprintf("package %s has no symbol %q", pkgSym.Name, selector.Name.Name))
		return []*Type{InvalidType}
	}

	switch member.Kind {
	case SymbolTask:
		return c.checkDirectTaskCallResultTypes(member, argTypes, argSpans, span)

	case SymbolOverload:
		return c.checkOverloadCallResultTypes(member, argTypes, argSpans, span)

	default:
		c.diags.Add(selector.Name.Span(), fmt.Sprintf("package symbol %s.%s is not callable", pkgSym.Name, selector.Name.Name))
		return []*Type{InvalidType}
	}
}

func (c *Checker) checkOverloadCallResultTypes(sym *Symbol, argTypes []*Type, argSpans []source.Span, span source.Span) []*Type {
	info := sym.Overload
	if info == nil {
		c.diags.Add(sym.Span, fmt.Sprintf("symbol %q is not a valid overload", sym.Name))
		return []*Type{InvalidType}
	}

	result := c.resolveOverload(info, argTypes)

	if !result.Matched {
		c.diags.Add(
			span,
			fmt.Sprintf("no overload of %q matches argument types (%s)", info.Name, c.formatTypes(argTypes)),
		)
		return []*Type{InvalidType}
	}

	if result.Ambiguous {
		c.diags.Add(
			span,
			fmt.Sprintf("ambiguous overload call %q with argument types (%s)", info.Name, c.formatTypes(argTypes)),
		)
		return []*Type{InvalidType}
	}

	c.checkTaskTypeCallArguments(result.Candidate.Type, argTypes, argSpans, span)
	return result.Candidate.Type.Results
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
	case TypeBool,
		TypeInt, TypeUint,
		TypeI8, TypeI16, TypeI32, TypeI64,
		TypeU8, TypeU16, TypeU32, TypeU64,
		TypeChar,
		TypeF32, TypeF64,
		TypeString, TypeCstring:
		return true

	default:
		return false
	}
}

func (c *Checker) isShadowedPrimitive(scope *Scope, name string) bool {
	sym := scope.Lookup(name)
	return sym != nil && !sym.Builtin
}

func (c *Checker) primitiveTaskKind(scope *Scope, name string) (builtin.TaskKind, bool) {
	if c.isShadowedPrimitive(scope, name) {
		return builtin.TaskInvalid, false
	}

	task, ok := builtin.LookupTask(name)
	if !ok {
		return builtin.TaskInvalid, false
	}

	return task.Kind, true
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
			info.Symbols[symbolName] = exportSymbolSignatureOnly(sym)
		}
	}

	return info
}

func exportSymbolSignatureOnly(sym *Symbol) *Symbol {
	if sym == nil {
		return nil
	}

	out := *sym

	// Imported tasks and types must be type-checkable from exported typed
	// signatures, not from source bodies/declarations. Generic struct fields
	// keep their field TypeAst through FieldInfo.
	if out.Kind == SymbolTask || out.Kind == SymbolType {
		out.Node = nil
	}

	if out.Type != nil {
		out.Type = cloneExportType(out.Type, map[*Type]*Type{})
	}

	if out.Overload != nil {
		overload := *out.Overload
		overload.Candidates = nil

		for _, candidate := range out.Overload.Candidates {
			overload.Candidates = append(overload.Candidates, exportSymbolSignatureOnly(candidate))
		}

		out.Overload = &overload
	}

	return &out
}

func cloneExportType(t *Type, seen map[*Type]*Type) *Type {
	if t == nil {
		return nil
	}

	if cached := seen[t]; cached != nil {
		return cached
	}

	out := *t
	seen[t] = &out

	out.Elem = cloneExportType(t.Elem, seen)
	out.Underlying = cloneExportType(t.Underlying, seen)

	out.Fields = nil
	for _, field := range t.Fields {
		out.Fields = append(out.Fields, FieldInfo{
			Name:    field.Name,
			Type:    cloneExportType(field.Type, seen),
			TypeAst: field.TypeAst,
			Span:    field.Span,
		})
	}

	out.Members = nil
	for _, member := range t.Members {
		out.Members = append(out.Members, cloneExportType(member, seen))
	}

	out.InterfaceRequirements = nil
	for _, req := range t.InterfaceRequirements {
		cloned := InterfaceRequirementInfo{
			Name: req.Name,
			Span: req.Span,
		}

		for _, param := range req.Params {
			cloned.Params = append(cloned.Params, cloneExportType(param, seen))
		}

		for _, result := range req.Results {
			cloned.Results = append(cloned.Results, cloneExportType(result, seen))
		}

		out.InterfaceRequirements = append(out.InterfaceRequirements, cloned)
	}

	out.Implements = nil
	for _, iface := range t.Implements {
		out.Implements = append(out.Implements, cloneExportType(iface, seen))
	}

	out.Params = nil
	for _, param := range t.Params {
		out.Params = append(out.Params, cloneExportType(param, seen))
	}

	out.Results = nil
	for _, result := range t.Results {
		out.Results = append(out.Results, cloneExportType(result, seen))
	}

	out.ParamDefaults = append([]ast.Expr(nil), t.ParamDefaults...)
	out.ParamHasDefault = append([]bool(nil), t.ParamHasDefault...)
	out.ParamIsVariadic = append([]bool(nil), t.ParamIsVariadic...)
	out.GenericParams = append([]ast.GenericParam(nil), t.GenericParams...)

	return &out
}

func DebugSummary(scope *Scope) string {
	count := 0

	for _, sym := range scope.Symbols {
		if builtin.IsType(sym.Name) {
			continue
		}

		count++
	}

	return fmt.Sprintf("checked_symbols=%d", count)
}
