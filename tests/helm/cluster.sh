#!/bin/sh
# Helm ownership/upgrade acceptance on the explicitly supplied disposable cluster only.
set -eu
: "${TIMONE_ACCEPTANCE_KUBECONFIG:?Explicit disposable acceptance kubeconfig required}"
context=$(kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" config current-context)
[ "$context" = kind-timone-acceptance ] || { echo 'Refusing non-acceptance cluster' >&2; exit 1; }
server=$(kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" config view --minify -o 'jsonpath={.clusters[0].cluster.server}')
case "$server" in
  https://127.0.0.1:*|https://localhost:*|https://\[::1\]:*) ;;
  *) echo 'Acceptance API must use HTTPS on loopback' >&2; exit 1 ;;
esac
chart=deploy/helm/timone
release=timone-chart-audit
created_a=false
created_b=false
cleanup() {
  if [ "$created_b" = true ]; then
    helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" uninstall "$release" --namespace timone-chart-b || true
    kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" delete namespace timone-chart-b --wait=false || true
  fi
  if [ "$created_a" = true ]; then
    helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" uninstall "$release" --namespace timone-chart-a || true
    kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" delete namespace timone-chart-a --wait=false || true
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" create namespace timone-chart-a
created_a=true
kubectl --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" create namespace timone-chart-b
created_b=true
# replicaCount stays one; runtime readiness is covered by the separate lifecycle suite.
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" install "$release" "$chart" --namespace timone-chart-a --set auth.existingSecret=test-auth --set rbac.legacyNames=true
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" upgrade "$release" "$chart" --namespace timone-chart-a --set auth.existingSecret=test-auth
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" install "$release" "$chart" --namespace timone-chart-b --set auth.existingSecret=test-auth
# Helm must also be able to restore legacy identities during rollback.
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" rollback "$release" 1 --namespace timone-chart-a
helm --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" upgrade "$release" "$chart" --namespace timone-chart-a --set auth.existingSecret=test-auth
printf '%s\n' 'Helm legacy-name upgrade/rollback and same-name namespace coexistence passed.'
