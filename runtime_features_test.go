package flue

import (
	"context"
	"errors"
	"testing"
)

func TestGuardrailsValidateInputOutputAndToolCalls(t *testing.T) {
	t.Parallel()

	blocked := GuardrailFunc(func(_ context.Context, req GuardrailRequest) (GuardrailResult, error) {
		switch req.Stage {
		case GuardrailStageInput:
			if contains(req.Content, "secret") {
				return GuardrailResult{Allowed: false, Reason: "input contains secret"}, nil
			}
		case GuardrailStageOutput:
			if contains(req.Content, "unsafe") {
				return GuardrailResult{Allowed: false, Reason: "unsafe output"}, nil
			}
		case GuardrailStageTool:
			if req.ToolCall != nil && req.ToolCall.Name == "danger" {
				return GuardrailResult{Allowed: false, Reason: "dangerous tool"}, nil
			}
		}
		return GuardrailResult{Allowed: true}, nil
	})

	model := ModelFunc(func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		if contains(lastMessage(req.Messages).Content, "use danger") {
			return ModelResponse{ToolCalls: []ToolCall{{ID: "call-danger", Name: "danger"}}}, nil
		}
		return ModelResponse{Content: "safe"}, nil
	})
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:         "guarded",
		Model:      model,
		ModelName:  "test/model",
		Env:        NewMemoryEnv(),
		Guardrails: []Guardrail{blocked},
		Tools: []Tool{{
			Name:        "danger",
			Description: "dangerous operation",
			Execute: func(context.Context, map[string]any) (string, error) {
				return "should not run", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.Prompt(context.Background(), "contains secret"); !errors.Is(err, ErrGuardrailBlocked) {
		t.Fatalf("input guardrail error = %v", err)
	}
	if _, err := session.Prompt(context.Background(), "use danger"); !errors.Is(err, ErrGuardrailBlocked) {
		t.Fatalf("tool guardrail error = %v", err)
	}

	unsafeModel := ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
		return ModelResponse{Content: "unsafe"}, nil
	})
	outputAgent, err := NewAgent(context.Background(), AgentConfig{
		ID:         "output",
		Model:      unsafeModel,
		ModelName:  "test/model",
		Env:        NewMemoryEnv(),
		Guardrails: []Guardrail{blocked},
	})
	if err != nil {
		t.Fatal(err)
	}
	outputSession, err := outputAgent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outputSession.Prompt(context.Background(), "hello"); !errors.Is(err, ErrGuardrailBlocked) {
		t.Fatalf("output guardrail error = %v", err)
	}
}

func TestTracerReceivesRunModelToolAndCheckpointEvents(t *testing.T) {
	t.Parallel()

	var events []TraceEvent
	tracer := TracerFunc(func(_ context.Context, event TraceEvent) {
		events = append(events, event)
	})
	calls := 0
	model := ModelFunc(func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		calls++
		if calls == 1 {
			return ModelResponse{ToolCalls: []ToolCall{{ID: "call-ping", Name: "ping"}}}, nil
		}
		return ModelResponse{Content: "done"}, nil
	})
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:        "trace",
		Model:     model,
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Tracer:    tracer,
		Tools: []Tool{{
			Name:        "ping",
			Description: "ping",
			Execute: func(context.Context, map[string]any) (string, error) {
				return "pong", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Prompt(context.Background(), "run"); err != nil {
		t.Fatal(err)
	}

	for _, want := range []TraceEventType{
		TraceEventRunStart,
		TraceEventCheckpoint,
		TraceEventModelStart,
		TraceEventToolStart,
		TraceEventToolEnd,
		TraceEventRunEnd,
	} {
		if !hasTraceEvent(events, want) {
			t.Fatalf("missing trace event %s in %#v", want, events)
		}
	}
}

func TestApprovalPausePersistsPendingToolAndResumeExecutesIt(t *testing.T) {
	t.Parallel()

	ran := false
	calls := 0
	model := ModelFunc(func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		calls++
		if calls == 1 {
			return ModelResponse{ToolCalls: []ToolCall{{
				ID:        "call-delete",
				Name:      "delete_file",
				Arguments: map[string]any{"path": "notes.txt"},
			}}}, nil
		}
		if !contains(lastMessage(req.Messages).Content, "approved") {
			t.Fatalf("resume did not pass approved tool result back: %#v", req.Messages)
		}
		return ModelResponse{Content: "deleted"}, nil
	})
	store := NewMemoryStore()
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:        "approval",
		Model:     model,
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     store,
		Tools: []Tool{{
			Name:             "delete_file",
			Description:      "delete a file",
			RequiresApproval: true,
			Execute: func(context.Context, map[string]any) (string, error) {
				ran = true
				return "approved delete", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = session.Prompt(context.Background(), "delete notes")
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected approval error, got %v", err)
	}
	if ran {
		t.Fatal("tool ran before approval")
	}
	data, ok := store.Load("approval/s1")
	if !ok || len(data.PendingApprovals) != 1 {
		t.Fatalf("pending approval not persisted: %#v", data.PendingApprovals)
	}

	resp, err := session.Resume(context.Background(), approvalErr.Request.ID, ApprovalDecision{Type: ApprovalDecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	if !ran || resp.Text != "deleted" {
		t.Fatalf("resume response=%#v ran=%v", resp, ran)
	}
	data, _ = store.Load("approval/s1")
	if len(data.PendingApprovals) != 0 {
		t.Fatalf("pending approval not cleared: %#v", data.PendingApprovals)
	}
}

func TestFileStoreCanResumeApprovalAfterAgentRecreation(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pausedAgent, err := NewAgent(context.Background(), AgentConfig{
		ID: "durable-approval",
		Model: ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
			return ModelResponse{ToolCalls: []ToolCall{{ID: "call-send", Name: "send_email"}}}, nil
		}),
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     store,
		Tools: []Tool{{
			Name:             "send_email",
			Description:      "send email",
			RequiresApproval: true,
			Execute: func(context.Context, map[string]any) (string, error) {
				return "should not run before restart", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := pausedAgent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Prompt(context.Background(), "send it")
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected approval error, got %v", err)
	}

	resumed := false
	resumedAgent, err := NewAgent(context.Background(), AgentConfig{
		ID:        "durable-approval",
		Model:     ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) { return ModelResponse{Content: "sent"}, nil }),
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     store,
		Tools: []Tool{{
			Name:             "send_email",
			Description:      "send email",
			RequiresApproval: true,
			Execute: func(context.Context, map[string]any) (string, error) {
				resumed = true
				return "sent after approval", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resumedSession, err := resumedAgent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := resumedSession.Resume(context.Background(), approvalErr.Request.ID, ApprovalDecision{Type: ApprovalDecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed || resp.Text != "sent" {
		t.Fatalf("resume response=%#v resumed=%v", resp, resumed)
	}
}

func TestCheckpointsPersistFailedRunsForDurableRecovery(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:        "durable",
		Model:     ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) { return ModelResponse{}, errors.New("boom") }),
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     store,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.Prompt(context.Background(), "work"); err == nil {
		t.Fatal("expected model error")
	}
	data, ok := store.Load("durable/s1")
	if !ok {
		t.Fatal("session missing")
	}
	if len(data.Checkpoints) == 0 {
		t.Fatalf("expected checkpoints after failed run: %#v", data)
	}
	if len(data.Runs) == 0 {
		t.Fatalf("expected run state after failed run: %#v", data)
	}
	var runID string
	for id := range data.Runs {
		runID = id
	}

	recoveredAgent, err := NewAgent(context.Background(), AgentConfig{
		ID: "durable",
		Model: ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
			return ModelResponse{Content: "recovered"}, nil
		}),
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     store,
	})
	if err != nil {
		t.Fatal(err)
	}
	recoveredSession, err := recoveredAgent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := recoveredSession.ResumeRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "recovered" {
		t.Fatalf("resume run response = %#v", resp)
	}
}

func TestHandoffToolTransfersWorkToNamedAgent(t *testing.T) {
	t.Parallel()

	reviewer, err := NewAgent(context.Background(), AgentConfig{
		ID: "reviewer",
		Model: ModelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
			return ModelResponse{Content: "reviewed"}, nil
		}),
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	store := NewMemoryStore()
	lead, err := NewAgent(context.Background(), AgentConfig{
		ID: "lead",
		Model: ModelFunc(func(_ context.Context, req ModelRequest) (ModelResponse, error) {
			calls++
			if calls == 1 {
				return ModelResponse{ToolCalls: []ToolCall{{ID: "call-handoff", Name: "handoff", Arguments: map[string]any{"target": "reviewer", "prompt": "check this"}}}}, nil
			}
			if !contains(lastMessage(req.Messages).Content, "reviewed") {
				t.Fatalf("handoff result not returned to lead: %#v", req.Messages)
			}
			return ModelResponse{Content: "merged"}, nil
		}),
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
		Store:     store,
		Handoffs:  map[string]*Agent{"reviewer": reviewer},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := lead.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := session.Prompt(context.Background(), "delegate")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "merged" {
		t.Fatalf("response = %#v", resp)
	}
	data, _ := store.Load("lead/s1")
	if len(data.Handoffs) != 1 || data.Handoffs[0].ToAgentID != "reviewer" {
		t.Fatalf("handoff not persisted: %#v", data.Handoffs)
	}
}

func TestPromptStreamEmitsModelTokensAndHTTPStreamsCustomEvents(t *testing.T) {
	t.Parallel()

	var tokens []string
	model := StreamingModelFunc(func(ctx context.Context, req ModelRequest, emit StreamEmitter) (ModelResponse, error) {
		if err := emit(ctx, StreamEvent{Type: StreamEventToken, Delta: "hel"}); err != nil {
			return ModelResponse{}, err
		}
		if err := emit(ctx, StreamEvent{Type: StreamEventToken, Delta: "lo"}); err != nil {
			return ModelResponse{}, err
		}
		return ModelResponse{Content: "hello"}, nil
	})
	agent, err := NewAgent(context.Background(), AgentConfig{
		ID:        "stream",
		Model:     model,
		ModelName: "test/model",
		Env:       NewMemoryEnv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.Session(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := session.PromptStream(context.Background(), "say hello", func(_ context.Context, event StreamEvent) error {
		if event.Type == StreamEventToken {
			tokens = append(tokens, event.Delta)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" || len(tokens) != 2 || tokens[0] != "hel" || tokens[1] != "lo" {
		t.Fatalf("response=%#v tokens=%#v", resp, tokens)
	}
}

func hasTraceEvent(events []TraceEvent, eventType TraceEventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
