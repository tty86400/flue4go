# AI Agent 扩展说明

Flue4Go 的代码和文档刻意为后续 AI coding agent 留了稳定入口。目标是：未来的 Agent 不需要猜架构，只要按固定接口扩展。

## 稳定扩展点

| 想扩展什么 | 应该改哪里 |
|---|---|
| 新模型提供商 | 实现 `Model.Generate` |
| 新沙箱 | 实现 `Env` |
| 远程沙箱 | 实现 `SandboxAPI`，再用 `NewRemoteEnv` 包装 |
| 新持久化 | 实现 `SessionStore` |
| 新业务工具 | 新增 `Tool` |
| 新 HTTP agent | `Registry.Handle()` |
| 新技能 | `.agents/skills/<name>/SKILL.md` |
| 新角色 | `roles/<name>.md` |
| 历史压缩 | 实现 `Compactor` |

## AI Agent 修改代码的规则

1. 先写能失败的测试，锁住行为。
2. 优先扩展接口，不把 provider 代码塞进核心 runtime。
3. 不要削弱 `LocalEnv` 的路径限制。
4. 工具输出必须有边界。
5. 修改公共 API 时同步更新中文和英文文档。

## 推荐工作流

```text
读 README.zh-CN.md
  |
  v
读 docs/zh-CN/ARCHITECTURE.md
  |
  v
定位扩展接口
  |
  v
写测试
  |
  v
实现最小代码
  |
  v
go test ./... && go vet ./...
```

## 给 Agent 的上下文文件

```text
AGENTS.md                      项目级规则
.agents/skills/*/SKILL.md      可复用技能
roles/*.md                     角色指令
docs/zh-CN/*.md                中文导读
```
