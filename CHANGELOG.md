# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Native AMD64 and ARM64 container build and runtime checks in CI.
- Docker Hub publication of tested images with version and latest multi-platform tags.
- Container installation and release credential setup instructions.

## [1.3.0] - 2026-11-04

### Added

- HPA, PodDisruptionBudget, ResourceQuota, LimitRange, and EndpointSlice inspection with related workload and Service context.
- Resource investigations covering scheduling, rollouts, node maintenance, storage, deletion, network paths and NetworkPolicy rules, and Job and CronJob execution evidence.
- CPU and memory efficiency analysis and ConfigMap and Secret dependency inspection without exposing payloads.
- Incident investigations, workload log aggregation, and JSON and Markdown investigation exports; logs are downloaded separately.
- Container log controls for previous instances, start time, bounded tails, text filtering, downloads, and optional live refresh.
- Resource snapshots with local file comparison, saved views, resource favorites, and focused topology investigations.
- Optional persistent observation history with configurable retention, disk budget, and sampling interval.
- Administrator-configured HTTPS runbook links for diagnostic issues.
- Required disposable Kubernetes acceptance checks in CI, with real restricted-access fixtures reused by browser tests.
- Browser overview performance checks, concurrent overview allocation benchmarks, desktop failure-path coverage, and macOS update and rollback integration tests.

### Changed

- Update `golang.org/x/net` to `v0.62.0`.
- Always use ad-hoc macOS desktop signing and disable notarization in the desktop builder.
- Refresh the application logo, README and contribution guide.
- Require Go 1.27.3 and align the container build toolchain with the module.
- Update electron-builder to 26.18.0 and use minimatch 11.0.1 with a compatibility adapter for legacy desktop packaging callers.
- Share workload log controls, investigation time-window handling, download helpers, and resource-tab capability rules across investigation views.

### Fixed

- Reject malformed query encoding, unsupported or duplicate query parameters, and invalid boolean sort directions in resource queries.
- Count individual Kubernetes resource reads toward the sanitized response-byte budget.
- Recover from backend exits during desktop connection setup and return to the connection screen after unexpected backend shutdowns.
- Fail stalled macOS update downloads after 30 seconds and cap total download time at 15 minutes, cleaning up download timers on completion or failure.
- Keep snapshot resource selections synchronized with URL and namespace changes, ignore stale file imports, and reuse snapshot comparisons until their inputs change.
- Use a keyboard-accessible workspace-switching dialog with focus management and theme-aware colors for incident history charts.

## [1.2.0] - 2026-10-27

### Added

- Desktop auto-updates from GitHub release feeds with SHA-512 verification, including in-place updates for unsigned macOS builds and package-manager guidance for Homebrew, Scoop, and deb installs.
- Application update controls in desktop Settings and an app-wide update banner, with manual checks, download progress, restart-to-install actions, and error recovery.
- Desktop update manifests generated and validated during packaging and included in GitHub releases.

### Changed

- Reuse successful release quality checks when packaging desktop installers, while retaining installer tests.
- Update Biome to 2.6.3.

## [1.1.0] - 2026-10-24

### Added

- Keyboard-accessible horizontal scrolling for operational tables, with visible focus indicators and improved navigation semantics.
- CI checks for Go vulnerabilities, frontend and desktop dependency advisories, and benchmark allocation budgets.

### Changed

- Reorganized the README around source builds, cluster access, deployment, configuration, and development workflows.
- Precompress embedded JavaScript and CSS assets and prepare asset bytes and ETags once, sharing them across workspace replacements.
- Share API query results for 30 seconds while retaining immediate refreshes on live invalidation, reducing duplicate requests between the application shell and feature views.
- Reduce allocations in diagnostics, application ownership traversal, and topology label matching.
- Split application navigation, global search, workspace routes, and topology graph construction into dedicated components and modules.
- Consolidate workspace API response contracts and bound URL pagination parameters across resource lists and the timeline.
- Refresh frontend dependencies, pin CI and release actions to immutable commits, and publish releases only after all assets have been uploaded.

### Removed

- Container image publication to GHCR, Helm chart distribution, and their dedicated workflows and tests.

### Fixed

- Exercise demo startup, HTTP health checks, and graceful shutdown in the Homebrew formula test without Kubernetes credentials.
- Retry desktop packaging up to three times when macOS cannot detach a temporary DMG volume because the device is busy; other packaging failures still fail immediately.
- Validate the public Homebrew desktop cask in the tap synchronization workflow by installing it and checking the application and bundled backend executables.
- Synchronize workspace changes across API consumers, global search, and live updates, discarding stale responses and avoiding duplicate stream teardown.
- Fall back to the summary tab and remove invalid resource-detail tab parameters from the URL.
- Display warning and unknown health states correctly in the topology minimap and mark unresolved routing references as critical.
- Accept maximum-length authentication tokens in files with CRLF line endings while continuing to reject oversized input.

## [1.0.0] - 2026-10-22

### Added

- First implementation of Timone.
