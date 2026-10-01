package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeBackend struct{ initialized string }

func (f *fakeBackend) ServerInfo() Implementation { return Implementation{Name: "t", Version: "1"} }
func (f *fakeBackend) Instructions() string       { return "hi" }
func (f *fakeBackend) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{Name: "a", InputSchema: map[string]any{"type": "object"}}}, nil
}
func (f *fakeBackend) CallTool(_ context.Context, name string, _ json.RawMessage) (*ToolResult, error) {
	if name == "boom" {
		return nil, errors.New("boom")
	}
	return JSONResult(map[string]string{"ok": name}, false), nil
}
func (f *fakeBackend) ListResources(context.Context) ([]Resource, error) { return nil, nil }
func (f *fakeBackend) ListResourceTemplates(context.Context) ([]ResourceTemplate, error) {
	return nil, nil
}
func (f *fakeBackend) ReadResource(context.Context, string) ([]ResourceContents, error) {
	return nil, nil
}
func (f *fakeBackend) ListPrompts(context.Context) ([]Prompt, error) { return nil, nil }
func (f *fakeBackend) GetPrompt(context.Context, string, map[string]string) (string, []PromptMessage, error) {
	return "", nil, nil
}
func (f *fakeBackend) Initialized(_ context.Context, _ InitializeParams, v string) { f.initialized = v }

func req(method, params string) *Request {
	r := &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
	if params != "" {
		r.Params = json.RawMessage(params)
	}
	return r
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	b := &fakeBackend{}
	resp := Dispatch(context.Background(), b, req("initialize", `{"protocolVersion":"2025-06-18","clientInfo":{"name":"c","version":"1"}}`))
	if resp.Error != nil || b.initialized != "2025-06-18" {
		t.Fatalf("got %+v %q", resp.Error, b.initialized)
	}
	Dispatch(context.Background(), b, req("initialize", `{"protocolVersion":"1999-01-01"}`))
	if b.initialized != SupportedVersions[0] {
		t.Fatalf("unknown version should fall back to latest, got %q", b.initialized)
	}
}

func TestNotificationsHaveNoResponse(t *testing.T) {
	r := &Request{JSONRPC: "2.0", Method: "notifications/initialized"}
	if Dispatch(context.Background(), &fakeBackend{}, r) != nil {
		t.Fatal("notification must not produce a response")
	}
}

func TestErrors(t *testing.T) {
	b := &fakeBackend{}
	if r := Dispatch(context.Background(), b, req("nope", "")); r.Error == nil || r.Error.Code != CodeMethodNotFound {
		t.Fatalf("want method not found, got %+v", r)
	}
	if r := Dispatch(context.Background(), b, req("tools/call", `{"name":""}`)); r.Error == nil || r.Error.Code != CodeInvalidParams {
		t.Fatalf("want invalid params, got %+v", r)
	}
	if r := Dispatch(context.Background(), b, req("tools/call", `{"name":"boom"}`)); r.Error == nil || r.Error.Code != CodeInternal {
		t.Fatalf("want internal error, got %+v", r)
	}
	if r := Dispatch(context.Background(), b, &Request{JSONRPC: "1.0", ID: json.RawMessage(`1`), Method: "ping"}); r.Error == nil || r.Error.Code != CodeInvalidRequest {
		t.Fatalf("want invalid request, got %+v", r)
	}
	r := Dispatch(context.Background(), b, req("tools/call", `{"name":"x","arguments":{}}`))
	res := r.Result.(*ToolResult)
	if res.IsError || res.Content[0].Type != "text" || res.StructuredContent == nil {
		t.Fatalf("unexpected tool result %+v", res)
	}
}
