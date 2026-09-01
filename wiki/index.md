---
tags: [index]
---

Start with [[concepts/architecture-overview]] for the pipeline end to end.

## Concepts — how it works

- [[concepts/architecture-overview]] — capture → consolidate → recall pipeline, config, processors
- [[concepts/session-capture]] — hooks, live digests, session titles, implicit session end, incarnations
- [[concepts/consolidation-pipeline]] — `process`, JSON proposal, lint, cron timer
- [[concepts/mcp-server]] — the seven MCP tools agents get; `memoria_recall` is the read-only recall path; server-level Instructions + trust-rewritten tool descriptions since PR #12 (v0.13.0)
- [[concepts/cross-project-search]] — `@project`/`@all` selectors let `memoria search` and MCP `memoria_search` reach sibling projects' wikis — shipped PR #12, v0.13.0
- [[concepts/init-setup-multi-agent]] — multi-agent hook/MCP install, the `clients:` registry in config
- [[concepts/recall-and-run]] — bootstrap, AGENTS.md recall block, `run`'s session picker + cross-harness handoff packet (ask-first since `6414c80`), `search`
- [[concepts/multirepo-parent-project]] — how memoria behaves on a multirepo parent folder; the eparts verdict — adaptations shipped as `31b7251`
- [[concepts/global-capture-mode]] — `bootstrap --global`/`--global-path` and `setup --global`: capture sessions in unregistered folders into a `_global` pseudo-project — shipped `8e495d5`
- [[concepts/handoff-vs-ai-memory]] — research: ai-memory's deterministic workstream handoff vs memoria's — spawned the packet handoff (`ad5d206`)
- [[concepts/processor-directory-trust]] — per-provider cwd handling for the CLI processors (codex, claude); git-trust fix `dc9c30f`
- [[concepts/processor-models-and-effort]] — configurable model/effort for cheaper wiki work (sonnet/gpt-5.4.mini by default; claude moved off haiku after `6f653b9`)
- [[concepts/wiki-auto-commit]] — how wiki commits are built; opt-in for applies, always for `memoria commit`
- [[concepts/queue-write-safety]] — file locking and atomic operations protect queue and status files from concurrent writes
- [[concepts/ci-release-pipeline]] — GitHub Actions CI/release pipeline: version-check gate, auto-tag + auto-release on merge, gotestsum vitest-style test output — shipped v0.8.0/v0.8.1
- [[concepts/self-update-command]] — `memoria update`: checks GitHub releases, checksum-verifies, self-replaces the running binary; release binaries trimmed ~15MB→~10MB via `-ldflags="-s -w"` — shipped on `feat/update-command`, no PR yet
- [[concepts/status-table]] — `memoria status` renders as a borderless lipgloss table instead of prose lines — shipped on `feat/status-table`, PR #5 (open, not approved), version bumped to 0.10.0
- [[concepts/session-decay]] — `lastUsed` date stamps on `sessions/` pages: deterministic touch-on-delivery, cron-swept soft-delete (15d) / hard-delete (30d) — shipped `82490e0`, PR #10 (open, not approved), version bumped to 0.12.0
- [[research/ai-memory-workstream-comparison]] — external (chatgpt) deep-dive on ai-memory v1.19.2 that fed the `run` rework

## Decisions — why it's shaped this way

- [[decisions/0001-plain-markdown-no-db]] — plain `.md` in the repo, versioned with the project
- [[decisions/0002-llm-never-writes-files]] — proposal + review gate
- [[decisions/0003-never-block-the-agent]] — LLM-driven work runs detached
- [[decisions/0004-embedded-prompts-with-file-override]] — prompts ship in the binary (`cmd/memoria/prompts/`), a config file overrides
- [[decisions/0005-auto-apply-is-opt-in]] — autopilot off by default
- [[decisions/0006-wiki-auto-commit-on-apply]] — applied wiki changes auto-commit when inside a git repo (default reversed by 0010)
- [[decisions/0007-queue-safety-via-file-locking]] — file locking for queue writes, not append-only redesign
- [[decisions/0010-wiki-auto-commit-is-opt-in]] — auto-commit off by default; `memoria commit` is the manual path
- [[decisions/0011-deterministic-json-repair-over-retry]] — malformed processor JSON gets a deterministic byte-level repair pass instead of losing the whole batch
- [[decisions/0012-ci-cd-release-pipeline]] — automate release on merge to main, protect main with a no-bypass GitHub ruleset; Windows dropped from release targets
- [[decisions/0013-deterministic-decay-over-salience-model]] — reject ai-memory's SQLite/FTS5/link-neighbor ranking for memoria; adopt only a deterministic `lastUsed` decay stamp

## Skills — how-to references

- [[skills/release-ritual]] — how to cut a release: pick version, bump const, PR, merge (mechanics automated since `decisions/0012`)

## Rules — standing instructions

- [[rules/no-ai-attribution]] — never include AI-tool attribution footers or session links in commits/PRs/issues for this project; set after a PR #5 body correction

## Gotchas — what bit us

- [[gotchas/hooks-global-capture-opt-in]] — hooks are global, capture needs `bootstrap`; partially superseded by `bootstrap --global` (`8e495d5`)
- [[gotchas/implicit-session-end]] — a new session ends the previous one; reopens spawn incarnations
- [[gotchas/prompt-over-stdin-argv-limit]] — processor prompt goes over stdin (E2BIG)
- [[gotchas/stale-prompt-overrides]] — old materialized prompt files silently pin prompts
- [[gotchas/module-path-mismatch-breaks-go-install]] — go.mod path mismatch breaks install; fixed in `0e9c164`
- [[gotchas/auto-apply-rewrites-wiki-mid-session]] — background auto-apply consolidation can modify `wiki/` while a session is running
- [[gotchas/subagent-stop-promoted-to-user-request]] — handoff auto-ran a subagent's internal note; ask-first since `6414c80`
- [[gotchas/codex-refuses-untrusted-directories]] — codex exec refuses to run outside git repos or trusted paths; fixed in `dc9c30f`
- [[gotchas/processor-json-parse-failures]] — cheap-model quote-escaping bugs silently poisoned whole consolidation batches; fixed by deterministic repair in `6f653b9`

## Sessions — the episodic log

- [[sessions/2f4ed960-d4d1-4d74-87e8-7b4104179fb4]] — processor E2BIG root cause + `process --inspect`
- [[sessions/85a0d12d-60b4-4442-b107-f40c097122d8]] — MVP loop closed: eight commits from prompt embed to autopilot
- [[sessions/4f8bca2c-fb25-45fb-a0ee-cc7e9a42e5d3]] — `run` rework: session picker replaces `--last-session` (finished in incarnation 2)
- [[sessions/c113e710-6cad-49b4-8ae1-4cb15e1b5b54]] — three empty capture-only sessions right after the run rework
- [[sessions/0075f9e5-98eb-4d2e-bd20-3e8f5c52b069]] — ai-memory research: why their handoff is faster
- [[sessions/cf99acf5-2ee6-4118-90b5-1a2964766475]] — run rework committed: `0d9d968` via /git-commit
- [[sessions/f99b3c0c-68d0-4132-a4b1-cc2538dc1ce7]] — ai-memory research prompt re-asked after /clear; no work captured
- [[sessions/703f30e4-aabc-48d9-bf89-9bbd90d2428a]] — wiki research + session pages committed: `3252ce0`
- [[sessions/e915be64-f9af-4a50-a147-8a4831159754]] — `run` handoff rebuilt: digest pointer → self-contained packet (`ad5d206`)
- [[sessions/80f131bd-209e-4610-8bf3-07dac9154545]] — multi-agent init/setup shipped: `7128a3d`
- [[sessions/019fb0a0-48d0-71f1-954f-7036553b2133]] — handoff packet dogfooded: codex resumes the claude-code session
- [[sessions/6d290df9-cc9e-4703-bcc4-2143eba007aa]] — handoff round-trip: claude-code resumes the codex session; obsidian ignore committed (`c0ee15d`)
- [[sessions/7facd470-c6e9-489b-b490-58832dabc6e2]] — claude session titles captured into digests and the run picker (`c2e7e3a`), plus prompts moved to `cmd/memoria/prompts/` (`c478df6`)
- [[sessions/019fb0c0-23aa-7583-9035-ad2d71dd4ac6]] — wiki committed as `147ed73` over a packet handoff; "what did we do?" answered from the session's own page
- [[sessions/67c500e5-dc86-4a64-8e79-a76444932b79]] — `memoria_recall` shipped (`d6edeea`): read-only "what did we do?" answers, digest rerouted
- [[sessions/019fb0d1-0ea2-71c3-a0c2-616d5973374c]] — deferred wiki commit lands as `4d9171a` over a fourth packet handoff; recall question answered write-free
- [[sessions/e112664f-9954-4d8e-8fd2-a11e13d66bc0]] — handoff made ask-first (`6414c80`); multirepo assessment for eparts, adaptations committed as `31b7251` in incarnation 2
- [[sessions/c133e40b-8547-4a99-bc98-d14b7029ccfe]] — codex git-trust fix, processor model config, wiki auto-commit — `dc9c30f` + `f0bf53a`
- [[sessions/44c49197-2dea-4615-8ec2-27849c744218]] — open wiki folders with drop-and-warn validation (`06c9311`)
- [[sessions/16f159dc-d22a-413f-a839-76f7fa29eab3]] — queue write race condition fixed with file locking (`0f961a7`)
- [[sessions/756f94c9-e579-493d-a839-76f7fa29eab3.md]] — first release: v0.7.0 tagged, `memoria version` subcommand, release ritual documented
- [[sessions/29f4cf2e-fb32-47d3-9fb3-595ab07b17e7]] — curl installer script, init PATH detection, module-path mismatch fixed — `0e9c164`
- [[sessions/1ac019b3-5e9e-468e-a503-db42b7fa7ffd]] — stale prompt override + empty-digest + session-path canonicalization fixes, apply-time race detection and continuation marker
- [[sessions/4911da88-3f02-4f70-a22a-45a5405242df]] — wiki auto-commit made opt-in, `memoria commit` shipped, four consolidation bugs fixed
- [[sessions/019fc03b-ce00-7cc2-b8f4-fb706df1f37d]] — from-scratch Memoria evaluation plus ai-memory v1.22.0 flaw-handling comparison
- [[sessions/b978ee64-4761-45b0-ada9-beb4fc8b02a9]] — research wiki pages committed: `e4a83e0`
- [[sessions/61bd82ab-125a-4915-90cc-bdd5135716df]] — processor JSON repair shipped: `6f653b9`, malformed batches now get a deterministic repair pass instead of being lost
- [[sessions/19c76dfd-1284-48fa-bbba-486b6f7d66f0]] — `--global`/`--global-path` bootstrap flags requested; recon only, no decision yet → implemented the same day as `8e495d5` ([[sessions/266a3d10-ad0c-44f1-86f7-379941908fbf]])
- [[sessions/266a3d10-ad0c-44f1-86f7-379941908fbf]] — global capture mode shipped: `8e495d5` — `bootstrap --global`/`--global-path`, `setup --global`, `_global` pseudo-project
- [[sessions/e7535e5e-c210-4029-8496-3ab3c84ea9dd]] — release pipeline shipped: v0.8.0 (CI/CD + branch protection) and v0.8.1 (vitest-style test output)
- [[sessions/6f2b7832-1db8-4e7a-a2c0-ca0cc207e4c8]] — `memoria update` self-update command + release-binary install script shipped on `feat/update-command` (no PR yet); release binaries trimmed ~15MB→~10MB via `-ldflags="-s -w"`
- [[sessions/de6b74fc-4caf-4133-ab00-ee307afe6d78]] — `memoria status` restyled as a lipgloss table (PR #5, unmerged, version bumped to 0.10.0); no-AI-attribution rule set after a PR body correction
- [[sessions/acbf5d0d-d82b-4852-98fc-97cfbfb5da35]] — PR #9 closed as redundant (fix already on main via `8c0432e`); ai-memory Q&A on SQLite/FTS5/link-neighbor/decay; `lastUsed` session-page decay shipped, PR #10 (open, not approved), version bumped to 0.12.0
- [[sessions/075393cf-e94c-4b79-a4c5-4feec580aa66]] — cross-project search selectors + MCP trust rewrite shipped: PR #12 (v0.12.1→0.13.0)