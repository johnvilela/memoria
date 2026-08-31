---
tags: [adr, processor, json, reliability]
---

# ADR 0011: Repair malformed processor JSON deterministically, not via an AI retry

**Decision** (commit `6f653b9`, [[sessions/61bd82ab-125a-4915-90cc-bdd5135716df]]): when a processor's JSON output fails to parse, attempt one deterministic byte-level repair pass before giving up — not a round-trip back to the model. Explicit direction behind the choice: "Try to always solve anything with less AI interation possible and more things being deterministic when is possible."

## The problem

The consolidation contract embeds whole markdown page bodies as JSON string values, so every `"` in a body must be escaped as `\"`. The cheapest configured model (haiku, the then-default) reliably mis-escaped quotes at scale on real content — dropping a backslash closes the JSON string early and the parser fails on the next bare word. This was a structural fragility of the transport, not a one-off fluke — see [[gotchas/processor-json-parse-failures]].

## Mechanism

`cmd/jsonout.go` is now the single parser behind all five processor call sites (`process`, `digest`, `lint` report, `lint` fix, `seed`):

```
extract → unmarshal ──fail──→ repairJSON() → unmarshal ──fail──→ dump raw + offset window
```

`repairJSON` is a byte scanner: a `"` inside a string value closes it only when the next non-space byte is `,` `:` `}` `]` or EOF — any other next byte means a dropped backslash, so the quote gets escaped. Raw control characters are treated the same way. On genuinely valid JSON this condition never triggers, so the function is the identity on valid input (verified against 9 shapes). Known, documented, and tested ceiling: a truly ambiguous case (e.g. `say "hi", ok`) still fails loudly rather than silently corrupting a page.

A repaired run is never silent — it's flagged in `memoria.log`, in stdout, and in the `memoria status` detail ("— output repaired"), and the raw output is kept on disk so a bad repair can be audited.

## What this replaced

The "cheapest real mitigation" identified during diagnosis was, in order: dump raw output + offset on failure, retry once (ask the model to resend given the error), then a bigger model for large batches. Only the first idea (dump on failure) and the model bump shipped as-is; the middle option — an AI retry round-trip — was explicitly not built, in favor of the deterministic repair layer described above, which removes the failure class without spending a second model call.

## Side effects landing in the same commit

- The claude processor's default model moved `haiku` → `sonnet` (`processor_model: haiku` still selects the old default explicitly) — [[concepts/processor-models-and-effort]].
- One escaping-instruction line was added to all four prompt contracts (digest, seed, lint, wiki/process).
- Writing the round-trip repair tests caught a real pre-existing bug: an `err` shadowed inside an `if`/`else` was writing `# <nil>` into the failure dump file instead of the actual error.

Related: [[gotchas/processor-json-parse-failures]] (the failure this fixes), [[concepts/consolidation-pipeline]] (where the processor output lands), [[decisions/0004-embedded-prompts-with-file-override]] (the prompt contracts touched).