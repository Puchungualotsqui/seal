package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	jsonRPCVersion = "2.0"

	methodInitialize  = "initialize"
	methodInitialized = "initialized"
	methodShutdown    = "shutdown"
	methodExit        = "exit"

	methodDidOpen   = "textDocument/didOpen"
	methodDidChange = "textDocument/didChange"
	methodDidSave   = "textDocument/didSave"
	methodDidClose  = "textDocument/didClose"

	methodDefinition    = "textDocument/definition"
	methodCompletion    = "textDocument/completion"
	methodHover         = "textDocument/hover"
	methodSignatureHelp = "textDocument/signatureHelp"
	methodFormatting    = "textDocument/formatting"

	methodDocumentSymbol = "textDocument/documentSymbol"
	methodReferences     = "textDocument/references"
	methodPrepareRename  = "textDocument/prepareRename"
	methodRename         = "textDocument/rename"

	methodPublishDiagnostics = "textDocument/publishDiagnostics"

	methodCancelRequest = "$/cancelRequest"
	methodSetTrace      = "$/setTrace"
)

const (
	errorCodeParseError           = -32700
	errorCodeInvalidRequest       = -32600
	errorCodeMethodNotFound       = -32601
	errorCodeInvalidParams        = -32602
	errorCodeInternalError        = -32603
	errorCodeServerNotInitialized = -32002
)

const (
	textDocumentSyncNone        = 0
	textDocumentSyncFull        = 1
	textDocumentSyncIncremental = 2
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type InitializeParams struct {
	ProcessID *int `json:"processId,omitempty"`

	ClientInfo *ClientInfo `json:"clientInfo,omitempty"`

	RootPath *string `json:"rootPath,omitempty"`
	RootURI  *string `json:"rootUri,omitempty"`

	WorkspaceFolders []WorkspaceFolder `json:"workspaceFolders,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ServerCapabilities struct {
	PositionEncoding string `json:"positionEncoding,omitempty"`

	TextDocumentSync TextDocumentSyncOptions `json:"textDocumentSync"`

	DefinitionProvider         bool                  `json:"definitionProvider,omitempty"`
	HoverProvider              bool                  `json:"hoverProvider,omitempty"`
	DocumentFormattingProvider bool                  `json:"documentFormattingProvider,omitempty"`
	DocumentSymbolProvider     bool                  `json:"documentSymbolProvider,omitempty"`
	ReferencesProvider         bool                  `json:"referencesProvider,omitempty"`
	RenameProvider             *RenameOptions        `json:"renameProvider,omitempty"`
	CompletionProvider         *CompletionOptions    `json:"completionProvider,omitempty"`
	SignatureHelpProvider      *SignatureHelpOptions `json:"signatureHelpProvider,omitempty"`
}

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`

	TrimTrailingWhitespace *bool `json:"trimTrailingWhitespace,omitempty"`
	InsertFinalNewline     *bool `json:"insertFinalNewline,omitempty"`
	TrimFinalNewlines      *bool `json:"trimFinalNewlines,omitempty"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentSymbolKind int

const (
	DocumentSymbolFile          DocumentSymbolKind = 1
	DocumentSymbolModule        DocumentSymbolKind = 2
	DocumentSymbolNamespace     DocumentSymbolKind = 3
	DocumentSymbolPackage       DocumentSymbolKind = 4
	DocumentSymbolClass         DocumentSymbolKind = 5
	DocumentSymbolMethod        DocumentSymbolKind = 6
	DocumentSymbolProperty      DocumentSymbolKind = 7
	DocumentSymbolField         DocumentSymbolKind = 8
	DocumentSymbolConstructor   DocumentSymbolKind = 9
	DocumentSymbolEnum          DocumentSymbolKind = 10
	DocumentSymbolInterface     DocumentSymbolKind = 11
	DocumentSymbolFunction      DocumentSymbolKind = 12
	DocumentSymbolVariable      DocumentSymbolKind = 13
	DocumentSymbolConstant      DocumentSymbolKind = 14
	DocumentSymbolString        DocumentSymbolKind = 15
	DocumentSymbolNumber        DocumentSymbolKind = 16
	DocumentSymbolBoolean       DocumentSymbolKind = 17
	DocumentSymbolArray         DocumentSymbolKind = 18
	DocumentSymbolObject        DocumentSymbolKind = 19
	DocumentSymbolKey           DocumentSymbolKind = 20
	DocumentSymbolNull          DocumentSymbolKind = 21
	DocumentSymbolEnumMember    DocumentSymbolKind = 22
	DocumentSymbolStruct        DocumentSymbolKind = 23
	DocumentSymbolEvent         DocumentSymbolKind = 24
	DocumentSymbolOperator      DocumentSymbolKind = 25
	DocumentSymbolTypeParameter DocumentSymbolKind = 26
)

type DocumentSymbol struct {
	Name string `json:"name"`

	Detail string `json:"detail,omitempty"`

	Kind DocumentSymbolKind `json:"kind"`

	Range Range `json:"range"`

	SelectionRange Range `json:"selectionRange"`

	Children []DocumentSymbol `json:"children,omitempty"`
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`

	Position Position `json:"position"`

	Context ReferenceContext `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type PrepareRenameParams = TextDocumentPositionParams

type PrepareRenameResult struct {
	Range Range `json:"range"`

	Placeholder string `json:"placeholder"`
}

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`

	Position Position `json:"position"`

	NewName string `json:"newName"`
}

type RenameOptions struct {
	PrepareProvider bool `json:"prepareProvider,omitempty"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes,omitempty"`
}

type HoverParams = TextDocumentPositionParams

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`

	Range *Range `json:"range,omitempty"`
}

type CompletionOptions struct {
	ResolveProvider bool `json:"resolveProvider,omitempty"`

	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
}

type SignatureHelpParams = TextDocumentPositionParams

type SignatureHelp struct {
	Signatures []SignatureInformation `json:"signatures"`

	ActiveSignature *int `json:"activeSignature,omitempty"`
	ActiveParameter *int `json:"activeParameter,omitempty"`
}

type SignatureInformation struct {
	Label string `json:"label"`

	Parameters []ParameterInformation `json:"parameters,omitempty"`

	ActiveParameter *int `json:"activeParameter,omitempty"`
}

type ParameterInformation struct {
	Label string `json:"label"`
}

type TextDocumentSyncOptions struct {
	OpenClose bool         `json:"openClose"`
	Change    int          `json:"change"`
	Save      *SaveOptions `json:"save,omitempty"`
}

type SaveOptions struct {
	IncludeText bool `json:"includeText"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DefinitionParams = TextDocumentPositionParams

type CompletionParams = TextDocumentPositionParams

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type TextDocumentContentChangeEvent struct {
	/*
		Range is omitted for full-document synchronization.

		The Seal server advertises full synchronization and rejects ranged
		changes until incremental synchronization is implemented.
	*/
	Range *Range `json:"range,omitempty"`

	RangeLength *int `json:"rangeLength,omitempty"`

	Text string `json:"text"`
}

type DidChangeTextDocumentParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`

	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type PublishDiagnosticsParams struct {
	URI string `json:"uri"`

	Version *int `json:"version,omitempty"`

	Diagnostics []ProtocolDiagnostic `json:"diagnostics"`
}

type ProtocolDiagnostic struct {
	Range Range `json:"range"`

	Severity int `json:"severity,omitempty"`

	Code string `json:"code,omitempty"`

	Source string `json:"source,omitempty"`

	Message string `json:"message"`
}

type CompletionItemKind int

const (
	CompletionItemText          CompletionItemKind = 1
	CompletionItemMethod        CompletionItemKind = 2
	CompletionItemFunction      CompletionItemKind = 3
	CompletionItemConstructor   CompletionItemKind = 4
	CompletionItemField         CompletionItemKind = 5
	CompletionItemVariable      CompletionItemKind = 6
	CompletionItemClass         CompletionItemKind = 7
	CompletionItemInterface     CompletionItemKind = 8
	CompletionItemModule        CompletionItemKind = 9
	CompletionItemProperty      CompletionItemKind = 10
	CompletionItemUnit          CompletionItemKind = 11
	CompletionItemValue         CompletionItemKind = 12
	CompletionItemEnum          CompletionItemKind = 13
	CompletionItemKeyword       CompletionItemKind = 14
	CompletionItemSnippet       CompletionItemKind = 15
	CompletionItemColor         CompletionItemKind = 16
	CompletionItemFile          CompletionItemKind = 17
	CompletionItemReference     CompletionItemKind = 18
	CompletionItemFolder        CompletionItemKind = 19
	CompletionItemEnumMember    CompletionItemKind = 20
	CompletionItemConstant      CompletionItemKind = 21
	CompletionItemStruct        CompletionItemKind = 22
	CompletionItemEvent         CompletionItemKind = 23
	CompletionItemOperator      CompletionItemKind = 24
	CompletionItemTypeParameter CompletionItemKind = 25
)

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label string `json:"label"`

	Kind CompletionItemKind `json:"kind,omitempty"`

	Detail string `json:"detail,omitempty"`

	/*
		SortText controls the initial client ordering.

		The Seal server prefixes ordinary names with "0:" and underscore names
		with "1:" so names beginning with "_" appear after ordinary symbols.
	*/
	SortText string `json:"sortText,omitempty"`

	FilterText string `json:"filterText,omitempty"`
	InsertText string `json:"insertText,omitempty"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func FileURIToPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("file URI cannot be empty")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf(
			"parsing file URI %q: %w",
			value,
			err,
		)
	}

	if parsed.Scheme != "file" {
		return "", fmt.Errorf(
			"unsupported document URI scheme %q",
			parsed.Scheme,
		)
	}

	path := parsed.Path

	if runtime.GOOS == "windows" {
		if parsed.Host != "" &&
			!strings.EqualFold(parsed.Host, "localhost") {
			uncPath :=
				`\\` +
					parsed.Host +
					filepath.FromSlash(path)

			return filepath.Clean(uncPath), nil
		}

		/*
			file:///C:/project/main.seal is parsed with /C:/project/main.seal
			as its path.
		*/
		if len(path) >= 3 &&
			path[0] == '/' &&
			path[2] == ':' {
			path = path[1:]
		}
	} else if parsed.Host != "" &&
		parsed.Host != "localhost" {
		path =
			"//" +
				parsed.Host +
				path
	}

	path = filepath.FromSlash(path)

	if path == "" {
		return "", fmt.Errorf(
			"file URI %q has no path",
			value,
		)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return filepath.Clean(absolutePath), nil
}

func PathToFileURI(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	absolutePath = filepath.Clean(absolutePath)
	slashPath := filepath.ToSlash(absolutePath)

	if runtime.GOOS == "windows" {
		if strings.HasPrefix(slashPath, "//") {
			withoutPrefix :=
				strings.TrimPrefix(
					slashPath,
					"//",
				)

			host, rest, found :=
				strings.Cut(
					withoutPrefix,
					"/",
				)

			if !found {
				return "", fmt.Errorf(
					"invalid UNC path %q",
					path,
				)
			}

			return (&url.URL{
				Scheme: "file",
				Host:   host,
				Path:   "/" + rest,
			}).String(), nil
		}

		if len(slashPath) >= 2 &&
			slashPath[1] == ':' {
			slashPath =
				"/" +
					slashPath
		}
	}

	return (&url.URL{
		Scheme: "file",
		Path:   slashPath,
	}).String(), nil
}
