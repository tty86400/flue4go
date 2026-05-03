---
name: framework-maintainer
description: Maintain Flue4Go with security, performance, and Go API stability in mind.
---

When changing Flue4Go:

1. Start from a failing test that exercises public behavior.
2. Keep model-provider, sandbox-provider, and persistence-provider code behind interfaces.
3. Do not weaken `LocalEnv` path confinement.
4. Bound tool output and request input sizes.
5. Update `docs/UPSTREAM_PARITY.md` when changing upstream coverage.
