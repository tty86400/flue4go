# Security Notes

中文版本：[docs/zh-CN/SECURITY.md](zh-CN/SECURITY.md)

Security is a first-class design axis because agent harnesses routinely handle filesystem, shell, network, and secret boundaries.

## Current Guardrails

| Surface | Guardrail |
|---|---|
| Local filesystem | `NewLocalEnv(root)` rejects path traversal outside `root`. |
| HTTP routing | Agent names and ids must match a constrained route identifier pattern. |
| HTTP body | JSON request bodies are capped at 1 MiB. |
| Tool output | Read, grep, glob, bash results are bounded or truncated. |
| Runtime guardrails | `Guardrail` can block or sanitize input, output, and tool calls. |
| Sensitive tools | `Tool.RequiresApproval` pauses execution before side effects. |
| Auditability | `Tracer` and checkpoints expose model/tool/approval events for review. |
| Model provider | Core runtime receives a `Model` interface and does not store API keys. |
| File persistence | `FileStore` sanitizes session keys and writes JSON with restrictive permissions. |
| MCP | MCP headers are supplied by trusted Go code, not prompt-visible context. |

## Operator Responsibilities

| Area | Recommendation |
|---|---|
| Shell access | Prefer `MemoryEnv` for high-scale agents and expose `LocalEnv` only to trusted workflows. |
| Secrets | Inject secrets in trusted Go handler code, not in AGENTS.md or skills. |
| Custom tools | Validate names, arguments, and authorization before calling external systems; mark sensitive tools with `RequiresApproval`. |
| Persistence | Use encrypted or access-controlled stores for production session history, run state, checkpoints, and approvals. |
| Guardrails | Keep business-specific policies in deterministic or model-backed `Guardrail` implementations. |

## Future Hardening

- Allowlist/denylist policy for shell commands.
- Per-tool timeout and byte budgets.
- MCP server authorization policy.
- External tracing/exporter adapters.
- Distributed lease/retry support for production durable execution.
