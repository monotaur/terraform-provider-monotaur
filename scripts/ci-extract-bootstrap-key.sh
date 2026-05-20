#!/usr/bin/env bash
# Wait for the Monotaur bootstrap Secret to appear, decode the admin API key
# from it, mask the value in workflow logs, and export it to $GITHUB_ENV as
# MONOTAUR_API_KEY. On timeout, dump pod/log diagnostics to stderr and exit 1.
#
# Environment:
#   NAMESPACE   Kubernetes namespace                 (default: monotaur)
#   SECRET      Bootstrap secret name                (default: monotaur-bootstrap-credentials)
#   DATA_KEY    Key inside the secret's data map    (default: bootstrap.key)
#   TIMEOUT     Seconds to wait for the secret       (default: 300)
#   DEPLOYMENT  Deployment to tail on timeout        (default: monotaur-monotaur-core)
set -euo pipefail

NAMESPACE="${NAMESPACE:-monotaur}"
SECRET="${SECRET:-monotaur-bootstrap-credentials}"
DATA_KEY="${DATA_KEY:-bootstrap.key}"
TIMEOUT="${TIMEOUT:-300}"
DEPLOYMENT="${DEPLOYMENT:-monotaur-monotaur-core}"

echo "Waiting up to ${TIMEOUT}s for secret ${SECRET} in ns ${NAMESPACE}..."

end=$(( $(date +%s) + TIMEOUT ))
while ! kubectl -n "$NAMESPACE" get secret "$SECRET" >/dev/null 2>&1; do
  if [ "$(date +%s)" -ge "$end" ]; then
    echo "Timed out waiting for ${SECRET}" >&2
    kubectl -n "$NAMESPACE" get pods -o wide >&2 || true
    kubectl -n "$NAMESPACE" logs "deploy/${DEPLOYMENT}" --tail=100 >&2 || true
    exit 1
  fi
  sleep 5
done

# Escape dots in the data-map key for JSONPath (e.g. bootstrap.key -> bootstrap\.key)
jsonpath_key="${DATA_KEY//./\\.}"
encoded=$(kubectl -n "$NAMESPACE" get secret "$SECRET" \
  -o "jsonpath={.data.${jsonpath_key}}")

if [ -z "$encoded" ]; then
  echo "Secret ${SECRET} is missing data key '${DATA_KEY}'" >&2
  kubectl -n "$NAMESPACE" get secret "$SECRET" -o yaml >&2
  exit 1
fi

key=$(printf '%s' "$encoded" | base64 -d)
if [ -z "$key" ]; then
  echo "Decoded value for ${DATA_KEY} in ${SECRET} is empty" >&2
  exit 1
fi

# Mask in workflow logs and export to subsequent steps.
echo "::add-mask::${key}"
if [ -n "${GITHUB_ENV:-}" ]; then
  printf 'MONOTAUR_API_KEY=%s\n' "$key" >> "$GITHUB_ENV"
  echo "Wrote MONOTAUR_API_KEY to \$GITHUB_ENV"
else
  echo "GITHUB_ENV not set; printing key to stdout (masked above)"
  printf '%s\n' "$key"
fi
