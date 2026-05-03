package flue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPServerRoutesAgentsAndRejectsNonWebhook(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Handle("hello", Triggers{Webhook: true}, func(ctx context.Context, req RequestContext) (any, error) {
		return map[string]any{"id": req.ID, "echo": req.Payload["name"]}, nil
	})
	registry.Handle("ci", Triggers{}, func(ctx context.Context, req RequestContext) (any, error) {
		return map[string]any{"ok": true}, nil
	})

	server := NewHTTPServer(registry, HTTPServerOptions{})

	req := httptest.NewRequest(http.MethodPost, "/agents/hello/abc", strings.NewReader(`{"name":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Result["echo"] != "world" {
		t.Fatalf("unexpected body: %#v", body)
	}

	req = httptest.NewRequest(http.MethodPost, "/agents/ci/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-webhook status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPServerSupportsWebhookAcceptedAndSSE(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	called := make(chan struct{}, 1)
	registry.Handle("hello", Triggers{Webhook: true}, func(ctx context.Context, req RequestContext) (any, error) {
		called <- struct{}{}
		return map[string]any{"ok": true}, nil
	})
	server := NewHTTPServer(registry, HTTPServerOptions{})

	req := httptest.NewRequest(http.MethodPost, "/agents/hello/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook", "true")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook status = %d body=%s", rec.Code, rec.Body.String())
	}
	<-called

	req = httptest.NewRequest(http.MethodPost, "/agents/hello/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sse status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: result") || !strings.Contains(body, "event: idle") {
		t.Fatalf("unexpected sse body: %s", body)
	}
}
