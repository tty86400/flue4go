# 中文资料入口

这里是 Flue4Go 的中文资料目录。目标是让第一次接触 Agent Harness 的 Go 开发者，也能按顺序读完并写出第一个可运行 Agent。

## 推荐阅读路径

```text
README.zh-CN.md
  |
  v
QUICKSTART.md
  |
  v
ARCHITECTURE.md
  |
  +--> SECURITY.md
  +--> AI_AGENT_SUPPORT.md
  +--> UPSTREAM_PARITY.md
  +--> GLOSSARY.md
```

| 文档 | 用途 |
|---|---|
| `QUICKSTART.md` | 从零跑 CLI、写 HTTP Agent、接模型 |
| `ARCHITECTURE.md` | 理解 Registry、Agent、Session、Env、Tool 的关系 |
| `SECURITY.md` | 理解沙箱、路径限制、HTTP 限制和密钥边界 |
| `AI_AGENT_SUPPORT.md` | 给后续 AI coding agent 的扩展约定 |
| `UPSTREAM_PARITY.md` | 对照上游 TypeScript Flue 的功能面 |
| `GLOSSARY.md` | 新手术语表 |
| `COMPLETION_AUDIT.md` | 当前目标和验证命令 |
