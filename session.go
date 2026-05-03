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
		session.data = SessionData{Version: 1, Metadata: map[string]any{}, CreatedAt: time.Now().UTC()}
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
	s.data.Messages = append(s.data.Messages, Message{Role: MessageRoleUser, Content: prompt})
	if err := s.saveLocked(); err != nil {
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
	modelName := s.resolveModelName(opts)
	systemPrompt := s.resolveSystemPrompt(opts)

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
		resp, err := s.agent.model.Generate(ctx, req)
		if err != nil {
			return PromptResponse{}, err
		}
		assistant := Message{Role: MessageRoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls}
		s.data.Messages = append(s.data.Messages, assistant)
		if len(resp.ToolCalls) == 0 {
			if err := s.maybeCompactLocked(ctx); err != nil {
				return PromptResponse{}, err
			}
			if err := s.saveLocked(); err != nil {
				return PromptResponse{}, err
			}
			return PromptResponse{Text: resp.Content}, nil
		}
		for _, call := range resp.ToolCalls {
			tool, ok := toolByName[call.Name]
			if !ok {
				return PromptResponse{}, fmt.Errorf("model requested unknown tool %q", call.Name)
			}
			result, err := tool.Execute(ctx, call.Arguments)
			if err != nil {
				result = "ERROR: " + err.Error()
			}
			s.data.Messages = append(s.data.Messages, Message{
				Role:       MessageRoleTool,
				Name:       call.Name,
				ToolCallID: call.ID,
				Content:    result,
			})
		}
		if err := s.saveLocked(); err != nil {
			return PromptResponse{}, err
		}
	}
	return PromptResponse{}, fmt.Errorf("maximum model turns exceeded: %d", s.agent.maxTurns)
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
	builtins := createBuiltinTools(scoped, func(ctx context.Context, text string, childOpts PromptOptions) (PromptResponse, error) {
		return s.runTask(ctx, text, childOpts)
	})
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
		data:     SessionData{Version: 1, Metadata: map[string]any{}, CreatedAt: time.Now().UTC()},
		depth:    s.depth + 1,
	}
	return child.runPrompt(ctx, BuildPromptText(text, false), opts, "task")
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
