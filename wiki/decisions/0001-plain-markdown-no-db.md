---
tags: [adr, storage, markdown, git]
---

# ADR 0001: Plain markdown in the repo, no database, no embeddings

**Decision** (README intro): "No database, no embeddings — plain `.md` files, versioned with the project." Search is text/tag matching over files ([[concepts/recall-and-run]]), not vector retrieval.

The split (README §Markdown in the repo):

- **Session digests stay untracked** — `.memoria/` is gitignored during setup (commits 5a2f216 and 8b34990).
- **The curated `wiki/` folder is meant to be committed**, so memory travels with the project.
- **Deleted pages land in `wiki/trash/`** instead of vanishing, tagged `deleted` and hidden from search unless explicitly included.

Global state that is not project knowledge (config, pending queue, debug log) lives under `~/.config/memoria/` instead ([[concepts/architecture-overview]]).

The README credits the shape of this design to Fabio Akita's ai-memory and Andrej Karpathy's LLM wiki idea.