package flue

import (
	"context"
	"errors"
	"time"
)

// GuardrailStage describes where a safety check runs.
type GuardrailStage string

const (
	GuardrailStageInput  GuardrailStage = "input"
	GuardrailStageOutput GuardrailStage = "output"
	GuardrailStageTool   GuardrailStage = "tool"
)

// GuardrailRequest is the provider-neutral safety check payload.
type GuardrailRequest struct {
	AgentID   string
	SessionID string
	RunID     string
	Stage     GuardrailStage
	Content   string
	ToolCall  *ToolCall
	Metadata  map[string]any
}

// GuardrailResult controls whether execution may continue. Content can replace
// input/output text when a guardrail wants to sanitize rather than block.
type GuardrailResult struct {
	Allowed  bool
	Reason   string
	Content  string
	Metadata map[string]any
}

// Guardrail validates input, output, or tool-call safety.
type Guardrail interface {
	Check(context.Context, GuardrailRequest) (GuardrailResult, error)
}

// GuardrailFunc adapts a function into a Guardrail.
type GuardrailFunc func(context.Context, GuardrailRequest) (GuardrailResult, error)

// Check implements Guardrail.
func (f GuardrailFunc) Check(ctx context.Context, req GuardrailRequest) (GuardrailResult, error) {
	return f(ctx, req)
}

// ErrGuardrailBlocked marks a blocked input, output, or tool call.
var ErrGuardrailBlocked = errors.New("guardrail blocked execution")

// GuardrailError includes the concrete stage and reason.
type GuardrailError struct {
	Stage  GuardrailStage
	Reason string
}

// Error implements error.
func (e *GuardrailError) Error() string {
	if e.Reason == "" {
		return ErrGuardrailBlocked.Error()
	}
	return ErrGuardrailBlocked.Error() + ": " + e.Reason
}

// Unwrap implements errors.Unwrap.
func (e *GuardrailError) Unwrap() error { return ErrGuardrailBlocked }

// TraceEventType identifies runtime lifecycle events.
type TraceEventType string

const (
	TraceEventRunStart         TraceEventType = "run.start"
	TraceEventRunEnd           TraceEventType = "run.end"
	TraceEventRunError         TraceEventType = "run.error"
	TraceEventModelStart       TraceEventType = "model.start"
	TraceEventModelEnd         TraceEventType = "model.end"
	TraceEventToolStart        TraceEventType = "tool.start"
	TraceEventToolEnd          TraceEventType = "tool.end"
	TraceEventCheckpoint       TraceEventType = "checkpoint"
	TraceEventGuardrailBlocked TraceEventType = "guardrail.blocked"
	TraceEventApprovalRequired TraceEventType = "approval.required"
	TraceEventHandoff          TraceEventType = "handoff"
)

// TraceEvent is the structured observability record emitted by the runtime.
type TraceEvent struct {
	Type       TraceEventType `json:"type"`
	Time       time.Time      `json:"time"`
	AgentID    string         `json:"agentId,omitempty"`
	SessionID  string         `json:"sessionId,omitempty"`
	RunID      string         `json:"runId,omitempty"`
	Turn       int            `json:"turn,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Content    string         `json:"content,omitempty"`
	Error      string         `json:"error,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Tracer receives structured runtime events.
type Tracer interface {
	Trace(context.Context, TraceEvent)
}

// TracerFunc adapts a function into a Tracer.
type TracerFunc func(context.Context, TraceEvent)

// Trace implements Tracer.
func (f TracerFunc) Trace(ctx context.Context, event TraceEvent) { f(ctx, event) }

// ApprovalDecisionType is a human or policy decision for a pending tool call.
type ApprovalDecisionType string

const (
	ApprovalDecisionApprove ApprovalDecisionType = "approve"
	ApprovalDecisionReject  ApprovalDecisionType = "reject"
)

// ApprovalRequest is persisted when a sensitive tool call pauses execution.
type ApprovalRequest struct {
	ID        string         `json:"id"`
	RunID     string         `json:"runId"`
	AgentID   string         `json:"agentId"`
	SessionID string         `json:"sessionId"`
	ToolCall  ToolCall       `json:"toolCall"`
	CreatedAt time.Time      `json:"createdAt"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ApprovalDecision resumes or rejects a paused tool call.
type ApprovalDecision struct {
	Type      ApprovalDecisionType `json:"type"`
	Reason    string               `json:"reason,omitempty"`
	Arguments map[string]any       `json:"arguments,omitempty"`
}

// ApprovalPolicy can auto-approve or reject sensitive tool calls.
type ApprovalPolicy interface {
	Decide(context.Context, ApprovalRequest) (ApprovalDecision, bool, error)
}

// ApprovalPolicyFunc adapts a function into an ApprovalPolicy.
type ApprovalPolicyFunc func(context.Context, ApprovalRequest) (ApprovalDecision, bool, error)

// Decide implements ApprovalPolicy.
func (f ApprovalPolicyFunc) Decide(ctx context.Context, req ApprovalRequest) (ApprovalDecision, bool, error) {
	return f(ctx, req)
}

// ApprovalRequiredError is returned when execution pauses for human review.
type ApprovalRequiredError struct {
	Request ApprovalRequest
}

// Error implements error.
func (e *ApprovalRequiredError) Error() string {
	return "approval required for tool " + e.Request.ToolCall.Name
}

// RunStatus tracks durable execution state.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusPaused    RunStatus = "paused"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

// RunState is persisted so a process can inspect, resume, or repair runs.
type RunState struct {
	ID                string         `json:"id"`
	Status            RunStatus      `json:"status"`
	Model             string         `json:"model,omitempty"`
	SystemPrompt      string         `json:"systemPrompt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	LastError         string         `json:"lastError,omitempty"`
	PendingApprovalID string         `json:"pendingApprovalId,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// Checkpoint is a durable snapshot after important runtime steps.
type Checkpoint struct {
	ID        string    `json:"id"`
	RunID     string    `json:"runId"`
	Step      string    `json:"step"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"createdAt"`
}

// HandoffRequest is the explicit contract for transferring work to another
// registered Agent.
type HandoffRequest struct {
	ID            string `json:"id"`
	FromAgentID   string `json:"fromAgentId"`
	FromSessionID string `json:"fromSessionId"`
	ToAgentID     string `json:"toAgentId"`
	Prompt        string `json:"prompt"`
	Summary       string `json:"summary,omitempty"`
}

// HandoffResult is persisted after a target Agent completes the transferred work.
type HandoffResult struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	SessionID string `json:"sessionId"`
}

// HandoffRecord captures cross-agent lineage for debugging and replay.
type HandoffRecord struct {
	FromAgentID string         `json:"fromAgentId"`
	ToAgentID   string         `json:"toAgentId"`
	Request     HandoffRequest `json:"request"`
	Result      HandoffResult  `json:"result"`
	CreatedAt   time.Time      `json:"createdAt"`
}
