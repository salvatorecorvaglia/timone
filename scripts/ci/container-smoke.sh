#!/usr/bin/env bash
set -euo pipefail

image=${1:?Usage: container-smoke.sh IMAGE}
name="timone-smoke-$$"
token=$(openssl rand -hex 32)
cleanup() {
  docker logs "$name" || true
  docker rm -f "$name" >/dev/null || true
}
trap cleanup EXIT

test "$(docker image inspect --format '{{.Config.User}}' "$image")" = '10001:10001'
docker run -d --name "$name" --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true -p 127.0.0.1::8080 \
  -e TIMONE_MODE=demo -e TIMONE_ENV=development -e TIMONE_TOKEN="$token" "$image"
address="http://$(docker port "$name" 8080/tcp)"
ready=false
for attempt in {1..60}; do
  if curl --fail --silent "$address/healthz" >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
test "$ready" = true
curl --fail --silent "$address/" | grep -q '<html'
test "$(curl --silent --output /dev/null --write-out '%{http_code}' "$address/api/v1/overview")" = 401
curl --fail --silent -H "Authorization: Bearer $token" "$address/api/v1/overview" \
  | python3 -c 'import json,sys; data=json.load(sys.stdin); assert isinstance(data, dict) and data'
docker stop --time 15 "$name" >/dev/null
test "$(docker inspect --format '{{.State.ExitCode}}' "$name")" = 0
