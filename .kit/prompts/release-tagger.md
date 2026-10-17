---
description: Semantic version tagging workflow - analyzes commits and tags releases
---

# Release Tagging Workflow

Tag a new version of this Go project following semantic versioning.

## Steps

1. **Fetch remote tags**: `git fetch --tags origin`

2. **Find latest version**: `git tag -l | sort -V | tail -5` to see recent tags

3. **Analyze changes since last tag**:
   - `git log <latest-tag>..HEAD --oneline` - list commits
   - `git diff <latest-tag>..HEAD --stat` - see file stats
   - `git diff <latest-tag>..HEAD --name-only` - see changed files

4. **Determine version bump** (Semantic Versioning):
   - **MAJOR (X.0.0)**: Breaking API changes, incompatible modifications
   - **MINOR (0.X.0)**: New features, backward-compatible additions
   - **PATCH (0.0.X)**: Bug fixes, backward-compatible fixes
   
   Look for indicators:
   - `feat:` or `feature:` commits → MINOR
   - `fix:` or `bugfix:` commits → PATCH
   - `breaking:` or `BREAKING CHANGE:` → MAJOR
   - Breaking API changes in `pkg/` or public interfaces → MAJOR
   - New commands, flags, or features → MINOR
   - Documentation-only changes → PATCH (or skip)

5. **Calculate new version**: Increment appropriate segment, reset lower segments to 0

6. **Draft tag message**:
   - Summarize key changes from commits
   - Group by type (Features, Fixes, Breaking Changes)
   - Keep concise but informative

7. **Create annotated tag**: `git tag -a vX.Y.Z -m "vX.Y.Z - <summary>\n\n<detailed list>"`

8. **Push tag**: `git push origin vX.Y.Z`

## Guidelines

- Always fetch remote tags first to avoid conflicts
- Use annotated tags (`-a`) with descriptive messages
- Follow semver strictly - when in doubt, prefer conservative bump (patch over minor)
- For Go projects, changes to `pkg/` or exported APIs warrant careful version consideration
- If no changes since last tag, suggest skipping the release
- Include commit summaries in the tag message body

## Example Tag Message Format

```
v0.30.1 - Bug fixes for model handling and UI improvements

Fixes:
- Properly handle think tags from Qwen/DeepSeek models
- Handle custom provider model persistence and bare model names

Improvements:
- UI style refactoring and cleanup
```

Wait for the user to confirm the version and message before executing tag commands.

---

$@
