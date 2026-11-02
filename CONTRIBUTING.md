# Contributing to Timone ☸️

Thanks for helping improve Timone. Bug reports, feature ideas, documentation fixes, and code contributions are welcome.

## Development setup

Install Go 1.27.0 or newer, Node.js 24 or newer, and pnpm 12.16.1, as pinned in `web/package.json` and `desktop/package.json`. The commands below use a POSIX shell and run from the repository root unless stated otherwise.

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
go test -race -count=1 -timeout=10m ./...
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

Set `TIMONE_ACCEPTANCE_ARTIFACTS` when running the script locally to retain
per-version evidence, including a separate restricted overview response.

CI gates dependency security with `pnpm --dir web audit --audit-level=low`,
`node scripts/audit-desktop.mjs`, and the pinned Go vulnerability scanner. The desktop
review permits only its explicitly tested build-dependency exception; new advisories,
runtime exposure, and scanner failures fail the gate.

### Performance checks

Run `python3 scripts/check_benchmarks.py` for three fixed-work samples of each
bounded service/provider benchmark. CI checks median bytes and allocations per
operation against `tests/performance/budgets.json`; elapsed time is reported but
not gated on shared runners. Budgets allow roughly 40% headroom for allocator and
platform differences and should be tightened after measured optimizations; increases require an explained
fixture or behavior change. Missing benchmark samples fail the check.

For an allocation profile:

```sh
go test -run '^$' -bench 'BenchmarkDenseApplications|BenchmarkDiagnosticsWithRetainedHistory' \
  -benchtime=10x -memprofile=/tmp/timone-allocs.out -o /tmp/timone-profile.test ./internal/service
go run github.com/google/pprof@v0.0.0-20261016150608-1facf8f91b50 -top -alloc_space /tmp/timone-allocs.out
```

The initial profile identifies issue aggregation, relationship construction, and
formatting as allocation hotspots; profile totals also include fixture setup.
Preserve read authorization, projection ownership, bounds, and cancellation when
optimizing these paths. The static asset optimization shares prepared immutable
bytes and validators across workspace replacements and serves build-generated
gzip variants only for JavaScript/CSS. API and SSE responses are not compressed.

Core dark semantic colors live in `desktop/palette.css`, imported by both desktop
onboarding and the web stylesheet; the web light theme overrides the same tokens.

## Investigation and history changes

Keep investigations read-only and preserve namespace scope, RBAC checks, cancellation, and bounded resource reads. Cover permission-denied and partial-data behavior alongside successful results. Configuration dependency views must not expose ConfigMap or Secret payloads; investigation exports exclude logs.

For snapshots and saved views, cover schema validation, size limits, and resource or cluster identity handling. For persistent history, verify retention and disk limits using a temporary directory, and preserve the exclusion of event messages and resource configurations. History is disabled by default and remains disabled in desktop deployments. Runbook references must remain HTTPS links and must not execute commands or contact endpoints automatically.

Use the service and API tests under `internal/service` and `internal/api`, frontend tests beside each feature, and `tests/browser/product.spec.mjs` for coverage appropriate to the behavior changed. Generate frontend contract fixtures with `pnpm --dir web run test:fixtures` when API projections change.

For desktop changes, install its dependencies and run its unit tests:

```sh
pnpm --dir desktop install --frozen-lockfile
pnpm --dir desktop test
```

## Submit a pull request

Use the [pull request template](.github/PULL_REQUEST_TEMPLATE/PULL_REQUEST_TEMPLATE.md). Describe the change, link any related issue, and list the checks you ran. Include screenshots for visible UI changes and call out any migration or configuration steps reviewers need to try the change. If a check could not be run, say why.

---

Happy coding! ☸️
