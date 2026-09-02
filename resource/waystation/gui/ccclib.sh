#!/usr/bin/env bash
set -euo pipefail

# Rides this gui on the local ccc-lib checkout (a sibling of the ccc repo) — the
# npm equivalent of the go.work pattern: while attached, the published pin in
# package.json is rewritten to a file:.yalc spec, so a dirty `git status` IS the
# off-baseline flag. Never commit the yalc-modified package.json; the .yalc/
# machinery itself is gitignored.
#
#   ccclib.sh local     build ccc-lib, publish it to the local yalc store, attach
#   ccclib.sh push      rebuild ccc-lib and update every attached consumer
#   ccclib.sh restore   detach and reinstall the pinned registry version

GUI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CCC_LIB="${CCC_LIB:-"$GUI_DIR/../../../../ccc-lib"}"

build_and_publish() {
  (cd "$CCC_LIB" && bun run build)
  (cd "$CCC_LIB/dist/ccc-lib" && yalc publish --push)
}

case "${1:-}" in
  local)
    build_and_publish
    (cd "$GUI_DIR" && yalc add @cccteam/ccc-lib)
    ;;
  push)
    build_and_publish
    ;;
  restore)
    cd "$GUI_DIR"
    yalc remove --all
    rm -rf .yalc yalc.lock
    git checkout -- package.json package-lock.json
    npm ci
    ;;
  *)
    echo "usage: ccclib.sh <local|push|restore>" >&2
    exit 1
    ;;
esac
