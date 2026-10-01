// Package mcp holds the Model Context Protocol wire types (JSON-RPC 2.0 over
// Streamable HTTP) and a transport-independent dispatcher. Business logic —
// which tools exist, who may call them — is supplied by the server package
// through the Backend interface.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// SupportedVersions are protocol revisions this server speaks, newest first.
var SupportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r *Request) IsNotification() bool { return len(r.ID) == 0 || string(r.ID) == "null" }

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Message) }

type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ClientInfo      Implementation `json:"clientInfo"`
	Capabilities    map[string]any `json:"capabilities"`
}

type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint bool   `json:"destructiveHint"`
	IdempotentHint  bool   `json:"idempotentHint"`
	OpenWorldHint   bool   `json:"openWorldHint"`
}

type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema map[string]any  `json:"inputSchema"`
	Annotations ToolAnnotations `json:"annotations"`
}

type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type ToolResult struct {
	Content           []Content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

// JSONResult renders v both as text (for clients that only read content) and
// as structuredContent.
func JSONResult(v any, isError bool) *ToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &ToolResult{Content: []Content{{Type: "text", Text: string(b)}}, StructuredContent: v, IsError: isError}
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// Backend is implemented by the gateway for one authenticated caller.
type Backend interface {
	ServerInfo() Implementation
	Instructions() string
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error)
	ListResources(ctx context.Context) ([]Resource, error)
	ListResourceTemplates(ctx context.Context) ([]ResourceTemplate, error)
	ReadResource(ctx context.Context, uri string) ([]ResourceContents, error)
	ListPrompts(ctx context.Context) ([]Prompt, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (string, []PromptMessage, error)
	Initialized(ctx context.Context, p InitializeParams, negotiated string)
}

func NegotiateVersion(requested string) string {
	for _, v := range SupportedVersions {
		if v == requested {
			return v
		}
	}
	return SupportedVersions[0]
}

// Dispatch handles one JSON-RPC request. It returns nil for notifications.
func Dispatch(ctx context.Context, b Backend, req *Request) *Response {
	if req.JSONRPC != "2.0" || req.Method == "" {
		return errorResponse(req.ID, CodeInvalidRequest, "invalid JSON-RPC request", nil)
	}
	result, rpcErr := route(ctx, b, req)
	if req.IsNotification() {
		return nil
	}
	if rpcErr != nil {
		return &Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	if result == nil {
		result = struct{}{}
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func errorResponse(id json.RawMessage, code int, msg string, data any) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg, Data: data}}
}

func ParseErrorResponse(msg string) *Response {
	return errorResponse(nil, CodeParseError, msg, nil)
}

func decode(raw json.RawMessage, v any) *RPCError {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	return nil
}

func internal(err error) *RPCError {
	if rpc, ok := err.(*RPCError); ok {
		return rpc
	}
	return &RPCError{Code: CodeInternal, Message: err.Error()}
}

func route(ctx context.Context, b Backend, req *Request) (any, *RPCError) {
	switch req.Method {
	case "initialize":
		var p InitializeParams
		if e := decode(req.Params, &p); e != nil {
			return nil, e
		}
		v := NegotiateVersion(p.ProtocolVersion)
		b.Initialized(ctx, p, v)
		return map[string]any{
			"protocolVersion": v,
			"serverInfo":      b.ServerInfo(),
			"instructions":    b.Instructions(),
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false, "subscribe": false},
				"prompts":   map[string]any{"listChanged": false},
				"logging":   map[string]any{},
			},
		}, nil
	case "notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed":
		return nil, nil
	case "ping":
		return struct{}{}, nil
	case "logging/setLevel":
		return struct{}{}, nil
	case "tools/list":
		tools, err := b.ListTools(ctx)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if e := decode(req.Params, &p); e != nil {
			return nil, e
		}
		if p.Name == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "tool name is required"}
		}
		res, err := b.CallTool(ctx, p.Name, p.Arguments)
		if err != nil {
			return nil, internal(err)
		}
		return res, nil
	case "resources/list":
		rs, err := b.ListResources(ctx)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]any{"resources": rs}, nil
	case "resources/templates/list":
		ts, err := b.ListResourceTemplates(ctx)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]any{"resourceTemplates": ts}, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if e := decode(req.Params, &p); e != nil {
			return nil, e
		}
		cs, err := b.ReadResource(ctx, p.URI)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]any{"contents": cs}, nil
	case "prompts/list":
		ps, err := b.ListPrompts(ctx)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]any{"prompts": ps}, nil
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if e := decode(req.Params, &p); e != nil {
			return nil, e
		}
		desc, msgs, err := b.GetPrompt(ctx, p.Name, p.Arguments)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]any{"description": desc, "messages": msgs}, nil
	}
	return nil, &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
}
