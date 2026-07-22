package lsp

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/resolver"
	"seal/internal/source"
)

type semanticTargetKind uint8

const (
	semanticTargetInvalid semanticTargetKind = iota
	semanticTargetResolver
	semanticTargetField
	semanticTargetEnumVariant
)

type semanticTarget struct {
	Kind       semanticTargetKind
	Name       string
	Definition source.Span
	Occurrence source.Span
}

func (s *Server) documentSymbols(params DocumentSymbolParams) ([]DocumentSymbol, error) {
	packageSnapshot, file, _, _, err := s.resolveDocumentPosition(
		params.TextDocument.URI,
		Position{},
	)
	if err != nil {
		return nil, err
	}
	if packageSnapshot == nil || file == nil {
		return []DocumentSymbol{}, nil
	}

	global := packageSnapshot.Result.CheckerScope
	if global == nil {
		return []DocumentSymbol{}, nil
	}

	symbols := make([]DocumentSymbol, 0, len(global.Symbols))

	for _, symbol := range global.Symbols {
		if symbol == nil || symbol.Builtin || !sameNavigationFile(symbol.Span.File, file) {
			continue
		}

		fullSpan := symbol.Span
		if symbol.Node != nil {
			fullSpan = symbol.Node.Span()
		}
		if fullSpan.File == nil {
			fullSpan = symbol.Span
		}

		documentSymbol := DocumentSymbol{
			Name:           symbol.Name,
			Detail:         checkerDocumentSymbolDetail(symbol),
			Kind:           checkerDocumentSymbolKind(symbol),
			Range:          protocolRangeFromSpan(fullSpan),
			SelectionRange: protocolRangeFromSpan(symbol.Span),
			Children:       checkerDocumentSymbolChildren(symbol, file),
		}

		symbols = append(symbols, documentSymbol)
	}

	sortDocumentSymbols(symbols)
	return symbols, nil
}

func checkerDocumentSymbolKind(symbol *checker.Symbol) DocumentSymbolKind {
	if symbol == nil {
		return DocumentSymbolVariable
	}

	switch symbol.Kind {
	case checker.SymbolConst:
		return DocumentSymbolConstant
	case checker.SymbolVar, checker.SymbolParam:
		return DocumentSymbolVariable
	case checker.SymbolTask:
		return DocumentSymbolFunction
	case checker.SymbolOverload:
		return DocumentSymbolFunction
	case checker.SymbolForeignTaskABI:
		return DocumentSymbolFunction
	case checker.SymbolPackage:
		return DocumentSymbolPackage
	case checker.SymbolType:
		if symbol.Type == nil {
			return DocumentSymbolClass
		}
		switch symbol.Type.Kind {
		case checker.TypeStruct:
			return DocumentSymbolStruct
		case checker.TypeEnum:
			return DocumentSymbolEnum
		case checker.TypeInterface:
			return DocumentSymbolInterface
		default:
			return DocumentSymbolClass
		}
	default:
		return DocumentSymbolVariable
	}
}

func checkerDocumentSymbolDetail(symbol *checker.Symbol) string {
	if symbol == nil || symbol.Type == nil {
		return ""
	}

	switch symbol.Kind {
	case checker.SymbolTask, checker.SymbolConst, checker.SymbolVar, checker.SymbolParam:
		return symbol.Type.String()
	case checker.SymbolType:
		if symbol.Type.Kind == checker.TypeDistinct && symbol.Type.Underlying != nil {
			return symbol.Type.Underlying.String()
		}
	}

	return ""
}

func checkerDocumentSymbolChildren(symbol *checker.Symbol, file *source.File) []DocumentSymbol {
	if symbol == nil || symbol.Type == nil {
		return nil
	}

	typ := symbol.Type
	children := make([]DocumentSymbol, 0)

	for _, generic := range typ.GenericParams {
		span := generic.Name.Span()
		if generic.Name.Name == "" || !sameNavigationFile(span.File, file) {
			continue
		}
		children = append(children, DocumentSymbol{
			Name:           generic.Name.Name,
			Kind:           DocumentSymbolTypeParameter,
			Range:          protocolRangeFromSpan(span),
			SelectionRange: protocolRangeFromSpan(span),
		})
	}

	for _, field := range typ.Fields {
		if field.Name == "" || !sameNavigationFile(field.Span.File, file) {
			continue
		}
		detail := ""
		if field.Type != nil {
			detail = field.Type.String()
		}
		children = append(children, DocumentSymbol{
			Name:           field.Name,
			Detail:         detail,
			Kind:           DocumentSymbolField,
			Range:          protocolRangeFromSpan(field.Span),
			SelectionRange: protocolRangeFromSpan(field.Span),
		})
	}

	for _, variant := range typ.Variants {
		if variant.Name == "" || !sameNavigationFile(variant.Span.File, file) {
			continue
		}
		children = append(children, DocumentSymbol{
			Name:           variant.Name,
			Kind:           DocumentSymbolEnumMember,
			Range:          protocolRangeFromSpan(variant.Span),
			SelectionRange: protocolRangeFromSpan(variant.Span),
		})
	}

	for _, requirement := range typ.InterfaceRequirements {
		if requirement.Name == "" || !sameNavigationFile(requirement.Span.File, file) {
			continue
		}
		children = append(children, DocumentSymbol{
			Name:           requirement.Name,
			Detail:         checkerInterfaceRequirementDetail(requirement),
			Kind:           DocumentSymbolMethod,
			Range:          protocolRangeFromSpan(requirement.Span),
			SelectionRange: protocolRangeFromSpan(requirement.Span),
		})
	}

	sortDocumentSymbols(children)
	return children
}

func checkerInterfaceRequirementDetail(requirement checker.InterfaceRequirementInfo) string {
	params := make([]string, 0, len(requirement.Params))
	for index, typ := range requirement.Params {
		text := "<invalid>"
		if typ != nil {
			text = typ.String()
		}
		if index < len(requirement.ParamIsVariadic) && requirement.ParamIsVariadic[index] {
			text = "..." + text
		}
		params = append(params, text)
	}

	results := make([]string, 0, len(requirement.Results))
	for _, typ := range requirement.Results {
		if typ == nil {
			results = append(results, "<invalid>")
		} else {
			results = append(results, typ.String())
		}
	}

	detail := "task(" + strings.Join(params, ", ") + ")"
	if len(results) == 1 {
		detail += " " + results[0]
	} else if len(results) > 1 {
		detail += " (" + strings.Join(results, ", ") + ")"
	}
	return detail
}

func sortDocumentSymbols(symbols []DocumentSymbol) {
	sort.SliceStable(symbols, func(i, j int) bool {
		left := symbols[i]
		right := symbols[j]
		if left.Range.Start.Line != right.Range.Start.Line {
			return left.Range.Start.Line < right.Range.Start.Line
		}
		if left.Range.Start.Character != right.Range.Start.Character {
			return left.Range.Start.Character < right.Range.Start.Character
		}
		return left.Name < right.Name
	})
}

func (s *Server) references(params ReferenceParams) ([]Location, error) {
	packageSnapshot, file, offset, _, err := s.resolveDocumentPosition(
		params.TextDocument.URI,
		params.Position,
	)
	if err != nil {
		return nil, err
	}
	if packageSnapshot == nil || file == nil {
		return []Location{}, nil
	}

	resolverSemantic := &packageSnapshot.Result.ResolverSemantic
	checkerSemantic := packageSnapshot.Result.SemanticInfo
	checkerScope := packageSnapshot.Result.CheckerScope

	target := semanticTargetAt(
		file,
		offset,
		resolverSemantic,
		checkerSemantic,
		checkerScope,
	)
	if target.Kind == semanticTargetInvalid {
		return []Location{}, nil
	}

	spans := semanticTargetReferenceSpans(
		target,
		resolverSemantic,
		checkerSemantic,
	)
	if params.Context.IncludeDeclaration {
		spans = append(spans, target.Definition)
	}

	spans = uniqueSortedNavigationSpans(spans)
	locations := make([]Location, 0, len(spans))
	for _, span := range spans {
		location, locationErr := locationFromSpan(span)
		if locationErr != nil || location == nil {
			continue
		}
		locations = append(locations, *location)
	}
	return locations, nil
}

func (s *Server) prepareRename(params PrepareRenameParams) (*PrepareRenameResult, error) {
	packageSnapshot, file, offset, _, err := s.resolveDocumentPosition(
		params.TextDocument.URI,
		params.Position,
	)
	if err != nil {
		return nil, err
	}
	if packageSnapshot == nil || file == nil {
		return nil, nil
	}

	target := semanticTargetAt(
		file,
		offset,
		&packageSnapshot.Result.ResolverSemantic,
		packageSnapshot.Result.SemanticInfo,
		packageSnapshot.Result.CheckerScope,
	)
	if target.Kind == semanticTargetInvalid {
		return nil, nil
	}
	if !semanticTargetDeclaredInSnapshot(
		target,
		&packageSnapshot.Result.ResolverSemantic,
		packageSnapshot.Result.CheckerScope,
	) {
		return nil, errors.New("cannot rename a declaration outside the current package")
	}

	return &PrepareRenameResult{
		Range:       protocolRangeFromSpan(target.Occurrence),
		Placeholder: target.Name,
	}, nil
}

func (s *Server) rename(params RenameParams) (*WorkspaceEdit, error) {
	packageSnapshot, file, offset, _, err := s.resolveDocumentPosition(
		params.TextDocument.URI,
		params.Position,
	)
	if err != nil {
		return nil, err
	}
	if packageSnapshot == nil || file == nil {
		return nil, nil
	}

	newName := strings.TrimSpace(params.NewName)
	if !validSealRenameIdentifier(newName) {
		return nil, fmt.Errorf("%q is not a valid Seal identifier", params.NewName)
	}
	if isSealRenameKeyword(newName) {
		return nil, fmt.Errorf("%q is a reserved Seal name", newName)
	}

	resolverSemantic := &packageSnapshot.Result.ResolverSemantic
	checkerSemantic := packageSnapshot.Result.SemanticInfo
	checkerScope := packageSnapshot.Result.CheckerScope

	target := semanticTargetAt(
		file,
		offset,
		resolverSemantic,
		checkerSemantic,
		checkerScope,
	)
	if target.Kind == semanticTargetInvalid {
		return nil, errors.New("no renameable symbol at the requested position")
	}
	if newName == target.Name {
		return &WorkspaceEdit{Changes: map[string][]TextEdit{}}, nil
	}
	if !semanticTargetDeclaredInSnapshot(target, resolverSemantic, checkerScope) {
		return nil, errors.New("cannot rename a declaration outside the current package")
	}
	if err :=
		validateRenameCollision(
			target,
			newName,
			resolverSemantic,
			checkerScope,
		); err != nil {
		return nil, err
	}

	spans := semanticTargetReferenceSpans(target, resolverSemantic, checkerSemantic)
	spans = append(spans, target.Definition)
	spans = uniqueSortedNavigationSpans(spans)

	changes := map[string][]TextEdit{}
	for _, span := range spans {
		location, locationErr := locationFromSpan(span)
		if locationErr != nil || location == nil {
			continue
		}
		uri := fmt.Sprint(location.URI)
		changes[uri] = append(changes[uri], TextEdit{
			Range:   location.Range,
			NewText: newName,
		})
	}

	for uri := range changes {
		edits := changes[uri]
		sort.SliceStable(edits, func(i, j int) bool {
			left := edits[i].Range.Start
			right := edits[j].Range.Start
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			return left.Character < right.Character
		})
		changes[uri] = edits
	}

	return &WorkspaceEdit{Changes: changes}, nil
}

func semanticTargetAt(
	file *source.File,
	offset int,
	resolverSemantic *resolver.SemanticInfo,
	checkerSemantic checker.SemanticInfo,
	checkerScope *checker.Scope,
) semanticTarget {
	if file == nil {
		return semanticTarget{}
	}

	if resolverSemantic != nil {
		if use :=
			resolverSemantic.UseAt(
				file,
				offset,
			); use != nil &&
			use.Definition.File != nil {
			return semanticTarget{
				Kind: semanticTargetResolver,

				Name: use.Name,

				Definition: use.Definition,

				Occurrence: use.Use,
			}
		}

		if definition :=
			resolverSemantic.DefinitionAt(
				file,
				offset,
			); definition != nil &&
			definition.Span.File != nil {
			return semanticTarget{
				Kind: semanticTargetResolver,

				Name: definition.Name,

				Definition: definition.Span,

				Occurrence: definition.Span,
			}
		}
	}

	if selector := checkerSemantic.SelectorAt(file, offset); selector != nil {
		if field := checkerFieldForSelector(checkerSemantic, selector); field != nil {
			return semanticTarget{
				Kind:       semanticTargetField,
				Name:       field.Name,
				Definition: field.Span,
				Occurrence: selector.Name.Span(),
			}
		}
	}

	if target, ok := compoundFieldTargetAt(checkerSemantic, file, offset); ok {
		return target
	}

	if expr := checkerSemantic.ExprAt(file, offset); expr != nil {
		if literal, ok := expr.(*ast.DotIdentExpr); ok {
			if enumType := enumTypeForLiteral(checkerSemantic, literal); enumType != nil {
				if variant := checkerEnumVariant(enumType, literal.Name.Name); variant != nil {
					return semanticTarget{
						Kind:       semanticTargetEnumVariant,
						Name:       variant.Name,
						Definition: variant.Span,
						Occurrence: literal.Name.Span(),
					}
				}
			}
		}
	}

	if target, ok := declaredCheckerMemberTargetAt(checkerScope, file, offset); ok {
		return target
	}

	return semanticTarget{}
}

func compoundFieldTargetAt(
	semantic checker.SemanticInfo,
	file *source.File,
	offset int,
) (semanticTarget, bool) {
	for expr, typ := range semantic.ExprTypes {
		literal, ok := expr.(*ast.CompoundLiteralExpr)
		if !ok || literal == nil {
			continue
		}
		container := selectorFieldContainerType(typ)
		if container == nil {
			continue
		}
		for _, item := range literal.Fields {
			span := item.Name.Span()
			if !spanContainsNavigationOffset(span, file, offset) {
				continue
			}
			field := checkerFieldByName(container, item.Name.Name)
			if field == nil {
				continue
			}
			return semanticTarget{
				Kind:       semanticTargetField,
				Name:       field.Name,
				Definition: field.Span,
				Occurrence: span,
			}, true
		}
	}
	return semanticTarget{}, false
}

func declaredCheckerMemberTargetAt(
	scope *checker.Scope,
	file *source.File,
	offset int,
) (semanticTarget, bool) {
	if scope == nil {
		return semanticTarget{}, false
	}

	for _, symbol := range scope.Symbols {
		if symbol == nil || symbol.Type == nil {
			continue
		}
		for _, field := range symbol.Type.Fields {
			if spanContainsNavigationOffset(field.Span, file, offset) {
				return semanticTarget{
					Kind:       semanticTargetField,
					Name:       field.Name,
					Definition: field.Span,
					Occurrence: field.Span,
				}, true
			}
		}
		for _, variant := range symbol.Type.Variants {
			if spanContainsNavigationOffset(variant.Span, file, offset) {
				return semanticTarget{
					Kind:       semanticTargetEnumVariant,
					Name:       variant.Name,
					Definition: variant.Span,
					Occurrence: variant.Span,
				}, true
			}
		}
	}

	for _, child := range scope.Children {
		if target, ok := declaredCheckerMemberTargetAt(child, file, offset); ok {
			return target, true
		}
	}

	return semanticTarget{}, false
}

func semanticTargetReferenceSpans(
	target semanticTarget,
	resolverSemantic *resolver.SemanticInfo,
	checkerSemantic checker.SemanticInfo,
) []source.Span {
	switch target.Kind {
	case semanticTargetResolver:
		if resolverSemantic == nil {
			return nil
		}
		return resolverSemantic.UsesOfDefinition(target.Definition)
	case semanticTargetField:
		return checkerFieldReferenceSpans(checkerSemantic, target.Definition)
	case semanticTargetEnumVariant:
		return checkerEnumVariantReferenceSpans(checkerSemantic, target.Definition)
	default:
		return nil
	}
}

func checkerFieldReferenceSpans(
	semantic checker.SemanticInfo,
	definition source.Span,
) []source.Span {
	spans := make([]source.Span, 0)

	for expr, typ := range semantic.ExprTypes {
		switch node := expr.(type) {
		case *ast.SelectorExpr:
			field := checkerFieldForSelector(semantic, node)
			if field != nil && sameNavigationSpan(field.Span, definition) {
				spans = append(spans, node.Name.Span())
			}
		case *ast.CompoundLiteralExpr:
			container := selectorFieldContainerType(typ)
			if container == nil {
				continue
			}
			for _, item := range node.Fields {
				field := checkerFieldByName(container, item.Name.Name)
				if field != nil && sameNavigationSpan(field.Span, definition) {
					spans = append(spans, item.Name.Span())
				}
			}
		}
	}

	return spans
}

func checkerEnumVariantReferenceSpans(
	semantic checker.SemanticInfo,
	definition source.Span,
) []source.Span {
	spans := make([]source.Span, 0)
	for expr := range semantic.ExprTypes {
		literal, ok := expr.(*ast.DotIdentExpr)
		if !ok || literal == nil {
			continue
		}
		enumType := enumTypeForLiteral(semantic, literal)
		if enumType == nil {
			continue
		}
		variant := checkerEnumVariant(enumType, literal.Name.Name)
		if variant != nil && sameNavigationSpan(variant.Span, definition) {
			spans = append(spans, literal.Name.Span())
		}
	}
	return spans
}

func enumTypeForLiteral(
	semantic checker.SemanticInfo,
	literal *ast.DotIdentExpr,
) *checker.Type {
	if literal == nil {
		return nil
	}
	if expected := semantic.ExpectedExprTypes[literal]; expected != nil && expected.Kind == checker.TypeEnum {
		return expected
	}

	for expr := range semantic.ExprTypes {
		binary, ok := expr.(*ast.BinaryExpr)
		if !ok || binary == nil {
			continue
		}
		if binary.Left == literal {
			if typ := semantic.ExprTypes[binary.Right]; typ != nil && typ.Kind == checker.TypeEnum {
				return typ
			}
		}
		if binary.Right == literal {
			if typ := semantic.ExprTypes[binary.Left]; typ != nil && typ.Kind == checker.TypeEnum {
				return typ
			}
		}
	}
	return nil
}

func checkerFieldByName(typ *checker.Type, name string) *checker.FieldInfo {
	if typ == nil {
		return nil
	}
	for index := range typ.Fields {
		if typ.Fields[index].Name == name {
			return &typ.Fields[index]
		}
	}
	return nil
}

func semanticTargetDeclaredInSnapshot(
	target semanticTarget,
	resolverSemantic *resolver.SemanticInfo,
	checkerScope *checker.Scope,
) bool {
	switch target.Kind {
	case semanticTargetResolver:
		if resolverSemantic == nil || target.Definition.File == nil {
			return false
		}
		definition := resolverSemantic.DefinitionAt(
			target.Definition.File,
			target.Definition.Start,
		)
		return definition != nil && sameNavigationSpan(definition.Span, target.Definition)
	case semanticTargetField, semanticTargetEnumVariant:
		return checkerMemberDefinitionExists(checkerScope, target.Kind, target.Definition)
	default:
		return false
	}
}

func checkerMemberDefinitionExists(
	scope *checker.Scope,
	kind semanticTargetKind,
	definition source.Span,
) bool {
	if scope == nil {
		return false
	}
	for _, symbol := range scope.Symbols {
		if symbol == nil || symbol.Type == nil {
			continue
		}
		if kind == semanticTargetField {
			for _, field := range symbol.Type.Fields {
				if sameNavigationSpan(field.Span, definition) {
					return true
				}
			}
		}
		if kind == semanticTargetEnumVariant {
			for _, variant := range symbol.Type.Variants {
				if sameNavigationSpan(variant.Span, definition) {
					return true
				}
			}
		}
	}
	for _, child := range scope.Children {
		if checkerMemberDefinitionExists(child, kind, definition) {
			return true
		}
	}
	return false
}

func validateRenameCollision(
	target semanticTarget,
	newName string,
	resolverSemantic *resolver.SemanticInfo,
	checkerScope *checker.Scope,
) error {
	if target.Kind ==
		semanticTargetResolver &&
		resolverSemantic != nil {
		definition :=
			resolverSemantic.DefinitionAt(
				target.Definition.File,
				target.Definition.Start,
			)

		if definition != nil &&
			definition.Symbol != nil &&
			definition.Symbol.Scope != nil {
			existing :=
				definition.Symbol.Scope.LookupVisible(
					newName,
				)

			if existing != nil &&
				existing != definition.Symbol {
				/*
					User tasks may intentionally replace builtin task names,
					matching Resolver.declareSymbol.
				*/
				allowedBuiltinOverride :=
					existing.Builtin &&
						existing.Kind ==
							resolver.SymbolBuiltinTask &&
						definition.Symbol.Kind ==
							resolver.SymbolTask

				if !allowedBuiltinOverride {
					return fmt.Errorf(
						"%s %q already exists in this scope",
						existing.Kind.String(),
						newName,
					)
				}
			}
		}
	}

	return validateMemberRenameCollision(
		target,
		newName,
		checkerScope,
	)
}

func validateMemberRenameCollision(
	target semanticTarget,
	newName string,
	scope *checker.Scope,
) error {
	if target.Kind != semanticTargetField && target.Kind != semanticTargetEnumVariant {
		return nil
	}
	container := checkerMemberContainer(scope, target.Kind, target.Definition)
	if container == nil {
		return nil
	}

	if target.Kind == semanticTargetField {
		for _, field := range container.Fields {
			if field.Name == newName && !sameNavigationSpan(field.Span, target.Definition) {
				return fmt.Errorf("field %q already exists", newName)
			}
		}
	}
	if target.Kind == semanticTargetEnumVariant {
		for _, variant := range container.Variants {
			if variant.Name == newName && !sameNavigationSpan(variant.Span, target.Definition) {
				return fmt.Errorf("enum variant %q already exists", newName)
			}
		}
	}
	return nil
}

func checkerMemberContainer(
	scope *checker.Scope,
	kind semanticTargetKind,
	definition source.Span,
) *checker.Type {
	if scope == nil {
		return nil
	}
	for _, symbol := range scope.Symbols {
		if symbol == nil || symbol.Type == nil {
			continue
		}
		if kind == semanticTargetField {
			for _, field := range symbol.Type.Fields {
				if sameNavigationSpan(field.Span, definition) {
					return symbol.Type
				}
			}
		}
		if kind == semanticTargetEnumVariant {
			for _, variant := range symbol.Type.Variants {
				if sameNavigationSpan(variant.Span, definition) {
					return symbol.Type
				}
			}
		}
	}
	for _, child := range scope.Children {
		if container := checkerMemberContainer(child, kind, definition); container != nil {
			return container
		}
	}
	return nil
}

func uniqueSortedNavigationSpans(spans []source.Span) []source.Span {
	filtered := make([]source.Span, 0, len(spans))
	for _, span := range spans {
		if span.File == nil {
			continue
		}
		duplicate := false
		for _, existing := range filtered {
			if sameNavigationSpan(existing, span) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			filtered = append(filtered, span)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left := filtered[i]
		right := filtered[j]
		leftPath := normalizedNavigationPath(left.File)
		rightPath := normalizedNavigationPath(right.File)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		if left.Start != right.Start {
			return left.Start < right.Start
		}
		return left.End < right.End
	})
	return filtered
}

func spanContainsNavigationOffset(span source.Span, file *source.File, offset int) bool {
	if !sameNavigationFile(span.File, file) {
		return false
	}
	return offset >= span.Start && offset <= span.End
}

func sameNavigationSpan(left, right source.Span) bool {
	return sameNavigationFile(left.File, right.File) &&
		left.Start == right.Start &&
		left.End == right.End
}

func sameNavigationFile(left, right *source.File) bool {
	if left == right {
		return left != nil
	}
	if left == nil || right == nil || left.Path == "" || right.Path == "" {
		return false
	}
	leftPath := filepath.Clean(left.Path)
	rightPath := filepath.Clean(right.Path)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(leftPath, rightPath)
	}
	return leftPath == rightPath
}

func normalizedNavigationPath(file *source.File) string {
	if file == nil {
		return ""
	}
	path := filepath.Clean(file.Path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func navigationSpanText(span source.Span) string {
	if span.File == nil {
		return ""
	}
	start := span.Start
	end := span.End
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(span.File.Text) {
		start = len(span.File.Text)
	}
	if end > len(span.File.Text) {
		end = len(span.File.Text)
	}
	return span.File.Text[start:end]
}

func validSealRenameIdentifier(name string) bool {
	if name == "" {
		return false
	}
	first, width := utf8.DecodeRuneInString(name)
	if first == utf8.RuneError && width == 0 {
		return false
	}
	if first != '_' && !unicode.IsLetter(first) {
		return false
	}
	for _, value := range name[width:] {
		if value != '_' && !unicode.IsLetter(value) && !unicode.IsDigit(value) {
			return false
		}
	}
	return true
}

func isSealRenameKeyword(name string) bool {
	_, exists := sealRenameKeywords[name]
	return exists
}

var sealRenameKeywords = map[string]struct{}{
	"any": {}, "bool": {}, "break": {}, "case": {}, "char": {},
	"continue": {}, "cstring": {}, "default": {}, "defer": {},
	"distinct": {}, "else": {}, "enum": {}, "extern": {}, "f32": {},
	"f64": {}, "false": {}, "for": {}, "i8": {}, "i16": {}, "i32": {},
	"i64": {}, "if": {}, "impl": {}, "in": {}, "int": {},
	"interface": {}, "intrinsic": {}, "nil": {}, "operator": {},
	"overload": {}, "pure": {}, "rawptr": {}, "return": {},
	"self": {}, "string": {}, "struct": {}, "switch": {}, "task": {},
	"test": {}, "true": {}, "trusted": {}, "type": {}, "u8": {},
	"u16": {}, "u32": {}, "u64": {}, "uint": {}, "union": {},
	"using": {}, "void": {},
}
