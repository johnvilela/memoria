---
tags: [adr, review-gate, safety, llm]
---

# ADR 0002: The LLM never writes files — proposal + review gate

**Decision** (README §How it works): "The LLM never writes files: you (or the agent, via `memoria_consolidate`) review `.memoria/proposal.json`, then apply writes the pages and archives the sessions."

The processor only *proposes* — a JSON list of 1-5 pages with wikilinks and tags. Writing happens in a separate, deterministic apply step: `memoria process --apply` on the CLI, or a second `memoria_consolidate` call with `apply=true` from an agent ([[concepts/mcp-server]]). Even MCP page writes go through memoria's own code: `memoria_write_page` validates the path and renders the tags frontmatter itself.

Lint follows the same shape (commit c5fb59b): findings first, then `--apply` (a second LLM pass) or `--deny "why"` with a reason future runs remember.

The gate has two sanctioned bypasses, both explicit opt-ins: `--cron-apply` (the timer applies proposals without review) and `--auto-apply` autopilot ([[decisions/0005-auto-apply-is-opt-in]]). The README notes "the review gate comes back the moment you turn it off."