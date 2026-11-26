# Changelog

All notable changes are written here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases use [semantic versioning](https://semver.org/) and a git tag `vX.Y.Z`. The changelog is updated before a release is made.

There is no release yet. While the version is 0.x, minor releases can change interfaces.

## [Unreleased]

### Added

- Design document for OpenAgentix Software Factory (`docs/design.md`), a draft. Nothing in it is proven by a run.
- Decision record 0001, core contracts, as one consolidated record: three moments for a person and an outcome view in plain words; acceptance checks written before building that the builder cannot change; a machine gate of verify, independent review and rule-based risk; a staging branch that is rebuilt on every event, with Accept and Undo; a propose token and an optional, separate accept token; interactive planning with no limit, then unattended work; quick and standard lanes; methods including installed plugins; the dial with autopilot as the default; trust from outcomes after Accept; trajectory per goal, with off course re-planned with the person. A proposal.
- Decision record 0002, threat model for the single-machine factory, matching record 0001. A proposal.
- Go package `internal/api`: shared types for the factory configuration, task files, run records and worker results, with strict parsing and validation. Unit tests included. It matches record 0001: result, issues, review and acceptance manifest files, acceptance states, task method, permissions and autonomy ceiling, factory risk paths, run record and evidence bundle, run phases and dial levels. Examples under `examples/` cover every file type.
- `oxf version`, the only command in `cmd/oxf`.
- `Makefile` targets `test`, `lint` and `build`.
- Community and governance files: Apache-2.0 licence, code of conduct, contributing guide with DCO sign-off, governance, maintainers, security policy, adopters list, roadmap, and a guide for AI coding agents (`AGENTS.md`).
- Issue and pull request templates, label list, code owners, Dependabot configuration, and the `ci` and `dco` workflows.
