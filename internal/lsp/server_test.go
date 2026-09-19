package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/avestura/hcl-schema/pkg/hclschema"
)

// client drives the server over a pair of pipes, the same way an editor would.
type client struct {
	t      *testing.T
	toSrv  *io.PipeWriter
	fromSr *bufio.Reader
	nextID int
	done   chan error
}

func newClient(t *testing.T) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	srv := NewServer(inR, outW, Options{
		Loader: &hclschema.Loader{
			Cache:   hclschema.NewMemoryCache(),
			Offline: true,
		},
	})
	done := make(chan error, 1)
	go func() {
		err := srv.Run()
		outW.Close()
		done <- err
	}()

	return &client{t: t, toSrv: inW, fromSr: bufio.NewReader(outR), done: done}
}

func (c *client) send(method string, params any, withID bool) json.RawMessage {
	c.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	var id json.RawMessage
	if withID {
		c.nextID++
		id = json.RawMessage(strconv.Itoa(c.nextID))
		msg["id"] = c.nextID
	}
	body, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c.toSrv, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatal(err)
	}
	return id
}

func (c *client) read() *Message {
	c.t.Helper()
	length := -1
	for {
		line, err := c.fromSr.ReadString('\n')
		if err != nil {
			c.t.Fatalf("reading header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				c.t.Fatal(err)
			}
		}
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.fromSr, buf); err != nil {
		c.t.Fatalf("reading body: %v", err)
	}
	var msg Message
	if err := json.Unmarshal(buf, &msg); err != nil {
		c.t.Fatalf("decoding %s: %v", buf, err)
	}
	return &msg
}

// readUntil skips notifications until a message with the given method arrives,
// or, when method is empty, until a response carrying a result arrives.
func (c *client) readUntil(method string) *Message {
	c.t.Helper()
	for i := 0; i < 20; i++ {
		msg := c.read()
		if method == "" && msg.Method == "" {
			return msg
		}
		if method != "" && msg.Method == method {
			return msg
		}
	}
	c.t.Fatalf("no %q message arrived", method)
	return nil
}

func (c *client) close() {
	c.send("exit", nil, false)
	c.toSrv.Close()
	<-c.done
}

func resultAs(t *testing.T, msg *Message, target any) {
	t.Helper()
	raw, err := json.Marshal(msg.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decoding result %s: %v", raw, err)
	}
}

func testdataURI(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "pkg", "hclschema", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return pathToURI(abs)
}

func TestInitializeAdvertisesCapabilities(t *testing.T) {
	c := newClient(t)
	defer c.close()

	c.send("initialize", map[string]any{}, true)
	msg := c.readUntil("")

	var res struct {
		Capabilities struct {
			HoverProvider          bool `json:"hoverProvider"`
			DefinitionProvider     bool `json:"definitionProvider"`
			DocumentSymbolProvider bool `json:"documentSymbolProvider"`
			CompletionProvider     struct {
				TriggerCharacters []string `json:"triggerCharacters"`
			} `json:"completionProvider"`
		} `json:"capabilities"`
	}
	resultAs(t, msg, &res)

	if !res.Capabilities.HoverProvider || !res.Capabilities.DefinitionProvider ||
		!res.Capabilities.DocumentSymbolProvider {
		t.Fatalf("missing capabilities: %+v", res.Capabilities)
	}
	if len(res.Capabilities.CompletionProvider.TriggerCharacters) == 0 {
		t.Fatal("no completion trigger characters advertised")
	}
}

// The buffer is what gets validated, not whatever is on disk. This is the
// behaviour the shell-out integration could not provide.
func TestDiagnosticsComeFromTheBuffer(t *testing.T) {
	c := newClient(t)
	defer c.close()

	c.send("initialize", map[string]any{}, true)
	c.readUntil("")

	uri := testdataURI(t, "typed_ok.hcl")
	c.send("textDocument/didOpen", DidOpenParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: "hcl",
			Version:    1,
			// On disk this file is valid; the buffer is not.
			Text: "__schema = \"typed.schema.hcl\"\nname = \"Not Valid\"\n",
		},
	}, false)

	msg := c.readUntil("textDocument/publishDiagnostics")
	var p PublishDiagnosticsParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.URI != uri {
		t.Fatalf("diagnostics published for %q", p.URI)
	}
	if len(p.Diagnostics) == 0 {
		t.Fatal("expected diagnostics for the unsaved buffer")
	}
	found := false
	for _, d := range p.Diagnostics {
		if strings.Contains(d.Message, "pattern") && d.Severity == severityError {
			found = true
			if d.Range.Start.Line != 1 {
				t.Fatalf("diagnostic on line %d, want 1", d.Range.Start.Line)
			}
		}
	}
	if !found {
		t.Fatalf("no pattern error among %+v", p.Diagnostics)
	}
}

func TestDiagnosticsClearOnFix(t *testing.T) {
	c := newClient(t)
	defer c.close()
	c.send("initialize", map[string]any{}, true)
	c.readUntil("")

	uri := testdataURI(t, "typed_ok.hcl")
	c.send("textDocument/didOpen", DidOpenParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "__schema = \"typed.schema.hcl\"\nname = \"BAD\"\n"},
	}, false)
	first := c.readUntil("textDocument/publishDiagnostics")
	var p PublishDiagnosticsParams
	_ = json.Unmarshal(first.Params, &p)
	if len(p.Diagnostics) == 0 {
		t.Fatal("expected an initial diagnostic")
	}

	c.send("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri},
		"contentChanges": []map[string]any{{"text": "__schema = \"typed.schema.hcl\"\nname = \"ok\"\n"}},
	}, false)
	second := c.readUntil("textDocument/publishDiagnostics")
	_ = json.Unmarshal(second.Params, &p)
	if len(p.Diagnostics) != 0 {
		t.Fatalf("diagnostics should clear once fixed, got %+v", p.Diagnostics)
	}
}

func TestCompletionInsideBlock(t *testing.T) {
	c := newClient(t)
	defer c.close()
	c.send("initialize", map[string]any{}, true)
	c.readUntil("")

	uri := testdataURI(t, "cardinality_ok.hcl")
	text := "__schema = \"cardinality.schema.hcl\"\nlistener \"http\" {\n  \n}\n"
	c.send("textDocument/didOpen", DidOpenParams{
		TextDocument: TextDocumentItem{URI: uri, Text: text},
	}, false)
	c.readUntil("textDocument/publishDiagnostics")

	c.send("textDocument/completion", PositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 2, Character: 2},
	}, true)
	msg := c.readUntil("")

	var items []CompletionItem
	resultAs(t, msg, &items)
	if len(items) != 1 || items[0].Label != "port" {
		t.Fatalf("expected only port, got %+v", items)
	}
	if items[0].Kind != kindField {
		t.Fatalf("port should be a field completion, got kind %d", items[0].Kind)
	}
}

func TestHoverShowsSchemaDocumentation(t *testing.T) {
	c := newClient(t)
	defer c.close()
	c.send("initialize", map[string]any{}, true)
	c.readUntil("")

	uri := testdataURI(t, "typed_ok.hcl")
	c.send("textDocument/didOpen", DidOpenParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "__schema = \"typed.schema.hcl\"\nport = 80\n"},
	}, false)
	c.readUntil("textDocument/publishDiagnostics")

	c.send("textDocument/hover", PositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 1, Character: 1},
	}, true)
	msg := c.readUntil("")

	var h Hover
	resultAs(t, msg, &h)
	if !strings.Contains(h.Contents.Value, "port") || !strings.Contains(h.Contents.Value, "number") {
		t.Fatalf("unexpected hover: %q", h.Contents.Value)
	}
}

func TestDefinitionJumpsIntoTheSchema(t *testing.T) {
	c := newClient(t)
	defer c.close()
	c.send("initialize", map[string]any{}, true)
	c.readUntil("")

	uri := testdataURI(t, "typed_ok.hcl")
	c.send("textDocument/didOpen", DidOpenParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "__schema = \"typed.schema.hcl\"\nport = 80\n"},
	}, false)
	c.readUntil("textDocument/publishDiagnostics")

	c.send("textDocument/definition", PositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 1, Character: 1},
	}, true)
	msg := c.readUntil("")

	var loc Location
	resultAs(t, msg, &loc)
	if !strings.HasSuffix(loc.URI, "typed.schema.hcl") {
		t.Fatalf("definition points at %q", loc.URI)
	}
	if loc.Range.Start.Line == 0 && loc.Range.End.Line == 0 {
		t.Fatalf("definition range looks empty: %+v", loc.Range)
	}
}

func TestDocumentSymbolOutline(t *testing.T) {
	c := newClient(t)
	defer c.close()
	c.send("initialize", map[string]any{}, true)
	c.readUntil("")

	uri := testdataURI(t, "cardinality_ok.hcl")
	c.send("textDocument/didOpen", DidOpenParams{
		TextDocument: TextDocumentItem{
			URI:  uri,
			Text: "__schema = \"cardinality.schema.hcl\"\nlistener \"http\" {\n  port = 80\n}\n",
		},
	}, false)
	c.readUntil("textDocument/publishDiagnostics")

	c.send("textDocument/documentSymbol", DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, true)
	msg := c.readUntil("")

	var syms []DocumentSymbol
	resultAs(t, msg, &syms)

	var listener *DocumentSymbol
	for i := range syms {
		if strings.HasPrefix(syms[i].Name, "listener") {
			listener = &syms[i]
		}
	}
	if listener == nil {
		t.Fatalf("no listener symbol in %+v", syms)
	}
	if len(listener.Children) != 1 || listener.Children[0].Name != "port" {
		t.Fatalf("unexpected children: %+v", listener.Children)
	}
}

func TestUnknownMethodGetsAnError(t *testing.T) {
	c := newClient(t)
	defer c.close()
	c.send("textDocument/nonsense", map[string]any{}, true)
	msg := c.readUntil("")
	if msg.Error == nil || msg.Error.Code != codeMethodNotFound {
		t.Fatalf("expected a method-not-found error, got %+v", msg)
	}
}

// --- Position mapping ---------------------------------------------------------

func TestLineIndexRoundTrip(t *testing.T) {
	text := []byte("a = 1\nbb = \"two\"\n\nccc = 3\n")
	li := newLineIndex(text)
	for _, pos := range []Position{
		{Line: 0, Character: 0},
		{Line: 1, Character: 3},
		{Line: 3, Character: 5},
	} {
		off := li.offset(pos)
		got := li.position(off)
		if got != pos {
			t.Fatalf("round trip %+v -> %d -> %+v", pos, off, got)
		}
	}
}

// HCL counts characters while LSP counts UTF-16 units, so anything outside the
// basic multilingual plane would shift ranges without an explicit conversion.
func TestLineIndexCountsUTF16Units(t *testing.T) {
	text := []byte("a = \"\U0001F600\"\nb = 2\n")
	li := newLineIndex(text)

	emojiStart := strings.Index(string(text), "\U0001F600")
	pos := li.position(emojiStart + 4) // just past the 4-byte rune
	if pos.Line != 0 {
		t.Fatalf("line = %d, want 0", pos.Line)
	}
	// 'a', ' ', '=', ' ', '"' is 5 units, and the emoji is a surrogate pair.
	if pos.Character != 7 {
		t.Fatalf("character = %d, want 7", pos.Character)
	}
}

func TestURIRoundTrip(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("testdata", "x.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := uriToPath(pathToURI(abs)); got != abs {
		t.Fatalf("round trip: %q -> %q", abs, got)
	}
}
