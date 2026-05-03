package flue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectMCPServerExposesTools(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]any{"protocolVersion": "2025-03-26"},
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{"tools": []map[string]any{{
					"name":        "echo",
					"description": "echo input",
					"inputSchema": map[string]any{"type": "object"},
				}}},
			})
		case "tools/call":
			if req.Params["name"] != "echo" {
				t.Fatalf("tool name = %v", req.Params["name"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "hello mcp"}}},
			})
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	}))
	defer server.Close()

	conn, err := ConnectMCPServer(context.Background(), "test-server", MCPServerOptions{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.Tools) != 1 {
		t.Fatalf("tool count = %d", len(conn.Tools))
	}
	if conn.Tools[0].Name != "mcp__test-server__echo" {
		t.Fatalf("tool name = %q", conn.Tools[0].Name)
	}
	got, err := conn.Tools[0].Execute(context.Background(), map[string]any{"message": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello mcp" {
		t.Fatalf("tool result = %q", got)
	}
}
