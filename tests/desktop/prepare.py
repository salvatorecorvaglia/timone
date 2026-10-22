#!/usr/bin/env python3
"""Create fixtures only on the explicitly named, disposable desktop Kind cluster."""

import datetime
import ipaddress
import json
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlparse

kubeconfig = Path(os.environ["TIMONE_DESKTOP_ACCEPTANCE_KUBECONFIG"]).resolve()
root = Path(os.environ["TIMONE_DESKTOP_ACCEPTANCE_DIR"]).resolve()
command = ["kubectl", "--kubeconfig", str(kubeconfig)]
raw = json.loads(subprocess.check_output(command + ["config", "view", "--raw", "-o", "json"]))
if raw.get("current-context") != "kind-timone-desktop-acceptance":
    raise SystemExit("Refusing a non-desktop-acceptance cluster")
cluster = next(item for item in raw["clusters"] if item["name"] == "kind-timone-desktop-acceptance")
endpoint = urlparse(cluster["cluster"]["server"])
if endpoint.scheme != "https" or not ipaddress.ip_address(endpoint.hostname).is_loopback:
    raise SystemExit("Acceptance API must use HTTPS on loopback")
root.mkdir(parents=True, exist_ok=True, mode=0o700)

objects = []
for suffix in ["a", "b"]:
    namespace = f"timone-desktop-{suffix}"
    objects.extend([
        {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": namespace}},
        {"apiVersion": "v1", "kind": "Service", "metadata": {"name": f"desktop-proof-{suffix}", "namespace": namespace}, "spec": {"ports": [{"port": 8080}], "selector": {"app": "acceptance-only"}}},
    ])
objects += [
    {"apiVersion": "v1", "kind": "ServiceAccount", "metadata": {"name": "desktop-reader", "namespace": "timone-desktop-a"}},
    {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole", "metadata": {"name": "timone-desktop-reader"}, "rules": [{"apiGroups": ["*"], "resources": ["pods", "pods/log", "services", "endpoints", "endpointslices", "namespaces", "nodes", "configmaps", "events", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs", "ingresses", "networkpolicies", "serviceaccounts", "roles", "rolebindings", "clusterroles", "clusterrolebindings", "persistentvolumes", "persistentvolumeclaims", "storageclasses", "customresourcedefinitions", "horizontalpodautoscalers", "poddisruptionbudgets"], "verbs": ["get", "list", "watch"]}]},
    {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding", "metadata": {"name": "timone-desktop-reader"}, "roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "timone-desktop-reader"}, "subjects": [{"kind": "ServiceAccount", "name": "desktop-reader", "namespace": "timone-desktop-a"}]},
]
subprocess.run(command + ["apply", "-f", "-"], input=json.dumps({"apiVersion": "v1", "kind": "List", "items": objects}), text=True, check=True)
token = subprocess.check_output(command + ["create", "token", "desktop-reader", "-n", "timone-desktop-a", "--duration=1h"], text=True).strip()
credential = root / "credential.json"
expires = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(minutes=30)).isoformat().replace("+00:00", "Z")
credential.write_text(json.dumps({"apiVersion": "client.authentication.k8s.io/v1", "kind": "ExecCredential", "status": {"token": token, "expirationTimestamp": expires}}))
credential.chmod(0o600)
plugin = root / "credential-plugin.py"
plugin.write_text('import pathlib, sys\nsys.stdout.write(pathlib.Path(sys.argv[1]).read_text())\n')
raw["users"] = [{"name": "desktop-token", "user": {"token": token}}, {"name": "desktop-exec", "user": {"exec": {"apiVersion": "client.authentication.k8s.io/v1", "command": sys.executable, "args": [str(plugin), str(credential)], "interactiveMode": "Never"}}}]
raw["contexts"] = [{"name": f"desktop-{suffix}", "context": {"cluster": cluster["name"], "user": "desktop-token", "namespace": f"timone-desktop-{suffix}"}} for suffix in ["a", "b"]] + [{"name": "desktop-exec", "context": {"cluster": cluster["name"], "user": "desktop-exec", "namespace": "timone-desktop-a"}}]
raw["current-context"] = "desktop-a"
fixture = root / "kubeconfig.json"
fixture.write_text(json.dumps(raw))
fixture.chmod(0o600)
for verb, resource in [("create", "deployments"), ("list", "secrets")]:
    result = subprocess.run(command + ["auth", "can-i", verb, resource, "-n", "timone-desktop-a", "--as=system:serviceaccount:timone-desktop-a:desktop-reader"], capture_output=True, text=True)
    if result.returncode != 1 or result.stdout.strip() != "no":
        raise SystemExit("Fixture reader has unexpected privileges")
print("Desktop fixtures ready: two namespace contexts, short-lived read-only credentials, trusted exec plugin; writes and Secret access denied.")
