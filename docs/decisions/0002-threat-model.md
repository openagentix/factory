# 0002. Threat model for the single-machine factory

- Status: proposed, 2026-11-26. Not independently reviewed. A security review is required before a public release.
- Scope: the local product of the first release, as designed in decision record 0001. Rows that speak of a container apply to the container runner planned for a later release.

## What we protect

1. The git host credentials: the propose token, and the optional accept token. Also the web app's login token.
2. The model key, where one is used, and the money behind it.
3. The repository: its default branch, its tests, its acceptance checks, its CI configuration.
4. The host machine and other files on it.
5. Private data in the repository and tickets.

## Who can attack

- **Anyone who can write a ticket or a pull request comment.** On a public repository that is the whole internet. This text reaches the worker's prompt.
- **Content the worker reads**: dependencies, web pages, files in the repository.
- **The worker itself**, when a model takes a shortcut such as editing a test.
- **Anyone who can push to the repository**, including to `factory-staging`.
- **Anyone who can reach the web app.**

## Threats and controls

| # | Threat | Control in the first release | What remains |
|---|---|---|---|
| 1 | Ticket text steers the worker to steal a git host token | The factory never gives a worker either token. The tokens are kept in the operating system's credential store, not in a plain file. The worker's environment has no token variables, credential helper or agent socket | In local mode the worker runs as the same user as `oxf`, so the factory cannot stop it reading what that user can read. The credential store is a barrier, not a wall. Recommended on a dedicated machine, not built: run `oxf` under its own operating system user |
| 2 | Ticket text steers the worker to steal the model key | Locally no key is given. Where a key is given, a deadline and a cost limit bound the damage | The key can be sent out, because outbound network is open. Use a key with a low spend limit. OpenAgentix gives a per-run gateway key and an outbound allowlist |
| 3 | The worker leaks private repository content | No control beyond the limits above | Open. Do not run on a repository with secrets in its history |
| 4 | The default branch changes without a person's Accept | The factory gives the worker no credential. The propose token is used for no write to the default branch, and branch protection must stop its user from updating it. The accept token is used only on Accept, on Undo after Accept, and on an earned automatic Accept, which is not active in the first release. Accept and Undo need the login token | On GitHub a token that can push branches can also merge, so the split depends on branch protection that a person must set; the factory checks and warns. In local mode a worker that reads a token from the person's account (threat 1) can use it directly. A stolen login token can press Accept In local mode a worker running as the same user could also read the login token and call Accept itself. Running `oxf` under its own operating system user removes this |
| 5 | The worker edits tests, acceptance checks or CI to pass | `.factory/`, the acceptance path and, by default, `.github/**` are protected: touching them fails the run. Acceptance checks are written by a separate run before building and pass their own review. Checks run outside the worker. A changed or deleted existing test makes the risk high | Weak new tests are not detected. A check can pass for the wrong reason. The builder can change the code a check calls. A repository that unprotects `.github/**` relies on the high-risk question instead |
| 6 | The worker changes the factory's own configuration | `.factory/` is protected for every task except `onboard`. The orchestrator reads it from the default branch only, so a change takes effect only after Accept | A person who accepts a bad configuration change |
| 7 | Triggers from untrusted users start costly runs | Triggers act only on events from users with write access or from the factory. The factory's comments carry a marker and never trigger. A daily throttle per task. A cap on open agent pull requests | A maintainer who labels or answers a hostile ticket still starts a run |
| 8 | Run loops burn money | `maxAttempts` counts work-stage starts per ticket, across `implement` and `fix-ci`; `acceptance` counts per goal and `onboard` per repository. A fix run's own failure triggers nothing. Off course stops building on a goal | None known |
| 9 | Container escape to the host | A plain Docker container, non-root user, no privileged mode, no Docker socket, only the workspace and the in and out directories mounted | A shared kernel. Best effort. Kubernetes and OpenAgentix can use a stronger runtime class |
| 10 | Secrets in logs or the web app | Events and logs pass a redaction filter for the known token and key values. The web app never returns a token | A secret the factory does not know about is not redacted |
| 11 | The web app is reached by others | It listens on localhost by default. It has no user accounts, so a login token is required on any other address, and always for Accept and Undo | Anyone who holds the login token can accept |
| 12 | A hostile pull request from a fork triggers a review run | Fork pull requests are not reviewed in the first release. Review runs get no git host token, as for all runs | As threat 2 |
| 13 | Someone pushes to `factory-staging`, so a person accepts a state the factory did not build | Staging is rebuilt from the default branch and the recorded list of changes on every event. Accept names the staging commit the person saw, and is refused if the branch head differs from the record. Restricting who may push to `factory-staging` to the propose token's user is recommended; force pushes must stay allowed, because every rebuild needs one | A push between the check and the merge, where pushes are not restricted |

## Rules for the code

- The worker spec is built in one place. A test asserts it has no git host token, no Docker socket and no host path beyond the three mounts.
- Checks, acceptance checks and Try it run code the worker wrote. They run with the same scrubbed environment and the same sandbox as the worker, never with a token in their environment.
- Ticket and comment text is wrapped and labelled as untrusted in every prompt.
- With the propose token, the publish step accepts only: push a branch with the prefix `factory/`; write `factory-staging` through the combine action, whose content is always a rebuild from the default branch and the recorded list; open or update a draft pull request or the one staging pull request; add a comment; add or remove a label from a fixed list; create at most twelve issues per run as sub-issues of the goal; set a commit status. It refuses any write to the default branch.
- With the accept token, the publish step accepts only: merge the staging pull request at the commit the person accepted; merge a revert of an accepted change on Undo. Automatic Accept is not active in the first release. The accept token is loaded only for these actions and never reaches a worker.
- Branch protection on the default branch is checked at start. If the propose token's user could update the default branch, the web app shows a standing warning.
- With a personal access token, pull requests are opened as the token's user. The docs recommend a separate machine user for the propose token. A GitHub App comes later.
- Tokens are kept in the operating system's credential store. Never in a task file, never in a plain file, never written to the database.

## Local mode: what changes

In the first release the work stage is a process on the person's own machine, started with their own Claude or Codex login, as the same operating system user as `oxf`. There is no container.

| # | Threat | Control in the first release | What remains |
|---|---|---|---|
| L1 | Ticket text steers the agent to use the person's git credentials | Token variables, the credential helper and the agent socket are removed from the process environment. The harness sandbox limits reads outside the workspace directory where the harness supports it. Only users with write access trigger runs | The home directory is readable by the same user. A determined attack can find credentials there. This is the same exposure as running the agent by hand on an untrusted ticket. A separate operating system user for `oxf` narrows it and is recommended, not built |
| L2 | The agent reads or changes files outside the workspace directory | The harness sandbox limits writes, and reads where supported, to the workspace directory | Depends on the harness. Not verified by a run |
| L3 | The agent leaks private files to the network | No control | Open. Outbound network is not restricted |
| L4 | The factory mishandles a login token | It never reads, copies or forwards one. It only starts the official program. A test asserts the factory code never opens the harness credential stores | None known |
| L5 | Someone on the local network reaches the web app or the take-the-wheel endpoint | Both listen on localhost by default. Another address needs a login token | No user accounts |
| L6 | Two machines work the same ticket, or a crashed machine holds it | Claim by label and marker comment, earliest wins. A claim expires after a set time | A short window of double work. The expiry time has no default yet |
| L7 | A person who took the wheel submits weak work | The same verify stage, independent review and machine gate as an unattended run | None beyond the limits of the gate |
| L8 | With no prompts, the agent runs a harmful command that a person would have refused | A permission policy per run: an allowlist of commands, writes limited to the workspace directory. Outside the policy is denied and recorded | The policy is only as good as its list, and depends on each harness honouring it. Not verified by a run |
| L9 | The agent makes a wrong choice where it used to ask | Rulings are recorded and shown in the engineering view. Verify and review are outside the agent | A wrong ruling that passes the gate reaches staging. The person sees its effect only through the acceptance list |
| L10 | A person accepts without trying, and the factory takes that as trust | Accept is never evidence. Trust comes from outcomes after Accept, and only when the change was exercised | Outcomes take time and can be silent. With one maintainer there is no second person |
| L11 | The review is steered by text in the change, or shares the builder's blind spot | The review writes a fixed schema. A change whose review did not agree is never combined, not even by a person. A same-harness review marks the change single-opinion, which never counts toward automatic Accept. High risk by content is always a question to the person | A steered review that says "agrees" lets a medium or low risk change into staging when its checks pass |
| L12 | The accept token is stolen or misused | It is optional, kept in the credential store, and used only for the actions listed above. Workers are never given it | As threat 1: a worker running as the same user may reach it |
| L13 | A weak check shows a criterion as working when it is not | The check was written before building by a separate run, reviewed against the person's wording, and kept on a protected path. A criterion with no agreed check is "could not confirm" | A check that passes for the wrong reason |
| L14 | Try it runs agent-written code on the person's machine | It runs in the same sandbox and scrubbed environment as the checks. The person is told it is code the factory wrote that no person has read | The sandbox is the harness's, and is not verified by a run. Code that waits for the person to use it can do what that sandbox allows |
| L15 | A planning session is steered by ticket text or a plugin | Planning is a live conversation the person watches. A planning session in the web app writes only the spec, the plan and notes, under a permission policy. Its output still passes an independent review shown to the person | A person who approves a plan without reading it |

Never start a harness with a flag that skips all permission checks on a person's own machine. No prompts in unattended runs must come from a policy, not from switching checks off.

## Found during the first build

A second agent, told to attack each package, made these happen before anything was merged. Each is fixed and kept as a test.

| Found | Fix |
|---|---|
| Repository settings made the orchestrator's git run a program, outside any sandbox | The factory's own repository, a guard with fixed lists, a checkout written from objects |
| Repository settings sent the propose token to another server during a push | A push from a fresh temporary repository, an address the factory builds, a credential only for that exact host |
| A value that starts with a dash became a flag of the harness, including one that turns permission checks off | Every value validated and passed as `--flag=value`, with a test that allows only known flags. Run for real once, with Claude Code only |
| The stub agent wrote and deleted outside its directories | Resolved paths, no links followed, runs only when switched on |
| A finished run record could be rewritten | No update to a run in a terminal phase |
| The run database was readable by other users of the machine | Directory 0700, files 0600 |
| A path written in an odd form slipped past the acceptance path protection | One rule for every repository path |

## Honest summary

In local mode the honest statement is: the same trust as running Claude Code or Codex yourself, plus a verify stage, an independent review and a publish step the agent does not control. The factory gives the agent no credential, but the agent runs as the same user and the factory cannot wall off that user's files. The default branch moves only through the accept token, on a person's Accept or Undo, or through a developer. That split holds only if the git host's branch protection enforces it. In a container the git host is well protected, because the worker has no credential for it and cannot read the person's home directory. Enforced budgets, outbound rules and an audit record are what OpenAgentix adds. The web app and the docs must say all of this.
