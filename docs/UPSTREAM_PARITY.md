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
| Build plugins for Node/Cloudflare | Not copied | Go version uses compiled handlers instead of TS bundling |
