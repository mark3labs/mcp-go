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

Body text of the prompt. Reference user-supplied arguments
with positional placeholders (see "Argument placeholders" below).
```

- **Filename** → slug: `commit-push.md` becomes `/commit-push`
- **Frontmatter**: only `description` is recognised; keep it under ~80 chars
- **Body**: plain markdown; the full text is submitted as the user's message when the template fires
- **Required args**: kit infers required positional args from the highest `$N` it finds *outside* backtick/tilde code fences — a stray `$2` in active prose means kit will refuse to run without 2 arguments

## Argument placeholders

kit performs shell-style substitution before sending the prompt to the model:

- `$1`, `$2`, … — positional arguments (1-indexed)
- `${1}`, `${2}`, … — same, brace form (use when followed by digits/letters: `${1}_suffix`)
- `$@` — all arguments joined by spaces (zero or more, optional)
- `$+` — all arguments, **at least one required**
- `$ARGUMENTS` / `${ARGUMENTS}` — alias for `$@`
- `${@:N}` — args from the Nth onwards (1-indexed, bash-style)
- `${@:N:L}` — `L` args starting from the Nth

### ⚠️ Critical: code fences and inline code preserve placeholders verbatim

Anything inside triple-backtick fences, `~~~` fences, or single-backtick `inline` code spans is **left untouched** so example code samples don't get corrupted. That means:

- An inline-coded `gh issue view $1` stays literal `$1` in the model's input ❌
- The same command without backticks: gh issue view $1 → expands to `gh issue view 42` ✓

**Rule of thumb:** if you want a placeholder to substitute, keep it outside backticks and fences. If you want a literal `$1` in the output (e.g. teaching the user shell syntax), put it inside backticks.

### Workarounds for "I want it to look like code AND substitute"

1. **Drop the backticks** around just the placeholder portion — the rest can still read as a command line in prose
2. **Use a 4-space-indented code block** instead of a triple-backtick fence — kit only skips backtick/tilde fences, so indentation-style code blocks still get substitution:

       git push -u origin "$(git branch --show-current)"
       gh pr create --title "fix: ... (#$1)" --base main

3. **Bind once, reference loosely**: put `Issue: $1` at the top in prose, then leave the backticked examples literal — the model will substitute mentally

## Steps

1. **Understand the workflow** the user described in $@ — ask a clarifying question if the intent is ambiguous
2. **Choose a filename**: short, lowercase, hyphen-separated, descriptive (e.g. `code-review.md`)
3. **Write the description**: one sentence, imperative, fits in autocomplete
4. **Decide on arguments**:
   - No args needed → omit placeholders entirely
   - One required value (issue number, PR url, file path) → use `$1`
   - Free-form trailing context → end with a single `$@` line
   - Multiple distinct values → use `$1`, `$2`, … and document each at the top
5. **Draft the body**:
   - Open with a single sentence stating the goal, weaving in `$1`/`$@` where the value belongs
   - Use `## Steps` for multi-step workflows; use plain prose for simple prompts
   - Be specific: name commands, flags, and file paths where relevant
   - **Audit every backtick and code fence**: any `$N` or `$@` inside them will not expand — was that intentional? If not, apply one of the workarounds above
6. **Write the file** to `.kit/prompts/<slug>.md`
7. **Verify substitution** by mentally (or actually) replacing `$1`/`$@` with a sample value and confirming every reference resolves — and that the prompt's *own* example snippets don't accidentally bump the required-arg count (wrap illustrative `$N` examples in triple-backtick fences, not 4-space indentation, so `RequiredArgs()` ignores them)
8. **Confirm** by showing the final file content and the slash command that activates it (e.g. `/code-review 42`)

## Guidelines

- Keep prompts action-oriented — they should tell kit *what to do*, not just *what to think about*
- Prefer concrete steps over vague instructions
- A prompt that does one thing well beats one that tries to cover every edge case
- If the workflow already exists as a prompt, suggest extending it instead of duplicating
- When in doubt about substitution behaviour, write the file and run `/<slug> testvalue` once to confirm — wrong placement of backticks is the #1 failure mode
