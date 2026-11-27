# Roadmap

These are plans, not promises. Dates are not set. Scope can change. A release is made only when its tests pass and its claims have been shown by a run. This page is the one place for release content. The [design](docs/design.md) explains the why.

## Built today

- Parts, not wired together, each with tests, under `internal/`: `api` (shared types with strict parsing), `store`, `harness`, `githost`, `verify`, `repo`, `acceptance`, `gate`, `runner` (one work stage as a local process), `staging` (the holding area: rebuild, Accept, Undo, kept in memory for now) , `floor` (the read model of the Floor screen) and `server` (the local web server: the key, the request checks, live events; it serves a placeholder page for now).
- `oxf version`.
- Four trial runs by hand that show an installed agent can be started unattended. See [the trial record](docs/trials/2026-11-26-unattended-runs.md).
- The web app's Floor, Needs you and Goals screens, shown with sample data only. Built with `make web`.

Nothing else works yet. There is no release.

## v0.1

A small engineering team plans a goal in conversation, then the factory builds it with nobody watching, on the teammates' own machines, with the Claude Code and Codex already installed there.

Build order:

1. **The quick lane, unattended, on one machine.** One goal with a few acceptance lines. Acceptance checks, one build run under a permission policy, checks outside the builder, an independent review, the staging branch, trajectory rules per goal.
2. **Team basics.** The list of trusted logins, and claims that work across teammates' machines, so a second person's machine can take work.
3. **The standard lane.** Interactive planning to a spec and tickets.
4. **Take the wheel.** A person's own session claims a piece of work.

**The web app is built alongside these steps.** It starts with a demo line on a made-up project, moved by real factory code. Each backend piece then makes one more station of the line real. Its design is in decision record 0004.

A question stops only its own ticket, and a failed check or a disagreeing second opinion goes back to the builder as the next attempt. Both are already in records 0001 and 0003. The build is not done.

Also in v0.1: the dial, the trust and attention records, built-in prompts, use of an installed workflow plugin by name, a demo mode.

There is a useful product after step 2, with the web app's Floor, Accept and Undo, if later steps slip.

## After the first release works

Designed, not built. No order or date is set.

- Notifications for what needs a person, off by default, starting with a browser notification from the open web app.
- Live activity of the agent on this machine, as a short plain status on the Live screen.
- Preparing the workspace early for a ticket this machine could claim.
- A second example project made mostly of documents. A check that finds no files to check is "could not confirm", never a pass.
- A standing test that plants a known defect and confirms the factory's own verify stage catches it. A miss turns the signal red.
- Tries and usage per accepted change, on Record and Runs and changes, and tries on the Floor, with a stop at a limit set per goal.
- A failed check is run once more. A check that changes its answer is marked unreliable and never counts as a pass.
- A scan of each change for secrets and new dependencies. A secret fails the change. A new dependency is a question for a person.
- A short guide for coding agents, written at onboarding, with its commands tested.
- A record of the team's usual lead time before the factory starts, so later numbers can be compared with it.

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
