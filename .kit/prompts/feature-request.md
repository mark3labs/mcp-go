---
description: Create a feature request using the GitHub template
---

Create a feature request for the Kit repository. The user wants to request: $@

## Feature Request Template

This prompt uses the `feature_request` GitHub template which requires:

| Field | Required | Purpose |
|-------|----------|---------|
| **Feature Description** | Yes | What should be added or changed |
| **Motivation / Use Case** | Yes | Why is this needed? What problem does it solve? |
| **Proposed Implementation** | No | How do you think this should work? |

## Steps

1. **Understand the request** from `$@`
   - What capability is missing?
   - What would the ideal behavior look like?

2. **Ask clarifying questions** if needed:
   - "What problem does this solve for you?"
   - "How would you expect this to work?"
   - "Are there similar features in other tools you use?"

3. **Craft the title** using conventional format:
   - `feat: <short description>`
   - Lowercase, imperative mood, ≤72 chars
   - Good examples:
     - `feat: add keyboard shortcut for clearing input`
     - `feat: support custom themes per extension`
     - `feat: add fuzzy matching to model selector`
   - Bad examples:
     - `Feature request: can we have...` (too vague)
     - `It would be nice if...` (not imperative)

4. **Build the body** with the template fields:

   **Feature Description:**
   - Clear statement of what to add/change
   - Be specific about the behavior
   - Include UI/UX details if relevant

   **Motivation / Use Case:**
   - What problem does this solve?
   - Current workaround (if any) and why it's insufficient
   - Who benefits from this feature?

   **Proposed Implementation** (optional but helpful):
   - High-level approach
   - API changes if applicable
   - Example usage code

5. **Create the issue**:
   ```bash
   gh issue create --template feature_request --title "feat: ..." --body "..."
   ```

6. **Confirm success**:
   - Show the issue URL and number
   - Mention it was created with the feature_request template

## Guidelines

- Focus on the *problem* first, then the solution
- Include concrete examples of how the feature would be used
- Consider edge cases and mention them
- If proposing API changes, show before/after code
- Check if similar features exist in related tools (mention them for reference)
- Align with Kit's philosophy: TUI-first, extension-based, keyboard-driven

## Example

User: `/feature-request I want to be able to customize tool border colors dynamically`

You:
1. Title: `feat: dynamic border colors for tool results based on status`
2. Body:
   - **Feature Description**: Allow `ToolRenderConfig` to accept a function that determines border color based on tool result content or status, enabling dynamic visual feedback.
   - **Motivation**: When running multiple tools, it's hard to distinguish file reads (blue), shell commands (green), and errors (red) without custom colors per result.
   - **Proposed Implementation**: Add `BorderColorFunc` callback that receives `(result string, isError bool)` and returns a color string.

3. Execute: `gh issue create --template feature_request --title "feat: ..." --body "..."`
4. Confirm: Created issue #43 using feature_request template
