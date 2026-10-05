# The verdict on one run's plan of a pull request in the layers workflow, run by its plan
# job once every tofu plan job of the pull request has finished.
#
#   plan-verdict.sh <results> <run> <runs...>
#
# results holds one directory per run that planned, plan-<run>: name (the run as the
# workflow calls it), status (tofu plan's exit code), plan.txt (what tofu plan printed),
# errors.json (the plan's errors, [{summary, detail}]) and outputs.json (the output
# changes the plan makes, {output: [actions]}, without their values; {} when the plan
# failed). run is this run's directory name without plan-; runs are every run the pull
# request touches, in layer order, the order the merge applies them in.
#
# It prints the plan's one line for the pull request and exits 0 when the plan passes:
# the plan succeeded, or it failed only because it reads outputs that the plans of runs
# before it create, which exist once the merge has applied those runs ("planned after
# <run> applies"). Any other failure exits 1, as does a run that did not plan.
set -euo pipefail

results=$1
run=$2
shift 2
dir="$results/plan-$run"
failed="The plan failed; the log says why."
if [ ! -f "$dir/status" ]; then
  echo "The plan did not run; the log of its tofu plan job says why."
  exit 1
fi
if [ "$(cat "$dir/status")" = 0 ]; then
  summary=$(grep -E '^(Plan:|No changes\.)' "$dir/plan.txt" | tail -n 1 || true)
  # A plan that changes outputs alone prints neither line, only "Changes to Outputs:".
  if [ -z "$summary" ] && grep -q '^Changes to Outputs:' "$dir/plan.txt"; then
    summary="No resource changes; outputs change only."
  fi
  echo "${summary:-no summary line}"
  exit 0
fi

# The runs before this one in the order the merge applies them; this run is among them.
earlier=()
found=""
for r in "$@"; do
  if [ "$r" = "$run" ]; then
    found=1
    break
  fi
  earlier+=("$r")
done
if [ -z "$found" ]; then
  echo "$failed"
  exit 1
fi

# Every error is an unsupported attribute: an object read without the attribute named.
lacks='^This object does not have an attribute named "[^"]+"\.$'
others=$(jq --arg lacks "$lacks" '[.[] | select(.summary != "Unsupported attribute" or ((.detail // "") | test($lacks) | not))] | length' "$dir/errors.json")
mapfile -t attributes < <(jq -r '.[] | (.detail // "") | capture("^This object does not have an attribute named \"(?<name>[^\"]+)\"\\.$") | .name' "$dir/errors.json" | sort -u)
if [ "$others" != 0 ] || [ "${#attributes[@]}" = 0 ]; then
  echo "$failed"
  exit 1
fi

# Each attribute is an output the plan of a run before this one creates.
declare -A outputs
producers=()
for attribute in "${attributes[@]}"; do
  producer=""
  for r in "${earlier[@]}"; do
    if [ -f "$results/plan-$r/outputs.json" ] && jq -e --arg a "$attribute" '.[$a] == ["create"]' "$results/plan-$r/outputs.json" > /dev/null; then
      producer=$(cat "$results/plan-$r/name")
      break
    fi
  done
  if [ -z "$producer" ]; then
    echo "$failed"
    exit 1
  fi
  if [ -z "${outputs[$producer]+set}" ]; then
    producers+=("$producer")
    outputs[$producer]=$attribute
  else
    outputs[$producer]+=" $attribute"
  fi
done

# joined joins words as a sentence does: a, b and c.
joined() {
  local out="" i
  for ((i = 1; i <= $#; i++)); do
    if [ "$i" -gt 1 ]; then
      if [ "$i" -eq "$#" ]; then out+=" and "; else out+=", "; fi
    fi
    out+="${!i}"
  done
  printf '%s' "$out"
}

if [ "${#producers[@]}" -eq 1 ]; then
  read -r -a names <<< "${outputs[${producers[0]}]}"
  noun="the output"
  if [ "${#names[@]}" -gt 1 ]; then noun="the outputs"; fi
  echo "planned after ${producers[0]} applies: this plan reads $noun $(joined "${names[@]}"), which ${producers[0]}'s plan creates, and the merge applies ${producers[0]} first."
  exit 0
fi
sources=""
for producer in "${producers[@]}"; do
  read -r -a names <<< "${outputs[$producer]}"
  sources+="${sources:+; }$(joined "${names[@]}") from $producer"
done
echo "planned after $(joined "${producers[@]}") apply: this plan reads outputs their plans create ($sources), and the merge applies them first."
