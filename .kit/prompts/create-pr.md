---
description: Open a GitHub PR for the current branch using the repo's PR template
---

Open a GitHub pull request for the current branch, filling out the repository's PR template with a description grounded in the actual commits and diff.

## Steps

1. **Verify the branch is pushed**:
   - `git status -sb` and `git log @{u}..HEAD --oneline 2>/dev/null` — if there is no upstream or unpushed commits, run `git push -u origin "$(git branch --show-current)"` first
   - If the working tree is dirty, stop and tell the user to commit first (suggest `/commit-push`)
2. **Gather context**:
   - `git log origin/main..HEAD --oneline` — list of commits going into the PR
   - `git diff origin/main...HEAD --stat` then `git diff origin/main...HEAD` — read the actual changes
   - Identify the linked issue (from commit messages, branch name, or extra user input: $@) — capture as `Fixes #N` if applicable
3. **Locate the PR template**:
   - Check `.github/pull_request_template.md`, `.github/PULL_REQUEST_TEMPLATE.md`, or `docs/pull_request_template.md`
   - If none exists, use a minimal `## Description` / `## Type of Change` / `## Checklist` structure
4. **Draft the PR body** by filling out the template:
   - **Description**: 1–3 short paragraphs explaining *what* changed and *why*, grounded in the diff. Include a brief before/after example for new APIs when useful.
   - **Fixes #N**: only if there is a real linked issue
   - **Type of Change**: tick the single most accurate box with `[x]` (leave others as `[ ]`)
   - **Checklist**: tick items that are genuinely true (style, self-review, tests added, docs updated)
   - **Additional Information**: bullet list of added / modified files and any backward-compatibility notes
   - Remove template sections explicitly marked "remove if not applicable" (e.g. MCP Spec Compliance) when they don't apply
5. **Write the body to a temp file**: `/tmp/pr-body-<branch-or-issue>.md` — never inline a long body via `--body`, always use `--body-file`
6. **Choose the title**: prefer the subject of the primary commit if it already follows Conventional Commits; otherwise craft one in the same style (`<type>(<scope>): <imperative summary>`, ≤72 chars)
7. **Create the PR**:
   ```
   gh pr create \
     --title "<title>" \
     --body-file /tmp/pr-body-<...>.md \
     --base main \
     --head "$(git branch --show-current)"
   ```
   Use the repo's actual default branch if it isn't `main` (`gh repo view --json defaultBranchRef -q .defaultBranchRef.name`)
8. **Report the PR URL** returned by `gh` and stop

## Guidelines

- Read the diff and commit messages — do **not** invent features that aren't in the code
- One PR per logical change; if the branch contains unrelated commits, surface that and ask before continuing
- Keep the description focused on reviewer-relevant information (what / why), not a replay of the diff
- Only check checklist boxes that are actually satisfied; leave the rest unchecked rather than lying
- If `gh` is not authenticated (`gh auth status` fails), stop and tell the user

$@
