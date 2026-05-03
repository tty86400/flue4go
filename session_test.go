package flue

import (
	"context"
	"testing"
)

func TestSessionRunsToolLoopAndPersistsHistory(t *testing.T) {
	t.Parallel()

	env := NewMemoryEnv()
	if err := env.WriteFile(context.Background(), "notes.txt", []byte("hello from flue4go")); err != nil {
		t.Fatal(err)
	}

	calls := 0
	model := ModelFunc(func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		calls++
		switch calls {
		case 1:
			if len(req.Tools) == 0 {
				t.Fatal("expected built-in tools")
			}
			return ModelResponse{ToolCalls: []ToolCall{{
				ID:        "call-read",
				Name:      "read",
				Arguments: map[string]any{"path": "notes.txt"},
			}}}, nil
		default:
			if !contains(lastMessage(req.Messages).Content, "hello from flue4go") {
				t.Fatalf("tool result not passed back to model: %#v", req.Messages)
			}
			return ModelResponse{Content: "read complete"}, nil
		}
	})

	store := NewMemoryStore()
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:        "agent",
		Model:     model,
		ModelName: "test/model",
		Env:       env,
		Store:     store,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := session.Prompt(context.Background(), "read notes")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "read complete" {
		t.Fatalf("response text = %q", resp.Text)
	}
	data, ok := store.Load("agent/s1")
	if !ok {
		t.Fatal("session was not persisted")
	}
	if len(data.Messages) < 3 {
		t.Fatalf("expected user, tool and assistant history; got %d", len(data.Messages))
	}
}

func lastMessage(messages []Message) Message {
	return messages[len(messages)-1]
}
