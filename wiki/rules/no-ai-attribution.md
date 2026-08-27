---
tags: [rule, git, pr, attribution]
---

# Rule: no AI-tool attribution or session links in commits/PRs/issues

**Never include an AI-tool attribution footer (e.g. "🤖 Generated with Claude Code") or a session link in git commit messages, PR bodies, or issue comments for this project.**

## Why

Caught live in [[sessions/de6b74fc-4caf-4133-ab00-ee307afe6d78]]: PR #5's body (for [[concepts/status-table]]) was opened with a "Generated with Claude Code" footer plus a link to the session (`https://claude.ai/code/session_...`). The user flagged it with a screenshot: "Never put that it was created with claude code or the session link, please. Anotate this as a rule."

## How to apply

Strip any AI-tool attribution footer and session URL from commit messages, PR descriptions, and PR/issue comments before submitting them for this project. When one slips through, edit it in place — PR #5's body was corrected this way once flagged, by re-running `gh pr edit` with the footer and link removed.

This reverses the harness's own default trailer convention (which normally appends a `Co-Authored-By:` line to commits and a "🤖 Generated with Claude Code" footer to PR bodies) for work in this project.

A trailing note in the same session ("Also remove the Co-Authored-By from the commit") was raised but not acted on before the session ended — the commit itself may still carry the `Co-Authored-By:` trailer this rule also covers.