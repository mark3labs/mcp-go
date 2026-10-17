---
description: Implement the fix/feature/docs change requested by a GitHub issue
---

Resolve GitHub issue #$1 by reading it, classifying it, and producing the appropriate code or doc change. **Stop once the working tree contains the change** — committing, pushing, and opening a PR are handled by `/commit-push` and `/create-pr`.

## Steps

1. **Fetch the issue**:
   - Run: gh issue view $1 --json number,title,body,labels,state,author,comments
   - If the issue is closed, stop and ask the user whether to proceed
   - Read the **entire** thread including comments — the latest comment often refines the ask

2. **Classify the issue** from labels, title prefix, and body content:
   - `bug` / `fix:` → reproduce, then fix
   - `enhancement` / `feature` / `feat:` → design, then implement
   - `documentation` / `docs:` → locate and update docs
   - `question` / `discussion` → answer in a comment, do **not** write code
   - Anything else → ask the user how to proceed

3. **Create a working branch** off the default branch:
   - `git checkout main && git pull --ff-only`
   - Branch name: <type>/$1-<slug> (e.g. `fix/42-borderColor-ignored`, `feat/57-keyboard-clear`, `docs/63-widget-lifecycle`)

4. **Do the work** based on type:

   ### Bug (`bug` label / `fix:` title)
   - Reproduce the failure first (write a failing test if feasible) — if you cannot reproduce, comment on the issue asking for clarification and stop
   - Locate the root cause; do not patch symptoms
   - Add or extend a regression test that fails before and passes after the fix
   - Run `go test ./... -race` and `golangci-lint run`

   ### Feature (`enhancement` / `feature` label / `feat:` title)
   - Re-read the motivation and proposed implementation in the issue body
   - For large, ambiguous, or breaking changes, sketch the design in a comment on the issue and wait for sign-off before writing code
   - Implement behind sensible defaults; add godoc on every exported symbol
   - Add unit tests covering the new behaviour and edge cases
   - Update `README.md` / `docs/` if the public surface changed
   - Run `go test ./... -race` and `golangci-lint run`

   ### Documentation (`documentation` label / `docs:` title)
   - Open the file/URL referenced in the issue's "Documentation Location"
   - Apply the suggested improvement; verify code samples compile (`go build ./...`)
   - No tests required, but run `golangci-lint run` if Go files were touched

5. **Report**:
   - Branch name (`git branch --show-current`)
   - Summary of files changed (`git status -s`) and the diff highlights
   - Test/lint results (pass/fail with key output)
   - Suggest the next step explicitly:
     - `/commit-push` to commit with a Conventional Commit subject (the message should reference `(#$1)` and include `Fixes #$1` so merge auto-closes)
     - then `/create-pr $1` to open the pull request

## Guidelines

- This prompt **stops at a clean working tree with the change applied** — do not run `git commit`, `git push`, or `gh pr create`
- If the issue is unclear, post a clarifying comment on the issue and stop; do not guess
- Keep the change scoped to the issue; surface unrelated cleanups separately
- For breaking changes or architecture shifts, propose the design on the issue first and wait for maintainer sign-off
- If the issue is a duplicate or already fixed on `main`, comment with the reference and stop
- Do not close the issue manually — the eventual PR's `Fixes #$1` handles that on merge
