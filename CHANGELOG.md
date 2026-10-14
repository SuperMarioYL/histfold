# Changelog

All notable changes to histfold are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-10-14

First release. One binary that folds agent-era git history into reviewable,
task-attributed changesets.

### Added

- `histfold fold` — correlates the raw commit stream with Claude Code session
  logs, touched paths and commit trailers, prints a folded summary table
  (intent, agent and model, files, review status, confidence), and writes a
  self-contained foldable HTML timeline to `histfold.html`.
- `histfold fold --json` — machine-readable changesets on stdout for scripts.
- `histfold commits` — the normalized commit stream (hash, timestamp, author,
  files, agent provenance) as parsed by the ingest layer.
- Claude Code session discovery under `~/.claude/projects/<repo-slug>/`.
- Fold confidence tiers: 90% session+paths, 70% session time, 50% trailers,
  20% time gaps; review status from local refs only.
- `demo/seed.sh` — deterministic generator for a synthetic 212-commit
  agent-era repository with matching session logs.

[0.1.0]: https://github.com/SuperMarioYL/histfold/releases/tag/v0.1.0
