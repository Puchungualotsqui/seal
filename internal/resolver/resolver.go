package resolver

import (
	"fmt"
	"sort"

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
	return k == SymbolVar ||
		k == SymbolParam
}

func (k SymbolKind) IsCompileTime() bool {
	return !k.IsRuntime()
}

func (k SymbolKind) IsType() bool {
	return isTypeSymbolKind(k)
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

func (k ScopeKind) String() string {
	switch k {
	case ScopeGlobal:
		return "global"

	case ScopeBlock:
		return "block"

	case ScopeTask:
		return "task"

	case ScopeDecl:
		return "declaration"

	default:
		return "scope"
	}
}

type Scope struct {
	Kind    ScopeKind
	Parent  *Scope
	Symbols map[string]*Symbol
	TaskID  int

	/*
		InterfaceRequirements records callable interface requirement names
		separately from ordinary lexical symbols.

		Several interfaces may declare the same requirement name, so these
		must not be represented as ordinary symbols.
	*/
	InterfaceRequirements map[string]struct{}
}

func NewScope(
	kind ScopeKind,
	parent *Scope,
) *Scope {
	taskID := 0

	if parent != nil {
		taskID = parent.TaskID
	}

	return &Scope{
		Kind: kind,

		Parent: parent,

		Symbols: map[string]*Symbol{},

		TaskID: taskID,

		InterfaceRequirements: map[string]struct{}{},
	}
}

func (s *Scope) LookupLocal(
	name string,
) *Symbol {
	if s == nil {
		return nil
	}

	return s.Symbols[name]
}

func (s *Scope) LookupVisible(
	name string,
) *Symbol {
	for scope := s; scope != nil; scope = scope.Parent {
		if symbol :=
			scope.LookupLocal(
				name,
			); symbol != nil {
			return symbol
		}
	}

	return nil
}

/*
VisibleSymbols returns the symbols visible from this scope.

When a name exists in more than one scope, only the nearest declaration is
returned. Results are sorted by name and then by symbol kind so completion
results remain deterministic.
*/
func (s *Scope) VisibleSymbols() []*Symbol {
	if s == nil {
		return nil
	}

	seen :=
		map[string]bool{}

	var symbols []*Symbol

	for scope := s; scope != nil; scope = scope.Parent {
		for name, symbol := range scope.Symbols {
			if symbol == nil ||
				seen[name] {
				continue
			}

			seen[name] = true

			symbols = append(
				symbols,
				symbol,
			)
		}
	}

	sort.Slice(
		symbols,
		func(
			left int,
			right int,
		) bool {
			if symbols[left].Name !=
				symbols[right].Name {
				return symbols[left].Name <
					symbols[right].Name
			}

			return symbols[left].Kind <
				symbols[right].Kind
		},
	)

	return symbols
}

func (s *Scope) DeclareInterfaceRequirement(
	name string,
) {
	if s == nil ||
		name == "" {
		return
	}

	s.InterfaceRequirements[name] =
		struct{}{}
}

func (s *Scope) HasVisibleInterfaceRequirement(
	name string,
) bool {
	if name == "" {
		return false
	}

	for scope := s; scope != nil; scope = scope.Parent {
		if _, found :=
			scope.InterfaceRequirements[name]; found {
			return true
		}
	}

	return false
}

/*
VisibleInterfaceRequirements returns all interface requirement names visible
from this scope in deterministic order.
*/
func (s *Scope) VisibleInterfaceRequirements() []string {
	if s == nil {
		return nil
	}

	seen :=
		map[string]bool{}

	var names []string

	for scope := s; scope != nil; scope = scope.Parent {
		for name := range scope.InterfaceRequirements {
			if name == "" ||
				seen[name] {
				continue
			}

			seen[name] = true

			names = append(
				names,
				name,
			)
		}
	}

	sort.Strings(
		names,
	)

	return names
}

/*
Definition identifies one source declaration introduced into a lexical scope.

Symbol points to the exact resolver symbol. It is valid for the lifetime of the
resolver analysis snapshot.
*/
type Definition struct {
	Name string
	Kind SymbolKind
	Span source.Span

	Symbol *Symbol
}

/*
ResolvedUse connects a symbol use to its declaration.

Definition may have no source file for compiler builtins and synthetic package
symbols. Builtin identifies compiler-provided symbols.

PackageName is set for package symbols and package-qualified members.
*/
type ResolvedUse struct {
	/*
		Use is the exact source occurrence of the identifier.

		Definition is the declaration span that this occurrence resolves to.
	*/
	Use        source.Span
	Definition source.Span

	Name string
	Kind SymbolKind

	Builtin     bool
	PackageName string

	Symbol *Symbol
}

/*
ScopeRegion associates a source range with the lexical scope active in that
range.

Nested regions may overlap. ScopeAt selects the smallest and deepest matching
region.
*/
type ScopeRegion struct {
	Span  source.Span
	Scope *Scope
}

/*
SemanticInfo contains resolver-side information needed by editor tooling.

The AST and checker still provide type-specific semantic information. This
structure handles lexical declarations, lexical uses, package member uses, and
scope lookup.
*/
type SemanticInfo struct {
	GlobalScope *Scope

	Definitions  []Definition
	Uses         []ResolvedUse
	ScopeRegions []ScopeRegion
}

func (i *SemanticInfo) ScopeAt(
	file *source.File,
	offset int,
) *Scope {
	if i == nil {
		return nil
	}

	var best *Scope

	bestWidth := 0
	bestDepth := -1
	found := false

	for _, region := range i.ScopeRegions {
		if region.Scope == nil ||
			!spanContainsOffset(
				region.Span,
				file,
				offset,
			) {
			continue
		}

		width :=
			spanWidth(
				region.Span,
			)

		depth :=
			scopeDepth(
				region.Scope,
			)

		if !found ||
			width < bestWidth ||
			(width == bestWidth &&
				depth > bestDepth) {
			best =
				region.Scope

			bestWidth = width
			bestDepth = depth
			found = true
		}
	}

	if best != nil {
		return best
	}

	return i.GlobalScope
}

func (i *SemanticInfo) UseAt(
	file *source.File,
	offset int,
) *ResolvedUse {
	if i == nil {
		return nil
	}

	bestIndex := -1
	bestWidth := 0

	for index := range i.Uses {
		use :=
			&i.Uses[index]

		if !spanContainsOffset(
			use.Use,
			file,
			offset,
		) {
			continue
		}

		width :=
			spanWidth(
				use.Use,
			)

		if bestIndex < 0 ||
			width < bestWidth {
			bestIndex = index
			bestWidth = width
		}
	}

	if bestIndex < 0 {
		return nil
	}

	return &i.Uses[bestIndex]
}

func (i *SemanticInfo) DefinitionAt(
	file *source.File,
	offset int,
) *Definition {
	if i == nil {
		return nil
	}

	bestIndex := -1
	bestWidth := 0

	for index := range i.Definitions {
		definition :=
			&i.Definitions[index]

		if !spanContainsOffset(
			definition.Span,
			file,
			offset,
		) {
			continue
		}

		width :=
			spanWidth(
				definition.Span,
			)

		if bestIndex < 0 ||
			width < bestWidth {
			bestIndex = index
			bestWidth = width
		}
	}

	if bestIndex < 0 {
		return nil
	}

	return &i.Definitions[bestIndex]
}

/*
UsesOfDefinition returns every source occurrence resolved to definition.

The declaration itself is not included. LSP references can include the
declaration separately according to ReferenceContext.IncludeDeclaration.
*/
func (i *SemanticInfo) UsesOfDefinition(
	definition source.Span,
) []source.Span {
	if i == nil ||
		definition.File == nil {
		return nil
	}

	var uses []source.Span

	for index := range i.Uses {
		use :=
			&i.Uses[index]

		if use.Use.File == nil ||
			use.Definition.File == nil {
			continue
		}

		if !sameSourceFile(
			use.Definition.File,
			definition.File,
		) {
			continue
		}

		if use.Definition.Start !=
			definition.Start ||
			use.Definition.End !=
				definition.End {
			continue
		}

		uses =
			append(
				uses,
				use.Use,
			)
	}

	sort.SliceStable(
		uses,
		func(
			left int,
			right int,
		) bool {
			leftPath :=
				uses[left].File.Path

			rightPath :=
				uses[right].File.Path

			if leftPath != rightPath {
				return leftPath <
					rightPath
			}

			if uses[left].Start !=
				uses[right].Start {
				return uses[left].Start <
					uses[right].Start
			}

			return uses[left].End <
				uses[right].End
		},
	)

	return uses
}

func sameSourceFile(
	left *source.File,
	right *source.File,
) bool {
	if left == nil ||
		right == nil {
		return false
	}

	if left == right {
		return true
	}

	return left.Path != "" &&
		left.Path == right.Path
}

func spanContainsOffset(
	span source.Span,
	file *source.File,
	offset int,
) bool {
	if span.File == nil ||
		file == nil ||
		!sameSourceFile(
			span.File,
			file,
		) {
		return false
	}

	start := span.Start
	end := span.End

	if start < 0 {
		start = 0
	}

	if end < start {
		end = start
	}

	if offset < 0 {
		offset = 0
	}

	if offset > len(file.Text) {
		offset = len(file.Text)
	}

	return offset >= start &&
		offset <= end
}

func spanWidth(
	span source.Span,
) int {
	if span.End <= span.Start {
		return 0
	}

	return span.End -
		span.Start
}

func scopeDepth(
	scope *Scope,
) int {
	depth := 0

	for current := scope; current != nil; current = current.Parent {
		depth++
	}

	return depth
}

type Resolver struct {
	diags      *diag.Reporter
	global     *Scope
	nextTaskID int
	packages   map[string]*PackageInfo

	semantic SemanticInfo
}

func New(
	diags *diag.Reporter,
) *Resolver {
	return NewWithPackages(
		diags,
		nil,
	)
}

func NewWithPackages(
	diags *diag.Reporter,
	packages map[string]*PackageInfo,
) *Resolver {
	global :=
		NewScope(
			ScopeGlobal,
			nil,
		)

	resolver :=
		&Resolver{
			diags:      diags,
			global:     global,
			nextTaskID: 1,
			packages:   packages,

			semantic: SemanticInfo{
				GlobalScope: global,
			},
		}

	resolver.declareBuiltins()
	resolver.declarePackages()

	return resolver
}

type PackageInfo struct {
	Name    string
	Symbols map[string]*PackageSymbol

	/*
		InterfaceRequirements contains exported interface requirement names.

		They are not ordinary package symbols because calls such as:

		    Read(reader)

		are resolved by argument types in the checker.
	*/
	InterfaceRequirements map[string]struct{}
}

type PackageSymbol struct {
	Name string
	Kind SymbolKind
	Span source.Span
}

func (r *Resolver) GlobalScope() *Scope {
	if r == nil {
		return nil
	}

	return r.global
}

/*
SemanticInfo returns a copy of the resolver semantic snapshot.

The contained Scope and Symbol pointers remain owned by the resolver snapshot.
The returned slices may be modified independently.
*/
func (r *Resolver) SemanticInfo() SemanticInfo {
	if r == nil {
		return SemanticInfo{}
	}

	return SemanticInfo{
		GlobalScope: r.semantic.GlobalScope,

		Definitions: append(
			[]Definition(nil),
			r.semantic.Definitions...,
		),

		Uses: append(
			[]ResolvedUse(nil),
			r.semantic.Uses...,
		),

		ScopeRegions: append(
			[]ScopeRegion(nil),
			r.semantic.ScopeRegions...,
		),
	}
}

func (r *Resolver) ResolveFile(
	file *ast.File,
) *Scope {
	if r == nil {
		return nil
	}

	if file == nil {
		return r.global
	}

	for _, declaration := range file.Decls {
		r.declareDeclSymbol(
			r.global,
			declaration,
		)
	}

	for _, declaration := range file.Decls {
		r.resolveDecl(
			r.global,
			declaration,
		)
	}

	return r.global
}

func (r *Resolver) recordDefinition(
	symbol *Symbol,
) {
	if r == nil ||
		symbol == nil ||
		symbol.Name == "" ||
		symbol.Builtin ||
		symbol.Span.File == nil {
		return
	}

	r.semantic.Definitions =
		append(
			r.semantic.Definitions,
			Definition{
				Name: symbol.Name,
				Kind: symbol.Kind,
				Span: symbol.Span,

				Symbol: symbol,
			},
		)
}

func (r *Resolver) recordSymbolUse(
	use source.Span,
	symbol *Symbol,
) {
	if r == nil ||
		symbol == nil ||
		use.File == nil {
		return
	}

	packageName := ""

	if symbol.Kind ==
		SymbolPackage &&
		symbol.Package != nil {
		packageName =
			symbol.Package.Name
	}

	r.semantic.Uses =
		append(
			r.semantic.Uses,
			ResolvedUse{
				Use:        use,
				Definition: symbol.Span,

				Name: symbol.Name,
				Kind: symbol.Kind,

				Builtin:     symbol.Builtin,
				PackageName: packageName,

				Symbol: symbol,
			},
		)
}

func (r *Resolver) recordPackageSymbolUse(
	use source.Span,
	pkg *PackageInfo,
	symbol *PackageSymbol,
) {
	if r == nil ||
		symbol == nil ||
		use.File == nil {
		return
	}

	packageName := ""

	if pkg != nil {
		packageName = pkg.Name
	}

	r.semantic.Uses =
		append(
			r.semantic.Uses,
			ResolvedUse{
				Use:        use,
				Definition: symbol.Span,

				Name: symbol.Name,
				Kind: symbol.Kind,

				PackageName: packageName,
			},
		)
}

func (r *Resolver) recordScopeRegion(
	span source.Span,
	scope *Scope,
) {
	if r == nil ||
		scope == nil ||
		span.File == nil {
		return
	}

	if span.End < span.Start {
		span.End = span.Start
	}

	for _, existing := range r.semantic.ScopeRegions {
		if existing.Scope == scope &&
			existing.Span.File == span.File &&
			existing.Span.Start == span.Start &&
			existing.Span.End == span.End {
			return
		}
	}

	r.semantic.ScopeRegions =
		append(
			r.semantic.ScopeRegions,
			ScopeRegion{
				Span:  span,
				Scope: scope,
			},
		)
}

func (r *Resolver) declareBuiltins() {
	for _, typ := range builtin.Types {
		r.declareBuiltin(
			typ.Name,
			SymbolBuiltinType,
		)
	}

	for _, task := range builtin.Tasks {
		r.declareBuiltin(
			task.Name,
			SymbolBuiltinTask,
		)
	}
}

func (r *Resolver) declareBuiltin(
	name string,
	kind SymbolKind,
) {
	r.global.Symbols[name] =
		&Symbol{
			Name:    name,
			Kind:    kind,
			Scope:   r.global,
			Builtin: true,
		}
}

func (r *Resolver) declareSymbol(
	scope *Scope,
	name string,
	kind SymbolKind,
	span source.Span,
	node ast.Node,
) *Symbol {
	if scope == nil ||
		name == "" {
		return nil
	}

	if existing :=
		scope.LookupVisible(
			name,
		); existing != nil {
		if existing.Builtin &&
			existing.Kind ==
				SymbolBuiltinTask {
			/*
				User tasks may override builtin task names.
			*/
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

	symbol :=
		&Symbol{
			Name:   name,
			Kind:   kind,
			Span:   span,
			Node:   node,
			Scope:  scope,
			TaskID: scope.TaskID,
		}

	scope.Symbols[name] =
		symbol

	r.recordDefinition(
		symbol,
	)

	return symbol
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
	if existing :=
		scope.LookupLocal(
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
	symbol :=
		&Symbol{
			Name:   name.Name,
			Kind:   SymbolVar,
			Span:   name.Span(),
			Node:   node,
			Scope:  scope,
			TaskID: scope.TaskID,
		}

	scope.Symbols[name.Name] =
		symbol

	r.recordDefinition(
		symbol,
	)

	return symbol
}

func genericParamSymbolKind(
	category ast.GenericParamCategory,
) SymbolKind {
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

func (r *Resolver) declareGenericParams(
	scope *Scope,
	params []ast.GenericParam,
) {
	for _, parameter := range params {
		kind :=
			genericParamSymbolKind(
				parameter.Category,
			)

		if kind ==
			SymbolInvalid {
			r.diags.Add(
				parameter.Span(),
				fmt.Sprintf(
					"invalid generic parameter category for %q",
					parameter.Name.Name,
				),
			)

			continue
		}

		r.declareSymbol(
			scope,
			parameter.Name.Name,
			kind,
			parameter.Name.Span(),
			nil,
		)
	}
}

func (r *Resolver) resolveGenericParams(
	scope *Scope,
	params []ast.GenericParam,
) {
	for _, parameter := range params {
		if parameter.Type != nil {
			r.resolveType(
				scope,
				parameter.Type,
			)
		}

		for _, constraint := range parameter.Constraints {
			r.resolveGenericConstraint(
				scope,
				constraint,
			)
		}
	}
}

func (r *Resolver) resolveGenericConstraint(
	scope *Scope,
	constraint ast.GenericConstraint,
) {
	switch value :=
		constraint.(type) {
	case *ast.GenericExprConstraint:
		r.resolveExpr(
			scope,
			value.Expr,
		)

	case *ast.GenericFieldConstraint:
		if value.HasType &&
			value.Type != nil {
			r.resolveType(
				scope,
				value.Type,
			)
		}

	case *ast.GenericImplConstraint:
		r.resolveType(
			scope,
			value.Interface,
		)

	case *ast.GenericEnumVariantConstraint:
		/*
			Variant names are checked later by the checker.
		*/

	case *ast.GenericUnionMemberConstraint:
		r.resolveType(
			scope,
			value.Member,
		)

	case *ast.GenericTaskConstraint:
		for _, parameter := range value.Params {
			r.resolveType(
				scope,
				parameter,
			)
		}

		for _, result := range value.Results {
			r.resolveType(
				scope,
				result,
			)
		}
	}
}

func (r *Resolver) resolveGenericArg(
	scope *Scope,
	arg ast.GenericArg,
) {
	switch arg.Kind {
	case ast.GenericArgType:
		if arg.Type != nil {
			r.resolveType(
				scope,
				arg.Type,
			)
		}

	case ast.GenericArgExpr:
		if arg.Expr != nil {
			r.resolveExpr(
				scope,
				arg.Expr,
			)
		}
	}
}

func isTypeSymbolKind(
	kind SymbolKind,
) bool {
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

func (r *Resolver) declareGenericSymbol(
	scope *Scope,
	name string,
	kind SymbolKind,
	span source.Span,
) *Symbol {
	if scope == nil {
		return nil
	}

	if existing :=
		scope.LookupLocal(
			name,
		); existing != nil {
		if existing.Kind == kind {
			return existing
		}
	}

	return r.declareSymbol(
		scope,
		name,
		kind,
		span,
		nil,
	)
}

func (r *Resolver) declareDeclSymbol(
	scope *Scope,
	declaration ast.Decl,
) {
	switch value :=
		declaration.(type) {
	case *ast.ConstDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolConst,
			value.Name.Span(),
			value,
		)

	case *ast.ForeignValueDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolConst,
			value.Name.Span(),
			value,
		)

	case *ast.StructDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolStruct,
			value.Name.Span(),
			value,
		)

	case *ast.TaskDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolTask,
			value.Name.Span(),
			value,
		)

	case *ast.ForeignTypeDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolForeignType,
			value.Name.Span(),
			value,
		)

	case *ast.ForeignTaskDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolForeignTaskABI,
			value.Name.Span(),
			value,
		)

	case *ast.EnumDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolEnum,
			value.Name.Span(),
			value,
		)

	case *ast.UnionDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolUnion,
			value.Name.Span(),
			value,
		)

	case *ast.InterfaceDecl:
		symbol :=
			r.declareSymbol(
				scope,
				value.Name.Name,
				SymbolInterface,
				value.Name.Span(),
				value,
			)

		if symbol == nil {
			return
		}

		for _, requirement := range value.Requirements {
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
			value.Name,
			SymbolOverload,
			value.Span(),
			value,
		)

	case *ast.DistinctDecl:
		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolDistinct,
			value.Name.Span(),
			value,
		)

	case *ast.DirectiveDecl:
		/*
			c :: @c_import { ... } is code-generation metadata, not a visible
			Seal symbol.
		*/

	case *ast.ImplDecl:
		/*
			An impl declaration does not introduce a lexical symbol.
		*/
	}
}

func (r *Resolver) resolveDecl(
	scope *Scope,
	declaration ast.Decl,
) {
	switch value :=
		declaration.(type) {
	case *ast.ConstDecl:
		r.resolveExpr(
			scope,
			value.Value,
		)

	case *ast.ForeignValueDecl:
		if value.Type != nil {
			r.resolveType(
				scope,
				value.Type,
			)
		}

	case *ast.ForeignTypeDecl,
		*ast.ForeignTaskDecl:
		/*
			Their C token sequences are opaque to the resolver.
		*/

	case *ast.StructDecl:
		r.resolveStructDecl(
			scope,
			value,
		)

	case *ast.TaskDecl:
		r.resolveTaskDecl(
			scope,
			value,
		)

	case *ast.DistinctDecl:
		r.resolveType(
			scope,
			value.Underlying,
		)

	case *ast.EnumDecl:
		r.resolveEnumDecl(
			scope,
			value,
		)

	case *ast.UnionDecl:
		for _, member := range value.Members {
			r.resolveType(
				scope,
				member,
			)
		}

	case *ast.InterfaceDecl:
		r.resolveInterfaceDecl(
			scope,
			value,
		)

	case *ast.ImplDecl:
		r.resolveImplDecl(
			scope,
			value,
		)

	case *ast.OverloadDecl:
		for _, name := range value.Names {
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

func (r *Resolver) resolveStructDecl(
	parent *Scope,
	declaration *ast.StructDecl,
) {
	if declaration == nil {
		return
	}

	scope :=
		NewScope(
			ScopeDecl,
			parent,
		)

	r.recordScopeRegion(
		declaration.Span(),
		scope,
	)

	r.declareGenericParams(
		scope,
		declaration.GenericParams,
	)

	r.resolveGenericParams(
		scope,
		declaration.GenericParams,
	)

	for _, field := range declaration.Fields {
		r.resolveType(
			scope,
			field.Type,
		)
	}
}

func (r *Resolver) resolveEnumDecl(
	scope *Scope,
	declaration *ast.EnumDecl,
) {
	if declaration == nil {
		return
	}

	if declaration.Underlying != nil {
		r.resolveType(
			scope,
			declaration.Underlying,
		)
	}

	seen :=
		map[string]source.Span{}

	for _, variant := range declaration.Variants {
		if previous, found :=
			seen[variant.Name]; found {
			r.diags.Add(
				variant.Span(),
				fmt.Sprintf(
					"duplicate enum variant %q, previous declaration at %s",
					variant.Name,
					previous.String(),
				),
			)

			continue
		}

		seen[variant.Name] =
			variant.Span()
	}
}

func (r *Resolver) resolveInterfaceDecl(
	parent *Scope,
	declaration *ast.InterfaceDecl,
) {
	if declaration == nil {
		return
	}

	scope :=
		NewScope(
			ScopeDecl,
			parent,
		)

	r.recordScopeRegion(
		declaration.Span(),
		scope,
	)

	r.declareGenericParams(
		scope,
		declaration.GenericParams,
	)

	r.resolveGenericParams(
		scope,
		declaration.GenericParams,
	)

	seenRequirements :=
		map[string]source.Span{}

	for _, requirement := range declaration.Requirements {
		if requirement == nil {
			continue
		}

		if previous, found :=
			seenRequirements[requirement.Name.Name]; found {
			r.diags.Add(
				requirement.Name.Span(),
				fmt.Sprintf(
					"duplicate interface requirement %q, previous declaration at %s",
					requirement.Name.Name,
					previous.String(),
				),
			)
		} else {
			seenRequirements[requirement.Name.Name] = requirement.Name.Span()
		}

		seenParams :=
			map[string]source.Span{}

		for _, parameter := range requirement.Params {
			if parameter.Name.Name != "" &&
				parameter.Name.Name != "_" {
				if previous, found :=
					seenParams[parameter.Name.Name]; found {
					r.diags.Add(
						parameter.Name.Span(),
						fmt.Sprintf(
							"duplicate interface requirement parameter %q, previous declaration at %s",
							parameter.Name.Name,
							previous.String(),
						),
					)
				} else {
					seenParams[parameter.Name.Name] = parameter.Name.Span()
				}
			}

			if parameter.Type != nil {
				r.resolveInterfaceRequirementType(
					scope,
					parameter.Type,
				)
			}

			if parameter.HasDefault {
				r.diags.Add(
					parameter.Name.Span(),
					fmt.Sprintf(
						"interface requirement parameter %q cannot have a default value",
						parameter.Name.Name,
					),
				)
			}
		}

		for _, result := range requirement.Results {
			if result != nil {
				r.resolveInterfaceRequirementType(
					scope,
					result,
				)
			}
		}
	}
}

func (r *Resolver) resolveInterfaceRequirementType(
	scope *Scope,
	typ ast.Type,
) {
	r.resolveTypeWithInterfaceSelf(
		scope,
		typ,
		true,
	)
}

func (r *Resolver) resolveImplDecl(
	parent *Scope,
	declaration *ast.ImplDecl,
) {
	if declaration == nil {
		return
	}

	implScope :=
		NewScope(
			ScopeDecl,
			parent,
		)

	r.recordScopeRegion(
		declaration.Span(),
		implScope,
	)

	r.declareGenericParams(
		implScope,
		declaration.GenericParams,
	)

	r.resolveGenericParams(
		implScope,
		declaration.GenericParams,
	)

	if declaration.Interface != nil {
		r.resolveType(
			implScope,
			declaration.Interface,
		)
	}

	if declaration.Target != nil {
		r.resolveType(
			implScope,
			declaration.Target,
		)
	}

	if len(
		declaration.UsingPath,
	) > 0 {
		if len(
			declaration.Entries,
		) > 0 {
			r.diags.Add(
				declaration.Span(),
				"delegated impl cannot contain manual impl entries",
			)
		}

		seenEmpty := false

		for _, part := range declaration.UsingPath {
			if part.Name == "" {
				seenEmpty = true
				break
			}
		}

		if seenEmpty {
			r.diags.Add(
				declaration.Span(),
				"using path contains an empty field name",
			)
		}

		/*
			Field names in a using path are not normal lexical symbols. Their
			existence and types are resolved by the checker from Target.
		*/
		return
	}

	seenEntries :=
		map[string]source.Span{}

	for index := range declaration.Entries {
		entry :=
			&declaration.Entries[index]

		if previous, found :=
			seenEntries[entry.Name.Name]; found {
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

		if entry.Task != nil &&
			entry.Alias != nil {
			r.diags.Add(
				entry.Span(),
				fmt.Sprintf(
					"impl entry %q cannot contain both a task and an alias",
					entry.Name.Name,
				),
			)

			continue
		}

		if entry.Task == nil &&
			entry.Alias == nil {
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
			r.resolveTaskDecl(
				implScope,
				entry.Task,
			)
		}

		if entry.Alias != nil {
			r.resolveExpr(
				implScope,
				entry.Alias,
			)
		}
	}
}

func (r *Resolver) resolveTaskDecl(
	parent *Scope,
	declaration *ast.TaskDecl,
) {
	if declaration == nil {
		return
	}

	genericScope :=
		NewScope(
			ScopeDecl,
			parent,
		)

	r.recordScopeRegion(
		declaration.Span(),
		genericScope,
	)

	r.declareGenericParams(
		genericScope,
		declaration.GenericParams,
	)

	r.resolveGenericParams(
		genericScope,
		declaration.GenericParams,
	)

	if declaration.ForeignABI != nil {
		r.resolveExpr(
			genericScope,
			declaration.ForeignABI,
		)
	}

	taskScope :=
		NewScope(
			ScopeTask,
			genericScope,
		)

	taskScope.TaskID =
		r.nextTaskID

	r.nextTaskID++

	for _, parameter := range declaration.Params {
		r.resolveType(
			taskScope,
			parameter.Type,
		)

		if parameter.HasDefault {
			r.resolveExpr(
				genericScope,
				parameter.Default,
			)
		}

		r.declareSymbol(
			taskScope,
			parameter.Name.Name,
			SymbolParam,
			parameter.Name.Span(),
			nil,
		)
	}

	for _, result := range declaration.Results {
		r.resolveType(
			taskScope,
			result,
		)
	}

	if declaration.Body != nil {
		r.resolveBlockInScope(
			declaration.Body,
			taskScope,
			false,
		)
	}
}

func (r *Resolver) resolveBlockInScope(
	block *ast.BlockStmt,
	parent *Scope,
	createChild bool,
) {
	if block == nil ||
		parent == nil {
		return
	}

	scope := parent

	if createChild {
		scope =
			NewScope(
				ScopeBlock,
				parent,
			)
	}

	r.recordScopeRegion(
		block.Span(),
		scope,
	)

	for _, statement := range block.Stmts {
		r.resolveStmt(
			scope,
			statement,
		)
	}
}

func (r *Resolver) resolveStmt(
	scope *Scope,
	statement ast.Stmt,
) {
	switch value :=
		statement.(type) {
	case *ast.DeclStmt:
		r.declareDeclSymbol(
			scope,
			value.Decl,
		)

		r.resolveDecl(
			scope,
			value.Decl,
		)

	case *ast.BlockStmt:
		r.resolveBlockInScope(
			value,
			scope,
			true,
		)

	case *ast.ReturnStmt:
		for _, result := range value.Values {
			r.resolveExpr(
				scope,
				result,
			)
		}

	case *ast.BreakStmt,
		*ast.ContinueStmt:
		return

	case *ast.DeferStmt:
		if value.Call != nil {
			r.resolveExpr(
				scope,
				value.Call,
			)
		}

		if value.Body != nil {
			/*
				A deferred block has its own local scope, but it may reference
				symbols visible at the defer declaration.
			*/
			r.resolveBlockInScope(
				value.Body,
				scope,
				true,
			)
		}

	case *ast.SealStmt:
		r.resolveExpr(
			scope,
			value.Target,
		)

	case *ast.ExprStmt:
		r.resolveExpr(
			scope,
			value.Expr,
		)

	case *ast.MultiVarDeclStmt:
		/*
			The RHS is resolved before newly declared names become visible.

			    left, right := Make(left)
		*/
		r.resolveExpr(
			scope,
			value.Value,
		)

		for _, name := range value.Names {
			r.declareShortVar(
				scope,
				name,
				value,
			)
		}

	case *ast.MultiAssignStmt:
		for _, name := range value.Names {
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
			value.Value,
		)

	case *ast.AssignStmt:
		r.resolveExpr(
			scope,
			value.Left,
		)

		r.resolveExpr(
			scope,
			value.Right,
		)

	case *ast.VarDeclStmt:
		if value.HasType {
			r.resolveType(
				scope,
				value.Type,
			)
		}

		if value.HasValue {
			r.resolveExpr(
				scope,
				value.Value,
			)
		}

		r.declareSymbol(
			scope,
			value.Name.Name,
			SymbolVar,
			value.Name.Span(),
			nil,
		)

	case *ast.IfStmt:
		r.resolveExpr(
			scope,
			value.Cond,
		)

		r.resolveBlockInScope(
			value.Then,
			scope,
			true,
		)

		if value.Else != nil {
			r.resolveStmt(
				scope,
				value.Else,
			)
		}

	case *ast.ForStmt:
		forScope :=
			NewScope(
				ScopeBlock,
				scope,
			)

		r.recordScopeRegion(
			value.Span(),
			forScope,
		)

		if value.Init != nil {
			r.resolveStmt(
				forScope,
				value.Init,
			)
		}

		if value.Cond != nil {
			r.resolveExpr(
				forScope,
				value.Cond,
			)
		}

		if value.Post != nil {
			r.resolveStmt(
				forScope,
				value.Post,
			)
		}

		r.resolveBlockInScope(
			value.Body,
			forScope,
			true,
		)

	case *ast.SwitchStmt:
		r.resolveSwitchStmt(
			scope,
			value,
		)
	}
}

func (r *Resolver) resolveType(
	scope *Scope,
	typ ast.Type,
) {
	r.resolveTypeWithInterfaceSelf(
		scope,
		typ,
		false,
	)
}

func (r *Resolver) resolveTypeWithInterfaceSelf(
	scope *Scope,
	typ ast.Type,
	allowInterfaceSelf bool,
) {
	switch value :=
		typ.(type) {
	case *ast.InterfaceSelfType:
		if !allowInterfaceSelf {
			r.diags.Add(
				value.Span(),
				`"self" type is only available inside interface requirements`,
			)
		}

	case *ast.NamedType:
		if len(
			value.Parts,
		) == 0 {
			return
		}

		first :=
			value.Parts[0]

		symbol :=
			r.resolveSymbolUse(
				scope,
				first.Name,
				first.Span(),
			)

		if symbol == nil {
			return
		}

		if len(
			value.Parts,
		) == 1 {
			if isTypeSymbolKind(
				symbol.Kind,
			) {
				return
			}

			if symbol.Kind.IsRuntime() {
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

		if symbol.Kind !=
			SymbolPackage {
			r.diags.Add(
				first.Span(),
				fmt.Sprintf(
					"%q is not a package",
					first.Name,
				),
			)

			return
		}

		if symbol.Package == nil {
			r.diags.Add(
				first.Span(),
				fmt.Sprintf(
					"package %q has no symbol table",
					first.Name,
				),
			)

			return
		}

		if len(
			value.Parts,
		) != 2 {
			r.diags.Add(
				value.Span(),
				"package-qualified types currently support exactly one package and one member",
			)

			return
		}

		memberName :=
			value.Parts[1].Name

		member :=
			symbol.Package.Symbols[memberName]

		if member == nil {
			r.diags.Add(
				value.Parts[1].Span(),
				fmt.Sprintf(
					"package %s has no type %q",
					first.Name,
					memberName,
				),
			)

			return
		}

		r.recordPackageSymbolUse(
			value.Parts[1].Span(),
			symbol.Package,
			member,
		)

		if isTypeSymbolKind(
			member.Kind,
		) {
			return
		}

		r.diags.Add(
			value.Parts[1].Span(),
			fmt.Sprintf(
				"package symbol %s.%s is not a type",
				first.Name,
				memberName,
			),
		)

	case *ast.PointerType:
		r.resolveTypeWithInterfaceSelf(
			scope,
			value.Elem,
			allowInterfaceSelf,
		)

	case *ast.GenericType:
		r.resolveTypeWithInterfaceSelf(
			scope,
			value.Base,
			allowInterfaceSelf,
		)

		for _, arg := range value.Args {
			r.resolveGenericArgWithInterfaceSelf(
				scope,
				arg,
				allowInterfaceSelf,
			)
		}

	case *ast.InlineArrayType:
		r.resolveTypeWithInterfaceSelf(
			scope,
			value.Elem,
			allowInterfaceSelf,
		)

		r.resolveExpr(
			scope,
			value.Length,
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
			r.resolveExpr(
				scope,
				arg.Expr,
			)
		}
	}
}

func (r *Resolver) resolveCallCallee(
	scope *Scope,
	callee ast.Expr,
) {
	if callee == nil {
		return
	}

	/*
		Interface requirements are not ordinary lexical symbols.

		    Read(reader)

		If no ordinary symbol named Read exists but Read is a visible interface
		requirement, defer candidate selection to the checker.
	*/
	if identifier, valid :=
		callee.(*ast.IdentExpr); valid {
		if scope.LookupVisible(
			identifier.Name.Name,
		) == nil &&
			scope.HasVisibleInterfaceRequirement(
				identifier.Name.Name,
			) {
			return
		}
	}

	r.resolveExpr(
		scope,
		callee,
	)
}

func (r *Resolver) resolveExpr(
	scope *Scope,
	expression ast.Expr,
) {
	if expression == nil {
		return
	}

	switch value :=
		expression.(type) {
	case *ast.IdentExpr:
		r.resolveSymbolUse(
			scope,
			value.Name.Name,
			value.Name.Span(),
		)

	case *ast.DotIdentExpr:
		/*
			.None and .ErrorReading require checker type context.
		*/

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
			value.Expr,
		)

	case *ast.BinaryExpr:
		r.resolveExpr(
			scope,
			value.Left,
		)

		r.resolveExpr(
			scope,
			value.Right,
		)

	case *ast.CallExpr:
		r.resolveCallCallee(
			scope,
			value.Callee,
		)

		for _, argument := range value.Args {
			r.resolveExpr(
				scope,
				argument,
			)
		}

	case *ast.GenericExpr:
		r.resolveExpr(
			scope,
			value.Base,
		)

		for _, argument := range value.Args {
			r.resolveGenericArg(
				scope,
				argument,
			)
		}

	case *ast.TaskPointerExpr:
		r.resolveExpr(
			scope,
			value.Task,
		)

	case *ast.InlineArrayExpr:
		r.resolveType(
			scope,
			value.Elem,
		)

		r.resolveExpr(
			scope,
			value.Length,
		)

		for _, item := range value.Values {
			r.resolveExpr(
				scope,
				item,
			)
		}

	case *ast.SpreadExpr:
		r.resolveExpr(
			scope,
			value.Expr,
		)

	case *ast.SelectorExpr:
		if identifier, valid :=
			value.Left.(*ast.IdentExpr); valid {
			symbol :=
				r.resolveSymbolUse(
					scope,
					identifier.Name.Name,
					identifier.Name.Span(),
				)

			if symbol == nil {
				return
			}

			if symbol.Kind ==
				SymbolPackage {
				if symbol.Package == nil {
					r.diags.Add(
						identifier.Span(),
						fmt.Sprintf(
							"package %q has no symbol table",
							identifier.Name.Name,
						),
					)

					return
				}

				member :=
					symbol.Package.Symbols[value.Name.Name]

				if member == nil {
					r.diags.Add(
						value.Name.Span(),
						fmt.Sprintf(
							"package %s has no symbol %q",
							identifier.Name.Name,
							value.Name.Name,
						),
					)

					return
				}

				r.recordPackageSymbolUse(
					value.Name.Span(),
					symbol.Package,
					member,
				)

				return
			}
		}

		r.resolveExpr(
			scope,
			value.Left,
		)

	case *ast.IndexExpr:
		r.resolveExpr(
			scope,
			value.Left,
		)

		r.resolveExpr(
			scope,
			value.Index,
		)

	case *ast.CompoundLiteralExpr:
		r.resolveType(
			scope,
			value.Type,
		)

		for _, field := range value.Fields {
			r.resolveExpr(
				scope,
				field.Value,
			)
		}

		for _, item := range value.Values {
			r.resolveExpr(
				scope,
				item,
			)
		}
	}
}

func (r *Resolver) resolveSymbolUse(
	scope *Scope,
	name string,
	span source.Span,
) *Symbol {
	if scope == nil {
		return nil
	}

	symbol :=
		scope.LookupVisible(
			name,
		)

	if symbol == nil {
		r.diags.Add(
			span,
			fmt.Sprintf(
				"undefined symbol %q",
				name,
			),
		)

		return nil
	}

	r.recordSymbolUse(
		span,
		symbol,
	)

	if symbol.Kind.IsRuntime() &&
		symbol.TaskID != scope.TaskID {
		r.diags.Add(
			span,
			fmt.Sprintf(
				"nested task cannot capture runtime symbol %q declared at %s",
				name,
				symbol.Span.String(),
			),
		)

		return symbol
	}

	return symbol
}

func (r *Resolver) resolveSwitchStmt(
	scope *Scope,
	statement *ast.SwitchStmt,
) {
	if statement == nil {
		return
	}

	r.resolveExpr(
		scope,
		statement.Target,
	)

	for _, switchCase := range statement.Cases {
		caseScope :=
			NewScope(
				ScopeBlock,
				scope,
			)

		r.recordScopeRegion(
			switchCase.Loc,
			caseScope,
		)

		switch switchCase.Kind {
		case ast.SwitchCaseUnionMember:
			r.resolveType(
				scope,
				switchCase.UnionMember,
			)

			if statement.IsUnionSwitch &&
				statement.BindName.Name != "" {
				r.declareSymbol(
					caseScope,
					statement.BindName.Name,
					SymbolVar,
					statement.BindName.Span(),
					statement,
				)
			}

		case ast.SwitchCaseNil:
			if statement.IsUnionSwitch &&
				statement.BindName.Name != "" {
				r.declareSymbol(
					caseScope,
					statement.BindName.Name,
					SymbolVar,
					statement.BindName.Span(),
					statement,
				)
			}

		case ast.SwitchCaseExpr:
			r.resolveExpr(
				scope,
				switchCase.Expr,
			)

		case ast.SwitchCaseEnumVariant,
			ast.SwitchCaseDefault:
			/*
				Resolved by the checker.
			*/
		}

		for _, caseStatement := range switchCase.Body {
			r.resolveStmt(
				caseScope,
				caseStatement,
			)
		}
	}
}

func (r *Resolver) declarePackages() {
	for name, pkg := range r.packages {
		if name == "" ||
			pkg == nil {
			continue
		}

		r.global.Symbols[name] =
			&Symbol{
				Name:    name,
				Kind:    SymbolPackage,
				Scope:   r.global,
				Builtin: true,
				Package: pkg,
			}

		for requirementName := range pkg.InterfaceRequirements {
			r.global.DeclareInterfaceRequirement(
				requirementName,
			)
		}
	}
}

func ExportPackage(
	name string,
	scope *Scope,
) *PackageInfo {
	info :=
		&PackageInfo{
			Name: name,

			Symbols: map[string]*PackageSymbol{},

			InterfaceRequirements: map[string]struct{}{},
		}

	if scope == nil {
		return info
	}

	for symbolName, symbol := range scope.Symbols {
		if symbol == nil ||
			symbol.Builtin {
			continue
		}

		if symbol.Kind ==
			SymbolInterface {
			if declaration, valid :=
				symbol.Node.(*ast.InterfaceDecl); valid &&
				declaration != nil {
				for _, requirement := range declaration.Requirements {
					if requirement == nil ||
						requirement.Name.Name == "" {
						continue
					}

					info.InterfaceRequirements[requirement.Name.Name] = struct{}{}
				}
			}
		}

		switch symbol.Kind {
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
			info.Symbols[symbolName] =
				&PackageSymbol{
					Name: symbolName,
					Kind: symbol.Kind,
					Span: symbol.Span,
				}
		}
	}

	return info
}

func DebugSummary(
	scope *Scope,
) string {
	if scope == nil {
		return "global_symbols=0"
	}

	count := 0

	for _, symbol := range scope.Symbols {
		if symbol != nil &&
			!symbol.Builtin {
			count++
		}
	}

	return fmt.Sprintf(
		"global_symbols=%d",
		count,
	)
}
