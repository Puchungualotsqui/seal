package cgen

import (
	"fmt"
	"math/big"
	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/token"
	"strings"
)

type CType struct {
	Name     string
	SealName string

	IsVariadic     bool
	IsInterface    bool
	IsDynInterface bool

	IsInlineArray bool
	InlineLength  uint64

	Elem *CType
}

func (t CType) String() string {
	if t.IsVariadic && t.Elem != nil {
		return "..." + t.Elem.String()
	}

	if t.IsInlineArray {
		elem := "<invalid>"
		if t.Elem != nil {
			elem = t.Elem.String()
		}

		return fmt.Sprintf(
			"@inline_array<%s, %d>",
			elem,
			t.InlineLength,
		)
	}

	return t.Name
}

func (t CType) PhysicalInlineLength() uint64 {
	if t.InlineLength == 0 {
		return 1
	}

	return t.InlineLength
}

func (t CType) Decl(name string) string {
	if t.IsInlineArray {
		if t.Elem == nil {
			return CInvalid.Decl(name)
		}

		return t.Elem.Decl(
			fmt.Sprintf(
				"%s[%d]",
				name,
				t.PhysicalInlineLength(),
			),
		)
	}

	if name == "" {
		return strings.TrimSpace(t.Name)
	}

	return fmt.Sprintf("%s %s", t.Name, name)
}

func (t CType) TypeName() string {
	if t.IsInlineArray {
		return strings.TrimSpace(
			t.Decl(""),
		)
	}

	return t.Name
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

func cImportedTaskName(packageName string, taskName string, info TaskInfo) string {
	if info.IsExtern && info.ExternName != "" {
		return info.ExternName
	}

	return cPackageTaskName(packageName, taskName)
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

	case *ast.InlineArrayType:
		return g.cInlineArrayTypeFromAst(
			typ,
			nil,
		)

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

func (g *Generator) cTypeFromAstInContext(t ast.Type) CType {
	if g.genericSubst != nil {
		return g.cTypeFromAstWithGenericArgs(t, g.genericSubst)
	}

	return g.cTypeFromAst(t)
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

			return g.genericTaskReturnType(
				instance,
			)
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
				g.typePackageInfo(
					packageName,
				); pkg != nil {
				if rawInfo, ok :=
					pkg.Tasks[selector.Name.Name]; ok {
					info :=
						g.taskInfoInPackageContext(
							packageName,
							selector.Name.Name,
							rawInfo,
						)

					return info.ReturnType
				}
			}
		}
	}

	return CInvalid
}

func (g *Generator) binaryOperandTypes(
	e *ast.BinaryExpr,
) (CType, CType) {
	if e == nil {
		return CInvalid, CInvalid
	}

	left := g.inferExprType(
		e.Left,
		nil,
	)

	right := g.inferExprType(
		e.Right,
		nil,
	)

	// Contextual enum literals such as:
	//
	//     state == .Empty
	//     .Empty == state
	//
	// do not carry a standalone type. The checker has already accepted the
	// expression, so when one operand is an enum and the other is a dot enum
	// literal, propagate the concrete enum type to the literal.
	if _, ok := e.Left.(*ast.DotIdentExpr); ok &&
		!isInvalidCType(right) &&
		g.isEnumCType(right) {
		left = g.inferExprType(
			e.Left,
			&right,
		)
	}

	if _, ok := e.Right.(*ast.DotIdentExpr); ok &&
		!isInvalidCType(left) &&
		g.isEnumCType(left) {
		right = g.inferExprType(
			e.Right,
			&left,
		)
	}

	return left, right
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

	case *ast.InlineArrayExpr:
		if expected != nil &&
			expected.IsInlineArray {
			return *expected
		}

		return g.cInlineArrayTypeFromAst(
			&ast.InlineArrayType{
				Elem:   e.Elem,
				Length: e.Length,
				Loc:    e.Loc,
			},
			g.genericSubst,
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
			g.inferExprType(
				e.Expr,
				nil,
			)

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
		left, right :=
			g.binaryOperandTypes(e)

		if isShiftOperator(e.Op) {
			return g.shiftResultCType(
				e,
				left,
				expected,
			)
		}

		if g.hasOperatorOverload(
			e.Op.String(),
		) {
			if candidate, ok :=
				g.resolveOverload(
					e.Op.String(),
					[]CType{
						left,
						right,
					},
				); ok {
				return g.tasks[candidate].ReturnType
			}
		}

		if e.Op == token.NotEq &&
			g.hasOperatorOverload("==") {
			if _, ok :=
				g.resolveOverload(
					"==",
					[]CType{
						left,
						right,
					},
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
				g.typePackageInfo(
					packageName,
				); pkg != nil {
				if rawInfo, ok :=
					pkg.Tasks[e.Name.Name]; ok {
					info :=
						g.taskInfoInPackageContext(
							packageName,
							e.Name.Name,
							rawInfo,
						)

					return info.ReturnType
				}
			}
		}

		leftType :=
			g.inferExprType(
				e.Left,
				nil,
			)

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
		return g.cTypeFromAstInContext(
			e.Type,
		)
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

func (g *Generator) isUnion(t CType) bool {
	_, ok := g.unions[t.SealName]
	return ok
}

func (g *Generator) isEnumCType(
	t CType,
) bool {
	if isInvalidCType(t) {
		return false
	}

	if g.enums[t.SealName] != nil {
		return true
	}

	checkPackages := func(
		packages map[string]*PackageInfo,
	) bool {
		for mapKey, pkg := range packages {
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

	if checkPackages(g.packages) {
		return true
	}

	return checkPackages(
		g.workspacePackages,
	)
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

func (g *Generator) lookupStructFieldTypeByIndex(
	structName string,
	index int,
) CType {
	if index < 0 {
		return CInvalid
	}

	if d := g.structs[structName]; d != nil {
		if index >= len(d.Fields) {
			return CInvalid
		}

		return g.cTypeFromAst(
			d.Fields[index].Type,
		)
	}

	if info := g.genericStructs[structName]; info != nil {
		if index >= len(info.Decl.Fields) {
			return CInvalid
		}

		subst :=
			genericArgSubstForCGen(
				info.Decl.GenericParams,
				info.Args,
			)

		return g.cTypeFromAstWithGenericArgs(
			info.Decl.Fields[index].Type,
			subst,
		)
	}

	if info := g.importedGenericStructs[structName]; info != nil {
		if index >= len(info.Decl.Fields) {
			return CInvalid
		}

		subst :=
			genericArgSubstForCGen(
				info.Decl.GenericParams,
				info.Args,
			)

		return g.cTypeFromAstWithGenericArgsInTypeContext(
			info.PackageName,
			info.Decl.Fields[index].Type,
			subst,
		)
	}

	for pkgName, pkg := range g.packages {
		if pkg == nil {
			continue
		}

		for typeName, decl := range pkg.Structs {
			if cImportedTypeName(pkgName, typeName) != structName {
				continue
			}

			if index >= len(decl.Fields) {
				return CInvalid
			}

			return g.cTypeFromAstInTypeContext(
				pkgName,
				decl.Fields[index].Type,
			)
		}
	}

	return CInvalid
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

func parseSealIntegerLiteralForCGen(
	value string,
) (*big.Int, bool) {
	normalized := strings.ReplaceAll(
		strings.TrimSpace(value),
		"_",
		"",
	)

	if normalized == "" {
		return nil, false
	}

	sign := 1

	switch normalized[0] {
	case '+':
		normalized = normalized[1:]

	case '-':
		sign = -1
		normalized = normalized[1:]
	}

	if normalized == "" {
		return nil, false
	}

	base := 10
	digits := normalized

	if len(normalized) >= 2 &&
		normalized[0] == '0' {
		switch normalized[1] {
		case 'x', 'X':
			base = 16
			digits = normalized[2:]

		case 'b', 'B':
			base = 2
			digits = normalized[2:]

		case 'o', 'O':
			base = 8
			digits = normalized[2:]
		}
	}

	if digits == "" {
		return nil, false
	}

	result := new(big.Int)

	if _, ok := result.SetString(
		digits,
		base,
	); !ok {
		return nil, false
	}

	if sign < 0 {
		result.Neg(result)
	}

	return result, true
}

func normalizeCIntegerLiteral(
	value string,
) string {
	integer, ok := parseSealIntegerLiteralForCGen(
		value,
	)
	if !ok {
		// The checker should already have diagnosed malformed literals.
		// Removing separators still gives the C compiler a useful fallback
		// rather than emitting Seal-specific underscore syntax.
		return strings.ReplaceAll(
			value,
			"_",
			"",
		)
	}

	// Values representable by int64 can be emitted as ordinary decimal C
	// constants. Decimal emission also prevents literals such as 0123 from
	// being interpreted as legacy C octal.
	if integer.IsInt64() {
		return integer.String()
	}

	// The checker guarantees that accepted positive integer constants fit
	// their destination type. Values above INT64_MAX but within u64 require
	// an explicitly unsigned 64-bit C constant.
	if integer.Sign() >= 0 &&
		integer.BitLen() <= 64 {
		return fmt.Sprintf(
			"UINT64_C(%s)",
			integer.String(),
		)
	}

	// This path should only be reached after an earlier checker diagnostic.
	// Keep the exact decimal value in the generated output so the backend
	// does not silently truncate or wrap it.
	return integer.String()
}

func exprCName(expr ast.Expr) string {
	if expr == nil {
		return "nil"
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name.Name

	case *ast.SelectorExpr:
		return exprCName(e.Left) +
			"_" +
			e.Name.Name

	case *ast.IntLitExpr:
		return normalizeCIntegerLiteral(e.Value)

	case *ast.FloatLitExpr:
		return strings.ReplaceAll(
			e.Value,
			".",
			"_",
		)

	case *ast.StringLitExpr:
		return strings.Trim(
			e.Value,
			`"`,
		)

	case *ast.CStringLitExpr:
		return strings.Trim(
			strings.TrimPrefix(
				e.Value,
				"c",
			),
			`"`,
		)

	case *ast.CharLitExpr:
		return strings.Trim(
			e.Value,
			`'`,
		)

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		return "nil"

	case *ast.UnaryExpr:
		return e.Op.String() +
			exprCName(e.Expr)

	case *ast.BinaryExpr:
		return exprCName(e.Left) +
			"_" +
			e.Op.String() +
			"_" +
			exprCName(e.Right)

	case *ast.CallExpr:
		var args []string

		for _, arg := range e.Args {
			args = append(
				args,
				exprCName(arg),
			)
		}

		return exprCName(e.Callee) +
			"_" +
			strings.Join(args, "_")

	case *ast.GenericExpr:
		var args []string

		for _, arg := range e.Args {
			args = append(
				args,
				genericValueArgCName(arg),
			)
		}

		return exprCName(e.Base) +
			"_" +
			strings.Join(args, "_")
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

func (g *Generator) cBinaryOp(
	op token.Kind,
) string {
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

	case token.ShiftLeft:
		return "<<"

	case token.ShiftRight:
		return ">>"

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
