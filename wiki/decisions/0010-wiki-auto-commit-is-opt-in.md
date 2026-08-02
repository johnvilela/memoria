---
tags: [adr, wiki, git, auto-commit, defaults]
---

# ADR 0010: Wiki auto-commit is opt-in, and `memoria commit` is the manual path

**Decision**: the auto-commit introduced by [[decisions/0006-wiki-auto-commit-on-apply]] is now gated on `wiki_auto_commit` (config, `memoria init --auto-commit` or the TUI prompt), **off by default**. A new `memoria commit` command covers the manual path: it stages and commits the project's wiki folder — modified pages *and* new untracked ones — with the same `docs(wiki): ...` message shape, `{action}` = `update`, or a `-m` subject.

**Rationale**: committing on every applied write takes the commit decision away from the user. Wiki pages land as a side effect of ending a session or running lint, so the commits arrive at moments the user didn't choose, interleaved with their own work. Ownership of the history belongs to whoever owns the repo; memoria offers the commit, it doesn't take it. Same reasoning as [[decisions/0005-auto-apply-is-opt-in]] — the automation exists, the default is the reviewed path.

**Consequence**: installs that predate the flag stop auto-committing. `wiki_auto_commit` is absent from their config, and the Go zero value is `false`. Accepted deliberately: re-running `memoria setup --auto-commit` restores the old behavior in one command.

**Shape**: `commitWiki` keeps its signature and becomes the gate (`if !cfg.WikiAutoCommit { return }`), so the four call sites — proposal apply, lint fix, seed, session digest — are untouched. The work moved to `commitWikiGit(wikiRoot, msg)` (returns errors instead of swallowing them, so the command can report and the auto path can keep logging-and-skipping) and `wikiCommitMessage(...)`. `memoria commit` calls those two directly, ignoring the gate: an explicit command always commits.

**Listing changes**: `wikiChanges` uses `git status --porcelain -z -uall` — `-uall` because git otherwise collapses a brand-new folder into a single `sessions/` entry, and the whole point is committing new pages; `-z` because it never quotes non-ASCII names. Porcelain paths are repo-root relative, so `rev-parse --show-prefix` is stripped off to get the wiki-relative paths the message summary expects.

Related: [[concepts/wiki-auto-commit]] (how it works), [[decisions/0006-wiki-auto-commit-on-apply]] (superseded default).
