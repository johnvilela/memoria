---
tags: [wiki, git, auto-commit]
---

When memoria applies changes to the wiki (consolidation proposals, lint fixes, bootstrap seed, session digests), it now automatically commits them if the wiki folder sits inside a git repository. This keeps your wiki history clean without manual commit steps.

**How it works** (commit `f0bf53a`, wikigit.go):

After cada memoria writes or deletes wiki pages, `commitWiki(cfg, wikiRoot, action, summary, fileCount, projName)` stages the `wiki/` folder and commits it. Staging uses pathspec (`git add wiki/`) so your own staged non-wiki files stay staged and untouched — the wiki commit goes in, your other staged work stays ready for your own commit.

**When it commits**:
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

Customize the template:

```yaml
wiki_commit_message: "wiki: {action} [{count}] {project}"
```

Placeholders: `{action}` (apply/fix/seed/digest), `{summary}` (reason/file list), `{count}` (page count), `{project}` (project name).

**Edge cases**:
- Wiki not in a git repo → skipped silently (the wiki folder still writes, no commit)
- Git command fails → logged, never breaks the apply
- No `.git` at wiki root, but one in an ancestor → detected and committed (respects parent repos, so multirepo setups work)
- No push — commits stay local

Related: [[decisions/0006-wiki-auto-commit-on-apply]].