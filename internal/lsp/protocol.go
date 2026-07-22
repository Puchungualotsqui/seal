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

	methodPublishDiagnostics = "textDocument/publishDiagnostics"

	methodCancelRequest = "$/cancelRequest"
	methodSetTrace      = "$/setTrace"
)

const (
	errorCodeParseError     = -32700
	errorCodeInvalidRequest = -32600
	errorCodeMethodNotFound = -32601
	errorCodeInvalidParams  = -32602
	errorCodeInternalError  = -32603

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
	PositionEncoding string                  `json:"positionEncoding,omitempty"`
	TextDocumentSync TextDocumentSyncOptions `json:"textDocumentSync"`
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

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func FileURIToPath(
	value string,
) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf(
			"file URI cannot be empty",
		)
	}

	parsed, err :=
		url.Parse(value)

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
			!strings.EqualFold(
				parsed.Host,
				"localhost",
			) {
			uncPath :=
				`\\` +
					parsed.Host +
					filepath.FromSlash(path)

			return filepath.Clean(
				uncPath,
			), nil
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

	path =
		filepath.FromSlash(path)

	if path == "" {
		return "", fmt.Errorf(
			"file URI %q has no path",
			value,
		)
	}

	absolutePath, err :=
		filepath.Abs(path)

	if err != nil {
		return "", err
	}

	return filepath.Clean(
		absolutePath,
	), nil
}

func PathToFileURI(
	path string,
) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf(
			"path cannot be empty",
		)
	}

	absolutePath, err :=
		filepath.Abs(path)

	if err != nil {
		return "", err
	}

	absolutePath =
		filepath.Clean(
			absolutePath,
		)

	slashPath :=
		filepath.ToSlash(
			absolutePath,
		)

	if runtime.GOOS == "windows" {
		if strings.HasPrefix(
			slashPath,
			"//",
		) {
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
