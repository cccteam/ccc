#!/bin/bash
# Lodestar persona walkthrough: drives every persona's proof by curl through real
# sessions (login, cookies, XSRF: the served stack, not the test router), plus the droid
# channel under the API key and the client portal under its own prefix through the
# simulated directory. Prints PASS/FAIL per check.
#
# Run it against a FRESHLY bootstrapped stack (overmind start, or the Procfile's spanner +
# server commands, both under -tags skipAuth) with the .envrc variables exported here
# too (APP_DROIDS_API_KEY opens the droid channel; APP_USERNAME=client and APP_ROLES name
# the simulated directory's answer). Single-shot: several checks move workflow state, so
# a rerun needs a fresh bootstrap: a new emulator, or `go run -tags skipAuth
# ./cmd/bootstrap -reset` against a database that already carries the schema (the real
# instance case; README, "Running against a real Spanner instance"), run with the server
# stopped and the server started again after it. The overdue flip waits for the seeded
# three-minute mission unless LODESTAR_SKIP_FLIP=1.
#
# Demonstrates: walkthrough.
set -u
S=$(mktemp -d)
trap 'rm -rf "$S"' EXIT
B=${LODESTAR_URL:-http://127.0.0.1:${PORT:-8090}}
API=$B/api
PORTAL=$B/portal/api
DROIDS=$B/droids
KEY=${APP_DROIDS_API_KEY:-}
fails=0

login() { # login <persona>: the crew's password login under the console prefix
  local p=$1
  rm -f "$S/$p.jar"
  # -L: the XSRF handshake answers a 307 set-and-retry; -c saves the jar.
  curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"username\":\"$p\",\"password\":\"lodestar\"}" "$API/user/login"
}

login_portal() { # login_portal <persona>: the directory login, simulated under skipAuth (APP_USERNAME on the server)
  local p=$1
  rm -f "$S/$p.jar"
  # The login route sends the browser to the directory, which returns it to the registered
  # callback (the dev proxy's host in .envrc); the walkthrough talks to the server directly,
  # so it follows the callback on the server's own host.
  local location
  location=$(curl -s -D - -o /dev/null -c "$S/$p.jar" -b "$S/$p.jar" "$PORTAL/user/login?returnUrl=/portal/tracker" | awk 'tolower($1)=="location:" {print $2}' | tr -d '\r')
  location="$B/${location#*://*/}"
  curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -o /dev/null "$location"
  curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -o /dev/null "$PORTAL/user/session"
}

xsrf() { # xsrf <persona>: the auth's own XSRF cookie, crew-xsrf for the crew, members-xsrf for the client
  local name=crew-xsrf
  [ "$1" = client ] && name=members-xsrf
  awk -v n="$name" '$6==n {print $7}' "$S/$1.jar" | tail -1
}

req() { # req <persona> <method> <url> [body] [extra curl args...]
  local p=$1 m=$2 u=$3 body=${4:-}; shift 4 2>/dev/null || shift $#
  if [ -n "$body" ]; then
    curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -H 'Content-Type: application/json' \
      -X "$m" -d "$body" -w '\n%{http_code}' "$@" "$u"
  else
    curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -X "$m" -w '\n%{http_code}' "$@" "$u"
  fi
}

dryrun() { req "$1" "$2" "$3" "$4" -H 'X-Dry-Run: true'; }

upload() { # upload <persona> <url> <json> <file>...: a multipart @upload, the request part first, a file part per file
  local p=$1 u=$2 json=$3; shift 3
  local parts=(); for f in "$@"; do parts+=(-F "file=@$f"); done
  curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -F "request=$json;type=application/json" "${parts[@]}" -w '\n%{http_code}' "$u"
}

droid() { # droid <method> <url> [body]
  local m=$1 u=$2 body=${3:-}
  curl -s -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -X "$m" ${body:+-d "$body"} -w '\n%{http_code}' "$u"
}

check() { # check <label> <want-status> <response-with-trailing-status>
  local label=$1 want=$2 resp=$3
  local got=${resp##*$'\n'}
  if [ "$got" = "$want" ]; then
    echo "PASS  $label ($got)"
  else
    echo "FAIL  $label: status $got, want $want: $(echo "$resp" | head -1 | head -c 200)"
    fails=$((fails + 1))
    if [ -n "${LODESTAR_DEBUG:-}" ]; then ls "$S"; for j in "$S"/*.jar; do echo "$j: $(grep -c 'xsrf' "$j") xsrf line(s)"; done; fi
  fi
}

body() { echo "${1%$'\n'*}"; }

py() { python3 -c "import json,sys; rows=json.load(sys.stdin); $1"; }

assert_py() { # assert_py <label> <response> <python expression over rows -> bool>
  local label=$1 resp=$2 expr=$3
  if body "$resp" | py "sys.exit(0 if ($expr) else 1)" 2>/dev/null; then
    echo "PASS  $label"
  else
    echo "FAIL  $label: $(body "$resp" | head -c 200)"; fails=$((fails + 1))
  fi
}

ANVIL=$API/sectors/anvil
HAULER=80000000-0000-4000-8000-000000000001
CORVID=80000000-0000-4000-8000-000000000002
CONVOY=80000000-0000-4000-8000-000000000003
COURIER=80000000-0000-4000-8000-000000000004
QUARANTINE=80000000-0000-4000-8000-000000000008
HAMMER=50000000-0000-4000-8000-000000000001
TONGS=50000000-0000-4000-8000-000000000002
KINGFISHER=70000000-0000-4000-8000-000000000001
DOCK_ONE=60000000-0000-4000-8000-000000000001
QUARANTINE_BAY=60000000-0000-4000-8000-000000000002
LANTERN=70000000-0000-4000-8000-000000000003
LANTERN_REFIT=a0000000-0000-4000-8000-000000000001
MULE_REFIT=a0000000-0000-4000-8000-000000000002
SAMARITAN_REFIT=a0000000-0000-4000-8000-000000000003
POD_BOND=b0000000-0000-4000-8000-000000000001
DRONES_BOND=b0000000-0000-4000-8000-000000000002
BULLION_BOND=b0000000-0000-4000-8000-000000000003
HALVARD=10000000-0000-4000-8000-000000000001
MERIDIAN=10000000-0000-4000-8000-000000000002
BASTION_RELAY=10000000-0000-4000-8000-000000000003
CONVOY_SORTIE=90000000-0000-4000-8000-000000000001

for p in governor marshal cadet pilot veteran lead dispatcher overseer booking wingco engineer quartermaster supercargo salvor yeoman archivist assessor hazards dock watch; do
  login "$p"
done
login_portal client
echo "--- personas logged in (crew by password, the client through the simulated directory) ---"

# ---- the two auths ----
r=$(req client GET "$PORTAL/user/session"); assert_py "cleo is signed in through the directory" "$r" "rows['authenticated'] and rows['username']=='client'"
r=$(req client GET "$ANVIL/missions"); check "a client's session is a stranger to the console" 401 "$r"
r=$(req marshal GET "$PORTAL/user-domains"); check "a crew session is a stranger to the portal" 401 "$r"

# ---- the star chart: user-domains ----
r=$(req cadet GET "$API/user-domains"); check "cadet's chart lights Anvil only" 200 "$r"
assert_py "cadet's chart is [anvil]" "$r" "rows == ['anvil']"
r=$(req archivist GET "$API/user-domains"); assert_py "archivist's chart lights all three" "$r" "rows == ['anvil','bastion','cinder']"
r=$(req marshal GET "$API/sectors/cinder/missions"); check "Cinder is dark for the marshal: the concealed-sector guard answers 404 (the star chart's labelled bypass shows this)" 404 "$r"

# ---- flight deck: per-persona boards, paged from the descriptor ----
r=$(req cadet GET "$ANVIL/missions?capabilities=Execute&limit=200"); check "cadet lists missions" 200 "$r"
assert_py "cadet sees hazard 1 and 2 only, fourteen of them" "$r" "len(rows)==14 and all(m['hazard'] in (1,2) for m in rows)"
assert_py "cadet's Claim lights on the open low-hazard rows only" "$r" "all(('ClaimMission' in m['zzCapabilities']['Execute']) == (m['statusId']=='open') for m in rows)"
r=$(req marshal GET "$ANVIL/missions"); assert_py "the default page is the descriptor's twenty-five" "$r" "len(rows)==25"
r=$(req pilot GET "$ANVIL/missions?limit=200"); assert_py "pilot: clearance 3 and certifications decide" "$r" "sorted(m['id'][-3:] for m in rows) == ['001','002','004','006','008','012','013','017','019','023','024','027','028','029','032','033']"
r=$(req veteran GET "$ANVIL/missions?limit=200"); assert_py "veteran: NOT (hazard IN (1,2) OR fee < 5000)" "$r" "sorted(m['id'][-3:] for m in rows) == ['002','003','005','007','014','015','016','019','024','025','026','030','031']"
r=$(req wingco GET "$ANVIL/missions?limit=200"); assert_py "wingco: hazard >= 4" "$r" "sorted(m['id'][-3:] for m in rows) == ['003','005','015','016','020','021','025','026','030','031']"
r=$(req wingco GET "$ANVIL/squadrons"); assert_py "wingco: wing IN subject.wings (Forge Wing's two)" "$r" "len(rows)==2"
r=$(req lead GET "$ANVIL/missions?capabilities=Execute,Create&limit=200"); assert_py "lead: Hammer's missions" "$r" "sorted(m['id'][-3:] for m in rows) == ['002','003','005','014','018','020','022','028']"
assert_py "lead: Add sortie lights on the underway convoy only" "$r" "all((m['zzCapabilities']['Create']==['Sorties']) == (m['statusId']=='underway') for m in rows)"
r=$(req archivist GET "$ANVIL/missions?limit=200"); assert_py "archivist: fee redacted until completed" "$r" "rows and all(('fee' in m) == (m['statusId']=='completed') for m in rows)"
r=$(req archivist GET "$ANVIL/missions?sort=fee"); check "the archivist sorts by fee: the masked cells run over the visible projection" 200 "$r"
assert_py "masked fees fall to the NULL region, first ascending in Spanner's placement" "$r" "[('fee' in m) for m in rows] == [False,False,False,False,True,True,True]"
r=$(req archivist GET "$ANVIL/missions?filter=sectorId:eq:anvil,fee:isnull"); assert_py "fee isnull matches exactly the redacted rows" "$r" "len(rows)==4 and all('fee' not in m for m in rows)"
# The assessor's grid sorts by a hazard it does not display. The key is a named variant
# of INT64 (HazardLevel), concealing, hers only while a mission is open, and left out of
# columns=, so every page turns on the cursor's copy decoded into the field's own type,
# and the missions no longer open walk the NULL region, first in Spanner's placement.
r=$(req assessor GET "$ANVIL/missions?columns=id&limit=200"); every=$(body "$r" | py "print(','.join(sorted(m['id'] for m in rows)))")
page=$(curl -s -D "$S/hazard.h" -L -b "$S/assessor.jar" -H "X-XSRF-TOKEN: $(xsrf assessor)" "$ANVIL/missions?columns=id,title&sort=hazard&limit=2")
seen=$(echo "$page" | py "print(','.join(m['id'] for m in rows))"); shape=$(echo "$page" | py "print(all(sorted(m)==['id','title'] for m in rows))")
next=$(grep -i '^Link:' "$S/hazard.h" | sed -n 's/.*<\([^>]*\)>; rel="next".*/\1/p')
while [ -n "$next" ]; do
  page=$(curl -s -D "$S/hazard.h" -L -b "$S/assessor.jar" -H "X-XSRF-TOKEN: $(xsrf assessor)" "$B$next")
  seen="$seen,$(echo "$page" | py "print(','.join(m['id'] for m in rows))")"
  [ "$(echo "$page" | py "print(all(sorted(m)==['id','title'] for m in rows))")" = True ] || shape=False
  next=$(grep -i '^Link:' "$S/hazard.h" | sed -n 's/.*<\([^>]*\)>; rel="next".*/\1/p')
done
if [ "$(echo "$seen" | tr ',' '\n' | sort | paste -sd,)" = "$every" ] && [ "$shape" = True ]; then echo "PASS  the assessor's hazard sort, a named-variant key outside the projection, pages every Anvil mission once, rows carrying id and title alone"; else echo "FAIL  assessor hazard walk: seen=$seen shape=$shape"; fails=$((fails+1)); fi
r=$(req quartermaster GET "$ANVIL/missions?sort=fee"); check "the quartermaster, granted no fee, is refused the sort naming the field" 403 "$r"
r=$(req marshal GET "$ANVIL/missions?capabilities=Execute&limit=200"); assert_py "marshal: every legal edge lit on the underway convoy" "$r" "{'CompleteMission','FailMission','HoldMission'} <= set(next(m for m in rows if m['id']=='$CONVOY')['zzCapabilities']['Execute'])"
r=$(req marshal GET "$ANVIL/missions?limit=201"); check "a page over Missions' declared maximum of 200 is refused, never clamped" 400 "$r"
r=$(req marshal GET "$ANVIL/missions?limit=all"); check "Missions declares a maximum, so limit=all is refused" 400 "$r"
r=$(req marshal GET "$ANVIL/missions?offset=5"); check "offset is refused; the cursor is its replacement" 400 "$r"
r=$(req marshal GET "$API/pilots?limit=all"); assert_py "the crew roster declares no maximum: limit=all answers every pilot" "$r" "len(rows)==17"
# Every paged list request carries an order. The hull catalog and the briefing catalog declare no @order, so a bare GET or a limit on either without a sort is refused naming the two ways out; limit=all reads each whole and unsorted (the hulls in Spanner's own order, the sheets in the catalog's sequence), and a requested sort pages them by cursor.
r=$(req marshal GET "$API/ship-classes"); check "the hull catalog declares no order: a bare GET is refused" 400 "$r"
assert_py "the refusal names the resource and the two ways out" "$r" "rows=={'message':'ShipClasses declares no order; add a sort, or ask limit=all'}"
r=$(req marshal GET "$API/ship-classes?limit=2"); check "a page of the hull catalog without a sort is refused too" 400 "$r"
page=$(curl -s -D "$S/hulls.h" -L -b "$S/marshal.jar" -H "X-XSRF-TOKEN: $(xsrf marshal)" "$API/ship-classes?limit=all")
if [ "$(echo "$page" | py "print(len(rows))")" = 4 ] && ! grep -qi '^Link:' "$S/hulls.h"; then echo "PASS  limit=all reads the whole hull catalog, unsorted, with no Link header"; else echo "FAIL  hull catalog whole read: $(echo "$page" | head -c 200) $(tr '\n' ' ' < "$S/hulls.h" | head -c 300)"; fails=$((fails+1)); fi
page=$(curl -s -D "$S/hulls.h" -L -b "$S/marshal.jar" -H "X-XSRF-TOKEN: $(xsrf marshal)" "$API/ship-classes?sort=designation&limit=2")
grep -qi '^Link:.*rel="next"' "$S/hulls.h" && echo "PASS  a requested sort pages the hull catalog by cursor" || { echo "FAIL  hull catalog sorted: no Link header"; fails=$((fails+1)); }
r=$(req marshal GET "$API/briefing-templates"); check "the briefing catalog declares no order: a bare GET is refused" 400 "$r"
r=$(req marshal GET "$API/briefing-templates?limit=all"); assert_py "read whole, the briefing catalog lists in its own sequence, the standard sheet first, neither by name nor by key" "$r" "[t['id'] for t in rows]==['standard','hazard-first','client-facing','dispatch']"
r=$(req marshal GET "$API/briefing-templates?sort=name"); assert_py "a requested sort orders the briefing catalog by name" "$r" "[t['name'] for t in rows]==sorted(t['name'] for t in rows)"
r=$(req dispatcher GET "$ANVIL/client-rosters/$HALVARD?columns=id,name,contactCount"); check "the client roster, a keyed view, serves a read: the dispatcher reads Halvard Freight's roster row" 200 "$r"
assert_py "the roster row carries the picker's display columns" "$r" "rows['name']=='Halvard Freight' and rows['contactCount']==1"
# The console's pickers read on the maximum each source declares, from the generated descriptor. The roster and the hangars declare one: a picker pages them (the first page with its count and its Link relations, never limit=all) and reads the chosen row by key, and the Ships page's Hangar column resolves a page's hangars with one in filter; the hull catalog declares none, so its picker and the Class column read it whole with limit=all. Demonstrates: picker.paged, picker.whole, column.referenced-in.
page=$(curl -s -D "$S/roster.h" -L -b "$S/dispatcher.jar" -H "X-XSRF-TOKEN: $(xsrf dispatcher)" "$ANVIL/client-rosters?columns=id,name,contactCount&limit=2&count=true")
if [ "$(echo "$page" | py "print(len(rows))")" = 2 ] && grep -qi '^Total-Count: 4' "$S/roster.h" && grep -qi '^Link:.*rel="next"' "$S/roster.h"; then echo "PASS  the roster picker's first page: two of four, the count, and a next relation"; else echo "FAIL  roster picker page: $(echo "$page" | head -c 200) $(tr '\n' ' ' < "$S/roster.h" | head -c 300)"; fails=$((fails+1)); fi
r=$(req dispatcher GET "$ANVIL/client-rosters?columns=id,name&limit=all"); check "the roster is never read whole: limit=all is refused under its maximum" 400 "$r"
r=$(req marshal GET "$ANVIL/hangars?columns=id,name,zone&count=true"); assert_py "the hangar picker's page lists Anvil's two hangars in their declared order" "$r" "[h['name'] for h in rows]==['Anvil Dock One','Quarantine Bay']"
r=$(req marshal GET "$ANVIL/hangars/$DOCK_ONE?columns=id,name"); assert_py "the chosen hangar is read by key, whichever page the picker is on" "$r" "rows['name']=='Anvil Dock One'"
r=$(req marshal GET "$ANVIL/hangars?filter=id:in:($DOCK_ONE,$QUARANTINE_BAY)&columns=id,name&limit=2"); assert_py "the Ships page's Hangar column resolves a page's hangars with one in filter" "$r" "sorted(h['name'] for h in rows)==['Anvil Dock One','Quarantine Bay']"
r=$(req marshal GET "$API/ship-classes?columns=id,designation&limit=all"); assert_py "the class picker reads the hull catalog whole, id and designation alone" "$r" "len(rows)==4 and all(set(c)=={'id','designation'} for c in rows)"

# ---- the flight deck's edges, dry runs first ----
r=$(dryrun lead POST "$ANVIL/hold-mission" "{\"missionId\":\"$CONVOY\",\"reason\":\"debris on the lane\"}"); check "lead's dry run of Hold is refused in the notes grant's words: the armed write, before anything is touched" 403 "$r"
r=$(dryrun marshal POST "$ANVIL/hold-mission" "{\"missionId\":\"$CONVOY\",\"reason\":\"debris on the lane\"}"); check "the marshal's dry run of Hold would commit" 200 "$r"
r=$(req lead GET "$ANVIL/missions?limit=200"); assert_py "the dry run left the convoy underway" "$r" "next(m for m in rows if m['id']=='$CONVOY')['statusId']=='underway'"
r=$(req cadet POST "$ANVIL/claim-mission" "{\"missionId\":\"$QUARANTINE\",\"squadronId\":\"$TONGS\"}"); check "cadet claims a hazard-2 mission" 200 "$r"
r=$(req pilot POST "$ANVIL/claim-mission" "{\"missionId\":\"$CONVOY\",\"squadronId\":\"$TONGS\"}"); check "pilot refused the hazard-4 convoy (grant, not body)" 403 "$r"
r=$(req lead POST "$ANVIL/hold-mission" "{\"missionId\":\"$CONVOY\",\"reason\":\"debris on the lane\"}"); check "lead may Execute Hold but holds no Update on the notes: the armed body refuses" 403 "$r"
r=$(req marshal POST "$ANVIL/hold-mission" "{\"missionId\":\"$CONVOY\",\"reason\":\"debris on the lane\"}"); check "the marshal holds the convoy" 200 "$r"
r=$(req quartermaster PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/sortie-expenses\",\"value\":{\"sortieId\":\"$CONVOY_SORTIE\",\"category\":\"fuel\",\"amount\":100}}]"); check "quartermaster refused while on hold (two hops deep)" 403 "$r"
r=$(req lead POST "$ANVIL/resume-mission" "{\"missionId\":\"$CONVOY\"}"); check "lead resumes the convoy (the loop)" 200 "$r"
r=$(req quartermaster PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/sortie-expenses\",\"value\":{\"sortieId\":\"$CONVOY_SORTIE\",\"category\":\"fuel\",\"amount\":100}}]"); check "quartermaster books an expense while underway" 200 "$r"
r=$(req booking POST "$ANVIL/stand-down-mission" "{\"missionId\":\"$CONVOY\"}"); check "booking cannot stand down the marshal's booking" 403 "$r"
r=$(req marshal POST "$ANVIL/stand-down-mission" "{\"missionId\":\"$CONVOY\"}"); check "nobody stands down an underway mission (hold it first)" 403 "$r"
r=$(req booking POST "$ANVIL/stand-down-mission" "{\"missionId\":\"$HAULER\"}"); check "booking stands down her own open booking" 200 "$r"
printf 'Three survey barges through the debris belt; hold formation at the belt edge.' > "$S/brief.txt"
r=$(upload marshal "$ANVIL/attach-mission-document" "{\"missionId\":\"$CONVOY\",\"title\":\"Escort brief\"}" "$S/brief.txt"); check "marshal attaches the escort brief (a multipart @upload the transaction claims)" 200 "$r"
r=$(req marshal GET "$ANVIL/mission-documents?filter=missionId:eq:$CONVOY"); assert_py "the brief is listed with its store key" "$r" "rows[0]['title']=='Escort brief' and rows[0]['fileName']=='brief.txt' and rows[0]['storeKey']"
# The BYTES column rides as one base64 string (the generated interface says string, display type bytes), the SHA-256 of the file.
assert_py "the brief's digest is the SHA-256 of its bytes, one base64 string on the wire" "$r" "__import__('base64').b64decode(rows[0]['digest'])==__import__('hashlib').sha256(open('$S/brief.txt','rb').read()).digest()"
DOC=$(body "$r" | py "print(rows[0]['id'])")
r=$(req marshal GET "$ANVIL/mission-documents/$DOC/content"); check "the brief downloads through the application's own route" 200 "$r"
r=$(dryrun lead POST "$ANVIL/complete-mission" "{\"missionId\":\"$CONVOY\"}"); check "lead's dry run of Complete would commit (the Paymaster's checker posts the settlement)" 200 "$r"
r=$(req lead POST "$ANVIL/complete-mission" "{\"missionId\":\"$CONVOY\"}"); check "lead completes Hammer's convoy (the method answers with the settlement)" 200 "$r"
assert_py "the settlement is the fee less the booked expenses" "$r" "rows['fee']=='15000' and rows['expenses']=='1600' and rows['net']=='13400'"
r=$(req marshal GET "$ANVIL/missions?limit=200"); assert_py "the convoy completed with its settlement posted through the Paymaster role" "$r" "next(m for m in rows if m['id']=='$CONVOY')['statusId']=='completed' and next(m for m in rows if m['id']=='$CONVOY')['settlement']=='13400'"
r=$(req archivist GET "$ANVIL/ships-log-entries"); assert_py "the ship's log names 'lead as role Paymaster' on the settlement" "$r" "any('lead as role Paymaster' in e['eventSource'] for e in rows)"

# ---- dispatcher: the two-grant PATCH ----
r=$(req dispatcher PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$QUARANTINE\",\"value\":{\"notes\":\"client called\",\"deadline\":\"2026-12-01T00:00:00Z\"}}]"); check "dispatcher extends a deadline and notes it" 200 "$r"
r=$(req dispatcher PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$QUARANTINE\",\"value\":{\"deadline\":\"2026-09-05T00:00:00Z\"}}]"); check "pulling a deadline in is refused (new.deadline >= deadline)" 403 "$r"
r=$(req dispatcher PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$QUARANTINE\",\"value\":{\"assignedSquadronId\":\"50000000-0000-4000-8000-000000000004\"}}]"); check "assigning a foreign squadron is refused (new.x IN subject.set)" 403 "$r"

# ---- booking: fee limit ----
r=$(req booking PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/missions\",\"value\":{\"clientId\":\"$HALVARD\",\"kindId\":\"courier\",\"title\":\"Walkthrough booking\",\"hazard\":1,\"fee\":26000,\"deadline\":\"2027-01-01T00:00:00Z\"}}]"); check "booking over the fee limit is refused" 403 "$r"
r=$(req booking PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/missions\",\"value\":{\"clientId\":\"$HALVARD\",\"kindId\":\"courier\",\"title\":\"Walkthrough booking\",\"hazard\":1,\"fee\":9000,\"deadline\":\"2027-01-01T00:00:00Z\"}}]"); check "booking within the fee limit" 200 "$r"

# ---- the briefing: the client-form method ----
r=$(req marshal POST "$ANVIL/compile-briefing" '{"includeHazards":true}'); check "the marshal compiles a briefing (a method that runs outside a transaction)" 200 "$r"
assert_py "the marshal's sheet: every mission, every fee, the hazard board" "$r" "rows['missions']==31 and rows['feesRedacted']==0 and len(rows['hazardBoard'])>0"
r=$(req archivist POST "$ANVIL/compile-briefing" '{"includeHazards":true}'); check "the archivist compiles hers" 200 "$r"
assert_py "the archivist's sheet counts the redactions her grant makes (the hauler stood down above joined the four), the hazard board is withheld" "$r" "rows['missions']==9 and rows['feesRedacted']==5 and rows['hazardWithheld']"
r=$(dryrun marshal POST "$ANVIL/compile-briefing" '{}'); check "a dry run of the client-form method is refused: nothing to roll back" 400 "$r"
r=$(req cadet POST "$ANVIL/compile-briefing" '{}'); check "the cadet holds no briefing" 403 "$r"

# ---- the ledger: the pushdown computed resource ----
r=$(req governor GET "$API/service-ledgers"); assert_py "the ledger lists sectors by fees outstanding, pushed into SQL" "$r" "[l['sectorId'] for l in rows]==['anvil','bastion','cinder']"
r=$(req governor GET "$API/service-ledgers?filter=name:eq:Bastion"); assert_py "a filter on the ledger is taken into the statement" "$r" "[l['sectorId'] for l in rows]==['bastion']"
r=$(req governor GET "$API/service-ledgers?sort=name:desc&limit=1"); assert_py "a page of one, sorted, pushed down" "$r" "[l['sectorId'] for l in rows]==['cinder']"

# ---- standing orders: the key-less list ----
# StandingOrders declares no @primarykey, so it is a whole read-only list: a bare GET serves the book in its own order, a sort orders it, limit=all is the explicit spelling of the same shape, and a numeric limit or a cursor is refused naming the key as the way to page. There is no read route and no Read permission to grant.
r=$(req yeoman GET "$API/standing-orders"); check "the yeoman reads the standing orders: a key-less list, served whole on a bare GET" 200 "$r"
assert_py "the whole book in its own order, section by section, neither alphabetical nor keyed" "$r" "[o['section'] for o in rows]==['General','General','Flight','Flight','Hangar','Salvage']"
r=$(req yeoman GET "$API/standing-orders?sort=section"); assert_py "a requested sort orders the book by section" "$r" "[o['section'] for o in rows]==sorted(o['section'] for o in rows) and len(rows)==6"
r=$(req yeoman GET "$API/standing-orders?limit=all"); assert_py "limit=all is the explicit spelling of the one shape" "$r" "len(rows)==6"
r=$(req yeoman GET "$API/standing-orders?limit=10"); check "a key-less list does not page: a limit is refused" 400 "$r"
assert_py "the refusal names the resource, the missing key, and the way to page" "$r" "rows=={'message':'StandingOrders declares no primary key, so its list is served whole and does not page; drop the limit, or declare @primarykey to page'}"
r=$(req yeoman GET "$API/standing-orders?cursor=v4.local.anything"); check "a cursor is refused the same way" 400 "$r"
r=$(req yeoman GET "$API/standing-orders/General"); check "a key-less list has no read route" 404 "$r"
r=$(req cadet GET "$API/standing-orders"); check "the cadet holds no orders desk" 403 "$r"

# ---- hangar deck ----
r=$(req engineer PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/refits/$LANTERN_REFIT\",\"value\":{\"estimate\":1000}}]"); check "engineer's estimate refused before inspection" 403 "$r"
r=$(req engineer POST "$ANVIL/inspect-ship" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "engineer inspects the Lantern (the method answers a typed report)" 200 "$r"
assert_py "the inspection report names the ship" "$r" "rows['shipName']=='Lantern'"
r=$(req engineer PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/refits/$LANTERN_REFIT\",\"value\":{\"estimate\":1000.6}}]"); check "estimate lands after inspection (rounded)" 200 "$r"
r=$(req engineer POST "$ANVIL/begin-refit" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "begin refit" 200 "$r"
r=$(req engineer POST "$ANVIL/start-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "start flight test (marked-file method)" 200 "$r"
r=$(req engineer POST "$ANVIL/fail-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "fail flight test (the backward edge)" 200 "$r"
r=$(req engineer POST "$ANVIL/start-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "start flight test again" 200 "$r"
r=$(req engineer POST "$ANVIL/pass-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "pass flight test stamps LastRefitAt" 200 "$r"
r=$(req engineer POST "$ANVIL/scrap-ship" "{\"refitId\":\"$MULE_REFIT\"}"); check "engineer holds no Scrap" 403 "$r"
r=$(req marshal POST "$ANVIL/scrap-ship" "{\"refitId\":\"$MULE_REFIT\"}"); check "marshal scraps from inspected" 200 "$r"
r=$(req marshal POST "$ANVIL/scrap-ship" "{\"refitId\":\"$SAMARITAN_REFIT\"}"); check "marshal scraps from in_refit (three chutes into one pit)" 200 "$r"
r=$(req pilot POST "$ANVIL/hail-ship" "{\"shipId\":\"$KINGFISHER\"}"); check "pilot hails a docked ship (the Touch answers No Content)" 204 "$r"
r=$(req pilot POST "$ANVIL/hail-ship" "{\"shipId\":\"$LANTERN\"}"); check "pilot cannot hail the quarantined Lantern" 403 "$r"
# The ARRAY<INT64> column rides as a JSON array of numbers (the generated interface says number[]); a ship with no bays carries [].
r=$(req marshal GET "$ANVIL/ships?limit=100"); assert_py "the Stubborn Mule's cargo bays are a JSON array of numbers and the Kingfisher's an empty array" "$r" "next(s for s in rows if s['registry']=='LS-202')['cargoBays']==[60,60,30] and next(s for s in rows if s['registry']=='LS-101')['cargoBays']==[]"
r=$(req marshal GET "$ANVIL/ships?sort=cargoBays:asc"); check "a sort naming the array column answers 400" 400 "$r"
# A nullable ARRAY<STRING(16)> column typed by the plain slice: NULL (Tongs has not filed) and [] (a stated none) are different answers, and a null in a PATCH is accepted where the column allows it and refused where it does not.
r=$(req marshal GET "$ANVIL/squadrons"); assert_py "Hammer's callsigns are filed and Tongs's are null (not yet filed)" "$r" "next(s for s in rows if s['name']=='Hammer')['callsigns']==['Hammerfall','Anvil Actual'] and next(s for s in rows if s['name']=='Tongs')['callsigns'] is None"
r=$(req marshal PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/squadrons/$TONGS\",\"value\":{\"callsigns\":[]}}]"); check "marshal files Tongs as flying silent ([] is a stated answer)" 200 "$r"
r=$(req marshal PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/squadrons/$TONGS\",\"value\":{\"callsigns\":null}}]"); check "a null clears the filing (the column allows NULL, the request struct says so)" 200 "$r"
r=$(req marshal GET "$ANVIL/squadrons/$TONGS"); assert_py "Tongs reads back null after the clear" "$r" "rows['callsigns'] is None"
r=$(req marshal PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/ships/$KINGFISHER\",\"value\":{\"cargoBays\":null}}]"); check "a null into the NOT NULL array column is refused at decode" 400 "$r"
r=$(req dock GET "$ANVIL/refits"); d=${r##*$'\n'}
r=$(req watch GET "$ANVIL/refits"); n=${r##*$'\n'}
if { [ "$d" = 200 ] && [ "$n" = 403 ]; } || { [ "$d" = 403 ] && [ "$n" = 200 ]; }; then echo "PASS  exactly one shift sees the hangar deck (dara=$d nadia=$n)"; else echo "FAIL  shift pair: dara=$d nadia=$n"; fails=$((fails+1)); fi

# ---- squadrons config page: the array config's request ----
# The Squadrons page's "Other squadrons in this sector" is an arrayConfig over the same resource, its listFilter excluding the page's row by the indexed key; Squadrons declares a maximum, so the library asks one server page (the key column alone, with the count) and draws each row by the iterated config. Demonstrates: config.array.
r=$(req marshal GET "$ANVIL/squadrons?columns=id&filter=id:ne:$HAMMER&count=true"); check "the other squadrons in Anvil: one server page of the squadrons that are not Hammer" 200 "$r"
assert_py "the page holds Tongs alone, its key and nothing else" "$r" "[s['id'] for s in rows]==['$TONGS'] and all(set(s)=={'id'} for s in rows)"

# ---- salvage hold: the nullable walk, the receipt ----
page=$(curl -s -D "$S/hold.h" -L -b "$S/supercargo.jar" -H "X-XSRF-TOKEN: $(xsrf supercargo)" "$ANVIL/consignments?limit=4")
echo "$page" | py "sys.exit(0 if all(c['releasedAt'] is not None for c in rows) and len(rows)==4 else 1)" && echo "PASS  the hold's first page, releasedAt desc: released cargo first, unreleased (NULL) last in Spanner's placement" || { echo "FAIL  hold page 1: $(echo "$page" | head -c 200)"; fails=$((fails+1)); }
next=$(grep -i '^Link:' "$S/hold.h" | sed -n 's/.*<\([^>]*\)>; rel="next".*/\1/p')
seen=$(echo "$page" | py "print(','.join(c['id'] for c in rows))")
count=4
while [ -n "$next" ]; do
  page=$(curl -s -D "$S/hold.h" -L -b "$S/supercargo.jar" -H "X-XSRF-TOKEN: $(xsrf supercargo)" "$B$next")
  count=$((count + $(echo "$page" | py "print(len(rows))")))
  seen="$seen,$(echo "$page" | py "print(','.join(c['id'] for c in rows))")"
  next=$(grep -i '^Link:' "$S/hold.h" | sed -n 's/.*<\([^>]*\)>; rel="next".*/\1/p')
done
if [ "$count" = 13 ] && [ "$(echo "$seen" | tr ',' '\n' | sort -u | wc -l)" = 13 ]; then echo "PASS  the walk crosses the NULL boundary: thirteen rows, each once"; else echo "FAIL  hold walk: $count rows, $(echo "$seen" | tr ',' '\n' | sort -u | wc -l) distinct"; fails=$((fails+1)); fi
r=$(req supercargo GET "$ANVIL/consignments?sort=releasedAt&limit=5"); assert_py "ascending puts the unreleased cargo (NULL) first in Spanner's placement, five of them on this page" "$r" "len(rows)==5 and all(c['releasedAt'] is None for c in rows)"
r=$(req supercargo POST "$ANVIL/release-consignment" "{\"consignmentId\":\"$POD_BOND\"}"); check "supercargo releases a consignment (the armed manifest read, a typed receipt)" 200 "$r"
assert_py "the receipt names the bond" "$r" "rows['bondCode']=='BND-ANV-0001' and rows['releasedAt']"
r=$(req supercargo POST "$ANVIL/release-consignment" "{\"consignmentId\":\"$POD_BOND\"}"); check "second release is the frame's uniform Forbidden" 403 "$r"
r=$(req supercargo PATCH "$API/resources" "[{\"op\":\"remove\",\"path\":\"/sectors/anvil/consignments/$DRONES_BOND\"}]"); check "supercargo disposes of expired bond" 200 "$r"
r=$(req supercargo PATCH "$API/resources" "[{\"op\":\"remove\",\"path\":\"/sectors/anvil/consignments/$BULLION_BOND\"}]"); check "live bond cannot be disposed of" 403 "$r"
r=$(req salvor GET "$API/clients"); assert_py "salvor's roster is the covered and undecided outfits (Halvard, Meridian, Bastion Relay), never Vellum's refused cover" "$r" "sorted(c['id'] for c in rows) == sorted(['$HALVARD','$MERIDIAN','$BASTION_RELAY']) and all('insured' in c for c in rows)"

# ---- call log: create-form narrowing ----
r=$(req cadet GET "$API/permission-digest?domain=anvil"); assert_py "cadet's digest narrows the call form to summary and severity" "$r" "'DistressCalls.summary' in rows and 'DistressCalls.callerContact' not in rows"
r=$(req cadet PATCH "$API/resources" '[{"op":"add","path":"/sectors/anvil/distress-calls","value":{"summary":"Debris on the approach","severity":2}}]'); check "cadet files a two-field call" 200 "$r"

# ---- droid channel ----
if [ -n "$KEY" ]; then
  r=$(droid POST "$DROIDS/sectors/anvil/ingest-droid-reports" "{\"shipId\":\"$KINGFISHER\",\"subsystem\":\"hull\",\"reading\":0.95,\"recordedAt\":\"2026-09-04T12:00:00Z\"}"); check "droid posts a reading (one per call; the payload is flat)" 200 "$r"
  r=$(droid POST "$DROIDS/sectors/anvil/ingest-droid-reports" "{\"shipId\":\"$KINGFISHER\",\"subsystem\":\"reactor\",\"reading\":0.55,\"recordedAt\":\"2026-09-04T12:01:00Z\"}"); check "droid posts a second reading" 200 "$r"
  r=$(droid POST "$DROIDS/sectors/anvil/ingest-droid-reports" "{\"shipId\":\"$KINGFISHER\",\"reading\":0.9}"); check "a reading without a subsystem is refused by the body" 400 "$r"
  r=$(droid GET "$DROIDS/sectors/anvil/droid-reports"); check "droid lists its channel" 200 "$r"
  r=$(req marshal GET "$ANVIL/droid-reports"); check "droid reports have no human route" 404 "$r"
  r=$(req hazards GET "$ANVIL/sector-hazard-boards?limit=all"); assert_py "hazard board shows the worst hull reading" "$r" "any(b['shipName']=='Kingfisher' and b['subsystem']=='hull' and b['worstReading']==0.95 for b in rows)"
  assert_py "the board carries the recent readings behind the worst, newest first" "$r" "any(b['subsystem']=='hull' and b['shipName']=='Kingfisher' and [x['value'] for x in b['recent']][0]==0.95 for b in rows)"
  r=$(req hazards GET "$ANVIL/sector-hazard-boards?columns=recent.value"); check "nothing inside the nested field is a column" 400 "$r"
  r=$(req hazards GET "$ANVIL/sector-hazard-boards?filter=shipName:eq:Kingfisher,subsystem:eq:reactor"); assert_py "the board is filtered like a table: the body takes the ship name, the handler the subsystem" "$r" "[(b['shipName'],b['subsystem']) for b in rows]==[('Kingfisher','reactor')]"
  r=$(req hazards GET "$ANVIL/sector-hazard-boards?limit=1"); assert_py "the board pages: the worst reading first, one row" "$r" "len(rows)==1 and rows[0]['worstReading']==0.95"
  r=$(droid POST "$DROIDS/sectors/anvil/release-consignment" "{\"consignmentId\":\"b0000000-0000-4000-8000-000000000005\"}"); check "the droid releases bond through the shared method, reading the manifest under its own grant" 200 "$r"
  assert_py "the droid's receipt" "$r" "rows['bondCode']=='BND-ANV-0005'"
  r=$(droid GET "$DROIDS/sectors/anvil/droid-reports?offset=2"); check "offset is refused on the droid channel too" 400 "$r"
  r=$(curl -s -X POST -H 'Content-Type: application/json' -d '{}' -w '\n%{http_code}' "$DROIDS/sectors/anvil/ingest-droid-reports"); check "the droid channel refuses without the key" 401 "$r"
else
  echo "SKIP  droid channel: set APP_DROIDS_API_KEY on the server and here"
fi

# ---- portal ----
r=$(req client GET "$PORTAL/user-domains"); assert_py "cleo's portal lists every sector: the directory's roles are swept across the roster" "$r" "rows == ['anvil','bastion','cinder']"
r=$(req client GET "$PORTAL/sectors/anvil/missions?capabilities=Execute&limit=200"); check "cleo tracks Halvard's missions" 200 "$r"
assert_py "portal width excludes assignedSquadronId/notes/settlement" "$r" "rows and all('assignedSquadronId' not in m and 'settlement' not in m for m in rows)"
assert_py "Stand down lights only on her company's open, claimed, or on-hold rows" "$r" "all(('StandDownMission' in m['zzCapabilities']['Execute']) == (m['statusId'] in ('open','claimed','on_hold')) for m in rows)"
page=$(curl -s -D "$S/portal.h" -L -b "$S/client.jar" -H "X-XSRF-TOKEN: $(xsrf client)" "$PORTAL/sectors/anvil/missions?limit=4")
grep -qi '^Link:.*rel="next"' "$S/portal.h" && echo "PASS  the portal tracker pages with the Link header under its own prefix" || { echo "FAIL  portal paging: no Link header"; fails=$((fails+1)); }
r=$(req client POST "$PORTAL/sectors/anvil/stand-down-mission" "{\"missionId\":\"$QUARANTINE\"}"); check "cleo stands down her company's claimed mission" 200 "$r"
r=$(req client POST "$PORTAL/sectors/anvil/stand-down-mission" "{\"missionId\":\"$CORVID\"}"); check "cleo cannot stand down Meridian's mission" 403 "$r"
r=$(req client PATCH "$PORTAL/resources" '[{"op":"add","path":"/sectors/anvil/distress-calls","value":{"summary":"Hauler drifting","severity":4,"callerContact":"cleo@halvard.example"}}]'); check "cleo files a call with her contact" 200 "$r"
r=$(req client GET "$PORTAL/sectors/anvil/refits"); check "refits are not a portal member" 404 "$r"
r=$(req client GET "$PORTAL/sectors/anvil/client-statements"); check "cleo reads her company's statement (the portal-only manual resource)" 200 "$r"
assert_py "the statement carries the stand-down of her company's mission" "$r" "any(l['missionId']=='$QUARANTINE' for l in rows)"
r=$(req marshal GET "$ANVIL/client-statements"); check "the statement is not mounted on the console" 404 "$r"

# ---- ship's log ----
r=$(req archivist GET "$ANVIL/ships-log-entries"); check "archivist reads the ship's log" 200 "$r"
assert_py "the hail is in the log as a touch" "$r" "any(e['tableName']=='Ships' and list((e.get('changeSet') or {}).keys())==['UpdatedAt'] for e in rows)"
r=$(req cadet GET "$ANVIL/ships-log-entries"); check "cadet holds no log grant" 403 "$r"

# ---- bulletin ----
r=$(req marshal POST "$API/issue-bulletin" '{"announcement":"All hands: drill at 0600"}'); check "bulletin officer issues a bulletin" 200 "$r"
r=$(req cadet POST "$API/issue-bulletin" '{"announcement":"unauthorized"}'); check "cadet refused the bulletin" 403 "$r"

# ---- impersonation: view as, act as, the watch desk ----
r=$(req marshal POST "$API/impersonate" '{"kind":"user","principal":"cadet","reason":"walkthrough"}'); check "marshal mints a view-as session" 200 "$r"
MINTED=$(body "$r" | py "print(rows['sessionId'])")
r=$(req marshal GET "$API/user/session"); assert_py "the console is now Cass's" "$r" "rows['username']=='cadet' and rows['impersonation']['actor']=='marshal'"
r=$(req marshal GET "$ANVIL/missions?capabilities=Execute&limit=200"); assert_py "every edge unlit under the mask" "$r" "rows and all(not m['zzCapabilities']['Execute'] for m in rows)"
r=$(req marshal POST "$ANVIL/claim-mission" "{\"missionId\":\"$HAULER\",\"squadronId\":\"$TONGS\"}"); check "a forged write on the read-only session is stopped at the door by EnforceReadOnlyMask" 403 "$r"
r=$(req marshal POST "$API/impersonate" '{"kind":"user","principal":"pilot"}'); check "chaining is refused" 403 "$r"
r=$(req governor GET "$API/impersonations"); check "the governor's watch desk lists the live sessions" 200 "$r"
assert_py "the minted session is listed with its two-hour cap" "$r" "any(s['sessionId']=='$MINTED' and s['actor']=='marshal' and s['principal']=='cadet' for s in rows)"
r=$(req cadet GET "$API/impersonations"); check "the cadet may not operate the desk" 403 "$r"
r=$(req marshal POST "$API/impersonate/end" '{}'); assert_py "marshal returns to self" "$r" "rows['restored'] is True"
r=$(req marshal GET "$API/user/session"); assert_py "the console is Maren's again, no record" "$r" "rows['username']=='marshal' and not rows.get('impersonation')"
r=$(req marshal POST "$API/impersonate" '{"kind":"user","principal":"cadet","reason":"to be revoked"}'); check "marshal mints another view-as session" 200 "$r"
MINTED=$(body "$r" | py "print(rows['sessionId'])")
r=$(req governor DELETE "$API/impersonations/$MINTED"); check "the governor revokes it from the watch desk" 204 "$r"
r=$(req marshal GET "$ANVIL/missions"); check "the revoked console's next request is refused" 401 "$r"
login marshal
r=$(req governor POST "$API/impersonate" '{"kind":"role","principal":"Dispatcher","reason":"walkthrough"}'); check "governor assumes the Dispatcher role" 200 "$r"
r=$(req governor PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$COURIER\",\"value\":{\"deadline\":\"2026-12-02T00:00:00Z\"}}]"); check "deadline extended under the role (grant B, on hold is not terminal)" 200 "$r"
r=$(req governor PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$CORVID\",\"value\":{\"assignedSquadronId\":\"$HAMMER\"}}]"); check "assignment refused on a claimed mission: subject is Greer, not a dispatcher" 403 "$r"
r=$(req archivist GET "$ANVIL/ships-log-entries"); assert_py "the log names 'governor as role Dispatcher'" "$r" "any('governor as role Dispatcher' in e['eventSource'] for e in rows)"

# ---- the overdue flip ----
if [ "${LODESTAR_SKIP_FLIP:-}" != 1 ]; then
  echo "INFO  waiting for the Corvid deadline (bootstrap + 3 minutes) to pass for the overseer's flip..."
  for i in $(seq 1 40); do
    r=$(req overseer PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$CORVID\",\"value\":{\"assignedSquadronId\":\"$TONGS\"}}]")
    [ "${r##*$'\n'}" = 200 ] && break
    sleep 5
  done
  check "overseer reassigns the claimed mission once overdue (deadline < now flipped)" 200 "$r"
fi

echo
if [ "$fails" -eq 0 ]; then echo "ALL CHECKS PASSED"; else echo "$fails CHECK(S) FAILED"; fi
