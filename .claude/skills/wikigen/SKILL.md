---
name: wikigen
description: Generate GitHub Wiki documentation from source code repositories. Use when the user asks to create wiki, generate documentation, document a codebase, or create technical docs for a repository.
argument-hint: "[owner/repo or repos.txt path]"
disable-model-invocation: true
allowed-tools: Bash(./wikigen *), Read, Glob
---

# wikigen — GitHub Wiki Generator

Generate GitHub Wiki from source code using Claude Code.

## Quick Start

```bash
# Build if needed
go build -o wikigen .

# Preview structure first (recommended)
./wikigen -dry-run owner/repo

# Generate wiki
./wikigen owner/repo

# Local directory (no clone needed)
./wikigen /path/to/local/repo
./wikigen .

# Batch from file
./wikigen -f repos.txt

# Retry failed pages
./wikigen -retry
```

## Key Flags

| Flag | Default | Description |
|---|---|---|
| `-f` | - | Repository list file |
| `-p` | 1 | Parallel repos |
| `-pp` | 3 | Parallel pages per repo |
| `-model` | - | Claude model (haiku, sonnet, opus) |
| `-lang` | ja | Output language |
| `-dry-run` | false | Structure only, no generation |
| `-json` | false | JSON output to stdout |
| `-local` | - | Local directory (skip clone) |
| `-retry` | false | Retry failed pages only |

## repos.txt Format

```
# GitHub repos (cloned)
owner/repo

# Local directories (no clone)
/path/to/local/repo
./relative/path

# Multi-repo (grouped into one wiki)
project:owner/repo1
project:/path/to/local
```

## Environment Variables (.env)

- `GITHUB_TOKEN` — GitHub PAT (optional, SSH used if empty)
- `CLAUDE_MODEL` — default model
- `WIKI_PARALLEL` — repo parallelism
- `WIKI_PAGE_PARALLEL` — page parallelism
- `WIKI_LANGUAGE` — output language

## Output

GitHub Wiki compatible files in `./wiki-output/{project}/`:
- `Home.md` — table of contents
- `_Sidebar.md` — navigation
- `{Page-Name}.md` — individual pages
- `_errors.log` — errors (if any)

## Important Rules

- Always use `-dry-run` first to preview the wiki structure before full generation
- Always check `_errors.log` after generation
- Use `-retry` for failed pages instead of regenerating everything
- SSH is default for cloning. Set `GITHUB_TOKEN` in `.env` for PAT mode
