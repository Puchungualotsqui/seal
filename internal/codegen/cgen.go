package cgen

import (
	"fmt"
	"strings"

	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/source"
)

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

	exprTypes map[ast.Expr]*checker.Type

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

	tasks      map[string]TaskInfo
	overloads  map[string][]string
	consts     map[string]CType
	constDecls map[string]*ast.ConstDecl

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
		diags:                        diags,
		packageName:                  packageName,
		packages:                     packages,
		workspacePackages:            packages,
		requiredExternalTypes:        map[string]externalConcreteType{},
		emittedRequiredExternalTypes: map[string]bool{},
		exprTypes: cloneExprTypes(
			semantic.ExprTypes,
		),
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
		constDecls:                    map[string]*ast.ConstDecl{},
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

	if pkg := packageInfoBySemanticName(
		g.packages,
		packageName,
	); pkg != nil {
		return pkg
	}

	return packageInfoBySemanticName(
		g.workspacePackages,
		packageName,
	)
}

func packageInfoBySemanticName(
	packages map[string]*PackageInfo,
	packageName string,
) *PackageInfo {
	if packageName == "" ||
		len(packages) == 0 {
		return nil
	}

	if pkg := packages[packageName]; pkg != nil {
		return pkg
	}

	for _, pkg := range packages {
		if pkg == nil {
			continue
		}

		if pkg.Name == packageName {
			return pkg
		}
	}

	return nil
}

func cloneExprTypes(
	input map[ast.Expr]*checker.Type,
) map[ast.Expr]*checker.Type {
	if len(input) == 0 {
		return map[ast.Expr]*checker.Type{}
	}

	out := make(
		map[ast.Expr]*checker.Type,
		len(input),
	)

	for expr, typ := range input {
		out[expr] = typ
	}

	return out
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

	g.seedNonGenericInterfaceInstances()
	g.collectImportedInterfaceInstances()
	g.seedRequestedGenericInstances()

	// Requests seeded into this package may contain concrete types owned by a
	// package that depends on this one, such as app.Point in mem.NewC<Point>.
	g.collectRequiredExternalTypes()

	// Collect interfaces referenced directly by the current file before
	// deciding which workspace packages must be promoted into the visible
	// package set.
	//
	// A package can be available through workspacePackages without being
	// present in packages. This happens, for example, when listsExample uses
	// mem interfaces and concrete types reached through another package.
	g.collectInterfaceInstancesFromFile(file)

	// Promote workspace packages that contain implementations for interfaces
	// used by the current translation unit. Re-run imported interface
	// discovery after each promotion because a promoted package may expose
	// additional interface references.
	for {
		beforePackages := len(g.packages)
		beforeInterfaces := len(g.interfaceInstances)
		beforeTemplates := len(g.implTemplates)

		g.collectImportedImplTemplates()
		g.collectImportedInterfaceInstances()
		g.collectInterfaceInstancesFromFile(file)

		if beforePackages == len(g.packages) &&
			beforeInterfaces == len(g.interfaceInstances) &&
			beforeTemplates == len(g.implTemplates) {
			break
		}
	}

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
	g.line("#include <string.h>")
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

		case *ast.ConstDecl:
			g.constDecls[d.Name.Name] = d

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
			g.constDecls[d.Name.Name] = d
			g.consts[d.Name.Name] = g.inferExprType(d.Value, nil)
		}
	}
}

func argsSpan(args []ast.Expr) source.Span {
	if len(args) == 0 {
		return source.Span{}
	}

	return args[0].Span()
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
