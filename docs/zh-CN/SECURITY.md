# 安全说明

Agent Harness 的风险点通常不是“模型会不会回答错”，而是模型能不能读文件、执行命令、访问密钥、调用外部系统。Flue4Go 的设计默认把这些边界放在 Go 代码里，而不是交给 prompt。

## 当前防护

| 风险面 | 防护 |
|---|---|
| 本地路径逃逸 | `NewLocalEnv(root)` 会拒绝访问 root 外的路径 |
| HTTP 路由 | agent name 和 id 必须是受限字符 |
| HTTP 请求体 | JSON body 限制为 1 MiB |
| 工具输出 | read/grep/glob/bash 有数量或大小限制 |
| 密钥 | 核心 runtime 不保存模型 API key |
| 文件持久化 | `FileStore` 清洗 key，使用较严格文件权限 |
| MCP | MCP header 由可信 Go 代码传入，不暴露给 prompt |

## 使用建议

| 场景 | 建议 |
|---|---|
| 面向公网的 Agent | 默认只暴露 `Webhook: true` 的 handler |
| 需要跑 shell | 优先限制到专用目录，避免把用户主目录挂进去 |
| 需要访问密钥 | 在 Go handler 中注入，不写进 `AGENTS.md` |
| 自定义工具 | 参数必须校验，外部调用必须有权限控制 |
| 生产持久化 | 使用加密或访问受控的存储实现 `SessionStore` |

## 新手最容易踩的坑

| 坑 | 说明 |
|---|---|
| 把真实项目根目录直接暴露给不可信 Agent | Agent 可能读写超出你预期的文件 |
| 把 token 写进 Markdown | Markdown 会进入模型上下文 |
| 自定义工具不校验参数 | 模型可能传入意外参数 |
| shell 无超时 | 长命令可能卡住会话 |

## 后续可加强点

- shell 命令 allowlist/denylist
- 每个工具单独 timeout 和 byte budget
- 审计日志
- MCP server 权限策略
- 按租户隔离的生产 `SessionStore`
