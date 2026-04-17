---
description: Scaffold a new prompt template in .kit/prompts/
---

Create a new kit prompt template. The user wants a prompt that does: $@

## What a prompt template is

A prompt template is a `.md` file in `.kit/prompts/` (project-local) or `~/.kit/prompts/` (global).
It becomes a `/slug` slash command in the kit input box — typed as `/filename` with optional arguments.

## File format

```
---
description: One-line description shown in autocomplete
---

Body text of the prompt. Use $@ for all user-supplied arguments,
$1 $2 etc. for positional arguments.
```

- **Filename** → slug: `commit-push.md` becomes `/commit-push`
- **Frontmatter**: only `description` is recognised; keep it under ~80 chars
- **Body**: plain markdown; the full text is submitted as the user's message when the template fires
- **Arguments**: `$@` expands to everything the user typed after the slash command name;
  `$1`, `$2` for individual positional args; omit entirely if no arguments are needed

## Steps

1. **Understand the workflow** the user described in `$@` — ask a clarifying question if the intent is ambiguous
2. **Choose a filename**: short, lowercase, hyphen-separated, descriptive (e.g. `code-review.md`)
3. **Write the description**: one sentence, imperative, fits in autocomplete
4. **Draft the body**:
   - Open with a single sentence stating the goal
   - Use `## Steps` for multi-step workflows; use plain prose for simple prompts
   - Be specific: name commands, flags, and file paths where relevant
   - End with `$@` on its own line if the user might want to pass context or a hint; omit if the prompt is self-contained
5. **Write the file** to `.kit/prompts/<slug>.md`
6. **Confirm** by showing the final file content and the slash command that activates it

## Guidelines

- Keep prompts action-oriented — they should tell kit *what to do*, not just *what to think about*
- Prefer concrete steps over vague instructions
- A prompt that does one thing well beats one that tries to cover every edge case
- If the workflow already exists as a prompt, suggest extending it instead of duplicating
