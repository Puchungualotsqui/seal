package lsp

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

/*
Document represents one open editor buffer.

Text is the complete current document contents. The initial LSP implementation
will use full-document synchronization, so incremental edits are not represented
here.
*/
type Document struct {
	URI     string
	Path    string
	Version int
	Text    string
}

/*
DocumentSnapshot is an immutable copy of all currently open documents.

Revision changes whenever a document is opened, changed, or closed. Workspace
analysis uses it to avoid publishing results produced from stale editor text.
*/
type DocumentSnapshot struct {
	Revision uint64

	Documents []Document

	byPath map[string]Document
	byURI  map[string]Document
}

func (s DocumentSnapshot) DocumentByPath(
	path string,
) (Document, bool) {
	key, err :=
		canonicalPath(
			path,
		)

	if err != nil {
		return Document{},
			false
	}

	document, found :=
		s.byPath[key]

	return document,
		found
}

func (s DocumentSnapshot) DocumentByURI(
	uri string,
) (Document, bool) {
	document, found :=
		s.byURI[uri]

	return document,
		found
}

func (s DocumentSnapshot) TextForPath(
	path string,
) (string, bool) {
	document, found :=
		s.DocumentByPath(
			path,
		)

	if !found {
		return "",
			false
	}

	return document.Text,
		true
}

/*
DocumentStore owns all open editor documents.

All methods are safe for concurrent use. Snapshot returns copies, so callers
may analyze documents without holding the store lock.
*/
type DocumentStore struct {
	mu sync.RWMutex

	revision uint64

	byPath map[string]Document
	byURI  map[string]string
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{
		byPath: map[string]Document{},

		byURI: map[string]string{},
	}
}

func (s *DocumentStore) Revision() uint64 {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.revision
}

func (s *DocumentStore) Open(
	document Document,
) error {
	if s == nil {
		return fmt.Errorf(
			"missing document store",
		)
	}

	normalized, key, err :=
		normalizeDocument(
			document,
		)

	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if previousPath, found :=
		s.byURI[normalized.URI]; found &&
		previousPath != key {
		delete(
			s.byPath,
			previousPath,
		)
	}

	s.byPath[key] =
		normalized

	s.byURI[normalized.URI] =
		key

	s.revision++

	return nil
}

func (s *DocumentStore) Change(
	uri string,
	version int,
	text string,
) error {
	if s == nil {
		return fmt.Errorf(
			"missing document store",
		)
	}

	if strings.TrimSpace(uri) == "" {
		return fmt.Errorf(
			"document URI cannot be empty",
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pathKey, found :=
		s.byURI[uri]

	if !found {
		return fmt.Errorf(
			"document %q is not open",
			uri,
		)
	}

	document, found :=
		s.byPath[pathKey]

	if !found {
		return fmt.Errorf(
			"document store is missing path for %q",
			uri,
		)
	}

	/*
		LSP document versions must increase. Some clients use zero for an
		initial version, so only reject a version that moves backwards or
		repeats an already accepted change.
	*/
	if version <= document.Version {
		return fmt.Errorf(
			"document %q received stale version %d; current version is %d",
			uri,
			version,
			document.Version,
		)
	}

	document.Version =
		version

	document.Text =
		text

	s.byPath[pathKey] =
		document

	s.revision++

	return nil
}

/*
Replace changes a document by path instead of URI.

This is useful for tests and for protocol clients that have already converted
a URI into a filesystem path.
*/
func (s *DocumentStore) Replace(
	path string,
	version int,
	text string,
) error {
	if s == nil {
		return fmt.Errorf(
			"missing document store",
		)
	}

	key, err :=
		canonicalPath(
			path,
		)

	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	document, found :=
		s.byPath[key]

	if !found {
		return fmt.Errorf(
			"document %q is not open",
			path,
		)
	}

	if version <= document.Version {
		return fmt.Errorf(
			"document %q received stale version %d; current version is %d",
			path,
			version,
			document.Version,
		)
	}

	document.Version =
		version

	document.Text =
		text

	s.byPath[key] =
		document

	s.revision++

	return nil
}

func (s *DocumentStore) Close(
	uri string,
) (Document, bool) {
	if s == nil {
		return Document{},
			false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pathKey, found :=
		s.byURI[uri]

	if !found {
		return Document{},
			false
	}

	document, found :=
		s.byPath[pathKey]

	delete(
		s.byURI,
		uri,
	)

	delete(
		s.byPath,
		pathKey,
	)

	s.revision++

	return document,
		found
}

func (s *DocumentStore) GetByPath(
	path string,
) (Document, bool) {
	if s == nil {
		return Document{},
			false
	}

	key, err :=
		canonicalPath(
			path,
		)

	if err != nil {
		return Document{},
			false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	document, found :=
		s.byPath[key]

	return document,
		found
}

func (s *DocumentStore) GetByURI(
	uri string,
) (Document, bool) {
	if s == nil {
		return Document{},
			false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	pathKey, found :=
		s.byURI[uri]

	if !found {
		return Document{},
			false
	}

	document, found :=
		s.byPath[pathKey]

	return document,
		found
}

func (s *DocumentStore) Snapshot() DocumentSnapshot {
	if s == nil {
		return DocumentSnapshot{
			byPath: map[string]Document{},

			byURI: map[string]Document{},
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot :=
		DocumentSnapshot{
			Revision: s.revision,

			Documents: make(
				[]Document,
				0,
				len(s.byPath),
			),

			byPath: make(
				map[string]Document,
				len(s.byPath),
			),

			byURI: make(
				map[string]Document,
				len(s.byURI),
			),
		}

	for key, document := range s.byPath {
		snapshot.Documents =
			append(
				snapshot.Documents,
				document,
			)

		snapshot.byPath[key] =
			document
	}

	for uri, pathKey := range s.byURI {
		document, found :=
			s.byPath[pathKey]

		if !found {
			continue
		}

		snapshot.byURI[uri] =
			document
	}

	return snapshot
}

func normalizeDocument(
	document Document,
) (
	Document,
	string,
	error,
) {
	if strings.TrimSpace(
		document.URI,
	) == "" {
		return Document{},
			"",
			fmt.Errorf(
				"document URI cannot be empty",
			)
	}

	if strings.TrimSpace(
		document.Path,
	) == "" {
		return Document{},
			"",
			fmt.Errorf(
				"document path cannot be empty",
			)
	}

	absolutePath, err :=
		filepath.Abs(
			document.Path,
		)

	if err != nil {
		return Document{},
			"",
			err
	}

	document.Path =
		filepath.Clean(
			absolutePath,
		)

	key :=
		canonicalPathFromAbsolute(
			document.Path,
		)

	return document,
		key,
		nil
}

func canonicalPath(
	path string,
) (
	string,
	error,
) {
	if strings.TrimSpace(path) == "" {
		return "",
			fmt.Errorf(
				"path cannot be empty",
			)
	}

	absolutePath, err :=
		filepath.Abs(
			path,
		)

	if err != nil {
		return "",
			err
	}

	return canonicalPathFromAbsolute(
			absolutePath,
		),
		nil
}

func canonicalPathFromAbsolute(
	path string,
) string {
	path =
		filepath.Clean(
			path,
		)

	/*
		Windows paths are case-insensitive under the ordinary filesystems used
		by Seal development. Normalize case so editor and filesystem spellings
		refer to the same document overlay.
	*/
	if runtime.GOOS ==
		"windows" {
		path =
			strings.ToLower(
				path,
			)
	}

	return path
}
