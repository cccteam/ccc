#!/bin/bash
# Summarize plans.sh output: one line per shape with the totals Spanner reports and the
# plan's decisive nodes (what it scans, whether it sorts, how it joins the subqueries).
# Usage: perf/summarize.sh <output-dir>...
set -u
for dir in "$@"; do
  echo "## $(basename "$dir")"
  printf '%-42s %9s %9s %6s %8s  %s\n' shape elapsed cpu rows scanned "scans | joins"
  for f in "$dir"/*.txt; do
    name=$(basename "$f" .txt)
    totals=$(awk -F'│' '/msecs/ && NF>5 {gsub(/ /,"",$2); gsub(/ /,"",$3); gsub(/ /,"",$4); gsub(/ /,"",$5); print $2, $3, $4, $5; exit}' "$f")
    scans=$(grep -o -E 'scan_target: [A-Za-z0-9_]+, scan_type: [A-Za-z]+' "$f" | sed -E 's/scan_target: //; s/, scan_type: / /' | sort | uniq -c | awk '{printf "%s×%s(%s) ", $2, $1, $3}')
    joins=$(grep -o -E 'RELATIONAL (Sort Limit|Sort|Hash Join|Cross Apply|Distributed Cross Apply|Hash Aggregate|Stream Aggregate|Filter Scan|Apply|Merge Join|Semi Apply|Anti Semi Apply|Distributed Merge Union)' "$f" | sed 's/RELATIONAL //' | sort | uniq -c | awk '{c=$1; $1=""; sub(/^ /,""); printf "%s×%s ", $0, c}')
    set -- $totals
    printf '%-42s %9s %9s %6s %8s  %s| %s\n' "$name" "${1:-?}" "${2:-?}" "${3:-?}" "${4:-?}" "$scans" "$joins"
  done
  echo
done
