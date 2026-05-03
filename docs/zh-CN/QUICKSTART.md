# 快速上手

这份文档从“完全不知道 Flue4Go”开始，带你跑通一个最小 Agent。

## 1. 确认环境

需要 Go 1.22 或更高版本。

```powershell
go version
go test ./...
```

## 2. 初始化工作区

```powershell
go run ./cmd/fluego init --workspace .flue
```

检查工作区内容：

```powershell
go run ./cmd/fluego inspect --workspace .flue
```

你会看到 skills 和 roles 被识别出来。

## 3. 启动示例服务

```powershell
go run ./cmd/fluego serve-example --addr :3000
```

另开终端：

```powershell
go run ./cmd/fluego list --url http://localhost:3000
go run ./cmd/fluego run echo --id demo --url http://localhost:3000 --payload '{"text":"hello"}'
```

## 4. 写自己的 HTTP Agent

新建一个 Go 程序：

```go
package main

import (
	"context"
	"net/http"

	flue "github.com/xwlv/flue4go"
)

func main() {
	registry := flue.NewRegistry()

	registry.Handle("support", flue.Triggers{Webhook: true}, func(ctx context.Context, req flue.RequestContext) (any, error) {
		message, _ := req.Payload["message"].(string)
		return map[string]any{
			"reply": "收到问题：" + message,
		}, nil
	})

	_ = http.ListenAndServe(":3000", flue.NewHTTPServer(registry, flue.HTTPServerOptions{}))
}
```

调用：

```powershell
go run ./cmd/fluego run support --id u001 --url http://localhost:3000 --payload '{"message":"如何重置密码？"}'
```

## 5. 接入模型 Session

`Registry` 负责 HTTP 入口，`Agent/Session` 负责真正的模型循环。

```go
model := flue.ModelFunc(func(ctx context.Context, req flue.ModelRequest) (flue.ModelResponse, error) {
	return flue.ModelResponse{Content: "这是模型返回内容"}, nil
})

agent, err := flue.NewAgent(ctx, flue.AgentConfig{
	ID:        "assistant",
	Model:     model,
	ModelName: "local/mock",
	Env:       flue.NewMemoryEnv(),
})
if err != nil {
	return err
}

session, err := agent.Session(ctx, "demo")
if err != nil {
	return err
}

resp, err := session.Prompt(ctx, "请总结这个问题")
```

## 6. 什么时候用哪个 Env

| Env | 适合场景 |
|---|---|
| `NewMemoryEnv()` | 快速、高并发、无真实文件依赖 |
| `NewLocalEnv(root)` | CI、代码仓库分析、需要真实文件 |
| `NewRemoteEnv(api, cwd, cleanup)` | Docker、Daytona、SSH、K8s 等远程沙箱 |

## 7. 最小排错表

| 现象 | 先看哪里 |
|---|---|
| skill 找不到 | `.agents/skills/<name>/SKILL.md` 是否存在 |
| role 找不到 | `roles/<name>.md` 是否存在 |
| 本地文件读不到 | `NewLocalEnv(root)` 的 root 是否正确 |
| HTTP 404 | agent 是否注册，是否 `Webhook: true` |
| 模型不调用工具 | `ModelRequest.Tools` 是否传给模型 provider |

## 8. GitHub CI 和发布

推送到 GitHub 后，`.github/workflows/ci.yml` 会自动检查：

```text
gofmt -> go test -> go vet -> go build -> CLI smoke
```

发布多平台 CLI：

```powershell
git tag v0.1.0
git push origin v0.1.0
```

也可以本地先构建：

```powershell
.\scripts\build.ps1
```
