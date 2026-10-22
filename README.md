# Timone ☸️

**Your control panel for Kubernetes insights and diagnostics**

**Timone** is a read-only web and desktop control panel for Kubernetes insights and diagnostics.

## Features

- Cluster overview, resource search, sanitized resource details, events, and bounded pod logs.
- Application views, declared resource topology, routing references, and deterministic diagnostics with supporting evidence.
- Current CPU/memory metrics, resource budgets, and optional Prometheus history.
- Configuration audit findings, ServiceAccount grant inspection, and authorization views.
- Argo CD and Flux integration visibility, explicit desired-manifest drift investigations, and optional Helm revision comparisons.
- Administrator-selected custom resources, exposed through metadata and standard conditions.
- Read-only MCP tools for resource inspection, diagnostics, metrics, and integrations.

Visibility follows the configured Kubernetes credentials and namespace scope. Missing permissions and unavailable integrations are reported as coverage gaps. Topology describes declared relationships; it does not measure network traffic. Observed changes are retained in memory for at most one hour and may miss changes between observations.

## Requirements

For development and local builds:

- Go 1.26.10 or newer.
- Node.js 24 or newer and pnpm 11.23.0.
- A POSIX shell for the commands below.

Docker with Compose is an alternative for running the demo. Real cluster access requires a kubeconfig or an in-cluster ServiceAccount; Helm deployment also requires Helm and kubectl.

## Install a release (macOS and Linux)

Download the archive for your version, operating system, and architecture from
[GitHub Releases](https://github.com/salvatorecorvaglia/timone/releases), together
with `checksums.txt` from the same release. Archives use the name
`timone_<version>_<os>_<arch>.tar.gz`: choose `darwin` for macOS or `linux` for Linux,
and `amd64` for Intel/AMD or `arm64` for ARM64. Go and Node.js are not required.

In the download directory, set the filename to the archive you downloaded:

```sh
archive='timone_<version>_<os>_<arch>.tar.gz'
```

Verify the archive before extracting it. On Linux:

```sh
sha256sum "$archive"
```

On macOS:

```sh
shasum -a 256 "$archive"
```

Compare the output with the entry for that exact filename in `checksums.txt`.
Continue only if the hashes match. Then install in a writable directory:

```sh
tar -xzf "$archive" timone
mkdir -p "$HOME/.local/bin"
install -m 755 timone "$HOME/.local/bin/timone"
# Optional: enable kubectl timone (requires kubectl).
ln -s timone "$HOME/.local/bin/kubectl-timone"
```

Add `~/.local/bin` to your shell's PATH if needed, then run `timone` or
`kubectl timone`. If the symlink already exists and points to `timone`, keep it.
To update or roll back, repeat these steps with the desired release archive.
Windows binaries are available as ZIP archives on the same release page.

## Homebrew

Install the prebuilt application on macOS and Linux, on Intel/AMD or ARM64:

```sh
brew install salvatorecorvaglia/tap/timone
timone
# Or, with kubectl installed:
kubectl timone
```

Homebrew installs the prebuilt application without Go or Node.js. Future updates
use `brew update` followed by `brew upgrade salvatorecorvaglia/tap/timone`.
The tap checks for new stable releases hourly; synchronization can also be run
manually from its GitHub Actions page.

## Desktop

The desktop application bundles the backend with a native application window,
kubeconfig/context selection. Demo mode is available only when running the desktop from source in development. Download available installers from
[GitHub Releases](https://github.com/salvatorecorvaglia/timone/releases), or use the
Homebrew and Scoop channels when the corresponding packages are published.

```sh
# macOS desktop
brew install --cask salvatorecorvaglia/tap/timone-desktop
```

```powershell
# Windows x64 desktop (Scoop required)
scoop bucket add timone https://github.com/salvatorecorvaglia/scoop-bucket
scoop install timone/timone-desktop
```

The current desktop installers are not publisher-signed/notarized; operating-system
security policies may block execution. Linux DEB/AppImage downloads are available
from [GitHub Releases](https://github.com/salvatorecorvaglia/timone/releases).

## Quick start: demo

From the project root:

```sh
pnpm --dir web install --frozen-lockfile
go mod download
pnpm --dir web run build
CGO_ENABLED=0 go build -trimpath -o bin/timone ./cmd/timone
TIMONE_ENV=development ./bin/timone --mode demo
```

Open <http://127.0.0.1:8080>. Demo mode uses simulated resources and does not require Kubernetes credentials. Stop the process with Ctrl+C.

For frontend hot reload and development checks, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Connect to Kubernetes locally

After building the application, run it against your current kubeconfig context:

```sh
./bin/timone
```

Timone opens the UI in your browser and listens on <http://127.0.0.1:8080>.
Use `--no-browser` for headless sessions. Automatic browser opening applies only to
local mode with a loopback listener. Demo and in-cluster modes do not open a browser.

Select a specific kubeconfig, context, and namespace:

```sh
./bin/timone --kubeconfig "$HOME/.kube/config" --context my-context --namespace my-namespace
```

Open <http://127.0.0.1:8080>. Replace the context and namespace with your own values. If `TIMONE_CONTEXT` is unset, the kubeconfig's current context is used. If `TIMONE_NAMESPACE` is empty, Timone uses the context namespace, or all readable namespaces when the context has none.

If startup reports a missing kubeconfig context, run `kubectl config get-contexts`
and choose an available context with `timone --context <name>`. If the list is empty,
configure cluster access first; a saved `current-context` name alone does not define
a context. You can explore Timone without a cluster using `TIMONE_ENV=development timone --mode demo`.

Run `./bin/timone --help` for command-line options and `./bin/timone --version` for
the embedded application version. Explicit flags override environment variables;
when `TIMONE_MODE` is unset, the command defaults to `local`. Use `TIMONE_ENV=development` with `--mode demo` for
simulated resources. Use `--namespace=""` to override the context namespace and
select all readable namespaces. A binary or symlink named `kubectl-timone` on
`PATH` also supports invocation through `kubectl timone`.

Kubeconfig exec credential plugins require explicit opt-in with `--allow-exec-auth`
or `TIMONE_ALLOW_EXEC_AUTH=true`. Install the required provider CLI and use only trusted, non-interactive credential plugins.

## Run with Docker Compose

```sh
export TIMONE_TOKEN="$(openssl rand -hex 32)"
docker compose up --build -d
```

Open <http://127.0.0.1:8080> and sign in with the value of `TIMONE_TOKEN`. The included Compose service runs in demo mode and binds the published port to loopback. Set `TIMONE_PORT` to change the host port.

```sh
docker compose logs -f timone
docker compose down
```

## Configuration and authentication

Timone reads environment variables directly. It does **not** automatically load a `.env` file; [.env.example](.env.example) documents the available settings. Docker Compose separately uses its own environment-file substitution.

| Variable | Default | Purpose |
| --- | --- | --- |
| `TIMONE_MODE` | `local` (CLI), `incluster` (Docker) | `local`, `incluster`, or development-only `demo` |
| `TIMONE_ENV` | unset | Set to `development` to enable demo mode; production rejects demo mode by default |
| `TIMONE_LISTEN` | `127.0.0.1:8080` | HTTP listen address |
| `TIMONE_TOKEN` | Unset | Shared authentication token |
| `TIMONE_TOKEN_FILE` | Unset | Rotating token file; mutually exclusive with `TIMONE_TOKEN` |
| `TIMONE_AUTH_MODE` | `token` | Token login or trusted `proxy` authentication |
| `TIMONE_PUBLIC_URL` | Unset | External origin, such as `https://timone.example.org` |
| `KUBECONFIG` | Standard kubeconfig lookup | Local kubeconfig path or path list |
| `TIMONE_CONTEXT` | Current context | Local Kubernetes context |
| `TIMONE_NAMESPACE` | Context namespace, otherwise all readable | Namespace scope |
| `TIMONE_RETAINED_MIB` | `16` | Sanitized cache/informer retention budget, from 8 to 128 MiB; not a process memory limit |
| `TIMONE_ACCESS_LOG` | `true` | Structured access logging |

Tokens must contain 32–512 printable ASCII characters without spaces. A token is required for non-loopback listeners, in-cluster mode, and proxy mode. API and MCP clients can send `Authorization: Bearer <token>`.

For shared deployments, terminate TLS at a trusted reverse proxy and set `TIMONE_PUBLIC_URL` to the external HTTPS origin. Proxy authentication additionally requires explicit direct-peer CIDRs in `TIMONE_TRUSTED_PROXIES` and the gateway's authenticated `X-Forwarded-User` header. Token-login forwarded-address rate limiting uses the separate `TIMONE_LOGIN_TRUSTED_PROXIES` setting.

Optional integrations are explicitly configured:

- **Prometheus:** set `TIMONE_PROMETHEUS_URL`; optional token-file, CA, and exact cluster-label settings are listed in `.env.example`. History supports bounded 1h, 6h, and 24h windows. Credentials require HTTPS; unauthenticated internal HTTP requires explicit opt-in.
- **Helm history:** set `TIMONE_HELM_DETAILS=true` and grant Secret list access. Summaries and comparisons omit values and full manifests, but the process still needs permission to read the underlying Secrets.
- **Custom resources:** set `TIMONE_CUSTOM_RESOURCES` to up to 32 explicit CRD names and grant the corresponding CRD and instance read permissions.
- **GitOps drift:** configure `TIMONE_ARGO_URL` or `TIMONE_FLUX_RENDERED_URL` and the relevant credential-file settings for explicit desired-manifest investigations.
- **Diagnostics:** adjust startup grace, warning-event window, and cumulative restart threshold with the `TIMONE_DIAGNOSTICS_*` settings in `.env.example`.

Resource projections redact Secret payloads, literal environment values, HTTP probe/lifecycle header values and other selected sensitive fields. ConfigMaps, command arguments, labels, controller messages and pod logs can still contain sensitive operator-provided information. Pod logs require separate Kubernetes permission.

## Deploy with Helm

Container and Helm publication is optional and disabled by default for releases.
To enable it, set the repository Actions variable `PUBLISH_CONTAINER` to `true`.
The **Container and Helm distribution** workflow can also be run manually for an
existing stable release. Binary and desktop publication is independent of this setting.

To deploy from this checkout, build and push an image to a registry accessible to
your cluster. Replace `registry.example.com/your-user/timone` with your repository:

```sh
export TIMONE_IMAGE=registry.example.com/your-user/timone
export TIMONE_VERSION="$(python3 scripts/version.py --check)"
docker build -t "$TIMONE_IMAGE:$TIMONE_VERSION" .
docker push "$TIMONE_IMAGE:$TIMONE_VERSION"
```

Create an authentication Secret, then install:

```sh
kubectl create namespace timone
export TIMONE_TOKEN="$(openssl rand -hex 32)"
kubectl -n timone create secret generic timone-auth \
  --from-literal=token="$TIMONE_TOKEN"

helm upgrade --install timone ./deploy/helm/timone \
  --namespace timone \
  --set image.repository="$TIMONE_IMAGE" \
  --set-string image.tag="$TIMONE_VERSION" \
  --set-string image.digest="" \
  --set auth.existingSecret=timone-auth \
  --wait --timeout 3m

kubectl -n timone port-forward service/timone-timone 8080:8080
```

Open <http://127.0.0.1:8080> and sign in with `TIMONE_TOKEN`. Ensure your cluster
can pull the image, and build it for the architecture used by your cluster nodes.

Images support the same configuration variables as the native binary.
Non-loopback listeners require a token. Published chart packages, when enabled,
pin an image digest; `image.digest` takes precedence over `image.tag`.

See [chart values](deploy/helm/timone/values.yaml) for namespace scoping, RBAC, ingress, resource limits, and integrations. The default chart reads namespaced resources cluster-wide. Set `scope.namespace` to constrain those reads; `rbac.clusterResources` separately controls cluster-scoped visibility. Secret and pod-log permissions are disabled by default. The optional NetworkPolicy denies ingress with an empty peer list, so configure trusted ingress peers when enabling it.

## MCP and health endpoints

The same server exposes a stateless Streamable HTTP MCP endpoint at `/mcp`. Configure an MCP client with the server URL, for example `http://127.0.0.1:8080/mcp`, and a bearer token when authentication is enabled. Tools use the same scoped service layer as the dashboard and do not modify cluster resources.

`GET /healthz` provides a liveness check; `GET /readyz` provides a readiness check. These endpoints are available without authentication.

---

## 🤝 Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## 📜 Changelog

Detailed release history and version changes can be found in [CHANGELOG.md](CHANGELOG.md).

## 🔐 Security

If you discover a security vulnerability, please see our [Security Policy](SECURITY.md).

## 📝 License

Distributed under the MIT License. See [LICENSE](LICENSE) for more information.

---

**Author**: [Salvatore Corvaglia](https://github.com/salvatorecorvaglia)