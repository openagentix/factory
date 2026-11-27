# 0001. Core contracts for the single-machine factory

- Status: proposed, 2026-11-26. The owner approved the scope and the positioning. Four small trial runs by hand tested the unattended start (see "What runs have shown"). Apart from that, nothing in this record has been run. Every rule below is a design, not a result.
- Scope: the local product, on a laptop or a dedicated machine, using the Claude Code and Codex already installed there. Every contract is shaped so that a container runner, Kubernetes and OpenAgentix need no schema change.
- Product: OpenAgentix Software Factory. Command `oxf`. API group `factory.openagentix.org`.

## Context

Free tools already turn an issue into a pull request. Practitioners report where teams hurt now: review capacity is the limit, agents edit tests to pass them, pull request text often does not match the change, cost per outcome is not visible, and there is no basis for trusting an agent. These are other people's reports, not findings from our runs, so this record quotes no numbers.

The product is for small engineering teams first, roughly two to twenty engineers on a shared codebase, then for larger organisations. A solo engineer can use it, but it is not designed around one. It assumes an engineering team that owns its codebase. Engineers do not have time to read every change an agent makes, so the factory judges the work. The team plans with the factory for as long as it needs, with no cap, and that is where its attention belongs. After the plan is approved, the factory builds and judges the work alone, shows outcomes in plain words, and says plainly what it could not confirm. The product line: **"Spend your time deciding what to build. None watching it get built."** Positioning: **the software factory that earns its autonomy.** Trust comes from what happened after a person accepted the work.

## Vocabulary

Task, trigger, run, runner, worker, ticket tracker, git host and orchestrator are used as in Matt Pocock's software-factory skill (MIT), with thanks. A **run** is one execution of a task, from trigger to finish.

- **Harness**: the coding agent program a worker starts: Claude Code, Codex CLI, or a stub for tests and demo mode.
- **Goal**: a business outcome, as an issue labelled `factory:goal`. **Spec**: the goal written out in `docs/specs/<slug>.md`, with numbered acceptance criteria in the person's words. **Ticket**: one piece of the plan, a sub-issue of the goal. **Change**: the one commit a ticket produced, with its evidence.
- **Planning**: a live conversation between the person and an agent that ends when the person approves the plan. **The plan is approved** is the moment the factory starts working alone.
- **Acceptance check**: an executable check for one criterion, written before building, that the builder cannot change.
- **Machine gate**: what a change must pass before it is combined into staging.
- **Staging**: the branch `factory-staging`: the default branch plus every change that passed. **Accept**: a person's choice to move the default branch to the staging state they saw.
- **Exception**: a plain question or statement that the factory sends to a person after the plan is approved.
- **Publish step**: the step that writes to the git host, outside the worker. **Evidence bundle**: the record of what a run did and what was checked, built by the orchestrator.

## Three moments and two views

| Moment | The factory asks, in plain words | It never asks |
|---|---|---|
| What do you want? | A planning conversation with no limit on rounds or questions. It ends with "here is what you asked for and how we will know it works. Right?" | Anything about code |
| Is this what you meant? | The acceptance list with its states, and Try it where the project gives a start command | To read a change |
| Accept it? | "This is what works now. Accept?" | To approve a merge |

After the plan is approved, at the default dial level, nothing else reaches the person except exceptions. An **exception** is one plain question about what they want, or a plain statement that a line is not working yet. It is never a request to read a change. A high-risk change is put as its consequence: "this changes how other programs talk to yours. Is that what you want?".

**Try it** starts the project's start command the same way the checks are run (see the verify stage). The person is told that it runs code the factory wrote and no person has read.

**Outcome view**, what the whole team watches: goals, the acceptance list with its three states, plain questions waiting for a person, Try it, Accept, Undo for each change, and the trajectory of each goal. It never uses the words pull request, merge, diff, review, branch or commit, so a product owner or a teammate from another discipline can also read it. **Engineering view**: runs, evidence bundles, decision cards, pull requests, the builder's and reviewer's opinions. In the web app the outcome view is the Floor, Needs you and Goals screens, and the engineering view is Runs and changes and Autonomy (record 0004). The outcome view opens first for everyone, and a person can set the engineering view as their own start page. An engineer who wants control takes it on purpose: opens the engineering view, sets a task's dial to guided, or takes the wheel on a ticket. The factory builds the rest.

## Lanes and planning

**The process fits the size of the job.** A heavy method on a small job wastes hours. No method on a large job starts a bad course. The first release has two lanes.

| Lane | For | Planning, with the person | The plan is approved when | Then, alone |
|---|---|---|---|---|
| Quick | A fix, a tweak, a small feature | A short conversation: the goal and one to three acceptance lines | The person confirms the lines | The orchestrator writes a short spec file (the goal and its numbered lines) and creates one ticket. No `brainstorm` or `decompose` run. Then `acceptance`, `implement`, the machine gate |
| Standard | A feature | `brainstorm` to a spec, then `decompose` to tickets | The person approves the plan after approving the spec | `acceptance`, then each ticket |

- The factory proposes a lane. The person can change it with one action. How the factory should choose is not settled; until real runs show better, the person's choice wins.
- Planning shrinks with the size of the job. Checking does not. Every lane has acceptance checks and the machine gate.
- A goal moves up a lane only when it goes off course and the person agrees. It never moves down without the person saying so.
- Direction for a later release, not designed here: a **deep** lane for a new product or a large rework. It would add research, a roadmap and phases, built one phase at a time. It is likely to be driven by an installed plugin such as Open GSD, after a trial.

**Planning is interactive and has no limit.** The person spends as much time as they need. Planning runs as a live conversation, either in the person's own Claude Code or Codex session, through the Model Context Protocol tools, or in the web app, where `oxf` relays the conversation to a harness session. A workflow plugin's interactive planning steps, such as superpowers brainstorming or Open GSD discuss, are used as they are. The unattended contract does not apply to planning.

- When the person says the spec or the plan is ready, an independent `review` runs on it. Its points are shown to the person in the planning conversation. The person decides whether to change it or approve it. A review never blocks or approves a plan on its own.
- An approved spec is combined into staging as a change, so it reaches the default branch with the work on Accept.
- A planning session in the web app writes only the spec, the plan and its notes. It runs under the same permission policy rules as any run.

## Methods

A method is how the agent works inside one run or planning session. The factory does not own it. `spec.method` names one of:

| Value | Meaning |
|---|---|
| `builtin` | Short prompts that ship with `oxf`. Always available |
| `plugin:<name>` | A workflow plugin the person has installed for their harness, such as superpowers or Open GSD. The run tells the agent to use it by name |
| a file path | A prompt file in the repository |

- The factory ships and copies no text from any plugin. At start `oxf` reports which plugins it found and their versions.
- A plugin that is named but not installed is not an error. The run uses `builtin`, records `method` (what was asked) and `methodUsed` (what ran), and the engineering view says so.
- In an unattended run, the unattended contract goes in front of every method, so a plugin step that would ask a person becomes a ruling or a question.
- A plugin's own verification is information only. It never sets an acceptance state and never passes the machine gate.
- The ideas behind `builtin` come from superpowers, Open GSD and Matt Pocock's skills, with thanks. The text is our own.

## Tasks after the plan is approved

Durable state lives in the git host and the orchestrator's store. An unattended run never waits for a person. It ends, and the next event starts the next run. A pipeline is a chain of triggers, not a workflow engine.

| Task | Started by | Writes | Passes when | Attempts |
|---|---|---|---|---|
| `onboard` | A repository is added | A change inside `.factory/` only, with a readiness report | Its review agrees. It takes effect after Accept | Per repository. When used up, a plain statement that setup did not finish |
| `acceptance` | The plan is approved | One check per criterion and a manifest, under the acceptance path only | Its review agrees | Per goal. When used up, every criterion without an agreed check is "could not confirm", the person is told, and building goes ahead. The manifest is then put into staging as it is, with those criteria marked as having no check, so the tickets can become ready |
| `implement` | A ticket is ready | One change | The machine gate | Per ticket, shared with `fix-ci` |
| `fix-ci` | A required check on the git host failed on an open agent pull request | A fix commit on that pull request's branch | The machine gate, again | Per ticket, shared with `implement` |
| `review` | A spec, plan, set of checks or change is ready | `review.json` | Never gated. It is the opinion others use | None |

- `maxAttempts` (default 3: one first try and two rework rounds) is one setting. It counts work-stage starts, including a person's take-the-wheel submission. A run cut off by sleep or a restart does not use up an attempt. Three cut-offs, or three runs that lost their claim, on one ticket stop it until a trusted person removes `factory:needs-human` (record 0003).
- A ticket becomes `factory:ready` when the acceptance manifest of its goal is in staging (with or without a check for every criterion), every ticket it depends on is in staging, and the cap on open agent pull requests (`maxOpenAgentPullRequests`) allows it. Otherwise it stays `factory:planned` and the web app shows why.
- At dial level 1 a person also approves each output before it is combined (see The dial).
- When both harnesses are installed, `review` uses the one that did not write the subject.
- A fix run's own failure triggers nothing. Pull requests from forks are not reviewed in the first release.

## A run and its three stages

1. **Work.** The worker gets a plain directory holding the files of the current staging state (for `review`, of its subject), the input files and a prompt. The directory has no `.git`. The factory gives it no git host credential. It edits files. It does not commit. After the work stage the orchestrator imports the directory's files into the factory's own repository and makes one commit there, under the factory's own author name. A trial showed that one harness's sandbox blocks writes to git's own files, and this way no run needs git in its policy.
2. **Verify.** The orchestrator computes the change, checks protected paths, and runs the task's checks and the acceptance checks on a clean checkout of the commit, with no model access. The checks run code the worker wrote, so they are run as the worker is run: with the same scrubbed environment, never with the tokens in their environment, and inside an operating system sandbox that limits writes to the checkout. The orchestrator starts that sandbox itself, because the harness sandbox covers only the harness. Which sandbox tool is used on each system is not settled, and until it is, checks and Try it run with the scrubbed environment only. That is a stated limit. Then it builds the evidence bundle.
3. **Publish.** The publish step performs the actions the output files ask for, from a fixed list (see Credentials).

An unattended run never asks a person. Each run has a **permission policy**: the commands it may run (`spec.permissions.commands`) and the paths it may write, which are the workspace directory only. The adapter turns it into the harness's own settings. A request outside the policy is denied and recorded. A harness is never started in a mode that asks a person during an unattended run, and never in a mode that skips all permission checks.

The **unattended contract** is a fixed text placed before any method in every run after the plan is approved: you run unattended; never ask for confirmation; where your method says to ask a person, if the choice can be undone and is inside the ticket, decide and record a ruling; otherwise write the question to the result and stop. The contract also lists what the run's policy allows, and says that a denied command is denied alone, so the agent must try an allowed way before it gives up. A trial run stopped wrongly after one denial because it was told neither.

**Take the wheel.** A person can do a work stage, or a planning session, in their own session. `oxf` serves a Model Context Protocol endpoint with four tools, a public contract: `factory_list` (goals to plan and ready tickets), `factory_claim` (claims a goal or ticket, makes the workspace directory, returns the brief and paths), `factory_ask` (posts a question through the publish step), `factory_submit` (hands back a spec, a plan or a change). Verify, review, the machine gate and publish are unchanged.

**Claims across machines.** A run claims its ticket with a marker comment with its machine id and run id, then reads the comments back. The live claim with the lowest comment id wins, and only when it names this machine and this run. The other run releases its claim and ends with no change. Every run releases its claim when it ends. The label `factory:claimed` is a hint for people and for filtering; it never decides a claim. A claim expires after `claimExpiry` in `factory.yaml`, default 2 hours, so a crashed machine does not hold a ticket for ever. Detail in record 0003.

**Team work** is part of the core. Designed, not built, beyond the claims above:

- Each teammate runs `oxf` on their own machine, with their own agent login and their own git host account. Nobody shares a login. The git host is the one place work is coordinated.
- A list of trusted logins in `factory.yaml` says whose machines act for the factory. A claim, an answer or an approval from any other login is ignored.
- A `review` is preferred on a different teammate's machine and harness than the one that built the subject.

**Triggers** act only on events from a trusted person (on the list of trusted logins, with write access), or from the factory itself. An answer or approval from a writer who is not on the list is shown as context and does not count (record 0003). Every comment the factory writes carries a hidden marker with the run id and never triggers a run. An answer given in the web app is posted with an answer marker, and an approval as the label of record 0003. Both count as the person's (record 0004). Kinds: `issueLabeled`, `issueCommented`, `pullRequestOpened`, `pullRequestMerged`, `checkFailed`, `schedule`. On one machine triggers are polled, so no public endpoint is needed.

## The factory's own repository

Git obeys settings stored in a repository: programs to run, addresses to rewrite, other places to read objects from. Packages of the first build were broken this way more than once, by a second agent told to attack them. One attack sent the propose token to another server. The pull requests of 2026-11-26 hold the record. So:

- **The factory never runs a network git command, and never reads configuration, in a directory an agent or a check has worked in.** It keeps its own bare repository per project, which it creates itself, in its own data directory. Every judgement starts there.
- **The agent's workspace is a plain directory with no `.git`.** The orchestrator writes the files out from its own repository before the run and imports them back after it. `internal/repo` has the export and import. The local runner in `internal/runner` uses them to run a work stage: it opens the workspace directory once, before the agent starts, and exports and imports only through that opened directory. It is not yet wired to the rest of the factory. An agent that wants git commands has none. The harness's own file tools do not need them.
- **A guard runs first in every function of the verify stage that uses git.** It fails closed unless every configuration key and every entry in the repository's layout is on a fixed list. It accepts known-good things. It does not hunt for known-bad ones. Creating the repository is the one git command that runs before it, on an empty directory.
- **A push runs from a fresh temporary repository**, into which one commit is fetched from a directory the caller names. That fetch has hardened settings and no guard of its own, so the caller must name the factory's own repository. The remote address is built by the factory. The token is given only to that exact host over https, and only to the one step that pushes.
- **Checks run on a copy written from git objects**, with no `.git`, an allowlisted environment and an empty home directory.

The limit: the guard detects a repository that was tampered with, at the start of each call. It cannot stop a process running as the same user from changing the repository between calls. A check runs code the agent wrote, as that user. A separate operating system user for `oxf`, or a sandbox around checks, is what reduces this. Neither exists in the first release.

## How changes to the factory itself are reviewed

Every package of the first build passed its own tests. Most then failed a review by a second agent that was told to attack it. The findings are listed in the threat model. So for this project, and as the default for the `review` task on security-relevant paths:

- The reviewer is told who the attacker is and what they control, and is asked to break the change with inputs of its own, not to read it for style.
- A finding counts when the reviewer made it happen: a program ran, a token arrived somewhere, a file was written outside its place.
- A fix is first a failing test that reproduces the finding.
- When the same family of finding returns, the approach changes from refusing bad inputs to accepting only known-good ones.
- Two rework rounds, then stop and decide. What remains is written on the change as a known limit and filed as a ticket.

## Rules the first build established

- **An empty, missing or unknown value always means failure, or "do not combine". It never means success.** A blank review value was read as a pass.
- **A fix is new code. It is attacked like any other change before it is merged.** A cleanup added as a fix deleted another run's objects.
- **Every change is tested on Linux as well as on a developer's machine before it is proposed.** Tests that passed on macOS failed in CI on Linux.

The evidence is in this repository's pull requests of 2026-11-26 and 2026-11-27.

## Credentials

The factory never gives a worker a credential for the git host. The publish step holds two keys, kept apart.

| Key | Granted | Used for | Never used for |
|---|---|---|---|
| Propose token | At setup, required | Push branches under `factory/`; write `factory-staging` through the combine action; open and update draft pull requests and the one staging pull request; comment; update its own comments; lock the conversation of a factory ticket, an approved goal or the staging pull request; add or remove labels from a fixed list; create at most twelve issues per run as sub-issues of the goal; set a commit status | Any write to the default branch |
| Accept token | Separately by the person, optional | Merge staging into the default branch on a person's Accept; merge a revert on Undo after Accept; an earned automatic Accept | Anything else |

The git host must enforce the split. On GitHub a token that can push a branch can also merge, so the propose token's user must be one that branch protection does not allow to update the default branch. The factory checks the protection rules at start and shows a standing warning when they are missing. For `factory-staging`, restrict who may push to the propose token's user. Do not forbid force pushes: every rebuild needs one.

**Local mode limit.** In local mode the worker runs as the same operating system user as `oxf`. The factory cannot stop it reading what that user can read, including the home directory. Controls: the tokens are kept in the operating system's credential store, not in a plain file; git host credentials are removed from the worker's environment; the harness sandbox limits reads outside the workspace directory where the harness supports it. Recommended setup on a dedicated machine, not built in the first release: run `oxf` under its own operating system user, apart from the user the agent runs as.

## The worker contract

The same for every runner and every harness. Paths are shown as in a container. The local runner gives directories in their place, named in `FACTORY_IN` and `FACTORY_OUT`.

**In**

| Path or variable | Content |
|---|---|
| `/workspace` | The files of the staging state, as a plain directory with no `.git`. Read-write. No remote credential is given |
| `/factory/in/task.json` | The resolved task, read-only |
| `/factory/in/prompt.md` | Unattended contract, method, task prompt, trigger data. Ticket and comment text is marked as untrusted |
| `/factory/in/trigger.json` | The trigger payload, and for rework the review points and the failure record |
| `/factory/out/` | Empty, writable |
| `FACTORY_MODEL_BASE_URL`, `FACTORY_MODEL_API_KEY`, `FACTORY_MODEL` | Model access, only for metered runners. Not set locally |
| `FACTORY_DEADLINE_SECONDS`, `FACTORY_MAX_COST_USD` | Limits |

**Out.** The worker never calls the git host. It writes files, and the publish step acts on them. Every file is parsed strictly: an unknown field, a missing file or an invalid file means the run failed. The orchestrator never parses free text to decide an outcome.

`/factory/out/result.json`, always:

```json
{
  "schemaVersion": 1,
  "status": "succeeded",
  "summary": "Customers can move a booking to another day.",
  "claims": [ { "text": "Added a move endpoint with a test", "files": ["internal/booking/move.go", "internal/booking/move_test.go"] } ],
  "criteria": [ { "id": 1, "opinion": "met", "note": "Covered by the new handler" } ],
  "rulings": [ { "decision": "Kept the old endpoint as an alias", "why": "Two callers still use it", "costIfWrong": "One extra route to remove later" } ],
  "questions": [],
  "usage": { "billing": "plan", "inputTokens": 12000, "outputTokens": 3400 },
  "harness": { "name": "claude-code", "version": "2.1.326", "model": "" }
}
```

- `status` is one of `succeeded`, `failed`, `needs_human`, `no_change`. `criteria[].opinion` is one of `met`, `not_met`, `unsure`. It is shown in the engineering view only and never sets a criterion's state.
- A **ruling** is written when the method would have asked a person, the choice can be undone and it is inside the ticket. Otherwise the worker writes a question and ends with `needs_human`. Questions go to the inbox. No run waits on them; the ticket stops until a trusted person answers on the ticket and removes `factory:needs-human` (record 0003).
- `usage.billing` is `metered` (an API key or gateway: dollars and tokens), `plan` (a subscription login: tokens, no dollar figure) or `unknown`. `costUSD` is given only for metered usage. No dollar figure is ever estimated for plan usage. `usage.source` and `usage.enforced` are written by the orchestrator; values from the worker are ignored. The evidence bundle takes `worker` and `usage` from the adapter the orchestrator started, never from `harness` or `usage` in `result.json` (record 0003).

`/factory/out/issues.json`, from `decompose`:

```json
{ "schemaVersion": 1, "issues": [
  { "title": "Move endpoint", "body": "Let a customer move a booking.", "criteria": [1, 2], "dependsOn": [] },
  { "title": "Confirmation email", "body": "Send an email after a move.", "criteria": [3], "dependsOn": [0] } ] }
```

`dependsOn` lists indexes in the same file. The publish step writes dependencies and criteria into each issue body in a fixed, machine-read block.

`/factory/out/review.json`, from `review`. `subject` is `spec`, `plan`, `acceptance` or `change`; `verdict` is `agrees`, `concerns` or `disagrees`; `severity` is `minor`, `major` or `blocking`. The publish step renders it as one comment.

```json
{ "schemaVersion": 1, "subject": "change", "verdict": "agrees",
  "points": [ { "severity": "minor", "text": "The error message is vague", "file": "internal/booking/move.go" } ],
  "criteria": [ { "id": 1, "opinion": "met", "note": "" } ] }
```

`/factory/out/events.jsonl`: one JSON object per line for the live view, of kind `message`, `tool`, `status` or `denied`. Never a secret. Also: the changed files in `/workspace`, which the orchestrator commits, and the raw harness log on standard output and error.

**Harness adapters** are thin Go code that build the command line for one program (`claude -p --output-format stream-json`, `codex exec --json`) and read its event stream. These flags were used in the trial runs with Claude Code 2.1.326 and Codex CLI 0.170.0 on 2026-11-26. The adapters are built in `internal/harness`, with a stub, and are not yet wired to the rest of the factory. **A stub harness** makes fixed edits from a script, for tests and demo mode. No paid model calls in tests.

## Acceptance checks

The person's answers to "how will you know it worked?" become the spec's numbered acceptance criteria. `acceptance` runs once the plan is approved and before any building. It writes one executable check per criterion and one manifest per goal, under `acceptance/<goal number>/` (the root is `acceptancePath` in `factory.yaml`):

```json
{ "schemaVersion": 1, "goal": 40, "criteria": [
  { "id": 1, "text": "A customer can move a booking to another day", "run": "go test ./acceptance/40 -run TestMoveBooking" },
  { "id": 2, "text": "Works on a phone", "run": "", "reason": "No phone browser is available to the checks" } ] }
```

- The acceptance path is protected for every task except `acceptance`, and `acceptance` may write nothing else. The builder cannot change the manifest or the check files.
- The manifest is not configuration. The orchestrator reads it from the staging state under test. The protected-path rule keeps a change from altering it.
- The checks pass their own independent review, which compares each check with the person's wording, before they are combined into staging. If the review does not agree, its points go back to `acceptance` while the goal's attempts last.
- The orchestrator runs each command in the verify stage of every change and on every staging rebuild. A check that already passes before any building is flagged in the engineering view.
- A check may be changed only by a new `acceptance` run that passes its own review. When a builder or reviewer says a check is wrong, that is recorded, and the orchestrator may start such a run. `implement` never changes a check. Such a run uses the goal's `acceptance` attempts.

The state of each criterion is computed only by the orchestrator, from the checks. There are three states and no others. The outcome view may add a note such as "being worked on" beside a line that is not working yet; a note is not a state.

| State | Meaning |
|---|---|
| Working | Its check passes |
| Not working yet | Its check fails |
| Could not confirm | No check exists for it, `acceptance` reported that it could not write one, or no check passed review. Never shown as working |

## The machine gate and risk

A change from `implement` or `fix-ci` passes the machine gate when all three hold, in this order:

1. **Verify passes**: no protected path was touched, the task's checks pass, and no acceptance check that is working on staging fails on the change.
2. **An independent review agrees**: `review.json` with subject `change` and verdict `agrees`.
3. **Risk is not high**, by the rules below.

If verify fails or the review does not agree, the failure or the review points go back to the builder as a new attempt. No person can pass a change whose review did not agree. A rework or `fix-ci` commit makes earlier verify and review results stale, and the change goes through the gate again. When the ticket's attempts are used up, its goal is off course (see Trajectory), and that is the one notice the person gets: in plain words, the ticket's lines are not working yet.

A review by the same harness as the builder may pass a change, but the change is marked **single-opinion**. Single-opinion changes never count toward automatic Accept.

**Risk** is about what changed, never about the review. It is rule based; no model sets it. The rules are tried in order and the first match wins.

| Order | Risk | When |
|---|---|---|
| 1 | High | A contract, security or dependency path from `riskPaths` changed. Or an existing test file was changed or deleted |
| 2 | Low | Only paths listed as documents changed |
| 3 | Low | The review came from a different harness than the builder, every criterion the ticket serves is working with the change, every changed file is covered by a claim, and the change is under a size limit from the settings |
| 4 | Medium | Everything else. This includes a ticket that serves no criterion, a served criterion that is not working yet or could not be confirmed, and a review by the same harness as the builder |

New test files do not raise the risk. Changing or deleting an existing test does, because that is how a change makes itself pass. A change to a protected path, or one that breaks a working acceptance check, fails verify before risk is computed.

A change that is high risk and has an agreeing review is an exception: one plain question about its consequence. A yes combines it, recorded as passed by a person. A no takes it back to its ticket as a new attempt.

**The decision card** is the engineering view of one change, computed by the orchestrator from structured inputs, never written by a model: changed paths under `riskPaths` and changed test files; the rulings, each with an overturn action; what is not confirmed (files no claim covers, criteria that could not be confirmed, opinions that differ from the computed state, policy denials); the review verdict and points; the risk with its reasons.

## Staging, Accept and Undo

There is one staging branch per repository, `factory-staging`. It is not under the `factory/` prefix. Only the publish step's **combine** action writes it. `fix-ci`, agent pull requests and the agent-branch rule do not apply to it.

**Staging is rebuilt, never patched.** The orchestrator keeps the list of passed changes, in the order they passed. Staging is the current default branch plus each change on the list, one commit per change. On any event (a change passes, a change is undone, the default branch moves), the orchestrator rebuilds staging from the default branch and the list, and runs the task checks and the acceptance checks on the result.

- A change that no longer applies cleanly, or whose addition makes a task check or a working acceptance check fail, is taken out. The orchestrator finds the first such change in order, takes it out and rebuilds again. The change goes back to its ticket and is built again on the new staging, which uses an attempt. If the ticket has no attempt left, the goal is off course.
- A change already contained in the default branch drops off the list.
- If the default branch alone fails a task check, staging stops at the default branch. The outcome view says the project's own checks are failing and a developer is needed.
- Each staging state is recorded: its commit, the default branch commit it was built on, its changes in order, and the state of every criterion.
- With several machines, one machine holds the staging claim for a repository, taken and expired by the same rule as a ticket claim, on the staging pull request, whose conversation is locked the same way (record 0003). Only that machine combines, rebuilds and accepts.

**Agent pull requests.** Each change has a branch `factory/<run id>` and a draft pull request into `factory-staging`, labelled `factory:agent`. It is the engineering trace and the place where `fix-ci` works. When the change is combined, the publish step closes the pull request with a comment naming its commit on staging.

**Accept** takes the whole of staging: the exact state the person saw, named by its commit. Accept and Undo always need the web app's login token, also on localhost. If staging has been rebuilt since, or its branch head differs from the recorded state, Accept is refused and the person sees the current state. While an Accept is pending, staging is frozen: no rebuild and no new change until the Accept lands or the person cancels it.

- With the accept token, the publish step merges the staging pull request into the default branch with a merge commit, so each change keeps its own commit and outcomes are attributed per change.
- Without it, the outcome view says "ask a developer to finish this step", and the engineering view says that a developer must merge the staging pull request without squashing. If the per-change commits are lost, the outcomes of those changes are recorded as unattributed.
- If the git host refuses the merge, that is an exception and the Accept is cancelled.

**Undo** works on any change on the list, not only the last. Before Accept, the change is taken off the list and staging is rebuilt. Changes whose tickets depend on its ticket are undone with it, and the person is told which lines this affects. After Accept, Undo is a revert commit on the default branch, merged with the accept token, then a rebuild; without the accept token the factory opens the revert as a pull request for a developer. An undone change's ticket is closed as taken back and is not built again unless the person asks.

## The dial

One level per unattended task sets where the factory stops for a person after the plan is approved. Planning is always with the person and is not on the dial.

| Level | Name | A person approves |
|---|---|---|
| 0 | Off | The task does not run |
| 1 | Guided | Each output of the task before it is combined. This is the engineering profile. Until a guided output can be approved, it starts no work |
| 2 | Autopilot | Nothing until Accept. The default |
| 3 | Automatic Accept | Nothing, for classes of change that earned it. Shown, not active, in the first release |

- Exceptions reach a person at every level.
- The first level of each task is written by `oxf init`, never by `oxf run`. A missing or unreadable level means the task is held (record 0003).
- `spec.autonomy.ceiling` in the task file sets the highest level. It changes only through the default branch.
- The current level is stored by the orchestrator, per machine, and moved in the web app at or below the ceiling (record 0004). Every change is logged with time and the previous level. On OpenAgentix, platform policy supplies the ceiling and can only lower it.

## Trust and the attention record

**Observe before enforce** (designed, not built). Any rule that can stop or pass a change, such as a dial level, a risk rule or a class switch for automatic Accept, can run first in an `observe` mode. The factory records what it would have decided and changes nothing. A team reads that record before it turns the rule on.

Trust comes from outcomes after Accept, never from the act of accepting, and never from opening a card. An outcome is recorded per change, by its commit, inside a window:

- **Bad**: the change is reverted, a later fix names it, or the default branch turns red on the Accept merge commit and stays red when run again. A red build counts against every change in that Accept.
- **Good**: none of that happened, and the change was exercised: a check on the default branch ran tests that cover the changed files, as far as the project's test tool can tell, or later the change was deployed.
- **Silent**: otherwise. Silence counts for nothing. **Unattributed**: the change's commit was lost in the merge. It counts for nothing.

A documents-only change has nothing to exercise. For that kind alone, good means no revert and no correcting change inside the window: weaker evidence, for less at stake.

A **class of change** is the task, the risk level, and the most serious kind of path touched, in this order: security, contract, dependencies, code, tests, documents. The orchestrator assigns it from the change. A class is **eligible** for automatic Accept when it is low risk and, inside the window, has at least a set number of good outcomes, no bad outcome, and every review from a different harness than the builder. The count and the window are settings with no default. The factory proposes an eligible class and a person switches it on. Every switch is logged. A bad outcome switches the class off, and it needs a fresh full count. In the first release eligibility is computed and shown only.

The **attention record**, per task: touches per accepted goal, questions asked, rulings made, rulings overturned. A ruling is overturned when a person marks it so in the engineering view. Many overturned rulings say turn the dial down. Planning time is not counted against the factory.

**Who still has to act.** Every exception, and every change waiting at a gate, states in plain words in the outcome view who must act and what is needed. Designed, not built.

## Trajectory

Each goal has a course, computed by rules, never by a model. The thresholds are settings. These starting values are untested.

- **Off course** when any of these happens: a ticket of the goal uses all its attempts; the same criterion stops working on two rebuilds, counted even when the change that caused it was taken out; a person undoes changes of the goal twice. One Undo counts once, however many dependent changes go with it.
- **Drifting** when a criterion stopped working on one rebuild, or one change of the goal was undone, or no criterion of the goal became working over its last three changes. **On course** otherwise. Drifting: the factory keeps working and says so in the outcome view.

The **last good point** of a goal is the most recent staging state in which every criterion of that goal that had ever been working was working. It never goes back past accepted work.

**Off course** has one answer:

1. Stop building on that goal and close its open tickets.
2. Take that goal's changes on staging back to its last good point: they come off the list and staging is rebuilt. Other goals' changes stay.
3. Tell the person in plain words which lines are not working yet.
4. Plan again with the person, with the failure record as input. For a quick goal, propose the standard lane.

## The run record and the evidence bundle

The **run record** is stored in SQLite. Its field names follow the planned OpenAgentix `AgentRun`, so that kind can back it later: `id` (a time-ordered random id made on the machine, unique across machines; record 0003), `task`, `attempt`, `goal`, `ticket`, `trigger` (kind and reference; the kind is one of the six trigger kinds, `factory` for a run the factory started itself after another run, such as `acceptance`, `review`, `onboard` or a rebuild, or `person` for take the wheel or an action in the web app), `mode` (`unattended` or `wheel`), `method`, `methodUsed`, `input` (resolved task and prompt hash; from record 0003 also `input.base` and `input.subject`), `limits`, `phase`, `result`, `usage` (with its source), `provenance` (the evidence bundle), `outputs` (branch, commit, pull request), `createdAt`, `startedAt`, `finishedAt`, and from record 0003 `heldReason`, `endReason`, `concurrencyKey`, `claim` and `startedOnHost`. The phases are `queued`, `held` (waiting, with a reason code from record 0003, such as the cap, a throttle or a staging freeze), `working`, `verifying`, `publishing`, `succeeded`, `failed`, `needs_human` and `cancelled`.

The orchestrator also stores each staging state and the list of passed changes, the dial levels and their log, and the outcome of each accepted change. The trust record is a query over these and outcomes read from the git host, never a second source of truth.

The **evidence bundle** is built by the orchestrator in the verify stage. The review, risk and gate fields are added when the review ends.

```json
{
  "schemaVersion": 1,
  "run": "r_01H8Z", "task": "implement", "goal": 40, "ticket": 42, "attempt": 1, "mode": "unattended",
  "method": "plugin:superpowers", "methodUsed": "builtin",
  "trigger": { "kind": "issueLabeled", "ref": "#42" },
  "worker": { "name": "claude-code", "version": "2.1.326", "model": "" },
  "base": "9f2c1e7", "commit": "a1b2c3d",
  "diff": { "files": 2, "added": 84, "removed": 12, "paths": ["internal/booking/move.go", "internal/booking/move_test.go"] },
  "claims": [ { "text": "Added a move endpoint with a test", "files": ["internal/booking/move.go", "internal/booking/move_test.go"] } ],
  "rulings": [ { "decision": "Kept the old endpoint as an alias", "why": "Two callers still use it", "costIfWrong": "One extra route to remove later" } ],
  "denials": [ { "request": "curl https://example.com", "reason": "command not in policy" } ],
  "protectedPaths": { "touched": [], "ok": true },
  "testFilesChanged": [], "testFilesAdded": ["internal/booking/move_test.go"],
  "checks": [ { "name": "test", "exitCode": 0, "durationSeconds": 41, "ranBy": "orchestrator" } ],
  "acceptance": [ { "id": 1, "state": "working", "before": "not_working", "check": "go test ./acceptance/40 -run TestMoveBooking" } ],
  "review": { "run": "r_01H9A", "harness": "codex", "verdict": "agrees", "singleOpinion": false },
  "risk": { "level": "low", "reasons": ["review from the other harness", "served criteria working"] },
  "gate": "passed",
  "usage": { "billing": "plan", "inputTokens": 12000, "outputTokens": 3400, "source": "harness", "enforced": false },
  "limits": { "deadline": "30m", "maxCostUSD": 5 }
}
```

`checks[].timedOut` is true when the deadline stopped the check, and is left out otherwise. `acceptance[].state` and `before` are `working`, `not_working` or `could_not_confirm`. `check` holds the manifest command that produced the state. It is required for `working` and `not_working`, and empty for `could_not_confirm`. `gate` is `pending`, `passed`, `failed` or `exception`. `usage.enforced` is true only when a governance provider enforced the limit; otherwise the web app says "reported, not enforced". A run with no change still records worker, usage, limits and outputs, with diff, checks and risk empty. The agent pull request body has three parts: Summary, Evidence, Risk.

## Interfaces that vary by where it runs

**How to undo, and spending limits** (designed, not built). Every action the publish step takes is recorded with how to take it back, and Undo in the outcome view uses that record. For metered use there is also a spending limit per task kind, beside the per-run limit: reported on a laptop, enforced on OpenAgentix.

```go
type Runner interface { Start(ctx context.Context, spec WorkSpec) (Handle, error) } // the work stage
type Handle interface { Logs(ctx context.Context) (io.ReadCloser, error); Wait(ctx context.Context) (int, error); Cancel(ctx context.Context) error }
type Governance interface { // model access and recorded use
    ModelAccess(ctx context.Context, run RunRef) (ModelAccess, error)
    Enforced() bool
    Record(ctx context.Context, run RunRef, usage Usage) error
}
type GitHost interface { /* poll events, read tickets, the fixed publish actions with the propose or accept token */ }
```

| Where | Runner | Governance |
|---|---|---|
| Local, first release | A process in a fresh workspace directory | The person's own login, usage as reported |
| Container, later | Docker, with a worker image built on the person's machine | An API key |
| Kubernetes, later | A Job | Metered model access |
| OpenAgentix, later | A Job, or platform runs when `AgentRun` and `Workspace` exist | A gateway key per run, an enforced budget, an audit record |

**On OpenAgentix** (designed, not built; the platform pieces are planned and not all exist, and the factory uses them as the platform provides them): the factory's concepts map onto platform resources. A run is the platform's planned run resource and the workspace is its planned workspace. The propose and accept credentials are platform credential grants. The dial's ceiling comes from platform policy and can only be lowered there. A person's Accept on a high-risk change is a platform approval request. Model access goes through the platform gateway, with a key and an enforced budget per run, and outbound rules are the platform's. The evidence bundle becomes the platform's signed provenance record, and the web app mounts in the platform console. The factory keeps its own API group and concepts. The same task files and acceptance manifests run unchanged on a laptop and on OpenAgentix. What is enforced there and only reported on a laptop: budgets, isolation of a run from the person's files and from other runs, outbound rules and audit. The limits named in this record, the same-user limit and no operating system sandbox for checks, are the ones this rung removes.

**Local runner rules**, and others that vary by place:

- **Setup** (designed, not built). For each agent program, the factory shows whether runs will be billed to an API key or to the signed-in plan, and recommends a key for a public repository.
- It starts the official `claude` or `codex` program in place. It never reads, copies or forwards a login token. Login and billing position: see Not settled.
- It removes git host credentials from the process environment: no token variables, no credential helper, no agent socket. This does not stop the agent reading the home directory (see Credentials, local mode limit).
- It uses the strictest sandbox setting of the harness that still lets the task run. A deadline always applies, and the process group is killed at it. Runs are designed to be safe to repeat. No trial has cut a run off part way to show it.
- A cost limit applies only to metered usage. The orchestrator reads usage from the event stream and cancels the run past the limit. This lags, so the web app says "best effort".
- **Toolchain.** Locally the stages use the compilers on the machine. The container runner names a base image in `factory.yaml` (`image:`). Outbound network is open in both stages.
- **Demo mode.** A fake git host with a seeded, made-up project and a story that waits for the person, the local runner and the stub harness. No login, no token. Every screen says it is a demo (record 0004).
- **Listening.** On a laptop, localhost. On a dedicated machine `oxf` runs as a service and may listen on the local network. The web app has no user accounts: every data route needs the login token, on every address, localhost included (record 0004).

## Configuration

In the repository under `.factory/`: `factory.yaml`, `tasks/*.yaml`, `prompts/*.md`. The orchestrator reads `.factory/` from the default branch only, so a change to it takes effect only after Accept. Unknown fields are an error. The schema version is in `apiVersion`. Task files are never taken from the binary: a repository with no `.factory/` runs nothing. Until `onboard` is built, a person commits `.factory/` by hand. The prompts of the `builtin` method ship in the binary.

**Protected paths.** `.factory/**` (except for `onboard`, which may change nothing else) and the acceptance path (except for `acceptance`) are always protected. No agent run, `onboard` included, may change `spec.trustedLogins`, `spec.claimExpiry` or `spec.lockTickets`; only a person does (record 0003). `factory.yaml` `protectedPaths` adds to them and defaults to `.github/**`, so a change to CI configuration fails the run. A repository may remove `.github/**` from that list and put it under `riskPaths.security` instead. No path may appear both in a protected list and in `riskPaths`; validation rejects that. A task's `protectedPaths` adds to all of these.

```yaml
apiVersion: factory.openagentix.org/v1alpha1
kind: Factory
metadata: { name: main }
spec:
  repository: acme/widgets
  defaultWorker: { harness: claude-code }
  maxOpenAgentPullRequests: 3     # changes open and not yet combined
  pollInterval: 1m
  trustedLogins:                  # required, 1 to 20; see record 0003
    - { login: alice, id: 1001 }
  claimExpiry: 2h                 # the default
  lockTickets: true               # the default
  acceptancePath: acceptance
  start: "make run"               # optional, lets Try it start the application
  protectedPaths: [".github/**"]  # the default
  riskPaths:
    contract: ["api/**"]
    security: ["internal/auth/**"]
    dependencies: ["go.mod", "go.sum"]
    documents: ["docs/**", "*.md"]
```

```yaml
apiVersion: factory.openagentix.org/v1alpha1
kind: Task
metadata: { name: implement }
spec:
  description: Build one ready ticket as a change.
  trigger:
    issueLabeled: { label: "factory:ready" }
    throttle: { maxPerDay: 5 }
  worker: { harness: claude-code, model: "" }   # claude-code, codex or stub; empty model means the default
  method: builtin                               # builtin, plugin:<name>, or a prompt file
  prompt: prompts/implement.md
  permissions:
    commands: ["go build ./...", "go test ./..."]
  checks:
    - { name: test, run: "go test ./..." }
  limits: { deadline: 30m, maxCostUSD: 5, maxAttempts: 3 }   # cost: metered only; attempts per ticket, shared with fix-ci
  autonomy: { ceiling: 2 }
  outputs: { pullRequest: { draft: true }, comment: true }
  concurrencyKey: "issue-{{ .Number }}"
```

Machine settings (installed harnesses, the listen address) live in the person's home directory, never in the repository. A value every machine of a team must share, such as the claim expiry, is in `factory.yaml` (record 0003). The two tokens are kept in the operating system's credential store, never in a task file, never in a plain file, never in the database. On localhost the login token is made fresh at each start and never stored; for a service on another address it is kept in the credential store (record 0004).

## Stack

Go, one binary, with the web app embedded through `go:embed`. SQLite through a pure Go driver, so no C toolchain is needed. The web app is Svelte 5 with runes, SvelteKit as a static single-page app, no utility style framework for now with the approved design's CSS variables as the tokens, no component kit, a strict content security policy, and live events by server-sent events read with `fetch` (record 0004). The container runner's worker image is built on the person's machine, so the Claude Code binary is never shipped in a public image. Dependencies are MIT or Apache-2.0 only. Bundled fonts may be under the SIL Open Font Licence 1.1 (record 0004).

## Name

OpenAgentix Software Factory, decided by the owner. The site and the API group are `factory.openagentix.org`, a domain the owner holds. The command `oxf` is a working name. Tagline: "Runs on your laptop. Governed on OpenAgentix." The second half is designed, not built. The group is the product's own, separate from the platform group `openagentix.io`, as OpenAgentix ADR 0012 requires. The risk of one brand is that the name suggests the platform is required; the tagline and a quick start with no platform must answer that. No trademark search was done. The name and group live in one Go constant and one web constant.

## What runs have shown

Four trial runs by hand on 2026-11-26 are written up in `docs/trials/2026-11-26-unattended-runs.md`. In short: Claude Code and Codex both start with no terminal and end with a result file a program can read. A deny-by-default policy denies without asking and records the denial. A ruling and a question both come out in the result file as designed. A start on a subscription login works and can be told apart from a start on an API key. These were tiny tasks on one machine. Everything else in this record is still unproven.

## Not settled

Left to the first real runs, and not designed further here:

- Every threshold: the trajectory values, the trust count and window, the size limit in the risk rules, the claim expiry (default 2 hours, untested).
- The harness flags for a no-prompt permission policy are known for Claude Code from a trial (deny by default with an allowlist). For Codex only the sandbox setting was tried.
- Whether to shut a person's global agent instructions and tools out of a run. A trial showed they leak in, cost tokens and change behaviour.
- How well "exercised" can be computed.
- How staging rebuilds behave on a large repository, in time and in checks run, and how the staging claim passes between machines.
- **Logins and plan terms.** The factory starts the unmodified, official agent program that a person is signed in to on their own machine. It never reads, copies, shares or pools a login. Each teammate uses their own. Whether a plan allows the amount and kind of unattended use a team intends, and whether a personal login may be used for a public or open-source repository, is for that team to check with the vendor of each program. Vendors publish guidance on automation. It differs between programs and changes over time. The maintainers read the published terms and guidance of both programs on 2026-11-27. That was a reading, not legal advice, and nothing in this project is an assurance about any plan. Anyone running the factory on a public or open-source repository should use an API key unless they have confirmed their plan allows otherwise. Any hosted or shared service must use API keys or each person's own sign-in. At setup the factory tells the person which billing each agent will use (see Local runner rules, Setup). If a plan does not fit, unattended runs need an API key, and take the wheel remains.
- A workflow plugin was loaded for one run by path and its skill was called by name, in one trial with Claude Code. Driving a whole planning or build method this way, and doing it with Codex or with Open GSD, is not tried.
- How the factory should choose a lane.

## Limits

- Apart from four small trial runs of the unattended start, nothing here has been run. Every rule is unmeasured, including its false alarms and misses. The whole weight is on the factory's own checking, the least proven part of this field. "Working" is only as strong as its check. A check can pass for the wrong reason, and a builder can change the code a check calls.
- Local mode has no container. The worker runs as the same user as `oxf` and can read what that user can read, including the home directory. The factory gives it no credential, but cannot wall the machine off.
- Try it runs code no person has read on the person's machine, inside the checks' sandbox.
- Usage is what the harness reports. Nothing is enforced locally. Outbound network is open.
- A review can be steered by text in the change. With one harness installed it shares the builder's blind spots.
- The agent chooses which files it touches, so it can keep a change in a low-risk class. `riskPaths` and the size limit reduce this. They do not remove it.
- With one maintainer, the same person plans, accepts and switches a class on. The record is honest, but not independent.
- One staging branch means goals in flight are accepted together. A person who wants only part uses Undo first.
- A red build after Accept counts against every change in that Accept, which is coarse. Try it needs a start command from the project; otherwise the outcome view shows the acceptance list alone.
- The web app has no user accounts. Anyone with the login token can press Accept.
- The accept token is a stronger credential than proposing needs. It is optional and held by the publish step only.
- Two harnesses from the start keep the contract honest, at the cost of two adapters. Polling adds delay before a run starts.

## History

The sections on the factory's own repository and on how changes are reviewed were added on 2026-11-26 from what the first build showed.

This record was consolidated on 2026-11-26, after three independent reviews in a row found contradictions between decisions that had been stacked on each other during one day. Two changes were merged unread on the first day, which showed that a merge button is one more "yes" and led to the design for an engineer who should not have to read every change. Each time, an independent reviewer found the real problems before a person did, which led to the machine gate and the acceptance checks.

On 2026-11-27 record 0003 changed these rules: triggers count only from trusted persons, claims are ordered by comment id with a 2 hour expiry in `factory.yaml`, cut-offs per ticket are limited, a missing dial level means do not run, questions stop their ticket, the run record has new fields, evidence takes worker and usage from the orchestrator, and the propose token may update its own comments and lock a ticket. After a review the same day: `oxf init` writes the first dial levels, claims must name their own machine and run and are always released, `factory:claimed` is a hint only, task files never come from the binary, and `onboard` may not change the team fields of `factory.yaml`.

On 2026-11-27 record 0004 changed these rules: the outcome and engineering views are named screens of the approved design, demo mode uses the local runner and waits for the person, every data route needs the login token on every address, the login token is made fresh at each start on localhost, the dial level is per machine, an approval in the web app is the label of record 0003, and the Stack drops the utility style framework, reads live events with `fetch` and allows bundled fonts under the SIL Open Font Licence 1.1.
