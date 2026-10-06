#!/usr/bin/env bash
# harbor's after-traffic hook, for the render and check tests.
set -euo pipefail
echo "after-traffic: ${_ENV} ${RELEASE}"
