# Upstream Parity

中文版本：[docs/zh-CN/UPSTREAM_PARITY.md](zh-CN/UPSTREAM_PARITY.md)

Source baseline: `withastro/flue` at commit `8fdf8e0e9df5bd33c3120f846d510c86188861f6`.

| Upstream feature | Go status | Evidence |
|---|---|---|
| SDK types for Agent, Session, Tool, Store, Env | Implemented | `types.go`, `session.go`, `store.go` |
| `AGENTS.md`, `CLAUDE.md`, skills, roles discovery | Implemented | `context.go`, `context_test.go` |
| Built-in tools `read/write/edit/bash/grep/glob/task` | Implemented | `tools.go`, `session_test.go` |
| Session prompt loop and persistence | Implemented | `session.go`, `session_test.go` |
| Structured result extraction | Implemented | `result.go`, `result_test.go` |
| HTTP health, manifest, agent invoke | Implemented | `http.go`, `http_test.go` |
| Webhook accepted mode and SSE final events | Implemented | `http.go`, `http_test.go` |
| CLI | Implemented in Go form | `cmd/fluego/main.go` supports init/inspect/list/run/build/example server |
| File-backed session persistence | Implemented | `store.go`, `filestore_test.go` |
| MCP adapter | Implemented | `mcp.go`, `mcp_test.go` |
| Compaction hook | Implemented | `types.go`, `session.go`, `compaction_test.go` |
| Remote/container sandbox adapter | Implemented | `remote_env.go`, `remote_env_test.go` |
| Guardrails for input/output/tool calls | Implemented | `Guardrail`, `GuardrailFunc`, `GuardrailStage*`, `runtime_features_test.go` |
| Tracing/observability events | Implemented | `TraceEvent`, `Tracer`, `TracerFunc`, `session.go`, `runtime_features_test.go` |
| Human-in-the-loop approval pause/resume | Implemented | `Tool.RequiresApproval`, `ApprovalRequest`, `Session.Resume`, `runtime_features_test.go` |
| Durable run state and checkpoints | Implemented | `RunState`, `Checkpoint`, `Session.ResumeRun`, `SessionData.Runs`, `SessionData.Checkpoints`, `runtime_features_test.go` |
| Multi-agent handoff | Implemented | Built-in `handoff` tool, `AgentConfig.Handoffs`, `HandoffRecord`, `runtime_features_test.go` |
| Token/runtime streaming | Implemented | `StreamingModel`, `PromptStream`, `StreamEvent`, HTTP `RequestContext.Emit`, `http_test.go` |
| Build plugins for Node/Cloudflare | Not copied | Go version uses compiled handlers instead of TS bundling |

## LangGraph-Inspired Runtime Capabilities

| Capability | Flue4Go surface | Current boundary |
|---|---|---|
| Guardrails | Guardrails run before input, final output, and tool execution. | Built-in PII or model classifiers are intentionally not bundled; users provide deterministic or model-backed `Guardrail` implementations. |
| Tracing / Observability | `Tracer` receives run/model/tool/checkpoint/approval/handoff lifecycle events. | No external exporter is bundled; bridge `Tracer` to logs, OpenTelemetry, LangSmith, or a database in application code. |
| Human collaboration | Tools can set `RequiresApproval`; execution persists an `ApprovalRequest` and resumes with `Session.Resume`. | Resuming prompt-scoped custom tools requires passing those tools again to `Resume`. Agent-level tools resume without extra setup. |
| Durable execution | `SessionData` stores `RunState`, `Checkpoint`, `PendingApprovals`, and handoff lineage in the configured `SessionStore`; `Session.ResumeRun` restores the latest checkpoint. | This is step checkpointing for the Flue4Go prompt loop, not distributed leases. Side-effecting tools should still be idempotent. |
| Multi-agent handoff | Built-in `handoff` transfers work to a named `AgentConfig.Handoffs` target and records lineage. | Handoff is explicit and tool-based; it does not implement graph-supervisor scheduling. |
| Streaming | `StreamingModel` emits token deltas; `PromptStream` and HTTP SSE forward `StreamEvent`s. | Streaming depends on the model adapter emitting events; non-streaming models still return final content. |
