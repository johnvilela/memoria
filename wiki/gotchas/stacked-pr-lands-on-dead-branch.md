---
tags: [gotcha, git, pr, branch]
---

# Stacking a PR on another PR's branch doesn't retarget to main when the base squash-merges first

Hit for real in [[sessions/b83a5def-41be-43be-82b5-902466d6a5e6]]: PR #16 ([[concepts/mcp-auto-trust]] + [[gotchas/mcp-consolidate-stale-done-status]]) was opened with `gh pr create --base feat/tokenized-search`, stacked on PR #15 ([[concepts/cross-project-search]]'s branch), on the assumption that it would "retarget to main automatically once #15 merges." PR #15 squash-merged into main first. PR #16 was then merged — but into `feat/tokenized-search`, not main; GitHub never retargeted its base. Since #15's squash-merge collapsed its commits into one new commit on main, `feat/tokenized-search`'s history was no longer reachable from main, so merging #16 updated only that now-orphaned branch. Main stayed at the pre-#16 version (0.15.0, no `installTrust`) even though both PRs showed as MERGED in `gh pr list`.

## How it was caught

The user noticed PR #16's merge target looked wrong and asked for a same-content PR pointed at main. Diagnosis: `gh pr view <n> --json state,baseRefName` on both PRs, `git fetch origin main` + `git log origin/main`, and diffing `origin/main`'s `cmd/main.go`/`cmd/hooks.go` against what #16 was supposed to add — main had neither the version bump nor `installTrust`.

## The fix

`git log origin/main..fix/mcp-consolidate-trust` listed the commits unique to the trust branch (3: the code fix plus two wiki commits). Branch a fresh checkout off current `origin/main`, `git cherry-pick` those commits — they applied cleanly — verify the version const and full test suite, then push and open a new PR based on `main` directly (re-using the original PR's body plus a note that it's a re-land). This shipped as **PR #17** ([[concepts/mcp-auto-trust]], [[gotchas/mcp-consolidate-stale-done-status]]).

## Lesson

Don't trust "it'll retarget once the base merges" for a stacked PR (`gh pr create --base <other-PR-branch>`) without checking after the base merges. Squash-merging the base PR is a specific trap: it deletes the base branch's commit history from main's ancestry, so a stacked PR merged afterward lands its changes nowhere main can see, even though both PRs report MERGED. Verify with `git log origin/main..<branch>` (non-empty output = commits still missing from main) before treating a stacked-PR merge as done.