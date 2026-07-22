package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/resolver"
	"seal/internal/source"
)

type incomingMessage struct {
	JSONRPC string `json:"jsonrpc"`

	ID json.RawMessage `json:"id,omitempty"`

	Method string `json:"method,omitempty"`

	Params json.RawMessage `json:"params,omitempty"`
}

type outgoingResponse struct {
	JSONRPC string `json:"jsonrpc"`

	ID json.RawMessage `json:"id"`

	Result json.RawMessage `json:"result,omitempty"`

	Error *ResponseError `json:"error,omitempty"`
}

type outgoingNotification struct {
	JSONRPC string `json:"jsonrpc"`

	Method string `json:"method"`

	Params any `json:"params,omitempty"`
}

type ServerOptions struct {
	DefaultRoot string

	Logger *log.Logger

	Name    string
	Version string
}

type Server struct {
	transport *Transport
	logger    *log.Logger

	defaultRoot string

	name    string
	version string

	workspace *Workspace

	initialized bool
	shutdown    bool

	publishedDiagnostics map[string]struct{}
}

func NewServer(
	transport *Transport,
	options ServerOptions,
) *Server {
	logger := options.Logger

	if logger == nil {
		logger =
			log.New(
				io.Discard,
				"",
				0,
			)
	}

	name := options.Name

	if name == "" {
		name = "Seal Language Server"
	}

	version := options.Version

	if version == "" {
		version = "0.1.0"
	}

	return &Server{
		transport: transport,
		logger:    logger,

		defaultRoot: options.DefaultRoot,

		name:    name,
		version: version,

		publishedDiagnostics: map[string]struct{}{},
	}
}

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf(
		"LSP exit requested with code %d",
		e.Code,
	)
}

func (s *Server) Serve(
	ctx context.Context,
) error {
	if s == nil ||
		s.transport == nil {
		return fmt.Errorf(
			"missing LSP server transport",
		)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		payload, err :=
			s.transport.ReadMessage()

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		err =
			s.handlePayload(
				ctx,
				payload,
			)

		if err == nil {
			continue
		}

		var exitError *ExitError

		if errors.As(
			err,
			&exitError,
		) {
			return exitError
		}

		s.logger.Printf(
			"request handling failed: %v",
			err,
		)
	}
}

func (s *Server) handlePayload(
	ctx context.Context,
	payload []byte,
) error {
	message :=
		incomingMessage{}

	if err :=
		json.Unmarshal(
			payload,
			&message,
		); err != nil {
		return s.sendError(
			json.RawMessage("null"),
			&ResponseError{
				Code:    errorCodeParseError,
				Message: "Parse error",
			},
		)
	}

	if message.JSONRPC != jsonRPCVersion {
		if hasRequestID(message.ID) {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidRequest,
					Message: "Invalid Request",
				},
			)
		}

		return nil
	}

	if strings.TrimSpace(message.Method) == "" {
		if hasRequestID(message.ID) {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidRequest,
					Message: "Invalid Request",
				},
			)
		}

		/*
			The server currently sends no client requests, so any incoming
			response can be ignored.
		*/
		return nil
	}

	if hasRequestID(message.ID) {
		return s.handleRequest(
			ctx,
			message,
		)
	}

	return s.handleNotification(
		ctx,
		message,
	)
}

func (s *Server) handleRequest(
	ctx context.Context,
	message incomingMessage,
) error {
	if s.shutdown {
		return s.sendError(
			message.ID,
			&ResponseError{
				Code:    errorCodeInvalidRequest,
				Message: "server has already shut down",
			},
		)
	}

	if message.Method != methodInitialize &&
		!s.initialized {
		return s.sendError(
			message.ID,
			&ResponseError{
				Code:    errorCodeServerNotInitialized,
				Message: "Server not initialized",
			},
		)
	}

	switch message.Method {
	case methodInitialize:
		if s.initialized {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidRequest,
					Message: "server is already initialized",
				},
			)
		}

		params :=
			InitializeParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidParams,
					Message: err.Error(),
				},
			)
		}

		root, err :=
			resolveInitializeRoot(
				params,
				s.defaultRoot,
			)

		if err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidParams,
					Message: err.Error(),
				},
			)
		}

		workspace, err :=
			NewWorkspace(root)

		if err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code: errorCodeInternalError,
					Message: fmt.Sprintf(
						"initializing Seal workspace: %v",
						err,
					),
				},
			)
		}

		s.workspace = workspace
		s.initialized = true

		return s.sendResult(
			message.ID,
			InitializeResult{
				Capabilities: ServerCapabilities{
					PositionEncoding: "utf-16",

					TextDocumentSync: TextDocumentSyncOptions{
						OpenClose: true,
						Change:    textDocumentSyncFull,
						Save: &SaveOptions{
							IncludeText: false,
						},
					},

					DefinitionProvider: true,

					HoverProvider: true,

					CompletionProvider: &CompletionOptions{
						ResolveProvider: false,

						TriggerCharacters: []string{
							".",
						},
					},

					SignatureHelpProvider: &SignatureHelpOptions{
						TriggerCharacters: []string{
							"(",
							",",
						},

						RetriggerCharacters: []string{
							",",
						},
					},
				},

				ServerInfo: &ServerInfo{
					Name:    s.name,
					Version: s.version,
				},
			},
		)

	case methodSignatureHelp:
		params := SignatureHelpParams{}

		if err := decodeParams(message.Params, &params); err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidParams,
					Message: err.Error(),
				},
			)
		}

		help, err := s.signatureHelp(params)

		if err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInternalError,
					Message: err.Error(),
				},
			)
		}

		return s.sendResult(
			message.ID,
			help,
		)

	case methodHover:
		params :=
			HoverParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidParams,
					Message: err.Error(),
				},
			)
		}

		hover, err :=
			s.hover(
				params,
			)

		if err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInternalError,
					Message: err.Error(),
				},
			)
		}

		return s.sendResult(
			message.ID,
			hover,
		)

	case methodShutdown:
		s.shutdown = true

		return s.sendResult(
			message.ID,
			nil,
		)

	case methodDefinition:
		params :=
			DefinitionParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidParams,
					Message: err.Error(),
				},
			)
		}

		location, err :=
			s.definition(
				params,
			)

		if err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInternalError,
					Message: err.Error(),
				},
			)
		}

		return s.sendResult(
			message.ID,
			location,
		)

	case methodCompletion:
		params :=
			CompletionParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInvalidParams,
					Message: err.Error(),
				},
			)
		}

		completion, err :=
			s.completion(
				params,
			)

		if err != nil {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code:    errorCodeInternalError,
					Message: err.Error(),
				},
			)
		}

		return s.sendResult(
			message.ID,
			completion,
		)

	default:
		return s.sendError(
			message.ID,
			&ResponseError{
				Code: errorCodeMethodNotFound,
				Message: fmt.Sprintf(
					"method %q is not supported",
					message.Method,
				),
			},
		)
	}
}

func (s *Server) handleNotification(
	ctx context.Context,
	message incomingMessage,
) error {
	if message.Method == methodExit {
		exitCode := 1

		if s.shutdown {
			exitCode = 0
		}

		return &ExitError{
			Code: exitCode,
		}
	}

	if s.shutdown {
		return nil
	}

	if !s.initialized {
		s.logger.Printf(
			"ignoring notification %q before initialization",
			message.Method,
		)

		return nil
	}

	switch message.Method {
	case methodInitialized:
		return s.analyzeAndPublish(ctx)

	case methodDidOpen:
		params :=
			DidOpenTextDocumentParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return err
		}

		path, err :=
			FileURIToPath(
				params.TextDocument.URI,
			)

		if err != nil {
			return err
		}

		if err :=
			s.workspace.OpenDocument(
				Document{
					URI: params.TextDocument.URI,

					Path: path,

					Version: params.TextDocument.Version,

					Text: params.TextDocument.Text,
				},
			); err != nil {
			return err
		}

		return s.analyzeAndPublish(ctx)

	case methodDidChange:
		params :=
			DidChangeTextDocumentParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return err
		}

		if len(params.ContentChanges) == 0 {
			return nil
		}

		for _, change := range params.ContentChanges {
			if change.Range != nil {
				return fmt.Errorf(
					"incremental text changes are not supported",
				)
			}
		}

		/*
			With full synchronization there should be one content change. Using
			the final change is defensive against clients that send more.
		*/
		change :=
			params.ContentChanges[len(params.ContentChanges)-1]

		if err :=
			s.workspace.ChangeDocument(
				params.TextDocument.URI,
				params.TextDocument.Version,
				change.Text,
			); err != nil {
			return err
		}

		return s.analyzeAndPublish(ctx)

	case methodDidSave:
		params :=
			DidSaveTextDocumentParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return err
		}

		return s.analyzeAndPublish(ctx)

	case methodDidClose:
		params :=
			DidCloseTextDocumentParams{}

		if err :=
			decodeParams(
				message.Params,
				&params,
			); err != nil {
			return err
		}

		s.workspace.CloseDocument(
			params.TextDocument.URI,
		)

		return s.analyzeAndPublish(ctx)

	case methodCancelRequest,
		methodSetTrace:
		return nil

	default:
		/*
			Unknown notifications do not receive JSON-RPC error responses.
		*/
		return nil
	}
}

/*
definition resolves both symbol uses and declaration names.

For a declaration name, definition returns the declaration itself. This makes
the behavior predictable when the command is invoked on either side of a
reference.
*/
func (s *Server) definition(
	params DefinitionParams,
) (
	*Location,
	error,
) {
	packageSnapshot,
		file,
		offset,
		_,
		err :=
		s.resolveDocumentPosition(
			params.TextDocument.URI,
			params.Position,
		)

	if err != nil {
		return nil, err
	}

	if packageSnapshot == nil ||
		file == nil {
		return nil, nil
	}

	resolverSemantic :=
		&packageSnapshot.Result.ResolverSemantic

	/*
		Ordinary lexical and package-qualified references remain resolver-owned.
	*/
	if use :=
		resolverSemantic.UseAt(
			file,
			offset,
		); use != nil &&
		use.Definition.File != nil {
		return locationFromSpan(
			use.Definition,
		)
	}

	/*
		Struct fields are checker-owned because their meaning depends on the
		resolved receiver type.
	*/
	checkerSemantic :=
		packageSnapshot.Result.SemanticInfo

	if selector :=
		checkerSemantic.SelectorAt(
			file,
			offset,
		); selector != nil {
		field :=
			checkerFieldForSelector(
				checkerSemantic,
				selector,
			)

		if field != nil &&
			field.Span.File != nil {
			return locationFromSpan(
				field.Span,
			)
		}
	}

	/*
		Invoking definition on a declaration returns that declaration.
	*/
	if definition :=
		resolverSemantic.DefinitionAt(
			file,
			offset,
		); definition != nil &&
		definition.Span.File != nil {
		return locationFromSpan(
			definition.Span,
		)
	}

	return nil, nil
}

func (s *Server) hover(
	params HoverParams,
) (
	*Hover,
	error,
) {
	packageSnapshot,
		file,
		offset,
		_,
		err :=
		s.resolveDocumentPosition(
			params.TextDocument.URI,
			params.Position,
		)

	if err != nil {
		return nil, err
	}

	if packageSnapshot == nil ||
		file == nil {
		return nil, nil
	}

	checkerSemantic :=
		packageSnapshot.Result.SemanticInfo

	checkerScope :=
		packageSnapshot.Result.CheckerScope

	/*
		Field and package-member selectors need to be handled before generic
		expression hover so the selected member name is displayed.
	*/
	if selector :=
		checkerSemantic.SelectorAt(
			file,
			offset,
		); selector != nil {
		if field :=
			checkerFieldForSelector(
				checkerSemantic,
				selector,
			); field != nil {
			typ := "<invalid>"

			if field.Type != nil {
				typ = field.Type.String()
			}

			return hoverFromText(
				fmt.Sprintf(
					"field %s: %s",
					field.Name,
					typ,
				),
				selector.Name.Span(),
			), nil
		}

		if member, qualifiedName :=
			checkerPackageMemberForSelector(
				checkerScope,
				selector,
			); member != nil {
			return hoverFromText(
				checkerSymbolHoverText(
					member,
					qualifiedName,
				),
				selector.Name.Span(),
			), nil
		}
	}

	/*
		Contextual enum literals retain TypeEnumLiteral as their expression
		type, so use ExpectedExprTypes to recover the containing enum.
	*/
	if expr :=
		checkerSemantic.ExprAt(
			file,
			offset,
		); expr != nil {
		if enumLiteral, ok :=
			expr.(*ast.DotIdentExpr); ok {
			if expectedType, found :=
				checkerSemantic.ExpectedTypeFor(
					expr,
				); found {
				if variant :=
					checkerEnumVariant(
						expectedType,
						enumLiteral.Name.Name,
					); variant != nil {
					return hoverFromText(
						fmt.Sprintf(
							"enum variant %s.%s",
							expectedType.String(),
							variant.Name,
						),
						enumLiteral.Name.Span(),
					), nil
				}
			}
		}
	}

	/*
		Use resolver navigation to map a symbol use back to its checker
		declaration and type.
	*/
	resolverSemantic :=
		&packageSnapshot.Result.ResolverSemantic

	if use := resolverSemantic.UseAt(file, offset); use != nil {
		if checkerScope != nil {
			if symbol := checkerScope.FindSymbolBySpan(use.Definition); symbol != nil {
				hoverSpan := source.Span{
					File:  file,
					Start: offset,
					End:   offset,
				}

				/*
					For ordinary expression identifiers, ExprAt normally returns the
					exact IdentExpr under the cursor, giving hover a proper range.
				*/
				if expr := checkerSemantic.ExprAt(file, offset); expr != nil {
					hoverSpan = expr.Span()
				}

				return hoverFromText(
					checkerSymbolHoverText(symbol, ""),
					hoverSpan,
				), nil
			}
		}
	}

	/*
		Hover directly on a declaration.
	*/
	if checkerScope != nil {
		if symbol :=
			checkerScope.SymbolAt(
				file,
				offset,
			); symbol != nil {
			return hoverFromText(
				checkerSymbolHoverText(
					symbol,
					"",
				),
				symbol.Span,
			), nil
		}
	}

	/*
		Fallback: display the type of the smallest checked expression under the
		cursor.
	*/
	if typ, expr, found :=
		checkerSemantic.TypeAt(
			file,
			offset,
		); found &&
		typ != nil &&
		typ.Kind != checker.TypeInvalid {
		return hoverFromText(
			"type: "+typ.String(),
			expr.Span(),
		), nil
	}

	return nil, nil
}

func checkerSymbolHoverText(
	symbol *checker.Symbol,
	displayName string,
) string {
	if symbol == nil {
		return ""
	}

	name := displayName

	if name == "" {
		name = symbol.Name
	}

	switch symbol.Kind {
	case checker.SymbolPackage:
		return "package " + name

	case checker.SymbolType:
		if symbol.Type == nil {
			return "type " + name
		}

		if symbol.Type.Kind ==
			checker.TypeDistinct &&
			symbol.Type.Underlying != nil {
			return fmt.Sprintf(
				"distinct %s: %s",
				name,
				symbol.Type.Underlying.String(),
			)
		}

		return "type " + name

	case checker.SymbolTask:
		if symbol.Type == nil {
			return "task " + name
		}

		return fmt.Sprintf(
			"%s :: %s",
			name,
			symbol.Type.String(),
		)

	case checker.SymbolOverload:
		return "overload " + name

	case checker.SymbolForeignTaskABI:
		return "foreign task ABI " + name

	case checker.SymbolConst,
		checker.SymbolVar,
		checker.SymbolParam:
		if symbol.Type == nil {
			return fmt.Sprintf(
				"%s %s",
				symbol.Kind.String(),
				name,
			)
		}

		return fmt.Sprintf(
			"%s %s: %s",
			symbol.Kind.String(),
			name,
			symbol.Type.String(),
		)

	default:
		if symbol.Type != nil {
			return fmt.Sprintf(
				"%s: %s",
				name,
				symbol.Type.String(),
			)
		}

		return name
	}
}

func hoverFromText(
	text string,
	span source.Span,
) *Hover {
	hover :=
		&Hover{
			Contents: MarkupContent{
				Kind: "markdown",

				Value: "```seal\n" +
					text +
					"\n```",
			},
		}

	if span.File != nil {
		protocolRange :=
			protocolRangeFromSpan(
				span,
			)

		hover.Range =
			&protocolRange
	}

	return hover
}

/*
completion returns:

  - package members after packageName.
  - checker-resolved fields after value.
  - otherwise all lexically visible symbols.

The client performs prefix and fuzzy filtering using FilterText. Sending all
available candidates gives better results than strict server-side filtering.
*/
func (s *Server) completion(
	params CompletionParams,
) (
	CompletionList,
	error,
) {
	empty :=
		CompletionList{
			IsIncomplete: false,
			Items:        []CompletionItem{},
		}

	packageSnapshot,
		file,
		offset,
		_,
		err :=
		s.resolveDocumentPosition(
			params.TextDocument.URI,
			params.Position,
		)

	if err != nil {
		return empty, err
	}

	if packageSnapshot == nil ||
		file == nil {
		return empty, nil
	}

	resolverSemantic :=
		&packageSnapshot.Result.ResolverSemantic

	scope :=
		resolverSemantic.ScopeAt(
			file,
			offset,
		)

	if scope == nil {
		return empty, nil
	}

	context :=
		completionContextAt(
			file.Text,
			offset,
		)

	if context.AfterDot {
		/*
			First interpret a simple identifier receiver as a package.
		*/
		if context.PackageName != "" {
			symbol :=
				scope.LookupVisible(
					context.PackageName,
				)

			if symbol != nil &&
				symbol.Kind ==
					resolver.SymbolPackage &&
				symbol.Package != nil {
				items :=
					make(
						[]CompletionItem,
						0,
						len(
							symbol.Package.Symbols,
						),
					)

				for _, member := range symbol.Package.Symbols {
					if member == nil ||
						member.Name == "" {
						continue
					}

					items =
						append(
							items,
							makeCompletionItem(
								member.Name,
								member.Kind,
								fmt.Sprintf(
									"%s from package %s",
									member.Kind.String(),
									symbol.Package.Name,
								),
							),
						)
				}

				sortCompletionItems(
					items,
				)

				return CompletionList{
					IsIncomplete: false,

					Items: items,
				}, nil
			}
		}

		checkerSemantic :=
			packageSnapshot.Result.SemanticInfo

		/*
			Try an ordinary selector receiver:

				value.
				pointer.
				call().
				items[index].
		*/
		receiverType,
			receiverExpr,
			receiverFound :=
			checkerSemantic.TypeEndingAtOrBefore(
				file,
				context.DotOffset,
			)

		if receiverFound &&
			receiverExpr != nil &&
			onlyWhitespaceBetween(
				file.Text,
				receiverExpr.Span().End,
				context.DotOffset,
			) {
			items :=
				selectorFieldCompletionItems(
					receiverType,
				)

			return CompletionList{
				IsIncomplete: false,

				Items: items,
			}, nil
		}

		/*
			No receiver was found, so this can be a contextual enum literal:

				state Status = .
				state = .
				return .
		*/
		expectedType,
			_,
			expectedFound :=
			checkerSemantic.ExpectedTypeAt(
				file,
				context.DotOffset,
			)

		if !expectedFound {
			return empty, nil
		}

		items :=
			enumCompletionItems(
				expectedType,
			)

		return CompletionList{
			IsIncomplete: false,

			Items: items,
		}, nil
	}

	visible :=
		scope.VisibleSymbols()

	items :=
		make(
			[]CompletionItem,
			0,
			len(visible),
		)

	seen :=
		map[string]bool{}

	for _, symbol := range visible {
		if symbol == nil ||
			symbol.Name == "" ||
			seen[symbol.Name] {
			continue
		}

		seen[symbol.Name] = true

		detail :=
			symbol.Kind.String()

		if symbol.Builtin {
			detail =
				"builtin " +
					detail
		}

		items =
			append(
				items,
				makeCompletionItem(
					symbol.Name,
					symbol.Kind,
					detail,
				),
			)
	}

	/*
		Interface requirements are callable names but are deliberately stored
		outside the ordinary lexical symbol table.
	*/
	for _, name := range scope.VisibleInterfaceRequirements() {
		if name == "" ||
			seen[name] {
			continue
		}

		seen[name] = true

		items =
			append(
				items,
				CompletionItem{
					Label: name,

					Kind: CompletionItemFunction,

					Detail: "interface requirement",

					SortText: completionSortText(
						name,
					),

					FilterText: name,

					InsertText: name,
				},
			)
	}

	sortCompletionItems(items)

	return CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func enumCompletionItems(
	typ *checker.Type,
) []CompletionItem {
	if typ == nil ||
		typ.Kind != checker.TypeEnum {
		return []CompletionItem{}
	}

	items :=
		make(
			[]CompletionItem,
			0,
			len(typ.Variants),
		)

	seen :=
		map[string]bool{}

	for _, variant := range typ.Variants {
		if variant.Name == "" ||
			seen[variant.Name] {
			continue
		}

		seen[variant.Name] = true

		items =
			append(
				items,
				CompletionItem{
					Label: variant.Name,

					Kind: CompletionItemEnumMember,

					Detail: "variant of " +
						typ.String(),

					SortText: completionSortText(
						variant.Name,
					),

					FilterText: variant.Name,

					InsertText: variant.Name,
				},
			)
	}

	sortCompletionItems(
		items,
	)

	return items
}

func (s *Server) resolveDocumentPosition(
	uri string,
	position Position,
) (
	*PackageSnapshot,
	*source.File,
	int,
	string,
	error,
) {
	if s.workspace == nil {
		return nil,
			nil,
			0,
			"",
			fmt.Errorf(
				"workspace is not initialized",
			)
	}

	path, err :=
		FileURIToPath(uri)

	if err != nil {
		return nil,
			nil,
			0,
			"",
			err
	}

	snapshot :=
		s.workspace.Snapshot()

	if snapshot == nil {
		return nil,
			nil,
			0,
			path,
			nil
	}

	packageSnapshot :=
		snapshot.PackageForPath(path)

	if packageSnapshot == nil {
		return nil,
			nil,
			0,
			path,
			nil
	}

	file :=
		sourceFileForPath(
			packageSnapshot,
			path,
		)

	if file == nil {
		return packageSnapshot,
			nil,
			0,
			path,
			nil
	}

	offset :=
		file.OffsetFromLSPPosition(
			source.LSPPosition{
				Line: position.Line,

				Character: position.Character,
			},
		)

	return packageSnapshot,
		file,
		offset,
		path,
		nil
}

func sourceFileForPath(
	packageSnapshot *PackageSnapshot,
	path string,
) *source.File {
	if packageSnapshot == nil {
		return nil
	}

	requestedKey, err :=
		canonicalPath(path)

	if err != nil {
		return nil
	}

	for _, parsedFile := range packageSnapshot.Result.Files {
		if parsedFile == nil ||
			parsedFile.Source == nil {
			continue
		}

		candidateKey, err :=
			canonicalPath(
				parsedFile.Source.Path,
			)

		if err != nil {
			continue
		}

		if candidateKey == requestedKey {
			return parsedFile.Source
		}
	}

	return nil
}

func locationFromSpan(
	span source.Span,
) (
	*Location,
	error,
) {
	if span.File == nil {
		return nil, nil
	}

	uri, err :=
		PathToFileURI(
			span.File.Path,
		)

	if err != nil {
		return nil, err
	}

	return &Location{
		URI: uri,

		Range: protocolRangeFromSpan(
			span,
		),
	}, nil
}

func protocolRangeFromSpan(
	span source.Span,
) Range {
	lspRange :=
		span.LSPRange()

	return Range{
		Start: Position{
			Line: lspRange.Start.Line,

			Character: lspRange.Start.Character,
		},

		End: Position{
			Line: lspRange.End.Line,

			Character: lspRange.End.Character,
		},
	}
}

type completionContext struct {
	AfterDot bool

	/*
		PackageName is the simple identifier directly before the dot. It is
		also populated for ordinary identifiers; the completion handler decides
		whether that identifier actually denotes a package.
	*/
	PackageName string

	// DotOffset is the byte offset of the selector dot.
	DotOffset int
}

type signatureCandidate struct {
	Symbol   *checker.Symbol
	TaskType *checker.Type
	Name     string
}

func (s *Server) signatureHelp(
	params SignatureHelpParams,
) (*SignatureHelp, error) {
	packageSnapshot, file, offset, _, err :=
		s.resolveDocumentPosition(
			params.TextDocument.URI,
			params.Position,
		)

	if err != nil {
		return nil, err
	}

	if packageSnapshot == nil || file == nil {
		return nil, nil
	}

	semantic := packageSnapshot.Result.SemanticInfo

	call := semantic.CallAt(file, offset)

	if call == nil {
		return nil, nil
	}

	resolution, found :=
		semantic.CallResolutionFor(call)

	if !found {
		return nil, nil
	}

	candidates :=
		signatureCandidatesForResolution(
			call,
			resolution,
		)

	if len(candidates) == 0 {
		return nil, nil
	}

	activeArgument :=
		activeCallArgument(
			call,
			file,
			offset,
		)

	activeSignature :=
		activeSignatureForResolution(
			candidates,
			resolution,
		)

	signatures := make(
		[]SignatureInformation,
		0,
		len(candidates),
	)

	for _, candidate := range candidates {
		activeParameter, hasActiveParameter :=
			activeTaskParameter(
				candidate.TaskType,
				activeArgument,
			)

		signature :=
			makeSignatureInformation(
				candidate,
				activeParameter,
				hasActiveParameter,
			)

		signatures = append(
			signatures,
			signature,
		)
	}

	help := &SignatureHelp{
		Signatures: signatures,
	}

	if activeSignature >= 0 {
		help.ActiveSignature =
			intPointer(activeSignature)
	}

	if activeSignature >= 0 &&
		activeSignature < len(candidates) {
		if activeParameter, ok :=
			activeTaskParameter(
				candidates[activeSignature].TaskType,
				activeArgument,
			); ok {
			help.ActiveParameter =
				intPointer(activeParameter)
		}
	}

	return help, nil
}

func signatureCandidatesForResolution(
	call *ast.CallExpr,
	resolution checker.CallResolution,
) []signatureCandidate {
	name :=
		resolution.Name

	if name == "" {
		name =
			callCalleeName(
				call.Callee,
			)
	}

	if resolution.PackageName != "" &&
		name != "" &&
		!strings.Contains(name, ".") {
		name =
			resolution.PackageName +
				"." +
				name
	}

	/*
		A successfully selected generic overload has a specialized task type.
		Show that specialization rather than its unspecialized candidates.
	*/
	if resolution.Kind ==
		checker.CallResolutionGenericOverload &&
		resolution.TaskType != nil {
		return []signatureCandidate{
			{
				Symbol: resolution.Candidate,

				TaskType: resolution.TaskType,

				Name: name,
			},
		}
	}

	if resolution.Kind ==
		checker.CallResolutionOverload &&
		len(resolution.Candidates) > 0 {
		var candidates []signatureCandidate

		for _, symbol := range resolution.Candidates {
			if symbol == nil ||
				symbol.Type == nil ||
				symbol.Type.Kind != checker.TypeTask {
				continue
			}

			candidates = append(
				candidates,
				signatureCandidate{
					Symbol: symbol,

					TaskType: symbol.Type,

					Name: name,
				},
			)
		}

		return candidates
	}

	if resolution.TaskType == nil ||
		resolution.TaskType.Kind != checker.TypeTask {
		/*
			An unresolved overload still has useful candidate signatures.
		*/
		var candidates []signatureCandidate

		for _, symbol := range resolution.Candidates {
			if symbol == nil ||
				symbol.Type == nil ||
				symbol.Type.Kind != checker.TypeTask {
				continue
			}

			candidates = append(
				candidates,
				signatureCandidate{
					Symbol: symbol,

					TaskType: symbol.Type,

					Name: name,
				},
			)
		}

		return candidates
	}

	return []signatureCandidate{
		{
			Symbol: resolution.Candidate,

			TaskType: resolution.TaskType,

			Name: name,
		},
	}
}

func activeSignatureForResolution(
	candidates []signatureCandidate,
	resolution checker.CallResolution,
) int {
	if len(candidates) == 0 {
		return -1
	}

	if resolution.Candidate == nil {
		return 0
	}

	for i, candidate := range candidates {
		if candidate.Symbol ==
			resolution.Candidate {
			return i
		}

		if candidate.Symbol == nil {
			continue
		}

		if candidate.Symbol.Name ==
			resolution.Candidate.Name &&
			candidate.Symbol.Span.Start ==
				resolution.Candidate.Span.Start &&
			candidate.Symbol.Span.End ==
				resolution.Candidate.Span.End {
			return i
		}
	}

	return 0
}

func activeTaskParameter(
	taskType *checker.Type,
	activeArgument int,
) (int, bool) {
	if taskType == nil ||
		taskType.Kind != checker.TypeTask ||
		len(taskType.Params) == 0 {
		return 0, false
	}

	if activeArgument < 0 {
		activeArgument = 0
	}

	isVariadic :=
		taskType.IsVariadic

	if !isVariadic {
		for _, variadic := range taskType.ParamIsVariadic {
			if variadic {
				isVariadic = true
				break
			}
		}
	}

	if isVariadic &&
		activeArgument >=
			len(taskType.Params)-1 {
		return len(taskType.Params) - 1,
			true
	}

	if activeArgument >=
		len(taskType.Params) {
		return len(taskType.Params) - 1,
			true
	}

	return activeArgument, true
}

func makeSignatureInformation(
	candidate signatureCandidate,
	activeParameter int,
	hasActiveParameter bool,
) SignatureInformation {
	taskType := candidate.TaskType
	name := candidate.Name

	if name == "" && candidate.Symbol != nil {
		name = candidate.Symbol.Name
	}

	if name == "" && taskType != nil {
		name = taskType.Name
	}

	if name == "" {
		name = "<task>"
	}

	name =
		signatureGenericName(
			name,
			taskType,
		)

	parameterLabels :=
		make(
			[]string,
			0,
			len(taskType.Params),
		)

	parameters :=
		make(
			[]ParameterInformation,
			0,
			len(taskType.Params),
		)

	taskDecl, _ :=
		candidate.Symbol.Node.(*ast.TaskDecl)

	for i, typ := range taskType.Params {
		typeName := "<invalid>"

		if typ != nil {
			typeName = typ.String()
		}

		variadic :=
			taskType.IsVariadic &&
				i ==
					len(taskType.Params)-1

		if i < len(taskType.ParamIsVariadic) &&
			taskType.ParamIsVariadic[i] {
			variadic = true
		}

		if variadic {
			typeName = "..." + typeName
		}

		parameterName := ""

		if taskDecl != nil &&
			i < len(taskDecl.Params) {
			parameterName =
				taskDecl.Params[i].Name.Name
		}

		label := typeName

		if parameterName != "" {
			label =
				parameterName +
					" " +
					typeName
		}

		if i < len(taskType.ParamHasDefault) &&
			taskType.ParamHasDefault[i] {
			label += " = default"
		}

		parameterLabels = append(
			parameterLabels,
			label,
		)

		parameters = append(
			parameters,
			ParameterInformation{
				Label: label,
			},
		)
	}

	label :=
		name +
			"(" +
			strings.Join(
				parameterLabels,
				", ",
			) +
			")" +
			signatureResultSuffix(
				taskType,
			)

	signature :=
		SignatureInformation{
			Label: label,

			Parameters: parameters,
		}

	if hasActiveParameter {
		signature.ActiveParameter =
			intPointer(activeParameter)
	}

	return signature
}

func signatureGenericName(
	name string,
	taskType *checker.Type,
) string {
	if taskType == nil ||
		len(taskType.GenericParams) == 0 ||
		strings.Contains(name, "<") {
		return name
	}

	var params []string

	for _, param := range taskType.GenericParams {
		if param.Name.Name == "" {
			continue
		}

		params = append(
			params,
			param.Name.Name,
		)
	}

	if len(params) == 0 {
		return name
	}

	return name +
		"<" +
		strings.Join(params, ", ") +
		">"
}

func signatureResultSuffix(
	taskType *checker.Type,
) string {
	if taskType == nil ||
		len(taskType.Results) == 0 {
		return ""
	}

	results :=
		make(
			[]string,
			0,
			len(taskType.Results),
		)

	for _, result := range taskType.Results {
		if result == nil {
			results = append(
				results,
				"<invalid>",
			)
			continue
		}

		results = append(
			results,
			result.String(),
		)
	}

	if len(results) == 1 {
		return " " + results[0]
	}

	return " (" +
		strings.Join(results, ", ") +
		")"
}

func callCalleeName(
	expr ast.Expr,
) string {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name.Name

	case *ast.SelectorExpr:
		left :=
			callCalleeName(
				e.Left,
			)

		if left == "" {
			return e.Name.Name
		}

		return left +
			"." +
			e.Name.Name

	case *ast.GenericExpr:
		return callCalleeName(
			e.Base,
		)
	}

	return ""
}

func activeCallArgument(
	call *ast.CallExpr,
	file *source.File,
	offset int,
) int {
	if call == nil ||
		file == nil {
		return 0
	}

	if offset < 0 {
		offset = 0
	}

	if offset > len(file.Text) {
		offset = len(file.Text)
	}

	if len(call.Args) == 0 {
		return 0
	}

	for i, arg := range call.Args {
		if arg == nil {
			continue
		}

		span := arg.Span()

		if offset < span.Start {
			if i == 0 {
				return 0
			}

			previousEnd :=
				call.Args[i-1].Span().End

			if sourceContainsComma(
				file.Text,
				previousEnd,
				offset,
			) {
				return i
			}

			return i - 1
		}

		if offset <= span.End {
			return i
		}
	}

	lastIndex :=
		len(call.Args) - 1

	lastEnd :=
		call.Args[lastIndex].Span().End

	if sourceContainsComma(
		file.Text,
		lastEnd,
		offset,
	) {
		return len(call.Args)
	}

	return lastIndex
}

func sourceContainsComma(
	text string,
	start int,
	end int,
) bool {
	if start < 0 {
		start = 0
	}

	if end < start {
		return false
	}

	if start > len(text) {
		start = len(text)
	}

	if end > len(text) {
		end = len(text)
	}

	return strings.Contains(
		text[start:end],
		",",
	)
}

func intPointer(
	value int,
) *int {
	return &value
}

/*
completionContextAt recognizes:

	package.
	package.Member
	package . Member

It also records the dot for arbitrary expression receivers:

	call().
	items[index].
	value.field.

The parser is not used because completion is often requested while the current
selector is syntactically incomplete.
*/
func completionContextAt(
	text string,
	offset int,
) completionContext {
	if offset < 0 {
		offset = 0
	}

	if offset > len(text) {
		offset = len(text)
	}

	prefixStart :=
		identifierStart(
			text,
			offset,
		)

	cursor :=
		skipWhitespaceBackward(
			text,
			prefixStart,
		)

	if cursor == 0 {
		return completionContext{}
	}

	value, width :=
		utf8.DecodeLastRuneInString(
			text[:cursor],
		)

	if value != '.' {
		return completionContext{}
	}

	dotOffset :=
		cursor -
			width

	cursor = dotOffset

	cursor =
		skipWhitespaceBackward(
			text,
			cursor,
		)

	packageStart :=
		identifierStart(
			text,
			cursor,
		)

	if packageStart == cursor {
		return completionContext{
			AfterDot: true,

			DotOffset: dotOffset,
		}
	}

	return completionContext{
		AfterDot: true,

		PackageName: text[packageStart:cursor],

		DotOffset: dotOffset,
	}
}

/*
selectorFieldCompletionItems returns fields accessible through Seal selector
syntax.

The checker currently performs one automatic pointer dereference during field
selection, so completion follows the same rule.
*/
func selectorFieldCompletionItems(
	typ *checker.Type,
) []CompletionItem {
	typ =
		selectorFieldContainerType(
			typ,
		)

	if typ == nil {
		return []CompletionItem{}
	}

	items :=
		make(
			[]CompletionItem,
			0,
			len(typ.Fields),
		)

	seen :=
		map[string]bool{}

	for _, field := range typ.Fields {
		if field.Name == "" ||
			seen[field.Name] {
			continue
		}

		seen[field.Name] = true

		detail :=
			"field"

		if field.Type != nil {
			detail =
				"field: " +
					field.Type.String()
		}

		items =
			append(
				items,
				CompletionItem{
					Label: field.Name,

					Kind: CompletionItemField,

					Detail: detail,

					SortText: completionSortText(
						field.Name,
					),

					FilterText: field.Name,

					InsertText: field.Name,
				},
			)
	}

	sortCompletionItems(
		items,
	)

	return items
}

func selectorFieldContainerType(
	typ *checker.Type,
) *checker.Type {
	if typ == nil {
		return nil
	}

	/*
		Match checkSelectorExpr, which automatically dereferences one typed
		pointer before looking for fields.
	*/
	if typ.Kind ==
		checker.TypePointer {
		typ = typ.Elem
	}

	if typ == nil {
		return nil
	}

	switch typ.Kind {
	case checker.TypeStruct,
		checker.TypeTypeParam:
		return typ

	default:
		return nil
	}
}

/*
onlyWhitespaceBetween verifies that the expression selected by
ExprEndingAtOrBefore is directly adjacent to the completion dot, allowing
formatting whitespace:

	value .
*/
func onlyWhitespaceBetween(
	text string,
	start int,
	end int,
) bool {
	if start < 0 {
		start = 0
	}

	if end < 0 {
		end = 0
	}

	if start > len(text) {
		start = len(text)
	}

	if end > len(text) {
		end = len(text)
	}

	if start > end {
		return false
	}

	return strings.TrimSpace(
		text[start:end],
	) == ""
}

func identifierStart(
	text string,
	offset int,
) int {
	if offset < 0 {
		return 0
	}

	if offset > len(text) {
		offset = len(text)
	}

	for offset > 0 {
		value, width :=
			utf8.DecodeLastRuneInString(
				text[:offset],
			)

		if width <= 0 ||
			!isIdentifierRune(value) {
			break
		}

		offset -= width
	}

	return offset
}

func skipWhitespaceBackward(
	text string,
	offset int,
) int {
	if offset < 0 {
		return 0
	}

	if offset > len(text) {
		offset = len(text)
	}

	for offset > 0 {
		value, width :=
			utf8.DecodeLastRuneInString(
				text[:offset],
			)

		if width <= 0 ||
			!unicode.IsSpace(value) {
			break
		}

		offset -= width
	}

	return offset
}

func isIdentifierRune(
	value rune,
) bool {
	return value == '_' ||
		unicode.IsLetter(value) ||
		unicode.IsDigit(value)
}

func makeCompletionItem(
	name string,
	kind resolver.SymbolKind,
	detail string,
) CompletionItem {
	return CompletionItem{
		Label: name,

		Kind: completionItemKind(
			kind,
		),

		Detail: detail,

		SortText: completionSortText(
			name,
		),

		FilterText: name,

		InsertText: name,
	}
}

func completionItemKind(
	kind resolver.SymbolKind,
) CompletionItemKind {
	switch kind {
	case resolver.SymbolPackage:
		return CompletionItemModule

	case resolver.SymbolConst:
		return CompletionItemConstant

	case resolver.SymbolVar,
		resolver.SymbolParam:
		return CompletionItemVariable

	case resolver.SymbolTask,
		resolver.SymbolOverload,
		resolver.SymbolForeignTaskABI,
		resolver.SymbolBuiltinTask:
		return CompletionItemFunction

	case resolver.SymbolStruct:
		return CompletionItemStruct

	case resolver.SymbolEnum:
		return CompletionItemEnum

	case resolver.SymbolInterface:
		return CompletionItemInterface

	case resolver.SymbolBitSet:
		return CompletionItemEnum

	case resolver.SymbolDistinct,
		resolver.SymbolUnion,
		resolver.SymbolForeignType,
		resolver.SymbolBuiltinType:
		return CompletionItemClass

	case resolver.SymbolGenericType,
		resolver.SymbolGenericEnum,
		resolver.SymbolGenericUnion,
		resolver.SymbolGenericTask:
		return CompletionItemTypeParameter

	case resolver.SymbolGenericValue:
		return CompletionItemValue

	default:
		return CompletionItemText
	}
}

/*
completionSortText groups names beginning with "_" after ordinary identifiers.

Examples:

	Alpha      -> 0:alpha
	Print      -> 0:print
	_internal  -> 1:_internal
	__builtin  -> 1:__builtin
*/
func completionSortText(
	label string,
) string {
	group := "0:"

	if strings.HasPrefix(
		label,
		"_",
	) {
		group = "1:"
	}

	return group +
		strings.ToLower(label)
}

func checkerFieldForSelector(
	semantic checker.SemanticInfo,
	selector *ast.SelectorExpr,
) *checker.FieldInfo {
	if selector == nil ||
		selector.Left == nil {
		return nil
	}

	receiverType :=
		semantic.ExprTypes[selector.Left]

	container :=
		selectorFieldContainerType(
			receiverType,
		)

	if container == nil {
		return nil
	}

	for i := range container.Fields {
		if container.Fields[i].Name ==
			selector.Name.Name {
			return &container.Fields[i]
		}
	}

	return nil
}

func checkerPackageMemberForSelector(
	scope *checker.Scope,
	selector *ast.SelectorExpr,
) (
	*checker.Symbol,
	string,
) {
	if scope == nil ||
		selector == nil {
		return nil, ""
	}

	packageIdent, ok :=
		selector.Left.(*ast.IdentExpr)

	if !ok {
		return nil, ""
	}

	packageSymbol :=
		scope.Lookup(
			packageIdent.Name.Name,
		)

	if packageSymbol == nil ||
		packageSymbol.Kind !=
			checker.SymbolPackage ||
		packageSymbol.Package == nil {
		return nil, ""
	}

	member :=
		packageSymbol.Package.Symbols[selector.Name.Name]

	if member == nil {
		return nil, ""
	}

	return member,
		packageIdent.Name.Name +
			"." +
			selector.Name.Name
}

func checkerEnumVariant(
	typ *checker.Type,
	name string,
) *checker.EnumVariantInfo {
	if typ == nil ||
		typ.Kind != checker.TypeEnum {
		return nil
	}

	for i := range typ.Variants {
		if typ.Variants[i].Name ==
			name {
			return &typ.Variants[i]
		}
	}

	return nil
}

func sortCompletionItems(
	items []CompletionItem,
) {
	sort.SliceStable(
		items,
		func(
			left int,
			right int,
		) bool {
			if items[left].SortText !=
				items[right].SortText {
				return items[left].SortText <
					items[right].SortText
			}

			return items[left].Label <
				items[right].Label
		},
	)
}

func (s *Server) analyzeAndPublish(
	ctx context.Context,
) error {
	if s.workspace == nil {
		return fmt.Errorf(
			"workspace is not initialized",
		)
	}

	var snapshot *WorkspaceSnapshot
	var err error

	/*
		Stale analysis should be rare while requests are processed serially,
		but retry so this remains correct once background analysis is added.
	*/
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err =
			s.workspace.Analyze(ctx)

		if !errors.Is(
			err,
			ErrStaleAnalysis,
		) {
			break
		}
	}

	if err != nil {
		return err
	}

	return s.publishSnapshotDiagnostics(
		snapshot,
	)
}

type diagnosticBatch struct {
	Path string

	Diagnostics []diag.Diagnostic
}

func (s *Server) publishSnapshotDiagnostics(
	snapshot *WorkspaceSnapshot,
) error {
	if snapshot == nil {
		return nil
	}

	batches :=
		map[string]*diagnosticBatch{}

	for _, packageName := range snapshot.Order {
		packageSnapshot :=
			snapshot.Packages[packageName]

		if packageSnapshot == nil {
			continue
		}

		for _, parsedFile := range packageSnapshot.Result.Files {
			if parsedFile == nil ||
				parsedFile.Source == nil {
				continue
			}

			key, err :=
				canonicalPath(
					parsedFile.Source.Path,
				)

			if err != nil {
				continue
			}

			if batches[key] == nil {
				batches[key] =
					&diagnosticBatch{
						Path: parsedFile.Source.Path,

						Diagnostics: []diag.Diagnostic{},
					}
			}
		}

		for _, diagnostic := range packageSnapshot.Result.Diagnostics {
			if diagnostic.Span.File == nil {
				continue
			}

			path :=
				diagnostic.Span.File.Path

			key, err :=
				canonicalPath(path)

			if err != nil {
				continue
			}

			batch :=
				batches[key]

			if batch == nil {
				batch =
					&diagnosticBatch{
						Path: path,

						Diagnostics: []diag.Diagnostic{},
					}

				batches[key] =
					batch
			}

			batch.Diagnostics =
				append(
					batch.Diagnostics,
					diagnostic,
				)
		}
	}

	keys :=
		make(
			[]string,
			0,
			len(batches),
		)

	for key := range batches {
		keys =
			append(
				keys,
				key,
			)
	}

	sort.Strings(keys)

	documentSnapshot :=
		s.workspace.Documents().Snapshot()

	currentlyPublished :=
		map[string]struct{}{}

	for _, key := range keys {
		batch :=
			batches[key]

		uri, err :=
			PathToFileURI(
				batch.Path,
			)

		if err != nil {
			return err
		}

		protocolDiagnostics :=
			make(
				[]ProtocolDiagnostic,
				0,
				len(batch.Diagnostics),
			)

		for _, diagnostic := range batch.Diagnostics {
			protocolDiagnostics =
				append(
					protocolDiagnostics,
					convertDiagnostic(
						diagnostic,
					),
				)
		}

		var version *int

		if document, found :=
			documentSnapshot.DocumentByPath(
				batch.Path,
			); found {
			documentVersion :=
				document.Version

			version =
				&documentVersion
		}

		if err :=
			s.sendNotification(
				methodPublishDiagnostics,
				PublishDiagnosticsParams{
					URI: uri,

					Version: version,

					Diagnostics: protocolDiagnostics,
				},
			); err != nil {
			return err
		}

		currentlyPublished[uri] =
			struct{}{}
	}

	var removedURIs []string

	for uri := range s.publishedDiagnostics {
		if _, found :=
			currentlyPublished[uri]; found {
			continue
		}

		removedURIs =
			append(
				removedURIs,
				uri,
			)
	}

	sort.Strings(
		removedURIs,
	)

	for _, uri := range removedURIs {
		if err :=
			s.sendNotification(
				methodPublishDiagnostics,
				PublishDiagnosticsParams{
					URI: uri,

					Diagnostics: []ProtocolDiagnostic{},
				},
			); err != nil {
			return err
		}
	}

	s.publishedDiagnostics =
		currentlyPublished

	return nil
}

func convertDiagnostic(
	diagnostic diag.Diagnostic,
) ProtocolDiagnostic {
	severity := 1

	switch diagnostic.Severity {
	case diag.SeverityWarning:
		severity = 2

	case diag.SeverityInformation:
		severity = 3

	case diag.SeverityHint:
		severity = 4

	case diag.SeverityInvalid,
		diag.SeverityError:
		severity = 1
	}

	return ProtocolDiagnostic{
		Range: protocolRangeFromSpan(
			diagnostic.Span,
		),

		Severity: severity,

		Code: diagnostic.Code,

		Source: diagnostic.Source,

		Message: diagnostic.Message,
	}
}

func (s *Server) sendResult(
	id json.RawMessage,
	result any,
) error {
	encodedResult, err :=
		json.Marshal(result)

	if err != nil {
		return err
	}

	return s.transport.WriteMessage(
		outgoingResponse{
			JSONRPC: jsonRPCVersion,

			ID: copyRawMessage(id),

			Result: encodedResult,
		},
	)
}

func (s *Server) sendError(
	id json.RawMessage,
	responseError *ResponseError,
) error {
	return s.transport.WriteMessage(
		outgoingResponse{
			JSONRPC: jsonRPCVersion,

			ID: copyRawMessage(id),

			Error: responseError,
		},
	)
}

func (s *Server) sendNotification(
	method string,
	params any,
) error {
	return s.transport.WriteMessage(
		outgoingNotification{
			JSONRPC: jsonRPCVersion,

			Method: method,

			Params: params,
		},
	)
}

func decodeParams(
	raw json.RawMessage,
	target any,
) error {
	trimmed :=
		bytes.TrimSpace(raw)

	if len(trimmed) == 0 ||
		bytes.Equal(
			trimmed,
			[]byte("null"),
		) {
		return nil
	}

	if err :=
		json.Unmarshal(
			trimmed,
			target,
		); err != nil {
		return fmt.Errorf(
			"invalid request parameters: %w",
			err,
		)
	}

	return nil
}

func hasRequestID(
	id json.RawMessage,
) bool {
	return len(
		bytes.TrimSpace(id),
	) > 0
}

func copyRawMessage(
	message json.RawMessage,
) json.RawMessage {
	if len(message) == 0 {
		return json.RawMessage(
			"null",
		)
	}

	return append(
		json.RawMessage(nil),
		message...,
	)
}

func resolveInitializeRoot(
	params InitializeParams,
	fallback string,
) (string, error) {
	for _, folder := range params.WorkspaceFolders {
		if strings.TrimSpace(
			folder.URI,
		) == "" {
			continue
		}

		path, err :=
			FileURIToPath(
				folder.URI,
			)

		if err != nil {
			return "", err
		}

		return path, nil
	}

	if params.RootURI != nil &&
		strings.TrimSpace(
			*params.RootURI,
		) != "" {
		return FileURIToPath(
			*params.RootURI,
		)
	}

	if params.RootPath != nil &&
		strings.TrimSpace(
			*params.RootPath,
		) != "" {
		return *params.RootPath, nil
	}

	if strings.TrimSpace(fallback) != "" {
		return fallback, nil
	}

	return "", fmt.Errorf(
		"initialize request did not provide a workspace root",
	)
}
