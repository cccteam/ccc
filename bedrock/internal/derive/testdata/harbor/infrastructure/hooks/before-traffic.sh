#!/usr/bin/env bash
# harbor's before-traffic hook, for the render and check tests.
set -euo pipefail
echo "before-traffic: ${_ENV} ${RELEASE}"
