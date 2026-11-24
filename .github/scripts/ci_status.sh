#!/usr/bin/env bash
# ci_status.sh [BRANCH] — the latest run of every workflow, per trigger
# event (push, schedule, workflow_dispatch, ...), on BRANCH (default
# master). Read-only (`gh run list`). Exit 1 when any latest completed run
# is not success/skipped: a red SCHEDULED workflow (weekly full-suite,
# nightly fuzz-soak, ...) is as much a red master as a red push.
#
# The coordinator runs it after every merge wave and at least daily
# (AGENTS.md §4). Columns: workflow, event, status/conclusion, age, run id,
# head sha.
set -euo pipefail
branch="${1:-master}"

rows="$(gh run list --branch "$branch" --limit 200 \
  --json workflowName,event,status,conclusion,createdAt,databaseId,headSha \
  --jq 'group_by(.workflowName + "|" + .event)
        | map(max_by(.createdAt))
        | sort_by(.workflowName, .event)
        | .[]
        | [.workflowName, .event,
           (if .status == "completed" then .conclusion else .status end),
           .createdAt, (.databaseId | tostring), .headSha[0:8]]
        | @tsv')"

# A schedule row of a workflow whose file (in this checkout) no longer has
# an active `cron:` line is history, not a live signal (stdlib-gate's
# schedule was removed under BUG-0033): shown as retired, never red.
root="$(git rev-parse --show-toplevel)"
paths="$(gh workflow list --all --json name,path --jq '.[] | [.name, .path] | @tsv')"
rows="$(printf '%s\n' "$rows" | while IFS=$'\t' read -r wf ev st rest; do
  if [ "$ev" = schedule ]; then
    path="$(printf '%s\n' "$paths" | awk -F'\t' -v n="$wf" '$1 == n { print $2; exit }')"
    if [ -z "$path" ] || ! grep -qE '^[[:space:]]*-[[:space:]]*cron:' "$root/$path" 2>/dev/null; then
      st="retired($st)"
    fi
  fi
  printf '%s\t%s\t%s\t%s\n' "$wf" "$ev" "$st" "$rest"
done)"

printf '%s\n' "$rows" | column -t -s $'\t'
red="$(printf '%s\n' "$rows" | awk -F'\t' '$3 != "success" && $3 != "skipped" && $3 !~ /^retired/ && $3 != "in_progress" && $3 != "queued" && $3 != "waiting" && $3 != "pending" && $3 != "requested"')"
if [ -n "$red" ]; then
  echo
  echo "RED (latest run per workflow/event on $branch):"
  printf '%s\n' "$red" | awk -F'\t' '{ printf "  %s [%s] %s: gh run view %s\n", $1, $2, $3, $5 }'
  exit 1
fi
