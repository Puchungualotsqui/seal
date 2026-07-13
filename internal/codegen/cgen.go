package cgen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

type CType struct {
	Name     string
	SealName string

	IsVariadic     bool
	IsInterface    bool
	IsDynInterface bool
	Elem           *CType
}

func (t CType) String() string {
	if t.IsVariadic && t.Elem != nil {
		return "..." + t.Elem.String()
	}

	return t.Name
}

func (t CType) Decl(name string) string {
	return fmt.Sprintf("%s %s", t.Name, name)
}

var (
	CInvalid = CType{Name: "/*invalid*/ int", SealName: "<invalid>"}

	// Internal C-only helpers.
	CVoid       = CType{Name: "void", SealName: "void"}
	CMainReturn = CType{Name: "int", SealName: "__c_main_return"}

	CBool = CType{Name: "bool", SealName: "bool"}

	CInt  = CType{Name: "intptr_t", SealName: "int"}
	CUint = CType{Name: "uintptr_t", SealName: "uint"}

	CI8  = CType{Name: "int8_t", SealName: "i8"}
	CI16 = CType{Name: "int16_t", SealName: "i16"}
	CI32 = CType{Name: "int32_t", SealName: "i32"}
	CI64 = CType{Name: "int64_t", SealName: "i64"}

	CU8  = CType{Name: "uint8_t", SealName: "u8"}
	CU16 = CType{Name: "uint16_t", SealName: "u16"}
	CU32 = CType{Name: "uint32_t", SealName: "u32"}
	CU64 = CType{Name: "uint64_t", SealName: "u64"}

	CF32 = CType{Name: "float", SealName: "f32"}
	CF64 = CType{Name: "double", SealName: "f64"}

	CChar       = CType{Name: "uint32_t", SealName: "char"}
	CRawptr     = CType{Name: "void *", SealName: "rawptr"}
	CAny        = CType{Name: "sealAny", SealName: "any"}
	CSealString = CType{Name: "sealString", SealName: "string"}
	CCString    = CType{Name: "const char *", SealName: "cstring"}
	CNil        = CType{Name: "void *", SealName: "nil"}
)

type valueInfo struct {
	Type CType
}

type loopControl struct {
	scope         *scope
	breakLabel    string
	continueLabel string
}

type deferredAction struct {
	// Call contains a fully prepared C call. Call-form defer arguments are
	// evaluated and captured when the defer statement is encountered.
	Call string

	// Body is emitted when the containing scope exits. Expressions inside a
	// block-form defer are therefore evaluated at scope-exit time.
	Body *ast.BlockStmt
}

type scope struct {
	parent *scope
	vars   map[string]valueInfo
	defers []deferredAction
}

func newScope(parent *scope) *scope {
	return &scope{
		parent: parent,
		vars:   map[string]valueInfo{},
	}
}

func (s *scope) addDeferredCall(code string) {
	if s == nil || code == "" {
		return
	}

	s.defers = append(
		s.defers,
		deferredAction{
			Call: code,
		},
	)
}

func (s *scope) addDeferredBlock(body *ast.BlockStmt) {
	if s == nil || body == nil {
		return
	}

	s.defers = append(
		s.defers,
		deferredAction{
			Body: body,
		},
	)
}

func (s *scope) declare(name string, typ CType) {
	s.vars[name] = valueInfo{Type: typ}
}

func (s *scope) lookup(name string) (valueInfo, bool) {
	for current := s; current != nil; current = current.parent {
		if v, ok := current.vars[name]; ok {
			return v, true
		}
	}

	return valueInfo{}, false
}

type ImportedGenericTaskInstance struct {
	PackageName string
	TaskName    string
	Name        string
	Info        TaskInfo
	Args        []ast.GenericArg
}

type TaskInfo struct {
	Decl          *ast.TaskDecl
	GenericParams []ast.GenericParam

	ParamTypeAsts  []ast.Type
	ResultTypeAsts []ast.Type
	ParamNames     []string

	ReturnType  CType
	ReturnTypes []CType
	ParamTypes  []CType

	RequiredParams  int
	ParamDefaults   []ast.Expr
	ParamHasDefault []bool
	ParamIsVariadic []bool
	IsVariadic      bool

	IsExtern   bool
	ExternName string

	IsPure        bool
	IsIntrinsic   bool
	IsTrustedPure bool
}

type PackageInfo struct {
	Name      string
	Tasks     map[string]TaskInfo
	Overloads map[string][]string

	Structs    map[string]*ast.StructDecl
	Distincts  map[string]*ast.DistinctDecl
	Enums      map[string]*ast.EnumDecl
	Unions     map[string]*ast.UnionDecl
	Interfaces map[string]*ast.InterfaceDecl

	Impls []*ast.ImplDecl
}

type externalConcreteType struct {
	PackageName string
	TypeName    string
}

type InterfaceInstance struct {
	// Stable semantic identity, for example:
	//
	//   Positioned
	//   Reader<int>
	//   io.Reader<int>
	Key string

	// Generated C name:
	//
	//   Positioned
	//   Reader_int
	//   io_Reader_int
	CName string

	PackageName string
	BaseName    string

	Decl *ast.InterfaceDecl
	Args []ast.GenericArg

	// Interface generic parameters substituted with this instance's args.
	Subst map[string]ast.GenericArg

	IsDyn bool
}

type ImplTemplate struct {
	PackageName string
	Decl        *ast.ImplDecl

	GenericParams []ast.GenericParam

	Interface ast.Type
	Target    ast.Type

	Entries map[string]ast.ImplEntry

	UsingPath []ast.Ident
}

type ResolvedImplInstance struct {
	Key string

	Template  *ImplTemplate
	Interface *InterfaceInstance

	// Concrete target after generic matching.
	Target CType

	// Substitution inferred from the interface and target patterns.
	//
	// Reader<T> + Box<T>, matched against Reader<int> + Box<int>:
	//
	//     T -> int
	Subst map[string]ast.GenericArg

	// Set for `using` implementations.
	Delegated *ResolvedImplInstance

	// Concrete field path reached by `using`.
	UsingPath []ast.Ident
}

type GenericStructInstance struct {
	Name string
	Decl *ast.StructDecl
	Args []ast.GenericArg
}

type ImportedGenericStructInstance struct {
	PackageName string
	TypeName    string
	Name        string
	Decl        *ast.StructDecl
	Args        []ast.GenericArg
}

type GenericTaskInstance struct {
	Name string
	Decl *ast.TaskDecl
	Args []ast.GenericArg
}

type Generator struct {
	diags *diag.Reporter

	out    strings.Builder
	indent int

	packageName string

	// packages contains the packages visible as source-level dependencies of the
	// package currently being generated.
	packages map[string]*PackageInfo

	// workspacePackages contains metadata for every checked package in the
	// workspace. It is used only to materialize concrete types carried into an
	// owning package by cross-package generic-specialization requests.
	//
	// For example:
	//
	//	app calls mem.NewC<Point>()
	//	mem receives the specialization argument app.Point
	//
	// mem is not allowed to access app as a source dependency, but CGen still
	// needs app's type metadata to emit sizeof(app_Point).
	workspacePackages map[string]*PackageInfo

	requiredExternalTypes        map[string]externalConcreteType
	emittedRequiredExternalTypes map[string]bool

	indexResolutions map[*ast.IndexExpr]checker.IndexResolution
	lenResolutions   map[*ast.CallExpr]checker.LenResolution

	genericOverloadCalls map[*ast.GenericExpr]checker.GenericOverloadCallResolution

	genericInstanceRequests        *GenericInstanceRequestSet
	pendingGenericInstanceRequests []GenericInstanceRequest

	structs    map[string]*ast.StructDecl
	enums      map[string]*ast.EnumDecl
	unions     map[string]*ast.UnionDecl
	interfaces map[string]*ast.InterfaceDecl

	genericStructs        map[string]*GenericStructInstance
	emittedGenericStructs map[string]bool
	genericTasks          map[string]*GenericTaskInstance
	emittedGenericTasks   map[string]bool

	importedGenericStructs        map[string]*ImportedGenericStructInstance
	emittedImportedGenericStructs map[string]bool

	typeContextPackage string

	genericSubst         map[string]ast.GenericArg
	importedGenericTasks map[string]*ImportedGenericTaskInstance

	tasks     map[string]TaskInfo
	overloads map[string][]string
	consts    map[string]CType

	emittedVariadics map[string]bool

	scope     *scope
	taskScope *scope

	loopStack []loopControl

	currentTask                 *ast.TaskDecl
	currentGenericTaskName      string
	currentResults              []CType
	currentReturnStructOverride *CType

	tempCounter int

	distincts map[string]*ast.DistinctDecl

	interfaceInstances map[string]*InterfaceInstance

	implTemplates []*ImplTemplate

	resolvedImpls  map[string]*ResolvedImplInstance
	resolvingImpls map[string]bool

	implDecls []*ast.ImplDecl
}

func New(diags *diag.Reporter) *Generator {
	return NewWithPackagesAndSemanticInfo(
		diags,
		"",
		nil,
		checker.SemanticInfo{},
	)
}

func NewWithSemanticInfo(
	diags *diag.Reporter,
	semantic checker.SemanticInfo,
) *Generator {
	return NewWithPackagesAndSemanticInfo(
		diags,
		"",
		nil,
		semantic,
	)
}

func NewWithPackages(
	diags *diag.Reporter,
	packageName string,
	packages map[string]*PackageInfo,
) *Generator {
	return NewWithPackagesAndSemanticInfo(
		diags,
		packageName,
		packages,
		checker.SemanticInfo{},
	)
}

func NewWithPackagesAndSemanticInfo(
	diags *diag.Reporter,
	packageName string,
	packages map[string]*PackageInfo,
	semantic checker.SemanticInfo,
) *Generator {
	return &Generator{
		diags:                         diags,
		packageName:                   packageName,
		packages:                      packages,
		workspacePackages:             packages,
		requiredExternalTypes:         map[string]externalConcreteType{},
		emittedRequiredExternalTypes:  map[string]bool{},
		indexResolutions:              cloneIndexResolutions(semantic.IndexResolutions),
		lenResolutions:                cloneLenResolutions(semantic.LenResolutions),
		genericInstanceRequests:       NewGenericInstanceRequestSet(),
		structs:                       map[string]*ast.StructDecl{},
		enums:                         map[string]*ast.EnumDecl{},
		unions:                        map[string]*ast.UnionDecl{},
		interfaces:                    map[string]*ast.InterfaceDecl{},
		genericStructs:                map[string]*GenericStructInstance{},
		emittedGenericStructs:         map[string]bool{},
		importedGenericTasks:          map[string]*ImportedGenericTaskInstance{},
		genericTasks:                  map[string]*GenericTaskInstance{},
		emittedGenericTasks:           map[string]bool{},
		genericOverloadCalls:          cloneGenericOverloadCalls(semantic.GenericOverloadCalls),
		importedGenericStructs:        map[string]*ImportedGenericStructInstance{},
		emittedImportedGenericStructs: map[string]bool{},
		tasks:                         map[string]TaskInfo{},
		overloads:                     map[string][]string{},
		consts:                        map[string]CType{},
		emittedVariadics:              map[string]bool{},
		distincts:                     map[string]*ast.DistinctDecl{},
		interfaceInstances:            map[string]*InterfaceInstance{},
		implTemplates:                 nil,
		resolvedImpls:                 map[string]*ResolvedImplInstance{},
		resolvingImpls:                map[string]bool{},
		implDecls:                     nil,
	}
}

func (g *Generator) SetWorkspacePackages(
	packages map[string]*PackageInfo,
) {
	if packages == nil {
		g.workspacePackages = map[string]*PackageInfo{}
		return
	}

	g.workspacePackages = packages
}

func (g *Generator) typePackageInfo(
	packageName string,
) *PackageInfo {
	if packageName == "" {
		return nil
	}

	if pkg := g.packages[packageName]; pkg != nil {
		return pkg
	}

	return g.workspacePackages[packageName]
}

func cloneIndexResolutions(
	input map[*ast.IndexExpr]checker.IndexResolution,
) map[*ast.IndexExpr]checker.IndexResolution {
	if len(input) == 0 {
		return map[*ast.IndexExpr]checker.IndexResolution{}
	}

	out := make(
		map[*ast.IndexExpr]checker.IndexResolution,
		len(input),
	)

	for expr, resolution := range input {
		out[expr] = resolution
	}

	return out
}

func cloneLenResolutions(
	input map[*ast.CallExpr]checker.LenResolution,
) map[*ast.CallExpr]checker.LenResolution {
	if len(input) == 0 {
		return map[*ast.CallExpr]checker.LenResolution{}
	}

	out := make(
		map[*ast.CallExpr]checker.LenResolution,
		len(input),
	)

	for call, resolution := range input {
		out[call] = resolution
	}

	return out
}

func cloneGenericOverloadCalls(
	input map[*ast.GenericExpr]checker.GenericOverloadCallResolution,
) map[*ast.GenericExpr]checker.GenericOverloadCallResolution {
	if len(input) == 0 {
		return map[*ast.GenericExpr]checker.GenericOverloadCallResolution{}
	}

	out := make(
		map[*ast.GenericExpr]checker.GenericOverloadCallResolution,
		len(input),
	)

	for expr, resolution := range input {
		out[expr] = resolution
	}

	return out
}

func semanticCandidateIdentity(
	candidate *checker.Symbol,
	packageName string,
) (string, string) {
	if candidate == nil {
		return "", ""
	}

	taskName := candidate.Name

	if dot := strings.LastIndex(taskName, "."); dot >= 0 {
		if packageName == "" {
			packageName = taskName[:dot]
		}

		taskName = taskName[dot+1:]
	}

	return packageName, taskName
}

func namedTypeFromCheckerName(name string) ast.Type {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	rawParts := strings.Split(name, ".")
	parts := make([]ast.Ident, 0, len(rawParts))

	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		parts = append(parts, ast.Ident{Name: part})
	}

	if len(parts) == 0 {
		return nil
	}

	return &ast.NamedType{
		Parts: parts,
	}
}

func checkerTypeToAstType(typ *checker.Type) ast.Type {
	if typ == nil {
		return nil
	}

	switch typ.Kind {
	case checker.TypePointer:
		elem := checkerTypeToAstType(typ.Elem)
		if elem == nil {
			return nil
		}

		return &ast.PointerType{
			Elem: elem,
		}

	case checker.TypeStruct,
		checker.TypeInterface:
		if typ.GenericBaseName != "" {
			base := namedTypeFromCheckerName(typ.GenericBaseName)
			if base == nil {
				return nil
			}

			args := checkerGenericArgumentsToAst(
				typ.GenericArguments,
			)

			return &ast.GenericType{
				Base: base,
				Args: args,
			}
		}

		return namedTypeFromCheckerName(typ.Name)

	case checker.TypeDistinct,
		checker.TypeEnum,
		checker.TypeUnion,
		checker.TypeTypeParam,
		checker.TypeValueParam:
		return namedTypeFromCheckerName(typ.Name)

	case checker.TypeVoid:
		return namedTypeFromCheckerName("void")

	case checker.TypeBool:
		return namedTypeFromCheckerName("bool")

	case checker.TypeInt,
		checker.TypeUntypedInt:
		return namedTypeFromCheckerName("int")

	case checker.TypeUint:
		return namedTypeFromCheckerName("uint")

	case checker.TypeI8:
		return namedTypeFromCheckerName("i8")

	case checker.TypeI16:
		return namedTypeFromCheckerName("i16")

	case checker.TypeI32:
		return namedTypeFromCheckerName("i32")

	case checker.TypeI64:
		return namedTypeFromCheckerName("i64")

	case checker.TypeU8:
		return namedTypeFromCheckerName("u8")

	case checker.TypeU16:
		return namedTypeFromCheckerName("u16")

	case checker.TypeU32:
		return namedTypeFromCheckerName("u32")

	case checker.TypeU64:
		return namedTypeFromCheckerName("u64")

	case checker.TypeF32:
		return namedTypeFromCheckerName("f32")

	case checker.TypeF64,
		checker.TypeUntypedFloat:
		return namedTypeFromCheckerName("f64")

	case checker.TypeChar:
		return namedTypeFromCheckerName("char")

	case checker.TypeString:
		return namedTypeFromCheckerName("string")

	case checker.TypeCstring:
		return namedTypeFromCheckerName("cstring")

	case checker.TypeRawptr:
		return namedTypeFromCheckerName("rawptr")

	case checker.TypeAny:
		return namedTypeFromCheckerName("any")

	case checker.TypeInterfaceSelf:
		return &ast.InterfaceSelfType{}
	}

	if typ.Name != "" {
		return namedTypeFromCheckerName(typ.Name)
	}

	return nil
}

func checkerGenericArgumentsToAst(
	input []checker.GenericArgumentInfo,
) []ast.GenericArg {
	if len(input) == 0 {
		return nil
	}

	out := make([]ast.GenericArg, 0, len(input))

	for _, arg := range input {
		out = append(
			out,
			checkerGenericArgumentToAst(arg),
		)
	}

	return out
}

func checkerGenericArgumentToAst(
	arg checker.GenericArgumentInfo,
) ast.GenericArg {
	switch arg.Category {
	case ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion:
		return ast.GenericArg{
			Kind: ast.GenericArgType,
			Type: checkerTypeToAstType(arg.Type),
		}

	case ast.GenericParamTask:
		if arg.Expr != nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: arg.Expr,
			}
		}

		name := arg.Key
		if name == "" && arg.Type != nil {
			name = arg.Type.Name
		}

		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: &ast.IdentExpr{
				Name: ast.Ident{Name: name},
			},
		}

	case ast.GenericParamInt:
		if arg.Expr != nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: arg.Expr,
			}
		}

		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: &ast.IntLitExpr{
				Value: arg.Key,
			},
		}

	case ast.GenericParamBool:
		if arg.Expr != nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: arg.Expr,
			}
		}

		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: &ast.BoolLitExpr{
				Value: arg.Key == "true",
			},
		}

	case ast.GenericParamString:
		if arg.Expr != nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: arg.Expr,
			}
		}

		value := arg.Key
		if _, err := strconv.Unquote(value); err != nil {
			value = strconv.Quote(value)
		}

		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: &ast.StringLitExpr{
				Value: value,
			},
		}

	case ast.GenericParamValue:
		if arg.Expr != nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: arg.Expr,
			}
		}

		if arg.Key == "true" || arg.Key == "false" {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: &ast.BoolLitExpr{
					Value: arg.Key == "true",
				},
			}
		}

		if _, err := strconv.ParseInt(arg.Key, 0, 64); err == nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: &ast.IntLitExpr{
					Value: arg.Key,
				},
			}
		}

		if _, err := strconv.Unquote(arg.Key); err == nil {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: &ast.StringLitExpr{
					Value: arg.Key,
				},
			}
		}

		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: &ast.IdentExpr{
				Name: ast.Ident{Name: arg.Key},
			},
		}
	}

	if typ := checkerTypeToAstType(arg.Type); typ != nil {
		return ast.GenericArg{
			Kind: ast.GenericArgType,
			Type: typ,
		}
	}

	if arg.Expr != nil {
		return ast.GenericArg{
			Kind: ast.GenericArgExpr,
			Expr: arg.Expr,
		}
	}

	return ast.GenericArg{
		Kind: ast.GenericArgExpr,
		Expr: &ast.IdentExpr{
			Name: ast.Ident{Name: arg.Key},
		},
	}
}

func (g *Generator) semanticTaskSelection(
	candidate *checker.Symbol,
	packageName string,
	genericArguments []checker.GenericArgumentInfo,
	span source.Span,
) (string, TaskInfo, bool) {
	packageName, taskName := semanticCandidateIdentity(
		candidate,
		packageName,
	)

	if taskName == "" {
		g.error(
			span,
			"checker semantic resolution has no task candidate",
		)
		return "", TaskInfo{}, false
	}

	args := checkerGenericArgumentsToAst(genericArguments)
	args = g.genericArgsInContext(args)

	isImported :=
		packageName != "" &&
			packageName != g.packageName

	if isImported {
		pkg := g.packages[packageName]
		if pkg == nil {
			g.error(
				span,
				fmt.Sprintf(
					"checker selected task %s.%s, but package metadata is unavailable",
					packageName,
					taskName,
				),
			)
			return "", TaskInfo{}, false
		}

		info, ok := pkg.Tasks[taskName]
		if !ok {
			g.error(
				span,
				fmt.Sprintf(
					"checker selected unknown imported task %s.%s",
					packageName,
					taskName,
				),
			)
			return "", TaskInfo{}, false
		}

		if len(info.GenericParams) > 0 {
			g.collectGenericStructInstancesFromGenericArgsForParams(
				info.GenericParams,
				args,
			)

			name := g.registerImportedGenericTaskInstance(
				packageName,
				taskName,
				info,
				args,
			)

			instance := g.importedGenericTasks[name]
			if instance == nil {
				g.error(
					span,
					fmt.Sprintf(
						"could not register imported specialization %s.%s",
						packageName,
						taskName,
					),
				)
				return "", TaskInfo{}, false
			}

			return name,
				g.taskInfoFromImportedGenericTaskInstance(instance),
				true
		}

		localizedInfo :=
			g.taskInfoInPackageContext(
				packageName,
				taskName,
				info,
			)

		return cImportedTaskName(
			packageName,
			taskName,
			localizedInfo,
		), localizedInfo, true
	}

	info, ok := g.tasks[taskName]
	if !ok {
		g.error(
			span,
			fmt.Sprintf(
				"checker selected unknown local task %q",
				taskName,
			),
		)
		return "", TaskInfo{}, false
	}

	if len(info.GenericParams) > 0 {
		if info.Decl == nil {
			g.error(
				span,
				fmt.Sprintf(
					"generic task %q has no declaration for specialization",
					taskName,
				),
			)
			return "", TaskInfo{}, false
		}

		g.collectGenericStructInstancesFromGenericArgsForParams(
			info.GenericParams,
			args,
		)

		name := g.registerGenericTaskInstance(
			info.Decl,
			args,
		)

		instance := g.genericTasks[name]
		if instance == nil {
			g.error(
				span,
				fmt.Sprintf(
					"could not register specialization of %q",
					taskName,
				),
			)
			return "", TaskInfo{}, false
		}

		return name,
			g.taskInfoFromGenericTaskInstance(instance),
			true
	}

	name := g.cTaskName(taskName)

	if info.IsExtern && info.ExternName != "" {
		name = info.ExternName
	}

	return name, info, true
}

func (g *Generator) isAddressableExprForReference(
	expr ast.Expr,
) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope == nil {
			return false
		}

		_, ok := g.scope.lookup(e.Name.Name)
		return ok

	case *ast.SelectorExpr:
		leftType := g.inferExprType(e.Left, nil)

		if strings.HasPrefix(leftType.SealName, "*") {
			return true
		}

		return g.isAddressableExprForReference(e.Left)

	case *ast.UnaryExpr:
		return e.Op == token.Star

	case *ast.IndexExpr:
		resolution, ok := g.indexResolutions[e]
		if !ok || resolution.Candidate != nil {
			return false
		}

		leftType := g.inferExprType(e.Left, nil)

		if leftType.SealName == "rawptr" {
			return true
		}

		if leftType.IsVariadic {
			return g.isAddressableExprForReference(e.Left)
		}

		if g.isByteIndexableCType(leftType) {
			return g.isAddressableByteSource(e.Left)
		}
	}

	return false
}

func (g *Generator) emitSemanticReceiverArgument(
	expr ast.Expr,
	prepared string,
	expected CType,
) string {
	if !strings.HasPrefix(expected.SealName, "*") {
		if prepared != "" {
			return prepared
		}

		return g.emitExpr(expr, &expected)
	}

	actual := g.inferExprType(expr, nil)

	if actual.SealName == expected.SealName {
		if prepared != "" {
			return prepared
		}

		return g.emitExpr(expr, &expected)
	}

	expectedElemName := strings.TrimPrefix(
		expected.SealName,
		"*",
	)

	if expected.Elem != nil {
		expectedElemName = expected.Elem.SealName
	}

	if actual.SealName != expectedElemName {
		if prepared != "" {
			return prepared
		}

		return g.emitExpr(expr, &expected)
	}

	value := prepared
	if value == "" {
		value = g.emitExpr(expr, &actual)
	}

	if prepared != "" ||
		g.isAddressableExprForReference(expr) {
		return fmt.Sprintf("&(%s)", value)
	}

	if actual.SealName == "string" {
		return fmt.Sprintf(
			"((%s[]){%s})",
			actual.Name,
			value,
		)
	}

	g.error(
		expr.Span(),
		fmt.Sprintf(
			"checker-selected task requires *%s, but the receiver is not addressable",
			actual.SealName,
		),
	)

	return "NULL"
}

func (g *Generator) emitSemanticTaskCall(
	name string,
	info TaskInfo,
	args []ast.Expr,
	preparedArgs []string,
) string {
	outArgs := make(
		[]string,
		0,
		len(info.ParamTypes),
	)

	for i, arg := range args {
		prepared := ""

		if preparedArgs != nil &&
			i < len(preparedArgs) {
			prepared = preparedArgs[i]
		}

		expected := (*CType)(nil)

		if i < len(info.ParamTypes) {
			expected = &info.ParamTypes[i]
		}

		// Bracket and len overloads use a pointer receiver while Seal syntax
		// passes the value expression. Preserve the existing automatic
		// reference behavior for the first argument.
		if i == 0 && expected != nil {
			outArgs = append(
				outArgs,
				g.emitSemanticReceiverArgument(
					arg,
					prepared,
					*expected,
				),
			)
			continue
		}

		if prepared != "" {
			outArgs = append(
				outArgs,
				prepared,
			)
			continue
		}

		outArgs = append(
			outArgs,
			g.emitExpr(
				arg,
				expected,
			),
		)
	}

	// A checker-selected generic overload may have default parameters.
	// Candidate selection has already happened, so defaults must come from
	// the selected specialized TaskInfo rather than from overload lookup.
	if !info.IsVariadic {
		for i := len(args); i < len(info.ParamTypes); i++ {
			if i >= len(info.ParamHasDefault) ||
				!info.ParamHasDefault[i] {
				continue
			}

			if i >= len(info.ParamDefaults) ||
				info.ParamDefaults[i] == nil {
				g.error(
					source.Span{},
					fmt.Sprintf(
						"selected task %s is missing default argument %d",
						name,
						i+1,
					),
				)
				continue
			}

			expected := info.ParamTypes[i]

			outArgs = append(
				outArgs,
				g.emitExpr(
					info.ParamDefaults[i],
					&expected,
				),
			)
		}
	}

	return fmt.Sprintf(
		"%s(%s)",
		name,
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) emitSemanticTaskCallInTypeContext(
	packageName string,
	name string,
	info TaskInfo,
	args []ast.Expr,
	preparedArgs []string,
) string {
	if packageName == "" ||
		packageName == g.packageName {
		return g.emitSemanticTaskCall(
			name,
			info,
			args,
			preparedArgs,
		)
	}

	old := g.typeContextPackage
	g.typeContextPackage = packageName

	defer func() {
		g.typeContextPackage = old
	}()

	return g.emitSemanticTaskCall(
		name,
		info,
		args,
		preparedArgs,
	)
}

func (g *Generator) emitBuiltinIndexRead(
	e *ast.IndexExpr,
	leftType CType,
) string {
	left := g.emitExpr(e.Left, nil)
	index := g.emitExpr(e.Index, &CInt)

	switch {
	case leftType.SealName == "rawptr":
		return fmt.Sprintf(
			"((unsigned char *)(%s))[%s]",
			left,
			index,
		)

	case leftType.SealName == "string":
		return fmt.Sprintf(
			"seal_string_index(%s, %s)",
			left,
			index,
		)

	case leftType.SealName == "cstring":
		return fmt.Sprintf(
			"seal_cstring_index(%s, %s)",
			left,
			index,
		)

	case leftType.IsVariadic:
		if leftType.Elem == nil {
			g.error(
				e.Left.Span(),
				"cannot index invalid variadic value",
			)
			return "0"
		}

		return fmt.Sprintf(
			"(%s).data[%s]",
			left,
			index,
		)

	case g.isByteIndexableCType(leftType):
		return g.emitByteIndexExpr(
			e,
			leftType,
			left,
			index,
		)
	}

	g.error(
		e.Left.Span(),
		fmt.Sprintf(
			"checker selected builtin indexing for unsupported type %s",
			leftType.String(),
		),
	)

	return "0"
}

func (g *Generator) emitBuiltinIndexLValue(
	e *ast.IndexExpr,
	leftType CType,
) (string, CType, bool) {
	left := g.emitExpr(e.Left, nil)
	index := g.emitExpr(e.Index, &CInt)

	switch {
	case leftType.SealName == "rawptr":
		return fmt.Sprintf(
			"((unsigned char *)(%s))[%s]",
			left,
			index,
		), CU8, true

	case leftType.SealName == "string":
		g.error(
			e.Left.Span(),
			"string indexing is read-only",
		)
		return "0", CInvalid, false

	case leftType.SealName == "cstring":
		g.error(
			e.Left.Span(),
			"cstring indexing is read-only",
		)
		return "0", CInvalid, false

	case leftType.IsVariadic:
		if leftType.Elem == nil {
			g.error(
				e.Left.Span(),
				"cannot assign through invalid variadic value",
			)
			return "0", CInvalid, false
		}

		return fmt.Sprintf(
			"(%s).data[%s]",
			left,
			index,
		), *leftType.Elem, true

	case g.isByteIndexableCType(leftType):
		if !g.isAddressableByteSource(e.Left) {
			g.error(
				e.Left.Span(),
				"byte-index assignment requires an addressable value",
			)
			return "0", CInvalid, false
		}

		return g.emitByteIndexExpr(
			e,
			leftType,
			left,
			index,
		), CU8, true
	}

	g.error(
		e.Left.Span(),
		fmt.Sprintf(
			"checker selected builtin index assignment for unsupported type %s",
			leftType.String(),
		),
	)

	return "0", CInvalid, false
}

func (g *Generator) emitIndexExpr(
	e *ast.IndexExpr,
) string {
	resolution, ok := g.indexResolutions[e]
	if !ok {
		g.error(
			e.Span(),
			"missing checker resolution for index expression",
		)
		return "0"
	}

	if resolution.Candidate != nil {
		name, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return "0"
		}

		return g.emitSemanticTaskCall(
			name,
			info,
			[]ast.Expr{
				e.Left,
				e.Index,
			},
			nil,
		)
	}

	leftType := g.inferExprType(e.Left, nil)

	return g.emitBuiltinIndexRead(
		e,
		leftType,
	)
}

func (g *Generator) emitIndexAssignment(
	e *ast.IndexExpr,
	op token.Kind,
	right ast.Expr,
) string {
	resolution, ok := g.indexResolutions[e]
	if !ok {
		g.error(
			e.Span(),
			"missing checker resolution for index assignment",
		)
		return "0"
	}

	if resolution.Candidate != nil {
		if op != token.Assign {
			g.error(
				e.Span(),
				"compound assignment through an overloaded index setter is not supported by C codegen",
			)
			return "0"
		}

		name, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return "0"
		}

		return g.emitSemanticTaskCall(
			name,
			info,
			[]ast.Expr{
				e.Left,
				e.Index,
				right,
			},
			nil,
		)
	}

	leftType := g.inferExprType(e.Left, nil)

	lvalue, valueType, ok := g.emitBuiltinIndexLValue(
		e,
		leftType,
	)
	if !ok {
		return "0"
	}

	value := g.emitExpr(right, &valueType)

	return fmt.Sprintf(
		"%s %s %s",
		lvalue,
		g.cAssignOp(op),
		value,
	)
}

func (g *Generator) emitAssignmentExpr(
	s *ast.AssignStmt,
) string {
	if index, ok := s.Left.(*ast.IndexExpr); ok {
		return g.emitIndexAssignment(
			index,
			s.Op,
			s.Right,
		)
	}

	leftType := g.inferExprType(s.Left, nil)
	left := g.emitExpr(s.Left, nil)
	right := g.emitExpr(s.Right, &leftType)

	return fmt.Sprintf(
		"%s %s %s",
		left,
		g.cAssignOp(s.Op),
		right,
	)
}

func (g *Generator) indexExprType(
	e *ast.IndexExpr,
) CType {
	resolution, ok := g.indexResolutions[e]
	if !ok {
		return CInvalid
	}

	if resolution.Candidate != nil {
		_, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return CInvalid
		}

		return info.ReturnType
	}

	leftType := g.inferExprType(e.Left, nil)

	switch {
	case leftType.SealName == "rawptr":
		return CU8

	case leftType.SealName == "string":
		return CChar

	case leftType.SealName == "cstring":
		return CChar

	case leftType.IsVariadic &&
		leftType.Elem != nil:
		return *leftType.Elem

	case g.isByteIndexableCType(leftType):
		return CU8
	}

	return CInvalid
}

func ExportPackageInfo(
	packageName string,
	file *ast.File,
	reporter *diag.Reporter,
) *PackageInfo {
	return ExportPackageInfoWithSemanticInfo(
		packageName,
		file,
		reporter,
		nil,
		checker.SemanticInfo{},
	)
}

func ExportPackageInfoWithSemanticInfo(
	packageName string,
	file *ast.File,
	reporter *diag.Reporter,
	packages map[string]*PackageInfo,
	semantic checker.SemanticInfo,
) *PackageInfo {
	g := NewWithPackagesAndSemanticInfo(
		reporter,
		packageName,
		packages,
		semantic,
	)

	g.collect(file)

	return &PackageInfo{
		Name:       packageName,
		Tasks:      g.tasks,
		Overloads:  g.overloads,
		Structs:    g.structs,
		Distincts:  g.distincts,
		Enums:      g.enums,
		Unions:     g.unions,
		Interfaces: g.interfaces,
		Impls: append(
			[]*ast.ImplDecl(nil),
			g.implDecls...,
		),
	}
}

func (g *Generator) newTemp(prefix string) string {
	name := fmt.Sprintf("__seal_%s_%d", prefix, g.tempCounter)
	g.tempCounter++
	return name
}

func (g *Generator) Generate(file *ast.File) string {
	g.collect(file)
	g.collectImportedImplTemplates()

	g.seedNonGenericInterfaceInstances()
	g.collectImportedInterfaceInstances()
	g.seedRequestedGenericInstances()

	// Requests seeded into this package may contain concrete types owned by a
	// package that depends on this one, such as app.Point in mem.NewC<app.Point>.
	g.collectRequiredExternalTypes()

	g.finalizeInterfaceAndGenericInstances(file)

	// Nested specializations discovered during finalization may introduce more
	// caller-owned concrete types.
	g.collectRequiredExternalTypes()

	g.line("// Generated by seal.")
	g.line("#include <stdbool.h>")
	g.line("#include <stdlib.h>")
	g.line("#include <stdint.h>")
	g.line("#include <stddef.h>")
	g.line("#include <stdio.h>")
	g.line("#include <assert.h>")
	g.line("")
	g.line("#ifndef NULL")
	g.line("#define NULL ((void*)0)")
	g.line("#endif")
	g.line("")

	g.emitCImports(file)
	g.emitRuntimeSupport()

	g.emitDistincts(file)
	g.emitEnums(file)
	g.emitImportedEnums()

	// Interface representations must exist before an externally materialized
	// structure can store one by value.
	g.emitInterfaceValueTypes()

	g.emitRequiredExternalTypes()

	g.emitImportedStructs()
	g.emitStructs(file)
	g.emitGenericStructs()
	g.emitImportedGenericStructs()
	g.emitUnions(file)

	// Interface result structures and dynamic vtables can depend on concrete
	// struct types, so emit them after all concrete struct definitions.
	g.emitInterfaceResultStructs()
	g.emitDynamicInterfaceVTableTypes()

	g.emitTaskVariadicRuntimeTypes()
	g.emitConstants(file)

	g.emitImportedResultStructs()
	g.emitImportedGenericResultStructs()
	g.emitResultStructs(file)
	g.emitGenericResultStructs()

	g.emitImportedTaskPrototypes()
	g.emitImportedGenericTaskPrototypes()
	g.emitTaskPrototypes(file)
	g.emitGenericTaskPrototypes()

	g.emitImplVTables()
	g.emitDynamicInterfaceDispatchers()
	g.emitStaticInterfaceDispatchers()
	g.emitTasks(file)
	g.emitGenericTasks()

	return g.out.String()
}

func (g *Generator) collect(file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.DistinctDecl:
			g.distincts[d.Name.Name] = d

		case *ast.StructDecl:
			g.structs[d.Name.Name] = d

		case *ast.EnumDecl:
			g.enums[d.Name.Name] = d

		case *ast.UnionDecl:
			g.unions[d.Name.Name] = d

		case *ast.InterfaceDecl:
			g.interfaces[d.Name.Name] = d

		case *ast.ImplDecl:
			g.implDecls = append(g.implDecls, d)

			info := g.implTemplateFromDecl(d)
			if info != nil {
				g.implTemplates = append(g.implTemplates, info)
			}
		}
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.OverloadDecl:
			for _, name := range d.Names {
				g.overloads[d.Name] = append(g.overloads[d.Name], name.Name)
			}

		case *ast.TaskDecl:
			if d.IsTest {
				continue
			}

			info := TaskInfo{
				Decl:           d,
				GenericParams:  d.GenericParams,
				RequiredParams: len(d.Params),
				IsExtern:       d.IsExtern,
				ExternName:     d.ExternName,
				IsPure:         d.IsPure,
				IsIntrinsic:    d.IsIntrinsic,
				IsTrustedPure:  d.IsTrustedPure,
			}

			for _, param := range d.Params {
				info.ParamNames = append(info.ParamNames, param.Name.Name)
				info.ParamTypeAsts = append(info.ParamTypeAsts, param.Type)
			}

			for _, result := range d.Results {
				info.ResultTypeAsts = append(info.ResultTypeAsts, result)
			}

			// Generic tasks are templates. Do not lower their parameter/result
			// types during collection, because those types may contain type
			// parameters such as T, N, or nested forms like Box<Pair<int, T>>.
			//
			// Concrete parameter/result C types are produced later by
			// genericTaskSignature and emitGenericTaskInstance with a real
			// substitution map.
			if len(d.GenericParams) > 0 {
				if len(d.Results) == 0 {
					info.ReturnType = CVoid
				} else {
					info.ReturnType = CInvalid
				}

				for i, param := range d.Params {
					info.ParamTypes = append(info.ParamTypes, CInvalid)
					info.ParamDefaults = append(info.ParamDefaults, param.Default)
					info.ParamHasDefault = append(info.ParamHasDefault, param.HasDefault)
					info.ParamIsVariadic = append(info.ParamIsVariadic, param.IsVariadic)

					if param.IsVariadic {
						info.IsVariadic = true
						if info.RequiredParams == len(d.Params) {
							info.RequiredParams = i
						}
					}

					if param.HasDefault && info.RequiredParams == len(d.Params) {
						info.RequiredParams = i
					}
				}

				g.tasks[d.Name.Name] = info
				continue
			}

			for _, result := range d.Results {
				info.ReturnTypes = append(info.ReturnTypes, g.cTypeFromAst(result))
			}

			if len(d.Results) == 0 {
				if d.Name.Name == "Main" {
					info.ReturnType = CMainReturn
				} else {
					info.ReturnType = CVoid
				}
			} else if len(d.Results) == 1 {
				info.ReturnType = info.ReturnTypes[0]
			} else {
				resultName := g.taskResultStructName(d.Name.Name)
				info.ReturnType = CType{
					Name:     resultName,
					SealName: resultName,
				}
			}

			for i, param := range d.Params {
				info.ParamTypes = append(info.ParamTypes, g.cTypeFromAst(param.Type))
				info.ParamDefaults = append(info.ParamDefaults, param.Default)
				info.ParamHasDefault = append(info.ParamHasDefault, param.HasDefault)
				info.ParamIsVariadic = append(info.ParamIsVariadic, param.IsVariadic)

				if param.IsVariadic {
					info.IsVariadic = true
					if info.RequiredParams == len(d.Params) {
						info.RequiredParams = i
					}
				}

				if param.HasDefault && info.RequiredParams == len(d.Params) {
					info.RequiredParams = i
				}
			}

			g.tasks[d.Name.Name] = info

		case *ast.ConstDecl:
			g.consts[d.Name.Name] = g.inferExprType(d.Value, nil)
		}
	}
}

func resolvedImplKey(interfaceKey string, concrete CType) string {
	return interfaceKey + "|" + concrete.SealName
}

func (g *Generator) implTemplateFromDecl(
	d *ast.ImplDecl,
) *ImplTemplate {
	return g.implTemplateFromDeclInPackage(
		g.packageName,
		d,
	)
}

func (g *Generator) implTemplateFromDeclInPackage(
	packageName string,
	d *ast.ImplDecl,
) *ImplTemplate {
	if d == nil ||
		d.Interface == nil ||
		d.Target == nil {
		return nil
	}

	entries := map[string]ast.ImplEntry{}

	for _, entry := range d.Entries {
		entries[entry.Name.Name] = entry
	}

	return &ImplTemplate{
		PackageName: packageName,
		Decl:        d,
		GenericParams: append(
			[]ast.GenericParam(nil),
			d.GenericParams...,
		),
		Interface: d.Interface,
		Target:    d.Target,
		Entries:   entries,
		UsingPath: append(
			[]ast.Ident(nil),
			d.UsingPath...,
		),
	}
}

func (g *Generator) collectImportedImplTemplates() {
	if len(g.packages) == 0 {
		return
	}

	packageNames := make([]string, 0, len(g.packages))

	for packageName := range g.packages {
		packageNames = append(packageNames, packageName)
	}

	sort.Strings(packageNames)

	for _, packageName := range packageNames {
		pkg := g.packages[packageName]
		if pkg == nil {
			continue
		}

		for _, decl := range pkg.Impls {
			template := g.implTemplateFromDeclInPackage(
				packageName,
				decl,
			)
			if template == nil {
				continue
			}

			g.implTemplates = append(
				g.implTemplates,
				template,
			)
		}
	}
}

func (g *Generator) registerInterfaceInstance(
	packageName string,
	baseName string,
	decl *ast.InterfaceDecl,
	args []ast.GenericArg,
) *InterfaceInstance {
	if decl == nil {
		return nil
	}

	args = normalizeGenericArgsForCGenParams(decl.GenericParams, args)

	key := g.interfaceInstanceKey(packageName, baseName, decl.GenericParams, args)

	if existing := g.interfaceInstances[key]; existing != nil {
		return existing
	}

	cName := g.interfaceInstanceCName(
		packageName,
		baseName,
		decl.GenericParams,
		args,
	)

	instance := &InterfaceInstance{
		Key:         key,
		CName:       cName,
		PackageName: packageName,
		BaseName:    baseName,
		Decl:        decl,
		Args:        append([]ast.GenericArg(nil), args...),
		Subst:       genericArgSubstForCGen(decl.GenericParams, args),
		IsDyn:       decl.IsDyn,
	}

	g.interfaceInstances[key] = instance

	// Register interfaces referenced by this interface's requirement
	// signatures before interface value representations are emitted.
	//
	// Store the instance first so mutually-referential interface signatures
	// do not recurse indefinitely.
	g.withTypeContext(packageName, func() {
		for _, req := range decl.Requirements {
			if req == nil {
				continue
			}

			for _, param := range req.Params {
				paramType := g.substituteTypeAstForCGen(
					param.Type,
					instance.Subst,
				)

				g.collectInterfaceInstancesFromType(
					paramType,
				)
			}

			for _, result := range req.Results {
				resultType := g.substituteTypeAstForCGen(
					result,
					instance.Subst,
				)

				g.collectInterfaceInstancesFromType(
					resultType,
				)
			}
		}
	})

	return instance
}

func (g *Generator) interfaceInstanceKey(
	packageName string,
	baseName string,
	params []ast.GenericParam,
	args []ast.GenericArg,
) string {
	name := baseName
	if packageName != "" {
		name = packageName + "." + baseName
	}

	if len(args) == 0 {
		return name
	}

	var parts []string

	for i, arg := range args {
		category := ast.GenericParamInvalid
		if i < len(params) {
			category = params[i].Category
		}

		switch category {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			parts = append(parts, g.genericTypeArgCName(arg))

		case ast.GenericParamTask:
			parts = append(parts, g.genericTaskArgCName(arg))

		default:
			parts = append(parts, genericValueArgCName(arg))
		}
	}

	return name + "<" + strings.Join(parts, ",") + ">"
}

func (g *Generator) interfaceInstanceCName(
	packageName string,
	baseName string,
	params []ast.GenericParam,
	args []ast.GenericArg,
) string {
	var parts []string

	if packageName != "" {
		parts = append(parts, sanitizeCName(packageName))
	}

	parts = append(parts, sanitizeCName(baseName))

	for i, arg := range args {
		category := ast.GenericParamInvalid
		if i < len(params) {
			category = params[i].Category
		}

		switch category {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			parts = append(parts, g.genericTypeArgCName(arg))

		case ast.GenericParamTask:
			parts = append(parts, g.genericTaskArgCName(arg))

		default:
			parts = append(parts, genericValueArgCName(arg))
		}
	}

	return strings.Join(parts, "_")
}

func (g *Generator) collectGenericMonomorphizations(file *ast.File) {
	g.collectGenericStructInstances(file)

	processedTasks := map[string]bool{}
	processedImportedTasks := map[string]bool{}

	for {
		taskNames := make([]string, 0, len(g.genericTasks))
		for name := range g.genericTasks {
			if !processedTasks[name] {
				taskNames = append(taskNames, name)
			}
		}
		sort.Strings(taskNames)

		importedTaskNames := make([]string, 0, len(g.importedGenericTasks))
		for name := range g.importedGenericTasks {
			if !processedImportedTasks[name] {
				importedTaskNames = append(importedTaskNames, name)
			}
		}
		sort.Strings(importedTaskNames)

		if len(taskNames) == 0 && len(importedTaskNames) == 0 {
			return
		}

		for _, name := range taskNames {
			processedTasks[name] = true
			g.collectGenericTaskInstanceMonomorphizations(name)
		}

		for _, name := range importedTaskNames {
			processedImportedTasks[name] = true
			g.collectImportedGenericTaskInstanceMonomorphizations(name)
		}
	}
}

func (g *Generator) collectImportedGenericTaskInstanceMonomorphizations(
	name string,
) {
	info := g.importedGenericTasks[name]
	if info == nil {
		return
	}

	subst := genericArgSubstForCGen(
		info.Info.GenericParams,
		info.Args,
	)

	oldSubst := g.genericSubst
	g.genericSubst = subst

	defer func() {
		g.genericSubst = oldSubst
	}()

	g.collectGenericStructInstancesFromGenericArgsForParams(
		info.Info.GenericParams,
		info.Args,
	)

	g.withTypeContext(info.PackageName, func() {
		for _, paramType := range info.Info.ParamTypeAsts {
			g.collectGenericStructInstancesFromType(
				g.substituteTypeAstForCGen(
					paramType,
					subst,
				),
			)
		}

		for _, resultType := range info.Info.ResultTypeAsts {
			g.collectGenericStructInstancesFromType(
				g.substituteTypeAstForCGen(
					resultType,
					subst,
				),
			)
		}

		for i, hasDefault := range info.Info.ParamHasDefault {
			if !hasDefault ||
				i >= len(info.Info.ParamDefaults) {
				continue
			}

			g.collectGenericStructInstancesFromExpr(
				g.substituteExprForCGen(
					info.Info.ParamDefaults[i],
					subst,
				),
			)
		}
	})
}

func (g *Generator) collectGenericTaskInstanceMonomorphizations(
	name string,
) {
	info := g.genericTasks[name]
	if info == nil || info.Decl == nil {
		return
	}

	subst := genericTaskSubstForCGen(
		info.Decl.GenericParams,
		info.Args,
	)

	oldSubst := g.genericSubst
	g.genericSubst = subst

	defer func() {
		g.genericSubst = oldSubst
	}()

	for i, arg := range info.Args {
		if i >= len(info.Decl.GenericParams) {
			break
		}

		if info.Decl.GenericParams[i].Category !=
			ast.GenericParamTask {
			continue
		}

		g.collectGenericTaskArgInstance(
			g.substituteGenericArgForCGen(
				arg,
				subst,
			),
		)
	}

	for _, param := range info.Decl.Params {
		g.collectGenericStructInstancesFromType(
			g.substituteTypeAstForCGen(
				param.Type,
				subst,
			),
		)

		if param.HasDefault {
			g.collectGenericStructInstancesFromExpr(
				g.substituteExprForCGen(
					param.Default,
					subst,
				),
			)
		}
	}

	for _, result := range info.Decl.Results {
		g.collectGenericStructInstancesFromType(
			g.substituteTypeAstForCGen(
				result,
				subst,
			),
		)
	}

	g.collectGenericStructInstancesFromBlockWithGenericArgs(
		info.Decl.Body,
		subst,
	)
}

func (g *Generator) collectGenericTaskArgsFromParams(params []ast.GenericParam, args []ast.GenericArg) {
	for i, param := range params {
		if i >= len(args) {
			break
		}

		if param.Category != ast.GenericParamTask {
			continue
		}

		g.collectGenericTaskArgInstance(args[i])
	}
}

func (g *Generator) collectGenericTaskArgInstance(arg ast.GenericArg) {
	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		return
	}

	gen, ok := arg.Expr.(*ast.GenericExpr)
	if !ok {
		return
	}

	switch base := gen.Base.(type) {
	case *ast.IdentExpr:
		if packageName,
			taskName,
			info,
			ok :=
			g.importedGenericTaskInfoFromTypeContext(
				base.Name.Name,
			); ok {
			g.registerImportedGenericTaskInstance(
				packageName,
				taskName,
				info,
				gen.Args,
			)

			g.collectGenericStructInstancesFromGenericArgsForParams(
				info.GenericParams,
				gen.Args,
			)
			return
		}

		info, ok := g.tasks[base.Name.Name]
		if !ok || info.Decl == nil || len(info.GenericParams) == 0 {
			return
		}

		g.registerGenericTaskInstance(info.Decl, gen.Args)
		g.collectGenericStructInstancesFromGenericArgsForParams(info.GenericParams, gen.Args)

	case *ast.SelectorExpr:
		pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(base)
		if !ok {
			return
		}

		g.registerImportedGenericTaskInstance(pkgName, taskName, info, gen.Args)
		g.collectGenericStructInstancesFromGenericArgsForParams(info.GenericParams, gen.Args)
	}
}

func (g *Generator) collectGenericStructInstancesFromBlockWithGenericArgs(block *ast.BlockStmt, subst map[string]ast.GenericArg) {
	if block == nil {
		return
	}

	for _, stmt := range block.Stmts {
		g.collectGenericStructInstancesFromStmtWithGenericArgs(stmt, subst)
	}
}

func (g *Generator) collectGenericStructInstancesFromStmtWithGenericArgs(
	stmt ast.Stmt,
	subst map[string]ast.GenericArg,
) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		// Local declarations are not supported by this C backend phase yet.

	case *ast.BlockStmt:
		g.collectGenericStructInstancesFromBlockWithGenericArgs(
			s,
			subst,
		)

	case *ast.ReturnStmt:
		for _, value := range s.Values {
			g.collectGenericStructInstancesFromExpr(
				g.substituteExprForCGen(
					value,
					subst,
				),
			)
		}

	case *ast.DeferStmt:
		if s.Call != nil {
			g.collectGenericStructInstancesFromExpr(
				g.substituteExprForCGen(
					s.Call,
					subst,
				),
			)
		}

		if s.Body != nil {
			g.collectGenericStructInstancesFromBlockWithGenericArgs(
				s.Body,
				subst,
			)
		}

	case *ast.SealStmt:
		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Target,
				subst,
			),
		)

	case *ast.ExprStmt:
		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Expr,
				subst,
			),
		)

	case *ast.MultiVarDeclStmt:
		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Value,
				subst,
			),
		)

	case *ast.AssignStmt:
		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Left,
				subst,
			),
		)

		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Right,
				subst,
			),
		)

	case *ast.VarDeclStmt:
		if s.HasType {
			g.collectGenericStructInstancesFromType(
				g.substituteTypeAstForCGen(
					s.Type,
					subst,
				),
			)
		}

		if s.HasValue {
			g.collectGenericStructInstancesFromExpr(
				g.substituteExprForCGen(
					s.Value,
					subst,
				),
			)
		}

	case *ast.IfStmt:
		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Cond,
				subst,
			),
		)

		g.collectGenericStructInstancesFromBlockWithGenericArgs(
			s.Then,
			subst,
		)

		if s.Else != nil {
			g.collectGenericStructInstancesFromStmtWithGenericArgs(
				s.Else,
				subst,
			)
		}

	case *ast.ForStmt:
		if s.Init != nil {
			g.collectGenericStructInstancesFromStmtWithGenericArgs(
				s.Init,
				subst,
			)
		}

		if s.Cond != nil {
			g.collectGenericStructInstancesFromExpr(
				g.substituteExprForCGen(
					s.Cond,
					subst,
				),
			)
		}

		if s.Post != nil {
			g.collectGenericStructInstancesFromStmtWithGenericArgs(
				s.Post,
				subst,
			)
		}

		g.collectGenericStructInstancesFromBlockWithGenericArgs(
			s.Body,
			subst,
		)

	case *ast.SwitchStmt:
		g.collectGenericStructInstancesFromExpr(
			g.substituteExprForCGen(
				s.Target,
				subst,
			),
		)

		for _, swCase := range s.Cases {
			if swCase.UnionMember != nil {
				g.collectGenericStructInstancesFromType(
					g.substituteTypeAstForCGen(
						swCase.UnionMember,
						subst,
					),
				)
			}

			if swCase.Expr != nil {
				g.collectGenericStructInstancesFromExpr(
					g.substituteExprForCGen(
						swCase.Expr,
						subst,
					),
				)
			}

			for _, bodyStmt := range swCase.Body {
				g.collectGenericStructInstancesFromStmtWithGenericArgs(
					bodyStmt,
					subst,
				)
			}
		}
	}
}

func (g *Generator) collectGenericStructInstances(file *ast.File) {
	for _, decl := range file.Decls {
		g.collectGenericStructInstancesFromDecl(decl)
	}
}

func (g *Generator) collectGenericStructInstancesFromDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		g.collectGenericStructInstancesFromExpr(d.Value)

	case *ast.StructDecl:
		if len(d.GenericParams) > 0 {
			return
		}

		for _, field := range d.Fields {
			g.collectGenericStructInstancesFromType(field.Type)
		}

	case *ast.DistinctDecl:
		g.collectGenericStructInstancesFromType(d.Underlying)

	case *ast.UnionDecl:
		for _, member := range d.Members {
			g.collectGenericStructInstancesFromType(member)
		}

	case *ast.InterfaceDecl:
		for _, req := range d.Requirements {
			for _, param := range req.Params {
				g.collectGenericStructInstancesFromType(param.Type)
			}

			for _, result := range req.Results {
				g.collectGenericStructInstancesFromType(result)
			}
		}

	case *ast.ImplDecl:
		// Generic implementations are templates. Their interface, target, and
		// inline entry bodies can contain unresolved parameters such as T.
		//
		// Do not register template-shaped instances such as Box<T>. Concrete
		// instances are discovered from actual uses such as Box<int>,
		// Holder<int>, or cast<Readable<int>>.
		if len(d.GenericParams) > 0 {
			return
		}

		g.collectGenericStructInstancesFromType(d.Interface)
		g.collectGenericStructInstancesFromType(d.Target)

		for _, entry := range d.Entries {
			if entry.Task != nil {
				g.collectGenericStructInstancesFromDecl(entry.Task)
			}

			if entry.Alias != nil {
				g.collectGenericStructInstancesFromExpr(entry.Alias)
			}
		}

	case *ast.TaskDecl:
		// Generic tasks are templates. Do not collect their parameter,
		// result, or body types here, because this would register instances
		// such as Box<T>. Concrete instances are collected through
		// collectGenericTaskInstanceTypes after calls like Make<int>() are seen.
		if len(d.GenericParams) > 0 {
			return
		}

		for _, param := range d.Params {
			g.collectGenericStructInstancesFromType(param.Type)

			if param.HasDefault {
				g.collectGenericStructInstancesFromExpr(param.Default)
			}
		}

		for _, result := range d.Results {
			g.collectGenericStructInstancesFromType(result)
		}

		g.collectGenericStructInstancesFromBlock(d.Body)
	}
}

func (g *Generator) collectGenericStructInstancesFromBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}

	for _, stmt := range block.Stmts {
		g.collectGenericStructInstancesFromStmt(stmt)
	}
}

func (g *Generator) collectGenericStructInstancesFromStmt(
	stmt ast.Stmt,
) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		g.collectGenericStructInstancesFromDecl(
			s.Decl,
		)

	case *ast.BlockStmt:
		g.collectGenericStructInstancesFromBlock(
			s,
		)

	case *ast.ReturnStmt:
		for _, value := range s.Values {
			g.collectGenericStructInstancesFromExpr(
				value,
			)
		}

	case *ast.DeferStmt:
		if s.Call != nil {
			g.collectGenericStructInstancesFromExpr(
				s.Call,
			)
		}

		if s.Body != nil {
			g.collectGenericStructInstancesFromBlock(
				s.Body,
			)
		}

	case *ast.SealStmt:
		g.collectGenericStructInstancesFromExpr(
			s.Target,
		)

	case *ast.ExprStmt:
		g.collectGenericStructInstancesFromExpr(
			s.Expr,
		)

	case *ast.MultiVarDeclStmt:
		g.collectGenericStructInstancesFromExpr(
			s.Value,
		)

	case *ast.AssignStmt:
		g.collectGenericStructInstancesFromExpr(
			s.Left,
		)

		g.collectGenericStructInstancesFromExpr(
			s.Right,
		)

	case *ast.VarDeclStmt:
		if s.HasType {
			g.collectGenericStructInstancesFromType(
				s.Type,
			)
		}

		if s.HasValue {
			g.collectGenericStructInstancesFromExpr(
				s.Value,
			)
		}

	case *ast.IfStmt:
		g.collectGenericStructInstancesFromExpr(
			s.Cond,
		)

		g.collectGenericStructInstancesFromBlock(
			s.Then,
		)

		if s.Else != nil {
			g.collectGenericStructInstancesFromStmt(
				s.Else,
			)
		}

	case *ast.ForStmt:
		if s.Init != nil {
			g.collectGenericStructInstancesFromStmt(
				s.Init,
			)
		}

		if s.Cond != nil {
			g.collectGenericStructInstancesFromExpr(
				s.Cond,
			)
		}

		if s.Post != nil {
			g.collectGenericStructInstancesFromStmt(
				s.Post,
			)
		}

		g.collectGenericStructInstancesFromBlock(
			s.Body,
		)

	case *ast.SwitchStmt:
		g.collectGenericStructInstancesFromExpr(
			s.Target,
		)

		for _, swCase := range s.Cases {
			if swCase.UnionMember != nil {
				g.collectGenericStructInstancesFromType(
					swCase.UnionMember,
				)
			}

			if swCase.Expr != nil {
				g.collectGenericStructInstancesFromExpr(
					swCase.Expr,
				)
			}

			for _, bodyStmt := range swCase.Body {
				g.collectGenericStructInstancesFromStmt(
					bodyStmt,
				)
			}
		}
	}
}

func (g *Generator) collectGenericStructInstancesFromType(typ ast.Type) {
	switch t := typ.(type) {
	case *ast.InterfaceSelfType:
		return

	case *ast.PointerType:
		g.collectGenericStructInstancesFromType(t.Elem)

	case *ast.GenericType:
		if pkgName, typeName, ok := packageTypeNameFromAst(t.Base); ok {
			if pkg := g.packages[pkgName]; pkg != nil {
				if decl := pkg.Structs[typeName]; decl != nil && len(decl.GenericParams) > 0 {
					_ = g.cTypeFromGenericType(t)
				}
			}
		} else {
			baseName := typeNameFromAst(t.Base)

			if decl := g.structs[baseName]; decl != nil && len(decl.GenericParams) > 0 {
				_ = g.cTypeFromGenericType(t)
			} else if g.typeContextPackage != "" {
				if pkg := g.packages[g.typeContextPackage]; pkg != nil {
					if decl := pkg.Structs[baseName]; decl != nil && len(decl.GenericParams) > 0 {
						_ = g.cTypeFromGenericType(t)
					}
				}
			}
		}

		g.collectGenericStructInstancesFromType(t.Base)

		for _, arg := range t.Args {
			g.collectGenericStructInstancesFromGenericArg(arg)
		}
	}
}

func exprFromTypeAstForCGen(typ ast.Type) ast.Expr {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 0 {
			return nil
		}

		expr := ast.Expr(&ast.IdentExpr{Name: t.Parts[0]})

		for i := 1; i < len(t.Parts); i++ {
			expr = &ast.SelectorExpr{
				Left: expr,
				Name: t.Parts[i],
				Loc:  t.Loc,
			}
		}

		return expr

	default:
		return nil
	}
}

func (g *Generator) collectGenericStructInstancesFromGenericArg(arg ast.GenericArg) {
	switch arg.Kind {
	case ast.GenericArgType:
		g.collectGenericStructInstancesFromType(arg.Type)

	case ast.GenericArgExpr:
		if arg.Expr == nil {
			return
		}

		if gen, ok := arg.Expr.(*ast.GenericExpr); ok {
			switch base := gen.Base.(type) {
			case *ast.IdentExpr:
				if info, ok := g.tasks[base.Name.Name]; ok && len(info.GenericParams) > 0 {
					g.collectGenericTaskArgInstance(arg)
					return
				}

			case *ast.SelectorExpr:
				if _, _, _, ok := g.importedGenericTaskInfoFromSelector(base); ok {
					g.collectGenericTaskArgInstance(arg)
					return
				}
			}
		}

		if typ := typeAstFromExprForCGen(arg.Expr); typ != nil {
			g.collectGenericStructInstancesFromType(typ)
			return
		}

		g.collectGenericStructInstancesFromExpr(arg.Expr)
	}
}

func (g *Generator) collectGenericStructInstancesFromExpr(
	expr ast.Expr,
) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.UnaryExpr:
		g.collectGenericStructInstancesFromExpr(e.Expr)

	case *ast.BinaryExpr:
		g.collectGenericStructInstancesFromExpr(e.Left)
		g.collectGenericStructInstancesFromExpr(e.Right)

	case *ast.CallExpr:
		g.collectGenericStructInstancesFromExpr(e.Callee)

		for _, arg := range e.Args {
			g.collectGenericStructInstancesFromExpr(arg)
		}

		if resolution, ok := g.lenResolutions[e]; ok &&
			resolution.Candidate != nil {
			_, _, _ = g.semanticTaskSelection(
				resolution.Candidate,
				resolution.PackageName,
				resolution.GenericArguments,
				e.Span(),
			)
		}

	case *ast.GenericExpr:
		if resolution, ok :=
			g.genericOverloadCalls[e]; ok {
			if resolution.Candidate == nil {
				g.error(
					e.Span(),
					"checker generic-overload resolution has no candidate",
				)
				return
			}

			_, _, _ = g.semanticTaskSelection(
				resolution.Candidate,
				resolution.PackageName,
				resolution.GenericArguments,
				e.Span(),
			)

			// The checker-provided generic arguments are collected by
			// semanticTaskSelection according to the selected candidate's
			// generic parameter categories.
			g.collectGenericStructInstancesFromExpr(
				e.Base,
			)

			return
		}

		handledAsTask := false

		switch base := e.Base.(type) {
		case *ast.IdentExpr:
			if packageName,
				taskName,
				info,
				ok :=
				g.importedGenericTaskInfoFromTypeContext(
					base.Name.Name,
				); ok {
				g.registerImportedGenericTaskInstance(
					packageName,
					taskName,
					info,
					e.Args,
				)

				g.collectGenericStructInstancesFromGenericArgsForParams(
					info.GenericParams,
					e.Args,
				)

				handledAsTask = true
				break
			}

			if info, ok := g.tasks[base.Name.Name]; ok &&
				info.Decl != nil &&
				len(info.GenericParams) > 0 {
				g.registerGenericTaskInstance(
					info.Decl,
					e.Args,
				)

				g.collectGenericStructInstancesFromGenericArgsForParams(
					info.GenericParams,
					e.Args,
				)

				handledAsTask = true
			}

		case *ast.SelectorExpr:
			if pkgName,
				taskName,
				info,
				ok :=
				g.importedGenericTaskInfoFromSelector(
					base,
				); ok {
				g.registerImportedGenericTaskInstance(
					pkgName,
					taskName,
					info,
					e.Args,
				)

				g.collectGenericStructInstancesFromGenericArgsForParams(
					info.GenericParams,
					e.Args,
				)

				handledAsTask = true
			}
		}

		g.collectGenericStructInstancesFromExpr(e.Base)

		if !handledAsTask {
			for _, arg := range e.Args {
				g.collectGenericStructInstancesFromGenericArg(
					arg,
				)
			}
		}

	case *ast.SpreadExpr:
		g.collectGenericStructInstancesFromExpr(e.Expr)

	case *ast.SelectorExpr:
		g.collectGenericStructInstancesFromExpr(e.Left)

	case *ast.IndexExpr:
		g.collectGenericStructInstancesFromExpr(e.Left)
		g.collectGenericStructInstancesFromExpr(e.Index)

		if resolution, ok := g.indexResolutions[e]; ok &&
			resolution.Candidate != nil {
			_, _, _ = g.semanticTaskSelection(
				resolution.Candidate,
				resolution.PackageName,
				resolution.GenericArguments,
				e.Span(),
			)
		}

	case *ast.CompoundLiteralExpr:
		g.collectGenericStructInstancesFromType(e.Type)

		for _, field := range e.Fields {
			g.collectGenericStructInstancesFromExpr(
				field.Value,
			)
		}

		for _, value := range e.Values {
			g.collectGenericStructInstancesFromExpr(value)
		}
	}
}

func (g *Generator) collectGenericStructInstancesFromGenericArgsForParams(params []ast.GenericParam, args []ast.GenericArg) {
	for i, arg := range args {
		if i >= len(params) {
			g.collectGenericStructInstancesFromGenericArg(arg)
			continue
		}

		switch params[i].Category {
		case ast.GenericParamTask:
			// Important: task arguments such as Swap<int> look syntactically
			// like generic expressions, but they are not generic type/struct
			// instances. Register/collect them as task instances only.
			g.collectGenericTaskArgInstance(arg)

		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			g.collectGenericStructInstancesFromGenericArg(arg)

		default:
			// Value parameters can contain type-looking expressions in places
			// like size(T), but should not reinterpret task arguments as types.
			if arg.Kind == ast.GenericArgExpr && arg.Expr != nil {
				g.collectGenericStructInstancesFromExpr(arg.Expr)
			}
		}
	}
}

func (g *Generator) emitGenericStructs() {
	names := make([]string, 0, len(g.genericStructs))
	for name := range g.genericStructs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		g.emitGenericStructInstance(name, map[string]bool{})
	}
}

func (g *Generator) emitGenericStructInstance(name string, visiting map[string]bool) {
	if g.emittedGenericStructs[name] {
		return
	}

	if isInvalidCStructName(name) {
		g.emittedGenericStructs[name] = true
		return
	}

	info := g.genericStructs[name]
	if info == nil || info.Decl == nil || isInvalidCStructName(info.Name) || isInvalidCStructName(info.Decl.Name.Name) {
		g.emittedGenericStructs[name] = true
		return
	}

	if visiting[name] {
		g.error(info.Decl.Name.Span(), fmt.Sprintf("recursive generic struct instantiation %s is not supported yet", name))
		return
	}

	visiting[name] = true

	subst := genericArgSubstForCGen(info.Decl.GenericParams, info.Args)

	for _, field := range info.Decl.Fields {
		g.emitGenericStructDepsForType(field.Type, subst, visiting)
	}

	g.linef("typedef struct %s {", info.Name)
	g.indent++

	for _, field := range info.Decl.Fields {
		fieldType := g.cTypeFromAstWithGenericArgs(field.Type, subst)
		g.linef("%s;", fieldType.Decl(field.Name.Name))
	}

	g.indent--
	g.linef("} %s;", info.Name)
	g.line("")

	g.emittedGenericStructs[name] = true
	visiting[name] = false
}

func genericArgSubstForCGen(params []ast.GenericParam, args []ast.GenericArg) map[string]ast.GenericArg {
	subst := map[string]ast.GenericArg{}

	for i, param := range params {
		if i >= len(args) {
			break
		}

		subst[param.Name.Name] = args[i]
	}

	return subst
}

func (g *Generator) emitGenericStructDepsForType(typ ast.Type, subst map[string]ast.GenericArg, visiting map[string]bool) {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			name := t.Parts[0].Name

			if arg, ok := subst[name]; ok {
				if genericArgIsSingleNameForCGen(arg, name) {
					return
				}

				g.emitGenericStructDepsForGenericArg(
					arg,
					subst,
					visiting,
				)
			}
		}

	case *ast.PointerType:
		g.emitGenericStructDepsForType(t.Elem, subst, visiting)

	case *ast.GenericType:
		args := make([]ast.GenericArg, 0, len(t.Args))
		for _, arg := range t.Args {
			args = append(args, g.substituteGenericArgForCGen(arg, subst))
		}

		gen := &ast.GenericType{
			Base: t.Base,
			Args: args,
			Loc:  t.Loc,
		}

		baseName := typeNameFromAst(gen.Base)
		if decl := g.structs[baseName]; decl != nil && len(decl.GenericParams) > 0 {
			depName := g.registerGenericStructInstance(decl, gen.Args)
			g.emitGenericStructInstance(depName, visiting)
			return
		}

		for _, arg := range args {
			g.emitGenericStructDepsForGenericArg(arg, subst, visiting)
		}
	}
}

func (g *Generator) emitGenericStructDepsForGenericArg(arg ast.GenericArg, subst map[string]ast.GenericArg, visiting map[string]bool) {
	switch arg.Kind {
	case ast.GenericArgType:
		g.emitGenericStructDepsForType(arg.Type, subst, visiting)

	case ast.GenericArgExpr:
		if id, ok := arg.Expr.(*ast.IdentExpr); ok {
			if replacement, exists := subst[id.Name.Name]; exists {
				if genericArgIsSingleNameForCGen(replacement, id.Name.Name) {
					return
				}

				g.emitGenericStructDepsForGenericArg(replacement, subst, visiting)
				return
			}
		}

		if gen, ok := arg.Expr.(*ast.GenericExpr); ok {
			if typ := typeAstFromExprForCGen(gen); typ != nil {
				g.emitGenericStructDepsForType(typ, subst, visiting)
			}
		}
	}
}

func genericArgIsSingleNameForCGen(arg ast.GenericArg, name string) bool {
	switch arg.Kind {
	case ast.GenericArgType:
		named, ok := arg.Type.(*ast.NamedType)
		return ok && len(named.Parts) == 1 && named.Parts[0].Name == name

	case ast.GenericArgExpr:
		id, ok := arg.Expr.(*ast.IdentExpr)
		return ok && id.Name.Name == name
	}

	return false
}

func (g *Generator) substituteGenericArgForCGen(arg ast.GenericArg, subst map[string]ast.GenericArg) ast.GenericArg {
	switch arg.Kind {
	case ast.GenericArgType:
		return ast.GenericArg{
			Kind: ast.GenericArgType,
			Type: g.substituteTypeAstForCGen(arg.Type, subst),
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
			Expr: g.substituteExprForCGen(arg.Expr, subst),
			Loc:  arg.Loc,
		}
	}

	return arg
}

func (g *Generator) substituteTypeAstForCGen(typ ast.Type, subst map[string]ast.GenericArg) ast.Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok := subst[t.Parts[0].Name]; ok {
				if typ := typeAstFromGenericArgForCGen(arg); typ != nil {
					return typ
				}
			}
		}

		return t

	case *ast.PointerType:
		return &ast.PointerType{
			Elem: g.substituteTypeAstForCGen(t.Elem, subst),
			Loc:  t.Loc,
		}

	case *ast.GenericType:
		args := make([]ast.GenericArg, 0, len(t.Args))
		for _, arg := range t.Args {
			args = append(args, g.substituteGenericArgForCGen(arg, subst))
		}

		return &ast.GenericType{
			Base: g.substituteTypeAstForCGen(t.Base, subst),
			Args: args,
			Loc:  t.Loc,
		}
	}

	return typ
}

func typeAstFromGenericArgForCGen(arg ast.GenericArg) ast.Type {
	switch arg.Kind {
	case ast.GenericArgType:
		return arg.Type

	case ast.GenericArgExpr:
		return typeAstFromExprForCGen(arg.Expr)
	}

	return nil
}

func (g *Generator) substituteExprForCGen(
	expr ast.Expr,
	subst map[string]ast.GenericArg,
) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if arg, ok := subst[e.Name.Name]; ok &&
			arg.Kind == ast.GenericArgExpr &&
			arg.Expr != nil {
			if genericArgIsSingleNameForCGen(
				arg,
				e.Name.Name,
			) {
				return e
			}

			return g.substituteExprForCGen(
				arg.Expr,
				subst,
			)
		}

		return e

	case *ast.UnaryExpr:
		return &ast.UnaryExpr{
			Op: e.Op,
			Expr: g.substituteExprForCGen(
				e.Expr,
				subst,
			),
			Loc: e.Loc,
		}

	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Left: g.substituteExprForCGen(
				e.Left,
				subst,
			),
			Op: e.Op,
			Right: g.substituteExprForCGen(
				e.Right,
				subst,
			),
			Loc: e.Loc,
		}

	case *ast.CallExpr:
		args := make(
			[]ast.Expr,
			0,
			len(e.Args),
		)

		for _, arg := range e.Args {
			args = append(
				args,
				g.substituteExprForCGen(
					arg,
					subst,
				),
			)
		}

		out := &ast.CallExpr{
			Callee: g.substituteExprForCGen(
				e.Callee,
				subst,
			),
			Args: args,
			Loc:  e.Loc,
		}

		if resolution, ok := g.lenResolutions[e]; ok {
			g.lenResolutions[out] = resolution
		}

		return out

	case *ast.GenericExpr:
		args := make(
			[]ast.GenericArg,
			0,
			len(e.Args),
		)

		for _, arg := range e.Args {
			args = append(
				args,
				g.substituteGenericArgForCGen(
					arg,
					subst,
				),
			)
		}

		out := &ast.GenericExpr{
			Base: g.substituteExprForCGen(
				e.Base,
				subst,
			),
			Args: args,
			Loc:  e.Loc,
		}

		// Generic task bodies are copied through AST substitution before
		// emission. Preserve the checker-selected overload candidate on the
		// copied expression, just as index and len resolutions are preserved.
		if resolution, ok :=
			g.genericOverloadCalls[e]; ok {
			g.genericOverloadCalls[out] =
				resolution
		}

		return out

	case *ast.SpreadExpr:
		return &ast.SpreadExpr{
			Expr: g.substituteExprForCGen(
				e.Expr,
				subst,
			),
			Loc: e.Loc,
		}

	case *ast.SelectorExpr:
		return &ast.SelectorExpr{
			Left: g.substituteExprForCGen(
				e.Left,
				subst,
			),
			Name: e.Name,
			Loc:  e.Loc,
		}

	case *ast.IndexExpr:
		out := &ast.IndexExpr{
			Left: g.substituteExprForCGen(
				e.Left,
				subst,
			),
			Index: g.substituteExprForCGen(
				e.Index,
				subst,
			),
			Loc: e.Loc,
		}

		if resolution, ok := g.indexResolutions[e]; ok {
			g.indexResolutions[out] = resolution
		}

		return out

	case *ast.CompoundLiteralExpr:
		fields := make(
			[]ast.LiteralField,
			0,
			len(e.Fields),
		)

		for _, field := range e.Fields {
			fields = append(
				fields,
				ast.LiteralField{
					Name: field.Name,
					Value: g.substituteExprForCGen(
						field.Value,
						subst,
					),
				},
			)
		}

		values := make(
			[]ast.Expr,
			0,
			len(e.Values),
		)

		for _, value := range e.Values {
			values = append(
				values,
				g.substituteExprForCGen(
					value,
					subst,
				),
			)
		}

		return &ast.CompoundLiteralExpr{
			Type: g.substituteTypeAstForCGen(
				e.Type,
				subst,
			),
			Fields: fields,
			Values: values,
			Loc:    e.Loc,
		}
	}

	return expr
}

func (g *Generator) cTypeFromAstWithGenericArgs(typ ast.Type, subst map[string]ast.GenericArg) CType {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok := subst[t.Parts[0].Name]; ok {
				return g.cTypeFromGenericArgWithGenericArgs(arg, subst)
			}
		}

		return g.cTypeFromAst(t)

	case *ast.PointerType:
		elem := g.cTypeFromAstWithGenericArgs(t.Elem, subst)

		return CType{
			Name:     elem.Name + " *",
			SealName: "*" + elem.SealName,
			Elem:     &elem,
		}

	case *ast.GenericType:
		args := make([]ast.GenericArg, 0, len(t.Args))
		for _, arg := range t.Args {
			args = append(args, g.substituteGenericArgForCGen(arg, subst))
		}

		return g.cTypeFromGenericType(&ast.GenericType{
			Base: t.Base,
			Args: args,
			Loc:  t.Loc,
		})
	}

	return g.cTypeFromAst(typ)
}

func (g *Generator) cTypeFromGenericArgWithGenericArgs(arg ast.GenericArg, subst map[string]ast.GenericArg) CType {
	switch arg.Kind {
	case ast.GenericArgType:
		return g.cTypeFromAstWithGenericArgs(arg.Type, subst)

	case ast.GenericArgExpr:
		if id, ok := arg.Expr.(*ast.IdentExpr); ok {
			if replacement, exists := subst[id.Name.Name]; exists {
				if genericArgIsSingleNameForCGen(replacement, id.Name.Name) {
					return CInvalid
				}

				return g.cTypeFromGenericArgWithGenericArgs(replacement, subst)
			}
		}

		return g.cTypeFromGenericArg(arg)
	}

	return CInvalid
}

func (g *Generator) constExprWithGenericArgs(expr ast.Expr, subst map[string]ast.GenericArg) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if arg, ok := subst[e.Name.Name]; ok && arg.Kind == ast.GenericArgExpr {
			if genericArgIsSingleNameForCGen(arg, e.Name.Name) {
				return e.Name.Name
			}

			return g.constExprWithGenericArgs(arg.Expr, subst)
		}

		return e.Name.Name

	case *ast.IntLitExpr:
		return e.Value

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.UnaryExpr:
		return fmt.Sprintf("(%s%s)", g.cUnaryOp(e.Op), g.constExprWithGenericArgs(e.Expr, subst))

	case *ast.BinaryExpr:
		return fmt.Sprintf("(%s %s %s)",
			g.constExprWithGenericArgs(e.Left, subst),
			g.cBinaryOp(e.Op),
			g.constExprWithGenericArgs(e.Right, subst),
		)
	}

	return g.emitExpr(g.substituteExprForCGen(expr, subst), nil)
}

func typeNameFromAst(t ast.Type) string {
	switch x := t.(type) {
	case *ast.NamedType:
		if len(x.Parts) == 0 {
			return ""
		}

		return x.Parts[len(x.Parts)-1].Name

	case *ast.GenericType:
		return typeNameFromAst(x.Base)

	default:
		return ""
	}
}

func typeNameFromGenericArg(arg ast.GenericArg) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return typeNameFromAst(arg.Type)

	case ast.GenericArgExpr:
		switch e := arg.Expr.(type) {
		case *ast.IdentExpr:
			return e.Name.Name

		case *ast.SelectorExpr:
			return e.Name.Name
		}
	}

	return ""
}

func (g *Generator) cTypeFromGenericArg(arg ast.GenericArg) CType {
	if g.genericSubst != nil {
		switch arg.Kind {
		case ast.GenericArgExpr:
			if id, ok := arg.Expr.(*ast.IdentExpr); ok {
				if replacement, exists := g.genericSubst[id.Name.Name]; exists {
					if genericArgIsSingleNameForCGen(replacement, id.Name.Name) {
						return CInvalid
					}

					return g.cTypeFromGenericArgWithGenericArgs(replacement, g.genericSubst)
				}
			}

		case ast.GenericArgType:
			return g.cTypeFromAstWithGenericArgs(arg.Type, g.genericSubst)
		}
	}

	switch arg.Kind {
	case ast.GenericArgType:
		if arg.Type == nil {
			return CInvalid
		}

		return g.cTypeFromAst(arg.Type)

	case ast.GenericArgExpr:
		switch e := arg.Expr.(type) {
		case *ast.IdentExpr:
			name := e.Name.Name

			if spec, ok := builtin.LookupType(name); ok {
				return CType{Name: spec.CName, SealName: spec.Name}
			}

			if _, ok := g.distincts[name]; ok {
				return CType{Name: name, SealName: name}
			}

			if _, ok := g.structs[name]; ok {
				return CType{Name: name, SealName: name}
			}

			if _, ok := g.enums[name]; ok {
				return CType{Name: name, SealName: name}
			}

			if _, ok := g.unions[name]; ok {
				return CType{Name: name, SealName: name}
			}

			if iface := g.interfaces[name]; iface != nil {
				return CType{
					Name:           name,
					SealName:       name,
					IsInterface:    true,
					IsDynInterface: iface.IsDyn,
				}
			}

			g.error(e.Span(), fmt.Sprintf("expected type argument, got %q", name))
			return CInvalid

		case *ast.SelectorExpr:
			if typ := typeAstFromExprForCGen(e); typ != nil {
				return g.cTypeFromAstInContext(typ)
			}

			g.error(e.Span(), "expected type argument")
			return CInvalid

		case *ast.GenericExpr:
			typ := typeAstFromExprForCGen(e)
			if typ == nil {
				g.error(e.Span(), "expected type argument")
				return CInvalid
			}

			return g.cTypeFromAstInContext(typ)

		default:
			g.error(arg.Span(), "expected type argument")
			return CInvalid
		}
	}

	return CInvalid
}

func (g *Generator) cTypeFromAstInContext(t ast.Type) CType {
	if g.genericSubst != nil {
		return g.cTypeFromAstWithGenericArgs(t, g.genericSubst)
	}

	return g.cTypeFromAst(t)
}

func (g *Generator) registerGenericTaskInstance(decl *ast.TaskDecl, args []ast.GenericArg) string {
	args = normalizeGenericArgsForCGenParams(decl.GenericParams, args)

	name := g.specializedTaskCName(decl, args)

	if _, exists := g.genericTasks[name]; exists {
		return name
	}

	copiedArgs := append([]ast.GenericArg(nil), args...)

	g.genericTasks[name] = &GenericTaskInstance{
		Name: name,
		Decl: decl,
		Args: copiedArgs,
	}

	return name
}

func (g *Generator) specializedTaskCName(decl *ast.TaskDecl, args []ast.GenericArg) string {
	parts := []string{}

	if g.packageName != "" {
		parts = append(parts, sanitizeCName(g.packageName))
	}

	parts = append(parts, sanitizeCName(decl.Name.Name))

	for i, arg := range args {
		paramCategory := ast.GenericParamInvalid
		if i < len(decl.GenericParams) {
			paramCategory = decl.GenericParams[i].Category
		}

		switch paramCategory {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			parts = append(parts, g.genericTypeArgCName(arg))

		case ast.GenericParamTask:
			parts = append(parts, g.genericTaskArgCName(arg))

		default:
			parts = append(parts, genericValueArgCName(arg))
		}
	}

	return strings.Join(parts, "_")
}

func genericTaskSubstForCGen(params []ast.GenericParam, args []ast.GenericArg) map[string]ast.GenericArg {
	return genericArgSubstForCGen(params, args)
}

func isBuiltinTypeName(name string) bool {
	return builtin.IsType(name)
}

func (g *Generator) isLocalValueName(name string) bool {
	if g.scope == nil {
		return false
	}

	_, ok := g.scope.lookup(name)
	return ok
}

func (g *Generator) cTypeFromSizeArg(expr ast.Expr) (CType, bool) {
	id, ok := expr.(*ast.IdentExpr)
	if !ok {
		return CInvalid, false
	}

	name := id.Name.Name

	if g.isLocalValueName(name) {
		return CInvalid, false
	}

	if g.genericSubst != nil {
		if arg, ok := g.genericSubst[name]; ok {
			switch arg.Kind {
			case ast.GenericArgType:
				return g.cTypeFromAstWithGenericArgs(arg.Type, g.genericSubst), true

			case ast.GenericArgExpr:
				if typ := typeAstFromExprForCGen(arg.Expr); typ != nil {
					ct := g.cTypeFromAstWithGenericArgs(typ, g.genericSubst)
					if ct.SealName != "<invalid>" {
						return ct, true
					}
				}
			}
		}
	}

	if isBuiltinTypeName(name) ||
		g.distincts[name] != nil ||
		g.structs[name] != nil ||
		g.enums[name] != nil ||
		g.unions[name] != nil ||
		g.interfaces[name] != nil {
		return g.cTypeFromAst(&ast.NamedType{
			Parts: []ast.Ident{id.Name},
		}), true
	}

	return CInvalid, false
}

func (g *Generator) emitSizeCall(e *ast.CallExpr) string {
	if len(e.Args) != 1 {
		g.error(e.Span(), "size expects 1 argument")
		return "0"
	}

	if typ, ok := g.cTypeFromSizeArg(e.Args[0]); ok {
		return fmt.Sprintf(
			"(uintptr_t)sizeof(%s)",
			typ.Name,
		)
	}

	argType := g.inferExprType(e.Args[0], nil)
	value := g.emitExpr(e.Args[0], nil)

	switch argType.SealName {
	case "string":
		return fmt.Sprintf(
			"(uintptr_t)(%s).len",
			value,
		)

	case "cstring":
		return fmt.Sprintf(
			"seal_cstring_byte_len(%s)",
			value,
		)
	}

	return fmt.Sprintf(
		"(uintptr_t)sizeof(%s)",
		value,
	)
}

func (g *Generator) emitNoArgRuntimeCall(name string, cName string, e *ast.CallExpr) string {
	if len(e.Args) != 0 {
		g.error(e.Span(), fmt.Sprintf("%s expects 0 arguments", name))
	}

	return cName + "()"
}

func (g *Generator) emitPanicCall(e *ast.CallExpr) string {
	if len(e.Args) == 0 {
		return "seal_panic_empty()"
	}

	if len(e.Args) != 1 {
		g.error(e.Span(), "panic expects 0 or 1 argument")
		return "seal_panic_empty()"
	}

	argType := g.inferExprType(e.Args[0], nil)
	arg := g.emitExpr(e.Args[0], &argType)

	switch argType.SealName {
	case "string":
		return fmt.Sprintf("seal_panic_string(%s)", arg)

	case "cstring":
		return fmt.Sprintf("seal_panic_cstring(%s)", arg)

	default:
		g.error(e.Args[0].Span(), fmt.Sprintf("panic expects string or cstring, got %s", argType.String()))
		return "seal_panic_empty()"
	}
}

func (g *Generator) emitAssertCall(e *ast.CallExpr) string {
	if len(e.Args) != 1 {
		g.error(e.Span(), "assert expects 1 argument")
		return "assert(false)"
	}

	cond := g.emitExpr(e.Args[0], &CBool)
	return fmt.Sprintf("assert(%s)", cond)
}

func (g *Generator) emitDistincts(file *ast.File) {
	emitted := false

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.DistinctDecl)
		if !ok {
			continue
		}

		underlying := g.cTypeFromAst(d.Underlying)
		g.linef("typedef %s %s;", underlying.Name, d.Name.Name)
		emitted = true
	}

	if emitted {
		g.line("")
	}
}

func (g *Generator) emitEnumDefinition(
	cName string,
	d *ast.EnumDecl,
	underlying *CType,
) {
	if d == nil || cName == "" {
		return
	}

	// Avoid invalid empty C enums even if an earlier compiler phase allowed
	// one through. An empty enum is represented as its chosen integer type.
	if len(d.Variants) == 0 {
		base := CInt

		if underlying != nil &&
			!isInvalidCType(*underlying) {
			base = *underlying
		}

		g.linef(
			"typedef %s %s;",
			base.Name,
			cName,
		)
		g.line("")
		return
	}

	// Without an explicit Seal underlying type, retain the existing C enum
	// representation.
	if underlying == nil {
		g.linef("typedef enum %s {", cName)
		g.indent++

		for i, variant := range d.Variants {
			comma := ","
			if i == len(d.Variants)-1 {
				comma = ""
			}

			g.linef(
				"%s_%s%s",
				cName,
				variant.Name,
				comma,
			)
		}

		g.indent--
		g.linef("} %s;", cName)
		g.line("")
		return
	}

	base := *underlying

	if isInvalidCType(base) {
		g.error(
			d.Underlying.Span(),
			fmt.Sprintf(
				"cannot lower enum %s with invalid underlying type",
				d.Name.Name,
			),
		)

		base = CInt
	}

	// C11 does not provide portable syntax for fixing the underlying storage
	// type of a C enum. Use an integer typedef for the value representation
	// and an anonymous enum for integer constant expressions.
	//
	// This gives values of `enum u32` exact uint32_t storage while keeping
	// variants usable in switch case labels.
	g.linef(
		"typedef %s %s;",
		base.Name,
		cName,
	)

	g.line("enum {")
	g.indent++

	for i, variant := range d.Variants {
		comma := ","
		if i == len(d.Variants)-1 {
			comma = ""
		}

		g.linef(
			"%s_%s = %d%s",
			cName,
			variant.Name,
			i,
			comma,
		)
	}

	g.indent--
	g.line("};")
	g.line("")
}

func (g *Generator) emitEnums(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.EnumDecl)
		if !ok {
			continue
		}

		var underlying *CType

		if d.Underlying != nil {
			converted := g.cTypeFromAst(d.Underlying)
			underlying = &converted
		}

		g.emitEnumDefinition(
			d.Name.Name,
			d,
			underlying,
		)
	}
}

func (g *Generator) emitImportedEnums() {
	if len(g.packages) == 0 {
		return
	}

	packageNames := make(
		[]string,
		0,
		len(g.packages),
	)

	for packageName := range g.packages {
		packageNames = append(
			packageNames,
			packageName,
		)
	}

	sort.Strings(packageNames)

	for _, packageName := range packageNames {
		pkg := g.packages[packageName]
		if pkg == nil {
			continue
		}

		enumNames := make(
			[]string,
			0,
			len(pkg.Enums),
		)

		for enumName := range pkg.Enums {
			enumNames = append(
				enumNames,
				enumName,
			)
		}

		sort.Strings(enumNames)

		for _, enumName := range enumNames {
			d := pkg.Enums[enumName]
			if d == nil {
				continue
			}

			var underlying *CType

			if d.Underlying != nil {
				converted :=
					g.cTypeFromAstInTypeContext(
						packageName,
						d.Underlying,
					)

				underlying = &converted
			}

			g.emitEnumDefinition(
				cImportedTypeName(
					packageName,
					enumName,
				),
				d,
				underlying,
			)
		}
	}
}

func (g *Generator) emitImportedStructs() {
	if len(g.packages) == 0 {
		return
	}

	pkgNames := make([]string, 0, len(g.packages))
	for pkgName := range g.packages {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)

	emitted := false

	for _, pkgName := range pkgNames {
		pkg := g.packages[pkgName]
		if pkg == nil {
			continue
		}

		structNames := make([]string, 0, len(pkg.Structs))
		for name := range pkg.Structs {
			structNames = append(structNames, name)
		}
		sort.Strings(structNames)

		for _, structName := range structNames {
			decl := pkg.Structs[structName]
			if decl == nil || decl.IsIntrinsic || len(decl.GenericParams) > 0 {
				continue
			}

			cName := cImportedTypeName(pkgName, structName)

			g.linef("typedef struct %s {", cName)
			g.indent++

			for _, field := range decl.Fields {
				fieldType := g.cTypeFromAstInTypeContext(pkgName, field.Type)
				g.linef("%s;", fieldType.Decl(field.Name.Name))
			}

			g.indent--
			g.linef("} %s;", cName)
			g.line("")

			emitted = true
		}
	}

	if emitted {
		g.line("")
	}
}

func externalConcreteTypeKey(
	packageName string,
	typeName string,
) string {
	return packageName + "." + typeName
}

func (g *Generator) addRequiredExternalType(
	packageName string,
	typeName string,
) {
	if packageName == "" ||
		typeName == "" ||
		packageName == g.packageName {
		return
	}

	// Ordinary dependencies are already emitted by emitImportedStructs,
	// emitImportedEnums, and the other normal imported-type emitters.
	if g.packages[packageName] != nil {
		return
	}

	pkg := g.typePackageInfo(packageName)
	if pkg == nil ||
		!g.packageHasType(pkg, typeName) {
		return
	}

	// Interface representations use the existing interface-instance system.
	if iface := pkg.Interfaces[typeName]; iface != nil {
		if len(iface.GenericParams) == 0 {
			g.registerInterfaceInstance(
				packageName,
				typeName,
				iface,
				nil,
			)
		}

		return
	}

	key := externalConcreteTypeKey(
		packageName,
		typeName,
	)

	g.requiredExternalTypes[key] = externalConcreteType{
		PackageName: packageName,
		TypeName:    typeName,
	}
}

func (g *Generator) qualifiedGenericTypeForOwner(
	ownerPackage string,
	typ *ast.GenericType,
) *ast.GenericType {
	if typ == nil {
		return nil
	}

	if _, _, qualified :=
		packageTypeNameFromAst(typ.Base); qualified {
		return typ
	}

	baseName := typeNameFromAst(typ.Base)
	if ownerPackage == "" || baseName == "" {
		return typ
	}

	return &ast.GenericType{
		Base: &ast.NamedType{
			Parts: []ast.Ident{
				{Name: ownerPackage},
				{Name: baseName},
			},
			Loc: typ.Base.Span(),
		},
		Args: append(
			[]ast.GenericArg(nil),
			typ.Args...,
		),
		Loc: typ.Loc,
	}
}

func (g *Generator) collectRequiredExternalTypeFromArg(
	ownerPackage string,
	arg ast.GenericArg,
) {
	switch arg.Kind {
	case ast.GenericArgType:
		g.collectRequiredExternalTypeFromType(
			ownerPackage,
			arg.Type,
		)

	case ast.GenericArgExpr:
		if typ := typeAstFromExprForCGen(arg.Expr); typ != nil {
			g.collectRequiredExternalTypeFromType(
				ownerPackage,
				typ,
			)
		}
	}
}

func (g *Generator) collectRequiredExternalTypeFromType(
	ownerPackage string,
	typ ast.Type,
) {
	if typ == nil {
		return
	}

	switch t := typ.(type) {
	case *ast.InterfaceSelfType:
		return

	case *ast.NamedType:
		if len(t.Parts) == 0 {
			return
		}

		packageName := ownerPackage
		typeName := t.Parts[len(t.Parts)-1].Name

		if len(t.Parts) >= 2 {
			packageName = t.Parts[0].Name
		} else if builtin.IsType(typeName) {
			return
		}

		g.addRequiredExternalType(
			packageName,
			typeName,
		)

	case *ast.PointerType:
		g.collectRequiredExternalTypeFromType(
			ownerPackage,
			t.Elem,
		)

	case *ast.GenericType:
		qualified :=
			g.qualifiedGenericTypeForOwner(
				ownerPackage,
				t,
			)

		packageName, typeName, ok :=
			packageTypeNameFromAst(qualified.Base)

		if ok {
			pkg := g.typePackageInfo(packageName)

			if pkg != nil {
				if decl := pkg.Structs[typeName]; decl != nil &&
					len(decl.GenericParams) > 0 {
					// Register the concrete external generic structure so the
					// existing imported-generic-struct emitter can generate it.
					_ = g.cTypeFromGenericType(
						qualified,
					)
				} else {
					g.addRequiredExternalType(
						packageName,
						typeName,
					)
				}
			}
		}

		for _, arg := range t.Args {
			g.collectRequiredExternalTypeFromArg(
				ownerPackage,
				arg,
			)
		}
	}
}

func (g *Generator) collectRequiredExternalTypes() {
	collectArgs := func(
		ownerPackage string,
		params []ast.GenericParam,
		args []ast.GenericArg,
	) {
		for i, arg := range args {
			category := ast.GenericParamInvalid

			if i < len(params) {
				category = params[i].Category
			}

			switch category {
			case ast.GenericParamType,
				ast.GenericParamEnum,
				ast.GenericParamUnion:
				g.collectRequiredExternalTypeFromArg(
					ownerPackage,
					arg,
				)
			}
		}
	}

	for _, instance := range g.genericTasks {
		if instance == nil ||
			instance.Decl == nil {
			continue
		}

		collectArgs(
			g.packageName,
			instance.Decl.GenericParams,
			instance.Args,
		)
	}

	for _, instance := range g.genericStructs {
		if instance == nil ||
			instance.Decl == nil {
			continue
		}

		collectArgs(
			g.packageName,
			instance.Decl.GenericParams,
			instance.Args,
		)
	}

	// Discover external types referenced by fields or underlying types of
	// other required external types.
	processed := map[string]bool{}

	for {
		var pending []string

		for key := range g.requiredExternalTypes {
			if !processed[key] {
				pending = append(pending, key)
			}
		}

		if len(pending) == 0 {
			break
		}

		sort.Strings(pending)

		for _, key := range pending {
			processed[key] = true

			ref := g.requiredExternalTypes[key]
			pkg := g.typePackageInfo(
				ref.PackageName,
			)

			if pkg == nil {
				continue
			}

			if decl := pkg.Structs[ref.TypeName]; decl != nil &&
				len(decl.GenericParams) == 0 {
				for _, field := range decl.Fields {
					g.collectRequiredExternalTypeFromType(
						ref.PackageName,
						field.Type,
					)
				}
			}

			if decl := pkg.Distincts[ref.TypeName]; decl != nil {
				g.collectRequiredExternalTypeFromType(
					ref.PackageName,
					decl.Underlying,
				)
			}

			if decl := pkg.Enums[ref.TypeName]; decl != nil &&
				decl.Underlying != nil {
				g.collectRequiredExternalTypeFromType(
					ref.PackageName,
					decl.Underlying,
				)
			}

			if decl := pkg.Unions[ref.TypeName]; decl != nil {
				for _, member := range decl.Members {
					g.collectRequiredExternalTypeFromType(
						ref.PackageName,
						member,
					)
				}
			}
		}
	}
}

func (g *Generator) requiredExternalKeyForNamedType(
	ownerPackage string,
	typ *ast.NamedType,
) (string, bool) {
	if typ == nil || len(typ.Parts) == 0 {
		return "", false
	}

	packageName := ownerPackage
	typeName := typ.Parts[len(typ.Parts)-1].Name

	if len(typ.Parts) >= 2 {
		packageName = typ.Parts[0].Name
	}

	key := externalConcreteTypeKey(
		packageName,
		typeName,
	)

	_, ok := g.requiredExternalTypes[key]
	return key, ok
}

func (g *Generator) emitRequiredExternalDependencies(
	ownerPackage string,
	typ ast.Type,
	visiting map[string]bool,
) {
	switch t := typ.(type) {
	case *ast.NamedType:
		key, ok :=
			g.requiredExternalKeyForNamedType(
				ownerPackage,
				t,
			)

		if ok {
			g.emitRequiredExternalTypeDefinition(
				key,
				visiting,
			)
		}

	case *ast.PointerType:
		// A forward declaration is sufficient for pointer fields.
		return

	case *ast.GenericType:
		for _, arg := range t.Args {
			if arg.Kind == ast.GenericArgType {
				g.emitRequiredExternalDependencies(
					ownerPackage,
					arg.Type,
					visiting,
				)
			}
		}

		qualified :=
			g.qualifiedGenericTypeForOwner(
				ownerPackage,
				t,
			)

		concrete := g.cTypeFromGenericType(
			qualified,
		)

		if info :=
			g.importedGenericStructs[concrete.Name]; info != nil {
			g.emitImportedGenericStructInstance(
				info.Name,
				map[string]bool{},
			)
		}
	}
}

func (g *Generator) emitRequiredExternalUnion(
	ref externalConcreteType,
	decl *ast.UnionDecl,
) {
	cName := cImportedTypeName(
		ref.PackageName,
		ref.TypeName,
	)

	g.linef("typedef enum %s_Tag {", cName)
	g.indent++
	g.linef("%s_Tag_nil = 0,", cName)

	for i, member := range decl.Members {
		memberType :=
			g.cTypeFromAstInTypeContext(
				ref.PackageName,
				member,
			)

		comma := ","
		if i == len(decl.Members)-1 {
			comma = ""
		}

		g.linef(
			"%s_Tag_%s%s",
			cName,
			memberType.SealName,
			comma,
		)
	}

	g.indent--
	g.linef("} %s_Tag;", cName)
	g.line("")

	g.linef("typedef struct %s {", cName)
	g.indent++
	g.linef("%s_Tag tag;", cName)
	g.line("union {")
	g.indent++

	for _, member := range decl.Members {
		memberType :=
			g.cTypeFromAstInTypeContext(
				ref.PackageName,
				member,
			)

		g.linef(
			"%s;",
			memberType.Decl(
				sanitizeCName(
					memberType.SealName,
				),
			),
		)
	}

	g.indent--
	g.line("} as;")
	g.indent--
	g.linef("} %s;", cName)
	g.line("")
}

func (g *Generator) emitRequiredExternalTypeDefinition(
	key string,
	visiting map[string]bool,
) {
	if g.emittedRequiredExternalTypes[key] {
		return
	}

	if visiting[key] {
		ref := g.requiredExternalTypes[key]

		g.error(
			source.Span{},
			fmt.Sprintf(
				"recursive by-value external type dependency %s.%s",
				ref.PackageName,
				ref.TypeName,
			),
		)
		return
	}

	ref, ok := g.requiredExternalTypes[key]
	if !ok {
		return
	}

	pkg := g.typePackageInfo(ref.PackageName)
	if pkg == nil {
		return
	}

	visiting[key] = true

	cName := cImportedTypeName(
		ref.PackageName,
		ref.TypeName,
	)

	switch {
	case pkg.Distincts[ref.TypeName] != nil:
		decl := pkg.Distincts[ref.TypeName]

		g.emitRequiredExternalDependencies(
			ref.PackageName,
			decl.Underlying,
			visiting,
		)

		underlying :=
			g.cTypeFromAstInTypeContext(
				ref.PackageName,
				decl.Underlying,
			)

		g.linef(
			"typedef %s %s;",
			underlying.Name,
			cName,
		)
		g.line("")

	case pkg.Enums[ref.TypeName] != nil:
		decl := pkg.Enums[ref.TypeName]

		var underlying *CType

		if decl.Underlying != nil {
			converted :=
				g.cTypeFromAstInTypeContext(
					ref.PackageName,
					decl.Underlying,
				)

			underlying = &converted
		}

		g.emitEnumDefinition(
			cName,
			decl,
			underlying,
		)

	case pkg.Unions[ref.TypeName] != nil:
		decl := pkg.Unions[ref.TypeName]

		for _, member := range decl.Members {
			g.emitRequiredExternalDependencies(
				ref.PackageName,
				member,
				visiting,
			)
		}

		g.emitRequiredExternalUnion(
			ref,
			decl,
		)

	case pkg.Structs[ref.TypeName] != nil:
		decl := pkg.Structs[ref.TypeName]

		if len(decl.GenericParams) != 0 {
			break
		}

		for _, field := range decl.Fields {
			g.emitRequiredExternalDependencies(
				ref.PackageName,
				field.Type,
				visiting,
			)
		}

		// The typedef forward declaration is emitted by
		// emitRequiredExternalTypes.
		g.linef("struct %s {", cName)
		g.indent++

		for _, field := range decl.Fields {
			fieldType :=
				g.cTypeFromAstInTypeContext(
					ref.PackageName,
					field.Type,
				)

			g.linef(
				"%s;",
				fieldType.Decl(
					field.Name.Name,
				),
			)
		}

		g.indent--
		g.line("};")
		g.line("")
	}

	visiting[key] = false
	g.emittedRequiredExternalTypes[key] = true
}

func (g *Generator) emitRequiredExternalTypes() {
	if len(g.requiredExternalTypes) == 0 {
		return
	}

	keys := make(
		[]string,
		0,
		len(g.requiredExternalTypes),
	)

	for key := range g.requiredExternalTypes {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	// Forward-declare every required non-generic structure first so pointer
	// fields can refer to mutually dependent structures.
	emittedForward := false

	for _, key := range keys {
		ref := g.requiredExternalTypes[key]
		pkg := g.typePackageInfo(ref.PackageName)

		if pkg == nil {
			continue
		}

		decl := pkg.Structs[ref.TypeName]
		if decl == nil ||
			len(decl.GenericParams) != 0 {
			continue
		}

		cName := cImportedTypeName(
			ref.PackageName,
			ref.TypeName,
		)

		g.linef(
			"typedef struct %s %s;",
			cName,
			cName,
		)

		emittedForward = true
	}

	if emittedForward {
		g.line("")
	}

	visiting := map[string]bool{}

	for _, key := range keys {
		g.emitRequiredExternalTypeDefinition(
			key,
			visiting,
		)
	}
}

func (g *Generator) emitImportedGenericStructs() {
	names := make([]string, 0, len(g.importedGenericStructs))
	for name := range g.importedGenericStructs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		g.emitImportedGenericStructInstance(name, map[string]bool{})
	}
}

func (g *Generator) emitImportedGenericStructInstance(name string, visiting map[string]bool) {
	if g.emittedImportedGenericStructs[name] {
		return
	}

	if isInvalidCStructName(name) {
		g.emittedImportedGenericStructs[name] = true
		return
	}

	info := g.importedGenericStructs[name]
	if info == nil || info.Decl == nil || isInvalidCStructName(info.Name) || isInvalidCStructName(info.TypeName) || isInvalidCStructName(info.Decl.Name.Name) {
		g.emittedImportedGenericStructs[name] = true
		return
	}

	if visiting[name] {
		g.error(info.Decl.Name.Span(), fmt.Sprintf("recursive imported generic struct instantiation %s is not supported yet", name))
		return
	}

	visiting[name] = true

	subst := genericArgSubstForCGen(info.Decl.GenericParams, info.Args)

	g.withTypeContext(info.PackageName, func() {
		for _, field := range info.Decl.Fields {
			g.emitImportedGenericStructDepsForType(field.Type, subst, visiting)
		}
	})

	g.linef("typedef struct %s {", info.Name)
	g.indent++

	g.withTypeContext(info.PackageName, func() {
		for _, field := range info.Decl.Fields {
			fieldType := g.cTypeFromAstWithGenericArgs(field.Type, subst)
			g.linef("%s;", fieldType.Decl(field.Name.Name))
		}
	})

	g.indent--
	g.linef("} %s;", info.Name)
	g.line("")

	g.emittedImportedGenericStructs[name] = true
	visiting[name] = false
}

func (g *Generator) emitImportedGenericStructDepsForType(typ ast.Type, subst map[string]ast.GenericArg, visiting map[string]bool) {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			name := t.Parts[0].Name

			if arg, ok := subst[name]; ok {
				if genericArgIsSingleNameForCGen(arg, name) {
					return
				}

				g.emitImportedGenericStructDepsForGenericArg(
					arg,
					subst,
					visiting,
				)
			}
		}

	case *ast.PointerType:
		g.emitImportedGenericStructDepsForType(t.Elem, subst, visiting)

	case *ast.GenericType:
		args := make([]ast.GenericArg, 0, len(t.Args))
		for _, arg := range t.Args {
			args = append(args, g.substituteGenericArgForCGen(arg, subst))
		}

		gen := &ast.GenericType{
			Base: t.Base,
			Args: args,
			Loc:  t.Loc,
		}

		ct := g.cTypeFromGenericType(gen)

		if info := g.importedGenericStructs[ct.Name]; info != nil {
			g.emitImportedGenericStructInstance(info.Name, visiting)
			return
		}

		for _, arg := range args {
			g.emitImportedGenericStructDepsForGenericArg(arg, subst, visiting)
		}
	}
}

func (g *Generator) emitImportedGenericStructDepsForGenericArg(arg ast.GenericArg, subst map[string]ast.GenericArg, visiting map[string]bool) {
	switch arg.Kind {
	case ast.GenericArgType:
		g.emitImportedGenericStructDepsForType(arg.Type, subst, visiting)

	case ast.GenericArgExpr:
		if id, ok := arg.Expr.(*ast.IdentExpr); ok {
			if replacement, exists := subst[id.Name.Name]; exists {
				if genericArgIsSingleNameForCGen(replacement, id.Name.Name) {
					return
				}

				g.emitImportedGenericStructDepsForGenericArg(replacement, subst, visiting)
				return
			}
		}

		if typ := typeAstFromExprForCGen(arg.Expr); typ != nil {
			g.emitImportedGenericStructDepsForType(typ, subst, visiting)
		}
	}
}

func (g *Generator) emitStructs(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.StructDecl)
		if !ok {
			continue
		}

		if d.IsIntrinsic {
			continue
		}

		if len(d.GenericParams) > 0 {
			continue
		}

		if isInvalidCStructName(d.Name.Name) {
			continue
		}

		g.linef("typedef struct %s {", d.Name.Name)
		g.indent++

		for _, field := range d.Fields {
			fieldType := g.cTypeFromAst(field.Type)
			g.linef("%s;", fieldType.Decl(field.Name.Name))
		}

		g.indent--
		g.linef("} %s;", d.Name.Name)
		g.line("")
	}
}

func (g *Generator) emitConstants(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.ConstDecl)
		if !ok {
			continue
		}

		typ := g.inferExprType(d.Value, nil)
		value := g.emitExpr(d.Value, &typ)
		g.linef("static const %s = %s;", typ.Decl(d.Name.Name), value)
	}

	if len(g.consts) > 0 {
		g.line("")
	}
}

func (g *Generator) emitImportedResultStructs() {
	if len(g.packages) == 0 {
		return
	}

	names := make(
		[]string,
		0,
		len(g.packages),
	)

	for name := range g.packages {
		names = append(names, name)
	}

	sort.Strings(names)

	emitted := false
	seen := map[string]bool{}

	for _, pkgName := range names {
		pkg := g.packages[pkgName]
		if pkg == nil {
			continue
		}

		taskNames := make(
			[]string,
			0,
			len(pkg.Tasks),
		)

		for taskName := range pkg.Tasks {
			taskNames = append(
				taskNames,
				taskName,
			)
		}

		sort.Strings(taskNames)

		for _, taskName := range taskNames {
			rawInfo := pkg.Tasks[taskName]

			// Generic result structures are emitted by
			// emitImportedGenericResultStructs.
			if len(rawInfo.GenericParams) != 0 {
				continue
			}

			info := g.taskInfoInPackageContext(
				pkgName,
				taskName,
				rawInfo,
			)

			if len(info.ReturnTypes) <= 1 {
				continue
			}

			name := info.ReturnType.Name

			if name == "" {
				name = packageTaskResultStructName(
					pkgName,
					taskName,
				)
			}

			if seen[name] {
				continue
			}

			seen[name] = true

			g.linef(
				"typedef struct %s {",
				name,
			)
			g.indent++

			for i, resultType := range info.ReturnTypes {
				g.linef(
					"%s;",
					resultType.Decl(
						fmt.Sprintf(
							"_%d",
							i,
						),
					),
				)
			}

			g.indent--
			g.linef(
				"} %s;",
				name,
			)
			g.line("")

			emitted = true
		}
	}

	if emitted {
		g.line("")
	}
}

func (g *Generator) emitGenericResultStructs() {
	names := make([]string, 0, len(g.genericTasks))
	for name := range g.genericTasks {
		names = append(names, name)
	}
	sort.Strings(names)

	emitted := false

	for _, name := range names {
		info := g.genericTasks[name]
		if info == nil || info.Decl == nil || len(info.Decl.Results) <= 1 {
			continue
		}

		resultTypes := g.genericTaskReturnTypes(info)
		resultName := g.genericTaskResultStructName(info.Name)

		g.linef("typedef struct %s {", resultName)
		g.indent++

		for i, resultType := range resultTypes {
			g.linef("%s;", resultType.Decl(fmt.Sprintf("_%d", i)))
		}

		g.indent--
		g.linef("} %s;", resultName)
		g.line("")

		emitted = true
	}

	if emitted {
		g.line("")
	}
}

func (g *Generator) emitResultStructs(file *ast.File) {
	emitted := false

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.TaskDecl)
		if !ok || d.IsTest || len(d.GenericParams) > 0 || len(d.Results) <= 1 {
			continue
		}

		info := g.tasks[d.Name.Name]

		g.linef("typedef struct %s {", info.ReturnType.Name)
		g.indent++

		for i, resultType := range info.ReturnTypes {
			g.linef("%s;", resultType.Decl(fmt.Sprintf("_%d", i)))
		}

		g.indent--
		g.linef("} %s;", info.ReturnType.Name)
		g.line("")

		emitted = true
	}

	if emitted {
		g.line("")
	}
}

func (g *Generator) emitImportedGenericResultStructs() {
	names := make([]string, 0, len(g.importedGenericTasks))
	for name := range g.importedGenericTasks {
		names = append(names, name)
	}
	sort.Strings(names)

	emitted := false

	for _, name := range names {
		info := g.importedGenericTasks[name]
		if info == nil {
			continue
		}

		resultTypes := g.importedGenericTaskReturnTypes(info)
		if len(resultTypes) <= 1 {
			continue
		}

		resultName := g.importedGenericTaskResultStructName(info.Name)

		g.linef("typedef struct %s {", resultName)
		g.indent++

		for i, resultType := range resultTypes {
			g.linef("%s;", resultType.Decl(fmt.Sprintf("_%d", i)))
		}

		g.indent--
		g.linef("} %s;", resultName)
		g.line("")

		emitted = true
	}

	if emitted {
		g.line("")
	}
}

func (g *Generator) emitImportedGenericTaskPrototypes() {
	names := make([]string, 0, len(g.importedGenericTasks))
	for name := range g.importedGenericTasks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := g.importedGenericTasks[name]
		if info == nil {
			continue
		}

		g.linef("%s;", g.importedGenericTaskSignature(info))
	}

	if len(names) > 0 {
		g.line("")
	}
}

func (g *Generator) emitImportedTaskPrototypes() {
	if len(g.packages) == 0 {
		return
	}

	names := make([]string, 0, len(g.packages))
	for name := range g.packages {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, pkgName := range names {
		pkg := g.packages[pkgName]
		if pkg == nil {
			continue
		}

		taskNames := make([]string, 0, len(pkg.Tasks))
		for taskName := range pkg.Tasks {
			taskNames = append(taskNames, taskName)
		}
		sort.Strings(taskNames)

		for _, taskName := range taskNames {
			info := pkg.Tasks[taskName]
			if taskName == "Main" || info.IsIntrinsic || len(info.GenericParams) > 0 {
				continue
			}

			g.linef("%s;", g.packageTaskSignature(pkgName, taskName, info))
		}
	}

	g.line("")
}

func (g *Generator) packageTaskSignature(
	packageName string,
	taskName string,
	info TaskInfo,
) string {
	info = g.taskInfoInPackageContext(
		packageName,
		taskName,
		info,
	)

	name := cImportedTaskName(
		packageName,
		taskName,
		info,
	)

	ret := info.ReturnType.Name

	if len(info.ParamTypes) == 0 {
		return fmt.Sprintf(
			"%s %s(void)",
			ret,
			name,
		)
	}

	var params []string

	for i, paramType := range info.ParamTypes {
		if i < len(info.ParamIsVariadic) &&
			info.ParamIsVariadic[i] {
			if info.IsExtern {
				params = append(params, "...")
				break
			}

			params = append(
				params,
				g.variadicCType(paramType).Decl(
					fmt.Sprintf("arg%d", i),
				),
			)
			break
		}

		params = append(
			params,
			paramType.Decl(
				fmt.Sprintf("arg%d", i),
			),
		)
	}

	return fmt.Sprintf(
		"%s %s(%s)",
		ret,
		name,
		strings.Join(params, ", "),
	)
}

func (g *Generator) taskInfoInPackageContext(
	packageName string,
	taskName string,
	info TaskInfo,
) TaskInfo {
	if packageName == "" ||
		packageName == g.packageName {
		return info
	}

	out := info

	// TaskInfo stores CTypes as seen by the package that declared the task.
	// Rebuild them from the source AST so local package types such as
	// CAllocator become mem_CAllocator in the importing translation unit.
	if len(info.ParamTypeAsts) > 0 {
		out.ParamTypes = make(
			[]CType,
			0,
			len(info.ParamTypeAsts),
		)

		for _, paramType := range info.ParamTypeAsts {
			out.ParamTypes = append(
				out.ParamTypes,
				g.cTypeFromAstInTypeContext(
					packageName,
					paramType,
				),
			)
		}
	}

	if len(info.ResultTypeAsts) > 0 {
		out.ReturnTypes = make(
			[]CType,
			0,
			len(info.ResultTypeAsts),
		)

		for _, resultType := range info.ResultTypeAsts {
			out.ReturnTypes = append(
				out.ReturnTypes,
				g.cTypeFromAstInTypeContext(
					packageName,
					resultType,
				),
			)
		}

		switch len(out.ReturnTypes) {
		case 1:
			out.ReturnType = out.ReturnTypes[0]

		default:
			name := packageTaskResultStructName(
				packageName,
				taskName,
			)

			out.ReturnType = CType{
				Name:     name,
				SealName: name,
			}
		}
	} else if len(info.ReturnTypes) == 0 {
		out.ReturnType = CVoid
		out.ReturnTypes = nil
	}

	return out
}

func (g *Generator) emitTaskPrototypes(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.TaskDecl)
		if !ok || d.IsTest || d.IsIntrinsic {
			continue
		}

		if len(d.GenericParams) > 0 {
			continue
		}

		g.linef("%s;", g.taskSignature(d, false))
	}

	if len(g.tasks) > 0 {
		g.line("")
	}
}

func (g *Generator) emitGenericTaskPrototypes() {
	names := make([]string, 0, len(g.genericTasks))
	for name := range g.genericTasks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := g.genericTasks[name]
		if info == nil {
			continue
		}

		g.linef("%s;", g.genericTaskSignature(info, false))
	}

	if len(names) > 0 {
		g.line("")
	}
}

func (g *Generator) emitGenericTasks() {
	for {
		names := make([]string, 0, len(g.genericTasks))
		for name := range g.genericTasks {
			if !g.emittedGenericTasks[name] {
				names = append(names, name)
			}
		}

		if len(names) == 0 {
			return
		}

		sort.Strings(names)

		for _, name := range names {
			g.emitGenericTaskInstance(name)
		}
	}
}

func (g *Generator) emitGenericTaskInstance(name string) {
	if g.emittedGenericTasks[name] {
		return
	}

	info := g.genericTasks[name]
	if info == nil {
		return
	}

	g.emittedGenericTasks[name] = true

	decl := info.Decl
	subst := genericTaskSubstForCGen(decl.GenericParams, info.Args)

	oldSubst := g.genericSubst
	oldTask := g.currentTask
	oldGenericTaskName := g.currentGenericTaskName
	oldResults := g.currentResults

	g.genericSubst = subst
	g.currentTask = decl
	g.currentGenericTaskName = name
	g.currentResults = nil

	for _, result := range decl.Results {
		g.currentResults = append(g.currentResults, g.cTypeFromAstWithGenericArgs(result, subst))
	}

	g.linef("%s {", g.genericTaskSignature(info, true))
	g.indent++

	oldScope := g.scope
	oldTaskScope := g.taskScope

	g.scope = newScope(oldScope)
	g.taskScope = g.scope

	for _, param := range decl.Params {
		paramType := g.cTypeFromAstWithGenericArgs(param.Type, subst)
		g.scope.declare(param.Name.Name, paramType)
	}

	g.emitBlockStatements(decl.Body)
	g.emitActiveDefers()

	g.scope = oldScope
	g.taskScope = oldTaskScope

	g.indent--
	g.line("}")
	g.line("")

	g.genericSubst = oldSubst
	g.currentTask = oldTask
	g.currentGenericTaskName = oldGenericTaskName
	g.currentResults = oldResults
}

func (g *Generator) genericTaskSignature(info *GenericTaskInstance, definition bool) string {
	decl := info.Decl
	subst := genericTaskSubstForCGen(decl.GenericParams, info.Args)

	ret := g.genericTaskReturnType(info)

	if len(decl.Params) == 0 {
		return fmt.Sprintf("%s %s(void)", ret.Name, info.Name)
	}

	var params []string

	for _, param := range decl.Params {
		paramType := g.cTypeFromAstWithGenericArgs(param.Type, subst)

		if param.IsVariadic {
			g.error(param.Name.Span(), fmt.Sprintf("generic task %q with variadic parameters is not supported by C codegen yet", decl.Name.Name))
			paramType = CInvalid
		}

		params = append(params, paramType.Decl(param.Name.Name))
	}

	return fmt.Sprintf("%s %s(%s)", ret.Name, info.Name, strings.Join(params, ", "))
}

func (g *Generator) emitTasks(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.TaskDecl)
		if !ok || d.IsTest || d.IsExtern || d.IsIntrinsic {
			continue
		}

		if len(d.GenericParams) > 0 {
			continue
		}

		info := g.tasks[d.Name.Name]
		if info.IsExtern {
			continue
		}

		g.emitTask(d)
		g.line("")
	}
}

func (g *Generator) emitTask(d *ast.TaskDecl) {
	info := g.tasks[d.Name.Name]

	g.currentTask = d
	g.currentResults = nil
	g.currentGenericTaskName = ""

	for _, result := range d.Results {
		g.currentResults = append(g.currentResults, g.cTypeFromAst(result))
	}

	g.linef("%s {", g.taskSignature(d, true))
	g.indent++

	oldScope := g.scope
	oldTaskScope := g.taskScope

	g.scope = newScope(oldScope)
	g.taskScope = g.scope

	for i, param := range d.Params {
		if i < len(info.ParamTypes) {
			paramType := info.ParamTypes[i]

			if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
				paramType = g.variadicCType(paramType)
			}

			g.scope.declare(param.Name.Name, paramType)
		}
	}

	g.emitBlockStatements(d.Body)

	g.emitActiveDefers()

	if d.Name.Name == "Main" && len(d.Results) == 0 {
		g.line("return 0;")
	}

	g.scope = oldScope
	g.taskScope = oldTaskScope

	g.indent--
	g.line("}")

	g.currentTask = nil
	g.currentResults = nil
}

func (g *Generator) emitActiveDefers() {
	for sc := g.scope; sc != nil; sc = sc.parent {
		g.emitDefersInScope(sc)

		if sc == g.taskScope {
			break
		}
	}
}

func (g *Generator) currentLoopControl() (
	loopControl,
	bool,
) {
	if len(g.loopStack) == 0 {
		return loopControl{}, false
	}

	return g.loopStack[len(g.loopStack)-1], true
}

func (g *Generator) emitDefersThroughScope(
	target *scope,
) bool {
	if target == nil {
		return false
	}

	for current := g.scope; current != nil; current = current.parent {
		g.emitDefersInScope(current)

		if current == target {
			return true
		}

		if current == g.taskScope {
			break
		}
	}

	return false
}

func (g *Generator) emitBreakStmt(
	s *ast.BreakStmt,
) {
	control, ok := g.currentLoopControl()
	if !ok {
		g.error(
			s.Span(),
			"break is only valid inside a for loop",
		)
		return
	}

	if !g.emitDefersThroughScope(control.scope) {
		g.error(
			s.Span(),
			"could not find the target for-loop scope while emitting break",
		)
		return
	}

	g.linef(
		"goto %s;",
		control.breakLabel,
	)
}

func (g *Generator) emitContinueStmt(
	s *ast.ContinueStmt,
) {
	control, ok := g.currentLoopControl()
	if !ok {
		g.error(
			s.Span(),
			"continue is only valid inside a for loop",
		)
		return
	}

	if !g.emitDefersThroughScope(control.scope) {
		g.error(
			s.Span(),
			"could not find the target for-loop scope while emitting continue",
		)
		return
	}

	g.linef(
		"goto %s;",
		control.continueLabel,
	)
}

func (g *Generator) emitDefersInScope(sc *scope) {
	if sc == nil {
		return
	}

	// A deferred block must be emitted using the lexical scope in which the
	// defer statement was registered. This prevents an inner scope that
	// happens to contain the return statement from changing identifier/type
	// lookup inside an outer deferred block.
	previousScope := g.scope
	g.scope = sc

	defer func() {
		g.scope = previousScope
	}()

	for i := len(sc.defers) - 1; i >= 0; i-- {
		g.emitDeferredAction(
			sc.defers[i],
		)
	}
}

func (g *Generator) emitDeferredAction(
	action deferredAction,
) {
	if action.Call != "" {
		g.linef(
			"%s;",
			action.Call,
		)
		return
	}

	if action.Body == nil {
		return
	}

	// A deferred block executes as an independent control-flow region.
	// It must not inherit a surrounding for-loop target.
	previousLoopStack := g.loopStack
	g.loopStack = nil

	defer func() {
		g.loopStack = previousLoopStack
	}()

	// Give the deferred body its own lexical scope. This supports local
	// variables and nested defers within the deferred body.
	g.line("{")
	g.indent++

	parentScope := g.scope
	g.scope = newScope(parentScope)

	g.emitBlockStatements(
		action.Body,
	)

	// A defer declared inside this deferred block exits when the deferred
	// block itself finishes.
	g.emitDefersInScope(
		g.scope,
	)

	g.scope = parentScope

	g.indent--
	g.line("}")
}

func (g *Generator) taskSignature(d *ast.TaskDecl, definition bool) string {
	info := g.tasks[d.Name.Name]

	name := g.cTaskName(d.Name.Name)
	if info.IsExtern && info.ExternName != "" {
		name = info.ExternName
	}

	ret := info.ReturnType.Name

	if len(d.Params) == 0 {
		return fmt.Sprintf("%s %s(void)", ret, name)
	}

	var params []string

	for i, param := range d.Params {
		paramType := CInvalid
		if i < len(info.ParamTypes) {
			paramType = info.ParamTypes[i]
		}

		if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
			if info.IsExtern {
				params = append(params, "...")
				break
			}

			params = append(params, g.variadicCType(paramType).Decl(param.Name.Name))
			break
		}

		params = append(params, paramType.Decl(param.Name.Name))
	}

	return fmt.Sprintf("%s %s(%s)", ret, name, strings.Join(params, ", "))
}

func (g *Generator) taskResultStructName(taskName string) string {
	name := sanitizeCName(taskName)

	if g.packageName != "" {
		name = sanitizeCName(g.packageName) + "_" + name
	}

	return name + "_Result"
}

func packageTaskResultStructName(packageName string, taskName string) string {
	return sanitizeCName(packageName) + "_" + sanitizeCName(taskName) + "_Result"
}

func (g *Generator) genericTaskResultStructName(instanceName string) string {
	return instanceName + "_Result"
}

func (g *Generator) genericTaskReturnTypes(info *GenericTaskInstance) []CType {
	if info == nil || info.Decl == nil {
		return nil
	}

	subst := genericTaskSubstForCGen(info.Decl.GenericParams, info.Args)

	results := make([]CType, 0, len(info.Decl.Results))
	for _, result := range info.Decl.Results {
		results = append(results, g.cTypeFromAstWithGenericArgs(result, subst))
	}

	return results
}

func (g *Generator) genericTaskReturnType(info *GenericTaskInstance) CType {
	results := g.genericTaskReturnTypes(info)

	if len(results) == 0 {
		return CVoid
	}

	if len(results) == 1 {
		return results[0]
	}

	name := g.genericTaskResultStructName(info.Name)

	return CType{
		Name:     name,
		SealName: name,
	}
}

func (g *Generator) currentReturnStructType() CType {
	if g.currentReturnStructOverride != nil {
		return *g.currentReturnStructOverride
	}

	if g.currentGenericTaskName != "" {
		name := g.genericTaskResultStructName(g.currentGenericTaskName)

		return CType{
			Name:     name,
			SealName: name,
		}
	}

	if g.currentTask != nil {
		if info, ok := g.tasks[g.currentTask.Name.Name]; ok {
			return info.ReturnType
		}
	}

	return CInvalid
}

func (g *Generator) genericArgsInContext(args []ast.GenericArg) []ast.GenericArg {
	if g.genericSubst == nil {
		return args
	}

	out := make([]ast.GenericArg, 0, len(args))
	for _, arg := range args {
		out = append(out, g.substituteGenericArgForCGen(arg, g.genericSubst))
	}

	return out
}

func (g *Generator) crossPackageTypeOwner(
	name string,
) string {
	if name == "" ||
		g.packageName == "" ||
		builtin.IsType(name) {
		return ""
	}

	// When CGen is emitting code originating in another package, an
	// unqualified type name belongs to that type context first.
	if g.typeContextPackage != "" &&
		g.typeContextPackage != g.packageName {
		pkg := g.typePackageInfo(
			g.typeContextPackage,
		)

		if g.packageHasType(pkg, name) {
			return g.typeContextPackage
		}
	}

	// Otherwise, check whether the type belongs to the package currently
	// being generated.
	if g.structs[name] != nil ||
		g.distincts[name] != nil ||
		g.enums[name] != nil ||
		g.unions[name] != nil ||
		g.interfaces[name] != nil {
		return g.packageName
	}

	return ""
}

func (g *Generator) qualifyTypeForCrossPackageRequest(
	typ ast.Type,
) ast.Type {
	if typ == nil {
		return nil
	}

	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) != 1 {
			// Already package-qualified.
			return t
		}

		owner := g.crossPackageTypeOwner(
			t.Parts[0].Name,
		)

		if owner == "" {
			return t
		}

		parts := []ast.Ident{
			{
				Name: owner,
			},
		}

		parts = append(
			parts,
			t.Parts...,
		)

		return &ast.NamedType{
			Parts: parts,
			Loc:   t.Loc,
		}

	case *ast.PointerType:
		return &ast.PointerType{
			Elem: g.qualifyTypeForCrossPackageRequest(
				t.Elem,
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
			if arg.Kind == ast.GenericArgType {
				args = append(
					args,
					g.qualifyTypeArgForCrossPackageRequest(
						arg,
					),
				)
				continue
			}

			args = append(args, arg)
		}

		return &ast.GenericType{
			Base: g.qualifyTypeForCrossPackageRequest(
				t.Base,
			),
			Args: args,
			Loc:  t.Loc,
		}
	}

	return typ
}

func (g *Generator) qualifyTypeArgForCrossPackageRequest(
	arg ast.GenericArg,
) ast.GenericArg {
	typ := typeAstFromGenericArgForCGen(arg)
	if typ == nil {
		return arg
	}

	return ast.GenericArg{
		Kind: ast.GenericArgType,
		Type: g.qualifyTypeForCrossPackageRequest(
			typ,
		),
		Loc: arg.Loc,
	}
}

func (g *Generator) crossPackageRequestGenericArgs(
	params []ast.GenericParam,
	args []ast.GenericArg,
) []ast.GenericArg {
	if len(args) == 0 {
		return nil
	}

	out := make(
		[]ast.GenericArg,
		0,
		len(args),
	)

	for i, arg := range args {
		category := ast.GenericParamInvalid

		if i < len(params) {
			category = params[i].Category
		}

		switch category {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			out = append(
				out,
				g.qualifyTypeArgForCrossPackageRequest(
					arg,
				),
			)

		default:
			// This also handles extra arguments defensively when their AST
			// already identifies them explicitly as type arguments.
			if category == ast.GenericParamInvalid &&
				arg.Kind == ast.GenericArgType {
				out = append(
					out,
					g.qualifyTypeArgForCrossPackageRequest(
						arg,
					),
				)
				continue
			}

			out = append(out, arg)
		}
	}

	return out
}

func (g *Generator) cTaskName(name string) string {
	if name == "Main" {
		return "main"
	}

	if g.packageName != "" {
		return sanitizeCName(g.packageName) + "_" + sanitizeCName(name)
	}

	return sanitizeCName(name)
}

func cPackageTaskName(packageName string, taskName string) string {
	return sanitizeCName(packageName) + "_" + sanitizeCName(taskName)
}

func cImportedTypeName(packageName string, typeName string) string {
	return sanitizeCName(packageName) + "_" + sanitizeCName(typeName)
}

func packageTypeNameFromAst(t ast.Type) (string, string, bool) {
	named, ok := t.(*ast.NamedType)
	if !ok || len(named.Parts) < 2 {
		return "", "", false
	}

	pkgName := named.Parts[0].Name
	typeName := named.Parts[len(named.Parts)-1].Name

	if pkgName == "" || typeName == "" {
		return "", "", false
	}

	return pkgName, typeName, true
}

func (g *Generator) packageHasType(pkg *PackageInfo, typeName string) bool {
	if pkg == nil {
		return false
	}

	if pkg.Structs[typeName] != nil ||
		pkg.Distincts[typeName] != nil ||
		pkg.Enums[typeName] != nil ||
		pkg.Unions[typeName] != nil ||
		pkg.Interfaces[typeName] != nil {
		return true
	}

	return false
}

func (g *Generator) withTypeContext(packageName string, fn func()) {
	old := g.typeContextPackage
	g.typeContextPackage = packageName
	fn()
	g.typeContextPackage = old
}

func (g *Generator) cTypeFromAstInTypeContext(packageName string, typ ast.Type) CType {
	old := g.typeContextPackage
	g.typeContextPackage = packageName
	out := g.cTypeFromAst(typ)
	g.typeContextPackage = old
	return out
}

func (g *Generator) cTypeFromAstWithGenericArgsInTypeContext(packageName string, typ ast.Type, subst map[string]ast.GenericArg) CType {
	old := g.typeContextPackage
	g.typeContextPackage = packageName
	out := g.cTypeFromAstWithGenericArgs(typ, subst)
	g.typeContextPackage = old
	return out
}

func sanitizeCName(name string) string {
	var b strings.Builder

	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' ||
			ch >= 'A' && ch <= 'Z' ||
			ch >= '0' && ch <= '9' ||
			ch == '_' {
			b.WriteRune(ch)
			continue
		}

		b.WriteByte('_')
	}

	if b.Len() == 0 {
		return "_"
	}

	return b.String()
}

func (g *Generator) emitInterfaceValueTypes() {
	keys := make([]string, 0, len(g.interfaceInstances))

	for key := range g.interfaceInstances {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		instance := g.interfaceInstances[key]
		if instance == nil {
			continue
		}

		if instance.IsDyn {
			g.emitDynInterfaceValueType(instance)
		} else {
			g.emitStaticInterface(instance)
		}
	}
}

func (g *Generator) emitDynamicInterfaceVTableTypes() {
	keys := make([]string, 0, len(g.interfaceInstances))

	for key := range g.interfaceInstances {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		instance := g.interfaceInstances[key]
		if instance == nil || !instance.IsDyn {
			continue
		}

		g.emitDynInterfaceVTableType(instance)
	}
}

func (g *Generator) seedNonGenericInterfaceInstances() {
	for name, decl := range g.interfaces {
		if len(decl.GenericParams) != 0 {
			continue
		}

		g.registerInterfaceInstance(
			g.packageName,
			name,
			decl,
			nil,
		)
	}
}

func (g *Generator) collectImportedInterfaceInstances() {
	if len(g.packages) == 0 {
		return
	}

	packageNames := make([]string, 0, len(g.packages))

	for packageName := range g.packages {
		packageNames = append(packageNames, packageName)
	}

	sort.Strings(packageNames)

	for _, packageName := range packageNames {
		pkg := g.packages[packageName]
		if pkg == nil {
			continue
		}

		interfaceNames := make(
			[]string,
			0,
			len(pkg.Interfaces),
		)

		for name := range pkg.Interfaces {
			interfaceNames = append(interfaceNames, name)
		}

		sort.Strings(interfaceNames)

		// All ordinary imported interfaces are valid imported types.
		// This applies equally to static and dynamic interfaces.
		for _, name := range interfaceNames {
			decl := pkg.Interfaces[name]
			if decl == nil ||
				len(decl.GenericParams) != 0 {
				continue
			}

			g.registerInterfaceInstance(
				packageName,
				name,
				decl,
				nil,
			)
		}

		g.withTypeContext(packageName, func() {
			structNames := make(
				[]string,
				0,
				len(pkg.Structs),
			)

			for name := range pkg.Structs {
				structNames = append(
					structNames,
					name,
				)
			}

			sort.Strings(structNames)

			for _, name := range structNames {
				decl := pkg.Structs[name]
				if decl == nil ||
					len(decl.GenericParams) != 0 {
					continue
				}

				for _, field := range decl.Fields {
					g.collectInterfaceInstancesFromType(
						field.Type,
					)
				}
			}

			distinctNames := make(
				[]string,
				0,
				len(pkg.Distincts),
			)

			for name := range pkg.Distincts {
				distinctNames = append(
					distinctNames,
					name,
				)
			}

			sort.Strings(distinctNames)

			for _, name := range distinctNames {
				decl := pkg.Distincts[name]
				if decl == nil {
					continue
				}

				g.collectInterfaceInstancesFromType(
					decl.Underlying,
				)
			}

			unionNames := make(
				[]string,
				0,
				len(pkg.Unions),
			)

			for name := range pkg.Unions {
				unionNames = append(unionNames, name)
			}

			sort.Strings(unionNames)

			for _, name := range unionNames {
				decl := pkg.Unions[name]
				if decl == nil {
					continue
				}

				for _, member := range decl.Members {
					g.collectInterfaceInstancesFromType(
						member,
					)
				}
			}

			taskNames := make(
				[]string,
				0,
				len(pkg.Tasks),
			)

			for name := range pkg.Tasks {
				taskNames = append(taskNames, name)
			}

			sort.Strings(taskNames)

			for _, name := range taskNames {
				info := pkg.Tasks[name]

				if len(info.GenericParams) != 0 {
					continue
				}

				for _, param := range info.ParamTypeAsts {
					g.collectInterfaceInstancesFromType(
						param,
					)
				}

				for _, result := range info.ResultTypeAsts {
					g.collectInterfaceInstancesFromType(
						result,
					)
				}

				for i, hasDefault := range info.ParamHasDefault {
					if !hasDefault ||
						i >= len(info.ParamDefaults) {
						continue
					}

					g.collectInterfaceInstancesFromExpr(
						info.ParamDefaults[i],
					)
				}
			}

			for _, decl := range pkg.Impls {
				if decl == nil ||
					len(decl.GenericParams) != 0 {
					continue
				}

				g.collectInterfaceInstancesFromType(
					decl.Interface,
				)
				g.collectInterfaceInstancesFromType(
					decl.Target,
				)

				for _, entry := range decl.Entries {
					if entry.Task != nil {
						g.collectInterfaceInstancesFromDecl(
							entry.Task,
						)

						// Imported inline implementation bodies are emitted
						// in this translation unit, so their generic calls
						// must be discovered before prototypes are emitted.
						g.collectGenericStructInstancesFromDecl(
							entry.Task,
						)
					}

					if entry.Alias != nil {
						g.collectInterfaceInstancesFromExpr(
							entry.Alias,
						)
						g.collectGenericStructInstancesFromExpr(
							entry.Alias,
						)
					}
				}
			}
		})
	}
}

func (g *Generator) finalizeInterfaceAndGenericInstances(
	file *ast.File,
) {
	for {
		beforeInterfaces := len(g.interfaceInstances)
		beforeImpls := len(g.resolvedImpls)
		beforeGenericStructs := len(g.genericStructs)
		beforeImportedGenericStructs :=
			len(g.importedGenericStructs)
		beforeGenericTasks := len(g.genericTasks)
		beforeImportedGenericTasks :=
			len(g.importedGenericTasks)

		g.collectGenericMonomorphizations(file)
		g.collectInterfaceInstancesFromFile(file)
		g.discoverRequestedInterfaceImpls()

		if beforeInterfaces == len(g.interfaceInstances) &&
			beforeImpls == len(g.resolvedImpls) &&
			beforeGenericStructs == len(g.genericStructs) &&
			beforeImportedGenericStructs ==
				len(g.importedGenericStructs) &&
			beforeGenericTasks == len(g.genericTasks) &&
			beforeImportedGenericTasks ==
				len(g.importedGenericTasks) {
			break
		}
	}

	// Delegation must be connected only after the complete visible
	// implementation set has been discovered.
	g.resolveDelegatedImplInstances()
}

func (g *Generator) collectInterfaceInstancesFromFile(file *ast.File) {
	if file == nil {
		return
	}

	for _, decl := range file.Decls {
		g.collectInterfaceInstancesFromDecl(decl)
	}
}

func (g *Generator) collectInterfaceInstancesFromDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		g.collectInterfaceInstancesFromExpr(d.Value)

	case *ast.StructDecl:
		if len(d.GenericParams) > 0 {
			return
		}

		for _, field := range d.Fields {
			g.collectInterfaceInstancesFromType(field.Type)
		}

	case *ast.DistinctDecl:
		g.collectInterfaceInstancesFromType(d.Underlying)

	case *ast.UnionDecl:
		for _, member := range d.Members {
			g.collectInterfaceInstancesFromType(member)
		}

	case *ast.ImplDecl:
		// Generic impl declarations are templates. Concrete interface
		// instances are collected from actual type uses and casts.
		if len(d.GenericParams) > 0 {
			return
		}

		g.collectInterfaceInstancesFromType(d.Interface)
		g.collectInterfaceInstancesFromType(d.Target)

		for _, entry := range d.Entries {
			if entry.Task != nil {
				g.collectInterfaceInstancesFromDecl(entry.Task)
			}

			if entry.Alias != nil {
				g.collectInterfaceInstancesFromExpr(entry.Alias)
			}
		}

	case *ast.TaskDecl:
		if len(d.GenericParams) > 0 {
			return
		}

		for _, param := range d.Params {
			g.collectInterfaceInstancesFromType(param.Type)

			if param.HasDefault {
				g.collectInterfaceInstancesFromExpr(param.Default)
			}
		}

		for _, result := range d.Results {
			g.collectInterfaceInstancesFromType(result)
		}

		g.collectInterfaceInstancesFromBlock(d.Body)
	}
}

func (g *Generator) collectInterfaceInstancesFromBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}

	for _, stmt := range block.Stmts {
		g.collectInterfaceInstancesFromStmt(stmt)
	}
}

func (g *Generator) collectInterfaceInstancesFromStmt(
	stmt ast.Stmt,
) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		g.collectInterfaceInstancesFromDecl(
			s.Decl,
		)

	case *ast.BlockStmt:
		g.collectInterfaceInstancesFromBlock(
			s,
		)

	case *ast.ReturnStmt:
		for _, value := range s.Values {
			g.collectInterfaceInstancesFromExpr(
				value,
			)
		}

	case *ast.DeferStmt:
		if s.Call != nil {
			g.collectInterfaceInstancesFromExpr(
				s.Call,
			)
		}

		if s.Body != nil {
			g.collectInterfaceInstancesFromBlock(
				s.Body,
			)
		}

	case *ast.SealStmt:
		g.collectInterfaceInstancesFromExpr(
			s.Target,
		)

	case *ast.ExprStmt:
		g.collectInterfaceInstancesFromExpr(
			s.Expr,
		)

	case *ast.MultiVarDeclStmt:
		g.collectInterfaceInstancesFromExpr(
			s.Value,
		)

	case *ast.AssignStmt:
		g.collectInterfaceInstancesFromExpr(
			s.Left,
		)

		g.collectInterfaceInstancesFromExpr(
			s.Right,
		)

	case *ast.VarDeclStmt:
		if s.HasType {
			g.collectInterfaceInstancesFromType(
				s.Type,
			)
		}

		if s.HasValue {
			g.collectInterfaceInstancesFromExpr(
				s.Value,
			)
		}

	case *ast.IfStmt:
		g.collectInterfaceInstancesFromExpr(
			s.Cond,
		)

		g.collectInterfaceInstancesFromBlock(
			s.Then,
		)

		if s.Else != nil {
			g.collectInterfaceInstancesFromStmt(
				s.Else,
			)
		}

	case *ast.ForStmt:
		if s.Init != nil {
			g.collectInterfaceInstancesFromStmt(
				s.Init,
			)
		}

		if s.Cond != nil {
			g.collectInterfaceInstancesFromExpr(
				s.Cond,
			)
		}

		if s.Post != nil {
			g.collectInterfaceInstancesFromStmt(
				s.Post,
			)
		}

		g.collectInterfaceInstancesFromBlock(
			s.Body,
		)

	case *ast.SwitchStmt:
		g.collectInterfaceInstancesFromExpr(
			s.Target,
		)

		for _, swCase := range s.Cases {
			if swCase.UnionMember != nil {
				g.collectInterfaceInstancesFromType(
					swCase.UnionMember,
				)
			}

			if swCase.Expr != nil {
				g.collectInterfaceInstancesFromExpr(
					swCase.Expr,
				)
			}

			for _, bodyStmt := range swCase.Body {
				g.collectInterfaceInstancesFromStmt(
					bodyStmt,
				)
			}
		}
	}
}

func (g *Generator) collectInterfaceInstancesFromType(typ ast.Type) {
	if typ == nil {
		return
	}

	switch t := typ.(type) {
	case *ast.InterfaceSelfType:
		return

	case *ast.NamedType:
		_ = g.cTypeFromAst(t)

	case *ast.PointerType:
		g.collectInterfaceInstancesFromType(t.Elem)

	case *ast.GenericType:
		_ = g.cTypeFromGenericType(t)

		g.collectInterfaceInstancesFromType(t.Base)

		for _, arg := range t.Args {
			switch arg.Kind {
			case ast.GenericArgType:
				g.collectInterfaceInstancesFromType(arg.Type)

			case ast.GenericArgExpr:
				g.collectInterfaceInstancesFromExpr(arg.Expr)
			}
		}
	}
}

func (g *Generator) collectInterfaceInstancesFromExpr(expr ast.Expr) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.UnaryExpr:
		g.collectInterfaceInstancesFromExpr(e.Expr)

	case *ast.BinaryExpr:
		g.collectInterfaceInstancesFromExpr(e.Left)
		g.collectInterfaceInstancesFromExpr(e.Right)

	case *ast.CallExpr:
		g.collectInterfaceInstancesFromExpr(e.Callee)

		for _, arg := range e.Args {
			g.collectInterfaceInstancesFromExpr(arg)
		}

	case *ast.GenericExpr:
		// A GenericExpr in expression position is usually a generic task call
		// or intrinsic, for example Identity<int>, cast<T>, anyAs<T>, or
		// anyIs<T>. Do not reinterpret the whole expression as a generic type.
		//
		// Interface types occurring in its generic arguments are still
		// collected below.
		g.collectInterfaceInstancesFromExpr(e.Base)

		for _, arg := range e.Args {
			switch arg.Kind {
			case ast.GenericArgType:
				g.collectInterfaceInstancesFromType(arg.Type)

			case ast.GenericArgExpr:
				g.collectInterfaceInstancesFromExpr(arg.Expr)
			}
		}

	case *ast.SpreadExpr:
		g.collectInterfaceInstancesFromExpr(e.Expr)

	case *ast.SelectorExpr:
		g.collectInterfaceInstancesFromExpr(e.Left)

	case *ast.IndexExpr:
		g.collectInterfaceInstancesFromExpr(e.Left)
		g.collectInterfaceInstancesFromExpr(e.Index)

	case *ast.CompoundLiteralExpr:
		g.collectInterfaceInstancesFromType(e.Type)

		for _, field := range e.Fields {
			g.collectInterfaceInstancesFromExpr(field.Value)
		}

		for _, value := range e.Values {
			g.collectInterfaceInstancesFromExpr(value)
		}
	}
}

func (g *Generator) emitDynInterfaceValueType(
	instance *InterfaceInstance,
) {
	if instance == nil {
		return
	}

	// The interface object needs the vtable pointer type before the complete
	// vtable structure is emitted.
	g.linef(
		"typedef struct %s_vtable %s_vtable;",
		instance.CName,
		instance.CName,
	)
	g.line("")

	g.linef("typedef struct %s {", instance.CName)
	g.indent++
	g.line("void *data;")
	g.linef("const %s_vtable *vtable;", instance.CName)
	g.indent--
	g.linef("} %s;", instance.CName)
	g.line("")
}

func (g *Generator) emitDynInterfaceVTableType(
	instance *InterfaceInstance,
) {
	if instance == nil || instance.Decl == nil {
		return
	}

	// The typedef was already introduced by emitDynInterfaceValueType.
	// Complete the previously forward-declared structure here.
	g.linef("struct %s_vtable {", instance.CName)
	g.indent++

	for _, req := range instance.Decl.Requirements {
		ret := g.interfaceRequirementReturnType(instance, req)

		params := []string{"void *data"}

		for i := 1; i < len(req.Params); i++ {
			paramType := g.interfaceRequirementParamType(
				instance,
				req,
				i,
			)

			params = append(
				params,
				paramType.Decl(fmt.Sprintf("arg%d", i)),
			)
		}

		g.linef(
			"%s (*%s)(%s);",
			ret.Name,
			sanitizeCName(req.Name.Name),
			strings.Join(params, ", "),
		)
	}

	g.indent--
	g.line("};")
	g.line("")
}

func (g *Generator) emitStaticInterface(
	instance *InterfaceInstance,
) {
	g.linef("typedef struct %s {", instance.CName)
	g.indent++
	g.line("uintptr_t tag;")
	g.line("void *data;")
	g.indent--
	g.linef("} %s;", instance.CName)
	g.line("")
}

func interfaceImplTagName(
	instance *InterfaceInstance,
	concrete CType,
) string {
	return sanitizeCName(instance.CName) +
		"_Tag_" +
		sanitizeCName(concrete.SealName)
}

func interfaceImplTagValue(
	info *ResolvedImplInstance,
) uint64 {
	if info == nil ||
		info.Interface == nil ||
		info.Template == nil {
		return 1
	}

	targetAst := info.Target.SealName

	if info.Template.Target != nil {
		concreteTarget :=
			info.Template.Target

		if len(info.Subst) != 0 {
			concreteTarget =
				substituteImplTypeAstForTag(
					concreteTarget,
					info.Subst,
				)
		}

		targetAst = canonicalImplTypeTagKey(
			info.Template.PackageName,
			concreteTarget,
		)
	}

	key := info.Interface.Key +
		"|" +
		targetAst

	// FNV-1a 64-bit. This must remain stable because static interface values
	// can cross package boundaries.
	hash := uint64(14695981039346656037)

	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= 1099511628211
	}

	if hash == 0 {
		hash = 1
	}

	return hash
}

func substituteImplTypeAstForTag(
	typ ast.Type,
	subst map[string]ast.GenericArg,
) ast.Type {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 1 {
			if arg, ok := subst[t.Parts[0].Name]; ok {
				if replacement :=
					typeAstFromGenericArgForCGen(arg); replacement != nil {
					return replacement
				}
			}
		}

		return t

	case *ast.PointerType:
		return &ast.PointerType{
			Elem: substituteImplTypeAstForTag(
				t.Elem,
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
			switch arg.Kind {
			case ast.GenericArgType:
				args = append(args, ast.GenericArg{
					Kind: ast.GenericArgType,
					Type: substituteImplTypeAstForTag(
						arg.Type,
						subst,
					),
					Loc: arg.Loc,
				})

			case ast.GenericArgExpr:
				if id, ok :=
					arg.Expr.(*ast.IdentExpr); ok {
					if replacement, exists :=
						subst[id.Name.Name]; exists {
						args = append(
							args,
							replacement,
						)
						continue
					}
				}

				args = append(args, arg)
			}
		}

		return &ast.GenericType{
			Base: substituteImplTypeAstForTag(
				t.Base,
				subst,
			),
			Args: args,
			Loc:  t.Loc,
		}
	}

	return typ
}

func canonicalImplTypeTagKey(
	packageName string,
	typ ast.Type,
) string {
	switch t := typ.(type) {
	case *ast.NamedType:
		var parts []string

		for _, part := range t.Parts {
			parts = append(parts, part.Name)
		}

		if len(parts) == 1 &&
			packageName != "" &&
			!builtin.IsType(parts[0]) {
			return packageName + "." + parts[0]
		}

		return strings.Join(parts, ".")

	case *ast.PointerType:
		return "*" +
			canonicalImplTypeTagKey(
				packageName,
				t.Elem,
			)

	case *ast.GenericType:
		var args []string

		for _, arg := range t.Args {
			switch arg.Kind {
			case ast.GenericArgType:
				args = append(
					args,
					canonicalImplTypeTagKey(
						packageName,
						arg.Type,
					),
				)

			case ast.GenericArgExpr:
				args = append(
					args,
					genericRequestArgKey(arg),
				)
			}
		}

		return canonicalImplTypeTagKey(
			packageName,
			t.Base,
		) + "<" +
			strings.Join(args, ",") +
			">"
	}

	return "<invalid>"
}

func (g *Generator) emitStaticInterfaceTagDefinitions(
	instance *InterfaceInstance,
	impls []*ResolvedImplInstance,
) {
	seenValues := map[uint64]*ResolvedImplInstance{}

	for _, impl := range impls {
		value := interfaceImplTagValue(impl)

		if previous := seenValues[value]; previous != nil &&
			previous.Key != impl.Key {
			g.error(
				impl.Template.Decl.Span(),
				fmt.Sprintf(
					"static interface tag collision between %s and %s",
					previous.Target.SealName,
					impl.Target.SealName,
				),
			)
		} else {
			seenValues[value] = impl
		}

		g.linef(
			"#define %s ((uintptr_t)UINT64_C(0x%016x))",
			interfaceImplTagName(
				instance,
				impl.Target,
			),
			value,
		)
	}

	if len(impls) > 0 {
		g.line("")
	}
}

func (g *Generator) interfaceRequirementReturnType(
	instance *InterfaceInstance,
	req *ast.TaskSignature,
) CType {
	results := g.interfaceRequirementResultTypes(instance, req)

	switch len(results) {
	case 0:
		return CVoid

	case 1:
		return results[0]

	default:
		name := interfaceRequirementResultStructName(
			instance.CName,
			req.Name.Name,
		)

		return CType{
			Name:     name,
			SealName: name,
		}
	}
}

func interfaceRequirementResultStructName(
	interfaceCName string,
	requirementName string,
) string {
	return sanitizeCName(interfaceCName) +
		"_" +
		sanitizeCName(requirementName) +
		"_Result"
}

func (g *Generator) emitInterfaceResultStructs() {
	keys := make([]string, 0, len(g.interfaceInstances))

	for key := range g.interfaceInstances {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		instance := g.interfaceInstances[key]

		for _, req := range instance.Decl.Requirements {
			results := g.interfaceRequirementResultTypes(instance, req)
			if len(results) <= 1 {
				continue
			}

			name := interfaceRequirementResultStructName(
				instance.CName,
				req.Name.Name,
			)

			g.linef("typedef struct %s {", name)
			g.indent++

			for i, result := range results {
				g.linef(
					"%s;",
					result.Decl(fmt.Sprintf("_%d", i)),
				)
			}

			g.indent--
			g.linef("} %s;", name)
			g.line("")
		}
	}
}

func (g *Generator) interfaceInstanceForCType(
	iface CType,
) (*InterfaceInstance, bool) {
	instance := g.interfaceInstances[iface.SealName]
	return instance, instance != nil
}

func (g *Generator) lookupInterfaceRequirement(
	iface CType,
	name string,
) (*InterfaceInstance, *ast.TaskSignature, bool) {
	instance, ok := g.interfaceInstanceForCType(iface)
	if !ok || instance.Decl == nil {
		return nil, nil, false
	}

	for _, req := range instance.Decl.Requirements {
		if req.Name.Name == name {
			return instance, req, true
		}
	}

	return instance, nil, false
}

func (g *Generator) interfaceRequirementParamType(
	instance *InterfaceInstance,
	req *ast.TaskSignature,
	index int,
) CType {
	if instance == nil || req == nil || index < 0 || index >= len(req.Params) {
		return CInvalid
	}

	// The checker guarantees the first parameter is exactly *self.
	if index == 0 {
		return CRawptr
	}

	return g.cTypeFromAstWithGenericArgsInTypeContext(
		instance.PackageName,
		req.Params[index].Type,
		instance.Subst,
	)
}

func (g *Generator) interfaceRequirementResultTypes(
	instance *InterfaceInstance,
	req *ast.TaskSignature,
) []CType {
	if instance == nil || req == nil {
		return nil
	}

	var out []CType

	for _, result := range req.Results {
		out = append(
			out,
			g.cTypeFromAstWithGenericArgsInTypeContext(
				instance.PackageName,
				result,
				instance.Subst,
			),
		)
	}

	return out
}

func (g *Generator) emitInterfaceDispatchCall(iface CType, taskName string, args []ast.Expr, preparedArgs []string) string {
	if g.isDynInterfaceName(iface.SealName) {
		return g.emitDynInterfaceDispatchCall(iface, taskName, args, preparedArgs)
	}

	return g.emitStaticInterfaceDispatchCall(iface, taskName, args, preparedArgs)
}

func (g *Generator) emitDynInterfaceDispatchCall(
	iface CType,
	taskName string,
	args []ast.Expr,
	preparedArgs []string,
) string {
	instance, req, ok := g.lookupInterfaceRequirement(
		iface,
		taskName,
	)
	if !ok {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"interface %s has no requirement %q",
				iface.SealName,
				taskName,
			),
		)
		return "0"
	}

	if len(args) == 0 {
		g.error(
			argsSpan(args),
			"interface dispatch requires a receiver",
		)
		return "0"
	}

	receiver := ""

	if len(preparedArgs) > 0 {
		receiver = preparedArgs[0]
	} else {
		receiver = g.emitExpr(args[0], &iface)
	}

	outArgs := []string{receiver}

	for i := 1; i < len(args); i++ {
		if preparedArgs != nil &&
			i < len(preparedArgs) {
			outArgs = append(
				outArgs,
				preparedArgs[i],
			)
			continue
		}

		expected := (*CType)(nil)

		if i < len(req.Params) {
			paramType := g.interfaceRequirementParamType(
				instance,
				req,
				i,
			)
			expected = &paramType
		}

		outArgs = append(
			outArgs,
			g.emitExpr(args[i], expected),
		)
	}

	return fmt.Sprintf(
		"%s(%s)",
		dynamicInterfaceDispatcherName(
			instance.CName,
			taskName,
		),
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) emitStaticInterfaceDispatchCall(
	iface CType,
	taskName string,
	args []ast.Expr,
	preparedArgs []string,
) string {
	instance, req, ok := g.lookupInterfaceRequirement(iface, taskName)
	if !ok {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"interface %s has no requirement %q",
				iface.SealName,
				taskName,
			),
		)
		return "0"
	}

	if len(args) == 0 {
		g.error(argsSpan(args), "interface dispatch requires a receiver")
		return "0"
	}

	receiver := ""
	if len(preparedArgs) > 0 {
		receiver = preparedArgs[0]
	} else {
		receiver = g.emitExpr(args[0], &iface)
	}

	var outArgs []string
	outArgs = append(outArgs, receiver)

	for i := 1; i < len(args); i++ {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := (*CType)(nil)

		if i < len(req.Params) {
			paramType := g.interfaceRequirementParamType(
				instance,
				req,
				i,
			)
			expected = &paramType
		}

		outArgs = append(
			outArgs,
			g.emitExpr(args[i], expected),
		)
	}

	return fmt.Sprintf(
		"%s(%s)",
		staticInterfaceDispatcherName(instance.CName, taskName),
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) implAliasTaskInfo(
	packageName string,
	expr ast.Expr,
) (string, TaskInfo, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		// Unqualified names inside an imported impl belong to the package
		// where the implementation was declared.
		if packageName != "" &&
			packageName != g.packageName {
			pkg := g.packages[packageName]
			if pkg == nil {
				return "", TaskInfo{}, false
			}

			info, ok := pkg.Tasks[e.Name.Name]
			if !ok {
				return "", TaskInfo{}, false
			}

			info = g.taskInfoInPackageContext(
				packageName,
				e.Name.Name,
				info,
			)

			return cImportedTaskName(
				packageName,
				e.Name.Name,
				info,
			), info, true
		}

		info, ok := g.tasks[e.Name.Name]
		if !ok {
			return "", TaskInfo{}, false
		}

		return g.cTaskName(e.Name.Name), info, true

	case *ast.SelectorExpr:
		id, ok := e.Left.(*ast.IdentExpr)
		if !ok {
			return "", TaskInfo{}, false
		}

		pkg := g.packages[id.Name.Name]
		if pkg == nil {
			return "", TaskInfo{}, false
		}

		info, ok := pkg.Tasks[e.Name.Name]
		if !ok {
			return "", TaskInfo{}, false
		}

		info = g.taskInfoInPackageContext(
			id.Name.Name,
			e.Name.Name,
			info,
		)

		return cImportedTaskName(
			id.Name.Name,
			e.Name.Name,
			info,
		), info, true
	}

	return "", TaskInfo{}, false
}

func interfaceWrapperName(iface string, concrete string, task string) string {
	return sanitizeCName(iface) + "_" + sanitizeCName(concrete) + "_" + sanitizeCName(task)
}

func interfaceVTableName(iface string, concrete string) string {
	return sanitizeCName(iface) + "_" + sanitizeCName(concrete) + "_vtable"
}

func (g *Generator) emitBlockStatements(block *ast.BlockStmt) {
	if block == nil {
		return
	}

	for _, stmt := range block.Stmts {
		g.emitStmt(stmt)
	}
}

func (g *Generator) emitStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		g.error(
			s.Span(),
			"local declarations are not supported by C codegen yet",
		)

	case *ast.BlockStmt:
		g.line("{")
		g.indent++

		oldScope := g.scope
		g.scope = newScope(oldScope)

		g.emitBlockStatements(s)
		g.emitDefersInScope(g.scope)

		g.scope = oldScope

		g.indent--
		g.line("}")

	case *ast.ReturnStmt:
		g.emitReturnStmt(s)

	case *ast.BreakStmt:
		g.emitBreakStmt(s)

	case *ast.ContinueStmt:
		g.emitContinueStmt(s)

	case *ast.DeferStmt:
		g.emitDeferStmt(s)

	case *ast.SealStmt:
		// `seal` is a checker/language rule.
		// It has no first-backend C output.

	case *ast.ExprStmt:
		g.linef(
			"%s;",
			g.emitExpr(s.Expr, nil),
		)

	case *ast.AssignStmt:
		g.linef(
			"%s;",
			g.emitAssignmentExpr(s),
		)

	case *ast.VarDeclStmt:
		g.emitVarDeclStmt(s)

	case *ast.MultiVarDeclStmt:
		g.emitMultiVarDeclStmt(s)

	case *ast.IfStmt:
		cond := g.emitExpr(s.Cond, &CBool)

		g.linef("if (%s) {", cond)
		g.indent++

		oldScope := g.scope
		g.scope = newScope(oldScope)

		g.emitBlockStatements(s.Then)
		g.emitDefersInScope(g.scope)

		g.scope = oldScope
		g.indent--

		if s.Else != nil {
			g.line("} else {")
			g.indent++

			oldScope := g.scope
			g.scope = newScope(oldScope)

			g.emitStmt(s.Else)
			g.emitDefersInScope(g.scope)

			g.scope = oldScope
			g.indent--
			g.line("}")
		} else {
			g.line("}")
		}

	case *ast.ForStmt:
		g.emitForStmt(s)

	case *ast.SwitchStmt:
		g.emitSwitchStmt(s)
	}
}

func (g *Generator) emitForwardedMultiReturnCall(call *ast.CallExpr) bool {
	resultTypes := g.callReturnTypes(call)
	if len(resultTypes) <= 1 {
		return false
	}

	resultType := g.currentReturnStructType()
	if resultType.SealName == "<invalid>" {
		return false
	}

	callType := g.inferExprType(call, nil)
	if callType.SealName == "<invalid>" {
		return false
	}

	callTemp := g.newTemp("forward_result")
	resultTemp := g.newTemp("return_value")

	g.linef("%s = %s;", callType.Decl(callTemp), g.emitExpr(call, &callType))
	g.linef("%s = {0};", resultType.Decl(resultTemp))

	count := len(g.currentResults)
	if len(resultTypes) < count {
		count = len(resultTypes)
	}

	for i := 0; i < count; i++ {
		g.linef("%s._%d = %s._%d;", resultTemp, i, callTemp, i)
	}

	g.emitActiveDefers()
	g.linef("return %s;", resultTemp)

	return true
}

func (g *Generator) emitReturnStmt(s *ast.ReturnStmt) {
	if len(s.Values) == 0 {
		g.emitActiveDefers()

		if g.currentTask != nil && g.currentTask.Name.Name == "Main" {
			g.line("return 0;")
			return
		}

		g.line("return;")
		return
	}

	if len(g.currentResults) > 1 && len(s.Values) == 1 {
		if call, ok := s.Values[0].(*ast.CallExpr); ok {
			if g.emitForwardedMultiReturnCall(call) {
				return
			}
		}
	}

	if len(g.currentResults) > 1 {
		if g.currentTask == nil {
			g.error(s.Span(), "multi-result return outside task")
			return
		}

		resultType := g.currentReturnStructType()
		resultTemp := g.newTemp("return_value")

		g.linef("%s = {0};", resultType.Decl(resultTemp))

		count := len(s.Values)
		if len(g.currentResults) < count {
			count = len(g.currentResults)
		}

		for i := 0; i < count; i++ {
			expected := g.currentResults[i]
			g.linef("%s._%d = %s;", resultTemp, i, g.emitExpr(s.Values[i], &expected))
		}

		g.emitActiveDefers()
		g.linef("return %s;", resultTemp)
		return
	}

	expected := (*CType)(nil)
	if len(g.currentResults) == 1 {
		expected = &g.currentResults[0]
	}

	resultTemp := g.newTemp("return_value")
	resultType := CInvalid
	if expected != nil {
		resultType = *expected
	} else {
		resultType = g.inferExprType(s.Values[0], nil)
	}

	g.linef("%s = %s;", resultType.Decl(resultTemp), g.emitExpr(s.Values[0], &resultType))
	g.emitActiveDefers()
	g.linef("return %s;", resultTemp)
}

func (g *Generator) emitVarDeclStmt(
	s *ast.VarDeclStmt,
) {
	var typ CType

	switch {
	case s.HasType:
		typ = g.cTypeFromAstInContext(s.Type)

	case s.HasValue:
		typ = g.inferExprType(s.Value, nil)

	default:
		typ = CInvalid
	}

	if s.Name.Name == "_" {
		if s.HasValue {
			g.linef(
				"(void)(%s);",
				g.emitExpr(s.Value, &typ),
			)
		}

		return
	}

	g.scope.declare(s.Name.Name, typ)

	if s.HasValue {
		value := g.emitExpr(s.Value, &typ)

		g.linef(
			"%s = %s;",
			typ.Decl(s.Name.Name),
			value,
		)
		return
	}

	g.linef("%s;", typ.Decl(s.Name.Name))
}

func (g *Generator) emitForStmt(
	s *ast.ForStmt,
) {
	oldScope := g.scope
	loopScope := newScope(oldScope)
	g.scope = loopScope

	init := ""
	if s.Init != nil {
		init = g.emitForPart(s.Init)
	}

	cond := ""
	if s.Cond != nil {
		cond = g.emitExpr(
			s.Cond,
			&CBool,
		)
	}

	post := ""
	if s.Post != nil {
		post = g.emitForPart(s.Post)
	}

	breakLabel := g.newTemp(
		"loop_break",
	)

	continueLabel := g.newTemp(
		"loop_continue",
	)

	control := loopControl{
		scope:         loopScope,
		breakLabel:    breakLabel,
		continueLabel: continueLabel,
	}

	g.loopStack = append(
		g.loopStack,
		control,
	)

	switch {
	case s.Init == nil &&
		s.Cond == nil &&
		s.Post == nil:
		g.line("for (;;) {")

	case s.Init == nil &&
		s.Post == nil:
		g.linef(
			"for (; %s; ) {",
			cond,
		)

	default:
		g.linef(
			"for (%s; %s; %s) {",
			init,
			cond,
			post,
		)
	}

	g.indent++

	g.emitBlockStatements(
		s.Body,
	)

	// Normal iteration completion exits the lexical body scope, so its
	// deferred actions execute before the C for-loop post expression.
	g.emitDefersInScope(
		loopScope,
	)

	// A generated continue jumps here after explicitly running every defer
	// belonging to the scopes it exits. Falling through this label causes C
	// to execute the for-loop post expression and then recheck the condition.
	g.linef(
		"%s: ;",
		continueLabel,
	)

	g.indent--
	g.line("}")

	g.loopStack =
		g.loopStack[:len(g.loopStack)-1]

	g.scope = oldScope

	// A generated break jumps outside every intervening switch and exits the
	// current for loop.
	g.linef(
		"%s: ;",
		breakLabel,
	)
}

func (g *Generator) emitForPart(
	stmt ast.Stmt,
) string {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		var typ CType

		switch {
		case s.HasType:
			typ = g.cTypeFromAstInContext(s.Type)

		case s.HasValue:
			typ = g.inferExprType(s.Value, nil)

		default:
			typ = CInvalid
		}

		g.scope.declare(s.Name.Name, typ)

		if s.HasValue {
			value := g.emitExpr(s.Value, &typ)

			return fmt.Sprintf(
				"%s = %s",
				typ.Decl(s.Name.Name),
				value,
			)
		}

		return typ.Decl(s.Name.Name)

	case *ast.AssignStmt:
		return g.emitAssignmentExpr(s)

	case *ast.ExprStmt:
		return g.emitExpr(s.Expr, nil)

	default:
		g.error(
			stmt.Span(),
			"unsupported for-loop component in C codegen",
		)
		return ""
	}
}

func (g *Generator) emitSwitchStmt(s *ast.SwitchStmt) {
	if s.IsTypeSwitch {
		g.emitAnyTypeSwitchStmt(s)
		return
	}

	if s.IsUnionSwitch {
		g.emitUnionSwitchStmt(s)
		return
	}

	targetType := g.inferExprType(s.Target, nil)
	target := g.emitExpr(s.Target, nil)

	g.linef("switch (%s) {", target)
	g.indent++

	for _, swCase := range s.Cases {
		switch swCase.Kind {
		case ast.SwitchCaseEnumVariant:
			g.linef("case %s_%s:", targetType.SealName, swCase.EnumVariant.Name)

		case ast.SwitchCaseDefault:
			g.line("default:")

		case ast.SwitchCaseExpr:
			g.linef("case %s:", g.emitExpr(swCase.Expr, &targetType))

		default:
			g.error(swCase.Loc, "unsupported switch case in C codegen")
			continue
		}

		g.indent++

		oldScope := g.scope
		g.scope = newScope(oldScope)

		for _, stmt := range swCase.Body {
			g.emitStmt(stmt)
		}

		g.emitDefersInScope(g.scope)
		g.line("break;")

		g.scope = oldScope

		g.indent--
	}

	g.indent--
	g.line("}")
}

func (g *Generator) emitAnyTypeSwitchStmt(s *ast.SwitchStmt) {
	targetType := g.inferExprType(s.Target, nil)
	if targetType.SealName != "any" {
		g.error(s.Target.Span(), fmt.Sprintf("type switch target is not any: %s", targetType.String()))
		return
	}

	target := g.emitExpr(s.Target, nil)

	g.linef("switch ((%s).type) {", target)
	g.indent++

	for _, swCase := range s.Cases {
		switch swCase.Kind {
		case ast.SwitchCaseUnionMember:
			caseType := g.cTypeFromAst(swCase.UnionMember)
			kind, ok := g.sealTypeKindFor(caseType)
			if !ok {
				g.error(swCase.UnionMember.Span(), fmt.Sprintf("unsupported any type switch case %s", caseType.String()))
				continue
			}

			g.linef("case %s:", kind)

		case ast.SwitchCaseDefault:
			g.line("default:")

		default:
			g.error(swCase.Loc, "unsupported case in any type switch")
			continue
		}

		g.indent++

		oldScope := g.scope
		g.scope = newScope(oldScope)

		for _, stmt := range swCase.Body {
			g.emitStmt(stmt)
		}

		g.emitDefersInScope(g.scope)
		g.line("break;")

		g.scope = oldScope

		g.indent--
	}

	g.indent--
	g.line("}")
}

func (g *Generator) emitUnionSwitchStmt(s *ast.SwitchStmt) {
	targetType := g.inferExprType(s.Target, nil)
	if !g.isUnion(targetType) {
		g.error(s.Target.Span(), fmt.Sprintf("union switch target is not a union: %s", targetType.String()))
		return
	}

	targetTemp := g.newTemp("union_switch")
	g.linef("%s = %s;", targetType.Decl(targetTemp), g.emitExpr(s.Target, &targetType))

	g.linef("switch (%s.tag) {", targetTemp)
	g.indent++

	for _, swCase := range s.Cases {
		switch swCase.Kind {
		case ast.SwitchCaseUnionMember:
			memberType := g.cTypeFromAst(swCase.UnionMember)
			g.linef("case %s_Tag_%s:", targetType.SealName, memberType.SealName)
			g.indent++
			g.line("{")
			g.indent++

			oldScope := g.scope
			g.scope = newScope(oldScope)

			if s.BindName.Name != "" {
				g.linef("%s = %s.as.%s;", memberType.Decl(s.BindName.Name), targetTemp, memberType.SealName)
				g.scope.declare(s.BindName.Name, memberType)
			}

			for _, stmt := range swCase.Body {
				g.emitStmt(stmt)
			}

			g.emitDefersInScope(g.scope)
			g.line("break;")

			g.scope = oldScope

			g.indent--
			g.line("}")
			g.indent--

		case ast.SwitchCaseNil:
			g.linef("case %s_Tag_nil:", targetType.SealName)
			g.indent++
			g.line("{")
			g.indent++

			oldScope := g.scope
			g.scope = newScope(oldScope)

			if s.BindName.Name != "" {
				g.linef("void *%s = NULL;", s.BindName.Name)
				g.scope.declare(s.BindName.Name, CNil)
			}

			for _, stmt := range swCase.Body {
				g.emitStmt(stmt)
			}

			g.emitDefersInScope(g.scope)
			g.line("break;")

			g.scope = oldScope

			g.indent--
			g.line("}")
			g.indent--

		case ast.SwitchCaseDefault:
			g.line("default:")
			g.indent++
			g.line("{")
			g.indent++

			oldScope := g.scope
			g.scope = newScope(oldScope)

			for _, stmt := range swCase.Body {
				g.emitStmt(stmt)
			}

			g.emitDefersInScope(g.scope)
			g.line("break;")

			g.scope = oldScope

			g.indent--
			g.line("}")
			g.indent--

		default:
			g.error(swCase.Loc, "unsupported union switch case in C codegen")
		}
	}

	g.indent--
	g.line("}")
}

func (g *Generator) genericTaskParamInfo(name string) (TaskInfo, bool) {
	if g.genericSubst == nil {
		return TaskInfo{}, false
	}

	arg, ok := g.genericSubst[name]
	if !ok {
		return TaskInfo{}, false
	}

	return g.taskInfoFromGenericArg(arg)
}

func (g *Generator) taskInfoFromGenericArg(
	arg ast.GenericArg,
) (TaskInfo, bool) {
	if g.genericSubst != nil {
		arg = g.substituteGenericArgForCGen(
			arg,
			g.genericSubst,
		)
	}

	if arg.Kind != ast.GenericArgExpr ||
		arg.Expr == nil {
		return TaskInfo{}, false
	}

	switch e := arg.Expr.(type) {
	case *ast.IdentExpr:
		if _, info, found :=
			g.importedTaskInfoFromTypeContext(
				e.Name.Name,
			); found {
			return info, true
		}

		info, ok := g.tasks[e.Name.Name]
		if !ok {
			return TaskInfo{}, false
		}

		return info, true

	case *ast.SelectorExpr:
		id, ok := e.Left.(*ast.IdentExpr)
		if !ok {
			return TaskInfo{}, false
		}

		pkg := g.packages[id.Name.Name]
		if pkg == nil {
			return TaskInfo{}, false
		}

		info, ok := pkg.Tasks[e.Name.Name]
		if !ok {
			return TaskInfo{}, false
		}

		info = g.taskInfoInPackageContext(
			id.Name.Name,
			e.Name.Name,
			info,
		)

		return info, true

	case *ast.GenericExpr:
		switch base := e.Base.(type) {
		case *ast.IdentExpr:
			if packageName, taskName, info, found :=
				g.importedGenericTaskInfoFromTypeContext(
					base.Name.Name,
				); found {
				callArgs := e.Args

				if g.genericSubst != nil {
					callArgs = make(
						[]ast.GenericArg,
						0,
						len(e.Args),
					)

					for _, genericArg := range e.Args {
						callArgs = append(
							callArgs,
							g.substituteGenericArgForCGen(
								genericArg,
								g.genericSubst,
							),
						)
					}
				}

				name := g.registerImportedGenericTaskInstance(
					packageName,
					taskName,
					info,
					callArgs,
				)

				instance := g.importedGenericTasks[name]
				if instance == nil {
					return TaskInfo{}, false
				}

				return g.taskInfoFromImportedGenericTaskInstance(
					instance,
				), true
			}

			info, ok := g.tasks[base.Name.Name]
			if !ok ||
				len(info.GenericParams) == 0 ||
				info.Decl == nil {
				return TaskInfo{}, false
			}

			callArgs := e.Args

			if g.genericSubst != nil {
				callArgs = make(
					[]ast.GenericArg,
					0,
					len(e.Args),
				)

				for _, genericArg := range e.Args {
					callArgs = append(
						callArgs,
						g.substituteGenericArgForCGen(
							genericArg,
							g.genericSubst,
						),
					)
				}
			}

			name := g.registerGenericTaskInstance(
				info.Decl,
				callArgs,
			)

			instance := g.genericTasks[name]
			if instance == nil {
				return TaskInfo{}, false
			}

			return g.taskInfoFromGenericTaskInstance(
				instance,
			), true

		case *ast.SelectorExpr:
			pkgName, taskName, info, ok :=
				g.importedGenericTaskInfoFromSelector(base)

			if !ok {
				return TaskInfo{}, false
			}

			callArgs := e.Args

			if g.genericSubst != nil {
				callArgs = make(
					[]ast.GenericArg,
					0,
					len(e.Args),
				)

				for _, genericArg := range e.Args {
					callArgs = append(
						callArgs,
						g.substituteGenericArgForCGen(
							genericArg,
							g.genericSubst,
						),
					)
				}
			}

			name := g.registerImportedGenericTaskInstance(
				pkgName,
				taskName,
				info,
				callArgs,
			)

			instance := g.importedGenericTasks[name]
			if instance == nil {
				return TaskInfo{}, false
			}

			return g.taskInfoFromImportedGenericTaskInstance(
				instance,
			), true
		}
	}

	return TaskInfo{}, false
}

func (g *Generator) taskInfoFromImportedGenericTaskInstance(instance *ImportedGenericTaskInstance) TaskInfo {
	if instance == nil {
		return TaskInfo{
			ReturnType:  CInvalid,
			ReturnTypes: []CType{CInvalid},
		}
	}

	subst := genericArgSubstForCGen(instance.Info.GenericParams, instance.Args)

	info := TaskInfo{
		Decl:           nil,
		GenericParams:  nil,
		ReturnType:     g.importedGenericTaskReturnType(instance),
		ReturnTypes:    g.importedGenericTaskReturnTypes(instance),
		RequiredParams: instance.Info.RequiredParams,
		IsExtern:       instance.Info.IsExtern,
		ExternName:     instance.Info.ExternName,
		IsPure:         instance.Info.IsPure,
		IsIntrinsic:    instance.Info.IsIntrinsic,
		IsTrustedPure:  instance.Info.IsTrustedPure,
	}

	if info.RequiredParams == 0 {
		info.RequiredParams = len(instance.Info.ParamTypeAsts)
	}

	g.withTypeContext(instance.PackageName, func() {
		for i, paramAst := range instance.Info.ParamTypeAsts {
			paramType := g.cTypeFromAstWithGenericArgs(paramAst, subst)

			info.ParamTypes = append(info.ParamTypes, paramType)

			hasDefault := false
			if i < len(instance.Info.ParamHasDefault) {
				hasDefault = instance.Info.ParamHasDefault[i]
			}

			isVariadic := false
			if i < len(instance.Info.ParamIsVariadic) {
				isVariadic = instance.Info.ParamIsVariadic[i]
			}

			info.ParamHasDefault = append(info.ParamHasDefault, hasDefault)
			info.ParamIsVariadic = append(info.ParamIsVariadic, isVariadic)

			if hasDefault && i < len(instance.Info.ParamDefaults) {
				info.ParamDefaults = append(info.ParamDefaults, g.substituteExprForCGen(instance.Info.ParamDefaults[i], subst))
			} else {
				info.ParamDefaults = append(info.ParamDefaults, nil)
			}

			if isVariadic {
				info.IsVariadic = true
				if info.RequiredParams == len(instance.Info.ParamTypeAsts) {
					info.RequiredParams = i
				}
			}

			if hasDefault && info.RequiredParams == len(instance.Info.ParamTypeAsts) {
				info.RequiredParams = i
			}
		}
	})

	return info
}

func (g *Generator) taskInfoFromGenericTaskInstance(instance *GenericTaskInstance) TaskInfo {
	if instance == nil || instance.Decl == nil {
		return TaskInfo{
			ReturnType:  CInvalid,
			ReturnTypes: []CType{CInvalid},
		}
	}

	decl := instance.Decl
	subst := genericTaskSubstForCGen(decl.GenericParams, instance.Args)

	info := TaskInfo{
		Decl:           decl,
		GenericParams:  nil,
		ReturnType:     g.genericTaskReturnType(instance),
		ReturnTypes:    g.genericTaskReturnTypes(instance),
		RequiredParams: len(decl.Params),
		IsExtern:       decl.IsExtern,
		ExternName:     decl.ExternName,
		IsPure:         decl.IsPure,
		IsIntrinsic:    decl.IsIntrinsic,
		IsTrustedPure:  decl.IsTrustedPure,
	}

	for i, param := range decl.Params {
		paramType := g.cTypeFromAstWithGenericArgs(param.Type, subst)

		info.ParamTypes = append(info.ParamTypes, paramType)
		info.ParamHasDefault = append(info.ParamHasDefault, param.HasDefault)
		info.ParamIsVariadic = append(info.ParamIsVariadic, param.IsVariadic)

		if param.HasDefault {
			info.ParamDefaults = append(info.ParamDefaults, g.substituteExprForCGen(param.Default, subst))
		} else {
			info.ParamDefaults = append(info.ParamDefaults, nil)
		}

		if param.IsVariadic {
			info.IsVariadic = true
			if info.RequiredParams == len(decl.Params) {
				info.RequiredParams = i
			}
		}

		if param.HasDefault && info.RequiredParams == len(decl.Params) {
			info.RequiredParams = i
		}
	}

	return info
}

func (g *Generator) genericTaskParamArg(name string) (ast.GenericArg, bool) {
	if g.genericSubst == nil {
		return ast.GenericArg{}, false
	}

	if g.scope != nil {
		if _, isLocal := g.scope.lookup(name); isLocal {
			return ast.GenericArg{}, false
		}
	}

	arg, ok := g.genericSubst[name]
	return arg, ok
}

func (g *Generator) genericTaskParamReturnType(name string) (CType, bool) {
	arg, ok := g.genericTaskParamArg(name)
	if !ok {
		return CInvalid, false
	}

	return g.taskReturnTypeFromGenericArg(arg)
}

func (g *Generator) genericTaskParamReturnTypes(name string) ([]CType, bool) {
	arg, ok := g.genericTaskParamArg(name)
	if !ok {
		return nil, false
	}

	return g.taskReturnTypesFromGenericArg(arg)
}

func (g *Generator) taskReturnTypesFromGenericArg(arg ast.GenericArg) ([]CType, bool) {
	info, ok := g.taskInfoFromGenericArg(arg)
	if !ok {
		return nil, false
	}

	return info.ReturnTypes, true
}

func (g *Generator) genericTaskParamCallName(name string) (string, bool) {
	arg, ok := g.genericTaskParamArg(name)
	if !ok {
		return "", false
	}

	return g.taskCallNameFromGenericArg(arg)
}

func (g *Generator) taskReturnTypeFromGenericArg(arg ast.GenericArg) (CType, bool) {
	info, ok := g.taskInfoFromGenericArg(arg)
	if !ok {
		return CInvalid, false
	}

	return info.ReturnType, true
}

func (g *Generator) taskCallNameFromGenericArg(
	arg ast.GenericArg,
) (string, bool) {
	if g.genericSubst != nil {
		arg = g.substituteGenericArgForCGen(
			arg,
			g.genericSubst,
		)
	}

	if arg.Kind != ast.GenericArgExpr ||
		arg.Expr == nil {
		g.error(
			arg.Span(),
			"generic task parameter requires a task argument",
		)
		return "0", true
	}

	switch e := arg.Expr.(type) {
	case *ast.IdentExpr:
		if packageName, info, found :=
			g.importedTaskInfoFromTypeContext(
				e.Name.Name,
			); found {
			if len(info.GenericParams) > 0 {
				g.error(
					e.Span(),
					fmt.Sprintf(
						"generic task argument %q requires specialization",
						e.Name.Name,
					),
				)
				return "0", true
			}

			return cImportedTaskName(
				packageName,
				e.Name.Name,
				info,
			), true
		}

		info, ok := g.tasks[e.Name.Name]
		if !ok {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"unknown task argument %q",
					e.Name.Name,
				),
			)
			return "0", true
		}

		if len(info.GenericParams) > 0 {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"generic task argument %q requires specialization",
					e.Name.Name,
				),
			)
			return "0", true
		}

		return g.cTaskName(e.Name.Name), true

	case *ast.SelectorExpr:
		id, ok := e.Left.(*ast.IdentExpr)
		if !ok {
			g.error(
				e.Span(),
				"unsupported task argument selector",
			)
			return "0", true
		}

		pkg := g.packages[id.Name.Name]
		if pkg == nil {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"unknown package %q",
					id.Name.Name,
				),
			)
			return "0", true
		}

		info, ok := pkg.Tasks[e.Name.Name]
		if !ok {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"package %s has no task %q",
					id.Name.Name,
					e.Name.Name,
				),
			)
			return "0", true
		}

		if len(info.GenericParams) > 0 {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"imported generic task argument %q requires specialization",
					e.Name.Name,
				),
			)
			return "0", true
		}

		return cImportedTaskName(
			id.Name.Name,
			e.Name.Name,
			info,
		), true

	case *ast.GenericExpr:
		switch base := e.Base.(type) {
		case *ast.IdentExpr:
			if packageName, taskName, info, found :=
				g.importedGenericTaskInfoFromTypeContext(
					base.Name.Name,
				); found {
				callArgs := e.Args

				if g.genericSubst != nil {
					callArgs = make(
						[]ast.GenericArg,
						0,
						len(e.Args),
					)

					for _, genericArg := range e.Args {
						callArgs = append(
							callArgs,
							g.substituteGenericArgForCGen(
								genericArg,
								g.genericSubst,
							),
						)
					}
				}

				name := g.registerImportedGenericTaskInstance(
					packageName,
					taskName,
					info,
					callArgs,
				)

				return name, true
			}

			info, ok := g.tasks[base.Name.Name]
			if !ok ||
				info.Decl == nil ||
				len(info.GenericParams) == 0 {
				g.error(
					e.Span(),
					fmt.Sprintf(
						"generic task argument %q is not supported by C codegen yet",
						base.Name.Name,
					),
				)
				return "0", true
			}

			callArgs := e.Args

			if g.genericSubst != nil {
				callArgs = make(
					[]ast.GenericArg,
					0,
					len(e.Args),
				)

				for _, genericArg := range e.Args {
					callArgs = append(
						callArgs,
						g.substituteGenericArgForCGen(
							genericArg,
							g.genericSubst,
						),
					)
				}
			}

			name := g.registerGenericTaskInstance(
				info.Decl,
				callArgs,
			)

			return name, true

		case *ast.SelectorExpr:
			pkgName, taskName, info, ok :=
				g.importedGenericTaskInfoFromSelector(base)

			if !ok {
				g.error(
					e.Span(),
					"unsupported imported generic task argument",
				)
				return "0", true
			}

			callArgs := e.Args

			if g.genericSubst != nil {
				callArgs = make(
					[]ast.GenericArg,
					0,
					len(e.Args),
				)

				for _, genericArg := range e.Args {
					callArgs = append(
						callArgs,
						g.substituteGenericArgForCGen(
							genericArg,
							g.genericSubst,
						),
					)
				}
			}

			name := g.registerImportedGenericTaskInstance(
				pkgName,
				taskName,
				info,
				callArgs,
			)

			return name, true
		}
	}

	g.error(
		arg.Span(),
		"unsupported generic task argument",
	)

	return "0", true
}

func (g *Generator) emitExpr(
	expr ast.Expr,
	expected *CType,
) string {
	if expected != nil &&
		expected.SealName == "any" {
		return g.emitAnyExpr(expr)
	}

	if expected != nil {
		if value, ok :=
			g.tryEmitInterfaceConversion(
				*expected,
				expr,
			); ok {
			return value
		}
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope != nil {
			if _, ok :=
				g.scope.lookup(e.Name.Name); ok {
				return e.Name.Name
			}
		}

		if g.genericSubst != nil {
			if arg, ok :=
				g.genericSubst[e.Name.Name]; ok &&
				arg.Kind == ast.GenericArgExpr &&
				arg.Expr != nil {
				if genericArgIsSingleNameForCGen(
					arg,
					e.Name.Name,
				) {
					return e.Name.Name
				}

				return g.emitExpr(
					arg.Expr,
					expected,
				)
			}
		}

		return e.Name.Name

	case *ast.DotIdentExpr:
		if expected != nil &&
			expected.SealName != "" &&
			g.isEnumCType(*expected) {
			return fmt.Sprintf(
				"%s_%s",
				expected.SealName,
				e.Name.Name,
			)
		}

		g.error(
			e.Span(),
			fmt.Sprintf(
				"enum literal .%s needs C codegen context",
				e.Name.Name,
			),
		)
		return "0"

	case *ast.IntLitExpr:
		return e.Value

	case *ast.FloatLitExpr:
		return e.Value

	case *ast.StringLitExpr:
		return g.emitStringLiteral(e)

	case *ast.CStringLitExpr:
		return g.emitCStringLiteral(e)

	case *ast.CharLitExpr:
		return g.emitCharLiteral(e)

	case *ast.GenericExpr:
		g.error(
			e.Span(),
			"generic expression cannot be emitted as a value",
		)
		return "0"

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		if expected != nil &&
			g.isUnion(*expected) {
			return fmt.Sprintf(
				"(%s){.tag = %s_Tag_nil}",
				expected.Name,
				expected.SealName,
			)
		}

		if expected != nil &&
			g.isInterfaceCType(*expected) {
			return g.nilInterfaceValue(*expected)
		}

		return "NULL"

	case *ast.UnaryExpr:
		return fmt.Sprintf(
			"(%s%s)",
			g.cUnaryOp(e.Op),
			g.emitExpr(e.Expr, nil),
		)

	case *ast.BinaryExpr:
		leftType := g.inferExprType(e.Left, nil)
		rightType := g.inferExprType(e.Right, nil)

		if value, ok :=
			g.emitBuiltinTextBinaryExpr(
				e,
				leftType,
				rightType,
			); ok {
			return value
		}

		if g.hasOperatorOverload(e.Op.String()) {
			if candidate, ok :=
				g.resolveOverload(
					e.Op.String(),
					[]CType{
						leftType,
						rightType,
					},
				); ok {
				left := g.emitExpr(
					e.Left,
					&leftType,
				)

				right := g.emitExpr(
					e.Right,
					&rightType,
				)

				return fmt.Sprintf(
					"%s(%s, %s)",
					g.cTaskName(candidate),
					left,
					right,
				)
			}
		}

		if e.Op == token.NotEq &&
			g.hasOperatorOverload("==") {
			if candidate, ok :=
				g.resolveOverload(
					"==",
					[]CType{
						leftType,
						rightType,
					},
				); ok {
				left := g.emitExpr(
					e.Left,
					&leftType,
				)

				right := g.emitExpr(
					e.Right,
					&rightType,
				)

				return fmt.Sprintf(
					"(!%s(%s, %s))",
					g.cTaskName(candidate),
					left,
					right,
				)
			}
		}

		left := g.emitExpr(e.Left, nil)
		right := g.emitExpr(e.Right, nil)

		return fmt.Sprintf(
			"(%s %s %s)",
			left,
			g.cBinaryOp(e.Op),
			right,
		)

	case *ast.CallExpr:
		return g.emitCallExpr(e)

	case *ast.SpreadExpr:
		g.error(
			e.Span(),
			"spread can only be emitted as a call argument",
		)
		return "0"

	case *ast.SelectorExpr:
		if id, ok :=
			e.Left.(*ast.IdentExpr); ok {
			if _, ok :=
				g.packages[id.Name.Name]; ok {
				return cPackageTaskName(
					id.Name.Name,
					e.Name.Name,
				)
			}
		}

		left := g.emitExpr(e.Left, nil)
		leftType :=
			g.inferExprType(e.Left, nil)

		if leftType.SealName == "string" {
			g.error(
				e.Name.Span(),
				fmt.Sprintf(
					"string has no field %q",
					e.Name.Name,
				),
			)
			return "0"
		}

		if leftType.SealName == "cstring" {
			g.error(
				e.Name.Span(),
				fmt.Sprintf(
					"cstring has no field %q",
					e.Name.Name,
				),
			)
			return "0"
		}

		if strings.HasPrefix(
			leftType.SealName,
			"*",
		) {
			return fmt.Sprintf(
				"(%s)->%s",
				left,
				e.Name.Name,
			)
		}

		return fmt.Sprintf(
			"(%s).%s",
			left,
			e.Name.Name,
		)

	case *ast.IndexExpr:
		return g.emitIndexExpr(e)

	case *ast.CompoundLiteralExpr:
		typ := g.cTypeFromAstInContext(e.Type)

		if _, ok :=
			g.distincts[typ.SealName]; ok {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"distinct type %s cannot be constructed with a literal; use cast<%s>(value)",
					typ.SealName,
					typ.SealName,
				),
			)
			return "0"
		}

		if expected != nil &&
			g.isUnion(*expected) &&
			g.unionHasMember(
				expected.SealName,
				typ.SealName,
			) {
			payload :=
				g.emitCompoundLiteral(e, typ)

			return fmt.Sprintf(
				"(%s){.tag = %s_Tag_%s, .as.%s = %s}",
				expected.Name,
				expected.SealName,
				typ.SealName,
				typ.SealName,
				payload,
			)
		}

		return g.emitCompoundLiteral(e, typ)
	}

	g.error(
		expr.Span(),
		"unsupported expression in C codegen",
	)

	return "0"
}

func (g *Generator) emitBuiltinTextBinaryExpr(
	e *ast.BinaryExpr,
	leftType CType,
	rightType CType,
) (string, bool) {
	if e.Op != token.EqEq &&
		e.Op != token.NotEq {
		return "", false
	}

	var comparison string

	switch {
	case leftType.SealName == "string" &&
		rightType.SealName == "string":
		left := g.emitExpr(e.Left, &leftType)
		right := g.emitExpr(e.Right, &rightType)

		comparison = fmt.Sprintf(
			"seal_string_equal(%s, %s)",
			left,
			right,
		)

	case leftType.SealName == "cstring" &&
		rightType.SealName == "cstring":
		left := g.emitExpr(e.Left, &leftType)
		right := g.emitExpr(e.Right, &rightType)

		comparison = fmt.Sprintf(
			"seal_cstring_equal(%s, %s)",
			left,
			right,
		)

	default:
		return "", false
	}

	if e.Op == token.NotEq {
		return fmt.Sprintf(
			"(!(%s))",
			comparison,
		), true
	}

	return comparison, true
}

func (g *Generator) emitStringLiteral(
	e *ast.StringLitExpr,
) string {
	value, err := strconv.Unquote(e.Value)
	if err != nil {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"invalid string literal: %v",
				err,
			),
		)

		return "(sealString){.data = NULL, .len = 0}"
	}

	if !utf8.ValidString(value) {
		g.error(
			e.Span(),
			"string literal must contain valid UTF-8",
		)

		return "(sealString){.data = NULL, .len = 0}"
	}

	return fmt.Sprintf(
		"(sealString){.data = (const uint8_t *)%s, .len = (uintptr_t)%d}",
		quoteCByteString(value),
		len(value),
	)
}

func (g *Generator) emitCStringLiteral(
	e *ast.CStringLitExpr,
) string {
	raw := strings.TrimPrefix(
		e.Value,
		"c",
	)

	value, err := strconv.Unquote(raw)
	if err != nil {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"invalid cstring literal: %v",
				err,
			),
		)

		return `""`
	}

	if !utf8.ValidString(value) {
		g.error(
			e.Span(),
			"cstring literal must contain valid UTF-8",
		)

		return `""`
	}

	if strings.IndexByte(value, 0) >= 0 {
		g.error(
			e.Span(),
			"cstring literal cannot contain an embedded null byte",
		)

		return `""`
	}

	return quoteCByteString(value)
}

func (g *Generator) emitCharLiteral(
	e *ast.CharLitExpr,
) string {
	value, err := strconv.Unquote(e.Value)
	if err != nil {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"invalid char literal: %v",
				err,
			),
		)

		return "0"
	}

	if !utf8.ValidString(value) {
		g.error(
			e.Span(),
			"char literal must contain valid UTF-8",
		)

		return "0"
	}

	if utf8.RuneCountInString(value) != 1 {
		g.error(
			e.Span(),
			"char literal must contain exactly one Unicode scalar value",
		)

		return "0"
	}

	scalar, _ := utf8.DecodeRuneInString(value)

	if !utf8.ValidRune(scalar) {
		g.error(
			e.Span(),
			"char literal contains an invalid Unicode scalar value",
		)

		return "0"
	}

	return fmt.Sprintf(
		"((uint32_t)%d)",
		scalar,
	)
}

func (g *Generator) emitAnyExpr(expr ast.Expr) string {
	srcType := g.inferExprType(expr, nil)

	if srcType.SealName == "any" {
		return g.emitExpr(expr, nil)
	}

	value := g.emitExpr(expr, &srcType)

	spec, ok := builtin.LookupType(srcType.SealName)
	if !ok || spec.AnyCtor == "" {
		g.error(expr.Span(), fmt.Sprintf("cannot box %s as any yet", srcType.String()))
		return "sealAny_any((sealAny){0})"
	}

	return fmt.Sprintf("%s(%s)", spec.AnyCtor, value)
}

func (g *Generator) emitCallExpr(e *ast.CallExpr) string {
	return g.emitCallExprWithArgs(e, nil)
}

func (g *Generator) emitGenericCall(gen *ast.GenericExpr, args []ast.Expr, preparedArgs []string) string {
	if resolution, ok :=
		g.genericOverloadCalls[gen]; ok {
		if resolution.Candidate == nil {
			g.error(
				gen.Span(),
				"checker generic-overload resolution has no candidate",
			)
			return "0"
		}

		name, info, selected :=
			g.semanticTaskSelection(
				resolution.Candidate,
				resolution.PackageName,
				resolution.GenericArguments,
				gen.Span(),
			)

		if !selected {
			return "0"
		}

		return g.emitSemanticTaskCallInTypeContext(
			resolution.PackageName,
			name,
			info,
			args,
			preparedArgs,
		)
	}

	if id, ok := gen.Base.(*ast.IdentExpr); ok {
		if task, ok := builtin.LookupTask(id.Name.Name); ok && task.Generic {
			return g.emitGenericIntrinsicCall(gen, args)
		}

		if packageName,
			taskName,
			info,
			ok :=
			g.importedGenericTaskInfoFromTypeContext(
				id.Name.Name,
			); ok {
			callArgs := gen.Args

			if g.genericSubst != nil {
				callArgs = make(
					[]ast.GenericArg,
					0,
					len(gen.Args),
				)

				for _, arg := range gen.Args {
					callArgs = append(
						callArgs,
						g.substituteGenericArgForCGen(
							arg,
							g.genericSubst,
						),
					)
				}
			}

			name :=
				g.registerImportedGenericTaskInstance(
					packageName,
					taskName,
					info,
					callArgs,
				)

			subst := genericArgSubstForCGen(
				info.GenericParams,
				callArgs,
			)

			return g.emitGenericCallToNameInTypeContext(
				packageName,
				name,
				info.ParamTypeAsts,
				info.ParamDefaults,
				info.ParamHasDefault,
				subst,
				args,
				preparedArgs,
			)
		}

		info, ok := g.tasks[id.Name.Name]
		if !ok || len(info.GenericParams) == 0 || info.Decl == nil {
			g.error(gen.Span(), fmt.Sprintf("generic task call %q is not supported by C codegen yet", id.Name.Name))
			return "0"
		}

		callArgs := gen.Args
		if g.genericSubst != nil {
			callArgs = make([]ast.GenericArg, 0, len(gen.Args))
			for _, arg := range gen.Args {
				callArgs = append(callArgs, g.substituteGenericArgForCGen(arg, g.genericSubst))
			}
		}

		name := g.registerGenericTaskInstance(info.Decl, callArgs)
		subst := genericTaskSubstForCGen(info.GenericParams, callArgs)

		return g.emitGenericCallToName(name, info.ParamTypeAsts, info.ParamDefaults, info.ParamHasDefault, subst, args, preparedArgs)
	}

	if selector, ok := gen.Base.(*ast.SelectorExpr); ok {
		pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(selector)
		if !ok {
			g.error(gen.Span(), "unsupported imported generic task call")
			return "0"
		}

		callArgs := gen.Args
		if g.genericSubst != nil {
			callArgs = make([]ast.GenericArg, 0, len(gen.Args))
			for _, arg := range gen.Args {
				callArgs = append(callArgs, g.substituteGenericArgForCGen(arg, g.genericSubst))
			}
		}

		name := g.registerImportedGenericTaskInstance(pkgName, taskName, info, callArgs)
		subst := genericArgSubstForCGen(info.GenericParams, callArgs)

		return g.emitGenericCallToNameInTypeContext(pkgName, name, info.ParamTypeAsts, info.ParamDefaults, info.ParamHasDefault, subst, args, preparedArgs)
	}

	g.error(gen.Base.Span(), "unsupported generic callee")
	return "0"
}

func (g *Generator) emitGenericCallToNameInTypeContext(packageName string, name string, paramTypes []ast.Type, paramDefaults []ast.Expr, paramHasDefault []bool, subst map[string]ast.GenericArg, args []ast.Expr, preparedArgs []string) string {
	old := g.typeContextPackage
	g.typeContextPackage = packageName
	out := g.emitGenericCallToName(name, paramTypes, paramDefaults, paramHasDefault, subst, args, preparedArgs)
	g.typeContextPackage = old
	return out
}

func (g *Generator) emitGenericCallToName(name string, paramTypes []ast.Type, paramDefaults []ast.Expr, paramHasDefault []bool, subst map[string]ast.GenericArg, args []ast.Expr, preparedArgs []string) string {
	var outArgs []string

	for i, arg := range args {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := (*CType)(nil)

		if i < len(paramTypes) {
			paramType := g.cTypeFromAstWithGenericArgs(paramTypes[i], subst)
			expected = &paramType
		}

		outArgs = append(outArgs, g.emitExpr(arg, expected))
	}

	for i := len(args); i < len(paramTypes); i++ {
		if i >= len(paramHasDefault) || !paramHasDefault[i] {
			continue
		}

		expected := g.cTypeFromAstWithGenericArgs(paramTypes[i], subst)
		defaultExpr := ast.Expr(nil)

		if i < len(paramDefaults) {
			defaultExpr = g.substituteExprForCGen(paramDefaults[i], subst)
		}

		outArgs = append(outArgs, g.emitExpr(defaultExpr, &expected))
	}

	return fmt.Sprintf("%s(%s)", name, strings.Join(outArgs, ", "))
}

func (g *Generator) emitCallExprWithArgs(
	e *ast.CallExpr,
	preparedArgs []string,
) string {
	if gen, ok :=
		e.Callee.(*ast.GenericExpr); ok {
		return g.emitGenericCall(
			gen,
			e.Args,
			preparedArgs,
		)
	}

	if id, ok :=
		e.Callee.(*ast.IdentExpr); ok {
		isLocal := false

		if g.scope != nil {
			_, isLocal =
				g.scope.lookup(id.Name.Name)
		}

		if !isLocal {
			if name, ok :=
				g.genericTaskParamCallName(
					id.Name.Name,
				); ok {
				info, hasInfo :=
					g.genericTaskParamInfo(
						id.Name.Name,
					)

				if hasInfo {
					return g.emitDirectCNamedCall(
						name,
						e.Args,
						preparedArgs,
						info.ParamTypes,
					)
				}

				return g.emitDirectCNamedCall(
					name,
					e.Args,
					preparedArgs,
					nil,
				)
			}
		}

		if _, ok := g.lenResolutions[e]; ok {
			return g.emitLenCall(
				e,
				preparedArgs,
			)
		}

		if packageName, _, ok :=
			g.importedTaskInfoFromTypeContext(
				id.Name.Name,
			); ok {
			return g.emitPackageTaskCall(
				packageName,
				id.Name.Name,
				e.Args,
				preparedArgs,
			)
		}

		if len(e.Args) > 0 {
			firstType :=
				g.inferExprType(
					e.Args[0],
					nil,
				)

			if g.isInterfaceCType(firstType) {
				if _, _, ok :=
					g.lookupInterfaceRequirement(
						firstType,
						id.Name.Name,
					); ok {
					return g.emitInterfaceDispatchCall(
						firstType,
						id.Name.Name,
						e.Args,
						preparedArgs,
					)
				}
			}
		}

		if _, ok := g.tasks[id.Name.Name]; ok {
			return g.emitTaskCall(
				id.Name.Name,
				e.Args,
				preparedArgs,
			)
		}

		if _, ok :=
			g.overloads[id.Name.Name]; ok {
			argTypes := make(
				[]CType,
				0,
				len(e.Args),
			)

			for _, arg := range e.Args {
				argTypes = append(
					argTypes,
					g.inferExprType(
						arg,
						nil,
					),
				)
			}

			if candidate, ok :=
				g.resolveOverload(
					id.Name.Name,
					argTypes,
				); ok {
				return g.emitTaskCall(
					candidate,
					e.Args,
					preparedArgs,
				)
			}
		}

		if kind, ok :=
			g.primitiveTaskKind(
				id.Name.Name,
			); ok {
			switch kind {
			case builtin.TaskLen:
				g.error(
					e.Span(),
					"missing checker resolution for primitive len call",
				)
				return "0"

			case builtin.TaskSize:
				return g.emitSizeCall(e)

			case builtin.TaskAssert:
				return g.emitAssertCall(e)

			case builtin.TaskPanic:
				return g.emitPanicCall(e)

			case builtin.TaskTrap:
				return g.emitNoArgRuntimeCall(
					"trap",
					"seal_trap",
					e,
				)

			case builtin.TaskUnreachable:
				return g.emitNoArgRuntimeCall(
					"unreachable",
					"seal_unreachable",
					e,
				)
			}
		}
	}

	if selector, ok :=
		e.Callee.(*ast.SelectorExpr); ok {
		if id, ok :=
			selector.Left.(*ast.IdentExpr); ok {
			if pkg :=
				g.packages[id.Name.Name]; pkg != nil {
				return g.emitPackageTaskCall(
					id.Name.Name,
					selector.Name.Name,
					e.Args,
					preparedArgs,
				)
			}
		}
	}

	var args []string

	if preparedArgs != nil {
		args = append(args, preparedArgs...)
	} else {
		for _, arg := range e.Args {
			args = append(
				args,
				g.emitExpr(arg, nil),
			)
		}
	}

	return fmt.Sprintf(
		"%s(%s)",
		g.emitExpr(e.Callee, nil),
		strings.Join(args, ", "),
	)
}

func (g *Generator) emitGenericIntrinsicCall(
	gen *ast.GenericExpr,
	args []ast.Expr,
) string {
	id, ok := gen.Base.(*ast.IdentExpr)
	if !ok {
		g.error(
			gen.Base.Span(),
			"only intrinsic generic calls are supported here",
		)
		return "0"
	}

	task, ok := builtin.LookupTask(id.Name.Name)
	if !ok || !task.Generic {
		g.error(
			id.Span(),
			fmt.Sprintf(
				"unknown generic intrinsic %q",
				id.Name.Name,
			),
		)
		return "0"
	}

	if len(gen.Args) != 1 {
		g.error(
			gen.Span(),
			fmt.Sprintf(
				"%s expects exactly 1 type argument",
				id.Name.Name,
			),
		)
		return "0"
	}

	targetArg := gen.Args[0]

	if g.genericSubst != nil {
		targetArg =
			g.substituteGenericArgForCGen(
				targetArg,
				g.genericSubst,
			)
	}

	target := g.cTypeFromGenericArg(targetArg)

	switch task.Kind {
	case builtin.TaskAnyIs:
		if len(args) != 1 {
			g.error(
				gen.Span(),
				"anyIs expects exactly 1 value argument",
			)
			return "false"
		}

		value := g.emitExpr(args[0], nil)

		kind, ok := g.sealTypeKindFor(target)
		if !ok {
			g.error(
				gen.Args[0].Span(),
				fmt.Sprintf(
					"anyIs does not support %s yet",
					target.String(),
				),
			)
			return "false"
		}

		return fmt.Sprintf(
			"((%s).type == %s)",
			value,
			kind,
		)

	case builtin.TaskAnyAs:
		if len(args) != 1 {
			g.error(
				gen.Span(),
				"anyAs expects exactly 1 value argument",
			)
			return "0"
		}

		value := g.emitExpr(args[0], nil)

		field, ok := g.sealAnyFieldFor(target)
		if !ok {
			g.error(
				gen.Args[0].Span(),
				fmt.Sprintf(
					"anyAs does not support %s yet",
					target.String(),
				),
			)
			return "0"
		}

		if target.SealName == "any" {
			return value
		}

		return fmt.Sprintf(
			"((%s).value.%s)",
			value,
			field,
		)

	case builtin.TaskCast:
		if g.isInterfaceCType(target) {
			if len(args) != 1 {
				g.error(
					gen.Span(),
					"interface cast expects exactly 1 value argument",
				)
				return g.nilInterfaceValue(target)
			}

			value, ok :=
				g.tryEmitInterfaceConversion(
					target,
					args[0],
				)

			if ok {
				return value
			}

			g.error(
				args[0].Span(),
				fmt.Sprintf(
					"cannot lower cast from %s to interface %s",
					g.inferExprType(args[0], nil).String(),
					target.String(),
				),
			)

			return g.nilInterfaceValue(target)
		}

		switch target.SealName {
		case "string":
			if len(args) != 2 {
				g.error(
					gen.Span(),
					"cast<string> expects data and byte length",
				)

				return "(sealString){.data = NULL, .len = 0}"
			}

			data := g.emitExpr(
				args[0],
				nil,
			)

			byteLength := g.emitExpr(
				args[1],
				&CUint,
			)

			return fmt.Sprintf(
				"(sealString){.data = (const uint8_t *)(%s), .len = (uintptr_t)(%s)}",
				data,
				byteLength,
			)

		case "cstring":
			if len(args) != 2 {
				g.error(
					gen.Span(),
					"cast<cstring> expects exactly 2 arguments: rawptr and uint byte length",
				)

				return "((const char *)NULL)"
			}

			sourceType := g.inferExprType(
				args[0],
				nil,
			)

			if sourceType.SealName == "string" {
				g.error(
					args[0].Span(),
					"cannot cast string directly to cstring because string is not guaranteed to be null-terminated",
				)

				return "((const char *)NULL)"
			}

			data := g.emitExpr(
				args[0],
				nil,
			)

			byteLength := g.emitExpr(
				args[1],
				&CUint,
			)

			return fmt.Sprintf(
				"seal_cstring_from_parts((const char *)(%s), (uintptr_t)(%s))",
				data,
				byteLength,
			)

		case "rawptr":
			if len(args) != 1 {
				g.error(
					gen.Span(),
					"cast<rawptr> expects exactly 1 value argument",
				)
				return "NULL"
			}

			sourceType :=
				g.inferExprType(
					args[0],
					nil,
				)

			value := g.emitExpr(
				args[0],
				nil,
			)

			switch sourceType.SealName {
			case "string":
				return fmt.Sprintf(
					"((void *)((%s).data))",
					value,
				)

			case "cstring":
				return fmt.Sprintf(
					"((void *)(%s))",
					value,
				)
			}

			return fmt.Sprintf(
				"((%s)(%s))",
				target.Name,
				value,
			)

		default:
			if len(args) != 1 {
				g.error(
					gen.Span(),
					fmt.Sprintf(
						"cast<%s> expects exactly 1 value argument",
						target.SealName,
					),
				)
				return "0"
			}

			value := g.emitExpr(
				args[0],
				nil,
			)

			return fmt.Sprintf(
				"((%s)(%s))",
				target.Name,
				value,
			)
		}

	default:
		g.error(
			id.Span(),
			fmt.Sprintf(
				"unknown generic intrinsic %q",
				id.Name.Name,
			),
		)
		return "0"
	}
}

func (g *Generator) nilInterfaceValue(iface CType) string {
	if iface.IsDynInterface {
		return fmt.Sprintf(
			"(%s){.data = NULL, .vtable = NULL}",
			iface.Name,
		)
	}

	return fmt.Sprintf(
		"(%s){.tag = 0, .data = NULL}",
		iface.Name,
	)
}

func (g *Generator) importedTaskInfoFromTypeContext(
	name string,
) (string, TaskInfo, bool) {
	packageName := g.typeContextPackage

	if packageName == "" ||
		packageName == g.packageName {
		return "", TaskInfo{}, false
	}

	pkg := g.packages[packageName]
	if pkg == nil {
		return "", TaskInfo{}, false
	}

	info, ok := pkg.Tasks[name]
	if !ok || len(info.GenericParams) != 0 {
		return "", TaskInfo{}, false
	}

	info = g.taskInfoInPackageContext(
		packageName,
		name,
		info,
	)

	return packageName, info, true
}

func (g *Generator) importedGenericTaskInfoFromTypeContext(
	name string,
) (string, string, TaskInfo, bool) {
	packageName := g.typeContextPackage

	if packageName == "" ||
		packageName == g.packageName {
		return "", "", TaskInfo{}, false
	}

	pkg := g.packages[packageName]
	if pkg == nil {
		return "", "", TaskInfo{}, false
	}

	info, ok := pkg.Tasks[name]
	if !ok || len(info.GenericParams) == 0 {
		return "", "", TaskInfo{}, false
	}

	return packageName, name, info, true
}

func (g *Generator) importedGenericTaskInfoFromSelector(sel *ast.SelectorExpr) (string, string, TaskInfo, bool) {
	id, ok := sel.Left.(*ast.IdentExpr)
	if !ok {
		return "", "", TaskInfo{}, false
	}

	pkg := g.packages[id.Name.Name]
	if pkg == nil {
		return "", "", TaskInfo{}, false
	}

	info, ok := pkg.Tasks[sel.Name.Name]
	if !ok {
		return "", "", TaskInfo{}, false
	}

	if len(info.GenericParams) == 0 {
		return "", "", TaskInfo{}, false
	}

	return id.Name.Name, sel.Name.Name, info, true
}

func (g *Generator) registerImportedGenericTaskInstance(
	packageName string,
	taskName string,
	info TaskInfo,
	args []ast.GenericArg,
) string {
	args = normalizeGenericArgsForCGenParams(
		info.GenericParams,
		args,
	)

	// Keep the original arguments for type lowering in the calling package.
	//
	// For example, app sees:
	//
	//     mem.NewC<Point>() -> Point *
	//
	// The owning package request, however, must retain the source package:
	//
	//     Point -> app.Point
	//
	// This lets mem generate sizeof(app_Point) rather than sizeof(Point).
	requestArgs := g.crossPackageRequestGenericArgs(
		info.GenericParams,
		args,
	)

	name := g.specializedImportedTaskCName(
		packageName,
		taskName,
		info,
		requestArgs,
	)

	if _, exists := g.importedGenericTasks[name]; exists {
		g.addImportedGenericTaskRequest(
			packageName,
			taskName,
			append(
				[]ast.GenericArg(nil),
				requestArgs...,
			),
		)

		return name
	}

	copiedArgs := append(
		[]ast.GenericArg(nil),
		args...,
	)

	g.importedGenericTasks[name] =
		&ImportedGenericTaskInstance{
			PackageName: packageName,
			TaskName:    taskName,
			Name:        name,
			Info:        info,
			Args:        copiedArgs,
		}

	g.addImportedGenericTaskRequest(
		packageName,
		taskName,
		append(
			[]ast.GenericArg(nil),
			requestArgs...,
		),
	)

	return name
}

func (g *Generator) specializedImportedTaskCName(packageName string, taskName string, info TaskInfo, args []ast.GenericArg) string {
	parts := []string{sanitizeCName(packageName), sanitizeCName(taskName)}

	for i, arg := range args {
		paramCategory := ast.GenericParamInvalid
		if i < len(info.GenericParams) {
			paramCategory = info.GenericParams[i].Category
		}

		switch paramCategory {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			parts = append(parts, g.genericTypeArgCName(arg))

		case ast.GenericParamTask:
			parts = append(parts, g.genericTaskArgCName(arg))

		default:
			parts = append(parts, genericValueArgCName(arg))
		}
	}

	return strings.Join(parts, "_")
}

func (g *Generator) importedGenericTaskReturnTypes(info *ImportedGenericTaskInstance) []CType {
	if info == nil {
		return nil
	}

	subst := genericArgSubstForCGen(info.Info.GenericParams, info.Args)

	results := make([]CType, 0, len(info.Info.ResultTypeAsts))

	g.withTypeContext(info.PackageName, func() {
		for _, result := range info.Info.ResultTypeAsts {
			results = append(results, g.cTypeFromAstWithGenericArgs(result, subst))
		}
	})

	return results
}

func (g *Generator) importedGenericTaskReturnType(info *ImportedGenericTaskInstance) CType {
	results := g.importedGenericTaskReturnTypes(info)

	if len(results) == 0 {
		return CVoid
	}

	if len(results) == 1 {
		return results[0]
	}

	name := g.importedGenericTaskResultStructName(info.Name)

	return CType{
		Name:     name,
		SealName: name,
	}
}

func (g *Generator) importedGenericTaskResultStructName(instanceName string) string {
	return instanceName + "_Result"
}

func (g *Generator) importedGenericTaskSignature(info *ImportedGenericTaskInstance) string {
	if info == nil {
		return "/*invalid*/ int invalid_imported_generic(void)"
	}

	subst := genericArgSubstForCGen(info.Info.GenericParams, info.Args)
	ret := g.importedGenericTaskReturnType(info)

	var params []string

	for i, paramAst := range info.Info.ParamTypeAsts {
		paramType := g.cTypeFromAstWithGenericArgsInTypeContext(info.PackageName, paramAst, subst)

		name := fmt.Sprintf("arg%d", i)
		if i < len(info.Info.ParamNames) && info.Info.ParamNames[i] != "" {
			name = info.Info.ParamNames[i]
		}

		if i < len(info.Info.ParamIsVariadic) && info.Info.ParamIsVariadic[i] {
			if info.Info.IsExtern {
				params = append(params, "...")
				break
			}

			params = append(params, g.variadicCType(paramType).Decl(name))
			break
		}

		params = append(params, paramType.Decl(name))
	}

	if len(params) == 0 {
		return fmt.Sprintf("%s %s(void)", ret.Name, info.Name)
	}

	return fmt.Sprintf("%s %s(%s)", ret.Name, info.Name, strings.Join(params, ", "))
}

func (g *Generator) sealTypeKindFor(t CType) (string, bool) {
	spec, ok := builtin.LookupType(t.SealName)
	if !ok || spec.AnyKind == "" {
		return "", false
	}

	return spec.AnyKind, true
}

func (g *Generator) sealAnyFieldFor(t CType) (string, bool) {
	if t.SealName == "any" {
		return "", true
	}

	spec, ok := builtin.LookupType(t.SealName)
	if !ok || spec.AnyField == "" {
		return "", false
	}

	return spec.AnyField, true
}

func (g *Generator) emitLenCall(
	e *ast.CallExpr,
	preparedArgs []string,
) string {
	resolution, ok := g.lenResolutions[e]
	if !ok {
		g.error(
			e.Span(),
			"missing checker resolution for len call",
		)
		return "0"
	}

	if len(e.Args) != 1 {
		g.error(
			e.Span(),
			"len expects 1 argument",
		)
		return "0"
	}

	if resolution.Candidate != nil {
		name, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return "0"
		}

		return g.emitSemanticTaskCall(
			name,
			info,
			e.Args,
			preparedArgs,
		)
	}

	argType := g.inferExprType(e.Args[0], nil)

	arg := ""
	if len(preparedArgs) > 0 {
		arg = preparedArgs[0]
	} else {
		arg = g.emitExpr(e.Args[0], nil)
	}

	switch {
	case argType.SealName == "string":
		return fmt.Sprintf(
			"seal_string_scalar_len(%s)",
			arg,
		)

	case argType.SealName == "cstring":
		return fmt.Sprintf(
			"seal_cstring_scalar_len(%s)",
			arg,
		)

	case argType.IsVariadic:
		return fmt.Sprintf(
			"((uintptr_t)(%s).len)",
			arg,
		)
	}

	g.error(
		e.Args[0].Span(),
		fmt.Sprintf(
			"checker selected builtin len for unsupported type %s",
			argType.String(),
		),
	)

	return "0"
}

func (g *Generator) emitDirectCNamedCall(name string, args []ast.Expr, preparedArgs []string, expectedParams []CType) string {
	var outArgs []string

	for i, arg := range args {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := (*CType)(nil)
		if i < len(expectedParams) {
			expected = &expectedParams[i]
		}

		outArgs = append(outArgs, g.emitExpr(arg, expected))
	}

	return fmt.Sprintf("%s(%s)", name, strings.Join(outArgs, ", "))
}

func (g *Generator) emitTaskCall(taskName string, args []ast.Expr, preparedArgs []string) string {
	info, hasTask := g.tasks[taskName]

	name := g.cTaskName(taskName)
	if hasTask && info.IsExtern && info.ExternName != "" {
		name = info.ExternName
	}

	if hasTask && info.IsVariadic && !info.IsExtern {
		return g.emitSealVariadicTaskCall(name, info, args, preparedArgs)
	}

	var outArgs []string

	for i, arg := range args {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := (*CType)(nil)
		if hasTask && i < len(info.ParamTypes) {
			if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
				expected = nil
			} else {
				expected = &info.ParamTypes[i]
			}
		}

		outArgs = append(outArgs, g.emitExpr(arg, expected))
	}

	if hasTask && !info.IsVariadic {
		for i := len(args); i < len(info.ParamTypes); i++ {
			if i < len(info.ParamHasDefault) && info.ParamHasDefault[i] {
				expected := info.ParamTypes[i]
				outArgs = append(outArgs, g.emitExpr(info.ParamDefaults[i], &expected))
			}
		}
	}

	return fmt.Sprintf("%s(%s)", name, strings.Join(outArgs, ", "))
}

func (g *Generator) emitSealVariadicTaskCall(name string, info TaskInfo, args []ast.Expr, preparedArgs []string) string {
	total := len(info.ParamTypes)
	fixedCount := total - 1

	var outArgs []string

	for i := 0; i < fixedCount && i < len(args); i++ {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := info.ParamTypes[i]
		outArgs = append(outArgs, g.emitExpr(args[i], &expected))
	}

	elemType := CInvalid
	if total > 0 {
		elemType = info.ParamTypes[total-1]
	}

	var rest []ast.Expr
	if len(args) > fixedCount {
		rest = args[fixedCount:]
	}

	if len(rest) == 1 {
		if spread, ok := rest[0].(*ast.SpreadExpr); ok {
			outArgs = append(outArgs, g.emitSpreadAsVariadic(elemType, spread))
			return fmt.Sprintf("%s(%s)", name, strings.Join(outArgs, ", "))
		}
	}

	outArgs = append(outArgs, g.emitVariadicLiteral(elemType, rest, preparedArgs, fixedCount))

	return fmt.Sprintf("%s(%s)", name, strings.Join(outArgs, ", "))
}

func (g *Generator) emitMultiVarDeclStmt(s *ast.MultiVarDeclStmt) {
	call, ok := s.Value.(*ast.CallExpr)
	if !ok {
		g.error(s.Value.Span(), "multi-value declaration requires a task call")
		return
	}

	resultTypes := g.callReturnTypes(call)

	if len(resultTypes) != len(s.Names) {
		g.error(
			s.Span(),
			fmt.Sprintf("multi-value declaration mismatch: expected %d name(s), got %d result value(s)", len(s.Names), len(resultTypes)),
		)
	}

	resultType := g.inferExprType(call, nil)
	resultTemp := g.newTemp("multi_result")

	g.linef("%s = %s;", resultType.Decl(resultTemp), g.emitExpr(call, &resultType))

	count := len(s.Names)
	if len(resultTypes) < count {
		count = len(resultTypes)
	}

	for i := 0; i < count; i++ {
		name := s.Names[i]
		if name.Name == "_" {
			continue
		}

		itemType := resultTypes[i]
		g.scope.declare(name.Name, itemType)
		g.linef("%s = %s._%d;", itemType.Decl(name.Name), resultTemp, i)
	}
}

func (g *Generator) emitSpreadAsVariadic(
	elem CType,
	spread *ast.SpreadExpr,
) string {
	variadicType := g.variadicCType(elem)
	srcType := g.inferExprType(
		spread.Expr,
		nil,
	)

	if srcType.IsVariadic {
		if srcType.Elem == nil {
			g.error(
				spread.Span(),
				"cannot spread invalid variadic value",
			)

			return fmt.Sprintf(
				"(%s){.data = NULL, .len = 0}",
				variadicType.Name,
			)
		}

		if srcType.Elem.SealName != elem.SealName {
			g.error(
				spread.Span(),
				fmt.Sprintf(
					"cannot spread %s into ...%s",
					srcType.String(),
					elem.SealName,
				),
			)

			return fmt.Sprintf(
				"(%s){.data = NULL, .len = 0}",
				variadicType.Name,
			)
		}

		return g.emitExpr(spread.Expr, nil)
	}

	g.error(
		spread.Span(),
		fmt.Sprintf(
			"cannot spread %s; expected variadic value",
			srcType.String(),
		),
	)

	return fmt.Sprintf(
		"(%s){.data = NULL, .len = 0}",
		variadicType.Name,
	)
}

func (g *Generator) emitAnyRuntimeSupport() {
	anyTypes := builtin.AnyTypes()

	g.line("typedef enum sealTypeKind {")
	g.indent++
	g.line("sealType_invalid = 0,")

	for i, typ := range anyTypes {
		comma := ","
		if i == len(anyTypes)-1 {
			comma = ""
		}

		g.linef("%s%s", typ.AnyKind, comma)
	}

	g.indent--
	g.line("} sealTypeKind;")
	g.line("")

	g.line("typedef struct sealAny {")
	g.indent++
	g.line("sealTypeKind type;")
	g.line("union {")
	g.indent++

	for _, typ := range anyTypes {
		if typ.Name == "any" {
			continue
		}

		if typ.AnyField == "" {
			continue
		}

		g.linef("%s %s;", typ.CName, typ.AnyField)
	}

	g.indent--
	g.line("} value;")
	g.indent--
	g.line("} sealAny;")
	g.line("")

	for _, typ := range anyTypes {
		if typ.Name == "any" {
			g.line("static inline sealAny sealAny_any(sealAny value) { return value; }")
			continue
		}

		if typ.AnyField == "" {
			continue
		}

		g.linef(
			"static inline sealAny %s(%s value) { sealAny out; out.type = %s; out.value.%s = value; return out; }",
			typ.AnyCtor,
			typ.CName,
			typ.AnyKind,
			typ.AnyField,
		)
	}

	g.line("")
}

func (g *Generator) emitVariadicLiteral(elem CType, args []ast.Expr, preparedArgs []string, preparedOffset int) string {
	variadicType := g.variadicCType(elem)

	if len(args) == 0 {
		return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
	}

	var values []string

	for i, arg := range args {
		globalIndex := preparedOffset + i

		if preparedArgs != nil && globalIndex < len(preparedArgs) {
			values = append(values, preparedArgs[globalIndex])
			continue
		}
		values = append(values, g.emitExpr(arg, &elem))
	}

	return fmt.Sprintf(
		"(%s){.data = (%s[]){%s}, .len = %d}",
		variadicType.Name,
		elem.Name,
		strings.Join(values, ", "),
		len(values),
	)
}

func (g *Generator) emitDeferStmt(
	s *ast.DeferStmt,
) {
	if s == nil {
		return
	}

	if s.Body != nil {
		if s.Call != nil {
			g.error(
				s.Span(),
				"defer cannot contain both a call and a block",
			)
			return
		}

		g.scope.addDeferredBlock(
			s.Body,
		)
		return
	}

	if s.Call == nil {
		g.error(
			s.Span(),
			"defer requires a task call or a block",
		)
		return
	}

	call, ok := s.Call.(*ast.CallExpr)
	if !ok {
		g.error(
			s.Span(),
			"call-form defer requires a task call",
		)
		return
	}

	code := g.emitDeferredCall(call)

	g.scope.addDeferredCall(
		code,
	)
}

func (g *Generator) emitDeferredCall(call *ast.CallExpr) string {
	preparedArgs := make([]string, 0, len(call.Args))

	for _, arg := range call.Args {
		argType := g.inferExprType(arg, nil)
		temp := g.newTemp("defer_arg")

		g.linef("%s = %s;", argType.Decl(temp), g.emitExpr(arg, &argType))
		preparedArgs = append(preparedArgs, temp)
	}

	return g.emitCallExprWithArgs(call, preparedArgs)
}

func (g *Generator) emitUnions(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.UnionDecl)
		if !ok {
			continue
		}

		if d.Raw {
			g.error(d.Name.Span(), "@rawUnion is not supported by this C codegen phase yet")
			continue
		}

		g.linef("typedef enum %s_Tag {", d.Name.Name)
		g.indent++
		g.linef("%s_Tag_nil = 0,", d.Name.Name)

		for i, member := range d.Members {
			memberType := g.cTypeFromAst(member)
			comma := ","
			if i == len(d.Members)-1 {
				comma = ""
			}

			g.linef("%s_Tag_%s%s", d.Name.Name, memberType.SealName, comma)
		}

		g.indent--
		g.linef("} %s_Tag;", d.Name.Name)
		g.line("")

		g.linef("typedef struct %s {", d.Name.Name)
		g.indent++
		g.linef("%s_Tag tag;", d.Name.Name)
		g.line("union {")
		g.indent++

		for _, member := range d.Members {
			memberType := g.cTypeFromAst(member)
			g.linef("%s;", memberType.Decl(memberType.SealName))
		}

		g.indent--
		g.line("} as;")
		g.indent--
		g.linef("} %s;", d.Name.Name)
		g.line("")
	}
}

func (g *Generator) emitPackageTaskCall(
	packageName string,
	taskName string,
	args []ast.Expr,
	preparedArgs []string,
) string {
	pkg := g.packages[packageName]
	if pkg == nil {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"unknown package %q",
				packageName,
			),
		)
		return "0"
	}

	info, hasTask := pkg.Tasks[taskName]
	if !hasTask {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"package %s has no task %q",
				packageName,
				taskName,
			),
		)
		return "0"
	}

	info = g.taskInfoInPackageContext(
		packageName,
		taskName,
		info,
	)

	name := cImportedTaskName(
		packageName,
		taskName,
		info,
	)

	if info.IsVariadic && !info.IsExtern {
		return g.emitSealVariadicTaskCall(
			name,
			info,
			args,
			preparedArgs,
		)
	}

	var outArgs []string

	for i, arg := range args {
		if preparedArgs != nil &&
			i < len(preparedArgs) {
			outArgs = append(
				outArgs,
				preparedArgs[i],
			)
			continue
		}

		expected := (*CType)(nil)

		if i < len(info.ParamTypes) {
			if i < len(info.ParamIsVariadic) &&
				info.ParamIsVariadic[i] {
				expected = nil
			} else {
				expected = &info.ParamTypes[i]
			}
		}

		outArgs = append(
			outArgs,
			g.emitExpr(arg, expected),
		)
	}

	if !info.IsVariadic {
		for i := len(args); i < len(info.ParamTypes); i++ {
			if i >= len(info.ParamHasDefault) ||
				!info.ParamHasDefault[i] {
				continue
			}

			if i >= len(info.ParamDefaults) ||
				info.ParamDefaults[i] == nil {
				g.error(
					argsSpan(args),
					fmt.Sprintf(
						"imported task %s.%s is missing default argument %d",
						packageName,
						taskName,
						i+1,
					),
				)
				continue
			}

			expected := info.ParamTypes[i]
			value := ""

			// The default expression belongs to the declaring package.
			g.withTypeContext(
				packageName,
				func() {
					value = g.emitExpr(
						info.ParamDefaults[i],
						&expected,
					)
				},
			)

			outArgs = append(
				outArgs,
				value,
			)
		}
	}

	return fmt.Sprintf(
		"%s(%s)",
		name,
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) emitCompoundLiteral(e *ast.CompoundLiteralExpr, typ CType) string {
	if _, ok := g.distincts[typ.SealName]; ok {
		g.error(e.Span(), fmt.Sprintf("distinct type %s cannot be constructed with a literal; use cast<%s>(value)", typ.SealName, typ.SealName))
		return "0"
	}

	var values []string

	for _, field := range e.Fields {
		fieldType := g.lookupStructFieldType(typ.SealName, field.Name.Name)
		values = append(values, fmt.Sprintf(".%s = %s", field.Name.Name, g.emitExpr(field.Value, &fieldType)))
	}

	for _, value := range e.Values {
		values = append(values, g.emitExpr(value, nil))
	}

	return fmt.Sprintf("(%s){%s}", typ.Name, strings.Join(values, ", "))
}

func (g *Generator) emitTaskVariadicRuntimeTypes() {
	names := make([]string, 0, len(g.tasks))
	for name := range g.tasks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := g.tasks[name]

		for i, paramType := range info.ParamTypes {
			if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
				g.emitVariadicRuntimeType(paramType)
			}
		}
	}

	if len(names) > 0 {
		g.line("")
	}
}

func (g *Generator) emitRuntimeSupport() {
	g.line("typedef struct sealString {")
	g.indent++
	g.line("const uint8_t *data;")
	g.line("uintptr_t len;")
	g.indent--
	g.line("} sealString;")
	g.line("")

	g.emitAnyRuntimeSupport()

	g.line("static inline void seal_trap(void) {")
	g.indent++
	g.line("#if defined(__GNUC__) || defined(__clang__)")
	g.line("__builtin_trap();")
	g.line("#else")
	g.line("abort();")
	g.line("#endif")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_unreachable(void) {")
	g.indent++
	g.line("#if defined(__GNUC__) || defined(__clang__)")
	g.line("__builtin_unreachable();")
	g.line("#else")
	g.line("abort();")
	g.line("#endif")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_empty(void) {")
	g.indent++
	g.line(`fprintf(stderr, "panic\n");`)
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_cstring(const char *message) {")
	g.indent++
	g.line(`fprintf(stderr, "panic: %s\n", message ? message : "<null>");`)
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_string(sealString message) {")
	g.indent++
	g.line(`fputs("panic: ", stderr);`)
	g.line("if (message.len != 0) {")
	g.indent++
	g.line("if (message.data == NULL) {")
	g.indent++
	g.line(`fputs("<invalid string>", stderr);`)
	g.indent--
	g.line("} else {")
	g.indent++
	g.line("fwrite(message.data, 1, (size_t)message.len, stderr);")
	g.indent--
	g.line("}")
	g.indent--
	g.line("}")
	g.line(`fputc('\n', stderr);`)
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_utf8_fail(void) {")
	g.indent++
	g.line(`seal_panic_cstring("invalid UTF-8 or string index out of bounds");`)
	g.line("return 0;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_cstring_byte_len(const char *value) {")
	g.indent++
	g.line("if (value == NULL) {")
	g.indent++
	g.line("return 0;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("const uint8_t *bytes = (const uint8_t *)value;")
	g.line("uintptr_t len = 0;")
	g.line("")
	g.line("while (bytes[len] != 0) {")
	g.indent++
	g.line("len++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return len;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline const char *seal_cstring_from_parts(")
	g.indent++
	g.line("const char *data,")
	g.line("uintptr_t byte_len")
	g.indent--
	g.line(") {")
	g.indent++

	g.line("if (data == NULL) {")
	g.indent++
	g.line(`seal_panic_cstring("cannot construct cstring from a null pointer");`)
	g.line("return NULL;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (((const uint8_t *)data)[byte_len] != 0) {")
	g.indent++
	g.line(`seal_panic_cstring("cstring data is not null-terminated at the supplied byte length");`)
	g.line("return NULL;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("return data;")

	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline bool seal_utf8_is_continuation(uint8_t byte) {")
	g.indent++
	g.line("return (byte & 0xC0u) == 0x80u;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_utf8_decode_one(")
	g.indent++
	g.line("const uint8_t *data,")
	g.line("uintptr_t byte_len,")
	g.line("uintptr_t *offset")
	g.indent--
	g.line(") {")
	g.indent++

	g.line("if (data == NULL || offset == NULL || *offset >= byte_len) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("uintptr_t i = *offset;")
	g.line("uint8_t b0 = data[i];")
	g.line("")

	g.line("if (b0 <= 0x7Fu) {")
	g.indent++
	g.line("*offset = i + 1;")
	g.line("return (uint32_t)b0;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (b0 >= 0xC2u && b0 <= 0xDFu) {")
	g.indent++
	g.line("if (byte_len - i < 2) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("uint8_t b1 = data[i + 1];")
	g.line("")
	g.line("if (!seal_utf8_is_continuation(b1)) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("*offset = i + 2;")
	g.line("")
	g.line("return ((uint32_t)(b0 & 0x1Fu) << 6) |")
	g.indent++
	g.line("(uint32_t)(b1 & 0x3Fu);")
	g.indent--
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (b0 >= 0xE0u && b0 <= 0xEFu) {")
	g.indent++
	g.line("if (byte_len - i < 3) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("uint8_t b1 = data[i + 1];")
	g.line("uint8_t b2 = data[i + 2];")
	g.line("")
	g.line("if (!seal_utf8_is_continuation(b1) ||")
	g.indent++
	g.line("!seal_utf8_is_continuation(b2)) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.indent--
	g.line("")
	g.line("if (b0 == 0xE0u && b1 < 0xA0u) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (b0 == 0xEDu && b1 > 0x9Fu) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("*offset = i + 3;")
	g.line("")
	g.line("return ((uint32_t)(b0 & 0x0Fu) << 12) |")
	g.indent++
	g.line("((uint32_t)(b1 & 0x3Fu) << 6) |")
	g.line("(uint32_t)(b2 & 0x3Fu);")
	g.indent--
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (b0 >= 0xF0u && b0 <= 0xF4u) {")
	g.indent++
	g.line("if (byte_len - i < 4) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("uint8_t b1 = data[i + 1];")
	g.line("uint8_t b2 = data[i + 2];")
	g.line("uint8_t b3 = data[i + 3];")
	g.line("")
	g.line("if (!seal_utf8_is_continuation(b1) ||")
	g.indent++
	g.line("!seal_utf8_is_continuation(b2) ||")
	g.line("!seal_utf8_is_continuation(b3)) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.indent--
	g.line("")
	g.line("if (b0 == 0xF0u && b1 < 0x90u) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (b0 == 0xF4u && b1 > 0x8Fu) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("*offset = i + 4;")
	g.line("")
	g.line("return ((uint32_t)(b0 & 0x07u) << 18) |")
	g.indent++
	g.line("((uint32_t)(b1 & 0x3Fu) << 12) |")
	g.line("((uint32_t)(b2 & 0x3Fu) << 6) |")
	g.line("(uint32_t)(b3 & 0x3Fu);")
	g.indent--
	g.indent--
	g.line("}")
	g.line("")

	g.line("return seal_utf8_fail();")

	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_utf8_scalar_count(")
	g.indent++
	g.line("const uint8_t *data,")
	g.line("uintptr_t byte_len")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("uintptr_t offset = 0;")
	g.line("uintptr_t count = 0;")
	g.line("")
	g.line("while (offset < byte_len) {")
	g.indent++
	g.line("(void)seal_utf8_decode_one(data, byte_len, &offset);")
	g.line("count++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return count;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_string_scalar_len(sealString value) {")
	g.indent++
	g.line("return seal_utf8_scalar_count(value.data, value.len);")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_cstring_scalar_len(const char *value) {")
	g.indent++
	g.line("uintptr_t byte_len = seal_cstring_byte_len(value);")
	g.line("")
	g.line("return seal_utf8_scalar_count(")
	g.indent++
	g.line("(const uint8_t *)value,")
	g.line("byte_len")
	g.indent--
	g.line(");")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline intptr_t seal_utf8_normalize_index(")
	g.indent++
	g.line("intptr_t index,")
	g.line("uintptr_t length")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("if (length > (uintptr_t)INTPTR_MAX) {")
	g.indent++
	g.line("return (intptr_t)seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("intptr_t normalized = index;")
	g.line("")
	g.line("if (normalized < 0) {")
	g.indent++
	g.line("normalized += (intptr_t)length;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (normalized < 0 ||")
	g.indent++
	g.line("(uintptr_t)normalized >= length) {")
	g.indent++
	g.line("return (intptr_t)seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.indent--
	g.line("")
	g.line("return normalized;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_string_index(")
	g.indent++
	g.line("sealString value,")
	g.line("intptr_t index")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("uintptr_t scalar_len = seal_string_scalar_len(value);")
	g.line("intptr_t normalized =")
	g.indent++
	g.line("seal_utf8_normalize_index(index, scalar_len);")
	g.indent--
	g.line("")
	g.line("uintptr_t offset = 0;")
	g.line("uintptr_t current = 0;")
	g.line("")
	g.line("while (offset < value.len) {")
	g.indent++
	g.line("uint32_t scalar = seal_utf8_decode_one(")
	g.indent++
	g.line("value.data,")
	g.line("value.len,")
	g.line("&offset")
	g.indent--
	g.line(");")
	g.line("")
	g.line("if (current == (uintptr_t)normalized) {")
	g.indent++
	g.line("return scalar;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("current++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_cstring_index(")
	g.indent++
	g.line("const char *value,")
	g.line("intptr_t index")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("uintptr_t byte_len = seal_cstring_byte_len(value);")
	g.line("uintptr_t scalar_len = seal_utf8_scalar_count(")
	g.indent++
	g.line("(const uint8_t *)value,")
	g.line("byte_len")
	g.indent--
	g.line(");")
	g.line("")
	g.line("intptr_t normalized =")
	g.indent++
	g.line("seal_utf8_normalize_index(index, scalar_len);")
	g.indent--
	g.line("")
	g.line("uintptr_t offset = 0;")
	g.line("uintptr_t current = 0;")
	g.line("")
	g.line("while (offset < byte_len) {")
	g.indent++
	g.line("uint32_t scalar = seal_utf8_decode_one(")
	g.indent++
	g.line("(const uint8_t *)value,")
	g.line("byte_len,")
	g.line("&offset")
	g.indent--
	g.line(");")
	g.line("")
	g.line("if (current == (uintptr_t)normalized) {")
	g.indent++
	g.line("return scalar;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("current++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline bool seal_string_equal(")
	g.indent++
	g.line("sealString left,")
	g.line("sealString right")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("if (left.len != right.len) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (left.len == 0) {")
	g.indent++
	g.line("return true;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (left.data == NULL || right.data == NULL) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("for (uintptr_t i = 0; i < left.len; i++) {")
	g.indent++
	g.line("if (left.data[i] != right.data[i]) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return true;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline bool seal_cstring_equal(")
	g.indent++
	g.line("const char *left,")
	g.line("const char *right")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("if (left == right) {")
	g.indent++
	g.line("return true;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (left == NULL || right == NULL) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("const uint8_t *a = (const uint8_t *)left;")
	g.line("const uint8_t *b = (const uint8_t *)right;")
	g.line("uintptr_t i = 0;")
	g.line("")
	g.line("while (a[i] != 0 && b[i] != 0) {")
	g.indent++
	g.line("if (a[i] != b[i]) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("i++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return a[i] == b[i];")
	g.indent--
	g.line("}")
	g.line("")

	for _, typ := range builtin.Types {
		cType := CType{
			Name:     typ.CName,
			SealName: typ.Name,
		}

		g.emitVariadicRuntimeType(cType)
	}

	g.line("")
}

func (g *Generator) emitVariadicRuntimeType(
	elem CType,
) {
	variadicType := g.variadicCType(elem)
	name := variadicType.Name

	if g.emittedVariadics[name] {
		return
	}

	g.emittedVariadics[name] = true

	g.linef("typedef struct %s {", name)
	g.indent++
	g.linef("%s *data;", elem.Name)
	g.line("size_t len;")
	g.indent--
	g.linef("} %s;", name)
}

func (g *Generator) variadicCType(elem CType) CType {
	elemName := elem.SealName

	name := "sealVariadic_" + sanitizeCName(elemName)

	return CType{
		Name:       name,
		SealName:   "..." + elem.SealName,
		IsVariadic: true,
		Elem:       &elem,
	}
}

func (g *Generator) emitCImports(file *ast.File) {
	emitted := false

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.DirectiveDecl)
		if !ok {
			continue
		}

		if d.Directive.Name != "c_import" {
			continue
		}

		for i := 0; i < len(d.Body); i++ {
			tok := d.Body[i]

			if tok.Kind == token.Ident && tok.Lexeme == "include" {
				if i+1 >= len(d.Body) || d.Body[i+1].Kind != token.StringLit {
					g.error(tok.Span, "expected string literal after include in @c_import")
					continue
				}

				g.linef("#include %s", d.Body[i+1].Lexeme)
				emitted = true
				i++
				continue
			}

			if tok.Kind == token.Ident {
				g.error(tok.Span, fmt.Sprintf("unsupported @c_import directive item %q", tok.Lexeme))
			}
		}
	}

	if emitted {
		g.line("")
	}
}

func argsSpan(args []ast.Expr) source.Span {
	if len(args) == 0 {
		return source.Span{}
	}

	return args[0].Span()
}

func (g *Generator) callReturnTypes(
	expr ast.Expr,
) []CType {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return []CType{
			g.inferExprType(expr, nil),
		}
	}

	if resolution, ok :=
		g.lenResolutions[call]; ok {
		if resolution.Candidate == nil {
			return []CType{CUint}
		}

		_, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			call.Span(),
		)
		if !ok {
			return []CType{CInvalid}
		}

		return info.ReturnTypes
	}

	if id, ok :=
		call.Callee.(*ast.IdentExpr); ok {
		if g.scope == nil {
			if results, ok :=
				g.genericTaskParamReturnTypes(
					id.Name.Name,
				); ok {
				return results
			}
		} else if _, isLocal :=
			g.scope.lookup(id.Name.Name); !isLocal {
			if results, ok :=
				g.genericTaskParamReturnTypes(
					id.Name.Name,
				); ok {
				return results
			}
		}
	}

	if gen, ok :=
		call.Callee.(*ast.GenericExpr); ok {
		if resolution, exists :=
			g.genericOverloadCalls[gen]; exists {
			if resolution.Candidate == nil {
				return []CType{CInvalid}
			}

			_, info, selected :=
				g.semanticTaskSelection(
					resolution.Candidate,
					resolution.PackageName,
					resolution.GenericArguments,
					call.Span(),
				)

			if !selected {
				return []CType{CInvalid}
			}

			return append(
				[]CType(nil),
				info.ReturnTypes...,
			)
		}
		if id, ok :=
			gen.Base.(*ast.IdentExpr); ok {
			if task, ok :=
				builtin.LookupTask(
					id.Name.Name,
				); ok && task.Generic {
				switch task.Kind {
				case builtin.TaskAnyIs:
					return []CType{CBool}

				case builtin.TaskAnyAs,
					builtin.TaskCast:
					if len(gen.Args) == 1 {
						arg :=
							gen.Args[0]

						if g.genericSubst != nil {
							arg =
								g.substituteGenericArgForCGen(
									arg,
									g.genericSubst,
								)
						}

						return []CType{
							g.cTypeFromGenericArg(arg),
						}
					}
				}

				return []CType{CInvalid}
			}

			if packageName,
				taskName,
				info,
				found :=
				g.importedGenericTaskInfoFromTypeContext(
					id.Name.Name,
				); found {
				callArgs :=
					g.genericArgsInContext(
						gen.Args,
					)

				name :=
					g.registerImportedGenericTaskInstance(
						packageName,
						taskName,
						info,
						callArgs,
					)

				instance :=
					g.importedGenericTasks[name]

				if instance == nil {
					return []CType{CInvalid}
				}

				return g.importedGenericTaskReturnTypes(
					instance,
				)
			}

			info, ok := g.tasks[id.Name.Name]
			if ok &&
				len(info.GenericParams) > 0 &&
				info.Decl != nil {
				callArgs :=
					g.genericArgsInContext(
						gen.Args,
					)

				name :=
					g.registerGenericTaskInstance(
						info.Decl,
						callArgs,
					)

				instance := g.genericTasks[name]
				if instance == nil {
					return []CType{CInvalid}
				}

				return g.genericTaskReturnTypes(
					instance,
				)
			}
		}

		if selector, ok :=
			gen.Base.(*ast.SelectorExpr); ok {
			pkgName,
				taskName,
				info,
				ok :=
				g.importedGenericTaskInfoFromSelector(
					selector,
				)

			if ok {
				callArgs :=
					g.genericArgsInContext(
						gen.Args,
					)

				name :=
					g.registerImportedGenericTaskInstance(
						pkgName,
						taskName,
						info,
						callArgs,
					)

				instance :=
					g.importedGenericTasks[name]

				if instance == nil {
					return []CType{CInvalid}
				}

				return g.importedGenericTaskReturnTypes(
					instance,
				)
			}
		}
	}

	if id, ok :=
		call.Callee.(*ast.IdentExpr); ok {
		if _, info, found :=
			g.importedTaskInfoFromTypeContext(
				id.Name.Name,
			); found {
			return info.ReturnTypes
		}

		if len(call.Args) > 0 {
			firstType :=
				g.inferExprType(
					call.Args[0],
					nil,
				)

			if g.isInterfaceCType(firstType) {
				if instance,
					req,
					ok :=
					g.lookupInterfaceRequirement(
						firstType,
						id.Name.Name,
					); ok {
					return g.interfaceRequirementResultTypes(
						instance,
						req,
					)
				}
			}
		}

		if info, ok :=
			g.tasks[id.Name.Name]; ok {
			return info.ReturnTypes
		}

		if _, ok :=
			g.overloads[id.Name.Name]; ok {
			argTypes := make(
				[]CType,
				0,
				len(call.Args),
			)

			for _, arg := range call.Args {
				argTypes = append(
					argTypes,
					g.inferExprType(
						arg,
						nil,
					),
				)
			}

			candidate, ok :=
				g.resolveOverload(
					id.Name.Name,
					argTypes,
				)

			if !ok {
				return []CType{CInvalid}
			}

			return g.tasks[candidate].ReturnTypes
		}

		if kind, ok :=
			g.primitiveTaskKind(
				id.Name.Name,
			); ok {
			switch kind {
			case builtin.TaskSize:
				return []CType{CUint}

			case builtin.TaskAssert,
				builtin.TaskPanic,
				builtin.TaskTrap,
				builtin.TaskUnreachable:
				return []CType{CVoid}

			case builtin.TaskLen:
				return []CType{CInvalid}
			}
		}
	}

	if selector, ok :=
		call.Callee.(*ast.SelectorExpr); ok {
		if id, ok :=
			selector.Left.(*ast.IdentExpr); ok {
			packageName := id.Name.Name

			if pkg :=
				g.packages[packageName]; pkg != nil {
				if info, ok :=
					pkg.Tasks[selector.Name.Name]; ok {
					info =
						g.taskInfoInPackageContext(
							packageName,
							selector.Name.Name,
							info,
						)

					return append(
						[]CType(nil),
						info.ReturnTypes...,
					)
				}
			}
		}
	}

	return []CType{
		g.inferExprType(expr, nil),
	}
}

func (g *Generator) inferCallExprType(
	e *ast.CallExpr,
) CType {
	if resolution, ok :=
		g.lenResolutions[e]; ok {
		if resolution.Candidate == nil {
			return CUint
		}

		_, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return CInvalid
		}

		return info.ReturnType
	}

	if id, ok :=
		e.Callee.(*ast.IdentExpr); ok {
		if g.scope == nil {
			if ret, ok :=
				g.genericTaskParamReturnType(
					id.Name.Name,
				); ok {
				return ret
			}
		} else if _, isLocal :=
			g.scope.lookup(id.Name.Name); !isLocal {
			if ret, ok :=
				g.genericTaskParamReturnType(
					id.Name.Name,
				); ok {
				return ret
			}
		}
	}

	if gen, ok :=
		e.Callee.(*ast.GenericExpr); ok {
		if resolution, exists :=
			g.genericOverloadCalls[gen]; exists {
			if resolution.Candidate == nil {
				return CInvalid
			}

			_, info, selected :=
				g.semanticTaskSelection(
					resolution.Candidate,
					resolution.PackageName,
					resolution.GenericArguments,
					e.Span(),
				)

			if !selected {
				return CInvalid
			}

			return info.ReturnType
		}
		if id, ok :=
			gen.Base.(*ast.IdentExpr); ok {
			if task, ok :=
				builtin.LookupTask(
					id.Name.Name,
				); ok && task.Generic {
				switch task.Kind {
				case builtin.TaskAnyIs:
					return CBool

				case builtin.TaskAnyAs,
					builtin.TaskCast:
					if len(gen.Args) == 1 {
						targetArg := gen.Args[0]

						if g.genericSubst != nil {
							targetArg =
								g.substituteGenericArgForCGen(
									targetArg,
									g.genericSubst,
								)
						}

						return g.cTypeFromGenericArg(
							targetArg,
						)
					}
				}

				return CInvalid
			}

			if packageName,
				taskName,
				info,
				found :=
				g.importedGenericTaskInfoFromTypeContext(
					id.Name.Name,
				); found {
				callArgs :=
					g.genericArgsInContext(
						gen.Args,
					)

				name :=
					g.registerImportedGenericTaskInstance(
						packageName,
						taskName,
						info,
						callArgs,
					)

				instance :=
					g.importedGenericTasks[name]

				if instance == nil {
					return CInvalid
				}

				return g.importedGenericTaskReturnType(
					instance,
				)
			}

			info, ok := g.tasks[id.Name.Name]
			if !ok ||
				len(info.GenericParams) == 0 ||
				info.Decl == nil {
				return CInvalid
			}

			callArgs :=
				g.genericArgsInContext(
					gen.Args,
				)

			name :=
				g.registerGenericTaskInstance(
					info.Decl,
					callArgs,
				)

			instance := g.genericTasks[name]
			if instance == nil {
				return CInvalid
			}

			return g.genericTaskReturnType(instance)
		}

		if selector, ok :=
			gen.Base.(*ast.SelectorExpr); ok {
			pkgName,
				taskName,
				info,
				ok :=
				g.importedGenericTaskInfoFromSelector(
					selector,
				)

			if !ok {
				return CInvalid
			}

			callArgs :=
				g.genericArgsInContext(
					gen.Args,
				)

			name :=
				g.registerImportedGenericTaskInstance(
					pkgName,
					taskName,
					info,
					callArgs,
				)

			instance :=
				g.importedGenericTasks[name]

			if instance == nil {
				return CInvalid
			}

			return g.importedGenericTaskReturnType(
				instance,
			)
		}

		return CInvalid
	}

	if id, ok :=
		e.Callee.(*ast.IdentExpr); ok {
		if _, info, found :=
			g.importedTaskInfoFromTypeContext(
				id.Name.Name,
			); found {
			return info.ReturnType
		}

		if len(e.Args) > 0 {
			firstType :=
				g.inferExprType(
					e.Args[0],
					nil,
				)

			if g.isInterfaceCType(firstType) {
				if instance,
					req,
					ok :=
					g.lookupInterfaceRequirement(
						firstType,
						id.Name.Name,
					); ok {
					return g.interfaceRequirementReturnType(
						instance,
						req,
					)
				}
			}
		}

		if info, ok :=
			g.tasks[id.Name.Name]; ok {
			return info.ReturnType
		}

		if _, ok :=
			g.overloads[id.Name.Name]; ok {
			argTypes := make(
				[]CType,
				0,
				len(e.Args),
			)

			for _, arg := range e.Args {
				argTypes = append(
					argTypes,
					g.inferExprType(
						arg,
						nil,
					),
				)
			}

			candidate, ok :=
				g.resolveOverload(
					id.Name.Name,
					argTypes,
				)
			if !ok {
				return CInvalid
			}

			return g.tasks[candidate].ReturnType
		}

		if kind, ok :=
			g.primitiveTaskKind(
				id.Name.Name,
			); ok {
			switch kind {
			case builtin.TaskSize:
				return CUint

			case builtin.TaskAssert,
				builtin.TaskPanic,
				builtin.TaskTrap,
				builtin.TaskUnreachable:
				return CVoid

			case builtin.TaskLen:
				return CInvalid
			}
		}
	}

	if selector, ok :=
		e.Callee.(*ast.SelectorExpr); ok {
		if id, ok :=
			selector.Left.(*ast.IdentExpr); ok {
			packageName := id.Name.Name

			if pkg :=
				g.packages[packageName]; pkg != nil {
				if info, ok :=
					pkg.Tasks[selector.Name.Name]; ok {
					info =
						g.taskInfoInPackageContext(
							packageName,
							selector.Name.Name,
							info,
						)

					return info.ReturnType
				}
			}
		}
	}

	return CInvalid
}

func (g *Generator) inferExprType(
	expr ast.Expr,
	expected *CType,
) CType {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope != nil {
			if value, ok :=
				g.scope.lookup(e.Name.Name); ok {
				return value.Type
			}
		}

		if g.genericSubst != nil {
			if arg, ok :=
				g.genericSubst[e.Name.Name]; ok &&
				arg.Kind == ast.GenericArgExpr &&
				arg.Expr != nil {
				if genericArgIsSingleNameForCGen(
					arg,
					e.Name.Name,
				) {
					return CInvalid
				}

				return g.inferExprType(
					arg.Expr,
					expected,
				)
			}
		}

		if typ, ok :=
			g.consts[e.Name.Name]; ok {
			return typ
		}

		if _, info, ok :=
			g.importedTaskInfoFromTypeContext(
				e.Name.Name,
			); ok {
			return info.ReturnType
		}

		if info, ok :=
			g.tasks[e.Name.Name]; ok {
			return info.ReturnType
		}

		return CInvalid

	case *ast.DotIdentExpr:
		if expected != nil {
			return *expected
		}

		return CInvalid

	case *ast.SpreadExpr:
		return g.inferExprType(
			e.Expr,
			expected,
		)

	case *ast.IntLitExpr:
		return CInt

	case *ast.GenericExpr:
		return CInvalid

	case *ast.FloatLitExpr:
		if expected != nil &&
			expected.SealName == "f32" {
			return CF32
		}

		return CF64

	case *ast.StringLitExpr:
		return CSealString

	case *ast.CStringLitExpr:
		return CCString

	case *ast.CharLitExpr:
		return CChar

	case *ast.BoolLitExpr:
		return CBool

	case *ast.NilLitExpr:
		if expected != nil {
			return *expected
		}

		return CNil

	case *ast.UnaryExpr:
		inner :=
			g.inferExprType(e.Expr, nil)

		switch e.Op {
		case token.Amp:
			return CType{
				Name:     inner.Name + " *",
				SealName: "*" + inner.SealName,
				Elem:     &inner,
			}

		case token.Star:
			if strings.HasPrefix(
				inner.SealName,
				"*",
			) {
				if inner.Elem != nil {
					return *inner.Elem
				}

				return CType{
					Name: strings.TrimSpace(
						strings.TrimSuffix(
							strings.TrimSpace(
								inner.Name,
							),
							"*",
						),
					),
					SealName: strings.TrimPrefix(
						inner.SealName,
						"*",
					),
				}
			}
		}

		return inner

	case *ast.BinaryExpr:
		left :=
			g.inferExprType(e.Left, nil)
		right :=
			g.inferExprType(e.Right, nil)

		if g.hasOperatorOverload(
			e.Op.String(),
		) {
			if candidate, ok :=
				g.resolveOverload(
					e.Op.String(),
					[]CType{left, right},
				); ok {
				return g.tasks[candidate].ReturnType
			}
		}

		if e.Op == token.NotEq &&
			g.hasOperatorOverload("==") {
			if _, ok :=
				g.resolveOverload(
					"==",
					[]CType{left, right},
				); ok {
				return CBool
			}
		}

		switch e.Op {
		case token.EqEq,
			token.NotEq,
			token.Lt,
			token.LtEq,
			token.Gt,
			token.GtEq,
			token.AndAnd,
			token.OrOr:
			return CBool
		}

		if left.SealName == "f64" ||
			right.SealName == "f64" {
			return CF64
		}

		if left.SealName == "f32" ||
			right.SealName == "f32" {
			return CF32
		}

		return left

	case *ast.CallExpr:
		return g.inferCallExprType(e)

	case *ast.SelectorExpr:
		if id, ok :=
			e.Left.(*ast.IdentExpr); ok {
			packageName := id.Name.Name

			if pkg :=
				g.packages[packageName]; pkg != nil {
				if info, ok :=
					pkg.Tasks[e.Name.Name]; ok {
					info =
						g.taskInfoInPackageContext(
							packageName,
							e.Name.Name,
							info,
						)

					return info.ReturnType
				}
			}
		}

		leftType :=
			g.inferExprType(e.Left, nil)

		if leftType.SealName == "string" ||
			leftType.SealName == "cstring" {
			return CInvalid
		}

		sealName := strings.TrimPrefix(
			leftType.SealName,
			"*",
		)

		return g.lookupStructFieldType(
			sealName,
			e.Name.Name,
		)

	case *ast.IndexExpr:
		return g.indexExprType(e)

	case *ast.CompoundLiteralExpr:
		return g.cTypeFromAstInContext(e.Type)
	}

	return CInvalid
}

func (g *Generator) resolveOverload(name string, argTypes []CType) (string, bool) {
	candidates := g.overloads[name]

	bestName := ""
	bestScore := 1 << 30
	ambiguous := false

	for _, candidate := range candidates {
		info, ok := g.tasks[candidate]
		if !ok {
			continue
		}

		score, ok := g.callScore(info, argTypes)
		if !ok {
			continue
		}

		if score < bestScore {
			bestName = candidate
			bestScore = score
			ambiguous = false
			continue
		}

		if score == bestScore {
			ambiguous = true
		}
	}

	if ambiguous {
		return "", false
	}

	if bestName == "" {
		return "", false
	}

	return bestName, true
}

func (g *Generator) callScore(info TaskInfo, argTypes []CType) (int, bool) {
	required := info.RequiredParams
	total := len(info.ParamTypes)

	if info.IsVariadic {
		fixedCount := total - 1

		if len(argTypes) < required {
			return 0, false
		}

		score := 0

		count := len(argTypes)
		if count > fixedCount {
			count = fixedCount
		}

		for i, argType := range argTypes[:count] {
			itemScore, ok := g.conversionScore(info.ParamTypes[i], argType)
			if !ok {
				return 0, false
			}

			score += itemScore
		}

		score += (len(argTypes) - count) * 20
		return score, true
	}

	if len(argTypes) < required || len(argTypes) > total {
		return 0, false
	}

	score := 0

	for i, argType := range argTypes {
		itemScore, ok := g.conversionScore(info.ParamTypes[i], argType)
		if !ok {
			return 0, false
		}

		score += itemScore
	}

	score += (total - len(argTypes)) * 5

	return score, true
}

func (g *Generator) conversionScore(dst CType, src CType) (int, bool) {
	if dst.SealName == src.SealName {
		return 0, true
	}

	if dst.SealName == "any" {
		return 20, true
	}

	if src.SealName == "int" && (dst.SealName == "f32" || dst.SealName == "f64") {
		return 2, true
	}

	if src.SealName == "f32" && dst.SealName == "f64" {
		return 2, true
	}

	if g.isUnion(dst) {
		if src.SealName == "nil" || g.unionHasMember(dst.SealName, src.SealName) {
			return 1, true
		}
	}

	if src.SealName == "nil" && strings.HasPrefix(dst.SealName, "*") {
		return 1, true
	}

	if g.isInterfaceCType(dst) &&
		strings.HasPrefix(src.SealName, "*") {
		instance, ok := g.interfaceInstanceForCType(dst)
		if !ok {
			return 0, false
		}

		concrete := CType{
			Name:     strings.TrimSuffix(strings.TrimSpace(src.Name), "*"),
			SealName: strings.TrimPrefix(src.SealName, "*"),
		}

		if g.findResolvedImpl(instance, concrete) != nil {
			return 3, true
		}
	}

	return 0, false
}

func (g *Generator) isByteIndexableCType(
	t CType,
) bool {
	return g.isScalarByteIndexableCType(t)
}

func (g *Generator) isScalarByteIndexableCType(
	t CType,
) bool {
	if t.IsVariadic {
		return false
	}

	switch t.SealName {
	case "bool",
		"int",
		"uint",
		"i8",
		"i16",
		"i32",
		"i64",
		"u8",
		"u16",
		"u32",
		"u64",
		"char",
		"f32",
		"f64":
		return true
	}

	return strings.HasPrefix(
		t.SealName,
		"*",
	)
}

func (g *Generator) isAddressableByteSource(
	expr ast.Expr,
) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope == nil {
			return false
		}

		_, ok := g.scope.lookup(e.Name.Name)
		return ok

	case *ast.SelectorExpr:
		leftType :=
			g.inferExprType(e.Left, nil)

		if strings.HasPrefix(
			leftType.SealName,
			"*",
		) {
			return true
		}

		return g.isAddressableByteSource(e.Left)

	case *ast.UnaryExpr:
		return e.Op == token.Star

	case *ast.IndexExpr:
		resolution, ok :=
			g.indexResolutions[e]

		if !ok ||
			resolution.Candidate != nil {
			return false
		}

		leftType :=
			g.inferExprType(e.Left, nil)

		if leftType.IsVariadic ||
			leftType.SealName == "rawptr" {
			return true
		}

		if g.isByteIndexableCType(leftType) {
			return g.isAddressableByteSource(
				e.Left,
			)
		}
	}

	return false
}

func (g *Generator) emitByteIndexExpr(e *ast.IndexExpr, leftType CType, left string, index string) string {
	if g.isAddressableByteSource(e.Left) {
		return fmt.Sprintf("((unsigned char *)&(%s))[%s]", left, index)
	}

	if g.isScalarByteIndexableCType(leftType) {
		return fmt.Sprintf("((unsigned char *)&(%s){%s})[%s]", leftType.Name, left, index)
	}

	g.error(e.Left.Span(), "byte indexing a non-addressable composite value requires assigning it to a variable first")
	return "0"
}

func (g *Generator) isUnion(t CType) bool {
	_, ok := g.unions[t.SealName]
	return ok
}

func (g *Generator) isEnumCType(t CType) bool {
	if g.enums[t.SealName] != nil {
		return true
	}

	for packageName, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		for enumName := range pkg.Enums {
			if cImportedTypeName(
				packageName,
				enumName,
			) == t.SealName {
				return true
			}
		}
	}

	return false
}

func (g *Generator) isInterfaceCType(t CType) bool {
	if t.IsInterface {
		return true
	}

	_, ok := g.interfaceInstances[t.SealName]
	return ok
}

func (g *Generator) isDynInterfaceName(name string) bool {
	instance := g.interfaceInstances[name]
	return instance != nil && instance.IsDyn
}

func staticInterfaceDispatcherName(iface string, req string) string {
	return sanitizeCName(iface) + "_" + sanitizeCName(req)
}

func (g *Generator) tryEmitInterfaceConversion(
	expected CType,
	expr ast.Expr,
) (string, bool) {
	if !g.isInterfaceCType(expected) {
		return "", false
	}

	instance, ok := g.interfaceInstanceForCType(expected)
	if !ok {
		return "", false
	}

	src := g.inferExprType(expr, nil)

	if src.SealName == expected.SealName {
		return "", false
	}

	if src.SealName == "nil" {
		return g.nilInterfaceValue(expected), true
	}

	if !strings.HasPrefix(src.SealName, "*") {
		return "", false
	}

	concrete := CType{
		Name:     strings.TrimSuffix(strings.TrimSpace(src.Name), "*"),
		SealName: strings.TrimPrefix(src.SealName, "*"),
	}

	impl := g.findResolvedImpl(instance, concrete)
	if impl == nil {
		g.error(
			expr.Span(),
			fmt.Sprintf(
				"%s does not implement %s",
				concrete.SealName,
				expected.SealName,
			),
		)

		return g.nilInterfaceValue(expected), true
	}

	value := g.emitExpr(expr, nil)

	if instance.IsDyn {
		return fmt.Sprintf(
			"(%s){.data = (void *)%s, .vtable = &%s}",
			expected.Name,
			value,
			interfaceVTableName(
				instance.CName,
				concrete.SealName,
			),
		), true
	}

	return fmt.Sprintf(
		"(%s){.tag = %s, .data = (void *)%s}",
		expected.Name,
		interfaceImplTagName(instance, concrete),
		value,
	), true
}

func (g *Generator) resolveDelegatedImplInstances() {
	keys := make([]string, 0, len(g.resolvedImpls))

	for key := range g.resolvedImpls {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		info := g.resolvedImpls[key]
		if info == nil || len(info.UsingPath) == 0 {
			continue
		}

		fieldType, ok := g.resolveImplUsingPathType(
			info.Target,
			info.UsingPath,
		)
		if !ok {
			continue
		}

		delegatedTarget, ok := dereferenceImplTargetType(fieldType)
		if !ok {
			g.error(
				info.Template.Decl.Span(),
				fmt.Sprintf(
					"invalid delegated target reached through %s",
					implUsingPathString(info.UsingPath),
				),
			)
			continue
		}

		delegated := g.findResolvedImpl(
			info.Interface,
			delegatedTarget,
		)
		if delegated == nil {
			g.error(
				info.Template.Decl.Span(),
				fmt.Sprintf(
					"cannot delegate %s for %s through %s: %s does not implement %s",
					info.Interface.Key,
					info.Target.SealName,
					implUsingPathString(info.UsingPath),
					delegatedTarget.SealName,
					info.Interface.Key,
				),
			)
			continue
		}

		if delegated == info {
			g.error(
				info.Template.Decl.Span(),
				fmt.Sprintf(
					"delegated impl %s for %s delegates to itself",
					info.Interface.Key,
					info.Target.SealName,
				),
			)
			continue
		}

		info.Delegated = delegated

		if g.delegationWouldCycle(info, delegated) {
			info.Delegated = nil

			g.error(
				info.Template.Decl.Span(),
				fmt.Sprintf(
					"delegated impl %s for %s creates a delegation cycle",
					info.Interface.Key,
					info.Target.SealName,
				),
			)
		}
	}
}

func (g *Generator) resolveImplUsingPathType(
	target CType,
	path []ast.Ident,
) (CType, bool) {
	current := target

	for _, part := range path {
		container := current

		if strings.HasPrefix(container.SealName, "*") {
			var ok bool
			container, ok = dereferenceImplTargetType(container)
			if !ok {
				return CInvalid, false
			}
		}

		fieldType := g.lookupStructFieldType(
			container.SealName,
			part.Name,
		)
		if isInvalidCType(fieldType) {
			g.error(
				part.Span(),
				fmt.Sprintf(
					"type %s has no field %q for delegated impl",
					container.SealName,
					part.Name,
				),
			)
			return CInvalid, false
		}

		current = fieldType
	}

	return current, true
}

func dereferenceImplTargetType(t CType) (CType, bool) {
	if isInvalidCType(t) {
		return CInvalid, false
	}

	if !strings.HasPrefix(t.SealName, "*") {
		return t, true
	}

	if t.Elem != nil && !isInvalidCType(*t.Elem) {
		return *t.Elem, true
	}

	name := strings.TrimSpace(t.Name)
	name = strings.TrimSpace(strings.TrimSuffix(name, "*"))

	sealName := strings.TrimPrefix(t.SealName, "*")

	if name == "" || sealName == "" {
		return CInvalid, false
	}

	return CType{
		Name:     name,
		SealName: sealName,
	}, true
}

func (g *Generator) delegationWouldCycle(
	start *ResolvedImplInstance,
	next *ResolvedImplInstance,
) bool {
	visited := map[string]bool{}

	for current := next; current != nil; current = current.Delegated {
		if current == start || current.Key == start.Key {
			return true
		}

		if visited[current.Key] {
			return true
		}

		visited[current.Key] = true
	}

	return false
}

func (g *Generator) discoverRequestedInterfaceImpls() bool {
	overallChanged := false

	for {
		changed := false

		keys := make([]string, 0, len(g.interfaceInstances))

		for key := range g.interfaceInstances {
			keys = append(keys, key)
		}

		sort.Strings(keys)

		for _, key := range keys {
			instance := g.interfaceInstances[key]
			if instance == nil {
				continue
			}

			for _, template := range g.implTemplates {
				if template == nil {
					continue
				}

				resolvedInstances :=
					g.resolveImplTemplateForInterface(
						template,
						instance,
					)

				for _, resolved := range resolvedInstances {
					if resolved == nil {
						continue
					}

					if _, exists :=
						g.resolvedImpls[resolved.Key]; exists {
						// The checker owns ambiguity diagnostics.
						continue
					}

					g.resolvedImpls[resolved.Key] =
						resolved

					g.collectResolvedImplMonomorphizations(
						resolved,
					)

					changed = true
					overallChanged = true
				}
			}
		}

		if !changed {
			break
		}
	}

	return overallChanged
}

func (g *Generator) resolveImplTemplateForInterface(
	template *ImplTemplate,
	instance *InterfaceInstance,
) []*ResolvedImplInstance {
	if template == nil || instance == nil {
		return nil
	}

	baseSubst := map[string]ast.GenericArg{}

	paramKinds := implGenericParamKindsForCGen(
		template.GenericParams,
	)

	if !g.matchInterfaceTemplate(
		template.Interface,
		template.PackageName,
		instance,
		baseSubst,
		paramKinds,
	) {
		return nil
	}

	var out []*ResolvedImplInstance
	seen := map[string]bool{}

	addResolved := func(
		target CType,
		subst map[string]ast.GenericArg,
	) {
		if isInvalidCType(target) {
			return
		}

		key := resolvedImplKey(instance.Key, target)

		if seen[key] {
			return
		}

		seen[key] = true

		out = append(out, &ResolvedImplInstance{
			Key:       key,
			Template:  template,
			Interface: instance,
			Target:    target,
			Subst:     cloneImplSubst(subst),
			UsingPath: append(
				[]ast.Ident(nil),
				template.UsingPath...,
			),
		})
	}

	// Fast path: every generic argument was inferred from the interface.
	if implTemplateSubstCompleteForCGen(
		template.GenericParams,
		baseSubst,
	) {
		targetAst := g.substituteTypeAstForCGen(
			template.Target,
			baseSubst,
		)

		target :=
			g.cTypeFromAstInTypeContext(
				template.PackageName,
				targetAst,
			)

		addResolved(target, baseSubst)
		return out
	}

	// Some implementation parameters can occur only in the target:
	//
	//     impl<T> Readable :: Box<T>
	//
	// Match against every concrete target type already discovered in the
	// current whole-program compilation.
	for _, candidate := range g.implTargetCandidates() {
		subst := cloneImplSubst(baseSubst)

		if !g.matchImplTargetCType(
			template.Target,
			candidate,
			template.PackageName,
			subst,
			paramKinds,
		) {
			continue
		}

		if !implTemplateSubstCompleteForCGen(
			template.GenericParams,
			subst,
		) {
			continue
		}

		addResolved(candidate, subst)
	}

	return out
}

func cloneImplSubst(
	input map[string]ast.GenericArg,
) map[string]ast.GenericArg {
	out := map[string]ast.GenericArg{}

	for name, arg := range input {
		out[name] = arg
	}

	return out
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

func (g *Generator) matchInterfaceTemplate(
	pattern ast.Type,
	patternPackageName string,
	instance *InterfaceInstance,
	subst map[string]ast.GenericArg,
	paramKinds map[string]ast.GenericParamCategory,
) bool {
	if pattern == nil || instance == nil {
		return false
	}

	switch p := pattern.(type) {
	case *ast.NamedType:
		if len(p.Parts) == 0 {
			return false
		}

		if len(p.Parts) == 1 {
			return patternPackageName ==
				instance.PackageName &&
				p.Parts[0].Name ==
					instance.BaseName &&
				len(instance.Args) == 0
		}

		pkgName := p.Parts[0].Name
		name := p.Parts[len(p.Parts)-1].Name

		return pkgName == instance.PackageName &&
			name == instance.BaseName &&
			len(instance.Args) == 0

	case *ast.GenericType:
		baseName := typeNameFromAst(p.Base)
		if baseName != instance.BaseName {
			return false
		}

		expectedPackage := patternPackageName

		if pkgName, _, ok :=
			packageTypeNameFromAst(p.Base); ok {
			expectedPackage = pkgName
		}

		if expectedPackage != instance.PackageName {
			return false
		}

		if len(p.Args) != len(instance.Args) {
			return false
		}

		for i := range p.Args {
			if !g.matchImplGenericArg(
				p.Args[i],
				instance.Args[i],
				subst,
				paramKinds,
			) {
				return false
			}
		}

		return true
	}

	return false
}

func (g *Generator) matchImplGenericArg(
	pattern ast.GenericArg,
	actual ast.GenericArg,
	subst map[string]ast.GenericArg,
	paramKinds map[string]ast.GenericParamCategory,
) bool {
	switch pattern.Kind {
	case ast.GenericArgType:
		return g.matchImplType(
			pattern.Type,
			typeAstFromGenericArgForCGen(actual),
			subst,
			paramKinds,
		)

	case ast.GenericArgExpr:
		if id, ok := pattern.Expr.(*ast.IdentExpr); ok {
			if _, generic := paramKinds[id.Name.Name]; generic {
				if existing, found := subst[id.Name.Name]; found {
					return genericRequestArgKey(existing) ==
						genericRequestArgKey(actual)
				}

				subst[id.Name.Name] = actual
				return true
			}
		}

		return genericRequestArgKey(pattern) ==
			genericRequestArgKey(actual)
	}

	return false
}

func (g *Generator) matchImplType(
	pattern ast.Type,
	actual ast.Type,
	subst map[string]ast.GenericArg,
	paramKinds map[string]ast.GenericParamCategory,
) bool {
	if pattern == nil || actual == nil {
		return pattern == actual
	}

	switch p := pattern.(type) {
	case *ast.NamedType:
		if len(p.Parts) == 1 {
			name := p.Parts[0].Name
			category, generic := paramKinds[name]

			if generic &&
				isImplTypeGenericCategoryForCGen(category) {
				arg := ast.GenericArg{
					Kind: ast.GenericArgType,
					Type: actual,
					Loc:  actual.Span(),
				}

				if existing, ok := subst[name]; ok {
					return genericRequestArgKey(existing) ==
						genericRequestArgKey(arg)
				}

				subst[name] = arg
				return true
			}
		}

		a, ok := actual.(*ast.NamedType)
		if !ok || len(p.Parts) != len(a.Parts) {
			return false
		}

		for i := range p.Parts {
			if p.Parts[i].Name != a.Parts[i].Name {
				return false
			}
		}

		return true

	case *ast.PointerType:
		a, ok := actual.(*ast.PointerType)

		return ok && g.matchImplType(
			p.Elem,
			a.Elem,
			subst,
			paramKinds,
		)

	case *ast.GenericType:
		a, ok := actual.(*ast.GenericType)
		if !ok {
			return false
		}

		if !g.matchImplType(
			p.Base,
			a.Base,
			subst,
			paramKinds,
		) {
			return false
		}

		if len(p.Args) != len(a.Args) {
			return false
		}

		for i := range p.Args {
			if !g.matchImplGenericArg(
				p.Args[i],
				a.Args[i],
				subst,
				paramKinds,
			) {
				return false
			}
		}

		return true
	}

	return false
}

func (g *Generator) implTargetCandidates() []CType {
	seen := map[string]CType{}

	add := func(typ CType) {
		if isInvalidCType(typ) ||
			typ.IsInterface ||
			typ.IsVariadic {
			return
		}

		seen[typ.SealName] = typ
	}

	for _, spec := range builtin.Types {
		add(CType{
			Name:     spec.CName,
			SealName: spec.Name,
		})
	}

	for name, decl := range g.structs {
		if decl == nil ||
			len(decl.GenericParams) != 0 {
			continue
		}

		add(CType{
			Name:     name,
			SealName: name,
		})
	}

	for name := range g.distincts {
		add(CType{
			Name:     name,
			SealName: name,
		})
	}

	for name := range g.enums {
		add(CType{
			Name:     name,
			SealName: name,
		})
	}

	for name := range g.unions {
		add(CType{
			Name:     name,
			SealName: name,
		})
	}

	for name := range g.genericStructs {
		add(CType{
			Name:     name,
			SealName: name,
		})
	}

	for name := range g.importedGenericStructs {
		add(CType{
			Name:     name,
			SealName: name,
		})
	}

	packageNames := make([]string, 0, len(g.packages))

	for packageName := range g.packages {
		packageNames = append(
			packageNames,
			packageName,
		)
	}

	sort.Strings(packageNames)

	for _, packageName := range packageNames {
		pkg := g.packages[packageName]
		if pkg == nil {
			continue
		}

		for name, decl := range pkg.Structs {
			if decl == nil ||
				len(decl.GenericParams) != 0 {
				continue
			}

			cName := cImportedTypeName(
				packageName,
				name,
			)

			add(CType{
				Name:     cName,
				SealName: cName,
			})
		}

		for name := range pkg.Distincts {
			cName := cImportedTypeName(
				packageName,
				name,
			)

			add(CType{
				Name:     cName,
				SealName: cName,
			})
		}

		for name := range pkg.Enums {
			cName := cImportedTypeName(
				packageName,
				name,
			)

			add(CType{
				Name:     cName,
				SealName: cName,
			})
		}

		for name := range pkg.Unions {
			cName := cImportedTypeName(
				packageName,
				name,
			)

			add(CType{
				Name:     cName,
				SealName: cName,
			})
		}
	}

	names := make([]string, 0, len(seen))

	for name := range seen {
		names = append(names, name)
	}

	sort.Strings(names)

	out := make([]CType, 0, len(names))

	for _, name := range names {
		out = append(out, seen[name])
	}

	return out
}

func (g *Generator) matchImplTargetCType(
	pattern ast.Type,
	actual CType,
	patternPackageName string,
	subst map[string]ast.GenericArg,
	paramKinds map[string]ast.GenericParamCategory,
) bool {
	if pattern == nil || isInvalidCType(actual) {
		return false
	}

	switch p := pattern.(type) {
	case *ast.NamedType:
		if len(p.Parts) == 1 {
			name := p.Parts[0].Name

			category, generic := paramKinds[name]

			if generic &&
				isImplTypeGenericCategoryForCGen(
					category,
				) {
				arg := ast.GenericArg{
					Kind: ast.GenericArgType,
					Type: g.astTypeForCType(actual),
					Loc:  p.Span(),
				}

				if existing, found := subst[name]; found {
					return genericRequestArgKey(existing) ==
						genericRequestArgKey(arg)
				}

				subst[name] = arg
				return true
			}
		}

		expected :=
			g.cTypeFromAstWithGenericArgsInTypeContext(
				patternPackageName,
				p,
				subst,
			)

		return !isInvalidCType(expected) &&
			expected.SealName == actual.SealName

	case *ast.PointerType:
		if !strings.HasPrefix(actual.SealName, "*") {
			return false
		}

		elem, ok := dereferenceImplTargetType(actual)
		if !ok {
			return false
		}

		return g.matchImplTargetCType(
			p.Elem,
			elem,
			patternPackageName,
			subst,
			paramKinds,
		)

	case *ast.GenericType:
		actualPackage,
			actualBase,
			actualArgs,
			ok := g.genericImplTargetIdentity(actual)

		if !ok {
			return false
		}

		expectedPackage := patternPackageName

		if packageName, _, qualified :=
			packageTypeNameFromAst(p.Base); qualified {
			expectedPackage = packageName
		}

		if expectedPackage != actualPackage ||
			typeNameFromAst(p.Base) != actualBase ||
			len(p.Args) != len(actualArgs) {
			return false
		}

		for i := range p.Args {
			if !g.matchImplGenericArg(
				p.Args[i],
				actualArgs[i],
				subst,
				paramKinds,
			) {
				return false
			}
		}

		return true
	}

	return false
}

func (g *Generator) genericImplTargetIdentity(
	actual CType,
) (
	packageName string,
	baseName string,
	args []ast.GenericArg,
	ok bool,
) {
	if info := g.genericStructs[actual.Name]; info != nil {
		return g.packageName,
			info.Decl.Name.Name,
			append([]ast.GenericArg(nil), info.Args...),
			true
	}

	if info :=
		g.importedGenericStructs[actual.Name]; info != nil {
		return info.PackageName,
			info.TypeName,
			append([]ast.GenericArg(nil), info.Args...),
			true
	}

	return "", "", nil, false
}

func (g *Generator) astTypeForCType(
	typ CType,
) ast.Type {
	if strings.HasPrefix(typ.SealName, "*") {
		elem, ok := dereferenceImplTargetType(typ)
		if ok {
			return &ast.PointerType{
				Elem: g.astTypeForCType(elem),
			}
		}
	}

	if info := g.genericStructs[typ.Name]; info != nil {
		return &ast.GenericType{
			Base: &ast.NamedType{
				Parts: []ast.Ident{
					{Name: info.Decl.Name.Name},
				},
			},
			Args: append(
				[]ast.GenericArg(nil),
				info.Args...,
			),
		}
	}

	if info :=
		g.importedGenericStructs[typ.Name]; info != nil {
		return &ast.GenericType{
			Base: &ast.NamedType{
				Parts: []ast.Ident{
					{Name: info.PackageName},
					{Name: info.TypeName},
				},
			},
			Args: append(
				[]ast.GenericArg(nil),
				info.Args...,
			),
		}
	}

	if _, ok := builtin.LookupType(typ.SealName); ok {
		return &ast.NamedType{
			Parts: []ast.Ident{
				{Name: typ.SealName},
			},
		}
	}

	if g.structs[typ.SealName] != nil ||
		g.distincts[typ.SealName] != nil ||
		g.enums[typ.SealName] != nil ||
		g.unions[typ.SealName] != nil {
		return &ast.NamedType{
			Parts: []ast.Ident{
				{Name: typ.SealName},
			},
		}
	}

	for packageName, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		for typeName := range pkg.Structs {
			if cImportedTypeName(
				packageName,
				typeName,
			) == typ.SealName {
				return &ast.NamedType{
					Parts: []ast.Ident{
						{Name: packageName},
						{Name: typeName},
					},
				}
			}
		}

		for typeName := range pkg.Distincts {
			if cImportedTypeName(
				packageName,
				typeName,
			) == typ.SealName {
				return &ast.NamedType{
					Parts: []ast.Ident{
						{Name: packageName},
						{Name: typeName},
					},
				}
			}
		}

		for typeName := range pkg.Enums {
			if cImportedTypeName(
				packageName,
				typeName,
			) == typ.SealName {
				return &ast.NamedType{
					Parts: []ast.Ident{
						{Name: packageName},
						{Name: typeName},
					},
				}
			}
		}

		for typeName := range pkg.Unions {
			if cImportedTypeName(
				packageName,
				typeName,
			) == typ.SealName {
				return &ast.NamedType{
					Parts: []ast.Ident{
						{Name: packageName},
						{Name: typeName},
					},
				}
			}
		}
	}

	return &ast.NamedType{
		Parts: []ast.Ident{
			{Name: typ.SealName},
		},
	}
}

func (g *Generator) collectResolvedImplMonomorphizations(
	info *ResolvedImplInstance,
) {
	if info == nil ||
		info.Template == nil {
		return
	}

	oldSubst := g.genericSubst
	g.genericSubst = info.Subst

	defer func() {
		g.genericSubst = oldSubst
	}()

	g.withTypeContext(
		info.Template.PackageName,
		func() {
			for _, entry := range info.Template.Entries {
				if entry.Task != nil {
					for _, param := range entry.Task.Params {
						typ :=
							g.substituteTypeAstForCGen(
								param.Type,
								info.Subst,
							)

						g.collectGenericStructInstancesFromType(
							typ,
						)

						g.collectInterfaceInstancesFromType(
							typ,
						)

						if param.HasDefault {
							value :=
								g.substituteExprForCGen(
									param.Default,
									info.Subst,
								)

							g.collectGenericStructInstancesFromExpr(
								value,
							)

							g.collectInterfaceInstancesFromExpr(
								value,
							)
						}
					}

					for _, result := range entry.Task.Results {
						typ :=
							g.substituteTypeAstForCGen(
								result,
								info.Subst,
							)

						g.collectGenericStructInstancesFromType(
							typ,
						)

						g.collectInterfaceInstancesFromType(
							typ,
						)
					}

					g.collectGenericStructInstancesFromBlockWithGenericArgs(
						entry.Task.Body,
						info.Subst,
					)
				}

				if entry.Alias != nil {
					value :=
						g.substituteExprForCGen(
							entry.Alias,
							info.Subst,
						)

					g.collectGenericStructInstancesFromExpr(
						value,
					)

					g.collectInterfaceInstancesFromExpr(
						value,
					)
				}
			}
		},
	)
}

func (g *Generator) emitResolvedInterfaceWrapperPrototype(
	info *ResolvedImplInstance,
	req *ast.TaskSignature,
) {
	if info == nil || info.Interface == nil || req == nil {
		return
	}

	ret := g.interfaceRequirementReturnType(
		info.Interface,
		req,
	)

	wrapperName := interfaceWrapperName(
		info.Interface.CName,
		info.Target.SealName,
		req.Name.Name,
	)

	params := []string{"void *data"}

	for i := 1; i < len(req.Params); i++ {
		paramType := g.interfaceRequirementParamType(
			info.Interface,
			req,
			i,
		)

		params = append(
			params,
			paramType.Decl(fmt.Sprintf("arg%d", i)),
		)
	}

	g.linef(
		"static %s %s(%s);",
		ret.Name,
		wrapperName,
		strings.Join(params, ", "),
	)
}

func (g *Generator) resolvedImplsForInterface(
	instance *InterfaceInstance,
) []*ResolvedImplInstance {
	var out []*ResolvedImplInstance

	for _, impl := range g.resolvedImpls {
		if impl == nil || impl.Interface == nil {
			continue
		}

		if impl.Interface.Key == instance.Key {
			out = append(out, impl)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Target.SealName < out[j].Target.SealName
	})

	return out
}

func (g *Generator) findResolvedImpl(
	instance *InterfaceInstance,
	target CType,
) *ResolvedImplInstance {
	if instance == nil {
		return nil
	}

	key := resolvedImplKey(instance.Key, target)
	return g.resolvedImpls[key]
}

func (g *Generator) emitImplVTables() {
	keys := make([]string, 0, len(g.resolvedImpls))

	for key := range g.resolvedImpls {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	// Emit every wrapper declaration first. Delegated wrappers may call
	// another wrapper whose definition sorts later in the output.
	emittedPrototype := false

	for _, key := range keys {
		info := g.resolvedImpls[key]
		if info == nil ||
			info.Interface == nil ||
			info.Interface.Decl == nil {
			continue
		}

		for _, req := range info.Interface.Decl.Requirements {
			g.emitResolvedInterfaceWrapperPrototype(
				info,
				req,
			)
			emittedPrototype = true
		}
	}

	if emittedPrototype {
		g.line("")
	}

	for _, key := range keys {
		info := g.resolvedImpls[key]
		if info == nil {
			continue
		}

		for _, req := range info.Interface.Decl.Requirements {
			g.emitResolvedInterfaceWrapper(info, req)
		}

		if info.Interface.IsDyn {
			g.emitResolvedInterfaceVTable(info)
		}
	}
}

func (g *Generator) emitResolvedInterfaceVTable(
	info *ResolvedImplInstance,
) {
	vtableName := interfaceVTableName(
		info.Interface.CName,
		info.Target.SealName,
	)

	g.linef(
		"static const %s_vtable %s = {",
		info.Interface.CName,
		vtableName,
	)
	g.indent++

	for _, req := range info.Interface.Decl.Requirements {
		g.linef(
			".%s = %s,",
			sanitizeCName(req.Name.Name),
			interfaceWrapperName(
				info.Interface.CName,
				info.Target.SealName,
				req.Name.Name,
			),
		)
	}

	g.indent--
	g.line("};")
	g.line("")
}

func (g *Generator) emitResolvedInterfaceWrapper(
	info *ResolvedImplInstance,
	req *ast.TaskSignature,
) {
	ret := g.interfaceRequirementReturnType(
		info.Interface,
		req,
	)

	wrapperName := interfaceWrapperName(
		info.Interface.CName,
		info.Target.SealName,
		req.Name.Name,
	)

	entry, hasEntry := info.Template.Entries[req.Name.Name]

	var params []string
	params = append(params, "void *data")

	for i := 1; i < len(req.Params); i++ {
		paramType := g.interfaceRequirementParamType(
			info.Interface,
			req,
			i,
		)

		params = append(
			params,
			paramType.Decl(fmt.Sprintf("arg%d", i)),
		)
	}

	g.linef(
		"static %s %s(%s) {",
		ret.Name,
		wrapperName,
		strings.Join(params, ", "),
	)
	g.indent++

	if len(info.UsingPath) > 0 {
		g.emitDelegatedInterfaceWrapperBody(info, req, ret)
		g.indent--
		g.line("}")
		g.line("")
		return
	}

	if !hasEntry {
		g.error(
			req.Name.Span(),
			fmt.Sprintf(
				"impl %s for %s is missing requirement %q",
				info.Interface.Key,
				info.Target.SealName,
				req.Name.Name,
			),
		)

		if ret.SealName != "void" {
			g.line("return 0;")
		}

		g.indent--
		g.line("}")
		g.line("")
		return
	}

	if entry.Alias != nil {
		g.emitAliasInterfaceWrapperBody(info, req, entry, ret)
		g.indent--
		g.line("}")
		g.line("")
		return
	}

	if entry.Task != nil {
		g.emitInlineInterfaceWrapperBody(
			info,
			req,
			entry,
			ret,
		)
		g.indent--
		g.line("}")
		g.line("")
		return
	}

	g.error(
		entry.Name.Span(),
		fmt.Sprintf(
			"impl entry %q has no task body or alias",
			entry.Name.Name,
		),
	)

	if ret.SealName != "void" {
		g.line("return 0;")
	}

	g.indent--
	g.line("}")
	g.line("")
}

func (g *Generator) emitAliasInterfaceWrapperBody(
	info *ResolvedImplInstance,
	req *ast.TaskSignature,
	entry ast.ImplEntry,
	ret CType,
) {
	targetName, targetInfo, ok := g.implAliasTaskInfo(
		info.Template.PackageName,
		entry.Alias,
	)
	if !ok {
		g.error(
			entry.Alias.Span(),
			fmt.Sprintf(
				"unsupported impl alias for %q",
				req.Name.Name,
			),
		)

		if ret.SealName == "void" {
			g.line("return;")
		} else if len(req.Results) > 1 {
			g.linef(
				"%s __seal_result = {0};",
				ret.Name,
			)
			g.line("return __seal_result;")
		} else {
			g.line("return 0;")
		}

		return
	}

	callArgs := []string{
		fmt.Sprintf("(%s *)data", info.Target.Name),
	}

	for i := 1; i < len(req.Params); i++ {
		callArgs = append(
			callArgs,
			fmt.Sprintf("arg%d", i),
		)
	}

	args := strings.Join(callArgs, ", ")

	if ret.SealName == "void" {
		g.linef(
			"%s(%s);",
			targetName,
			args,
		)
		g.line("return;")
		return
	}

	// A task with multiple return values has its own generated C result
	// struct. Even when the fields are structurally identical, C does not
	// allow returning that struct as the differently named result struct
	// used by the interface requirement.
	//
	// Copy the fields explicitly into the interface wrapper's result type.
	if len(req.Results) > 1 {
		targetResultType := targetInfo.ReturnType.Name

		if targetResultType == "" {
			g.error(
				entry.Alias.Span(),
				fmt.Sprintf(
					"cannot determine result type for impl alias %q",
					req.Name.Name,
				),
			)
			targetResultType = targetName + "_Result"
		}

		g.linef(
			"%s __seal_impl_result = %s(%s);",
			targetResultType,
			targetName,
			args,
		)
		g.linef(
			"%s __seal_wrapper_result = {0};",
			ret.Name,
		)

		for i := range req.Results {
			g.linef(
				"__seal_wrapper_result._%d = __seal_impl_result._%d;",
				i,
				i,
			)
		}

		g.line("return __seal_wrapper_result;")
		return
	}

	g.linef(
		"return %s(%s);",
		targetName,
		args,
	)
}

func (g *Generator) emitInlineInterfaceWrapperBody(
	info *ResolvedImplInstance,
	req *ast.TaskSignature,
	entry ast.ImplEntry,
	ret CType,
) {
	oldTask := g.currentTask
	oldScope := g.scope
	oldTaskScope := g.taskScope
	oldResults := g.currentResults
	oldSubst := g.genericSubst
	oldReturnStructOverride := g.currentReturnStructOverride
	oldTypeContextPackage := g.typeContextPackage

	g.scope = newScope(oldScope)
	g.taskScope = g.scope
	g.genericSubst = info.Subst
	g.typeContextPackage = info.Template.PackageName

	// Use the interface requirement's ABI result types. The checker has
	// already verified that the inline implementation has the same Seal
	// signature.
	g.currentResults = g.interfaceRequirementResultTypes(
		info.Interface,
		req,
	)

	g.currentReturnStructOverride = nil

	if len(g.currentResults) > 1 {
		wrapperReturnType := ret
		g.currentReturnStructOverride = &wrapperReturnType
	}

	// Prevent an interface requirement named Main from being treated as the
	// generated C entry point.
	wrapperTask := *entry.Task
	wrapperTask.Name.Name = "__seal_interface_wrapper"
	g.currentTask = &wrapperTask

	if len(entry.Task.Params) > 0 {
		first := entry.Task.Params[0]

		firstType :=
			g.cTypeFromAstWithGenericArgsInTypeContext(
				info.Template.PackageName,
				first.Type,
				info.Subst,
			)

		g.linef(
			"%s = (%s)data;",
			firstType.Decl(first.Name.Name),
			firstType.Name,
		)

		g.scope.declare(first.Name.Name, firstType)
	}

	for i := 1; i < len(entry.Task.Params); i++ {
		param := entry.Task.Params[i]

		paramType :=
			g.cTypeFromAstWithGenericArgsInTypeContext(
				info.Template.PackageName,
				param.Type,
				info.Subst,
			)

		sourceName := fmt.Sprintf("arg%d", i)

		if param.Name.Name != sourceName {
			g.linef(
				"%s = %s;",
				paramType.Decl(param.Name.Name),
				sourceName,
			)
		}

		g.scope.declare(param.Name.Name, paramType)
	}

	g.emitBlockStatements(entry.Task.Body)
	g.emitActiveDefers()

	g.scope = oldScope
	g.taskScope = oldTaskScope
	g.currentResults = oldResults
	g.genericSubst = oldSubst
	g.currentTask = oldTask
	g.currentReturnStructOverride = oldReturnStructOverride
	g.typeContextPackage = oldTypeContextPackage
}

func (g *Generator) emitDelegatedInterfaceWrapperBody(
	info *ResolvedImplInstance,
	req *ast.TaskSignature,
	ret CType,
) {
	if info == nil || info.Interface == nil || info.Template == nil {
		if ret.SealName != "void" {
			g.line("return 0;")
		}
		return
	}

	if info.Delegated == nil {
		fieldType, ok := g.resolveImplUsingPathType(
			info.Target,
			info.UsingPath,
		)
		if !ok {
			if ret.SealName != "void" {
				g.line("return 0;")
			}
			return
		}

		delegatedTarget, ok := dereferenceImplTargetType(fieldType)
		if !ok {
			g.error(
				info.Template.Decl.Span(),
				fmt.Sprintf(
					"invalid delegated target reached through %s",
					implUsingPathString(info.UsingPath),
				),
			)

			if ret.SealName != "void" {
				g.line("return 0;")
			}
			return
		}

		info.Delegated = g.findResolvedImpl(
			info.Interface,
			delegatedTarget,
		)
	}

	if info.Delegated == nil {
		fieldType, _ := g.resolveImplUsingPathType(
			info.Target,
			info.UsingPath,
		)
		delegatedTarget, _ := dereferenceImplTargetType(fieldType)

		g.error(
			info.Template.Decl.Span(),
			fmt.Sprintf(
				"cannot delegate %s for %s through %s: %s does not implement %s",
				req.Name.Name,
				info.Target.SealName,
				implUsingPathString(info.UsingPath),
				delegatedTarget.SealName,
				info.Interface.Key,
			),
		)

		if ret.SealName != "void" {
			g.line("return 0;")
		}
		return
	}

	dataExpr, ok := g.emitDelegatedDataProjection(info)
	if !ok {
		if ret.SealName != "void" {
			g.line("return 0;")
		}
		return
	}

	wrapper := interfaceWrapperName(
		info.Interface.CName,
		info.Delegated.Target.SealName,
		req.Name.Name,
	)

	callArgs := []string{dataExpr}

	for i := 1; i < len(req.Params); i++ {
		callArgs = append(
			callArgs,
			fmt.Sprintf("arg%d", i),
		)
	}

	if ret.SealName == "void" {
		g.linef(
			"%s(%s);",
			wrapper,
			strings.Join(callArgs, ", "),
		)
		g.line("return;")
		return
	}

	g.linef(
		"return %s(%s);",
		wrapper,
		strings.Join(callArgs, ", "),
	)
}

func (g *Generator) emitDelegatedDataProjection(
	info *ResolvedImplInstance,
) (string, bool) {
	if info == nil || len(info.UsingPath) == 0 {
		return "", false
	}

	currentType := info.Target

	// `data` always points to the outer implementation target.
	currentExpr := fmt.Sprintf(
		"((%s *)data)",
		info.Target.Name,
	)
	exprIsPointer := true

	for _, part := range info.UsingPath {
		containerType := currentType

		if strings.HasPrefix(containerType.SealName, "*") {
			var ok bool
			containerType, ok = dereferenceImplTargetType(
				containerType,
			)
			if !ok {
				return "", false
			}
		}

		fieldType := g.lookupStructFieldType(
			containerType.SealName,
			part.Name,
		)
		if isInvalidCType(fieldType) {
			g.error(
				part.Span(),
				fmt.Sprintf(
					"type %s has no field %q for delegated impl",
					containerType.SealName,
					part.Name,
				),
			)
			return "", false
		}

		operator := "."
		if exprIsPointer {
			operator = "->"
		}

		currentExpr = fmt.Sprintf(
			"(%s)%s%s",
			currentExpr,
			operator,
			part.Name,
		)

		currentType = fieldType
		exprIsPointer = strings.HasPrefix(
			currentType.SealName,
			"*",
		)
	}

	if exprIsPointer {
		// Pointer field:
		//
		//     transform *Transform
		//
		// The field value is already the implementation data pointer.
		return fmt.Sprintf(
			"(void *)(%s)",
			currentExpr,
		), true
	}

	// Value field:
	//
	//     transform Transform
	//
	// Pass the address of the embedded value.
	return fmt.Sprintf(
		"(void *)&(%s)",
		currentExpr,
	), true
}

func implUsingPathString(path []ast.Ident) string {
	var parts []string

	for _, item := range path {
		parts = append(parts, item.Name)
	}

	return strings.Join(parts, ".")
}

func dynamicInterfaceDispatcherName(iface string, req string) string {
	return sanitizeCName(iface) +
		"_" +
		sanitizeCName(req)
}

func (g *Generator) emitDynamicInterfaceDispatchers() {
	keys := make([]string, 0, len(g.interfaceInstances))

	for key := range g.interfaceInstances {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		instance := g.interfaceInstances[key]
		if instance == nil ||
			instance.Decl == nil ||
			!instance.IsDyn {
			continue
		}

		for _, req := range instance.Decl.Requirements {
			g.emitDynamicInterfaceDispatcher(
				instance,
				req,
			)
		}
	}
}

func (g *Generator) emitDynamicInterfaceDispatcher(
	instance *InterfaceInstance,
	req *ast.TaskSignature,
) {
	if instance == nil || req == nil {
		return
	}

	ret := g.interfaceRequirementReturnType(
		instance,
		req,
	)

	name := dynamicInterfaceDispatcherName(
		instance.CName,
		req.Name.Name,
	)

	receiverType := CType{
		Name:           instance.CName,
		SealName:       instance.Key,
		IsInterface:    true,
		IsDynInterface: true,
	}

	params := []string{
		receiverType.Decl("receiver"),
	}

	for i := 1; i < len(req.Params); i++ {
		paramType := g.interfaceRequirementParamType(
			instance,
			req,
			i,
		)

		params = append(
			params,
			paramType.Decl(fmt.Sprintf("arg%d", i)),
		)
	}

	g.linef(
		"static %s %s(%s) {",
		ret.Name,
		name,
		strings.Join(params, ", "),
	)
	g.indent++

	fieldName := sanitizeCName(req.Name.Name)

	g.linef(
		"if (receiver.vtable == NULL || receiver.vtable->%s == NULL) {",
		fieldName,
	)
	g.indent++

	g.line(
		`seal_panic_cstring("dynamic interface dispatch on nil or invalid vtable");`,
	)

	// seal_panic_cstring aborts, but the fallback keeps ordinary C compilers
	// satisfied without relying on compiler-specific noreturn annotations.
	if ret.SealName == "void" {
		g.line("return;")
	} else {
		g.linef(
			"return (%s){0};",
			ret.Name,
		)
	}

	g.indent--
	g.line("}")

	callArgs := []string{"receiver.data"}

	for i := 1; i < len(req.Params); i++ {
		callArgs = append(
			callArgs,
			fmt.Sprintf("arg%d", i),
		)
	}

	if ret.SealName == "void" {
		g.linef(
			"receiver.vtable->%s(%s);",
			fieldName,
			strings.Join(callArgs, ", "),
		)
		g.line("return;")
	} else {
		g.linef(
			"return receiver.vtable->%s(%s);",
			fieldName,
			strings.Join(callArgs, ", "),
		)
	}

	g.indent--
	g.line("}")
	g.line("")
}

func (g *Generator) emitStaticInterfaceDispatchers() {
	keys := make([]string, 0, len(g.interfaceInstances))

	for key := range g.interfaceInstances {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		instance := g.interfaceInstances[key]
		if instance == nil || instance.IsDyn {
			continue
		}

		impls := g.resolvedImplsForInterface(instance)
		g.emitStaticInterfaceTagDefinitions(instance, impls)

		for _, req := range instance.Decl.Requirements {
			g.emitStaticInterfaceDispatcher(
				instance,
				req,
				impls,
			)
		}
	}
}

func (g *Generator) emitStaticInterfaceDispatcher(
	instance *InterfaceInstance,
	req *ast.TaskSignature,
	impls []*ResolvedImplInstance,
) {
	ret := g.interfaceRequirementReturnType(instance, req)

	name := staticInterfaceDispatcherName(
		instance.CName,
		req.Name.Name,
	)

	var params []string
	params = append(
		params,
		CType{
			Name:        instance.CName,
			SealName:    instance.Key,
			IsInterface: true,
		}.Decl("receiver"),
	)

	for i := 1; i < len(req.Params); i++ {
		paramType := g.interfaceRequirementParamType(
			instance,
			req,
			i,
		)

		params = append(
			params,
			paramType.Decl(fmt.Sprintf("arg%d", i)),
		)
	}

	g.linef(
		"static %s %s(%s) {",
		ret.Name,
		name,
		strings.Join(params, ", "),
	)
	g.indent++

	g.line("switch (receiver.tag) {")
	g.indent++

	for _, impl := range impls {
		wrapper := interfaceWrapperName(
			instance.CName,
			impl.Target.SealName,
			req.Name.Name,
		)

		g.linef(
			"case %s:",
			interfaceImplTagName(instance, impl.Target),
		)
		g.indent++

		var args []string
		args = append(args, "receiver.data")

		for i := 1; i < len(req.Params); i++ {
			args = append(args, fmt.Sprintf("arg%d", i))
		}

		if ret.SealName == "void" {
			g.linef(
				"%s(%s);",
				wrapper,
				strings.Join(args, ", "),
			)
			g.line("return;")
		} else {
			g.linef(
				"return %s(%s);",
				wrapper,
				strings.Join(args, ", "),
			)
		}

		g.indent--
	}

	g.line("default:")
	g.indent++

	g.line(
		`seal_panic_cstring("static interface dispatch on nil or invalid tag");`,
	)

	if ret.SealName == "void" {
		g.line("return;")
	} else {
		g.linef(
			"return (%s){0};",
			ret.Name,
		)
	}

	g.indent--
	g.indent--
	g.line("}")

	g.indent--
	g.line("}")
	g.line("")
}

func (g *Generator) unionHasMember(unionName string, memberName string) bool {
	u := g.unions[unionName]
	if u == nil {
		return false
	}

	for _, member := range u.Members {
		memberType := g.cTypeFromAst(member)
		if memberType.SealName == memberName {
			return true
		}
	}

	return false
}

func (g *Generator) hasOperatorOverload(name string) bool {
	if !isOperatorName(name) {
		return false
	}

	_, ok := g.overloads[name]
	return ok
}

func isOperatorName(name string) bool {
	switch name {
	case "+", "-", "*", "/", "%", "==", "!=", "<", ">", "<=", ">=", "&", "|", "^":
		return true
	default:
		return false
	}
}

func (g *Generator) cTypeFromAst(t ast.Type) CType {
	switch typ := t.(type) {
	case *ast.NamedType:
		if len(typ.Parts) == 0 {
			return CInvalid
		}

		if len(typ.Parts) >= 2 {
			pkgName := typ.Parts[0].Name
			typeName :=
				typ.Parts[len(typ.Parts)-1].Name

			if pkg :=
				g.typePackageInfo(pkgName); pkg != nil &&
				g.packageHasType(pkg, typeName) {
				return g.importedNamedCType(
					pkgName,
					typeName,
				)
			}

			return CType{
				Name: cImportedTypeName(
					pkgName,
					typeName,
				),
				SealName: cImportedTypeName(
					pkgName,
					typeName,
				),
			}
		}

		name :=
			typ.Parts[len(typ.Parts)-1].Name

		// Only a foreign type context produces an imported C name.
		if g.typeContextPackage != "" &&
			g.typeContextPackage != g.packageName {
			if pkg :=
				g.typePackageInfo(
					g.typeContextPackage,
				); pkg != nil &&
				g.packageHasType(pkg, name) {
				return g.importedNamedCType(
					g.typeContextPackage,
					name,
				)
			}
		}

		if spec, ok :=
			builtin.LookupType(name); ok {
			return CType{
				Name:     spec.CName,
				SealName: spec.Name,
			}
		}

		if _, ok := g.distincts[name]; ok {
			return CType{
				Name:     name,
				SealName: name,
			}
		}

		if iface := g.interfaces[name]; iface != nil {
			instance :=
				g.registerInterfaceInstance(
					g.packageName,
					name,
					iface,
					nil,
				)

			if instance == nil {
				return CInvalid
			}

			return CType{
				Name:           instance.CName,
				SealName:       instance.Key,
				IsInterface:    true,
				IsDynInterface: instance.IsDyn,
			}
		}

		return CType{
			Name:     name,
			SealName: name,
		}

	case *ast.InterfaceSelfType:
		g.error(
			typ.Span(),
			`interface "self" requires interface requirement or impl context`,
		)
		return CInvalid

	case *ast.PointerType:
		elem := g.cTypeFromAst(
			typ.Elem,
		)

		return CType{
			Name:     elem.Name + " *",
			SealName: "*" + elem.SealName,
			Elem:     &elem,
		}

	case *ast.GenericType:
		return g.cTypeFromGenericType(
			typ,
		)
	}

	return CInvalid
}

func (g *Generator) importedNamedCType(
	packageName string,
	typeName string,
) CType {
	name := cImportedTypeName(
		packageName,
		typeName,
	)

	if pkg := g.typePackageInfo(packageName); pkg != nil {
		if iface := pkg.Interfaces[typeName]; iface != nil {
			instance := g.registerInterfaceInstance(
				packageName,
				typeName,
				iface,
				nil,
			)

			if instance == nil {
				return CInvalid
			}

			return CType{
				Name:           instance.CName,
				SealName:       instance.Key,
				IsInterface:    true,
				IsDynInterface: instance.IsDyn,
			}
		}
	}

	return CType{
		Name:     name,
		SealName: name,
	}
}

func (g *Generator) cTypeFromGenericType(
	typ *ast.GenericType,
) CType {
	if typ == nil || typ.Base == nil {
		return CInvalid
	}

	args := append(
		[]ast.GenericArg(nil),
		typ.Args...,
	)

	if g.genericSubst != nil {
		args = make(
			[]ast.GenericArg,
			0,
			len(typ.Args),
		)

		for _, arg := range typ.Args {
			args = append(
				args,
				g.substituteGenericArgForCGen(
					arg,
					g.genericSubst,
				),
			)
		}
	}

	// An explicitly package-qualified generic type.
	//
	//     mem.Box<int>
	//     app.Container<string>
	if packageName, typeName, qualified :=
		packageTypeNameFromAst(
			typ.Base,
		); qualified {
		pkg := g.typePackageInfo(
			packageName,
		)

		if pkg != nil {
			if iface := pkg.Interfaces[typeName]; iface != nil {
				instance :=
					g.registerInterfaceInstance(
						packageName,
						typeName,
						iface,
						args,
					)

				if instance == nil {
					return CInvalid
				}

				return CType{
					Name:           instance.CName,
					SealName:       instance.Key,
					IsInterface:    true,
					IsDynInterface: instance.IsDyn,
				}
			}

			if decl := pkg.Structs[typeName]; decl != nil &&
				len(decl.GenericParams) > 0 {
				name :=
					g.registerImportedGenericStructInstance(
						packageName,
						typeName,
						decl,
						args,
					)

				return CType{
					Name:     name,
					SealName: name,
				}
			}
		}
	}

	baseName := typeNameFromAst(
		typ.Base,
	)

	// An unqualified type emitted while processing declarations belonging to
	// another package belongs to that package before it belongs to the
	// package currently being generated.
	if g.typeContextPackage != "" &&
		g.typeContextPackage != g.packageName {
		pkg := g.typePackageInfo(
			g.typeContextPackage,
		)

		if pkg != nil {
			if iface := pkg.Interfaces[baseName]; iface != nil {
				instance :=
					g.registerInterfaceInstance(
						g.typeContextPackage,
						baseName,
						iface,
						args,
					)

				if instance == nil {
					return CInvalid
				}

				return CType{
					Name:           instance.CName,
					SealName:       instance.Key,
					IsInterface:    true,
					IsDynInterface: instance.IsDyn,
				}
			}

			if decl := pkg.Structs[baseName]; decl != nil &&
				len(decl.GenericParams) > 0 {
				name :=
					g.registerImportedGenericStructInstance(
						g.typeContextPackage,
						baseName,
						decl,
						args,
					)

				return CType{
					Name:     name,
					SealName: name,
				}
			}
		}
	}

	if iface := g.interfaces[baseName]; iface != nil {
		instance :=
			g.registerInterfaceInstance(
				g.packageName,
				baseName,
				iface,
				args,
			)

		if instance == nil {
			return CInvalid
		}

		return CType{
			Name:           instance.CName,
			SealName:       instance.Key,
			IsInterface:    true,
			IsDynInterface: instance.IsDyn,
		}
	}

	if decl := g.structs[baseName]; decl != nil &&
		len(decl.GenericParams) > 0 {
		name :=
			g.registerGenericStructInstance(
				decl,
				args,
			)

		return CType{
			Name:     name,
			SealName: name,
		}
	}

	base := g.cTypeFromAst(
		typ.Base,
	)

	g.error(
		typ.Span(),
		fmt.Sprintf(
			"generic type instantiation of %s is not supported by C codegen yet",
			base.String(),
		),
	)

	return base
}

func (g *Generator) registerImportedGenericStructInstance(
	packageName string,
	typeName string,
	decl *ast.StructDecl,
	args []ast.GenericArg,
) string {
	if packageName == "" ||
		typeName == "" ||
		decl == nil ||
		isInvalidCStructName(typeName) ||
		isInvalidCStructName(decl.Name.Name) {
		return CInvalid.Name
	}

	args = normalizeGenericArgsForCGenParams(
		decl.GenericParams,
		args,
	)

	// Preserve caller-local argument spelling for the representation emitted
	// in the caller, but use package-qualified arguments for the owning
	// package request and stable specialization name.
	requestArgs :=
		g.crossPackageRequestGenericArgs(
			decl.GenericParams,
			args,
		)

	name :=
		g.specializedImportedStructCName(
			packageName,
			typeName,
			decl,
			requestArgs,
		)

	if isInvalidCStructName(name) {
		return CInvalid.Name
	}

	if _, exists :=
		g.importedGenericStructs[name]; exists {
		g.addImportedGenericStructRequest(
			packageName,
			typeName,
			append(
				[]ast.GenericArg(nil),
				requestArgs...,
			),
		)

		return name
	}

	g.importedGenericStructs[name] =
		&ImportedGenericStructInstance{
			PackageName: packageName,
			TypeName:    typeName,
			Name:        name,
			Decl:        decl,
			Args: append(
				[]ast.GenericArg(nil),
				args...,
			),
		}

	g.addImportedGenericStructRequest(
		packageName,
		typeName,
		append(
			[]ast.GenericArg(nil),
			requestArgs...,
		),
	)

	return name
}

func (g *Generator) specializedImportedStructCName(packageName string, typeName string, decl *ast.StructDecl, args []ast.GenericArg) string {
	parts := []string{sanitizeCName(packageName), sanitizeCName(typeName)}

	for i, arg := range args {
		paramCategory := ast.GenericParamInvalid
		if decl != nil && i < len(decl.GenericParams) {
			paramCategory = decl.GenericParams[i].Category
		}

		switch paramCategory {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			parts = append(parts, g.genericTypeArgCName(arg))

		default:
			parts = append(parts, genericValueArgCName(arg))
		}
	}

	return strings.Join(parts, "_")
}

func isInvalidCType(t CType) bool {
	return t.Name == "" ||
		t.SealName == "<invalid>" ||
		strings.Contains(t.Name, "/*invalid*/") ||
		strings.Contains(t.SealName, "<invalid>")
}

func isInvalidCStructName(name string) bool {
	return name == "" ||
		strings.Contains(name, "/*invalid*/") ||
		strings.Contains(name, "<invalid>") ||
		strings.ContainsAny(name, " \t\r\n*-/+()[]{}.;,")
}

func (g *Generator) registerGenericStructInstance(decl *ast.StructDecl, args []ast.GenericArg) string {
	args = normalizeGenericArgsForCGenParams(decl.GenericParams, args)
	if decl == nil || isInvalidCStructName(decl.Name.Name) {
		return CInvalid.Name
	}

	name := g.specializedStructCName(decl, args)
	if isInvalidCStructName(name) {
		return CInvalid.Name
	}

	if _, exists := g.genericStructs[name]; exists {
		return name
	}

	copiedArgs := append([]ast.GenericArg(nil), args...)

	g.genericStructs[name] = &GenericStructInstance{
		Name: name,
		Decl: decl,
		Args: copiedArgs,
	}

	return name
}

func (g *Generator) specializedStructCName(decl *ast.StructDecl, args []ast.GenericArg) string {
	parts := []string{}

	if g.packageName != "" {
		parts = append(parts, sanitizeCName(g.packageName))
	}

	parts = append(parts, sanitizeCName(decl.Name.Name))

	for i, arg := range args {
		paramCategory := ast.GenericParamInvalid
		if i < len(decl.GenericParams) {
			paramCategory = decl.GenericParams[i].Category
		}

		switch paramCategory {
		case ast.GenericParamType,
			ast.GenericParamEnum,
			ast.GenericParamUnion:
			parts = append(parts, g.genericTypeArgCName(arg))

		default:
			parts = append(parts, genericValueArgCName(arg))
		}
	}

	return strings.Join(parts, "_")
}

func (g *Generator) genericTaskArgCName(arg ast.GenericArg) string {
	if g.genericSubst != nil {
		arg = g.substituteGenericArgForCGen(arg, g.genericSubst)
	}

	switch arg.Kind {
	case ast.GenericArgExpr:
		return sanitizeCName(exprCName(arg.Expr))

	case ast.GenericArgType:
		return sanitizeCName(typeNameFromAst(arg.Type))
	}

	return "invalid_task"
}

func (g *Generator) genericTypeArgCName(arg ast.GenericArg) string {
	if g.genericSubst != nil {
		arg = g.substituteGenericArgForCGen(arg, g.genericSubst)
	}

	switch arg.Kind {
	case ast.GenericArgType:
		return sanitizeCName(g.cTypeFromAstInContext(arg.Type).SealName)

	case ast.GenericArgExpr:
		if arg.Expr == nil {
			return "invalid_type"
		}

		if typ := typeAstFromExprForCGen(arg.Expr); typ != nil {
			return sanitizeCName(g.cTypeFromAstInContext(typ).SealName)
		}

		switch e := arg.Expr.(type) {
		case *ast.IdentExpr:
			name := e.Name.Name

			if g.typeContextPackage != "" {
				if pkg := g.typePackageInfo(
					g.typeContextPackage,
				); pkg != nil &&
					g.packageHasType(pkg, name) {
					return sanitizeCName(cImportedTypeName(g.typeContextPackage, name))
				}
			}

			if g.genericSubst != nil {
				if replacement, exists := g.genericSubst[name]; exists {
					return sanitizeCName(g.cTypeFromGenericArgWithGenericArgs(replacement, g.genericSubst).SealName)
				}
			}

			return sanitizeCName(name)
		}
	}

	return sanitizeCName(genericValueArgCName(arg))
}

func genericValueArgCName(arg ast.GenericArg) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return sanitizeCName(typeNameFromAst(arg.Type))

	case ast.GenericArgExpr:
		return sanitizeCName(exprCName(arg.Expr))
	}

	return "invalid"
}

func exprCName(expr ast.Expr) string {
	if expr == nil {
		return "nil"
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name.Name

	case *ast.SelectorExpr:
		return exprCName(e.Left) + "_" + e.Name.Name

	case *ast.IntLitExpr:
		return e.Value

	case *ast.FloatLitExpr:
		return strings.ReplaceAll(e.Value, ".", "_")

	case *ast.StringLitExpr:
		return strings.Trim(e.Value, `"`)

	case *ast.CStringLitExpr:
		return strings.Trim(strings.TrimPrefix(e.Value, "c"), `"`)

	case *ast.CharLitExpr:
		return strings.Trim(e.Value, `'`)

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		return "nil"

	case *ast.UnaryExpr:
		return e.Op.String() + exprCName(e.Expr)

	case *ast.BinaryExpr:
		return exprCName(e.Left) + "_" + e.Op.String() + "_" + exprCName(e.Right)

	case *ast.CallExpr:
		var args []string
		for _, arg := range e.Args {
			args = append(args, exprCName(arg))
		}

		return exprCName(e.Callee) + "_" + strings.Join(args, "_")

	case *ast.GenericExpr:
		var args []string
		for _, arg := range e.Args {
			args = append(args, genericValueArgCName(arg))
		}

		return exprCName(e.Base) + "_" + strings.Join(args, "_")
	}

	return "expr"
}

func typeAstFromExprForCGen(expr ast.Expr) ast.Type {
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
		base := typeAstFromExprForCGen(e.Base)
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

func cImportedTaskName(packageName string, taskName string, info TaskInfo) string {
	if info.IsExtern && info.ExternName != "" {
		return info.ExternName
	}

	return cPackageTaskName(packageName, taskName)
}

func (g *Generator) cAssignOp(op token.Kind) string {
	switch op {
	case token.Assign:
		return "="
	case token.PlusEq:
		return "+="
	case token.MinusEq:
		return "-="
	case token.StarEq:
		return "*="
	case token.SlashEq:
		return "/="
	case token.PercentEq:
		return "%="
	default:
		return "="
	}
}

func (g *Generator) cUnaryOp(op token.Kind) string {
	switch op {
	case token.Minus:
		return "-"
	case token.Bang:
		return "!"
	case token.Tilde:
		return "~"
	case token.Amp:
		return "&"
	case token.Star:
		return "*"
	default:
		return ""
	}
}

func (g *Generator) cBinaryOp(op token.Kind) string {
	switch op {
	case token.Plus:
		return "+"
	case token.Minus:
		return "-"
	case token.Star:
		return "*"
	case token.Slash:
		return "/"
	case token.Percent:
		return "%"
	case token.EqEq:
		return "=="
	case token.NotEq:
		return "!="
	case token.Lt:
		return "<"
	case token.LtEq:
		return "<="
	case token.Gt:
		return ">"
	case token.GtEq:
		return ">="
	case token.AndAnd:
		return "&&"
	case token.OrOr:
		return "||"
	case token.Amp:
		return "&"
	case token.Pipe:
		return "|"
	case token.Caret:
		return "^"
	default:
		return "/*op*/"
	}
}

func (g *Generator) primitiveTaskKind(name string) (builtin.TaskKind, bool) {
	if g.isLocalValueName(name) {
		return builtin.TaskInvalid, false
	}

	task, ok := builtin.LookupTask(name)
	if !ok {
		return builtin.TaskInvalid, false
	}

	return task.Kind, true
}

func (g *Generator) lookupStructFieldType(structName string, fieldName string) CType {
	if d := g.structs[structName]; d != nil {
		for _, field := range d.Fields {
			if field.Name.Name == fieldName {
				return g.cTypeFromAst(field.Type)
			}
		}

		return CInvalid
	}

	if info := g.genericStructs[structName]; info != nil {
		subst := genericArgSubstForCGen(info.Decl.GenericParams, info.Args)

		for _, field := range info.Decl.Fields {
			if field.Name.Name == fieldName {
				return g.cTypeFromAstWithGenericArgs(field.Type, subst)
			}
		}

		return CInvalid
	}

	if info := g.importedGenericStructs[structName]; info != nil {
		subst := genericArgSubstForCGen(info.Decl.GenericParams, info.Args)

		for _, field := range info.Decl.Fields {
			if field.Name.Name == fieldName {
				return g.cTypeFromAstWithGenericArgsInTypeContext(info.PackageName, field.Type, subst)
			}
		}

		return CInvalid
	}

	for pkgName, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		for typeName, decl := range pkg.Structs {
			if cImportedTypeName(pkgName, typeName) != structName {
				continue
			}

			for _, field := range decl.Fields {
				if field.Name.Name == fieldName {
					return g.cTypeFromAstInTypeContext(pkgName, field.Type)
				}
			}

			return CInvalid
		}
	}

	return CInvalid
}

func (g *Generator) line(s string) {
	g.out.WriteString(strings.Repeat("\t", g.indent))
	g.out.WriteString(s)
	g.out.WriteByte('\n')
}

func (g *Generator) linef(format string, args ...any) {
	g.line(fmt.Sprintf(format, args...))
}

func (g *Generator) error(span source.Span, message string) {
	g.diags.Add(span, message)
}

func quoteCByteString(value string) string {
	var out strings.Builder

	out.WriteByte('"')

	for i := 0; i < len(value); i++ {
		current := value[i]

		switch current {
		case '"':
			out.WriteString(`\"`)

		case '\\':
			out.WriteString(`\\`)

		default:
			// Keep ordinary printable ASCII readable. Question marks are
			// escaped to prevent old C11 trigraph processing from changing
			// source bytes before tokenization.
			if current >= 0x20 &&
				current <= 0x7E &&
				current != '?' {
				out.WriteByte(current)
				continue
			}

			fmt.Fprintf(
				&out,
				`\%03o`,
				current,
			)
		}
	}

	out.WriteByte('"')

	return out.String()
}
