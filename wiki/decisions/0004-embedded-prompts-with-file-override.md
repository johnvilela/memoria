---
tags: [adr, prompts, config, override]
---

# ADR 0004: Prompts are embedded, overridable by file — not materialized

**Decision** (commit 6ea9c87 "refactor(prompt): stop materializing defaults, embed with file override"): the LLM prompts ship inside the binary and are no longer written out as default files on disk. To customize one, create a file with the same name in `~/.config/memoria/` and it takes precedence (README §How it works).

The prompt sources in the repo: `cmd/memoria/wiki-prompt.md` (batch consolidation), `cmd/memoria/digest-prompt.md` (per-session digest), plus `cmd/memoria/lint-prompt.md` and `cmd/memoria/seed-prompt.md` in the file listing. The README explicitly documents the override mechanism for the first two: "replaceable by creating a file with the same name in `~/.config/memoria/`".

The commit message implies the earlier approach *materialized* default prompt files to disk; the exact prior mechanics aren't described in the available sources beyond that verb.

Related constraint on how prompts reach the processor: they go over stdin, not argv — see [[gotchas/prompt-over-stdin-argv-limit]].