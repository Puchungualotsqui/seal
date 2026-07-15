package checker

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

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
	TypeInlineArray

	TypeUntypedInt
	TypeUntypedFloat

	TypePointer
	TypeVariadic
	TypeStruct
	TypeDistinct
	TypeEnum
	TypeUnion
	TypeInterface
	TypeInterfaceSelf
	TypeTask
	TypePackage
	TypeTypeParam
	TypeValueParam
)

type GenericArgumentInfo struct {
	Category ast.GenericParamCategory

	// Type is:
	//
	//   - the concrete type for type/enum/union arguments
	//   - the task type for task arguments
	//   - the value type for comptime value arguments
	Type *Type

	// Expr is preserved for comptime value arguments.
	Expr ast.Expr

	// Key is a stable display key used for identity and matching.
	Key string
}

type InterfaceRequirementInfo struct {
	Name string

	Params  []*Type
	Results []*Type

	ParamIsVariadic []bool

	IsPure        bool
	IsTrustedPure bool

	Span source.Span
}

type ImplEntryInfo struct {
	Name string

	TaskSymbol *Symbol
	Alias      ast.Expr

	Span source.Span
}

type UsingProjectionStep struct {
	Name string

	// Container is the struct reached before selecting this field.
	Container *Type

	// FieldType is the declared field type.
	FieldType *Type

	// ThroughPointer means the container must be dereferenced before selecting
	// the field.
	ThroughPointer bool

	Span source.Span
}

type ImplInfo struct {
	PackageName string

	Decl *ast.ImplDecl

	GenericParams []ast.GenericParam

	// These may contain TypeTypeParam placeholders for generic impls.
	Interface *Type
	Target    *Type

	Entries map[string]*ImplEntryInfo

	UsingPath  []string
	UsingSteps []UsingProjectionStep

	Scope *Scope
	Span  source.Span

	DelegatesTo *ImplInfo

	Checked  bool
	Checking bool
	Usable   bool
}

type ImplMatch struct {
	Types  map[string]*Type
	Values map[string]GenericArgumentInfo
}

type ResolvedImpl struct {
	Info *ImplInfo

	Interface *Type
	Target    *Type

	Match ImplMatch

	Delegated *ResolvedImpl
}

type ImplResolutionKind int

const (
	ImplResolutionNotFound ImplResolutionKind = iota
	ImplResolutionFound
	ImplResolutionAmbiguous
)

type ImplResolution struct {
	Kind ImplResolutionKind

	Resolved *ResolvedImpl
	Matches  []*ResolvedImpl
}

func (r ImplResolution) Found() bool {
	return r.Kind == ImplResolutionFound &&
		r.Resolved != nil
}

func (r ImplResolution) Ambiguous() bool {
	return r.Kind == ImplResolutionAmbiguous
}

type Type struct {
	Kind TypeKind
	Name string

	Elem *Type

	InlineLengthExpr     ast.Expr
	InlineLengthKey      string
	InlineLength         int64
	InlineLengthKnown    bool
	InlineLengthSymbolic bool

	Fields   []FieldInfo
	Variants []EnumVariantInfo
	Members  []*Type

	InterfaceRequirements []InterfaceRequirementInfo

	// Keep this only for generic type-parameter constraints. Concrete impl
	// lookup must use Checker.impls.
	Implements []*Type

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

	GenericParams []ast.GenericParam

	// Populated on concrete generic specializations such as Box<int> and
	// Reader<int>.
	GenericBaseName  string
	GenericArguments []GenericArgumentInfo

	IsDynInterface bool

	// IntConstant is non-nil only for compile-time integer expressions.
	//
	// It is primarily carried by TypeUntypedInt values, but may also be
	// preserved temporarily while checking explicitly typed constant
	// expressions.
	//
	// A separate *big.Int must be allocated whenever the value is changed.
	IntConstant *big.Int
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
		if t.Elem == nil {
			return "*<invalid>"
		}

		return "*" + t.Elem.String()

	case TypeInlineArray:
		elem := "<invalid>"
		if t.Elem != nil {
			elem = t.Elem.String()
		}

		length := t.InlineLengthKey

		if length == "" {
			if t.InlineLengthKnown {
				length = strconv.FormatInt(
					t.InlineLength,
					10,
				)
			} else {
				length = "<invalid>"
			}
		}

		return fmt.Sprintf(
			"@inline_array<%s, %s>",
			elem,
			length,
		)

	case TypeVariadic:
		if t.Elem == nil {
			return "...<invalid>"
		}

		return "..." + t.Elem.String()

	case TypeTask:
		var params []string

		for i, p := range t.Params {
			if i < len(t.ParamIsVariadic) &&
				t.ParamIsVariadic[i] {
				params = append(
					params,
					"..."+p.String(),
				)
			} else {
				params = append(
					params,
					p.String(),
				)
			}
		}

		var results []string

		for _, r := range t.Results {
			results = append(
				results,
				r.String(),
			)
		}

		if len(results) == 0 {
			return fmt.Sprintf(
				"task(%s)",
				strings.Join(params, ", "),
			)
		}

		return fmt.Sprintf(
			"task(%s) %s",
			strings.Join(params, ", "),
			strings.Join(results, ", "),
		)

	case TypePackage:
		return "package " + t.Name

	case TypeNil:
		return "nil"

	case TypeEnumLiteral:
		return "." + t.Name

	case TypeInterfaceSelf:
		return "self"

	case TypeStruct,
		TypeEnum,
		TypeUnion,
		TypeInterface,
		TypeTypeParam,
		TypeValueParam:
		return t.Name

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

	// Exported interface implementation patterns.
	Impls []*ImplInfo
}

type OverloadInfo struct {
	Name       string
	IsOperator bool
	Candidates []*Symbol
	Span       source.Span
}

type IndexResolutionKind int

const (
	IndexResolutionInvalid IndexResolutionKind = iota

	IndexResolutionVariadicRead
	IndexResolutionVariadicWrite

	IndexResolutionInlineArrayRead
	IndexResolutionInlineArrayWrite

	IndexResolutionPrimitiveByteRead
	IndexResolutionPrimitiveByteWrite

	IndexResolutionRawptrRead
	IndexResolutionRawptrWrite

	// UTF-8 character indexing. Both return char.
	IndexResolutionStringRead
	IndexResolutionCstringRead

	IndexResolutionOverloadRead
	IndexResolutionOverloadWrite
)

type GenericOverloadCallResolution struct {
	Candidate *Symbol
	TaskType  *Type

	PackageName string

	GenericArguments []GenericArgumentInfo
}

type InterfaceConversionResolution struct {
	// SourcePointer is the complete source type passed to cast, such as:
	//
	//     *mem.CAllocator
	SourcePointer *Type

	// Concrete is the implementation target after removing the pointer:
	//
	//     mem.CAllocator
	Concrete *Type

	// Interface is the destination interface:
	//
	//     mem.Allocator
	Interface *Type

	// Impl is the exact implementation selected by the checker.
	Impl *ResolvedImpl
}

type SemanticInfo struct {
	// ExprTypes contains the checker-resolved type of every expression.
	//
	// C codegen must prefer this information over attempting to infer the
	// expression type again.
	ExprTypes map[ast.Expr]*Type

	IndexResolutions map[*ast.IndexExpr]IndexResolution
	LenResolutions   map[*ast.CallExpr]LenResolution

	GenericOverloadCalls map[*ast.GenericExpr]GenericOverloadCallResolution

	// InterfaceConversions records successful:
	//
	//     cast<SomeInterface>(&value)
	//
	// conversions. The key is the GenericExpr used as the call callee.
	InterfaceConversions map[*ast.GenericExpr]InterfaceConversionResolution
}

func (c *Checker) SemanticInfo() SemanticInfo {
	info := SemanticInfo{
		ExprTypes: make(
			map[ast.Expr]*Type,
			len(c.exprTypes),
		),
		IndexResolutions: make(
			map[*ast.IndexExpr]IndexResolution,
			len(c.indexResolutions),
		),
		LenResolutions: make(
			map[*ast.CallExpr]LenResolution,
			len(c.lenResolutions),
		),
		GenericOverloadCalls: make(
			map[*ast.GenericExpr]GenericOverloadCallResolution,
			len(c.genericOverloadCalls),
		),
		InterfaceConversions: make(
			map[*ast.GenericExpr]InterfaceConversionResolution,
			len(c.interfaceConversions),
		),
	}

	for expr, typ := range c.exprTypes {
		info.ExprTypes[expr] = typ
	}

	for expr, resolution := range c.indexResolutions {
		info.IndexResolutions[expr] = resolution
	}

	for call, resolution := range c.lenResolutions {
		info.LenResolutions[call] = resolution
	}

	for expr, resolution := range c.genericOverloadCalls {
		cloned := resolution
		cloned.GenericArguments = append(
			[]GenericArgumentInfo(nil),
			resolution.GenericArguments...,
		)

		info.GenericOverloadCalls[expr] = cloned
	}

	for expr, resolution := range c.interfaceConversions {
		info.InterfaceConversions[expr] = resolution
	}

	return info
}

type IndexResolution struct {
	Kind IndexResolutionKind

	Candidate *Symbol
	TaskType  *Type

	PackageName      string
	GenericArguments []GenericArgumentInfo
}

type LenResolutionKind int

const (
	LenResolutionInvalid LenResolutionKind = iota

	LenResolutionVariadic

	LenResolutionInlineArray

	// UTF-8 Unicode scalar count.
	LenResolutionString
	LenResolutionCstring

	LenResolutionOverload
)

type LenResolution struct {
	Kind LenResolutionKind

	Candidate *Symbol
	TaskType  *Type

	PackageName      string
	GenericArguments []GenericArgumentInfo
}

type Scope struct {
	Parent  *Scope
	Symbols map[string]*Symbol

	// Top-level impl patterns prepared in this scope.
	Impls []*ImplInfo
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

type Options struct {
	// 	<= 0 means disabled
	GenericConstraintMaxDepth int
}

type Checker struct {
	diags *diag.Reporter

	global   *Scope
	packages map[string]*PackageInfo

	specializedTypes map[string]*Type

	currentResults []*Type

	currentLoopDepth  int
	currentDeferDepth int

	preparingTasks map[*ast.TaskDecl]bool

	preparingOverloads map[*ast.OverloadDecl]bool
	preparedOverloads  map[*ast.OverloadDecl]bool

	exprTypes map[ast.Expr]*Type

	indexResolutions map[*ast.IndexExpr]IndexResolution
	lenResolutions   map[*ast.CallExpr]LenResolution

	genericOverloadCalls map[*ast.GenericExpr]GenericOverloadCallResolution

	interfaceConversions map[*ast.GenericExpr]InterfaceConversionResolution

	options Options

	genericConstraintEvalStack []string
	packageScopes              map[string]*Scope

	impls      []*ImplInfo
	implByDecl map[*ast.ImplDecl]*ImplInfo
}

func New(diags *diag.Reporter) *Checker {
	return NewWithPackagesAndOptions(diags, nil, Options{})
}

func NewWithPackages(diags *diag.Reporter, packages map[string]*PackageInfo) *Checker {
	return NewWithPackagesAndOptions(diags, packages, Options{})
}

func NewWithPackagesAndOptions(
	diags *diag.Reporter,
	packages map[string]*PackageInfo,
	options Options,
) *Checker {
	if packages == nil {
		packages = map[string]*PackageInfo{}
	}

	c := &Checker{
		diags:            diags,
		packages:         packages,
		specializedTypes: map[string]*Type{},

		preparingTasks:     map[*ast.TaskDecl]bool{},
		preparingOverloads: map[*ast.OverloadDecl]bool{},
		preparedOverloads:  map[*ast.OverloadDecl]bool{},

		exprTypes: map[ast.Expr]*Type{},

		indexResolutions: map[*ast.IndexExpr]IndexResolution{},
		lenResolutions:   map[*ast.CallExpr]LenResolution{},

		genericOverloadCalls: map[*ast.GenericExpr]GenericOverloadCallResolution{},

		interfaceConversions: map[*ast.GenericExpr]InterfaceConversionResolution{},

		options:       options,
		packageScopes: map[string]*Scope{},
		implByDecl:    map[*ast.ImplDecl]*ImplInfo{},
	}

	c.global = NewScope(nil)
	c.declareBuiltins(c.global)
	c.declarePackages(c.global)
	c.importPackageImpls()

	return c
}

func (c *Checker) IndexResolutionFor(
	expr *ast.IndexExpr,
) (IndexResolution, bool) {
	if c == nil || expr == nil {
		return IndexResolution{}, false
	}

	resolution, ok := c.indexResolutions[expr]
	return resolution, ok
}

func (c *Checker) ExprTypeFor(
	expr ast.Expr,
) (*Type, bool) {
	if c == nil || expr == nil {
		return nil, false
	}

	typ, ok := c.exprTypes[expr]
	return typ, ok
}

func (c *Checker) InterfaceConversionFor(
	expr *ast.GenericExpr,
) (InterfaceConversionResolution, bool) {
	if c == nil || expr == nil {
		return InterfaceConversionResolution{}, false
	}

	resolution, ok := c.interfaceConversions[expr]
	return resolution, ok
}

func (c *Checker) LenResolutionFor(
	expr *ast.CallExpr,
) (LenResolution, bool) {
	if c == nil || expr == nil {
		return LenResolution{}, false
	}

	resolution, ok := c.lenResolutions[expr]
	return resolution, ok
}

type overloadLookup struct {
	Symbol *Symbol
	Scope  *Scope
	Name   string

	PackageName string
}

func (c *Checker) overloadLookups(
	scope *Scope,
	name string,
	argTypes []*Type,
) []overloadLookup {
	if scope == nil {
		scope = c.global
	}

	var out []overloadLookup
	seen := map[string]bool{}

	add := func(
		key string,
		sym *Symbol,
		lookupScope *Scope,
		displayName string,
		packageName string,
	) {
		if sym == nil ||
			sym.Kind != SymbolOverload ||
			sym.Overload == nil {
			return
		}

		if seen[key] {
			return
		}

		seen[key] = true

		out = append(out, overloadLookup{
			Symbol:      sym,
			Scope:       lookupScope,
			Name:        displayName,
			PackageName: packageName,
		})
	}

	add(
		name,
		scope.Lookup(name),
		scope,
		name,
		"",
	)

	for _, typ := range argTypes {
		pkgName, ok := packageNameFromType(typ)
		if !ok {
			continue
		}

		pkgSym := scope.Lookup(pkgName)
		if pkgSym == nil ||
			pkgSym.Kind != SymbolPackage ||
			pkgSym.Package == nil {
			continue
		}

		member := pkgSym.Package.Symbols[name]
		if member == nil ||
			member.Kind != SymbolOverload ||
			member.Overload == nil {
			continue
		}

		pkgScope := c.scopeForPackageInfo(
			pkgName,
			pkgSym.Package,
		)
		qualifiedMember := c.importedPackageMemberSymbol(
			pkgName,
			member,
		)

		add(
			pkgName+"."+name,
			qualifiedMember,
			pkgScope,
			pkgName+"."+name,
			pkgName,
		)
	}

	return out
}

func nominalBaseName(typ *Type) string {
	if typ == nil {
		return ""
	}

	// GenericBaseName correctly distinguishes:
	//
	//     LocalBox<api.Item>  -> LocalBox
	//     api.Box<LocalItem>  -> api.Box
	//
	// Looking only at Name would incorrectly interpret dots appearing inside
	// generic arguments as package ownership.
	if typ.GenericBaseName != "" {
		return typ.GenericBaseName
	}

	name := typ.Name

	if i := strings.Index(name, "<"); i >= 0 {
		name = name[:i]
	}

	return name
}

func currentPackageOwnsInterface(typ *Type) bool {
	if typ == nil || typ.Kind != TypeInterface {
		return false
	}

	name := nominalBaseName(typ)

	return name != "" &&
		!strings.Contains(name, ".")
}

func currentPackageOwnsImplTarget(typ *Type) bool {
	if typ == nil {
		return false
	}

	switch typ.Kind {
	case TypePointer,
		TypeVariadic:
		// A wrapper does not create ownership of the wrapped type.
		return currentPackageOwnsImplTarget(typ.Elem)

	case TypeStruct,
		TypeDistinct,
		TypeEnum,
		TypeUnion:
		name := nominalBaseName(typ)

		return name != "" &&
			!strings.Contains(name, ".")

	default:
		// Builtins, raw pointers, task types, type parameters, interfaces and
		// other structural/compiler types are not owned nominal target types.
		return false
	}
}

func packageNameFromType(typ *Type) (string, bool) {
	if typ == nil {
		return "", false
	}

	switch typ.Kind {
	case TypePointer,
		TypeVariadic:
		return packageNameFromType(typ.Elem)
	}

	name := nominalBaseName(typ)
	if name == "" {
		return "", false
	}

	dot := strings.Index(name, ".")
	if dot <= 0 {
		return "", false
	}

	return name[:dot], true
}

func (c *Checker) CheckFile(file *ast.File) *Scope {
	for _, decl := range file.Decls {
		c.declareDecl(c.global, decl)
	}

	// Prepare all named types, tasks, and interfaces before impl patterns.
	for _, decl := range file.Decls {
		switch decl.(type) {
		case *ast.OverloadDecl, *ast.ImplDecl:
			continue
		}

		c.prepareDecl(c.global, decl)
	}

	for _, decl := range file.Decls {
		overload, ok := decl.(*ast.OverloadDecl)
		if !ok {
			continue
		}

		c.prepareOverloadDecl(c.global, overload)
	}

	// Every impl pattern must be registered before any impl is checked.
	for _, decl := range file.Decls {
		impl, ok := decl.(*ast.ImplDecl)
		if !ok {
			continue
		}

		c.prepareImplDecl(c.global, impl)
	}

	// Impl signatures are checked before task bodies so casts and interface
	// dispatch inside ordinary tasks can resolve every local impl.
	for _, decl := range file.Decls {
		impl, ok := decl.(*ast.ImplDecl)
		if !ok {
			continue
		}

		c.checkImplDecl(c.global, impl)
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

type genericOverloadResolution struct {
	Candidate *Symbol
	TaskType  *Type

	GenericArguments []overloadGenericArgument

	Score int

	Matched   bool
	Ambiguous bool
}

type overloadGenericArgumentClass int

const (
	overloadGenericArgumentInvalid overloadGenericArgumentClass = iota
	overloadGenericArgumentType
	overloadGenericArgumentTask
	overloadGenericArgumentValue
)

type overloadGenericArgument struct {
	Class overloadGenericArgumentClass

	Type *Type
	Expr ast.Expr

	Key string

	IsCompileTime bool
}

type receiverOverloadResolution struct {
	Candidate *Symbol
	TaskType  *Type

	GenericArguments []GenericArgumentInfo
	PackageName      string

	Score int

	Matched     bool
	Ambiguous   bool
	HadOverload bool
}

func (c *Checker) checkCallArgumentTypes(
	scope *Scope,
	args []ast.Expr,
) ([]*Type, []source.Span) {
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
			c.diags.Add(
				spread.Span(),
				"spread argument must be the last argument",
			)
		}

		spreadType := c.checkExpr(scope, spread.Expr)

		if spreadType.Kind != TypeVariadic {
			c.diags.Add(
				spread.Span(),
				fmt.Sprintf(
					"cannot spread %s; expected variadic value",
					spreadType.String(),
				),
			)

			types = append(types, InvalidType)
			spans = append(spans, spread.Span())
			continue
		}

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
	}

	return types, spans
}

func (c *Checker) resolveOverload(
	info *OverloadInfo,
	argTypes []*Type,
) overloadResolution {
	best := overloadResolution{
		Score: 1 << 30,
	}

	for _, candidate := range info.Candidates {
		if candidate.Type == nil ||
			candidate.Type.Kind != TypeTask {
			continue
		}

		// Generic overload candidates require an explicit generic call:
		//
		//     Process<int>(value)
		//
		// They must not participate in:
		//
		//     Process(value)
		//
		// Otherwise a zero-runtime-parameter generic candidate such as
		// Foo<T type>() would incorrectly match Foo().
		if len(candidate.Type.GenericParams) > 0 {
			continue
		}

		score, ok := c.callScore(
			candidate.Type,
			argTypes,
		)
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

func (c *Checker) genericBaseSymbol(
	scope *Scope,
	base ast.Expr,
) (*Symbol, string, bool) {
	if scope == nil {
		scope = c.global
	}

	switch b := base.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(b.Name.Name)
		if sym == nil {
			c.diags.Add(
				b.Span(),
				fmt.Sprintf(
					"undefined generic symbol %q",
					b.Name.Name,
				),
			)
			return nil, "", false
		}

		return sym, "", true

	case *ast.SelectorExpr:
		id, ok := b.Left.(*ast.IdentExpr)
		if !ok {
			c.diags.Add(
				b.Span(),
				"generic symbol must be a task, overload, type, or package-qualified member",
			)
			return nil, "", false
		}

		pkgSym := scope.Lookup(id.Name.Name)
		if pkgSym == nil {
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"undefined package %q",
					id.Name.Name,
				),
			)
			return nil, "", false
		}

		if pkgSym.Kind != SymbolPackage {
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"%q is not a package",
					id.Name.Name,
				),
			)
			return nil, "", false
		}

		if pkgSym.Package == nil {
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"package %q has no symbol table",
					id.Name.Name,
				),
			)
			return nil, "", false
		}

		member := pkgSym.Package.Symbols[b.Name.Name]
		if member == nil {
			c.diags.Add(
				b.Name.Span(),
				fmt.Sprintf(
					"package %s has no symbol %q",
					id.Name.Name,
					b.Name.Name,
				),
			)
			return nil, "", false
		}

		return c.importedPackageMemberSymbol(
			id.Name.Name,
			member,
		), id.Name.Name, true

	default:
		c.diags.Add(
			base.Span(),
			"expected generic task or overload name",
		)
		return nil, "", false
	}
}

func (c *Checker) overloadGenericArgumentFromSymbol(
	scope *Scope,
	arg ast.GenericArg,
	sym *Symbol,
) overloadGenericArgument {
	if sym == nil {
		return overloadGenericArgument{
			Class: overloadGenericArgumentInvalid,
			Key:   genericArgDisplay(arg),
		}
	}

	switch sym.Kind {
	case SymbolType:
		return overloadGenericArgument{
			Class: overloadGenericArgumentType,
			Type:  sym.Type,
			Key:   genericArgDisplay(arg),
		}

	case SymbolTask:
		c.ensureTaskSymbolPrepared(scope, sym)

		if sym.Type == nil ||
			sym.Type.Kind != TypeTask {
			return overloadGenericArgument{
				Class: overloadGenericArgumentInvalid,
				Key:   genericArgDisplay(arg),
			}
		}

		if len(sym.Type.GenericParams) > 0 {
			c.diags.Add(
				arg.Span(),
				fmt.Sprintf(
					"generic task argument %q requires specialization",
					sym.Name,
				),
			)

			return overloadGenericArgument{
				Class: overloadGenericArgumentInvalid,
				Key:   genericArgDisplay(arg),
			}
		}

		return overloadGenericArgument{
			Class: overloadGenericArgumentTask,
			Type:  sym.Type,
			Key:   genericArgDisplay(arg),
		}

	case SymbolConst:
		return overloadGenericArgument{
			Class:         overloadGenericArgumentValue,
			Type:          sym.Type,
			Expr:          arg.Expr,
			Key:           genericArgDisplay(arg),
			IsCompileTime: true,
		}

	case SymbolVar,
		SymbolParam:
		return overloadGenericArgument{
			Class: overloadGenericArgumentValue,
			Type:  sym.Type,
			Expr:  arg.Expr,
			Key:   genericArgDisplay(arg),

			// Runtime variables cannot satisfy a comptime generic parameter.
			IsCompileTime: false,
		}

	default:
		return overloadGenericArgument{
			Class: overloadGenericArgumentInvalid,
			Key:   genericArgDisplay(arg),
		}
	}
}

func (c *Checker) resolveOverloadGenericArgument(
	scope *Scope,
	arg ast.GenericArg,
) overloadGenericArgument {
	switch arg.Kind {
	case ast.GenericArgType:
		typ := c.typeFromGenericArg(
			scope,
			arg,
		)

		return overloadGenericArgument{
			Class: overloadGenericArgumentType,
			Type:  typ,
			Key:   genericArgDisplay(arg),
		}

	case ast.GenericArgExpr:
		if arg.Expr == nil {
			return overloadGenericArgument{
				Class: overloadGenericArgumentInvalid,
				Key:   genericArgDisplay(arg),
			}
		}

		switch e := arg.Expr.(type) {
		case *ast.IdentExpr:
			sym := scope.Lookup(e.Name.Name)
			if sym == nil {
				c.diags.Add(
					e.Span(),
					fmt.Sprintf(
						"undefined generic argument %q",
						e.Name.Name,
					),
				)

				return overloadGenericArgument{
					Class: overloadGenericArgumentInvalid,
					Key:   genericArgDisplay(arg),
				}
			}

			return c.overloadGenericArgumentFromSymbol(
				scope,
				arg,
				sym,
			)

		case *ast.SelectorExpr:
			if id, ok := e.Left.(*ast.IdentExpr); ok {
				pkgSym := scope.Lookup(id.Name.Name)

				if pkgSym != nil &&
					pkgSym.Kind == SymbolPackage {
					sym, _, ok := c.genericBaseSymbol(
						scope,
						e,
					)
					if !ok {
						return overloadGenericArgument{
							Class: overloadGenericArgumentInvalid,
							Key:   genericArgDisplay(arg),
						}
					}

					return c.overloadGenericArgumentFromSymbol(
						scope,
						arg,
						sym,
					)
				}
			}

		case *ast.GenericExpr:
			sym, _, ok := c.genericBaseSymbol(
				scope,
				e.Base,
			)
			if !ok {
				return overloadGenericArgument{
					Class: overloadGenericArgumentInvalid,
					Key:   genericArgDisplay(arg),
				}
			}

			switch sym.Kind {
			case SymbolType:
				typeAst := typeAstFromExpr(e)
				if typeAst == nil {
					c.diags.Add(
						e.Span(),
						"expected generic type argument",
					)

					return overloadGenericArgument{
						Class: overloadGenericArgumentInvalid,
						Key:   genericArgDisplay(arg),
					}
				}

				return overloadGenericArgument{
					Class: overloadGenericArgumentType,
					Type: c.typeFromAst(
						scope,
						typeAst,
					),
					Key: genericArgDisplay(arg),
				}

			case SymbolTask:
				return overloadGenericArgument{
					Class: overloadGenericArgumentTask,
					Type: c.taskFromGenericArg(
						scope,
						arg,
					),
					Key: genericArgDisplay(arg),
				}

			case SymbolOverload:
				c.diags.Add(
					e.Span(),
					"an overload cannot currently be passed as a task generic argument",
				)

				return overloadGenericArgument{
					Class: overloadGenericArgumentInvalid,
					Key:   genericArgDisplay(arg),
				}
			}
		}

		valueType := c.checkExpr(
			scope,
			arg.Expr,
		)

		isCompileTime := c.isCompileTimeGenericExpr(
			scope,
			arg.Expr,
		)

		if _, ok := c.evalGenericConstExpr(
			scope,
			arg.Expr,
		); ok {
			isCompileTime = true
		}

		return overloadGenericArgument{
			Class:         overloadGenericArgumentValue,
			Type:          valueType,
			Expr:          arg.Expr,
			Key:           genericArgDisplay(arg),
			IsCompileTime: isCompileTime,
		}
	}

	return overloadGenericArgument{
		Class: overloadGenericArgumentInvalid,
		Key:   genericArgDisplay(arg),
	}
}

func (c *Checker) resolveOverloadGenericArguments(
	scope *Scope,
	args []ast.GenericArg,
) []overloadGenericArgument {
	out := make(
		[]overloadGenericArgument,
		0,
		len(args),
	)

	for _, arg := range args {
		out = append(
			out,
			c.resolveOverloadGenericArgument(
				scope,
				arg,
			),
		)
	}

	return out
}

func (c *Checker) overloadGenericArgumentScore(
	scope *Scope,
	resolved overloadGenericArgument,
	param ast.GenericParam,
	subst map[string]ast.GenericArg,
) (int, bool) {
	if resolved.Class ==
		overloadGenericArgumentInvalid ||
		resolved.Type == nil ||
		resolved.Type.Kind == TypeInvalid {
		return 0, false
	}

	switch param.Category {
	case ast.GenericParamType:
		if resolved.Class !=
			overloadGenericArgumentType {
			return 0, false
		}

		switch resolved.Type.Kind {
		case TypeEnum,
			TypeUnion,
			TypeInterface,
			TypeTask,
			TypePackage:
			return 0, false
		}

		return 0, true

	case ast.GenericParamEnum:
		return 0,
			resolved.Class ==
				overloadGenericArgumentType &&
				resolved.Type.Kind ==
					TypeEnum

	case ast.GenericParamUnion:
		return 0,
			resolved.Class ==
				overloadGenericArgumentType &&
				resolved.Type.Kind ==
					TypeUnion

	case ast.GenericParamTask:
		// The task signature constraint is validated after selecting the
		// candidate. It does not participate in overload identity.
		return 0,
			resolved.Class ==
				overloadGenericArgumentTask &&
				resolved.Type.Kind ==
					TypeTask

	case ast.GenericParamInt:
		if resolved.Class !=
			overloadGenericArgumentValue ||
			!resolved.IsCompileTime {
			return 0, false
		}

		return c.conversionScore(
			IntType,
			resolved.Type,
		)

	case ast.GenericParamBool:
		if resolved.Class !=
			overloadGenericArgumentValue ||
			!resolved.IsCompileTime {
			return 0, false
		}

		return c.conversionScore(
			BoolType,
			resolved.Type,
		)

	case ast.GenericParamString:
		if resolved.Class !=
			overloadGenericArgumentValue ||
			!resolved.IsCompileTime {
			return 0, false
		}

		return c.conversionScore(
			StringType,
			resolved.Type,
		)

	case ast.GenericParamValue:
		if resolved.Class !=
			overloadGenericArgumentValue ||
			!resolved.IsCompileTime ||
			param.Type == nil {
			return 0, false
		}

		expected := c.typeFromAstWithGenericArgs(
			scope,
			param.Type,
			subst,
		)

		if expected == nil ||
			expected.Kind == TypeInvalid {
			return 0, false
		}

		return c.conversionScore(
			expected,
			resolved.Type,
		)
	}

	return 0, false
}

func (c *Checker) genericOverloadArgumentsScore(
	scope *Scope,
	resolved []overloadGenericArgument,
	args []ast.GenericArg,
	params []ast.GenericParam,
) (int, bool) {
	if len(resolved) != len(params) ||
		len(args) != len(params) {
		return 0, false
	}

	subst := genericArgSubst(
		params,
		args,
	)

	score := 0

	for i := range params {
		itemScore, ok :=
			c.overloadGenericArgumentScore(
				scope,
				resolved[i],
				params[i],
				subst,
			)
		if !ok {
			return 0, false
		}

		score += itemScore
	}

	return score, true
}

func (c *Checker) instantiateGenericOverloadCandidate(
	scope *Scope,
	packageName string,
	candidate *Symbol,
	args []ast.GenericArg,
) *Type {
	if candidate == nil ||
		candidate.Type == nil ||
		candidate.Type.Kind != TypeTask {
		return InvalidType
	}

	if len(args) !=
		len(candidate.Type.GenericParams) {
		return InvalidType
	}

	if packageName != "" {
		return c.taskTypeFromImportedGenericSignature(
			scope,
			packageName,
			candidate.Type,
			args,
			candidate.Span,
		)
	}

	if taskDecl, ok :=
		candidate.Node.(*ast.TaskDecl); ok {
		return c.taskTypeFromGenericCall(
			scope,
			taskDecl,
			args,
		)
	}

	return c.taskTypeFromGenericSignature(
		scope,
		candidate.Type,
		args,
		candidate.Span,
	)
}

func (c *Checker) resolveGenericOverload(
	scope *Scope,
	info *OverloadInfo,
	packageName string,
	genericArgs []ast.GenericArg,
	resolvedGenericArgs []overloadGenericArgument,
	runtimeArgTypes []*Type,
) genericOverloadResolution {
	best := genericOverloadResolution{
		Score: 1 << 30,
	}

	if info == nil {
		return best
	}

	for _, candidate := range info.Candidates {
		if candidate == nil ||
			candidate.Type == nil ||
			candidate.Type.Kind != TypeTask {
			continue
		}

		// A generic call considers only generic candidates.
		if len(candidate.Type.GenericParams) == 0 {
			continue
		}

		genericScore, ok :=
			c.genericOverloadArgumentsScore(
				scope,
				resolvedGenericArgs,
				genericArgs,
				candidate.Type.GenericParams,
			)
		if !ok {
			continue
		}

		instantiated :=
			c.instantiateGenericOverloadCandidate(
				scope,
				packageName,
				candidate,
				genericArgs,
			)

		if instantiated == nil ||
			instantiated.Kind != TypeTask {
			continue
		}

		runtimeScore, ok := c.callScore(
			instantiated,
			runtimeArgTypes,
		)
		if !ok {
			continue
		}

		score := genericScore + runtimeScore

		if !best.Matched || score < best.Score {
			best = genericOverloadResolution{
				Candidate:        candidate,
				TaskType:         instantiated,
				GenericArguments: resolvedGenericArgs,
				Score:            score,
				Matched:          true,
			}
			continue
		}

		if score == best.Score {
			best.Ambiguous = true
		}
	}

	return best
}

func overloadGenericArgumentInfos(
	params []ast.GenericParam,
	args []ast.GenericArg,
	resolved []overloadGenericArgument,
) []GenericArgumentInfo {
	count := len(params)

	if len(args) < count {
		count = len(args)
	}

	if len(resolved) < count {
		count = len(resolved)
	}

	out := make(
		[]GenericArgumentInfo,
		0,
		count,
	)

	for i := 0; i < count; i++ {
		info := GenericArgumentInfo{
			Category: params[i].Category,
			Type:     resolved[i].Type,
			Key:      resolved[i].Key,
		}

		if isValueGenericCategory(
			params[i].Category,
		) {
			info.Expr = args[i].Expr
		}

		out = append(out, info)
	}

	return out
}

func formatGenericArguments(
	args []ast.GenericArg,
) string {
	parts := make(
		[]string,
		0,
		len(args),
	)

	for _, arg := range args {
		parts = append(
			parts,
			genericArgDisplay(arg),
		)
	}

	return strings.Join(parts, ", ")
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

func (c *Checker) conversionScore(
	dst *Type,
	src *Type,
) (int, bool) {
	if dst == nil || src == nil {
		return 100, true
	}

	if dst.Kind == TypeInvalid ||
		src.Kind == TypeInvalid {
		return 100, true
	}

	if c.sameType(dst, src) {
		return 0, true
	}

	if src.Kind == TypeNil {
		if c.typeAcceptsNil(dst) {
			return 1, true
		}

		return 0, false
	}

	if dst.Kind == TypeAny {
		return 20, true
	}

	if dst.Kind == TypeDistinct {
		if src.Kind == TypeUntypedInt ||
			src.Kind == TypeUntypedFloat {
			if !c.assignable(
				dst.Underlying,
				src,
			) {
				return 0, false
			}

			if !integerConstantConversionAllowed(
				dst.Underlying,
				src,
			) {
				return 0, false
			}

			return 1, true
		}

		return 0, false
	}

	if src.Kind == TypeDistinct {
		return 0, false
	}

	if dst.Kind == TypeEnum &&
		src.Kind == TypeEnumLiteral {
		if c.enumHasVariant(
			dst,
			src.Name,
		) {
			return 1, true
		}

		return 0, false
	}

	if dst.Kind == TypeUnion {
		if c.unionHasMember(dst, src) {
			return 1, true
		}

		return 0, false
	}

	if dst.Kind == TypeInterface {
		return 0, false
	}

	if src.Kind == TypeUntypedInt {
		if c.isIntegerLike(dst) {
			if !integerConstantConversionAllowed(
				dst,
				src,
			) {
				return 0, false
			}

			return 1, true
		}

		switch dst.Kind {
		case TypeF32:
			return 3, true

		case TypeF64:
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

func (c *Checker) importedPackageMemberSymbol(packageName string, sym *Symbol) *Symbol {
	if sym == nil {
		return nil
	}

	out := *sym

	if out.Type != nil {
		out.Type = c.qualifyImportedTypeForPackage(packageName, out.Type)
	}

	if out.Overload != nil {
		overload := *out.Overload
		overload.Candidates = nil

		for _, candidate := range out.Overload.Candidates {
			overload.Candidates = append(overload.Candidates, c.importedPackageMemberSymbol(packageName, candidate))
		}

		out.Overload = &overload

		// Imported overloads already carry exported candidate signatures.
		// Do not re-prepare them against the imported package scope, because
		// that would rebuild candidates with unqualified local type names.
		out.Node = nil
	}

	return &out
}

func (c *Checker) qualifyImportedTypeForPackage(packageName string, typ *Type) *Type {
	return c.qualifyImportedTypeForPackageSeen(packageName, typ, map[*Type]*Type{})
}

func (c *Checker) qualifyImportedTypeForPackageSeen(packageName string, typ *Type, seen map[*Type]*Type) *Type {
	if typ == nil {
		return nil
	}

	if packageName == "" {
		return typ
	}

	if cached := seen[typ]; cached != nil {
		return cached
	}

	out := *typ
	seen[typ] = &out

	switch typ.Kind {
	case TypeStruct,
		TypeDistinct,
		TypeEnum,
		TypeUnion,
		TypeInterface:
		if out.Name != "" && !strings.Contains(out.Name, ".") {
			out.Name = packageName + "." + out.Name
		}
	}

	if out.GenericBaseName != "" &&
		!strings.Contains(out.GenericBaseName, ".") {
		out.GenericBaseName = packageName + "." + out.GenericBaseName
	}

	out.GenericArguments = nil

	for _, arg := range typ.GenericArguments {
		cloned := arg
		cloned.Type = c.qualifyImportedTypeForPackageSeen(
			packageName,
			arg.Type,
			seen,
		)

		out.GenericArguments = append(
			out.GenericArguments,
			cloned,
		)
	}

	out.Elem = c.qualifyImportedTypeForPackageSeen(
		packageName,
		typ.Elem,
		seen,
	)
	out.Underlying = c.qualifyImportedTypeForPackageSeen(packageName, typ.Underlying, seen)

	out.Fields = nil
	for _, field := range typ.Fields {
		out.Fields = append(out.Fields, FieldInfo{
			Name:    field.Name,
			Type:    c.qualifyImportedTypeForPackageSeen(packageName, field.Type, seen),
			TypeAst: field.TypeAst,
			Span:    field.Span,
		})
	}

	out.Members = nil
	for _, member := range typ.Members {
		out.Members = append(out.Members, c.qualifyImportedTypeForPackageSeen(packageName, member, seen))
	}

	out.InterfaceRequirements = nil
	for _, req := range typ.InterfaceRequirements {
		cloned := InterfaceRequirementInfo{
			Name:            req.Name,
			ParamIsVariadic: append([]bool(nil), req.ParamIsVariadic...),
			IsPure:          req.IsPure,
			IsTrustedPure:   req.IsTrustedPure,
			Span:            req.Span,
		}

		for _, param := range req.Params {
			cloned.Params = append(cloned.Params, c.qualifyImportedTypeForPackageSeen(packageName, param, seen))
		}

		for _, result := range req.Results {
			cloned.Results = append(cloned.Results, c.qualifyImportedTypeForPackageSeen(packageName, result, seen))
		}

		out.InterfaceRequirements = append(out.InterfaceRequirements, cloned)
	}

	out.Implements = nil
	for _, iface := range typ.Implements {
		out.Implements = append(out.Implements, c.qualifyImportedTypeForPackageSeen(packageName, iface, seen))
	}

	out.Params = nil
	for _, param := range typ.Params {
		out.Params = append(out.Params, c.qualifyImportedTypeForPackageSeen(packageName, param, seen))
	}

	out.Results = nil
	for _, result := range typ.Results {
		out.Results = append(out.Results, c.qualifyImportedTypeForPackageSeen(packageName, result, seen))
	}

	out.ParamDefaults = append([]ast.Expr(nil), typ.ParamDefaults...)
	out.ParamHasDefault = append([]bool(nil), typ.ParamHasDefault...)
	out.ParamIsVariadic = append([]bool(nil), typ.ParamIsVariadic...)
	out.GenericParams = append([]ast.GenericParam(nil), typ.GenericParams...)

	return &out
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

func (c *Checker) declareDecl(
	scope *Scope,
	decl ast.Decl,
) {
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
				GenericParams: append(
					[]ast.GenericParam(nil),
					d.GenericParams...,
				),
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
				GenericParams: append(
					[]ast.GenericParam(nil),
					d.GenericParams...,
				),
				IsDynInterface: d.IsDyn,
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
		// c :: @c_import { ... } is codegen metadata, not a visible
		// Seal symbol.
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

func (c *Checker) ensureOverloadSymbolPrepared(scope *Scope, sym *Symbol) {
	if sym == nil || sym.Kind != SymbolOverload || sym.Overload == nil {
		return
	}

	decl, ok := sym.Node.(*ast.OverloadDecl)
	if !ok {
		return
	}

	if scope == nil {
		scope = c.global
	}

	c.prepareOverloadDecl(scope, decl)
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
	if d == nil {
		return
	}

	if scope == nil {
		scope = c.global
	}

	sym := scope.Lookup(d.Name)
	if sym == nil || sym.Overload == nil {
		return
	}

	if c.preparedOverloads[d] {
		return
	}

	if c.preparingOverloads[d] {
		return
	}

	c.preparingOverloads[d] = true
	defer delete(c.preparingOverloads, d)

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

		c.ensureTaskSymbolPrepared(scope, candidate)

		if candidate.Type == nil || candidate.Type.Kind != TypeTask {
			c.diags.Add(name.Span(), fmt.Sprintf("overload candidate %q has invalid task type", name.Name))
			continue
		}

		candidates = append(candidates, candidate)
	}

	sym.Overload.Candidates = candidates
	c.preparedOverloads[d] = true
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

func (c *Checker) prepareStructDecl(
	parent *Scope,
	d *ast.StructDecl,
) {
	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil || sym.Type == nil {
		return
	}

	/*
		Generic metadata must be available before resolving fields.

		This is also initialized by declareDecl, but assigning it here keeps
		this function correct when it is used for a locally declared struct or
		through any future preparation path.
	*/
	sym.Type.GenericParams = append(
		[]ast.GenericParam(nil),
		d.GenericParams...,
	)

	scope := c.scopeWithGenericParams(
		parent,
		d.GenericParams,
	)

	fields := make(
		[]FieldInfo,
		0,
		len(d.Fields),
	)

	for _, field := range d.Fields {
		fieldType := c.typeFromAst(
			scope,
			field.Type,
		)

		fields = append(fields, FieldInfo{
			Name:    field.Name.Name,
			Type:    fieldType,
			TypeAst: field.Type,
			Span:    field.Name.Span(),
		})
	}

	sym.Type.Fields = fields
}

func (c *Checker) prepareInterfaceDecl(
	parent *Scope,
	d *ast.InterfaceDecl,
) {
	sym := parent.LookupLocal(
		d.Name.Name,
	)

	if sym == nil ||
		sym.Type == nil {
		return
	}

	sym.Type.GenericParams = append(
		[]ast.GenericParam(nil),
		d.GenericParams...,
	)

	sym.Type.IsDynInterface = d.IsDyn

	scope := c.scopeWithGenericParams(
		parent,
		d.GenericParams,
	)

	requirements := make(
		[]InterfaceRequirementInfo,
		0,
		len(d.Requirements),
	)

	for _, req := range d.Requirements {
		info := InterfaceRequirementInfo{
			Name:          req.Name.Name,
			IsPure:        req.IsPure,
			IsTrustedPure: req.IsTrustedPure,
			Span:          req.Loc,
		}

		for _, param := range req.Params {
			c.rejectInlineArraySignatureType(
				param.Type,
				param.Type.Span(),
				fmt.Sprintf(
					"interface requirement parameter %q",
					param.Name.Name,
				),
			)

			info.Params = append(
				info.Params,
				c.typeFromInterfaceRequirementAst(
					scope,
					param.Type,
				),
			)

			info.ParamIsVariadic = append(
				info.ParamIsVariadic,
				param.IsVariadic,
			)
		}

		for _, result := range req.Results {
			c.rejectInlineArraySignatureType(
				result,
				result.Span(),
				"interface requirement result",
			)

			info.Results = append(
				info.Results,
				c.typeFromInterfaceRequirementAst(
					scope,
					result,
				),
			)
		}

		requirements = append(
			requirements,
			info,
		)
	}

	sym.Type.InterfaceRequirements = requirements
}

func (c *Checker) substituteInlineArrayType(
	scope *Scope,
	typ *Type,
	argSubst map[string]ast.GenericArg,
	substituteElem func(*Type) *Type,
) *Type {
	if typ == nil ||
		typ.Kind != TypeInlineArray {
		return InvalidType
	}

	elem := substituteElem(
		typ.Elem,
	)

	lengthExpr := c.substituteGenericExpr(
		typ.InlineLengthExpr,
		argSubst,
	)

	span := source.Span{}

	if lengthExpr != nil {
		span = lengthExpr.Span()
	}

	return c.inlineArrayTypeFromParts(
		scope,
		elem,
		lengthExpr,
		span,
	)
}

func (c *Checker) genericExprReferencesPlaceholder(
	scope *Scope,
	expr ast.Expr,
) bool {
	if expr == nil {
		return false
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(e.Name.Name)
		if sym == nil {
			return false
		}

		switch sym.Kind {
		case SymbolType:
			return sym.Type != nil &&
				sym.Type.Kind == TypeTypeParam

		case SymbolConst:
			/*
				Generic value parameters are represented as constants without
				an originating ConstDecl node.

				Ordinary source constants retain their declaration node and
				can therefore be evaluated normally.
			*/
			return sym.Node == nil

		case SymbolTask:
			/*
				A generic task parameter is represented as a task symbol
				without a source task declaration.
			*/
			if sym.Node == nil {
				return true
			}

			return sym.Type != nil &&
				c.typeContainsGenericPlaceholder(sym.Type)

		default:
			return false
		}

	case *ast.SelectorExpr:
		/*
			A package-qualified constant or type is concrete. For an ordinary
			value selector, only the receiver can carry a generic placeholder.
		*/
		if id, ok := e.Left.(*ast.IdentExpr); ok {
			sym := scope.Lookup(id.Name.Name)
			if sym != nil && sym.Kind == SymbolPackage {
				return false
			}
		}

		return c.genericExprReferencesPlaceholder(
			scope,
			e.Left,
		)

	case *ast.InlineArrayExpr:
		elem := c.typeFromAst(
			scope,
			e.Elem,
		)

		if c.typeContainsGenericPlaceholder(elem) {
			return true
		}

		if c.genericExprReferencesPlaceholder(
			scope,
			e.Length,
		) {
			return true
		}

		for _, value := range e.Values {
			if c.genericExprReferencesPlaceholder(
				scope,
				value,
			) {
				return true
			}
		}

		return false

	case *ast.SpreadExpr:
		return c.genericExprReferencesPlaceholder(
			scope,
			e.Expr,
		)

	case *ast.UnaryExpr:
		return c.genericExprReferencesPlaceholder(
			scope,
			e.Expr,
		)

	case *ast.BinaryExpr:
		return c.genericExprReferencesPlaceholder(
			scope,
			e.Left,
		) ||
			c.genericExprReferencesPlaceholder(
				scope,
				e.Right,
			)

	case *ast.CallExpr:
		if c.genericExprReferencesPlaceholder(
			scope,
			e.Callee,
		) {
			return true
		}

		for _, arg := range e.Args {
			if c.genericExprReferencesPlaceholder(
				scope,
				arg,
			) {
				return true
			}
		}

		return false

	case *ast.GenericExpr:
		if c.genericExprReferencesPlaceholder(
			scope,
			e.Base,
		) {
			return true
		}

		for _, arg := range e.Args {
			switch arg.Kind {
			case ast.GenericArgType:
				typ := c.typeFromAst(
					scope,
					arg.Type,
				)

				if c.typeContainsGenericPlaceholder(typ) {
					return true
				}

			case ast.GenericArgExpr:
				if c.genericExprReferencesPlaceholder(
					scope,
					arg.Expr,
				) {
					return true
				}
			}
		}

		return false

	case *ast.IndexExpr:
		return c.genericExprReferencesPlaceholder(
			scope,
			e.Left,
		) ||
			c.genericExprReferencesPlaceholder(
				scope,
				e.Index,
			)

	case *ast.CompoundLiteralExpr:
		litType := c.typeFromAst(
			scope,
			e.Type,
		)

		if c.typeContainsGenericPlaceholder(litType) {
			return true
		}

		for _, field := range e.Fields {
			if c.genericExprReferencesPlaceholder(
				scope,
				field.Value,
			) {
				return true
			}
		}

		for _, value := range e.Values {
			if c.genericExprReferencesPlaceholder(
				scope,
				value,
			) {
				return true
			}
		}

		return false
	}

	return false
}

func (c *Checker) typeContainsGenericPlaceholder(
	typ *Type,
) bool {
	return c.typeContainsGenericPlaceholderSeen(
		typ,
		map[*Type]bool{},
	)
}

func (c *Checker) typeContainsGenericPlaceholderSeen(
	typ *Type,
	seen map[*Type]bool,
) bool {
	if typ == nil {
		return false
	}

	if seen[typ] {
		return false
	}

	seen[typ] = true

	switch typ.Kind {
	case TypeTypeParam,
		TypeValueParam:
		return true

	case TypePointer,
		TypeVariadic:
		return c.typeContainsGenericPlaceholderSeen(
			typ.Elem,
			seen,
		)

	case TypeInlineArray:
		if typ.InlineLengthSymbolic {
			return true
		}

		return c.typeContainsGenericPlaceholderSeen(
			typ.Elem,
			seen,
		)
	}

	for _, arg := range typ.GenericArguments {
		if c.typeContainsGenericPlaceholderSeen(
			arg.Type,
			seen,
		) {
			return true
		}
	}

	for _, field := range typ.Fields {
		if c.typeContainsGenericPlaceholderSeen(
			field.Type,
			seen,
		) {
			return true
		}
	}

	for _, member := range typ.Members {
		if c.typeContainsGenericPlaceholderSeen(
			member,
			seen,
		) {
			return true
		}
	}

	for _, param := range typ.Params {
		if c.typeContainsGenericPlaceholderSeen(
			param,
			seen,
		) {
			return true
		}
	}

	for _, result := range typ.Results {
		if c.typeContainsGenericPlaceholderSeen(
			result,
			seen,
		) {
			return true
		}
	}

	return false
}

func (c *Checker) genericArgumentsContainPlaceholders(
	scope *Scope,
	args []ast.GenericArg,
) bool {
	for _, arg := range args {
		switch arg.Kind {
		case ast.GenericArgType:
			typ := c.typeFromAst(
				scope,
				arg.Type,
			)

			if c.typeContainsGenericPlaceholder(typ) {
				return true
			}

		case ast.GenericArgExpr:
			if c.genericExprReferencesPlaceholder(
				scope,
				arg.Expr,
			) {
				return true
			}
		}
	}

	return false
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
		valueType := c.checkExpr(scope, d.Value)

		typ := InvalidType

		if valueType != nil && valueType.Kind == TypeNil {
			c.diags.Add(
				d.Value.Span(),
				"nil cannot initialize an untyped constant",
			)
		} else {
			typ = c.defaultType(valueType)
		}

		if sym := scope.LookupLocal(d.Name.Name); sym != nil {
			sym.Type = typ
		}

	case *ast.StructDecl:
	// Already prepared.

	case *ast.EnumDecl:
		// Already prepared and validated.

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

func (c *Checker) bracketCandidateSignatureValid(
	overloadName string,
	taskType *Type,
) bool {
	if taskType == nil || taskType.Kind != TypeTask {
		return false
	}

	if c.taskHasDefaultParameters(taskType) {
		return false
	}

	if taskType.IsVariadic ||
		taskTypeHasVariadicParam(taskType) {
		return false
	}

	switch overloadName {
	case "[]":
		if !taskType.IsPure &&
			!taskType.IsTrustedPure {
			return false
		}

		if len(taskType.Params) != 2 ||
			len(taskType.Results) != 1 {
			return false
		}

	case "[]=":
		if len(taskType.Params) != 3 ||
			len(taskType.Results) != 0 {
			return false
		}

	default:
		return false
	}

	if !c.isOverloadReceiverParameter(taskType.Params[0]) {
		return false
	}

	if len(taskType.Params) < 2 ||
		!c.sameType(taskType.Params[1], IntType) {
		return false
	}

	return true
}

func (c *Checker) isOverloadReceiverParameter(
	t *Type,
) bool {
	if t == nil ||
		t.Kind != TypePointer ||
		t.Elem == nil {
		return false
	}

	return t.Elem.Kind == TypeStruct
}

func (c *Checker) checkBracketOverloadCandidate(
	overloadName string,
	candidate *Symbol,
	taskType *Type,
) {
	if candidate == nil ||
		taskType == nil ||
		taskType.Kind != TypeTask {
		return
	}

	if overloadName == "[]" &&
		!taskType.IsPure &&
		!taskType.IsTrustedPure {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				"bracket operator [] candidate %q must be pure",
				candidate.Name,
			),
		)
	}

	if c.taskHasDefaultParameters(taskType) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				"bracket operator %s candidate %q cannot have default parameters",
				overloadName,
				candidate.Name,
			),
		)
	}

	if taskType.IsVariadic ||
		taskTypeHasVariadicParam(taskType) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				"bracket operator %s candidate %q cannot be variadic",
				overloadName,
				candidate.Name,
			),
		)
	}

	expectedParams := 2
	expectedResults := 1

	if overloadName == "[]=" {
		expectedParams = 3
		expectedResults = 0
	}

	if len(taskType.Params) != expectedParams {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				"bracket operator %s candidate %q must have exactly %d parameters",
				overloadName,
				candidate.Name,
				expectedParams,
			),
		)
	}

	if len(taskType.Results) != expectedResults {
		if expectedResults == 0 {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"bracket assignment operator []= candidate %q must not return a value",
					candidate.Name,
				),
			)
		} else {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"bracket operator [] candidate %q must return exactly 1 value",
					candidate.Name,
				),
			)
		}
	}

	if len(taskType.Params) > 0 &&
		!c.isOverloadReceiverParameter(taskType.Params[0]) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				"first parameter of bracket operator %s candidate %q must be a pointer to a struct",
				overloadName,
				candidate.Name,
			),
		)
	}

	if len(taskType.Params) > 1 &&
		!c.sameType(taskType.Params[1], IntType) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				"second parameter of bracket operator %s candidate %q must have type int",
				overloadName,
				candidate.Name,
			),
		)
	}
}

func (c *Checker) lenCandidateSignatureValid(
	taskType *Type,
) bool {
	if taskType == nil ||
		taskType.Kind != TypeTask {
		return false
	}

	if !taskType.IsPure &&
		!taskType.IsTrustedPure {
		return false
	}

	if c.taskHasDefaultParameters(taskType) {
		return false
	}

	if taskType.IsVariadic ||
		taskTypeHasVariadicParam(taskType) {
		return false
	}

	if len(taskType.Params) != 1 ||
		len(taskType.Results) != 1 {
		return false
	}

	if !c.isOverloadReceiverParameter(
		taskType.Params[0],
	) {
		return false
	}

	// Keep overloaded len consistent with builtin variadic len.
	return c.sameType(
		taskType.Results[0],
		UintType,
	)
}

func (c *Checker) checkLenOverloadCandidate(
	candidate *Symbol,
	taskType *Type,
) {
	if candidate == nil ||
		taskType == nil ||
		taskType.Kind != TypeTask {
		return
	}

	if !taskType.IsPure &&
		!taskType.IsTrustedPure {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`len overload candidate %q must be pure`,
				candidate.Name,
			),
		)
	}

	if c.taskHasDefaultParameters(taskType) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`len overload candidate %q cannot have default parameters`,
				candidate.Name,
			),
		)
	}

	if taskType.IsVariadic ||
		taskTypeHasVariadicParam(taskType) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`len overload candidate %q cannot be variadic`,
				candidate.Name,
			),
		)
	}

	if len(taskType.Params) != 1 {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`len overload candidate %q must have exactly 1 parameter`,
				candidate.Name,
			),
		)
	}

	if len(taskType.Results) != 1 {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`len overload candidate %q must return exactly 1 value`,
				candidate.Name,
			),
		)
	}

	if len(taskType.Params) > 0 &&
		!c.isOverloadReceiverParameter(
			taskType.Params[0],
		) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`first parameter of len overload candidate %q must be a pointer to a struct`,
				candidate.Name,
			),
		)
	}

	if len(taskType.Results) == 1 &&
		!c.sameType(
			taskType.Results[0],
			UintType,
		) {
		c.diags.Add(
			candidate.Span,
			fmt.Sprintf(
				`len overload candidate %q must return uint`,
				candidate.Name,
			),
		)
	}
}

func (c *Checker) checkOverloadDecl(
	scope *Scope,
	d *ast.OverloadDecl,
) {
	sym := scope.LookupLocal(d.Name)
	if sym == nil || sym.Overload == nil {
		return
	}

	info := sym.Overload

	for _, candidate := range info.Candidates {
		taskType := candidate.Type

		if taskType == nil || taskType.Kind != TypeTask {
			continue
		}

		if d.Name == "len" {
			c.checkLenOverloadCandidate(
				candidate,
				taskType,
			)
			continue
		}

		if !info.IsOperator {
			continue
		}

		switch d.Name {
		case "[]", "[]=":
			c.checkBracketOverloadCandidate(
				d.Name,
				candidate,
				taskType,
			)
			continue
		}

		if !taskType.IsPure &&
			!taskType.IsTrustedPure {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"operator overload %q requires pure task candidate %q",
					d.Name,
					candidate.Name,
				),
			)
		}

		if c.taskHasDefaultParameters(taskType) {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"operator overload %q candidate %q cannot have default parameters",
					d.Name,
					candidate.Name,
				),
			)
		}

		if len(taskType.Params) != 2 {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"operator overload %q candidate %q must have exactly 2 parameters",
					d.Name,
					candidate.Name,
				),
			)
		}

		if len(taskType.Results) != 1 {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"operator overload %q candidate %q must return exactly 1 value",
					d.Name,
					candidate.Name,
				),
			)
		}

		if isComparisonOperatorName(d.Name) &&
			len(taskType.Results) == 1 &&
			!c.sameType(taskType.Results[0], BoolType) {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"comparison operator overload %q candidate %q must return bool",
					d.Name,
					candidate.Name,
				),
			)
		}

		if len(taskType.Params) == 2 &&
			c.isBuiltinPrimitiveForOperator(taskType.Params[0]) &&
			c.isBuiltinPrimitiveForOperator(taskType.Params[1]) {
			c.diags.Add(
				candidate.Span,
				fmt.Sprintf(
					"operator overload %q cannot replace built-in primitive operator behavior",
					d.Name,
				),
			)
		}
	}

	c.checkDuplicateOverloadSignatures(info)
}

func (c *Checker) shouldUseLenDispatch(
	scope *Scope,
	name string,
) bool {
	if name != "len" {
		return false
	}

	sym := scope.Lookup(name)

	// No checker symbol means the builtin len name is active.
	if sym == nil {
		return true
	}

	// A local len overload augments builtin variadic len rather than disabling
	// it.
	return sym.Kind == SymbolOverload
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

type overloadGenericParamRef struct {
	Index    int
	Category ast.GenericParamCategory
}

func overloadGenericParamRefs(
	params []ast.GenericParam,
) map[string]overloadGenericParamRef {
	refs := map[string]overloadGenericParamRef{}

	for i, param := range params {
		if param.Name.Name == "" {
			continue
		}

		refs[param.Name.Name] = overloadGenericParamRef{
			Index:    i,
			Category: param.Category,
		}
	}

	return refs
}

func overloadGenericParamRefKey(
	ref overloadGenericParamRef,
) string {
	return fmt.Sprintf(
		"$%d:%d",
		ref.Index,
		int(ref.Category),
	)
}

func overloadTypePlaceholderRef(
	typ *Type,
	refs map[string]overloadGenericParamRef,
) (overloadGenericParamRef, bool) {
	if typ == nil || typ.Name == "" {
		return overloadGenericParamRef{}, false
	}

	ref, ok := refs[typ.Name]
	if !ok {
		return overloadGenericParamRef{}, false
	}

	switch ref.Category {
	case ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion:
		return ref, typ.Kind == TypeTypeParam

	case ast.GenericParamTask:
		return ref, typ.Kind == TypeTask

	case ast.GenericParamInt,
		ast.GenericParamBool,
		ast.GenericParamString,
		ast.GenericParamValue:
		return ref, typ.Kind == TypeValueParam
	}

	return overloadGenericParamRef{}, false
}

func sameGenericOverloadParameterSignature(
	a []ast.GenericParam,
	b []ast.GenericParam,
) bool {
	if len(a) != len(b) {
		return false
	}

	aRefs := overloadGenericParamRefs(a)
	bRefs := overloadGenericParamRefs(b)

	for i := range a {
		if a[i].Category != b[i].Category {
			return false
		}

		// Constraints deliberately do not participate in overload identity.
		//
		// Therefore these are duplicates:
		//
		//     Buffer16 :: task<Size int[Size == 16]>()
		//     Buffer32 :: task<Size int[Size == 32]>()
		//
		// GenericParamValue is different: its declared value type is part of
		// the parameter itself, rather than an additional constraint.
		if a[i].Category != ast.GenericParamValue {
			continue
		}

		aKey := overloadTypeAstSignatureKey(
			a[i].Type,
			aRefs,
		)
		bKey := overloadTypeAstSignatureKey(
			b[i].Type,
			bRefs,
		)

		if aKey != bKey {
			return false
		}
	}

	return true
}

func overloadTypeAstSignatureKey(
	typ ast.Type,
	refs map[string]overloadGenericParamRef,
) string {
	if typ == nil {
		return "<nil>"
	}

	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if ref, ok :=
				refs[t.Parts[0].Name]; ok {
				return overloadGenericParamRefKey(
					ref,
				)
			}
		}

		var parts []string

		for _, part := range t.Parts {
			parts = append(
				parts,
				part.Name,
			)
		}

		return "named:" +
			strings.Join(
				parts,
				".",
			)

	case *ast.InterfaceSelfType:
		return "self"

	case *ast.PointerType:
		return "*" +
			overloadTypeAstSignatureKey(
				t.Elem,
				refs,
			)

	case *ast.InlineArrayType:
		return "inline-array:<" +
			overloadTypeAstSignatureKey(
				t.Elem,
				refs,
			) +
			"," +
			overloadExprSignatureKey(
				t.Length,
				refs,
			) +
			">"

	case *ast.GenericType:
		var args []string

		for _, arg := range t.Args {
			args = append(
				args,
				overloadGenericArgAstSignatureKey(
					arg,
					refs,
				),
			)
		}

		return overloadTypeAstSignatureKey(
			t.Base,
			refs,
		) +
			"<" +
			strings.Join(
				args,
				",",
			) +
			">"
	}

	return "<type>"
}

func (c *Checker) rejectInlineArraySignatureType(
	typ ast.Type,
	span source.Span,
	context string,
) {
	if !typeAstContainsInlineArray(
		typ,
	) {
		return
	}

	c.diags.Add(
		span,
		fmt.Sprintf(
			"@inline_array cannot be used directly as %s; wrap inline storage in a struct instead",
			context,
		),
	)
}

func typeAstContainsInlineArray(
	typ ast.Type,
) bool {
	if typ == nil {
		return false
	}

	switch t := typ.(type) {
	case *ast.InlineArrayType:
		return true

	case *ast.PointerType:
		return typeAstContainsInlineArray(
			t.Elem,
		)

	case *ast.GenericType:
		if typeAstContainsInlineArray(
			t.Base,
		) {
			return true
		}

		for _, arg := range t.Args {
			if arg.Kind != ast.GenericArgType ||
				arg.Type == nil {
				continue
			}

			if typeAstContainsInlineArray(
				arg.Type,
			) {
				return true
			}
		}
	}

	return false
}

func overloadGenericArgAstSignatureKey(
	arg ast.GenericArg,
	refs map[string]overloadGenericParamRef,
) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return "type:" + overloadTypeAstSignatureKey(
			arg.Type,
			refs,
		)

	case ast.GenericArgExpr:
		return "expr:" + overloadExprSignatureKey(
			arg.Expr,
			refs,
		)
	}

	return "<arg>"
}

func overloadExprSignatureKey(
	expr ast.Expr,
	refs map[string]overloadGenericParamRef,
) string {
	if expr == nil {
		return "<nil>"
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if ref, ok := refs[e.Name.Name]; ok {
			return overloadGenericParamRefKey(ref)
		}

		return "id:" + e.Name.Name

	case *ast.SelectorExpr:
		return "select:" +
			overloadExprSignatureKey(e.Left, refs) +
			"." +
			e.Name.Name

	case *ast.IntLitExpr:
		return "int:" + integerLiteralIdentity(
			e.Value,
		)

	case *ast.FloatLitExpr:
		return "float:" + e.Value

	case *ast.StringLitExpr:
		return "string:" + e.Value

	case *ast.CStringLitExpr:
		return "cstring:" + e.Value

	case *ast.CharLitExpr:
		return "char:" + e.Value

	case *ast.BoolLitExpr:
		if e.Value {
			return "bool:true"
		}

		return "bool:false"

	case *ast.NilLitExpr:
		return "nil"

	case *ast.UnaryExpr:
		return "unary:" +
			e.Op.String() +
			overloadExprSignatureKey(e.Expr, refs)

	case *ast.BinaryExpr:
		return "binary:" +
			overloadExprSignatureKey(e.Left, refs) +
			e.Op.String() +
			overloadExprSignatureKey(e.Right, refs)

	case *ast.CallExpr:
		var args []string

		for _, arg := range e.Args {
			args = append(
				args,
				overloadExprSignatureKey(arg, refs),
			)
		}

		return "call:" +
			overloadExprSignatureKey(e.Callee, refs) +
			"(" +
			strings.Join(args, ",") +
			")"

	case *ast.GenericExpr:
		var args []string

		for _, arg := range e.Args {
			args = append(
				args,
				overloadGenericArgAstSignatureKey(
					arg,
					refs,
				),
			)
		}

		return "generic:" +
			overloadExprSignatureKey(e.Base, refs) +
			"<" +
			strings.Join(args, ",") +
			">"

	case *ast.IndexExpr:
		return "index:" +
			overloadExprSignatureKey(e.Left, refs) +
			"[" +
			overloadExprSignatureKey(e.Index, refs) +
			"]"

	case *ast.CompoundLiteralExpr:
		return "literal:" +
			overloadTypeAstSignatureKey(e.Type, refs)
	}

	return exprDisplay(expr)
}

func overloadGenericArgumentInfoSignatureKey(
	arg GenericArgumentInfo,
	refs map[string]overloadGenericParamRef,
) string {
	if arg.Expr != nil {
		return overloadExprSignatureKey(
			arg.Expr,
			refs,
		)
	}

	if arg.Type != nil {
		if ref, ok := overloadTypePlaceholderRef(
			arg.Type,
			refs,
		); ok {
			return overloadGenericParamRefKey(ref)
		}

		return arg.Type.String()
	}

	return arg.Key
}

func sameOverloadSignatureType(
	c *Checker,
	a *Type,
	b *Type,
	aRefs map[string]overloadGenericParamRef,
	bRefs map[string]overloadGenericParamRef,
) bool {
	if a == nil || b == nil {
		return a == b
	}

	aRef, aIsRef := overloadTypePlaceholderRef(
		a,
		aRefs,
	)
	bRef, bIsRef := overloadTypePlaceholderRef(
		b,
		bRefs,
	)

	if aIsRef || bIsRef {
		return aIsRef &&
			bIsRef &&
			aRef.Index == bRef.Index &&
			aRef.Category == bRef.Category
	}

	if a.Kind != b.Kind {
		return false
	}

	switch a.Kind {
	case TypePointer,
		TypeVariadic:
		return sameOverloadSignatureType(
			c,
			a.Elem,
			b.Elem,
			aRefs,
			bRefs,
		)

	case TypeInlineArray:
		if a.InlineLengthKnown !=
			b.InlineLengthKnown {
			return false
		}

		if a.InlineLengthKnown {
			if a.InlineLength != b.InlineLength {
				return false
			}
		} else if a.InlineLengthKey !=
			b.InlineLengthKey {
			return false
		}

		return sameOverloadSignatureType(
			c,
			a.Elem,
			b.Elem,
			aRefs,
			bRefs,
		)

	case TypeStruct,
		TypeInterface:
		if a.GenericBaseName != "" ||
			b.GenericBaseName != "" {
			if a.GenericBaseName == "" ||
				b.GenericBaseName == "" ||
				a.GenericBaseName != b.GenericBaseName ||
				len(a.GenericArguments) !=
					len(b.GenericArguments) {
				return false
			}

			for i := range a.GenericArguments {
				left := a.GenericArguments[i]
				right := b.GenericArguments[i]

				if left.Category !=
					right.Category {
					return false
				}

				switch {
				case isTypeGenericCategory(
					left.Category,
				):
					if !sameOverloadSignatureType(
						c,
						left.Type,
						right.Type,
						aRefs,
						bRefs,
					) {
						return false
					}

				case left.Category ==
					ast.GenericParamTask:
					if !sameOverloadSignatureType(
						c,
						left.Type,
						right.Type,
						aRefs,
						bRefs,
					) {
						return false
					}

				default:
					leftKey :=
						overloadGenericArgumentInfoSignatureKey(
							left,
							aRefs,
						)

					rightKey :=
						overloadGenericArgumentInfoSignatureKey(
							right,
							bRefs,
						)

					if leftKey != rightKey {
						return false
					}
				}
			}

			return true
		}

		return a.Name == b.Name

	case TypeTask:
		if len(a.Params) != len(b.Params) ||
			len(a.Results) != len(b.Results) {
			return false
		}

		for i := range a.Params {
			if !sameOverloadSignatureType(
				c,
				a.Params[i],
				b.Params[i],
				aRefs,
				bRefs,
			) {
				return false
			}
		}

		for i := range a.Results {
			if !sameOverloadSignatureType(
				c,
				a.Results[i],
				b.Results[i],
				aRefs,
				bRefs,
			) {
				return false
			}
		}

		return true

	case TypeDistinct,
		TypeEnum,
		TypeUnion,
		TypeTypeParam,
		TypeValueParam:
		return a.Name == b.Name

	default:
		return c.sameType(
			a,
			b,
		)
	}
}

func sameParamSignature(
	c *Checker,
	a *Type,
	b *Type,
) bool {
	if a == nil || b == nil {
		return false
	}

	if !sameGenericOverloadParameterSignature(
		a.GenericParams,
		b.GenericParams,
	) {
		return false
	}

	if len(a.Params) != len(b.Params) {
		return false
	}

	aRefs := overloadGenericParamRefs(
		a.GenericParams,
	)
	bRefs := overloadGenericParamRefs(
		b.GenericParams,
	)

	for i := range a.Params {
		if !sameOverloadSignatureType(
			c,
			a.Params[i],
			b.Params[i],
			aRefs,
			bRefs,
		) {
			return false
		}
	}

	return true
}

func (c *Checker) checkImplDecl(
	scope *Scope,
	d *ast.ImplDecl,
) {
	_ = scope

	info := c.implByDecl[d]
	if info == nil {
		return
	}

	c.ensureImplChecked(info)
}

func (c *Checker) ensureImplChecked(info *ImplInfo) bool {
	if info == nil {
		return false
	}

	if info.Checked {
		return info.Usable
	}

	if info.Checking {
		c.diags.Add(
			info.Span,
			fmt.Sprintf(
				"cyclic delegated implementation of %s for %s",
				info.Interface.String(),
				info.Target.String(),
			),
		)
		return false
	}

	info.Checking = true
	defer func() {
		info.Checking = false
		info.Checked = true
	}()

	if info.Interface == nil ||
		info.Interface.Kind != TypeInterface ||
		info.Target == nil ||
		info.Target.Kind == TypeInvalid {
		info.Usable = false
		return false
	}

	if len(info.UsingPath) > 0 {
		info.Usable = c.checkDelegatedImplInfo(info)
		return info.Usable
	}

	info.Usable = c.checkManualImplInfo(info)
	return info.Usable
}

func (c *Checker) substituteInterfaceSelf(
	t *Type,
	self *Type,
) *Type {
	if t == nil {
		return InvalidType
	}

	switch t.Kind {
	case TypeInterfaceSelf:
		return self

	case TypePointer:
		return &Type{
			Kind: TypePointer,
			Elem: c.substituteInterfaceSelf(t.Elem, self),
		}

	case TypeVariadic:
		return &Type{
			Kind: TypeVariadic,
			Name: t.Name,
			Elem: c.substituteInterfaceSelf(
				t.Elem,
				self,
			),
		}

	default:
		return t
	}
}

func (c *Checker) checkManualImplInfo(info *ImplInfo) bool {
	valid := true
	usedEntries := map[string]bool{}

	for _, req := range info.Interface.InterfaceRequirements {
		entry := info.Entries[req.Name]
		if entry == nil {
			c.diags.Add(
				info.Span,
				fmt.Sprintf(
					"type %s does not implement %s: missing requirement %q",
					info.Target.String(),
					info.Interface.String(),
					req.Name,
				),
			)
			valid = false
			continue
		}

		usedEntries[req.Name] = true

		var expectedParams []*Type
		for _, param := range req.Params {
			expectedParams = append(
				expectedParams,
				c.substituteInterfaceSelf(
					param,
					info.Target,
				),
			)
		}

		var expectedResults []*Type
		for _, result := range req.Results {
			expectedResults = append(
				expectedResults,
				c.substituteInterfaceSelf(
					result,
					info.Target,
				),
			)
		}

		var actual *Type

		switch {
		case entry.TaskSymbol != nil:
			actual = entry.TaskSymbol.Type

			if taskDecl, ok := entry.TaskSymbol.Node.(*ast.TaskDecl); ok {
				if len(taskDecl.GenericParams) > 0 {
					c.diags.Add(
						taskDecl.Name.Span(),
						"interface requirement implementation cannot introduce task-generic parameters",
					)
					valid = false
				}
			}

		case entry.Alias != nil:
			actual = c.checkExpr(info.Scope, entry.Alias)

		default:
			c.diags.Add(
				entry.Span,
				fmt.Sprintf(
					"impl entry %q has no implementation",
					entry.Name,
				),
			)
			valid = false
			continue
		}

		if (req.IsPure || req.IsTrustedPure) &&
			(actual == nil ||
				(!actual.IsPure && !actual.IsTrustedPure)) {
			c.diags.Add(
				entry.Span,
				fmt.Sprintf(
					"implementation of pure requirement %q must be pure",
					req.Name,
				),
			)
			valid = false
		}

		if !c.taskSignatureMatches(
			actual,
			expectedParams,
			expectedResults,
		) {
			c.diags.Add(
				entry.Span,
				fmt.Sprintf(
					"impl entry %q has wrong signature; expected %s",
					req.Name,
					c.formatTaskSignature(
						req.Name,
						expectedParams,
						expectedResults,
					),
				),
			)
			valid = false
			continue
		}

		if len(actual.ParamIsVariadic) == len(req.ParamIsVariadic) {
			for i := range req.ParamIsVariadic {
				if actual.ParamIsVariadic[i] != req.ParamIsVariadic[i] {
					c.diags.Add(
						entry.Span,
						fmt.Sprintf(
							"impl entry %q has incompatible variadic parameter %d",
							req.Name,
							i+1,
						),
					)
					valid = false
				}
			}
		}

		if entry.TaskSymbol != nil {
			if taskDecl, ok := entry.TaskSymbol.Node.(*ast.TaskDecl); ok {
				c.checkInlineImplTask(
					info.Scope,
					taskDecl,
					actual,
				)
			}
		}
	}

	for name, entry := range info.Entries {
		if usedEntries[name] {
			continue
		}

		c.diags.Add(
			entry.Span,
			fmt.Sprintf(
				"impl entry %q is not a requirement of %s",
				name,
				info.Interface.String(),
			),
		)
		valid = false
	}

	return valid
}

func (c *Checker) checkDelegatedImplInfo(info *ImplInfo) bool {
	selected, steps, ok := c.resolveUsingPathType(
		info.Target,
		info.Decl.UsingPath,
	)

	if !ok {
		return false
	}

	info.UsingSteps = steps

	delegatedTarget := selected
	if delegatedTarget.Kind == TypePointer {
		delegatedTarget = delegatedTarget.Elem
	}

	resolution := c.resolveImplAt(
		info.Interface,
		delegatedTarget,
		info.Span,
		info,
		true,
	)

	switch resolution.Kind {
	case ImplResolutionAmbiguous:
		// resolveImplAt already emitted the correct ambiguity diagnostic.
		// Do not also claim that the selected type does not implement the
		// interface.
		return false

	case ImplResolutionNotFound:
		c.diags.Add(
			info.Span,
			fmt.Sprintf(
				"cannot delegate %s for %s through %s: selected type %s does not implement the interface",
				info.Interface.String(),
				info.Target.String(),
				strings.Join(info.UsingPath, "."),
				delegatedTarget.String(),
			),
		)
		return false

	case ImplResolutionFound:
		info.DelegatesTo = resolution.Resolved.Info
		return true

	default:
		return false
	}
}

func (c *Checker) resolveUsingPathType(
	target *Type,
	path []ast.Ident,
) (*Type, []UsingProjectionStep, bool) {
	current := target
	var steps []UsingProjectionStep

	for _, part := range path {
		throughPointer := false

		if current != nil && current.Kind == TypePointer {
			throughPointer = true
			current = current.Elem
		}

		if current == nil {
			c.diags.Add(
				part.Span(),
				fmt.Sprintf(
					"cannot select delegated field %q on invalid type",
					part.Name,
				),
			)
			return InvalidType, steps, false
		}

		if current.Kind != TypeStruct {
			c.diags.Add(
				part.Span(),
				fmt.Sprintf(
					"cannot select delegated field %q on non-struct type %s",
					part.Name,
					current.String(),
				),
			)
			return InvalidType, steps, false
		}

		var field *FieldInfo
		for i := range current.Fields {
			if current.Fields[i].Name == part.Name {
				field = &current.Fields[i]
				break
			}
		}

		if field == nil {
			c.diags.Add(
				part.Span(),
				fmt.Sprintf(
					"type %s has no field %q in using path",
					current.String(),
					part.Name,
				),
			)
			return InvalidType, steps, false
		}

		steps = append(steps, UsingProjectionStep{
			Name:           part.Name,
			Container:      current,
			FieldType:      field.Type,
			ThroughPointer: throughPointer,
			Span:           part.Span(),
		})

		current = field.Type
	}

	return current, steps, true
}

func genericParamKinds(
	params []ast.GenericParam,
) map[string]ast.GenericParamCategory {
	out := map[string]ast.GenericParamCategory{}

	for _, param := range params {
		out[param.Name.Name] = param.Category
	}

	return out
}

func isTypeGenericCategory(category ast.GenericParamCategory) bool {
	switch category {
	case ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion:
		return true

	default:
		return false
	}
}

func isValueGenericCategory(category ast.GenericParamCategory) bool {
	switch category {
	case ast.GenericParamInt,
		ast.GenericParamBool,
		ast.GenericParamString,
		ast.GenericParamValue:
		return true

	default:
		return false
	}
}

func (c *Checker) matchImplType(
	pattern *Type,
	actual *Type,
	paramKinds map[string]ast.GenericParamCategory,
	match *ImplMatch,
) bool {
	if pattern == nil || actual == nil {
		return false
	}

	if pattern.Kind == TypeInvalid || actual.Kind == TypeInvalid {
		return false
	}

	if pattern.Kind == TypeTypeParam {
		category, generic := paramKinds[pattern.Name]
		if generic && isTypeGenericCategory(category) {
			if existing := match.Types[pattern.Name]; existing != nil {
				return c.sameType(existing, actual)
			}

			match.Types[pattern.Name] = actual
			return true
		}
	}

	if pattern.Kind != actual.Kind {
		return false
	}

	switch pattern.Kind {
	case TypePointer, TypeVariadic:
		return c.matchImplType(
			pattern.Elem,
			actual.Elem,
			paramKinds,
			match,
		)

	case TypeStruct, TypeInterface:
		if pattern.GenericBaseName != "" ||
			actual.GenericBaseName != "" {
			if pattern.GenericBaseName == "" ||
				actual.GenericBaseName == "" ||
				pattern.GenericBaseName != actual.GenericBaseName ||
				len(pattern.GenericArguments) != len(actual.GenericArguments) {
				return false
			}

			for i := range pattern.GenericArguments {
				if !c.matchImplGenericArgument(
					pattern.GenericArguments[i],
					actual.GenericArguments[i],
					paramKinds,
					match,
				) {
					return false
				}
			}

			return true
		}

		return pattern.Name == actual.Name

	case TypeDistinct,
		TypeEnum,
		TypeUnion,
		TypeTypeParam,
		TypeValueParam:
		return pattern.Name == actual.Name

	default:
		return c.sameType(pattern, actual)
	}
}

func (c *Checker) matchImplGenericArgument(
	pattern GenericArgumentInfo,
	actual GenericArgumentInfo,
	paramKinds map[string]ast.GenericParamCategory,
	match *ImplMatch,
) bool {
	if isTypeGenericCategory(pattern.Category) {
		return c.matchImplType(
			pattern.Type,
			actual.Type,
			paramKinds,
			match,
		)
	}

	if isValueGenericCategory(pattern.Category) {
		if id, ok := pattern.Expr.(*ast.IdentExpr); ok {
			category, generic := paramKinds[id.Name.Name]
			if generic && isValueGenericCategory(category) {
				if existing, found := match.Values[id.Name.Name]; found {
					return existing.Key == actual.Key
				}

				match.Values[id.Name.Name] = actual
				return true
			}
		}

		return pattern.Key == actual.Key
	}

	if pattern.Category == ast.GenericParamTask {
		return c.sameType(pattern.Type, actual.Type)
	}

	return pattern.Key == actual.Key
}

func receiverGenericMatchComplete(
	params []ast.GenericParam,
	match ImplMatch,
) bool {
	for _, param := range params {
		name := param.Name.Name
		if name == "" {
			continue
		}

		switch {
		case isTypeGenericCategory(param.Category):
			if match.Types[name] == nil {
				return false
			}

		case isValueGenericCategory(param.Category):
			if _, ok := match.Values[name]; !ok {
				return false
			}

		default:
			// Task-generic parameters cannot be inferred from the bracket
			// receiver.
			return false
		}
	}

	return true
}

func receiverGenericArguments(
	params []ast.GenericParam,
	match ImplMatch,
) ([]GenericArgumentInfo, bool) {
	args := make(
		[]GenericArgumentInfo,
		0,
		len(params),
	)

	for _, param := range params {
		name := param.Name.Name
		if name == "" {
			return nil, false
		}

		switch {
		case isTypeGenericCategory(param.Category):
			typ := match.Types[name]
			if typ == nil {
				return nil, false
			}

			args = append(args, GenericArgumentInfo{
				Category: param.Category,
				Type:     typ,
				Key:      typ.String(),
			})

		case isValueGenericCategory(param.Category):
			value, ok := match.Values[name]
			if !ok {
				return nil, false
			}

			value.Category = param.Category

			if value.Key == "" {
				value.Key = genericArgumentInfoDisplay(value)
			}

			args = append(args, value)

		default:
			// Task-generic parameters cannot be inferred solely from a
			// receiver type.
			return nil, false
		}
	}

	return args, true
}

func genericArgumentInfoDisplay(
	arg GenericArgumentInfo,
) string {
	switch {
	case isTypeGenericCategory(arg.Category):
		if arg.Type != nil {
			return arg.Type.String()
		}

	case arg.Category == ast.GenericParamTask:
		if arg.Type != nil {
			return arg.Type.String()
		}

	default:
		if arg.Key != "" {
			return arg.Key
		}

		if arg.Expr != nil {
			return exprDisplay(arg.Expr)
		}
	}

	return "<invalid>"
}

func genericTypeNameFromArgumentInfo(
	base string,
	args []GenericArgumentInfo,
) string {
	parts := make([]string, 0, len(args))

	for _, arg := range args {
		parts = append(
			parts,
			genericArgumentInfoDisplay(arg),
		)
	}

	return fmt.Sprintf(
		"%s<%s>",
		base,
		strings.Join(parts, ", "),
	)
}

func (c *Checker) substituteReceiverMatchType(
	t *Type,
	match ImplMatch,
	seen map[*Type]*Type,
) *Type {
	if t == nil {
		return nil
	}

	if t.Kind == TypeTypeParam {
		if replacement := match.Types[t.Name]; replacement != nil {
			return replacement
		}

		return t
	}

	if cached := seen[t]; cached != nil {
		return cached
	}

	out := *t
	seen[t] = &out

	out.Elem = c.substituteReceiverMatchType(
		t.Elem,
		match,
		seen,
	)
	out.Underlying = c.substituteReceiverMatchType(
		t.Underlying,
		match,
		seen,
	)

	out.GenericArguments = nil

	for _, arg := range t.GenericArguments {
		cloned := arg

		if isValueGenericCategory(cloned.Category) {
			if id, ok := cloned.Expr.(*ast.IdentExpr); ok {
				if replacement, found :=
					match.Values[id.Name.Name]; found {
					cloned = replacement
				}
			}
		}

		cloned.Type = c.substituteReceiverMatchType(
			cloned.Type,
			match,
			seen,
		)

		out.GenericArguments = append(
			out.GenericArguments,
			cloned,
		)
	}

	if out.GenericBaseName != "" &&
		len(out.GenericArguments) > 0 {
		out.Name = genericTypeNameFromArgumentInfo(
			out.GenericBaseName,
			out.GenericArguments,
		)
	}

	out.Fields = nil
	for _, field := range t.Fields {
		out.Fields = append(
			out.Fields,
			FieldInfo{
				Name: field.Name,
				Type: c.substituteReceiverMatchType(
					field.Type,
					match,
					seen,
				),
				TypeAst: field.TypeAst,
				Span:    field.Span,
			},
		)
	}

	out.Members = nil
	for _, member := range t.Members {
		out.Members = append(
			out.Members,
			c.substituteReceiverMatchType(
				member,
				match,
				seen,
			),
		)
	}

	out.InterfaceRequirements = nil
	for _, req := range t.InterfaceRequirements {
		cloned := InterfaceRequirementInfo{
			Name: req.Name,
			ParamIsVariadic: append(
				[]bool(nil),
				req.ParamIsVariadic...,
			),
			IsPure:        req.IsPure,
			IsTrustedPure: req.IsTrustedPure,
			Span:          req.Span,
		}

		for _, param := range req.Params {
			cloned.Params = append(
				cloned.Params,
				c.substituteReceiverMatchType(
					param,
					match,
					seen,
				),
			)
		}

		for _, result := range req.Results {
			cloned.Results = append(
				cloned.Results,
				c.substituteReceiverMatchType(
					result,
					match,
					seen,
				),
			)
		}

		out.InterfaceRequirements = append(
			out.InterfaceRequirements,
			cloned,
		)
	}

	out.Implements = nil
	for _, iface := range t.Implements {
		out.Implements = append(
			out.Implements,
			c.substituteReceiverMatchType(
				iface,
				match,
				seen,
			),
		)
	}

	out.Params = nil
	for _, param := range t.Params {
		out.Params = append(
			out.Params,
			c.substituteReceiverMatchType(
				param,
				match,
				seen,
			),
		)
	}

	out.Results = nil
	for _, result := range t.Results {
		out.Results = append(
			out.Results,
			c.substituteReceiverMatchType(
				result,
				match,
				seen,
			),
		)
	}

	return &out
}

func (c *Checker) receiverCandidateTaskType(
	candidate *Symbol,
	argTypes []*Type,
) (*Type, []GenericArgumentInfo, int, bool) {
	if candidate == nil ||
		candidate.Type == nil ||
		candidate.Type.Kind != TypeTask {
		return nil, nil, 0, false
	}

	taskType := candidate.Type

	if len(taskType.GenericParams) == 0 {
		score, ok := c.callScore(
			taskType,
			argTypes,
		)

		return taskType, nil, score, ok
	}

	if len(taskType.Params) != len(argTypes) ||
		len(taskType.Params) == 0 {
		return nil, nil, 0, false
	}

	match := ImplMatch{
		Types:  map[string]*Type{},
		Values: map[string]GenericArgumentInfo{},
	}

	kinds := genericParamKinds(
		taskType.GenericParams,
	)

	// Receiver-owned overloads infer generic arguments only from the first
	// receiver parameter. Other arguments validate the selected
	// specialization but do not select another specialization.
	if !c.matchImplType(
		taskType.Params[0],
		argTypes[0],
		kinds,
		&match,
	) {
		return nil, nil, 0, false
	}

	if !receiverGenericMatchComplete(
		taskType.GenericParams,
		match,
	) {
		return nil, nil, 0, false
	}

	genericArgs, ok := receiverGenericArguments(
		taskType.GenericParams,
		match,
	)
	if !ok {
		return nil, nil, 0, false
	}

	specialized := c.substituteReceiverMatchType(
		taskType,
		match,
		map[*Type]*Type{},
	)

	specialized.GenericParams = nil
	specialized.Name = genericTypeNameFromArgumentInfo(
		taskType.Name,
		genericArgs,
	)

	score, ok := c.callScore(
		specialized,
		argTypes,
	)
	if !ok {
		return nil, nil, 0, false
	}

	return specialized, genericArgs, score, true
}

func (c *Checker) resolveReceiverOverloadAt(
	scope *Scope,
	name string,
	argTypes []*Type,
	validCandidate func(*Type) bool,
) receiverOverloadResolution {
	lookups := c.overloadLookups(
		scope,
		name,
		argTypes,
	)

	best := receiverOverloadResolution{
		Score:       1 << 30,
		HadOverload: len(lookups) > 0,
	}

	for _, lookup := range lookups {
		c.ensureOverloadSymbolPrepared(
			lookup.Scope,
			lookup.Symbol,
		)

		if lookup.Symbol == nil ||
			lookup.Symbol.Overload == nil {
			continue
		}

		for _, candidate := range lookup.Symbol.Overload.Candidates {
			if candidate == nil ||
				candidate.Type == nil ||
				candidate.Type.Kind != TypeTask {
				continue
			}

			if validCandidate != nil &&
				!validCandidate(candidate.Type) {
				continue
			}

			taskType, genericArgs, score, ok :=
				c.receiverCandidateTaskType(
					candidate,
					argTypes,
				)
			if !ok {
				continue
			}

			if !best.Matched || score < best.Score {
				best = receiverOverloadResolution{
					Candidate:        candidate,
					TaskType:         taskType,
					GenericArguments: genericArgs,
					PackageName:      lookup.PackageName,
					Score:            score,
					Matched:          true,
					HadOverload:      true,
				}
				continue
			}

			if score == best.Score {
				best.Ambiguous = true
			}
		}
	}

	return best
}

func (c *Checker) resolveBracketOverloadAt(
	scope *Scope,
	name string,
	argTypes []*Type,
) receiverOverloadResolution {
	return c.resolveReceiverOverloadAt(
		scope,
		name,
		argTypes,
		func(taskType *Type) bool {
			return c.bracketCandidateSignatureValid(
				name,
				taskType,
			)
		},
	)
}

func (c *Checker) implPatternMatches(
	info *ImplInfo,
	iface *Type,
	target *Type,
) bool {
	if info == nil {
		return false
	}

	match := ImplMatch{
		Types:  map[string]*Type{},
		Values: map[string]GenericArgumentInfo{},
	}

	kinds := genericParamKinds(info.GenericParams)

	if !c.matchImplType(
		info.Interface,
		iface,
		kinds,
		&match,
	) {
		return false
	}

	return c.matchImplType(
		info.Target,
		target,
		kinds,
		&match,
	)
}

func (c *Checker) resolveImplAt(
	iface *Type,
	target *Type,
	span source.Span,
	exclude *ImplInfo,
	diagnose bool,
) ImplResolution {
	var matches []*ResolvedImpl

	for _, info := range c.impls {
		if info == nil || info == exclude {
			continue
		}

		match := ImplMatch{
			Types:  map[string]*Type{},
			Values: map[string]GenericArgumentInfo{},
		}

		kinds := genericParamKinds(info.GenericParams)

		// Match before checking the implementation. This prevents unrelated
		// delegated implementations from being recursively checked and
		// incorrectly diagnosed as cycles.
		if !c.matchImplType(
			info.Interface,
			iface,
			kinds,
			&match,
		) {
			continue
		}

		if !c.matchImplType(
			info.Target,
			target,
			kinds,
			&match,
		) {
			continue
		}

		if info.Checking {
			// Because this implementation matches the requested pair,
			// encountering it while it is active is a genuine delegation
			// cycle.
			c.ensureImplChecked(info)
			continue
		}

		if !c.ensureImplChecked(info) {
			continue
		}

		matches = append(matches, &ResolvedImpl{
			Info:      info,
			Interface: iface,
			Target:    target,
			Match:     match,
		})
	}

	switch len(matches) {
	case 0:
		return ImplResolution{
			Kind: ImplResolutionNotFound,
		}

	case 1:
		return ImplResolution{
			Kind:     ImplResolutionFound,
			Resolved: matches[0],
			Matches:  matches,
		}

	default:
		if diagnose {
			c.diags.Add(
				span,
				fmt.Sprintf(
					"ambiguous implementation of %s for %s",
					iface.String(),
					target.String(),
				),
			)
		}

		return ImplResolution{
			Kind:    ImplResolutionAmbiguous,
			Matches: matches,
		}
	}
}

func implGenericParamKindsForCGen(
	params []ast.GenericParam,
) map[string]ast.GenericParamCategory {
	out := map[string]ast.GenericParamCategory{}

	for _, param := range params {
		if param.Name.Name == "" {
			continue
		}

		out[param.Name.Name] = param.Category
	}

	return out
}

func isImplTypeGenericCategoryForCGen(
	category ast.GenericParamCategory,
) bool {
	switch category {
	case ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion:
		return true

	default:
		return false
	}
}

func implTemplateSubstCompleteForCGen(
	params []ast.GenericParam,
	subst map[string]ast.GenericArg,
) bool {
	for _, param := range params {
		if param.Name.Name == "" {
			continue
		}

		if _, ok := subst[param.Name.Name]; !ok {
			return false
		}
	}

	return true
}

func (c *Checker) implMayParticipate(info *ImplInfo) bool {
	if info == nil {
		return false
	}

	if info.Checked {
		return info.Usable
	}

	if info.Checking {
		// The currently checking implementation must not resolve itself.
		return false
	}

	return c.ensureImplChecked(info)
}

func (c *Checker) checkInlineImplTask(
	parent *Scope,
	d *ast.TaskDecl,
	taskType *Type,
) {
	if d == nil || taskType == nil {
		return
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
	}

	oldResults := c.currentResults
	oldLoopDepth := c.currentLoopDepth
	oldDeferDepth := c.currentDeferDepth

	// An inline impl task has its own return, loop, and defer contexts.
	c.currentResults = taskType.Results
	c.currentLoopDepth = 0
	c.currentDeferDepth = 0

	c.checkBlockInScope(
		taskScope,
		d.Body,
		false,
	)

	c.currentResults = oldResults
	c.currentLoopDepth = oldLoopDepth
	c.currentDeferDepth = oldDeferDepth
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

func (c *Checker) typeImplementsInterface(
	concreteType *Type,
	ifaceType *Type,
) bool {
	if concreteType == nil || ifaceType == nil {
		return false
	}

	if concreteType.Kind == TypeTypeParam {
		for _, implemented := range concreteType.Implements {
			if c.sameType(implemented, ifaceType) {
				return true
			}
		}

		return false
	}

	resolution := c.resolveImplAt(
		ifaceType,
		concreteType,
		source.Span{},
		nil,
		false,
	)

	return resolution.Found()
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

func (c *Checker) checkInterfaceDecl(
	scope *Scope,
	d *ast.InterfaceDecl,
) {
	_ = scope
	seenRequirements := map[string]source.Span{}

	for _, req := range d.Requirements {
		if req == nil {
			continue
		}

		if previous, exists := seenRequirements[req.Name.Name]; exists {
			c.diags.Add(
				req.Name.Span(),
				fmt.Sprintf(
					"duplicate interface requirement %q, previous requirement at %s",
					req.Name.Name,
					previous.String(),
				),
			)
		} else {
			seenRequirements[req.Name.Name] = req.Name.Span()
		}

		if len(req.Params) == 0 {
			c.diags.Add(
				req.Name.Span(),
				fmt.Sprintf(
					"interface requirement %q must have a first receiver parameter of type *self",
					req.Name.Name,
				),
			)
			continue
		}

		if !isInterfaceSelfPointerAst(req.Params[0].Type) {
			c.diags.Add(
				req.Params[0].Type.Span(),
				fmt.Sprintf(
					"first parameter of interface requirement %q must have type *self",
					req.Name.Name,
				),
			)
		}

		if req.Params[0].IsVariadic {
			c.diags.Add(
				req.Params[0].Name.Span(),
				"interface receiver parameter cannot be variadic",
			)
		}

		for i, param := range req.Params {
			if param.IsVariadic {
				c.diags.Add(
					param.Name.Span(),
					fmt.Sprintf(
						"interface requirement %q cannot be variadic yet",
						req.Name.Name,
					),
				)
			}

			if param.HasDefault {
				c.diags.Add(
					param.Name.Span(),
					fmt.Sprintf(
						"interface requirement parameter %q cannot have a default value",
						param.Name.Name,
					),
				)
			}

			if i > 0 && typeAstContainsInterfaceSelf(param.Type) {
				c.diags.Add(
					param.Type.Span(),
					`"self" may currently appear only in the first interface receiver parameter`,
				)
			}
		}

		for _, result := range req.Results {
			if typeAstContainsInterfaceSelf(result) {
				c.diags.Add(
					result.Span(),
					`interface requirement results cannot currently contain "self"`,
				)
			}
		}
	}
}

func isInterfaceSelfPointerAst(t ast.Type) bool {
	ptr, ok := t.(*ast.PointerType)
	if !ok {
		return false
	}

	_, ok = ptr.Elem.(*ast.InterfaceSelfType)
	return ok
}

func typeAstContainsInterfaceSelf(
	t ast.Type,
) bool {
	switch x := t.(type) {
	case *ast.InterfaceSelfType:
		return true

	case *ast.PointerType:
		return typeAstContainsInterfaceSelf(
			x.Elem,
		)

	case *ast.InlineArrayType:
		return typeAstContainsInterfaceSelf(
			x.Elem,
		)

	case *ast.GenericType:
		if typeAstContainsInterfaceSelf(
			x.Base,
		) {
			return true
		}

		for _, arg := range x.Args {
			if arg.Kind ==
				ast.GenericArgType &&
				arg.Type != nil &&
				typeAstContainsInterfaceSelf(
					arg.Type,
				) {
				return true
			}
		}
	}

	return false
}

func (c *Checker) prepareImplDecl(
	parent *Scope,
	d *ast.ImplDecl,
) {
	if d == nil {
		return
	}

	implScope := c.scopeWithGenericParams(
		parent,
		d.GenericParams,
	)

	iface := c.typeFromAst(implScope, d.Interface)
	target := c.typeFromAst(implScope, d.Target)

	if iface.Kind != TypeInterface &&
		iface.Kind != TypeInvalid {
		c.diags.Add(
			d.Interface.Span(),
			fmt.Sprintf("%s is not an interface", iface.String()),
		)
	}

	if target.Kind == TypeInterface {
		c.diags.Add(
			d.Target.Span(),
			"interface implementation target cannot itself be an interface",
		)
	}

	if iface.Kind == TypeInterface &&
		target.Kind != TypeInvalid &&
		target.Kind != TypeInterface &&
		!currentPackageOwnsInterface(iface) &&
		!currentPackageOwnsImplTarget(target) {
		c.diags.Add(
			d.Span(),
			fmt.Sprintf(
				"orphan impl is not allowed: current package owns neither interface %s nor target type %s",
				iface.String(),
				target.String(),
			),
		)
	}

	info := &ImplInfo{
		Decl:          d,
		GenericParams: append([]ast.GenericParam(nil), d.GenericParams...),
		Interface:     iface,
		Target:        target,
		Entries:       map[string]*ImplEntryInfo{},
		Scope:         implScope,
		Span:          d.Span(),
	}

	for _, part := range d.UsingPath {
		info.UsingPath = append(info.UsingPath, part.Name)
	}

	for i := range d.Entries {
		entry := &d.Entries[i]

		entryInfo := &ImplEntryInfo{
			Name:  entry.Name.Name,
			Alias: entry.Alias,
			Span:  entry.Span(),
		}

		if entry.Task != nil {
			taskType := c.taskTypeFromDecl(
				implScope,
				entry.Task,
			)

			entryInfo.TaskSymbol = &Symbol{
				Name: entry.Name.Name,
				Kind: SymbolTask,
				Type: taskType,
				Span: entry.Name.Span(),
				Node: entry.Task,
			}
		}

		if _, exists := info.Entries[entry.Name.Name]; exists {
			c.diags.Add(
				entry.Name.Span(),
				fmt.Sprintf(
					"duplicate impl entry %q",
					entry.Name.Name,
				),
			)
			continue
		}

		info.Entries[entry.Name.Name] = entryInfo
	}

	parent.Impls = append(parent.Impls, info)
	c.impls = append(c.impls, info)
	c.implByDecl[d] = info
}

func (c *Checker) checkTaskDecl(
	parent *Scope,
	d *ast.TaskDecl,
) {
	taskSym := parent.LookupLocal(d.Name.Name)
	if taskSym == nil {
		return
	}

	taskType := taskSym.Type

	if taskType == nil ||
		taskType.Kind != TypeTask {
		taskType = c.taskTypeFromDecl(
			parent,
			d,
		)
		taskSym.Type = taskType
	}

	genericScope := c.scopeWithGenericParams(
		parent,
		d.GenericParams,
	)

	c.checkTaskDefaultParameters(
		genericScope,
		d,
		taskType,
	)

	if d.IsExtern {
		if d.IsPure && !d.IsTrustedPure {
			c.diags.Add(
				d.Name.Span(),
				"extern task cannot be marked pure; use @trusted_pure extern(...) if this C function is safe to treat as pure",
			)
		}

		if d.Body != nil {
			c.diags.Add(
				d.Body.Span(),
				fmt.Sprintf(
					"extern task %q cannot have a body",
					d.Name.Name,
				),
			)
		}

		return
	}

	if d.IsIntrinsic {
		if d.Body != nil {
			c.diags.Add(
				d.Body.Span(),
				fmt.Sprintf(
					"intrinsic task %q cannot have a body",
					d.Name.Name,
				),
			)
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
	oldLoopDepth := c.currentLoopDepth
	oldDeferDepth := c.currentDeferDepth

	// Every task starts independent return, loop, and defer contexts. This
	// matters when a nested task declaration appears inside a loop or deferred
	// block.
	c.currentResults = taskType.Results
	c.currentLoopDepth = 0
	c.currentDeferDepth = 0

	c.checkBlockInScope(
		taskScope,
		d.Body,
		false,
	)

	c.currentResults = oldResults
	c.currentLoopDepth = oldLoopDepth
	c.currentDeferDepth = oldDeferDepth
}

func (c *Checker) importPackageImpls() {
	for packageName, pkg := range c.packages {
		if pkg == nil {
			continue
		}

		scope := c.scopeForPackageInfo(packageName, pkg)

		for _, exported := range pkg.Impls {
			if exported == nil {
				continue
			}

			info := c.importedImplInfo(
				packageName,
				exported,
			)
			info.Scope = scope
			info.Checked = true
			info.Usable = true

			c.impls = append(c.impls, info)
		}
	}
}

func (c *Checker) importedImplInfo(
	packageName string,
	info *ImplInfo,
) *ImplInfo {
	out := *info

	out.PackageName = packageName
	out.Scope = nil
	out.DelegatesTo = nil
	out.UsingSteps = nil
	out.Checking = false

	out.Interface = c.qualifyImportedTypeForPackage(
		packageName,
		info.Interface,
	)
	out.Target = c.qualifyImportedTypeForPackage(
		packageName,
		info.Target,
	)

	out.GenericParams = append(
		[]ast.GenericParam(nil),
		info.GenericParams...,
	)
	out.UsingPath = append(
		[]string(nil),
		info.UsingPath...,
	)

	out.Entries = map[string]*ImplEntryInfo{}

	for name, entry := range info.Entries {
		if entry == nil {
			continue
		}

		cloned := *entry

		if entry.TaskSymbol != nil {
			cloned.TaskSymbol = c.importedPackageMemberSymbol(
				packageName,
				entry.TaskSymbol,
			)
		}

		out.Entries[name] = &cloned
	}

	return &out
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

	case TypeInlineArray:
		return c.substituteInlineArrayType(
			scope,
			typ,
			argSubst,
			func(elem *Type) *Type {
				return c.substituteImportedGenericSignatureType(
					scope,
					packageName,
					elem,
					typeSubst,
					argSubst,
				)
			},
		)

	case TypePointer:
		return &Type{
			Kind: TypePointer,
			Elem: c.substituteImportedGenericSignatureType(scope, packageName, typ.Elem, typeSubst, argSubst),
		}

	case TypeInterfaceSelf:
		return typ

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
		out.GenericArguments = nil

		if typ.Name != "" {
			out.Name = c.substitutedGenericDisplayName(
				typ.Name,
				typeSubst,
			)

			if packageName != "" &&
				!strings.Contains(out.Name, ".") {
				out.Name = packageName + "." + out.Name
			}
		}

		if typ.GenericBaseName != "" {
			out.GenericBaseName = c.substitutedGenericDisplayName(
				typ.GenericBaseName,
				typeSubst,
			)

			if packageName != "" &&
				!strings.Contains(out.GenericBaseName, ".") {
				out.GenericBaseName =
					packageName + "." + out.GenericBaseName
			}
		}

		for _, arg := range typ.GenericArguments {
			cloned := arg

			if cloned.Type != nil {
				cloned.Type =
					c.substituteImportedGenericSignatureType(
						scope,
						packageName,
						cloned.Type,
						typeSubst,
						argSubst,
					)
			}

			if cloned.Expr != nil {
				cloned.Expr = c.substituteGenericExpr(
					cloned.Expr,
					argSubst,
				)
				cloned.Key = exprDisplay(cloned.Expr)
			} else if cloned.Type != nil {
				cloned.Key = cloned.Type.String()
			}

			out.GenericArguments = append(
				out.GenericArguments,
				cloned,
			)
		}

		for _, field := range typ.Fields {
			out.Fields = append(out.Fields, FieldInfo{
				Name: field.Name,
				Type: c.substituteImportedGenericSignatureType(
					scope,
					packageName,
					field.Type,
					typeSubst,
					argSubst,
				),
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
				Name:            req.Name,
				ParamIsVariadic: append([]bool(nil), req.ParamIsVariadic...),
				IsPure:          req.IsPure,
				IsTrustedPure:   req.IsTrustedPure,
				Span:            req.Span,
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

	case TypeInlineArray:
		return c.substituteInlineArrayType(
			scope,
			typ,
			argSubst,
			func(elem *Type) *Type {
				return c.substituteGenericSignatureType(
					scope,
					elem,
					typeSubst,
					argSubst,
				)
			},
		)

	case TypeInterfaceSelf:
		return typ

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
		out.GenericArguments = nil

		if typ.Name != "" {
			out.Name = c.substitutedGenericDisplayName(
				typ.Name,
				typeSubst,
			)
		}

		if typ.GenericBaseName != "" {
			out.GenericBaseName = c.substitutedGenericDisplayName(
				typ.GenericBaseName,
				typeSubst,
			)
		}

		for _, arg := range typ.GenericArguments {
			cloned := arg

			if cloned.Type != nil {
				cloned.Type = c.substituteGenericSignatureType(
					scope,
					cloned.Type,
					typeSubst,
					argSubst,
				)
			}

			if cloned.Expr != nil {
				cloned.Expr = c.substituteGenericExpr(
					cloned.Expr,
					argSubst,
				)
				cloned.Key = exprDisplay(cloned.Expr)
			} else if cloned.Type != nil {
				cloned.Key = cloned.Type.String()
			}

			out.GenericArguments = append(
				out.GenericArguments,
				cloned,
			)
		}

		for _, field := range typ.Fields {
			out.Fields = append(out.Fields, FieldInfo{
				Name: field.Name,
				Type: c.substituteGenericSignatureType(
					scope,
					field.Type,
					typeSubst,
					argSubst,
				),
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
				Name:            req.Name,
				ParamIsVariadic: append([]bool(nil), req.ParamIsVariadic...),
				IsPure:          req.IsPure,
				IsTrustedPure:   req.IsTrustedPure,
				Span:            req.Span,
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

func (c *Checker) taskTypeFromDecl(
	scope *Scope,
	d *ast.TaskDecl,
) *Type {
	genericScope := c.scopeWithGenericParams(
		scope,
		d.GenericParams,
	)

	var params []*Type
	var paramDefaults []ast.Expr
	var paramHasDefault []bool
	var paramIsVariadic []bool

	requiredParams := len(d.Params)
	isVariadic := false

	for i, param := range d.Params {
		c.rejectInlineArraySignatureType(
			param.Type,
			param.Type.Span(),
			fmt.Sprintf(
				"task parameter %q",
				param.Name.Name,
			),
		)

		params = append(
			params,
			c.typeFromAst(
				genericScope,
				param.Type,
			),
		)

		paramDefaults = append(
			paramDefaults,
			param.Default,
		)

		paramHasDefault = append(
			paramHasDefault,
			param.HasDefault,
		)

		paramIsVariadic = append(
			paramIsVariadic,
			param.IsVariadic,
		)

		if param.IsVariadic {
			isVariadic = true

			if requiredParams ==
				len(d.Params) {
				requiredParams = i
			}
		}

		if param.HasDefault &&
			requiredParams == len(d.Params) {
			requiredParams = i
		}
	}

	var results []*Type

	for _, result := range d.Results {
		c.rejectInlineArraySignatureType(
			result,
			result.Span(),
			"task result",
		)

		results = append(
			results,
			c.typeFromAst(
				genericScope,
				result,
			),
		)
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

func (c *Checker) checkStmt(
	scope *Scope,
	stmt ast.Stmt,
) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		c.declareDecl(
			scope,
			s.Decl,
		)
		c.prepareDecl(
			scope,
			s.Decl,
		)
		c.checkDecl(
			scope,
			s.Decl,
		)

	case *ast.BlockStmt:
		c.checkBlockInScope(
			scope,
			s,
			true,
		)

	case *ast.ReturnStmt:
		c.checkReturnStmt(
			scope,
			s,
		)

	case *ast.BreakStmt:
		if c.currentLoopDepth == 0 {
			c.diags.Add(
				s.Span(),
				"break is only valid inside a for loop",
			)
		}

	case *ast.ContinueStmt:
		if c.currentLoopDepth == 0 {
			c.diags.Add(
				s.Span(),
				"continue is only valid inside a for loop",
			)
		}

	case *ast.DeferStmt:
		c.checkDeferStmt(
			scope,
			s,
		)

	case *ast.SealStmt:
		c.checkExpr(
			scope,
			s.Target,
		)

	case *ast.MultiVarDeclStmt:
		c.checkMultiVarDeclStmt(
			scope,
			s,
		)

	case *ast.ExprStmt:
		c.checkExpr(
			scope,
			s.Expr,
		)

	case *ast.AssignStmt:
		c.checkAssignStmt(
			scope,
			s,
		)

	case *ast.VarDeclStmt:
		c.checkVarDeclStmt(
			scope,
			s,
		)

	case *ast.IfStmt:
		cond := c.checkExpr(
			scope,
			s.Cond,
		)

		c.checkBoolCondition(
			cond,
			s.Cond.Span(),
			"if condition must be bool",
		)

		c.checkBlockInScope(
			scope,
			s.Then,
			true,
		)

		if s.Else != nil {
			c.checkStmt(
				scope,
				s.Else,
			)
		}

	case *ast.ForStmt:
		forScope := NewScope(scope)

		if s.Init != nil {
			c.checkStmt(
				forScope,
				s.Init,
			)
		}

		if s.Cond != nil {
			cond := c.checkExpr(
				forScope,
				s.Cond,
			)

			c.checkBoolCondition(
				cond,
				s.Cond.Span(),
				"for condition must be bool",
			)
		}

		if s.Post != nil {
			c.checkStmt(
				forScope,
				s.Post,
			)
		}

		oldLoopDepth := c.currentLoopDepth
		c.currentLoopDepth = oldLoopDepth + 1

		c.checkBlockInScope(
			forScope,
			s.Body,
			true,
		)

		c.currentLoopDepth = oldLoopDepth

	case *ast.SwitchStmt:
		c.checkSwitchStmt(
			scope,
			s,
		)
	}
}

func (c *Checker) checkDeferStmt(
	scope *Scope,
	s *ast.DeferStmt,
) {
	if s == nil {
		return
	}

	if c.currentDeferDepth > 0 {
		c.diags.Add(
			s.Span(),
			"nested defer is not allowed inside a defer block",
		)
	}

	hasCall := s.Call != nil
	hasBody := s.Body != nil

	if hasCall == hasBody {
		c.diags.Add(
			s.Span(),
			"defer must contain exactly one task call or one block",
		)
	}

	if s.Call != nil {
		call, ok := s.Call.(*ast.CallExpr)
		if !ok {
			c.diags.Add(
				s.Call.Span(),
				"defer expression must be a task call",
			)

			// Still check the expression so ordinary type/name diagnostics are
			// not lost.
			c.checkExpr(
				scope,
				s.Call,
			)
		} else {
			// Use the result-list checker rather than checkExpr. A deferred
			// task may return any number of values because its results are
			// discarded.
			c.checkCallResultTypes(
				scope,
				call,
			)
		}
	}

	if s.Body != nil {
		oldLoopDepth := c.currentLoopDepth
		oldDeferDepth := c.currentDeferDepth

		// A deferred block executes when leaving the task, not at its lexical
		// declaration point. It therefore cannot break or continue a loop that
		// lexically surrounds the defer statement.
		c.currentLoopDepth = 0
		c.currentDeferDepth = oldDeferDepth + 1

		c.checkBlockInScope(
			scope,
			s.Body,
			true,
		)

		c.currentLoopDepth = oldLoopDepth
		c.currentDeferDepth = oldDeferDepth
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

func (c *Checker) checkReturnStmt(
	scope *Scope,
	s *ast.ReturnStmt,
) {
	if c.currentDeferDepth > 0 {
		c.diags.Add(
			s.Span(),
			"return is not allowed inside a defer block",
		)

		// Check returned expressions for independent name and type errors, but
		// do not compare them with the surrounding task's result signature.
		for _, value := range s.Values {
			if call, ok := value.(*ast.CallExpr); ok {
				c.checkCallResultTypes(
					scope,
					call,
				)
				continue
			}

			c.checkExpr(
				scope,
				value,
			)
		}

		return
	}

	expected := c.currentResults

	if len(s.Values) == 1 &&
		len(expected) > 1 {
		if call, ok := s.Values[0].(*ast.CallExpr); ok {
			results := c.checkCallResultTypes(
				scope,
				call,
			)

			if len(results) != len(expected) {
				c.diags.Add(
					s.Span(),
					fmt.Sprintf(
						"return count mismatch: expected %d value(s), got %d",
						len(expected),
						len(results),
					),
				)
				return
			}

			for i, result := range results {
				c.checkAssignable(
					expected[i],
					result,
					s.Values[0].Span(),
				)
			}

			return
		}
	}

	if len(s.Values) != len(expected) {
		c.diags.Add(
			s.Span(),
			fmt.Sprintf(
				"return count mismatch: expected %d value(s), got %d",
				len(expected),
				len(s.Values),
			),
		)

		for _, value := range s.Values {
			c.checkExpr(
				scope,
				value,
			)
		}

		return
	}

	for i, value := range s.Values {
		got := c.checkExpr(
			scope,
			value,
		)

		c.checkAssignable(
			expected[i],
			got,
			value.Span(),
		)
	}
}

func (c *Checker) checkAssignStmt(
	scope *Scope,
	s *ast.AssignStmt,
) {
	if index, ok := s.Left.(*ast.IndexExpr); ok {
		if s.Op != token.Assign {
			c.diags.Add(
				s.Left.Span(),
				"indexed compound assignment is not supported; use an explicit bracket read and bracket assignment",
			)

			c.checkExpr(
				scope,
				index.Left,
			)

			indexType := c.checkExpr(
				scope,
				index.Index,
			)

			c.checkBracketIndexType(
				indexType,
				index.Index.Span(),
			)

			c.checkExpr(
				scope,
				s.Right,
			)

			return
		}

		c.checkIndexAssignment(
			scope,
			index,
			s.Right,
		)

		return
	}

	if id, ok := s.Left.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)

		if sym != nil {
			if sym.Kind == SymbolParam {
				c.diags.Add(
					id.Span(),
					fmt.Sprintf(
						"cannot reassign parameter %q",
						id.Name.Name,
					),
				)
			}

			if sym.Kind == SymbolConst {
				c.diags.Add(
					id.Span(),
					fmt.Sprintf(
						"cannot assign to constant %q",
						id.Name.Name,
					),
				)
			}

			if sym.Kind == SymbolTask ||
				sym.Kind == SymbolType ||
				sym.Kind == SymbolPackage {
				c.diags.Add(
					id.Span(),
					fmt.Sprintf(
						"cannot assign to %q",
						id.Name.Name,
					),
				)
			}
		}
	}

	leftType := c.checkExpr(
		scope,
		s.Left,
	)

	rightType := c.checkExpr(
		scope,
		s.Right,
	)

	if leftType != nil &&
		leftType.Kind == TypeInlineArray {
		c.diags.Add(
			s.Left.Span(),
			"@inline_array storage cannot be assigned as a whole; assign individual elements instead",
		)

		return
	}

	c.checkAssignable(
		leftType,
		rightType,
		s.Right.Span(),
	)
}

func (c *Checker) checkIndexAssignment(
	scope *Scope,
	index *ast.IndexExpr,
	value ast.Expr,
) {
	receiverType := c.checkExpr(
		scope,
		index.Left,
	)
	indexType := c.checkExpr(
		scope,
		index.Index,
	)
	valueType := c.checkExpr(
		scope,
		value,
	)

	c.checkBracketIndexType(
		indexType,
		index.Index.Span(),
	)

	if receiverType == nil {
		return
	}

	switch receiverType.Kind {
	case TypeInvalid:
		return

	case TypeInlineArray:
		if receiverType.Elem == nil {
			return
		}

		c.checkAssignable(
			receiverType.Elem,
			valueType,
			value.Span(),
		)

		c.indexResolutions[index] = IndexResolution{
			Kind: IndexResolutionInlineArrayWrite,
		}

		return

	case TypeString:
		c.diags.Add(
			index.Span(),
			"cannot assign to immutable string index",
		)
		return

	case TypeCstring:
		c.diags.Add(
			index.Span(),
			"cannot assign to immutable cstring index",
		)
		return

	case TypeVariadic:
		if receiverType.Elem != nil {
			c.checkAssignable(
				receiverType.Elem,
				valueType,
				value.Span(),
			)
		}

		c.indexResolutions[index] = IndexResolution{
			Kind: IndexResolutionVariadicWrite,
		}
		return

	case TypeRawptr:
		c.checkAssignable(
			U8Type,
			valueType,
			value.Span(),
		)

		c.indexResolutions[index] = IndexResolution{
			Kind: IndexResolutionRawptrWrite,
		}
		return
	}

	if c.isPrimitiveByteIndexable(receiverType) {
		if !c.isMutableAddressableExpr(
			scope,
			index.Left,
		) {
			c.diags.Add(
				index.Left.Span(),
				"byte-index assignment requires a mutable addressable value",
			)
		}

		c.checkAssignable(
			U8Type,
			valueType,
			value.Span(),
		)

		c.indexResolutions[index] = IndexResolution{
			Kind: IndexResolutionPrimitiveByteWrite,
		}
		return
	}

	switch receiverType.Kind {
	case TypeStruct:
		if !c.isMutableAddressableExpr(
			scope,
			index.Left,
		) {
			c.diags.Add(
				index.Left.Span(),
				"bracket assignment operator []= requires a mutable addressable receiver",
			)
		}

		receiverPointer := &Type{
			Kind: TypePointer,
			Elem: receiverType,
		}

		resolution := c.resolveBracketOverloadAt(
			scope,
			"[]=",
			[]*Type{
				receiverPointer,
				IntType,
				valueType,
			},
		)

		if resolution.Ambiguous {
			c.diags.Add(
				index.Span(),
				fmt.Sprintf(
					"ambiguous bracket assignment operator []= for receiver %s, index int, and value %s",
					receiverType.String(),
					valueType.String(),
				),
			)
			return
		}

		if !resolution.HadOverload {
			c.diags.Add(
				index.Span(),
				fmt.Sprintf(
					"type %s does not define bracket assignment operator []=",
					receiverType.String(),
				),
			)
			return
		}

		if !resolution.Matched {
			c.diags.Add(
				index.Span(),
				fmt.Sprintf(
					"no bracket assignment operator []= matches receiver %s, index int, and value %s",
					receiverType.String(),
					valueType.String(),
				),
			)
			return
		}

		c.indexResolutions[index] = IndexResolution{
			Kind:             IndexResolutionOverloadWrite,
			Candidate:        resolution.Candidate,
			TaskType:         resolution.TaskType,
			PackageName:      resolution.PackageName,
			GenericArguments: resolution.GenericArguments,
		}
		return

	case TypeEnum:
		c.diags.Add(
			index.Left.Span(),
			fmt.Sprintf(
				"enum type %s cannot be indexed",
				receiverType.String(),
			),
		)

	case TypeUnion:
		c.diags.Add(
			index.Left.Span(),
			fmt.Sprintf(
				"union type %s cannot be indexed",
				receiverType.String(),
			),
		)

	case TypeInterface:
		c.diags.Add(
			index.Left.Span(),
			fmt.Sprintf(
				"interface type %s cannot be indexed",
				receiverType.String(),
			),
		)

	case TypePointer:
		c.diags.Add(
			index.Left.Span(),
			fmt.Sprintf(
				"typed pointer %s cannot be indexed",
				receiverType.String(),
			),
		)

	default:
		c.diags.Add(
			index.Left.Span(),
			fmt.Sprintf(
				"type %s cannot be indexed",
				receiverType.String(),
			),
		)
	}
}

func (c *Checker) checkVarDeclStmt(
	scope *Scope,
	s *ast.VarDeclStmt,
) {
	var varType *Type

	if s.HasType {
		varType = c.typeFromAst(
			scope,
			s.Type,
		)
	}

	if s.HasValue {
		var valueType *Type

		if s.HasType {
			valueType = c.checkExprWithExpected(
				scope,
				s.Value,
				varType,
			)

			c.checkAssignable(
				varType,
				valueType,
				s.Value.Span(),
			)
		} else {
			valueType = c.checkExpr(
				scope,
				s.Value,
			)

			switch valueType.Kind {
			case TypeEnumLiteral:
				c.diags.Add(
					s.Value.Span(),
					fmt.Sprintf(
						"enum literal .%s needs explicit type",
						valueType.Name,
					),
				)
				varType = InvalidType

			case TypeNil:
				c.diags.Add(
					s.Value.Span(),
					"nil needs explicit type",
				)
				varType = InvalidType

			case TypeUntypedInt:
				if valueType.IntConstant != nil &&
					!integerConstantFitsType(
						valueType.IntConstant,
						IntType,
					) {
					c.diags.Add(
						s.Value.Span(),
						fmt.Sprintf(
							"integer constant %s cannot be inferred as int because it is outside the range %s; specify an explicit integer type",
							valueType.IntConstant.String(),
							integerRangeDescription(
								IntType,
							),
						),
					)

					varType = InvalidType
				} else {
					varType = c.defaultType(
						valueType,
					)
				}

			default:
				varType = c.defaultType(
					valueType,
				)
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

func isUnicodeScalar(value rune) bool {
	if value < 0 || value > utf8.MaxRune {
		return false
	}

	// UTF-16 surrogate values are not Unicode scalar values.
	return value < 0xD800 || value > 0xDFFF
}

func (c *Checker) checkStringLiteral(
	e *ast.StringLitExpr,
) {
	value, err := unquoteSealLiteral(
		e.Value,
	)
	if err != nil {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf(
				"invalid string literal: %v",
				err,
			),
		)
		return
	}

	if !utf8.ValidString(value) {
		c.diags.Add(
			e.Span(),
			"string literal must contain valid UTF-8",
		)
	}
}

func (c *Checker) checkCStringLiteral(
	e *ast.CStringLitExpr,
) {
	if len(e.Value) < 2 ||
		e.Value[0] != 'c' {
		c.diags.Add(
			e.Span(),
			"invalid cstring literal",
		)
		return
	}

	value, err := unquoteSealLiteral(
		e.Value[1:],
	)
	if err != nil {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf(
				"invalid cstring literal: %v",
				err,
			),
		)
		return
	}

	if !utf8.ValidString(value) {
		c.diags.Add(
			e.Span(),
			"cstring literal must contain valid UTF-8",
		)
	}

	if strings.IndexByte(value, 0) >= 0 {
		c.diags.Add(
			e.Span(),
			"cstring literal cannot contain an embedded null byte",
		)
	}
}

func (c *Checker) checkCharLiteral(
	e *ast.CharLitExpr,
) {
	value, err := unquoteSealLiteral(
		e.Value,
	)
	if err != nil {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf(
				"invalid char literal: %v",
				err,
			),
		)
		return
	}

	if !utf8.ValidString(value) {
		c.diags.Add(
			e.Span(),
			"char literal must contain valid UTF-8",
		)
		return
	}

	runes := []rune(value)

	if len(runes) != 1 {
		c.diags.Add(
			e.Span(),
			"char literal must contain exactly one Unicode scalar value",
		)
		return
	}

	if !isUnicodeScalar(runes[0]) {
		c.diags.Add(
			e.Span(),
			"char literal is not a valid Unicode scalar value",
		)
	}
}

func inlineArrayLengthKey(
	expr ast.Expr,
) string {
	if expr == nil {
		return ""
	}

	if literal, ok := expr.(*ast.IntLitExpr); ok {
		value, err := parseSealBigIntLiteral(
			literal.Value,
		)
		if err == nil {
			return value.String()
		}
	}

	return exprDisplay(expr)
}

func (c *Checker) isValidInlineArrayElementType(
	typ *Type,
) bool {
	if typ == nil {
		return false
	}

	switch typ.Kind {
	case TypeInvalid:
		/*
			An invalid type has already produced its own diagnostic.
			Accept it here to avoid cascading diagnostics.
		*/
		return true

	case TypeVoid,
		TypeNil,
		TypeEnumLiteral,
		TypeUntypedInt,
		TypeUntypedFloat,
		TypeVariadic,
		TypeTask,
		TypePackage,
		TypeValueParam,
		TypeInterfaceSelf:
		return false

	case TypeInlineArray:
		/*
			Nested inline arrays are valid recursively:

			    @inline_array<
			        @inline_array<int, 4>,
			        8
			    >

			The innermost stored type must itself be valid.
		*/
		return typ.Elem != nil &&
			c.isValidInlineArrayElementType(
				typ.Elem,
			)

	default:
		return true
	}
}

func (c *Checker) inlineArrayTypeFromParts(
	scope *Scope,
	elem *Type,
	lengthExpr ast.Expr,
	span source.Span,
) *Type {
	result := &Type{
		Kind:             TypeInlineArray,
		Elem:             elem,
		InlineLengthExpr: lengthExpr,
		InlineLengthKey: inlineArrayLengthKey(
			lengthExpr,
		),
	}

	if elem == nil {
		result.Elem = InvalidType
	} else if !c.isValidInlineArrayElementType(
		elem,
	) {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"%s cannot be used as @inline_array element type",
				elem.String(),
			),
		)

		result.Elem = InvalidType
	}

	if lengthExpr == nil {
		c.diags.Add(
			span,
			"@inline_array requires a compile-time length",
		)

		return result
	}

	lengthType := c.checkExpr(
		scope,
		lengthExpr,
	)

	c.checkAssignable(
		IntType,
		lengthType,
		lengthExpr.Span(),
	)

	/*
		A generic value parameter such as N is symbolically compile-time:

		    StackArray<T, N> {
		        _data @inline_array<T, N>
		    }

		The exact value becomes available when the enclosing generic
		declaration is specialized.
	*/
	if c.genericExprReferencesPlaceholder(
		scope,
		lengthExpr,
	) {
		result.InlineLengthSymbolic = true
		return result
	}

	value, ok := c.evalGenericConstExpr(
		scope,
		lengthExpr,
	)

	if !ok ||
		value.Kind != genericConstInt {
		c.diags.Add(
			lengthExpr.Span(),
			"@inline_array length must be evaluable as a compile-time int",
		)

		return result
	}

	if value.IntValue < 0 {
		c.diags.Add(
			lengthExpr.Span(),
			fmt.Sprintf(
				"@inline_array length cannot be negative, got %d",
				value.IntValue,
			),
		)

		return result
	}

	result.InlineLength = value.IntValue
	result.InlineLengthKnown = true
	result.InlineLengthSymbolic = false
	result.InlineLengthKey = strconv.FormatInt(
		value.IntValue,
		10,
	)

	return result
}

func (c *Checker) checkInlineArrayExpr(
	scope *Scope,
	e *ast.InlineArrayExpr,
) *Type {
	if e == nil {
		return InvalidType
	}

	elem := c.typeFromAst(
		scope,
		e.Elem,
	)

	result := c.inlineArrayTypeFromParts(
		scope,
		elem,
		e.Length,
		e.Span(),
	)

	if len(e.Values) > 0 {
		if !result.InlineLengthKnown {
			c.diags.Add(
				e.Span(),
				"non-empty @inline_array initializer requires a concrete compile-time length",
			)
		} else if int64(len(e.Values)) !=
			result.InlineLength {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"@inline_array<%s, %d> requires exactly %d initializer value(s), got %d",
					result.Elem.String(),
					result.InlineLength,
					result.InlineLength,
					len(e.Values),
				),
			)
		}
	}

	for _, value := range e.Values {
		got := c.checkExprWithExpected(
			scope,
			value,
			result.Elem,
		)

		c.checkAssignable(
			result.Elem,
			got,
			value.Span(),
		)
	}

	return result
}

func (c *Checker) checkExpr(
	scope *Scope,
	expr ast.Expr,
) (result *Type) {
	if expr == nil {
		return InvalidType
	}

	defer func() {
		if result == nil {
			result = InvalidType
		}

		c.exprTypes[expr] = result
	}()

	switch e := expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(e.Name.Name)
		if sym == nil {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"undefined symbol %q",
					e.Name.Name,
				),
			)

			return InvalidType
		}

		if sym.Type == nil {
			return InvalidType
		}

		return sym.Type

	case *ast.DotIdentExpr:
		return &Type{
			Kind: TypeEnumLiteral,
			Name: e.Name.Name,
		}

	case *ast.InlineArrayExpr:
		return c.checkInlineArrayExpr(
			scope,
			e,
		)

	case *ast.SpreadExpr:
		c.diags.Add(
			e.Span(),
			"spread can only be used as a call argument",
		)

		c.checkExpr(
			scope,
			e.Expr,
		)

		return InvalidType

	case *ast.GenericExpr:
		c.diags.Add(
			e.Span(),
			"generic expression cannot be used as a value",
		)

		return InvalidType

	case *ast.IntLitExpr:
		value, err := parseSealBigIntLiteral(
			e.Value,
		)
		if err != nil {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"invalid integer literal: %v",
					err,
				),
			)

			return InvalidType
		}

		return untypedIntConstantType(
			value,
		)

	case *ast.FloatLitExpr:
		return UntypedFloatType

	case *ast.StringLitExpr:
		c.checkStringLiteral(e)
		return StringType

	case *ast.CStringLitExpr:
		c.checkCStringLiteral(e)
		return CstringType

	case *ast.CharLitExpr:
		c.checkCharLiteral(e)
		return CharType

	case *ast.BoolLitExpr:
		return BoolType

	case *ast.NilLitExpr:
		return NilType

	case *ast.UnaryExpr:
		return c.checkUnaryExpr(
			scope,
			e,
		)

	case *ast.BinaryExpr:
		return c.checkBinaryExpr(
			scope,
			e,
		)

	case *ast.CallExpr:
		return c.checkCallExpr(
			scope,
			e,
		)

	case *ast.SelectorExpr:
		return c.checkSelectorExpr(
			scope,
			e,
		)

	case *ast.IndexExpr:
		return c.checkIndexExpr(
			scope,
			e,
		)

	case *ast.CompoundLiteralExpr:
		return c.checkCompoundLiteralExpr(
			scope,
			e,
		)
	}

	return InvalidType
}

func (c *Checker) checkExprWithExpected(
	scope *Scope,
	expr ast.Expr,
	expected *Type,
) *Type {
	_ = expected

	return c.checkExpr(scope, expr)
}

func (c *Checker) checkUnaryExpr(
	scope *Scope,
	e *ast.UnaryExpr,
) *Type {
	typ := c.checkExpr(
		scope,
		e.Expr,
	)

	switch e.Op {
	case token.Minus:
		if !c.isNumeric(typ) {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"operator '-' requires numeric type, got %s",
					typ.String(),
				),
			)
			return InvalidType
		}

		if typ.Kind == TypeUntypedInt &&
			typ.IntConstant != nil {
			value := new(big.Int).Neg(
				typ.IntConstant,
			)

			return untypedIntConstantType(
				value,
			)
		}

		return typ

	case token.Bang:
		if !c.sameType(typ, BoolType) {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"operator '!' requires bool, got %s",
					typ.String(),
				),
			)
			return InvalidType
		}

		return BoolType

	case token.Amp:
		if typ.Kind == TypeNil {
			c.diags.Add(
				e.Span(),
				"cannot take the address of nil",
			)
			return InvalidType
		}

		return &Type{
			Kind: TypePointer,
			Elem: typ,
		}

	case token.Star:
		if typ.Kind != TypePointer {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"cannot dereference non-pointer type %s",
					typ.String(),
				),
			)
			return InvalidType
		}

		return typ.Elem

	case token.Tilde:
		if !c.isIntegerLike(typ) {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"operator '~' requires integer type, got %s",
					typ.String(),
				),
			)
			return InvalidType
		}

		if typ.Kind == TypeUntypedInt &&
			typ.IntConstant != nil {
			value := new(big.Int).Not(
				typ.IntConstant,
			)

			return untypedIntConstantType(
				value,
			)
		}

		return typ
	}

	return InvalidType
}

func (c *Checker) integerBinaryResultType(
	op token.Kind,
	left *Type,
	right *Type,
	result *Type,
	span source.Span,
) *Type {
	if result == nil ||
		result.Kind != TypeUntypedInt ||
		left == nil ||
		right == nil ||
		left.IntConstant == nil ||
		right.IntConstant == nil {
		return result
	}

	value := new(big.Int)

	switch op {
	case token.Plus:
		value.Add(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Minus:
		value.Sub(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Star:
		value.Mul(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Slash:
		if right.IntConstant.Sign() == 0 {
			c.diags.Add(
				span,
				"integer division by zero",
			)
			return InvalidType
		}

		value.Quo(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Percent:
		if right.IntConstant.Sign() == 0 {
			c.diags.Add(
				span,
				"integer remainder by zero",
			)
			return InvalidType
		}

		value.Rem(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Amp:
		value.And(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Pipe:
		value.Or(
			left.IntConstant,
			right.IntConstant,
		)

	case token.Caret:
		value.Xor(
			left.IntConstant,
			right.IntConstant,
		)

	default:
		return result
	}

	return untypedIntConstantType(
		value,
	)
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

			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
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

			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
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

			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
		}

		if result, ok := c.checkOperatorOverload(scope, e.Op.String(), []*Type{left, right}, e.Span(), true); ok {
			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
		}

		c.diags.Add(e.Span(), fmt.Sprintf("operator %q requires integer operands", e.Op.String()))
		return InvalidType

	case token.EqEq:
		if c.builtinEqualityCompatible(left, right) {
			return BoolType
		}

		if result, ok := c.checkOperatorOverload(scope, "==", []*Type{left, right}, e.Span(), true); ok {
			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
		}

		c.diags.Add(e.Span(), fmt.Sprintf("cannot compare %s and %s", left.String(), right.String()))
		return InvalidType

	case token.NotEq:
		if c.builtinEqualityCompatible(left, right) {
			return BoolType
		}

		if result, ok := c.checkOperatorOverload(scope, "!=", []*Type{left, right}, e.Span(), false); ok {
			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
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
			return c.integerBinaryResultType(
				e.Op,
				left,
				right,
				result,
				e.Span(),
			)
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
	lookups := c.overloadLookups(scope, name, argTypes)
	if len(lookups) == 0 {
		return InvalidType, false
	}

	best := overloadResolution{
		Score: 1 << 30,
	}

	for _, lookup := range lookups {
		c.ensureOverloadSymbolPrepared(lookup.Scope, lookup.Symbol)

		result := c.resolveOverload(lookup.Symbol.Overload, argTypes)
		if !result.Matched {
			continue
		}

		if result.Ambiguous {
			c.diags.Add(
				span,
				fmt.Sprintf("ambiguous operator overload %q with operand types (%s)", name, c.formatTypes(argTypes)),
			)
			return InvalidType, true
		}

		if !best.Matched || result.Score < best.Score {
			best = result
			continue
		}

		if result.Score == best.Score {
			best.Ambiguous = true
		}
	}

	if !best.Matched {
		if diagnoseMissing {
			c.diags.Add(
				span,
				fmt.Sprintf("no operator overload %q matches operand types (%s)", name, c.formatTypes(argTypes)),
			)
		}

		return InvalidType, true
	}

	if best.Ambiguous {
		c.diags.Add(
			span,
			fmt.Sprintf("ambiguous operator overload %q with operand types (%s)", name, c.formatTypes(argTypes)),
		)

		return InvalidType, true
	}

	return c.resultTypeFromCall(best.Candidate.Type, span), true
}

func (c *Checker) builtinEqualityCompatible(
	a *Type,
	b *Type,
) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind == TypeInvalid ||
		b.Kind == TypeInvalid {
		return true
	}

	if c.isNumeric(a) &&
		c.isNumeric(b) {
		_, ok := c.numericResultType(
			a,
			b,
		)
		return ok
	}

	if c.sameType(a, BoolType) &&
		c.sameType(b, BoolType) {
		return true
	}

	// String and cstring equality compare their contents.
	if c.sameType(a, StringType) &&
		c.sameType(b, StringType) {
		return true
	}

	if c.sameType(a, CstringType) &&
		c.sameType(b, CstringType) {
		return true
	}

	if a.Kind == TypeEnum &&
		b.Kind == TypeEnum {
		return c.sameType(a, b)
	}

	if a.Kind == TypeEnum &&
		b.Kind == TypeEnumLiteral {
		return c.enumHasVariant(
			a,
			b.Name,
		)
	}

	if b.Kind == TypeEnum &&
		a.Kind == TypeEnumLiteral {
		return c.enumHasVariant(
			b,
			a.Name,
		)
	}

	// Raw pointers use address identity.
	if a.Kind == TypeRawptr &&
		b.Kind == TypeRawptr {
		return true
	}

	// Typed pointers use address identity, but only pointers with compatible
	// element types may be compared directly.
	if a.Kind == TypePointer &&
		b.Kind == TypePointer {
		return c.sameType(
			a,
			b,
		)
	}

	if a.Kind == TypePointer &&
		b.Kind == TypeNil {
		return true
	}

	if b.Kind == TypePointer &&
		a.Kind == TypeNil {
		return true
	}

	if a.Kind == TypeRawptr &&
		b.Kind == TypeNil {
		return true
	}

	if b.Kind == TypeRawptr &&
		a.Kind == TypeNil {
		return true
	}

	if a.Kind == TypeDistinct &&
		b.Kind == TypeDistinct &&
		c.sameType(a, b) {
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

	c.diags.Add(
		e.Span(),
		"multi-result interface task call cannot be used as a single expression; destructure it with a, b :=",
	)
	return InvalidType, true
}

func (c *Checker) checkInterfaceDispatchCallResultTypes(
	scope *Scope,
	e *ast.CallExpr,
	argTypes []*Type,
	argSpans []source.Span,
) ([]*Type, bool) {
	_ = scope

	id, ok := e.Callee.(*ast.IdentExpr)
	if !ok || len(argTypes) == 0 {
		return nil, false
	}

	ifaceType := argTypes[0]
	if ifaceType == nil || ifaceType.Kind != TypeInterface {
		return nil, false
	}

	req := c.lookupInterfaceRequirement(
		ifaceType,
		id.Name.Name,
	)
	if req == nil {
		return nil, false
	}

	if len(argTypes) != len(req.Params) {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf(
				"interface call argument count mismatch for %q: expected %d, got %d",
				req.Name,
				len(req.Params),
				len(argTypes),
			),
		)
	}

	// Argument zero is the interface wrapper. The underlying *self receiver is
	// extracted by generated static dispatch or the dynamic vtable.
	count := len(argTypes)
	if len(req.Params) < count {
		count = len(req.Params)
	}

	for i := 1; i < count; i++ {
		c.checkAssignable(
			req.Params[i],
			argTypes[i],
			argSpans[i],
		)
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

func (c *Checker) checkCallResultTypes(
	scope *Scope,
	e *ast.CallExpr,
) []*Type {
	/*
		Handle size before ordinary argument checking for the same reason as
		checkCallExpr: its argument may be a type expression rather than a
		runtime value expression.
	*/
	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if kind, ok := c.primitiveTaskKind(
			scope,
			id.Name.Name,
		); ok && kind == builtin.TaskSize {
			return []*Type{
				c.checkSizeCall(
					scope,
					e.Args,
					e.Span(),
				),
			}
		}
	}

	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		return c.checkGenericCallResultTypes(
			scope,
			gen,
			nil,
			nil,
			e.Args,
			e.Span(),
		)
	}

	argTypes, argSpans := c.checkCallArgumentTypes(
		scope,
		e.Args,
	)

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if scope.Lookup(id.Name.Name) == nil {
			if results, ok :=
				c.checkInterfaceDispatchCallResultTypes(
					scope,
					e,
					argTypes,
					argSpans,
				); ok {
				return results
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if c.shouldUseLenDispatch(
			scope,
			id.Name.Name,
		) {
			return []*Type{
				c.checkLenCall(
					scope,
					e,
					argTypes,
				),
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if kind, ok := c.primitiveTaskKind(
			scope,
			id.Name.Name,
		); ok {
			switch kind {
			case builtin.TaskSize:
				/*
					Already handled before argument checking.
					This remains as a defensive fallback.
				*/
				return []*Type{
					c.checkSizeCall(
						scope,
						e.Args,
						e.Span(),
					),
				}

			case builtin.TaskAssert:
				return []*Type{
					c.checkAssertCall(
						e.Args,
						argTypes,
						e.Span(),
					),
				}

			case builtin.TaskPanic:
				return []*Type{
					c.checkPanicCall(
						e.Args,
						argTypes,
						e.Span(),
					),
				}

			case builtin.TaskTrap:
				return []*Type{
					c.checkNoArgVoidPrimitive(
						"trap",
						e.Args,
						argTypes,
						e.Span(),
					),
				}

			case builtin.TaskUnreachable:
				return []*Type{
					c.checkNoArgVoidPrimitive(
						"unreachable",
						e.Args,
						argTypes,
						e.Span(),
					),
				}
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym == nil {
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"undefined symbol %q",
					id.Name.Name,
				),
			)
			return []*Type{
				InvalidType,
			}
		}

		switch sym.Kind {
		case SymbolTask:
			return c.checkDirectTaskCallResultTypes(
				sym,
				argTypes,
				argSpans,
				e.Span(),
			)

		case SymbolOverload:
			return c.checkOverloadCallResultTypes(
				sym,
				argTypes,
				argSpans,
				e.Span(),
			)

		default:
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"cannot call non-task symbol %q",
					id.Name.Name,
				),
			)
			return []*Type{
				InvalidType,
			}
		}
	}

	if selector, ok :=
		e.Callee.(*ast.SelectorExpr); ok {
		if id, ok :=
			selector.Left.(*ast.IdentExpr); ok {
			pkgSym := scope.Lookup(id.Name.Name)

			if pkgSym != nil &&
				pkgSym.Kind == SymbolPackage {
				return c.checkPackageCallResultTypes(
					pkgSym,
					selector,
					argTypes,
					argSpans,
					e.Span(),
				)
			}
		}
	}

	calleeType := c.checkExpr(
		scope,
		e.Callee,
	)

	if calleeType.Kind != TypeTask {
		c.diags.Add(
			e.Callee.Span(),
			fmt.Sprintf(
				"cannot call non-task type %s",
				calleeType.String(),
			),
		)
		return []*Type{
			InvalidType,
		}
	}

	c.checkTaskTypeCallArguments(
		calleeType,
		argTypes,
		argSpans,
		e.Span(),
	)

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

func (c *Checker) checkGenericOverloadCallResultTypes(
	scope *Scope,
	gen *ast.GenericExpr,
	sym *Symbol,
	packageName string,
	args []ast.Expr,
	span source.Span,
) []*Type {
	if sym == nil ||
		sym.Kind != SymbolOverload {
		return []*Type{InvalidType}
	}

	c.ensureOverloadSymbolPrepared(
		scope,
		sym,
	)

	info := sym.Overload
	if info == nil {
		c.diags.Add(
			sym.Span,
			fmt.Sprintf(
				"symbol %q is not a valid overload",
				sym.Name,
			),
		)
		return []*Type{InvalidType}
	}

	resolvedGenericArgs :=
		c.resolveOverloadGenericArguments(
			scope,
			gen.Args,
		)

	runtimeArgTypes, runtimeArgSpans :=
		c.checkCallArgumentTypes(
			scope,
			args,
		)

	result := c.resolveGenericOverload(
		scope,
		info,
		packageName,
		gen.Args,
		resolvedGenericArgs,
		runtimeArgTypes,
	)

	if !result.Matched {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"no generic overload of %q matches generic arguments <%s> and argument types (%s)",
				info.Name,
				formatGenericArguments(gen.Args),
				c.formatTypes(runtimeArgTypes),
			),
		)

		return []*Type{InvalidType}
	}

	if result.Ambiguous {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"ambiguous generic overload call %q with generic arguments <%s> and argument types (%s)",
				info.Name,
				formatGenericArguments(gen.Args),
				c.formatTypes(runtimeArgTypes),
			),
		)

		return []*Type{InvalidType}
	}

	// Candidate selection uses only generic parameter categories and runtime
	// parameter conversion. Constraints are checked only after one candidate
	// has been selected.
	c.checkGenericArgsAgainstParams(
		scope,
		gen.Args,
		result.Candidate.Type.GenericParams,
		gen.Span(),
	)

	c.checkTaskTypeCallArguments(
		result.TaskType,
		runtimeArgTypes,
		runtimeArgSpans,
		span,
	)

	genericArguments := overloadGenericArgumentInfos(
		result.Candidate.Type.GenericParams,
		gen.Args,
		resolvedGenericArgs,
	)

	c.genericOverloadCalls[gen] =
		GenericOverloadCallResolution{
			Candidate:        result.Candidate,
			TaskType:         result.TaskType,
			PackageName:      packageName,
			GenericArguments: genericArguments,
		}

	return result.TaskType.Results
}

func (c *Checker) checkGenericCallResultTypes(
	scope *Scope,
	gen *ast.GenericExpr,
	argTypes []*Type,
	argSpans []source.Span,
	args []ast.Expr,
	span source.Span,
) []*Type {
	_ = argTypes
	_ = argSpans

	if id, ok := gen.Base.(*ast.IdentExpr); ok {
		if task, ok :=
			builtin.LookupTask(id.Name.Name); ok &&
			task.Generic {
			actualTypes, _ :=
				c.checkCallArgumentTypes(
					scope,
					args,
				)

			result := c.checkGenericIntrinsicCall(
				scope,
				gen,
				actualTypes,
				args,
				span,
			)

			return []*Type{result}
		}
	}

	sym, packageName, ok := c.genericBaseSymbol(
		scope,
		gen.Base,
	)
	if !ok {
		return []*Type{InvalidType}
	}

	switch sym.Kind {
	case SymbolOverload:
		return c.checkGenericOverloadCallResultTypes(
			scope,
			gen,
			sym,
			packageName,
			args,
			span,
		)

	case SymbolTask:
		// Continue with ordinary generic-task specialization.

	default:
		c.diags.Add(
			gen.Base.Span(),
			fmt.Sprintf(
				"generic callee %q is not a task or overload",
				sym.Name,
			),
		)
		return []*Type{InvalidType}
	}

	c.ensureTaskSymbolPrepared(
		scope,
		sym,
	)

	if sym.Type == nil ||
		sym.Type.Kind != TypeTask {
		c.diags.Add(
			gen.Base.Span(),
			"generic callee has invalid task type",
		)
		return []*Type{InvalidType}
	}

	if len(sym.Type.GenericParams) == 0 {
		c.diags.Add(
			gen.Span(),
			fmt.Sprintf(
				"task %q is not generic",
				sym.Name,
			),
		)
		return []*Type{InvalidType}
	}

	c.checkGenericArgsAgainstParams(
		scope,
		gen.Args,
		sym.Type.GenericParams,
		gen.Span(),
	)

	taskDecl, _ :=
		sym.Node.(*ast.TaskDecl)

	var instantiated *Type

	switch {
	case packageName != "":
		instantiated =
			c.taskTypeFromImportedGenericSignature(
				scope,
				packageName,
				sym.Type,
				gen.Args,
				gen.Span(),
			)

	case taskDecl != nil:
		instantiated = c.taskTypeFromGenericCall(
			scope,
			taskDecl,
			gen.Args,
		)

	default:
		instantiated = c.taskTypeFromGenericSignature(
			scope,
			sym.Type,
			gen.Args,
			gen.Span(),
		)
	}

	c.checkGenericTaskCallArguments(
		scope,
		instantiated,
		args,
		span,
	)

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

func (c *Checker) checkGenericTaskCallArguments(
	scope *Scope,
	taskType *Type,
	args []ast.Expr,
	span source.Span,
) {
	if taskType == nil || taskType.Kind != TypeTask {
		for _, arg := range args {
			c.checkExpr(scope, arg)
		}
		return
	}

	required := taskType.RequiredParams
	total := len(taskType.Params)

	if required == 0 &&
		total > 0 &&
		len(taskType.ParamHasDefault) == 0 &&
		!taskType.IsVariadic {
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
				fmt.Sprintf(
					"task call argument count mismatch: expected at least %d, got %d",
					required,
					len(args),
				),
			)
		}

		count := len(args)
		if count > fixedCount {
			count = fixedCount
		}

		for i := 0; i < count; i++ {
			if spread, ok := args[i].(*ast.SpreadExpr); ok {
				c.diags.Add(
					spread.Span(),
					"spread variadic argument cannot be used for fixed parameter",
				)
				c.checkExpr(scope, spread.Expr)
				continue
			}

			expected := InvalidType
			if i < len(taskType.Params) {
				expected = taskType.Params[i]
			}

			got := c.checkExprWithExpected(
				scope,
				args[i],
				expected,
			)
			c.checkAssignable(
				expected,
				got,
				args[i].Span(),
			)
		}

		for i := fixedCount; i < len(args); i++ {
			if spread, ok := args[i].(*ast.SpreadExpr); ok {
				c.checkGenericVariadicSpreadArgument(
					scope,
					spread,
					variadicElem,
				)
				continue
			}

			got := c.checkExprWithExpected(
				scope,
				args[i],
				variadicElem,
			)
			c.checkAssignable(
				variadicElem,
				got,
				args[i].Span(),
			)
		}

		return
	}

	if len(args) < required || len(args) > total {
		if required == total {
			c.diags.Add(
				span,
				fmt.Sprintf(
					"task call argument count mismatch: expected %d, got %d",
					total,
					len(args),
				),
			)
		} else {
			c.diags.Add(
				span,
				fmt.Sprintf(
					"task call argument count mismatch: expected %d to %d, got %d",
					required,
					total,
					len(args),
				),
			)
		}
	}

	count := len(args)
	if total < count {
		count = total
	}

	for i := 0; i < count; i++ {
		if spread, ok := args[i].(*ast.SpreadExpr); ok {
			c.diags.Add(
				spread.Span(),
				"cannot spread variadic argument into non-variadic task",
			)
			c.checkExpr(scope, spread.Expr)
			continue
		}

		expected := taskType.Params[i]
		got := c.checkExprWithExpected(
			scope,
			args[i],
			expected,
		)
		c.checkAssignable(
			expected,
			got,
			args[i].Span(),
		)
	}

	for i := count; i < len(args); i++ {
		c.checkExpr(scope, args[i])
	}
}

func (c *Checker) checkGenericVariadicSpreadArgument(
	scope *Scope,
	spread *ast.SpreadExpr,
	expectedElem *Type,
) {
	spreadType := c.checkExpr(
		scope,
		spread.Expr,
	)

	if spreadType.Kind != TypeVariadic {
		c.diags.Add(
			spread.Span(),
			fmt.Sprintf(
				"cannot spread %s; expected variadic value",
				spreadType.String(),
			),
		)
		return
	}

	if spreadType.Elem == nil {
		c.diags.Add(
			spread.Span(),
			"cannot spread invalid variadic value",
		)
		return
	}

	c.checkAssignable(
		expectedElem,
		spreadType.Elem,
		spread.Span(),
	)
}

func (c *Checker) checkCallExpr(
	scope *Scope,
	e *ast.CallExpr,
) *Type {
	/*
		Handle size before ordinary call-argument checking.

		A size argument may be either a value expression:

		    size(value)

		or a type expression:

		    size(T)
		    size(_Slot<T>)
		    size(pkg.Container<T>)

		Generic type expressions are represented by ast.GenericExpr and are
		not valid runtime values, so they must not pass through
		checkCallArgumentTypes.
	*/
	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if kind, ok := c.primitiveTaskKind(
			scope,
			id.Name.Name,
		); ok && kind == builtin.TaskSize {
			return c.checkSizeCall(
				scope,
				e.Args,
				e.Span(),
			)
		}
	}

	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		return c.checkGenericCallExpr(
			scope,
			gen,
			nil,
			nil,
			e.Args,
			e.Span(),
		)
	}

	argTypes, argSpans := c.checkCallArgumentTypes(
		scope,
		e.Args,
	)

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if scope.Lookup(id.Name.Name) == nil {
			if result, ok := c.checkInterfaceDispatchCall(
				scope,
				e,
				argTypes,
				argSpans,
			); ok {
				return result
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if c.shouldUseLenDispatch(
			scope,
			id.Name.Name,
		) {
			return c.checkLenCall(
				scope,
				e,
				argTypes,
			)
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		if kind, ok := c.primitiveTaskKind(
			scope,
			id.Name.Name,
		); ok {
			switch kind {
			case builtin.TaskSize:
				/*
					Already handled before argument checking.
					This case remains defensive in case the control flow is
					changed later.
				*/
				return c.checkSizeCall(
					scope,
					e.Args,
					e.Span(),
				)

			case builtin.TaskAssert:
				return c.checkAssertCall(
					e.Args,
					argTypes,
					e.Span(),
				)

			case builtin.TaskPanic:
				return c.checkPanicCall(
					e.Args,
					argTypes,
					e.Span(),
				)

			case builtin.TaskTrap:
				return c.checkNoArgVoidPrimitive(
					"trap",
					e.Args,
					argTypes,
					e.Span(),
				)

			case builtin.TaskUnreachable:
				return c.checkNoArgVoidPrimitive(
					"unreachable",
					e.Args,
					argTypes,
					e.Span(),
				)
			}
		}
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym == nil {
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"undefined symbol %q",
					id.Name.Name,
				),
			)
			return InvalidType
		}

		switch sym.Kind {
		case SymbolTask:
			return c.checkDirectTaskCall(
				sym,
				argTypes,
				argSpans,
				e.Span(),
			)

		case SymbolOverload:
			return c.checkOverloadCall(
				sym,
				argTypes,
				argSpans,
				e.Span(),
			)

		default:
			c.diags.Add(
				id.Span(),
				fmt.Sprintf(
					"cannot call non-task symbol %q",
					id.Name.Name,
				),
			)
			return InvalidType
		}
	}

	if selector, ok := e.Callee.(*ast.SelectorExpr); ok {
		if id, ok := selector.Left.(*ast.IdentExpr); ok {
			pkgSym := scope.Lookup(id.Name.Name)
			if pkgSym != nil &&
				pkgSym.Kind == SymbolPackage {
				return c.checkPackageCall(
					pkgSym,
					selector,
					argTypes,
					argSpans,
					e.Span(),
				)
			}
		}
	}

	calleeType := c.checkExpr(
		scope,
		e.Callee,
	)

	if calleeType.Kind != TypeTask {
		c.diags.Add(
			e.Callee.Span(),
			fmt.Sprintf(
				"cannot call non-task type %s",
				calleeType.String(),
			),
		)
		return InvalidType
	}

	return c.checkTaskTypeCall(
		calleeType,
		argTypes,
		argSpans,
		e.Span(),
	)
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

	member = c.importedPackageMemberSymbol(pkgSym.Name, member)

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

func (c *Checker) isCharCastIntegerType(
	t *Type,
) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeUntypedInt,
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
		TypeChar:
		return true

	default:
		return false
	}
}

func (c *Checker) checkStringLikeConstructorCast(
	name string,
	targetType *Type,
	args []ast.Expr,
	argTypes []*Type,
	span source.Span,
) *Type {
	if len(argTypes) != 2 {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"cast<%s> expects exactly 2 arguments: rawptr and uint byte length; got %d",
				name,
				len(argTypes),
			),
		)
		return targetType
	}

	c.checkAssignable(
		RawptrType,
		argTypes[0],
		args[0].Span(),
	)

	c.checkAssignable(
		UintType,
		argTypes[1],
		args[1].Span(),
	)

	return targetType
}

func (c *Checker) checkCastIntrinsicCall(
	scope *Scope,
	genericExpr *ast.GenericExpr,
	targetType *Type,
	args []ast.Expr,
	argTypes []*Type,
	span source.Span,
) *Type {
	switch targetType.Kind {
	case TypeString:
		return c.checkStringLikeConstructorCast(
			"string",
			StringType,
			args,
			argTypes,
			span,
		)

	case TypeCstring:
		return c.checkStringLikeConstructorCast(
			"cstring",
			CstringType,
			args,
			argTypes,
			span,
		)
	}

	if len(argTypes) != 1 {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"cast<%s> expects exactly 1 value argument, got %d",
				targetType.String(),
				len(argTypes),
			),
		)
		return targetType
	}

	sourceType := argTypes[0]

	if sourceType == nil ||
		sourceType.Kind == TypeInvalid ||
		targetType == nil ||
		targetType.Kind == TypeInvalid {
		return targetType
	}

	/*
		A cast appearing inside an unspecialized generic declaration may use
		a symbolic source or destination type:

		    Wrap :: task<T type>(value int) T {
		        return cast<T>(value)
		    }

		The primitive cast rules cannot decide whether this is valid until T
		is replaced with a concrete specialization argument.

		The cast expression nevertheless has the symbolic destination type,
		which allows the surrounding generic task body and default parameter
		expressions to be checked.
	*/
	if c.typeContainsGenericPlaceholder(targetType) ||
		c.typeContainsGenericPlaceholder(sourceType) ||
		c.genericExprReferencesPlaceholder(scope, args[0]) {
		return targetType
	}

	// Strings expose only their UTF-8 data pointer.
	if sourceType.Kind == TypeString {
		if targetType.Kind == TypeRawptr {
			return RawptrType
		}

		c.diags.Add(
			args[0].Span(),
			fmt.Sprintf(
				"string may only be cast to rawptr, not %s",
				targetType.String(),
			),
		)
		return targetType
	}

	// Cstrings expose only their underlying C byte pointer.
	if sourceType.Kind == TypeCstring {
		if targetType.Kind == TypeRawptr {
			return RawptrType
		}

		c.diags.Add(
			args[0].Span(),
			fmt.Sprintf(
				"cstring may only be cast to rawptr, not %s",
				targetType.String(),
			),
		)
		return targetType
	}

	if sourceType.Kind == TypeNil &&
		targetType.Kind != TypeInterface {
		if !c.typeAcceptsNil(targetType) {
			c.diags.Add(
				args[0].Span(),
				fmt.Sprintf(
					"cannot cast nil to %s",
					targetType.String(),
				),
			)
		}

		return targetType
	}

	/*
		Interface construction requires a pointer to the concrete implementing
		value. Record the exact checker-selected implementation so codegen does
		not need to repeat type inference or implementation matching.
	*/
	if targetType.Kind == TypeInterface {
		if sourceType.Kind != TypePointer ||
			sourceType.Elem == nil {
			c.diags.Add(
				args[0].Span(),
				fmt.Sprintf(
					"interface cast to %s requires a pointer to an implementing value, got %s",
					targetType.String(),
					sourceType.String(),
				),
			)
			return targetType
		}

		concreteType := sourceType.Elem

		resolution := c.resolveImplAt(
			targetType,
			concreteType,
			args[0].Span(),
			nil,
			true,
		)

		switch resolution.Kind {
		case ImplResolutionAmbiguous:
			// resolveImplAt emitted the ambiguity diagnostic.

		case ImplResolutionNotFound:
			c.diags.Add(
				args[0].Span(),
				fmt.Sprintf(
					"cannot cast %s to %s: no matching implementation",
					sourceType.String(),
					targetType.String(),
				),
			)

		case ImplResolutionFound:
			if genericExpr != nil {
				c.interfaceConversions[genericExpr] =
					InterfaceConversionResolution{
						SourcePointer: sourceType,
						Concrete:      concreteType,
						Interface:     targetType,
						Impl:          resolution.Resolved,
					}
			}
		}

		return targetType
	}

	if sourceType.Kind == TypeInterface {
		c.diags.Add(
			args[0].Span(),
			fmt.Sprintf(
				"cannot directly cast interface %s to %s",
				sourceType.String(),
				targetType.String(),
			),
		)
		return targetType
	}

	if !c.primitiveCastAllowed(
		targetType,
		sourceType,
	) {
		c.diags.Add(
			args[0].Span(),
			fmt.Sprintf(
				"cannot cast %s to %s",
				sourceType.String(),
				targetType.String(),
			),
		)
		return targetType
	}

	c.checkConstantCastRange(
		targetType,
		sourceType,
		args[0].Span(),
	)

	return targetType
}

func (c *Checker) checkGenericIntrinsicCall(
	scope *Scope,
	gen *ast.GenericExpr,
	argTypes []*Type,
	args []ast.Expr,
	span source.Span,
) *Type {
	id, ok := gen.Base.(*ast.IdentExpr)
	if !ok {
		c.diags.Add(
			gen.Base.Span(),
			"only intrinsic generic calls are supported here",
		)
		return InvalidType
	}

	name := id.Name.Name

	if c.isShadowedPrimitive(scope, name) {
		c.diags.Add(
			id.Span(),
			fmt.Sprintf(
				"%q is not an intrinsic generic task in this scope",
				name,
			),
		)
		return InvalidType
	}

	task, ok := builtin.LookupTask(name)
	if !ok || !task.Generic {
		c.diags.Add(
			id.Span(),
			fmt.Sprintf(
				"unknown generic intrinsic %q",
				name,
			),
		)
		return InvalidType
	}

	if len(gen.Args) != 1 {
		c.diags.Add(
			gen.Span(),
			fmt.Sprintf(
				"%s expects exactly 1 type argument",
				name,
			),
		)
		return InvalidType
	}

	targetType := c.typeFromGenericArg(
		scope,
		gen.Args[0],
	)

	switch task.Kind {
	case builtin.TaskAnyAs:
		if len(argTypes) != 1 {
			c.diags.Add(
				span,
				fmt.Sprintf(
					"anyAs expects exactly 1 value argument, got %d",
					len(argTypes),
				),
			)
			return targetType
		}

		if !c.sameType(argTypes[0], AnyType) {
			c.diags.Add(
				args[0].Span(),
				fmt.Sprintf(
					"anyAs expects any, got %s",
					argTypes[0].String(),
				),
			)
		}

		return targetType

	case builtin.TaskAnyIs:
		if len(argTypes) != 1 {
			c.diags.Add(
				span,
				fmt.Sprintf(
					"anyIs expects exactly 1 value argument, got %d",
					len(argTypes),
				),
			)
			return BoolType
		}

		if !c.sameType(argTypes[0], AnyType) {
			c.diags.Add(
				args[0].Span(),
				fmt.Sprintf(
					"anyIs expects any, got %s",
					argTypes[0].String(),
				),
			)
		}

		return BoolType

	case builtin.TaskCast:
		return c.checkCastIntrinsicCall(
			scope,
			gen,
			targetType,
			args,
			argTypes,
			span,
		)

	default:
		c.diags.Add(
			id.Span(),
			fmt.Sprintf(
				"unknown generic intrinsic %q",
				name,
			),
		)
		return InvalidType
	}
}

func (c *Checker) checkSizeCall(
	scope *Scope,
	args []ast.Expr,
	span source.Span,
) *Type {
	if len(args) != 1 {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"size expects 1 argument, got %d",
				len(args),
			),
		)

		/*
			Check additional arguments normally so independent diagnostics,
			such as undefined symbols, are still reported.
		*/
		for _, arg := range args {
			c.checkExpr(
				scope,
				arg,
			)
		}

		return UintType
	}

	arg := args[0]

	/*
		First try to interpret the argument syntactically as a type.

		This supports:

		    size(T)
		    size(int)
		    size(_Slot<T>)
		    size(lists.Array<int>)
		    size(*Node)

		Identifier expressions are intentionally considered type expressions
		only when they resolve to SymbolType. This preserves size(value) for
		ordinary variables.
	*/
	var typ *Type

	switch e := arg.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(
			e.Name.Name,
		)

		if sym != nil &&
			sym.Kind == SymbolType {
			typ = sym.Type
		}

	case *ast.SelectorExpr:
		typeAst := typeAstFromExpr(e)

		if typeAst != nil {
			/*
				Only treat a selector as a type when its leading identifier
				names a package and the selected package member is a type.
				Otherwise it remains an ordinary value selector.
			*/
			if packageIdent, ok :=
				e.Left.(*ast.IdentExpr); ok {
				packageSymbol := scope.Lookup(
					packageIdent.Name.Name,
				)

				if packageSymbol != nil &&
					packageSymbol.Kind ==
						SymbolPackage &&
					packageSymbol.Package != nil {
					member :=
						packageSymbol.Package.Symbols[e.Name.Name]

					if member != nil &&
						member.Kind == SymbolType {
						typ = c.typeFromAst(
							scope,
							typeAst,
						)
					}
				}
			}
		}

	case *ast.GenericExpr:
		typeAst := typeAstFromExpr(e)

		if typeAst == nil {
			c.diags.Add(
				e.Span(),
				"size generic argument must form a valid instantiated type",
			)
			return UintType
		}

		typ = c.typeFromAst(
			scope,
			typeAst,
		)
	}

	/*
		If it was not syntactically and semantically recognized as a type,
		treat it as an ordinary value expression.
	*/
	if typ == nil {
		typ = c.checkExpr(
			scope,
			arg,
		)
	}

	if typ == nil ||
		typ.Kind == TypeInvalid {
		return UintType
	}

	switch typ.Kind {
	case TypeVoid,
		TypeNil,
		TypePackage,
		TypeTask,
		TypeEnumLiteral:
		c.diags.Add(
			arg.Span(),
			fmt.Sprintf(
				"size does not support %s",
				typ.String(),
			),
		)
	}

	return UintType
}

func (c *Checker) checkLenCall(
	scope *Scope,
	call *ast.CallExpr,
	argTypes []*Type,
) *Type {
	if call == nil {
		return UintType
	}

	if len(argTypes) != 1 {
		c.diags.Add(
			call.Span(),
			fmt.Sprintf(
				"len expects 1 argument, got %d",
				len(argTypes),
			),
		)
		return UintType
	}

	receiverType := argTypes[0]
	if receiverType == nil ||
		receiverType.Kind == TypeInvalid {
		return UintType
	}

	switch receiverType.Kind {
	case TypeString:
		c.lenResolutions[call] = LenResolution{
			Kind: LenResolutionString,
		}
		return UintType

	case TypeInlineArray:
		c.lenResolutions[call] = LenResolution{
			Kind: LenResolutionInlineArray,
		}

		return UintType

	case TypeCstring:
		c.lenResolutions[call] = LenResolution{
			Kind: LenResolutionCstring,
		}
		return UintType

	case TypeVariadic:
		c.lenResolutions[call] = LenResolution{
			Kind: LenResolutionVariadic,
		}
		return UintType
	}

	receiverPointer := &Type{
		Kind: TypePointer,
		Elem: receiverType,
	}

	resolution := c.resolveReceiverOverloadAt(
		scope,
		"len",
		[]*Type{
			receiverPointer,
		},
		func(taskType *Type) bool {
			return c.lenCandidateSignatureValid(
				taskType,
			)
		},
	)

	if resolution.Ambiguous {
		c.diags.Add(
			call.Span(),
			fmt.Sprintf(
				"ambiguous len overload for receiver %s",
				receiverType.String(),
			),
		)
		return UintType
	}

	if !resolution.HadOverload {
		c.diags.Add(
			call.Args[0].Span(),
			fmt.Sprintf(
				"len does not support %s",
				receiverType.String(),
			),
		)
		return UintType
	}

	if !resolution.Matched {
		c.diags.Add(
			call.Span(),
			fmt.Sprintf(
				"no len overload matches receiver %s",
				receiverType.String(),
			),
		)
		return UintType
	}

	if !c.isAddressableExpr(
		scope,
		call.Args[0],
	) {
		c.diags.Add(
			call.Args[0].Span(),
			"len overload requires an addressable receiver",
		)
	}

	c.lenResolutions[call] = LenResolution{
		Kind:             LenResolutionOverload,
		Candidate:        resolution.Candidate,
		TaskType:         resolution.TaskType,
		PackageName:      resolution.PackageName,
		GenericArguments: resolution.GenericArguments,
	}

	return c.resultTypeFromCall(
		resolution.TaskType,
		call.Span(),
	)
}

func (c *Checker) checkOverloadCall(sym *Symbol, argTypes []*Type, argSpans []source.Span, span source.Span) *Type {
	c.ensureOverloadSymbolPrepared(nil, sym)

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

func (c *Checker) checkSelectorExpr(
	scope *Scope,
	e *ast.SelectorExpr,
) *Type {
	if id, ok := e.Left.(*ast.IdentExpr); ok {
		sym := scope.Lookup(id.Name.Name)
		if sym != nil &&
			sym.Kind == SymbolPackage {
			return c.checkPackageSelectorExpr(
				sym,
				e,
			)
		}
	}

	leftType := c.checkExpr(
		scope,
		e.Left,
	)

	switch leftType.Kind {
	case TypeString:
		c.diags.Add(
			e.Name.Span(),
			fmt.Sprintf(
				"string has no accessible field %q",
				e.Name.Name,
			),
		)
		return InvalidType

	case TypeCstring:
		c.diags.Add(
			e.Name.Span(),
			fmt.Sprintf(
				"cstring has no accessible field %q",
				e.Name.Name,
			),
		)
		return InvalidType
	}

	if leftType.Kind == TypeInterface {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf(
				"interface method syntax is invalid; use %s(value, ...) instead",
				e.Name.Name,
			),
		)
		return InvalidType
	}

	if leftType.Kind == TypePointer {
		leftType = leftType.Elem
	}

	if leftType.Kind != TypeStruct &&
		leftType.Kind != TypeTypeParam {
		c.diags.Add(
			e.Span(),
			fmt.Sprintf(
				"cannot access field %q on non-struct type %s",
				e.Name.Name,
				leftType.String(),
			),
		)
		return InvalidType
	}

	for _, field := range leftType.Fields {
		if field.Name == e.Name.Name {
			return field.Type
		}
	}

	c.diags.Add(
		e.Name.Span(),
		fmt.Sprintf(
			"type %s has no field %q",
			leftType.String(),
			e.Name.Name,
		),
	)

	return InvalidType
}

func (c *Checker) checkIndexExpr(
	scope *Scope,
	e *ast.IndexExpr,
) *Type {
	receiverType := c.checkExpr(
		scope,
		e.Left,
	)
	indexType := c.checkExpr(
		scope,
		e.Index,
	)

	c.checkBracketIndexType(
		indexType,
		e.Index.Span(),
	)

	if receiverType == nil {
		return InvalidType
	}

	switch receiverType.Kind {
	case TypeInvalid:
		return InvalidType

	case TypeString:
		c.indexResolutions[e] = IndexResolution{
			Kind: IndexResolutionStringRead,
		}
		return CharType

	case TypeCstring:
		c.indexResolutions[e] = IndexResolution{
			Kind: IndexResolutionCstringRead,
		}
		return CharType

	case TypeInlineArray:
		if receiverType.Elem == nil {
			return InvalidType
		}

		c.indexResolutions[e] = IndexResolution{
			Kind: IndexResolutionInlineArrayRead,
		}

		return receiverType.Elem

	case TypeVariadic:
		if receiverType.Elem == nil {
			return InvalidType
		}

		c.indexResolutions[e] = IndexResolution{
			Kind: IndexResolutionVariadicRead,
		}
		return receiverType.Elem

	case TypeRawptr:
		c.indexResolutions[e] = IndexResolution{
			Kind: IndexResolutionRawptrRead,
		}
		return U8Type
	}

	if c.isPrimitiveByteIndexable(receiverType) {
		c.indexResolutions[e] = IndexResolution{
			Kind: IndexResolutionPrimitiveByteRead,
		}
		return U8Type
	}

	switch receiverType.Kind {
	case TypeStruct:
		if !c.isAddressableExpr(
			scope,
			e.Left,
		) {
			c.diags.Add(
				e.Left.Span(),
				"bracket operator [] requires an addressable receiver",
			)
		}

		receiverPointer := &Type{
			Kind: TypePointer,
			Elem: receiverType,
		}

		resolution := c.resolveBracketOverloadAt(
			scope,
			"[]",
			[]*Type{
				receiverPointer,
				IntType,
			},
		)

		if resolution.Ambiguous {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"ambiguous bracket operator [] for receiver %s and index int",
					receiverType.String(),
				),
			)
			return InvalidType
		}

		if !resolution.HadOverload {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"type %s does not define bracket operator []",
					receiverType.String(),
				),
			)
			return InvalidType
		}

		if !resolution.Matched {
			c.diags.Add(
				e.Span(),
				fmt.Sprintf(
					"no bracket operator [] matches receiver %s and index int",
					receiverType.String(),
				),
			)
			return InvalidType
		}

		c.indexResolutions[e] = IndexResolution{
			Kind:             IndexResolutionOverloadRead,
			Candidate:        resolution.Candidate,
			TaskType:         resolution.TaskType,
			PackageName:      resolution.PackageName,
			GenericArguments: resolution.GenericArguments,
		}

		return c.resultTypeFromCall(
			resolution.TaskType,
			e.Span(),
		)

	case TypeEnum:
		c.diags.Add(
			e.Left.Span(),
			fmt.Sprintf(
				"enum type %s cannot be indexed",
				receiverType.String(),
			),
		)

	case TypeUnion:
		c.diags.Add(
			e.Left.Span(),
			fmt.Sprintf(
				"union type %s cannot be indexed",
				receiverType.String(),
			),
		)

	case TypeInterface:
		c.diags.Add(
			e.Left.Span(),
			fmt.Sprintf(
				"interface type %s cannot be indexed",
				receiverType.String(),
			),
		)

	case TypePointer:
		c.diags.Add(
			e.Left.Span(),
			fmt.Sprintf(
				"typed pointer %s cannot be indexed",
				receiverType.String(),
			),
		)

	default:
		c.diags.Add(
			e.Left.Span(),
			fmt.Sprintf(
				"type %s cannot be indexed",
				receiverType.String(),
			),
		)
	}

	return InvalidType
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

	qualified := c.importedPackageMemberSymbol(pkgSym.Name, member)
	if qualified == nil {
		return InvalidType
	}

	return qualified.Type
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
	return c.typeFromAstContext(scope, typ, false)
}

func (c *Checker) typeFromInterfaceRequirementAst(
	scope *Scope,
	typ ast.Type,
) *Type {
	return c.typeFromAstContext(scope, typ, true)
}

func (c *Checker) typeFromAstContext(
	scope *Scope,
	typ ast.Type,
	allowInterfaceSelf bool,
) *Type {
	switch t := typ.(type) {
	case *ast.InterfaceSelfType:
		if !allowInterfaceSelf {
			c.diags.Add(
				t.Span(),
				`"self" type is only available inside interface requirements`,
			)

			return InvalidType
		}

		return &Type{
			Kind: TypeInterfaceSelf,
			Name: "self",
		}

	case *ast.InlineArrayType:
		elem := c.typeFromAstContext(
			scope,
			t.Elem,
			allowInterfaceSelf,
		)

		return c.inlineArrayTypeFromParts(
			scope,
			elem,
			t.Length,
			t.Span(),
		)

	case *ast.NamedType:
		if len(t.Parts) == 0 {
			return InvalidType
		}

		if len(t.Parts) > 1 {
			if len(t.Parts) != 2 {
				c.diags.Add(
					t.Span(),
					"package-qualified types currently support exactly one package and one member",
				)

				return InvalidType
			}

			first := t.Parts[0]
			member := t.Parts[1]

			sym := scope.Lookup(first.Name)
			if sym == nil {
				c.diags.Add(
					first.Span(),
					fmt.Sprintf(
						"undefined type or package %q",
						first.Name,
					),
				)

				return InvalidType
			}

			if sym.Kind != SymbolPackage {
				c.diags.Add(
					first.Span(),
					fmt.Sprintf(
						"%q is not a package",
						first.Name,
					),
				)

				return InvalidType
			}

			if sym.Package == nil {
				c.diags.Add(
					first.Span(),
					fmt.Sprintf(
						"package %q has no symbol table",
						first.Name,
					),
				)

				return InvalidType
			}

			memberSym := sym.Package.Symbols[member.Name]
			if memberSym == nil {
				c.diags.Add(
					member.Span(),
					fmt.Sprintf(
						"package %s has no type %q",
						first.Name,
						member.Name,
					),
				)

				return InvalidType
			}

			if memberSym.Kind != SymbolType {
				c.diags.Add(
					member.Span(),
					fmt.Sprintf(
						"package symbol %s.%s is not a type",
						first.Name,
						member.Name,
					),
				)

				return InvalidType
			}

			return c.qualifyImportedTypeForPackage(
				first.Name,
				memberSym.Type,
			)
		}

		name := t.Parts[0]

		sym := scope.Lookup(name.Name)
		if sym == nil {
			c.diags.Add(
				name.Span(),
				fmt.Sprintf(
					"undefined type %q",
					name.Name,
				),
			)

			return InvalidType
		}

		if sym.Kind != SymbolType {
			c.diags.Add(
				name.Span(),
				fmt.Sprintf(
					"%q is not a type",
					name.Name,
				),
			)

			return InvalidType
		}

		return sym.Type

	case *ast.PointerType:
		return &Type{
			Kind: TypePointer,
			Elem: c.typeFromAstContext(
				scope,
				t.Elem,
				allowInterfaceSelf,
			),
		}

	case *ast.GenericType:
		return c.typeFromGenericTypeAstContext(
			scope,
			t,
			allowInterfaceSelf,
		)
	}

	return InvalidType
}

func (c *Checker) typeFromGenericTypeAstContext(
	scope *Scope,
	t *ast.GenericType,
	allowInterfaceSelf bool,
) *Type {
	baseType := c.typeFromAstContext(scope, t.Base, allowInterfaceSelf)

	if baseType.Kind == TypeInvalid {
		return InvalidType
	}

	// Canonical interface requirements use *self directly. Generic occurrences
	// of self can be added later if the interface representation supports them.
	c.checkGenericArgsAgainstParams(
		scope,
		t.Args,
		baseType.GenericParams,
		t.Span(),
	)

	if len(baseType.GenericParams) == 0 {
		return baseType
	}

	switch baseType.Kind {
	case TypeStruct:
		decl, baseName := c.structDeclForGenericBase(
			scope,
			t.Base,
			baseType,
		)
		return c.specializeStructType(
			scope,
			t,
			baseType,
			decl,
			baseName,
		)

	case TypeInterface:
		return c.specializeInterfaceType(
			scope,
			t,
			baseType,
		)

	default:
		return baseType
	}
}

func (c *Checker) typeFromGenericTypeAst(
	scope *Scope,
	t *ast.GenericType,
) *Type {
	return c.typeFromGenericTypeAstContext(scope, t, false)
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

func (c *Checker) specializeInterfaceType(
	scope *Scope,
	gen *ast.GenericType,
	baseType *Type,
) *Type {
	if baseType == nil || baseType.Kind != TypeInterface {
		return InvalidType
	}

	if len(gen.Args) != len(baseType.GenericParams) {
		return InvalidType
	}

	baseName := baseType.Name
	name := c.specializedTypeName(baseName, gen.Args)

	if cached := c.specializedTypes[name]; cached != nil {
		return cached
	}

	typeSubst := c.genericTypeSubstFromArgs(
		scope,
		baseType.GenericParams,
		gen.Args,
	)

	argSubst := genericArgSubst(
		baseType.GenericParams,
		gen.Args,
	)

	typ := &Type{
		Kind:            TypeInterface,
		Name:            name,
		IsDynInterface:  baseType.IsDynInterface,
		GenericBaseName: baseName,
		GenericArguments: c.checkedGenericArguments(
			scope,
			baseType.GenericParams,
			gen.Args,
		),
	}

	// Cache before recursively cloning requirement types.
	c.specializedTypes[name] = typ

	for _, req := range baseType.InterfaceRequirements {
		cloned := InterfaceRequirementInfo{
			Name:            req.Name,
			ParamIsVariadic: append([]bool(nil), req.ParamIsVariadic...),
			IsPure:          req.IsPure,
			IsTrustedPure:   req.IsTrustedPure,
			Span:            req.Span,
		}

		for _, param := range req.Params {
			cloned.Params = append(
				cloned.Params,
				c.substituteGenericSignatureType(
					scope,
					param,
					typeSubst,
					argSubst,
				),
			)
		}

		for _, result := range req.Results {
			cloned.Results = append(
				cloned.Results,
				c.substituteGenericSignatureType(
					scope,
					result,
					typeSubst,
					argSubst,
				),
			)
		}

		typ.InterfaceRequirements = append(
			typ.InterfaceRequirements,
			cloned,
		)
	}

	return typ
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
		Kind:            TypeStruct,
		Name:            name,
		GenericParams:   nil,
		GenericBaseName: baseName,
		GenericArguments: c.checkedGenericArguments(
			scope,
			baseType.GenericParams,
			gen.Args,
		),
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

func (c *Checker) typeFromAstWithGenericArgs(
	scope *Scope,
	typ ast.Type,
	subst map[string]ast.GenericArg,
) *Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok := subst[t.Parts[0].Name]; ok {
				return c.typeFromGenericArg(
					scope,
					arg,
				)
			}
		}

		return c.typeFromAst(
			scope,
			t,
		)

	case *ast.PointerType:
		return &Type{
			Kind: TypePointer,
			Elem: c.typeFromAstWithGenericArgs(
				scope,
				t.Elem,
				subst,
			),
		}

	case *ast.InlineArrayType:
		elem := c.substituteTypeAst(
			t.Elem,
			subst,
		)

		length := c.substituteGenericExpr(
			t.Length,
			subst,
		)

		return c.typeFromAst(
			scope,
			&ast.InlineArrayType{
				Elem:   elem,
				Length: length,
				Loc:    t.Loc,
			},
		)

	case *ast.GenericType:
		args := make(
			[]ast.GenericArg,
			0,
			len(t.Args),
		)

		for _, arg := range t.Args {
			args = append(
				args,
				c.substituteGenericArg(
					arg,
					subst,
				),
			)
		}

		base := c.substituteTypeAst(
			t.Base,
			subst,
		)

		return c.typeFromAst(
			scope,
			&ast.GenericType{
				Base: base,
				Args: args,
				Loc:  t.Loc,
			},
		)
	}

	return c.typeFromAst(
		scope,
		typ,
	)
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

func (c *Checker) substituteTypeAst(
	typ ast.Type,
	subst map[string]ast.GenericArg,
) ast.Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok :=
				subst[t.Parts[0].Name]; ok {
				if argType :=
					genericArgAsTypeAst(arg); argType != nil {
					return argType
				}
			}
		}

		return t

	case *ast.InterfaceSelfType:
		return t

	case *ast.PointerType:
		return &ast.PointerType{
			Elem: c.substituteTypeAst(
				t.Elem,
				subst,
			),
			Loc: t.Loc,
		}

	case *ast.InlineArrayType:
		return &ast.InlineArrayType{
			Elem: c.substituteTypeAst(
				t.Elem,
				subst,
			),
			Length: c.substituteGenericExpr(
				t.Length,
				subst,
			),
			Loc: t.Loc,
		}

	case *ast.GenericType:
		args := make(
			[]ast.GenericArg,
			0,
			len(t.Args),
		)

		for _, arg := range t.Args {
			args = append(
				args,
				c.substituteGenericArg(
					arg,
					subst,
				),
			)
		}

		return &ast.GenericType{
			Base: c.substituteTypeAst(
				t.Base,
				subst,
			),
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

	case *ast.InlineArrayExpr:
		values := make(
			[]ast.Expr,
			0,
			len(e.Values),
		)

		for _, value := range e.Values {
			values = append(
				values,
				c.substituteGenericExpr(
					value,
					subst,
				),
			)
		}

		return &ast.InlineArrayExpr{
			Elem: c.substituteTypeAst(
				e.Elem,
				subst,
			),
			Length: c.substituteGenericExpr(
				e.Length,
				subst,
			),
			Values: values,
			Loc:    e.Loc,
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

func genericArgDisplay(
	arg ast.GenericArg,
) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return typeDisplay(
			arg.Type,
		)

	case ast.GenericArgExpr:
		if literal, ok :=
			arg.Expr.(*ast.IntLitExpr); ok {
			value, err := parseSealBigIntLiteral(
				literal.Value,
			)
			if err == nil {
				return value.String()
			}
		}

		return exprDisplay(
			arg.Expr,
		)
	}

	return "<invalid>"
}

func typeDisplay(
	typ ast.Type,
) string {
	switch t := typ.(type) {
	case *ast.NamedType:
		var parts []string

		for _, part := range t.Parts {
			parts = append(
				parts,
				part.Name,
			)
		}

		return strings.Join(
			parts,
			".",
		)

	case *ast.InterfaceSelfType:
		return "self"

	case *ast.PointerType:
		return "*" + typeDisplay(t.Elem)

	case *ast.InlineArrayType:
		return fmt.Sprintf(
			"@inline_array<%s, %s>",
			typeDisplay(t.Elem),
			exprDisplay(t.Length),
		)

	case *ast.GenericType:
		var args []string

		for _, arg := range t.Args {
			args = append(
				args,
				genericArgDisplay(arg),
			)
		}

		return typeDisplay(t.Base) +
			"<" +
			strings.Join(args, ", ") +
			">"
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

	case *ast.IndexExpr:
		return exprDisplay(e.Left) +
			"[" +
			exprDisplay(e.Index) +
			"]"

	case *ast.CompoundLiteralExpr:
		return typeDisplay(e.Type) + "{...}"

	case *ast.InlineArrayExpr:
		var values []string

		for _, value := range e.Values {
			values = append(
				values,
				exprDisplay(value),
			)
		}

		return fmt.Sprintf(
			"@inline_array<%s, %s>(%s)",
			typeDisplay(e.Elem),
			exprDisplay(e.Length),
			strings.Join(values, ", "),
		)
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

	case TypeInterfaceSelf:
		return t

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

func (c *Checker) checkedGenericArguments(
	scope *Scope,
	params []ast.GenericParam,
	args []ast.GenericArg,
) []GenericArgumentInfo {
	var out []GenericArgumentInfo

	for i, arg := range args {
		info := GenericArgumentInfo{
			Key: genericArgDisplay(arg),
		}

		if i < len(params) {
			info.Category = params[i].Category
		}

		switch info.Category {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			info.Type = c.typeFromGenericArg(scope, arg)

		case ast.GenericParamTask:
			info.Type = c.taskFromGenericArg(scope, arg)

		case ast.GenericParamInt,
			ast.GenericParamBool,
			ast.GenericParamString,
			ast.GenericParamValue:
			info.Type = c.valueFromGenericArg(scope, arg)
			info.Expr = arg.Expr

		default:
			info.Type = c.typeFromGenericArg(scope, arg)
		}

		out = append(out, info)
	}

	return out
}

func (c *Checker) checkGenericArgsAgainstParams(
	scope *Scope,
	args []ast.GenericArg,
	params []ast.GenericParam,
	span source.Span,
) {
	if len(params) == 0 {
		if len(args) > 0 {
			c.diags.Add(
				span,
				"non-generic symbol cannot receive generic arguments",
			)
		}

		return
	}

	if len(args) != len(params) {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"generic argument count mismatch: expected %d, got %d",
				len(params),
				len(args),
			),
		)
		return
	}

	subst := genericArgSubst(
		params,
		args,
	)

	/*
		Category and declared-type validation remains valid for symbolic
		specializations such as:

		    Slice<T>
		    StaticArray<T, Size>

		For example, T must still be a type argument and Size must still be an
		integer value argument.
	*/
	for i := range args {
		c.checkGenericArgAgainstParam(
			scope,
			args[i],
			params[i],
			subst,
		)
	}

	/*
		A nested specialization can legitimately contain parameters belonging
		to an enclosing generic declaration:

		    Array<T>
		    StaticArray<T, Size>
		    Deque<T>

		Its constraints cannot yet be evaluated because T and Size are
		placeholders, not concrete specialization arguments.

		The constraints will be checked again when the enclosing generic type
		or task is instantiated with concrete arguments.
	*/
	if c.genericArgumentsContainPlaceholders(
		scope,
		args,
	) {
		return
	}

	for i := range args {
		c.checkGenericArgConstraintsAgainstParam(
			scope,
			args[i],
			params[i],
			subst,
		)
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

func (c *Checker) enterGenericConstraintEval(name string, span source.Span) bool {
	maxDepth := c.options.GenericConstraintMaxDepth
	if maxDepth <= 0 {
		return true
	}

	for _, existing := range c.genericConstraintEvalStack {
		if existing == name {
			c.diags.Add(span, fmt.Sprintf("recursive generic constraint evaluation through %q", name))
			return false
		}
	}

	if len(c.genericConstraintEvalStack) >= maxDepth {
		c.diags.Add(span, fmt.Sprintf("generic constraint evaluation exceeded max depth %d while evaluating %q", maxDepth, name))
		return false
	}

	c.genericConstraintEvalStack = append(c.genericConstraintEvalStack, name)
	return true
}

func (c *Checker) exitGenericConstraintEval() {
	if c.options.GenericConstraintMaxDepth <= 0 {
		return
	}

	if len(c.genericConstraintEvalStack) == 0 {
		return
	}

	c.genericConstraintEvalStack = c.genericConstraintEvalStack[:len(c.genericConstraintEvalStack)-1]
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

			resolution := c.resolveImplAt(
				iface,
				actual,
				arg.Span(),
				nil,
				true,
			)

			switch resolution.Kind {
			case ImplResolutionAmbiguous:
				// The ambiguity diagnostic has already been emitted.

			case ImplResolutionNotFound:
				c.diags.Add(
					arg.Span(),
					fmt.Sprintf(
						"generic argument %s for %q must implement %s",
						actual.String(),
						param.Name.Name,
						iface.String(),
					),
				)

			case ImplResolutionFound:
				// Constraint satisfied.
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
						return c.qualifyImportedTypeForPackage(id.Name.Name, member.Type)
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

		if pkgName, ok := c.packageNameFromGenericExprBase(
			scope,
			e.Base,
		); ok {
			return c.taskTypeFromImportedGenericSignature(
				scope,
				pkgName,
				sym.Type,
				e.Args,
				e.Span(),
			)
		}

		taskDecl, _ := sym.Node.(*ast.TaskDecl)
		if taskDecl != nil {
			return c.taskTypeFromGenericCall(
				scope,
				taskDecl,
				e.Args,
			)
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

	return c.importedPackageMemberSymbol(id.Name.Name, member)
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

func (c *Checker) checkGenericArgIsCompileTimeValue(
	scope *Scope,
	arg ast.GenericArg,
	param ast.GenericParam,
) {
	if arg.Kind != ast.GenericArgExpr ||
		arg.Expr == nil {
		return
	}

	/*
		An enclosing generic value parameter is symbolically compile-time even
		though it has no concrete value at this stage.

		For example, Size in StaticArray<T, Size> is valid and will be replaced
		with a concrete compile-time value when the enclosing declaration is
		specialized.
	*/
	if c.genericExprReferencesPlaceholder(
		scope,
		arg.Expr,
	) {
		return
	}

	if _, ok := c.evalGenericConstExpr(
		scope,
		arg.Expr,
	); ok {
		return
	}

	if !c.isCompileTimeGenericExpr(
		scope,
		arg.Expr,
	) {
		c.diags.Add(
			arg.Span(),
			fmt.Sprintf(
				"generic parameter %q requires a compile-time value argument",
				param.Name.Name,
			),
		)
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
	genericConstStruct
)

type genericConstValue struct {
	Kind genericConstKind
	Type *Type

	BoolValue   bool
	IntValue    int64
	StringValue string

	StructFields map[string]genericConstValue
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

func (c *Checker) evalGenericConstExprWithEnv(
	scope *Scope,
	expr ast.Expr,
	env map[string]genericConstValue,
) (genericConstValue, bool) {
	if expr == nil {
		return genericConstValue{}, false
	}

	switch e := expr.(type) {
	case *ast.BoolLitExpr:
		return genericConstValue{
			Kind:      genericConstBool,
			Type:      BoolType,
			BoolValue: e.Value,
		}, true

	case *ast.IntLitExpr:
		value, err := parseSealIntLiteral(
			e.Value,
		)
		if err != nil {
			return genericConstValue{}, false
		}

		return genericConstValue{
			Kind:     genericConstInt,
			Type:     UntypedIntType,
			IntValue: value,
		}, true

	case *ast.StringLitExpr:
		value, err := unquoteSealLiteral(
			e.Value,
		)
		if err != nil {
			return genericConstValue{}, false
		}

		return genericConstValue{
			Kind:        genericConstString,
			Type:        StringType,
			StringValue: value,
		}, true

	case *ast.CompoundLiteralExpr:
		return c.evalGenericConstCompoundLiteral(
			scope,
			e,
			env,
		)

	case *ast.IdentExpr:
		if env != nil {
			if value, ok := env[e.Name.Name]; ok {
				return value, true
			}
		}

		sym := scope.Lookup(e.Name.Name)
		if sym == nil ||
			sym.Kind != SymbolConst {
			return genericConstValue{}, false
		}

		decl, ok := sym.Node.(*ast.ConstDecl)
		if !ok ||
			decl.Value == nil {
			return genericConstValue{}, false
		}

		return c.evalGenericConstExprWithEnv(
			scope,
			decl.Value,
			env,
		)

	case *ast.SelectorExpr:
		return c.evalGenericConstSelectorWithEnv(
			scope,
			e.Left,
			e.Name,
			env,
		)

	case *ast.UnaryExpr:
		value, ok := c.evalGenericConstExprWithEnv(
			scope,
			e.Expr,
			env,
		)
		if !ok {
			return genericConstValue{}, false
		}

		switch e.Op {
		case token.Bang:
			if value.Kind != genericConstBool {
				return genericConstValue{}, false
			}

			return genericConstValue{
				Kind:      genericConstBool,
				Type:      BoolType,
				BoolValue: !value.BoolValue,
			}, true

		case token.Minus:
			if value.Kind != genericConstInt {
				return genericConstValue{}, false
			}

			return genericConstValue{
				Kind:     genericConstInt,
				Type:     value.Type,
				IntValue: -value.IntValue,
			}, true
		}

		return genericConstValue{}, false

	case *ast.BinaryExpr:
		left, ok := c.evalGenericConstExprWithEnv(
			scope,
			e.Left,
			env,
		)
		if !ok {
			return genericConstValue{}, false
		}

		right, ok := c.evalGenericConstExprWithEnv(
			scope,
			e.Right,
			env,
		)
		if !ok {
			return genericConstValue{}, false
		}

		return c.evalGenericConstBinaryWithOverloads(
			scope,
			e.Op,
			left,
			right,
			e.Span(),
		)

	case *ast.CallExpr:
		return c.evalGenericConstCall(
			scope,
			e,
			env,
		)
	}

	return genericConstValue{}, false
}

func parseSealBigIntLiteral(
	literal string,
) (*big.Int, error) {
	normalized := strings.ReplaceAll(
		literal,
		"_",
		"",
	)

	if normalized == "" {
		return nil, fmt.Errorf(
			"empty integer literal",
		)
	}

	base := 10
	digits := normalized

	if strings.HasPrefix(normalized, "0x") ||
		strings.HasPrefix(normalized, "0X") {
		base = 16
		digits = normalized[2:]

		if digits == "" {
			return nil, fmt.Errorf(
				"hexadecimal literal requires at least one digit",
			)
		}
	}

	value := new(big.Int)

	if _, ok := value.SetString(digits, base); !ok {
		return nil, fmt.Errorf(
			"invalid base-%d integer literal %q",
			base,
			literal,
		)
	}

	return value, nil
}

func parseSealIntLiteral(
	literal string,
) (int64, error) {
	value, err := parseSealBigIntLiteral(
		literal,
	)
	if err != nil {
		return 0, err
	}

	if !value.IsInt64() {
		return 0, fmt.Errorf(
			"integer literal %q is outside the supported compile-time int64 range",
			literal,
		)
	}

	return value.Int64(), nil
}

func cloneBigInt(
	value *big.Int,
) *big.Int {
	if value == nil {
		return nil
	}

	return new(big.Int).Set(value)
}

func untypedIntConstantType(
	value *big.Int,
) *Type {
	return &Type{
		Kind:        TypeUntypedInt,
		Name:        "untyped int",
		IntConstant: cloneBigInt(value),
	}
}

func signedIntegerBounds(
	bits uint,
) (*big.Int, *big.Int) {
	limit := new(big.Int).Lsh(
		big.NewInt(1),
		bits-1,
	)

	minimum := new(big.Int).Neg(
		new(big.Int).Set(limit),
	)

	maximum := new(big.Int).Sub(
		new(big.Int).Set(limit),
		big.NewInt(1),
	)

	return minimum, maximum
}

func unsignedIntegerBounds(
	bits uint,
) (*big.Int, *big.Int) {
	minimum := big.NewInt(0)

	maximum := new(big.Int).Sub(
		new(big.Int).Lsh(
			big.NewInt(1),
			bits,
		),
		big.NewInt(1),
	)

	return minimum, maximum
}

func integerTypeBounds(
	t *Type,
) (*big.Int, *big.Int, bool) {
	if t == nil {
		return nil, nil, false
	}

	if t.Kind == TypeDistinct {
		return integerTypeBounds(
			t.Underlying,
		)
	}

	switch t.Kind {
	case TypeInt,
		TypeI64:
		minimum, maximum := signedIntegerBounds(64)
		return minimum, maximum, true

	case TypeI8:
		minimum, maximum := signedIntegerBounds(8)
		return minimum, maximum, true

	case TypeI16:
		minimum, maximum := signedIntegerBounds(16)
		return minimum, maximum, true

	case TypeI32:
		minimum, maximum := signedIntegerBounds(32)
		return minimum, maximum, true

	case TypeUint,
		TypeU64:
		minimum, maximum := unsignedIntegerBounds(64)
		return minimum, maximum, true

	case TypeU8:
		minimum, maximum := unsignedIntegerBounds(8)
		return minimum, maximum, true

	case TypeU16:
		minimum, maximum := unsignedIntegerBounds(16)
		return minimum, maximum, true

	case TypeU32:
		minimum, maximum := unsignedIntegerBounds(32)
		return minimum, maximum, true

	case TypeChar:
		return big.NewInt(0),
			big.NewInt(utf8.MaxRune),
			true

	default:
		return nil, nil, false
	}
}

func integerConstantFitsType(
	value *big.Int,
	dst *Type,
) bool {
	if value == nil || dst == nil {
		return true
	}

	if dst.Kind == TypeDistinct {
		return integerConstantFitsType(
			value,
			dst.Underlying,
		)
	}

	minimum, maximum, ok := integerTypeBounds(
		dst,
	)
	if !ok {
		return false
	}

	if value.Cmp(minimum) < 0 ||
		value.Cmp(maximum) > 0 {
		return false
	}

	if dst.Kind == TypeChar {
		if !value.IsInt64() {
			return false
		}

		return isUnicodeScalar(
			rune(value.Int64()),
		)
	}

	return true
}

func integerRangeDescription(
	t *Type,
) string {
	minimum, maximum, ok := integerTypeBounds(
		t,
	)
	if !ok {
		return t.String()
	}

	if t.Kind == TypeChar {
		return "Unicode scalar values U+0000 through U+10FFFF, excluding U+D800 through U+DFFF"
	}

	return fmt.Sprintf(
		"%s through %s",
		minimum.String(),
		maximum.String(),
	)
}

func integerConstantConversionAllowed(
	dst *Type,
	src *Type,
) bool {
	if dst == nil ||
		src == nil ||
		src.IntConstant == nil {
		return true
	}

	if !integerTypeAcceptsUntypedConstant(dst) {
		return true
	}

	return integerConstantFitsType(
		src.IntConstant,
		dst,
	)
}

func integerTypeAcceptsUntypedConstant(
	t *Type,
) bool {
	if t == nil {
		return false
	}

	if t.Kind == TypeDistinct {
		return integerTypeAcceptsUntypedConstant(
			t.Underlying,
		)
	}

	switch t.Kind {
	case TypeInt,
		TypeUint,
		TypeI8,
		TypeI16,
		TypeI32,
		TypeI64,
		TypeU8,
		TypeU16,
		TypeU32,
		TypeU64,
		TypeChar:
		return true

	default:
		return false
	}
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

func (c *Checker) evalGenericConstBinaryWithOverloads(scope *Scope, op token.Kind, left genericConstValue, right genericConstValue, span source.Span) (genericConstValue, bool) {
	if value, ok := evalGenericConstBuiltinBinary(op, left, right); ok {
		return value, true
	}

	if op == token.NotEq {
		if value, ok := c.evalGenericConstOperatorOverload(scope, "!=", left, right, span); ok {
			return value, true
		}

		value, ok := c.evalGenericConstOperatorOverload(scope, "==", left, right, span)
		if !ok {
			return genericConstValue{}, false
		}

		if value.Kind != genericConstBool {
			return genericConstValue{}, false
		}

		value.BoolValue = !value.BoolValue
		return value, true
	}

	return c.evalGenericConstOperatorOverload(scope, op.String(), left, right, span)
}

func (c *Checker) evalGenericConstOperatorOverload(scope *Scope, name string, left genericConstValue, right genericConstValue, span source.Span) (genericConstValue, bool) {
	if scope == nil {
		scope = c.global
	}

	argTypes := []*Type{left.Type, right.Type}
	lookups := c.overloadLookups(scope, name, argTypes)
	if len(lookups) == 0 {
		return genericConstValue{}, false
	}

	best := overloadResolution{
		Score: 1 << 30,
	}
	var bestScope *Scope

	for _, lookup := range lookups {
		c.ensureOverloadSymbolPrepared(lookup.Scope, lookup.Symbol)

		result := c.resolveOverload(lookup.Symbol.Overload, argTypes)
		if !result.Matched {
			continue
		}

		if result.Ambiguous {
			c.diags.Add(span, fmt.Sprintf("ambiguous generic constraint operator overload %q with operand types (%s)", name, c.formatTypes(argTypes)))
			return genericConstValue{}, false
		}

		if !best.Matched || result.Score < best.Score {
			best = result
			bestScope = lookup.Scope
			continue
		}

		if result.Score == best.Score {
			best.Ambiguous = true
		}
	}

	if !best.Matched {
		return genericConstValue{}, false
	}

	if best.Ambiguous {
		c.diags.Add(span, fmt.Sprintf("ambiguous generic constraint operator overload %q with operand types (%s)", name, c.formatTypes(argTypes)))
		return genericConstValue{}, false
	}

	candidate := best.Candidate
	c.ensureTaskSymbolPrepared(bestScope, candidate)

	if candidate == nil || candidate.Type == nil || candidate.Type.Kind != TypeTask {
		return genericConstValue{}, false
	}

	if !candidate.Type.IsPure && !candidate.Type.IsTrustedPure {
		c.diags.Add(span, fmt.Sprintf("generic constraint operator %q candidate %q must be pure", name, candidate.Name))
		return genericConstValue{}, false
	}

	if len(candidate.Type.Results) != 1 {
		c.diags.Add(span, fmt.Sprintf("generic constraint operator %q candidate %q must return exactly 1 value", name, candidate.Name))
		return genericConstValue{}, false
	}

	taskDecl, ok := candidate.Node.(*ast.TaskDecl)
	if !ok || taskDecl.Body == nil {
		c.diags.Add(span, fmt.Sprintf("generic constraint operator %q candidate %q cannot be evaluated because its body is unavailable", name, candidate.Name))
		return genericConstValue{}, false
	}

	evalName := candidate.Name
	if pkgName, ok := packageNameFromType(left.Type); ok {
		evalName = pkgName + "." + candidate.Name
	}

	value, ok := c.evalPureTaskConstBody(bestScope, evalName, span, taskDecl, []genericConstValue{left, right})
	if ok && candidate.Type != nil && len(candidate.Type.Results) == 1 {
		value.Type = candidate.Type.Results[0]
	}

	return value, ok
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
		value, ok := c.evalGenericConstCompoundLiteral(scope, r, env)
		if !ok {
			return genericConstValue{}, false
		}

		return genericConstSelectField(value, field.Name)

	case *ast.IdentExpr:
		if env != nil {
			if value, ok := env[r.Name.Name]; ok {
				return genericConstSelectField(value, field.Name)
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

func (c *Checker) evalGenericConstCompoundLiteral(scope *Scope, e *ast.CompoundLiteralExpr, env map[string]genericConstValue) (genericConstValue, bool) {
	if e == nil {
		return genericConstValue{}, false
	}

	litType := c.typeFromAst(scope, e.Type)
	if litType == nil || litType.Kind != TypeStruct {
		return genericConstValue{}, false
	}

	out := genericConstValue{
		Kind:         genericConstStruct,
		Type:         litType,
		StructFields: map[string]genericConstValue{},
	}

	for _, field := range e.Fields {
		value, ok := c.evalGenericConstExprWithEnv(scope, field.Value, env)
		if !ok {
			return genericConstValue{}, false
		}

		if fieldType := c.lookupField(litType, field.Name.Name); fieldType != nil {
			value.Type = fieldType
		}

		out.StructFields[field.Name.Name] = value
	}

	for i, valueExpr := range e.Values {
		if i >= len(litType.Fields) {
			return genericConstValue{}, false
		}

		value, ok := c.evalGenericConstExprWithEnv(scope, valueExpr, env)
		if !ok {
			return genericConstValue{}, false
		}

		value.Type = litType.Fields[i].Type
		out.StructFields[litType.Fields[i].Name] = value
	}

	return out, true
}

func genericConstSelectField(value genericConstValue, name string) (genericConstValue, bool) {
	if value.Kind != genericConstStruct {
		return genericConstValue{}, false
	}

	field, ok := value.StructFields[name]
	return field, ok
}

func (c *Checker) evalGenericConstCall(
	scope *Scope,
	e *ast.CallExpr,
	env map[string]genericConstValue,
) (genericConstValue, bool) {
	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		if id, ok := gen.Base.(*ast.IdentExpr); ok &&
			id.Name.Name == "cast" &&
			len(e.Args) == 1 {
			return c.evalGenericConstExprWithEnv(
				scope,
				e.Args[0],
				env,
			)
		}

		return genericConstValue{}, false
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok &&
		len(e.Args) == 1 {
		switch id.Name.Name {
		case "size":
			value, ok := c.evalGenericConstExprWithEnv(
				scope,
				e.Args[0],
				env,
			)
			if !ok ||
				value.Kind != genericConstString {
				return genericConstValue{}, false
			}

			return genericConstValue{
				Kind:     genericConstInt,
				Type:     UintType,
				IntValue: int64(len(value.StringValue)),
			}, true

		case "len":
			value, ok := c.evalGenericConstExprWithEnv(
				scope,
				e.Args[0],
				env,
			)
			if !ok ||
				value.Kind != genericConstString {
				return genericConstValue{}, false
			}

			return genericConstValue{
				Kind: genericConstInt,
				Type: UintType,
				IntValue: int64(
					utf8.RuneCountInString(
						value.StringValue,
					),
				),
			}, true
		}
	}

	sym, evalScope, evalName :=
		c.taskSymbolAndScopeFromConstCallCallee(
			scope,
			e.Callee,
		)

	if sym == nil {
		return genericConstValue{}, false
	}

	c.ensureTaskSymbolPrepared(
		evalScope,
		sym,
	)

	if sym.Type == nil ||
		sym.Type.Kind != TypeTask {
		return genericConstValue{}, false
	}

	if !sym.Type.IsPure &&
		!sym.Type.IsTrustedPure {
		c.diags.Add(
			e.Callee.Span(),
			fmt.Sprintf(
				"generic constraint call %q must be pure",
				sym.Name,
			),
		)
		return genericConstValue{}, false
	}

	if len(sym.Type.Results) != 1 {
		c.diags.Add(
			e.Callee.Span(),
			fmt.Sprintf(
				"generic constraint call %q must return exactly 1 value",
				sym.Name,
			),
		)
		return genericConstValue{}, false
	}

	var argValues []genericConstValue

	for _, arg := range e.Args {
		value, ok := c.evalGenericConstExprWithEnv(
			scope,
			arg,
			env,
		)
		if !ok {
			return genericConstValue{}, false
		}

		argValues = append(
			argValues,
			value,
		)
	}

	taskDecl, ok := sym.Node.(*ast.TaskDecl)
	if !ok ||
		taskDecl.Body == nil {
		c.diags.Add(
			e.Callee.Span(),
			fmt.Sprintf(
				"generic constraint call %q cannot be evaluated because its body is unavailable",
				sym.Name,
			),
		)
		return genericConstValue{}, false
	}

	if evalName == "" {
		evalName = sym.Name
	}

	value, ok := c.evalPureTaskConstBody(
		evalScope,
		evalName,
		e.Callee.Span(),
		taskDecl,
		argValues,
	)

	if ok &&
		sym.Type != nil &&
		len(sym.Type.Results) == 1 {
		value.Type = sym.Type.Results[0]
	}

	return value, ok
}

func (c *Checker) taskSymbolAndScopeFromConstCallCallee(scope *Scope, callee ast.Expr) (*Symbol, *Scope, string) {
	switch x := callee.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(x.Name.Name)
		if sym == nil || sym.Kind != SymbolTask {
			return nil, scope, ""
		}

		return sym, scope, sym.Name

	case *ast.SelectorExpr:
		id, ok := x.Left.(*ast.IdentExpr)
		if !ok {
			return nil, scope, ""
		}

		pkgSym := scope.Lookup(id.Name.Name)
		if pkgSym == nil || pkgSym.Kind != SymbolPackage || pkgSym.Package == nil {
			return nil, scope, ""
		}

		member := pkgSym.Package.Symbols[x.Name.Name]
		if member == nil || member.Kind != SymbolTask {
			return nil, scope, ""
		}

		evalScope := c.scopeForPackageInfo(id.Name.Name, pkgSym.Package)
		qualifiedMember := c.importedPackageMemberSymbol(id.Name.Name, member)

		return qualifiedMember, evalScope, id.Name.Name + "." + member.Name
	}

	return nil, scope, ""
}

func (c *Checker) evalPureTaskConstBody(scope *Scope, name string, span source.Span, d *ast.TaskDecl, args []genericConstValue) (genericConstValue, bool) {
	if d == nil || d.Body == nil {
		return genericConstValue{}, false
	}

	if !c.enterGenericConstraintEval(name, span) {
		return genericConstValue{}, false
	}
	defer c.exitGenericConstraintEval()

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

func normalizeSealQuotedLiteral(
	literal string,
) string {
	var out strings.Builder
	out.Grow(len(literal))

	for i := 0; i < len(literal); {
		if literal[i] != '\\' {
			out.WriteByte(literal[i])
			i++
			continue
		}

		if i+1 >= len(literal) {
			out.WriteByte(literal[i])
			i++
			continue
		}

		next := literal[i+1]

		// Seal accepts the conventional short null escape:
		//
		//     "\0"
		//
		// strconv.Unquote follows Go syntax, where an octal escape must
		// contain exactly three digits. Convert only the short form to
		// Go's hexadecimal spelling. Preserve escapes such as \000.
		if next == '0' {
			hasFollowingOctalDigit :=
				i+2 < len(literal) &&
					literal[i+2] >= '0' &&
					literal[i+2] <= '7'

			if !hasFollowingOctalDigit {
				out.WriteString(`\x00`)
				i += 2
				continue
			}
		}

		// Copy the complete escape pair. Consuming both bytes is important
		// for sequences such as "\\0", which denotes a backslash followed
		// by the ordinary character '0', not a null byte.
		out.WriteByte(literal[i])
		out.WriteByte(literal[i+1])
		i += 2
	}

	return out.String()
}

func unquoteSealLiteral(
	literal string,
) (string, error) {
	return strconv.Unquote(
		normalizeSealQuotedLiteral(literal),
	)
}

func evalGenericConstBuiltinBinary(op token.Kind, left genericConstValue, right genericConstValue) (genericConstValue, bool) {
	switch op {
	case token.AndAnd:
		if left.Kind == genericConstBool && right.Kind == genericConstBool {
			return genericConstValue{
				Kind:      genericConstBool,
				Type:      BoolType,
				BoolValue: left.BoolValue && right.BoolValue,
			}, true
		}

	case token.OrOr:
		if left.Kind == genericConstBool && right.Kind == genericConstBool {
			return genericConstValue{
				Kind:      genericConstBool,
				Type:      BoolType,
				BoolValue: left.BoolValue || right.BoolValue,
			}, true
		}

	case token.EqEq, token.NotEq:
		value, ok := genericConstEqual(left, right)
		if !ok {
			return genericConstValue{}, false
		}

		if op == token.NotEq {
			value = !value
		}

		return genericConstValue{
			Kind:      genericConstBool,
			Type:      BoolType,
			BoolValue: value,
		}, true

	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		if left.Kind == genericConstInt && right.Kind == genericConstInt {
			switch op {
			case token.Lt:
				return genericConstBoolValue(left.IntValue < right.IntValue), true
			case token.LtEq:
				return genericConstBoolValue(left.IntValue <= right.IntValue), true
			case token.Gt:
				return genericConstBoolValue(left.IntValue > right.IntValue), true
			case token.GtEq:
				return genericConstBoolValue(left.IntValue >= right.IntValue), true
			}
		}

		if left.Kind == genericConstString && right.Kind == genericConstString {
			switch op {
			case token.Lt:
				return genericConstBoolValue(left.StringValue < right.StringValue), true
			case token.LtEq:
				return genericConstBoolValue(left.StringValue <= right.StringValue), true
			case token.Gt:
				return genericConstBoolValue(left.StringValue > right.StringValue), true
			case token.GtEq:
				return genericConstBoolValue(left.StringValue >= right.StringValue), true
			}
		}

	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent:
		if left.Kind != genericConstInt || right.Kind != genericConstInt {
			return genericConstValue{}, false
		}

		switch op {
		case token.Plus:
			return genericConstIntValue(left.IntValue + right.IntValue), true
		case token.Minus:
			return genericConstIntValue(left.IntValue - right.IntValue), true
		case token.Star:
			return genericConstIntValue(left.IntValue * right.IntValue), true
		case token.Slash:
			if right.IntValue == 0 {
				return genericConstValue{}, false
			}

			return genericConstIntValue(left.IntValue / right.IntValue), true
		case token.Percent:
			if right.IntValue == 0 {
				return genericConstValue{}, false
			}

			return genericConstIntValue(left.IntValue % right.IntValue), true
		}
	}

	return genericConstValue{}, false
}

func genericConstBoolValue(value bool) genericConstValue {
	return genericConstValue{
		Kind:      genericConstBool,
		Type:      BoolType,
		BoolValue: value,
	}
}

func genericConstIntValue(value int64) genericConstValue {
	return genericConstValue{
		Kind:     genericConstInt,
		Type:     UntypedIntType,
		IntValue: value,
	}
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

func (c *Checker) checkAssignable(
	dst *Type,
	src *Type,
	span source.Span,
) {
	if dst == nil || src == nil {
		return
	}

	if dst.Kind == TypeInvalid ||
		src.Kind == TypeInvalid {
		return
	}

	if src.Kind == TypeUntypedInt &&
		src.IntConstant != nil &&
		integerTypeAcceptsUntypedConstant(dst) &&
		!integerConstantFitsType(
			src.IntConstant,
			dst,
		) {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"integer constant %s is outside the range of %s (%s)",
				src.IntConstant.String(),
				dst.String(),
				integerRangeDescription(dst),
			),
		)
		return
	}

	if c.sameType(dst, src) {
		return
	}

	if src.Kind == TypeNil {
		if c.typeAcceptsNil(dst) {
			return
		}

		c.diags.Add(
			span,
			fmt.Sprintf(
				"cannot assign nil to %s",
				dst.String(),
			),
		)
		return
	}

	if dst.Kind == TypeAny {
		return
	}

	if dst.Kind == TypeEnum &&
		src.Kind == TypeEnumLiteral {
		if !c.enumHasVariant(dst, src.Name) {
			c.diags.Add(
				span,
				fmt.Sprintf(
					"enum %s has no variant .%s",
					dst.String(),
					src.Name,
				),
			)
		}
		return
	}

	if src.Kind == TypeEnumLiteral {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"enum literal .%s needs contextual enum type",
				src.Name,
			),
		)
		return
	}

	if dst.Kind == TypeInterface {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"cannot assign %s to interface %s; use cast<%s>(value)",
				src.String(),
				dst.String(),
				dst.String(),
			),
		)
		return
	}

	if dst.Kind == TypeUnion {
		if c.unionHasMember(dst, src) {
			return
		}

		c.diags.Add(
			span,
			fmt.Sprintf(
				"cannot assign %s to union %s",
				src.String(),
				dst.String(),
			),
		)
		return
	}

	if !c.assignable(dst, src) {
		c.diags.Add(
			span,
			fmt.Sprintf(
				"cannot assign %s to %s",
				src.String(),
				dst.String(),
			),
		)
	}
}

func (c *Checker) isConcreteIntegerType(
	t *Type,
) bool {
	if t == nil {
		return false
	}

	if t.Kind == TypeDistinct {
		return c.isConcreteIntegerType(
			t.Underlying,
		)
	}

	switch t.Kind {
	case TypeInt,
		TypeUint,
		TypeI8,
		TypeI16,
		TypeI32,
		TypeI64,
		TypeU8,
		TypeU16,
		TypeU32,
		TypeU64,
		TypeChar:
		return true

	default:
		return false
	}
}

func (c *Checker) isConcreteNumericType(
	t *Type,
) bool {
	if t == nil {
		return false
	}

	if t.Kind == TypeDistinct {
		return c.isConcreteNumericType(
			t.Underlying,
		)
	}

	return c.isConcreteIntegerType(t) ||
		t.Kind == TypeF32 ||
		t.Kind == TypeF64
}

func (c *Checker) primitiveCastAllowed(
	dst *Type,
	src *Type,
) bool {
	if dst == nil || src == nil {
		return false
	}

	if dst.Kind == TypeInvalid ||
		src.Kind == TypeInvalid {
		return true
	}

	if c.sameType(dst, src) {
		return true
	}

	dstBase := dst
	if dstBase.Kind == TypeDistinct {
		dstBase = dstBase.Underlying
	}

	srcBase := src
	if srcBase.Kind == TypeDistinct {
		srcBase = srcBase.Underlying
	}

	if dstBase == nil || srcBase == nil {
		return false
	}

	if srcBase.Kind == TypeUntypedInt {
		return c.isConcreteNumericType(dstBase)
	}

	if srcBase.Kind == TypeUntypedFloat {
		return dstBase.Kind == TypeF32 ||
			dstBase.Kind == TypeF64
	}

	if c.isConcreteNumericType(dstBase) &&
		c.isConcreteNumericType(srcBase) {
		return true
	}

	if dstBase.Kind == TypeBool &&
		srcBase.Kind == TypeBool {
		return true
	}

	if dstBase.Kind == TypeRawptr {
		return srcBase.Kind == TypeRawptr ||
			srcBase.Kind == TypePointer
	}

	if dstBase.Kind == TypePointer {
		return srcBase.Kind == TypeRawptr ||
			srcBase.Kind == TypePointer
	}

	return false
}

func (c *Checker) checkConstantCastRange(
	targetType *Type,
	sourceType *Type,
	span source.Span,
) bool {
	if sourceType == nil ||
		sourceType.IntConstant == nil {
		return true
	}

	if !integerTypeAcceptsUntypedConstant(
		targetType,
	) {
		return true
	}

	if integerConstantFitsType(
		sourceType.IntConstant,
		targetType,
	) {
		return true
	}

	c.diags.Add(
		span,
		fmt.Sprintf(
			"integer constant %s is outside the range of %s (%s)",
			sourceType.IntConstant.String(),
			targetType.String(),
			integerRangeDescription(targetType),
		),
	)

	return false
}

func (c *Checker) typeAcceptsNil(t *Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypePointer,
		TypeRawptr,
		TypeUnion,
		TypeInterface:
		return true

	default:
		return false
	}
}

func (c *Checker) assignable(
	dst *Type,
	src *Type,
) bool {
	if dst == nil || src == nil {
		return true
	}

	if dst.Kind == TypeInvalid ||
		src.Kind == TypeInvalid {
		return true
	}

	if c.sameType(dst, src) {
		return true
	}

	if src.Kind == TypeNil {
		return c.typeAcceptsNil(dst)
	}

	if dst.Kind == TypeAny {
		return true
	}

	if dst.Kind == TypeDistinct {
		if src.Kind == TypeUntypedInt {
			return c.assignable(
				dst.Underlying,
				src,
			) &&
				integerConstantConversionAllowed(
					dst.Underlying,
					src,
				)
		}

		if src.Kind == TypeUntypedFloat {
			return c.assignable(
				dst.Underlying,
				src,
			)
		}

		return false
	}

	if src.Kind == TypeDistinct {
		return false
	}

	if dst.Kind == TypeEnum &&
		src.Kind == TypeEnumLiteral {
		return c.enumHasVariant(
			dst,
			src.Name,
		)
	}

	if dst.Kind == TypeInterface {
		return false
	}

	if dst.Kind == TypeUnion {
		return c.unionHasMember(
			dst,
			src,
		)
	}

	if src.Kind == TypeUntypedInt {
		if c.isIntegerLike(dst) {
			return integerConstantConversionAllowed(
				dst,
				src,
			)
		}

		return dst.Kind == TypeF32 ||
			dst.Kind == TypeF64
	}

	if src.Kind == TypeUntypedFloat {
		return dst.Kind == TypeF32 ||
			dst.Kind == TypeF64
	}

	return false
}

func (c *Checker) assignableEitherWay(a *Type, b *Type) bool {
	return c.assignable(a, b) || c.assignable(b, a)
}

func (c *Checker) sameType(
	a *Type,
	b *Type,
) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind == TypeInvalid ||
		b.Kind == TypeInvalid {
		return true
	}

	if a.Kind != b.Kind {
		return false
	}

	switch a.Kind {
	case TypePointer:
		return c.sameType(
			a.Elem,
			b.Elem,
		)

	case TypeInlineArray:
		if a.InlineLengthKnown !=
			b.InlineLengthKnown {
			return false
		}

		if a.InlineLengthKnown {
			if a.InlineLength !=
				b.InlineLength {
				return false
			}
		} else {
			if a.InlineLengthKey !=
				b.InlineLengthKey {
				return false
			}
		}

		return c.sameType(
			a.Elem,
			b.Elem,
		)

	case TypeVariadic:
		return c.sameType(
			a.Elem,
			b.Elem,
		)

	case TypeInterfaceSelf:
		return true

	case TypeStruct,
		TypeInterface:
		if a.GenericBaseName != "" ||
			b.GenericBaseName != "" {
			if a.GenericBaseName == "" ||
				b.GenericBaseName == "" ||
				a.GenericBaseName !=
					b.GenericBaseName ||
				len(a.GenericArguments) !=
					len(b.GenericArguments) {
				return false
			}

			for i := range a.GenericArguments {
				left := a.GenericArguments[i]
				right := b.GenericArguments[i]

				if left.Category !=
					right.Category {
					return false
				}

				switch {
				case isTypeGenericCategory(
					left.Category,
				):
					if !c.sameType(
						left.Type,
						right.Type,
					) {
						return false
					}

				case left.Category ==
					ast.GenericParamTask:
					if !c.sameType(
						left.Type,
						right.Type,
					) {
						return false
					}

				default:
					if left.Key != right.Key {
						return false
					}
				}
			}

			return true
		}

		return a.Name == b.Name

	case TypeDistinct,
		TypeEnum,
		TypeUnion,
		TypeTypeParam,
		TypeValueParam:
		return a.Name == b.Name

	case TypeTask:
		if len(a.Params) != len(b.Params) ||
			len(a.Results) != len(b.Results) {
			return false
		}

		for i := range a.Params {
			if !c.sameType(
				a.Params[i],
				b.Params[i],
			) {
				return false
			}
		}

		for i := range a.Results {
			if !c.sameType(
				a.Results[i],
				b.Results[i],
			) {
				return false
			}
		}

		return true

	default:
		return true
	}
}

func (c *Checker) defaultType(
	t *Type,
) *Type {
	if t == nil {
		return InvalidType
	}

	switch t.Kind {
	case TypeUntypedInt:
		if t.IntConstant != nil &&
			!integerConstantFitsType(
				t.IntConstant,
				IntType,
			) {
			return InvalidType
		}

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

func (c *Checker) isBracketIndexType(
	t *Type,
) bool {
	if t == nil ||
		t.Kind == TypeInvalid {
		return true
	}

	return t.Kind == TypeInt ||
		t.Kind == TypeUntypedInt
}

func (c *Checker) checkBracketIndexType(
	t *Type,
	span source.Span,
) bool {
	if c.isBracketIndexType(t) {
		return true
	}

	c.diags.Add(
		span,
		fmt.Sprintf(
			"bracket index must be int, got %s",
			t.String(),
		),
	)

	return false
}

func (c *Checker) isPrimitiveByteIndexable(
	t *Type,
) bool {
	if t == nil {
		return false
	}

	if t.Kind == TypeDistinct {
		return c.isPrimitiveByteIndexable(
			t.Underlying,
		)
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
		TypeChar:
		return true

	default:
		return false
	}
}

func (c *Checker) isAddressableExpr(
	scope *Scope,
	expr ast.Expr,
) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(
			e.Name.Name,
		)

		if sym == nil {
			return false
		}

		return sym.Kind == SymbolVar ||
			sym.Kind == SymbolParam

	case *ast.SelectorExpr:
		return c.isAddressableExpr(
			scope,
			e.Left,
		)

	case *ast.IndexExpr:
		resolution, ok :=
			c.indexResolutions[e]

		if !ok {
			return false
		}

		switch resolution.Kind {
		case IndexResolutionInlineArrayRead:
			/*
				An inline-array element occupies storage directly inside its
				parent array:

				    matrix[1]

				is addressable exactly when:

				    matrix

				is addressable.
			*/
			return c.isAddressableExpr(
				scope,
				e.Left,
			)

		case IndexResolutionVariadicRead:
			/*
				A variadic element refers to actual variadic storage.
			*/
			return true

		case IndexResolutionRawptrRead:
			/*
				Raw-pointer indexing dereferences actual memory.
			*/
			return true

		case IndexResolutionPrimitiveByteRead:
			/*
				Primitive byte indexing refers to a byte inside the original
				primitive value, so the base value must itself be addressable.
			*/
			return c.isAddressableExpr(
				scope,
				e.Left,
			)

		default:
			/*
				This deliberately rejects:

				    string[index]
				    cstring[index]
				    overloaded[index]

				String-like reads are immutable, while an overloaded [] may
				return a temporary task result rather than direct storage.
			*/
			return false
		}

	case *ast.UnaryExpr:
		return e.Op == token.Star
	}

	return false
}

func (c *Checker) isMutableAddressableExpr(
	scope *Scope,
	expr ast.Expr,
) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		sym := scope.Lookup(
			e.Name.Name,
		)

		return sym != nil &&
			sym.Kind == SymbolVar

	case *ast.SelectorExpr:
		return c.isMutableAddressableExpr(
			scope,
			e.Left,
		)

	case *ast.IndexExpr:
		resolution, ok :=
			c.indexResolutions[e]

		if !ok {
			return false
		}

		switch resolution.Kind {
		case IndexResolutionInlineArrayRead:
			/*
				Mutability propagates through inline-array indexing:

				    matrix[0]

				is mutable when matrix itself is mutable.
			*/
			return c.isMutableAddressableExpr(
				scope,
				e.Left,
			)

		case IndexResolutionVariadicRead:
			/*
				Variadic elements are writable according to the existing
				checkIndexAssignment semantics.
			*/
			return true

		case IndexResolutionRawptrRead:
			/*
				Raw pointers refer to mutable byte-addressable memory.
			*/
			return true

		case IndexResolutionPrimitiveByteRead:
			return c.isMutableAddressableExpr(
				scope,
				e.Left,
			)

		default:
			/*
				Do not consider overloaded [] results mutable automatically.
				The overload may return a value rather than a reference to
				storage.
			*/
			return false
		}

	case *ast.UnaryExpr:
		return e.Op == token.Star
	}

	return false
}

func (c *Checker) isValidEnumUnderlying(
	t *Type,
) bool {
	if t == nil {
		return false
	}

	switch t.Kind {
	case TypeInt,
		TypeUint,
		TypeI8,
		TypeI16,
		TypeI32,
		TypeI64,
		TypeU8,
		TypeU16,
		TypeU32,
		TypeU64:
		return true

	default:
		return false
	}
}

func maxEnumVariantCount(
	t *Type,
) (uint64, bool) {
	if t == nil {
		return 0, false
	}

	// Enum values are assigned from zero upward. Signed representations can
	// therefore use only their non-negative range.
	switch t.Kind {
	case TypeI8:
		return uint64(1) << 7, true

	case TypeI16:
		return uint64(1) << 15, true

	case TypeI32:
		return uint64(1) << 31, true

	case TypeI64:
		return uint64(1) << 63, true

	case TypeU8:
		return uint64(1) << 8, true

	case TypeU16:
		return uint64(1) << 16, true

	case TypeU32:
		return uint64(1) << 32, true

	case TypeInt,
		TypeUint,
		TypeU64:
		// int and uint depend on the selected target ABI. u64 can represent
		// more values than a Go uint64 count can express as a variant count.
		// These limits are not practically reachable by a source file, so no
		// checker-side count limit is necessary here.
		return 0, false

	default:
		return 0, false
	}
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

func (c *Checker) prepareEnumDecl(
	parent *Scope,
	d *ast.EnumDecl,
) {
	if d == nil {
		return
	}

	sym := parent.LookupLocal(d.Name.Name)
	if sym == nil || sym.Type == nil {
		return
	}

	// Preserve the existing untyped enum behavior by using int when no
	// representation is explicitly selected.
	underlying := IntType

	if d.Underlying != nil {
		underlying = c.typeFromAst(
			parent,
			d.Underlying,
		)

		if underlying != nil &&
			underlying.Kind != TypeInvalid &&
			!c.isValidEnumUnderlying(underlying) {
			c.diags.Add(
				d.Underlying.Span(),
				fmt.Sprintf(
					"enum underlying type must be a builtin integer type, got %s",
					underlying.String(),
				),
			)

			underlying = InvalidType
		}
	}

	sym.Type.Underlying = underlying

	var variants []EnumVariantInfo

	for _, variant := range d.Variants {
		variants = append(
			variants,
			EnumVariantInfo{
				Name: variant.Name,
				Span: variant.Span(),
			},
		)
	}

	sym.Type.Variants = variants

	maxVariants, limited := maxEnumVariantCount(
		underlying,
	)

	if limited &&
		uint64(len(variants)) > maxVariants {
		diagnosticSpan := d.Name.Span()

		if d.Underlying != nil {
			diagnosticSpan = d.Underlying.Span()
		}

		c.diags.Add(
			diagnosticSpan,
			fmt.Sprintf(
				"enum %s has %d variants, but underlying type %s can represent only %d auto-assigned values starting at 0",
				d.Name.Name,
				len(variants),
				underlying.String(),
				maxVariants,
			),
		)
	}
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

	member = c.importedPackageMemberSymbol(pkgSym.Name, member)

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
	c.ensureOverloadSymbolPrepared(nil, sym)

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
				key = "int:" + integerLiteralIdentity(
					lit.Value,
				)
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

func integerLiteralIdentity(
	literal string,
) string {
	value, err := parseSealBigIntLiteral(
		literal,
	)
	if err != nil {
		return literal
	}

	return value.String()
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
	case "+",
		"-",
		"*",
		"/",
		"%",
		"==",
		"!=",
		"<",
		">",
		"<=",
		">=",
		"&",
		"|",
		"^",
		"[]",
		"[]=":
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

	for _, impl := range scope.Impls {
		if impl == nil || !impl.Usable {
			continue
		}

		info.Impls = append(
			info.Impls,
			exportImplInfo(impl),
		)
	}

	return info
}

func exportImplInfo(info *ImplInfo) *ImplInfo {
	if info == nil {
		return nil
	}

	out := *info

	out.Scope = nil
	out.DelegatesTo = nil
	out.UsingSteps = nil
	out.Checking = false

	out.Interface = cloneExportType(
		info.Interface,
		map[*Type]*Type{},
	)
	out.Target = cloneExportType(
		info.Target,
		map[*Type]*Type{},
	)

	out.GenericParams = append(
		[]ast.GenericParam(nil),
		info.GenericParams...,
	)
	out.UsingPath = append(
		[]string(nil),
		info.UsingPath...,
	)

	out.Entries = map[string]*ImplEntryInfo{}

	for name, entry := range info.Entries {
		if entry == nil {
			continue
		}

		cloned := *entry

		if entry.TaskSymbol != nil {
			// Impl task bodies must remain available to codegen and generic
			// specialization in importing packages.
			task := *entry.TaskSymbol

			if task.Type != nil {
				task.Type = cloneExportType(
					task.Type,
					map[*Type]*Type{},
				)
			}

			cloned.TaskSymbol = &task
		}

		out.Entries[name] = &cloned
	}

	return &out
}

func exportSymbolSignatureOnly(sym *Symbol) *Symbol {
	if sym == nil {
		return nil
	}

	out := *sym

	// Imported tasks and types must be type-checkable from exported typed
	// signatures, not from source bodies/declarations. Generic struct fields
	// keep their field TypeAst through FieldInfo.
	if out.Kind == SymbolType {
		out.Node = nil
	}

	if out.Kind == SymbolTask {
		keepBody := false

		if out.Type != nil {
			keepBody =
				len(out.Type.GenericParams) > 0 ||
					out.Type.IsPure ||
					out.Type.IsTrustedPure
		}

		if !keepBody {
			out.Node = nil
		}
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

func (c *Checker) scopeForPackageInfo(packageName string, pkg *PackageInfo) *Scope {
	if pkg == nil {
		return c.global
	}

	key := packageName
	if key == "" {
		key = pkg.Name
	}

	if key != "" {
		if cached := c.packageScopes[key]; cached != nil {
			return cached
		}
	}

	scope := NewScope(nil)
	c.declareBuiltins(scope)
	c.declarePackages(scope)

	for name, sym := range pkg.Symbols {
		if sym == nil {
			continue
		}

		scope.Declare(&Symbol{
			Name:     name,
			Kind:     sym.Kind,
			Type:     sym.Type,
			Span:     sym.Span,
			Node:     sym.Node,
			Overload: sym.Overload,
			Builtin:  sym.Builtin,
			Package:  sym.Package,
		})
	}

	if key != "" {
		c.packageScopes[key] = scope
	}

	return scope
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
	out.GenericArguments = nil

	for _, arg := range t.GenericArguments {
		cloned := arg
		cloned.Type = cloneExportType(arg.Type, seen)

		out.GenericArguments = append(
			out.GenericArguments,
			cloned,
		)
	}

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
			Name:            req.Name,
			ParamIsVariadic: append([]bool(nil), req.ParamIsVariadic...),
			IsPure:          req.IsPure,
			IsTrustedPure:   req.IsTrustedPure,
			Span:            req.Span,
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
