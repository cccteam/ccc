#!/usr/bin/env bash
# sign-in-steps refuses a pairing of server and login page that cannot sign anyone in: the
# login page the skeletons pin must render every sign-in step the generated server asks
# for. The server's steps are impulse's, impulse/internal/authpkg/sign-in-steps.json, which
# its test holds to the policy the generator writes; the page's are what
# @cccteam/resource-angular declares under cccteam.signInSteps in its package.json, read from
# the registry's version document at the version the skeletons' web roots pin (the ledger's
# test holds every candidate to the last step's pin, so one root speaks for all). Each step
# the page does not declare fails the check by name: the pull request moving the session pin
# then waits for the web library release that renders the step, or does not require it. A
# page declaring nothing, a release from before the declaration, lacks every step.
set -euo pipefail

server=impulse/internal/authpkg/sign-in-steps.json
root=impulse/internal/skeleton/_candidates/solo/web/package.json
pin=$(jq -r '.dependencies["@cccteam/resource-angular"]' "$root")
pin=${pin#^}
pin=${pin#~}
if ! document=$(curl -sfL "https://registry.npmjs.org/@cccteam/resource-angular/$pin"); then
  echo "sign-in-steps: the registry has no @cccteam/resource-angular $pin, the version $root pins" >&2
  exit 2
fi
page=$(jq -c '.cccteam.signInSteps // []' <<< "$document")
missing=$(jq -r --argjson page "$page" '. - $page | .[]' "$server")
if [ -n "$missing" ]; then
  while IFS= read -r step; do
    echo "::error::the skeletons pin @cccteam/resource-angular $pin, which does not declare the sign-in step $step the generated server asks for (it declares $page): pin a release of the web library that renders it, or do not require it"
  done <<< "$missing"
  exit 1
fi
echo "@cccteam/resource-angular $pin declares the sign-in steps $page, and the generated server asks for $(jq -c . "$server")"
