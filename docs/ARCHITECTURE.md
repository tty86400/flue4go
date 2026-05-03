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
                 +---+---+---+----+
                     |   |   |
          +----------+   |   +--------------+
          v              v                  v
    SessionStore     Env sandbox       Model adapter
          |              |                  |
          v              v                  v
    history/state   files/shell/tools  provider call
```

## Key Interfaces

| Interface | Purpose |
|---|---|
| `Model` | Provider-neutral model adapter. |
| `Env` | Sandbox filesystem and shell abstraction. |
| `SessionStore` | Persistence for conversation history. |
| `Tool` | Callable model capability. |
| `Registry` | HTTP-facing agent handler registration. |

## Runtime Flow

```text
Session.Prompt
  |
  +-- append user message
  +-- discover scoped tools
  +-- call Model.Generate
  +-- execute tool calls in Env
  +-- append tool results
  +-- repeat until assistant returns final text
  +-- persist SessionData
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
