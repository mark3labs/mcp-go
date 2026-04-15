---
description: File a GitHub issue using the appropriate template
---

File a GitHub issue for the Kit repository. The user wants to create an issue about: $@

## Issue Templates Available

This repository has structured issue templates. You MUST use the appropriate template:

| Type | Template | Use For |
|------|----------|---------|
| `bug` | `bug_report` | Something is broken, not working as expected |
| `feat` | `feature_request` | New feature, enhancement, improvement |
| `docs` | `documentation` | Missing, incorrect, or unclear documentation |

## Steps

1. **Determine the issue type** from `$@`:
   - Bug → use `--template bug_report`
   - Feature → use `--template feature_request`  
   - Documentation → use `--template documentation`

2. **Ask clarifying questions** if critical info is missing:
   - For bugs: "What were you doing when this happened?" (reproduction steps)
   - For features: "What problem does this solve?" (motivation)
   - For docs: "Where did you look for this information?" (location)

3. **Craft the title** using conventional format:
   - `<type>: <short description>`
   - Lowercase, imperative mood, ≤72 chars
   - Examples:
     - `fix: ToolRenderConfig BorderColor ignored during rendering`
     - `feat: add keyboard shortcut for clearing input`
     - `docs: clarify extension widget lifecycle`

4. **File the issue** using the template:
   ```bash
   # For bugs
   gh issue create --template bug_report --title "fix: ..." --body "..."
   
   # For features
   gh issue create --template feature_request --title "feat: ..." --body "..."
   
   # For documentation
   gh issue create --template documentation --title "docs: ..." --body "..."
   ```

   The template will guide the user through the required fields. You need to provide:
   - **Bug reports**: Description, reproduction steps, expected vs actual behavior
   - **Feature requests**: Description, motivation/use case, optional proposed implementation
   - **Documentation**: Description, location of docs, suggested improvement

5. **Confirm success** by showing:
   - The issue URL
   - The issue number
   - Which template was used

## Template Field Guide

### Bug Report (`bug_report`)
Required fields in the body:
- **Bug Description** - what happened vs expected
- **Steps to Reproduce** - numbered list to recreate the bug
- **Relevant Code** - code snippets, configuration, error messages
- **Component** - which part of Kit (ui, extensions, session, etc.)
- **Version** - Kit version or commit hash

### Feature Request (`feature_request`)
Required fields in the body:
- **Feature Description** - what to add/change
- **Motivation / Use Case** - why this is needed
- **Proposed Implementation** - how it could work (optional)

### Documentation (`documentation`)
Required fields in the body:
- **Documentation Issue** - what's wrong or missing
- **Documentation Location** - file or URL where docs exist
- **Suggested Improvement** - how to fix the docs

## Guidelines

- ALWAYS use `--template <name>` instead of bare `gh issue create`
- Include file paths and line numbers when you know them
- Use triple backticks for code blocks
- Keep the body factual - avoid speculation unless in "Proposed Fix" section
- If you're unsure about technical details, say so in the issue
- For UI bugs, describe what you see vs what you expect
- For API bugs, include the relevant struct/function names

## Example Usage

User: `/file-issue The ToolRenderConfig BorderColor field is documented but never used in rendering`

You: 
1. Determine this is a **bug** (documented field doesn't work)
2. Use `--template bug_report`
3. Gather: reproduction steps (register renderer with BorderColor), expected (custom color), actual (default color)
4. Create issue with title `fix: ToolRenderConfig BorderColor and Background fields are ignored`
5. Confirm: Created issue #42 using bug_report template
