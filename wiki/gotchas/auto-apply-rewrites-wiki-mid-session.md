---
tags: [gotcha, auto-apply, wiki, background-jobs]
---

# Background auto-apply can rewrite wiki pages under an active session

Hit during [[sessions/67c500e5-dc86-4a64-8e79-a76444932b79]]: mid-session, `git diff --stat` showed four wiki pages modified — `index.md`, `concepts/recall-and-run.md`, `decisions/0004-embedded-prompts-with-file-override.md`, `sessions/7facd470-c6e9-489b-b490-58832dabc6e2.md` — that the session's own work never touched.

Cause: with `auto_apply` on, ending a session spawns a detached `memoria process` that writes straight to `wiki/` with no review gate ([[decisions/0005-auto-apply-is-opt-in]], [[concepts/consolidation-pipeline]]). One such job ran 23:03–23:07 — while this session was active in the same worktree — and per `~/.config/memoria/status.yaml` "applied 5 pages from 2 sessions".

Two consequences:

1. **Diff attribution.** Wiki diffs in the worktree may belong to the background consolidator, not the current session. Before explaining (or committing) them, check `~/.config/memoria/status.yaml` and the pages' mtimes (`stat -c '%y'`) against the session's own timeline — exactly how the mystery was solved here.
2. **Commit hygiene.** Feature commits must deliberately exclude the auto-applied pages: `d6edeea` staged only its six feature files and left the four rewritten pages plus an untracked auto-generated session page unstaged.