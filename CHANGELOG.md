# Changelog

All notable changes are written here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases use [semantic versioning](https://semver.org/) and a git tag `vX.Y.Z`. The changelog is updated before a release is made.

There is no release yet. While the version is 0.x, minor releases can change interfaces.

## [Unreleased]

### Changed

- The product name is now OpenAgentix Software Factory.
- Design: every run is unattended and a person decides at gates, not at prompts. The dial levels are off, guided, autopilot, and autopilot with merge. Decision 12 in decision record 0001.

### Added

- Design: built for the person who will not review. An outcome view in plain words, a machine gate with an independent review loop, a staging branch with Accept and Undo, and trajectory per goal. Decision 14 in decision record 0001.
- Design: the review gate as a decision card, effort set by risk, trust from outcomes and not from merges, and automatic merge per class of change. Decision 13 in decision record 0001.
- Design: permission policy per run, rulings in the result file, batched questions, an attention record in the trust ledger, and "take the wheel".

- Design document for OpenAgentix Software Factory (`docs/superpowers/specs/2026-11-26-openagentix-factory-design.md`), a draft. Nothing in it is proven by a run.
- Decision record 0001, core contracts, and decision record 0002, threat model for the single-machine factory. Both are proposals.
- Go package `internal/api`: shared types for the factory configuration, task files, run records and worker results, with strict parsing and validation. Unit tests included.
- `oxf version`, the only command in `cmd/oxf`.
- `Makefile` targets `test`, `lint` and `build`.
- Community and governance files: Apache-2.0 licence, code of conduct, contributing guide with DCO sign-off, governance, maintainers, security policy, adopters list, roadmap, and a guide for AI coding agents (`AGENTS.md`).
- Issue and pull request templates, label list, code owners, Dependabot configuration, and the `ci` and `dco` workflows.
