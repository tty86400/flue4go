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
| Node/Cloudflare build plugin | Go 版本不复制 | 用 `go build` 和编译期注册替代 |

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
