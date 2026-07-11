package cgen

import (
	"fmt"
	"sort"
	"strings"

	"seal/internal/ast"
)

type GenericInstanceKind string

const (
	GenericInstanceTask   GenericInstanceKind = "task"
	GenericInstanceStruct GenericInstanceKind = "struct"
)

type GenericInstanceRequest struct {
	Kind        GenericInstanceKind
	PackageName string
	SymbolName  string
	Args        []ast.GenericArg
}

func (r GenericInstanceRequest) Key() string {
	var parts []string

	parts = append(parts, string(r.Kind))
	parts = append(parts, r.PackageName)
	parts = append(parts, r.SymbolName)

	for _, arg := range r.Args {
		parts = append(parts, genericRequestArgKey(arg))
	}

	return strings.Join(parts, "|")
}

func (r GenericInstanceRequest) String() string {
	var args []string
	for _, arg := range r.Args {
		args = append(args, genericRequestArgDisplay(arg))
	}

	return fmt.Sprintf("%s %s.%s<%s>", r.Kind, r.PackageName, r.SymbolName, strings.Join(args, ", "))
}

type GenericInstanceRequestSet struct {
	items map[string]GenericInstanceRequest
}

func NewGenericInstanceRequestSet() *GenericInstanceRequestSet {
	return &GenericInstanceRequestSet{
		items: map[string]GenericInstanceRequest{},
	}
}

func (s *GenericInstanceRequestSet) Add(req GenericInstanceRequest) bool {
	if s == nil {
		return false
	}

	if req.PackageName == "" || req.SymbolName == "" || req.Kind == "" {
		return false
	}

	key := req.Key()
	if _, exists := s.items[key]; exists {
		return false
	}

	req.Args = cloneGenericArgsForRequest(req.Args)
	s.items[key] = req
	return true
}

func (s *GenericInstanceRequestSet) AddAll(reqs []GenericInstanceRequest) bool {
	changed := false

	for _, req := range reqs {
		if s.Add(req) {
			changed = true
		}
	}

	return changed
}

func (s *GenericInstanceRequestSet) List() []GenericInstanceRequest {
	if s == nil || len(s.items) == 0 {
		return nil
	}

	keys := make([]string, 0, len(s.items))
	for key := range s.items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]GenericInstanceRequest, 0, len(keys))
	for _, key := range keys {
		req := s.items[key]
		req.Args = cloneGenericArgsForRequest(req.Args)
		out = append(out, req)
	}

	return out
}

func cloneGenericArgsForRequest(args []ast.GenericArg) []ast.GenericArg {
	return append([]ast.GenericArg(nil), args...)
}

func genericRequestArgKey(arg ast.GenericArg) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return "type:" + genericRequestTypeKey(arg.Type)

	case ast.GenericArgExpr:
		return "expr:" + genericRequestExprKey(arg.Expr)
	}

	return "invalid"
}

func genericRequestTypeKey(typ ast.Type) string {
	switch t := typ.(type) {
	case *ast.NamedType:
		var parts []string
		for _, part := range t.Parts {
			parts = append(parts, part.Name)
		}

		return strings.Join(parts, ".")

	case *ast.PointerType:
		return "*" + genericRequestTypeKey(t.Elem)

	case *ast.GenericType:
		var args []string
		for _, arg := range t.Args {
			args = append(args, genericRequestArgKey(arg))
		}

		return genericRequestTypeKey(t.Base) + "<" + strings.Join(args, ",") + ">"
	}

	return "<type>"
}

func genericRequestExprKey(expr ast.Expr) string {
	if expr == nil {
		return "nil"
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name.Name

	case *ast.SelectorExpr:
		return genericRequestExprKey(e.Left) + "." + e.Name.Name

	case *ast.IntLitExpr:
		return e.Value

	case *ast.FloatLitExpr:
		return e.Value

	case *ast.StringLitExpr:
		return e.Value

	case *ast.CStringLitExpr:
		return e.Value

	case *ast.CharLitExpr:
		return e.Value

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		return "nil"

	case *ast.UnaryExpr:
		return e.Op.String() + genericRequestExprKey(e.Expr)

	case *ast.BinaryExpr:
		return "(" + genericRequestExprKey(e.Left) + e.Op.String() + genericRequestExprKey(e.Right) + ")"

	case *ast.CallExpr:
		var args []string
		for _, arg := range e.Args {
			args = append(args, genericRequestExprKey(arg))
		}

		return genericRequestExprKey(e.Callee) + "(" + strings.Join(args, ",") + ")"

	case *ast.GenericExpr:
		var args []string
		for _, arg := range e.Args {
			args = append(args, genericRequestArgKey(arg))
		}

		return genericRequestExprKey(e.Base) + "<" + strings.Join(args, ",") + ">"
	}

	return "<expr>"
}

func genericRequestArgDisplay(arg ast.GenericArg) string {
	switch arg.Kind {
	case ast.GenericArgType:
		return genericRequestTypeKey(arg.Type)

	case ast.GenericArgExpr:
		return genericRequestExprKey(arg.Expr)
	}

	return "<invalid>"
}

func (g *Generator) AddRequestedInstances(reqs []GenericInstanceRequest) {
	g.pendingGenericInstanceRequests = append(g.pendingGenericInstanceRequests, reqs...)
}

func (g *Generator) RequestedGenericInstances() []GenericInstanceRequest {
	return g.genericInstanceRequests.List()
}

func (g *Generator) addImportedGenericTaskRequest(packageName string, taskName string, args []ast.GenericArg) {
	if packageName == "" || taskName == "" {
		return
	}

	g.genericInstanceRequests.Add(GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: packageName,
		SymbolName:  taskName,
		Args:        args,
	})
}

func (g *Generator) addImportedGenericStructRequest(packageName string, typeName string, args []ast.GenericArg) {
	if packageName == "" || typeName == "" {
		return
	}

	g.genericInstanceRequests.Add(GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: packageName,
		SymbolName:  typeName,
		Args:        args,
	})
}

func (g *Generator) seedRequestedGenericInstances() {
	for _, req := range g.pendingGenericInstanceRequests {
		if g.packageName != "" && req.PackageName != g.packageName {
			continue
		}

		if req.PackageName == "" {
			continue
		}

		switch req.Kind {
		case GenericInstanceTask:
			info, ok := g.tasks[req.SymbolName]
			if !ok || info.Decl == nil || len(info.GenericParams) == 0 {
				continue
			}

			g.registerGenericTaskInstance(info.Decl, req.Args)

		case GenericInstanceStruct:
			decl := g.structs[req.SymbolName]
			if decl == nil || len(decl.GenericParams) == 0 {
				continue
			}

			g.registerGenericStructInstance(decl, req.Args)
		}
	}
}
