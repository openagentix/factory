# Roadmap

These are plans, not promises. Dates are not set. Scope can change. A release is made only when its tests pass and its claims have been shown by a run. This page is the one place for release content. The [design](docs/design.md) explains the why.

## Built today

- Shared types with strict parsing, in `internal/api`, with tests.
- `oxf version`.
- Four trial runs by hand that show an installed agent can be started unattended. See [the trial record](docs/trials/2026-11-26-unattended-runs.md).

Nothing else works yet. There is no release.

## v0.1

A person plans a goal in conversation, then the factory builds it with nobody watching, on the person's own machine, with the Claude Code and Codex already installed there.

Build order:

1. **The quick lane, unattended.** One goal with a few acceptance lines. Acceptance checks, one build run under a permission policy, checks outside the builder, an independent review, the staging branch.
2. **The web app.** The outcome view with the acceptance list, Accept and Undo. Then the engineering view.
3. **The standard lane.** Interactive planning to a spec and tickets. Trajectory.
4. **Take the wheel.** A person's own session claims a piece of work.

Also in v0.1: the dial, the trust and attention records, built-in prompts, use of an installed workflow plugin by name, a demo mode.

There is a useful product after step 2 if later steps slip.

## v0.2

A deep lane for large builds, if a trial with a workflow plugin supports it. Scenarios the builder never sees. A learning loop from changes that were taken back. A container runner that uses an API key. A starter template for new projects.

## v0.3

The ship phase: the factory triggers and watches a team's own delivery. `oxf run --once` for use inside other pipelines. A Kubernetes runner.

## v0.4

The observe phase: a production problem comes back as a ticket. An OpenAgentix governance provider, as far as the platform allows at that time.

## Not planned for v0.1

Container and Kubernetes runners, deploying, the deep lane, automatic Accept, git hosts other than GitHub, user accounts in the web app.

## Open question before any public claim

Whether the terms of Claude and Codex subscriptions allow unattended runs started by another program is not checked. A trial showed that it works. If the terms do not allow it, unattended runs need an API key, and take the wheel remains for subscription users.
