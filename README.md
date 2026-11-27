# OpenAgentix Software Factory

**Spend your time deciding what to build. None watching it get built.**

An open-source software factory. Your team plans a goal in conversation. The factory then builds it with nobody watching, checks the result itself, and shows what works. It starts the Claude Code and Codex CLI that each teammate has installed and signed in to on their own machine, and never reads or shares a login. Whether a plan's terms allow unattended use is for each team to check.

## Who it is for

Small engineering teams first, roughly two to twenty engineers on a shared codebase. Then larger organisations, on Kubernetes and on OpenAgentix. A solo engineer can use it, but it is not designed around one. It assumes an engineering team that owns its codebase.

> **Status: early. Not usable yet.**
>
> - The design is written. Seventeen Go packages are built and tested: `api`, `store`, `harness`, `githost`, `verify`, `repo`, `acceptance`, `gate`, `runner`, `staging`, `floor` (the read model of the Floor screen), `floor/collect`, `server` (the local web server), `demo`, `settings`, `tokens` and `redact`, all under `internal/`. They are wired together only in a demo, and the last three are not used by anything yet.
> - `oxf run --demo` shows the Floor screen with a made-up project on a fake git host. Real factory code moves the line. Three buttons work: answer a question, yes or no on a risky change, and approve a plan. Accept and Undo are not built. It proves nothing about a real job.
> - Four trial runs by hand show that an installed agent can be started unattended. See [the trial record](docs/trials/2026-11-26-unattended-runs.md).
> - There is no release, and no command works on a real repository. Every feature below is a plan.

## The idea

People who use coding agents today spend the day answering "yes" to prompts, or merging changes they did not read. Engineers do not have time to read every change an agent makes, and a gate that is passed unread adds nothing. This project is designed for the engineer who should not have to read everything.

The team decides three things. The factory does the rest.

| Moment | The factory asks, in plain words | It never asks |
|---|---|---|
| What do you want? | Questions about the outcome, for as long as planning needs | To read code to make progress |
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

- **You plan, with care.** Planning is a live conversation with no limit, and a team is expected to spend real time on it. That is where its attention belongs. Your answers to "how will you know it worked?" become the list above. A quick lane stays for small jobs.
- **Checks are written from your list before any building**, by a separate run. The builder cannot change them. A line with no check shows as "could not confirm", never as working.
- **Building is unattended.** No prompts. A permission policy decides what a run may do. Small choices are made and written down. Real questions wait for you in one place.
- **The factory judges the work**, with those checks and an independent review by a second agent. Passing work collects on a staging branch. Your project changes only when you accept.
- **Undo any change.** Anyone on the team can say "that broke something". After Accept, taking a change back needs the same permission as accepting, or a developer.
- **Made for a team.** Each teammate runs `oxf` on their own machine, with their own agent login and git host account. The git host is the one place work is coordinated. Nobody shares a login. A list of trusted logins says whose machines act for the factory. Review of a change is preferred on a different teammate's machine and harness. Designed, not built.
- **It watches the course of each goal.** Work that goes off course is hard to steer back, so it stops early and plans again with you.
- **The process fits the job.** A quick lane for small things, a standard lane for a feature. Checking is the same in both.
- **Keep your method.** If you use superpowers, Open GSD or your own prompts, the factory is designed to use what you have installed, by name. One trial run did this with one plugin. It ships none of their text.
- **Two views.** The outcome view is what the whole team watches, in plain words, so a product owner or a teammate from another discipline can read it too. The engineering view shows every run, change and review. Simple by default, full control one deliberate step away: the outcome view opens first for everyone. An engineer who wants control opens the engineering view, sets a task to guided so they approve each step, or takes the wheel on a ticket. A person can set the engineering view as their own start page.

An autonomy dial per task sets where the factory stops for a person. A trust record shows what happened after each accepted change. Both are described in the [design](docs/design.md).

## What works today

Only this. You need Go 1.25.

```sh
make test
go run ./cmd/oxf version
```

And a demo of the Floor screen. It also needs Node 22 and pnpm, to build the web app.

```sh
make web
make build-web
bin/oxf run --demo
```

It prints a link. Open it in a browser. Stop it with Ctrl-C. Every line it prints starts with `DEMO`. The project is made up and the git host is fake. The factory code that moves the line is real, except the stations marked scripted. The story waits where a person would act. You can answer the question, say yes or no to the risky change, and approve the plan of the second goal, and the line moves on. Accept and Undo are not built yet. It uses port 7477 on this machine only, and needs no login and no token.

## Where it runs

| Where | Isolation and limits |
|---|---|
| A teammate's laptop, or a dedicated machine. This is the first release | A workspace directory and the agent's own sandbox. Best effort. The same trust as running the agent yourself |
| Kubernetes, later | A container per run |
| [OpenAgentix](https://github.com/openagentix/openagentix), later | Enforced budgets, outbound rules and audit |

OpenAgentix is the platform, a sibling project in the same organisation. The factory does not need it. On OpenAgentix the factory is designed to run as part of the platform, with budgets, isolation, outbound rules and audit enforced instead of reported. Designed, not built.

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
