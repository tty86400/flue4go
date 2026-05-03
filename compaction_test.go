package flue

import (
	"context"
	"testing"
)

func TestSessionCompactsHistoryWhenMessageLimitExceeded(t *testing.T) {
	t.Parallel()

	model := ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
		return ModelResponse{Content: "assistant response"}, nil
	})
	compactor := CompactorFunc(func(ctx context.Context, messages []Message) (string, error) {
		if len(messages) == 0 {
			t.Fatal("expected messages to compact")
		}
		return "summary of older turns", nil
	})
	store := NewMemoryStore()
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:         "agent",
		Model:      model,
		ModelName:  "test/model",
		Env:        NewMemoryEnv(),
		Store:      store,
		Compactor:  compactor,
		Compaction: CompactionConfig{MaxMessages: 3, KeepRecent: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := session.Prompt(context.Background(), "turn"); err != nil {
			t.Fatal(err)
		}
	}
	data, ok := store.Load("agent/s1")
	if !ok {
		t.Fatal("session missing")
	}
	if len(data.Messages) > 3 {
		t.Fatalf("history was not compacted: %d messages", len(data.Messages))
	}
	if data.Messages[0].Role != MessageRoleSystem || data.Messages[0].Name != "compaction" {
		t.Fatalf("first message is not compaction summary: %+v", data.Messages[0])
	}
	if !contains(data.Messages[0].Content, "summary of older turns") {
		t.Fatalf("summary missing: %q", data.Messages[0].Content)
	}
}
