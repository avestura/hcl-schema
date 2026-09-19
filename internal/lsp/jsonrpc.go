// Package lsp implements a Language Server Protocol server for HCL documents
// validated by hcl-schema.
//
// It exists so that the editor integration no longer has to shell out to the
// CLI on every keystroke and validate whatever happens to be on disk: the
// server holds the unsaved buffer and checks that instead.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Message is a JSON-RPC 2.0 envelope. Requests, responses and notifications
// share one struct because the distinguishing fields are all optional.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError is a JSON-RPC error object.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes used by this server.
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// conn frames JSON-RPC messages over a stream using LSP's Content-Length
// headers.
type conn struct {
	r *bufio.Reader
	w io.Writer

	mu sync.Mutex
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReader(r), w: w}
}

// read returns the next message, or io.EOF when the stream closes.
func (c *conn) read() (*Message, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("message without Content-Length")
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(buf, &msg); err != nil {
		return nil, fmt.Errorf("%w: %s", errParse, err)
	}
	return &msg, nil
}

var errParse = fmt.Errorf("parse error")

func (c *conn) write(msg *Message) error {
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

func (c *conn) reply(id json.RawMessage, result any) error {
	if id == nil {
		return nil
	}
	return c.write(&Message{ID: id, Result: result})
}

func (c *conn) replyError(id json.RawMessage, code int, message string) error {
	if id == nil {
		return nil
	}
	return c.write(&Message{ID: id, Error: &ResponseError{Code: code, Message: message}})
}

func (c *conn) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(&Message{Method: method, Params: raw})
}
