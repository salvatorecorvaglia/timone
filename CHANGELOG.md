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

- First implementation of Timone.
