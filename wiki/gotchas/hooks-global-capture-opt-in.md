---
tags: [gotcha, hooks, bootstrap, opt-in]
---

# Hooks are global, but capture is opt-in per project

From README §How it works: "Hooks are global, but only projects registered with `memoria bootstrap` are captured; everything else is ignored."

Two surprises, one in each direction:

1. **After `memoria init`, nothing is captured yet.** Init installs the hooks and MCP server machine-wide, but sessions in a project produce no digests until you run `memoria bootstrap` inside it ([[concepts/recall-and-run]]). If digests aren't appearing under `.memoria/sessions/pending/`, check the project is in the `projects` list in `~/.config/memoria/config.yaml`.
2. **The hooks do run everywhere.** `memoria hook <event> --client <name>` fires on every session event in every project — untracked projects are just filtered out (and the hooks never block the agent either way, per [[decisions/0003-never-block-the-agent]]).

Historical wrinkle: responsibilities have shuffled between `init` and `bootstrap` over time — gitignoring `.memoria` appears in both a bootstrap commit (5a2f216) and a later init commit (8b34990), and wiki seeding moved from init to bootstrap (commit 37a7a77). The README's current word is that `bootstrap` gitignores `.memoria/`; trust the README over old commit subjects when they disagree.