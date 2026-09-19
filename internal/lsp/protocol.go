package lsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2"
)

// Position is an LSP position: zero-based, with Character counted in UTF-16
// code units.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is an LSP range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range within a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic is an LSP diagnostic.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// LSP diagnostic severities.
const (
	severityError   = 1
	severityWarning = 2
	severityInfo    = 3
)

// PublishDiagnosticsParams is the payload of textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// TextDocumentIdentifier names a document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem is a document with its contents.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// DidOpenParams is the payload of textDocument/didOpen.
type DidOpenParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeParams is the payload of textDocument/didChange. Only full-document
// sync is advertised, so each change carries the whole text.
type DidChangeParams struct {
	TextDocument   TextDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

// DidCloseParams is the payload of textDocument/didClose.
type DidCloseParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidSaveParams is the payload of textDocument/didSave.
type DidSaveParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

// PositionParams is the shared payload of the position-based requests.
type PositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DocumentSymbolParams is the payload of textDocument/documentSymbol.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// CompletionItem is one completion suggestion.
type CompletionItem struct {
	Label         string        `json:"label"`
	Kind          int           `json:"kind"`
	Detail        string        `json:"detail,omitempty"`
	Documentation *MarkupConten `json:"documentation,omitempty"`
	InsertText    string        `json:"insertText,omitempty"`
	SortText      string        `json:"sortText,omitempty"`
	Deprecated    bool          `json:"deprecated,omitempty"`
}

// MarkupConten carries Markdown for hover and completion documentation.
type MarkupConten struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// LSP completion item kinds.
const (
	kindField    = 5
	kindStruct   = 22
	kindValue    = 12
	kindProperty = 10
)

// Hover is the reply to textDocument/hover.
type Hover struct {
	Contents MarkupConten `json:"contents"`
	Range    *Range       `json:"range,omitempty"`
}

// DocumentSymbol is one entry in the outline.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// LSP symbol kinds.
const (
	symbolField  = 8
	symbolStruct = 23
)

// uriToPath converts a file:// URI to a local path, handling the Windows
// drive-letter form that would otherwise keep a leading slash.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
		p = filepath.FromSlash(p)
	}
	if decoded, err := url.PathUnescape(p); err == nil {
		p = decoded
	}
	return p
}

// pathToURI converts a local path to a file:// URI.
func pathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// lineIndex maps between byte offsets and LSP positions for one document.
//
// HCL reports columns in characters while LSP counts UTF-16 code units, so a
// document containing anything outside the basic multilingual plane would have
// its diagnostics drift without this conversion.
type lineIndex struct {
	text       []byte
	lineStarts []int
}

func newLineIndex(text []byte) *lineIndex {
	starts := []int{0}
	for i, b := range text {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{text: text, lineStarts: starts}
}

// position converts a byte offset to an LSP position.
func (li *lineIndex) position(byteOff int) Position {
	if byteOff < 0 {
		byteOff = 0
	}
	if byteOff > len(li.text) {
		byteOff = len(li.text)
	}
	// Binary search would be faster, but documents here are small and a linear
	// scan keeps the mapping obvious.
	line := 0
	for i, start := range li.lineStarts {
		if start > byteOff {
			break
		}
		line = i
	}
	start := li.lineStarts[line]
	return Position{Line: line, Character: utf16Len(li.text[start:byteOff])}
}

// offset converts an LSP position to a byte offset.
func (li *lineIndex) offset(pos Position) int {
	if pos.Line < 0 {
		return 0
	}
	if pos.Line >= len(li.lineStarts) {
		return len(li.text)
	}
	start := li.lineStarts[pos.Line]
	end := len(li.text)
	if pos.Line+1 < len(li.lineStarts) {
		end = li.lineStarts[pos.Line+1]
	}
	line := li.text[start:end]

	units := 0
	for i := 0; i < len(line); {
		if units >= pos.Character {
			return start + i
		}
		r, size := utf8.DecodeRune(line[i:])
		units += utf16.RuneLen(r)
		if units < 0 {
			units++
		}
		i += size
	}
	return end
}

// hclPos converts an LSP position to an hcl.Pos. Line and Column are one-based
// in HCL; Column is filled from the byte offset within the line, which is what
// HCL's position-lookup helpers compare against.
func (li *lineIndex) hclPos(pos Position) hcl.Pos {
	off := li.offset(pos)
	line := 0
	for i, start := range li.lineStarts {
		if start > off {
			break
		}
		line = i
	}
	start := li.lineStarts[line]
	col := utf8.RuneCount(li.text[start:off]) + 1
	return hcl.Pos{Line: line + 1, Column: col, Byte: off}
}

// rangeOf converts an HCL range to an LSP range.
func (li *lineIndex) rangeOf(r hcl.Range) Range {
	return Range{
		Start: li.position(r.Start.Byte),
		End:   li.position(r.End.Byte),
	}
}

func utf16Len(b []byte) int {
	n := 0
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		n += utf16.RuneLen(r)
		if r == utf8.RuneError && size == 1 {
			n++
		}
		i += size
	}
	return n
}
