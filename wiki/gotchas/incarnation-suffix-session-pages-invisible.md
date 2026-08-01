---
tags: [gotcha, session-pages, run, paths]
---

When the processor consolidates a session and writes a wiki page to `sessions/<filename>`, it uses the digest filename as the page name. But digest filenames include incarnation suffixes: `sessions/s1-2.md` for the second reopen of session `s1`. Readers (`memoria run`, `memoria_recall`, the MCP digest tool) hardcode the query as `sessions/<session_id>.md` — the bare id from the digest's frontmatter. They never find the suffixed page.

Hit on 2026-08-01: a 60KB session (`c589857b-...-2.md`) consolidated to `wiki/sessions/c589857b-...-2.md` (6287 bytes, all three features). When `memoria run` tried to build a handoff packet for that session, it looked for `sessions/c589857b-...md` (no suffix) and got nothing.

## Why it happens

Incarnations exist as internal bookkeeping — `resolveDigestPath` renames reopened digests to avoid overwriting processed ones. That's invisible to users, and should stay that way: sessions are keyed by their id, not by incarnation number. But the processor receives the full digest filename and uses it as the wiki page name. By the time the system tries to read the page back, the incarnation suffix is not part of the public interface anymore.

## The fix

CanonicalizeSessionPaths (called in `generateProposal` before `validatePages`): rewrite any page path matching the pattern `sessions/<known-sid>-<digit>+.md` to `sessions/<known-sid>.md` by reading the digest's frontmatter `session_id`. Matching is against the actual session ids in the batch — never by filename pattern (a UUID can legitimately end in `-400060889612`, so suffix-stripping by shape breaks those). Drop orphan pages naming no session in the batch. Report collisions (when two incarnations land on one path) so they're not silent. The full page is still written; the collision is visible in warnings.

## Outstanding design question

When two incarnations write to the same path, the second overwrites the first. `c589857b-...-1.md` (14KB) was replaced by `-2.md` (6287 bytes) — some episodic content was lost. A future fix might extend existing session pages rather than replace when `continues_from` is present, but that's a prompt-side or two-phase-write problem, not a path problem.

## Related

[[gotchas/implicit-session-end]] (how incarnations spawn), [[concepts/recall-and-run]] (where the path lookup happens).