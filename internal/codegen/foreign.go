package cgen

import (
	"fmt"
	"sort"
	"strings"

	"seal/internal/ast"
	"seal/internal/source"
	"seal/internal/token"
)

type ForeignTypeInfo struct {
	Name        string
	PackageName string
	CType       string
	Loc         source.Span
}

type ForeignValueInfo struct {
	Name        string
	PackageName string
	Type        ast.Type
	CValue      string
	Loc         source.Span
}

type ForeignTaskABIInfo struct {
	Name        string
	PackageName string
	Declaration string
	Address     string
	Loc         source.Span
}

/*
foreignTokenText must return the original source spelling of a token.

This assumes token.Token exposes Lexeme. Rename this field if your token
structure calls it Text or Value.
*/
func foreignTokenText(tok token.Token) string {
	return tok.Lexeme
}

/*
renderForeignTokenSequence converts raw foreign tokens into valid C text.

Joining tokens with spaces is intentional. C permits whitespace between these
tokens, and this avoids accidentally merging identifiers such as:

	SEAL_THREAD_RESULTSEAL_THREAD_CALL
*/
func renderForeignTokenSequence(
	tokens []token.Token,
) string {
	parts := make(
		[]string,
		0,
		len(tokens),
	)

	for index := 0; index < len(tokens); {
		/*
			Normalize:

				{ name }

			into:

				{name}

			This lets template substitution operate on stable placeholders,
			regardless of the spaces used in Seal source.
		*/
		if index+2 < len(tokens) &&
			foreignTokenText(tokens[index]) == "{" &&
			foreignTokenText(tokens[index+2]) == "}" {
			name :=
				foreignTokenText(
					tokens[index+1],
				)

			parts = append(
				parts,
				"{"+name+"}",
			)

			index += 3
			continue
		}

		parts = append(
			parts,
			foreignTokenText(tokens[index]),
		)

		index++
	}

	return strings.Join(parts, " ")
}

func renderForeignTemplate(
	template string,
	replacements map[string]string,
) string {
	keys := make(
		[]string,
		0,
		len(replacements),
	)

	for key := range replacements {
		keys = append(keys, key)
	}

	// Replace longer placeholders first in case future placeholder names
	// overlap.
	sort.Slice(
		keys,
		func(left int, right int) bool {
			return len(keys[left]) >
				len(keys[right])
		},
	)

	result := template

	for _, key := range keys {
		result = strings.ReplaceAll(
			result,
			"{"+key+"}",
			replacements[key],
		)
	}

	return result
}

func (g *Generator) foreignTaskDeclaration(
	info TaskInfo,
	cName string,
	paramNames []string,
	span source.Span,
) (string, bool) {
	if info.ForeignABI == nil {
		return "", false
	}

	replacements :=
		map[string]string{
			"name": cName,
		}

	paramCount := len(info.ParamTypes)

	if len(paramNames) > paramCount {
		paramCount = len(paramNames)
	}

	for index := 0; index < paramCount; index++ {
		name := fmt.Sprintf(
			"arg%d",
			index,
		)

		if index < len(paramNames) &&
			paramNames[index] != "" {
			name = paramNames[index]
		}

		replacements[fmt.Sprintf(
			"arg%d_name",
			index,
		)] = name
	}

	declaration :=
		renderForeignTemplate(
			info.ForeignABI.Declaration,
			replacements,
		)

	if strings.Contains(declaration, "{") ||
		strings.Contains(declaration, "}") {
		g.error(
			span,
			fmt.Sprintf(
				"foreign task ABI %s contains an unresolved declaration placeholder",
				info.ForeignABI.Name,
			),
		)

		return "/* invalid foreign declaration */ void " +
			cName +
			"(void)", true
	}

	return declaration, true
}

func (g *Generator) foreignTaskAddress(
	info TaskInfo,
	cName string,
	span source.Span,
) (string, bool) {
	if info.ForeignABI == nil {
		return "", false
	}

	address :=
		renderForeignTemplate(
			info.ForeignABI.Address,
			map[string]string{
				"name": cName,
			},
		)

	if strings.Contains(address, "{") ||
		strings.Contains(address, "}") {
		g.error(
			span,
			fmt.Sprintf(
				"foreign task ABI %s contains an unresolved address placeholder",
				info.ForeignABI.Name,
			),
		)

		return "NULL", true
	}

	return address, true
}

func (g *Generator) foreignTaskABIFromExpr(
	expr ast.Expr,
) *ForeignTaskABIInfo {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		return g.foreignTaskABIs[e.Name.Name]

	case *ast.SelectorExpr:
		id, ok :=
			e.Left.(*ast.IdentExpr)

		if !ok {
			return nil
		}

		packageName := id.Name.Name

		if packageName == g.packageName {
			return g.foreignTaskABIs[e.Name.Name]
		}

		pkg :=
			g.typePackageInfo(
				packageName,
			)

		if pkg == nil {
			return nil
		}

		return pkg.ForeignTaskABIs[e.Name.Name]
	}

	return nil
}

func (g *Generator) foreignCType(
	packageName string,
	typeName string,
	info *ForeignTypeInfo,
) CType {
	if info == nil ||
		info.CType == "" {
		return CInvalid
	}

	sealName := typeName

	if packageName != "" &&
		packageName != g.packageName {
		sealName =
			cImportedTypeName(
				packageName,
				typeName,
			)
	}

	return CType{
		Name:     info.CType,
		SealName: sealName,
	}
}

func (g *Generator) foreignValueInContext(
	name string,
) (
	*ForeignValueInfo,
	string,
	bool,
) {
	/*
		While emitting code belonging to another package, an unqualified
		foreign value belongs to that package first.

		For example, while materializing imported package threads:

		    success

		must resolve to threads.success rather than to a same-named value in
		the package currently being generated.
	*/
	if g.typeContextPackage != "" &&
		g.typeContextPackage !=
			g.packageName {
		pkg :=
			g.typePackageInfo(
				g.typeContextPackage,
			)

		if pkg != nil {
			if value :=
				pkg.ForeignValues[name]; value != nil {
				return value,
					g.typeContextPackage,
					true
			}
		}
	}

	if value :=
		g.foreignValues[name]; value != nil {
		return value,
			g.packageName,
			true
	}

	return nil, "", false
}

func (g *Generator) foreignValueType(
	value *ForeignValueInfo,
	packageName string,
) CType {
	if value == nil ||
		value.Type == nil {
		return CInvalid
	}

	/*
		The type belongs to the foreign value declaration itself. It is not a
		type AST from the active generic task and therefore must not be passed
		through cTypeFromAstInContext, which applies g.genericSubst.
	*/
	if packageName != "" &&
		packageName != g.packageName {
		return g.cTypeFromAstInTypeContext(
			packageName,
			value.Type,
		)
	}

	return g.cTypeFromAst(
		value.Type,
	)
}

func (g *Generator) emitTaskPointerExpr(
	expr *ast.TaskPointerExpr,
) string {
	if expr == nil {
		return "NULL"
	}

	resolution, ok :=
		g.taskPointerResolutions[expr]

	if !ok {
		g.error(
			expr.Span(),
			"missing checker resolution for @task_pointer",
		)

		return "NULL"
	}

	if resolution.Candidate == nil {
		g.error(
			expr.Span(),
			"checker resolution for @task_pointer has no task candidate",
		)

		return "NULL"
	}

	/*
		semanticTaskSelection performs all required work:

		- resolves local versus imported ownership,
		- applies the active generic substitution,
		- registers local generic specializations,
		- registers imported generic specializations,
		- returns the final mangled C function name,
		- returns the concrete specialized TaskInfo.
	*/
	name,
		info,
		selected :=
		g.semanticTaskSelection(
			resolution.Candidate,
			resolution.PackageName,
			resolution.GenericArguments,
			expr.Span(),
		)

	if !selected {
		return "NULL"
	}

	if info.ForeignABI == nil {
		g.error(
			expr.Span(),
			fmt.Sprintf(
				"task %s does not have a foreign task ABI",
				name,
			),
		)

		return "NULL"
	}

	address, foreign :=
		g.foreignTaskAddress(
			info,
			name,
			expr.Span(),
		)

	if !foreign {
		g.error(
			expr.Span(),
			fmt.Sprintf(
				"task %s does not provide a foreign address template",
				name,
			),
		)

		return "NULL"
	}

	return address
}

func taskInfoSpan(
	info TaskInfo,
) source.Span {
	if info.Decl != nil {
		return info.Decl.Span()
	}

	if info.ForeignABI != nil {
		return info.ForeignABI.Loc
	}

	return source.Span{}
}
