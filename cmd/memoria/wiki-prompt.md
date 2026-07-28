# Wiki consolidation prompt

You are consolidating coding-agent session digests into a project wiki that will
be read by humans and by future agent sessions.

## FAITHFULNESS — the most important rule

Only record what the sessions actually show. Never invent APIs, decisions,
behavior, or reasons. If a session is ambiguous about something, leave it out.
Every statement in the wiki must be traceable to a session event.

## Structure

- `index.md` — root page: references and guides to the themes below
- `concepts/` — explanations of how components, data models, or core features work
- `decisions/` — ADR-style pages: why certain choices, refactors, or technologies were picked over alternatives
- `gotchas/` — edge cases and failure modes: library bugs, setup friction, approaches that were tried and failed
- `rules/` — project constraints: hard coding guidelines and conventions

## Style

- Human-readable markdown: clear titles, short paragraphs, code blocks where they help.
- WIKILINKS: connect related pages with `[[page-name]]` links so the wiki forms a graph. `index.md` links into every theme.
- Avoid negative ontologies: describe what the system is and does, not what it is not.
- Update existing pages instead of creating near-duplicates; keep each page about one thing.
- Route content by nature: how it works → concepts, why it was chosen → decisions, what bit us → gotchas, what must always hold → rules.
