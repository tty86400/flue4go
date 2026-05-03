package flue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var routeIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Triggers declares how an agent may be invoked.
type Triggers struct {
	Webhook bool   `json:"webhook,omitempty"`
	Cron    string `json:"cron,omitempty"`
}

// RequestContext is passed to registered HTTP agent handlers.
type RequestContext struct {
	ID      string
	Name    string
	Payload map[string]any
	Env     map[string]string
}

// Handler is a registered Go-native agent endpoint.
type Handler func(context.Context, RequestContext) (any, error)

type registeredAgent struct {
	Name     string   `json:"name"`
	Triggers Triggers `json:"triggers"`
	Handler  Handler  `json:"-"`
}

// Registry stores agent handlers and public trigger metadata.
type Registry struct {
	agents map[string]registeredAgent
}

// NewRegistry creates an empty agent registry.
func NewRegistry() *Registry {
	return &Registry{agents: map[string]registeredAgent{}}
}

// Handle registers or replaces an agent handler.
func (r *Registry) Handle(name string, triggers Triggers, handler Handler) {
	if r.agents == nil {
		r.agents = map[string]registeredAgent{}
	}
	r.agents[name] = registeredAgent{Name: name, Triggers: triggers, Handler: handler}
}

// Manifest returns agent names and trigger metadata.
func (r *Registry) Manifest() []registeredAgent {
	out := make([]registeredAgent, 0, len(r.agents))
	for _, agent := range r.agents {
		out = append(out, registeredAgent{Name: agent.Name, Triggers: agent.Triggers})
	}
	return out
}

// HTTPServerOptions controls routing behavior.
type HTTPServerOptions struct {
	AllowNonWebhook bool
	Env             map[string]string
}

// NewHTTPServer exposes /health, /agents, and /agents/{name}/{id}.
//
// 中文说明：Go 版本不动态加载 agent 文件，而是通过 Registry.Handle()
// 显式注册。HTTP 层只负责路由、校验、payload 解析、webhook/SSE 包装。
func NewHTTPServer(registry *Registry, opts HTTPServerOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agents" {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": registry.Manifest()})
	})
	mux.HandleFunc("/agents/", func(w http.ResponseWriter, r *http.Request) {
		name, id, ok := parseAgentPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "agent routes require POST")
			return
		}
		if !routeIDRE.MatchString(name) || !routeIDRE.MatchString(id) {
			writeError(w, http.StatusBadRequest, "invalid_route", "agent name and id must be simple route identifiers")
			return
		}
		agent, ok := registry.agents[name]
		if !ok || (!agent.Triggers.Webhook && !opts.AllowNonWebhook) {
			writeError(w, http.StatusNotFound, "not_found", "agent not found")
			return
		}
		payload, err := parsePayload(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if r.Header.Get("X-Webhook") == "true" {
			// 中文导读：webhook 模式下立即返回 202，handler 在后台执行。
			// 适合 GitHub webhook、CI 事件这类不希望 HTTP 长时间挂起的场景。
			requestID := randomRequestID()
			go func() {
				_, _ = agent.Handler(context.Background(), RequestContext{
					ID:      id,
					Name:    name,
					Payload: payload,
					Env:     opts.Env,
				})
			}()
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "requestId": requestID})
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			// 中文导读：SSE 模式保留和上游 Flue 类似的调用形态。目前发送
			// result/idle/error 基础事件，后续可接入模型 token/tool 事件。
			writeSSE(w, r, agent, RequestContext{ID: id, Name: name, Payload: payload, Env: opts.Env})
			return
		}
		result, err := agent.Handler(r.Context(), RequestContext{
			ID:      id,
			Name:    name,
			Payload: payload,
			Env:     opts.Env,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "handler_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
	})
	return mux
}

func parseAgentPath(p string) (string, string, bool) {
	rest := strings.TrimPrefix(p, "/agents/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parsePayload(r *http.Request) (map[string]any, error) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "application/json") {
		return nil, fmt.Errorf("Content-Type must be application/json")
	}
	defer r.Body.Close()
	var payload map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		if errors.Is(err, http.ErrBodyNotAllowed) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeSSE(w http.ResponseWriter, r *http.Request, agent registeredAgent, req RequestContext) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	result, err := agent.Handler(r.Context(), req)
	if err != nil {
		writeSSEEvent(w, "error", map[string]any{"error": map[string]string{"code": "handler_error", "message": err.Error()}})
		writeSSEEvent(w, "idle", map[string]any{"type": "idle"})
		return
	}
	writeSSEEvent(w, "result", map[string]any{"type": "result", "data": result})
	writeSSEEvent(w, "idle", map[string]any{"type": "idle"})
}

func writeSSEEvent(w http.ResponseWriter, event string, value any) {
	encoded, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func randomRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "request"
	}
	return hex.EncodeToString(b[:])
}
