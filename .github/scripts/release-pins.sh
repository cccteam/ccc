#!/usr/bin/env bash
# release-pins refuses a release that would ship a sibling module pinned at a
# pseudo-version. The argument is the release-please component, the last segment of its
# branch name: ccc for the root module, otherwise the module's directory. The component's
# go.mod is read, and for impulse the skeleton templates and the upgrade ledger too, since
# an application's go.mod is written from them. Every cccteam requirement at a
# pseudo-version is named with the release that has to exist first, and the check fails.
# The impulse tool directive in a template is the one exception: it names impulse itself
# at the commit before the release, and impulse upgrade moves it to the running release.
set -euo pipefail

component=${1:?usage: release-pins.sh <component>}
case "$component" in
  ccc) dir=. ;;
  *) dir=$component ;;
esac
if [ ! -f "$dir/go.mod" ]; then
  echo "release-pins: no go.mod for component $component at $dir/go.mod" >&2
  exit 2
fi

files=("$dir/go.mod")
if [ "$component" = impulse ]; then
  files+=(impulse/internal/skeleton/_candidates/*/go.mod.tmpl impulse/internal/ledger/ledger.go)
fi

# A pseudo-version is vX.Y.Z-yyyymmddhhmmss-commit, vX.Y.Z-0.yyyymmddhhmmss-commit or
# vX.Y.Z-pre.0.yyyymmddhhmmss-commit: a timestamp and a commit after the version.
pseudo='v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]*)?[.-][0-9]{14}-[0-9a-f]{12}'
# A go.mod or a template names a sibling by its full path; a ledger step names it without
# the github.com/cccteam/ prefix.
sibling='(github\.com/cccteam/[A-Za-z0-9_./-]+|access|ccc(/[a-z-]+)?|db-initiator|httpio|logger|session|spxscan)'

found=0
for file in "${files[@]}"; do
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    line=${hit%%:*}
    pin=${hit#*:}
    module=${pin%% *}
    version=${pin##* }
    case "$file" in
      *.tmpl) [ "$module" = github.com/cccteam/ccc/impulse ] && continue ;;
    esac
    base=$(sed -E 's/(-[0-9A-Za-z.-]*)?[.-][0-9]{14}-[0-9a-f]{12}$//' <<<"$version")
    if [ "$base" = v0.0.0 ]; then
      release="its first release"
    else
      release="$base or a later release"
    fi
    echo "$file:$line: $module is pinned at $version; release $module ($release) and pin the tag first" >&2
    found=$((found + 1))
  done < <(grep -nEo "(^|[[:space:]])${sibling}[[:space:]]+${pseudo}" "$file" | sed -E 's/^([0-9]+):[[:space:]]*/\1:/' || true)
done

if [ "$found" -gt 0 ]; then
  echo "release-pins: $component would ship $found pin(s) at a pseudo-version; a release never does" >&2
  exit 1
fi
echo "release-pins: $component pins released siblings only"
