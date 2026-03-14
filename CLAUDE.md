# wikigen — GitHub Wiki Generator

## What is this?
A Go CLI that generates GitHub Wiki documentation from source code using `claude -p`.

## How to use
```bash
go build -o wikigen .
./wikigen owner/repo              # single repo
./wikigen -f repos.txt            # batch from file
./wikigen -retry                  # retry failed pages
./wikigen -dry-run -f repos.txt   # structure only, no generation
./wikigen -json -f repos.txt      # output results as JSON
```

## repos.txt format
```
owner/repo                    # standalone wiki
project:owner/repo1           # grouped into one wiki
project:owner/repo2
```

## Key flags
- `-p N` — parallel repos (default: 1)
- `-pp N` — parallel pages per repo (default: 3)
- `-model haiku|sonnet|opus` — Claude model
- `-lang ja|en|...` — output language
- `-dry-run` — determine structure only
- `-json` — JSON output to stdout
- `-retry` — retry failed pages only

## Environment variables (.env)
- `GITHUB_TOKEN` — GitHub PAT (optional, SSH used if empty)
- `CLAUDE_MODEL` — default model
- `WIKI_PARALLEL` — default repo parallelism
- `WIKI_PAGE_PARALLEL` — default page parallelism
- `WIKI_LANGUAGE` — default language

## Output
GitHub Wiki compatible files in `./wiki-output/{project}/`:
- `Home.md` — table of contents
- `_Sidebar.md` — navigation
- `{Page-Name}.md` — individual pages
- `_errors.log` — errors (if any)

## Important rules
- Always use `-dry-run` first to preview the structure before full generation
- Always check `_errors.log` after generation
- Use `-retry` for failed pages instead of regenerating everything
- Authentication: SSH by default, set `GITHUB_TOKEN` for PAT mode
