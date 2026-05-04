# Architecture

中文版本：[docs/zh-CN/ARCHITECTURE.md](zh-CN/ARCHITECTURE.md)

Flue4Go keeps the upstream Flue mental model while using Go's strengths: compiled handlers, explicit interfaces, and small runtime seams.

```text
                 +----------------+
HTTP request --->| Registry       |
                 +-------+--------+
                         |
                         v
                 +----------------+
                 | Handler        |
                 +-------+--------+
                         |
                         v
                 +----------------+
                 | Agent          |
                 +---+---+---+---+---+----+
                     |   |   |   |   |
          +----------+   |   |   |   +--------------+
          v              v   v   v                  v
    SessionStore     Env   Guardrails Tracer   Model adapter
          |              |   |   |                  |
          v              v   v   v                  v
    runs/checkpoints files/tools policy events      provider call
```

## Key Interfaces

| Interface | Purpose |
|---|---|
| `Model` | Provider-neutral model adapter. |
| `Env` | Sandbox filesystem and shell abstraction. |
| `SessionStore` | Persistence for conversation history. |
| `Tool` | Callable model capability. |
| `Registry` | HTTP-facing agent handler registration. |
| `Guardrail` | Input, output, and tool-call safety validation. |
| `Tracer` | Structured runtime observability events. |
| `StreamingModel` | Token/runtime streaming model adapter. |

## Runtime Flow

```text
Session.Prompt
  |
  +-- create RunState
  +-- run input guardrails
  +-- append user message and checkpoint
  +-- discover scoped tools
  +-- call Model.Generate or StreamingModel.Stream
  +-- emit trace events
  +-- run tool guardrails and optional approval pause
  +-- execute tool calls in Env or handoff target Agent
  +-- append tool results
  +-- repeat until assistant returns final text
  +-- run output guardrails
  +-- persist checkpointed SessionData
```

## Upstream Parity Strategy

| Upstream Flue | Flue4Go |
|---|---|
| TypeScript agent handler files | Compiled Go `Registry.Handle()` functions |
| `SessionEnv` | `Env` interface |
| `prompt/skill/task/shell` | `Session.Prompt/Skill/Task/Shell` |
| `AGENTS.md` and `.agents/skills` | Same on-disk convention |
| Node Hono generated server | Standard `net/http` handler |
| Cloudflare Worker/DO target | Future connector, not in MVP |
