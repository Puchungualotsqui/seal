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

	"seal/internal/diag"
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
	logger :=
		options.Logger

	if logger == nil {
		logger =
			log.New(
				io.Discard,
				"",
				0,
			)
	}

	name :=
		options.Name

	if name == "" {
		name =
			"Seal Language Server"
	}

	version :=
		options.Version

	if version == "" {
		version =
			"0.1.0"
	}

	return &Server{
		transport: transport,

		logger: logger,

		defaultRoot: options.DefaultRoot,

		name: name,

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
		if err :=
			ctx.Err(); err != nil {
			return err
		}

		payload, err :=
			s.transport.ReadMessage()

		if err != nil {
			if errors.Is(
				err,
				io.EOF,
			) {
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
				Code: errorCodeParseError,

				Message: "Parse error",
			},
		)
	}

	if message.JSONRPC !=
		jsonRPCVersion {
		if hasRequestID(
			message.ID,
		) {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code: errorCodeInvalidRequest,

					Message: "Invalid Request",
				},
			)
		}

		return nil
	}

	if strings.TrimSpace(
		message.Method,
	) == "" {
		if hasRequestID(
			message.ID,
		) {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code: errorCodeInvalidRequest,

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

	if hasRequestID(
		message.ID,
	) {
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
				Code: errorCodeInvalidRequest,

				Message: "server has already shut down",
			},
		)
	}

	switch message.Method {
	case methodInitialize:
		if s.initialized {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code: errorCodeInvalidRequest,

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
					Code: errorCodeInvalidParams,

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
					Code: errorCodeInvalidParams,

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

		s.workspace =
			workspace

		s.initialized =
			true

		return s.sendResult(
			message.ID,
			InitializeResult{
				Capabilities: ServerCapabilities{
					PositionEncoding: "utf-16",

					TextDocumentSync: TextDocumentSyncOptions{
						OpenClose: true,

						Change: textDocumentSyncFull,

						Save: &SaveOptions{
							IncludeText: false,
						},
					},
				},

				ServerInfo: &ServerInfo{
					Name: s.name,

					Version: s.version,
				},
			},
		)

	case methodShutdown:
		if !s.initialized {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code: errorCodeServerNotInitialized,

					Message: "Server not initialized",
				},
			)
		}

		s.shutdown =
			true

		return s.sendResult(
			message.ID,
			nil,
		)

	default:
		if !s.initialized {
			return s.sendError(
				message.ID,
				&ResponseError{
					Code: errorCodeServerNotInitialized,

					Message: "Server not initialized",
				},
			)
		}

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
	if message.Method ==
		methodExit {
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
		return s.analyzeAndPublish(
			ctx,
		)

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

		return s.analyzeAndPublish(
			ctx,
		)

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

		if len(
			params.ContentChanges,
		) == 0 {
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

		return s.analyzeAndPublish(
			ctx,
		)

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

		return s.analyzeAndPublish(
			ctx,
		)

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

		return s.analyzeAndPublish(
			ctx,
		)

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
	lspRange :=
		diagnostic.Span.LSPRange()

	severity :=
		1

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
		Range: Range{
			Start: Position{
				Line: lspRange.Start.Line,

				Character: lspRange.Start.Character,
			},

			End: Position{
				Line: lspRange.End.Line,

				Character: lspRange.End.Character,
			},
		},

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

		return path,
			nil
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
		return *params.RootPath,
			nil
	}

	if strings.TrimSpace(
		fallback,
	) != "" {
		return fallback,
			nil
	}

	return "", fmt.Errorf(
		"initialize request did not provide a workspace root",
	)
}
