package lsp

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/avestura/hcl-schema/pkg/hclschema"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// Options configures the server.
type Options struct {
	// Loader resolves schema references. A server is long-lived, so it keeps
	// an in-process cache in front of the disk one by default.
	Loader *hclschema.Loader

	// Strict applies draft 2026-09 rules regardless of what a schema pins.
	Strict bool

	// Log receives server-side messages. May be nil.
	Log io.Writer
}

// Server speaks LSP over a pair of streams.
type Server struct {
	conn *conn
	opts Options

	mu        sync.Mutex
	documents map[string]*document
	shutdown  bool
}

type document struct {
	uri     string
	path    string
	text    []byte
	version int

	index  *lineIndex
	file   *hcl.File
	schema *hclschema.Schema
}

// NewServer returns a server reading from r and writing to w.
func NewServer(r io.Reader, w io.Writer, opts Options) *Server {
	if opts.Loader == nil {
		opts.Loader = &hclschema.Loader{
			Cache:                  hclschema.NewMemoryCache(),
			AllowCrossHostRedirect: true,
		}
	}
	return &Server{
		conn:      newConn(r, w),
		opts:      opts,
		documents: map[string]*document{},
	}
}

// Run serves until the stream closes or the client asks the server to exit.
func (s *Server) Run() error {
	for {
		msg, err := s.conn.read()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, errParse):
			_ = s.conn.replyError(nil, codeParseError, err.Error())
			continue
		case err != nil:
			return err
		}
		if done := s.handle(msg); done {
			return nil
		}
	}
}

func (s *Server) handle(msg *Message) (done bool) {
	switch msg.Method {
	case "initialize":
		_ = s.conn.reply(msg.ID, s.capabilities())
	case "initialized":
		// Nothing to do; the client is ready.
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		_ = s.conn.reply(msg.ID, nil)
	case "exit":
		return true
	case "textDocument/didOpen":
		var p DidOpenParams
		if unmarshal(msg.Params, &p) {
			s.open(p.TextDocument.URI, []byte(p.TextDocument.Text), p.TextDocument.Version)
		}
	case "textDocument/didChange":
		var p DidChangeParams
		if unmarshal(msg.Params, &p) && len(p.ContentChanges) > 0 {
			// Full sync: the last change carries the whole document.
			last := p.ContentChanges[len(p.ContentChanges)-1].Text
			s.open(p.TextDocument.URI, []byte(last), 0)
		}
	case "textDocument/didSave":
		var p DidSaveParams
		if unmarshal(msg.Params, &p) && p.Text != nil {
			s.open(p.TextDocument.URI, []byte(*p.Text), 0)
		}
	case "textDocument/didClose":
		var p DidCloseParams
		if unmarshal(msg.Params, &p) {
			s.mu.Lock()
			delete(s.documents, p.TextDocument.URI)
			s.mu.Unlock()
			_ = s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
				URI:         p.TextDocument.URI,
				Diagnostics: []Diagnostic{},
			})
		}
	case "textDocument/completion":
		var p PositionParams
		if unmarshal(msg.Params, &p) {
			_ = s.conn.reply(msg.ID, s.completion(p))
		} else {
			_ = s.conn.reply(msg.ID, []CompletionItem{})
		}
	case "textDocument/hover":
		var p PositionParams
		if unmarshal(msg.Params, &p) {
			_ = s.conn.reply(msg.ID, s.hover(p))
		} else {
			_ = s.conn.reply(msg.ID, nil)
		}
	case "textDocument/definition":
		var p PositionParams
		if unmarshal(msg.Params, &p) {
			_ = s.conn.reply(msg.ID, s.definition(p))
		} else {
			_ = s.conn.reply(msg.ID, nil)
		}
	case "textDocument/documentSymbol":
		var p DocumentSymbolParams
		if unmarshal(msg.Params, &p) {
			_ = s.conn.reply(msg.ID, s.documentSymbol(p))
		} else {
			_ = s.conn.reply(msg.ID, []DocumentSymbol{})
		}
	default:
		if msg.ID != nil {
			_ = s.conn.replyError(msg.ID, codeMethodNotFound, "unsupported method: "+msg.Method)
		}
	}
	return false
}

func (s *Server) capabilities() map[string]any {
	return map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    1, // full
				"save":      map[string]any{"includeText": true},
			},
			"completionProvider": map[string]any{
				"triggerCharacters": []string{".", " ", "\""},
			},
			"hoverProvider":          true,
			"definitionProvider":     true,
			"documentSymbolProvider": true,
		},
		"serverInfo": map[string]any{"name": "hclschema"},
	}
}

// open records the buffer and republishes diagnostics for it. Checking the
// in-memory text is the whole point: the previous integration validated
// whatever was last written to disk.
func (s *Server) open(uri string, text []byte, version int) {
	path := uriToPath(uri)
	doc := &document{
		uri:     uri,
		path:    path,
		text:    text,
		version: version,
		index:   newLineIndex(text),
	}

	res := hclschema.Check(text, path, hclschema.LoadOptions{
		ParseOptions: hclschema.ParseOptions{
			Loader: s.opts.Loader,
			Strict: s.opts.Strict,
		},
		ValidateOptions: hclschema.ValidateOptions{Strict: s.opts.Strict},
	})
	doc.file = res.File
	doc.schema = res.Schema

	s.mu.Lock()
	s.documents[uri] = doc
	s.mu.Unlock()

	out := make([]Diagnostic, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		if d == nil {
			continue
		}
		// Diagnostics anchored in the schema document belong on the schema,
		// not on this buffer; surface them here without a misleading range.
		rng := Range{}
		if d.Subject != nil && sameFile(d.Subject.Filename, path) {
			rng = doc.index.rangeOf(*d.Subject)
		}
		out = append(out, Diagnostic{
			Range:    rng,
			Severity: severityOf(d.Severity),
			Source:   "hclschema",
			Message:  message(d),
		})
	}
	_ = s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: out,
	})
}

func (s *Server) doc(uri string) *document {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.documents[uri]
}

func (s *Server) completion(p PositionParams) []CompletionItem {
	doc := s.doc(p.TextDocument.URI)
	if doc == nil || doc.schema == nil || doc.file == nil {
		return []CompletionItem{}
	}
	cursor := doc.schema.At(doc.file, doc.index.hclPos(p.Position))
	if cursor == nil {
		return []CompletionItem{}
	}
	items := cursor.Completions()
	out := make([]CompletionItem, 0, len(items))
	for i, c := range items {
		item := CompletionItem{
			Label:      c.Label,
			Kind:       completionKind(c.Kind),
			Detail:     c.Detail,
			InsertText: c.InsertText,
			Deprecated: c.Deprecated,
			// Preserve the library's ordering, which puts attributes first.
			SortText: sortKey(i),
		}
		if c.Documentation != "" {
			item.Documentation = &MarkupConten{Kind: "markdown", Value: c.Documentation}
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) hover(p PositionParams) any {
	doc := s.doc(p.TextDocument.URI)
	if doc == nil || doc.schema == nil || doc.file == nil {
		return nil
	}
	cursor := doc.schema.At(doc.file, doc.index.hclPos(p.Position))
	if cursor == nil {
		return nil
	}
	text := cursor.Doc()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return Hover{Contents: MarkupConten{Kind: "markdown", Value: text}}
}

// definition jumps from a use in the instance to its declaration in the schema
// document.
func (s *Server) definition(p PositionParams) any {
	doc := s.doc(p.TextDocument.URI)
	if doc == nil || doc.schema == nil || doc.file == nil {
		return nil
	}
	cursor := doc.schema.At(doc.file, doc.index.hclPos(p.Position))
	if cursor == nil {
		return nil
	}
	rng, ok := cursor.DefinitionRange()
	if !ok || rng.Filename == "" {
		return nil
	}
	// The schema is a different file, so its byte offsets need that file's own
	// line index rather than this document's.
	src, _, diags := s.opts.Loader.Load(rng.Filename, "", "")
	if diags.HasErrors() {
		return nil
	}
	idx := newLineIndex(src)
	return Location{URI: pathToURI(rng.Filename), Range: idx.rangeOf(rng)}
}

func (s *Server) documentSymbol(p DocumentSymbolParams) []DocumentSymbol {
	doc := s.doc(p.TextDocument.URI)
	if doc == nil || doc.file == nil {
		return []DocumentSymbol{}
	}
	body, ok := doc.file.Body.(*hclsyntax.Body)
	if !ok {
		return []DocumentSymbol{}
	}
	return doc.symbols(body)
}

func (d *document) symbols(body *hclsyntax.Body) []DocumentSymbol {
	out := []DocumentSymbol{}
	for name, attr := range body.Attributes {
		out = append(out, DocumentSymbol{
			Name:           name,
			Kind:           symbolField,
			Range:          d.index.rangeOf(attr.SrcRange),
			SelectionRange: d.index.rangeOf(attr.NameRange),
		})
	}
	for _, blk := range body.Blocks {
		name := blk.Type
		if len(blk.Labels) > 0 {
			name += " " + strings.Join(blk.Labels, ".")
		}
		sym := DocumentSymbol{
			Name:           name,
			Kind:           symbolStruct,
			Range:          d.index.rangeOf(blk.Range()),
			SelectionRange: d.index.rangeOf(blk.DefRange()),
		}
		if blk.Body != nil {
			sym.Children = d.symbols(blk.Body)
		}
		out = append(out, sym)
	}
	return out
}

func unmarshal(raw json.RawMessage, target any) bool {
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func severityOf(s hcl.DiagnosticSeverity) int {
	switch s {
	case hcl.DiagError:
		return severityError
	case hcl.DiagWarning:
		return severityWarning
	}
	return severityInfo
}

func message(d *hcl.Diagnostic) string {
	if d.Detail == "" {
		return d.Summary
	}
	if d.Summary == "" {
		return d.Detail
	}
	return d.Summary + ": " + d.Detail
}

func completionKind(k hclschema.CompletionKind) int {
	switch k {
	case hclschema.CompletionBlock:
		return kindStruct
	case hclschema.CompletionValue:
		return kindValue
	case hclschema.CompletionAttribute:
		return kindField
	}
	return kindProperty
}

// sortKey keeps the server's ordering stable in clients that sort by sortText.
func sortKey(i int) string {
	const digits = "0123456789"
	if i < 0 {
		i = 0
	}
	return string([]byte{
		digits[(i/1000)%10], digits[(i/100)%10], digits[(i/10)%10], digits[i%10],
	})
}

func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	return strings.EqualFold(strings.ReplaceAll(a, "\\", "/"), strings.ReplaceAll(b, "\\", "/"))
}
