# Flue4Go

中文资料入口：[README.zh-CN.md](README.zh-CN.md)

Flue4Go is a Go-language reimplementation of [`withastro/flue`](https://github.com/withastro/flue). It recreates Flue's agent harness ideas in Go while adapting the architecture to Go best practices. It keeps the same product shape: agents, sessions, roles, skills, sandbox tools, task delegation, structured results, and HTTP invocation.

The Go version does not clone TypeScript's bundler-first design. Users register compiled Go handlers and bring their own model adapter. That keeps deployment simple, type-safe, and friendly to long-running AI agents that need stable interfaces.

## Status

Experimental MVP. The current implementation is intentionally focused on the core runtime:

| Area | Status |
|---|---|
| Agent/session runtime | Implemented |
| Memory and local sandbox | Implemented |
| Built-in tools: read/write/edit/bash/grep/glob/task | Implemented |
| AGENTS.md, CLAUDE.md, roles, skills discovery | Implemented |
| Structured result extraction | Implemented |
| HTTP registry: `/health`, `/agents`, `/agents/{name}/{id}` | Implemented |
| Webhook `X-Webhook: true` and basic SSE result events | Implemented |
| File-backed session store | Implemented |
| Streamable HTTP MCP tool adapter | Implemented |
| Message-history compaction hook | Implemented |
| Remote/container sandbox wrapper | Implemented |
| CLI workspace init/inspect/example server | Implemented |
| Cloudflare build target and hot build/watch loop | Go-specific divergence, documented |

## Architecture

```text
cmd/fluego
  |
  +-- init / inspect / serve-example
      |
      v
github.com/xwlv/flue4go
  |
  +-- Registry -----> HTTP Server
  |
  +-- Agent
       |
       +-- SessionStore
       +-- Env sandbox
       +-- Model adapter
       +-- Context discovery
       +-- Built-in + custom tools
              |
              v
          Session Prompt / Skill / Task / Shell
```

## Quick Start

```go
registry := flue.NewRegistry()
registry.Handle("hello", flue.Triggers{Webhook: true}, func(ctx context.Context, req flue.RequestContext) (any, error) {
    return map[string]any{"message": "hello " + req.ID}, nil
})

http.ListenAndServe(":3000", flue.NewHTTPServer(registry, flue.HTTPServerOptions{}))
```

Use a model adapter for real agent sessions:

```go
model := flue.ModelFunc(func(ctx context.Context, req flue.ModelRequest) (flue.ModelResponse, error) {
    // Call OpenAI, Anthropic, local vLLM, or an internal gateway here.
    return flue.ModelResponse{Content: "done"}, nil
})

agent, _ := flue.NewAgent(ctx, flue.AgentConfig{
    ID:        "support",
    Model:     model,
    ModelName: "provider/model",
    Env:       flue.NewMemoryEnv(),
})
session, _ := agent.Session(ctx, "customer-123")
result, _ := session.Prompt(ctx, "Help this customer.")
```

## CLI

```powershell
go run ./cmd/fluego init --workspace .flue
go run ./cmd/fluego inspect --workspace .flue
go run ./cmd/fluego list --url http://localhost:3000
go run ./cmd/fluego run echo --id test --url http://localhost:3000 --payload '{"name":"World"}'
go run ./cmd/fluego build --package ./cmd/my-agent --output ./dist/my-agent.exe
go run ./cmd/fluego serve-example --addr :3000
```

## Design Priorities

| Priority | Implementation choice |
|---|---|
| Security | Local paths are confined below sandbox root; HTTP route ids are validated; request bodies are size-limited. |
| Performance | Memory sandbox and store avoid process overhead; tool outputs are truncated; interfaces permit provider-specific streaming later. |
| Go best practice | Compiled handler registration, small interfaces, context-aware calls, standard library first. |
| AI-agent sustainability | Stable files, focused APIs, clear docs, tests covering harness behavior, and skills/roles/AGENTS compatibility. |

## Development

```powershell
go test ./...
go vet ./...
go test -race ./...
```

## License

MIT License. See [LICENSE](LICENSE).

## GitHub And Release Scripts

This repository includes GitHub-ready automation:

| File | Purpose |
|---|---|
| `.github/workflows/ci.yml` | Runs gofmt check, tests, vet, build, and CLI smoke on push/PR. |
| `.github/workflows/release.yml` | Builds multi-platform `fluego` binaries for `v*` tags and publishes GitHub releases. |
| `scripts/check.ps1` / `scripts/check.sh` | Local full verification. |
| `scripts/build.ps1` / `scripts/build.sh` | Local multi-platform CLI builds into `dist/`. |
| `Makefile` | Linux/macOS shortcuts for check, test, vet, build, and release-build. |

Windows:

```powershell
.\scripts\check.ps1
.\scripts\build.ps1
```

Linux/macOS:

```sh
./scripts/check.sh
./scripts/build.sh
```
