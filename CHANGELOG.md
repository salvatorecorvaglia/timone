# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
