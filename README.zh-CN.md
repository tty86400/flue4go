# Flue4Go 中文说明

Flue4Go 是一个使用 Go 语言复刻、源自 [`withastro/flue`](https://github.com/withastro/flue) 的 Agent Harness 框架。它不是简单翻译 TypeScript 代码，而是在对齐 Flue 核心功能的同时，按 Go 的最佳实践重新设计架构。你可以把它理解成“给 Agent 用的运行时骨架”：它负责会话、工具、沙箱、技能、角色、上下文发现、HTTP 调用和持久化；你只需要接入自己的模型和业务逻辑。

## 一句话理解

```text
你的业务 Handler
  |
  v
Flue4Go Agent
  |
  +-- Session 会话记忆
  +-- Env 沙箱文件/命令
  +-- Tools 工具调用
  +-- Guardrails / Tracing / Approval
  +-- Skills/roles/AGENTS.md 上下文
  +-- Model 模型适配器
```

## 当前能力

| 能力 | 状态 | 新手理解 |
|---|---|---|
| Agent / Session 运行时 | 已实现 | 让一次 Agent 任务有记忆、有工具、有模型 |
| 内存沙箱 / 本地沙箱 / 远程沙箱包装 | 已实现 | 控制 Agent 能看到和操作哪些文件/命令 |
| 内置工具 `read/write/edit/bash/grep/glob/task` | 已实现 | 给模型提供读写文件、搜索、执行命令、分派子任务能力 |
| `AGENTS.md` / `CLAUDE.md` / skills / roles | 已实现 | 用 Markdown 管理 Agent 规则、技能和角色 |
| 结构化结果提取 | 已实现 | 让模型按 JSON 结果返回，方便业务代码读取 |
| HTTP 调用 | 已实现 | 暴露 `/health`、`/agents`、`/agents/{name}/{id}` |
| Webhook / SSE | 已实现 | 支持异步接收和基础流式事件 |
| 文件持久化 | 已实现 | 会话记录可以保存到 JSON 文件 |
| MCP 工具适配 | 已实现 | 可以把远程 MCP 工具转成 Flue4Go 工具 |
| CLI | 已实现 | 初始化、检查、调用、构建、示例服务 |
| Guardrails | 已实现 | 可以在输入、输出、工具调用前做安全校验 |
| Tracing / Observability | 已实现 | Agent 运行过程会产出结构化事件 |
| 人机协作 | 已实现 | 敏感工具可暂停，审批后继续执行 |
| Durable Execution | 已实现基础版 | 保存 run state、checkpoint、pending approval，并可 `ResumeRun` |
| 多 Agent Handoff | 已实现 | 可用 `handoff` 工具交接给另一个 Agent |
| Streaming | 已实现 | 支持模型 token callback 和 HTTP SSE 事件桥接 |

## 5 分钟上手

### 1. 查看项目是否能编译

```powershell
go test ./...
go vet ./...
go build ./...
```

### 2. 初始化一个 Agent 工作区

```powershell
go run ./cmd/fluego init --workspace .flue
go run ./cmd/fluego inspect --workspace .flue
```

你会得到类似结构：

```text
.flue/
  AGENTS.md
  roles/
    reviewer.md
  .agents/
    skills/
      example/
        SKILL.md
```

### 3. 启动示例 HTTP 服务

```powershell
go run ./cmd/fluego serve-example --addr :3000
```

另开一个终端调用：

```powershell
go run ./cmd/fluego list --url http://localhost:3000
go run ./cmd/fluego run echo --id test --url http://localhost:3000 --payload '{"name":"World"}'
go run ./cmd/fluego run echo --id test --url http://localhost:3000 --payload '{"name":"World"}' --sse
```

### 4. 在自己的 Go 代码里注册 Agent

```go
package main

import (
	"context"
	"net/http"

	flue "github.com/xwlv/flue4go"
)

func main() {
	registry := flue.NewRegistry()

	registry.Handle("hello", flue.Triggers{Webhook: true}, func(ctx context.Context, req flue.RequestContext) (any, error) {
		return map[string]any{
			"id":      req.ID,
			"message": "hello from Flue4Go",
		}, nil
	})

	_ = http.ListenAndServe(":3000", flue.NewHTTPServer(registry, flue.HTTPServerOptions{}))
}
```

## 接入真实模型

Flue4Go 不绑定任何模型厂商。你只需要实现 `Model`：

```go
model := flue.ModelFunc(func(ctx context.Context, req flue.ModelRequest) (flue.ModelResponse, error) {
	// 在这里调用 OpenAI、Anthropic、本地模型或公司内部网关。
	// req.Messages 是当前会话上下文。
	// req.Tools 是本轮可用工具。
	return flue.ModelResponse{Content: "任务完成"}, nil
})
```

然后创建 Agent：

```go
agent, err := flue.NewAgent(ctx, flue.AgentConfig{
	ID:        "support",
	Model:     model,
	ModelName: "provider/model",
	Env:       flue.NewMemoryEnv(),
})
if err != nil {
	return err
}

session, err := agent.Session(ctx, "customer-123")
if err != nil {
	return err
}

result, err := session.Prompt(ctx, "请帮客户总结问题")
```

同一个 `AgentConfig` 可以继续挂上安全、观测和恢复相关能力：`Guardrails` 负责输入/输出/工具调用校验，`Tracer` 接收结构化事件，敏感工具可通过 `Tool.RequiresApproval` 暂停后再 `Session.Resume`，需要 token 流时使用 `PromptStream`。

```go
guardrail := flue.GuardrailFunc(func(ctx context.Context, req flue.GuardrailRequest) (flue.GuardrailResult, error) {
	if req.Stage == flue.GuardrailStageTool && req.ToolCall != nil && req.ToolCall.Name == "delete_file" {
		return flue.GuardrailResult{Allowed: false, Reason: "delete blocked"}, nil
	}
	return flue.GuardrailResult{Allowed: true}, nil
})

agent, _ := flue.NewAgent(ctx, flue.AgentConfig{
	Model:      model,
	Env:        flue.NewMemoryEnv(),
	Guardrails: []flue.Guardrail{guardrail},
	Tracer:     flue.TracerFunc(func(ctx context.Context, event flue.TraceEvent) {}),
})

session, _ := agent.Session(ctx, "customer-123")
_, _ = session.PromptStream(ctx, "请流式返回", func(ctx context.Context, event flue.StreamEvent) error {
	return nil
})
```

## 新手学习顺序

| 顺序 | 文件 | 学什么 |
|---|---|---|
| 1 | `README.zh-CN.md` | 项目是干什么的，如何跑起来 |
| 2 | `docs/zh-CN/QUICKSTART.md` | 从零写一个可调用 Agent |
| 3 | `docs/zh-CN/ARCHITECTURE.md` | 核心架构和调用链 |
| 4 | `docs/zh-CN/GLOSSARY.md` | 术语表 |
| 5 | `types.go` | 公共接口 |
| 6 | `session.go` | Agent 主循环 |
| 7 | `tools.go` | 内置工具 |
| 8 | `http.go` | HTTP 调用入口 |

## 设计取舍

| 目标 | 做法 |
|---|---|
| Go 最佳实践 | 用编译期注册 Handler，不动态加载 TypeScript 文件 |
| 安全优先 | `LocalEnv` 限制路径逃逸，HTTP 路由和请求体有限制 |
| 性能优先 | 内存沙箱/内存存储用于轻量任务，工具输出有截断，模型可流式输出 |
| 适合 AI Agent 后续开发 | 接口稳定、文档清晰、Guardrails、Tracing、Checkpoint、技能/角色/AGENTS 约定保留 |

## 更多中文资料

- [快速上手](docs/zh-CN/QUICKSTART.md)
- [架构说明](docs/zh-CN/ARCHITECTURE.md)
- [安全说明](docs/zh-CN/SECURITY.md)
- [AI Agent 扩展说明](docs/zh-CN/AI_AGENT_SUPPORT.md)
- [上游功能对齐说明](docs/zh-CN/UPSTREAM_PARITY.md)
- [术语表](docs/zh-CN/GLOSSARY.md)
- [完成审计](docs/zh-CN/COMPLETION_AUDIT.md)

## GitHub 仓库适配脚本

仓库已内置 GitHub Actions 和本地脚本，推送到 GitHub 后可以直接跑 CI 和 release 构建。

| 文件 | 作用 |
|---|---|
| `.github/workflows/ci.yml` | push/PR 时运行 gofmt 检查、测试、vet、build、CLI smoke。 |
| `.github/workflows/release.yml` | 推送 `v*` tag 时构建多平台 `fluego` 二进制并发布 Release。 |
| `scripts/check.ps1` / `scripts/check.sh` | 本地完整检查。 |
| `scripts/build.ps1` / `scripts/build.sh` | 本地多平台构建到 `dist/`。 |
| `Makefile` | Linux/macOS 下的快捷命令。 |

Windows：

```powershell
.\scripts\check.ps1
.\scripts\build.ps1
```

Linux/macOS：

```sh
./scripts/check.sh
./scripts/build.sh
```

## 开源协议

本项目使用 MIT License，详见 [LICENSE](LICENSE)。
