---
tags: [mcp, permissions, init, setup, trust]
---

# MCP auto-trust: `installTrust` allow-lists the memoria MCP server

Requested right after the tokenized-search fix shipped ([[sessions/b83a5def-41be-43be-82b5-902466d6a5e6]]): stop the agent asking for permission every time it calls memoria's MCP tools, \"especially the searches\" — friction against the MCP trust rewrite's own goal of searching before non-trivial work ([[concepts/mcp-server]]).

## What it does

New `installTrust(settingsPath string) error` in `cmd/hooks.go`: merges `\"mcp__memoria\"` into the `permissions.allow` list of a Claude Code `settings.json` file. `mcp__memoria` is Claude Code's allow-list syntax for trusting an entire MCP server — every tool the memoria server exposes, not one at a time. Other keys (e.g. `model`) and other permission rules (existing `allow` entries, `deny` entries) are preserved untouched; calling it twice does not duplicate the entry — confirmed by `TestInstallTrustFresh` (a fresh settings file gets `allow: [\"mcp__memoria\"]`) and `TestInstallTrustPreservesAndDedupes` (an existing `Bash(ls:*)` allow rule and `WebFetch` deny rule survive two calls, `mcp__memoria` appears exactly once).

## Wiring

Called from both `memoria init` and `memoria setup` (6 edits to `init.go`, 2 to `setup.go`) — trust is granted automatically at install/setup time, alongside the existing hook and MCP registration flow ([[concepts/init-setup-multi-agent]]), with no separate flag to opt into it. This is a departure from this codebase's usual opt-in-by-default posture for automation (compare [[decisions/0005-auto-apply-is-opt-in]], [[decisions/0010-wiki-auto-commit-is-opt-in]]) — granting MCP trust is not gated behind a flag.

Recon also checked the MCP Go SDK for `ToolAnnotations`/`ReadOnlyHint` support before landing on the settings.json approach, and the `memoria_search`/`memoria_recall` tool registrations in `mcp.go` picked up two further edits afterward — the exact change isn't visible in capture (Write/Edit events record only the file path, not the diff), so whether tool annotations ended up part of the shipped fix or were investigated and set aside isn't recorded.

## Status

Version bumped **0.15.0 → 0.16.0**; AGENTS.md updated. Committed as `fix(mcp): respawn consolidate past stale done status; auto-allow memoria tools on init` on branch `fix/mcp-consolidate-trust` (branched from the `feat/tokenized-search` checkout, per capture — no intervening switch back to `main` was recorded), bundled with the [[gotchas/mcp-consolidate-stale-done-status]] fix in the same commit. No PR was opened for this branch within the session; capture ends with the binary rebuilt right after the commit.

## Related

[[concepts/mcp-server]] (the seven tools and the trust rewrite this extends), [[concepts/init-setup-multi-agent]] (the install/setup flow this is wired into), [[gotchas/mcp-consolidate-stale-done-status]] (the bug fixed in the same commit).