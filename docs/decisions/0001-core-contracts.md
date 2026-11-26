# 0001. Core contracts for the single-machine factory

- Status: proposed, 2026-11-26. Owner approved the scope and positioning in session. The contracts below are not proven by a run yet.
- Scope: the local product (a laptop or a dedicated machine, using the Claude Code and Codex already installed there). Every contract is shaped so a container runner, Kubernetes and OpenAgentix need no schema change.
- Decision 12 replaces the dial levels of Decision 11: every run is unattended and a person decides at gates.
- Revised the same day: Decision 11 makes the local runner the default and adds co-pilot mode and the autonomy dial. Where an earlier decision speaks of a container, read "work stage", and see Decision 11 for the local form.
- The product is named OpenAgentix Software Factory. "AgentFleet" was the first working name. See "Name".

## Context

Free tools already turn an issue into a pull request. Published studies and practitioner reports from 2026 point to where teams hurt now:

- Review capacity is the limit. Agents raise the number of pull requests faster than the number of reviewers.
- Agents edit tests to pass them.
- Pull requests whose text does not match the diff are accepted less often and take longer to merge.
- Cost is not visible per outcome.
- No open-source product gives a basis for deciding how much to trust an agent on a given repository.

These points come from reading other people's reports, not from our own runs. This record quotes no numbers for that reason.

## Decision 1: positioning

**The software factory that earns its autonomy.**

Every run leaves evidence. Every merge, rework and revert moves a trust record. Autonomy is opened per repository and task class, by a person, on that record.

We do not claim a dark factory. We claim the autonomy level a run has proven.

Five product ideas follow from this. Each has a version in v0.1 or a planned release.

| Idea | v0.1 | Later |
|---|---|---|
| Evidence bundle per run | Yes | Signed provenance on OpenAgentix |
| Tamper-proof test gate | Protected paths, checks run outside the worker | Holdout scenarios the worker never sees |
| Autonomy dial | One per task, moved by a person | Ceiling set and enforced by OpenAgentix policy |
| Trust ledger | Read-only numbers beside the dial | Evidence that can unlock a higher dial level |
| Review-capacity brake | A cap on open agent pull requests | Queueing and priorities |
| Learning loop | No | A revert or repeated review comment becomes a proposed check (v0.2) |

## Decision 2: vocabulary

Task, trigger, run, runner, worker, ticket tracker, git host and orchestrator are used as in Matt Pocock's software-factory skill (MIT), with thanks. A run is one execution of a task, from trigger to finish. New terms:

- **Harness**: the coding agent program a worker launches. Claude Code and Codex CLI in v0.1.
- **Evidence bundle**: the record of what a run did and what was checked, built by the orchestrator, not by the worker.
- **Publish step**: the step that pushes a branch and writes to the git host. It runs outside the worker.
- **Trust ledger**: outcome counts per repository, task and harness.

## Decision 3: a run is never handed a write credential

A run has three stages. Only the first runs agent code.

1. **Work.** The worker gets the repository checkout and the prompt. The factory gives it **no git host credential**. It edits files and commits locally. In a container that is a hard fact. In local mode it is best effort, see Decision 11.
2. **Verify.** The orchestrator, outside the worker, computes the diff, checks protected paths, and runs the task's checks on a clean checkout of the commit, with no model access.
3. **Publish.** The orchestrator pushes the branch and opens or updates a draft pull request, with a token scoped to read and propose. It never merges.

This is the "safe outputs" pattern of GitHub Agentic Workflows and the rule in OpenAgentix ADR 0012. Ticket text can carry a prompt attack. With this split, an attacked worker has no token to steal and cannot write to the git host by itself.

A run never waits for a person. It ends. Questions go to the ticket as a comment, and the person's reply triggers a new run.

## Decision 4: task file

One YAML file per task. The same file must run on all three rungs.

```yaml
apiVersion: factory.openagentix.org/v1alpha1
kind: Task
metadata:
  name: implement
spec:
  description: Turn a ready ticket into a draft pull request.
  trigger:
    issueLabeled: { label: "factory:ready" }  # one of: issueLabeled, issueCommented, pullRequestOpened, pullRequestMerged, checkFailed, schedule
    throttle: { maxPerDay: 5 }
  worker:
    harness: claude-code                # claude-code | codex | stub (tests and demo)
    model: ""                           # empty means the harness default
  prompt: prompts/implement.md
  protectedPaths:                       # the worker may not change these
    - "**/*_test.go"
    - ".github/**"
  checks:                               # run by the orchestrator in the verify stage
    - { name: test, run: "go test ./..." }
  limits:
    deadline: 30m
    maxCostUSD: 5
    maxAttempts: 2                      # per ticket or pull request, then a person is asked
  outputs:
    pullRequest: { draft: true }
    comment: true
  concurrencyKey: "issue-{{ .Number }}" # stops two runs doing the same work
```

A `Factory` file names the repository, the default worker, and the review cap (`maxOpenAgentPullRequests`). When the cap is reached, triggers that would open a new pull request are held and shown as held.

The five questions from Matt Pocock's software-factory skill map onto the file: what triggers it (`trigger`), what it reads and writes (`outputs`, `protectedPaths`), what it triggers next (the labels and pull request events its outputs cause), where a person re-enters (`maxAttempts`, draft pull request), what stops double work (`concurrencyKey`).

Unknown fields are an error. The schema version is in `apiVersion`.

## Decision 5: worker contract

The same for every runner and every harness.

**In**

| Path or variable | Content |
|---|---|
| `/workspace` | Repository checkout on a new branch. Read-write. No remote credential |
| `/factory/in/task.json` | The resolved task (read-only) |
| `/factory/in/prompt.md` | The task prompt with trigger data rendered in. Ticket text is marked as untrusted |
| `/factory/in/trigger.json` | The trigger payload |
| `/factory/out/` | Empty, writable |
| `FACTORY_MODEL_BASE_URL`, `FACTORY_MODEL_API_KEY`, `FACTORY_MODEL` | Model access. On rung 3 these point at the gateway |
| `FACTORY_DEADLINE_SECONDS`, `FACTORY_MAX_COST_USD` | Limits |

**Run**: one container, no terminal, no interaction. The image entrypoint is `factory-worker`. It launches the harness, then writes the result. The orchestrator kills the container at the deadline.

**Out**

- `/factory/out/result.json`, fixed schema:

```json
{
  "schemaVersion": 1,
  "status": "succeeded | failed | needs_human | no_change",
  "summary": "what was done, in plain words",
  "claims": ["Added retry to fetch()", "Added a test for timeout"],
  "questions": [],
  "usage": { "billing": "metered | plan | unknown", "costUSD": 0.42, "inputTokens": 12000, "outputTokens": 3400 },
  "harness": { "name": "claude-code", "version": "2.1.326", "model": "..." }
}
```

- `/factory/out/events.jsonl`: one JSON object per line, for the live view. Kinds: `message`, `tool`, `status`. Never a secret.
- Commits on the branch in `/workspace`.
- Standard output and error: raw harness log.

A missing or invalid result file means the run failed. The orchestrator never parses free text to decide an outcome.

**Harness adapters** are thin. Each is a script in the worker image that maps the contract to one CLI: `claude -p --output-format stream-json` or `codex exec --json`. No harness logic lives in the orchestrator. Flags are confirmed present in Claude Code 2.1.326 and Codex CLI 0.170.0 on 2026-11-26. Behaviour in a container is not proven yet.

**A stub harness** reads a script and makes fixed edits. Tests and demo mode use it. No paid model calls in tests.

## Decision 6: evidence bundle

Built by the orchestrator in the verify stage. Stored with the run and rendered into the pull request body and the UI.

```json
{
  "schemaVersion": 1,
  "run": "r_01H...", "task": "implement", "trigger": { "kind": "issue", "ref": "#42" },
  "worker": { "harness": "claude-code", "version": "...", "model": "..." },
  "diff": { "files": 3, "added": 84, "removed": 12, "paths": ["..."] },
  "claims": ["..."],
  "protectedPaths": { "touched": [], "ok": true },
  "testFilesChanged": ["internal/x/x_test.go"],
  "checks": [ { "name": "test", "exitCode": 0, "durationSeconds": 41, "ranBy": "orchestrator" } ],
  "usage": { "billing": "plan", "inputTokens": 12000, "outputTokens": 3400, "enforced": false },
  "mode": "unattended | wheel",
  "limits": { "deadline": "30m", "maxCostUSD": 5 },
  "risk": { "level": "low | medium | high", "reasons": ["test files changed"] }
}
```

Rules:

- A change to a protected path fails the run.
- A change to any test file raises the risk level and is shown first to the reviewer.
- `usage.enforced` is `false` on rungs 1 and 2. The UI says "reported, not enforced". It is `true` only when a governance provider enforced the limit.
- Risk in v0.1 is rule based: test files changed, protected area near, diff size, new dependency files. No model judges risk.

The pull request body has three parts, after Matt Pocock's `pr` skill: Summary, Evidence, Merge danger.

## Decision 7: run record

Stored in SQLite on rung 1. Field names follow the planned OpenAgentix `AgentRun` so that kind can back the record later.

| Field | Note |
|---|---|
| `id`, `task`, `attempt` | |
| `trigger` | Kind and reference |
| `input` | Resolved task and prompt hash |
| `limits` | Deadline, max cost |
| `phase` | `queued`, `held`, `working`, `verifying`, `publishing`, `succeeded`, `failed`, `needs_human`, `cancelled` |
| `result` | The worker result |
| `usage` | Cost and tokens, with source |
| `provenance` | The evidence bundle |
| `outputs` | Branch, commit, pull request reference |
| `createdAt`, `startedAt`, `finishedAt` | |

The trust ledger is a query over run records plus pull request outcomes read from the git host (merged, closed, reverted). It is never a second source of truth.

## Decision 8: interfaces that vary by rung

```go
// Runner starts one disposable container per stage.
type Runner interface {
    Start(ctx context.Context, spec ContainerSpec) (Handle, error)
}
type Handle interface {
    Logs(ctx context.Context) (io.ReadCloser, error)
    Wait(ctx context.Context) (exitCode int, err error)
    Cancel(ctx context.Context) error
}

// Governance decides model access and records use.
type Governance interface {
    ModelAccess(ctx context.Context, run RunRef) (ModelAccess, error) // endpoint, key, model
    Enforced() bool                                                   // false on rungs 1 and 2
    Record(ctx context.Context, run RunRef, usage Usage) error
}

// GitHost is the ticket tracker and git host.
type GitHost interface { /* poll events, read ticket, push branch, open draft PR, comment, read PR outcome */ }
```

- Local (v0.1): local runner, a process in a git worktree. Local governance: the person's own login, usage as reported.
- Container (v0.2): Docker runner with an API key.
- Kubernetes: Job runner. Metered model access.
- OpenAgentix: Job runner, or platform runs when `AgentRun` and `Workspace` exist. A gateway key, an enforced budget, an audit record.

Triggers on rung 1 are polled. No public endpoint is needed.

## Decision 9: stack

- Orchestrator: Go, one binary, with the web app embedded (`go:embed`). SQLite through a pure Go driver, so no C toolchain is needed.
- Web app: the OpenAgentix console stack. Svelte 5 with runes, SvelteKit as a single-page app, Tailwind 4 with theme tokens, no component kit, strict content security policy. Live run events use server-sent events, which the console does not have yet.
- Harness adapters: Go code that builds the command line for the installed `claude` and `codex` and reads their event streams. A stub harness for tests and demo mode.
- Worker image, for the container runner in v0.2: built on the user's machine from a base image, so the Claude Code binary is never shipped in a public image.
- Licences: MIT and Apache-2.0 dependencies only.

## Decision 10: tasks that do not end in a code change

Added after the design review on 2026-11-26. `brainstorm`, `decompose` and `review` produce no code diff. `onboard` and `fix-ci` have special write rules.

**Triggers.** Two kinds are added: `issueCommented` (with a required issue label) and `pullRequestMerged` (with a path filter). Every trigger acts only on events from a user with write access to the repository. Every comment the factory writes carries a hidden marker with the run id. A comment with that marker never triggers a run. An answer typed in the web app is posted with an answer marker and counts as the person's.

**Outputs.** A worker never calls the git host. It writes files to `/factory/out/` and the publish step acts on them:

| File | Meaning | Publish action |
|---|---|---|
| `result.json` `questions` | Questions for a person | One comment on the trigger issue |
| commits in `/workspace` | A code or document change | Push a branch named `factory/<run id>`, open a draft pull request |
| `issues.json` | Ticket proposals from `decompose`: title, body, acceptance criteria, `dependsOn` by index | Create at most twelve issues as sub-issues of the goal, label `factory:planned`, write dependencies into each body in a fixed, machine-read block |
| `review.md` | A review from `review` | One comment on the pull request |

`issues.json` and `review.md` are part of the worker contract and are parsed strictly.

**Agent pull requests** are known by the branch prefix `factory/` and the label `factory:agent`. `fix-ci` may push only to such a branch. Its input includes the failed check's log, cut to a fixed size. `review` gets a checkout of the pull request head. Pull requests from forks are not reviewed in v0.1.

**Configuration.** The orchestrator reads `.factory/` from the default branch only, so a change in a pull request has no effect until a person merges it. `.factory/` is a protected path for every task except `onboard`, whose only allowed change is inside `.factory/`.

**Evidence without a diff.** The bundle is still written: worker, usage, limits, outputs created. Diff, checks and risk are empty and risk is not shown.

**Fields the worker cannot set.** `usage.enforced` and `usage.source` are written by the orchestrator. Values from the worker are ignored.

**Run links.** A run record carries the goal issue and the ticket issue it belongs to, so cost per goal can be queried.

**Cost limit.** On rung 1 the orchestrator reads usage from the event stream and cancels the run when the limit is passed. This lags, so the limit can be overshot. Where a harness has its own budget flag, the adapter sets it too. The web app says "best effort".

**Toolchain.** The work stage and the verify stage need the project's compilers. `factory.yaml` names a base image (`image:`). The default is a general image with Node, Python and Go. `oxf` builds a local worker image from the base by adding the worker entrypoint, the adapters and the harness programs. Building on the user's machine also avoids shipping the Claude Code binary in a public image. `onboard` detects the stack and proposes the image and the checks. Outbound network is open in both stages in v0.1, so dependencies can download. This is a stated limit.

**Demo mode.** A fake git host with a seeded repository and a scripted story, a fake runner and the stub harness. No Docker, no key, no token. The web app labels it as a demo on every screen.

## Decision 11: local runner, co-pilot mode and the autonomy dial

Added on 2026-11-26 after the owner chose a local-first product. People keep the Claude Code and Codex they already have, with the logins they already pay for.

**Local runner.** The default in v0.1. The worker contract of Decision 5 holds, with directories in place of mounts:

| Contract | Container | Local |
|---|---|---|
| `/workspace` | A mount | A fresh git worktree under the factory's work directory |
| `/factory/in`, `/factory/out` | Mounts | Directories beside the worktree. Their paths are given in `FACTORY_IN` and `FACTORY_OUT` |
| Model access | Endpoint and key in the environment | None given. The harness uses the person's own login |
| Entry point | `factory-worker` in the image | The harness adapter starts the installed `claude` or `codex` |

Rules for the local runner:

- It starts the official program in place. It never reads, copies or forwards a login token.
- It starts the process with git host credentials removed from the environment where it can: no token variables, no credential helper, no agent socket. This is best effort, because the person's home directory is still there.
- It uses the strictest sandbox and permission setting of the harness that still lets the task run. Never a "skip all permissions" flag on a person's own machine.
- A deadline always applies. The process group is killed at the deadline.
- Runs are safe to repeat. A run cut off by sleep or a restart is marked failed and may be started again.

**Co-pilot mode.** A run whose work stage is done by a person's own session. `oxf` serves a Model Context Protocol endpoint with four tools:

| Tool | Does |
|---|---|
| `factory_list` | Lists work that is ready for a co-pilot |
| `factory_claim` | Claims a ticket, creates the worktree and branch, returns the brief and the paths |
| `factory_ask` | Posts a question to the ticket through the publish step |
| `factory_submit` | Takes the summary and claims, ends the work stage, starts verify and publish |

The tool names and their inputs are a public contract. Verify, evidence and publish are identical to autopilot. The evidence bundle records `mode`.

**Autonomy dial.** (The levels in this paragraph are replaced by Decision 12.) One level per task: 0 off, 1 co-pilot, 2 autopilot with human review, 3 autopilot with merge. Level 3 is locked in v0.1.

- `spec.autonomy.ceiling` in the task file sets the highest level allowed. It changes by pull request.
- The current level is stored by the orchestrator and moved in the web app, at or below the ceiling. Every change is logged with time and the previous level.
- A task at level 1 never starts a process. A task at level 2 starts runs and also accepts a co-pilot claim.
- On OpenAgentix the platform policy supplies the ceiling. It can only lower it.

**Usage.** `usage.billing` is one of `metered` (an API key or gateway: dollars and tokens), `plan` (a subscription login: tokens, no dollar figure) or `unknown`. A cost limit applies only to metered usage. No dollar figure is ever estimated for plan usage.

**Method.** `spec.method` names how the agent should work: `builtin`, or a prompt file in the repository. The method text is placed before the task prompt. The factory judges only the result contract.

**Claims across machines.** Each machine has an id. A run claims its ticket by adding the label `factory:claimed` and a marker comment with the machine id and run id, then reads the comments back. The earliest marker wins. The loser ends its run with no change.

**Where it runs.** On a laptop the web app listens on localhost. On a dedicated machine `oxf` runs as a service and may listen on the local network behind a login token.

## Decision 12: gates, not prompts

Added on 2026-11-26. It replaces the dial levels and the "co-pilot mode" wording of Decision 11. The contracts of Decision 11 stand.

The value of the product is as little interaction as possible, with a reliable way to judge the result. So every run is unattended, and a person decides at fixed gates.

**Dial levels.** 0 off. 1 guided: the factory stops at every phase gate, which are the spec, the plan and every pull request. 2 autopilot: it stops at the pull request only. 3 autopilot with merge, locked in v0.1. A task at any level from 1 up starts runs by itself. The default for a new repository is 1.

**Permission policy.** Each run has a policy: the tools and commands it may use, and the paths it may write. The harness adapter turns the policy into that harness's own settings. A request inside the policy runs. A request outside is denied and recorded. The harness is never started in a mode that asks a person, and never in a mode that skips all checks.

**Rulings.** The result file gains a list:

```json
"rulings": [ { "decision": "Kept the old endpoint as an alias", "why": "Two callers still use it", "costIfWrong": "One extra route to remove later" } ]
```

A worker writes a ruling when its method would have asked a person and the choice can be undone and is inside the ticket. Otherwise it writes a question and ends with `needs_human`. The evidence bundle carries the rulings and the policy denials.

**Unattended contract.** A fixed text placed before any method. It tells the agent that it runs unattended, must not ask for confirmation, and must use rulings and questions as above. It is part of the worker contract.

**Questions are batched.** A run that ends with questions adds them to the inbox. Nothing else waits on them.

**Attention record.** The trust ledger also counts, per task: gate actions by a person per merged pull request, questions asked, rulings made, and rulings overturned. A ruling is counted as overturned when a person marks it so in the web app, or reverts the commit.

**Take the wheel.** The Model Context Protocol tools of Decision 11 stay, under that name. Claiming a ticket by hand is an action on a ticket, not a dial level. The run record keeps `mode` with the values `unattended` and `wheel`.

## Name

The first working name, "AgentFleet", is used by other projects in the same field, so it was dropped.

Decided by the owner on 2026-11-26: **OpenAgentix Software Factory**. The site and the API group are both `factory.openagentix.org`, a domain the owner holds. Command `oxf` is a working name. Tagline: "Runs alone on Docker. Governed on OpenAgentix."

- The group is the product's own, separate from the platform group `openagentix.io`, as OpenAgentix ADR 0012 requires.
- The platform group ends in `.io` and this group ends in `.org`. Both will show side by side on rung 3. Whether the platform group should move is a question for OpenAgentix, not for this record.
- One brand costs one maintainer less than two. The risk is that the name suggests the platform is required. The tagline and a quick start with no platform must answer that.
- No trademark search was done.

The API group and product name live in one Go constant and one web constant. Nothing is published until the owner says so.

## Limits, stated plainly

- Local mode has no container. The agent runs as the person, on their machine. See Decision 11.
- Usage is what the harness reports. Nothing is enforced locally. A harness that reports nothing shows "unknown".
- Whether subscription terms allow unattended runs started by another program is not checked.
- Protected paths and orchestrator-run checks reduce test gaming. They do not stop a worker from writing weak new tests.
- The trust ledger shows numbers. v0.1 does not move the dial by itself. A person does.
- Nothing in this record is proven until slice 1 runs.

## Consequences

- Two harnesses from the start keep the contract honest, at the cost of two adapters to maintain.
- The verify stage costs a second container start per run.
- Polling adds delay, up to the polling interval, before a run starts.
