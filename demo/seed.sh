#!/usr/bin/env bash
# seed.sh generates a synthetic agent-era repository for histfold demos:
# 212 interleaved micro-commits — eight Claude-style task bursts backed by
# real session logs (written where histfold discovers them), one
# trailer-only Codex burst, and human follow-up commits interleaved into
# two of the bursts.
#
# Usage: demo/seed.sh [target-dir]        (default: ./histfold-demo)
#
# The repository is deterministic: content and timestamps are fixed, so
# `histfold fold` reproduces the demo output exactly on every machine.
# Session logs are written to ~/.claude/projects/<repo-slug>/ because that
# is where histfold discovers them; the script prints the exact cleanup
# commands when it finishes. Nothing else outside the target directory is
# touched.
set -euo pipefail

dir=${1:-histfold-demo}
if [ -e "$dir" ]; then
  echo "seed: $dir already exists — remove it first" >&2
  exit 1
fi

mkdir -p "$dir"
abs=$(cd "$dir" && pwd -P)
cd "$abs"

git init -q -b main
git config user.name "Demo Author"
git config user.email "demo@histfold.local"

slug=$(printf '%s' "$abs" | sed 's/[^A-Za-z0-9-]/-/g')
logdir="$HOME/.claude/projects/$slug"
if [ -e "$logdir/seed-histfold-1.jsonl" ]; then
  echo "seed: $logdir already holds seeded logs — remove them first" >&2
  exit 1
fi
mkdir -p "$logdir"

# Fixed base epoch: 2026-10-12T09:00:00Z. Every timestamp below is an offset
# from it; that is what keeps commit hashes and fold output reproducible.
BASE=1791795600
iso() { # epoch -> ISO 8601 UTC (BSD and GNU date)
  if date -u -r 0 +%s >/dev/null 2>&1; then date -u -r "$1" +%Y-%m-%dT%H:%M:%SZ
  else date -u -d "@$1" +%Y-%m-%dT%H:%M:%SZ; fi
}

CLAUDE_TRAILER='Co-Authored-By: Claude <claude@anthropic.com>'
CODEX_TRAILER=$'Agent: Codex\nAgent-Model: codex-max'

count=0
cm() { # cm <epoch> <file> <line> <subject> [trailer]
  local epoch=$1 file=$2 line=$3 subject=$4 trailer=${5:-}
  mkdir -p "$(dirname "$file")"
  printf '%s\n' "$line" >> "$file"
  git add -A
  if [ -n "$trailer" ]; then
    GIT_AUTHOR_DATE="@$epoch +0000" GIT_COMMITTER_DATE="@$epoch +0000" \
      git commit -q -m "$subject" -m "$trailer"
  else
    GIT_AUTHOR_DATE="@$epoch +0000" GIT_COMMITTER_DATE="@$epoch +0000" \
      git commit -q -m "$subject"
  fi
  count=$((count + 1))
}

logline() { printf '%s\n' "$2" >> "$logdir/$1.jsonl"; }
start_session() { # <id> <epoch> <prompt> <model>
  logline "$1" "{\"type\":\"user\",\"timestamp\":\"$(iso "$2")\",\"sessionId\":\"$1\",\"message\":{\"role\":\"user\",\"content\":\"$3\"}}"
}
log_tool() { # <id> <epoch> <model> <repo-relative path>
  logline "$1" "{\"type\":\"assistant\",\"timestamp\":\"$(iso "$2")\",\"message\":{\"role\":\"assistant\",\"model\":\"$3\",\"content\":[{\"type\":\"tool_use\",\"name\":\"Edit\",\"input\":{\"file_path\":\"$abs/$4\"}}]}}"
}
end_session() { # <id> <epoch> <model>
  logline "$1" "{\"type\":\"assistant\",\"timestamp\":\"$(iso "$2")\",\"message\":{\"role\":\"assistant\",\"model\":\"$3\",\"content\":[{\"type\":\"text\",\"text\":\"task complete\"}]}}"
}

FILES=()
SUBJECTS=()
# run_task <id> <session_start> <first_commit> <n> <model> <prompt>
#          <human_file> [human_step ...]
#
# Emits n commits one minute apart: the listed steps are plain human commits
# on <human_file>, the rest are Claude co-authored micro-commits cycling
# through FILES and SUBJECTS. A matching session log lands next to them so
# histfold's session correlation has real data to work with.
run_task() {
  local id=$1 start=$2 first=$3 n=$4 model=$5 prompt=$6 human=$7; shift 7
  local humans=" $* "
  local i file subject log=seed-histfold-$id
  start_session "$log" "$start" "$prompt" "$model"
  for file in "${FILES[@]}"; do log_tool "$log" "$start" "$model" "$file"; done
  for ((i = 1; i <= n; i++)); do
    epoch=$((first + (i - 1) * 60))
    file=${FILES[$(( (i - 1) % ${#FILES[@]} ))]}
    subject=${SUBJECTS[$(( (i - 1) % ${#SUBJECTS[@]} ))]}
    if [[ $humans == *" $i "* ]]; then
      cm "$epoch" "$human" "human follow-up on $file" "review: adjust after $subject"
    else
      cm "$epoch" "$file" "task $id change $i" "$subject" "$CLAUDE_TRAILER"
    fi
  done
  end_session "$log" $(( first + (n - 1) * 60 + 120 )) "$model"
}

# Task 1 — 09:02..09:29, four human follow-ups on a session file (paths
# overlap the session log, so this folds at 90% confidence).
FILES=(internal/api/users.go internal/api/pagination.go internal/api/users_test.go)
SUBJECTS=("api: decode cursor" "api: clamp page size" "api: filter hidden users" "api: cache list counts" "tests: cover cursor edges")
run_task 1 $((BASE + 0))     $((BASE + 120)) 28 "claude-sonnet-4-5" \
  "Add cursor pagination to the user list API" internal/api/users.go 7 13 19 25

# Task 2 — 10:02..10:31.
FILES=(internal/report/export.go internal/report/tz.go)
SUBJECTS=("export: normalize zones" "export: stamp UTC suffix" "export: fix DST drift" "tests: freeze clocks")
run_task 2 $((BASE + 3600))  $((BASE + 3720)) 30 "claude-sonnet-4-5" \
  "Fix the timezone bug in report exports" internal/report/export.go

# Task 3 — 11:02..11:27.
FILES=(internal/api/ratelimit.go internal/api/middleware.go internal/api/ratelimit_test.go)
SUBJECTS=("ratelimit: token bucket" "ratelimit: per-key limits" "middleware: reject 429" "tests: burst edges")
run_task 3 $((BASE + 7200))  $((BASE + 7320)) 26 "claude-opus-4-6" \
  "Add rate limiting middleware" internal/api/ratelimit.go

# Task 4 — 13:02..13:23.
FILES=(internal/config/config.go internal/config/loader.go cmd/server/main.go)
SUBJECTS=("config: single loader" "config: typed sections" "config: drop globals" "server: read config once")
run_task 4 $((BASE + 14400)) $((BASE + 14520)) 22 "claude-sonnet-4-5" \
  "Refactor config loading into one package" internal/config/config.go

# Task 5 — 14:12..14:39.
FILES=(internal/report/csv.go internal/api/export_csv.go)
SUBJECTS=("csv: stream rows" "csv: quote fields" "api: export route" "tests: header shape")
run_task 5 $((BASE + 18600)) $((BASE + 18720)) 28 "claude-haiku-4-5" \
  "Add a CSV export endpoint" internal/report/csv.go

# Task 6 — 15:22..15:39.
FILES=(internal/dashboard/query.go internal/dashboard/query_test.go)
SUBJECTS=("dashboard: batch queries" "dashboard: drop N+1 join" "dashboard: index lookup" "tests: query counts")
run_task 6 $((BASE + 22800)) $((BASE + 22920)) 18 "claude-opus-4-6" \
  "Fix the N+1 query on the dashboard" internal/dashboard/query.go

# Task 7 — 16:17..16:36.
FILES=(internal/api/trace.go internal/api/middleware.go)
SUBJECTS=("trace: request ids" "trace: span logs" "trace: propagate headers" "middleware: log latency")
run_task 7 $((BASE + 26100)) $((BASE + 26220)) 20 "claude-sonnet-4-5" \
  "Add request tracing to the API handlers" internal/api/trace.go

# Trailer-only burst — 17:20..17:31, no session log: folds at 50% from the
# Agent trailers alone.
for ((i = 1; i <= 12; i++)); do
  if ((i % 2 == 0)); then subject="cache: drop redundant gets"
  else subject="cache: inline lookups"; fi
  cm $((BASE + 30000 + (i - 1) * 60)) internal/api/cache.go "codex change $i" \
    "$subject" "$CODEX_TRAILER"
done

# Task 8 — 18:27..18:54 on an unmerged branch; the four human steps touch
# README.md, a path the session log never recorded (70% confidence).
git checkout -q -b feature/table-driven-tests
FILES=(internal/api/users_test.go internal/api/ratelimit_test.go internal/dashboard/query_test.go)
SUBJECTS=("tests: table users" "tests: table ratelimit" "tests: table dashboard" "tests: shared cases")
run_task 8 $((BASE + 33900)) $((BASE + 34020)) 28 "claude-sonnet-4-5" \
  "Migrate handler tests to table-driven style" README.md 6 12 18 24

if [ "$count" -ne 212 ]; then
  echo "seed: produced $count commits, expected 212" >&2
  exit 1
fi

cat <<EOF

seeded 212 commits in $abs
session logs: $logdir

try it:

  cd $abs
  histfold fold
  histfold fold --json | head -n 5
  open histfold.html

to clean up, remove the repo and the seeded session logs:

  rm -rf '$abs' '$logdir'
EOF
