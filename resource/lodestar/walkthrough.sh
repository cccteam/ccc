#!/bin/bash
# Lodestar persona walkthrough: drives every persona's proof by curl through real
# sessions (login, cookies, XSRF — the served stack, not the test router), plus the
# droid channel under the API key and the client portal under its own prefix. Prints
# PASS/FAIL per check.
#
# Run it against a FRESHLY bootstrapped stack (overmind start, or the Procfile's
# spanner + server commands) with LODESTAR_DROID_API_KEY set on the server and
# exported here. Single-shot: several checks move workflow state, so a rerun needs a
# fresh bootstrap. The overdue flip waits for the seeded three-minute mission unless
# LODESTAR_SKIP_FLIP=1.
set -u
S=$(mktemp -d)
trap 'rm -rf "$S"' EXIT
B=${LODESTAR_URL:-http://127.0.0.1:8083}
API=$B/api
PORTAL=$B/portal
DROIDS=$B/droids
KEY=${LODESTAR_DROID_API_KEY:-}
fails=0

login() { # login <persona> [prefix]
  local p=$1 prefix=${2:-$API}
  rm -f "$S/$p.jar"
  # -L: the XSRF handshake answers a 307 set-and-retry; -c saves the jar.
  curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"username\":\"$p\",\"password\":\"lodestar\"}" "$prefix/user/login"
}

xsrf() { awk '$6=="XSRF-TOKEN" {print $7}' "$S/$1.jar" | tail -1; }

req() { # req <persona> <method> <url> [body]
  local p=$1 m=$2 u=$3 body=${4:-}
  if [ -n "$body" ]; then
    curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -H 'Content-Type: application/json' \
      -X "$m" -d "$body" -w '\n%{http_code}' "$u"
  else
    curl -s -L -c "$S/$p.jar" -b "$S/$p.jar" -H "X-XSRF-TOKEN: $(xsrf "$p")" -X "$m" -w '\n%{http_code}' "$u"
  fi
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
POD=80000000-0000-4000-8000-000000000005
QUARANTINE=80000000-0000-4000-8000-000000000008
HAMMER=50000000-0000-4000-8000-000000000001
TONGS=50000000-0000-4000-8000-000000000002
KINGFISHER=70000000-0000-4000-8000-000000000001
LANTERN=70000000-0000-4000-8000-000000000003
LANTERN_REFIT=a0000000-0000-4000-8000-000000000001
MULE_REFIT=a0000000-0000-4000-8000-000000000002
SAMARITAN_REFIT=a0000000-0000-4000-8000-000000000003
POD_BOND=b0000000-0000-4000-8000-000000000001
DRONES_BOND=b0000000-0000-4000-8000-000000000002
BULLION_BOND=b0000000-0000-4000-8000-000000000003
HALVARD=10000000-0000-4000-8000-000000000001
CONVOY_SORTIE=90000000-0000-4000-8000-000000000001

for p in governor marshal cadet pilot veteran lead dispatcher overseer booking wingco engineer quartermaster supercargo archivist hazards dock watch; do
  login "$p"
done
login client "$PORTAL"
echo "--- personas logged in ---"

# ---- the star chart: user-domains ----
r=$(req cadet GET "$API/user-domains"); check "cadet's chart lights Anvil only" 200 "$r"
assert_py "cadet's chart is [anvil]" "$r" "rows == ['anvil']"
r=$(req archivist GET "$API/user-domains"); assert_py "archivist's chart lights all three" "$r" "rows == ['anvil','bastion','cinder']"
r=$(req marshal GET "$API/sectors/cinder/missions"); check "Cinder is dark for the marshal (concealed)" 404 "$r"

# ---- flight deck: per-persona boards ----
r=$(req cadet GET "$ANVIL/missions?capabilities=Execute"); check "cadet lists missions" 200 "$r"
assert_py "cadet sees hazard 1 and 2 only" "$r" "rows and all(m['hazard'] in (1,2) for m in rows)"
assert_py "cadet's Claim lights on the open low-hazard rows only" "$r" "all(('ClaimMission' in m['zzCapabilities']['Execute']) == (m['statusId']=='open') for m in rows)"
r=$(req pilot GET "$ANVIL/missions"); assert_py "pilot: clearance 3 and certifications decide" "$r" "sorted(m['id'][-2:] for m in rows) == ['01','02','04','06','08']"
r=$(req veteran GET "$ANVIL/missions"); assert_py "veteran: NOT (hazard IN (1,2) OR fee < 5000)" "$r" "sorted(m['id'][-2:] for m in rows) == ['02','03','05','07']"
r=$(req wingco GET "$ANVIL/missions"); assert_py "wingco: hazard >= 4" "$r" "sorted(m['id'][-2:] for m in rows) == ['03','05']"
r=$(req wingco GET "$ANVIL/squadrons"); assert_py "wingco: wing IN subject.wings (Forge Wing's two)" "$r" "len(rows)==2"
r=$(req lead GET "$ANVIL/missions?capabilities=Execute,Create"); assert_py "lead: Hammer's missions" "$r" "sorted(m['id'][-2:] for m in rows) == ['02','03','05']"
assert_py "lead: Add sortie lights on the underway convoy only" "$r" "all((m['zzCapabilities']['Create']==['Sorties']) == (m['statusId']=='underway') for m in rows)"
r=$(req archivist GET "$ANVIL/missions"); assert_py "archivist: fee redacted until completed" "$r" "rows and all(('fee' in m) == (m['statusId']=='completed') for m in rows)"
r=$(req marshal GET "$ANVIL/missions?capabilities=Execute"); assert_py "marshal: every legal edge lit on the underway convoy" "$r" "sorted(next(m for m in rows if m['id']=='$CONVOY')['zzCapabilities']['Execute']) == ['CompleteMission','FailMission','HoldMission']"

# ---- the flight deck's edges ----
r=$(req cadet POST "$ANVIL/claim-mission" "{\"missionId\":\"$QUARANTINE\",\"squadronId\":\"$TONGS\"}"); check "cadet claims a hazard-2 mission" 200 "$r"
r=$(req pilot POST "$ANVIL/claim-mission" "{\"missionId\":\"$CONVOY\",\"squadronId\":\"$TONGS\"}"); check "pilot refused the hazard-4 convoy (grant, not body)" 403 "$r"
r=$(req lead POST "$ANVIL/hold-mission" "{\"missionId\":\"$CONVOY\",\"reason\":\"debris on the lane\"}"); check "lead holds the convoy" 200 "$r"
r=$(req quartermaster PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/sortie-expenses\",\"value\":{\"sortieId\":\"$CONVOY_SORTIE\",\"category\":\"fuel\",\"amount\":100}}]"); check "quartermaster refused while on hold (two hops deep)" 403 "$r"
r=$(req lead POST "$ANVIL/resume-mission" "{\"missionId\":\"$CONVOY\"}"); check "lead resumes the convoy (the loop)" 200 "$r"
r=$(req quartermaster PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/sortie-expenses\",\"value\":{\"sortieId\":\"$CONVOY_SORTIE\",\"category\":\"fuel\",\"amount\":100}}]"); check "quartermaster books an expense while underway" 200 "$r"
r=$(req booking POST "$ANVIL/stand-down-mission" "{\"missionId\":\"$CONVOY\"}"); check "booking cannot stand down the marshal's booking" 403 "$r"
r=$(req marshal POST "$ANVIL/stand-down-mission" "{\"missionId\":\"$CONVOY\"}"); check "nobody stands down an underway mission (hold it first)" 403 "$r"
r=$(req booking POST "$ANVIL/stand-down-mission" "{\"missionId\":\"$HAULER\"}"); check "booking stands down her own open booking" 200 "$r"
r=$(req lead POST "$ANVIL/complete-mission" "{\"missionId\":\"$CONVOY\"}"); check "lead completes Hammer's convoy" 200 "$r"
r=$(req lead GET "$ANVIL/missions"); assert_py "settlement = fee minus expenses" "$r" "next(m for m in rows if m['id']=='$CONVOY')['statusId']=='completed'"

# ---- dispatcher: the two-grant PATCH ----
r=$(req dispatcher PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$QUARANTINE\",\"value\":{\"notes\":\"client called\",\"deadline\":\"2026-12-01T00:00:00Z\"}}]"); check "dispatcher extends a deadline and notes it" 200 "$r"
r=$(req dispatcher PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$QUARANTINE\",\"value\":{\"deadline\":\"2026-09-05T00:00:00Z\"}}]"); check "pulling a deadline in is refused (new.deadline >= deadline)" 403 "$r"
r=$(req dispatcher PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/missions/$QUARANTINE\",\"value\":{\"assignedSquadronId\":\"50000000-0000-4000-8000-000000000004\"}}]"); check "assigning a foreign squadron is refused (new.x IN subject.set)" 403 "$r"

# ---- booking: fee limit ----
r=$(req booking PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/missions\",\"value\":{\"clientId\":\"$HALVARD\",\"kindId\":\"courier\",\"title\":\"Walkthrough booking\",\"hazard\":1,\"fee\":26000,\"deadline\":\"2027-01-01T00:00:00Z\"}}]"); check "booking over the fee limit is refused" 403 "$r"
r=$(req booking PATCH "$API/resources" "[{\"op\":\"add\",\"path\":\"/sectors/anvil/missions\",\"value\":{\"clientId\":\"$HALVARD\",\"kindId\":\"courier\",\"title\":\"Walkthrough booking\",\"hazard\":1,\"fee\":9000,\"deadline\":\"2027-01-01T00:00:00Z\"}}]"); check "booking within the fee limit" 200 "$r"

# ---- hangar deck ----
r=$(req engineer PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/refits/$LANTERN_REFIT\",\"value\":{\"estimate\":1000}}]"); check "engineer's estimate refused before inspection" 403 "$r"
r=$(req engineer POST "$ANVIL/inspect-ship" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "engineer inspects the Lantern" 200 "$r"
r=$(req engineer PATCH "$API/resources" "[{\"op\":\"patch\",\"path\":\"/sectors/anvil/refits/$LANTERN_REFIT\",\"value\":{\"estimate\":1000.6}}]"); check "estimate lands after inspection (rounded)" 200 "$r"
r=$(req engineer POST "$ANVIL/begin-refit" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "begin refit" 200 "$r"
r=$(req engineer POST "$ANVIL/start-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "start flight test (marked-file method)" 200 "$r"
r=$(req engineer POST "$ANVIL/fail-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "fail flight test (the backward edge)" 200 "$r"
r=$(req engineer POST "$ANVIL/start-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "start flight test again" 200 "$r"
r=$(req engineer POST "$ANVIL/pass-flight-test" "{\"refitId\":\"$LANTERN_REFIT\"}"); check "pass flight test stamps LastRefitAt" 200 "$r"
r=$(req engineer POST "$ANVIL/scrap-ship" "{\"refitId\":\"$MULE_REFIT\"}"); check "engineer holds no Scrap" 403 "$r"
r=$(req marshal POST "$ANVIL/scrap-ship" "{\"refitId\":\"$MULE_REFIT\"}"); check "marshal scraps from inspected" 200 "$r"
r=$(req marshal POST "$ANVIL/scrap-ship" "{\"refitId\":\"$SAMARITAN_REFIT\"}"); check "marshal scraps from in_refit (three chutes into one pit)" 200 "$r"
r=$(req pilot POST "$ANVIL/hail-ship" "{\"shipId\":\"$KINGFISHER\"}"); check "pilot hails a docked ship (the Touch)" 200 "$r"
r=$(req pilot POST "$ANVIL/hail-ship" "{\"shipId\":\"$LANTERN\"}"); check "pilot cannot hail the quarantined Lantern" 403 "$r"
r=$(req dock GET "$ANVIL/refits"); d=${r##*$'\n'}
r=$(req watch GET "$ANVIL/refits"); n=${r##*$'\n'}
if { [ "$d" = 200 ] && [ "$n" = 403 ]; } || { [ "$d" = 403 ] && [ "$n" = 200 ]; }; then echo "PASS  exactly one shift sees the hangar deck (dara=$d nadia=$n)"; else echo "FAIL  shift pair: dara=$d nadia=$n"; fails=$((fails+1)); fi

# ---- salvage hold ----
r=$(req supercargo POST "$ANVIL/release-consignment" "{\"consignmentId\":\"$POD_BOND\"}"); check "supercargo releases a consignment" 200 "$r"
r=$(req supercargo POST "$ANVIL/release-consignment" "{\"consignmentId\":\"$POD_BOND\"}"); check "second release is the frame's uniform Forbidden" 403 "$r"
r=$(req supercargo PATCH "$API/resources" "[{\"op\":\"remove\",\"path\":\"/sectors/anvil/consignments/$DRONES_BOND\"}]"); check "supercargo disposes of expired bond" 200 "$r"
r=$(req supercargo PATCH "$API/resources" "[{\"op\":\"remove\",\"path\":\"/sectors/anvil/consignments/$BULLION_BOND\"}]"); check "live bond cannot be disposed of" 403 "$r"

# ---- call log: create-form narrowing ----
r=$(req cadet GET "$API/permission-digest?domain=anvil"); assert_py "cadet's digest narrows the call form to summary and severity" "$r" "'DistressCalls.summary' in rows and 'DistressCalls.callerContact' not in rows"
r=$(req cadet PATCH "$API/resources" '[{"op":"add","path":"/sectors/anvil/distress-calls","value":{"summary":"Debris on the approach","severity":2}}]'); check "cadet files a two-field call" 200 "$r"

# ---- droid channel ----
if [ -n "$KEY" ]; then
  r=$(droid POST "$DROIDS/sectors/anvil/ingest-droid-reports" "{\"shipId\":\"$KINGFISHER\",\"subsystem\":\"hull\",\"reading\":0.95,\"recordedAt\":\"2026-09-04T12:00:00Z\"}"); check "droid posts a reading" 200 "$r"
  r=$(droid POST "$DROIDS/sectors/anvil/ingest-droid-reports" "{\"shipId\":\"$KINGFISHER\",\"subsystem\":\"reactor\",\"reading\":0.55,\"recordedAt\":\"2026-09-04T12:01:00Z\"}"); check "droid posts a second reading (one per call)" 200 "$r"
  r=$(droid GET "$DROIDS/sectors/anvil/droid-reports"); check "droid lists its channel" 200 "$r"
  r=$(req marshal GET "$ANVIL/droid-reports"); check "droid reports have no human route" 404 "$r"
  r=$(req hazards GET "$ANVIL/sector-hazard-boards"); assert_py "hazard board shows the worst hull reading" "$r" "any(b['shipName']=='Kingfisher' and b['subsystem']=='hull' and b['worstReading']==0.95 for b in rows)"
  r=$(curl -s -X POST -H 'Content-Type: application/json' -d '{}' -w '\n%{http_code}' "$DROIDS/sectors/anvil/ingest-droid-reports"); check "the droid channel refuses without the key" 401 "$r"
else
  echo "SKIP  droid channel: set LODESTAR_DROID_API_KEY on the server and here"
fi

# ---- portal ----
r=$(req client GET "$PORTAL/user-domains"); assert_py "cleo's portal lists Anvil" "$r" "rows == ['anvil']"
r=$(req client GET "$PORTAL/sectors/anvil/missions?capabilities=Execute"); check "cleo tracks Halvard's missions" 200 "$r"
assert_py "portal width excludes assignedSquadronId/notes/settlement" "$r" "rows and all('assignedSquadronId' not in m and 'settlement' not in m for m in rows)"
r=$(req client POST "$PORTAL/sectors/anvil/stand-down-mission" "{\"missionId\":\"$QUARANTINE\"}"); check "cleo stands down her company's claimed mission" 200 "$r"
r=$(req client POST "$PORTAL/sectors/anvil/stand-down-mission" "{\"missionId\":\"$CORVID\"}"); check "cleo cannot stand down Meridian's mission" 403 "$r"
r=$(req client PATCH "$PORTAL/resources" '[{"op":"add","path":"/sectors/anvil/distress-calls","value":{"summary":"Hauler drifting","severity":4,"callerContact":"cleo@halvard.example"}}]'); check "cleo files a call with her contact" 200 "$r"
r=$(req client GET "$PORTAL/sectors/anvil/refits"); check "refits are not a portal member" 404 "$r"

# ---- ship's log ----
r=$(req archivist GET "$ANVIL/ships-log-entries"); check "archivist reads the ship's log" 200 "$r"
assert_py "the hail is in the log as a touch" "$r" "any(e['tableName']=='Ships' and list((e.get('changeSet') or {}).keys())==['UpdatedAt'] for e in rows)"
r=$(req cadet GET "$ANVIL/ships-log-entries"); check "cadet holds no log grant" 403 "$r"

# ---- bulletin ----
r=$(req marshal POST "$API/issue-bulletin" '{"announcement":"All hands: drill at 0600"}'); check "bulletin officer issues a bulletin" 200 "$r"
r=$(req cadet POST "$API/issue-bulletin" '{"announcement":"unauthorized"}'); check "cadet refused the bulletin" 403 "$r"

# ---- impersonation: view as, act as ----
r=$(req marshal POST "$API/impersonate" '{"kind":"user","principal":"cadet","reason":"walkthrough"}'); check "marshal mints a view-as session" 200 "$r"
r=$(req marshal GET "$API/user/session"); assert_py "the console is now Cass's" "$r" "rows['username']=='cadet' and rows['impersonation']['actor']=='marshal'"
r=$(req marshal GET "$ANVIL/missions?capabilities=Execute"); assert_py "every edge unlit under the mask" "$r" "rows and all(not m['zzCapabilities']['Execute'] for m in rows)"
r=$(req marshal POST "$API/impersonate" '{"kind":"user","principal":"pilot"}'); check "chaining is refused" 403 "$r"
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
