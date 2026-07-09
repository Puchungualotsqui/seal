package cgen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

type CType struct {
	Name     string
	SealName string

	IsArray        bool
	IsVariadic     bool
	IsInterface    bool
	IsDynInterface bool
	ArrayLen       string
	Elem           *CType
}

func (t CType) String() string {
	if t.IsVariadic && t.Elem != nil {
		return "..." + t.Elem.String()
	}

	if t.IsArray && t.Elem != nil {
		return fmt.Sprintf("[%s]%s", t.ArrayLen, t.Elem.String())
	}

	return t.Name
}

func (t CType) Decl(name string) string {
	if t.IsArray && t.Elem != nil {
		return fmt.Sprintf("%s %s[%s]", t.Elem.Name, name, t.ArrayLen)
	}

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

type scope struct {
	parent *scope
	vars   map[string]valueInfo
	defers []string
}

func newScope(parent *scope) *scope {
	return &scope{
		parent: parent,
		vars:   map[string]valueInfo{},
	}
}

func (s *scope) addDefer(code string) {
	s.defers = append(s.defers, code)
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
}

type ImplInfo struct {
	Concrete  string
	Interface string
	Entries   map[string]ast.ImplEntry
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
	packages    map[string]*PackageInfo

	genericInstanceRequests        *GenericInstanceRequestSet
	pendingGenericInstanceRequests []GenericInstanceRequest

	structs    map[string]*ast.StructDecl
	enums      map[string]*ast.EnumDecl
	unions     map[string]*ast.UnionDecl
	interfaces map[string]*ast.InterfaceDecl
	impls      map[string][]string

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

	currentTask            *ast.TaskDecl
	currentGenericTaskName string
	currentResults         []CType

	tempCounter int

	distincts map[string]*ast.DistinctDecl

	implInfos map[string]map[string]*ImplInfo
}

func New(diags *diag.Reporter) *Generator {
	return NewWithPackages(diags, "", nil)
}

func NewWithPackages(diags *diag.Reporter, packageName string, packages map[string]*PackageInfo) *Generator {
	return &Generator{
		diags:                         diags,
		packageName:                   packageName,
		packages:                      packages,
		genericInstanceRequests:       NewGenericInstanceRequestSet(),
		structs:                       map[string]*ast.StructDecl{},
		enums:                         map[string]*ast.EnumDecl{},
		unions:                        map[string]*ast.UnionDecl{},
		interfaces:                    map[string]*ast.InterfaceDecl{},
		impls:                         map[string][]string{},
		genericStructs:                map[string]*GenericStructInstance{},
		emittedGenericStructs:         map[string]bool{},
		importedGenericTasks:          map[string]*ImportedGenericTaskInstance{},
		genericTasks:                  map[string]*GenericTaskInstance{},
		emittedGenericTasks:           map[string]bool{},
		importedGenericStructs:        map[string]*ImportedGenericStructInstance{},
		emittedImportedGenericStructs: map[string]bool{},
		tasks:                         map[string]TaskInfo{},
		overloads:                     map[string][]string{},
		consts:                        map[string]CType{},
		emittedVariadics:              map[string]bool{},
		distincts:                     map[string]*ast.DistinctDecl{},
		implInfos:                     map[string]map[string]*ImplInfo{},
	}
}

func ExportPackageInfo(packageName string, file *ast.File, reporter *diag.Reporter) *PackageInfo {
	g := NewWithPackages(reporter, packageName, nil)
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
	}
}

func (g *Generator) newTemp(prefix string) string {
	name := fmt.Sprintf("__seal_%s_%d", prefix, g.tempCounter)
	g.tempCounter++
	return name
}

func (g *Generator) Generate(file *ast.File) string {
	g.collect(file)
	g.seedRequestedGenericInstances()
	g.collectGenericMonomorphizations(file)

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
	g.emitImportedStructs()
	g.emitStructs(file)
	g.emitGenericStructs()
	g.emitImportedGenericStructs()
	g.emitUnions(file)
	g.emitInterfaces()
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
			info := g.implInfoFromDecl(d)
			if info == nil {
				continue
			}

			if _, ok := g.implInfos[info.Concrete]; !ok {
				g.implInfos[info.Concrete] = map[string]*ImplInfo{}
			}

			g.implInfos[info.Concrete][info.Interface] = info

			if !containsString(g.impls[info.Concrete], info.Interface) {
				g.impls[info.Concrete] = append(g.impls[info.Concrete], info.Interface)
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

func (g *Generator) collectImportedGenericTaskInstanceMonomorphizations(name string) {
	info := g.importedGenericTasks[name]
	if info == nil {
		return
	}

	subst := genericArgSubstForCGen(info.Info.GenericParams, info.Args)

	g.collectGenericStructInstancesFromGenericArgsForParams(info.Info.GenericParams, info.Args)

	g.withTypeContext(info.PackageName, func() {
		for _, paramType := range info.Info.ParamTypeAsts {
			g.collectGenericStructInstancesFromType(g.substituteTypeAstForCGen(paramType, subst))
		}

		for _, resultType := range info.Info.ResultTypeAsts {
			g.collectGenericStructInstancesFromType(g.substituteTypeAstForCGen(resultType, subst))
		}

		for i, hasDefault := range info.Info.ParamHasDefault {
			if !hasDefault || i >= len(info.Info.ParamDefaults) {
				continue
			}

			g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(info.Info.ParamDefaults[i], subst))
		}
	})
}

func (g *Generator) collectGenericTaskInstanceMonomorphizations(name string) {
	info := g.genericTasks[name]
	if info == nil || info.Decl == nil {
		return
	}

	subst := genericTaskSubstForCGen(info.Decl.GenericParams, info.Args)

	for i, arg := range info.Args {
		if i >= len(info.Decl.GenericParams) {
			break
		}

		if info.Decl.GenericParams[i].Category != ast.GenericParamTask {
			continue
		}

		g.collectGenericTaskArgInstance(g.substituteGenericArgForCGen(arg, subst))
	}

	for _, param := range info.Decl.Params {
		g.collectGenericStructInstancesFromType(g.substituteTypeAstForCGen(param.Type, subst))

		if param.HasDefault {
			g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(param.Default, subst))
		}
	}

	for _, result := range info.Decl.Results {
		g.collectGenericStructInstancesFromType(g.substituteTypeAstForCGen(result, subst))
	}

	g.collectGenericStructInstancesFromBlockWithGenericArgs(info.Decl.Body, subst)
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

func (g *Generator) collectGenericStructInstancesFromStmtWithGenericArgs(stmt ast.Stmt, subst map[string]ast.GenericArg) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		// Local declarations are not supported by this C backend phase yet.

	case *ast.BlockStmt:
		g.collectGenericStructInstancesFromBlockWithGenericArgs(s, subst)

	case *ast.ReturnStmt:
		for _, value := range s.Values {
			g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(value, subst))
		}

	case *ast.DeferStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Call, subst))

	case *ast.SealStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Target, subst))

	case *ast.ExprStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Expr, subst))

	case *ast.MultiVarDeclStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Value, subst))

	case *ast.AssignStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Left, subst))
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Right, subst))

	case *ast.VarDeclStmt:
		if s.HasType {
			g.collectGenericStructInstancesFromType(g.substituteTypeAstForCGen(s.Type, subst))
		}

		if s.HasValue {
			g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Value, subst))
		}

	case *ast.IfStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Cond, subst))
		g.collectGenericStructInstancesFromBlockWithGenericArgs(s.Then, subst)

		if s.Else != nil {
			g.collectGenericStructInstancesFromStmtWithGenericArgs(s.Else, subst)
		}

	case *ast.ForStmt:
		if s.Init != nil {
			g.collectGenericStructInstancesFromStmtWithGenericArgs(s.Init, subst)
		}

		if s.Cond != nil {
			g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Cond, subst))
		}

		if s.Post != nil {
			g.collectGenericStructInstancesFromStmtWithGenericArgs(s.Post, subst)
		}

		g.collectGenericStructInstancesFromBlockWithGenericArgs(s.Body, subst)

	case *ast.SwitchStmt:
		g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(s.Target, subst))

		for _, swCase := range s.Cases {
			if swCase.UnionMember != nil {
				g.collectGenericStructInstancesFromType(g.substituteTypeAstForCGen(swCase.UnionMember, subst))
			}

			if swCase.Expr != nil {
				g.collectGenericStructInstancesFromExpr(g.substituteExprForCGen(swCase.Expr, subst))
			}

			for _, bodyStmt := range swCase.Body {
				g.collectGenericStructInstancesFromStmtWithGenericArgs(bodyStmt, subst)
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
		g.collectGenericStructInstancesFromType(d.Interface)

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

func (g *Generator) collectGenericStructInstancesFromStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		g.collectGenericStructInstancesFromDecl(s.Decl)

	case *ast.BlockStmt:
		g.collectGenericStructInstancesFromBlock(s)

	case *ast.ReturnStmt:
		for _, value := range s.Values {
			g.collectGenericStructInstancesFromExpr(value)
		}

	case *ast.DeferStmt:
		g.collectGenericStructInstancesFromExpr(s.Call)

	case *ast.SealStmt:
		g.collectGenericStructInstancesFromExpr(s.Target)

	case *ast.ExprStmt:
		g.collectGenericStructInstancesFromExpr(s.Expr)

	case *ast.MultiVarDeclStmt:
		g.collectGenericStructInstancesFromExpr(s.Value)

	case *ast.AssignStmt:
		g.collectGenericStructInstancesFromExpr(s.Left)
		g.collectGenericStructInstancesFromExpr(s.Right)

	case *ast.VarDeclStmt:
		if s.HasType {
			g.collectGenericStructInstancesFromType(s.Type)
		}

		if s.HasValue {
			g.collectGenericStructInstancesFromExpr(s.Value)
		}

	case *ast.IfStmt:
		g.collectGenericStructInstancesFromExpr(s.Cond)
		g.collectGenericStructInstancesFromBlock(s.Then)

		if s.Else != nil {
			g.collectGenericStructInstancesFromStmt(s.Else)
		}

	case *ast.ForStmt:
		if s.Init != nil {
			g.collectGenericStructInstancesFromStmt(s.Init)
		}

		if s.Cond != nil {
			g.collectGenericStructInstancesFromExpr(s.Cond)
		}

		if s.Post != nil {
			g.collectGenericStructInstancesFromStmt(s.Post)
		}

		g.collectGenericStructInstancesFromBlock(s.Body)

	case *ast.SwitchStmt:
		g.collectGenericStructInstancesFromExpr(s.Target)

		for _, swCase := range s.Cases {
			if swCase.UnionMember != nil {
				g.collectGenericStructInstancesFromType(swCase.UnionMember)
			}

			if swCase.Expr != nil {
				g.collectGenericStructInstancesFromExpr(swCase.Expr)
			}

			for _, bodyStmt := range swCase.Body {
				g.collectGenericStructInstancesFromStmt(bodyStmt)
			}
		}
	}
}

func (g *Generator) collectGenericStructInstancesFromType(typ ast.Type) {
	switch t := typ.(type) {
	case *ast.PointerType:
		g.collectGenericStructInstancesFromType(t.Elem)

	case *ast.ArrayType:
		if t.Len != nil {
			g.collectGenericStructInstancesFromExpr(t.Len)
		}

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

func (g *Generator) collectGenericStructInstancesFromExpr(expr ast.Expr) {
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

	case *ast.GenericExpr:
		handledAsTask := false

		switch base := e.Base.(type) {
		case *ast.IdentExpr:
			if info, ok := g.tasks[base.Name.Name]; ok && info.Decl != nil && len(info.GenericParams) > 0 {
				g.registerGenericTaskInstance(info.Decl, e.Args)
				g.collectGenericStructInstancesFromGenericArgsForParams(info.GenericParams, e.Args)
				handledAsTask = true
			}

		case *ast.SelectorExpr:
			if pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(base); ok {
				g.registerImportedGenericTaskInstance(pkgName, taskName, info, e.Args)
				g.collectGenericStructInstancesFromGenericArgsForParams(info.GenericParams, e.Args)
				handledAsTask = true
			}
		}

		g.collectGenericStructInstancesFromExpr(e.Base)

		if !handledAsTask {
			for _, arg := range e.Args {
				g.collectGenericStructInstancesFromGenericArg(arg)
			}
		}

	case *ast.SpreadExpr:
		g.collectGenericStructInstancesFromExpr(e.Expr)

	case *ast.SelectorExpr:
		g.collectGenericStructInstancesFromExpr(e.Left)

	case *ast.IndexExpr:
		g.collectGenericStructInstancesFromExpr(e.Left)
		g.collectGenericStructInstancesFromExpr(e.Index)

	case *ast.ArrayLiteralExpr:
		for _, value := range e.Values {
			g.collectGenericStructInstancesFromExpr(value)
		}

	case *ast.CompoundLiteralExpr:
		g.collectGenericStructInstancesFromType(e.Type)

		for _, field := range e.Fields {
			g.collectGenericStructInstancesFromExpr(field.Value)
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
			if arg, ok := subst[t.Parts[0].Name]; ok {
				g.emitGenericStructDepsForGenericArg(arg, subst, visiting)
			}
		}

	case *ast.PointerType:
		g.emitGenericStructDepsForType(t.Elem, subst, visiting)

	case *ast.ArrayType:
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

	case *ast.ArrayType:
		return &ast.ArrayType{
			Len:      g.substituteExprForCGen(t.Len, subst),
			Inferred: t.Inferred,
			Elem:     g.substituteTypeAstForCGen(t.Elem, subst),
			Loc:      t.Loc,
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

func (g *Generator) substituteExprForCGen(expr ast.Expr, subst map[string]ast.GenericArg) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if arg, ok := subst[e.Name.Name]; ok && arg.Kind == ast.GenericArgExpr && arg.Expr != nil {
			if genericArgIsSingleNameForCGen(arg, e.Name.Name) {
				return e
			}

			return g.substituteExprForCGen(arg.Expr, subst)
		}

		return e

	case *ast.UnaryExpr:
		return &ast.UnaryExpr{
			Op:   e.Op,
			Expr: g.substituteExprForCGen(e.Expr, subst),
			Loc:  e.Loc,
		}

	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Left:  g.substituteExprForCGen(e.Left, subst),
			Op:    e.Op,
			Right: g.substituteExprForCGen(e.Right, subst),
			Loc:   e.Loc,
		}

	case *ast.CallExpr:
		args := make([]ast.Expr, 0, len(e.Args))
		for _, arg := range e.Args {
			args = append(args, g.substituteExprForCGen(arg, subst))
		}

		return &ast.CallExpr{
			Callee: g.substituteExprForCGen(e.Callee, subst),
			Args:   args,
			Loc:    e.Loc,
		}

	case *ast.GenericExpr:
		args := make([]ast.GenericArg, 0, len(e.Args))
		for _, arg := range e.Args {
			args = append(args, g.substituteGenericArgForCGen(arg, subst))
		}

		return &ast.GenericExpr{
			Base: g.substituteExprForCGen(e.Base, subst),
			Args: args,
			Loc:  e.Loc,
		}

	case *ast.SpreadExpr:
		return &ast.SpreadExpr{
			Expr: g.substituteExprForCGen(e.Expr, subst),
			Loc:  e.Loc,
		}

	case *ast.SelectorExpr:
		return &ast.SelectorExpr{
			Left: g.substituteExprForCGen(e.Left, subst),
			Name: e.Name,
			Loc:  e.Loc,
		}

	case *ast.IndexExpr:
		return &ast.IndexExpr{
			Left:  g.substituteExprForCGen(e.Left, subst),
			Index: g.substituteExprForCGen(e.Index, subst),
			Loc:   e.Loc,
		}

	case *ast.ArrayLiteralExpr:
		values := make([]ast.Expr, 0, len(e.Values))
		for _, value := range e.Values {
			values = append(values, g.substituteExprForCGen(value, subst))
		}

		return &ast.ArrayLiteralExpr{
			Values: values,
			Loc:    e.Loc,
		}

	case *ast.CompoundLiteralExpr:
		fields := make([]ast.LiteralField, 0, len(e.Fields))
		for _, field := range e.Fields {
			fields = append(fields, ast.LiteralField{
				Name:  field.Name,
				Value: g.substituteExprForCGen(field.Value, subst),
			})
		}

		values := make([]ast.Expr, 0, len(e.Values))
		for _, value := range e.Values {
			values = append(values, g.substituteExprForCGen(value, subst))
		}

		return &ast.CompoundLiteralExpr{
			Type:   g.substituteTypeAstForCGen(e.Type, subst),
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
		}

	case *ast.ArrayType:
		elem := g.cTypeFromAstWithGenericArgs(t.Elem, subst)
		length := ""

		if t.Inferred {
			length = ""
		} else if t.Len != nil {
			length = g.constExprWithGenericArgs(t.Len, subst)
		}

		sealName := "[]" + elem.SealName
		if !t.Inferred {
			sealName = "[" + length + "]" + elem.SealName
		}

		return CType{
			IsArray:  true,
			ArrayLen: length,
			Elem:     &elem,
			Name:     elem.Name,
			SealName: sealName,
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
		return fmt.Sprintf("(uintptr_t)sizeof(%s)", typ.Name)
	}

	argType := g.inferExprType(e.Args[0], nil)

	if argType.SealName == "string" {
		value := g.emitExpr(e.Args[0], nil)
		return fmt.Sprintf("(uintptr_t)(%s).byte_len", value)
	}

	if argType.SealName == "cstring" {
		g.error(e.Args[0].Span(), "size(cstring) is not supported because cstring length requires scanning memory")
		return "0"
	}

	value := g.emitExpr(e.Args[0], nil)
	return fmt.Sprintf("(uintptr_t)sizeof(%s)", value)
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

func (g *Generator) emitEnums(file *ast.File) {
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.EnumDecl)
		if !ok {
			continue
		}

		g.linef("typedef enum %s {", d.Name.Name)
		g.indent++

		for i, variant := range d.Variants {
			comma := ","
			if i == len(d.Variants)-1 {
				comma = ""
			}

			g.linef("%s_%s%s", d.Name.Name, variant.Name, comma)
		}

		g.indent--
		g.linef("} %s;", d.Name.Name)
		g.line("")
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
			if arg, ok := subst[t.Parts[0].Name]; ok {
				g.emitImportedGenericStructDepsForGenericArg(arg, subst, visiting)
			}
		}

	case *ast.PointerType:
		g.emitImportedGenericStructDepsForType(t.Elem, subst, visiting)

	case *ast.ArrayType:
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

	names := make([]string, 0, len(g.packages))
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

		taskNames := make([]string, 0, len(pkg.Tasks))
		for taskName := range pkg.Tasks {
			taskNames = append(taskNames, taskName)
		}
		sort.Strings(taskNames)

		for _, taskName := range taskNames {
			info := pkg.Tasks[taskName]
			if len(info.ReturnTypes) <= 1 {
				continue
			}

			name := info.ReturnType.Name
			if name == "" {
				name = packageTaskResultStructName(pkgName, taskName)
			}

			if seen[name] {
				continue
			}
			seen[name] = true

			g.linef("typedef struct %s {", name)
			g.indent++

			for i, resultType := range info.ReturnTypes {
				g.linef("%s;", resultType.Decl(fmt.Sprintf("_%d", i)))
			}

			g.indent--
			g.linef("} %s;", name)
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
		if !ok || d.IsTest || len(d.Results) <= 1 {
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

func (g *Generator) packageTaskSignature(packageName string, taskName string, info TaskInfo) string {
	name := cImportedTaskName(packageName, taskName, info)
	ret := info.ReturnType.Name

	if len(info.ParamTypes) == 0 {
		return fmt.Sprintf("%s %s(void)", ret, name)
	}

	var params []string

	for i, paramType := range info.ParamTypes {
		if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
			if info.IsExtern {
				params = append(params, "...")
				break
			}

			params = append(params, g.variadicCType(paramType).Decl(fmt.Sprintf("arg%d", i)))
			break
		}

		params = append(params, paramType.Decl(fmt.Sprintf("arg%d", i)))
	}

	return fmt.Sprintf("%s %s(%s)", ret, name, strings.Join(params, ", "))
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

func (g *Generator) emitDefersInScope(sc *scope) {
	for i := len(sc.defers) - 1; i >= 0; i-- {
		g.linef("%s;", sc.defers[i])
	}
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

func (g *Generator) importedNamedCType(packageName string, typeName string) CType {
	name := cImportedTypeName(packageName, typeName)

	if pkg := g.packages[packageName]; pkg != nil {
		if iface := pkg.Interfaces[typeName]; iface != nil {
			return CType{
				Name:           name,
				SealName:       name,
				IsInterface:    true,
				IsDynInterface: iface.IsDyn,
			}
		}
	}

	return CType{
		Name:     name,
		SealName: name,
	}
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

func (g *Generator) emitInterfaces() {
	if len(g.interfaces) == 0 {
		return
	}

	names := make([]string, 0, len(g.interfaces))
	for name := range g.interfaces {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		d := g.interfaces[name]

		if d.IsDyn {
			g.emitDynInterface(d)
		} else {
			g.emitStaticInterface(d)
		}
	}
}

func (g *Generator) emitDynInterface(d *ast.InterfaceDecl) {
	g.linef("typedef struct %s_vtable {", d.Name.Name)
	g.indent++

	for _, req := range d.Requirements {
		ret := g.interfaceRequirementReturnType(req)

		var params []string
		params = append(params, "void *data")

		for i := 1; i < len(req.Params); i++ {
			paramType := g.cTypeFromAst(req.Params[i].Type)
			params = append(params, paramType.Decl(fmt.Sprintf("arg%d", i)))
		}

		g.linef("%s (*%s)(%s);", ret.Name, sanitizeCName(req.Name.Name), strings.Join(params, ", "))
	}

	g.indent--
	g.linef("} %s_vtable;", d.Name.Name)
	g.line("")

	g.linef("typedef struct %s {", d.Name.Name)
	g.indent++
	g.line("void *data;")
	g.linef("%s_vtable *vtable;", d.Name.Name)
	g.indent--
	g.linef("} %s;", d.Name.Name)
	g.line("")
}

func (g *Generator) emitStaticInterface(d *ast.InterfaceDecl) {
	iface := d.Name.Name
	concretes := g.staticInterfaceConcreteTypes(iface)

	g.linef("typedef enum %s_Tag {", iface)
	g.indent++
	g.linef("%s,", staticInterfaceTagName(iface, ""))

	for i, concrete := range concretes {
		comma := ","
		if i == len(concretes)-1 {
			comma = ""
		}

		g.linef("%s%s", staticInterfaceTagName(iface, concrete), comma)
	}

	g.indent--
	g.linef("} %s_Tag;", iface)
	g.line("")

	g.linef("typedef struct %s {", iface)
	g.indent++
	g.linef("%s_Tag tag;", iface)
	g.line("union {")
	g.indent++

	if len(concretes) == 0 {
		g.line("char _empty;")
	} else {
		for _, concrete := range concretes {
			field := sanitizeCName(concrete)
			g.linef("%s *%s;", concrete, field)
		}
	}

	g.indent--
	g.line("} as;")
	g.indent--
	g.linef("} %s;", iface)
	g.line("")
}

func (g *Generator) interfaceRequirementReturnType(req *ast.TaskSignature) CType {
	if len(req.Results) == 0 {
		return CVoid
	}

	if len(req.Results) == 1 {
		return g.cTypeFromAst(req.Results[0])
	}

	g.error(req.Loc, fmt.Sprintf("interface requirement %q has multiple returns; interface dispatch does not support this yet", req.Name.Name))
	return CInvalid
}

func (g *Generator) lookupInterfaceRequirement(iface string, name string) (*ast.TaskSignature, bool) {
	d := g.interfaces[iface]
	if d == nil {
		return nil, false
	}

	for _, req := range d.Requirements {
		if req.Name.Name == name {
			return req, true
		}
	}

	return nil, false
}

func (g *Generator) emitInterfaceDispatchCall(iface CType, taskName string, args []ast.Expr, preparedArgs []string) string {
	if g.isDynInterfaceName(iface.SealName) {
		return g.emitDynInterfaceDispatchCall(iface, taskName, args, preparedArgs)
	}

	return g.emitStaticInterfaceDispatchCall(iface, taskName, args, preparedArgs)
}

func (g *Generator) emitDynInterfaceDispatchCall(iface CType, taskName string, args []ast.Expr, preparedArgs []string) string {
	req, ok := g.lookupInterfaceRequirement(iface.SealName, taskName)
	if !ok {
		g.error(argsSpan(args), fmt.Sprintf("interface %s has no requirement %q", iface.SealName, taskName))
		return "0"
	}

	receiver := ""
	if len(preparedArgs) > 0 {
		receiver = preparedArgs[0]
	} else {
		receiver = g.emitExpr(args[0], nil)
	}

	var outArgs []string
	outArgs = append(outArgs, fmt.Sprintf("(%s).data", receiver))

	for i := 1; i < len(args); i++ {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := (*CType)(nil)
		if i < len(req.Params) {
			paramType := g.cTypeFromAst(req.Params[i].Type)
			expected = &paramType
		}

		outArgs = append(outArgs, g.emitExpr(args[i], expected))
	}

	return fmt.Sprintf(
		"(%s).vtable->%s(%s)",
		receiver,
		sanitizeCName(taskName),
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) emitStaticInterfaceDispatchCall(iface CType, taskName string, args []ast.Expr, preparedArgs []string) string {
	req, ok := g.lookupInterfaceRequirement(iface.SealName, taskName)
	if !ok {
		g.error(argsSpan(args), fmt.Sprintf("interface %s has no requirement %q", iface.SealName, taskName))
		return "0"
	}

	receiver := ""
	if len(preparedArgs) > 0 {
		receiver = preparedArgs[0]
	} else {
		receiver = g.emitExpr(args[0], nil)
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
			paramType := g.cTypeFromAst(req.Params[i].Type)
			expected = &paramType
		}

		outArgs = append(outArgs, g.emitExpr(args[i], expected))
	}

	return fmt.Sprintf(
		"%s(%s)",
		staticInterfaceDispatcherName(iface.SealName, taskName),
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) emitImplVTables() {
	if len(g.implInfos) == 0 {
		return
	}

	concreteNames := make([]string, 0, len(g.implInfos))
	for concrete := range g.implInfos {
		concreteNames = append(concreteNames, concrete)
	}
	sort.Strings(concreteNames)

	for _, concrete := range concreteNames {
		ifaceNames := make([]string, 0, len(g.implInfos[concrete]))
		for iface := range g.implInfos[concrete] {
			ifaceNames = append(ifaceNames, iface)
		}
		sort.Strings(ifaceNames)

		for _, iface := range ifaceNames {
			g.emitImplVTable(g.implInfos[concrete][iface])
		}
	}
}

func (g *Generator) emitStaticInterfaceDispatchers() {
	if len(g.interfaces) == 0 {
		return
	}

	names := make([]string, 0, len(g.interfaces))
	for name := range g.interfaces {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ifaceDecl := g.interfaces[name]
		if ifaceDecl == nil || ifaceDecl.IsDyn {
			continue
		}

		for _, req := range ifaceDecl.Requirements {
			g.emitStaticInterfaceDispatcher(ifaceDecl, req)
		}
	}
}

func (g *Generator) emitStaticInterfaceDispatcher(ifaceDecl *ast.InterfaceDecl, req *ast.TaskSignature) {
	iface := ifaceDecl.Name.Name
	ret := g.interfaceRequirementReturnType(req)
	name := staticInterfaceDispatcherName(iface, req.Name.Name)

	var params []string
	params = append(params, CType{Name: iface, SealName: iface, IsInterface: true}.Decl("receiver"))

	for i := 1; i < len(req.Params); i++ {
		paramType := g.cTypeFromAst(req.Params[i].Type)
		params = append(params, paramType.Decl(fmt.Sprintf("arg%d", i)))
	}

	g.linef("static %s %s(%s) {", ret.Name, name, strings.Join(params, ", "))
	g.indent++

	g.line("switch (receiver.tag) {")
	g.indent++

	for _, concrete := range g.staticInterfaceConcreteTypes(iface) {
		field := sanitizeCName(concrete)
		wrapper := interfaceWrapperName(iface, concrete, req.Name.Name)

		g.linef("case %s:", staticInterfaceTagName(iface, concrete))
		g.indent++

		var args []string
		args = append(args, fmt.Sprintf("(void *)(receiver).as.%s", field))

		for i := 1; i < len(req.Params); i++ {
			args = append(args, fmt.Sprintf("arg%d", i))
		}

		if ret.SealName == "void" {
			g.linef("%s(%s);", wrapper, strings.Join(args, ", "))
			g.line("return;")
		} else {
			g.linef("return %s(%s);", wrapper, strings.Join(args, ", "))
		}

		g.indent--
	}

	g.line("default:")
	g.indent++
	g.line("seal_panic_cstring(\"static interface dispatch on nil or invalid tag\");")

	if ret.SealName != "void" {
		g.line("return 0;")
	}

	g.indent--

	g.indent--
	g.line("}")

	g.indent--
	g.line("}")
	g.line("")
}

func (g *Generator) emitImplVTable(info *ImplInfo) {
	if info == nil {
		return
	}

	ifaceDecl := g.interfaces[info.Interface]
	if ifaceDecl == nil {
		return
	}

	for _, req := range ifaceDecl.Requirements {
		g.emitInterfaceWrapper(info, req)
	}

	if !ifaceDecl.IsDyn {
		return
	}

	vtableName := interfaceVTableName(info.Interface, info.Concrete)

	g.linef("static %s_vtable %s = {", info.Interface, vtableName)
	g.indent++

	for _, req := range ifaceDecl.Requirements {
		g.linef(".%s = %s,", sanitizeCName(req.Name.Name), interfaceWrapperName(info.Interface, info.Concrete, req.Name.Name))
	}

	g.indent--
	g.line("};")
	g.line("")
}

func (g *Generator) emitInterfaceWrapper(info *ImplInfo, req *ast.TaskSignature) {
	ret := g.interfaceRequirementReturnType(req)
	wrapperName := interfaceWrapperName(info.Interface, info.Concrete, req.Name.Name)

	entry, hasEntry := info.Entries[req.Name.Name]
	if !hasEntry {
		g.error(req.Name.Span(), fmt.Sprintf("impl %s<%s> is missing requirement %q", info.Interface, info.Concrete, req.Name.Name))
		return
	}

	var params []string
	params = append(params, "void *data")

	for i := 1; i < len(req.Params); i++ {
		paramType := g.cTypeFromAst(req.Params[i].Type)
		paramName := fmt.Sprintf("arg%d", i)

		if entry.Task != nil && i < len(entry.Task.Params) {
			paramName = entry.Task.Params[i].Name.Name
		}

		params = append(params, paramType.Decl(paramName))
	}

	g.linef("static %s %s(%s) {", ret.Name, wrapperName, strings.Join(params, ", "))
	g.indent++

	if entry.Alias != nil {
		targetName, ok := g.implAliasTaskName(entry.Alias)
		if !ok {
			g.error(entry.Alias.Span(), fmt.Sprintf("unsupported impl alias for %q", req.Name.Name))
			if ret.SealName != "void" {
				g.line("return 0;")
			}
			g.indent--
			g.line("}")
			g.line("")
			return
		}

		var callArgs []string
		callArgs = append(callArgs, fmt.Sprintf("(%s *)data", info.Concrete))

		for i := 1; i < len(req.Params); i++ {
			paramName := fmt.Sprintf("arg%d", i)
			callArgs = append(callArgs, paramName)
		}

		if ret.SealName == "void" {
			g.linef("%s(%s);", targetName, strings.Join(callArgs, ", "))
		} else {
			g.linef("return %s(%s);", targetName, strings.Join(callArgs, ", "))
		}

		g.indent--
		g.line("}")
		g.line("")
		return
	}

	if entry.Task != nil {
		oldScope := g.scope
		oldTaskScope := g.taskScope
		oldResults := g.currentResults

		g.scope = newScope(oldScope)
		g.taskScope = g.scope
		g.currentResults = nil

		for _, result := range entry.Task.Results {
			g.currentResults = append(g.currentResults, g.cTypeFromAst(result))
		}

		if len(entry.Task.Params) > 0 {
			first := entry.Task.Params[0]
			firstType := g.cTypeFromAst(first.Type)

			g.linef("%s = (%s)data;", firstType.Decl(first.Name.Name), firstType.Name)
			g.scope.declare(first.Name.Name, firstType)
		}

		for i := 1; i < len(entry.Task.Params); i++ {
			param := entry.Task.Params[i]
			paramType := g.cTypeFromAst(param.Type)
			g.scope.declare(param.Name.Name, paramType)
		}

		g.emitBlockStatements(entry.Task.Body)

		g.scope = oldScope
		g.taskScope = oldTaskScope
		g.currentResults = oldResults

		g.indent--
		g.line("}")
		g.line("")
		return
	}

	g.error(entry.Name.Span(), fmt.Sprintf("impl entry %q has no task body or alias", entry.Name.Name))

	if ret.SealName != "void" {
		g.line("return 0;")
	}

	g.indent--
	g.line("}")
	g.line("")
}

func (g *Generator) implAliasTaskName(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return g.cTaskName(e.Name.Name), true

	case *ast.SelectorExpr:
		id, ok := e.Left.(*ast.IdentExpr)
		if !ok {
			return "", false
		}

		pkg := g.packages[id.Name.Name]
		if pkg == nil {
			return "", false
		}

		info, ok := pkg.Tasks[e.Name.Name]
		if !ok {
			return "", false
		}

		return cImportedTaskName(id.Name.Name, e.Name.Name, info), true
	}

	return "", false
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
		g.error(s.Span(), "local declarations are not supported by C codegen yet")

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

	case *ast.DeferStmt:
		g.emitDeferStmt(s)

	case *ast.SealStmt:
		// `seal` is a checker/language rule. It has no first-backend C output.

	case *ast.ExprStmt:
		g.linef("%s;", g.emitExpr(s.Expr, nil))

	case *ast.AssignStmt:
		leftType := g.inferExprType(s.Left, nil)
		left := g.emitExpr(s.Left, nil)
		right := g.emitExpr(s.Right, &leftType)
		g.linef("%s %s %s;", left, g.cAssignOp(s.Op), right)

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

func (g *Generator) emitVarDeclStmt(s *ast.VarDeclStmt) {
	var typ CType

	if s.HasType {
		typ = g.cTypeFromAstInContext(s.Type)

		if typ.IsArray && typ.ArrayLen == "" && s.HasValue {
			if arr, ok := s.Value.(*ast.ArrayLiteralExpr); ok {
				typ.ArrayLen = fmt.Sprintf("%d", len(arr.Values))
			}
		}
	} else if s.HasValue {
		typ = g.inferExprType(s.Value, nil)
	} else {
		typ = CInvalid
	}

	if s.Name.Name == "_" {
		if s.HasValue {
			g.linef("(void)(%s);", g.emitExpr(s.Value, &typ))
		}
		return
	}

	g.scope.declare(s.Name.Name, typ)

	if s.HasValue {
		value := g.emitExpr(s.Value, &typ)

		if typ.IsArray {
			g.linef("%s = %s;", typ.Decl(s.Name.Name), value)
		} else {
			g.linef("%s = %s;", typ.Decl(s.Name.Name), value)
		}

		return
	}

	g.linef("%s;", typ.Decl(s.Name.Name))
}

func (g *Generator) emitForStmt(s *ast.ForStmt) {
	oldScope := g.scope
	g.scope = newScope(oldScope)

	if s.Init == nil && s.Cond == nil && s.Post == nil {
		g.line("for (;;) {")
		g.indent++
		g.emitBlockStatements(s.Body)
		g.emitDefersInScope(g.scope)
		g.indent--
		g.line("}")
		g.scope = oldScope
		return
	}

	if s.Init == nil && s.Post == nil {
		cond := g.emitExpr(s.Cond, &CBool)
		g.linef("for (; %s; ) {", cond)
		g.indent++
		g.emitBlockStatements(s.Body)
		g.emitDefersInScope(g.scope)
		g.indent--
		g.line("}")
		g.scope = oldScope
		return
	}

	init := ""
	if s.Init != nil {
		init = g.emitForPart(s.Init)
	}

	cond := ""
	if s.Cond != nil {
		cond = g.emitExpr(s.Cond, &CBool)
	}

	post := ""
	if s.Post != nil {
		post = g.emitForPart(s.Post)
	}

	g.linef("for (%s; %s; %s) {", init, cond, post)
	g.indent++
	g.emitBlockStatements(s.Body)
	g.emitDefersInScope(g.scope)
	g.indent--
	g.line("}")

	g.scope = oldScope
}

func (g *Generator) emitForPart(stmt ast.Stmt) string {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		var typ CType

		if s.HasType {
			typ = g.cTypeFromAstInContext(s.Type)
		} else if s.HasValue {
			typ = g.inferExprType(s.Value, nil)
		} else {
			typ = CInvalid
		}

		g.scope.declare(s.Name.Name, typ)

		if s.HasValue {
			value := g.emitExpr(s.Value, &typ)
			return fmt.Sprintf("%s = %s", typ.Decl(s.Name.Name), value)
		}

		return typ.Decl(s.Name.Name)

	case *ast.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.emitExpr(s.Left, nil), g.cAssignOp(s.Op), g.emitExpr(s.Right, nil))

	case *ast.ExprStmt:
		return g.emitExpr(s.Expr, nil)

	default:
		g.error(stmt.Span(), "unsupported for-loop component in C codegen")
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

func (g *Generator) taskInfoFromGenericArg(arg ast.GenericArg) (TaskInfo, bool) {
	if g.genericSubst != nil {
		arg = g.substituteGenericArgForCGen(arg, g.genericSubst)
	}

	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		return TaskInfo{}, false
	}

	switch e := arg.Expr.(type) {
	case *ast.IdentExpr:
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

		return info, true

	case *ast.GenericExpr:
		switch base := e.Base.(type) {
		case *ast.IdentExpr:
			info, ok := g.tasks[base.Name.Name]
			if !ok || len(info.GenericParams) == 0 || info.Decl == nil {
				return TaskInfo{}, false
			}

			callArgs := e.Args
			if g.genericSubst != nil {
				callArgs = make([]ast.GenericArg, 0, len(e.Args))
				for _, genericArg := range e.Args {
					callArgs = append(callArgs, g.substituteGenericArgForCGen(genericArg, g.genericSubst))
				}
			}

			name := g.registerGenericTaskInstance(info.Decl, callArgs)
			instance := g.genericTasks[name]
			if instance == nil {
				return TaskInfo{}, false
			}

			return g.taskInfoFromGenericTaskInstance(instance), true

		case *ast.SelectorExpr:
			pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(base)
			if !ok {
				return TaskInfo{}, false
			}

			callArgs := e.Args
			if g.genericSubst != nil {
				callArgs = make([]ast.GenericArg, 0, len(e.Args))
				for _, genericArg := range e.Args {
					callArgs = append(callArgs, g.substituteGenericArgForCGen(genericArg, g.genericSubst))
				}
			}

			name := g.registerImportedGenericTaskInstance(pkgName, taskName, info, callArgs)
			instance := g.importedGenericTasks[name]
			if instance == nil {
				return TaskInfo{}, false
			}

			return g.taskInfoFromImportedGenericTaskInstance(instance), true
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

func (g *Generator) taskCallNameFromGenericArg(arg ast.GenericArg) (string, bool) {
	if g.genericSubst != nil {
		arg = g.substituteGenericArgForCGen(arg, g.genericSubst)
	}

	if arg.Kind != ast.GenericArgExpr || arg.Expr == nil {
		g.error(arg.Span(), "generic task parameter requires a task argument")
		return "0", true
	}

	switch e := arg.Expr.(type) {
	case *ast.IdentExpr:
		info, ok := g.tasks[e.Name.Name]
		if !ok {
			g.error(e.Span(), fmt.Sprintf("unknown task argument %q", e.Name.Name))
			return "0", true
		}

		if len(info.GenericParams) > 0 {
			g.error(e.Span(), fmt.Sprintf("generic task argument %q requires specialization", e.Name.Name))
			return "0", true
		}

		return g.cTaskName(e.Name.Name), true

	case *ast.SelectorExpr:
		id, ok := e.Left.(*ast.IdentExpr)
		if !ok {
			g.error(e.Span(), "unsupported task argument selector")
			return "0", true
		}

		pkg := g.packages[id.Name.Name]
		if pkg == nil {
			g.error(e.Span(), fmt.Sprintf("unknown package %q", id.Name.Name))
			return "0", true
		}

		info, ok := pkg.Tasks[e.Name.Name]
		if !ok {
			g.error(e.Span(), fmt.Sprintf("package %s has no task %q", id.Name.Name, e.Name.Name))
			return "0", true
		}

		if len(info.GenericParams) > 0 {
			g.error(e.Span(), fmt.Sprintf("imported generic task argument %q requires specialization", e.Name.Name))
			return "0", true
		}

		return cImportedTaskName(id.Name.Name, e.Name.Name, info), true

	case *ast.GenericExpr:
		switch base := e.Base.(type) {
		case *ast.IdentExpr:
			info, ok := g.tasks[base.Name.Name]
			if !ok || info.Decl == nil || len(info.GenericParams) == 0 {
				g.error(e.Span(), fmt.Sprintf("generic task argument %q is not supported by C codegen yet", base.Name.Name))
				return "0", true
			}

			callArgs := e.Args
			if g.genericSubst != nil {
				callArgs = make([]ast.GenericArg, 0, len(e.Args))
				for _, genericArg := range e.Args {
					callArgs = append(callArgs, g.substituteGenericArgForCGen(genericArg, g.genericSubst))
				}
			}

			name := g.registerGenericTaskInstance(info.Decl, callArgs)
			return name, true

		case *ast.SelectorExpr:
			pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(base)
			if !ok {
				g.error(e.Span(), "unsupported imported generic task argument")
				return "0", true
			}

			callArgs := e.Args
			if g.genericSubst != nil {
				callArgs = make([]ast.GenericArg, 0, len(e.Args))
				for _, genericArg := range e.Args {
					callArgs = append(callArgs, g.substituteGenericArgForCGen(genericArg, g.genericSubst))
				}
			}

			name := g.registerImportedGenericTaskInstance(pkgName, taskName, info, callArgs)
			return name, true
		}
	}

	g.error(arg.Span(), "unsupported generic task argument")
	return "0", true
}

func (g *Generator) emitExpr(expr ast.Expr, expected *CType) string {
	if expected != nil && expected.SealName == "any" {
		return g.emitAnyExpr(expr)
	}

	if expected != nil {
		if value, ok := g.tryEmitInterfaceConversion(*expected, expr); ok {
			return value
		}
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope != nil {
			if _, ok := g.scope.lookup(e.Name.Name); ok {
				return e.Name.Name
			}
		}

		if g.genericSubst != nil {
			if arg, ok := g.genericSubst[e.Name.Name]; ok && arg.Kind == ast.GenericArgExpr && arg.Expr != nil {
				if genericArgIsSingleNameForCGen(arg, e.Name.Name) {
					return e.Name.Name
				}

				return g.emitExpr(arg.Expr, expected)
			}
		}

		return e.Name.Name

	case *ast.DotIdentExpr:
		if expected != nil && expected.SealName != "" {
			if _, ok := g.enums[expected.SealName]; ok {
				return fmt.Sprintf("%s_%s", expected.SealName, e.Name.Name)
			}
		}

		g.error(e.Span(), fmt.Sprintf("enum literal .%s needs C codegen context", e.Name.Name))
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
		g.error(e.Span(), "generic expression cannot be emitted as a value")
		return "0"

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		if expected != nil && g.isUnion(*expected) {
			return fmt.Sprintf("(%s){.tag = %s_Tag_nil}", expected.Name, expected.SealName)
		}

		if expected != nil && g.isInterfaceCType(*expected) {
			return fmt.Sprintf("(%s){.data = NULL, .vtable = NULL}", expected.Name)
		}

		return "NULL"

	case *ast.UnaryExpr:
		return fmt.Sprintf("(%s%s)", g.cUnaryOp(e.Op), g.emitExpr(e.Expr, nil))

	case *ast.BinaryExpr:
		leftType := g.inferExprType(e.Left, nil)
		rightType := g.inferExprType(e.Right, nil)

		if g.hasOperatorOverload(e.Op.String()) {
			if candidate, ok := g.resolveOverload(e.Op.String(), []CType{leftType, rightType}); ok {
				left := g.emitExpr(e.Left, &leftType)
				right := g.emitExpr(e.Right, &rightType)
				return fmt.Sprintf("%s(%s, %s)", g.cTaskName(candidate), left, right)
			}
		}

		if e.Op == token.NotEq && g.hasOperatorOverload("==") {
			if candidate, ok := g.resolveOverload("==", []CType{leftType, rightType}); ok {
				left := g.emitExpr(e.Left, &leftType)
				right := g.emitExpr(e.Right, &rightType)
				return fmt.Sprintf("(!%s(%s, %s))", g.cTaskName(candidate), left, right)
			}
		}

		left := g.emitExpr(e.Left, nil)
		right := g.emitExpr(e.Right, nil)
		return fmt.Sprintf("(%s %s %s)", left, g.cBinaryOp(e.Op), right)

	case *ast.CallExpr:
		return g.emitCallExpr(e)

	case *ast.SpreadExpr:
		g.error(e.Span(), "spread can only be emitted as a call argument")
		return "0"

	case *ast.SelectorExpr:
		if id, ok := e.Left.(*ast.IdentExpr); ok {
			if _, ok := g.packages[id.Name.Name]; ok {
				// Package selector as value is only valid through calls for now.
				return cPackageTaskName(id.Name.Name, e.Name.Name)
			}
		}

		left := g.emitExpr(e.Left, nil)
		leftType := g.inferExprType(e.Left, nil)

		if leftType.SealName == "string" {
			g.error(e.Name.Span(), fmt.Sprintf("string has no field %q; use size(s) or s[i]", e.Name.Name))
			return "0"
		}

		if leftType.SealName == "cstring" {
			g.error(e.Name.Span(), fmt.Sprintf("cstring has no field %q", e.Name.Name))
			return "0"
		}

		if strings.HasSuffix(leftType.SealName, "*") {
			return fmt.Sprintf("(%s)->%s", left, e.Name.Name)
		}

		if strings.HasPrefix(leftType.SealName, "*") {
			return fmt.Sprintf("(%s)->%s", left, e.Name.Name)
		}

		return fmt.Sprintf("(%s).%s", left, e.Name.Name)

	case *ast.IndexExpr:
		leftType := g.inferExprType(e.Left, nil)
		left := g.emitExpr(e.Left, nil)
		index := g.emitExpr(e.Index, &CInt)

		if leftType.SealName == "string" {
			return fmt.Sprintf("sealString_at(%s, (ptrdiff_t)(%s))", left, index)
		}

		if leftType.SealName == "cstring" {
			g.error(e.Left.Span(), "cstring does not support character indexing")
			return "0"
		}

		if leftType.SealName == "rawptr" {
			return fmt.Sprintf("((unsigned char *)(%s))[%s]", left, index)
		}

		if leftType.IsVariadic {
			return fmt.Sprintf("(%s).data[%s]", left, index)
		}

		if leftType.IsArray {
			return fmt.Sprintf("%s[%s]", left, index)
		}

		if g.isByteIndexableCType(leftType) {
			return g.emitByteIndexExpr(e, leftType, left, index)
		}

		g.error(e.Left.Span(), fmt.Sprintf("cannot index type %s", leftType.String()))
		return "0"

	case *ast.ArrayLiteralExpr:
		var values []string

		elemExpected := (*CType)(nil)
		if expected != nil && expected.IsArray && expected.Elem != nil {
			elemExpected = expected.Elem
		}

		for _, value := range e.Values {
			values = append(values, g.emitExpr(value, elemExpected))
		}

		return "{" + strings.Join(values, ", ") + "}"

	case *ast.CompoundLiteralExpr:
		typ := g.cTypeFromAstInContext(e.Type)

		if _, ok := g.distincts[typ.SealName]; ok {
			g.error(e.Span(), fmt.Sprintf("distinct type %s cannot be constructed with a literal; use cast<%s>(value)", typ.SealName, typ.SealName))
			return "0"
		}

		if expected != nil && g.isUnion(*expected) && g.unionHasMember(expected.SealName, typ.SealName) {
			payload := g.emitCompoundLiteral(e, typ)
			return fmt.Sprintf("(%s){.tag = %s_Tag_%s, .as.%s = %s}",
				expected.Name,
				expected.SealName,
				typ.SealName,
				typ.SealName,
				payload,
			)
		}

		return g.emitCompoundLiteral(e, typ)
	}

	g.error(expr.Span(), "unsupported expression in C codegen")
	return "0"
}

func (g *Generator) emitStringLiteral(e *ast.StringLitExpr) string {
	value, err := strconv.Unquote(e.Value)
	if err != nil {
		g.error(e.Span(), fmt.Sprintf("invalid string literal: %v", err))
		return "(sealString){.data = NULL, .byte_len = 0}"
	}

	return fmt.Sprintf(
		"(sealString){.data = (const unsigned char *)%s, .byte_len = %d}",
		strconv.Quote(value),
		len([]byte(value)),
	)
}

func (g *Generator) emitCStringLiteral(e *ast.CStringLitExpr) string {
	raw := strings.TrimPrefix(e.Value, "c")

	value, err := strconv.Unquote(raw)
	if err != nil {
		g.error(e.Span(), fmt.Sprintf("invalid cstring literal: %v", err))
		return "\"\""
	}

	return strconv.Quote(value)
}

func (g *Generator) emitCharLiteral(e *ast.CharLitExpr) string {
	value, err := strconv.Unquote(e.Value)
	if err != nil {
		g.error(e.Span(), fmt.Sprintf("invalid char literal: %v", err))
		return "0"
	}

	runes := []rune(value)
	if len(runes) != 1 {
		g.error(e.Span(), "char literal must contain exactly one Unicode scalar value")
		return "0"
	}

	return fmt.Sprintf("%d", runes[0])
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
	if id, ok := gen.Base.(*ast.IdentExpr); ok {
		if task, ok := builtin.LookupTask(id.Name.Name); ok && task.Generic {
			return g.emitGenericIntrinsicCall(gen, args)
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

func (g *Generator) emitCallExprWithArgs(e *ast.CallExpr, preparedArgs []string) string {
	if gen, ok := e.Callee.(*ast.GenericExpr); ok {
		return g.emitGenericCall(gen, e.Args, preparedArgs)
	}

	if id, ok := e.Callee.(*ast.IdentExpr); ok {
		isLocal := false
		if g.scope != nil {
			_, isLocal = g.scope.lookup(id.Name.Name)
		}

		if !isLocal {
			if name, ok := g.genericTaskParamCallName(id.Name.Name); ok {
				info, hasInfo := g.genericTaskParamInfo(id.Name.Name)
				if hasInfo {
					return g.emitDirectCNamedCall(name, e.Args, preparedArgs, info.ParamTypes)
				}

				return g.emitDirectCNamedCall(name, e.Args, preparedArgs, nil)
			}
		}

		if kind, ok := g.primitiveTaskKind(id.Name.Name); ok {
			switch kind {
			case builtin.TaskLen:
				return g.emitLenCall(e)

			case builtin.TaskSize:
				return g.emitSizeCall(e)

			case builtin.TaskAssert:
				return g.emitAssertCall(e)

			case builtin.TaskPanic:
				return g.emitPanicCall(e)

			case builtin.TaskTrap:
				return g.emitNoArgRuntimeCall("trap", "seal_trap", e)

			case builtin.TaskUnreachable:
				return g.emitNoArgRuntimeCall("unreachable", "seal_unreachable", e)
			}
		}

		if len(e.Args) > 0 {
			firstType := g.inferExprType(e.Args[0], nil)

			if g.isInterfaceCType(firstType) {
				if _, ok := g.lookupInterfaceRequirement(firstType.SealName, id.Name.Name); ok {
					return g.emitInterfaceDispatchCall(firstType, id.Name.Name, e.Args, preparedArgs)
				}
			}
		}

		if _, ok := g.tasks[id.Name.Name]; ok {
			return g.emitTaskCall(id.Name.Name, e.Args, preparedArgs)
		}

		if _, ok := g.overloads[id.Name.Name]; ok {
			argTypes := make([]CType, 0, len(e.Args))
			for _, arg := range e.Args {
				argTypes = append(argTypes, g.inferExprType(arg, nil))
			}

			if candidate, ok := g.resolveOverload(id.Name.Name, argTypes); ok {
				return g.emitTaskCall(candidate, e.Args, preparedArgs)
			}
		}
	}

	if selector, ok := e.Callee.(*ast.SelectorExpr); ok {
		if id, ok := selector.Left.(*ast.IdentExpr); ok {
			if pkg := g.packages[id.Name.Name]; pkg != nil {
				return g.emitPackageTaskCall(id.Name.Name, selector.Name.Name, e.Args, preparedArgs)
			}
		}
	}

	var args []string

	if preparedArgs != nil {
		args = append(args, preparedArgs...)
	} else {
		for _, arg := range e.Args {
			args = append(args, g.emitExpr(arg, nil))
		}
	}

	return fmt.Sprintf("%s(%s)", g.emitExpr(e.Callee, nil), strings.Join(args, ", "))
}

func (g *Generator) emitGenericIntrinsicCall(gen *ast.GenericExpr, args []ast.Expr) string {
	id, ok := gen.Base.(*ast.IdentExpr)
	if !ok {
		g.error(gen.Base.Span(), "only intrinsic generic calls are supported here")
		return "0"
	}

	task, ok := builtin.LookupTask(id.Name.Name)
	if !ok || !task.Generic {
		g.error(id.Span(), fmt.Sprintf("unknown generic intrinsic %q", id.Name.Name))
		return "0"
	}

	if len(gen.Args) != 1 {
		g.error(gen.Span(), fmt.Sprintf("%s expects exactly 1 type argument", id.Name.Name))
		return "0"
	}

	if len(args) != 1 {
		g.error(gen.Span(), fmt.Sprintf("%s expects exactly 1 value argument", id.Name.Name))
		return "0"
	}

	targetArg := gen.Args[0]
	if g.genericSubst != nil {
		targetArg = g.substituteGenericArgForCGen(targetArg, g.genericSubst)
	}

	target := g.cTypeFromGenericArg(targetArg)
	value := g.emitExpr(args[0], nil)

	switch task.Kind {
	case builtin.TaskAnyIs:
		kind, ok := g.sealTypeKindFor(target)
		if !ok {
			g.error(gen.Args[0].Span(), fmt.Sprintf("anyIs does not support %s yet", target.String()))
			return "false"
		}

		return fmt.Sprintf("((%s).type == %s)", value, kind)

	case builtin.TaskAnyAs:
		field, ok := g.sealAnyFieldFor(target)
		if !ok {
			g.error(gen.Args[0].Span(), fmt.Sprintf("anyAs does not support %s yet", target.String()))
			return "0"
		}

		if target.SealName == "any" {
			return value
		}

		return fmt.Sprintf("((%s).value.%s)", value, field)

	case builtin.TaskCast:
		return fmt.Sprintf("((%s)(%s))", target.Name, value)

	default:
		g.error(id.Span(), fmt.Sprintf("unknown generic intrinsic %q", id.Name.Name))
		return "0"
	}
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

func (g *Generator) registerImportedGenericTaskInstance(packageName string, taskName string, info TaskInfo, args []ast.GenericArg) string {
	name := g.specializedImportedTaskCName(packageName, taskName, info, args)

	if _, exists := g.importedGenericTasks[name]; exists {
		g.addImportedGenericTaskRequest(packageName, taskName, args)
		return name
	}

	copiedArgs := append([]ast.GenericArg(nil), args...)

	g.importedGenericTasks[name] = &ImportedGenericTaskInstance{
		PackageName: packageName,
		TaskName:    taskName,
		Name:        name,
		Info:        info,
		Args:        copiedArgs,
	}

	g.addImportedGenericTaskRequest(packageName, taskName, copiedArgs)

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

func (g *Generator) emitLenCall(e *ast.CallExpr) string {
	if len(e.Args) != 1 {
		g.error(e.Span(), "len expects 1 argument")
		return "0"
	}

	argType := g.inferExprType(e.Args[0], nil)
	arg := g.emitExpr(e.Args[0], nil)

	if argType.IsVariadic {
		return fmt.Sprintf("((uintptr_t)(%s).len)", arg)
	}

	if argType.IsArray {
		return fmt.Sprintf("(uintptr_t)%s", argType.ArrayLen)
	}

	g.error(e.Args[0].Span(), fmt.Sprintf("len does not support %s", argType.String()))
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

func (g *Generator) emitSpreadAsVariadic(elem CType, spread *ast.SpreadExpr) string {
	variadicType := g.variadicCType(elem)
	srcType := g.inferExprType(spread.Expr, nil)

	if srcType.IsVariadic {
		if srcType.Elem == nil {
			g.error(spread.Span(), "cannot spread invalid variadic value")
			return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
		}

		if srcType.Elem.SealName != elem.SealName {
			g.error(spread.Span(), fmt.Sprintf("cannot spread %s into ...%s", srcType.String(), elem.SealName))
			return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
		}

		return g.emitExpr(spread.Expr, nil)
	}

	if srcType.IsArray {
		if srcType.Elem == nil {
			g.error(spread.Span(), "cannot spread invalid array")
			return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
		}

		if srcType.ArrayLen == "" {
			g.error(spread.Span(), "cannot spread array with unknown length")
			return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
		}

		if srcType.Elem.SealName == elem.SealName {
			return fmt.Sprintf(
				"(%s){.data = %s, .len = %s}",
				variadicType.Name,
				g.emitArrayDataPointer(spread.Expr, elem),
				srcType.ArrayLen,
			)
		}

		return g.emitRepackedArraySpread(elem, spread.Expr, srcType, variadicType)
	}

	g.error(spread.Span(), fmt.Sprintf("cannot spread %s; expected array or variadic value", srcType.String()))
	return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
}

func (g *Generator) emitArrayDataPointer(expr ast.Expr, elem CType) string {
	if arr, ok := expr.(*ast.ArrayLiteralExpr); ok {
		var values []string

		for _, value := range arr.Values {
			values = append(values, g.emitExpr(value, &elem))
		}

		return fmt.Sprintf("(%s[]){%s}", elem.Name, strings.Join(values, ", "))
	}

	return g.emitExpr(expr, nil)
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

func (g *Generator) emitRepackedArraySpread(elem CType, expr ast.Expr, srcType CType, variadicType CType) string {
	length, err := strconv.Atoi(srcType.ArrayLen)
	if err != nil {
		g.error(expr.Span(), "cannot repack spread array with non-literal length")
		return fmt.Sprintf("(%s){.data = NULL, .len = 0}", variadicType.Name)
	}

	var values []string

	if arr, ok := expr.(*ast.ArrayLiteralExpr); ok {
		for _, value := range arr.Values {
			values = append(values, g.emitExpr(value, &elem))
		}
	} else {
		for i := 0; i < length; i++ {
			indexExpr := &ast.IndexExpr{
				Left: expr,
				Index: &ast.IntLitExpr{
					Value: fmt.Sprintf("%d", i),
					Loc:   expr.Span(),
				},
				Loc: expr.Span(),
			}

			values = append(values, g.emitExpr(indexExpr, &elem))
		}
	}

	return fmt.Sprintf(
		"(%s){.data = (%s[]){%s}, .len = %d}",
		variadicType.Name,
		elem.Name,
		strings.Join(values, ", "),
		length,
	)
}

func (g *Generator) emitArrayElementLiteral(elem CType, arg ast.Expr) string {
	if !elem.IsArray || elem.Elem == nil {
		return g.emitExpr(arg, &elem)
	}

	argType := g.inferExprType(arg, nil)

	if lit, ok := arg.(*ast.ArrayLiteralExpr); ok {
		var values []string
		for _, value := range lit.Values {
			values = append(values, g.emitExpr(value, elem.Elem))
		}

		return "{" + strings.Join(values, ", ") + "}"
	}

	if !argType.IsArray || argType.ArrayLen == "" {
		return "{" + g.emitExpr(arg, &elem) + "}"
	}

	length, err := strconv.Atoi(argType.ArrayLen)
	if err != nil {
		return "{" + g.emitExpr(arg, &elem) + "}"
	}

	var values []string
	for i := 0; i < length; i++ {
		indexExpr := &ast.IndexExpr{
			Left: arg,
			Index: &ast.IntLitExpr{
				Value: fmt.Sprintf("%d", i),
				Loc:   arg.Span(),
			},
			Loc: arg.Span(),
		}

		values = append(values, g.emitExpr(indexExpr, elem.Elem))
	}

	return "{" + strings.Join(values, ", ") + "}"
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

		if elem.IsArray && elem.Elem != nil {
			values = append(values, g.emitArrayElementLiteral(elem, arg))
			continue
		}

		values = append(values, g.emitExpr(arg, &elem))
	}

	if elem.IsArray && elem.Elem != nil {
		return fmt.Sprintf(
			"(%s){.data = (%s[][%s]){%s}, .len = %d}",
			variadicType.Name,
			elem.Elem.Name,
			elem.ArrayLen,
			strings.Join(values, ", "),
			len(values),
		)
	}

	return fmt.Sprintf(
		"(%s){.data = (%s[]){%s}, .len = %d}",
		variadicType.Name,
		elem.Name,
		strings.Join(values, ", "),
		len(values),
	)
}

func (g *Generator) emitDeferStmt(s *ast.DeferStmt) {
	call, ok := s.Call.(*ast.CallExpr)
	if !ok {
		g.error(s.Span(), "defer currently supports only task calls")
		return
	}

	code := g.emitDeferredCall(call)
	g.scope.addDefer(code)
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

func (g *Generator) emitPackageTaskCall(packageName string, taskName string, args []ast.Expr, preparedArgs []string) string {
	pkg := g.packages[packageName]
	if pkg == nil {
		g.error(argsSpan(args), fmt.Sprintf("unknown package %q", packageName))
		return "0"
	}

	info, hasTask := pkg.Tasks[taskName]
	if !hasTask {
		g.error(argsSpan(args), fmt.Sprintf("package %s has no task %q", packageName, taskName))
		return "0"
	}

	name := cImportedTaskName(packageName, taskName, info)

	if info.IsVariadic && !info.IsExtern {
		return g.emitSealVariadicTaskCall(name, info, args, preparedArgs)
	}

	var outArgs []string

	for i, arg := range args {
		if preparedArgs != nil && i < len(preparedArgs) {
			outArgs = append(outArgs, preparedArgs[i])
			continue
		}

		expected := (*CType)(nil)
		if i < len(info.ParamTypes) {
			if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
				expected = nil
			} else {
				expected = &info.ParamTypes[i]
			}
		}

		outArgs = append(outArgs, g.emitExpr(arg, expected))
	}

	if !info.IsVariadic {
		for i := len(args); i < len(info.ParamTypes); i++ {
			if i < len(info.ParamHasDefault) && info.ParamHasDefault[i] {
				expected := info.ParamTypes[i]
				outArgs = append(outArgs, g.emitExpr(info.ParamDefaults[i], &expected))
			}
		}
	}

	return fmt.Sprintf("%s(%s)", name, strings.Join(outArgs, ", "))
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
	g.line("const unsigned char *data;")
	g.line("size_t byte_len;")
	g.indent--
	g.line("} sealString;")
	g.line("")

	g.line("static inline uint32_t sealUtf8DecodeAdvance(const unsigned char *data, size_t byte_len, size_t *offset) {")
	g.indent++
	g.line("if (*offset >= byte_len) return 0;")
	g.line("unsigned char b0 = data[*offset];")
	g.line("if (b0 < 0x80) { *offset += 1; return (uint32_t)b0; }")
	g.line("if ((b0 & 0xE0) == 0xC0 && *offset + 1 < byte_len) {")
	g.indent++
	g.line("uint32_t cp = ((uint32_t)(b0 & 0x1F) << 6) | (uint32_t)(data[*offset + 1] & 0x3F);")
	g.line("*offset += 2;")
	g.line("return cp;")
	g.indent--
	g.line("}")
	g.line("if ((b0 & 0xF0) == 0xE0 && *offset + 2 < byte_len) {")
	g.indent++
	g.line("uint32_t cp = ((uint32_t)(b0 & 0x0F) << 12) | ((uint32_t)(data[*offset + 1] & 0x3F) << 6) | (uint32_t)(data[*offset + 2] & 0x3F);")
	g.line("*offset += 3;")
	g.line("return cp;")
	g.indent--
	g.line("}")
	g.line("if ((b0 & 0xF8) == 0xF0 && *offset + 3 < byte_len) {")
	g.indent++
	g.line("uint32_t cp = ((uint32_t)(b0 & 0x07) << 18) | ((uint32_t)(data[*offset + 1] & 0x3F) << 12) | ((uint32_t)(data[*offset + 2] & 0x3F) << 6) | (uint32_t)(data[*offset + 3] & 0x3F);")
	g.line("*offset += 4;")
	g.line("return cp;")
	g.indent--
	g.line("}")
	g.line("*offset += 1;")
	g.line("return 0xFFFD;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline size_t sealString_len(sealString s) {")
	g.indent++
	g.line("size_t offset = 0;")
	g.line("size_t count = 0;")
	g.line("while (offset < s.byte_len) {")
	g.indent++
	g.line("(void)sealUtf8DecodeAdvance(s.data, s.byte_len, &offset);")
	g.line("count += 1;")
	g.indent--
	g.line("}")
	g.line("return count;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t sealString_at(sealString s, ptrdiff_t index) {")
	g.indent++
	g.line("size_t char_len = sealString_len(s);")
	g.line("ptrdiff_t resolved = index;")
	g.line("if (resolved < 0) resolved = (ptrdiff_t)char_len + resolved;")
	g.line("if (resolved < 0 || (size_t)resolved >= char_len) return 0;")
	g.line("size_t offset = 0;")
	g.line("size_t current = 0;")
	g.line("while (offset < s.byte_len) {")
	g.indent++
	g.line("uint32_t cp = sealUtf8DecodeAdvance(s.data, s.byte_len, &offset);")
	g.line("if (current == (size_t)resolved) return cp;")
	g.line("current += 1;")
	g.indent--
	g.line("}")
	g.line("return 0;")
	g.indent--
	g.line("}")
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
	g.line("fprintf(stderr, \"panic\\n\");")
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_cstring(const char *message) {")
	g.indent++
	g.line("fprintf(stderr, \"panic: %s\\n\", message ? message : \"<null>\");")
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_string(sealString message) {")
	g.indent++
	g.line("fprintf(stderr, \"panic: %.*s\\n\", (int)message.byte_len, (const char *)message.data);")
	g.line("abort();")
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

func (g *Generator) emitVariadicRuntimeType(elem CType) {
	variadicType := g.variadicCType(elem)
	name := variadicType.Name

	if g.emittedVariadics[name] {
		return
	}
	g.emittedVariadics[name] = true

	g.linef("typedef struct %s {", name)
	g.indent++

	if elem.IsArray && elem.Elem != nil {
		g.linef("%s (*data)[%s];", elem.Elem.Name, elem.ArrayLen)
	} else {
		g.linef("%s *data;", elem.Name)
	}

	g.line("size_t len;")
	g.indent--
	g.linef("} %s;", name)
}

func (g *Generator) variadicCType(elem CType) CType {
	elemName := elem.SealName

	if elem.IsArray && elem.Elem != nil {
		length := elem.ArrayLen
		if length == "" {
			length = "inferred"
		}

		elemName = "array_" + sanitizeCName(length) + "_" + sanitizeCName(elem.Elem.SealName)
	}

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

func (g *Generator) callReturnTypes(expr ast.Expr) []CType {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return []CType{g.inferExprType(expr, nil)}
	}

	if id, ok := call.Callee.(*ast.IdentExpr); ok {
		if results, ok := g.genericTaskParamReturnTypes(id.Name.Name); ok {
			return results
		}
	}

	if gen, ok := call.Callee.(*ast.GenericExpr); ok {
		if id, ok := gen.Base.(*ast.IdentExpr); ok {
			info, ok := g.tasks[id.Name.Name]
			if ok && len(info.GenericParams) > 0 {
				if info.Decl == nil {
					return []CType{CInvalid}
				}

				callArgs := g.genericArgsInContext(gen.Args)
				name := g.registerGenericTaskInstance(info.Decl, callArgs)
				instance := g.genericTasks[name]
				if instance == nil {
					return []CType{CInvalid}
				}

				return g.genericTaskReturnTypes(instance)
			}
		}

		if selector, ok := gen.Base.(*ast.SelectorExpr); ok {
			pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(selector)
			if ok {
				callArgs := g.genericArgsInContext(gen.Args)
				name := g.registerImportedGenericTaskInstance(pkgName, taskName, info, callArgs)
				instance := g.importedGenericTasks[name]
				if instance == nil {
					return []CType{CInvalid}
				}

				return g.importedGenericTaskReturnTypes(instance)
			}
		}
	}

	if id, ok := call.Callee.(*ast.IdentExpr); ok {
		if g.scope == nil {
			if results, ok := g.genericTaskParamReturnTypes(id.Name.Name); ok {
				return results
			}
		} else if _, isLocal := g.scope.lookup(id.Name.Name); !isLocal {
			if results, ok := g.genericTaskParamReturnTypes(id.Name.Name); ok {
				return results
			}
		}

		if id.Name.Name == "len" {
			return []CType{CUint}
		}

		if id.Name.Name == "size" {
			return []CType{CUint}
		}

		if id.Name.Name == "assert" {
			return []CType{CVoid}
		}

		if info, ok := g.tasks[id.Name.Name]; ok {
			return info.ReturnTypes
		}

		if _, ok := g.overloads[id.Name.Name]; ok {
			argTypes := make([]CType, 0, len(call.Args))
			for _, arg := range call.Args {
				argTypes = append(argTypes, g.inferExprType(arg, nil))
			}

			candidate, ok := g.resolveOverload(id.Name.Name, argTypes)
			if !ok {
				return []CType{CInvalid}
			}

			return g.tasks[candidate].ReturnTypes
		}
	}

	if selector, ok := call.Callee.(*ast.SelectorExpr); ok {
		if id, ok := selector.Left.(*ast.IdentExpr); ok {
			if pkg := g.packages[id.Name.Name]; pkg != nil {
				if info, ok := pkg.Tasks[selector.Name.Name]; ok {
					return info.ReturnTypes
				}
			}
		}
	}

	return []CType{g.inferExprType(expr, nil)}
}

func (g *Generator) inferExprType(expr ast.Expr, expected *CType) CType {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope != nil {
			if v, ok := g.scope.lookup(e.Name.Name); ok {
				return v.Type
			}
		}

		if g.genericSubst != nil {
			if arg, ok := g.genericSubst[e.Name.Name]; ok && arg.Kind == ast.GenericArgExpr && arg.Expr != nil {
				if genericArgIsSingleNameForCGen(arg, e.Name.Name) {
					return CInvalid
				}

				return g.inferExprType(arg.Expr, expected)
			}
		}

		if typ, ok := g.consts[e.Name.Name]; ok {
			return typ
		}

		if info, ok := g.tasks[e.Name.Name]; ok {
			return info.ReturnType
		}

		return CInvalid

	case *ast.DotIdentExpr:
		if expected != nil {
			return *expected
		}

		return CInvalid

	case *ast.SpreadExpr:
		return g.inferExprType(e.Expr, expected)

	case *ast.IntLitExpr:
		return CInt

	case *ast.GenericExpr:
		return CInvalid

	case *ast.FloatLitExpr:
		if expected != nil && expected.SealName == "f32" {
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
		inner := g.inferExprType(e.Expr, nil)

		switch e.Op {
		case token.Amp:
			return CType{
				Name:     inner.Name + " *",
				SealName: "*" + inner.SealName,
			}

		case token.Star:
			if strings.HasPrefix(inner.SealName, "*") {
				return CType{
					Name:     strings.TrimSuffix(strings.TrimSpace(inner.Name), "*"),
					SealName: strings.TrimPrefix(inner.SealName, "*"),
				}
			}
		}

		return inner

	case *ast.BinaryExpr:
		left := g.inferExprType(e.Left, nil)
		right := g.inferExprType(e.Right, nil)

		if g.hasOperatorOverload(e.Op.String()) {
			if candidate, ok := g.resolveOverload(e.Op.String(), []CType{left, right}); ok {
				info := g.tasks[candidate]
				return info.ReturnType
			}
		}

		if e.Op == token.NotEq && g.hasOperatorOverload("==") {
			if _, ok := g.resolveOverload("==", []CType{left, right}); ok {
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

		if left.SealName == "f64" || right.SealName == "f64" {
			return CF64
		}

		if left.SealName == "f32" || right.SealName == "f32" {
			return CF32
		}

		return left

	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.IdentExpr); ok {
			if g.scope == nil {
				if ret, ok := g.genericTaskParamReturnType(id.Name.Name); ok {
					return ret
				}
			} else if _, isLocal := g.scope.lookup(id.Name.Name); !isLocal {
				if ret, ok := g.genericTaskParamReturnType(id.Name.Name); ok {
					return ret
				}
			}
		}

		if id, ok := e.Callee.(*ast.IdentExpr); ok && id.Name.Name == "len" && !g.isLocalValueName("len") {
			return CUint
		}

		if id, ok := e.Callee.(*ast.IdentExpr); ok && id.Name.Name == "size" && !g.isLocalValueName("size") {
			return CUint
		}

		if id, ok := e.Callee.(*ast.IdentExpr); ok && id.Name.Name == "assert" && !g.isLocalValueName("assert") {
			return CVoid
		}

		if id, ok := e.Callee.(*ast.IdentExpr); ok {
			if ret, ok := g.genericTaskParamReturnType(id.Name.Name); ok {
				return ret
			}
		}

		if gen, ok := e.Callee.(*ast.GenericExpr); ok {
			if id, ok := gen.Base.(*ast.IdentExpr); ok {
				if task, ok := builtin.LookupTask(id.Name.Name); ok && task.Generic {
					switch task.Kind {
					case builtin.TaskAnyIs:
						return CBool

					case builtin.TaskAnyAs, builtin.TaskCast:
						if len(gen.Args) == 1 {
							targetArg := gen.Args[0]
							if g.genericSubst != nil {
								targetArg = g.substituteGenericArgForCGen(targetArg, g.genericSubst)
							}

							return g.cTypeFromGenericArg(targetArg)
						}
					}

					return CInvalid
				}

				info, ok := g.tasks[id.Name.Name]
				if !ok || len(info.GenericParams) == 0 {
					return CInvalid
				}

				if info.Decl == nil {
					return CInvalid
				}

				callArgs := g.genericArgsInContext(gen.Args)
				name := g.registerGenericTaskInstance(info.Decl, callArgs)
				instance := g.genericTasks[name]
				if instance == nil {
					return CInvalid
				}

				return g.genericTaskReturnType(instance)
			}

			if selector, ok := gen.Base.(*ast.SelectorExpr); ok {
				pkgName, taskName, info, ok := g.importedGenericTaskInfoFromSelector(selector)
				if !ok {
					return CInvalid
				}

				callArgs := g.genericArgsInContext(gen.Args)
				name := g.registerImportedGenericTaskInstance(pkgName, taskName, info, callArgs)
				instance := g.importedGenericTasks[name]
				if instance == nil {
					return CInvalid
				}

				return g.importedGenericTaskReturnType(instance)
			}

			return CInvalid
		}

		if id, ok := e.Callee.(*ast.IdentExpr); ok && len(e.Args) > 0 {
			firstType := g.inferExprType(e.Args[0], nil)

			if g.isInterfaceCType(firstType) {
				if req, ok := g.lookupInterfaceRequirement(firstType.SealName, id.Name.Name); ok {
					return g.interfaceRequirementReturnType(req)
				}
			}
		}

		if id, ok := e.Callee.(*ast.IdentExpr); ok {
			if info, ok := g.tasks[id.Name.Name]; ok {
				return info.ReturnType
			}

			if _, ok := g.overloads[id.Name.Name]; ok {
				argTypes := make([]CType, 0, len(e.Args))
				for _, arg := range e.Args {
					argTypes = append(argTypes, g.inferExprType(arg, nil))
				}

				candidate, ok := g.resolveOverload(id.Name.Name, argTypes)
				if !ok {
					return CInvalid
				}

				return g.tasks[candidate].ReturnType
			}
		}

		if selector, ok := e.Callee.(*ast.SelectorExpr); ok {
			if id, ok := selector.Left.(*ast.IdentExpr); ok {
				if pkg := g.packages[id.Name.Name]; pkg != nil {
					if info, ok := pkg.Tasks[selector.Name.Name]; ok {
						return info.ReturnType
					}
				}
			}
		}

		return CInvalid

	case *ast.SelectorExpr:
		if id, ok := e.Left.(*ast.IdentExpr); ok {
			if pkg := g.packages[id.Name.Name]; pkg != nil {
				if info, ok := pkg.Tasks[e.Name.Name]; ok {
					return info.ReturnType
				}
			}
		}

		leftType := g.inferExprType(e.Left, nil)

		if leftType.SealName == "string" || leftType.SealName == "cstring" {
			return CInvalid
		}

		sealName := strings.TrimPrefix(leftType.SealName, "*")
		return g.lookupStructFieldType(sealName, e.Name.Name)

	case *ast.IndexExpr:
		leftType := g.inferExprType(e.Left, nil)

		if leftType.SealName == "string" {
			return CChar
		}

		if leftType.SealName == "cstring" {
			return CInvalid
		}

		if leftType.SealName == "rawptr" {
			return CU8
		}

		if (leftType.IsArray || leftType.IsVariadic) && leftType.Elem != nil {
			return *leftType.Elem
		}

		if g.isByteIndexableCType(leftType) {
			return CU8
		}

		return CInvalid

	case *ast.ArrayLiteralExpr:
		elem := CInvalid

		if len(e.Values) > 0 {
			elem = g.inferExprType(e.Values[0], nil)
		}

		return CType{
			IsArray:  true,
			ArrayLen: fmt.Sprintf("%d", len(e.Values)),
			Elem:     &elem,
			Name:     elem.Name,
			SealName: "array",
		}

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

	return 0, false
}

func (g *Generator) isByteIndexableCType(t CType) bool {
	if t.IsArray || t.IsVariadic {
		return false
	}

	switch t.SealName {
	case "",
		"<invalid>",
		"void",
		"nil",
		"string",
		"cstring":
		return false

	default:
		return true
	}
}

func (g *Generator) isScalarByteIndexableCType(t CType) bool {
	if t.IsArray || t.IsVariadic {
		return false
	}

	switch t.SealName {
	case "bool",
		"int", "uint",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"char",
		"f32", "f64",
		"rawptr":
		return true

	default:
		return strings.HasPrefix(t.SealName, "*")
	}
}

func (g *Generator) isAddressableByteSource(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		_, ok := g.scope.lookup(e.Name.Name)
		return ok

	case *ast.SelectorExpr:
		return g.isAddressableByteSource(e.Left)

	case *ast.UnaryExpr:
		return e.Op == token.Star

	case *ast.IndexExpr:
		leftType := g.inferExprType(e.Left, nil)

		if leftType.IsArray || leftType.IsVariadic || leftType.SealName == "rawptr" {
			return true
		}

		if g.isByteIndexableCType(leftType) {
			return g.isAddressableByteSource(e.Left)
		}

		return false
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

func (g *Generator) isInterfaceCType(t CType) bool {
	if t.IsInterface {
		return true
	}

	_, ok := g.interfaces[t.SealName]
	return ok
}

func (g *Generator) isDynInterfaceName(name string) bool {
	d := g.interfaces[name]
	return d != nil && d.IsDyn
}

func (g *Generator) staticInterfaceConcreteTypes(iface string) []string {
	var names []string

	for concrete, impls := range g.implInfos {
		if _, ok := impls[iface]; ok {
			names = append(names, concrete)
		}
	}

	sort.Strings(names)
	return names
}

func staticInterfaceTagName(iface string, concrete string) string {
	if concrete == "" {
		return sanitizeCName(iface) + "_Tag_nil"
	}

	return sanitizeCName(iface) + "_Tag_" + sanitizeCName(concrete)
}

func staticInterfaceDispatcherName(iface string, req string) string {
	return sanitizeCName(iface) + "_" + sanitizeCName(req)
}

func (g *Generator) tryEmitInterfaceConversion(expected CType, expr ast.Expr) (string, bool) {
	if !g.isInterfaceCType(expected) {
		return "", false
	}

	src := g.inferExprType(expr, nil)

	if src.SealName == expected.SealName {
		return "", false
	}

	if src.SealName == "nil" {
		if g.isDynInterfaceName(expected.SealName) {
			return fmt.Sprintf("(%s){.data = NULL, .vtable = NULL}", expected.Name), true
		}

		return fmt.Sprintf("(%s){.tag = %s}", expected.Name, staticInterfaceTagName(expected.SealName, "")), true
	}

	if strings.HasPrefix(src.SealName, "*") {
		concrete := strings.TrimPrefix(src.SealName, "*")

		if !g.typeImplementsInterface(concrete, expected.SealName) {
			g.error(expr.Span(), fmt.Sprintf("%s does not implement %s", concrete, expected.SealName))
			if g.isDynInterfaceName(expected.SealName) {
				return fmt.Sprintf("(%s){.data = NULL, .vtable = NULL}", expected.Name), true
			}

			return fmt.Sprintf("(%s){.tag = %s}", expected.Name, staticInterfaceTagName(expected.SealName, "")), true
		}

		value := g.emitExpr(expr, nil)

		if g.isDynInterfaceName(expected.SealName) {
			return fmt.Sprintf(
				"(%s){.data = (void *)%s, .vtable = &%s}",
				expected.Name,
				value,
				interfaceVTableName(expected.SealName, concrete),
			), true
		}

		return fmt.Sprintf(
			"(%s){.tag = %s, .as.%s = %s}",
			expected.Name,
			staticInterfaceTagName(expected.SealName, concrete),
			sanitizeCName(concrete),
			value,
		), true
	}

	return "", false
}

func (g *Generator) typeImplementsInterface(concrete string, iface string) bool {
	for _, implemented := range g.impls[concrete] {
		if implemented == iface {
			return true
		}
	}

	return false
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
			typeName := typ.Parts[len(typ.Parts)-1].Name

			if pkg := g.packages[pkgName]; pkg != nil && g.packageHasType(pkg, typeName) {
				return g.importedNamedCType(pkgName, typeName)
			}

			return CType{
				Name:     cImportedTypeName(pkgName, typeName),
				SealName: cImportedTypeName(pkgName, typeName),
			}
		}

		name := typ.Parts[len(typ.Parts)-1].Name

		if g.typeContextPackage != "" {
			if pkg := g.packages[g.typeContextPackage]; pkg != nil && g.packageHasType(pkg, name) {
				return g.importedNamedCType(g.typeContextPackage, name)
			}
		}

		if spec, ok := builtin.LookupType(name); ok {
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
			return CType{
				Name:           name,
				SealName:       name,
				IsInterface:    true,
				IsDynInterface: iface.IsDyn,
			}
		}

		return CType{Name: name, SealName: name}

	case *ast.PointerType:
		elem := g.cTypeFromAst(typ.Elem)

		return CType{
			Name:     elem.Name + " *",
			SealName: "*" + elem.SealName,
		}

	case *ast.ArrayType:
		elem := g.cTypeFromAst(typ.Elem)
		length := ""

		if typ.Inferred {
			length = ""
		} else if typ.Len != nil {
			if lit, ok := typ.Len.(*ast.IntLitExpr); ok {
				length = lit.Value
			} else {
				length = g.emitExpr(typ.Len, &CInt)
			}
		}

		sealName := "[]" + elem.SealName
		if !typ.Inferred {
			sealName = "[" + length + "]" + elem.SealName
		}

		return CType{
			IsArray:  true,
			ArrayLen: length,
			Elem:     &elem,
			Name:     elem.Name,
			SealName: sealName,
		}

	case *ast.GenericType:
		return g.cTypeFromGenericType(typ)
	}

	return CInvalid
}

func (g *Generator) cTypeFromGenericType(typ *ast.GenericType) CType {
	args := typ.Args

	if g.genericSubst != nil {
		args = make([]ast.GenericArg, 0, len(typ.Args))
		for _, arg := range typ.Args {
			args = append(args, g.substituteGenericArgForCGen(arg, g.genericSubst))
		}
	}

	if pkgName, typeName, ok := packageTypeNameFromAst(typ.Base); ok {
		if pkg := g.packages[pkgName]; pkg != nil {
			if decl := pkg.Structs[typeName]; decl != nil && len(decl.GenericParams) > 0 {
				name := g.registerImportedGenericStructInstance(pkgName, typeName, decl, args)

				return CType{
					Name:     name,
					SealName: name,
				}
			}
		}

		base := g.cTypeFromAst(typ.Base)

		if g.isInterfaceCType(base) {
			return base
		}

		g.error(typ.Span(), "imported generic type instantiation is not supported by C codegen yet")
		return base
	}

	baseName := typeNameFromAst(typ.Base)

	if g.typeContextPackage != "" {
		if pkg := g.packages[g.typeContextPackage]; pkg != nil {
			if decl := pkg.Structs[baseName]; decl != nil && len(decl.GenericParams) > 0 {
				name := g.registerImportedGenericStructInstance(g.typeContextPackage, baseName, decl, args)

				return CType{
					Name:     name,
					SealName: name,
				}
			}
		}
	}

	if decl := g.structs[baseName]; decl != nil && len(decl.GenericParams) > 0 {
		name := g.registerGenericStructInstance(decl, args)

		return CType{
			Name:     name,
			SealName: name,
		}
	}

	base := g.cTypeFromAst(typ.Base)

	if g.isInterfaceCType(base) {
		return base
	}

	g.error(typ.Span(), "generic type instantiation is not supported by C codegen yet")
	return base
}

func (g *Generator) registerImportedGenericStructInstance(packageName string, typeName string, decl *ast.StructDecl, args []ast.GenericArg) string {
	if packageName == "" || typeName == "" || decl == nil || isInvalidCStructName(typeName) || isInvalidCStructName(decl.Name.Name) {
		return CInvalid.Name
	}

	name := g.specializedImportedStructCName(packageName, typeName, decl, args)
	if isInvalidCStructName(name) {
		return CInvalid.Name
	}

	if _, exists := g.importedGenericStructs[name]; exists {
		g.addImportedGenericStructRequest(packageName, typeName, args)
		return name
	}

	copiedArgs := append([]ast.GenericArg(nil), args...)

	g.importedGenericStructs[name] = &ImportedGenericStructInstance{
		PackageName: packageName,
		TypeName:    typeName,
		Name:        name,
		Decl:        decl,
		Args:        copiedArgs,
	}

	g.addImportedGenericStructRequest(packageName, typeName, copiedArgs)

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
				if pkg := g.packages[g.typeContextPackage]; pkg != nil && g.packageHasType(pkg, name) {
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func (g *Generator) implInfoFromDecl(d *ast.ImplDecl) *ImplInfo {
	gen, ok := d.Interface.(*ast.GenericType)
	if !ok {
		g.error(d.Interface.Span(), "impl must specialize an interface, for example: Drawable<Sprite> :: impl")
		return nil
	}

	iface := typeNameFromAst(gen.Base)
	if iface == "" {
		g.error(gen.Base.Span(), "expected interface name in impl")
		return nil
	}

	if len(gen.Args) == 0 {
		g.error(gen.Span(), "impl interface specialization requires at least one generic argument")
		return nil
	}

	concrete := typeNameFromGenericArg(gen.Args[0])
	if concrete == "" {
		g.error(gen.Args[0].Span(), "expected concrete type argument in impl")
		return nil
	}

	entries := map[string]ast.ImplEntry{}
	for _, entry := range d.Entries {
		entries[entry.Name.Name] = entry
	}

	return &ImplInfo{
		Concrete:  concrete,
		Interface: iface,
		Entries:   entries,
	}
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

func escapeCString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
