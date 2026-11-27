# Changelog

All notable changes are written here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases use [semantic versioning](https://semver.org/) and a git tag `vX.Y.Z`. The changelog is updated before a release is made.

There is no release yet. While the version is 0.x, minor releases can change interfaces.

## [Unreleased]

### Changed

- The audience is now small engineering teams first, then larger organisations. Solo founders who are not engineers are not the audience.
- Team work is part of the core design, and the v0.1 build order has a team basics step.
- The outcome view opens first for everyone. The engineering view is one deliberate step away.
- The wording on agent logins and plan terms is now one position, in record 0001, "Not settled".
- Design for OpenAgentix: the factory maps onto planned platform resources. Designed, not built.

### Added

- Decision record 0004, the web app and its read model: the approved Floor design (in `docs/design/`) and nine screens, a read model computed from existing records, live events, actions with the key on every data route, the stack, fonts under the SIL Open Font Licence 1.1, demo marking and accessibility rules. The web app is now built alongside the backend. Designed, not built. A proposal.
- Parts the staging branch will need: `internal/repo` can replay one change onto another base with no working directory and reads commit headers in git's own order; `internal/gate` can say whether a sealed decision passed one exact change and for which task; `internal/githost` refuses to push staging when staging is the default branch, and refuses branch names that differ from it only by case or that have a part starting with a dot.
- `internal/runner`: the local runner. It runs one work stage as a process in a plain workspace directory with a deadline, a scrubbed environment and no git host credential, then imports the files as one commit. The workspace is held by an opened directory, so an agent that swaps its directory for a link to another run's gets nothing. Known limits are in the package documentation. Not wired to a command yet.
- Decision record 0003, running on one machine and in a team: the `oxf init` and `oxf run` commands, machine settings, tokens from the operating system's credential tool, `trigger.json`, run record additions, a limit on cut-off runs, approving a goal by label, and the list of trusted logins with claims ordered by comment id. Designed, not built. A proposal.
- Design, not built: a list of trusted logins, claims across teammates' machines, and review on a different machine.
- Three rules the first build established, in record 0001 and `AGENTS.md`.
- The position on outside review services: the factory opens ordinary pull requests and does not depend on a review service.
- Design, not built: observe mode for rules, who-must-act notes, recorded undo for publish actions, and a spending limit per task kind.
- `internal/repo`: the factory's own bare git repository, with export and import of files through a plain directory, and a guard that refuses a repository holding any unknown configuration or layout entry.
- `internal/acceptance`: runs the acceptance checks of a goal and turns the results into three states: working, not working yet and could not confirm.
- `internal/gate`: the machine gate as pure decisions over values, where a blank or unknown value always means no.
- `internal/store`: run records and dial settings behind an interface, with a pure Go SQLite implementation. A finished run cannot be changed.
- `internal/harness`: adapters that start Claude Code and Codex unattended under a permission policy and read their events, a prompt renderer with the unattended contract, and a stub agent for tests.
- `internal/githost`: the fixed list of git host actions, with two tokens kept apart, a GitHub client and an in-memory fake.
- `internal/verify`: reads a change, enforces protected paths, runs checks on a clean copy, rates risk by rules and builds the evidence bundle. It works only on a repository the factory created.
- The core record now says how the factory keeps its own repository and how changes to the factory are reviewed, from what the first build showed.
- Design document for OpenAgentix Software Factory (`docs/design.md`), a draft. Nothing in it is proven by a run.
- Decision record 0001, core contracts, as one consolidated record: three moments for a person and an outcome view in plain words; acceptance checks written before building that the builder cannot change; a machine gate of verify, independent review and rule-based risk; a staging branch that is rebuilt on every event, with Accept and Undo; a propose token and an optional, separate accept token; interactive planning with no limit, then unattended work; quick and standard lanes; methods including installed plugins; the dial with autopilot as the default; trust from outcomes after Accept; trajectory per goal, with off course re-planned with the person. A proposal.
- Decision record 0002, threat model for the single-machine factory, matching record 0001. A proposal.
- Go package `internal/api`: shared types for the factory configuration, task files, run records and worker results, with strict parsing and validation. Unit tests included. It matches record 0001: result, issues, review and acceptance manifest files, acceptance states, task method, permissions and autonomy ceiling, factory risk paths, run record and evidence bundle, run phases and dial levels. Examples under `examples/` cover every file type.
- `oxf version`, the only command in `cmd/oxf`.
- `Makefile` targets `test`, `lint` and `build`.
- Community and governance files: Apache-2.0 licence, code of conduct, contributing guide with DCO sign-off, governance, maintainers, security policy, adopters list, roadmap, and a guide for AI coding agents (`AGENTS.md`).
- Issue and pull request templates, label list, code owners, Dependabot configuration, and the `ci` and `dco` workflows.
