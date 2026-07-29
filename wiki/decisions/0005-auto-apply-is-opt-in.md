---
tags: [adr, autopilot, defaults, review-gate]
---

# ADR 0005: Autopilot (--auto-apply) is off by default

**Decision** (commit 25ab844 "feat(config): add auto-apply autopilot for consolidation and lint"; README §Autopilot): `--auto-apply` is **off by default**. When enabled it removes every manual step:

- ending a session triggers consolidation by itself,
- proposals are written straight to the wiki without review,
- lint findings are fixed immediately.

"The review gate comes back the moment you turn it off" (README). The default therefore preserves [[decisions/0002-llm-never-writes-files]]: without explicit opt-in, a human or agent reviews `.memoria/proposal.json` before anything lands in `wiki/`.

Note the distinction from the related-but-narrower `--cron-apply`, which only makes the systemd timer's `process --all` runs apply proposals without review (README §Cron); `auto_apply` is a config-level switch (`~/.config/memoria/config.yaml`) covering consolidation *and* lint. Both are set via `memoria init` or reconfigured later via `memoria setup`.