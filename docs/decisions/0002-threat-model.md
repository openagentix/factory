# 0002. Threat model for the single-machine factory

- Status: proposed, 2026-11-26. Not independently reviewed. A security review is required before a public release.
- Scope: the local product of v0.1, as designed in decision record 0001, Decision 11. Rows that speak of a container apply to the container runner planned for v0.2.

## What we protect

1. The git host credential.
2. The model key and the money behind it.
3. The repository: its main branch, its tests, its CI configuration.
4. The host machine and other files on it.
5. Private data in the repository and tickets.

## Who can attack

- **Anyone who can write a ticket or a pull request comment.** On a public repository that is the whole internet. This text reaches the worker's prompt.
- **Content the worker reads**: dependencies, web pages, files in the repository.
- **The worker itself**, when a model takes a shortcut such as editing a test.

## Threats and controls

| # | Threat | Control in v0.1 | What remains |
|---|---|---|---|
| 1 | Ticket text steers the worker to steal the git host token | The worker never gets the token. The publish step runs outside the worker | None for this token |
| 2 | Ticket text steers the worker to steal the model key | The key is in the worker by need. A deadline and a per-run cost limit bound the damage | The key can be sent out. Outbound network is open on rung 1. Use a key with a low spend limit. Rung 3 gives a per-run gateway key and an outbound allowlist |
| 3 | The worker leaks private repository content | No control in v0.1 beyond the limits above | Open. Do not run on a repository with secrets in its history. Rung 3 outbound rules address this |
| 4 | The worker merges or deploys | No credential for it exists anywhere in the factory. The publish token is scoped to read and propose. The git host enforces branch protection | The owner must set branch protection. The factory checks for it at start and warns |
| 5 | The worker edits tests or CI to pass | Protected paths fail the run. Checks run in a fresh container by the orchestrator. Test file changes raise the risk level | Weak new tests are not detected |
| 6 | The worker changes the task files or prompts of the factory | `.factory/` is protected for every task except `onboard`. The orchestrator reads configuration from the default branch only, so a change takes effect only after a person merges it | A person who merges a bad configuration pull request without reading it |
| 7 | Triggers from untrusted users start costly runs | Every trigger acts only on events from a user with write access: labels, comments, merges. The factory's own comments carry a marker and never trigger. A daily throttle per task. A cap on open agent pull requests | A maintainer who labels or answers a hostile ticket still starts a run |
| 8 | Run loops burn money | `maxAttempts` per ticket or pull request. A fix run's own failure triggers nothing | None known |
| 9 | Container escape to the host | A plain Docker container, non-root user, no privileged mode, no Docker socket in the worker, only the workspace and the in and out directories mounted | A shared kernel. This is best effort. Rung 2 and 3 can use a stronger runtime class |
| 10 | Secrets in logs or the web app | Events and logs pass a redaction filter for the known key values. The web app never returns a key | A secret the factory does not know about is not redacted |
| 11 | The web app is reached by others | It listens on localhost by default. A login token is required when bound to another address | No user accounts in v0.1 |
| 12 | A hostile pull request from a fork triggers a review run with secrets | Review runs get only the diff and a model key. No git host token, as for all runs | As threat 2 |

## Rules for the code

- The worker container spec is built in one place. A test asserts it has no git host token, no Docker socket and no host path beyond the three mounts.
- Ticket and comment text is wrapped and labelled as untrusted in every prompt.
- The publish step accepts only: push a branch with the prefix `factory/`, open or update a draft pull request, add a comment, add or remove a label from a fixed list, and create at most twelve issues per run as sub-issues of the goal. Nothing else. It refuses any push to the default branch.
- Branch protection on the default branch is checked at start. If it is missing, the web app shows a standing warning. The factory still never pushes there.
- With a personal access token, pull requests are opened as the token's user, and GitHub does not let a user approve their own pull request. The docs recommend a separate machine user for the token. A GitHub App replaces this later.
- Secrets come from the environment or a file. Never from the task file. Never written to the database.

## Local mode: what changes

In v0.1 the work stage is a process on the person's own machine, started with their own Claude or Codex login. There is no container.

| # | Threat | Control in v0.1 | What remains |
|---|---|---|---|
| L1 | Ticket text steers the agent to use the person's git credentials | The factory removes token variables, the credential helper and the agent socket from the process environment. The harness sandbox is on. Only users with write access can trigger runs | The home directory is readable. A determined attack can find credentials there. This is the same exposure as running the agent by hand on an untrusted ticket |
| L2 | The agent reads or changes files outside the worktree | The harness sandbox limits writes to the worktree where the harness supports it | Depends on the harness. Not verified by a run |
| L3 | The agent leaks private files to the network | No control | Open. Outbound network is not restricted |
| L4 | The factory mishandles a login token | It never reads, copies or forwards one. It only starts the official program. A test asserts the factory code never opens the harness credential stores | None known |
| L5 | Someone on the local network reaches the web app or the take-the-wheel endpoint | Both listen on localhost by default. Another address needs a login token | No user accounts |
| L6 | Two machines work the same ticket | Claim by label and marker comment, earliest wins | A short window of double work, ended at the claim check |
| L7 | A person who took the wheel submits unreviewed work | The same verify stage and evidence bundle as autopilot. The pull request is a draft | None beyond the usual limits of verify |

| L8 | With no prompts, the agent runs a harmful command that a person would have refused | A permission policy per run: an allowlist of tools and commands, writes limited to the worktree. Outside the policy is denied and recorded | The policy is only as good as its list, and depends on each harness honouring it. Not verified by a run |
| L9 | The agent makes a wrong choice where it used to ask | Rulings are recorded and shown at the gate. The verify stage is outside the agent | A wrong ruling that passes the checks reaches the reviewer |
| L10 | A person merges without reading, and the factory counts that as trust | A merge without engagement is recorded as such and never raises trust. Trust comes from engaged reviews and, most of all, from outcomes after merge. High-risk changes need a confirmation per item in the web app | A person can still press merge on the git host. Engagement can be faked. Outcomes take time |

Never start a harness with a flag that skips all permission checks on a person's own machine. No prompts must come from a policy, not from switching checks off.

## Honest summary

In local mode the honest statement is: the same trust as running Claude Code or Codex yourself, plus a verify stage and a publish step the agent does not control. The factory hands the agent no credential, but it cannot wall off a person's own machine. In a container the git host is well protected, because the worker has no credential for it. Enforced budgets, outbound rules and an audit record are what OpenAgentix adds. The web app and the docs must say all of this.
