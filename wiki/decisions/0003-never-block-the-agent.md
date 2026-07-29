---
tags: [adr, background-jobs, detach, status]
---

# ADR 0003: Background everything — never block an active agent session

**Decision**: LLM-driven work runs detached; the interactive path (agent hooks, MCP tool calls, CLI invocations) always returns immediately.

Stated rationale in README §How it works: `memoria process` "detaches and returns immediately (the processor call can take minutes and must never block an active agent session)". The same section says plainly: "Hooks never block the agent."

How it shows up across the codebase:

- `memoria process` runs detached with status tracking and desktop notifications via notify-send (commit 899cef2); `memoria status` shows running / done / error per project.
- MCP long-running tools (digest, consolidate, lint) start a detached job on first call and poll on later calls — the same one-job-per-project tracking `memoria status` shows (README §MCP server, [[concepts/mcp-server]]).
- `memoria lint` audits in the background (commit c5fb59b); `memoria bootstrap --background` detaches wiki seeding (commit 37a7a77).
- Background work logs to a debug log at `~/.config/memoria` (commit de5dfee), since there is no terminal to print to.

Opt-outs exist where a human is watching: `process --foreground` and `process --inspect` (commit 5c50d83).