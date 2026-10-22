# Timone ☸️

**Your control panel for Kubernetes insights and diagnostics**

**Timone** helps you investigate cluster health, understand resource relationships, and diagnose workload issues using your existing Kubernetes access. A Go backend serves an embedded React interface and connects to one selected cluster at a time.

## Features

- Cluster overview, resource search, namespace filters, events, and workload diagnostics.
- Resource details and topology views for exploring dependencies and networking.
- Kubernetes metrics, with optional Prometheus integration for historical metrics.
- RBAC inspection and ServiceAccount permission investigations.
- Optional Helm history, explicitly enabled custom resources, and GitOps drift investigations through Argo CD or a rendered-manifest source for Flux.
- Read-only MCP tools for investigating the same cluster from a compatible AI client.
- Demo mode with simulated data and no Kubernetes credentials.

Visibility depends on the configured Kubernetes identity and its RBAC permissions. Timone does not modify cluster resources; suggested remediation commands remain actions for you to review and run separately.

## Quick start from source

Install **Go 1.26.10 or newer**, **Node.js 24 or newer**, and **pnpm 11.23.0**. Run these commands from the repository root in a POSIX shell:

```sh
go mod download
pnpm --dir web install --frozen-lockfile
pnpm --dir web run build
CGO_ENABLED=0 go build -trimpath -o bin/timone ./cmd/timone
```

The binary embeds `web/dist`, so build the frontend first. The resulting executable serves both the UI and backend without a separate frontend server.

Try the simulated cluster:

```sh
TIMONE_ENV=development ./bin/timone --mode demo --no-browser
```

Open <http://127.0.0.1:8080>. Stop the application with Ctrl+C.

### Connect to your cluster

```sh
./bin/timone
```

By default, Timone uses your current kubeconfig context, listens on `127.0.0.1:8080`, and opens the UI in your browser. Select a context and namespace explicitly when needed:

```sh
./bin/timone --context production --namespace payments
```

The default namespace comes from the selected context; if it has none, Timone uses all readable namespaces. Pass `--namespace=''` to explicitly select all readable namespaces within the configured scope.

For kubeconfigs that use a credential plugin, install the provider CLI and explicitly allow trusted, non-interactive execution:

```sh
./bin/timone --allow-exec-auth
```

Use `--kubeconfig` for another kubeconfig path or an OS-separated path list, `--no-browser` to suppress browser launch, and `--help` for all CLI options. Explicit flags override environment variables. To invoke the application as `kubectl timone`, place a copy named `kubectl-timone` on your `PATH`.

## Docker Compose demo

Docker Compose builds the application and starts it with simulated data. It does not mount your kubeconfig.

```sh
export TIMONE_TOKEN="$(openssl rand -hex 32)"
docker compose up --build -d
```

Open <http://127.0.0.1:8080> and enter the token at the login screen. Keep the token available in your shell for subsequent Compose commands.

```sh
docker compose down
```

The host port is bound to loopback. Set `TIMONE_PORT` before starting Compose to use another port.

## Configuration and authentication

Timone reads environment variables directly. It **does not automatically load `.env` files**; Docker Compose separately uses its own environment-file handling. See [.env.example](.env.example) for the full configuration reference.

| Setting | Purpose |
| --- | --- |
| `TIMONE_MODE` | `local`, `incluster`, or `demo`; the CLI defaults to `local`. |
| `TIMONE_LISTEN` | Listen address; defaults to `127.0.0.1:8080`. |
| `KUBECONFIG`, `TIMONE_CONTEXT`, `TIMONE_NAMESPACE` | Local cluster credentials and resource scope. |
| `TIMONE_TOKEN` / `TIMONE_TOKEN_FILE` | Shared authentication token; configure only one source. |
| `TIMONE_PUBLIC_URL` | External origin, including scheme, when serving behind a reverse proxy. |
| `TIMONE_ALLOW_EXEC_AUTH` | Explicit opt-in for trusted kubeconfig credential plugins. |
| `TIMONE_PROMETHEUS_URL` | Explicit opt-in for historical metrics; configure cluster isolation and credentials as needed. |
| `TIMONE_CUSTOM_RESOURCES` | Explicit allowlist of CRD names for metadata and condition inspection. |
| `TIMONE_HELM_DETAILS` | Opt-in for decoded Helm history summaries; requires Secret list permission. |

An authentication token is required for non-loopback listeners and all `incluster` deployments. Tokens must contain 32–512 printable ASCII characters without spaces. Browser users log in with the token; API and MCP clients can send `Authorization: Bearer <token>`. Token files support credential rotation.

For an authenticated identity gateway, set `TIMONE_AUTH_MODE=proxy`, configure direct gateway peer CIDRs in `TIMONE_TRUSTED_PROXIES`, and set an HTTPS `TIMONE_PUBLIC_URL`. The gateway must supply `X-Forwarded-User` and the shared credential in `X-Timone-Proxy-Token`, replacing incoming client values. Proxy mode requires a token and does not accept browser token login.

Timone uses the backend's Kubernetes credentials for resource access. UI authentication does not create a separate Kubernetes identity for each user. Grant that backend identity only the access appropriate for its audience. Secret payloads are redacted from responses, but granting Secret list access still permits the process to read them.

## MCP and HTTP endpoints

Connect a compatible MCP client to `http://127.0.0.1:8080/mcp` using Streamable HTTP. Supply the bearer token when authentication is enabled. Tools expose read-only cluster overviews, resource inspection, diagnostics, topology, and other investigations, subject to the same configured scope and Kubernetes permissions as the UI.

The backend also serves its HTTP API under `/api/v1`, live updates at `/api/v1/stream`, and unauthenticated health probes at `/healthz` and `/readyz`.

## Desktop development

The Electron application launches a bundled Go backend. After building the frontend as above, prepare a backend for your host platform and start the app:

```sh
mkdir -p desktop/resources
go build -trimpath -o desktop/resources/timone ./cmd/timone
pnpm --dir desktop install --frozen-lockfile
pnpm --dir desktop start
```

On Windows, name the backend `timone.exe`. Desktop packaging is configured in [desktop/builder.cjs](desktop/builder.cjs); `pnpm --dir desktop run dist` builds the configured installers for the host platform.

## Development and tests

For a frontend development server with hot reload, start the backend and frontend in separate terminals after the initial frontend build:

```sh
TIMONE_ENV=development TIMONE_MODE=demo \
  TIMONE_PUBLIC_URL=http://127.0.0.1:5173 go run ./cmd/timone
```

```sh
pnpm --dir web run dev
```

Open <http://127.0.0.1:5173>. Vite proxies `/api` and `/mcp` to the backend on port 8080.

Core checks:

```sh
go test ./...
go vet ./...
pnpm --dir web run typecheck
pnpm --dir web run lint
pnpm --dir web run test
pnpm --dir web run build
```