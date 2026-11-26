# Changelog

All notable changes are written here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases use [semantic versioning](https://semver.org/) and a git tag `vX.Y.Z`. The changelog is updated before a release is made.

There is no release yet. While the version is 0.x, minor releases can change interfaces.

## [Unreleased]

### Added

- Design document for OpenAgentix Factory (`docs/superpowers/specs/2026-11-26-openagentix-factory-design.md`), a draft. Nothing in it is proven by a run.
- Decision record 0001, core contracts, and decision record 0002, threat model for the single-machine factory. Both are proposals.
- Go package `internal/api`: shared types for the factory configuration, task files, run records and worker results, with strict parsing and validation. Unit tests included.
- `oxf version`, the only command in `cmd/oxf`.
- `Makefile` targets `test`, `lint` and `build`.
- Community and governance files: Apache-2.0 licence, code of conduct, contributing guide with DCO sign-off, governance, maintainers, security policy, adopters list, roadmap, and a guide for AI coding agents (`AGENTS.md`).
- Issue and pull request templates, label list, code owners, Dependabot configuration, and the `ci` and `dco` workflows.
