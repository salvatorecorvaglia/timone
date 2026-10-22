#!/bin/sh
# Real deployment checks on the explicitly supplied disposable cluster only.
set -eu
: "${TIMONE_ACCEPTANCE_KUBECONFIG:?Explicit disposable kubeconfig required}"
: "${TIMONE_TEST_IMAGE:?Local image reference required}"
context=$(kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" config current-context)
[ "$context" = kind-timone-acceptance ] || { echo 'Refusing non-acceptance cluster' >&2; exit 1; }
server=$(kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" config view --minify -o 'jsonpath={.clusters[0].cluster.server}')
case "$server" in
  https://127.0.0.1:*|https://localhost:*|https://\[::1\]:*) ;;
  *) echo 'Acceptance API must use HTTPS on loopback' >&2; exit 1 ;;
esac
namespace=timone-distribution
work=$(mktemp -d)
created=false
forward_pid=
cleanup() {
  if [ -n "$forward_pid" ]; then kill "$forward_pid" 2>/dev/null || true; wait "$forward_pid" 2>/dev/null || true; fi
  if [ "$created" = true ]; then
    helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" uninstall timone -n "$namespace" || true
    kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" delete namespace "$namespace" --wait=false || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" create namespace "$namespace"
created=true
openssl rand -hex 32 > "$work/token"
kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" -n "$namespace" create secret generic timone-auth --from-file="token=$work/token"
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" install timone deploy/helm/timone -n "$namespace" \
  --set auth.existingSecret=timone-auth --set image.repository="${TIMONE_TEST_IMAGE%:*}" \
  --set image.tag="${TIMONE_TEST_IMAGE##*:}" --set image.digest= --set image.pullPolicy=Never --wait --timeout 180s
kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" -n "$namespace" port-forward service/timone-timone :8080 --address 127.0.0.1 > "$work/forward.log" 2>&1 &
forward_pid=$!
python3 - "$work" <<'PYTHON'
import json
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.request

root = Path(sys.argv[1])
for _ in range(100):
    match = re.search(r'127\.0\.0\.1:(\d+) -> 8080', (root / 'forward.log').read_text())
    if match:
        break
    time.sleep(0.1)
else:
    raise SystemExit('Port-forward did not become ready')
base = 'http://127.0.0.1:' + match.group(1)
for endpoint in ['/healthz', '/readyz']:
    with urllib.request.urlopen(base + endpoint, timeout=10) as response:
        assert response.status == 200
try:
    urllib.request.urlopen(base + '/api/v1/overview', timeout=10)
except urllib.error.HTTPError as error:
    assert error.code == 401, error.code
else:
    raise SystemExit('Anonymous API access was allowed')
request = urllib.request.Request(base + '/api/v1/overview', headers={'Authorization': 'Bearer ' + (root / 'token').read_text().strip()})
with urllib.request.urlopen(request, timeout=10) as response:
    assert response.status == 200
    assert json.load(response)['mode'] == 'cluster'
print('In-cluster liveness/readiness, authenticated API and anonymous rejection passed.')
PYTHON
kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" auth can-i list pods -n "$namespace" --as="system:serviceaccount:$namespace:timone-timone" --quiet
for permission in 'update deployments' 'list secrets'; do
  # Intentional splitting of these fixed, non-user-supplied verb/resource pairs.
  if kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" auth can-i $permission -n "$namespace" --as="system:serviceaccount:$namespace:timone-timone" --quiet; then
    echo "Unexpected ServiceAccount permission: $permission" >&2
    exit 1
  fi
done
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" upgrade timone deploy/helm/timone -n "$namespace" --reuse-values --wait --timeout 180s
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" rollback timone 1 -n "$namespace" --wait --timeout 180s
printf '%s\n' 'Helm installation, readiness, authentication, read-only RBAC, upgrade and rollback passed.'
