---
tags: [gotcha, processor, json, reliability]
---

# Malformed processor JSON silently poisoned a whole consolidation batch

Hit for real on a sibling project, kakei ([[sessions/61bd82ab-125a-4915-90cc-bdd5135716df]]): starting a new session there triggered auto-consolidation on the previous session's end, which crashed with `processor returned invalid JSON: invalid character 'f' after object key:value pair` and wrote nothing to the wiki.

## Root cause

The consolidation JSON contract embeds whole markdown page bodies as single JSON string values — every `"` inside a body must come back as `\"`. The `claude-code` processor had no `processor_model` configured, so it ran on the then-default **haiku**. Replaying kakei's exact prompt through haiku, read-only, reproduced the shape: 37 escaped quotes in one page, several with a dropped backslash (`\"feat`, `\"flag`, `\"force`, matching literal tokens already present in kakei's own digest content). A dropped backslash closes the JSON string early, so the parser sees the next bare word (`feat`) where it expects `,` or `}` — exactly the reported error. This was inference from a re-run, not the literal failing bytes, because the processor's raw output is discarded on parse failure.

## What was NOT lost

`pending.yaml` still held the session with `ended: true` and its digest stayed in `pending/`. An `error` status doesn't block retry — `processAll` only skips a job that's still `running` — so the next cron firing or the next session-end in that project would retry the same batch (and fail the same way, since the underlying fragility wasn't model-random, it was structural).

## Gaps that made this a dead end for debugging

1. **Raw output discarded.** The processor's raw text — and the JSON `SyntaxError`'s byte offset — were both thrown away on parse failure. Nothing to inspect after the fact.
2. **No retry.** One bad escaped character kills the whole batch until an unrelated trigger (next cron, next session-end) fires, sometimes hours later.
3. **All-or-nothing batch.** Even a 5-page proposal dies because one page's body has a bad escape.
4. **Fragile transport.** Markdown-in-a-JSON-string asks the cheapest model to escape thousands of characters perfectly; there's nothing forcing correctness.
5. **Duplicated failure surface.** The same extract-then-unmarshal code was copy-pasted across `digest.go`, `lint.go`, `seed.go` and `process.go` — one failure mode, four places it could reappear.

## The fix

[[decisions/0011-deterministic-json-repair-over-retry]] — a shared `jsonout.go` parser with a deterministic byte-level repair pass, used by all five processor call sites, plus a bumped default model. See [[concepts/processor-models-and-effort]] for the model default and [[concepts/consolidation-pipeline]] for where this sits in the pipeline.