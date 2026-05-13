# Release Process

## 1. Land all changes on `master`

Ensure all code changes, grammar updates, and tests are committed and pushed.

## 2. Commit the version bump

Bump the version in all six manifest files — no other changes:

| File | Field |
|------|-------|
| `package.json` | `"version"` |
| `Cargo.toml` | `version` |
| `pyproject.toml` | `version` |
| `CMakeLists.txt` | `VERSION` |
| `Makefile` | `VERSION :=` |
| `tree-sitter.json` | `metadata.version` |

Commit message must be exactly:

```
Version vX.Y.Z
```

Push the commit.

## 3. Trigger the release workflow

Run the **Create Release** workflow from GitHub Actions (`workflow_dispatch`) with:

- **Tag** — `vX.Y.Z`
- **Title** — optional, defaults to the tag
- **Summary** — optional, prepended above auto-generated release notes
- **Pre-release** — check if applicable

The workflow generates the parser, runs the test suite, and creates a draft release. Publish the draft once reviewed.
