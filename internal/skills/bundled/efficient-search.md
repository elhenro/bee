---
name: efficient-search
type: prompt
description: cut context spend when searching, finding, listing, or reading. recipes for grep/find/read flags, count_only, head/tail, ripgrep, tree. invoke before exploring an unfamiliar repo or a large file.
tools: [grep, find, read, ls, shell]
auto_approve: [grep, find, read, ls]
---
You are searching a codebase or file. Goal: get the answer with the smallest
context spend. Scout before you haul.

Golden rule
- Never read a whole file to find one thing. Grep first, read the slice.
- Never list a tree blind. Glob the extension, or `count_only` first.
- Cap output at the source. Don't trim after the fact.

Recipe table

| Task                                  | Use                                                                          |
|---------------------------------------|------------------------------------------------------------------------------|
| Where is symbol X defined/used?       | `grep` with pattern `\bX\b`, `glob` to language ext. Add `context: 2` only if needed. |
| How many hits, where?                 | `grep` with `count_only: true` — per-file counts, no match bodies.            |
| Find files by name                    | `find` with `name: "*.go"` — bounded, 500 cap.                                |
| Read known location                   | `read` with `offset` + `limit`. Default limit is generous; pick small.        |
| Tail a log                            | `read` with `tail: N`.                                                       |
| List a dir                            | `read` on the directory, or `ls` with explicit path. Not `find name:"*"`.    |
| Survey repo shape                     | `shell` → `tree -L 2 -I 'node_modules|.git|dist|build'` or `git ls-files | head -50`. |
| Word/line count of a file             | `shell` → `wc -l <path>`. Don't read to count.                                |
| Multi-pattern OR                      | `grep` regex `(foo|bar|baz)` — one tool call, not three.                      |
| Fuzzy/PCRE features bee grep lacks    | `shell` → `rg -n --max-count=50 -g '*.go' <pat>`.                              |

Flags worth knowing
- `grep.context` clamps 0..5. Use 0 unless you need surrounding lines.
- `grep.count_only` is the cheapest "is this anywhere in the repo" check.
- `grep.glob` accepts a single ext like `go` or `ts`. Narrow before pattern.
- `read.offset` is 1-based; `limit` is line count. Pair them when you have a
  line number from a prior grep — read the 40-line neighborhood, not 2000.
- `read.tail` reads from EOF — for logs, never read full + slice in head.
- `find.name` uses `filepath.Match`, not regex. `*.go` works; `.*\.go` does not.

Shell fallbacks (only when bee tools don't fit)
- `rg -n --max-count=N -g '*.ext' pat` — when you need PCRE, file-type sets,
  or multi-thread perf grep doesn't expose.
- `head -n N` / `tail -n N` — when piping; for files, prefer `read offset/limit`.
- `tree -L 2 -I 'pat'` — repo shape, depth-limited.
- `git ls-files | head` — fastest "what's in this repo" survey.
- Always cap with `| head -N` or `--max-count=N` when piping a search.

Anti-patterns (what burns context)
- `read` on a file >500 lines without `offset`/`limit`.
- `grep` then `read` the same file in full — you already have the line number.
- `find name: "*"` to list a dir — use `read` or `ls`.
- `shell` → `cat <bigfile>` — always use `read`.
- `shell` → `grep -r pat .` without `--max-count` — unbounded dump.
- Re-reading an unchanged file (read tool caches and will stub; don't fight it).
- Iterating with `read offset=1 limit=2000` → `offset=2001` → ... when one
  `grep` would have located the section.

Decision flow
1. Do I know the file? → `read` with offset/limit.
2. Do I know a symbol/string? → `grep` with glob + count_only first.
3. Do I know a filename pattern? → `find`.
4. None of the above? → repo shape: `tree -L 2` or `git ls-files | head`.
