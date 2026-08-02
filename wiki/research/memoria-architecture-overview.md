---
tags: [research, architecture, memoria, hooks, processing, mcp]
---

# Memoria architecture overview

> Hooks capture sessions; plain Markdown is curated memory; no database or embeddings.

```mermaid
flowchart TB
    subgraph ENTRY["ENTRY POINTS"]
        direction LR
        AGENT["Agent CLIs<br/>Claude Code and Codex<br/>hooks + MCP stdio + memoria run"]
        TERMINAL["Terminal and automation<br/>init · setup · bootstrap<br/>process · lint · search · commit · status"]
        TIMER["OS scheduler<br/>systemd user timer<br/>launchd LaunchAgent"]
        PROCESSOR["Optional processors<br/>Claude Code · Codex · Gemini<br/>Ollama: coming soon"]
    end

    subgraph CORE["memoria — one Go binary"]
        direction TB

        subgraph INGRESS["Capture and routing"]
            direction LR
            HOOK["Hook endpoint<br/>non-blocking · stdout silent<br/>tracked projects only"]
            ROUTE["Project routing and config<br/>longest-path project match<br/>wiki folder + options"]
            CAPTURE["Capture and normalize<br/>selected lifecycle events<br/>recursion guard"]
            QUEUE["Session queue<br/>pending → ended → processed<br/>sessions.md + incarnation chain"]
        end

        subgraph SERVICES["Agent and human interfaces"]
            direction LR
            MCP["MCP server<br/>search · recall · digest<br/>consolidate · lint · write · delete"]
            CLI["CLI and TUI<br/>scriptable flags<br/>interactive setup and review"]
            ACCESS["Wiki access<br/>text and #tag search<br/>raw Markdown · trash opt-in"]
        end

        subgraph MAINTENANCE["Knowledge maintenance"]
            direction LR
            JOBS["Background jobs<br/>process · digest · lint<br/>one status slot per project"]
            REVIEW["Proposal and review gate<br/>proposal/report → review → apply<br/>auto-apply is optional"]
            SWEEP["Project sweep<br/>process --all<br/>sequential across projects"]
        end
    end

    subgraph STATE["DURABLE STATE"]
        direction LR
        CONFIG["User config<br/>config.yaml + pending.yaml<br/>status.yaml + prompts + memoria.log"]
        WORKING[".memoria/ working state<br/>pending + processed digests<br/>proposal.json + lint.jsonl<br/>gitignored"]
        WIKI["wiki/ Markdown<br/>curated source of truth<br/>tags + wikilinks + trash/<br/>versioned in the repository"]
        GIT["Git history<br/>manual memoria commit<br/>auto-commit is opt-in"]
    end

    AGENT -->|lifecycle hook JSON| HOOK
    AGENT -->|stdio tools| MCP
    TERMINAL --> CLI
    TIMER -.->|scheduled process --all| SWEEP

    HOOK --> ROUTE
    CONFIG -->|projects + options| ROUTE
    ROUTE -->|tracked project| CAPTURE
    CAPTURE --> QUEUE

    MCP -->|background tools| JOBS
    MCP -->|read/write tools| ACCESS
    CLI -->|process · digest · lint| JOBS
    CLI -->|search| ACCESS
    CLI -->|review · apply| REVIEW
    CLI -->|status| CONFIG
    CLI -->|commit| GIT
    QUEUE -->|ended sessions| JOBS
    SWEEP --> JOBS

    JOBS <-.->|LLM call| PROCESSOR
    JOBS -->|process and lint output| REVIEW
    JOBS -->|digest page| WIKI
    REVIEW -->|manual or automatic apply| WIKI

    QUEUE -->|digests + session index| WORKING
    QUEUE -->|central pending queue| CONFIG
    JOBS -->|status + log| CONFIG
    REVIEW -->|proposal + lint report| WORKING
    ACCESS <-->|read · write · trash| WIKI
    ACCESS -->|recall raw events| WORKING
    WIKI -->|scoped wiki commit| GIT

    classDef entry fill:#ffffff,stroke:#6c8cff,stroke-width:1.5px,color:#14213d;
    classDef processor fill:#fff8ed,stroke:#ff8a34,stroke-width:1.5px,color:#14213d;
    classDef core fill:#ffffff,stroke:#9aaeff,stroke-width:1.5px,color:#14213d;
    classDef queue fill:#edfff4,stroke:#23b26d,stroke-width:1.5px,color:#14213d;
    classDef review fill:#fff8dc,stroke:#e8a317,stroke-width:1.5px,color:#14213d;
    classDef state fill:#ffffff,stroke:#74839a,stroke-width:1.5px,color:#14213d;
    classDef wiki fill:#edfff4,stroke:#23b26d,stroke-width:1.5px,color:#14213d;

    class AGENT,TERMINAL,TIMER entry;
    class PROCESSOR processor;
    class HOOK,ROUTE,CAPTURE,MCP,CLI,ACCESS,JOBS,SWEEP core;
    class QUEUE queue;
    class REVIEW review;
    class CONFIG,WORKING,GIT state;
    class WIKI wiki;

    style CORE fill:#f1f4ff,stroke:#7185ff,stroke-width:2px
    style ENTRY fill:transparent,stroke:transparent
    style STATE fill:transparent,stroke:transparent
    style INGRESS fill:transparent,stroke:transparent
    style SERVICES fill:transparent,stroke:transparent
    style MAINTENANCE fill:transparent,stroke:transparent
```

## Flow in plain text

1. **Capture:** Claude Code or Codex invokes `memoria hook` with lifecycle JSON. Memoria resolves the current directory to a registered project, normalizes useful events, appends them to `.memoria/sessions/pending/`, and updates the central pending queue.
2. **Session lifecycle:** A real `session-end`, or the next session started in that project, marks queued digests as ended. Reopened processed sessions create a new incarnation linked to the previous digest.
3. **Consolidation:** `process`, the MCP background tools, or the OS scheduler collect ended sessions. A configured processor returns structured wiki pages; it never writes project files directly.
4. **Review and apply:** By default, processing writes `.memoria/proposal.json` for review. Applying it writes validated Markdown under `wiki/`, moves digests to `processed/`, removes queue entries, and deletes the proposal. `auto_apply` makes this automatic.
5. **Recall:** Humans use `search`; agents use MCP search and recall. Both read the project wiki, while recall can also assemble a handoff from raw session events and Git state.
6. **Versioning:** `wiki/` is meant to be committed. `memoria commit` scopes the commit to the wiki; `wiki_auto_commit` optionally commits applied writes automatically. `.memoria/` remains gitignored operational state.

The scheduler and detached commands reuse the same per-project background-job status slot. `process --all` sweeps registered projects sequentially because the shared YAML queue and status files are not transactionally locked.
