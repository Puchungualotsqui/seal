package resolver

import (
	"fmt"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/source"
)

type SymbolKind int

const (
	SymbolInvalid SymbolKind = iota

	SymbolPackage
	SymbolConst
	SymbolVar
	SymbolParam

	SymbolTask
	SymbolStruct
	SymbolEnum
	SymbolUnion
	SymbolInterface
	SymbolBitSet
	SymbolOverload

	SymbolGenericType
	SymbolGenericValue

	SymbolBuiltinType
	SymbolBuiltinTask
)

func (k SymbolKind) String() string {
	switch k {
	case SymbolPackage:
		return "package"
	case SymbolConst:
		return "constant"
	case SymbolVar:
		return "variable"
	case SymbolParam:
		return "parameter"
	case SymbolTask:
		return "task"
	case SymbolStruct:
		return "struct"
	case SymbolEnum:
		return "enum"
	case SymbolUnion:
		return "union"
	case SymbolInterface:
		return "interface"
	case SymbolBitSet:
		return "bit_set"
	case SymbolOverload:
		return "overload"
	case SymbolGenericType:
		return "generic type"
	case SymbolGenericValue:
		return "generic value"
	case SymbolBuiltinType:
		return "builtin type"
	case SymbolBuiltinTask:
		return "builtin task"
	default:
		return "symbol"
	}
}

func (k SymbolKind) IsRuntime() bool {
	return k == SymbolVar || k == SymbolParam
}

func (k SymbolKind) IsCompileTime() bool {
	return !k.IsRuntime()
}

type Symbol struct {
	Name    string
	Kind    SymbolKind
	Span    source.Span
	Node    ast.Node
	Scope   *Scope
	TaskID  int
	Builtin bool

	Package *PackageInfo
}

type ScopeKind int

const (
	ScopeGlobal ScopeKind = iota
	ScopeBlock
	ScopeTask
	ScopeDecl
)

type Scope struct {
	Kind    ScopeKind
	Parent  *Scope
	Symbols map[string]*Symbol
	TaskID  int
}

func NewScope(kind ScopeKind, parent *Scope) *Scope {
	taskID := 0
	if parent != nil {
		taskID = parent.TaskID
	}

	return &Scope{
		Kind:    kind,
		Parent:  parent,
		Symbols: map[string]*Symbol{},
		TaskID:  taskID,
	}
}

func (s *Scope) LookupLocal(name string) *Symbol {
	return s.Symbols[name]
}

func (s *Scope) LookupVisible(name string) *Symbol {
	for scope := s; scope != nil; scope = scope.Parent {
		if sym := scope.LookupLocal(name); sym != nil {
			return sym
		}
	}

	return nil
}

type Resolver struct {
	diags      *diag.Reporter
	global     *Scope
	nextTaskID int
	packages   map[string]*PackageInfo
}

func New(diags *diag.Reporter) *Resolver {
	return NewWithPackages(diags, nil)
}

func NewWithPackages(diags *diag.Reporter, packages map[string]*PackageInfo) *Resolver {
	r := &Resolver{
		diags:      diags,
		global:     NewScope(ScopeGlobal, nil),
		nextTaskID: 1,
		packages:   packages,
	}

	r.declareBuiltins()
	r.declarePackages()

	return r
}

type PackageInfo struct {
	Name    string
	Symbols map[string]*PackageSymbol
}

type PackageSymbol struct {
	Name string
	Kind SymbolKind
	Span source.Span
}

func (r *Resolver) GlobalScope() *Scope {
	return r.global
}

func (r *Resolver) ResolveFile(file *ast.File) *Scope {
	for _, decl := range file.Decls {
		r.declareDeclSymbol(r.global, decl)
	}

	for _, decl := range file.Decls {
		r.resolveDecl(r.global, decl)
	}

	return r.global
}

func (r *Resolver) declareBuiltins() {
	builtinTypes := []string{
		"void",
		"bool",
		"int",
		"i8",
		"i16",
		"i32",
		"i64",
		"u8",
		"u16",
		"u32",
		"u64",
		"usize",
		"isize",
		"f32",
		"f64",
		"string",
		"rawptr",
		"any",
		"uintptr",
		"voidptr",
	}

	for _, name := range builtinTypes {
		r.declareBuiltin(name, SymbolBuiltinType)
	}

	builtinTasks := []string{
		"Assert",
		"Print",
		"len",
		"Len",
		"Trap",
		"Panic",
		"Unreachable",
	}

	for _, name := range builtinTasks {
		r.declareBuiltin(name, SymbolBuiltinTask)
	}
}

func (r *Resolver) declareBuiltin(name string, kind SymbolKind) {
	r.global.Symbols[name] = &Symbol{
		Name:    name,
		Kind:    kind,
		Scope:   r.global,
		Builtin: true,
	}
}

func (r *Resolver) declareSymbol(scope *Scope, name string, kind SymbolKind, span source.Span, node ast.Node) *Symbol {
	if name == "" {
		return nil
	}

	if existing := scope.LookupVisible(name); existing != nil {
		r.diags.Add(
			span,
			fmt.Sprintf(
				"declaration of %q shadows visible %s declared at %s",
				name,
				existing.Kind.String(),
				existing.Span.String(),
			),
		)
		return nil
	}

	sym := &Symbol{
		Name:   name,
		Kind:   kind,
		Span:   span,
		Node:   node,
		Scope:  scope,
		TaskID: scope.TaskID,
	}

	scope.Symbols[name] = sym
	return sym
}

func (r *Resolver) declareGenericSymbol(scope *Scope, name string, kind SymbolKind, span source.Span) *Symbol {
	if existing := scope.LookupLocal(name); existing != nil {
		if existing.Kind == kind {
			return existing
		}
	}

	return r.declareSymbol(scope, name, kind, span, nil)
}

func (r *Resolver) declareDeclSymbol(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		r.declareSymbol(scope, d.Name.Name, SymbolConst, d.Name.Span(), d)

	case *ast.StructDecl:
		r.declareSymbol(scope, d.Name.Name, SymbolStruct, d.Name.Span(), d)

	case *ast.TaskDecl:
		r.declareSymbol(scope, d.Name.Name, SymbolTask, d.Name.Span(), d)

	case *ast.EnumDecl:
		r.declareSymbol(scope, d.Name.Name, SymbolEnum, d.Name.Span(), d)

	case *ast.UnionDecl:
		r.declareSymbol(scope, d.Name.Name, SymbolUnion, d.Name.Span(), d)

	case *ast.InterfaceDecl:
		r.declareSymbol(scope, d.Name.Name, SymbolInterface, d.Name.Span(), d)

	case *ast.OverloadDecl:
		r.declareSymbol(scope, d.Name, SymbolOverload, d.Span(), d)

	case *ast.DirectiveDecl:
		// c :: @c_import { ... } behaves like a package namespace symbol.
		r.declareSymbol(scope, d.Name.Name, SymbolPackage, d.Name.Span(), d)

	case *ast.ImplDecl:
		// impl does not introduce a new symbol.
		return
	}
}

func (r *Resolver) resolveDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		r.resolveExpr(scope, d.Value)

	case *ast.StructDecl:
		r.resolveStructDecl(scope, d)

	case *ast.TaskDecl:
		r.resolveTaskDecl(scope, d)

	case *ast.EnumDecl:
		r.resolveEnumDecl(d)

	case *ast.UnionDecl:
		for _, member := range d.Members {
			r.resolveType(scope, member)
		}

	case *ast.InterfaceDecl:
		r.resolveInterfaceDecl(scope, d)

	case *ast.ImplDecl:
		r.resolveImplDecl(scope, d)

	case *ast.OverloadDecl:
		for _, name := range d.Names {
			r.resolveSymbolUse(scope, name.Name, name.Span())
		}

	case *ast.DirectiveDecl:
		return
	}
}

func (r *Resolver) resolveStructDecl(parent *Scope, d *ast.StructDecl) {
	scope := NewScope(ScopeDecl, parent)

	for _, param := range d.Params {
		switch param.Kind {
		case ast.GenericTypeParam:
			r.declareSymbol(scope, param.Name.Name, SymbolGenericType, param.Name.Span(), nil)

		case ast.GenericValueParam:
			r.declareSymbol(scope, param.Name.Name, SymbolGenericValue, param.Name.Span(), nil)
		}
	}

	for _, field := range d.Fields {
		r.resolveType(scope, field.Type)
	}
}

func (r *Resolver) resolveEnumDecl(d *ast.EnumDecl) {
	seen := map[string]source.Span{}

	for _, variant := range d.Variants {
		if prev, ok := seen[variant.Name]; ok {
			r.diags.Add(
				variant.Span(),
				fmt.Sprintf("duplicate enum variant %q, previous declaration at %s", variant.Name, prev.String()),
			)
			continue
		}

		seen[variant.Name] = variant.Span()
	}
}

func (r *Resolver) resolveInterfaceDecl(parent *Scope, d *ast.InterfaceDecl) {
	scope := NewScope(ScopeDecl, parent)

	// Inside an interface requirement, $T is a special compile-time type
	// symbol meaning "the concrete implementing type".
	r.declareGenericSymbol(
		scope,
		"T",
		SymbolGenericType,
		d.Name.Span(),
	)

	for _, req := range d.Requirements {
		for _, param := range req.Params {
			r.resolveType(scope, param.Type)
			if param.HasDefault {
				r.diags.Add(
					param.Name.Span(),
					fmt.Sprintf("interface requirement parameter %q cannot have a default value", param.Name.Name),
				)
			}
		}

		for _, result := range req.Results {
			r.resolveType(scope, result)
		}
	}
}

func (r *Resolver) resolveImplDecl(scope *Scope, d *ast.ImplDecl) {
	typeSym := r.resolveSymbolUse(scope, d.TypeName.Name, d.TypeName.Span())
	if typeSym == nil {
		return
	}

	// The type must be declared in the same scope where the impl appears.
	// This supports package-local impls and local-scope impls.
	if typeSym.Scope != scope {
		r.diags.Add(
			d.TypeName.Span(),
			fmt.Sprintf("impl for %q must be declared in the type's defining scope", d.TypeName.Name),
		)
	}

	for _, iface := range d.Interfaces {
		r.resolveType(scope, iface)
	}
}

func (r *Resolver) resolveTaskDecl(parent *Scope, d *ast.TaskDecl) {
	taskScope := NewScope(ScopeTask, parent)
	taskScope.TaskID = r.nextTaskID
	r.nextTaskID++

	for _, param := range d.Params {
		r.resolveType(taskScope, param.Type)

		if param.HasDefault {
			r.resolveExpr(parent, param.Default)
		}

		r.declareSymbol(taskScope, param.Name.Name, SymbolParam, param.Name.Span(), nil)
	}

	for _, result := range d.Results {
		r.resolveType(taskScope, result)
	}

	r.resolveBlockInScope(d.Body, taskScope, false)
}

func (r *Resolver) resolveBlockInScope(block *ast.BlockStmt, parent *Scope, createChild bool) {
	if block == nil {
		return
	}

	scope := parent
	if createChild {
		scope = NewScope(ScopeBlock, parent)
	}

	for _, stmt := range block.Stmts {
		r.resolveStmt(scope, stmt)
	}
}

func (r *Resolver) resolveStmt(scope *Scope, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		r.declareDeclSymbol(scope, s.Decl)
		r.resolveDecl(scope, s.Decl)

	case *ast.BlockStmt:
		r.resolveBlockInScope(s, scope, true)

	case *ast.ReturnStmt:
		for _, value := range s.Values {
			r.resolveExpr(scope, value)
		}

	case *ast.DeferStmt:
		r.resolveExpr(scope, s.Call)

	case *ast.SealStmt:
		r.resolveExpr(scope, s.Target)

	case *ast.ExprStmt:
		r.resolveExpr(scope, s.Expr)

	case *ast.AssignStmt:
		r.resolveExpr(scope, s.Left)
		r.resolveExpr(scope, s.Right)

	case *ast.VarDeclStmt:
		if s.HasType {
			r.resolveType(scope, s.Type)
		}

		if s.HasValue {
			r.resolveExpr(scope, s.Value)
		}

		r.declareSymbol(scope, s.Name.Name, SymbolVar, s.Name.Span(), nil)

	case *ast.IfStmt:
		r.resolveExpr(scope, s.Cond)
		r.resolveBlockInScope(s.Then, scope, true)

		if s.Else != nil {
			r.resolveStmt(scope, s.Else)
		}

	case *ast.ForStmt:
		forScope := NewScope(ScopeBlock, scope)

		if s.Init != nil {
			r.resolveStmt(forScope, s.Init)
		}

		if s.Cond != nil {
			r.resolveExpr(forScope, s.Cond)
		}

		if s.Post != nil {
			r.resolveStmt(forScope, s.Post)
		}

		r.resolveBlockInScope(s.Body, forScope, true)

	case *ast.SwitchStmt:
		r.resolveSwitchStmt(scope, s)
	}
}

func (r *Resolver) resolveType(scope *Scope, typ ast.Type) {
	switch t := typ.(type) {
	case *ast.NamedType:
		if len(t.Parts) == 0 {
			return
		}

		first := t.Parts[0]

		sym := r.resolveSymbolUse(scope, first.Name, first.Span())
		if sym == nil {
			return
		}

		if len(t.Parts) == 1 {
			if sym.Kind.IsRuntime() {
				r.diags.Add(first.Span(), fmt.Sprintf("%q is a runtime symbol, not a type", first.Name))
			}
			return
		}

		if sym.Kind != SymbolPackage {
			r.diags.Add(first.Span(), fmt.Sprintf("%q is not a package", first.Name))
			return
		}

		if sym.Package == nil {
			r.diags.Add(first.Span(), fmt.Sprintf("package %q has no symbol table", first.Name))
			return
		}

		memberName := t.Parts[1].Name
		member := sym.Package.Symbols[memberName]
		if member == nil {
			r.diags.Add(t.Parts[1].Span(), fmt.Sprintf("package %s has no type %q", first.Name, memberName))
			return
		}

		switch member.Kind {
		case SymbolStruct, SymbolEnum, SymbolUnion, SymbolInterface, SymbolBitSet:
			return
		default:
			r.diags.Add(t.Parts[1].Span(), fmt.Sprintf("package symbol %s.%s is not a type", first.Name, memberName))
			return
		}

	case *ast.PointerType:
		r.resolveType(scope, t.Elem)

	case *ast.ArrayType:
		if !t.Inferred && t.Len != nil {
			r.resolveExpr(scope, t.Len)
		}

		r.resolveType(scope, t.Elem)

	case *ast.GenericType:
		r.resolveType(scope, t.Base)

		for _, arg := range t.Args {
			r.resolveExpr(scope, arg)
		}
	}
}

func (r *Resolver) resolveExpr(scope *Scope, expr ast.Expr) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		r.resolveSymbolUse(scope, e.Name.Name, e.Name.Span())

	case *ast.DotIdentExpr:
		// .None / .ErrorReading need type context.
		// The type checker resolves these later.

	case *ast.IntLitExpr,
		*ast.FloatLitExpr,
		*ast.StringLitExpr,
		*ast.BoolLitExpr,
		*ast.NilLitExpr:
		return

	case *ast.UnaryExpr:
		r.resolveExpr(scope, e.Expr)

	case *ast.BinaryExpr:
		r.resolveExpr(scope, e.Left)
		r.resolveExpr(scope, e.Right)

	case *ast.CallExpr:
		r.resolveExpr(scope, e.Callee)
		for _, arg := range e.Args {
			r.resolveExpr(scope, arg)
		}

	case *ast.SelectorExpr:
		if id, ok := e.Left.(*ast.IdentExpr); ok {
			sym := r.resolveSymbolUse(scope, id.Name.Name, id.Name.Span())
			if sym == nil {
				return
			}

			if sym.Kind == SymbolPackage {
				if sym.Package == nil {
					r.diags.Add(id.Span(), fmt.Sprintf("package %q has no symbol table", id.Name.Name))
					return
				}

				member := sym.Package.Symbols[e.Name.Name]
				if member == nil {
					r.diags.Add(e.Name.Span(), fmt.Sprintf("package %s has no symbol %q", id.Name.Name, e.Name.Name))
					return
				}

				return
			}
		}

		r.resolveExpr(scope, e.Left)

	case *ast.IndexExpr:
		r.resolveExpr(scope, e.Left)
		r.resolveExpr(scope, e.Index)

	case *ast.ArrayLiteralExpr:
		for _, value := range e.Values {
			r.resolveExpr(scope, value)
		}

	case *ast.CompoundLiteralExpr:
		r.resolveType(scope, e.Type)

		for _, field := range e.Fields {
			r.resolveExpr(scope, field.Value)
		}

		for _, value := range e.Values {
			r.resolveExpr(scope, value)
		}
	}
}

func (r *Resolver) resolveSymbolUse(scope *Scope, name string, span source.Span) *Symbol {
	sym := scope.LookupVisible(name)
	if sym == nil {
		r.diags.Add(span, fmt.Sprintf("undefined symbol %q", name))
		return nil
	}

	if sym.Kind.IsRuntime() && sym.TaskID != scope.TaskID {
		r.diags.Add(
			span,
			fmt.Sprintf("nested task cannot capture runtime symbol %q declared at %s", name, sym.Span.String()),
		)
		return sym
	}

	return sym
}

func (r *Resolver) resolveSwitchStmt(scope *Scope, s *ast.SwitchStmt) {
	r.resolveExpr(scope, s.Target)

	for _, swCase := range s.Cases {
		caseScope := NewScope(ScopeBlock, scope)

		switch swCase.Kind {
		case ast.SwitchCaseUnionMember:
			r.resolveType(scope, swCase.UnionMember)

			if s.IsUnionSwitch && s.BindName.Name != "" {
				r.declareSymbol(caseScope, s.BindName.Name, SymbolVar, s.BindName.Span(), s)
			}

		case ast.SwitchCaseNil:
			if s.IsUnionSwitch && s.BindName.Name != "" {
				r.declareSymbol(caseScope, s.BindName.Name, SymbolVar, s.BindName.Span(), s)
			}

		case ast.SwitchCaseExpr:
			r.resolveExpr(scope, swCase.Expr)

		case ast.SwitchCaseEnumVariant,
			ast.SwitchCaseDefault:
			// Resolved by type checker.
		}

		for _, stmt := range swCase.Body {
			r.resolveStmt(caseScope, stmt)
		}
	}
}

func (r *Resolver) declarePackages() {
	for name, pkg := range r.packages {
		if name == "" || pkg == nil {
			continue
		}

		r.global.Symbols[name] = &Symbol{
			Name:    name,
			Kind:    SymbolPackage,
			Scope:   r.global,
			Builtin: true,
			Package: pkg,
		}
	}
}

func ExportPackage(name string, scope *Scope) *PackageInfo {
	info := &PackageInfo{
		Name:    name,
		Symbols: map[string]*PackageSymbol{},
	}

	if scope == nil {
		return info
	}

	for symbolName, sym := range scope.Symbols {
		if sym == nil || sym.Builtin {
			continue
		}

		switch sym.Kind {
		case SymbolConst,
			SymbolTask,
			SymbolStruct,
			SymbolEnum,
			SymbolUnion,
			SymbolInterface,
			SymbolBitSet,
			SymbolOverload:
			info.Symbols[symbolName] = &PackageSymbol{
				Name: symbolName,
				Kind: sym.Kind,
				Span: sym.Span,
			}
		}
	}

	return info
}

func DebugSummary(scope *Scope) string {
	count := 0
	for _, sym := range scope.Symbols {
		if !sym.Builtin {
			count++
		}
	}

	return fmt.Sprintf("global_symbols=%d", count)
}
