#!/usr/bin/env bash
# Fail the build when total statement coverage falls below the project floor.
#
# Usage: scripts/check-coverage.sh [profile]   (default profile: coverage.out)
#
# The floor is read from COVERAGE_THRESHOLD and defaults to 80. Both
# .github/workflows/ci.yml and .github/workflows/release.yaml call this, so the
# check stays identical in the two places it runs and there is one number to
# change when the floor moves.
set -euo pipefail

PROFILE="${1:-coverage.out}"
THRESHOLD="${COVERAGE_THRESHOLD:-80}"

if [ ! -f "$PROFILE" ]; then
  echo "coverage profile not found: $PROFILE" >&2
  exit 1
fi

# `go tool cover -func` ends with:  total:  (statements)  80.7%
COVERAGE="$(go tool cover -func="$PROFILE" |
  awk '$1 == "total:" { sub(/%$/, "", $NF); print $NF }')"

if [ -z "$COVERAGE" ]; then
  echo "failed to extract total coverage from $PROFILE" >&2
  exit 1
fi

if awk -v c="$COVERAGE" -v t="$THRESHOLD" 'BEGIN { exit !(c < t) }'; then
  echo "::error::Coverage ${COVERAGE}% is below the ${THRESHOLD}% floor"
  exit 1
fi

echo "Coverage ${COVERAGE}% meets the ${THRESHOLD}% floor"
