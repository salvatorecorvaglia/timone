# Contributing to Timone ☸️

Thanks for helping improve Timone. Bug reports, feature ideas, documentation fixes, and code contributions are welcome.

## Development setup

Install Go 1.26.10 or newer, Node.js 24 or newer, and pnpm 11.23.0. The commands below use a POSIX shell and run from the repository root unless stated otherwise.

```sh
go mod download
pnpm --dir web install --frozen-lockfile
pnpm --dir web run build
```

The Go binary embeds `web/dist`, so build the frontend before building the application. Rebuild it whenever you want frontend changes included in a new binary.

Run the backend in demo mode:

```sh
TIMONE_ENV=development TIMONE_MODE=demo TIMONE_PUBLIC_URL=http://127.0.0.1:5173 go run ./cmd/timone
```

In a second terminal, start the frontend:

```sh
pnpm --dir web run dev
```

Open <http://127.0.0.1:5173>. Vite proxies `/api` and `/mcp` to the backend on port 8080. Demo mode needs no Kubernetes credentials. Stop both processes with Ctrl+C.

See [README.md](README.md) for local-cluster access, Docker Compose, and authentication. Configuration is documented in [.env.example](.env.example); the application does not automatically load `.env` files.

## Validation

Run the checks relevant to your change. For a change spanning the backend and frontend, use:

```sh
go test ./...
go vet ./...
pnpm --dir web run typecheck
pnpm --dir web run lint
pnpm --dir web run format:check
pnpm --dir web run test:coverage
pnpm --dir web run build
CGO_ENABLED=0 go build -trimpath -o bin/timone ./cmd/timone
go run ./tests/smoke -binary ./bin/timone
```

The frontend test command also requires Go to generate fixtures. The smoke runner starts and stops the built demo application without using cluster credentials.

For UI changes, run the Playwright product and accessibility checks after building the frontend and binary above:

```sh
pnpm --dir web exec playwright install chromium
pnpm --dir web run test:browser
```

The browser runner starts `bin/timone` in demo mode on port 18080; leave that port available. Reports are written to `playwright-report/` and `test-results/`.

For changes that need live Kubernetes acceptance coverage, use:

```sh
sh tests/cluster/run.sh
```

This requires Docker and kind v0.33.0 and creates and removes a disposable `timone-acceptance` cluster for each pinned Kubernetes version. Inspect the script before running it. Alternatively, `TIMONE_ACCEPTANCE_KUBECONFIG` selects an existing test cluster; use a dedicated test environment for these checks.

## Submit a pull request

Use the [pull request template](.github/PULL_REQUEST_TEMPLATE/PULL_REQUEST_TEMPLATE.md). Describe the change, link any related issue, and list the checks you ran. Include screenshots for visible UI changes and call out any migration or configuration steps reviewers need to try the change. If a check could not be run, say why.

---

Happy coding! ☸️
