package flue

import (
	"context"
	"testing"
)

type translationResult struct {
	Translation string `json:"translation"`
	Confidence  string `json:"confidence"`
}

func TestPromptIntoExtractsDelimitedJSON(t *testing.T) {
	t.Parallel()

	model := ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
		return ModelResponse{Content: "---RESULT_START---\n{\"translation\":\"bonjour\",\"confidence\":\"high\"}\n---RESULT_END---"}, nil
	})
	agent, err := NewAgent(context.Background(), AgentConfig{Model: model, ModelName: "test/model", Env: NewMemoryEnv()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}

	var out translationResult
	if err := session.PromptInto(context.Background(), "translate", &out); err != nil {
		t.Fatal(err)
	}
	if out.Translation != "bonjour" || out.Confidence != "high" {
		t.Fatalf("unexpected result: %+v", out)
	}
}
