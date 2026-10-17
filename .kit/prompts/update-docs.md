---
description: Audit and update project documentation (README and docs site) for a recent change
---

Review recent code changes, identify all documentation surfaces that should
mention them, and update each one — grounded in the actual diff, not guesses.

## Steps

1. **Identify the change**:
   - If the user input ($@) names a commit / PR / branch / topic, use that as the focus
   - Otherwise inspect `git log origin/main..HEAD --oneline` and `git diff origin/main...HEAD --stat` to discover what shipped on the current branch
   - Read the actual diff (`git diff origin/main...HEAD`) — never document features that aren't in the code

2. **Inventory the doc surfaces**:
   - `README.md` at the repo root
   - Any docs site (commonly `www/`, `docs/`, `site/`) — list its pages and identify the one(s) most thematically related to the change
   - Inline godoc / API reference comments on the new exported symbols
   - `CHANGELOG.md` if the project keeps one
   - Any `examples/` directory entries that demonstrate the affected area

3. **Audit each surface** with `grep`:
   - Search for the names of related existing APIs (e.g. if you added `IterTools`, grep for `ListTools`) to find every page that already discusses the area
   - Decide for each hit: does it need a cross-reference, a side-by-side comparison, or to stay untouched?

4. **Decide where new content lives**:
   - Prefer extending an existing page over creating a new one
   - For a docs site, place new sections near related content (check the page's `## Heading` outline first)
   - Skip surfaces that genuinely don't apply (e.g. a server-focused README for a client-only change) and say so explicitly

5. **Draft the updates**:
   - Lead with a one-sentence statement of what's new and why
   - Show concrete code examples copied from real signatures — verify against the source files
   - Include a comparison / "when to use which" table when adding an alternative to an existing API
   - Note backwards-compatibility behaviour if relevant

6. **Verify the docs build** before committing:
   - For vocs / docusaurus / mkdocs sites, run the local build command (e.g. `npx vocs build`, `mkdocs build`) and fix any MDX/markdown errors
   - For godoc, run `go vet ./...` and `go doc <pkg> <Symbol>` to sanity-check rendering

7. **Report**:
   - List every file changed and every file deliberately left alone (with a one-line reason)
   - Suggest the next step (typically `/commit-push`) — do not auto-commit unless asked

## Guidelines

- Read the diff before writing anything — invented API names erode trust faster than missing docs
- One change per doc commit; keep doc updates separate from code changes when possible
- Match the existing voice and formatting of each surface (headings, code-fence languages, table styles)
- Prefer linking between pages over duplicating content

$@
