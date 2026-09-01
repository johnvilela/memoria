---
tags: [search, mcp, cli, cross-project]
---

# Cross-project search: `@project` / `@all` selectors

Shipped on branch `feat/cross-project-search`, PR #12 ([[sessions/075393cf-e94c-4b79-a4c5-4feec580aa66]]). Version bumped **0.12.1 → 0.13.0**.

## The gap it closes

Before this, `memoria search` (CLI) and the MCP `memoria_search` tool both resolved exactly one workspace via `resolveWorkspace` ([[concepts/mcp-server]]) — the caller's own registered project, or `_global` as a fallback. With several projects registered, there was no way to search a sibling project's wiki from either surface.

## Syntax and resolution

Lead the query with `@<project-name>` tokens (repeatable) or `@all`:

- `memoria search @beta queue` / MCP `query: "@beta queue"` — search project `beta` by name.
- `@proj1 @proj2 term` — search multiple named projects.
- `@all term` — search every registered project; overrides other selectors; includes the `_global` pseudo-project ([[concepts/global-capture-mode]]) when global mode is on.
- An unknown project name errors, listing all known project names — usable by both a human and a self-correcting agent.
- No `@` prefix at all → unchanged: the existing single-workspace `resolveWorkspace` path, byte-identical output to before.

One parser, two callers: `splitSelectors`/`searchWorkspaces` (new, `cmd/search.go`) strip and resolve the leading `@` tokens against the project registry, shared by the CLI's `runSearch` and the MCP `mcpSearch` — the syntax and behavior are identical on both surfaces.

## Ranking

`searchWiki` was reshaped from a flat "does it contain the text" boolean into scored hits: score is the substring occurrence count (`strings.Count`), a `#tag` match scores 1; hits sort by score descending, then path ascending. When multiple workspaces are searched, all their hits merge into one ranked list, so the best match leads regardless of which project it came from.

## Output format

A selector search labels and lists hits as `project:path` — deliberately matching the wiki's own `[[project:page-path]]` wikilink vocabulary. A plain (no-selector) search keeps its old bare-path output unchanged, so existing scripts, agents, and the pinned non-TTY listing test are unaffected.

## MCP surface

`memoria_search`'s `matches` now always carry a `project` field (additive JSON field — every match names its source project, not just cross-project ones).

## Decay interaction

`touchLastUsed` ([[concepts/session-decay]]) stamps the *owning* project's wiki when a hit's content is delivered — each hit carries its own `wikiRoot`, so a cross-project hit refreshes the right wiki's `lastUsed`, not the caller's.

## Verification

TDD throughout (tests written and confirmed red first). Full suite green under `go vet`/`go test`. Live smoke test against the real registered-project config: `./memoria search @all memoria` returned a ranked cross-project list; `./memoria search @nope x` errored listing all 7 registered project names.

Related: [[concepts/mcp-server]] (the trust-rewrite that shipped as this PR's second commit), [[concepts/global-capture-mode]] (`_global`, reached via `@all`).