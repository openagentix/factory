# 0002. Threat model for the single-machine factory

- Status: proposed, 2026-11-26. Not independently reviewed. A security review is required before a public release.
- Scope: the local product of the first release, as designed in decision record 0001. Rows that speak of a container apply to the container runner planned for a later release.

## What we protect

1. The git host credentials: the propose token, and the optional accept token.
2. The model key, where one is used, and the money behind it.
3. The repository: its default branch, its tests, its acceptance checks, its CI configuration.
4. The host machine and other files on it.
5. Private data in the repository and tickets.

## Who can attack

- **Anyone who can write a ticket or a pull request comment.** On a public repository that is the whole internet. This text reaches the worker's prompt.
- **Content the worker reads**: dependencies, web pages, files in the repository.
- **The worker itself**, when a model takes a shortcut such as editing a test.
- **Anyone who can push to the repository**, including to `factory-staging`.

## Threats and controls

| # | Threat | Control in the first release | What remains |
|---|---|---|---|
| 1 | Ticket text steers the worker to steal a git host token | The worker never gets either token. Only the publish step, outside the worker, holds them | None for these tokens through the worker. See L1 for the person's own credentials |
| 2 | Ticket text steers the worker to steal the model key | Locally no key is given. Where a key is given, a deadline and a cost limit bound the damage | The key can be sent out, because outbound network is open. Use a key with a low spend limit. OpenAgentix gives a per-run gateway key and an outbound allowlist |
| 3 | The worker leaks private repository content | No control beyond the limits above | Open. Do not run on a repository with secrets in its history |
| 4 | The default branch changes without a person's Accept | The worker holds no credential. The propose token is used for no write to the default branch, and branch protection must stop its user from updating the default branch. The accept token is used only on Accept, on Undo after Accept, and on an earned automatic Accept, which is not active in the first release | On GitHub a token that can push branches can also merge, so the split depends on branch protection. The factory checks the rules at start and warns. A person must set them |
| 5 | The worker edits tests, acceptance checks or CI to pass | Protected paths, including `.factory/` and the acceptance path, fail the run. Acceptance checks are written by a separate run before building and pass their own review. Checks run outside the worker. A changed or deleted existing test makes the risk high | Weak new tests are not detected. A check can pass for the wrong reason. The builder can change the code a check calls |
| 6 | The worker changes the factory's own configuration | `.factory/` is protected for every task except `onboard`. The orchestrator reads it from the default branch only, so a change takes effect only after Accept | A person who accepts a bad configuration change |
| 7 | Triggers from untrusted users start costly runs | Triggers act only on events from users with write access or from the factory. The factory's comments carry a marker and never trigger. A daily throttle per task. A cap on open agent pull requests | A maintainer who labels or answers a hostile ticket still starts a run |
| 8 | Run loops burn money | `maxAttempts` counts every work-stage start per ticket, including fix runs. A fix run's own failure triggers nothing. Off course stops building on a goal | None known |
| 9 | Container escape to the host | A plain Docker container, non-root user, no privileged mode, no Docker socket, only the workspace and the in and out directories mounted | A shared kernel. Best effort. Kubernetes and OpenAgentix can use a stronger runtime class |
| 10 | Secrets in logs or the web app | Events and logs pass a redaction filter for the known token and key values. The web app never returns a token | A secret the factory does not know about is not redacted |
| 11 | The web app is reached by others | It listens on localhost by default. A login token is required on another address | No user accounts in the first release |
| 12 | A hostile pull request from a fork triggers a review run | Fork pull requests are not reviewed in the first release. Review runs get no git host token, as for all runs | As threat 2 |
| 13 | Someone pushes to `factory-staging`, so a person accepts a state the factory did not build | Staging is rebuilt from the default branch and the recorded change commits on every event. Accept names the staging commit the person saw, and the orchestrator refuses it if the branch head differs from its own record | A push between the check and the merge. Branch protection on `factory-staging` narrows this |

## Rules for the code

- The worker spec is built in one place. A test asserts it has no git host token, no Docker socket and no host path beyond the three mounts.
- Ticket and comment text is wrapped and labelled as untrusted in every prompt.
- With the propose token, the publish step accepts only: push a branch with the prefix `factory/`; write `factory-staging` through the combine action, whose content is always a rebuild from the default branch and recorded change commits; open or update a draft pull request or the one staging pull request; add a comment; add or remove a label from a fixed list; create at most twelve issues per run as sub-issues of the goal; set a commit status. It refuses any write to the default branch.
- With the accept token, the publish step accepts only: merge the staging pull request at the commit the person accepted; merge a revert of an accepted change on Undo. Automatic Accept is not active in the first release. The accept token is loaded only for these actions and never reaches a worker.
- Branch protection on the default branch is checked at start. If the propose token's user could update the default branch, the web app shows a standing warning.
- With a personal access token, pull requests are opened as the token's user. The docs recommend a separate machine user for the propose token. A GitHub App comes later.
- Secrets come from the environment or a file. Never from a task file. Never written to the database.

## Local mode: what changes

In the first release the work stage is a process on the person's own machine, started with their own Claude or Codex login. There is no container.

| # | Threat | Control in the first release | What remains |
|---|---|---|---|
| L1 | Ticket text steers the agent to use the person's git credentials | Token variables, the credential helper and the agent socket are removed from the process environment. The harness sandbox is on. Only users with write access trigger runs | The home directory is readable. A determined attack can find credentials there. This is the same exposure as running the agent by hand on an untrusted ticket |
| L2 | The agent reads or changes files outside the worktree | The harness sandbox limits writes to the worktree where the harness supports it | Depends on the harness. Not verified by a run |
| L3 | The agent leaks private files to the network | No control | Open. Outbound network is not restricted |
| L4 | The factory mishandles a login token | It never reads, copies or forwards one. It only starts the official program. A test asserts the factory code never opens the harness credential stores | None known |
| L5 | Someone on the local network reaches the web app or the take-the-wheel endpoint | Both listen on localhost by default. Another address needs a login token | No user accounts |
| L6 | Two machines work the same ticket | Claim by label and marker comment, earliest wins | A short window of double work, ended at the claim check |
| L7 | A person who took the wheel submits weak work | The same verify stage, independent review and machine gate as an unattended run | None beyond the limits of the gate |
| L8 | With no prompts, the agent runs a harmful command that a person would have refused | A permission policy per run: an allowlist of commands, writes limited to the worktree. Outside the policy is denied and recorded | The policy is only as good as its list, and depends on each harness honouring it. Not verified by a run |
| L9 | The agent makes a wrong choice where it used to ask | Rulings are recorded and shown in the engineering view. Verify and review are outside the agent | A wrong ruling that passes the gate reaches staging. The person sees its effect only through the acceptance list |
| L10 | A person accepts without trying, and the factory takes that as trust | Accept is never evidence. Trust comes from outcomes after Accept, and only when the change was exercised | Outcomes take time and can be silent. With one maintainer there is no second person |
| L11 | The review is steered by text in the change, or shares the builder's blind spot | The review writes a fixed schema. A review by the same harness marks the change single-opinion, which never counts toward automatic Accept. High risk is an exception whatever the review says | A steered review that says "agrees" lets a medium or low risk change into staging when its checks pass |
| L12 | The accept token is stolen or misused | It is optional and held by the publish step only. It is used only for the actions listed above. Workers never see it | A stolen token from the machine. A person who presses Accept without trying the result |
| L13 | A weak check shows a criterion as working when it is not | The check was written before building by a separate run, reviewed against the person's wording, and kept on a protected path. A criterion with no check is "could not confirm" | A check that passes for the wrong reason |

Never start a harness with a flag that skips all permission checks on a person's own machine. No prompts must come from a policy, not from switching checks off.

## Honest summary

In local mode the honest statement is: the same trust as running Claude Code or Codex yourself, plus a verify stage, an independent review and a publish step the agent does not control. The factory hands the agent no credential, but it cannot wall off a person's own machine. The default branch moves only through the accept token, on a person's Accept or Undo, or through a developer. That split holds only if the git host's branch protection enforces it. In a container the git host is well protected, because the worker has no credential for it. Enforced budgets, outbound rules and an audit record are what OpenAgentix adds. The web app and the docs must say all of this.
