#!/bin/bash
# Capture the query plan and execution statistics of every shape in perf/shapes on the
# real Spanner instance the environment names (GOOGLE_CLOUD_SPANNER_*), in PROFILE mode:
# the statement runs, and Spanner returns its plan with per-node row counts and latency.
# Usage: perf/plans.sh <output-dir> [shape-name...]
set -u
OUT=${1:?output directory}; shift
mkdir -p "$OUT"
DB=${GOOGLE_CLOUD_SPANNER_DATABASE_NAME:?}; INSTANCE=${GOOGLE_CLOUD_SPANNER_INSTANCE_ID:?}; PROJECT=${GOOGLE_CLOUD_SPANNER_PROJECT:?}
cd "$(dirname "$0")/shapes" || exit 1
shapes=("$@"); [ ${#shapes[@]} -eq 0 ] && shapes=($(ls *.sql | sed 's/\.sql$//'))
for name in "${shapes[@]}"; do
  sql=$(grep -v '^--' "$name.sql")
  start=$(date +%s.%N)
  gcloud spanner databases execute-sql "$DB" --instance="$INSTANCE" --project="$PROJECT" --query-mode=PROFILE --sql="$sql" > "$OUT/$name.txt" 2>&1
  rc=$?
  printf '%-45s exit=%d wall=%.2fs\n' "$name" "$rc" "$(echo "$(date +%s.%N) - $start" | bc)"
done
