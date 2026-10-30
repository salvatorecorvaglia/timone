# Contributing to Timone ☸️

Thanks for helping improve Timone. Bug reports, feature ideas, documentation fixes, and code contributions are welcome.

## Development setup

Install Go 1.26.10 or newer, Node.js 24 or newer, and pnpm 12.16.1. The commands below use a POSIX shell and run from the repository root unless stated otherwise.

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

The Go 1.26.10 / Apple M5 measurements after optimization are approximately
17.2 MB/op for diagnostics with retained history, 25.3 MB/op for 5,000 failures
with related events, 6.2 MB/op for dense applications, and 1.5 MB/op for 1,000 Pod
metric aggregates. Three fixed-work samples against the preceding commit measured
allocation-count reductions from 150,182 to 110,183 for retained-history diagnostics,
35,067 to 5,064 for healthy diagnostics, 195,264 to 155,262 for related failures,
7,102 to 2,589 for dense topology, and 210,310 to 69,275 for dense applications.
The changes avoid formatting string values, format evidence only for real signals,
compact diagnostic evidence in place, and use bounded stack storage for controller
cycle detection. These are synthetic in-process fixtures, not cluster latency or
resident-memory estimates.

Frontend query-sharing tests measure the shell/feature mount sequence: previously
a feature mounting after the shell request completed triggered a second request;
with a shared 30-second freshness window the same sequence makes one request.
Explicit invalidation still refreshes immediately, polling remains active, and
namespace/investigation query keys remain independent. Distinct overview and
investigation endpoints continue separate reads to preserve their response contracts.

The concurrent load test separately bounds allocation churn to 40 MiB per call
and retained heap growth to 32 MiB across its 80 investigations.

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

## Submit a pull request

Use the [pull request template](.github/PULL_REQUEST_TEMPLATE/PULL_REQUEST_TEMPLATE.md). Describe the change, link any related issue, and list the checks you ran. Include screenshots for visible UI changes and call out any migration or configuration steps reviewers need to try the change. If a check could not be run, say why.

---

Happy coding! ☸️
