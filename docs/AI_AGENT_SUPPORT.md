# AI Agent Support

中文版本：[docs/zh-CN/AI_AGENT_SUPPORT.md](zh-CN/AI_AGENT_SUPPORT.md)

Flue4Go is designed so future coding agents can extend it without reverse-engineering hidden conventions.

## Stable Extension Points

| Extension | File/API |
|---|---|
| Model provider | Implement `Model.Generate`. |
| Sandbox connector | Implement `Env`. |
| Remote sandbox connector | Implement `SandboxAPI` and wrap it with `NewRemoteEnv`. |
| Persistence | Implement `SessionStore`. |
| Domain tool | Provide `Tool{Name, Description, Parameters, Execute}`. |
| HTTP agent | Register with `Registry.Handle`. |
| Skill pack | Add `.agents/skills/<name>/SKILL.md`. |
| Role | Add `roles/<name>.md`. |
| History compaction | Implement `Compactor`. |

## Agent Development Checklist

1. Add a focused test for the desired runtime behavior.
2. Extend the smallest interface that owns that behavior.
3. Keep provider-specific code outside the core package.
4. Run `go test ./...`.
5. Update docs when changing public APIs.

## Workspace Convention

```text
workspace/
  AGENTS.md
  roles/
    reviewer.md
  .agents/
    skills/
      triage/
        SKILL.md
```

This mirrors upstream Flue and keeps prompts, skills, and roles editable by humans and agents.
