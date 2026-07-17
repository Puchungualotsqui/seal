package cgen

import (
	"fmt"
	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/source"
	"sort"
	"strings"
)

type valueInfo struct {
	Type CType
}

type loopControl struct {
	scope *scope

	breakLabel    string
	continueLabel string

	breakUsed    bool
	continueUsed bool
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

func (s *scope) lookupLocal(
	name string,
) (valueInfo, bool) {
	if s == nil {
		return valueInfo{}, false
	}

	value, ok := s.vars[name]
	return value, ok
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

func (g *Generator) emitImportedDistincts() {
	type importedDistinctEmission struct {
		PackageName string
		TypeName    string
		CName       string
		Decl        *ast.DistinctDecl
	}

	entries :=
		map[string]importedDistinctEmission{}

	/*
		Return the imported distinct represented by a named underlying type.

		The packageName argument is the package in whose source context the
		type was written. Therefore:

		    Identifier

		inside package auth means auth.Identifier, while:

		    ids.Identifier

		explicitly means ids.Identifier.
	*/
	distinctFromNamedType := func(
		contextPackageName string,
		named *ast.NamedType,
	) (
		string,
		string,
		*ast.DistinctDecl,
		bool,
	) {
		if named == nil ||
			len(named.Parts) == 0 {
			return "", "", nil, false
		}

		packageName := contextPackageName
		typeName :=
			named.Parts[len(named.Parts)-1].Name

		if len(named.Parts) >= 2 {
			packageName =
				named.Parts[0].Name
		}

		if packageName == "" ||
			typeName == "" {
			return "", "", nil, false
		}

		pkg :=
			g.typePackageInfo(
				packageName,
			)

		if pkg == nil {
			return "", "", nil, false
		}

		decl := pkg.Distincts[typeName]

		if decl == nil {
			return "", "", nil, false
		}

		return packageName,
			typeName,
			decl,
			true
	}

	isSwitchIntegerBuiltinName := func(
		name string,
	) bool {
		switch name {
		case "int",
			"uint",
			"i8",
			"i16",
			"i32",
			"i64",
			"u8",
			"u16",
			"u32",
			"u64",
			"char":
			return true

		default:
			return false
		}
	}

	/*
		Determine whether an imported distinct is recursively backed by a
		switch-compatible integer type without lowering arbitrary AST types.

		This is intentionally AST-based. Calling cTypeFromAst here for every
		imported distinct can register generic structures during the emission
		phase, after generic-instance collection has already completed.
	*/
	var isIntegerBackedDistinct func(
		string,
		string,
		*ast.DistinctDecl,
		map[string]bool,
	) bool

	isIntegerBackedDistinct = func(
		packageName string,
		typeName string,
		decl *ast.DistinctDecl,
		visiting map[string]bool,
	) bool {
		if packageName == "" ||
			typeName == "" ||
			decl == nil ||
			decl.Underlying == nil {
			return false
		}

		key :=
			packageName + "." + typeName

		if visiting[key] {
			return false
		}

		visiting[key] = true

		defer delete(
			visiting,
			key,
		)

		named, ok :=
			decl.Underlying.(*ast.NamedType)

		if !ok ||
			len(named.Parts) == 0 {
			return false
		}

		// Builtin types must be unqualified.
		if len(named.Parts) == 1 &&
			isSwitchIntegerBuiltinName(
				named.Parts[0].Name,
			) {
			return true
		}

		dependencyPackage,
			dependencyName,
			dependencyDecl,
			ok :=
			distinctFromNamedType(
				packageName,
				named,
			)

		if !ok {
			return false
		}

		return isIntegerBackedDistinct(
			dependencyPackage,
			dependencyName,
			dependencyDecl,
			visiting,
		)
	}

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

		for typeName, decl := range pkg.Distincts {
			if decl == nil {
				continue
			}

			if !isIntegerBackedDistinct(
				packageName,
				typeName,
				decl,
				map[string]bool{},
			) {
				continue
			}

			cName :=
				cImportedTypeName(
					packageName,
					typeName,
				)

			if _, alreadyCollected :=
				entries[cName]; alreadyCollected {
				continue
			}

			entries[cName] =
				importedDistinctEmission{
					PackageName: packageName,
					TypeName:    typeName,
					CName:       cName,
					Decl:        decl,
				}
		}
	}

	if len(entries) == 0 {
		return
	}

	names := make(
		[]string,
		0,
		len(entries),
	)

	for name := range entries {
		names = append(
			names,
			name,
		)
	}

	sort.Strings(names)

	const (
		distinctNotVisited uint8 = iota
		distinctVisiting
		distinctEmitted
		distinctFailed
	)

	states := map[string]uint8{}
	emittedAny := false

	var emit func(string) bool

	emit = func(name string) bool {
		switch states[name] {
		case distinctEmitted:
			return true

		case distinctFailed:
			return false

		case distinctVisiting:
			entry := entries[name]

			g.error(
				entry.Decl.Name.Span(),
				fmt.Sprintf(
					"cyclic imported distinct type involving %s",
					name,
				),
			)

			states[name] = distinctFailed
			return false
		}

		entry, exists := entries[name]

		if !exists {
			return true
		}

		states[name] = distinctVisiting

		/*
			If this distinct is backed by another imported distinct, emit that
			typedef first.

			This dependency lookup is AST-only so it does not register generic
			instances during C emission.
		*/
		if named, ok :=
			entry.Decl.Underlying.(*ast.NamedType); ok {
			dependencyPackage,
				dependencyName,
				_,
				hasDependency :=
				distinctFromNamedType(
					entry.PackageName,
					named,
				)

			if hasDependency {
				dependencyCName :=
					cImportedTypeName(
						dependencyPackage,
						dependencyName,
					)

				if _, collected :=
					entries[dependencyCName]; collected {
					if !emit(
						dependencyCName,
					) {
						states[name] =
							distinctFailed

						return false
					}
				}
			}
		}

		/*
			The preflight check above guarantees that this conversion can only
			traverse builtin integer types and imported distinct aliases. It
			cannot discover or register structs, interfaces, or generic struct
			instances.
		*/
		underlying :=
			g.cTypeFromAstInTypeContext(
				entry.PackageName,
				entry.Decl.Underlying,
			)

		if isInvalidCType(underlying) {
			g.error(
				entry.Decl.Underlying.Span(),
				fmt.Sprintf(
					"cannot lower underlying type of imported distinct %s",
					name,
				),
			)

			states[name] = distinctFailed
			return false
		}

		g.linef(
			"typedef %s;",
			underlying.Decl(
				entry.CName,
			),
		)

		states[name] = distinctEmitted
		emittedAny = true

		return true
	}

	for _, name := range names {
		emit(name)
	}

	if emittedAny {
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

	type importedStructEmission struct {
		PackageName string
		CName       string
		Decl        *ast.StructDecl
	}

	var entries []importedStructEmission

	seen := map[string]bool{}

	for mapKey, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		packageName := pkg.Name
		if packageName == "" {
			packageName = mapKey
		}

		if packageName == "" {
			continue
		}

		for structName, decl := range pkg.Structs {
			if decl == nil ||
				decl.IsIntrinsic ||
				len(decl.GenericParams) > 0 {
				continue
			}

			cName := cImportedTypeName(
				packageName,
				structName,
			)

			if seen[cName] {
				continue
			}

			seen[cName] = true

			entries = append(
				entries,
				importedStructEmission{
					PackageName: packageName,
					CName:       cName,
					Decl:        decl,
				},
			)
		}
	}

	sort.Slice(
		entries,
		func(i int, j int) bool {
			return entries[i].CName <
				entries[j].CName
		},
	)

	if len(entries) == 0 {
		return
	}

	/*
		Declare all imported struct typedef names before completing any
		struct body.

		This permits:

			struct Node {
				Node *Next;
			};

		and mutually recursive pointer fields.
	*/
	for _, entry := range entries {
		g.linef(
			"typedef struct %s %s;",
			entry.CName,
			entry.CName,
		)
	}

	g.line("")

	for _, entry := range entries {
		g.linef(
			"struct %s {",
			entry.CName,
		)
		g.indent++

		for _, field := range entry.Decl.Fields {
			fieldType :=
				g.cTypeFromAstInTypeContext(
					entry.PackageName,
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
}

func (g *Generator) emitStructs(
	file *ast.File,
) {
	var declarations []*ast.StructDecl

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

		if isInvalidCStructName(
			d.Name.Name,
		) {
			continue
		}

		declarations = append(
			declarations,
			d,
		)
	}

	if len(declarations) == 0 {
		return
	}

	/*
		Introduce every typedef before completing any struct.

		The old output:

			typedef struct Node {
				Node *Next;
			} Node;

		is invalid because Node is not a typedef until after the body.

		The new output is:

			typedef struct Node Node;

			struct Node {
				Node *Next;
			};
	*/
	for _, declaration := range declarations {
		g.linef(
			"typedef struct %s %s;",
			declaration.Name.Name,
			declaration.Name.Name,
		)
	}

	g.line("")

	for _, declaration := range declarations {
		g.linef(
			"struct %s {",
			declaration.Name.Name,
		)
		g.indent++

		for _, field := range declaration.Fields {
			fieldType :=
				g.cTypeFromAst(
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

	case *ast.MultiAssignStmt:
		g.emitMultiAssignStmt(s)

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

func multiResultField(
	resultTemp string,
	index int,
) string {
	return fmt.Sprintf(
		"%s._%d",
		resultTemp,
		index,
	)
}

func (g *Generator) emitMultiResultCall(
	value ast.Expr,
	targetCount int,
	statementSpan source.Span,
	operation string,
) (
	[]CType,
	string,
	bool,
) {
	if value == nil {
		g.error(
			statementSpan,
			fmt.Sprintf(
				"%s has no value",
				operation,
			),
		)

		return nil, "", false
	}

	call, ok := value.(*ast.CallExpr)
	if !ok {
		g.error(
			value.Span(),
			fmt.Sprintf(
				"%s requires a task call",
				operation,
			),
		)

		return nil, "", false
	}

	resultTypes := g.callReturnTypes(call)

	if len(resultTypes) <= 1 {
		g.error(
			call.Span(),
			fmt.Sprintf(
				"%s requires a task returning at least two values; task returns %d",
				operation,
				len(resultTypes),
			),
		)

		return nil, "", false
	}

	if len(resultTypes) != targetCount {
		g.error(
			statementSpan,
			fmt.Sprintf(
				"%s mismatch: %d target(s), but task returns %d value(s)",
				operation,
				targetCount,
				len(resultTypes),
			),
		)

		return nil, "", false
	}

	for index, resultType := range resultTypes {
		if isInvalidCType(resultType) {
			g.error(
				call.Span(),
				fmt.Sprintf(
					"cannot determine C type of result %d in %s",
					index+1,
					operation,
				),
			)

			return nil, "", false
		}
	}

	resultType := g.inferExprType(
		call,
		nil,
	)

	if isInvalidCType(resultType) {
		g.error(
			call.Span(),
			fmt.Sprintf(
				"cannot determine C result structure type for %s",
				operation,
			),
		)

		return nil, "", false
	}

	resultTemp := g.newTemp(
		"multi_result",
	)

	/*
		The call must execute exactly once. Every target reads from this
		temporary result structure.
	*/
	g.linef(
		"%s = %s;",
		resultType.Decl(resultTemp),
		g.emitExpr(call, &resultType),
	)

	return resultTypes, resultTemp, true
}

func (g *Generator) emitMultiResultDeclaration(
	name ast.Ident,
	itemType CType,
	resultTemp string,
	index int,
) {
	if name.Name == "_" {
		return
	}

	if g.scope == nil {
		g.error(
			name.Span(),
			"multi-value declaration outside a CGen scope",
		)
		return
	}

	if isInvalidCType(itemType) {
		g.error(
			name.Span(),
			fmt.Sprintf(
				"cannot lower multi-value declaration %q with invalid type",
				name.Name,
			),
		)
		return
	}

	if _, exists :=
		g.scope.lookupLocal(name.Name); exists {
		g.error(
			name.Span(),
			fmt.Sprintf(
				"checker classified %q as a new variable, but it already exists in the current CGen scope",
				name.Name,
			),
		)
		return
	}

	g.scope.declare(
		name.Name,
		itemType,
	)

	field := multiResultField(
		resultTemp,
		index,
	)

	if itemType.IsInlineArray {
		g.linef(
			"%s;",
			itemType.Decl(name.Name),
		)

		g.linef(
			"memcpy(%s, %s, sizeof(%s));",
			name.Name,
			field,
			name.Name,
		)

		return
	}

	g.linef(
		"%s = %s;",
		itemType.Decl(name.Name),
		field,
	)
}

func (g *Generator) emitMultiResultConvertedField(
	name ast.Ident,
	sourceType CType,
	targetType CType,
	field string,
) string {
	if sourceType.SealName ==
		targetType.SealName {
		return field
	}

	/*
		A concrete task result may be assigned into a union target.
	*/
	if g.isUnion(targetType) {
		if sourceType.SealName == "nil" {
			return fmt.Sprintf(
				"(%s){.tag = %s_Tag_nil}",
				targetType.Name,
				targetType.SealName,
			)
		}

		if g.unionHasMember(
			targetType.SealName,
			sourceType.SealName,
		) {
			return fmt.Sprintf(
				"(%s){.tag = %s_Tag_%s, .as.%s = %s}",
				targetType.Name,
				targetType.SealName,
				sourceType.SealName,
				sourceType.SealName,
				field,
			)
		}
	}

	if sourceType.SealName == "nil" &&
		strings.HasPrefix(
			targetType.SealName,
			"*",
		) {
		return "NULL"
	}

	/*
		Conversions to `any` and interface types are expression-based in the
		existing backend. Materialize the result field into an addressable
		temporary and reuse emitExpr's normal conversion path.
	*/
	if targetType.SealName == "any" ||
		g.isInterfaceCType(targetType) {
		if g.scope == nil {
			g.error(
				name.Span(),
				"cannot convert multi-result value outside a CGen scope",
			)

			return field
		}

		sourceTemp := g.newTemp(
			"multi_item",
		)

		g.linef(
			"%s = %s;",
			sourceType.Decl(sourceTemp),
			field,
		)

		g.scope.declare(
			sourceTemp,
			sourceType,
		)

		syntheticName := name
		syntheticName.Name = sourceTemp

		syntheticExpr := &ast.IdentExpr{
			Name: syntheticName,
		}

		return g.emitExpr(
			syntheticExpr,
			&targetType,
		)
	}

	/*
		For checker-approved scalar conversions, ordinary C assignment performs
		the required representation conversion.
	*/
	return field
}

func (g *Generator) emitMultiResultAssignment(
	name ast.Ident,
	sourceType CType,
	resultTemp string,
	index int,
) {
	if name.Name == "_" {
		return
	}

	if g.scope == nil {
		g.error(
			name.Span(),
			"multi-value assignment outside a CGen scope",
		)
		return
	}

	target, exists :=
		g.scope.lookup(name.Name)

	if !exists {
		g.error(
			name.Span(),
			fmt.Sprintf(
				"missing CGen variable for multi-value assignment target %q",
				name.Name,
			),
		)
		return
	}

	targetType := target.Type

	if isInvalidCType(targetType) ||
		isInvalidCType(sourceType) {
		g.error(
			name.Span(),
			fmt.Sprintf(
				"cannot lower multi-value assignment to %q with invalid type",
				name.Name,
			),
		)
		return
	}

	field := multiResultField(
		resultTemp,
		index,
	)

	/*
		C arrays cannot be assigned. Both new declarations and assignments to
		existing inline arrays therefore use memcpy.
	*/
	if targetType.IsInlineArray {
		if !sourceType.IsInlineArray {
			g.error(
				name.Span(),
				fmt.Sprintf(
					"cannot assign non-array result %s to inline-array target %q",
					sourceType.String(),
					name.Name,
				),
			)
			return
		}

		g.linef(
			"memcpy(%s, %s, sizeof(%s));",
			name.Name,
			field,
			name.Name,
		)

		return
	}

	if sourceType.IsInlineArray {
		g.error(
			name.Span(),
			fmt.Sprintf(
				"cannot assign inline-array result to non-array target %q",
				name.Name,
			),
		)
		return
	}

	value :=
		g.emitMultiResultConvertedField(
			name,
			sourceType,
			targetType,
			field,
		)

	g.linef(
		"%s = %s;",
		name.Name,
		value,
	)
}

func (g *Generator) emitMultiVarDeclStmt(
	s *ast.MultiVarDeclStmt,
) {
	if s == nil || s.Value == nil {
		return
	}

	resolution, ok :=
		g.multiVarDeclResolutionFor(s)

	if !ok {
		g.error(
			s.Span(),
			"missing checker resolution for multi-value short declaration",
		)
		return
	}

	if len(resolution.Bindings) !=
		len(s.Names) {
		g.error(
			s.Span(),
			fmt.Sprintf(
				"invalid checker resolution for multi-value short declaration: %d target(s), %d binding decision(s)",
				len(s.Names),
				len(resolution.Bindings),
			),
		)
		return
	}

	if g.scope == nil {
		g.error(
			s.Span(),
			"multi-value short declaration outside a CGen scope",
		)
		return
	}

	/*
		Validate the complete resolution before evaluating the call. Invalid
		checker/CGen state should not cause partial output or unexpected side
		effects.
	*/
	for index, binding := range resolution.Bindings {
		name := s.Names[index]

		switch binding {
		case checker.MultiVarBindingDiscard:
			if name.Name != "_" {
				g.error(
					name.Span(),
					fmt.Sprintf(
						"checker classified non-discard target %q as discard",
						name.Name,
					),
				)
				return
			}

		case checker.MultiVarBindingDeclare:
			if name.Name == "_" {
				g.error(
					name.Span(),
					"checker classified discard target as declaration",
				)
				return
			}

			if _, exists :=
				g.scope.lookupLocal(
					name.Name,
				); exists {
				g.error(
					name.Span(),
					fmt.Sprintf(
						"checker classified existing local %q as a declaration",
						name.Name,
					),
				)
				return
			}

		case checker.MultiVarBindingAssign:
			if name.Name == "_" {
				g.error(
					name.Span(),
					"checker classified discard target as assignment",
				)
				return
			}

			if _, exists :=
				g.scope.lookup(
					name.Name,
				); !exists {
				g.error(
					name.Span(),
					fmt.Sprintf(
						"checker classified undefined target %q as an assignment",
						name.Name,
					),
				)
				return
			}

		case checker.MultiVarBindingInvalid:
			g.error(
				name.Span(),
				fmt.Sprintf(
					"cannot emit invalid short-declaration target %q",
					name.Name,
				),
			)
			return

		default:
			g.error(
				name.Span(),
				fmt.Sprintf(
					"unknown short-declaration binding kind for %q",
					name.Name,
				),
			)
			return
		}
	}

	resultTypes,
		resultTemp,
		emitted :=
		g.emitMultiResultCall(
			s.Value,
			len(s.Names),
			s.Span(),
			"multi-value short declaration",
		)

	if !emitted {
		return
	}

	for index, binding := range resolution.Bindings {
		name := s.Names[index]
		itemType := resultTypes[index]

		switch binding {
		case checker.MultiVarBindingDiscard:
			// The call was evaluated once above. No storage is emitted.

		case checker.MultiVarBindingDeclare:
			g.emitMultiResultDeclaration(
				name,
				itemType,
				resultTemp,
				index,
			)

		case checker.MultiVarBindingAssign:
			g.emitMultiResultAssignment(
				name,
				itemType,
				resultTemp,
				index,
			)
		}
	}
}

func (g *Generator) emitMultiAssignStmt(
	s *ast.MultiAssignStmt,
) {
	if s == nil || s.Value == nil {
		return
	}

	if g.scope == nil {
		g.error(
			s.Span(),
			"multi-value assignment outside a CGen scope",
		)
		return
	}

	/*
		Validate all targets before emitting the call. `_` is not a real
		variable and requires no lookup.
	*/
	for _, name := range s.Names {
		if name.Name == "_" {
			continue
		}

		target, exists :=
			g.scope.lookup(name.Name)

		if !exists {
			g.error(
				name.Span(),
				fmt.Sprintf(
					"missing CGen variable for multi-value assignment target %q",
					name.Name,
				),
			)
			return
		}

		if isInvalidCType(target.Type) {
			g.error(
				name.Span(),
				fmt.Sprintf(
					"multi-value assignment target %q has invalid C type",
					name.Name,
				),
			)
			return
		}
	}

	resultTypes,
		resultTemp,
		emitted :=
		g.emitMultiResultCall(
			s.Value,
			len(s.Names),
			s.Span(),
			"multi-value assignment",
		)

	if !emitted {
		return
	}

	for index, name := range s.Names {
		g.emitMultiResultAssignment(
			name,
			resultTypes[index],
			resultTemp,
			index,
		)
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
	*loopControl,
	bool,
) {
	if len(g.loopStack) == 0 {
		return nil, false
	}

	return &g.loopStack[len(g.loopStack)-1],
		true
}

func (g *Generator) emitForStmt(
	s *ast.ForStmt,
) {
	oldScope := g.scope
	oldLoopStackLength :=
		len(g.loopStack)

	loopScope := newScope(
		oldScope,
	)
	g.scope = loopScope

	defer func() {
		g.scope = oldScope
		g.loopStack =
			g.loopStack[:oldLoopStackLength]
	}()

	init := ""
	if s.Init != nil {
		init = g.emitForPart(
			s.Init,
		)
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
		post = g.emitForPart(
			s.Post,
		)
	}

	breakLabel := g.newTemp(
		"loop_break",
	)

	continueLabel := g.newTemp(
		"loop_continue",
	)

	g.loopStack = append(
		g.loopStack,
		loopControl{
			scope: loopScope,

			breakLabel: breakLabel,

			continueLabel: continueLabel,
		},
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

	emittedControl :=
		&g.loopStack[len(g.loopStack)-1]

	g.emitDefersInScope(
		loopScope,
	)

	if emittedControl.continueUsed {
		g.linef(
			"%s: ;",
			emittedControl.continueLabel,
		)
	}

	g.indent--
	g.line("}")

	if emittedControl.breakUsed {
		g.linef(
			"%s: ;",
			emittedControl.breakLabel,
		)
	}
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

	case *ast.MultiVarDeclStmt:
		g.error(
			s.Span(),
			"multi-value short declaration cannot be emitted in a C for-loop header",
		)
		return ""

	case *ast.MultiAssignStmt:
		g.error(
			s.Span(),
			"multi-value assignment cannot be emitted in a C for-loop header",
		)
		return ""

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
	control, ok :=
		g.currentLoopControl()

	if !ok {
		g.error(
			s.Span(),
			"break is only valid inside a for loop",
		)
		return
	}

	if !g.emitDefersThroughScope(
		control.scope,
	) {
		g.error(
			s.Span(),
			"could not find the target for-loop scope while emitting break",
		)
		return
	}

	control.breakUsed = true

	g.linef(
		"goto %s;",
		control.breakLabel,
	)
}

func (g *Generator) emitContinueStmt(
	s *ast.ContinueStmt,
) {
	control, ok :=
		g.currentLoopControl()

	if !ok {
		g.error(
			s.Span(),
			"continue is only valid inside a for loop",
		)
		return
	}

	if !g.emitDefersThroughScope(
		control.scope,
	) {
		g.error(
			s.Span(),
			"could not find the target for-loop scope while emitting continue",
		)
		return
	}

	control.continueUsed = true

	g.linef(
		"goto %s;",
		control.continueLabel,
	)
}

func (g *Generator) emitValueSwitchCaseBody(
	swCase ast.SwitchCase,
) {
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
}

func (g *Generator) emitValueSwitchCaseExpr(
	expr ast.Expr,
	expected CType,
) string {
	return g.emitValueSwitchCaseExprSeen(
		expr,
		expected,
		map[string]bool{},
	)
}

func (g *Generator) emitValueSwitchCaseExprSeen(
	expr ast.Expr,
	expected CType,
	visiting map[string]bool,
) string {
	if expr == nil {
		return "0"
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		name := e.Name.Name

		// A local variable shadows constants and generic parameters. The
		// checker should reject it as a case value, but do not accidentally
		// inline another declaration with the same name.
		if g.scope != nil {
			if _, isLocal :=
				g.scope.lookup(name); isLocal {
				return g.emitExpr(
					expr,
					&expected,
				)
			}
		}

		// A generic integer value parameter becomes its concrete argument in
		// a specialized generic task.
		if g.genericSubst != nil {
			if arg, exists :=
				g.genericSubst[name]; exists &&
				arg.Kind == ast.GenericArgExpr &&
				arg.Expr != nil {
				if !genericArgIsSingleNameForCGen(
					arg,
					name,
				) {
					return g.emitValueSwitchCaseExprSeen(
						arg.Expr,
						expected,
						visiting,
					)
				}
			}
		}

		/*
			Seal constants are emitted elsewhere as:

			    static const intptr_t Value = 10;

			In C, Value is not an integer constant expression and therefore
			cannot be used directly as a case label. Inline the original Seal
			constant expression here.
		*/
		decl := g.constDecls[name]

		if decl == nil {
			return g.emitExpr(
				expr,
				&expected,
			)
		}

		if visiting[name] {
			g.error(
				expr.Span(),
				fmt.Sprintf(
					"cyclic constant %q in switch case",
					name,
				),
			)

			return "0"
		}

		visiting[name] = true

		value :=
			g.emitValueSwitchCaseExprSeen(
				decl.Value,
				expected,
				visiting,
			)

		delete(
			visiting,
			name,
		)

		return value

	case *ast.UnaryExpr:
		operator := g.cUnaryOp(
			e.Op,
		)

		// cUnaryOp historically has no explicit unary-plus entry.
		if operator == "" {
			operator = e.Op.String()
		}

		return fmt.Sprintf(
			"(%s%s)",
			operator,
			g.emitValueSwitchCaseExprSeen(
				e.Expr,
				expected,
				visiting,
			),
		)

	case *ast.BinaryExpr:
		return fmt.Sprintf(
			"(%s %s %s)",
			g.emitValueSwitchCaseExprSeen(
				e.Left,
				expected,
				visiting,
			),
			g.cBinaryOp(e.Op),
			g.emitValueSwitchCaseExprSeen(
				e.Right,
				expected,
				visiting,
			),
		)

	case *ast.CallExpr:
		/*
			Preserve integer casts while recursively inlining constants:

			    case cast<u8>(Maximum):
		*/
		generic, ok :=
			e.Callee.(*ast.GenericExpr)

		if !ok ||
			len(generic.Args) != 1 ||
			len(e.Args) != 1 {
			return g.emitExpr(
				expr,
				&expected,
			)
		}

		id, ok :=
			generic.Base.(*ast.IdentExpr)

		if !ok ||
			id.Name.Name != "cast" {
			return g.emitExpr(
				expr,
				&expected,
			)
		}

		targetArg := generic.Args[0]

		if g.genericSubst != nil {
			targetArg =
				g.substituteGenericArgForCGen(
					targetArg,
					g.genericSubst,
				)
		}

		targetType :=
			g.cTypeFromGenericArg(
				targetArg,
			)

		if isInvalidCType(targetType) {
			return g.emitExpr(
				expr,
				&expected,
			)
		}

		value :=
			g.emitValueSwitchCaseExprSeen(
				e.Args[0],
				targetType,
				visiting,
			)

		return fmt.Sprintf(
			"((%s)(%s))",
			targetType.TypeName(),
			value,
		)
	}

	return g.emitExpr(
		expr,
		&expected,
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

	targetType :=
		g.inferExprType(
			s.Target,
			nil,
		)

	if !g.isValueSwitchCType(targetType) {
		g.error(
			s.Target.Span(),
			fmt.Sprintf(
				"switch target must be an enum, integer, char, or integer-backed distinct type; got %s",
				targetType.String(),
			),
		)

		return
	}

	target :=
		g.emitExpr(
			s.Target,
			&targetType,
		)

	g.linef(
		"switch (%s) {",
		target,
	)
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

		g.emitValueSwitchCaseBody(
			swCase,
		)
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
