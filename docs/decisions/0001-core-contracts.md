# 0001. Core contracts for the single-machine factory

- Status: proposed, 2026-11-26. The owner approved the scope and the positioning. Nothing in this record has been run. Every rule below is a design, not a result.
- Scope: the local product, on a laptop or a dedicated machine, using the Claude Code and Codex already installed there. Every contract is shaped so that a container runner, Kubernetes and OpenAgentix need no schema change.
- Product: OpenAgentix Software Factory. Command `oxf`. API group `factory.openagentix.org`.

## Context

Free tools already turn an issue into a pull request. Reports from practitioners say where teams hurt now: review capacity is the limit, agents edit tests to pass them, pull request text often does not match the change, cost per outcome is not visible, and there is no basis for trusting an agent. These come from other people's reports, not our runs, so this record quotes no numbers.

The product is for a person who states a business goal and will not read code. It must do the judging itself, show outcomes in plain words, and say plainly what it could not confirm. Positioning: **the software factory that earns its autonomy.** Every run leaves evidence. Trust comes from what happened after a person accepted the work.

## Vocabulary

Task, trigger, run, runner, worker, ticket tracker, git host and orchestrator are used as in Matt Pocock's software-factory skill (MIT), with thanks. A **run** is one execution of a task, from trigger to finish.

- **Harness**: the coding agent program a worker starts: Claude Code, Codex CLI, or a stub for tests and demo mode.
- **Goal**: a business outcome, as an issue labelled `factory:goal`. **Spec**: the goal written out in `docs/specs/<slug>.md`, with numbered acceptance criteria in the person's words. **Ticket**: one piece of the plan, a sub-issue of the goal. **Change**: the one commit a ticket produced, with its evidence.
- **Acceptance check**: an executable check for one criterion, written before building, that the builder cannot change.
- **Machine gate**: what a change must pass before it is combined into staging.
- **Staging**: the branch `factory-staging`: the default branch plus everything that passed. **Accept**: a person's choice to move the default branch to the staging state they saw.
- **Exception**: anything the factory cannot settle alone. The only thing that interrupts a person.
- **Publish step**: the step that writes to the git host, outside the worker. **Evidence bundle**: the record of what a run did and what was checked, built by the orchestrator.

## Three moments and two views

At the default dial level a person is asked three things, and more only when an exception needs them.

| Moment | The factory asks, in plain words | It never asks |
|---|---|---|
| What do you want? | Questions about the business and the outcome, then "here is what you asked for and how we will know it works. Right?" | Anything about code |
| Is this what you meant? | The acceptance list with its states, and Try it where the project gives a start command | To read a change |
| Accept it? | "This is what works now. Accept?" | To approve a merge |

The first moment ends when the person approves the spec, which they see as the acceptance list. An **exception** reaches the person as one plain question about what they want, or as a plain statement that a line is not working yet, never as a request to read a change. A high-risk change is put as its consequence: "this changes how other programs talk to yours. Is that what you want?".

**Outcome view**, the default: goals, the acceptance list with its three states, plain questions, Try it, Accept, Undo, and the trajectory of each goal. It never uses the words pull request, merge, diff, review, branch or commit. **Engineering view**, one action away: runs, evidence bundles, decision cards, pull requests, the builder's and reviewer's opinions. A developer can take the wheel on a ticket or set the dial to guided, while the factory builds the rest.

## The pipeline and its tasks

Durable state lives in the git host and the orchestrator's store. A run never waits for a person. It ends, and the next event starts the next run. A pipeline is a chain of triggers, not a workflow engine.

| Task | Started by | Writes | Passes when |
|---|---|---|---|
| `onboard` | A repository is added | A change inside `.factory/` only, with a readiness report | Its review agrees. It takes effect after Accept |
| `brainstorm` | A goal is labelled, or the person answers | Questions, or a spec with numbered criteria | Its review agrees and the person approves it |
| `decompose` | The spec is approved | `issues.json`: at most twelve tickets, each naming the criteria it serves | Its review agrees |
| `acceptance` | The plan has passed | One check per criterion and a manifest, under the acceptance path only | Its review agrees |
| `implement` | A ticket is ready | One change | The machine gate |
| `review` | A spec, plan, set of checks or change is ready for review | `review.json` | Never gated. It is the opinion others wait for |
| `fix-ci` | A required check on the git host failed on an open agent pull request | A fix commit on that pull request's branch | The machine gate, again |

At dial level 1 a person also approves each output before it is used or combined (see The dial).

- A ticket becomes `factory:ready` when the acceptance checks of its goal are in staging, every ticket it depends on is in staging, and the cap on open agent pull requests (`maxOpenAgentPullRequests`) allows it. Otherwise it stays `factory:planned` and the web app shows why.
- `brainstorm` asks a few questions per round, at most three rounds, then writes the spec with its open points listed.
- The spec, the plan and the acceptance checks each pass an independent review before any building starts. A weak plan is the cheapest place to stop a bad course.
- An approved spec is combined into staging as a change, so it reaches the default branch with the work on Accept.
- When both harnesses are installed, `review` uses the one that did not write the subject.
- A fix run's own failure triggers nothing. Pull requests from forks are not reviewed in the first release.

## A run and its three stages

1. **Work.** The worker gets a checkout of the current staging state on a new branch (for `review`, of its subject), the input files and a prompt. It has no git host credential. It edits files and commits locally. The orchestrator squashes its commits into one.
2. **Verify.** The orchestrator, outside the worker and with no model access, computes the change, checks protected paths, runs the task's checks and the acceptance checks on a clean checkout of the commit, and builds the evidence bundle.
3. **Publish.** The publish step performs the actions the output files ask for, from a fixed list (see Credentials).

A run never asks a person. Each run has a **permission policy**: the commands it may run (`spec.permissions.commands`) and the paths it may write, which are the worktree only. The adapter turns it into the harness's own settings. A request outside the policy is denied and recorded. A harness is never started in a mode that asks a person, and never in a mode that skips all permission checks.

The **unattended contract** is a fixed text placed before any method: you run unattended; never ask for confirmation; where your method says to ask a person, if the choice can be undone and is inside the ticket, decide and record a ruling; otherwise write the question to the result and stop. **Bring your own method**: `spec.method` is `builtin` or a prompt file in the repository. The factory judges the result contract, not the route.

**Take the wheel.** A person can do the work stage in their own session. `oxf` serves a Model Context Protocol endpoint with four tools, a public contract: `factory_list` (ready work), `factory_claim` (claims a ticket, makes the worktree, returns the brief and paths), `factory_ask` (posts a question through the publish step), `factory_submit` (ends the work stage). Verify, the machine gate and publish are unchanged.

**Claims across machines**: a run claims its ticket with the label `factory:claimed` and a marker comment with its machine id and run id, then reads the comments back. The earliest marker wins. The other run ends with no change.

**Triggers** act only on events from a user with write access, or from the factory itself. Every comment the factory writes carries a hidden marker with the run id and never triggers a run. An answer or approval given in the web app is posted with an answer marker and counts as the person's. Kinds: `issueLabeled`, `issueCommented`, `pullRequestOpened`, `pullRequestMerged`, `checkFailed`, `schedule`. On one machine triggers are polled, so no public endpoint is needed.

## Credentials

The worker never holds any credential for the git host. The publish step holds two keys, kept apart.

| Key | Granted | Used for | Never used for |
|---|---|---|---|
| Propose token | At setup, required | Push branches under `factory/`; write `factory-staging` through the combine action; open and update draft pull requests and the one staging pull request; comment; add or remove labels from a fixed list; create at most twelve issues per run as sub-issues of the goal; set a commit status | Any write to the default branch |
| Accept token | Separately by the person, optional | Merge staging into the default branch on a person's Accept in the web app; merge a revert on Undo after Accept; an earned automatic Accept | Anything else |

Without the accept token, the factory keeps a pull request open from staging to the default branch and the engineering view says that a developer must merge it. The outcome view says "ask a developer to finish this step". The git host must enforce the split. On GitHub a token that can push a branch can also merge, so the propose token's user must be one that branch protection does not allow to update the default branch. The factory checks the protection rules at start and shows a standing warning when they are missing.

## The worker contract

The same for every runner and every harness. Paths are shown as in a container. The local runner gives directories in their place, named in `FACTORY_IN` and `FACTORY_OUT`.

**In**

| Path or variable | Content |
|---|---|
| `/workspace` | A checkout of the staging state on a new branch. Read-write. No remote credential |
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
- A **ruling** is written when the method would have asked a person, the choice can be undone and it is inside the ticket. Otherwise the worker writes a question and ends with `needs_human`. Questions go to the inbox. Nothing waits on them.
- `usage.billing` is `metered` (an API key or gateway: dollars and tokens), `plan` (a subscription login: tokens, no dollar figure) or `unknown`. `costUSD` is given only for metered usage. No dollar figure is ever estimated for plan usage. `usage.source` and `usage.enforced` are written by the orchestrator; values from the worker are ignored.

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

`/factory/out/events.jsonl`: one JSON object per line for the live view, of kind `message`, `tool`, `status` or `denied`. Never a secret. Also: commits on the branch in `/workspace`, and the raw harness log on standard output and error.

**Harness adapters** are thin Go code that build the command line for one program (`claude -p --output-format stream-json`, `codex exec --json`) and read its event stream. These flags were seen in Claude Code 2.1.326 and Codex CLI 0.170.0 on 2026-11-26, not run under this contract. **A stub harness** makes fixed edits from a script, for tests and demo mode. No paid model calls in tests.

## Acceptance checks

The answers to "how will you know it worked?" become the spec's numbered acceptance criteria. The `acceptance` task runs after the plan passes and before any building. It writes one executable check per criterion and a manifest, under the acceptance path, `acceptance/<goal number>/` by default (`acceptancePath` in `factory.yaml`):

```json
{ "schemaVersion": 1, "goal": 40, "criteria": [
  { "id": 1, "text": "A customer can move a booking to another day", "run": "go test ./acceptance/40 -run TestMoveBooking" },
  { "id": 5, "text": "Works on a phone", "run": "", "reason": "No phone browser is available to the checks" } ] }
```

- The acceptance path is protected for every task except `acceptance`, and `acceptance` may write nothing else. The builder cannot change the manifest or the check files.
- The manifest is not configuration. The orchestrator reads it from the staging state under test. The protected-path rule is what keeps a change from altering it.
- The checks pass their own independent review, which compares each check with the person's wording, before they are combined into staging.
- The orchestrator runs each command in the verify stage of every change and on every staging rebuild. A check that already passes before any building is flagged in the engineering view.
- A check may be changed only by a new `acceptance` run that passes its own review. When a builder or reviewer says a check is wrong, that is recorded, and the orchestrator may start such a run. `implement` never changes a check.

The state of each criterion is computed only by the orchestrator, from the checks:

| State | Meaning |
|---|---|
| Working | Its check passes |
| Not working yet | Its check fails |
| Could not confirm | No check exists for it, or `acceptance` reported that it could not write one. Never shown as working |

## The machine gate and risk

A change from `implement` or `fix-ci` passes the machine gate when all three hold:

1. **Verify passes**: no protected path was touched, the task's checks pass, and no acceptance check that is working on staging fails on the change.
2. **An independent review agrees**: `review.json` with subject `change` and verdict `agrees`.
3. **Risk is not high**, by the rules below.

If verify fails or the review does not agree, the failure or the review points go back to the builder as a new attempt. `maxAttempts` counts work-stage starts per ticket, from `implement`, `fix-ci` and take the wheel. The default is 3: one first try and two rework rounds. When they are used up, the ticket is an exception. A rework commit, or a `fix-ci` commit, makes earlier verify and review results stale, and the change goes through the gate again. A review by the same harness as the builder may pass a change, but the change is marked **single-opinion**. Single-opinion changes never count toward automatic Accept.

**Risk** is rule based. No model sets it. It is computed after the review. The rules are tried in order and the first match wins.

| Order | Risk | When |
|---|---|---|
| 1 | High | A contract, security, dependency or CI path from `riskPaths` changed. Or an existing test file was changed or deleted. Or the review verdict is `disagrees` |
| 2 | Low | Only paths listed as documents changed |
| 3 | Low | The review agrees and came from a different harness than the builder, every criterion the ticket serves is working with the change, every changed file is covered by a claim, and the change is under a size limit from the settings |
| 4 | Medium | Everything else. This includes a ticket that serves no criterion, a criterion that is not working yet or could not be confirmed, and a review by the same harness as the builder |

New test files do not raise the risk. Changing or deleting an existing test does, because that is how a change makes itself pass. A change to a protected path fails verify before risk is computed.

High risk is always an exception. The person is asked about its consequence in plain words. A yes combines the change, recorded as passed by a person. A no takes it back to its ticket. `onboard`, `brainstorm`, `decompose` and `acceptance` have no risk level. Their gate is their path rule and an independent review that agrees.

**The decision card** is the engineering view of one change, computed by the orchestrator from structured inputs, never written by a model: changed paths under `riskPaths` and changed test files; the rulings, each with an overturn action; what is not confirmed (files no claim covers, criteria that could not be confirmed, opinions that differ from the computed state, policy denials); the review verdict and points; the risk with its reasons.

## Staging, Accept and Undo

There is one staging branch per repository, `factory-staging`. It is not under the `factory/` prefix. Only the publish step's **combine** action writes it. `fix-ci`, agent pull requests and the agent-branch rule do not apply to it.

**Staging is rebuilt, never patched.** Staging is the current default branch plus each change that passed, applied in the order it passed, one commit per change. On any event (a change passes, a change is undone, the default branch moves), the orchestrator rebuilds staging from the default branch and runs the task checks and the acceptance checks on the result.

- A change that no longer applies cleanly, or whose addition makes a task check fail or makes a working acceptance check fail, is taken out. The orchestrator finds the first such change in order, takes it out and rebuilds again. The change goes back to its ticket: if the ticket has attempts left it is built again on the new staging; if not, it is an exception.
- A change already contained in the default branch drops out of staging.
- If the default branch alone fails a task check, staging stops at the default branch. The outcome view says the project's own checks are failing and a developer is needed.
- Each staging state is recorded: its commit, the default branch commit it was built on, its changes in order, and the state of every criterion.
- With several machines, one machine holds the staging claim for a repository, taken by the marker-comment rule on the staging pull request. Only that machine combines, rebuilds and accepts.

**Agent pull requests.** Each change has a branch `factory/<run id>` and a draft pull request into `factory-staging`, labelled `factory:agent`. It is the engineering trace and the place where `fix-ci` works. When the change is combined, the publish step closes the pull request with a comment naming its commit on staging.

**Accept** takes the whole of staging: the exact state the person saw, named by its commit. If staging has been rebuilt since, or its branch head differs from the recorded state, Accept is refused and the person sees the current state. With the accept token, the publish step merges the staging pull request into the default branch with a merge commit, so each change keeps its own commit and outcomes are attributed per change. Without it, the person's Accept is recorded, a developer merges that pull request, and no new change is combined until the default branch contains the accepted commit. If the git host refuses the merge, that is an exception.

**Undo** before Accept removes the change and rebuilds staging. Undo after Accept is a revert commit on the default branch, merged with the accept token, then a rebuild. Without the accept token the factory opens the revert as a pull request for a developer. Every change must be cheap to take back.

## The dial

One level per task sets where the factory stops for a person.

| Level | Name | A person approves |
|---|---|---|
| 0 | Off | The task does not run |
| 1 | Guided | The spec, the plan, and each change before it is combined. This is the engineering profile |
| 2 | Autopilot | Only the spec, and Accept. The default |
| 3 | Automatic Accept | Nothing, for classes of change that earned it. Shown, not active, in the first release |

- A task's level applies to its own output: `brainstorm` makes the spec, `decompose` the plan, the other tasks make changes. Exceptions reach a person at every level.
- `spec.autonomy.ceiling` in the task file sets the highest level. It changes only through the default branch.
- The current level is stored by the orchestrator and moved in the web app at or below the ceiling. Every change is logged with time and the previous level. On OpenAgentix, platform policy supplies the ceiling and can only lower it.

## Trust and the attention record

Trust comes from outcomes after Accept, never from the act of accepting, and never from opening a card. An outcome is recorded per change, by its commit, inside a window:

- **Bad**: the change is reverted, a later fix names it, or the default branch turns red on the Accept merge commit and stays red when run again. A red build counts against every change in that Accept.
- **Good**: none of that happened, and the change was exercised: a check on the default branch ran tests that cover the changed files, as far as the project's test tool can tell, or later the change was deployed.
- **Silent**: otherwise. Silence counts for nothing.

A documents-only change has nothing to exercise. For that kind alone, good means no revert and no correcting change inside the window: weaker evidence, for less at stake.

A **class of change** is the task, the risk level, and the most serious kind of path touched, in this order: security, contract, dependencies, CI, code, tests, documents. The orchestrator assigns it from the change. A class is **eligible** for automatic Accept when it is low risk and, inside the window, has at least a set number of good outcomes, no bad outcome, and every review from a different harness than the builder. The count and the window are settings with no default. The factory proposes an eligible class and a person switches it on. Every switch is logged. A bad outcome switches the class off, and it needs a fresh full count. In the first release eligibility is computed and shown only.

The **attention record**, per task: touches per accepted goal, questions asked, rulings made, rulings overturned. A ruling is overturned when a person marks it so in the engineering view. Many overturned rulings say turn the dial down.

## Trajectory

Each goal has a course, computed by rules, never by a model. The thresholds are settings. These starting values are untested.

- **Off course** when any of these happens: a ticket uses all its attempts; the same criterion stops working on two rebuilds, counted even when the change that caused it was taken out; two changes of the goal are undone.
- **Drifting** when a criterion stopped working on one rebuild, or one change of the goal was undone, or no criterion of the goal became working over its last three changes. **On course** otherwise.

The **last good point** is the most recent staging state in which every criterion that had ever been working was working. It never goes back past accepted work: if that state is older than the last Accept, the last good point is the default branch. Drifting: the factory keeps working and says so in the outcome view. Off course: it stops building on that goal, rebuilds staging at the last good point, closes the goal's open tickets, and runs `decompose` again from there with the failure record as input. It asks one plain question only when the cause is unclear intent.

## The run record and the evidence bundle

The **run record** is stored in SQLite. Its field names follow the planned OpenAgentix `AgentRun`, so that kind can back it later: `id`, `task`, `attempt` (work-stage starts on the ticket), `goal`, `ticket`, `trigger` (kind and reference), `mode` (`unattended` or `wheel`), `input` (resolved task and prompt hash), `limits`, `phase`, `result`, `usage` (with its source), `provenance` (the evidence bundle), `outputs` (branch, commit, pull request), `createdAt`, `startedAt`, `finishedAt`. The phases are `queued`, `held`, `working`, `verifying`, `publishing`, `succeeded`, `failed`, `needs_human` and `cancelled`.

The orchestrator also stores each staging state, the dial levels and their log, and the outcome of each accepted change. The trust record is a query over these and outcomes read from the git host, never a second source of truth.

The **evidence bundle** is built by the orchestrator in the verify stage. The review, risk and gate fields are added when the review ends.

```json
{
  "schemaVersion": 1,
  "run": "r_01H8Z", "task": "implement", "goal": 40, "ticket": 42, "attempt": 1, "mode": "unattended",
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
  "acceptance": [ { "id": 1, "state": "working", "before": "not_working" } ],
  "review": { "run": "r_01H9A", "harness": "codex", "verdict": "agrees", "singleOpinion": false },
  "risk": { "level": "low", "reasons": ["review agrees from the other harness", "served criteria working"] },
  "gate": "passed",
  "usage": { "billing": "plan", "inputTokens": 12000, "outputTokens": 3400, "source": "harness", "enforced": false },
  "limits": { "deadline": "30m", "maxCostUSD": 5 }
}
```

`acceptance[].state` and `before` are `working`, `not_working` or `could_not_confirm`. `gate` is `pending`, `passed`, `failed` or `exception`. `usage.enforced` is true only when a governance provider enforced the limit; otherwise the web app says "reported, not enforced". A run with no change still records worker, usage, limits and outputs, with diff, checks and risk empty. The agent pull request body has three parts: Summary, Evidence, Risk.

## Interfaces that vary by where it runs

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
| Local, first release | A process in a fresh git worktree | The person's own login, usage as reported |
| Container, later | Docker, with a worker image built on the person's machine | An API key |
| Kubernetes, later | A Job | Metered model access |
| OpenAgentix, later | A Job, or platform runs when `AgentRun` and `Workspace` exist | A gateway key per run, an enforced budget, an audit record |

**Local runner rules**, and others that vary by place:

- It starts the official `claude` or `codex` program in place. It never reads, copies or forwards a login token.
- It removes git host credentials from the process environment where it can: no token variables, no credential helper, no agent socket. This is best effort, because the home directory is still there.
- It uses the strictest sandbox setting of the harness that still lets the task run. A deadline always applies, and the process group is killed at it. Runs are safe to repeat. A run cut off by sleep or a restart is marked failed and may start again.
- A cost limit applies only to metered usage. The orchestrator reads usage from the event stream and cancels the run past the limit. This lags, so the web app says "best effort".
- **Toolchain.** Locally the stages use the compilers on the machine. The container runner names a base image in `factory.yaml` (`image:`). Outbound network is open in both stages.
- **Demo mode.** A fake git host with a seeded repository and a scripted story, a fake runner and the stub harness. No login, no token. Every screen says it is a demo.
- **Listening.** On a laptop, localhost. On a dedicated machine `oxf` runs as a service and may listen on the local network behind a login token.

## Configuration

In the repository under `.factory/`: `factory.yaml`, `tasks/*.yaml`, `prompts/*.md`. The orchestrator reads `.factory/` from the default branch only, so a change to it takes effect only after Accept. `.factory/` is protected for every task except `onboard`, which may change nothing else. Unknown fields are an error. The schema version is in `apiVersion`.

```yaml
apiVersion: factory.openagentix.org/v1alpha1
kind: Factory
metadata: { name: main }
spec:
  repository: acme/widgets
  defaultWorker: { harness: claude-code }
  maxOpenAgentPullRequests: 3     # changes open and not yet combined
  pollInterval: 1m
  acceptancePath: acceptance
  start: "make run"               # optional, lets Try it start the application
  riskPaths:
    contract: ["api/**"]
    security: ["internal/auth/**"]
    dependencies: ["go.mod", "go.sum"]
    ci: [".github/**"]
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
  method: builtin
  prompt: prompts/implement.md
  permissions:
    commands: ["go build ./...", "go test ./..."]
  protectedPaths:                 # added to .factory/** and the acceptance path
    - ".github/**"
  checks:
    - { name: test, run: "go test ./..." }
  limits: { deadline: 30m, maxCostUSD: 5, maxAttempts: 3 }   # cost: metered only; attempts: work-stage starts per ticket
  autonomy: { ceiling: 2 }
  outputs: { pullRequest: { draft: true }, comment: true }
  concurrencyKey: "issue-{{ .Number }}"
```

Machine settings (installed harnesses, the two tokens, the listen address, thresholds) live in the person's home directory, never in the repository. Secrets come from the environment or a file, never from a task file, and are never written to the database.

## Stack

Go, one binary, with the web app embedded through `go:embed`. SQLite through a pure Go driver, so no C toolchain is needed. The web app is Svelte 5 with runes, SvelteKit as a single-page app, Tailwind 4 with theme tokens, no component kit, a strict content security policy, and live events by server-sent events. The container runner's worker image is built on the person's machine, so the Claude Code binary is never shipped in a public image. Dependencies are MIT or Apache-2.0 only.

## Name

OpenAgentix Software Factory, decided by the owner. The site and the API group are `factory.openagentix.org`, a domain the owner holds. The command `oxf` is a working name. Tagline: "Runs on your laptop. Governed on OpenAgentix." The group is the product's own, separate from the platform group `openagentix.io`, as OpenAgentix ADR 0012 requires. The risk of one brand is that the name suggests the platform is required; the tagline and a quick start with no platform must answer that. No trademark search was done. The name and group live in one Go constant and one web constant.

## Not settled

Left to the first real runs, and not designed further here:

- Every threshold: the trajectory values above, the trust count and window, the size limit in the risk rules.
- The exact harness flags that give a no-prompt permission policy.
- How well "exercised" can be computed.
- How staging rebuilds behave on a large repository, in time and in checks run, and how the staging claim passes between machines.
- Whether subscription terms allow unattended runs started by another program. If they do not, unattended runs need an API key, and take the wheel remains for subscription users.

## Limits

- Nothing here has been run. Every rule is unmeasured, including its false alarms and misses. The whole weight is on the factory's own checking, the least proven part of this field. "Working" is only as strong as its check. A check can pass for the wrong reason, and a builder can change the code a check calls.
- Local mode has no container. The agent runs as the person, on their machine. Removing credentials from its environment is best effort.
- Usage is what the harness reports. Nothing is enforced locally. Outbound network is open.
- A review can be steered by text in the change. With one harness installed it shares the builder's blind spots.
- The agent chooses which files it touches, so it can keep a change in a low-risk class. `riskPaths` and the size limit reduce this. They do not remove it.
- With one maintainer, the same person approves the spec, accepts and switches a class on. The record is honest, but not independent.
- One staging branch means goals in flight are accepted together. A person who wants only part uses Undo first.
- A red build after Accept counts against every change in that Accept, which is coarse. Try it needs a start command from the project. Otherwise the outcome view shows the acceptance list alone.
- The accept token is a stronger credential than proposing needs. It is optional and held by the publish step only.
- Two harnesses from the start keep the contract honest, at the cost of two adapters. Polling adds delay before a run starts.

## History

This record was consolidated on 2026-11-26, after three independent reviews in a row found contradictions between decisions that had been stacked on each other during one day. Two changes were merged unread on the first day, which showed that a merge button is one more "yes" and led to the design for a person who will not read a change. Each time, an independent reviewer found the real problems before a person did, which led to the machine gate and the acceptance checks.
