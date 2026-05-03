# 架构说明

Flue4Go 保留上游 Flue 的核心心智模型：Agent、Session、Sandbox、Tools、Skills、Roles、HTTP invocation。但 Go 版本采用编译期注册和显式接口，不做 TypeScript bundler 动态生成。

## 总体结构

```text
HTTP request
  |
  v
+----------------+
| Registry       |  注册 agent 名称和 handler
+-------+--------+
        |
        v
+----------------+
| Handler        |  你的业务入口
+-------+--------+
        |
        v
+----------------+
| Agent          |  管理模型、沙箱、上下文、会话
+---+---+---+----+
    |   |   |
    |   |   +----------------+
    |   |                    v
    |   |              Model adapter
    |   |                    |
    |   v                    v
    | Env sandbox       provider call
    |
    v
SessionStore
```

## 核心对象

| 对象 | 作用 | 新手理解 |
|---|---|---|
| `Registry` | HTTP agent 注册表 | 告诉服务有哪些 agent |
| `Agent` | 运行时容器 | 管模型、工具、沙箱、上下文 |
| `Session` | 一条对话/任务线 | 保存消息历史，循环调用模型和工具 |
| `Model` | 模型适配器 | 你接 OpenAI/本地模型/内部网关的地方 |
| `Env` | 沙箱 | 控制 Agent 能操作的文件和命令 |
| `Tool` | 工具 | 模型可以调用的函数 |
| `SessionStore` | 存储 | 保存会话历史 |
| `Compactor` | 历史压缩器 | 历史太长时总结旧消息 |

## Session 调用链

```text
Session.Prompt("任务")
  |
  +-- 把用户消息追加到历史
  +-- 组装 system prompt、roles、skills、tools
  +-- 调用 Model.Generate
  +-- 如果模型要求 tool call：
  |     |
  |     +-- 在 Env 中执行工具
  |     +-- 把工具结果追加到历史
  |     +-- 再次调用模型
  |
  +-- 模型返回最终文本
  +-- 保存 SessionData
```

## 与上游 Flue 的关键差异

| 上游 TypeScript Flue | Flue4Go |
|---|---|
| 扫描 `.ts` agent 文件并 bundle | Go 代码里用 `Registry.Handle()` 注册 |
| Node/Cloudflare build plugin | 用 `go build` 输出二进制 |
| Hono server | 标准库 `net/http` |
| just-bash 虚拟沙箱 | `MemoryEnv` / `LocalEnv` / `RemoteEnv` |
| Valibot schema | Go struct + `PromptInto` / `SkillInto` |

差异的原因：Go 更适合显式接口、编译期检查和单二进制部署。
