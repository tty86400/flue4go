package flue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
)

// MCPServerOptions configures a minimal streamable-HTTP MCP connection.
type MCPServerOptions struct {
	URL             string
	Headers         map[string]string
	HTTPClient      *http.Client
	ClientName      string
	ClientVersion   string
	ProtocolVersion string
}

// MCPConnection is a set of MCP tools adapted to Flue4Go Tool values.
type MCPConnection struct {
	Name  string
	Tools []Tool
}

// ConnectMCPServer initializes an MCP server and adapts its tools.
//
// 中文说明：MCP 适配层会先 initialize，再 tools/list，把远程 MCP tool
// 转成 Flue4Go Tool。业务密钥应放在 opts.Headers 里，由可信 Go 代码传入。
func ConnectMCPServer(ctx context.Context, name string, opts MCPServerOptions) (MCPConnection, error) {
	if name == "" {
		return MCPConnection{}, errors.New("mcp server name is required")
	}
	if _, err := url.ParseRequestURI(opts.URL); err != nil {
		return MCPConnection{}, fmt.Errorf("invalid mcp url: %w", err)
	}
	client := &mcpHTTPClient{
		url:     opts.URL,
		headers: opts.Headers,
		client:  opts.HTTPClient,
	}
	if client.client == nil {
		client.client = http.DefaultClient
	}
	if opts.ClientName == "" {
		opts.ClientName = "flue4go"
	}
	if opts.ClientVersion == "" {
		opts.ClientVersion = "0.0.0"
	}
	if opts.ProtocolVersion == "" {
		opts.ProtocolVersion = "2025-03-26"
	}
	if _, err := client.call(ctx, "initialize", map[string]any{
		"protocolVersion": opts.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    opts.ClientName,
			"version": opts.ClientVersion,
		},
	}); err != nil {
		return MCPConnection{}, err
	}
	listResult, err := client.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return MCPConnection{}, err
	}
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Title       string         `json:"title"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	raw, _ := json.Marshal(listResult)
	if err := json.Unmarshal(raw, &listed); err != nil {
		return MCPConnection{}, err
	}
	tools := make([]Tool, 0, len(listed.Tools))
	seen := map[string]struct{}{}
	for _, remoteTool := range listed.Tools {
		toolName := "mcp__" + sanitizeMCPName(name) + "__" + sanitizeMCPName(remoteTool.Name)
		if _, ok := seen[toolName]; ok {
			return MCPConnection{}, fmt.Errorf("duplicate mcp tool name %q", toolName)
		}
		seen[toolName] = struct{}{}
		originalName := remoteTool.Name
		description := fmt.Sprintf("MCP tool %q from server %q.", originalName, name)
		if remoteTool.Title != "" && remoteTool.Title != originalName {
			description += " Title: " + remoteTool.Title + "."
		}
		if remoteTool.Description != "" {
			description += " " + remoteTool.Description
		}
		schema := remoteTool.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		tools = append(tools, Tool{
			Name:        toolName,
			Description: description,
			Parameters:  schema,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				result, err := client.call(ctx, "tools/call", map[string]any{
					"name":      originalName,
					"arguments": args,
				})
				if err != nil {
					return "", err
				}
				text, isErr := formatMCPToolResult(result)
				if isErr {
					return "", errors.New(text)
				}
				return text, nil
			},
		})
	}
	return MCPConnection{Name: name, Tools: tools}, nil
}

type mcpHTTPClient struct {
	url     string
	headers map[string]string
	client  *http.Client
	nextID  int64
}

func (c *mcpHTTPClient) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mcp http status %d", resp.StatusCode)
	}
	var rpc struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return nil, err
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result == nil {
		return map[string]any{}, nil
	}
	return rpc.Result, nil
}

func formatMCPToolResult(result map[string]any) (string, bool) {
	isErr, _ := result["isError"].(bool)
	var parts []string
	if structured, ok := result["structuredContent"]; ok {
		encoded, _ := json.MarshalIndent(structured, "", "  ")
		parts = append(parts, "Structured content:\n"+string(encoded))
	}
	if content, ok := result["content"].([]any); ok {
		for _, item := range content {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch obj["type"] {
			case "text":
				if text, ok := obj["text"].(string); ok {
					parts = append(parts, text)
				}
			case "image":
				parts = append(parts, "[Image content]")
			case "audio":
				parts = append(parts, "[Audio content]")
			case "resource", "resource_link":
				encoded, _ := json.Marshal(obj)
				parts = append(parts, string(encoded))
			}
		}
	}
	if len(parts) == 0 {
		encoded, _ := json.MarshalIndent(result, "", "  ")
		parts = append(parts, string(encoded))
	}
	return strings.Join(parts, "\n\n"), isErr
}

var mcpNameRE = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func sanitizeMCPName(value string) string {
	out := strings.Trim(mcpNameRE.ReplaceAllString(value, "_"), "_")
	if out == "" {
		return "unnamed"
	}
	return out
}
