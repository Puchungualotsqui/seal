package cgen

import "seal/internal/ast"

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
