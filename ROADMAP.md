# Roadmap

These are plans, not promises. Dates are not set. Scope can change, and a release is made only when its tests pass. The source is section 12 of the [design document](docs/superpowers/specs/2026-11-26-openagentix-factory-design.md). If this page and that document differ, the design document wins until a decision record says otherwise.

## Built today

- The shared types and strict parsing in `internal/api`, with tests.
- `oxf version`.

Nothing else works yet. There is no release.

## v0.1

Local runner that uses the Claude Code and Codex logins already on the machine. Unattended runs with gates, and take the wheel. The autonomy dial and the trust ledger. Goal to spec to tickets to pull requests with evidence. A web app. A demo mode that needs no login and no token.

Build order inside v0.1:

1. Autopilot slice: a ready ticket, the local `claude` or `codex` in a worktree, verification, an evidenced draft pull request.
2. The web app on top: pipeline, inbox, live run, evidence, the dial.
3. Take the wheel: the endpoint and the plugin.
4. The front half: goal to spec to tickets.

## v0.2

Checks from the spec that the builder cannot touch. A learning loop. A container runner that uses an API key. A starter template.

## v0.3

The ship phase. `oxf run --once`. A Kubernetes Job runner.

## v0.4

The observe phase. An OpenAgentix governance provider, as far as the platform allows at that time.

## Not planned for v0.1

Container and Kubernetes runners, deploying, merging without a person, git hosts other than GitHub, user accounts in the web app.

## Open question before any public claim

Whether the terms of Claude and Codex subscriptions allow unattended runs started by another program is not checked. If they do not, autopilot on a subscription is dropped and take the wheel remains.
