---
tags: [gotcha, mcp, search, bootstrap]
---

# Unregistered project + no `@` selector: MCP search fails, and the calling agent can silently fall back to its own native search

Every memoria MCP call resolves the caller's current working directory to a tracked project first. A plain `memoria_search` query with no `@project`/`@all` selector depends on that resolution succeeding — if the calling folder was never registered via `memoria bootstrap`, the call errors with `not inside a tracked project (run memoria bootstrap first)` before the query even runs.

## Hit for real

A user had been running memoria since before the `update` command existed — her install was a manual clone of the memoria repo itself, and that clone's own folder had never been `bootstrap`-registered (only her actual project, CE, was in her `config.yaml`). When her agent (Claude Code) tried to search memoria for CE-project knowledge from the unregistered clone folder, `memoria_search` errored — and instead of surfacing that error, her Claude Code session **silently fell back to its own built-in conversation/transcript search** (visible in her transcript only as \"Queried session store\" — nothing to do with memoria). That fallback found a single local chat transcript and reported the CE wiki as having no other relevant knowledge. The wiki itself was fine the whole time — her own log showed 5 pages applied from 10 sessions.

memoria has no visibility into this: the failure is the *calling harness* silently absorbing a tool error and substituting its own search, not a bug memoria's code can detect or intercept.

## How to avoid or catch it

- Prefix queries with `@<project>` or `@all` — selectors skip the cwd-registration check entirely ([[concepts/cross-project-search]]).
- Make sure the folder you're chatting from is registered: `memoria bootstrap`.
- If a search result looks suspiciously empty for a wiki you know is populated, check whether the query actually reached memoria (an MCP tool-call error, if surfaced) versus a native fallback silently substituting for it.

## Compounding factor: outdated installs

This user's install predated `memoria update` ([[concepts/self-update-command]]), so even after registering the folder, an old manually-cloned binary can still miss later fixes and features — including `@project`/`@all` selectors, decay ([[concepts/session-decay]]), and the tokenized-search fix in [[gotchas/search-substring-only-false-negatives]] (the second, independent bug stacked on top of this one in the same incident). `git pull` + reinstall is the general recommendation for any install old enough to predate `update`.