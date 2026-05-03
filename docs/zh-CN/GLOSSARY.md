# 术语表

| 术语 | 中文解释 | 在代码中 |
|---|---|---|
| Agent | 一个可执行任务的智能体运行时 | `Agent` |
| Session | 一条有历史的对话或任务线 | `Session` |
| Harness | 把模型、工具、沙箱、历史组织起来的运行框架 | 整个 package |
| Model | 模型适配器，不绑定具体厂商 | `Model` |
| Tool | 模型可以调用的函数 | `Tool` |
| Env | 沙箱环境，控制文件和命令 | `Env` |
| LocalEnv | 把本地目录挂成沙箱 | `NewLocalEnv` |
| MemoryEnv | 内存里的虚拟文件系统 | `NewMemoryEnv` |
| RemoteEnv | 远程/container 沙箱包装 | `NewRemoteEnv` |
| SessionStore | 保存会话历史的存储 | `SessionStore` |
| Skill | Markdown 写的可复用能力说明 | `.agents/skills/*/SKILL.md` |
| Role | 模型在某次调用中的角色指令 | `roles/*.md` |
| AGENTS.md | 项目级 Agent 规则 | `AGENTS.md` |
| MCP | Model Context Protocol，远程工具协议 | `ConnectMCPServer` |
| Compaction | 把旧历史压缩成摘要 | `Compactor` |
| Webhook | HTTP 异步接收模式 | `X-Webhook: true` |
| SSE | Server-Sent Events，服务端事件流 | `Accept: text/event-stream` |
