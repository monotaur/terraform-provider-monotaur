#!/usr/bin/env bash
# e2e-preflight.sh — validate required environment variables before running the
# E2E test suite. Exits non-zero with a one-line error message when any
# required variable is missing.

set -euo pipefail

DOCS="docs/e2e.md"
errors=0

if [ -z "${MONOTAUR_ENDPOINT:-}" ]; then
    echo "Error: MONOTAUR_ENDPOINT not set — see ${DOCS}" >&2
    errors=$((errors + 1))
fi

if [ -z "${MONOTAUR_API_KEY:-}" ]; then
    echo "Error: MONOTAUR_API_KEY not set — see ${DOCS}" >&2
    errors=$((errors + 1))
fi

if [ "${errors}" -gt 0 ]; then
    exit 1
fi
