#!/bin/sh
set -eu
# Explicit credentials preserve the existing one-target CI/operator entrypoint.
if [ -n "${TIMONE_ACCEPTANCE_KUBECONFIG:-}" ]; then
  exec go test -count=1 -timeout=8m -v ./tests/cluster
fi
command -v kind >/dev/null || { echo 'Install kind v0.33.0 or supply TIMONE_ACCEPTANCE_KUBECONFIG' >&2; exit 1; }
case "$(kind version)" in
  *" v0.33.0 "*) ;;
  *) echo 'The pinned node images require kind v0.33.0' >&2; exit 1 ;;
esac
command -v docker >/dev/null
docker info >/dev/null
if kind get clusters | awk '$0 == "timone-acceptance" { found=1 } END { exit !found }'; then
  echo 'Refusing to reuse or delete existing kind cluster timone-acceptance' >&2
  exit 1
fi
work=$(mktemp -d "${TMPDIR:-/tmp}/timone-live.XXXXXXXX")
owned=false
cleanup() {
  if [ "$owned" = true ]; then kind delete cluster --name timone-acceptance || return 1; fi
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for image in \
  kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed \
  kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
do
  export TIMONE_ACCEPTANCE_KUBECONFIG="$work/kubeconfig"
  owned=true
  kind create cluster --name timone-acceptance --image "$image" --kubeconfig "$TIMONE_ACCEPTANCE_KUBECONFIG" --wait 180s
  go test -count=1 -timeout=8m -v ./tests/cluster
  sh tests/helm/cluster.sh
  kind delete cluster --name timone-acceptance
  owned=false
done
