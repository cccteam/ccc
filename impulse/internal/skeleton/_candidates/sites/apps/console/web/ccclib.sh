#!/usr/bin/env bash
set -euo pipefail

# Rides this workspace on the local ccc-lib checkout (beside this application, or CCC_LIB) — the
# package-manager equivalent of the go.work pattern: while attached, the published pins in
# package.json are rewritten to file:.yalc specs, so a dirty `git status` IS the
# off-baseline flag. Never commit the yalc-modified package.json; the .yalc/
# machinery itself is gitignored. The checkout carries two packages: the
# framework-neutral client @cccteam/resource (the generated zz_gen_api.ts imports
# it) and the Angular library @cccteam/resource-angular, which depends on it.
#
#   ccclib.sh local     build both packages, publish them to the local yalc store, attach, bun install
#   ccclib.sh push      rebuild both packages and update every attached consumer; then restart the
#                       dev server (`overmind restart console-web`), which does not watch node_modules
#   ccclib.sh restore   detach and reinstall the pinned registry versions

GUI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CCC_LIB="${CCC_LIB:-"$GUI_DIR/../../../../ccc-lib"}"
if [ ! -d "$CCC_LIB" ]; then
  echo "ccclib.sh: no ccc-lib checkout at $CCC_LIB; clone github.com/cccteam/ccc-lib beside this application or set CCC_LIB" >&2
  exit 1
fi

build_and_publish() {
  (cd "$CCC_LIB" && bun run build)
  (cd "$CCC_LIB/dist/resource" && yalc publish --push)
  (cd "$CCC_LIB/dist/resource-angular" && yalc publish --push)
}

case "${1:-}" in
  local)
    build_and_publish
    (cd "$GUI_DIR" && yalc add @cccteam/resource @cccteam/resource-angular && bun install)
    ;;
  push)
    build_and_publish
    # The dev server bundles the two packages with the application code (angular.json's serve
    # options keep them out of Vite's prebundle), so a restart is all a push still needs: no
    # cache to clear, and a plain reload in the browser shows the new build.
    echo "ccclib.sh: packages pushed; restart the dev server to pick them up: overmind restart console-web"
    ;;
  restore)
    cd "$GUI_DIR"
    yalc remove --all
    rm -rf .yalc yalc.lock
    git checkout -- package.json bun.lock
    bun install --frozen-lockfile
    ;;
  *)
    echo "usage: ccclib.sh <local|push|restore>" >&2
    exit 1
    ;;
esac
