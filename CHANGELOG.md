# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Reorganized the README around source builds, cluster access, deployment, configuration, and development workflows.

### Fixed

- Retry desktop packaging up to three times when macOS cannot detach a temporary DMG volume because the device is busy; other packaging failures still fail immediately.
- Validate the public Homebrew desktop cask in the tap synchronization workflow by installing it and checking the application and bundled backend executables.

## [1.0.0] - 2026-10-22

### Added

- Read-only Kubernetes dashboard with cluster overview, resource search, namespace filters, events, bounded pod logs, and sanitized resource details.
- Application and topology views, workload diagnostics with supporting evidence, configuration audits, and RBAC and ServiceAccount permission investigations.
- Current CPU and memory metrics, resource budgets, and optional Prometheus history.
- Argo CD and Flux integration visibility, explicit desired-manifest drift investigations, optional Helm revision comparisons, and administrator-selected custom resource inspection.
- Read-only MCP tools and an HTTP API for cluster inspection and diagnostics, with live updates and health endpoints.
- Local kubeconfig and in-cluster access, shared-token and trusted-proxy authentication, namespace scoping, and explicit opt-in for kubeconfig credential plugins.
- Electron desktop application with kubeconfig and context selection, plus a development demo mode using simulated cluster data.
- Standalone binary, desktop, Homebrew, and Scoop distribution tooling, Docker Compose demo configuration, and a Helm deployment chart with read-only RBAC.
