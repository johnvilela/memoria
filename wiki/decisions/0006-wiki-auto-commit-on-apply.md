---
tags: [adr, wiki, git, auto-commit]
---

**Decision** (commit `f0bf53a`): when memoria applies changes to the wiki (proposals, lint fixes, seed, session pages), automatically stage and commit the `wiki/` folder if it sits inside a git repository. Commit message follows a configurable pattern; failures are logged and never break the apply.

**Rationale**: The curated `wiki/` folder is [[decisions/0001-plain-markdown-no-db|meant to be committed with the project]]. Requiring users to manually commit after every proposal, lint fix, or session digest is friction. Auto-commit removes that friction — the wiki stays version-controlled without extra steps.

The pathspec commit (`git add wiki/`) keeps user's own staged non-wiki files untouched, preserving intent when they're mid-feature.

**Scope**: applies only to structured memoria writes — proposal apply, lint fix apply, bootstrap seed, session digest pages. MCP `write_page` and `delete_page` are excluded because those typically happen mid-session during active work, and belong in the user's own commit.

**Failure handling**: git errors (repo gone, permissions, etc.) are logged to the debug log, never block the apply, and don't surface to the user or agent — silently continuing is safer than failing a consolidation because `git add` failed.

**Configuration** (config.yaml):

Default message: `docs(wiki): {action} — {summary}`

Optional override:

```yaml
wiki_commit_message: "wiki: {action} [{count}] {project}"
```

Message customization is an escape hatch; the default handles most workflows.

Related: [[decisions/0001-plain-markdown-no-db]] (why the wiki is versioned), [[concepts/wiki-auto-commit]] (how it works).