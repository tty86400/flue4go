// Package flue provides a Go-native agent harness inspired by withastro/flue.
//
// The package is intentionally small at the boundary: users bring their own
// model adapter, sandbox connector, and deployment shape while Flue4Go owns the
// common harness concerns: context discovery, tools, sessions, persistence,
// task delegation, and HTTP routing.
//
// 中文导读：这个包只负责“Agent 运行时骨架”，不绑定任何模型厂商。
// 新手可以先理解 4 个核心接口：
//   - Model：怎么调用大模型；
//   - Env：Agent 能在哪个沙箱里读写文件、执行命令；
//   - Tool：模型可以调用哪些函数；
//   - SessionStore：会话历史保存到哪里。
package flue

import (
	"context"
	"time"
)

// MessageRole identifies the speaker or runtime source of a conversation item.
type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

// Message is the provider-neutral conversation record stored by sessions.
type Message struct {
	Role       MessageRole `json:"role"`
	Content    string      `json:"content,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

// ToolCall is a provider-neutral request to execute a named tool.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ModelRequest is passed to a Model for each agent turn.
type ModelRequest struct {
	SystemPrompt string
	Model        string
	Messages     []Message
	Tools        []Tool
}

// ModelResponse is returned by a Model. A response may contain text, tool calls,
// or both. Tool calls are executed and fed back to the model until it stops
// requesting tools or MaxTurns is reached.
type ModelResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// Model is the only required LLM integration point. This keeps provider
// secrets and transport details outside the core harness.
//
// 中文说明：接 OpenAI、Anthropic、本地模型或公司内部网关时，只需要实现
// Generate。Flue4Go 不直接保存 API Key，也不假设模型返回格式。
type Model interface {
	Generate(context.Context, ModelRequest) (ModelResponse, error)
}

// ModelFunc adapts a function into a Model.
type ModelFunc func(context.Context, ModelRequest) (ModelResponse, error)

// Generate implements Model.
func (f ModelFunc) Generate(ctx context.Context, req ModelRequest) (ModelResponse, error) {
	return f(ctx, req)
}

// StreamEmitter receives incremental runtime output such as model token deltas
// and trace events.
type StreamEmitter func(context.Context, StreamEvent) error

// StreamingModel can return model output incrementally while still producing a
// final provider-neutral ModelResponse for the normal tool loop.
type StreamingModel interface {
	Generate(context.Context, ModelRequest) (ModelResponse, error)
	Stream(context.Context, ModelRequest, StreamEmitter) (ModelResponse, error)
}

// StreamingModelFunc adapts a streaming function into a Model.
type StreamingModelFunc func(context.Context, ModelRequest, StreamEmitter) (ModelResponse, error)

// Generate implements Model by collecting only the final response.
func (f StreamingModelFunc) Generate(ctx context.Context, req ModelRequest) (ModelResponse, error) {
	return f(ctx, req, nil)
}

// Stream implements StreamingModel.
func (f StreamingModelFunc) Stream(ctx context.Context, req ModelRequest, emit StreamEmitter) (ModelResponse, error) {
	return f(ctx, req, emit)
}

// StreamEventType identifies streamed runtime output.
type StreamEventType string

const (
	StreamEventToken  StreamEventType = "token"
	StreamEventTrace  StreamEventType = "trace"
	StreamEventResult StreamEventType = "result"
	StreamEventError  StreamEventType = "error"
	StreamEventIdle   StreamEventType = "idle"
)

// StreamEvent is a provider-neutral event suitable for SSE or direct callbacks.
type StreamEvent struct {
	Type  StreamEventType `json:"type"`
	Delta string          `json:"delta,omitempty"`
	Trace *TraceEvent     `json:"trace,omitempty"`
	Data  any             `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Compactor summarizes older messages when a session history exceeds its
// configured budget.
type Compactor interface {
	Compact(context.Context, []Message) (string, error)
}

// CompactorFunc adapts a function into a Compactor.
type CompactorFunc func(context.Context, []Message) (string, error)

// Compact implements Compactor.
func (f CompactorFunc) Compact(ctx context.Context, messages []Message) (string, error) {
	return f(ctx, messages)
}

// CompactionConfig controls message-history compaction. It is message-count
// based so it works across providers; model-specific token budgeting can be
// implemented inside a custom Compactor.
type CompactionConfig struct {
	MaxMessages int
	KeepRecent  int
}

// Tool is a provider-neutral callable exposed to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	// RequiresApproval pauses before Execute unless a human or policy decision
	// approves the tool call.
	RequiresApproval bool
	Execute          func(context.Context, map[string]any) (string, error)
}

// Command is an executable capability that can be scoped to a prompt, skill, or
// shell call. Built-in shell execution does not implicitly expose host secrets.
type Command struct {
	Name    string
	Execute func(context.Context, []string) (ShellResult, error)
}

// ShellResult captures process output without provider-specific wrapping.
type ShellResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// FileInfo is the portable stat result used by sandbox environments.
type FileInfo struct {
	IsFile        bool      `json:"isFile"`
	IsDirectory   bool      `json:"isDirectory"`
	IsSymlink     bool      `json:"isSymlink"`
	Size          int64     `json:"size"`
	ModifiedTime  time.Time `json:"modifiedTime"`
	WorkspacePath string    `json:"workspacePath"`
}

// Env is the universal sandbox interface. Implementations must resolve all
// paths relative to CWD unless an absolute workspace path is supplied.
//
// 中文说明：Env 是安全边界。模型通过工具读文件、写文件、执行命令时，
// 最终都会落到 Env。生产环境里应优先给 Agent 一个受限目录或远程沙箱，
// 不要直接暴露整台机器的用户目录。
type Env interface {
	Exec(context.Context, string, ExecOptions) (ShellResult, error)
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	Stat(context.Context, string) (FileInfo, error)
	ReadDir(context.Context, string) ([]string, error)
	Exists(context.Context, string) (bool, error)
	Mkdir(context.Context, string) error
	Remove(context.Context, string, RemoveOptions) error
	CWD() string
	ResolvePath(string) (string, error)
	Scope(context.Context, ScopeOptions) (Env, error)
	Cleanup(context.Context) error
}

// ExecOptions controls sandbox command execution.
type ExecOptions struct {
	CWD     string
	Env     map[string]string
	Timeout time.Duration
}

// ScopeOptions creates operation-scoped command/tool access.
type ScopeOptions struct {
	Commands []Command
}

// RemoveOptions controls sandbox deletion.
type RemoveOptions struct {
	Recursive bool
	Force     bool
}

// Skill is loaded from .agents/skills/<name>/SKILL.md or a markdown path.
type Skill struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Instructions string            `json:"instructions"`
	Frontmatter  map[string]string `json:"frontmatter,omitempty"`
}

// Role adds focused instructions and optional model selection to a session.
type Role struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Instructions string            `json:"instructions"`
	Model        string            `json:"model,omitempty"`
	Frontmatter  map[string]string `json:"frontmatter,omitempty"`
}

// DiscoveredContext is loaded from AGENTS.md, CLAUDE.md, roles, and skills.
type DiscoveredContext struct {
	SystemPrompt string
	Skills       map[string]Skill
	Roles        map[string]Role
}

// PromptResponse is the plain text result of a prompt, skill, or task call.
type PromptResponse struct {
	Text string `json:"text"`
}

// PromptOptions controls a single prompt/skill/task invocation.
type PromptOptions struct {
	Role     string
	Model    string
	Timeout  time.Duration
	Commands []Command
	Tools    []Tool
	Args     map[string]any
	// Guardrails are appended to AgentConfig.Guardrails for this call.
	Guardrails []Guardrail
	// Stream receives incremental token/runtime events for this call.
	Stream StreamEmitter
}

// PromptOption mutates PromptOptions.
type PromptOption func(*PromptOptions)

// WithRole selects a role for one call.
func WithRole(role string) PromptOption { return func(o *PromptOptions) { o.Role = role } }

// WithModel selects a model name for one call.
func WithModel(model string) PromptOption { return func(o *PromptOptions) { o.Model = model } }

// WithTimeout bounds one call.
func WithTimeout(timeout time.Duration) PromptOption {
	return func(o *PromptOptions) { o.Timeout = timeout }
}

// WithTools adds custom tools for one call.
func WithTools(tools ...Tool) PromptOption { return func(o *PromptOptions) { o.Tools = tools } }

// WithCommands adds scoped commands for one call.
func WithCommands(commands ...Command) PromptOption {
	return func(o *PromptOptions) { o.Commands = commands }
}

// WithArgs supplies skill arguments.
func WithArgs(args map[string]any) PromptOption { return func(o *PromptOptions) { o.Args = args } }

// WithGuardrails adds call-scoped guardrails.
func WithGuardrails(guardrails ...Guardrail) PromptOption {
	return func(o *PromptOptions) { o.Guardrails = guardrails }
}

// WithStream streams incremental model/runtime events for one call.
func WithStream(emit StreamEmitter) PromptOption { return func(o *PromptOptions) { o.Stream = emit } }

func collectOptions(opts []PromptOption) PromptOptions {
	var out PromptOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}
