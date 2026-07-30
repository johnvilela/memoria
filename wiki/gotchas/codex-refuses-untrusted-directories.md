---
tags: [gotcha, codex, processor, git-trust]
---

From [[sessions/c133e40b-8547-4a99-bc98-d14b7029ccfe]]: `codex exec` has a security model that refuses to run in directories outside a git repo or a hardcoded OS-level trusted-path list. Unlike `claude -p`, which tolerates any cwd, codex fails with "Not inside a trusted directory" when memoria tries to run it in `/tmp`.

Memoria's processor used `/tmp` as a recursion guard — to prevent nested agent sessions (the codex/claude subprocess itself) from capturing back into memoria's own hooks. This works fine for Claude. For Codex, the fix (commit `dc9c30f`) was to detect if the project root has `.git`, run codex there (codex trusts git repos natively), and only fall back to temp dir + `--skip-git-repo-check` when there's no `.git`.

The project directory was always safe to expose to codex: the processor's prompt is built by `buildSeedPrompt` (seed.go) and other builders that shell out to `git -C <dir>` themselves and embed the results as text — the subprocess receives complete context via stdin and never needs to read the filesystem. The `MEMORIA_NO_CAPTURE=1` env var (still in place) remains the primary recursion guard; the cwd is now a secondary lock (codex runs *in* a repo, so its own hooks won't capture anyway — double-checked).

Applies to all LLM call sites: seed, digest, process, lint. Codex was broken in all of them, not just bootstrap.

Related: [[concepts/processor-directory-trust]] (how the fix works).