---
tags: [wiki, git, auto-commit]
---

When memoria applies changes to the wiki (consolidation proposals, lint fixes, bootstrap seed, session digests), it can commit them automatically if the wiki folder sits inside a git repository. **Opt-in since [[decisions/0010-wiki-auto-commit-is-opt-in]]** — off unless `wiki_auto_commit: true` (`memoria init --auto-commit`, or the TUI prompt). Without it the wiki is left dirty for `memoria commit` or your own commit.

**How it works** (wikigit.go):

`commitWiki(cfg, wikiRoot, action, summary, count)` returns immediately unless `cfg.WikiAutoCommit`; otherwise it stages the wiki folder and commits it. Staging uses pathspec (`git add -A .` with cwd = the wiki root) so your own staged non-wiki files stay staged and untouched — the wiki commit goes in, your other staged work stays ready for your own commit.

`memoria commit` shares the same machinery (`commitWikiGit` + `wikiCommitMessage`) but ignores the config gate: an explicit command always commits.

**When it commits** (with `wiki_auto_commit: true`):
- Proposal apply (manual `--apply`, auto-apply, cron, MCP consolidate)
- Lint fix apply
- Bootstrap wiki seed
- Session digest page write

MCP `write_page` and `delete_page` are deliberately excluded — those typically happen mid-session while you're actively working, so they ride in your own commit instead.

**Message template** (config.yaml):

Default: `docs(wiki): {action} — {summary}`

Example outputs:
- `docs(wiki): apply proposal — 3 page(s) (index.md, concepts/queue.md, …)`
- `docs(wiki): fix lint findings — 2 page(s)`
- `docs(wiki): seed wiki — initial commit`
- `docs(wiki): update — 2 page(s) (gotchas/x.md, index.md)` — `memoria commit`

Customize the template:

```yaml
wiki_commit_message: "wiki: {action} [{count}] {project}"
```

Placeholders: `{action}` (apply/fix/seed/digest), `{summary}` (reason/file list), `{count}` (page count), `{project}` (project name).

**Edge cases**:
- `wiki_auto_commit` unset → no automatic commit at all (the default)
- Wiki not in a git repo → skipped silently (the wiki folder still writes, no commit); `memoria commit` says so and exits 1
- Git command fails → logged, never breaks the apply
- No `.git` at wiki root, but one in an ancestor → detected and committed (respects parent repos, so multirepo setups work)
- No push — commits stay local

Related: [[decisions/0006-wiki-auto-commit-on-apply]], [[decisions/0010-wiki-auto-commit-is-opt-in]].