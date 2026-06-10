# claude-code-sessions

Cross-platform TUI for browsing every local Claude Code session — CLI and Mac-app Local — in one place. Reads `~/.claude/projects/**/*.jsonl` directly (no hook, no DB, always in sync).

## Build

```sh
go build -o claude-code-sessions .
```

Cross-compile for Windows from Mac:

```sh
GOOS=windows GOARCH=amd64 go build -o claude-code-sessions.exe .
```

## Run

```sh
./claude-code-sessions
```

### Keys

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate |
| `enter` | Resume selected session |
| `d` | Resume + `--dangerously-skip-permissions` |
| `c` | Resume + `--chrome` |
| `D` | Resume + both flags |
| `/` | Fuzzy-search across sessions |
| `r` | Refresh |
| `q` | Quit |

## Shell wrapper

The binary prints a resume command (`cd <cwd> && claude [flags] --resume <id>`) to stdout on exit. Wrap it in a shell function so the resume happens in your current shell:

```sh
cs() { cmd=$(claude-code-sessions) && [ -n "$cmd" ] && eval "$cmd"; }
```

Add to `~/.zshrc` or `~/.bashrc`, reload, then type `cs` from anywhere.
