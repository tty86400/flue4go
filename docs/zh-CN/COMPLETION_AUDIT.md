# 完成审计

本文件记录当前项目目标和可验证证据。

## 原始目标

使用 Go 语言复刻 `withastro/flue` 的核心功能，采用 Go 最佳实践，并把性能、安全、注释、文档、AI agent 后续开发支持放在优先位置。

## 中文资料补充目标

| 要求 | 交付物 | 状态 |
|---|---|---|
| 现有资料复刻一份中文的 | `README.zh-CN.md`, `docs/zh-CN/*.md` | 已完成 |
| 代码中补充充足中文注解 | `types.go`, `session.go`, `env.go`, `tools.go`, `http.go`, `mcp.go`, `store.go`, `remote_env.go` | 已完成 |
| 新手能立马上手 | `README.zh-CN.md`, `docs/zh-CN/QUICKSTART.md`, `docs/zh-CN/GLOSSARY.md` | 已完成 |

## 功能完成证据

| 要求 | 证据 | 状态 |
|---|---|---|
| Go 实现 | `go.mod`, package `github.com/xwlv/flue4go` | 已完成 |
| Agent/session harness | `session.go`, `session_test.go` | 已完成 |
| Model adapter | `Model` in `types.go` | 已完成 |
| Sandbox abstraction | `Env`, `LocalEnv`, `MemoryEnv`, `RemoteEnv` | 已完成 |
| Session persistence | `MemoryStore`, `FileStore` | 已完成 |
| Built-in tools | `tools.go` | 已完成 |
| Skills/roles/AGENTS discovery | `context.go`, `context_test.go` | 已完成 |
| Structured result extraction | `result.go`, `result_test.go` | 已完成 |
| Task/subagent support | `Session.Task`, built-in `task` tool | 已完成 |
| HTTP routes | `http.go`, `http_test.go` | 已完成 |
| Webhook/SSE | `http.go`, `http_test.go` | 已完成 |
| MCP tools | `mcp.go`, `mcp_test.go` | 已完成 |
| CLI | `cmd/fluego/main.go` | 已完成 |
| AI-agent support | `.agents/skills/framework-maintainer/SKILL.md`, `docs/zh-CN/AI_AGENT_SUPPORT.md` | 已完成 |
| Guardrails 输入/输出/工具安全校验 | `Guardrail`, `GuardrailFunc`, `GuardrailStage*`, `runtime_features_test.go` | 已完成 |
| Tracing/Observability | `TraceEvent`, `Tracer`, `TracerFunc`, `runtime_features_test.go` | 已完成 |
| 人机协作审批暂停/恢复 | `Tool.RequiresApproval`, `ApprovalRequest`, `ApprovalDecision`, `Session.Resume`, `runtime_features_test.go` | 已完成 |
| Durable Execution 基础能力 | `RunState`, `Checkpoint`, `Session.ResumeRun`, `SessionData.Runs`, `SessionData.Checkpoints`, `runtime_features_test.go` | 已完成 |
| 多 Agent Handoff | `AgentConfig.Handoffs`, 内置 `handoff` 工具, `HandoffRecord`, `runtime_features_test.go` | 已完成 |
| Streaming | `StreamingModel`, `PromptStream`, `StreamEvent`, `RequestContext.Emit`, `http_test.go` | 已完成 |

## 验证命令

```powershell
go test -count=1 ./...
go vet ./...
go build ./...
go run ./cmd/fluego inspect --workspace .
```

说明：此前在 Windows 上尝试 `go test -race ./...` 时，进程以 `0xc0000139` 退出，未进入测试断言，疑似本机 race runtime/CGO 环境问题。
