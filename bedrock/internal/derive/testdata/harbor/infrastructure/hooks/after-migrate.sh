#!/usr/bin/env bash
# harbor's after-migrate hook, for the render and check tests.
set -euo pipefail
echo "after-migrate: ${_ENV} ${RELEASE}"
