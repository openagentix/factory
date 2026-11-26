# Contributing to OpenAgentix Software Factory

Thank you for looking. OpenAgentix Software Factory is a new project with one maintainer, and it wants more. This page says how to build it, how to send a change, and how you can become a maintainer.

By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). How the project is run is in [GOVERNANCE.md](GOVERNANCE.md). Who the maintainers are is in [MAINTAINERS.md](MAINTAINERS.md).

Status: a design and one tested Go package (`internal/api`). Most of the product does not exist yet. See [ROADMAP.md](ROADMAP.md).

## Where to start

Good first contributions, from easiest to hardest:

- **Documentation.** Fix a wrong or unclear sentence in the README, the design document or a decision record.
- **Tests.** Add a test for a case in `internal/api` that is not covered.
- **Review.** Read the [design document](docs/superpowers/specs/2026-11-26-openagentix-factory-design.md) and the [decision records](docs/decisions), and tell us what is unclear, risky or wrong. Early design review is worth a lot.
- **Bugs.** Run `make test`, try the code, and report what you find.

If you are unsure where to begin, open a GitHub Discussion or an issue and ask. Small questions are welcome.

## How we work

Issues are the single backlog. Look for the labels `good first issue` and `help wanted`. The labels are listed in `.github/labels.yml`. The factory's own intake format is the "Goal" issue template.

[AGENTS.md](AGENTS.md) is a short guide to the repository, its commands and its rules, for AI coding agents and for new contributors.

## Build and test

You need Go 1.25 or newer. Nothing else is needed today.

```sh
make test          # go test ./... -race
make lint          # go vet ./...
make build         # builds bin/oxf
go run ./cmd/oxf version
```

You do not need a model key, a login or a cluster to contribute code, tests or docs. Tests never make paid model calls. They use a stub.

## Test first

New behaviour comes with a test, and the test is written first.

1. Write one test that fails. Run it. Check that it fails for the right reason.
2. Write the smallest change that makes it pass. Run all tests.
3. Clean up. Keep the tests green.

For a bug fix, write a test that reproduces the bug before you fix it.

Do not claim "it works" without showing the command and its output. In the pull request, paste what you ran.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat(api): ...`, `fix(verify): ...`, `docs: ...`, `test: ...`, `chore: ...`. Put the area in brackets. Keep the first line short.
- One logical change per pull request. Small pull requests get reviewed faster.
- Two checks must pass before a pull request can merge into `main`: `ci-ok` and `DCO`.
- Fill in the pull request template: Summary, Evidence, Merge danger, and the checklist.
- Add a line to `CHANGELOG.md` under `[Unreleased]` for anything a user can see.
- Never put a secret, key or token in a commit, an issue or a log line.
- Defaults matter: new features are off by default and deny by default.

### Developer Certificate of Origin (DCO)

Every commit needs a `Signed-off-by` line. It says you wrote the change, or have the right to submit it, under the project's licence (Apache-2.0). The rules are the [Developer Certificate of Origin, version 1.1](https://developercertificate.org/). Read them there. This repository does not copy or change that text. There is no CLA.

Add the line with `-s`:

```sh
git commit -s -m "fix(api): short summary"
```

This adds `Signed-off-by: Your Name <your-address>` using your `user.name` and `user.email` from git. The address must be the one in your git configuration. To fix the last commit: `git commit --amend -s --no-edit`. For several commits: `git rebase --signoff main`.

Pull requests with unsigned commits cannot be merged.

## AI-assisted contributions

Using an AI coding assistant is allowed. This project is built for that.

- **A human signs off and is responsible.** The `Signed-off-by` line is yours, not the tool's. You must have read the change, understood it, and have the right to submit it under Apache-2.0. An assistant cannot sign off.
- **Keep the `Co-Authored-By` trailer** that your tool adds, so the history shows which commits had help. Do not remove it.
- **The same rules apply.** Test first, show the evidence, plain English docs, a decision record before a public contract. Do not paste generated output you have not run.
- **No secrets and no personal data** in prompts, transcripts, commits, issues or pull requests.
- Reviewers may ask you to explain any part of a change. "The tool wrote it" is not an answer.

## How decisions are recorded

Design decisions live as decision records in [`docs/decisions`](docs/decisions), one file per decision, numbered `NNNN-short-title.md`.

- Write a decision record before code for anything that adds or changes a public contract (the task file schema, the API group, command-line flags, event types, the evidence bundle, the worker contract), adds a dependency, or changes a security default.
- A record has a status line, the context, the options with their costs, the decision, and what it rules out. Say what you measured and what you did not. Copy the shape of an existing one.
- Records are reviewed in a pull request like code. Maintainers decide by lazy consensus (see [GOVERNANCE.md](GOVERNANCE.md)).
- A decision is not final because it is accepted. A later record can replace it. Say which one it replaces.

## Docs style

Plain, simple English. Short sentences. No marketing words. No em dashes. Say what is proven by a run and what is not. Name the limits.

## Proposing a larger change

1. Open an issue or Discussion with the problem first, not the solution. Use the "Design proposal" template. Say who has the problem and what you tried.
2. If maintainers agree it is worth doing, write a decision record as a pull request. Keep it short.
3. When the record is accepted, build it in slices. Each slice has its tests and its docs.

## Security problems

Do not open a public issue for a vulnerability. Follow [SECURITY.md](SECURITY.md).

## From contributor to maintainer

The full rules are in [GOVERNANCE.md](GOVERNANCE.md). In short:

1. **Contributor.** Anyone who sends a change, a review, a bug report or a doc fix.
2. **Reviewer.** After a steady record of good reviews and merged work, a maintainer can add you to reviewers for an area. Your approval then counts for that area.
3. **Maintainer.** Normally after about six months of regular work: several merged changes of substance, careful reviews of other people's changes, help in issues, and good judgement about security. Any maintainer can nominate you, or you can ask. Existing maintainers decide by lazy consensus, and vote if someone objects.

The project is looking for maintainers from other organisations. If you want that path, say so in an issue or Discussion and a maintainer will help you plan it.
