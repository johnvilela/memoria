---
tags: [gotcha, processor, stdin, limits]
---

# Processor prompt must go over stdin — argv has a size limit

From commit a04b554 "fix(processor): send prompt via stdin to avoid argv size limit" and the debugging session behind it ([[sessions/2f4ed960-d4d1-4d74-87e8-7b4104179fb4]]).

**How it failed in practice**: a `memoria process` run picked up 2 sessions and died with `claude: fork/exec: argument list too long` — E2BIG from `execve`, visible only in `~/.config/memoria/memoria.log`. Linux caps a *single argv element* at 128 KiB (`MAX_ARG_STRLEN`). The consolidation prompt bundles the wiki prompt + the full current wiki + the ended session digests (README §How it works) — at the time of the failure, a 20-page wiki plus a 119 KB digest — and was passed as one argv element, so the processor binary never even started.

**The fix**: `runProcessorCmd` sends the prompt via stdin (`cmd.Stdin = strings.NewReader(prompt)`). `claude -p` reads stdin natively; codex is invoked as `codex exec -` (`-` = read stdin). Stdin has no size ceiling.

**Regression guard** in `processor_test.go`:

- `TestRunProcessorCmdLargePromptViaStdin` — 300 KB prompt through `cat`, the exact failure scenario
- `TestRunProcessorCmdPromptNotInArgv` — prompt absent from argv even when small, so the regression can't sneak back
- `TestRunProcessorCmdReportsStderr` — error path keeps stderr detail

If you touch how `cmd/memoria/processor.go` invokes a processor (claude-code | codex | ollama | gemini), keep the prompt on stdin — an argv regression only breaks once the wiki and session backlog are big enough, which is exactly when it's hardest to notice in a quick test. Related prompt plumbing: [[decisions/0004-embedded-prompts-with-file-override]].