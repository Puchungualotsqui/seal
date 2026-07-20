package cgen

import (
	"fmt"
	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/checker"
	"seal/internal/source"
	"seal/internal/token"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (g *Generator) isAddressableExprForReference(
	expr ast.Expr,
) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope == nil {
			return false
		}

		_, ok := g.scope.lookup(e.Name.Name)
		return ok

	case *ast.SelectorExpr:
		leftType := g.inferExprType(e.Left, nil)

		if strings.HasPrefix(leftType.SealName, "*") {
			return true
		}

		return g.isAddressableExprForReference(e.Left)

	case *ast.UnaryExpr:
		return e.Op == token.Star

	case *ast.IndexExpr:
		resolution, ok := g.indexResolutions[e]
		if !ok || resolution.Candidate != nil {
			return false
		}

		leftType := g.inferExprType(e.Left, nil)

		if leftType.SealName == "rawptr" {
			return true
		}

		if leftType.IsVariadic {
			return g.isAddressableExprForReference(e.Left)
		}

		if g.isByteIndexableCType(leftType) {
			return g.isAddressableByteSource(e.Left)
		}

		if leftType.IsInlineArray ||
			leftType.IsVariadic {
			return g.isAddressableExprForReference(
				e.Left,
			)
		}
	}

	return false
}

func (g *Generator) emitSemanticReceiverArgument(
	expr ast.Expr,
	prepared string,
	expected CType,
) string {
	if !strings.HasPrefix(expected.SealName, "*") {
		if prepared != "" {
			return prepared
		}

		return g.emitExpr(expr, &expected)
	}

	actual := g.inferExprType(expr, nil)

	if actual.SealName == expected.SealName {
		if prepared != "" {
			return prepared
		}

		return g.emitExpr(expr, &expected)
	}

	expectedElemName := strings.TrimPrefix(
		expected.SealName,
		"*",
	)

	if expected.Elem != nil {
		expectedElemName = expected.Elem.SealName
	}

	if actual.SealName != expectedElemName {
		if prepared != "" {
			return prepared
		}

		return g.emitExpr(expr, &expected)
	}

	value := prepared
	if value == "" {
		value = g.emitExpr(expr, &actual)
	}

	if prepared != "" ||
		g.isAddressableExprForReference(expr) {
		return fmt.Sprintf("&(%s)", value)
	}

	if actual.SealName == "string" {
		return fmt.Sprintf(
			"((%s[]){%s})",
			actual.Name,
			value,
		)
	}

	g.error(
		expr.Span(),
		fmt.Sprintf(
			"checker-selected task requires *%s, but the receiver is not addressable",
			actual.SealName,
		),
	)

	return "NULL"
}

func (g *Generator) emitSemanticTaskCall(
	name string,
	info TaskInfo,
	args []ast.Expr,
	preparedArgs []string,
) string {
	outArgs := make(
		[]string,
		0,
		len(info.ParamTypes),
	)

	for i, arg := range args {
		prepared := ""

		if preparedArgs != nil &&
			i < len(preparedArgs) {
			prepared = preparedArgs[i]
		}

		expected := (*CType)(nil)

		if i < len(info.ParamTypes) {
			expected = &info.ParamTypes[i]
		}

		// Bracket and len overloads use a pointer receiver while Seal syntax
		// passes the value expression. Preserve the existing automatic
		// reference behavior for the first argument.
		if i == 0 && expected != nil {
			outArgs = append(
				outArgs,
				g.emitSemanticReceiverArgument(
					arg,
					prepared,
					*expected,
				),
			)
			continue
		}

		if prepared != "" {
			outArgs = append(
				outArgs,
				prepared,
			)
			continue
		}

		outArgs = append(
			outArgs,
			g.emitExpr(
				arg,
				expected,
			),
		)
	}

	// A checker-selected generic overload may have default parameters.
	// Candidate selection has already happened, so defaults must come from
	// the selected specialized TaskInfo rather than from overload lookup.
	if !info.IsVariadic {
		for i := len(args); i < len(info.ParamTypes); i++ {
			if i >= len(info.ParamHasDefault) ||
				!info.ParamHasDefault[i] {
				continue
			}

			if i >= len(info.ParamDefaults) ||
				info.ParamDefaults[i] == nil {
				g.error(
					source.Span{},
					fmt.Sprintf(
						"selected task %s is missing default argument %d",
						name,
						i+1,
					),
				)
				continue
			}

			expected := info.ParamTypes[i]

			outArgs = append(
				outArgs,
				g.emitExpr(
					info.ParamDefaults[i],
					&expected,
				),
			)
		}
	}

	return fmt.Sprintf(
		"%s(%s)",
		name,
		strings.Join(outArgs, ", "),
	)
}

func (g *Generator) emitSemanticTaskCallInTypeContext(
	packageName string,
	name string,
	info TaskInfo,
	args []ast.Expr,
	preparedArgs []string,
) string {
	if packageName == "" ||
		packageName == g.packageName {
		return g.emitSemanticTaskCall(
			name,
			info,
			args,
			preparedArgs,
		)
	}

	old := g.typeContextPackage
	g.typeContextPackage = packageName

	defer func() {
		g.typeContextPackage = old
	}()

	return g.emitSemanticTaskCall(
		name,
		info,
		args,
		preparedArgs,
	)
}

func (g *Generator) emitBuiltinIndexRead(
	e *ast.IndexExpr,
	leftType CType,
) string {
	left := g.emitExpr(e.Left, nil)
	index := g.emitExpr(e.Index, &CInt)

	switch {
	case leftType.SealName == "rawptr":
		return fmt.Sprintf(
			"((unsigned char *)(%s))[%s]",
			left,
			index,
		)

	case leftType.SealName == "string":
		return fmt.Sprintf(
			"seal_string_index(%s, %s)",
			left,
			index,
		)

	case leftType.SealName == "cstring":
		return fmt.Sprintf(
			"seal_cstring_index(%s, %s)",
			left,
			index,
		)

	case leftType.IsInlineArray:
		if leftType.Elem == nil {
			g.error(
				e.Left.Span(),
				"cannot index invalid @inline_array value",
			)

			return "0"
		}

		return fmt.Sprintf(
			"(%s)[%s]",
			left,
			index,
		)

	case leftType.IsVariadic:
		if leftType.Elem == nil {
			g.error(
				e.Left.Span(),
				"cannot index invalid variadic value",
			)
			return "0"
		}

		return fmt.Sprintf(
			"(%s).data[%s]",
			left,
			index,
		)

	case g.isByteIndexableCType(leftType):
		return g.emitByteIndexExpr(
			e,
			leftType,
			left,
			index,
		)
	}

	g.error(
		e.Left.Span(),
		fmt.Sprintf(
			"checker selected builtin indexing for unsupported type %s",
			leftType.String(),
		),
	)

	return "0"
}

func (g *Generator) emitBuiltinIndexLValue(
	e *ast.IndexExpr,
	leftType CType,
) (string, CType, bool) {
	left := g.emitExpr(e.Left, nil)
	index := g.emitExpr(e.Index, &CInt)

	switch {
	case leftType.SealName == "rawptr":
		return fmt.Sprintf(
			"((unsigned char *)(%s))[%s]",
			left,
			index,
		), CU8, true

	case leftType.SealName == "string":
		g.error(
			e.Left.Span(),
			"string indexing is read-only",
		)
		return "0", CInvalid, false

	case leftType.SealName == "cstring":
		g.error(
			e.Left.Span(),
			"cstring indexing is read-only",
		)
		return "0", CInvalid, false

	case leftType.IsInlineArray:
		if leftType.Elem == nil {
			g.error(
				e.Left.Span(),
				"cannot assign through invalid @inline_array value",
			)

			return "0", CInvalid, false
		}

		return fmt.Sprintf(
			"(%s)[%s]",
			left,
			index,
		), *leftType.Elem, true

	case leftType.IsVariadic:
		if leftType.Elem == nil {
			g.error(
				e.Left.Span(),
				"cannot assign through invalid variadic value",
			)
			return "0", CInvalid, false
		}

		return fmt.Sprintf(
			"(%s).data[%s]",
			left,
			index,
		), *leftType.Elem, true

	case g.isByteIndexableCType(leftType):
		if !g.isAddressableByteSource(e.Left) {
			g.error(
				e.Left.Span(),
				"byte-index assignment requires an addressable value",
			)
			return "0", CInvalid, false
		}

		return g.emitByteIndexExpr(
			e,
			leftType,
			left,
			index,
		), CU8, true
	}

	g.error(
		e.Left.Span(),
		fmt.Sprintf(
			"checker selected builtin index assignment for unsupported type %s",
			leftType.String(),
		),
	)

	return "0", CInvalid, false
}

func (g *Generator) emitIndexExpr(
	e *ast.IndexExpr,
) string {
	resolution, ok := g.indexResolutions[e]
	if !ok {
		g.error(
			e.Span(),
			"missing checker resolution for index expression",
		)
		return "0"
	}

	if resolution.Candidate != nil {
		name, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return "0"
		}

		return g.emitSemanticTaskCall(
			name,
			info,
			[]ast.Expr{
				e.Left,
				e.Index,
			},
			nil,
		)
	}

	leftType := g.inferExprType(e.Left, nil)

	return g.emitBuiltinIndexRead(
		e,
		leftType,
	)
}

func (g *Generator) emitIndexAssignment(
	e *ast.IndexExpr,
	op token.Kind,
	right ast.Expr,
) string {
	resolution, ok := g.indexResolutions[e]
	if !ok {
		g.error(
			e.Span(),
			"missing checker resolution for index assignment",
		)
		return "0"
	}

	if resolution.Candidate != nil {
		if op != token.Assign {
			g.error(
				e.Span(),
				"compound assignment through an overloaded index setter is not supported by C codegen",
			)
			return "0"
		}

		name, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return "0"
		}

		return g.emitSemanticTaskCall(
			name,
			info,
			[]ast.Expr{
				e.Left,
				e.Index,
				right,
			},
			nil,
		)
	}

	leftType := g.inferExprType(e.Left, nil)

	lvalue, valueType, ok := g.emitBuiltinIndexLValue(
		e,
		leftType,
	)
	if !ok {
		return "0"
	}

	value := g.emitExpr(right, &valueType)

	return fmt.Sprintf(
		"%s %s %s",
		lvalue,
		g.cAssignOp(op),
		value,
	)
}

func (g *Generator) emitAssignmentExpr(
	s *ast.AssignStmt,
) string {
	if index, ok := s.Left.(*ast.IndexExpr); ok {
		return g.emitIndexAssignment(
			index,
			s.Op,
			s.Right,
		)
	}

	leftType := g.inferExprType(s.Left, nil)

	if leftType.IsInlineArray {
		g.error(
			s.Span(),
			"cannot emit whole @inline_array assignment",
		)

		return "0"
	}

	left := g.emitExpr(s.Left, nil)
	right := g.emitExpr(s.Right, &leftType)

	return fmt.Sprintf(
		"%s %s %s",
		left,
		g.cAssignOp(s.Op),
		right,
	)
}

func (g *Generator) indexExprType(
	e *ast.IndexExpr,
) CType {
	resolution, ok := g.indexResolutions[e]
	if !ok {
		return CInvalid
	}

	if resolution.Candidate != nil {
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

	leftType := g.inferExprType(e.Left, nil)

	switch {
	case leftType.SealName == "rawptr":
		return CU8

	case leftType.SealName == "string":
		return CChar

	case leftType.SealName == "cstring":
		return CChar

	case leftType.IsInlineArray &&
		leftType.Elem != nil:
		return *leftType.Elem

	case leftType.IsVariadic &&
		leftType.Elem != nil:
		return *leftType.Elem

	case g.isByteIndexableCType(leftType):
		return CU8
	}

	return CInvalid
}

func (g *Generator) isAddressableByteSource(
	expr ast.Expr,
) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope == nil {
			return false
		}

		_, ok := g.scope.lookup(e.Name.Name)
		return ok

	case *ast.SelectorExpr:
		leftType :=
			g.inferExprType(e.Left, nil)

		if strings.HasPrefix(
			leftType.SealName,
			"*",
		) {
			return true
		}

		return g.isAddressableByteSource(
			e.Left,
		)

	case *ast.UnaryExpr:
		return e.Op == token.Star

	case *ast.IndexExpr:
		resolution, ok :=
			g.indexResolutions[e]

		if !ok ||
			resolution.Candidate != nil {
			return false
		}

		leftType :=
			g.inferExprType(
				e.Left,
				nil,
			)

		if leftType.IsInlineArray ||
			leftType.IsVariadic ||
			leftType.SealName == "rawptr" {
			return true
		}

		if g.isByteIndexableCType(
			leftType,
		) {
			return g.isAddressableByteSource(
				e.Left,
			)
		}
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

func isShiftOperator(
	op token.Kind,
) bool {
	return op == token.ShiftLeft ||
		op == token.ShiftRight
}

func checkerIntegerCType(
	typ *checker.Type,
) (CType, bool) {
	if typ == nil {
		return CInvalid, false
	}

	switch typ.Kind {
	case checker.TypeInt:
		return CInt, true

	case checker.TypeUint:
		return CUint, true

	case checker.TypeI8:
		return CI8, true

	case checker.TypeI16:
		return CI16, true

	case checker.TypeI32:
		return CI32, true

	case checker.TypeI64:
		return CI64, true

	case checker.TypeU8:
		return CU8, true

	case checker.TypeU16:
		return CU16, true

	case checker.TypeU32:
		return CU32, true

	case checker.TypeU64:
		return CU64, true

	case checker.TypeChar:
		return CChar, true

	case checker.TypeUntypedInt:
		return CInt, true

	default:
		return CInvalid, false
	}
}

func isShiftIntegerCType(
	typ CType,
) bool {
	switch typ.SealName {
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

func (g *Generator) shiftResultCType(
	e *ast.BinaryExpr,
	leftType CType,
	expected *CType,
) CType {
	if e == nil {
		return leftType
	}

	semanticType := g.exprTypes[e]

	if semanticType == nil {
		return leftType
	}

	/*
		An untyped integer shift receives its concrete representation from its
		context:

		    value u64 := 1 << 63
		    cast<u64>(1 << 63)

		The checker has already verified that the compile-time value fits that
		contextual type.
	*/
	if semanticType.Kind == checker.TypeUntypedInt {
		if expected != nil &&
			isShiftIntegerCType(*expected) {
			return *expected
		}

		return CInt
	}

	if converted, ok :=
		checkerIntegerCType(
			semanticType,
		); ok {
		return converted
	}

	return leftType
}

func (g *Generator) shiftLeftOperandCType(
	e *ast.BinaryExpr,
	leftType CType,
	resultType CType,
) CType {
	if e == nil ||
		e.Left == nil {
		return leftType
	}

	semanticType := g.exprTypes[e.Left]

	if semanticType != nil &&
		semanticType.Kind ==
			checker.TypeUntypedInt {
		return resultType
	}

	return leftType
}

func (g *Generator) emitShiftBinaryExpr(
	e *ast.BinaryExpr,
	leftType CType,
	rightType CType,
	expected *CType,
) string {
	resultType := g.shiftResultCType(
		e,
		leftType,
		expected,
	)

	if !isShiftIntegerCType(resultType) {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"cannot lower shift result type %s",
				resultType.String(),
			),
		)

		return "0"
	}

	leftOperandType :=
		g.shiftLeftOperandCType(
			e,
			leftType,
			resultType,
		)

	var leftExpected *CType

	if !isInvalidCType(leftOperandType) {
		leftExpected = &leftOperandType
	}

	var rightExpected *CType

	if !isInvalidCType(rightType) {
		rightExpected = &rightType
	}

	left := g.emitExpr(
		e.Left,
		leftExpected,
	)

	right := g.emitExpr(
		e.Right,
		rightExpected,
	)

	/*
		C applies integer promotions before shifting. Cast the left operand to
		the Seal result type and cast the result back afterward.

		The outer cast preserves Seal's left-operand result type for narrow
		integers:

		    u8 << int -> u8
		    i16 >> uint -> i16

		The shift remains intentionally unsafe. Runtime-negative counts,
		out-of-range counts, signed left-shift overflow, and shifting negative
		signed values retain the underlying C behavior.
	*/
	return fmt.Sprintf(
		"((%s)(((%s)(%s)) %s (%s)))",
		resultType.TypeName(),
		resultType.TypeName(),
		left,
		g.cBinaryOp(e.Op),
		right,
	)
}

func (g *Generator) emitExpr(
	expr ast.Expr,
	expected *CType,
) string {
	if expected != nil &&
		expected.SealName == "any" {
		return g.emitAnyExpr(expr)
	}

	if expected != nil {
		if value, ok :=
			g.tryEmitInterfaceConversion(
				*expected,
				expr,
			); ok {
			return value
		}
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if g.scope != nil {
			if _, ok :=
				g.scope.lookup(e.Name.Name); ok {
				return e.Name.Name
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
					return e.Name.Name
				}

				return g.emitExpr(
					arg.Expr,
					expected,
				)
			}
		}

		if value, _, ok :=
			g.foreignValueInContext(
				e.Name.Name,
			); ok {
			return value.CValue
		}

		return e.Name.Name

	case *ast.TaskPointerExpr:
		return g.emitTaskPointerExpr(e)

	case *ast.InlineArrayExpr:
		typ := g.inferExprType(
			e,
			expected,
		)

		if expected != nil &&
			expected.IsInlineArray {
			typ = *expected
		}

		if !typ.IsInlineArray {
			g.error(
				e.Span(),
				"@inline_array expression has no inline-array C type",
			)

			return "0"
		}

		return g.emitInlineArrayCompoundLiteral(
			e,
			typ,
		)

	case *ast.DotIdentExpr:
		if expected != nil &&
			expected.SealName != "" &&
			g.isEnumCType(*expected) {
			return fmt.Sprintf(
				"%s_%s",
				expected.SealName,
				e.Name.Name,
			)
		}

		g.error(
			e.Span(),
			fmt.Sprintf(
				"enum literal .%s needs C codegen context",
				e.Name.Name,
			),
		)
		return "0"

	case *ast.IntLitExpr:
		return normalizeCIntegerLiteral(e.Value)

	case *ast.FloatLitExpr:
		return e.Value

	case *ast.StringLitExpr:
		return g.emitStringLiteral(e)

	case *ast.CStringLitExpr:
		return g.emitCStringLiteral(e)

	case *ast.CharLitExpr:
		return g.emitCharLiteral(e)

	case *ast.GenericExpr:
		g.error(
			e.Span(),
			"generic expression cannot be emitted as a value",
		)
		return "0"

	case *ast.BoolLitExpr:
		if e.Value {
			return "true"
		}

		return "false"

	case *ast.NilLitExpr:
		if expected != nil &&
			g.isUnion(*expected) {
			return fmt.Sprintf(
				"(%s){.tag = %s_Tag_nil}",
				expected.Name,
				expected.SealName,
			)
		}

		if expected != nil &&
			g.isInterfaceCType(*expected) {
			return g.nilInterfaceValue(*expected)
		}

		return "NULL"

	case *ast.UnaryExpr:
		return fmt.Sprintf(
			"(%s%s)",
			g.cUnaryOp(e.Op),
			g.emitExpr(e.Expr, nil),
		)

	case *ast.BinaryExpr:
		leftType, rightType :=
			g.binaryOperandTypes(e)

		/*
			Shifts are primitive operations.

			Handle them before overload resolution so they can never accidentally
			be dispatched through an overload declaration.
		*/
		if isShiftOperator(e.Op) {
			return g.emitShiftBinaryExpr(
				e,
				leftType,
				rightType,
				expected,
			)
		}

		if value, ok :=
			g.emitBuiltinTextBinaryExpr(
				e,
				leftType,
				rightType,
			); ok {
			return value
		}

		if g.hasOperatorOverload(
			e.Op.String(),
		) {
			if candidate, ok :=
				g.resolveOverload(
					e.Op.String(),
					[]CType{
						leftType,
						rightType,
					},
				); ok {
				left := g.emitExpr(
					e.Left,
					&leftType,
				)

				right := g.emitExpr(
					e.Right,
					&rightType,
				)

				return fmt.Sprintf(
					"%s(%s, %s)",
					g.cTaskName(candidate),
					left,
					right,
				)
			}
		}

		/*
			Seal derives != from an available == overload.
		*/
		if e.Op == token.NotEq &&
			g.hasOperatorOverload("==") {
			if candidate, ok :=
				g.resolveOverload(
					"==",
					[]CType{
						leftType,
						rightType,
					},
				); ok {
				left := g.emitExpr(
					e.Left,
					&leftType,
				)

				right := g.emitExpr(
					e.Right,
					&rightType,
				)

				return fmt.Sprintf(
					"(!%s(%s, %s))",
					g.cTaskName(candidate),
					left,
					right,
				)
			}
		}

		var leftExpected *CType

		if !isInvalidCType(leftType) {
			leftExpected = &leftType
		}

		var rightExpected *CType

		if !isInvalidCType(rightType) {
			rightExpected = &rightType
		}

		left := g.emitExpr(
			e.Left,
			leftExpected,
		)

		right := g.emitExpr(
			e.Right,
			rightExpected,
		)

		return fmt.Sprintf(
			"(%s %s %s)",
			left,
			g.cBinaryOp(e.Op),
			right,
		)

	case *ast.CallExpr:
		return g.emitCallExpr(e)

	case *ast.SpreadExpr:
		g.error(
			e.Span(),
			"spread can only be emitted as a call argument",
		)
		return "0"

	case *ast.SelectorExpr:
		if id, ok :=
			e.Left.(*ast.IdentExpr); ok &&
			g.isPackageQualifier(
				id.Name.Name,
			) {
			packageName := id.Name.Name

			if packageName == g.packageName {
				if value :=
					g.foreignValues[e.Name.Name]; value != nil {
					return value.CValue
				}

				return g.cTaskName(
					e.Name.Name,
				)
			}

			if pkg :=
				g.typePackageInfo(
					packageName,
				); pkg != nil {
				if value :=
					pkg.ForeignValues[e.Name.Name]; value != nil {
					return value.CValue
				}
			}

			return cPackageTaskName(
				packageName,
				e.Name.Name,
			)
		}

		left := g.emitExpr(
			e.Left,
			nil,
		)

		leftType :=
			g.inferExprType(
				e.Left,
				nil,
			)

		if leftType.SealName == "string" {
			g.error(
				e.Name.Span(),
				fmt.Sprintf(
					"string has no field %q",
					e.Name.Name,
				),
			)
			return "0"
		}

		if leftType.SealName == "cstring" {
			g.error(
				e.Name.Span(),
				fmt.Sprintf(
					"cstring has no field %q",
					e.Name.Name,
				),
			)
			return "0"
		}

		if strings.HasPrefix(
			leftType.SealName,
			"*",
		) {
			return fmt.Sprintf(
				"(%s)->%s",
				left,
				e.Name.Name,
			)
		}

		return fmt.Sprintf(
			"(%s).%s",
			left,
			e.Name.Name,
		)

	case *ast.IndexExpr:
		return g.emitIndexExpr(e)

	case *ast.CompoundLiteralExpr:
		typ :=
			g.cTypeFromAstInContext(
				e.Type,
			)

		if _, ok :=
			g.distincts[typ.SealName]; ok {
			g.error(
				e.Span(),
				fmt.Sprintf(
					"distinct type %s cannot be constructed with a literal; use cast<%s>(value)",
					typ.SealName,
					typ.SealName,
				),
			)
			return "0"
		}

		if expected != nil &&
			g.isUnion(*expected) &&
			g.unionHasMember(
				expected.SealName,
				typ.SealName,
			) {
			payload :=
				g.emitCompoundLiteral(
					e,
					typ,
				)

			return fmt.Sprintf(
				"(%s){.tag = %s_Tag_%s, .as.%s = %s}",
				expected.Name,
				expected.SealName,
				typ.SealName,
				typ.SealName,
				payload,
			)
		}

		return g.emitCompoundLiteral(
			e,
			typ,
		)
	}

	g.error(
		expr.Span(),
		"unsupported expression in C codegen",
	)

	return "0"
}

func (g *Generator) emitBuiltinTextBinaryExpr(
	e *ast.BinaryExpr,
	leftType CType,
	rightType CType,
) (string, bool) {
	if e.Op != token.EqEq &&
		e.Op != token.NotEq {
		return "", false
	}

	var comparison string

	switch {
	case leftType.SealName == "string" &&
		rightType.SealName == "string":
		left := g.emitExpr(e.Left, &leftType)
		right := g.emitExpr(e.Right, &rightType)

		comparison = fmt.Sprintf(
			"seal_string_equal(%s, %s)",
			left,
			right,
		)

	case leftType.SealName == "cstring" &&
		rightType.SealName == "cstring":
		left := g.emitExpr(e.Left, &leftType)
		right := g.emitExpr(e.Right, &rightType)

		comparison = fmt.Sprintf(
			"seal_cstring_equal(%s, %s)",
			left,
			right,
		)

	default:
		return "", false
	}

	if e.Op == token.NotEq {
		return fmt.Sprintf(
			"(!(%s))",
			comparison,
		), true
	}

	return comparison, true
}

func (g *Generator) emitCompoundLiteral(
	e *ast.CompoundLiteralExpr,
	typ CType,
) string {
	if _, ok := g.distincts[typ.SealName]; ok {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"distinct type %s cannot be constructed with a literal; use cast<%s>(value)",
				typ.SealName,
				typ.SealName,
			),
		)

		return "0"
	}

	var values []string

	for _, field := range e.Fields {
		fieldType :=
			g.lookupStructFieldType(
				typ.SealName,
				field.Name.Name,
			)

		values = append(
			values,
			fmt.Sprintf(
				".%s = %s",
				field.Name.Name,
				g.emitStructLiteralFieldValue(
					field.Value,
					fieldType,
				),
			),
		)
	}

	for i, value := range e.Values {
		fieldType :=
			g.lookupStructFieldTypeByIndex(
				typ.SealName,
				i,
			)

		values = append(
			values,
			g.emitStructLiteralFieldValue(
				value,
				fieldType,
			),
		)
	}

	return fmt.Sprintf(
		"(%s){%s}",
		typ.Name,
		strings.Join(values, ", "),
	)
}

func (g *Generator) emitStringLiteral(
	e *ast.StringLitExpr,
) string {
	value, err := unquoteSealLiteral(
		e.Value,
	)
	if err != nil {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"invalid string literal: %v",
				err,
			),
		)

		return "(sealString){.data = NULL, .len = 0}"
	}

	if !utf8.ValidString(value) {
		g.error(
			e.Span(),
			"string literal must contain valid UTF-8",
		)

		return "(sealString){.data = NULL, .len = 0}"
	}

	return fmt.Sprintf(
		"(sealString){.data = (const uint8_t *)%s, .len = (uintptr_t)%d}",
		quoteCByteString(value),
		len(value),
	)
}

func (g *Generator) emitCStringLiteral(
	e *ast.CStringLitExpr,
) string {
	if len(e.Value) < 2 ||
		e.Value[0] != 'c' {
		g.error(
			e.Span(),
			"invalid cstring literal",
		)
		return `""`
	}

	value, err := unquoteSealLiteral(
		e.Value[1:],
	)
	if err != nil {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"invalid cstring literal: %v",
				err,
			),
		)

		return `""`
	}

	if !utf8.ValidString(value) {
		g.error(
			e.Span(),
			"cstring literal must contain valid UTF-8",
		)

		return `""`
	}

	if strings.IndexByte(value, 0) >= 0 {
		g.error(
			e.Span(),
			"cstring literal cannot contain an embedded null byte",
		)

		return `""`
	}

	return quoteCByteString(value)
}

func (g *Generator) emitCharLiteral(
	e *ast.CharLitExpr,
) string {
	value, err := unquoteSealLiteral(
		e.Value,
	)
	if err != nil {
		g.error(
			e.Span(),
			fmt.Sprintf(
				"invalid char literal: %v",
				err,
			),
		)

		return "0"
	}

	if !utf8.ValidString(value) {
		g.error(
			e.Span(),
			"char literal must contain valid UTF-8",
		)

		return "0"
	}

	if utf8.RuneCountInString(value) != 1 {
		g.error(
			e.Span(),
			"char literal must contain exactly one Unicode scalar value",
		)

		return "0"
	}

	scalar, _ := utf8.DecodeRuneInString(value)

	if !utf8.ValidRune(scalar) {
		g.error(
			e.Span(),
			"char literal contains an invalid Unicode scalar value",
		)

		return "0"
	}

	return fmt.Sprintf(
		"((uint32_t)%d)",
		scalar,
	)
}

func normalizeSealQuotedLiteral(
	literal string,
) string {
	var out strings.Builder
	out.Grow(len(literal))

	for i := 0; i < len(literal); {
		if literal[i] != '\\' {
			out.WriteByte(literal[i])
			i++
			continue
		}

		// Leave a trailing backslash unchanged. strconv.Unquote will report
		// the appropriate malformed-literal diagnostic.
		if i+1 >= len(literal) {
			out.WriteByte(literal[i])
			i++
			continue
		}

		next := literal[i+1]

		// Go does not accept the short escape \0. Seal does.
		//
		// Preserve longer octal escapes such as \000, but translate a short
		// \0 into Go's equivalent fixed-width hexadecimal escape.
		if next == '0' {
			hasFollowingOctalDigit :=
				i+2 < len(literal) &&
					literal[i+2] >= '0' &&
					literal[i+2] <= '7'

			if !hasFollowingOctalDigit {
				out.WriteString(`\x00`)
				i += 2
				continue
			}
		}

		// Copy the complete escape pair. In particular, this ensures that
		// "\\0" remains an escaped backslash followed by the character '0',
		// rather than being interpreted as a null byte.
		out.WriteByte(literal[i])
		out.WriteByte(literal[i+1])
		i += 2
	}

	return out.String()
}

func unquoteSealLiteral(
	literal string,
) (string, error) {
	return strconv.Unquote(
		normalizeSealQuotedLiteral(literal),
	)
}

func quoteCByteString(value string) string {
	var out strings.Builder

	out.WriteByte('"')

	for i := 0; i < len(value); i++ {
		current := value[i]

		switch current {
		case '"':
			out.WriteString(`\"`)

		case '\\':
			out.WriteString(`\\`)

		default:
			// Keep ordinary printable ASCII readable. Question marks are
			// escaped to prevent old C11 trigraph processing from changing
			// source bytes before tokenization.
			if current >= 0x20 &&
				current <= 0x7E &&
				current != '?' {
				out.WriteByte(current)
				continue
			}

			fmt.Fprintf(
				&out,
				`\%03o`,
				current,
			)
		}
	}

	out.WriteByte('"')

	return out.String()
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

func (g *Generator) cTypeFromSizeArg(
	expr ast.Expr,
) (CType, bool) {
	if expr == nil {
		return CInvalid, false
	}

	// A local identifier is always a runtime value, even when its name could
	// otherwise be interpreted as a type name.
	if id, ok := expr.(*ast.IdentExpr); ok {
		name := id.Name.Name

		if g.isLocalValueName(name) {
			return CInvalid, false
		}

		// Generic type parameter:
		//
		//     size(T)
		//
		// Resolve T through the active monomorphization substitution.
		if g.genericSubst != nil {
			if arg, exists := g.genericSubst[name]; exists {
				switch arg.Kind {
				case ast.GenericArgType:
					typ := g.cTypeFromAstWithGenericArgs(
						arg.Type,
						g.genericSubst,
					)

					if typ.SealName != "" &&
						typ.SealName != CInvalid.SealName {
						return typ, true
					}

				case ast.GenericArgExpr:
					if typeAst :=
						typeAstFromExprForCGen(
							arg.Expr,
						); typeAst != nil {
						typ :=
							g.cTypeFromAstWithGenericArgs(
								typeAst,
								g.genericSubst,
							)

						if typ.SealName != "" &&
							typ.SealName != CInvalid.SealName {
							return typ, true
						}
					}
				}
			}
		}

		// Normal non-generic type:
		//
		//     size(int)
		//     size(MyStruct)
		if isBuiltinTypeName(name) ||
			g.distincts[name] != nil ||
			g.structs[name] != nil ||
			g.enums[name] != nil ||
			g.unions[name] != nil ||
			g.interfaces[name] != nil ||
			g.foreignTypes[name] != nil {
			typ := g.cTypeFromAstInContext(
				&ast.NamedType{
					Parts: []ast.Ident{id.Name},
					Loc:   id.Span(),
				},
			)

			if typ.SealName != "" &&
				typ.SealName != CInvalid.SealName {
				return typ, true
			}
		}

		// An unqualified type inside imported generic code can belong to the
		// package currently stored in typeContextPackage.
		if g.typeContextPackage != "" {
			if pkg := g.typePackageInfo(
				g.typeContextPackage,
			); pkg != nil &&
				g.packageHasType(pkg, name) {
				typ := g.cTypeFromAstInContext(
					&ast.NamedType{
						Parts: []ast.Ident{id.Name},
						Loc:   id.Span(),
					},
				)

				if typ.SealName != "" &&
					typ.SealName != CInvalid.SealName {
					return typ, true
				}
			}
		}

		return CInvalid, false
	}

	// Generic type expression:
	//
	//     size(_Slot<T>)
	//     size(Box<int>)
	//     size(pkg.Box<T>)
	//
	// The parser represents these type-shaped arguments as GenericExpr when
	// they occur inside an expression argument list. Convert the expression
	// back to a type AST and resolve it under the current generic
	// substitution.
	if _, ok := expr.(*ast.GenericExpr); ok {
		typeAst := typeAstFromExprForCGen(expr)
		if typeAst == nil {
			return CInvalid, false
		}

		typ := g.cTypeFromAstInContext(typeAst)

		if typ.SealName == "" ||
			typ.SealName == CInvalid.SealName {
			return CInvalid, false
		}

		return typ, true
	}

	// Qualified non-generic type:
	//
	//     size(pkg.Type)
	//
	// Do not reinterpret arbitrary selectors such as value.Field as types.
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		id, ok := selector.Left.(*ast.IdentExpr)
		if !ok {
			return CInvalid, false
		}

		pkg := g.typePackageInfo(id.Name.Name)
		if pkg == nil ||
			!g.packageHasType(
				pkg,
				selector.Name.Name,
			) {
			return CInvalid, false
		}

		typeAst := typeAstFromExprForCGen(expr)
		if typeAst == nil {
			return CInvalid, false
		}

		typ := g.cTypeFromAstInContext(typeAst)

		if typ.SealName == "" ||
			typ.SealName == CInvalid.SealName {
			return CInvalid, false
		}

		return typ, true
	}

	return CInvalid, false
}

func (g *Generator) emitSizeCall(e *ast.CallExpr) string {
	if len(e.Args) != 1 {
		g.error(e.Span(), "size expects 1 argument")
		return "0"
	}

	if typ, ok := g.cTypeFromSizeArg(e.Args[0]); ok {
		return fmt.Sprintf(
			"(uintptr_t)sizeof(%s)",
			typ.TypeName(),
		)
	}

	argType := g.inferExprType(e.Args[0], nil)
	value := g.emitExpr(e.Args[0], nil)

	switch argType.SealName {
	case "string":
		return fmt.Sprintf(
			"(uintptr_t)(%s).len",
			value,
		)

	case "cstring":
		return fmt.Sprintf(
			"seal_cstring_byte_len(%s)",
			value,
		)
	}

	return fmt.Sprintf(
		"(uintptr_t)sizeof(%s)",
		value,
	)
}

func (g *Generator) emitLenCall(
	e *ast.CallExpr,
	preparedArgs []string,
) string {
	resolution, ok := g.lenResolutions[e]
	if !ok {
		g.error(
			e.Span(),
			"missing checker resolution for len call",
		)
		return "0"
	}

	if len(e.Args) != 1 {
		g.error(
			e.Span(),
			"len expects 1 argument",
		)
		return "0"
	}

	if resolution.Candidate != nil {
		name, info, ok := g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			e.Span(),
		)
		if !ok {
			return "0"
		}

		return g.emitSemanticTaskCall(
			name,
			info,
			e.Args,
			preparedArgs,
		)
	}

	argType := g.inferExprType(e.Args[0], nil)

	if argType.IsInlineArray {
		return fmt.Sprintf(
			"((uintptr_t)%d)",
			argType.InlineLength,
		)
	}

	arg := ""
	if len(preparedArgs) > 0 {
		arg = preparedArgs[0]
	} else {
		arg = g.emitExpr(e.Args[0], nil)
	}

	switch {
	case argType.SealName == "string":
		return fmt.Sprintf(
			"seal_string_scalar_len(%s)",
			arg,
		)

	case argType.SealName == "cstring":
		return fmt.Sprintf(
			"seal_cstring_scalar_len(%s)",
			arg,
		)

	case argType.IsVariadic:
		return fmt.Sprintf(
			"((uintptr_t)(%s).len)",
			arg,
		)
	}

	g.error(
		e.Args[0].Span(),
		fmt.Sprintf(
			"checker selected builtin len for unsupported type %s",
			argType.String(),
		),
	)

	return "0"
}

func (g *Generator) emitAssertCall(e *ast.CallExpr) string {
	if len(e.Args) != 1 {
		g.error(e.Span(), "assert expects 1 argument")
		return "assert(false)"
	}

	cond := g.emitExpr(e.Args[0], &CBool)
	return fmt.Sprintf("assert(%s)", cond)
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

func (g *Generator) emitNoArgRuntimeCall(name string, cName string, e *ast.CallExpr) string {
	if len(e.Args) != 0 {
		g.error(e.Span(), fmt.Sprintf("%s expects 0 arguments", name))
	}

	return cName + "()"
}

func (g *Generator) emitCallExpr(e *ast.CallExpr) string {
	return g.emitCallExprWithArgs(e, nil)
}

func (g *Generator) emitCallExprWithArgs(
	e *ast.CallExpr,
	preparedArgs []string,
) string {
	if gen, ok :=
		e.Callee.(*ast.GenericExpr); ok {
		return g.emitGenericCall(
			gen,
			e.Args,
			preparedArgs,
		)
	}

	if id, ok :=
		e.Callee.(*ast.IdentExpr); ok {
		isLocal := false

		if g.scope != nil {
			_, isLocal =
				g.scope.lookup(id.Name.Name)
		}

		if !isLocal {
			if name, ok :=
				g.genericTaskParamCallName(
					id.Name.Name,
				); ok {
				info, hasInfo :=
					g.genericTaskParamInfo(
						id.Name.Name,
					)

				if hasInfo {
					return g.emitDirectCNamedCall(
						name,
						e.Args,
						preparedArgs,
						info.ParamTypes,
					)
				}

				return g.emitDirectCNamedCall(
					name,
					e.Args,
					preparedArgs,
					nil,
				)
			}
		}

		if _, ok := g.lenResolutions[e]; ok {
			return g.emitLenCall(
				e,
				preparedArgs,
			)
		}

		if packageName, _, ok :=
			g.importedTaskInfoFromTypeContext(
				id.Name.Name,
			); ok {
			return g.emitPackageTaskCall(
				packageName,
				id.Name.Name,
				e.Args,
				preparedArgs,
			)
		}

		if len(e.Args) > 0 {
			firstType :=
				g.inferExprType(
					e.Args[0],
					nil,
				)

			if g.isInterfaceCType(firstType) {
				if _, _, ok :=
					g.lookupInterfaceRequirement(
						firstType,
						id.Name.Name,
					); ok {
					return g.emitInterfaceDispatchCall(
						firstType,
						id.Name.Name,
						e.Args,
						preparedArgs,
					)
				}
			}
		}

		if _, ok := g.tasks[id.Name.Name]; ok {
			return g.emitTaskCall(
				id.Name.Name,
				e.Args,
				preparedArgs,
			)
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

			if candidate, ok :=
				g.resolveOverload(
					id.Name.Name,
					argTypes,
				); ok {
				return g.emitTaskCall(
					candidate,
					e.Args,
					preparedArgs,
				)
			}
		}

		if kind, ok :=
			g.primitiveTaskKind(
				id.Name.Name,
			); ok {
			switch kind {
			case builtin.TaskLen:
				g.error(
					e.Span(),
					"missing checker resolution for primitive len call",
				)
				return "0"

			case builtin.TaskSize:
				return g.emitSizeCall(e)

			case builtin.TaskAssert:
				return g.emitAssertCall(e)

			case builtin.TaskPanic:
				return g.emitPanicCall(e)

			case builtin.TaskTrap:
				return g.emitNoArgRuntimeCall(
					"trap",
					"seal_trap",
					e,
				)

			case builtin.TaskUnreachable:
				return g.emitNoArgRuntimeCall(
					"unreachable",
					"seal_unreachable",
					e,
				)
			}
		}
	}

	if selector, ok :=
		e.Callee.(*ast.SelectorExpr); ok {
		if id, ok :=
			selector.Left.(*ast.IdentExpr); ok {
			packageName := id.Name.Name

			if g.isPackageQualifier(
				packageName,
			) {
				if packageName ==
					g.packageName {
					return g.emitTaskCall(
						selector.Name.Name,
						e.Args,
						preparedArgs,
					)
				}

				return g.emitPackageTaskCall(
					packageName,
					selector.Name.Name,
					e.Args,
					preparedArgs,
				)
			}
		}
	}

	var args []string

	if preparedArgs != nil {
		args = append(args, preparedArgs...)
	} else {
		for _, arg := range e.Args {
			args = append(
				args,
				g.emitExpr(arg, nil),
			)
		}
	}

	return fmt.Sprintf(
		"%s(%s)",
		g.emitExpr(e.Callee, nil),
		strings.Join(args, ", "),
	)
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

func (g *Generator) emitPackageTaskCall(
	packageName string,
	taskName string,
	args []ast.Expr,
	preparedArgs []string,
) string {
	pkg := g.typePackageInfo(
		packageName,
	)
	if pkg == nil {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"unknown package %q",
				packageName,
			),
		)

		return "0"
	}

	rawInfo, hasTask :=
		pkg.Tasks[taskName]

	if !hasTask {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"package %s has no task %q",
				packageName,
				taskName,
			),
		)

		return "0"
	}

	info := g.taskInfoInPackageContext(
		packageName,
		taskName,
		rawInfo,
	)

	name := cImportedTaskName(
		packageName,
		taskName,
		info,
	)

	if info.IsVariadic &&
		!info.IsExtern {
		return g.emitSealVariadicTaskCallWithDefaultContext(
			packageName,
			name,
			info,
			args,
			preparedArgs,
		)
	}

	var outArgs []string

	for index, arg := range args {
		if preparedArgs != nil &&
			index < len(preparedArgs) {
			outArgs = append(
				outArgs,
				preparedArgs[index],
			)

			continue
		}

		var expected *CType

		if index < len(info.ParamTypes) {
			if index <
				len(info.ParamIsVariadic) &&
				info.ParamIsVariadic[index] {
				expected = nil
			} else {
				expected =
					&info.ParamTypes[index]
			}
		}

		outArgs = append(
			outArgs,
			g.emitExpr(
				arg,
				expected,
			),
		)
	}

	if !info.IsVariadic {
		for index := len(args); index < len(info.ParamTypes); index++ {
			if index >=
				len(info.ParamHasDefault) ||
				!info.ParamHasDefault[index] {
				continue
			}

			if index >=
				len(info.ParamDefaults) ||
				info.ParamDefaults[index] ==
					nil {
				g.error(
					argsSpan(args),
					fmt.Sprintf(
						"imported task %s.%s is missing default argument %d",
						packageName,
						taskName,
						index+1,
					),
				)

				continue
			}

			expected :=
				info.ParamTypes[index]

			value := ""

			g.withTypeContext(
				packageName,
				func() {
					value = g.emitExpr(
						info.ParamDefaults[index],
						&expected,
					)
				},
			)

			outArgs = append(
				outArgs,
				value,
			)
		}
	}

	return fmt.Sprintf(
		"%s(%s)",
		name,
		strings.Join(
			outArgs,
			", ",
		),
	)
}

func (g *Generator) emitGenericCall(gen *ast.GenericExpr, args []ast.Expr, preparedArgs []string) string {
	if resolution, ok :=
		g.genericOverloadCalls[gen]; ok {
		if resolution.Candidate == nil {
			g.error(
				gen.Span(),
				"checker generic-overload resolution has no candidate",
			)
			return "0"
		}

		name, info, selected :=
			g.semanticTaskSelection(
				resolution.Candidate,
				resolution.PackageName,
				resolution.GenericArguments,
				gen.Span(),
			)

		if !selected {
			return "0"
		}

		return g.emitSemanticTaskCallInTypeContext(
			resolution.PackageName,
			name,
			info,
			args,
			preparedArgs,
		)
	}

	if id, ok := gen.Base.(*ast.IdentExpr); ok {
		if task, ok := builtin.LookupTask(id.Name.Name); ok && task.Generic {
			return g.emitGenericIntrinsicCall(gen, args)
		}

		if packageName,
			taskName,
			info,
			ok :=
			g.importedGenericTaskInfoFromTypeContext(
				id.Name.Name,
			); ok {
			callArgs := gen.Args

			if g.genericSubst != nil {
				callArgs = make(
					[]ast.GenericArg,
					0,
					len(gen.Args),
				)

				for _, arg := range gen.Args {
					callArgs = append(
						callArgs,
						g.substituteGenericArgForCGen(
							arg,
							g.genericSubst,
						),
					)
				}
			}

			name :=
				g.registerImportedGenericTaskInstance(
					packageName,
					taskName,
					info,
					callArgs,
				)

			subst := genericArgSubstForCGen(
				info.GenericParams,
				callArgs,
			)

			return g.emitGenericCallToNameInTypeContext(
				packageName,
				name,
				info.ParamTypeAsts,
				info.ParamDefaults,
				info.ParamHasDefault,
				subst,
				args,
				preparedArgs,
			)
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

func (g *Generator) emitGenericIntrinsicCall(
	gen *ast.GenericExpr,
	args []ast.Expr,
) string {
	id, ok := gen.Base.(*ast.IdentExpr)
	if !ok {
		g.error(
			gen.Base.Span(),
			"only intrinsic generic calls are supported here",
		)
		return "0"
	}

	task, ok := builtin.LookupTask(id.Name.Name)
	if !ok || !task.Generic {
		g.error(
			id.Span(),
			fmt.Sprintf(
				"unknown generic intrinsic %q",
				id.Name.Name,
			),
		)
		return "0"
	}

	if len(gen.Args) != 1 {
		g.error(
			gen.Span(),
			fmt.Sprintf(
				"%s expects exactly 1 type argument",
				id.Name.Name,
			),
		)
		return "0"
	}

	targetArg := gen.Args[0]

	if g.genericSubst != nil {
		targetArg =
			g.substituteGenericArgForCGen(
				targetArg,
				g.genericSubst,
			)
	}

	target := g.cTypeFromGenericArg(targetArg)

	switch task.Kind {
	case builtin.TaskAnyIs:
		if len(args) != 1 {
			g.error(
				gen.Span(),
				"anyIs expects exactly 1 value argument",
			)
			return "false"
		}

		value := g.emitExpr(args[0], nil)

		kind, ok := g.sealTypeKindFor(target)
		if !ok {
			g.error(
				gen.Args[0].Span(),
				fmt.Sprintf(
					"anyIs does not support %s yet",
					target.String(),
				),
			)
			return "false"
		}

		return fmt.Sprintf(
			"((%s).type == %s)",
			value,
			kind,
		)

	case builtin.TaskAnyAs:
		if len(args) != 1 {
			g.error(
				gen.Span(),
				"anyAs expects exactly 1 value argument",
			)
			return "0"
		}

		value := g.emitExpr(args[0], nil)

		field, ok := g.sealAnyFieldFor(target)
		if !ok {
			g.error(
				gen.Args[0].Span(),
				fmt.Sprintf(
					"anyAs does not support %s yet",
					target.String(),
				),
			)
			return "0"
		}

		if target.SealName == "any" {
			return value
		}

		return fmt.Sprintf(
			"((%s).value.%s)",
			value,
			field,
		)

	case builtin.TaskCast:
		if g.isInterfaceCType(target) {
			if len(args) != 1 {
				g.error(
					gen.Span(),
					"interface cast expects exactly 1 value argument",
				)
				return g.nilInterfaceValue(target)
			}

			value, ok :=
				g.tryEmitInterfaceConversion(
					target,
					args[0],
				)

			if ok {
				return value
			}

			g.error(
				args[0].Span(),
				fmt.Sprintf(
					"cannot lower cast from %s to interface %s",
					g.inferExprType(args[0], nil).String(),
					target.String(),
				),
			)

			return g.nilInterfaceValue(target)
		}

		switch target.SealName {
		case "string":
			if len(args) != 2 {
				g.error(
					gen.Span(),
					"cast<string> expects data and byte length",
				)

				return "(sealString){.data = NULL, .len = 0}"
			}

			data := g.emitExpr(
				args[0],
				nil,
			)

			byteLength := g.emitExpr(
				args[1],
				&CUint,
			)

			return fmt.Sprintf(
				"(sealString){.data = (const uint8_t *)(%s), .len = (uintptr_t)(%s)}",
				data,
				byteLength,
			)

		case "cstring":
			if len(args) != 2 {
				g.error(
					gen.Span(),
					"cast<cstring> expects exactly 2 arguments: rawptr and uint byte length",
				)

				return "((const char *)NULL)"
			}

			sourceType := g.inferExprType(
				args[0],
				nil,
			)

			if sourceType.SealName == "string" {
				g.error(
					args[0].Span(),
					"cannot cast string directly to cstring because string is not guaranteed to be null-terminated",
				)

				return "((const char *)NULL)"
			}

			data := g.emitExpr(
				args[0],
				nil,
			)

			byteLength := g.emitExpr(
				args[1],
				&CUint,
			)

			return fmt.Sprintf(
				"seal_cstring_from_parts((const char *)(%s), (uintptr_t)(%s))",
				data,
				byteLength,
			)

		case "rawptr":
			if len(args) != 1 {
				g.error(
					gen.Span(),
					"cast<rawptr> expects exactly 1 value argument",
				)
				return "NULL"
			}

			sourceType :=
				g.inferExprType(
					args[0],
					nil,
				)

			value := g.emitExpr(
				args[0],
				nil,
			)

			switch sourceType.SealName {
			case "string":
				return fmt.Sprintf(
					"((void *)((%s).data))",
					value,
				)

			case "cstring":
				return fmt.Sprintf(
					"((void *)(%s))",
					value,
				)
			}

			return fmt.Sprintf(
				"((%s)(%s))",
				target.Name,
				value,
			)

		default:
			if len(args) != 1 {
				g.error(
					gen.Span(),
					fmt.Sprintf(
						"cast<%s> expects exactly 1 value argument",
						target.SealName,
					),
				)
				return "0"
			}

			value := g.emitExpr(
				args[0],
				&target,
			)

			return fmt.Sprintf(
				"((%s)(%s))",
				target.Name,
				value,
			)
		}

	default:
		g.error(
			id.Span(),
			fmt.Sprintf(
				"unknown generic intrinsic %q",
				id.Name.Name,
			),
		)
		return "0"
	}
}

func (g *Generator) emitSealVariadicTaskCall(
	name string,
	info TaskInfo,
	args []ast.Expr,
	preparedArgs []string,
) string {
	return g.emitSealVariadicTaskCallWithDefaultContext(
		"",
		name,
		info,
		args,
		preparedArgs,
	)
}

func (g *Generator) emitSealVariadicTaskCallWithDefaultContext(
	defaultPackageName string,
	name string,
	info TaskInfo,
	args []ast.Expr,
	preparedArgs []string,
) string {
	totalParamCount := len(info.ParamTypes)

	if totalParamCount == 0 {
		g.error(
			argsSpan(args),
			fmt.Sprintf(
				"variadic task %s has no variadic parameter",
				name,
			),
		)

		return "0"
	}

	/*
		The final parameter stores the variadic element type.

		For:

		    Println(
		        format string = "",
		        args ...any,
		    )

		ParamTypes contains:

		    [string, any]

		and fixedParamCount is therefore one.
	*/
	fixedParamCount := totalParamCount - 1

	outArgs := make(
		[]string,
		0,
		totalParamCount,
	)

	/*
		Emit the fixed arguments explicitly supplied by the caller.

		Arguments beyond fixedParamCount belong to the variadic portion.
	*/
	providedFixedCount := len(args)

	if providedFixedCount >
		fixedParamCount {
		providedFixedCount =
			fixedParamCount
	}

	for index := 0; index < providedFixedCount; index++ {
		if preparedArgs != nil &&
			index < len(preparedArgs) {
			outArgs = append(
				outArgs,
				preparedArgs[index],
			)

			continue
		}

		expected := info.ParamTypes[index]

		outArgs = append(
			outArgs,
			g.emitExpr(
				args[index],
				&expected,
			),
		)
	}

	/*
		Fill omitted trailing fixed parameters from their defaults.

		The previous implementation skipped this step and immediately emitted
		the variadic container. Consequently:

		    Println()

		was lowered as:

		    fmt_Println(emptyVariadic)

		instead of:

		    fmt_Println(emptyString, emptyVariadic)
	*/
	for index := providedFixedCount; index < fixedParamCount; index++ {
		if index >= len(info.ParamHasDefault) ||
			!info.ParamHasDefault[index] {
			g.error(
				argsSpan(args),
				fmt.Sprintf(
					"variadic task %s is missing required fixed argument %d",
					name,
					index+1,
				),
			)

			return "0"
		}

		if index >= len(info.ParamDefaults) ||
			info.ParamDefaults[index] == nil {
			g.error(
				argsSpan(args),
				fmt.Sprintf(
					"variadic task %s is missing default expression for argument %d",
					name,
					index+1,
				),
			)

			return "0"
		}

		expected := info.ParamTypes[index]
		defaultValue := ""

		emitDefault := func() {
			defaultValue = g.emitExpr(
				info.ParamDefaults[index],
				&expected,
			)
		}

		/*
			Imported task defaults belong to the imported package's lexical
			and type context.

			Caller expressions must still be emitted in the caller context,
			so only the default expression is emitted under this temporary
			package context.
		*/
		if defaultPackageName != "" &&
			defaultPackageName !=
				g.packageName {
			g.withTypeContext(
				defaultPackageName,
				emitDefault,
			)
		} else {
			emitDefault()
		}

		outArgs = append(
			outArgs,
			defaultValue,
		)
	}

	var variadicArgs []ast.Expr

	if len(args) > fixedParamCount {
		variadicArgs =
			args[fixedParamCount:]
	}

	variadicElemType :=
		info.ParamTypes[totalParamCount-1]

	/*
		A single spread argument is already represented as a Seal variadic
		container and can be forwarded directly.
	*/
	if len(variadicArgs) == 1 {
		if spread, ok :=
			variadicArgs[0].(*ast.SpreadExpr); ok {
			outArgs = append(
				outArgs,
				g.emitSpreadAsVariadic(
					variadicElemType,
					spread,
				),
			)

			return fmt.Sprintf(
				"%s(%s)",
				name,
				strings.Join(
					outArgs,
					", ",
				),
			)
		}
	}

	/*
		Otherwise construct the variadic container from the remaining
		arguments. preparedOffset remains fixedParamCount because preparedArgs
		is indexed according to the original source argument list.
	*/
	outArgs = append(
		outArgs,
		g.emitVariadicLiteral(
			variadicElemType,
			variadicArgs,
			preparedArgs,
			fixedParamCount,
		),
	)

	return fmt.Sprintf(
		"%s(%s)",
		name,
		strings.Join(
			outArgs,
			", ",
		),
	)
}

func (g *Generator) emitSpreadAsVariadic(
	elem CType,
	spread *ast.SpreadExpr,
) string {
	variadicType := g.variadicCType(elem)
	srcType := g.inferExprType(
		spread.Expr,
		nil,
	)

	if srcType.IsVariadic {
		if srcType.Elem == nil {
			g.error(
				spread.Span(),
				"cannot spread invalid variadic value",
			)

			return fmt.Sprintf(
				"(%s){.data = NULL, .len = 0}",
				variadicType.Name,
			)
		}

		if srcType.Elem.SealName != elem.SealName {
			g.error(
				spread.Span(),
				fmt.Sprintf(
					"cannot spread %s into ...%s",
					srcType.String(),
					elem.SealName,
				),
			)

			return fmt.Sprintf(
				"(%s){.data = NULL, .len = 0}",
				variadicType.Name,
			)
		}

		return g.emitExpr(spread.Expr, nil)
	}

	g.error(
		spread.Span(),
		fmt.Sprintf(
			"cannot spread %s; expected variadic value",
			srcType.String(),
		),
	)

	return fmt.Sprintf(
		"(%s){.data = NULL, .len = 0}",
		variadicType.Name,
	)
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
		values = append(values, g.emitExpr(arg, &elem))
	}

	return fmt.Sprintf(
		"(%s){.data = (%s[]){%s}, .len = %d}",
		variadicType.Name,
		elem.Name,
		strings.Join(values, ", "),
		len(values),
	)
}
