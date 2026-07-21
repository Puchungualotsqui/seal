package cgen

import (
	"fmt"
	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/checker"
	"seal/internal/source"
	"sort"
	"strconv"
	"strings"
)

type ImportedGenericTaskInstance struct {
	PackageName string
	TaskName    string
	Name        string
	Info        TaskInfo
	Args        []ast.GenericArg
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

type externalConcreteType struct {
	PackageName string
	TypeName    string
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

	case checker.TypeInlineArray:
		elem := checkerTypeToAstType(typ.Elem)
		if elem == nil {
			return nil
		}

		length := typ.InlineLengthExpr

		if length == nil && typ.InlineLengthKnown {
			length = &ast.IntLitExpr{
				Value: fmt.Sprintf(
					"%d",
					typ.InlineLength,
				),
			}
		}

		if length == nil {
			return nil
		}

		return &ast.InlineArrayType{
			Elem:   elem,
			Length: length,
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
		if _, err := unquoteSealLiteral(value); err != nil {
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

		if _, ok := parseSealIntegerLiteralForCGen(
			arg.Key,
		); ok {
			return ast.GenericArg{
				Kind: ast.GenericArgExpr,
				Expr: &ast.IntLitExpr{
					Value: arg.Key,
				},
			}
		}

		if _, err := unquoteSealLiteral(arg.Key); err == nil {
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
		pkg :=
			g.typePackageInfo(
				packageName,
			)
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

	case *ast.MultiAssignStmt:
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

	case *ast.MultiAssignStmt:
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

func (g *Generator) collectGenericStructInstancesFromType(
	typ ast.Type,
) {
	switch t := typ.(type) {
	case *ast.InterfaceSelfType:
		return

	case *ast.PointerType:
		g.collectGenericStructInstancesFromType(
			t.Elem,
		)

	case *ast.InlineArrayType:
		g.collectGenericStructInstancesFromType(
			t.Elem,
		)

		g.collectGenericStructInstancesFromExpr(
			t.Length,
		)

	case *ast.GenericType:
		if pkgName,
			typeName,
			ok :=
			packageTypeNameFromAst(
				t.Base,
			); ok {
			if pkg :=
				g.typePackageInfo(
					pkgName,
				); pkg != nil {
				if decl :=
					pkg.Structs[typeName]; decl != nil &&
					len(decl.GenericParams) > 0 {
					_ = g.cTypeFromGenericType(
						t,
					)
				}
			}
		} else {
			baseName :=
				typeNameFromAst(
					t.Base,
				)

			if decl :=
				g.structs[baseName]; decl != nil &&
				len(decl.GenericParams) > 0 {
				_ = g.cTypeFromGenericType(
					t,
				)
			} else if g.typeContextPackage != "" {
				if pkg :=
					g.typePackageInfo(
						g.typeContextPackage,
					); pkg != nil {
					if decl :=
						pkg.Structs[baseName]; decl != nil &&
						len(decl.GenericParams) > 0 {
						_ = g.cTypeFromGenericType(
							t,
						)
					}
				}
			}
		}

		g.collectGenericStructInstancesFromType(
			t.Base,
		)

		for _, arg := range t.Args {
			g.collectGenericStructInstancesFromGenericArg(
				arg,
			)
		}
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

	case *ast.InlineArrayExpr:
		g.collectGenericStructInstancesFromType(e.Elem)
		g.collectGenericStructInstancesFromExpr(e.Length)

		for _, value := range e.Values {
			g.collectGenericStructInstancesFromExpr(value)
		}

	case *ast.TaskPointerExpr:
		resolution, ok :=
			g.taskPointerResolutions[e]

		if !ok {
			g.error(
				e.Span(),
				"missing checker resolution for @task_pointer",
			)
			return
		}

		if resolution.Candidate == nil {
			g.error(
				e.Span(),
				"checker resolution for @task_pointer has no task candidate",
			)
			return
		}

		/*
			This registers a generic specialization when needed. The returned
			name and TaskInfo are not needed during discovery.
		*/
		_, _, _ =
			g.semanticTaskSelection(
				resolution.Candidate,
				resolution.PackageName,
				resolution.GenericArguments,
				e.Span(),
			)

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

func typeNameFromAst(t ast.Type) string {
	switch x := t.(type) {
	case *ast.NamedType:
		if len(x.Parts) == 0 {
			return ""
		}

		return x.Parts[len(x.Parts)-1].Name

	case *ast.GenericType:
		return typeNameFromAst(x.Base)

	case *ast.InlineArrayType:
		return "inline_array_" +
			typeNameFromAst(x.Elem) +
			"_" +
			exprCName(x.Length)

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

func typeAstFromGenericArgForCGen(arg ast.GenericArg) ast.Type {
	switch arg.Kind {
	case ast.GenericArgType:
		return arg.Type

	case ast.GenericArgExpr:
		return typeAstFromExprForCGen(arg.Expr)
	}

	return nil
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

func genericTaskSubstForCGen(params []ast.GenericParam, args []ast.GenericArg) map[string]ast.GenericArg {
	return genericArgSubstForCGen(params, args)
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

	case *ast.InlineArrayType:
		return &ast.InlineArrayType{
			Elem: g.substituteTypeAstForCGen(
				t.Elem,
				subst,
			),
			Length: g.substituteExprForCGen(
				t.Length,
				subst,
			),
			Loc: t.Loc,
		}

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

	case *ast.InlineArrayExpr:
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

		return &ast.InlineArrayExpr{
			Elem: g.substituteTypeAstForCGen(
				e.Elem,
				subst,
			),
			Length: g.substituteExprForCGen(
				e.Length,
				subst,
			),
			Values: values,
			Loc:    e.Loc,
		}

	case *ast.TaskPointerExpr:
		/*
			Keep the original node.

			Task-pointer semantic resolutions are keyed by AST node identity.
			semanticTaskSelection applies g.genericSubst when the expression is
			processed, so this node does not need to be structurally rewritten.
		*/
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

func (g *Generator) cTypeFromAstWithGenericArgsInTypeContext(packageName string, typ ast.Type, subst map[string]ast.GenericArg) CType {
	old := g.typeContextPackage
	g.typeContextPackage = packageName
	out := g.cTypeFromAstWithGenericArgs(typ, subst)
	g.typeContextPackage = old
	return out
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

	case *ast.InlineArrayType:
		return g.cInlineArrayTypeFromAst(
			t,
			subst,
		)

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

func (g *Generator) cTypeFromGenericArg(arg ast.GenericArg) CType {
	if g.genericSubst != nil {
		switch arg.Kind {
		case ast.GenericArgExpr:
			if id, ok := arg.Expr.(*ast.IdentExpr); ok {
				if replacement, exists :=
					g.genericSubst[id.Name.Name]; exists {
					if genericArgIsSingleNameForCGen(
						replacement,
						id.Name.Name,
					) {
						return CInvalid
					}

					return g.cTypeFromGenericArgWithGenericArgs(
						replacement,
						g.genericSubst,
					)
				}
			}

		case ast.GenericArgType:
			return g.cTypeFromAstWithGenericArgs(
				arg.Type,
				g.genericSubst,
			)
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

			if spec, ok :=
				builtin.LookupType(name); ok {
				return CType{
					Name:     spec.CName,
					SealName: spec.Name,
				}
			}

			/*
				While lowering code originating in another package, an
				unqualified type argument belongs to that package first.

				For example, while specializing code from package threads:

				    Use<ThreadResult>()

				ThreadResult must resolve as threads.ThreadResult rather than
				as a same-named type in the package currently being generated.
			*/
			if g.typeContextPackage != "" &&
				g.typeContextPackage != g.packageName {
				pkg :=
					g.typePackageInfo(
						g.typeContextPackage,
					)

				if pkg != nil &&
					g.packageHasType(
						pkg,
						name,
					) {
					return g.importedNamedCType(
						g.typeContextPackage,
						name,
					)
				}
			}

			if foreign :=
				g.foreignTypes[name]; foreign != nil {
				return g.foreignCType(
					g.packageName,
					name,
					foreign,
				)
			}

			if _, ok :=
				g.distincts[name]; ok {
				return CType{
					Name:     name,
					SealName: name,
				}
			}

			if _, ok :=
				g.structs[name]; ok {
				return CType{
					Name:     name,
					SealName: name,
				}
			}

			if _, ok :=
				g.enums[name]; ok {
				return CType{
					Name:     name,
					SealName: name,
				}
			}

			if _, ok :=
				g.unions[name]; ok {
				return CType{
					Name:     name,
					SealName: name,
				}
			}

			if iface :=
				g.interfaces[name]; iface != nil {
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

			g.error(
				e.Span(),
				fmt.Sprintf(
					"expected type argument, got %q",
					name,
				),
			)

			return CInvalid

		case *ast.SelectorExpr:
			if typ :=
				typeAstFromExprForCGen(e); typ != nil {
				return g.cTypeFromAstInContext(
					typ,
				)
			}

			g.error(
				e.Span(),
				"expected type argument",
			)

			return CInvalid

		case *ast.GenericExpr:
			typ :=
				typeAstFromExprForCGen(e)

			if typ == nil {
				g.error(
					e.Span(),
					"expected type argument",
				)

				return CInvalid
			}

			return g.cTypeFromAstInContext(
				typ,
			)

		default:
			g.error(
				arg.Span(),
				"expected type argument",
			)

			return CInvalid
		}
	}

	return CInvalid
}

func (g *Generator) constExprWithGenericArgs(
	expr ast.Expr,
	subst map[string]ast.GenericArg,
) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if arg, ok := subst[e.Name.Name]; ok &&
			arg.Kind == ast.GenericArgExpr {
			if genericArgIsSingleNameForCGen(
				arg,
				e.Name.Name,
			) {
				return e.Name.Name
			}

			return g.constExprWithGenericArgs(
				arg.Expr,
				subst,
			)
		}

		return e.Name.Name

	case *ast.IntLitExpr:
		return normalizeCIntegerLiteral(e.Value)

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.UnaryExpr:
		return fmt.Sprintf(
			"(%s%s)",
			g.cUnaryOp(e.Op),
			g.constExprWithGenericArgs(
				e.Expr,
				subst,
			),
		)

	case *ast.BinaryExpr:
		return fmt.Sprintf(
			"(%s %s %s)",
			g.constExprWithGenericArgs(
				e.Left,
				subst,
			),
			g.cBinaryOp(e.Op),
			g.constExprWithGenericArgs(
				e.Right,
				subst,
			),
		)
	}

	return g.emitExpr(
		g.substituteExprForCGen(
			expr,
			subst,
		),
		nil,
	)
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

	case *ast.InlineArrayType:
		g.emitGenericStructDepsForType(
			t.Elem,
			subst,
			visiting,
		)

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

	// A package-qualified type may still refer to the package currently being
	// generated. This commonly happens with whole-workspace specialization
	// requests:
	//
	//     lists._Slot<int>
	//
	// while generating package lists.
	//
	// Registering that specialization as imported would place the same C type
	// in both genericStructs and importedGenericStructs, causing duplicate
	// typedef definitions.
	if packageName == g.packageName {
		localDecl := g.structs[typeName]

		if localDecl == nil {
			localDecl = decl
		}

		return g.registerGenericStructInstance(
			localDecl,
			args,
		)
	}

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

	if existing :=
		g.importedGenericStructs[name]; existing != nil {
		/*
			Always retain owner-qualified arguments.

			A qualified argument such as os.DirectoryEntry lowers to:

			    DirectoryEntry

			inside os.c, and:

			    os_DirectoryEntry

			inside every other translation unit.
		*/
		existing.Args = append(
			[]ast.GenericArg(nil),
			requestArgs...,
		)

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
				requestArgs...,
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

	case *ast.InlineArrayType:
		g.emitImportedGenericStructDepsForType(
			t.Elem,
			subst,
			visiting,
		)

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

func (g *Generator) importedGenericTaskInfoFromSelector(
	sel *ast.SelectorExpr,
) (string, string, TaskInfo, bool) {
	id, ok := sel.Left.(*ast.IdentExpr)
	if !ok {
		return "", "", TaskInfo{}, false
	}

	packageName := id.Name.Name

	pkg := g.typePackageInfo(
		packageName,
	)
	if pkg == nil {
		return "", "", TaskInfo{}, false
	}

	info, ok := pkg.Tasks[sel.Name.Name]
	if !ok ||
		len(info.GenericParams) == 0 {
		return "", "", TaskInfo{}, false
	}

	return packageName,
		sel.Name.Name,
		info,
		true
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

	// A workspace specialization request can qualify a task with the package
	// currently being generated:
	//
	//     lists.SomeGenericTask<int>
	//
	// In that package, it is a local specialization rather than an imported
	// one. Redirect it to genericTasks to avoid duplicate prototypes and
	// definitions with the same generated C name.
	if packageName == g.packageName {
		localInfo, ok := g.tasks[taskName]

		if ok &&
			localInfo.Decl != nil &&
			len(localInfo.GenericParams) > 0 {
			return g.registerGenericTaskInstance(
				localInfo.Decl,
				args,
			)
		}

		if info.Decl != nil {
			return g.registerGenericTaskInstance(
				info.Decl,
				args,
			)
		}
	}

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

	if existing :=
		g.importedGenericTasks[name]; existing != nil {
		existing.Args = append(
			[]ast.GenericArg(nil),
			requestArgs...,
		)

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
		requestArgs...,
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

		packageName := id.Name.Name
		taskName := e.Name.Name

		// Cross-package generic specialization requests preserve task
		// ownership by qualifying task arguments:
		//
		//     HashString -> lists.HashString
		//     PointHash  -> listsExample.PointHash
		//
		// When the qualifier names the package currently being generated,
		// resolve the task through the local task table.
		if packageName == g.packageName {
			info, ok := g.tasks[taskName]
			if !ok {
				return TaskInfo{}, false
			}

			return info, true
		}

		// A task-valued generic argument can belong to a workspace package
		// that is not a source-level dependency of the package currently
		// being generated.
		//
		// For example:
		//
		//     lists.HashMap<Point, string, PointHash, PointEqual>
		//
		// is specialized inside package lists, while PointHash and PointEqual
		// belong to listsExample. Use typePackageInfo so both ordinary visible
		// packages and workspace-only packages can provide task metadata.
		pkg := g.typePackageInfo(
			packageName,
		)
		if pkg == nil {
			return TaskInfo{}, false
		}

		info, ok := pkg.Tasks[taskName]
		if !ok {
			return TaskInfo{}, false
		}

		info = g.taskInfoInPackageContext(
			packageName,
			taskName,
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
				g.importedGenericTaskInfoFromSelector(
					base,
				)

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

func (g *Generator) taskInfoFromImportedGenericTaskInstance(
	instance *ImportedGenericTaskInstance,
) TaskInfo {
	if instance == nil {
		return TaskInfo{
			ReturnType:  CInvalid,
			ReturnTypes: []CType{CInvalid},
		}
	}

	subst :=
		genericArgSubstForCGen(
			instance.Info.GenericParams,
			instance.Args,
		)

	info := TaskInfo{
		Decl: instance.Info.Decl,

		GenericParams: nil,

		ParamNames: append(
			[]string(nil),
			instance.Info.ParamNames...,
		),

		ReturnType: g.importedGenericTaskReturnType(
			instance,
		),

		ReturnTypes: g.importedGenericTaskReturnTypes(
			instance,
		),

		RequiredParams: instance.Info.RequiredParams,

		IsExtern: instance.Info.IsExtern,

		ExternName: instance.Info.ExternName,

		IsPure: instance.Info.IsPure,

		IsIntrinsic: instance.Info.IsIntrinsic,

		IsTrustedPure: instance.Info.IsTrustedPure,

		ForeignABI: instance.Info.ForeignABI,
	}

	if info.RequiredParams == 0 &&
		len(instance.Info.ParamTypeAsts) > 0 &&
		len(instance.Info.ParamHasDefault) == 0 {
		info.RequiredParams =
			len(instance.Info.ParamTypeAsts)
	}

	g.withTypeContext(
		instance.PackageName,
		func() {
			for i, paramAst := range instance.Info.ParamTypeAsts {
				paramType :=
					g.cTypeFromAstWithGenericArgs(
						paramAst,
						subst,
					)

				info.ParamTypes = append(
					info.ParamTypes,
					paramType,
				)

				hasDefault := false

				if i <
					len(instance.Info.ParamHasDefault) {
					hasDefault =
						instance.Info.ParamHasDefault[i]
				}

				isVariadic := false

				if i <
					len(instance.Info.ParamIsVariadic) {
					isVariadic =
						instance.Info.ParamIsVariadic[i]
				}

				info.ParamHasDefault = append(
					info.ParamHasDefault,
					hasDefault,
				)

				info.ParamIsVariadic = append(
					info.ParamIsVariadic,
					isVariadic,
				)

				if hasDefault &&
					i < len(instance.Info.ParamDefaults) {
					info.ParamDefaults = append(
						info.ParamDefaults,
						g.substituteExprForCGen(
							instance.Info.ParamDefaults[i],
							subst,
						),
					)
				} else {
					info.ParamDefaults = append(
						info.ParamDefaults,
						nil,
					)
				}

				if isVariadic {
					info.IsVariadic = true

					if info.RequiredParams ==
						len(instance.Info.ParamTypeAsts) {
						info.RequiredParams = i
					}
				}

				if hasDefault &&
					info.RequiredParams ==
						len(instance.Info.ParamTypeAsts) {
					info.RequiredParams = i
				}
			}
		},
	)

	return info
}

func (g *Generator) taskInfoFromGenericTaskInstance(
	instance *GenericTaskInstance,
) TaskInfo {
	if instance == nil ||
		instance.Decl == nil {
		return TaskInfo{
			ReturnType:  CInvalid,
			ReturnTypes: []CType{CInvalid},
		}
	}

	decl := instance.Decl

	subst :=
		genericTaskSubstForCGen(
			decl.GenericParams,
			instance.Args,
		)

	foreignABI :=
		g.foreignTaskABIFromExpr(
			decl.ForeignABI,
		)

	/*
		The declaration should normally be resolvable directly. Retain the
		already-collected metadata as a fallback, because it is the canonical
		TaskInfo for this template.
	*/
	if foreignABI == nil {
		if template, ok :=
			g.tasks[decl.Name.Name]; ok {
			foreignABI =
				template.ForeignABI
		}
	}

	info := TaskInfo{
		Decl:          decl,
		GenericParams: nil,

		ReturnType: g.genericTaskReturnType(
			instance,
		),

		ReturnTypes: g.genericTaskReturnTypes(
			instance,
		),

		RequiredParams: len(decl.Params),

		IsExtern:   decl.IsExtern,
		ExternName: decl.ExternName,

		IsPure:        decl.IsPure,
		IsIntrinsic:   decl.IsIntrinsic,
		IsTrustedPure: decl.IsTrustedPure,

		ForeignABI: foreignABI,
	}

	for i, param := range decl.Params {
		paramType :=
			g.cTypeFromAstWithGenericArgs(
				param.Type,
				subst,
			)

		info.ParamNames = append(
			info.ParamNames,
			param.Name.Name,
		)

		info.ParamTypes = append(
			info.ParamTypes,
			paramType,
		)

		info.ParamHasDefault = append(
			info.ParamHasDefault,
			param.HasDefault,
		)

		info.ParamIsVariadic = append(
			info.ParamIsVariadic,
			param.IsVariadic,
		)

		if param.HasDefault {
			info.ParamDefaults = append(
				info.ParamDefaults,
				g.substituteExprForCGen(
					param.Default,
					subst,
				),
			)
		} else {
			info.ParamDefaults = append(
				info.ParamDefaults,
				nil,
			)
		}

		if param.IsVariadic {
			info.IsVariadic = true

			if info.RequiredParams ==
				len(decl.Params) {
				info.RequiredParams = i
			}
		}

		if param.HasDefault &&
			info.RequiredParams ==
				len(decl.Params) {
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

func (g *Generator) taskReturnTypesFromGenericArg(arg ast.GenericArg) ([]CType, bool) {
	info, ok := g.taskInfoFromGenericArg(arg)
	if !ok {
		return nil, false
	}

	return info.ReturnTypes, true
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

		packageName := id.Name.Name

		// Cross-package requests deliberately preserve ownership by turning
		// local task arguments into package-qualified expressions:
		//
		//     HashString -> lists.HashString
		//
		// When that request is later generated by package lists, the qualifier
		// names the current package and must resolve through g.tasks rather than
		// through g.packages.
		if packageName == g.packageName {
			info, ok := g.tasks[e.Name.Name]
			if !ok {
				g.error(
					e.Span(),
					fmt.Sprintf(
						"package %s has no task %q",
						packageName,
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
		}

		pkg := g.typePackageInfo(packageName)
		if pkg == nil {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"unknown package %q",
					packageName,
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
					packageName,
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
			packageName,
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

func (g *Generator) namedGenericTypeIdentityCName(
	typ ast.Type,
) (
	string,
	bool,
) {
	named, ok :=
		typ.(*ast.NamedType)

	if !ok ||
		len(named.Parts) == 0 {
		return "", false
	}

	typeName :=
		named.Parts[len(named.Parts)-1].Name

	if typeName == "" {
		return "", false
	}

	/*
		Explicitly qualified types always retain their semantic owner in the
		specialization identity.

		    os.DirectoryEntry -> os_DirectoryEntry

		This remains true while generating package os itself. C type lowering
		may use DirectoryEntry locally, but specialization identity must remain
		stable across translation units.
	*/
	if len(named.Parts) >= 2 {
		packageName :=
			named.Parts[0].Name

		if packageName == "" {
			return "", false
		}

		return sanitizeCName(
			cImportedTypeName(
				packageName,
				typeName,
			),
		), true
	}

	if spec, exists :=
		builtin.LookupType(typeName); exists {
		return sanitizeCName(
			spec.Name,
		), true
	}

	/*
		Unqualified user-defined types also receive their semantic owner.

		This ensures that these two registrations produce the same identity:

		    DynamicArray<DirectoryEntry>
		    DynamicArray<os.DirectoryEntry>
	*/
	owner :=
		g.crossPackageTypeOwner(
			typeName,
		)

	if owner != "" {
		return sanitizeCName(
			cImportedTypeName(
				owner,
				typeName,
			),
		), true
	}

	return "", false
}

func (g *Generator) genericTypeArgCName(
	arg ast.GenericArg,
) string {
	if g.genericSubst != nil {
		arg =
			g.substituteGenericArgForCGen(
				arg,
				g.genericSubst,
			)
	}

	if typ :=
		typeAstFromGenericArgForCGen(
			arg,
		); typ != nil {
		if name, ok :=
			g.namedGenericTypeIdentityCName(
				typ,
			); ok {
			return name
		}

		lowered :=
			g.cTypeFromAstInContext(
				typ,
			)

		if !isInvalidCType(lowered) {
			return sanitizeCName(
				lowered.SealName,
			)
		}
	}

	return sanitizeCName(
		genericValueArgCName(
			arg,
		),
	)
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
		g.interfaces[name] != nil ||
		g.foreignTypes[name] != nil {
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

	case *ast.InlineArrayType:
		return &ast.InlineArrayType{
			Elem: g.qualifyTypeForCrossPackageRequest(
				t.Elem,
			),
			Length: t.Length,
			Loc:    t.Loc,
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

func (g *Generator) genericTaskParamsForCrossPackageRequest(
	base ast.Expr,
) ([]ast.GenericParam, bool) {
	switch b := base.(type) {
	case *ast.IdentExpr:
		/*
			When lowering code originating in another package, an
			unqualified generic task name belongs to the active type
			context first.
		*/
		if _, _, info, ok :=
			g.importedGenericTaskInfoFromTypeContext(
				b.Name.Name,
			); ok {
			return info.GenericParams, true
		}

		info, ok := g.tasks[b.Name.Name]
		if !ok ||
			len(info.GenericParams) == 0 {
			return nil, false
		}

		return info.GenericParams, true

	case *ast.SelectorExpr:
		id, ok := b.Left.(*ast.IdentExpr)
		if !ok {
			return nil, false
		}

		packageName := id.Name.Name
		taskName := b.Name.Name

		/*
			A cross-package specialization request may qualify a task
			with the package currently being generated:

			    currentPackage.Task<...>

			In that situation, resolve it through the local task table.
		*/
		if packageName == g.packageName {
			info, ok := g.tasks[taskName]
			if !ok ||
				len(info.GenericParams) == 0 {
				return nil, false
			}

			return info.GenericParams, true
		}

		pkg := g.typePackageInfo(
			packageName,
		)
		if pkg == nil {
			return nil, false
		}

		info, ok := pkg.Tasks[taskName]
		if !ok ||
			len(info.GenericParams) == 0 {
			return nil, false
		}

		return info.GenericParams, true
	}

	return nil, false
}

func (g *Generator) qualifyTaskExprForCrossPackageRequest(
	expr ast.Expr,
) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		/*
			A generic task parameter may already have been substituted by
			another task-valued argument. Follow that substitution before
			deciding which package owns the task.
		*/
		if g.genericSubst != nil {
			if arg, ok :=
				g.genericSubst[e.Name.Name]; ok &&
				arg.Kind == ast.GenericArgExpr &&
				arg.Expr != nil {
				if !genericArgIsSingleNameForCGen(
					arg,
					e.Name.Name,
				) {
					return g.qualifyTaskExprForCrossPackageRequest(
						arg.Expr,
					)
				}
			}
		}

		packageName := ""

		if importedPackage, _, found :=
			g.importedTaskInfoFromTypeContext(
				e.Name.Name,
			); found {
			packageName = importedPackage
		} else if _, found :=
			g.tasks[e.Name.Name]; found {
			packageName = g.packageName
		}

		if packageName == "" {
			return expr
		}

		return &ast.SelectorExpr{
			Left: &ast.IdentExpr{
				Name: ast.Ident{
					Name: packageName,
					Loc:  e.Name.Loc,
				},
			},
			Name: e.Name,
			Loc:  e.Span(),
		}

	case *ast.SelectorExpr:
		/*
			The expression already preserves the task's owning package.
		*/
		return expr

	case *ast.GenericExpr:
		params, resolved :=
			g.genericTaskParamsForCrossPackageRequest(
				e.Base,
			)

		args := make(
			[]ast.GenericArg,
			0,
			len(e.Args),
		)

		if resolved {
			/*
				Qualify every argument according to the generic task's
				declared parameter category.

				This distinction is essential because both type names and
				task names are represented by identifier-like expressions:

				    WrappedWorker<int, ConsumeInt>

				The first argument is a type, while the second is a task.
				Syntax alone cannot distinguish them reliably.
			*/
			args = g.crossPackageRequestGenericArgs(
				params,
				e.Args,
			)
		} else {
			/*
				Do not guess that identifier expressions are types when the
				generic task metadata is unavailable. Preserve expression
				arguments exactly as they are.

				Explicit GenericArgType values are still safe to qualify.
			*/
			for _, arg := range e.Args {
				if arg.Kind == ast.GenericArgType {
					arg =
						g.qualifyTypeArgForCrossPackageRequest(
							arg,
						)
				}

				args = append(
					args,
					arg,
				)
			}
		}

		return &ast.GenericExpr{
			Base: g.qualifyTaskExprForCrossPackageRequest(
				e.Base,
			),
			Args: args,
			Loc:  e.Loc,
		}
	}

	return expr
}

func (g *Generator) qualifyTaskArgForCrossPackageRequest(
	arg ast.GenericArg,
) ast.GenericArg {
	if arg.Kind != ast.GenericArgExpr ||
		arg.Expr == nil {
		return arg
	}

	return ast.GenericArg{
		Kind: ast.GenericArgExpr,
		Expr: g.qualifyTaskExprForCrossPackageRequest(
			arg.Expr,
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

		case ast.GenericParamTask:
			out = append(
				out,
				g.qualifyTaskArgForCrossPackageRequest(
					arg,
				),
			)

		default:
			// Extra arguments may not have a corresponding parameter
			// available here. Preserve the existing defensive behavior:
			// explicitly type-shaped arguments are still qualified.
			if category == ast.GenericParamInvalid &&
				typeAstFromGenericArgForCGen(arg) != nil {
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

func (g *Generator) importedGenericTaskSignature(
	instance *ImportedGenericTaskInstance,
) string {
	if instance == nil {
		return "/*invalid*/ int invalid_imported_generic(void)"
	}

	info :=
		g.taskInfoFromImportedGenericTaskInstance(
			instance,
		)

	span := source.Span{}

	if info.Decl != nil {
		span = info.Decl.Span()
	} else if info.ForeignABI != nil {
		span = info.ForeignABI.Loc
	}

	if declaration, foreign :=
		g.foreignTaskDeclaration(
			info,
			instance.Name,
			info.ParamNames,
			span,
		); foreign {
		return declaration
	}

	ret := info.ReturnType.Name

	if len(info.ParamTypes) == 0 {
		return fmt.Sprintf(
			"%s %s(void)",
			ret,
			instance.Name,
		)
	}

	params := make(
		[]string,
		0,
		len(info.ParamTypes),
	)

	for index, paramType := range info.ParamTypes {
		paramName :=
			fmt.Sprintf(
				"arg%d",
				index,
			)

		if index < len(info.ParamNames) &&
			info.ParamNames[index] != "" {
			paramName =
				info.ParamNames[index]
		}

		if index <
			len(info.ParamIsVariadic) &&
			info.ParamIsVariadic[index] {
			if info.IsExtern {
				params = append(
					params,
					"...",
				)
				break
			}

			params = append(
				params,
				g.variadicCType(
					paramType,
				).Decl(
					paramName,
				),
			)

			break
		}

		params = append(
			params,
			paramType.Decl(
				paramName,
			),
		)
	}

	return fmt.Sprintf(
		"%s %s(%s)",
		ret,
		instance.Name,
		strings.Join(
			params,
			", ",
		),
	)
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
	subst := genericTaskSubstForCGen(
		decl.GenericParams,
		info.Args,
	)

	oldSubst := g.genericSubst
	oldTask := g.currentTask
	oldGenericTaskName := g.currentGenericTaskName
	oldResults := g.currentResults
	oldLoopStack := g.loopStack

	g.loopStack = nil

	defer func() {
		g.loopStack = oldLoopStack
	}()

	g.genericSubst = subst
	g.currentTask = decl
	g.currentGenericTaskName = name
	g.currentResults = nil

	for _, result := range decl.Results {
		g.currentResults = append(
			g.currentResults,
			g.cTypeFromAstWithGenericArgs(
				result,
				subst,
			),
		)
	}

	g.linef(
		"%s {",
		g.genericTaskSignature(info, true),
	)
	g.indent++

	oldScope := g.scope
	oldTaskScope := g.taskScope

	g.scope = newScope(oldScope)
	g.taskScope = g.scope

	for _, param := range decl.Params {
		paramType := g.cTypeFromAstWithGenericArgs(
			param.Type,
			subst,
		)

		g.scope.declare(
			param.Name.Name,
			paramType,
		)
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

func (g *Generator) genericTaskSignature(
	instance *GenericTaskInstance,
	definition bool,
) string {
	if instance == nil ||
		instance.Decl == nil {
		return "/*invalid*/ int invalid_generic_task(void)"
	}

	decl := instance.Decl

	info :=
		g.taskInfoFromGenericTaskInstance(
			instance,
		)

	if declaration, foreign :=
		g.foreignTaskDeclaration(
			info,
			instance.Name,
			info.ParamNames,
			decl.Span(),
		); foreign {
		return declaration
	}

	ret := info.ReturnType.Name

	if len(info.ParamTypes) == 0 {
		return fmt.Sprintf(
			"%s %s(void)",
			ret,
			instance.Name,
		)
	}

	params := make(
		[]string,
		0,
		len(info.ParamTypes),
	)

	for index, paramType := range info.ParamTypes {
		paramName :=
			fmt.Sprintf(
				"arg%d",
				index,
			)

		if index < len(info.ParamNames) &&
			info.ParamNames[index] != "" {
			paramName =
				info.ParamNames[index]
		}

		if index <
			len(info.ParamIsVariadic) &&
			info.ParamIsVariadic[index] {
			g.error(
				decl.Params[index].Name.Span(),
				fmt.Sprintf(
					"generic task %q with variadic parameters is not supported by C codegen yet",
					decl.Name.Name,
				),
			)

			paramType = CInvalid
		}

		params = append(
			params,
			paramType.Decl(
				paramName,
			),
		)
	}

	return fmt.Sprintf(
		"%s %s(%s)",
		ret,
		instance.Name,
		strings.Join(
			params,
			", ",
		),
	)
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

	// Ordinary visible dependencies are emitted through the normal imported
	// type registry. PackageInfo can be stored under a metadata key different
	// from its semantic package name, so use semantic lookup here.
	if packageInfoBySemanticName(
		g.packages,
		packageName,
	) != nil {
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

	case *ast.InlineArrayType:
		g.collectRequiredExternalTypeFromType(
			ownerPackage,
			t.Elem,
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

	case *ast.InlineArrayType:
		g.emitRequiredExternalDependencies(
			ownerPackage,
			t.Elem,
			visiting,
		)

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
			"typedef %s;",
			underlying.Decl(cName),
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

func normalizeGenericArgsForCGenParams(params []ast.GenericParam, args []ast.GenericArg) []ast.GenericArg {
	if len(params) == 0 || len(args) == 0 {
		return args
	}

	out := make([]ast.GenericArg, len(args))
	copy(out, args)

	for i := range out {
		if i >= len(params) {
			break
		}

		out[i] = normalizeGenericArgForCGenParam(params[i], out[i])
	}

	return out
}

func normalizeGenericArgForCGenParam(param ast.GenericParam, arg ast.GenericArg) ast.GenericArg {
	switch param.Category {
	case ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion:
		if arg.Kind == ast.GenericArgExpr && arg.Expr != nil {
			if typ := typeAstFromExprForCGen(arg.Expr); typ != nil {
				return ast.GenericArg{
					Kind: ast.GenericArgType,
					Type: typ,
					Loc:  arg.Loc,
				}
			}
		}

	case ast.GenericParamTask:
		// Task arguments must stay expressions:
		//
		//     Use<Identity<int>>()
		//     Use<rules.Identity<int>>()
		//
		// Do not reinterpret these as GenericArgType.
		return arg

	case ast.GenericParamInt,
		ast.GenericParamBool,
		ast.GenericParamString,
		ast.GenericParamValue:
		// Value arguments must stay expressions.
		return arg
	}

	return arg
}
