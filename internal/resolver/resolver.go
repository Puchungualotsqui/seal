package resolver

import (
	"fmt"

	"seal/internal/ast"
	"seal/internal/builtin"
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
	SymbolDistinct
	SymbolEnum
	SymbolUnion
	SymbolInterface
	SymbolBitSet
	SymbolOverload

	SymbolForeignType
	SymbolForeignTaskABI

	SymbolGenericType
	SymbolGenericEnum
	SymbolGenericUnion
	SymbolGenericTask
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
	case SymbolDistinct:
		return "distinct type"
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
	case SymbolForeignType:
		return "foreign type"
	case SymbolForeignTaskABI:
		return "foreign task ABI"
	case SymbolBuiltinType:
		return "builtin type"
	case SymbolBuiltinTask:
		return "builtin task"
	case SymbolGenericType:
		return "generic type"
	case SymbolGenericEnum:
		return "generic enum"
	case SymbolGenericUnion:
		return "generic union"
	case SymbolGenericTask:
		return "generic task"
	case SymbolGenericValue:
		return "generic value"
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

	// InterfaceRequirements records callable interface requirement names
	// separately from ordinary lexical symbols.
	//
	// Several interfaces may declare the same requirement name, so these
	// must not be represented as ordinary symbols.
	InterfaceRequirements map[string]struct{}
}

func NewScope(kind ScopeKind, parent *Scope) *Scope {
	taskID := 0
	if parent != nil {
		taskID = parent.TaskID
	}

	return &Scope{
		Kind:                  kind,
		Parent:                parent,
		Symbols:               map[string]*Symbol{},
		TaskID:                taskID,
		InterfaceRequirements: map[string]struct{}{},
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

func (s *Scope) DeclareInterfaceRequirement(name string) {
	if s == nil || name == "" {
		return
	}

	s.InterfaceRequirements[name] = struct{}{}
}

func (s *Scope) HasVisibleInterfaceRequirement(name string) bool {
	if name == "" {
		return false
	}

	for scope := s; scope != nil; scope = scope.Parent {
		if _, ok := scope.InterfaceRequirements[name]; ok {
			return true
		}
	}

	return false
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

	// InterfaceRequirements contains exported interface requirement names.
	//
	// They are not ordinary package symbols because calls such as:
	//
	//     Read(reader)
	//
	// are resolved by argument types in the checker.
	InterfaceRequirements map[string]struct{}
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
	for _, typ := range builtin.Types {
		r.declareBuiltin(typ.Name, SymbolBuiltinType)
	}

	for _, task := range builtin.Tasks {
		r.declareBuiltin(task.Name, SymbolBuiltinTask)
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
		if existing.Builtin && existing.Kind == SymbolBuiltinTask {
		} else {
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

func (r *Resolver) declareShortVar(
	scope *Scope,
	name ast.Ident,
	node ast.Node,
) *Symbol {
	if scope == nil ||
		name.Name == "" ||
		name.Name == "_" {
		return nil
	}

	/*
		A name already declared in this exact scope is reused by :=.

		    valid := false
		    value, valid := Pair()

		The checker determines whether the existing symbol is assignable.
	*/
	if existing := scope.LookupLocal(
		name.Name,
	); existing != nil {
		return existing
	}

	/*
		Short declarations intentionally use current-scope lookup rather than
		visible-scope lookup.

		    valid := false

		    {
		        value, valid := Pair()
		    }

		The inner valid is a new variable that shadows the outer valid.
	*/
	sym := &Symbol{
		Name:   name.Name,
		Kind:   SymbolVar,
		Span:   name.Span(),
		Node:   node,
		Scope:  scope,
		TaskID: scope.TaskID,
	}

	scope.Symbols[name.Name] = sym

	return sym
}

func genericParamSymbolKind(category ast.GenericParamCategory) SymbolKind {
	switch category {
	case ast.GenericParamType:
		return SymbolGenericType

	case ast.GenericParamEnum:
		return SymbolGenericEnum

	case ast.GenericParamUnion:
		return SymbolGenericUnion

	case ast.GenericParamTask:
		return SymbolGenericTask

	case ast.GenericParamInt,
		ast.GenericParamBool,
		ast.GenericParamString,
		ast.GenericParamValue:
		return SymbolGenericValue

	default:
		return SymbolInvalid
	}
}

func (r *Resolver) declareGenericParams(scope *Scope, params []ast.GenericParam) {
	for _, param := range params {
		kind := genericParamSymbolKind(param.Category)
		if kind == SymbolInvalid {
			r.diags.Add(param.Span(), fmt.Sprintf("invalid generic parameter category for %q", param.Name.Name))
			continue
		}

		r.declareSymbol(scope, param.Name.Name, kind, param.Name.Span(), nil)
	}
}

func (r *Resolver) resolveGenericParams(scope *Scope, params []ast.GenericParam) {
	for _, param := range params {
		if param.Type != nil {
			r.resolveType(scope, param.Type)
		}

		for _, constraint := range param.Constraints {
			r.resolveGenericConstraint(scope, constraint)
		}
	}
}

func (r *Resolver) resolveGenericConstraint(scope *Scope, constraint ast.GenericConstraint) {
	switch c := constraint.(type) {
	case *ast.GenericExprConstraint:
		r.resolveExpr(scope, c.Expr)

	case *ast.GenericFieldConstraint:
		if c.HasType && c.Type != nil {
			r.resolveType(scope, c.Type)
		}

	case *ast.GenericImplConstraint:
		r.resolveType(scope, c.Interface)

	case *ast.GenericEnumVariantConstraint:
		// Variant names are checked later by the checker.

	case *ast.GenericUnionMemberConstraint:
		r.resolveType(scope, c.Member)

	case *ast.GenericTaskConstraint:
		for _, param := range c.Params {
			r.resolveType(scope, param)
		}

		for _, result := range c.Results {
			r.resolveType(scope, result)
		}
	}
}

func (r *Resolver) resolveGenericArg(scope *Scope, arg ast.GenericArg) {
	switch arg.Kind {
	case ast.GenericArgType:
		if arg.Type != nil {
			r.resolveType(scope, arg.Type)
		}

	case ast.GenericArgExpr:
		if arg.Expr != nil {
			r.resolveExpr(scope, arg.Expr)
		}
	}
}

func isTypeSymbolKind(kind SymbolKind) bool {
	switch kind {
	case SymbolStruct,
		SymbolDistinct,
		SymbolEnum,
		SymbolUnion,
		SymbolInterface,
		SymbolBitSet,
		SymbolForeignType,
		SymbolGenericType,
		SymbolGenericEnum,
		SymbolGenericUnion,
		SymbolBuiltinType:
		return true

	default:
		return false
	}
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
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolConst,
			d.Name.Span(),
			d,
		)

	case *ast.ForeignValueDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolConst,
			d.Name.Span(),
			d,
		)

	case *ast.StructDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolStruct,
			d.Name.Span(),
			d,
		)

	case *ast.TaskDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolTask,
			d.Name.Span(),
			d,
		)

	case *ast.ForeignTypeDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolForeignType,
			d.Name.Span(),
			d,
		)

	case *ast.ForeignTaskDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolForeignTaskABI,
			d.Name.Span(),
			d,
		)

	case *ast.EnumDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolEnum,
			d.Name.Span(),
			d,
		)

	case *ast.UnionDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolUnion,
			d.Name.Span(),
			d,
		)

	case *ast.InterfaceDecl:
		sym := r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolInterface,
			d.Name.Span(),
			d,
		)
		if sym == nil {
			return
		}

		for _, requirement := range d.Requirements {
			if requirement == nil {
				continue
			}

			scope.DeclareInterfaceRequirement(
				requirement.Name.Name,
			)
		}

	case *ast.OverloadDecl:
		r.declareSymbol(
			scope,
			d.Name,
			SymbolOverload,
			d.Span(),
			d,
		)

	case *ast.DistinctDecl:
		r.declareSymbol(
			scope,
			d.Name.Name,
			SymbolDistinct,
			d.Name.Span(),
			d,
		)

	case *ast.DirectiveDecl:
		// c :: @c_import { ... } is codegen metadata, not a visible
		// Seal symbol.
		return

	case *ast.ImplDecl:
		// impl does not introduce a new symbol.
		return
	}
}

func (r *Resolver) resolveDecl(scope *Scope, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		r.resolveExpr(scope, d.Value)

	case *ast.ForeignValueDecl:
		if d.Type != nil {
			r.resolveType(scope, d.Type)
		}

	case *ast.ForeignTypeDecl,
		*ast.ForeignTaskDecl:
		// Their C token sequences are opaque to the resolver.
		return

	case *ast.StructDecl:
		r.resolveStructDecl(scope, d)

	case *ast.TaskDecl:
		r.resolveTaskDecl(scope, d)

	case *ast.DistinctDecl:
		r.resolveType(scope, d.Underlying)

	case *ast.EnumDecl:
		r.resolveEnumDecl(scope, d)

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
			r.resolveSymbolUse(
				scope,
				name.Name,
				name.Span(),
			)
		}

	case *ast.DirectiveDecl:
		return
	}
}

func (r *Resolver) resolveStructDecl(parent *Scope, d *ast.StructDecl) {
	scope := NewScope(ScopeDecl, parent)

	r.declareGenericParams(scope, d.GenericParams)
	r.resolveGenericParams(scope, d.GenericParams)

	for _, field := range d.Fields {
		r.resolveType(scope, field.Type)
	}
}

func (r *Resolver) resolveEnumDecl(scope *Scope, d *ast.EnumDecl) {
	if d == nil {
		return
	}

	if d.Underlying != nil {
		r.resolveType(scope, d.Underlying)
	}

	seen := map[string]source.Span{}

	for _, variant := range d.Variants {
		if prev, ok := seen[variant.Name]; ok {
			r.diags.Add(
				variant.Span(),
				fmt.Sprintf(
					"duplicate enum variant %q, previous declaration at %s",
					variant.Name,
					prev.String(),
				),
			)
			continue
		}

		seen[variant.Name] = variant.Span()
	}
}

func (r *Resolver) resolveInterfaceDecl(parent *Scope, d *ast.InterfaceDecl) {
	scope := NewScope(ScopeDecl, parent)

	r.declareGenericParams(scope, d.GenericParams)
	r.resolveGenericParams(scope, d.GenericParams)

	seenRequirements := map[string]source.Span{}

	for _, req := range d.Requirements {
		if req == nil {
			continue
		}

		if previous, exists := seenRequirements[req.Name.Name]; exists {
			r.diags.Add(
				req.Name.Span(),
				fmt.Sprintf(
					"duplicate interface requirement %q, previous declaration at %s",
					req.Name.Name,
					previous.String(),
				),
			)
		} else {
			seenRequirements[req.Name.Name] = req.Name.Span()
		}

		seenParams := map[string]source.Span{}

		for _, param := range req.Params {
			if param.Name.Name != "" && param.Name.Name != "_" {
				if previous, exists := seenParams[param.Name.Name]; exists {
					r.diags.Add(
						param.Name.Span(),
						fmt.Sprintf(
							"duplicate interface requirement parameter %q, previous declaration at %s",
							param.Name.Name,
							previous.String(),
						),
					)
				} else {
					seenParams[param.Name.Name] = param.Name.Span()
				}
			}

			if param.Type != nil {
				r.resolveInterfaceRequirementType(scope, param.Type)
			}

			if param.HasDefault {
				r.diags.Add(
					param.Name.Span(),
					fmt.Sprintf(
						"interface requirement parameter %q cannot have a default value",
						param.Name.Name,
					),
				)
			}
		}

		for _, result := range req.Results {
			if result != nil {
				r.resolveInterfaceRequirementType(scope, result)
			}
		}
	}
}

func (r *Resolver) resolveInterfaceRequirementType(scope *Scope, typ ast.Type) {
	r.resolveTypeWithInterfaceSelf(scope, typ, true)
}

func (r *Resolver) resolveImplDecl(parent *Scope, d *ast.ImplDecl) {
	implScope := NewScope(ScopeDecl, parent)

	r.declareGenericParams(implScope, d.GenericParams)
	r.resolveGenericParams(implScope, d.GenericParams)

	if d.Interface != nil {
		r.resolveType(implScope, d.Interface)
	}

	if d.Target != nil {
		r.resolveType(implScope, d.Target)
	}

	if len(d.UsingPath) > 0 {
		if len(d.Entries) > 0 {
			r.diags.Add(
				d.Span(),
				"delegated impl cannot contain manual impl entries",
			)
		}

		seenEmpty := false
		for _, part := range d.UsingPath {
			if part.Name == "" {
				seenEmpty = true
				break
			}
		}

		if seenEmpty {
			r.diags.Add(
				d.Span(),
				"using path contains an empty field name",
			)
		}

		// Field names in a using path are not normal lexical symbols.
		// Their existence and types are resolved by the checker from Target.
		return
	}

	seenEntries := map[string]source.Span{}

	for i := range d.Entries {
		entry := &d.Entries[i]

		if previous, exists := seenEntries[entry.Name.Name]; exists {
			r.diags.Add(
				entry.Name.Span(),
				fmt.Sprintf(
					"duplicate impl entry %q, previous declaration at %s",
					entry.Name.Name,
					previous.String(),
				),
			)
		} else {
			seenEntries[entry.Name.Name] = entry.Name.Span()
		}

		if entry.Task != nil && entry.Alias != nil {
			r.diags.Add(
				entry.Span(),
				fmt.Sprintf(
					"impl entry %q cannot contain both a task and an alias",
					entry.Name.Name,
				),
			)
			continue
		}

		if entry.Task == nil && entry.Alias == nil {
			r.diags.Add(
				entry.Span(),
				fmt.Sprintf(
					"impl entry %q has no implementation",
					entry.Name.Name,
				),
			)
			continue
		}

		if entry.Task != nil {
			r.resolveTaskDecl(implScope, entry.Task)
		}

		if entry.Alias != nil {
			r.resolveExpr(implScope, entry.Alias)
		}
	}
}

func (r *Resolver) resolveTaskDecl(
	parent *Scope,
	d *ast.TaskDecl,
) {
	genericScope := NewScope(
		ScopeDecl,
		parent,
	)

	r.declareGenericParams(
		genericScope,
		d.GenericParams,
	)

	r.resolveGenericParams(
		genericScope,
		d.GenericParams,
	)

	if d.ForeignABI != nil {
		r.resolveExpr(
			genericScope,
			d.ForeignABI,
		)
	}

	taskScope := NewScope(
		ScopeTask,
		genericScope,
	)

	taskScope.TaskID = r.nextTaskID
	r.nextTaskID++

	for _, param := range d.Params {
		r.resolveType(
			taskScope,
			param.Type,
		)

		if param.HasDefault {
			r.resolveExpr(
				genericScope,
				param.Default,
			)
		}

		r.declareSymbol(
			taskScope,
			param.Name.Name,
			SymbolParam,
			param.Name.Span(),
			nil,
		)
	}

	for _, result := range d.Results {
		r.resolveType(
			taskScope,
			result,
		)
	}

	if d.Body != nil {
		r.resolveBlockInScope(
			d.Body,
			taskScope,
			false,
		)
	}
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

	case *ast.BreakStmt,
		*ast.ContinueStmt:
		return

	case *ast.DeferStmt:
		if s.Call != nil {
			r.resolveExpr(scope, s.Call)
		}

		if s.Body != nil {
			// A deferred block has its own local scope, but it may reference
			// symbols visible at the defer declaration.
			r.resolveBlockInScope(s.Body, scope, true)
		}

	case *ast.SealStmt:
		r.resolveExpr(scope, s.Target)

	case *ast.ExprStmt:
		r.resolveExpr(scope, s.Expr)

	case *ast.MultiVarDeclStmt:
		/*
			The RHS is resolved before any newly declared names become visible.

			    left, right := Make(left)

			The left used by Make(left) refers to the previously visible symbol,
			not the left being declared by this statement.
		*/
		r.resolveExpr(
			scope,
			s.Value,
		)

		for _, name := range s.Names {
			r.declareShortVar(
				scope,
				name,
				s,
			)
		}

	case *ast.MultiAssignStmt:
		/*
			Multi-assignment introduces no symbols. Resolve each non-discard
			target as an ordinary symbol use, then resolve the RHS call.
		*/
		for _, name := range s.Names {
			if name.Name == "_" {
				continue
			}

			r.resolveSymbolUse(
				scope,
				name.Name,
				name.Span(),
			)
		}

		r.resolveExpr(
			scope,
			s.Value,
		)

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

		r.declareSymbol(
			scope,
			s.Name.Name,
			SymbolVar,
			s.Name.Span(),
			nil,
		)

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
	r.resolveTypeWithInterfaceSelf(scope, typ, false)
}

func (r *Resolver) resolveTypeWithInterfaceSelf(
	scope *Scope,
	typ ast.Type,
	allowInterfaceSelf bool,
) {
	switch t := typ.(type) {
	case *ast.InterfaceSelfType:
		if !allowInterfaceSelf {
			r.diags.Add(
				t.Span(),
				`"self" type is only available inside interface requirements`,
			)
		}

	case *ast.NamedType:
		if len(t.Parts) == 0 {
			return
		}

		first := t.Parts[0]

		sym := r.resolveSymbolUse(
			scope,
			first.Name,
			first.Span(),
		)
		if sym == nil {
			return
		}

		if len(t.Parts) == 1 {
			if isTypeSymbolKind(sym.Kind) {
				return
			}

			if sym.Kind.IsRuntime() {
				r.diags.Add(
					first.Span(),
					fmt.Sprintf(
						"%q is a runtime symbol, not a type",
						first.Name,
					),
				)
			} else {
				r.diags.Add(
					first.Span(),
					fmt.Sprintf(
						"%q is not a type",
						first.Name,
					),
				)
			}

			return
		}

		if sym.Kind != SymbolPackage {
			r.diags.Add(
				first.Span(),
				fmt.Sprintf(
					"%q is not a package",
					first.Name,
				),
			)
			return
		}

		if sym.Package == nil {
			r.diags.Add(
				first.Span(),
				fmt.Sprintf(
					"package %q has no symbol table",
					first.Name,
				),
			)
			return
		}

		if len(t.Parts) != 2 {
			r.diags.Add(
				t.Span(),
				"package-qualified types currently support exactly one package and one member",
			)
			return
		}

		memberName := t.Parts[1].Name
		member := sym.Package.Symbols[memberName]

		if member == nil {
			r.diags.Add(
				t.Parts[1].Span(),
				fmt.Sprintf(
					"package %s has no type %q",
					first.Name,
					memberName,
				),
			)
			return
		}

		if isTypeSymbolKind(member.Kind) {
			return
		}

		r.diags.Add(
			t.Parts[1].Span(),
			fmt.Sprintf(
				"package symbol %s.%s is not a type",
				first.Name,
				memberName,
			),
		)

	case *ast.PointerType:
		r.resolveTypeWithInterfaceSelf(
			scope,
			t.Elem,
			allowInterfaceSelf,
		)

	case *ast.GenericType:
		r.resolveTypeWithInterfaceSelf(
			scope,
			t.Base,
			allowInterfaceSelf,
		)

		for _, arg := range t.Args {
			r.resolveGenericArgWithInterfaceSelf(
				scope,
				arg,
				allowInterfaceSelf,
			)
		}

	case *ast.InlineArrayType:
		r.resolveTypeWithInterfaceSelf(
			scope,
			t.Elem,
			allowInterfaceSelf,
		)

		r.resolveExpr(
			scope,
			t.Length,
		)
	}
}

func (r *Resolver) resolveGenericArgWithInterfaceSelf(
	scope *Scope,
	arg ast.GenericArg,
	allowInterfaceSelf bool,
) {
	switch arg.Kind {
	case ast.GenericArgType:
		if arg.Type != nil {
			r.resolveTypeWithInterfaceSelf(
				scope,
				arg.Type,
				allowInterfaceSelf,
			)
		}

	case ast.GenericArgExpr:
		if arg.Expr != nil {
			r.resolveExpr(scope, arg.Expr)
		}
	}
}

func (r *Resolver) resolveCallCallee(scope *Scope, callee ast.Expr) {
	if callee == nil {
		return
	}

	// Interface requirements are not ordinary lexical symbols.
	//
	//     Read(reader)
	//
	// If no ordinary symbol named Read exists but Read is a visible interface
	// requirement, defer candidate selection to the checker.
	if id, ok := callee.(*ast.IdentExpr); ok {
		if scope.LookupVisible(id.Name.Name) == nil &&
			scope.HasVisibleInterfaceRequirement(id.Name.Name) {
			return
		}
	}

	r.resolveExpr(scope, callee)
}

func (r *Resolver) resolveExpr(
	scope *Scope,
	expr ast.Expr,
) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		r.resolveSymbolUse(
			scope,
			e.Name.Name,
			e.Name.Span(),
		)

	case *ast.DotIdentExpr:
		// .None / .ErrorReading need type context.
		// The type checker resolves these later.

	case *ast.IntLitExpr,
		*ast.FloatLitExpr,
		*ast.StringLitExpr,
		*ast.CStringLitExpr,
		*ast.CharLitExpr,
		*ast.BoolLitExpr,
		*ast.NilLitExpr:
		return

	case *ast.UnaryExpr:
		r.resolveExpr(
			scope,
			e.Expr,
		)

	case *ast.BinaryExpr:
		r.resolveExpr(
			scope,
			e.Left,
		)

		r.resolveExpr(
			scope,
			e.Right,
		)

	case *ast.CallExpr:
		r.resolveCallCallee(
			scope,
			e.Callee,
		)

		for _, arg := range e.Args {
			r.resolveExpr(
				scope,
				arg,
			)
		}

	case *ast.GenericExpr:
		r.resolveExpr(
			scope,
			e.Base,
		)

		for _, arg := range e.Args {
			r.resolveGenericArg(
				scope,
				arg,
			)
		}

	case *ast.TaskPointerExpr:
		r.resolveExpr(
			scope,
			e.Task,
		)

	case *ast.InlineArrayExpr:
		r.resolveType(
			scope,
			e.Elem,
		)

		r.resolveExpr(
			scope,
			e.Length,
		)

		for _, value := range e.Values {
			r.resolveExpr(
				scope,
				value,
			)
		}

	case *ast.SpreadExpr:
		r.resolveExpr(
			scope,
			e.Expr,
		)

	case *ast.SelectorExpr:
		if id, ok := e.Left.(*ast.IdentExpr); ok {
			sym := r.resolveSymbolUse(
				scope,
				id.Name.Name,
				id.Name.Span(),
			)
			if sym == nil {
				return
			}

			if sym.Kind == SymbolPackage {
				if sym.Package == nil {
					r.diags.Add(
						id.Span(),
						fmt.Sprintf(
							"package %q has no symbol table",
							id.Name.Name,
						),
					)
					return
				}

				member :=
					sym.Package.Symbols[e.Name.Name]

				if member == nil {
					r.diags.Add(
						e.Name.Span(),
						fmt.Sprintf(
							"package %s has no symbol %q",
							id.Name.Name,
							e.Name.Name,
						),
					)
					return
				}

				return
			}
		}

		r.resolveExpr(
			scope,
			e.Left,
		)

	case *ast.IndexExpr:
		r.resolveExpr(
			scope,
			e.Left,
		)

		r.resolveExpr(
			scope,
			e.Index,
		)

	case *ast.CompoundLiteralExpr:
		r.resolveType(
			scope,
			e.Type,
		)

		for _, field := range e.Fields {
			r.resolveExpr(
				scope,
				field.Value,
			)
		}

		for _, value := range e.Values {
			r.resolveExpr(
				scope,
				value,
			)
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

		for requirementName := range pkg.InterfaceRequirements {
			r.global.DeclareInterfaceRequirement(requirementName)
		}
	}
}

func ExportPackage(name string, scope *Scope) *PackageInfo {
	info := &PackageInfo{
		Name:                  name,
		Symbols:               map[string]*PackageSymbol{},
		InterfaceRequirements: map[string]struct{}{},
	}

	if scope == nil {
		return info
	}

	for symbolName, sym := range scope.Symbols {
		if sym == nil || sym.Builtin {
			continue
		}

		if sym.Kind == SymbolInterface {
			if decl, ok := sym.Node.(*ast.InterfaceDecl); ok &&
				decl != nil {
				for _, requirement := range decl.Requirements {
					if requirement == nil ||
						requirement.Name.Name == "" {
						continue
					}

					info.InterfaceRequirements[requirement.Name.Name] = struct{}{}
				}
			}
		}

		switch sym.Kind {
		case SymbolConst,
			SymbolTask,
			SymbolStruct,
			SymbolDistinct,
			SymbolEnum,
			SymbolUnion,
			SymbolInterface,
			SymbolBitSet,
			SymbolOverload,
			SymbolForeignType,
			SymbolForeignTaskABI:
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
