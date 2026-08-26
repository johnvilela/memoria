---
tags: [processor, config, cost]
---

Wiki work — seeding, digestion, consolidation, linting — is text processing with low reasoning demand. Memoria can use cheaper models than a human user would want for their own interactive agent. Commit `dc9c30f` added two config fields to let you choose:

```yaml
processor_model: sonnet        # or haiku/opus/gpt-5.4/gpt-5.4.mini/etc
processor_effort: medium        # codex only; claude has no effort flag
```

Defaults: `claude -p --model sonnet` and `codex exec -m gpt-5.4.mini -c model_reasoning_effort=high`. The config is read at runtime by every processor invocation. Omit the fields and the defaults apply.

**Why claude's default moved off haiku** (commit `6f653b9`, [[sessions/61bd82ab-125a-4915-90cc-bdd5135716df]]): haiku reliably mis-escaped `"` characters inside the JSON-embedded markdown bodies the consolidation contract requires, corrupting whole batches — see [[gotchas/processor-json-parse-failures]]. `processor_model: haiku` still selects the old default explicitly for anyone who wants the cheaper option now that malformed output gets a deterministic repair pass ([[decisions/0011-deterministic-json-repair-over-retry]]).

**Scope**: applies to all LLM tasks — seed wiki generation, session digest compilation, batch consolidation proposal, lint audit and auto-fix. Not to interactive `memoria run` (which launches your chosen agent binary with your chosen model via the agent's own CLI flags).

**Gemini processor**: hardcoded to gemini-2.5-flash (invokeGemini at processor.go:60-103, pure HTTP, no model arg). If you adopt Gemini, add `processor_model` support there.

Related: [[concepts/architecture-overview]] (processor section).