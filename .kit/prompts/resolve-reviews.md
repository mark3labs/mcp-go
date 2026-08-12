---
description: Fix bot review findings on the current PR, push, and poll until the review loop is clean
---

Resolve all automated review-bot findings (CodeRabbit and similar) on a pull request: verify each finding against the current code, fix the valid ones, push, wait for the bot's re-review, and repeat until no actionable comments remain. Extra context from the user: $@

## Identify the PR

- If the user input names a PR number, use it; otherwise resolve the PR for the current branch:

      gh pr view --json number,headRefName,state -q '{number: .number, branch: .headRefName, state: .state}'

- If the PR is merged or closed, stop and tell the user
- If the working tree is dirty with unrelated changes, stop and suggest `/commit-push` first
- Capture the repo slug once: `gh repo view --json nameWithOwner -q .nameWithOwner`

## Fetch the findings

Pull **both** comment surfaces — bots use them differently:

1. **Review-level bodies** (summary + "Actionable comments posted: N"):

       gh pr view <pr> --json reviews --jq '.reviews[] | select(.author.login | test("coderabbit|copilot|bot")) | {state, body}'

2. **Line comments** (the individual findings):

       gh api repos/<owner>/<repo>/pulls/<pr>/comments --jq '.[] | select(.user.login | endswith("[bot]")) | "=== \(.path):\(.line) ===\n\(.body)"'

CodeRabbit line comments embed a `🤖 Prompt for AI Agents` block — a ready-made instruction for each finding. Read the **full body**, not just the summary: severity markers (🟠 Major / 🟡 Minor), committable suggestions, and "Also applies to" line lists all matter.

## Triage each finding — verify before fixing

For every finding, check it against the **current** code (the comment may be outdated):

- **Still valid** → fix it, keeping the change minimal and scoped to the finding
- **Already addressed** (by a later commit or a previous loop iteration) → skip, note the commit that fixed it
- **Intentional behavior** the bot misread → skip, and reply on the thread explaining why:

      gh api repos/<owner>/<repo>/pulls/<pr>/comments/<comment-id>/replies -f body="..."

- **Wrong or out of scope** → skip with a brief reason in your report; don't silently ignore

Never blind-apply a bot's committable suggestion — read the surrounding code first. Bots regularly miss context like deliberate design decisions (check nearby comments and linked issues before "fixing" them away).

## Fix, validate, push

1. Apply the fixes; add or extend tests when a finding exposed a real gap (a fixed bug deserves a regression test)
2. Validate everything the repo's CI would:
   - `go build ./...` / `go vet ./...` / `gofmt -l .`
   - `go test -race ./...` (use an isolated `HOME` if local dotfiles pollute tests)
   - `golangci-lint run` on the touched packages
3. Commit with a Conventional Commit subject that references the review, e.g.:

       git commit -m "fix(<scope>): address <bot> review on <topic> (#<issue>)"

   Body: one bullet per finding fixed, one line per finding skipped with the reason.
4. `git push`

## Poll for the re-review

The bot re-reviews the new HEAD automatically after push. Poll — do not spam re-review requests:

1. Get the pushed SHA: `git log -1 --format=%H`
2. Poll the commit status until the bot's context reports completion (CodeRabbit sets a commit status):

       gh api repos/<owner>/<repo>/commits/<sha>/status --jq '{state, statuses: [.statuses[] | {context, state, description}]}'

   Wait for `context: "CodeRabbit"` → `state: "success"` with `description: "Review completed"`. Poll with `sleep 90`–`sleep 240` between checks; reviews typically land in 2–5 minutes.
3. Also confirm CI on the same commit: `gh pr checks <pr>`

## Check for new findings and loop

After the re-review completes:

1. Re-fetch line comments and diff against the set you already handled (compare comment IDs / created_at timestamps — new findings have new IDs)
2. Check that old threads resolved:

       gh api graphql -f query='query { repository(owner: "<owner>", name: "<repo>") { pullRequest(number: <pr>) { reviewThreads(first: 50) { nodes { isResolved isOutdated path } } } } }'

3. **New actionable comments** → go back to *Triage* and repeat the loop
4. **No new comments, all threads resolved (or outdated), bot status green** → done

**Loop guard:** cap at 4 iterations. If the bot keeps raising new findings after that, stop and summarize the remaining items for the user — repeated churn usually means a design disagreement that needs a human decision, not another auto-fix.

## Report

- Findings fixed (with severity), skipped (with reasons), and any threads replied to
- Commits pushed this session (`git log --oneline` of the new commits)
- Final state: bot review status, CI status, unresolved thread count
- If anything was intentionally left open, say so explicitly

## Guidelines

- Verify every finding against current code before touching anything — bots review diffs, not intent
- Keep each loop iteration a single commit; don't mix review fixes with unrelated work
- Reply on threads when skipping for "intentional behavior" — silent skips look like neglect and the bot may re-raise
- Prefer the bot's own `Prompt for AI Agents` phrasing when interpreting ambiguous findings
- Never `--force` push during the loop; the bot tracks incremental commits
- If the bot flags something CI also caught, fix once — don't attribute it twice
