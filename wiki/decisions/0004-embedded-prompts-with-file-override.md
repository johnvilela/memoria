---
tags: [adr, prompts, config, override]
---

# ADR 0004: Prompts are embedded, overridable by file — not materialized

**Decision** (commit 6ea9c87 "refactor(prompt): stop materializing defaults, embed with file override"): the LLM prompts ship inside the binary and are no longer written out as default files on disk. To customize one, create a file with the same name in `~/.config/memoria/` and it takes precedence (README §How it works).

The prompt sources live in `cmd/memoria/prompts/` since commit `c478df6` "refactor(cmd): move embedded prompts into prompts folder" ([[sessions/7facd470-c6e9-489b-b490-58832dabc6e2]]): `wiki-prompt.md` (batch consolidation), `digest-prompt.md` (per-session digest), `lint-prompt.md` and `seed-prompt.md`, each wired through a `//go:embed prompts/<name>` directive. The move deliberately left the `loadPromptFile` filenames alone — override files in `~/.config/memoria/` keep their flat names (`wiki-prompt.md`, not `prompts/wiki-prompt.md`). The README documents the override mechanism: "replaceable by creating a file with the same name in `~/.config/memoria/`".

The commit message of 6ea9c87 implies the earlier approach *materialized* default prompt files to disk; the exact prior mechanics aren't described in the available sources beyond that verb.

Related constraint on how prompts reach the processor: they go over stdin, not argv — see [[gotchas/prompt-over-stdin-argv-limit]].