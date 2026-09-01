#!/bin/bash
# Waystation persona walkthrough: drives the exact request shapes the Angular pages
# issue, through real persona sessions (login, cookies, XSRF — the served stack, not
# the test router). Prints PASS/FAIL per check.
#
# Run it against a FRESHLY bootstrapped stack (overmind start, or the Procfile's
# spanner + server commands). Single-shot: several checks move workflow state, so a
# rerun needs a fresh bootstrap.
set -u
S=$(mktemp -d)
trap 'rm -rf "$S"' EXIT
B=${WAYSTATION_URL:-http://127.0.0.1:8082}/api
fails=0

login() {
  rm -f "$S/$1.jar"
  curl -s -L -c "$S/$1.jar" -b "$S/$1.jar" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"waystation\"}" "$B/user/login"
}

xsrf() { awk '$6=="XSRF-TOKEN" {print $7}' "$S/$1.jar" | tail -1; }

req() { # req <persona> <method> <url> [body]
  local p=$1 m=$2 u=$3 body=${4:-}
  if [ -n "$body" ]; then
    curl -s -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -H 'Content-Type: application/json' \
      -X "$m" -d "$body" -w '\n%{http_code}' "$u"
  else
    curl -s -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -X "$m" -w '\n%{http_code}' "$u"
  fi
}

check() { # check <label> <want-status> <got-response-with-trailing-status>
  local label=$1 want=$2 resp=$3
  local got=${resp##*$'\n'}
  if [ "$got" = "$want" ]; then
    echo "PASS  $label ($got)"
  else
    echo "FAIL  $label: status $got, want $want: $(echo "$resp" | head -1 | head -c 200)"
    fails=$((fails + 1))
  fi
}

body() { echo "${1%$'\n'*}"; }

for p in foreman-okafor procurement-chen auditor-voss chief-alpha quartermaster-idris commander tech-rivera; do
  login "$p"
done
echo "--- personas logged in ---"

# ---- foreman-okafor: requisition flow (the requisitions page shapes) ----
r=$(req foreman-okafor GET "$B/waystations/ws-alpha/catalog-items" || true)
# catalog items are global; page uses /catalog-items
r=$(req foreman-okafor GET "$B/catalog-items")
check "foreman lists catalog items (global page source)" 200 "$r"
scrubber=$(body "$r" | python3 -c "import json,sys; print(next(i['id'] for i in json.load(sys.stdin) if 'Scrubber' in i.get('name','') or 'scrubber' in i.get('name','')))" 2>/dev/null)
cost=$(body "$r" | python3 -c "import json,sys; print(next(i['unitCost'] for i in json.load(sys.stdin) if i['id']=='$scrubber'))")
[ -z "$scrubber" ] && scrubber=$(body "$r" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['id'])")

r=$(req foreman-okafor PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/requisitions\",\"value\":{\"justification\":\"gui walkthrough draft\",\"neededBy\":\"2026-09-20\",\"waystationId\":\"ws-alpha\"}}]")
check "foreman creates requisition draft (consolidated add)" 200 "$r"
reqid=$(body "$r" | python3 -c "import json,sys; print(json.load(sys.stdin)['requisitions'][0])")

r=$(req foreman-okafor PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/requisition-lines/$reqid/1\",\"value\":{\"catalogItemId\":\"$scrubber\",\"quantity\":3,\"unitCostSnapshot\":$cost}}]")
check "foreman adds line (client-key interleaved add)" 200 "$r"

r=$(req foreman-okafor POST "$B/waystations/ws-alpha/submit-requisition" "{\"requisitionId\":\"$reqid\"}")
check "foreman submits requisition (RPC)" 200 "$r"

r=$(req foreman-okafor GET "$B/waystations/ws-alpha/requisitions")
total=$(body "$r" | python3 -c "import json,sys; print(next(x['totalCost'] for x in json.load(sys.stdin) if x['id']=='$reqid'))")
if python3 -c "exit(0 if float('$total') > 0 else 1)"; then
  echo "PASS  submit recomputed server-owned total ($total)"
else
  echo "FAIL  total after submit = $total, want > 0"; fails=$((fails+1))
fi

r=$(req foreman-okafor PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/requisition-lines/$reqid/2\",\"value\":{\"catalogItemId\":\"$scrubber\",\"quantity\":1,\"unitCostSnapshot\":$cost}}]")
check "post-submit line edit refused (state=draft grant)" 403 "$r"

# foreman work-order create: new.priority <= 3
r=$(req foreman-okafor GET "$B/waystations/ws-alpha/assets")
asset=$(body "$r" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['id'])")
r=$(req foreman-okafor PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/work-orders\",\"value\":{\"waystationId\":\"ws-alpha\",\"assetId\":\"$asset\",\"title\":\"gui walkthrough low\",\"priority\":5}}]")
check "foreman creating priority 5 refused (new.priority <= 3)" 403 "$r"
r=$(req foreman-okafor PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/work-orders\",\"value\":{\"waystationId\":\"ws-alpha\",\"assetId\":\"$asset\",\"title\":\"gui walkthrough routine\",\"priority\":3}}]")
check "foreman creating priority 3 allowed" 200 "$r"

# ---- procurement-chen: approval limits (requisitions page Approve button) ----
r=$(req procurement-chen POST "$B/waystations/ws-alpha/approve-requisition" "{\"requisitionId\":\"$reqid\"}")
check "chen approves within limit" 200 "$r"
overhaul="90000000-0000-4000-8000-000000000003"
r=$(req procurement-chen POST "$B/waystations/ws-alpha/approve-requisition" "{\"requisitionId\":\"$overhaul\"}")
check "chen approving 7120 over 5000 limit refused" 403 "$r"

# ---- auditor-voss: masked cells and terminal-only rows ----
r=$(req auditor-voss GET "$B/waystations/ws-alpha/requisition-lines")
masked=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
approved={'90000000-0000-4000-8000-000000000004','$reqid'}
ok=all(('unitCostSnapshot' in r)==(r['requisitionId'] in approved) for r in rows)
print('ok' if ok and rows else 'bad')")
if [ "$masked" = "ok" ]; then
  echo "PASS  auditor sees unitCostSnapshot only on approved lines (masked = absent key)"
else
  echo "FAIL  auditor masked-cell shape wrong"; fails=$((fails+1))
fi
r=$(req auditor-voss GET "$B/waystations/ws-alpha/incident-reports")
pii=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print('ok' if rows and all('reporterContact' not in r for r in rows) else 'bad')")
if [ "$pii" = "ok" ]; then
  echo "PASS  auditor incident rows arrive without reporterContact (PII withheld)"
else
  echo "FAIL  auditor PII shape wrong"; fails=$((fails+1))
fi
r=$(req auditor-voss GET "$B/waystations/ws-alpha/work-orders")
terminal=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print('ok' if rows and all(r['statusId'] in ('completed','cancelled') for r in rows) else 'bad')")
if [ "$terminal" = "ok" ]; then
  echo "PASS  auditor sees terminal work orders only"
else
  echo "FAIL  auditor work-order visibility wrong"; fails=$((fails+1))
fi

# ---- chief-alpha: full work-order arc (work-orders page shapes) ----
r=$(req chief-alpha PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/work-orders\",\"value\":{\"waystationId\":\"ws-alpha\",\"assetId\":\"$asset\",\"title\":\"gui arc\",\"summary\":\"walkthrough\",\"priority\":1}}]")
check "chief creates draft work order" 200 "$r"
woid=$(body "$r" | python3 -c "import json,sys; print(json.load(sys.stdin)['workOrders'][0])")

r=$(req chief-alpha PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/work-order-tasks/$woid/1\",\"value\":{\"instructions\":\"gui task\",\"done\":false}}]")
check "chief adds task (gui next-number shape)" 200 "$r"

team=$(req chief-alpha GET "$B/waystations/ws-alpha/teams" | head -1 | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['id'])")
due=$(date -u -d '+2 days' +%Y-%m-%dT%H:%M:%S.000Z)
r=$(req chief-alpha POST "$B/waystations/ws-alpha/schedule-work-order" \
  "{\"workOrderId\":\"$woid\",\"assignedTeamId\":\"$team\",\"dueAt\":\"$due\"}")
check "chief schedules (RPC with gui ISO dueAt)" 200 "$r"
r=$(req chief-alpha POST "$B/waystations/ws-alpha/start-work-order" "{\"workOrderId\":\"$woid\"}")
check "chief starts" 200 "$r"
r=$(req chief-alpha PATCH "$B/resources" \
  "[{\"op\":\"patch\",\"path\":\"/waystations/ws-alpha/work-order-tasks/$woid/1\",\"value\":{\"done\":true}}]")
check "task toggled done while in progress" 200 "$r"
r=$(req chief-alpha POST "$B/waystations/ws-alpha/complete-work-order" "{\"workOrderId\":\"$woid\"}")
check "chief completes" 200 "$r"
r=$(req chief-alpha PATCH "$B/resources" "[{\"op\":\"remove\",\"path\":\"/waystations/ws-alpha/work-orders/$woid\"}]")
check "chief deleting a completed order refused (delete gated to drafts)" 403 "$r"
draft="80000000-0000-4000-8000-000000000003"
r=$(req chief-alpha PATCH "$B/resources" "[{\"op\":\"remove\",\"path\":\"/waystations/ws-alpha/work-orders/$draft\"}]")
check "chief deletes the seeded draft" 200 "$r"

# ---- quartermaster-idris: logistics page ----
pending="b0000000-0000-4000-8000-000000000001"
arrived="b0000000-0000-4000-8000-000000000002"
r=$(req quartermaster-idris POST "$B/waystations/ws-alpha/receive-shipment" "{\"shipmentId\":\"$pending\"}")
check "quartermaster receives in-transit shipment" 200 "$r"
r=$(req quartermaster-idris POST "$B/waystations/ws-alpha/receive-shipment" "{\"shipmentId\":\"$pending\"}")
check "second receive refused (arrivedAt IS NULL gate)" 400 "$r"
expired="a0000000-0000-4000-8000-000000000002"
fresh="a0000000-0000-4000-8000-000000000001"
noexp="a0000000-0000-4000-8000-000000000003"
r=$(req quartermaster-idris PATCH "$B/resources" "[{\"op\":\"remove\",\"path\":\"/waystations/ws-alpha/inventory-lots/$expired\"}]")
check "quartermaster deletes expired lot" 200 "$r"
r=$(req quartermaster-idris PATCH "$B/resources" "[{\"op\":\"remove\",\"path\":\"/waystations/ws-alpha/inventory-lots/$fresh\"}]")
check "deleting fresh lot refused (expiry gate)" 403 "$r"
r=$(req quartermaster-idris PATCH "$B/resources" "[{\"op\":\"remove\",\"path\":\"/waystations/ws-alpha/inventory-lots/$noexp\"}]")
check "deleting no-expiry lot refused (NULL never matches)" 403 "$r"

# ---- tech-rivera: subject-set + derived tenancy on the board ----
r=$(req tech-rivera GET "$B/waystations/ws-alpha/work-orders")
alpha=$(body "$r" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
r=$(req tech-rivera GET "$B/waystations/ws-beta/work-orders")
beta=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print('ok' if all(r['waystationId']=='ws-beta' for r in rows) else 'bad', len(rows))")
echo "INFO  tech-rivera board: ws-alpha rows=$alpha, ws-beta=$beta (teams partitioned per waystation)"
r=$(req tech-rivera GET "$B/waystations/ws-alpha/assets")
zones=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print('ok' if rows and all(r['id'] != '60000000-0000-4000-8000-000000000005' for r in rows) else 'bad')")
if [ "$zones" = "ok" ]; then
  echo "PASS  technician asset list excludes the reactor-zone manifold (2-hop zone join)"
else
  echo "FAIL  technician zone filtering wrong"; fails=$((fails+1))
fi

# ---- commander: dashboard + status board sources ----
r=$(req commander GET "$B/fleet-summaries")
check "commander fleet summary (computed, global)" 200 "$r"
n=$(body "$r" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
echo "INFO  fleet summary rows: $n"
r=$(req commander GET "$B/waystations/ws-alpha/station-status-boards")
check "commander station status board (computed, domain)" 200 "$r"
r=$(req commander PATCH "$B/resources" \
  "[{\"op\":\"add\",\"path\":\"/waystations/ws-alpha/incident-reports\",\"value\":{\"waystationId\":\"ws-alpha\",\"summary\":\"gui walkthrough incident\",\"severity\":2,\"reporterContact\":\"deck 4 comms\",\"rawStatement\":\"observed variance\"}}]")
check "commander files incident (incidents page shape)" 200 "$r"
r=$(req commander GET "$B/waystations/ws-alpha/incident-reports")
case_ok=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
mine=[r for r in rows if r.get('summary')=='gui walkthrough incident']
print('ok' if mine and mine[0].get('caseNumber','').startswith('IR-') and 'rawStatement' not in mine[0] else 'bad')")
if [ "$case_ok" = "ok" ]; then
  echo "PASS  incident came back with server case number, without the input-only statement"
else
  echo "FAIL  incident server-field shape wrong"; fails=$((fails+1))
fi

# ---- audit trail (manual resource: hand-written route, @manualAddResource grant) ----
r=$(req auditor-voss GET "$B/audit-trail-entries")
check "auditor lists the audit trail (RecordsAuditor)" 200 "$r"
audit=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print('ok' if rows and all('tableName' in r and 'eventSource' in r for r in rows) else 'bad')")
if [ "$audit" = "ok" ]; then
  echo "PASS  audit trail carries the walkthrough's change events"
else
  echo "FAIL  audit trail shape wrong"; fails=$((fails+1))
fi
r=$(req commander GET "$B/audit-trail-entries")
check "commander lists the audit trail" 200 "$r"
r=$(req foreman-okafor GET "$B/audit-trail-entries")
check "foreman refused the audit trail (no grant)" 403 "$r"

# ---- logistics server-side sort (reserved sort parameter, gui shape) ----
r=$(req quartermaster-idris GET "$B/waystations/ws-alpha/inventory-lots?sort=expiresOn")
check "quartermaster lists lots sorted server-side" 200 "$r"

# ---- suppliers row filtering (config-driven page, VendorBrowser vs manager) ----
r=$(req foreman-okafor GET "$B/suppliers")
redline=$(body "$r" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print('absent' if all(r['id'] != '10000000-0000-4000-8000-000000000003' for r in rows) else 'present')")
echo "INFO  inactive supplier for foreman (VendorBrowser active=true filter): $redline"

echo
if [ "$fails" -eq 0 ]; then echo "ALL CHECKS PASSED"; else echo "$fails CHECK(S) FAILED"; fi
