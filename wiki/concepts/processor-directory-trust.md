---
tags: [processor, codex, claude, git-trust]
---

The CLI processors (`claude -p` and `codex exec`) are invoked from `invokeProcessor` (processor.go:23-38) at the tail of every LLM pipeline step: seed, digest, consolidation, lint. They receive the prompt via stdin (no argv limit, [[gotchas/prompt-over-stdin-argv-limit]]) and run in a subprocess controlled via `cmd.Dir` and environment.

**Why not the project directory?** The prompt is self-contained — `buildSeedPrompt` and other builders run `git -C <dir>` themselves to embed the history/tree/README, so the subprocess never reads the filesystem. Running in the project would expose the repo structure to the model, which may or may not be desired, and complicates testing (temporary projects don't have `.git`).

**The working directory and recursion guard** (processor.go:44-58):

Original design: all processors ran in `os.TempDir()` + `MEMORIA_NO_CAPTURE=1` env var (recursion guard — so memoria's own hooks don't capture the nested session).

Breakage (commit `dc9c30f`): codex has a security model that refuses to run outside git repos or OS-level trusted paths; temp dir fails. Claude tolerates it.

**Fixed behavior**: thread the project directory through the call chain. At runtime, `hasGitDir(dir)` checks if `<dir>/.git` exists. If yes, codex runs there (codex trusts git repos); if no, use temp dir + `--skip-git-repo-check`. Claude always uses temp dir (unaffected).

Both get `MEMORIA_NO_CAPTURE=1` — the env var is still the primary recursion guard; the cwd is now a secondary lock (if codex runs in a repo, its own hooks won't capture because memoria is looking for the project in the registry, and a temp cwd wouldn't match).

Multirepo parent projects: a parent registered without `.git` runs codex in temp dir with `--skip-git-repo-check`. Child repos with `.git` aren't invoked directly; the parent's bootstrap/seed/process handle everything.

Related: [[concepts/architecture-overview]] (processor invocation), [[gotchas/codex-refuses-untrusted-directories]] (the original bug).