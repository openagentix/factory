# OpenAgentix Factory

**Runs on your laptop. Governed on OpenAgentix.**

An open-source software factory. It turns a business goal into a spec, tickets and verified pull requests. It uses the Claude Code and Codex CLI that are already installed on your machine. It is the software factory that earns its autonomy.

> **Status: early design. Nothing works yet.**
>
> - The design is drafted. One Go package is built and tested: `internal/api` (shared types and strict parsing).
> - There is no release, no working command except `oxf version`, and no web app.
> - Nothing here has been proven by a run. Every feature below is a plan.
> - Two open questions are listed in the design document, including whether subscription terms allow unattended runs.

## The idea

You state a goal. The factory does the work in steps. A person steps in only where needed, and less over time as the factory earns trust.

```
Goal -> Spec -> Plan -> Build -> Verify -> Ship -> Observe
                                         (problems come back as tickets)
```

- The factory does not own the agents. It owns the work.
- You keep your Claude Code and Codex, with the logins you already pay for. The factory never reads or copies those logins.
- All durable state lives in your git host. A run never waits for a person. It ends, and your next action starts the next run.
- Every change comes with evidence: what changed, what was checked, what it used. The factory records it, not the agent.
- The agent cannot edit the checks that judge it. It never gets a credential that can merge or deploy.

## Autopilot and co-pilot

Every piece of work is a run with three stages: work, verify, publish. Only the work stage differs.

| Mode | Who does the work stage |
|---|---|
| Autopilot | `oxf` starts the installed `claude` or `codex` with no terminal, in a fresh git worktree |
| Co-pilot | Your own Claude Code or Codex session claims the ticket, works on it and hands it back |

Verify, evidence, publish and the trust ledger are the same in both.

## The autonomy dial

One setting per task. A person moves it. The repository sets a ceiling.

```
 implement                                  ceiling: autopilot
   off     co-pilot    [AUTOPILOT]    merge (locked)
    o---------o------------O- - - - - - - x
 last 30 days: 14 runs, 11 merged, 2 reworked, 0 reverted
 merge is locked: needs an enforced rung and a longer record
```

The trust ledger beside the dial shows what an agent has earned: runs, merged, reworked, reverted. In the first release it informs. It does not move the dial. This sketch is a design, not a screenshot.

## What works today

Only this. You need Go 1.25.

```sh
make test
go run ./cmd/oxf version
```

`make test` runs the tests of `internal/api`. `oxf version` prints a version string. That is all.

## Roadmap in brief

These are plans, not promises. See [ROADMAP.md](ROADMAP.md).

| Release | Content |
|---|---|
| v0.1 | Local runner with your existing logins. Autopilot and co-pilot. The dial and trust ledger. Goal to spec to tickets to pull requests with evidence. A web app. A demo mode |
| v0.2 | Checks from the spec that the builder cannot touch. A learning loop. A container runner with an API key |
| v0.3 | The ship phase. `oxf run --once`. A Kubernetes Job runner |
| v0.4 | The observe phase. An OpenAgentix governance provider |

## How it relates to OpenAgentix

[OpenAgentix](https://github.com/openagentix/openagentix) is the platform. This is a sibling project in the same organisation. It has its own repository, its own releases and its own maintainers' list. The factory does not need the platform.

| Where it runs | Isolation and limits |
|---|---|
| A laptop or a dedicated machine | A git worktree and the agent's own sandbox. Best effort. The same trust as running the agent yourself |
| Kubernetes (later) | A container per run |
| OpenAgentix (later) | Enforced budgets, outbound rules and audit |

The same task files are meant to run on every row. That is the design test. It has not been run.

## Read more

- [Design document](docs/superpowers/specs/2026-11-26-openagentix-factory-design.md)
- [Decision records](docs/decisions): [0001 core contracts](docs/decisions/0001-core-contracts.md), [0002 threat model](docs/decisions/0002-threat-model.md)
- [Contributing](CONTRIBUTING.md), [Governance](GOVERNANCE.md), [Maintainers](MAINTAINERS.md), [Code of conduct](CODE_OF_CONDUCT.md)
- [Security policy](SECURITY.md): report a vulnerability privately, not in a public issue
- [Changelog](CHANGELOG.md), [Roadmap](ROADMAP.md), [Adopters](ADOPTERS.md)
- [AGENTS.md](AGENTS.md): a guide for AI coding agents working in this repository

## Licence

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Contributions need a DCO sign-off (`git commit -s`). There is no CLA.
