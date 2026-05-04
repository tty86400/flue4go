# 上游功能对齐说明

上游基线：`withastro/flue` commit `8fdf8e0e9df5bd33c3120f846d510c86188861f6`。

| 上游功能 | Go 版本状态 | 证据 |
|---|---|---|
| Agent / Session / Tool / Store / Env 类型 | 已实现 | `types.go`, `session.go`, `store.go` |
| `AGENTS.md`, `CLAUDE.md`, skills, roles 发现 | 已实现 | `context.go`, `context_test.go` |
| 内置工具 `read/write/edit/bash/grep/glob/task` | 已实现 | `tools.go`, `session_test.go` |
| Session prompt loop 和持久化 | 已实现 | `session.go`, `session_test.go` |
| 结构化结果提取 | 已实现 | `result.go`, `result_test.go` |
| HTTP health/manifest/invoke | 已实现 | `http.go`, `http_test.go` |
| Webhook accepted 和 SSE final event | 已实现 | `http.go`, `http_test.go` |
| CLI | Go 形态已实现 | `cmd/fluego/main.go` |
| 文件持久化 | 已实现 | `store.go`, `filestore_test.go` |
| MCP adapter | 已实现 | `mcp.go`, `mcp_test.go` |
| Compaction hook | 已实现 | `types.go`, `session.go`, `compaction_test.go` |
| 远程/container sandbox wrapper | 已实现 | `remote_env.go`, `remote_env_test.go` |
| 输入/输出/工具调用 Guardrails | 已实现 | `Guardrail`, `GuardrailFunc`, `GuardrailStage*`, `runtime_features_test.go` |
| Tracing / Observability 事件 | 已实现 | `TraceEvent`, `Tracer`, `TracerFunc`, `session.go`, `runtime_features_test.go` |
| 人机协作审批暂停/恢复 | 已实现 | `Tool.RequiresApproval`, `ApprovalRequest`, `Session.Resume`, `runtime_features_test.go` |
| Durable run state 和 checkpoint | 已实现 | `RunState`, `Checkpoint`, `Session.ResumeRun`, `SessionData.Runs`, `SessionData.Checkpoints`, `runtime_features_test.go` |
| 多 Agent handoff | 已实现 | 内置 `handoff` 工具、`AgentConfig.Handoffs`, `HandoffRecord`, `runtime_features_test.go` |
| Token/runtime streaming | 已实现 | `StreamingModel`, `PromptStream`, `StreamEvent`, HTTP `RequestContext.Emit`, `http_test.go` |
| Node/Cloudflare build plugin | Go 版本不复制 | 用 `go build` 和编译期注册替代 |

## 参考 LangGraph 补齐的运行时能力

| 能力 | Flue4Go 入口 | 当前边界 |
|---|---|---|
| Guardrails | 在输入、最终输出、工具执行前运行 `Guardrail`。 | 不内置 PII 或模型分类器；业务侧自行提供确定性或模型型 `Guardrail`。 |
| Tracing / Observability | `Tracer` 接收 run/model/tool/checkpoint/approval/handoff 生命周期事件。 | 不绑定外部 exporter；应用层可桥接到日志、OpenTelemetry、LangSmith 或数据库。 |
| 人机协作 | 工具设置 `RequiresApproval` 后会暂停，持久化 `ApprovalRequest`，再用 `Session.Resume` 继续。 | 如果暂停的是 prompt-scoped 自定义工具，恢复时需要再次传入这些工具；Agent 级工具可直接恢复。 |
| Durable Execution | `SessionData` 保存 `RunState`、`Checkpoint`、`PendingApprovals` 和 handoff lineage；`Session.ResumeRun` 会从最新 checkpoint 恢复。 | 这是 Flue4Go prompt loop 的步骤 checkpoint，不是分布式 lease；有副作用的工具仍应保持幂等。 |
| 多 Agent Handoff | 内置 `handoff` 工具把任务交给 `AgentConfig.Handoffs` 中的目标 Agent，并记录 lineage。 | handoff 是显式 tool-based 交接，不包含 graph supervisor 调度器。 |
| Streaming | `StreamingModel` 产出 token delta；`PromptStream` 和 HTTP SSE 转发 `StreamEvent`。 | 是否有 token 流取决于模型适配器；普通 `Model` 仍返回最终文本。 |

## 为什么不复制 Node/Cloudflare build plugin

TypeScript 版需要扫描 agent 文件并 bundle 到 Node/Cloudflare runtime。Go 版更适合：

```text
Go source
  |
  v
Registry.Handle()
  |
  v
go build
  |
  v
single binary
```

这符合 Go 的部署模型，也减少运行时动态加载风险。
