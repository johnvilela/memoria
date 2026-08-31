---
tags: [gotcha, prompts, config, upgrade]
---

# Old materialized prompt files silently pin your prompts

Before commit 6ea9c87, memoria *materialized* default prompt files into `~/.config/memoria/` on first use. Since that refactor, prompts ship embedded in the binary and a file at `~/.config/memoria/<name>-prompt.md` acts as an **override** ([[decisions/0004-embedded-prompts-with-file-override]]).

The trap: a machine that ran the old version still has `wiki-prompt.md` and `seed-prompt.md` sitting in the config dir from the materialize era. They now count as overrides — pinning the prompt text as of the day they were written and silently ignoring improved embedded defaults after every binary upgrade.

Fix on such a machine: `diff` each copy against the embedded source (`cmd/<name>-prompt.md`) to confirm it carries no real edits, then delete it:

```
rm ~/.config/memoria/wiki-prompt.md ~/.config/memoria/seed-prompt.md
```

On the dev machine where this bit ([[sessions/85a0d12d-60b4-4442-b107-f40c097122d8]]), both files were verified byte-identical to the embedded defaults before removal was recommended. Keep a copy only if the diff shows deliberate customization — that's exactly what the override path is for.