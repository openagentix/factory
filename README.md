# OpenAgentix Software Factory

**Spend your time deciding what to build. None watching it get built.**

An open-source software factory. You plan a goal in conversation. The factory then builds it with nobody watching, checks the result itself, and shows you what works. It uses the Claude Code and Codex CLI that are already installed on your machine.

> **Status: early. Not usable yet.**
>
> - The design is written. One Go package is built and tested: `internal/api`.
> - Four trial runs by hand show that an installed agent can be started unattended. See [the trial record](docs/trials/2026-11-26-unattended-runs.md).
> - There is no release and no working command except `oxf version`. Every feature below is a plan.

## The idea

People who use coding agents today spend the day answering "yes" to prompts, or merging changes they did not read. This project is designed for the person who will not review, and who is right not to.

You decide three things. The factory does the rest.

| Moment | The factory asks, in plain words | It never asks |
|---|---|---|
| What do you want? | Questions about the outcome, for as long as planning needs | Anything about code |
| Is this what you meant? | "Here is what works and what does not. Try it" | To read a change |
| Accept it? | "Everything you asked for works. Accept?" | To approve a merge |

This is the sketch of what you would see. It is a design, not a screenshot.

```
 Let customers reschedule bookings              ON COURSE

  ok  A customer can move a booking to another day
  ok  A booking that already started cannot be moved
  ..  Old bookings still show the old time        being fixed
  ?   Works on a phone                    could not confirm

  [ Try it ]      [ Accept ]      [ Undo a change ]
```

## How it is meant to work

- **You plan, with care.** Planning is a live conversation with no limit. Your answers to "how will you know it worked?" become the list above.
- **Checks are written from your list before any building**, by a separate run. The builder cannot change them. A line with no check shows as "could not confirm", never as working.
- **Building is unattended.** No prompts. A permission policy decides what a run may do. Small choices are made and written down. Real questions wait for you in one place.
- **The factory judges the work**, with those checks and an independent review by a second agent. Passing work collects on a staging branch. Your project changes only when you accept.
- **Undo is always there.** You cannot judge code, but you can say "that broke something".
- **It watches the course of each goal.** Work that goes off course is hard to steer back, so it stops early and plans again with you.
- **The process fits the job.** A quick lane for small things, a standard lane for a feature. Checking is the same in both.
- **Keep your method.** If you use superpowers, Open GSD or your own prompts, the factory uses what you have installed, by name. It ships none of their text.
- **Developers can still steer.** An engineering view shows every run, change and review. A developer can take over a piece of work, or approve each step.

An autonomy dial per task sets where the factory stops for a person. A trust record shows what happened after each accepted change. Both are described in the [design](docs/design.md).

## What works today

Only this. You need Go 1.25.

```sh
make test
go run ./cmd/oxf version
```

## Where it runs

| Where | Isolation and limits |
|---|---|
| A laptop or a dedicated machine. This is the first release | A git worktree and the agent's own sandbox. Best effort. The same trust as running the agent yourself |
| Kubernetes, later | A container per run |
| [OpenAgentix](https://github.com/openagentix/openagentix), later | Enforced budgets, outbound rules and audit |

OpenAgentix is the platform, a sibling project in the same organisation. The factory does not need it.

## Read more

- [Design](docs/design.md): what it is and why
- [Core contracts](docs/decisions/0001-core-contracts.md): the exact rules and file formats
- [Threat model](docs/decisions/0002-threat-model.md)
- [Trial record](docs/trials/2026-11-26-unattended-runs.md): what has been run and seen
- [Roadmap](ROADMAP.md), [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md), [Governance](GOVERNANCE.md), [Maintainers](MAINTAINERS.md), [Code of conduct](CODE_OF_CONDUCT.md), [Security policy](SECURITY.md)
- [AGENTS.md](AGENTS.md): a guide for AI coding agents working in this repository

## Licence

Apache-2.0. See [LICENSE](LICENSE), [NOTICE](NOTICE) and [THIRD-PARTY.md](THIRD-PARTY.md). Contributions need a DCO sign-off (`git commit -s`). There is no CLA.
