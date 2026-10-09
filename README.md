# ccc

Utility types and functions created and maintained by the CCC team.

## Releasing the modules in this repository

Each module releases on its own through release-please: a merge to master opens or
updates the module's release pull request, and merging that cuts the tag. Two rules keep
a release from shipping code that names unreleased code.

A release never ships a sibling pinned at a pseudo-version. The `release pins` check runs
on release-please's branches and fails the release pull request when the module's
`go.mod` requires a cccteam module at a pseudo-version, or, for impulse, when a skeleton
template or the upgrade ledger's last step does; it names the module, the pin and the
release that has to exist first (`.github/scripts/release-pins.sh`). Every other pull
request passes it with no step run.

A wave moves leaf first. Inside a wave a skeleton that needs a change of a sibling not
yet released pins the pushed commit, with the ledger's step marked pending; that is
development, and the check does not run on it. When the sibling's tag exists, a repin
pull request, opened as a draft and made ready at the tag, moves every pin to the tag and
clears the mark; the release pull request merges only when its check is green. The order
is the module graph's: ccc, accesstypes, cache, pkg, securehash, sns, tracer, logger
and spxscan first, then httpio, cloud and db-initiator, then session, access and
middleware, then resource, then impulse, then bedrock.
