---
tags: [gotcha, handoff, subagent-stop, run]
---

# Handoff promoted a subagent's internal note to a user request

Hit for real on 2026-07-30 ([[sessions/e112664f-9954-4d8e-8fd2-a11e13d66bc0]]): the user quit a claude-code session and resumed it under codex via `memoria run`. The prior session's digest ended:

```
@stop 'Committed … left unstaged.'
@subagent-stop 'commit the wiki changes too'
@session-end reason: prompt_input_exit
```

The `@subagent-stop` line is an internal subagent annotation — the user never saw it (Claude Code fires `SubagentStop` for skills too; digests also show lines like `@subagent-stop '/git-commit'`). But `buildHandoff`'s lead scan walked backwards and took the first line matching either `@stop ` **or** `@subagent-stop ` — purely positional — so the packet read `Last reported state: @subagent-stop 'commit the wiki changes too'` followed by "Continue the work from exactly where the session stopped." Codex obeyed: five minutes of work the user never requested.

Scope of the defect, from recon:

- `memoria_recall` shared the lead — `resume=false` only dropped the trailing continue sentence, so recall answers carried the same misleading "last state".
- The lead is computed from the full `continues_from` chain **before** budget trimming, so it can even come from a different incarnation.
- `@session-end reason:` (e.g. `prompt_input_exit`) is recorded verbatim and consumed by no code path — the packet could not tell a deliberate user exit from a mid-turn crash.

**Fix**: commit `6414c80` — `fix(run): make handoff packet ask-first and prefer main @stop as lead`. The last main `@stop` always wins; `@subagent-stop` is only a fallback when the chain holds no `@stop`, labeled "internal subagent note — not a user request"; and the footer now tells the agent to report state in 1-3 lines and wait for the user's go-ahead instead of continuing. Current behavior in [[concepts/recall-and-run]].

**Reading digests after this**: an `@subagent-stop 'text'` line is a subagent's last message, not something the user typed or approved. The "deferred wiki commit" rescues in [[sessions/019fb0c0-23aa-7583-9035-ad2d71dd4ac6]] and [[sessions/019fb0d1-0ea2-71c3-a0c2-616d5973374c]], and the trailing "push it" lines in [[sessions/703f30e4-aabc-48d9-bf89-9bbd90d2428a]] and [[sessions/6d290df9-cc9e-4703-bcc4-2143eba007aa]], were all this same shape: `@subagent-stop` lines that packets forwarded as pending requests.