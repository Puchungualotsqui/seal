package cgen

import (
	"strings"
	"testing"

	"seal/internal/ast"
	"seal/internal/source"
)

func testIdentArg(name string) ast.GenericArg {
	loc := source.Span{}

	return ast.GenericArg{
		Kind: ast.GenericArgExpr,
		Expr: &ast.IdentExpr{
			Name: ast.Ident{
				Name: name,
				Loc:  loc,
			},
		},
		Loc: loc,
	}
}

func testIntArg(value string) ast.GenericArg {
	loc := source.Span{}

	return ast.GenericArg{
		Kind: ast.GenericArgExpr,
		Expr: &ast.IntLitExpr{
			Value: value,
			Loc:   loc,
		},
		Loc: loc,
	}
}

func testNamedTypeArg(name string) ast.GenericArg {
	loc := source.Span{}

	return ast.GenericArg{
		Kind: ast.GenericArgType,
		Type: &ast.NamedType{
			Parts: []ast.Ident{
				{
					Name: name,
					Loc:  loc,
				},
			},
			Loc: loc,
		},
		Loc: loc,
	}
}

func testQualifiedGenericTypeArg(pkgName string, typeName string, args ...ast.GenericArg) ast.GenericArg {
	loc := source.Span{}

	return ast.GenericArg{
		Kind: ast.GenericArgType,
		Type: &ast.GenericType{
			Base: &ast.NamedType{
				Parts: []ast.Ident{
					{
						Name: pkgName,
						Loc:  loc,
					},
					{
						Name: typeName,
						Loc:  loc,
					},
				},
				Loc: loc,
			},
			Args: args,
			Loc:  loc,
		},
		Loc: loc,
	}
}

func TestGenericInstanceRequestKeyDeduplicatesEquivalentRequests(t *testing.T) {
	set := NewGenericInstanceRequestSet()

	req := GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "types",
		SymbolName:  "Identity",
		Args: []ast.GenericArg{
			testIdentArg("int"),
		},
	}

	if !set.Add(req) {
		t.Fatalf("expected first add to change set")
	}

	if set.Add(req) {
		t.Fatalf("expected duplicate add not to change set")
	}

	list := set.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 request, got %d: %#v", len(list), list)
	}

	if list[0].Kind != GenericInstanceTask {
		t.Fatalf("expected task request, got %q", list[0].Kind)
	}

	if list[0].PackageName != "types" || list[0].SymbolName != "Identity" {
		t.Fatalf("unexpected request: %#v", list[0])
	}
}

func TestGenericInstanceRequestSetKeepsDifferentKindsSeparate(t *testing.T) {
	set := NewGenericInstanceRequestSet()

	taskReq := GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "types",
		SymbolName:  "Box",
		Args: []ast.GenericArg{
			testIdentArg("int"),
		},
	}

	structReq := GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "types",
		SymbolName:  "Box",
		Args: []ast.GenericArg{
			testIdentArg("int"),
		},
	}

	if !set.Add(taskReq) {
		t.Fatalf("expected task add to change set")
	}

	if !set.Add(structReq) {
		t.Fatalf("expected struct add to change set")
	}

	list := set.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 requests, got %d: %#v", len(list), list)
	}
}

func TestGenericInstanceRequestSetKeepsDifferentArgsSeparate(t *testing.T) {
	set := NewGenericInstanceRequestSet()

	if !set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "types",
		SymbolName:  "Buffer",
		Args: []ast.GenericArg{
			testIdentArg("int"),
			testIntArg("4"),
		},
	}) {
		t.Fatalf("expected first add to change set")
	}

	if !set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "types",
		SymbolName:  "Buffer",
		Args: []ast.GenericArg{
			testIdentArg("int"),
			testIntArg("8"),
		},
	}) {
		t.Fatalf("expected second add to change set")
	}

	list := set.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 requests, got %d: %#v", len(list), list)
	}
}

func TestGenericInstanceRequestSetSortsDeterministically(t *testing.T) {
	set := NewGenericInstanceRequestSet()

	set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "z",
		SymbolName:  "Make",
		Args:        []ast.GenericArg{testIdentArg("int")},
	})

	set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "a",
		SymbolName:  "Box",
		Args:        []ast.GenericArg{testIdentArg("string")},
	})

	set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "a",
		SymbolName:  "Identity",
		Args:        []ast.GenericArg{testIdentArg("int")},
	})

	list := set.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 requests, got %d: %#v", len(list), list)
	}

	got := []string{
		list[0].Key(),
		list[1].Key(),
		list[2].Key(),
	}

	if !(got[0] < got[1] && got[1] < got[2]) {
		t.Fatalf("expected sorted keys, got %#v", got)
	}
}

func TestGenericInstanceRequestNestedGenericArgKey(t *testing.T) {
	req := GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "types",
		SymbolName:  "Box",
		Args: []ast.GenericArg{
			testQualifiedGenericTypeArg("types", "Buffer", testIdentArg("int"), testIntArg("4")),
		},
	}

	key := req.Key()

	wantParts := []string{
		"struct",
		"types",
		"Box",
		"types.Buffer",
		"expr:int",
		"expr:4",
	}

	for _, want := range wantParts {
		if !strings.Contains(key, want) {
			t.Fatalf("expected key to contain %q, got %q", want, key)
		}
	}
}

func TestGenericInstanceRequestString(t *testing.T) {
	req := GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "types",
		SymbolName:  "MakeBuffer",
		Args: []ast.GenericArg{
			testIdentArg("int"),
			testIntArg("4"),
		},
	}

	got := req.String()

	if !strings.Contains(got, "task types.MakeBuffer<int, 4>") {
		t.Fatalf("unexpected request string: %q", got)
	}
}

func TestGenericInstanceRequestSetAddAll(t *testing.T) {
	set := NewGenericInstanceRequestSet()

	changed := set.AddAll([]GenericInstanceRequest{
		{
			Kind:        GenericInstanceStruct,
			PackageName: "types",
			SymbolName:  "Box",
			Args:        []ast.GenericArg{testIdentArg("int")},
		},
		{
			Kind:        GenericInstanceStruct,
			PackageName: "types",
			SymbolName:  "Box",
			Args:        []ast.GenericArg{testIdentArg("int")},
		},
	})

	if !changed {
		t.Fatalf("expected AddAll to report change")
	}

	if len(set.List()) != 1 {
		t.Fatalf("expected duplicate to be collapsed, got %#v", set.List())
	}

	changed = set.AddAll([]GenericInstanceRequest{
		{
			Kind:        GenericInstanceStruct,
			PackageName: "types",
			SymbolName:  "Box",
			Args:        []ast.GenericArg{testIdentArg("int")},
		},
	})

	if changed {
		t.Fatalf("expected duplicate AddAll to report no change")
	}
}

func TestGenericInstanceRequestSetIgnoresInvalidRequests(t *testing.T) {
	set := NewGenericInstanceRequestSet()

	if set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "",
		SymbolName:  "Box",
		Args:        []ast.GenericArg{testIdentArg("int")},
	}) {
		t.Fatalf("expected empty package request to be ignored")
	}

	if set.Add(GenericInstanceRequest{
		Kind:        GenericInstanceStruct,
		PackageName: "types",
		SymbolName:  "",
		Args:        []ast.GenericArg{testIdentArg("int")},
	}) {
		t.Fatalf("expected empty symbol request to be ignored")
	}

	if set.Add(GenericInstanceRequest{
		Kind:        "",
		PackageName: "types",
		SymbolName:  "Box",
		Args:        []ast.GenericArg{testIdentArg("int")},
	}) {
		t.Fatalf("expected empty kind request to be ignored")
	}

	if len(set.List()) != 0 {
		t.Fatalf("expected no requests, got %#v", set.List())
	}
}
