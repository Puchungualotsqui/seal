package cgen

import (
	"fmt"
	"seal/internal/ast"
	"seal/internal/builtin"
	"sort"
	"strings"
)

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

func (g *Generator) implTemplateMatchesRegisteredInterface(
	template *ImplTemplate,
) bool {
	if template == nil ||
		len(g.interfaceInstances) == 0 {
		return false
	}

	paramKinds := implGenericParamKindsForCGen(
		template.GenericParams,
	)

	for _, instance := range g.interfaceInstances {
		if instance == nil {
			continue
		}

		subst := map[string]ast.GenericArg{}

		if g.matchInterfaceTemplate(
			template.Interface,
			template.PackageName,
			instance,
			subst,
			paramKinds,
		) {
			return true
		}
	}

	return false
}

func (g *Generator) collectImportedImplTemplates() {
	if g.packages == nil {
		g.packages = map[string]*PackageInfo{}
	}

	// Avoid appending the same declaration again when this function runs
	// repeatedly during workspace-package promotion.
	seenDeclarations := map[*ast.ImplDecl]bool{}

	for _, template := range g.implTemplates {
		if template == nil ||
			template.Decl == nil {
			continue
		}

		seenDeclarations[template.Decl] = true
	}

	appendPackageTemplates := func(
		packageName string,
		pkg *PackageInfo,
	) {
		if packageName == "" ||
			packageName == g.packageName ||
			pkg == nil {
			return
		}

		for _, decl := range pkg.Impls {
			if decl == nil ||
				seenDeclarations[decl] {
				continue
			}

			template :=
				g.implTemplateFromDeclInPackage(
					packageName,
					decl,
				)

			if template == nil {
				continue
			}

			seenDeclarations[decl] = true

			g.implTemplates = append(
				g.implTemplates,
				template,
			)
		}
	}

	// First collect every implementation from ordinary source-level package
	// dependencies already present in g.packages.
	directPackages := map[string]*PackageInfo{}

	for mapKey, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		packageName := pkg.Name

		if packageName == "" {
			packageName = mapKey
		}

		if packageName == "" ||
			packageName == g.packageName {
			continue
		}

		// Prefer an entry whose semantic PackageInfo.Name is populated.
		existing := directPackages[packageName]

		if existing == nil ||
			existing.Name == "" {
			directPackages[packageName] = pkg
		}
	}

	directNames := make(
		[]string,
		0,
		len(directPackages),
	)

	for packageName := range directPackages {
		directNames = append(
			directNames,
			packageName,
		)
	}

	sort.Strings(directNames)

	for _, packageName := range directNames {
		appendPackageTemplates(
			packageName,
			directPackages[packageName],
		)
	}

	if len(g.workspacePackages) == 0 {
		return
	}

	// Next inspect workspace-only packages. Do not promote every package in
	// the workspace: that would make implementations visible without a
	// corresponding type/interface dependency.
	//
	// Promote only a package containing an implementation whose interface
	// matches an interface instance already referenced by this translation
	// unit.
	workspacePackages := map[string]*PackageInfo{}

	for mapKey, pkg := range g.workspacePackages {
		if pkg == nil {
			continue
		}

		packageName := pkg.Name

		if packageName == "" {
			packageName = mapKey
		}

		if packageName == "" ||
			packageName == g.packageName {
			continue
		}

		// The package is already directly visible.
		if packageInfoBySemanticName(
			g.packages,
			packageName,
		) != nil {
			continue
		}

		existing := workspacePackages[packageName]

		if existing == nil ||
			existing.Name == "" {
			workspacePackages[packageName] = pkg
		}
	}

	workspaceNames := make(
		[]string,
		0,
		len(workspacePackages),
	)

	for packageName := range workspacePackages {
		workspaceNames = append(
			workspaceNames,
			packageName,
		)
	}

	sort.Strings(workspaceNames)

	for _, packageName := range workspaceNames {
		pkg := workspacePackages[packageName]

		matchesReferencedInterface := false

		for _, decl := range pkg.Impls {
			if decl == nil {
				continue
			}

			template :=
				g.implTemplateFromDeclInPackage(
					packageName,
					decl,
				)

			if template == nil {
				continue
			}

			if g.implTemplateMatchesRegisteredInterface(
				template,
			) {
				matchesReferencedInterface = true
				break
			}
		}

		if !matchesReferencedInterface {
			continue
		}

		// Promote the package into the normal visible package set. This is
		// important beyond implementation discovery: imported structs, task
		// prototypes, helper tasks, extern names, inline impl bodies, enums,
		// and unions all use g.packages during C emission.
		g.packages[packageName] = pkg

		appendPackageTemplates(
			packageName,
			pkg,
		)
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

func (g *Generator) tryEmitInterfaceConversion(
	expected CType,
	expr ast.Expr,
) (string, bool) {
	if !g.isInterfaceCType(expected) {
		return "", false
	}

	instance, ok :=
		g.interfaceInstanceForCType(
			expected,
		)
	if !ok {
		return "", false
	}

	src := g.inferExprType(
		expr,
		nil,
	)

	// Do not generate a misleading interface diagnostic after type inference
	// has already failed. Returning false lets the original invalid-type
	// diagnostic remain the primary error.
	if isInvalidCType(src) {
		return "", false
	}

	if src.SealName == expected.SealName {
		return "", false
	}

	if src.SealName == "nil" {
		return g.nilInterfaceValue(
			expected,
		), true
	}

	if !strings.HasPrefix(
		src.SealName,
		"*",
	) {
		return "", false
	}

	concrete := CType{
		Name: strings.TrimSpace(
			strings.TrimSuffix(
				strings.TrimSpace(src.Name),
				"*",
			),
		),
		SealName: strings.TrimPrefix(
			src.SealName,
			"*",
		),
	}

	if isInvalidCType(concrete) {
		return "", false
	}

	impl := g.findResolvedImpl(
		instance,
		concrete,
	)
	if impl == nil {
		g.error(
			expr.Span(),
			fmt.Sprintf(
				"%s does not implement %s",
				concrete.SealName,
				expected.SealName,
			),
		)

		return g.nilInterfaceValue(
			expected,
		), true
	}

	value := g.emitExpr(
		expr,
		nil,
	)

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
		interfaceImplTagName(
			instance,
			concrete,
		),
		value,
	), true
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

func staticInterfaceDispatcherName(iface string, req string) string {
	return sanitizeCName(iface) + "_" + sanitizeCName(req)
}

func dynamicInterfaceDispatcherName(iface string, req string) string {
	return sanitizeCName(iface) +
		"_" +
		sanitizeCName(req)
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

func (g *Generator) discoverRequestedInterfaceImpls() bool {
	overallChanged := false

	for {
		changed := false

		keys := make(
			[]string,
			0,
			len(g.interfaceInstances),
		)

		for key := range g.interfaceInstances {
			keys = append(
				keys,
				key,
			)
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

					g.resolvedImpls[resolved.Key] = resolved

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

	type importedPackage struct {
		MapKey      string
		PackageName string
		Info        *PackageInfo
	}

	var packages []importedPackage

	for mapKey, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		packageName := pkg.Name

		if packageName == "" {
			packageName = mapKey
		}

		packages = append(
			packages,
			importedPackage{
				MapKey:      mapKey,
				PackageName: packageName,
				Info:        pkg,
			},
		)
	}

	sort.Slice(
		packages,
		func(i int, j int) bool {
			if packages[i].PackageName ==
				packages[j].PackageName {
				return packages[i].MapKey <
					packages[j].MapKey
			}

			return packages[i].PackageName <
				packages[j].PackageName
		},
	)

	for _, imported := range packages {
		packageName := imported.PackageName
		pkg := imported.Info

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

			add(
				g.importedNamedCType(
					packageName,
					name,
				),
			)
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
			add(
				g.importedNamedCType(
					packageName,
					name,
				),
			)
		}

		enumNames := make(
			[]string,
			0,
			len(pkg.Enums),
		)

		for name := range pkg.Enums {
			enumNames = append(
				enumNames,
				name,
			)
		}

		sort.Strings(enumNames)

		for _, name := range enumNames {
			add(
				g.importedNamedCType(
					packageName,
					name,
				),
			)
		}

		unionNames := make(
			[]string,
			0,
			len(pkg.Unions),
		)

		for name := range pkg.Unions {
			unionNames = append(
				unionNames,
				name,
			)
		}

		sort.Strings(unionNames)

		for _, name := range unionNames {
			add(
				g.importedNamedCType(
					packageName,
					name,
				),
			)
		}
	}

	names := make(
		[]string,
		0,
		len(seen),
	)

	for name := range seen {
		names = append(
			names,
			name,
		)
	}

	sort.Strings(names)

	out := make(
		[]CType,
		0,
		len(names),
	)

	for _, name := range names {
		out = append(
			out,
			seen[name],
		)
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

func implUsingPathString(path []ast.Ident) string {
	var parts []string

	for _, item := range path {
		parts = append(parts, item.Name)
	}

	return strings.Join(parts, ".")
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

	key := resolvedImplKey(
		instance.Key,
		target,
	)

	return g.resolvedImpls[key]
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
	oldLoopStack := g.loopStack

	g.loopStack = nil

	defer func() {
		g.loopStack = oldLoopStack
	}()

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
		g.currentReturnStructOverride =
			&wrapperReturnType
	}

	// Prevent an interface requirement named Main from being treated as the
	// generated C entry point.
	wrapperTask := *entry.Task
	wrapperTask.Name.Name =
		"__seal_interface_wrapper"
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

		g.scope.declare(
			first.Name.Name,
			firstType,
		)
	}

	for i := 1; i < len(entry.Task.Params); i++ {
		param := entry.Task.Params[i]

		paramType :=
			g.cTypeFromAstWithGenericArgsInTypeContext(
				info.Template.PackageName,
				param.Type,
				info.Subst,
			)

		sourceName := fmt.Sprintf(
			"arg%d",
			i,
		)

		if param.Name.Name != sourceName {
			g.linef(
				"%s = %s;",
				paramType.Decl(param.Name.Name),
				sourceName,
			)
		}

		g.scope.declare(
			param.Name.Name,
			paramType,
		)
	}

	g.emitBlockStatements(entry.Task.Body)
	g.emitActiveDefers()

	g.scope = oldScope
	g.taskScope = oldTaskScope
	g.currentResults = oldResults
	g.genericSubst = oldSubst
	g.currentTask = oldTask
	g.currentReturnStructOverride =
		oldReturnStructOverride
	g.typeContextPackage =
		oldTypeContextPackage
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

func (g *Generator) interfaceDispatcherSignature(
	instance *InterfaceInstance,
	req *ast.TaskSignature,
) string {
	if instance == nil ||
		req == nil {
		return ""
	}

	ret :=
		g.interfaceRequirementReturnType(
			instance,
			req,
		)

	name :=
		staticInterfaceDispatcherName(
			instance.CName,
			req.Name.Name,
		)

	if instance.IsDyn {
		name =
			dynamicInterfaceDispatcherName(
				instance.CName,
				req.Name.Name,
			)
	}

	receiverType := CType{
		Name:           instance.CName,
		SealName:       instance.Key,
		IsInterface:    true,
		IsDynInterface: instance.IsDyn,
	}

	params := []string{
		receiverType.Decl("receiver"),
	}

	for i := 1; i < len(req.Params); i++ {
		paramType :=
			g.interfaceRequirementParamType(
				instance,
				req,
				i,
			)

		params = append(
			params,
			paramType.Decl(
				fmt.Sprintf(
					"arg%d",
					i,
				),
			),
		)
	}

	return fmt.Sprintf(
		"static %s %s(%s)",
		ret.Name,
		name,
		strings.Join(
			params,
			", ",
		),
	)
}

func (g *Generator) emitInterfaceDispatcherPrototypes() {
	keys := make(
		[]string,
		0,
		len(g.interfaceInstances),
	)

	for key := range g.interfaceInstances {
		keys = append(
			keys,
			key,
		)
	}

	sort.Strings(keys)

	emitted := false

	for _, key := range keys {
		instance :=
			g.interfaceInstances[key]

		if instance == nil ||
			instance.Decl == nil {
			continue
		}

		for _, req := range instance.Decl.Requirements {
			signature :=
				g.interfaceDispatcherSignature(
					instance,
					req,
				)

			if signature == "" {
				continue
			}

			g.linef(
				"%s;",
				signature,
			)

			emitted = true
		}
	}

	if emitted {
		g.line("")
	}
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
	if instance == nil ||
		req == nil {
		return
	}

	ret :=
		g.interfaceRequirementReturnType(
			instance,
			req,
		)

	signature :=
		g.interfaceDispatcherSignature(
			instance,
			req,
		)

	if signature == "" {
		return
	}

	g.linef(
		"%s {",
		signature,
	)
	g.indent++

	fieldName :=
		sanitizeCName(
			req.Name.Name,
		)

	g.linef(
		"if (receiver.vtable == NULL || receiver.vtable->%s == NULL) {",
		fieldName,
	)
	g.indent++

	g.line(
		`seal_panic_cstring("dynamic interface dispatch on nil or invalid vtable");`,
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
	g.line("}")

	callArgs := []string{
		"receiver.data",
	}

	for i := 1; i < len(req.Params); i++ {
		callArgs = append(
			callArgs,
			fmt.Sprintf(
				"arg%d",
				i,
			),
		)
	}

	if ret.SealName == "void" {
		g.linef(
			"receiver.vtable->%s(%s);",
			fieldName,
			strings.Join(
				callArgs,
				", ",
			),
		)

		g.line("return;")
	} else {
		g.linef(
			"return receiver.vtable->%s(%s);",
			fieldName,
			strings.Join(
				callArgs,
				", ",
			),
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
	if instance == nil ||
		req == nil {
		return
	}

	ret :=
		g.interfaceRequirementReturnType(
			instance,
			req,
		)

	signature :=
		g.interfaceDispatcherSignature(
			instance,
			req,
		)

	if signature == "" {
		return
	}

	g.linef(
		"%s {",
		signature,
	)
	g.indent++

	g.line("switch (receiver.tag) {")
	g.indent++

	for _, impl := range impls {
		wrapper :=
			interfaceWrapperName(
				instance.CName,
				impl.Target.SealName,
				req.Name.Name,
			)

		g.linef(
			"case %s:",
			interfaceImplTagName(
				instance,
				impl.Target,
			),
		)
		g.indent++

		args := []string{
			"receiver.data",
		}

		for i := 1; i < len(req.Params); i++ {
			args = append(
				args,
				fmt.Sprintf(
					"arg%d",
					i,
				),
			)
		}

		if ret.SealName == "void" {
			g.linef(
				"%s(%s);",
				wrapper,
				strings.Join(
					args,
					", ",
				),
			)

			g.line("return;")
		} else {
			g.linef(
				"return %s(%s);",
				wrapper,
				strings.Join(
					args,
					", ",
				),
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
