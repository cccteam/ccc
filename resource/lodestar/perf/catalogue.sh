#!/bin/bash
# The statement catalogue: every statement the application issued in the latest complete
# window, from Spanner's own query statistics, with executions, rows scanned and returned,
# and latency. Reads the real Spanner instance the environment names.
# Usage: perf/catalogue.sh [10MINUTE|HOUR] > catalogue.json
set -u
WINDOW=${1:-10MINUTE}
DB=${GOOGLE_CLOUD_SPANNER_DATABASE_NAME:?}; INSTANCE=${GOOGLE_CLOUD_SPANNER_INSTANCE_ID:?}; PROJECT=${GOOGLE_CLOUD_SPANNER_PROJECT:?}
gcloud spanner databases execute-sql "$DB" --instance="$INSTANCE" --project="$PROJECT" --format=json --sql="
SELECT interval_end, text_fingerprint, execution_count,
       avg_rows_scanned, avg_rows, avg_latency_seconds, avg_cpu_seconds, avg_bytes,
       text
FROM SPANNER_SYS.QUERY_STATS_TOP_${WINDOW}
WHERE interval_end = (SELECT MAX(interval_end) FROM SPANNER_SYS.QUERY_STATS_TOP_${WINDOW})
ORDER BY avg_rows_scanned DESC, execution_count DESC"
