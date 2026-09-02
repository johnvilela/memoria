---
tags: [gotcha, search, mcp, ranking]
---

# Search was pure substring match — multi-word queries returned false \"wiki has nothing\" negatives

`searchWiki` matched a query as one literal case-insensitive substring against page content. A multi-word phrase query that didn't appear verbatim in any page returned zero hits — even when the wiki held pages clearly relevant to the individual terms in that phrase.

## Hit for real

A user's agent asked memoria about CE-project decisions using natural multi-word phrases and got no matches back, then reported that the wiki had nothing on the topic. Her own debug session demonstrated the exact shape of the bug: a single-term query `decay` returned 7 matching pages, but the phrase query `decay session pages` — words that individually all appear across those same pages — returned 0 hits, because the exact phrase never occurs verbatim anywhere. The agent had no way to know the zero-hit result was an artifact of exact-phrase matching rather than genuine absence, so it concluded the memory was empty.

This is a real trust problem for a tool designed to be treated as project ground truth ([[concepts/mcp-server]]'s MCP trust rewrite instructs agents to search before non-trivial work and trust results) — a false \"nothing found\" is worse than a slow search, because the agent stops looking.

## The fix

Shipped as commit `feat(search): match multi-word queries as terms with ranked partial fallback` on branch `feat/tokenized-search`, version bumped **0.14.0 → 0.15.0**. `searchWiki` now tokenizes the query into terms instead of treating it as one literal string: pages matching all terms (AND) rank as the primary tier, with a ranked partial-term fallback when no page contains every term. Since `searchWiki` is the single shared function behind the CLI, MCP `memoria_search`, and the `@project`/`@all` selector paths ([[concepts/cross-project-search]]), the fix applies uniformly everywhere a query is run.

Built TDD: `cmd/search_test.go` gained `TestSearchWikiMultiWord`, `TestRunSearchMultiWord`, `TestRunSearchAllMultiWord`, and `TestRunSearchMultiWordAND` before the implementation landed in `cmd/search.go`/`cmd/mcp.go`.

## Related

[[gotchas/unregistered-project-search-silent-fallback]] — the other, independent cause stacked on top of this one in the same incident. [[concepts/cross-project-search]] — where `searchWiki`'s ranking is documented in full.