package cgen

import (
	"fmt"
	"seal/internal/ast"
	"sort"
	"strings"
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

func (g *Generator) emitDistincts(file *ast.File) {
	emitted := false

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.DistinctDecl)
		if !ok {
			continue
		}

		underlying := g.cTypeFromAst(d.Underlying)

		g.linef(
			"typedef %s;",
			underlying.Decl(d.Name.Name),
		)

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

func (g *Generator) emitConstants(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.ConstDecl)
		if !ok {
			continue
		}

		typ := g.inferExprType(d.Value, nil)

		if typ.IsInlineArray {
			inline, ok :=
				d.Value.(*ast.InlineArrayExpr)

			if !ok {
				g.error(
					d.Value.Span(),
					"@inline_array constant must be initialized with an @inline_array literal",
				)

				g.linef(
					"static const %s = {0};",
					typ.Decl(d.Name.Name),
				)

				continue
			}

			g.linef(
				"static const %s = %s;",
				typ.Decl(d.Name.Name),
				g.emitInlineArrayInitializer(
					inline,
					typ,
				),
			)

			continue
		}

		value := g.emitExpr(d.Value, &typ)

		g.linef(
			"static const %s = %s;",
			typ.Decl(d.Name.Name),
			value,
		)
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

func (g *Generator) externalTaskArgumentPrototype(
	arg ast.GenericArg,
) (string, bool) {
	if arg.Kind != ast.GenericArgExpr ||
		arg.Expr == nil {
		return "", false
	}

	switch e := arg.Expr.(type) {
	case *ast.SelectorExpr:
		id, ok := e.Left.(*ast.IdentExpr)
		if !ok {
			return "", false
		}

		packageName := id.Name.Name
		taskName := e.Name.Name

		if packageName == "" ||
			packageName == g.packageName {
			return "", false
		}

		// Ordinary source-level dependencies are already handled by
		// emitImportedTaskPrototypes. This helper exists specifically for
		// workspace-only task dependencies introduced by cross-package
		// generic specialization requests.
		if packageInfoBySemanticName(
			g.packages,
			packageName,
		) != nil {
			return "", false
		}

		pkg := g.typePackageInfo(
			packageName,
		)
		if pkg == nil {
			return "", false
		}

		info, ok := pkg.Tasks[taskName]
		if !ok {
			return "", false
		}

		// Generic task arguments are registered as imported generic task
		// instances and receive prototypes through
		// emitImportedGenericTaskPrototypes.
		if len(info.GenericParams) != 0 {
			return "", false
		}

		// Functions already declared by the runtime headers must not receive
		// another Seal-generated declaration with potentially different ABI
		// spelling.
		if cExternDeclaredByRuntimeHeaders(info) {
			return "", false
		}

		return g.packageTaskSignature(
			packageName,
			taskName,
			info,
		), true
	}

	return "", false
}

func (g *Generator) emitImportedTaskPrototypes() {
	seen := map[string]bool{}
	emitted := false

	if len(g.packages) > 0 {
		names := make(
			[]string,
			0,
			len(g.packages),
		)

		for name := range g.packages {
			names = append(
				names,
				name,
			)
		}

		sort.Strings(names)

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
				info := pkg.Tasks[taskName]

				if taskName == "Main" ||
					info.IsIntrinsic ||
					len(info.GenericParams) > 0 {
					continue
				}

				// Standard-library externs are declared by the runtime headers
				// included at the top of every generated translation unit.
				if cExternDeclaredByRuntimeHeaders(info) {
					continue
				}

				signature := g.packageTaskSignature(
					pkgName,
					taskName,
					info,
				)

				// The same PackageInfo may occasionally be reachable through
				// more than one metadata-map key. Avoid duplicate C
				// declarations.
				if seen[signature] {
					continue
				}

				seen[signature] = true

				g.linef(
					"%s;",
					signature,
				)

				emitted = true
			}
		}
	}

	// A cross-package generic specialization can carry a task argument owned
	// by the caller package:
	//
	//     lists.NewHashMapEmpty<
	//         Point,
	//         string,
	//         listsExample.PointHash,
	//         listsExample.PointEqual,
	//     >(...)
	//
	// The specialization body is emitted into lists.c, so that translation
	// unit must contain declarations for PointHash and PointEqual even though
	// listsExample is not a source-level dependency of lists.
	//
	// Generic task arguments are already handled through
	// importedGenericTasks. Here we emit prototypes only for ordinary,
	// non-generic workspace-owned task arguments.
	instanceNames := make(
		[]string,
		0,
		len(g.genericTasks),
	)

	for name := range g.genericTasks {
		instanceNames = append(
			instanceNames,
			name,
		)
	}

	sort.Strings(instanceNames)

	for _, instanceName := range instanceNames {
		instance := g.genericTasks[instanceName]
		if instance == nil ||
			instance.Decl == nil {
			continue
		}

		for i, param := range instance.Decl.GenericParams {
			if param.Category != ast.GenericParamTask ||
				i >= len(instance.Args) {
				continue
			}

			signature, ok :=
				g.externalTaskArgumentPrototype(
					instance.Args[i],
				)

			if !ok ||
				signature == "" ||
				seen[signature] {
				continue
			}

			seen[signature] = true

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

func (g *Generator) emitTaskPrototypes(
	file *ast.File,
) {
	emitted := false

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.TaskDecl)
		if !ok ||
			d.IsTest ||
			d.IsIntrinsic {
			continue
		}

		if len(d.GenericParams) > 0 {
			continue
		}

		info, exists := g.tasks[d.Name.Name]
		if !exists {
			continue
		}

		// Functions supplied by the C standard library must use the exact
		// prototypes supplied by their headers. Seal's int/uint/rawptr types
		// are source-level types and do not necessarily reproduce the exact C
		// ABI declaration.
		if cExternDeclaredByRuntimeHeaders(info) {
			continue
		}

		g.linef(
			"%s;",
			g.taskSignature(
				d,
				false,
			),
		)

		emitted = true
	}

	if emitted {
		g.line("")
	}
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

	oldLoopStack := g.loopStack
	g.loopStack = nil

	defer func() {
		g.loopStack = oldLoopStack
	}()

	g.currentTask = d
	g.currentResults = nil
	g.currentGenericTaskName = ""

	for _, result := range d.Results {
		g.currentResults = append(
			g.currentResults,
			g.cTypeFromAst(result),
		)
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

			if i < len(info.ParamIsVariadic) &&
				info.ParamIsVariadic[i] {
				paramType = g.variadicCType(paramType)
			}

			g.scope.declare(
				param.Name.Name,
				paramType,
			)
		}
	}

	g.emitBlockStatements(d.Body)
	g.emitActiveDefers()

	if d.Name.Name == "Main" &&
		len(d.Results) == 0 {
		g.line("return 0;")
	}

	g.scope = oldScope
	g.taskScope = oldTaskScope

	g.indent--
	g.line("}")

	g.currentTask = nil
	g.currentResults = nil
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
			if typ.IsInlineArray {
				inline, ok :=
					s.Value.(*ast.InlineArrayExpr)

				if ok {
					temp :=
						g.newTemp(
							"discard_inline_array",
						)

					g.linef(
						"%s = %s;",
						typ.Decl(temp),
						g.emitInlineArrayInitializer(
							inline,
							typ,
						),
					)

					g.linef("(void)%s;", temp)
					return
				}
			}

			g.linef(
				"(void)(%s);",
				g.emitExpr(s.Value, &typ),
			)
		}

		return
	}

	g.scope.declare(s.Name.Name, typ)

	if typ.IsInlineArray {
		if s.HasValue {
			inline, ok :=
				s.Value.(*ast.InlineArrayExpr)

			if !ok {
				g.error(
					s.Value.Span(),
					"@inline_array variable must be initialized with an @inline_array literal",
				)

				g.linef(
					"%s = {0};",
					typ.Decl(s.Name.Name),
				)

				return
			}

			g.linef(
				"%s = %s;",
				typ.Decl(s.Name.Name),
				g.emitInlineArrayInitializer(
					inline,
					typ,
				),
			)

			return
		}

		g.linef("%s;", typ.Decl(s.Name.Name))
		return
	}

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

func (g *Generator) currentLoopControl() (
	loopControl,
	bool,
) {
	if len(g.loopStack) == 0 {
		return loopControl{}, false
	}

	return g.loopStack[len(g.loopStack)-1], true
}

func (g *Generator) emitForStmt(
	s *ast.ForStmt,
) {
	oldScope := g.scope
	oldLoopStackLength := len(g.loopStack)

	loopScope := newScope(oldScope)
	g.scope = loopScope

	defer func() {
		g.scope = oldScope
		g.loopStack =
			g.loopStack[:oldLoopStackLength]
	}()

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

		if typ.IsInlineArray {
			if s.HasValue {
				inline, ok :=
					s.Value.(*ast.InlineArrayExpr)

				if !ok {
					g.error(
						s.Value.Span(),
						"@inline_array variable must be initialized with an @inline_array literal",
					)

					return fmt.Sprintf(
						"%s = {0}",
						typ.Decl(s.Name.Name),
					)
				}

				return fmt.Sprintf(
					"%s = %s",
					typ.Decl(s.Name.Name),
					g.emitInlineArrayInitializer(
						inline,
						typ,
					),
				)
			}

			return typ.Decl(s.Name.Name)
		}

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

func (g *Generator) emitActiveDefers() {
	for sc := g.scope; sc != nil; sc = sc.parent {
		g.emitDefersInScope(sc)

		if sc == g.taskScope {
			break
		}
	}
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
			return false
		}
	}

	return false
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
