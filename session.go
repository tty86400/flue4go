package flue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultAgentID  = "default"
	defaultSession  = "default"
	defaultMaxTurns = 16
	maxTaskDepth    = 4
)

// AgentConfig configures a Go-native Flue runtime.
type AgentConfig struct {
	ID         string
	Model      Model
	ModelName  string
	Env        Env
	Store      SessionStore
	CWD        string
	Role       string
	Tools      []Tool
	Commands   []Command
	Guardrails []Guardrail
	Tracer     Tracer
	Approval   ApprovalPolicy
	Handoffs   map[string]*Agent
	MaxTurns   int
	Compactor  Compactor
	Compaction CompactionConfig
}

// Agent owns sandbox, context, tools, and session lifecycle for one runtime id.
//
// 中文说明：Agent 是“运行时容器”，它不代表一次对话，而是管理一组
// Session。通常一个业务 agent name 对应一个 Agent，不同用户/任务用不同
// Session id 隔离历史。
type Agent struct {
	id         string
	model      Model
	modelName  string
	env        Env
	store      SessionStore
	context    DiscoveredContext
	role       string
	tools      []Tool
	commands   []Command
	guardrails []Guardrail
	tracer     Tracer
	approval   ApprovalPolicy
	handoffs   map[string]*Agent
	maxTurns   int
	compactor  Compactor
	compaction CompactionConfig
	mu         sync.Mutex
	sessions   map[string]*Session
}

// NewAgent initializes context discovery and returns an agent runtime.
//
// 中文说明：NewAgent 会在创建时读取 Env.CWD() 下的 AGENTS.md、roles 和
// skills。因此如果你要让 Agent 看到某个工作区的规则，必须在 NewAgent
// 之前把 Env 指向正确目录。
func NewAgent(ctx context.Context, cfg AgentConfig) (*Agent, error) {
	if cfg.ID == "" {
		cfg.ID = defaultAgentID
	}
	if cfg.Env == nil {
		cfg.Env = NewMemoryEnv()
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryStore()
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = defaultMaxTurns
	}
	if err := validateTools(cfg.Tools); err != nil {
		return nil, err
	}
	discovered, err := DiscoverContext(ctx, cfg.Env)
	if err != nil {
		return nil, err
	}
	return &Agent{
		id:         cfg.ID,
		model:      cfg.Model,
		modelName:  cfg.ModelName,
		env:        cfg.Env,
		store:      cfg.Store,
		context:    discovered,
		role:       cfg.Role,
		tools:      append([]Tool(nil), cfg.Tools...),
		commands:   append([]Command(nil), cfg.Commands...),
		guardrails: append([]Guardrail(nil), cfg.Guardrails...),
		tracer:     cfg.Tracer,
		approval:   cfg.Approval,
		handoffs:   cloneHandoffs(cfg.Handoffs),
		maxTurns:   cfg.MaxTurns,
		compactor:  cfg.Compactor,
		compaction: cfg.Compaction,
		sessions:   map[string]*Session{},
	}, nil
}

// ID returns the agent id.
func (a *Agent) ID() string { return a.id }

// Session loads or creates a named session.
func (a *Agent) Session(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		id = defaultSession
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if session, ok := a.sessions[id]; ok {
		return session, nil
	}
	data, _ := a.store.Load(a.storageKey(id))
	session := &Session{
		id:       id,
		agent:    a,
		env:      a.env,
		storeKey: a.storageKey(id),
		data:     data,
		depth:    0,
	}
	if session.data.Version == 0 {
		session.data = newSessionData()
	}
	a.sessions[id] = session
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return session, nil
}

// Shell runs a command in the agent sandbox without adding conversation history.
func (a *Agent) Shell(ctx context.Context, command string, opts ExecOptions) (ShellResult, error) {
	scoped, err := a.env.Scope(ctx, ScopeOptions{Commands: a.commands})
	if err != nil {
		return ShellResult{}, err
	}
	return scoped.Exec(ctx, command, opts)
}

// Destroy cleans up the owned sandbox.
func (a *Agent) Destroy(ctx context.Context) error {
	return a.env.Cleanup(ctx)
}

func (a *Agent) storageKey(sessionID string) string {
	return a.id + "/" + sessionID
}

// Session is one conversation timeline.
type Session struct {
	id       string
	agent    *Agent
	env      Env
	storeKey string
	data     SessionData
	depth    int
	mu       sync.Mutex
}

// ID returns the session id.
func (s *Session) ID() string { return s.id }

// Prompt sends text to the model and runs tool loops until final text appears.
//
// 中文说明：Prompt 是最常用入口。它会自动加 headless preamble，让模型按
// 无人值守方式工作；如果模型返回 tool call，Session 会执行工具并继续
// 调模型，直到模型给出最终文本。
func (s *Session) Prompt(ctx context.Context, text string, opts ...PromptOption) (PromptResponse, error) {
	return s.runPrompt(ctx, BuildPromptText(text, false), collectOptions(opts), "prompt")
}

// PromptStream sends text to the model and emits incremental stream events.
func (s *Session) PromptStream(ctx context.Context, text string, emit StreamEmitter, opts ...PromptOption) (PromptResponse, error) {
	options := collectOptions(opts)
	options.Stream = emit
	return s.runPrompt(ctx, BuildPromptText(text, false), options, "prompt")
}

// PromptInto sends text and decodes the final delimited result block into out.
func (s *Session) PromptInto(ctx context.Context, text string, out any, opts ...PromptOption) error {
	resp, err := s.runPrompt(ctx, BuildPromptText(text, true), collectOptions(opts), "prompt")
	if err != nil {
		return err
	}
	return ExtractResult(resp.Text, out)
}

// Skill executes a named or path-addressed skill.
func (s *Session) Skill(ctx context.Context, name string, opts ...PromptOption) (PromptResponse, error) {
	options := collectOptions(opts)
	skill, err := s.resolveSkill(ctx, name)
	if err != nil {
		return PromptResponse{}, err
	}
	prompt, err := BuildSkillPrompt(skill, options.Args, false)
	if err != nil {
		return PromptResponse{}, err
	}
	return s.runPrompt(ctx, prompt, options, "skill")
}

// SkillInto executes a skill and decodes the final delimited result block.
func (s *Session) SkillInto(ctx context.Context, name string, out any, opts ...PromptOption) error {
	options := collectOptions(opts)
	skill, err := s.resolveSkill(ctx, name)
	if err != nil {
		return err
	}
	prompt, err := BuildSkillPrompt(skill, options.Args, true)
	if err != nil {
		return err
	}
	resp, err := s.runPrompt(ctx, prompt, options, "skill")
	if err != nil {
		return err
	}
	return ExtractResult(resp.Text, out)
}

// Shell runs a command and records the result as conversation context.
func (s *Session) Shell(ctx context.Context, command string, opts ExecOptions) (ShellResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scoped, err := s.env.Scope(ctx, ScopeOptions{Commands: s.agent.commands})
	if err != nil {
		return ShellResult{}, err
	}
	result, err := scoped.Exec(ctx, command, opts)
	if err != nil {
		return result, err
	}
	s.data.Messages = append(s.data.Messages, Message{
		Role:    MessageRoleTool,
		Name:    "shell",
		Content: fmt.Sprintf("command: %s\nstdout:\n%s\nstderr:\n%s\nexitCode: %d", command, result.Stdout, result.Stderr, result.ExitCode),
	})
	return result, s.saveLocked()
}

// Task delegates to a detached child session.
func (s *Session) Task(ctx context.Context, text string, opts ...PromptOption) (PromptResponse, error) {
	return s.runTask(ctx, text, collectOptions(opts))
}

// Resume applies a human approval decision and continues the paused run.
func (s *Session) Resume(ctx context.Context, approvalID string, decision ApprovalDecision, opts ...PromptOption) (PromptResponse, error) {
	return s.resumeApproval(ctx, approvalID, decision, collectOptions(opts))
}

// ResumeRun restores the latest checkpoint for a run and continues execution.
func (s *Session) ResumeRun(ctx context.Context, runID string, opts ...PromptOption) (PromptResponse, error) {
	return s.resumeRun(ctx, runID, collectOptions(opts))
}

// Delete removes persisted session state.
func (s *Session) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = SessionData{}
	return s.agent.store.Delete(s.storeKey)
}

func (s *Session) runPrompt(ctx context.Context, prompt string, opts PromptOptions, source string) (PromptResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agent.model == nil {
		return PromptResponse{}, errors.New("no model configured")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	runID := randomID()
	modelName := s.resolveModelName(opts)
	systemPrompt := s.resolveSystemPrompt(opts)
	s.ensureRuntimeMapsLocked()
	// Each prompt is tracked as a durable run before the first model call. This
	// ties failures, approval pauses, and ResumeRun recovery to one id.
	s.data.Runs[runID] = RunState{
		ID:           runID,
		Status:       RunStatusRunning,
		Model:        modelName,
		SystemPrompt: systemPrompt,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Metadata:     map[string]any{"source": source},
	}
	s.trace(ctx, opts, TraceEvent{Type: TraceEventRunStart, RunID: runID, Metadata: map[string]any{"source": source}})

	// Input guardrails run before the user message enters model history. A
	// guardrail can block or return sanitized content.
	guardedPrompt, err := s.applyGuardrailsLocked(ctx, opts, runID, GuardrailStageInput, prompt, nil)
	if err != nil {
		s.markRunFailedLocked(runID, err)
		_ = s.saveLocked()
		s.trace(ctx, opts, TraceEvent{Type: TraceEventRunError, RunID: runID, Error: err.Error()})
		return PromptResponse{}, err
	}
	prompt = guardedPrompt
	s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleUser, Content: prompt})
	// Checkpoint immediately after input so a crash after this point still has a
	// recoverable prompt and run record.
	if err := s.checkpointLocked(ctx, opts, runID, "input"); err != nil {
		return PromptResponse{}, err
	}

	tools, err := s.toolsForCall(ctx, opts)
	if err != nil {
		return PromptResponse{}, err
	}
	toolByName := map[string]Tool{}
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}

	for turn := 0; turn < s.agent.maxTurns; turn++ {
		// 中文导读：这是 Agent harness 的核心循环：
		// 1. 把历史、system prompt、工具列表交给模型；
		// 2. 如果模型要调用工具，就执行工具并把结果写回历史；
		// 3. 如果模型不再要工具，就把最终文本返回给调用方。
		// maxTurns 是保护阀，避免模型无限循环调用工具。
		req := ModelRequest{
			SystemPrompt: systemPrompt,
			Model:        modelName,
			Messages:     append([]Message(nil), s.data.Messages...),
			Tools:        tools,
		}
		s.trace(ctx, opts, TraceEvent{Type: TraceEventModelStart, RunID: runID, Turn: turn})
		resp, err := s.callModel(ctx, opts, req)
		if err != nil {
			s.markRunFailedLocked(runID, err)
			_ = s.checkpointLocked(ctx, opts, runID, "model_error")
			s.trace(ctx, opts, TraceEvent{Type: TraceEventRunError, RunID: runID, Turn: turn, Error: err.Error()})
			return PromptResponse{}, err
		}
		s.trace(ctx, opts, TraceEvent{Type: TraceEventModelEnd, RunID: runID, Turn: turn})
		assistant := Message{Role: MessageRoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls}
		s.data.Messages = append(s.data.Messages, assistant)
		// Store the model decision before executing tools. ResumeRun can restore
		// this checkpoint and execute pending tool calls once.
		if err := s.checkpointLocked(ctx, opts, runID, fmt.Sprintf("model_%d", turn)); err != nil {
			return PromptResponse{}, err
		}
		if len(resp.ToolCalls) == 0 {
			// Output guardrails run on final assistant text, after tool loops
			// settle but before returning content to the caller.
			content, err := s.applyGuardrailsLocked(ctx, opts, runID, GuardrailStageOutput, resp.Content, nil)
			if err != nil {
				s.markRunFailedLocked(runID, err)
				_ = s.saveLocked()
				s.trace(ctx, opts, TraceEvent{Type: TraceEventRunError, RunID: runID, Turn: turn, Error: err.Error()})
				return PromptResponse{}, err
			}
			s.data.Messages[len(s.data.Messages)-1].Content = content
			if err := s.maybeCompactLocked(ctx); err != nil {
				return PromptResponse{}, err
			}
			s.markRunStatusLocked(runID, RunStatusCompleted, "")
			if err := s.checkpointLocked(ctx, opts, runID, "completed"); err != nil {
				return PromptResponse{}, err
			}
			s.trace(ctx, opts, TraceEvent{Type: TraceEventRunEnd, RunID: runID, Turn: turn})
			if opts.Stream != nil {
				_ = opts.Stream(ctx, StreamEvent{Type: StreamEventResult, Data: PromptResponse{Text: content}})
				_ = opts.Stream(ctx, StreamEvent{Type: StreamEventIdle})
			}
			return PromptResponse{Text: content}, nil
		}
		for _, call := range resp.ToolCalls {
			tool, ok := toolByName[call.Name]
			if !ok {
				err := fmt.Errorf("model requested unknown tool %q", call.Name)
				s.markRunFailedLocked(runID, err)
				return PromptResponse{}, err
			}
			if _, err := s.applyGuardrailsLocked(ctx, opts, runID, GuardrailStageTool, "", &call); err != nil {
				s.markRunFailedLocked(runID, err)
				_ = s.saveLocked()
				return PromptResponse{}, err
			}
			if tool.RequiresApproval {
				approval, decided, err := s.resolveApprovalLocked(ctx, runID, call)
				if err != nil {
					s.markRunFailedLocked(runID, err)
					return PromptResponse{}, err
				}
				if !decided {
					// Pause before side effects. The approval request is persisted
					// with the run so another process can inspect and resume it.
					s.markRunPausedLocked(runID, approval.ID)
					s.data.PendingApprovals[approval.ID] = approval
					if err := s.checkpointLocked(ctx, opts, runID, "approval_required"); err != nil {
						return PromptResponse{}, err
					}
					s.trace(ctx, opts, TraceEvent{Type: TraceEventApprovalRequired, RunID: runID, Turn: turn, ToolName: call.Name, ToolCallID: call.ID})
					return PromptResponse{}, &ApprovalRequiredError{Request: approval}
				}
				if approvalDecisionRejected(approval.Metadata) {
					s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleTool, Name: call.Name, ToolCallID: call.ID, Content: "REJECTED: " + fmt.Sprint(approval.Metadata["reason"])})
					continue
				}
				if args, ok := approval.Metadata["arguments"].(map[string]any); ok && args != nil {
					call.Arguments = args
				}
			}
			s.trace(ctx, opts, TraceEvent{Type: TraceEventToolStart, RunID: runID, Turn: turn, ToolName: call.Name, ToolCallID: call.ID})
			result, err := s.executeToolLocked(ctx, opts, runID, tool, call)
			if err != nil {
				result = "ERROR: " + err.Error()
			}
			s.trace(ctx, opts, TraceEvent{Type: TraceEventToolEnd, RunID: runID, Turn: turn, ToolName: call.Name, ToolCallID: call.ID})
			s.data.Messages = append(s.data.Messages, Message{
				Role:       MessageRoleTool,
				Name:       call.Name,
				ToolCallID: call.ID,
				Content:    result,
			})
		}
		if err := s.checkpointLocked(ctx, opts, runID, fmt.Sprintf("tools_%d", turn)); err != nil {
			return PromptResponse{}, err
		}
	}
	err = fmt.Errorf("maximum model turns exceeded: %d", s.agent.maxTurns)
	s.markRunFailedLocked(runID, err)
	_ = s.saveLocked()
	return PromptResponse{}, err
}

func (s *Session) toolsForCall(ctx context.Context, opts PromptOptions) ([]Tool, error) {
	if err := validateTools(opts.Tools); err != nil {
		return nil, err
	}
	commands := append([]Command(nil), s.agent.commands...)
	commands = append(commands, opts.Commands...)
	scoped, err := s.env.Scope(ctx, ScopeOptions{Commands: commands})
	if err != nil {
		return nil, err
	}
	builtins := createBuiltinTools(scoped,
		func(ctx context.Context, text string, childOpts PromptOptions) (PromptResponse, error) {
			return s.runTask(ctx, text, childOpts)
		},
		func(ctx context.Context, req HandoffRequest, childOpts PromptOptions) (HandoffResult, error) {
			return s.runHandoff(ctx, req, childOpts)
		},
	)
	out := append([]Tool{}, builtins...)
	out = append(out, s.agent.tools...)
	out = append(out, opts.Tools...)
	return out, nil
}

func (s *Session) runTask(ctx context.Context, text string, opts PromptOptions) (PromptResponse, error) {
	if s.depth >= maxTaskDepth {
		return PromptResponse{}, fmt.Errorf("maximum task depth exceeded: %d", maxTaskDepth)
	}
	childID := "task-" + randomID()
	child := &Session{
		id:       childID,
		agent:    s.agent,
		env:      s.env,
		storeKey: s.agent.storageKey(childID),
		data:     newSessionData(),
		depth:    s.depth + 1,
	}
	return child.runPrompt(ctx, BuildPromptText(text, false), opts, "task")
}

func (s *Session) runHandoff(ctx context.Context, req HandoffRequest, opts PromptOptions) (HandoffResult, error) {
	target, ok := s.agent.handoffs[req.ToAgentID]
	if !ok || target == nil {
		return HandoffResult{}, fmt.Errorf("handoff target %q not registered", req.ToAgentID)
	}
	if req.ID == "" {
		req.ID = "handoff-" + randomID()
	}
	req.FromAgentID = s.agent.id
	req.FromSessionID = s.id
	childSessionID := s.id + "-" + req.ID
	child, err := target.Session(ctx, childSessionID)
	if err != nil {
		return HandoffResult{}, err
	}
	resp, err := child.runPrompt(ctx, BuildPromptText(req.Prompt, false), opts, "handoff")
	if err != nil {
		return HandoffResult{}, err
	}
	result := HandoffResult{ID: req.ID, Text: resp.Text, SessionID: childSessionID}
	s.data.Handoffs = append(s.data.Handoffs, HandoffRecord{
		FromAgentID: req.FromAgentID,
		ToAgentID:   req.ToAgentID,
		Request:     req,
		Result:      result,
		CreatedAt:   time.Now().UTC(),
	})
	s.trace(ctx, opts, TraceEvent{Type: TraceEventHandoff, ToolName: req.ToAgentID, Content: resp.Text})
	return result, nil
}

func (s *Session) resumeApproval(ctx context.Context, approvalID string, decision ApprovalDecision, opts PromptOptions) (PromptResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agent.model == nil {
		return PromptResponse{}, errors.New("no model configured")
	}
	s.ensureRuntimeMapsLocked()
	approval, ok := s.data.PendingApprovals[approvalID]
	if !ok {
		return PromptResponse{}, fmt.Errorf("approval %q not found", approvalID)
	}
	runID := approval.RunID
	if run, ok := s.data.Runs[runID]; ok {
		opts.Model = run.Model
	}
	tools, err := s.toolsForCall(ctx, opts)
	if err != nil {
		return PromptResponse{}, err
	}
	toolByName := map[string]Tool{}
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}
	call := approval.ToolCall
	tool, ok := toolByName[call.Name]
	if !ok {
		return PromptResponse{}, fmt.Errorf("tool %q not available for resume", call.Name)
	}
	delete(s.data.PendingApprovals, approvalID)
	s.markRunStatusLocked(runID, RunStatusRunning, "")
	if decision.Type == ApprovalDecisionReject {
		s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleTool, Name: call.Name, ToolCallID: call.ID, Content: "REJECTED: " + decision.Reason})
	} else {
		if decision.Arguments != nil {
			call.Arguments = decision.Arguments
		}
		result, err := s.executeToolLocked(ctx, opts, runID, tool, call)
		if err != nil {
			result = "ERROR: " + err.Error()
		}
		s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleTool, Name: call.Name, ToolCallID: call.ID, Content: result})
	}
	if err := s.checkpointLocked(ctx, opts, runID, "approval_resumed"); err != nil {
		return PromptResponse{}, err
	}
	return s.continueLocked(ctx, opts, runID, tools)
}

func (s *Session) resumeRun(ctx context.Context, runID string, opts PromptOptions) (PromptResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agent.model == nil {
		return PromptResponse{}, errors.New("no model configured")
	}
	s.ensureRuntimeMapsLocked()
	run, ok := s.data.Runs[runID]
	if !ok {
		return PromptResponse{}, fmt.Errorf("run %q not found", runID)
	}
	if run.PendingApprovalID != "" {
		if approval, ok := s.data.PendingApprovals[run.PendingApprovalID]; ok {
			return PromptResponse{}, &ApprovalRequiredError{Request: approval}
		}
	}
	checkpoint, ok := s.latestCheckpointLocked(runID)
	if !ok {
		return PromptResponse{}, fmt.Errorf("run %q has no checkpoint", runID)
	}
	// ResumeRun intentionally starts from the latest persisted checkpoint rather
	// than from Go stack state. This mirrors durable workflow replay: restore
	// recorded messages, then continue through the normal guarded tool loop.
	s.data.Messages = append([]Message(nil), checkpoint.Messages...)
	if run.Model != "" {
		opts.Model = run.Model
	}
	s.markRunStatusLocked(runID, RunStatusRunning, "")
	tools, err := s.toolsForCall(ctx, opts)
	if err != nil {
		return PromptResponse{}, err
	}
	toolByName := map[string]Tool{}
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}
	if len(s.data.Messages) > 0 {
		last := s.data.Messages[len(s.data.Messages)-1]
		if last.Role == MessageRoleAssistant && len(last.ToolCalls) > 0 {
			// If the checkpoint captured a model decision with pending tool calls,
			// execute those calls before asking the model for the next turn.
			for _, call := range last.ToolCalls {
				tool, ok := toolByName[call.Name]
				if !ok {
					return PromptResponse{}, fmt.Errorf("model requested unknown tool %q", call.Name)
				}
				if _, err := s.applyGuardrailsLocked(ctx, opts, runID, GuardrailStageTool, "", &call); err != nil {
					s.markRunFailedLocked(runID, err)
					_ = s.saveLocked()
					return PromptResponse{}, err
				}
				if tool.RequiresApproval {
					approval, decided, err := s.resolveApprovalLocked(ctx, runID, call)
					if err != nil {
						s.markRunFailedLocked(runID, err)
						return PromptResponse{}, err
					}
					if !decided {
						s.markRunPausedLocked(runID, approval.ID)
						s.data.PendingApprovals[approval.ID] = approval
						if err := s.checkpointLocked(ctx, opts, runID, "approval_required"); err != nil {
							return PromptResponse{}, err
						}
						return PromptResponse{}, &ApprovalRequiredError{Request: approval}
					}
				}
				result, err := s.executeToolLocked(ctx, opts, runID, tool, call)
				if err != nil {
					result = "ERROR: " + err.Error()
				}
				s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleTool, Name: call.Name, ToolCallID: call.ID, Content: result})
			}
			if err := s.checkpointLocked(ctx, opts, runID, "resume_tools"); err != nil {
				return PromptResponse{}, err
			}
		}
	}
	return s.continueLocked(ctx, opts, runID, tools)
}

func (s *Session) continueLocked(ctx context.Context, opts PromptOptions, runID string, tools []Tool) (PromptResponse, error) {
	toolByName := map[string]Tool{}
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}
	modelName := s.resolveModelName(opts)
	systemPrompt := s.resolveSystemPrompt(opts)
	if run, ok := s.data.Runs[runID]; ok {
		if run.Model != "" {
			modelName = run.Model
		}
		if run.SystemPrompt != "" {
			systemPrompt = run.SystemPrompt
		}
	}
	for turn := 0; turn < s.agent.maxTurns; turn++ {
		req := ModelRequest{SystemPrompt: systemPrompt, Model: modelName, Messages: append([]Message(nil), s.data.Messages...), Tools: tools}
		resp, err := s.callModel(ctx, opts, req)
		if err != nil {
			s.markRunFailedLocked(runID, err)
			_ = s.checkpointLocked(ctx, opts, runID, "resume_model_error")
			return PromptResponse{}, err
		}
		s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})
		if len(resp.ToolCalls) == 0 {
			content, err := s.applyGuardrailsLocked(ctx, opts, runID, GuardrailStageOutput, resp.Content, nil)
			if err != nil {
				s.markRunFailedLocked(runID, err)
				return PromptResponse{}, err
			}
			s.data.Messages[len(s.data.Messages)-1].Content = content
			s.markRunStatusLocked(runID, RunStatusCompleted, "")
			if err := s.checkpointLocked(ctx, opts, runID, "completed"); err != nil {
				return PromptResponse{}, err
			}
			s.trace(ctx, opts, TraceEvent{Type: TraceEventRunEnd, RunID: runID})
			return PromptResponse{Text: content}, nil
		}
		for _, call := range resp.ToolCalls {
			tool, ok := toolByName[call.Name]
			if !ok {
				return PromptResponse{}, fmt.Errorf("model requested unknown tool %q", call.Name)
			}
			if _, err := s.applyGuardrailsLocked(ctx, opts, runID, GuardrailStageTool, "", &call); err != nil {
				s.markRunFailedLocked(runID, err)
				_ = s.saveLocked()
				return PromptResponse{}, err
			}
			if tool.RequiresApproval {
				approval, decided, err := s.resolveApprovalLocked(ctx, runID, call)
				if err != nil {
					s.markRunFailedLocked(runID, err)
					return PromptResponse{}, err
				}
				if !decided {
					s.markRunPausedLocked(runID, approval.ID)
					s.data.PendingApprovals[approval.ID] = approval
					if err := s.checkpointLocked(ctx, opts, runID, "approval_required"); err != nil {
						return PromptResponse{}, err
					}
					s.trace(ctx, opts, TraceEvent{Type: TraceEventApprovalRequired, RunID: runID, ToolName: call.Name, ToolCallID: call.ID})
					return PromptResponse{}, &ApprovalRequiredError{Request: approval}
				}
				if approvalDecisionRejected(approval.Metadata) {
					s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleTool, Name: call.Name, ToolCallID: call.ID, Content: "REJECTED: " + fmt.Sprint(approval.Metadata["reason"])})
					continue
				}
				if args, ok := approval.Metadata["arguments"].(map[string]any); ok && args != nil {
					call.Arguments = args
				}
			}
			result, err := s.executeToolLocked(ctx, opts, runID, tool, call)
			if err != nil {
				result = "ERROR: " + err.Error()
			}
			s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleTool, Name: call.Name, ToolCallID: call.ID, Content: result})
		}
		if err := s.checkpointLocked(ctx, opts, runID, fmt.Sprintf("resume_tools_%d", turn)); err != nil {
			return PromptResponse{}, err
		}
	}
	err := fmt.Errorf("maximum model turns exceeded: %d", s.agent.maxTurns)
	s.markRunFailedLocked(runID, err)
	_ = s.saveLocked()
	return PromptResponse{}, err
}

func (s *Session) resolveSkill(ctx context.Context, name string) (Skill, error) {
	if skill, ok := s.agent.context.Skills[name]; ok {
		return skill, nil
	}
	if contains(name, "/") || contains(name, ".md") {
		return LoadSkillByPath(ctx, s.env, name)
	}
	var available []string
	for name := range s.agent.context.Skills {
		available = append(available, name)
	}
	return Skill{}, fmt.Errorf("skill %q not registered; available: %v", name, available)
}

func (s *Session) resolveModelName(opts PromptOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	roleName := s.resolveRoleName(opts)
	if roleName != "" {
		if role, ok := s.agent.context.Roles[roleName]; ok && role.Model != "" {
			return role.Model
		}
	}
	return s.agent.modelName
}

func (s *Session) resolveSystemPrompt(opts PromptOptions) string {
	base := s.agent.context.SystemPrompt
	roleName := s.resolveRoleName(opts)
	if roleName == "" {
		return base
	}
	if role, ok := s.agent.context.Roles[roleName]; ok {
		return base + "\n\n<role name=\"" + role.Name + "\">\n" + role.Instructions + "\n</role>"
	}
	return base
}

func (s *Session) resolveRoleName(opts PromptOptions) string {
	if opts.Role != "" {
		return opts.Role
	}
	return s.agent.role
}

func (s *Session) saveLocked() error {
	if s.data.Version == 0 {
		s.data.Version = 1
	}
	if s.data.Metadata == nil {
		s.data.Metadata = map[string]any{}
	}
	return s.agent.store.Save(s.storeKey, s.data)
}

func (s *Session) ensureRuntimeMapsLocked() {
	if s.data.Metadata == nil {
		s.data.Metadata = map[string]any{}
	}
	if s.data.Runs == nil {
		s.data.Runs = map[string]RunState{}
	}
	if s.data.PendingApprovals == nil {
		s.data.PendingApprovals = map[string]ApprovalRequest{}
	}
}

func (s *Session) applyGuardrailsLocked(ctx context.Context, opts PromptOptions, runID string, stage GuardrailStage, content string, call *ToolCall) (string, error) {
	guardrails := append([]Guardrail(nil), s.agent.guardrails...)
	guardrails = append(guardrails, opts.Guardrails...)
	for _, guardrail := range guardrails {
		if guardrail == nil {
			continue
		}
		result, err := guardrail.Check(ctx, GuardrailRequest{
			AgentID:   s.agent.id,
			SessionID: s.id,
			RunID:     runID,
			Stage:     stage,
			Content:   content,
			ToolCall:  call,
		})
		if err != nil {
			return "", err
		}
		if !result.Allowed {
			err := &GuardrailError{Stage: stage, Reason: result.Reason}
			s.trace(ctx, opts, TraceEvent{Type: TraceEventGuardrailBlocked, RunID: runID, Content: content, Error: err.Error(), Metadata: result.Metadata})
			return "", err
		}
		if result.Content != "" {
			content = result.Content
		}
	}
	return content, nil
}

func (s *Session) callModel(ctx context.Context, opts PromptOptions, req ModelRequest) (ModelResponse, error) {
	if opts.Stream != nil {
		if model, ok := s.agent.model.(StreamingModel); ok {
			return model.Stream(ctx, req, opts.Stream)
		}
	}
	return s.agent.model.Generate(ctx, req)
}

func (s *Session) executeToolLocked(ctx context.Context, opts PromptOptions, runID string, tool Tool, call ToolCall) (string, error) {
	result, err := tool.Execute(ctx, call.Arguments)
	if opts.Stream != nil {
		event := StreamEvent{Type: StreamEventTrace, Trace: &TraceEvent{
			Type:       TraceEventToolEnd,
			Time:       time.Now().UTC(),
			AgentID:    s.agent.id,
			SessionID:  s.id,
			RunID:      runID,
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Error:      errorString(err),
		}}
		_ = opts.Stream(ctx, event)
	}
	return result, err
}

func (s *Session) resolveApprovalLocked(ctx context.Context, runID string, call ToolCall) (ApprovalRequest, bool, error) {
	approval := ApprovalRequest{
		ID:        runID + "/" + call.ID,
		RunID:     runID,
		AgentID:   s.agent.id,
		SessionID: s.id,
		ToolCall:  call,
		CreatedAt: time.Now().UTC(),
		Metadata:  map[string]any{},
	}
	if s.agent.approval == nil {
		return approval, false, nil
	}
	decision, decided, err := s.agent.approval.Decide(ctx, approval)
	if err != nil || !decided {
		return approval, decided, err
	}
	approval.Metadata["decision"] = string(decision.Type)
	approval.Metadata["reason"] = decision.Reason
	if decision.Arguments != nil {
		approval.Metadata["arguments"] = decision.Arguments
	}
	return approval, true, nil
}

func approvalDecisionRejected(metadata map[string]any) bool {
	decision, _ := metadata["decision"].(string)
	return decision == string(ApprovalDecisionReject)
}

func (s *Session) checkpointLocked(ctx context.Context, opts PromptOptions, runID, step string) error {
	s.ensureRuntimeMapsLocked()
	checkpoint := Checkpoint{
		ID:        runID + "/" + step + "/" + randomID(),
		RunID:     runID,
		Step:      step,
		Messages:  append([]Message(nil), s.data.Messages...),
		CreatedAt: time.Now().UTC(),
	}
	s.data.Checkpoints = append(s.data.Checkpoints, checkpoint)
	s.trace(ctx, opts, TraceEvent{Type: TraceEventCheckpoint, RunID: runID, Metadata: map[string]any{"step": step}})
	return s.saveLocked()
}

func (s *Session) latestCheckpointLocked(runID string) (Checkpoint, bool) {
	for i := len(s.data.Checkpoints) - 1; i >= 0; i-- {
		if s.data.Checkpoints[i].RunID == runID {
			checkpoint := s.data.Checkpoints[i]
			checkpoint.Messages = append([]Message(nil), checkpoint.Messages...)
			return checkpoint, true
		}
	}
	return Checkpoint{}, false
}

func (s *Session) markRunPausedLocked(runID, approvalID string) {
	s.markRunStatusLocked(runID, RunStatusPaused, "")
	run := s.data.Runs[runID]
	run.PendingApprovalID = approvalID
	run.UpdatedAt = time.Now().UTC()
	s.data.Runs[runID] = run
}

func (s *Session) markRunFailedLocked(runID string, err error) {
	s.markRunStatusLocked(runID, RunStatusFailed, errorString(err))
}

func (s *Session) markRunStatusLocked(runID string, status RunStatus, lastErr string) {
	s.ensureRuntimeMapsLocked()
	run := s.data.Runs[runID]
	if run.ID == "" {
		run.ID = runID
		run.CreatedAt = time.Now().UTC()
	}
	run.Status = status
	run.LastError = lastErr
	run.UpdatedAt = time.Now().UTC()
	s.data.Runs[runID] = run
}

func (s *Session) trace(ctx context.Context, opts PromptOptions, event TraceEvent) {
	event.Time = time.Now().UTC()
	event.AgentID = s.agent.id
	event.SessionID = s.id
	if s.agent.tracer != nil {
		s.agent.tracer.Trace(ctx, event)
	}
	if opts.Stream != nil {
		copy := event
		_ = opts.Stream(ctx, StreamEvent{Type: StreamEventTrace, Trace: &copy})
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Session) maybeCompactLocked(ctx context.Context) error {
	cfg := s.agent.compaction
	if s.agent.compactor == nil || cfg.MaxMessages <= 0 || len(s.data.Messages) <= cfg.MaxMessages {
		return nil
	}
	keep := cfg.KeepRecent
	if keep <= 0 {
		keep = cfg.MaxMessages / 2
	}
	if keep < 1 {
		keep = 1
	}
	if keep >= cfg.MaxMessages {
		keep = cfg.MaxMessages - 1
	}
	if keep < 1 || len(s.data.Messages) <= keep {
		return nil
	}
	split := len(s.data.Messages) - keep
	summary, err := s.agent.compactor.Compact(ctx, append([]Message(nil), s.data.Messages[:split]...))
	if err != nil {
		return err
	}
	recent := append([]Message(nil), s.data.Messages[split:]...)
	s.data.Messages = append([]Message{{
		Role:    MessageRoleSystem,
		Name:    "compaction",
		Content: summary,
	}}, recent...)
	return nil
}

func randomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func newSessionData() SessionData {
	return SessionData{
		Version:          1,
		Metadata:         map[string]any{},
		Runs:             map[string]RunState{},
		PendingApprovals: map[string]ApprovalRequest{},
		CreatedAt:        time.Now().UTC(),
	}
}

func cloneHandoffs(in map[string]*Agent) map[string]*Agent {
	if len(in) == 0 {
		return nil
	}
	out := map[string]*Agent{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
