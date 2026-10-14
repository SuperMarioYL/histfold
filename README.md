**English** | [简体中文](./README.zh-CN.md)

<div align="center">

<img src="https://readme-typing-svg.demolab.com?font=JetBrains+Mono&weight=700&size=26&pause=1600&color=4A7BD0&center=true&vCenter=true&width=780&lines=One+agent+task.+Forty+micro-commits.;Fold+it+into+one+reviewable+changeset." alt="One agent task. Forty micro-commits. Fold it into one reviewable changeset.">

# Histfold

**Fold agent-era git history into reviewable task changesets.**

Your coding agent commits a hundred times a day, and git history stops being a
reviewable record. Histfold folds the raw commit stream into task-attributed
changesets, so maintainers can review what each agent did as one unit and
expand back to raw commits on demand.

[![CI](https://img.shields.io/github/actions/workflow/status/SuperMarioYL/histfold/ci.yml?branch=main&label=CI)](https://github.com/SuperMarioYL/histfold/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/SuperMarioYL/histfold)](https://github.com/SuperMarioYL/histfold/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-blue)](#quickstart)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

Free and open source · single static binary · zero configuration · read-only

</div>

## Why fold history

An agent task a human would once have shipped as one deliberate commit now
arrives as dozens of micro-commits, interleaved with human edits and carrying
no machine-readable task attribution. Scrolling `git log` reconstructs nothing:
the commits are individually meaningless, and the task they belonged to exists
only in a session log somewhere on disk.

The tools built around commit messages cannot fix this. Changelog generators
and `git log` filters group by message text, and agent micro-commits drown in
generated filler. Session replay explains one session to the person who ran
it, but never folds a repository's whole commit stream into task-level units.
And squashing rewrites the very record you are trying to review.

Histfold is the retroactive fold. It correlates three independent signals —
agent session logs, touched paths, and agent trailers in commit messages — to
rebuild task units from history that is already written, and presents them as
a strictly read-only view. Commits are never touched; a changeset is just a
lens over them.

## Quickstart

```bash
go install github.com/SuperMarioYL/histfold@latest
cd your-repo
histfold fold
open histfold.html
```

`histfold fold` reads `git log` and auto-discovers Claude Code session logs
under `~/.claude/projects/<repo-slug>/`, prints a folded summary, and writes a
self-contained `histfold.html` — a foldable timeline where each changeset is a
collapsed card that expands back to its raw commits. No config file, nothing
to set up, and nothing in the repository is modified.

Requirements: Go 1.24+ to install (or grab a prebuilt binary from
[Releases](https://github.com/SuperMarioYL/histfold/releases)), git 2.30+ on
PATH, macOS or Linux.

Repos without session logs still fold: agent trailers and time-gap clustering
take over, at lower confidence. Want to see a full-strength fold without
waiting for your own history? Run the [demo](#demo).

## Demo

No agent-heavy repo at hand? `demo/seed.sh` generates a synthetic one: 212
interleaved micro-commits — eight Claude task bursts with matching session
logs (written to `~/.claude/projects/`, where histfold discovers them), a
trailer-only Codex burst, human follow-ups, and one burst left on an unmerged
branch.

```bash
git clone https://github.com/SuperMarioYL/histfold
cd histfold
demo/seed.sh
cd histfold-demo
histfold fold
```

Output from the recorded run (see [docs/demo-results.json](docs/demo-results.json)
for the full, reproducible record):

```text
212 commits → 9 changesets
22 files touched, +212/-0

INTENT                                       AGENT   MODEL              FILES  REVIEW   CONF  COMMITS
Add cursor pagination to the user list API   Claude  claude-sonnet-4-5  3      on-main  90%   28
Fix the timezone bug in report exports       Claude  claude-sonnet-4-5  2      on-main  90%   30
Add rate limiting middleware                 Claude  claude-opus-4-6    3      on-main  90%   26
Refactor config loading into one package     Claude  claude-sonnet-4-5  3      on-main  90%   22
Add a CSV export endpoint                    Claude  claude-haiku-4-5   2      on-main  90%   28
Fix the N+1 query on the dashboard           Claude  claude-opus-4-6    2      on-main  90%   18
Add request tracing to the API handlers      Claude  claude-sonnet-4-5  2      on-main  90%   20
cache: inline lookups                        Codex   codex-max          1      on-main  50%   12
Migrate handler tests to table-driven style  Claude  claude-sonnet-4-5  4      branch   70%   28

confidence: 90% session+paths · 70% session time · 50% trailers · 20% time gaps
```

The same run, scripted: `histfold fold --json | head -n 5` emits
machine-readable changesets, and `histfold fold` also writes `histfold.html`,
which opens offline in any browser — zero external requests, expand any card
back to its raw commits. A recorded terminal session is checked in at
[assets/demo.cast](assets/demo.cast) (asciicast v3 — play it with
`asciinema play assets/demo.cast`); it covers the terminal steps, while the
timeline itself lives in your browser.

The seed is deterministic — fixed content and timestamps — so this exact fold
reproduces on any machine. To clean up, remove the repo directory and the
session log directory the seeder prints when it finishes.

## Usage

```text
histfold fold            # terminal summary + writes histfold.html
histfold fold --json     # changesets as pretty-printed JSON on stdout
histfold commits         # normalized commit stream with agent provenance
histfold version
```

`histfold commits` shows the raw material: hash, timestamp, author, touched
files, and the agent attribution detected in each message (trailer,
`Co-Authored-By`, or generated-with footer).

### Reading the fold

Each changeset carries a confidence score — the strength of the weakest
evidence used to attribute it:

| Confidence | Evidence |
|---|---|
| 90% | commit falls inside a matched session's activity window and its paths overlap the session's touched files |
| 70% | commit falls inside the window, but paths could not confirm the match |
| 50% | agent provenance from commit trailers only, no session matched |
| 20% | time-gap clustering with no agent signal at all |

The intent is the first user prompt of the matched session — the task's own
words — falling back to the cluster-head commit subject. Review status
(`on-main`, `branch`, `unknown`) is derived from local refs only; no network
is contacted.

### What it reads and writes

Reads: `git log --numstat` (via the system git binary) and
`~/.claude/projects/<repo-slug>/*.jsonl` session logs for the current
repository. Writes: `histfold.html` in the current directory, and whatever
`--json` prints to stdout. That is all — no hooks, no config, no git
mutation, working tree untouched.

## How it works

One Go binary, three internal packages, no servers or daemons:

```text
            histfold (single binary)
┌───────────┬──────────────┬────────────────┐
│ ingest    │ fold         │ render         │
│ system    │ pure         │ terminal table │
│ git exec  │ correlation  │ + embedded     │
│ + claude  │ functions,   │ timeline.html  │
│ jsonl +   │ no I/O       │ (stdlib tpl,   │
│ trailers  │              │ vanilla JS)    │
└───────────┴──────────────┴────────────────┘
```

The ingest layer shells out to the system git (robust across every repo
format without vendoring a VCS library) and adapts Claude Code session logs.
The fold engine is a pure correlation function over that data — the only
place folding policy lives, trivially testable. The renderer prints a terminal
table and a self-contained HTML timeline from an embedded template: vanilla
JS fold/expand, zero CDN requests, works offline.

v0.1 is deliberately single-player: your repositories, your session logs.
Folding correlates provenance found on the local machine, and only Claude
Code JSONL logs are adapted today.

## Roadmap

- Session adapters beyond Claude Code JSONL (Codex, Cursor logs).
- Cross-contributor folding for team repositories — needs a story for sharing
  session logs, so it is roadmap, not promise.
- Windows builds (v0.1 is macOS and Linux only).

## License

[MIT](./LICENSE) © 2026 SuperMarioYL

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
