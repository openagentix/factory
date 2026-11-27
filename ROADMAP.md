# Roadmap

These are plans, not promises. Dates are not set. Scope can change. A release is made only when its tests pass and its claims have been shown by a run. This page is the one place for release content. The [design](docs/design.md) explains the why.

## Built today

- Parts, not wired together, each with tests, under `internal/`: `api` (shared types with strict parsing), `store`, `harness`, `githost`, `verify`, `repo`, `acceptance`, `gate` and `runner` (one work stage as a local process).
- `oxf version`.
- Four trial runs by hand that show an installed agent can be started unattended. See [the trial record](docs/trials/2026-11-26-unattended-runs.md).

Nothing else works yet. There is no release.

## v0.1

A small engineering team plans a goal in conversation, then the factory builds it with nobody watching, on the teammates' own machines, with the Claude Code and Codex already installed there.

Build order:

1. **The quick lane, unattended, on one machine.** One goal with a few acceptance lines. Acceptance checks, one build run under a permission policy, checks outside the builder, an independent review, the staging branch.
2. **Team basics.** The list of trusted logins, and claims that work across teammates' machines, so a second person's machine can take work.
3. **The web app.** The outcome view with the acceptance list, Accept and Undo. Then the engineering view.
4. **The standard lane.** Interactive planning to a spec and tickets. Trajectory.
5. **Take the wheel.** A person's own session claims a piece of work.

Also in v0.1: the dial, the trust and attention records, built-in prompts, use of an installed workflow plugin by name, a demo mode.

There is a useful product after step 3 if later steps slip.

## v0.2

A deep lane for large builds. It is not designed yet, and depends on a trial with a workflow plugin. Scenarios the builder never sees. A learning loop from changes that were taken back. A container runner that uses an API key. A starter template for new projects.

## v0.3

The ship phase: the factory triggers and watches a team's own delivery. `oxf run --once` for use inside other pipelines. A Kubernetes runner.

## v0.4

The observe phase: a production problem comes back as a ticket. An OpenAgentix governance provider, as far as the platform allows at that time.

## Not planned for v0.1

Container and Kubernetes runners, deploying, the deep lane, automatic Accept, git hosts other than GitHub, user accounts in the web app.

## Open question before any public claim

Whether a plan's terms allow the unattended use a team intends is for that team to check. The position is in decision record 0001, "Not settled". A trial showed that the mechanism works. Where a plan does not fit, unattended runs need an API key, and take the wheel remains.
