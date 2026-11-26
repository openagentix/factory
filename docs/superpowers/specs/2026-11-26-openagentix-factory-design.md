# OpenAgentix Software Factory: design

Date: 2026-11-26. Revision 4: a local-first product with an autonomy dial, gates in place of prompts, and a review gate that is a decision. Status: working draft, published for comment. Nothing here is proven by a run.

Lines marked **(owner)** were decided by the owner. Lines marked **(call)** are the assistant's choices and can be changed.

Related: `docs/decisions/0001-core-contracts.md` (contracts), `docs/decisions/0002-threat-model.md`.

## 1. Intent

A solo founder or a small team states a business goal. The factory turns it into a spec, the spec into tickets, the tickets into verified pull requests, and later into deployed, observed software whose problems come back as tickets. A person steps in only where needed, and less over time as the factory earns trust. **(owner)**

**The factory does not own the agents. It owns the work.** People keep the Claude Code and Codex they already have, with the logins they already pay for, on their own machine. The factory orchestrates: it holds the pipeline, hands out work, checks results, records evidence and shows it all. **(owner)**

**The value in one line: as little interaction as possible, with a reliable way to judge the result.** **(owner)** People who use coding agents and workflow methods today spend the day answering "yes" to prompts. That is the problem this product removes. A factory that needs a person at every step is not a factory.

Why now: tools that turn one ticket into one pull request are free. The pain has moved to what surrounds that step. Reviewers are overloaded. Agents edit tests to pass them. Cost per outcome is unknown. There is no basis for deciding how far to trust an agent. Hosted app builders go from idea to a deployed app, but they are closed, they hold the code and the hosting, and they verify little.

What makes this product different:

1. **Your agents, your logins, your repository, your machines.** Open source, Apache-2.0. No API key needed to start.
2. **Gates, not prompts.** No run stops to ask for a yes. A person decides at a few fixed gates, in one pass.
3. **An autonomy dial per task.** It sets how many gates there are. Visible, and moved by a person.
4. **Evidence for every change.** The factory, not the agent, records what changed, what was checked and what it used.
5. **A trust ledger** that shows whether a higher dial setting has been earned, and how much of a person's attention the factory used.
6. **Verification from the spec.** Acceptance criteria are written before the code, and the builder cannot edit the checks.
7. **One engine from laptop to cluster.** A laptop, a dedicated machine, Kubernetes, and OpenAgentix where limits are enforced. **(owner)**

Positioning line: **the software factory that earns its autonomy.** Tagline: "Runs on your laptop. Governed on OpenAgentix." **(owner approved the positioning; tagline is a call)**

## 2. Who it is for

First: a small team with an existing codebase on GitHub, and solo founders. **(owner)** Then larger teams and enterprises, on Kubernetes and OpenAgentix. **(owner)**

| Where it runs | Who | Model access | Isolation |
|---|---|---|---|
| A laptop | One person, trying it or working with it | Existing Claude and Codex logins | A git worktree and the harness's own sandbox |
| A dedicated machine, such as a Mac mini | A founder or a small team that wants it always on | The same | The same |
| Kubernetes | Larger teams | Metered: API key or gateway | A container per run |
| OpenAgentix | Enterprises | Gateway key per run | Enforced budget, outbound rules, audit |

The same task files and the same web app on every row.

An existing repository starts with an **onboard** run that reads the repository, finds its test and lint commands and its CI, and proposes the factory configuration as a pull request with a readiness report. A new product starts from any template repository that has tests and CI, then onboards. v0.1 does not scaffold projects. **(call)**

## 3. Goals, non-goals, anti-goals

**Goals for v0.1**

- Works with the Claude Code and Codex already installed and logged in. No API key, no Docker, no cluster.
- A person types a goal in the web app and sees: questions to answer, a spec to approve, tickets appear, pull requests appear with evidence.
- No run waits at a prompt. People decide at gates (section 6).
- Any workflow method can be used inside a run: superpowers, Open GSD, the user's own. **(owner)**
- An autonomy dial per task in the web app, with the trust ledger beside it.
- First run within ten minutes of download. A demo mode needs no login and no GitHub token.
- A web app good enough to be the reason someone tries the product. **(owner: the visual interface is very important)**
- Every seam that Kubernetes and OpenAgentix will fill later exists as an interface now.

**Non-goals for v0.1**

- Container and Kubernetes runners, and the OpenAgentix governance provider. The interfaces exist. The backends come later.
- Deploying, observing production, and the learning loop.
- Merging. The top dial setting is shown but locked.
- Git hosts other than GitHub. User accounts in the web app.
- A workflow engine with branches, joins and waits.

**Anti-goals: it fails if**

- It opens more pull requests than its people can review.
- It claims autonomy or isolation that a run has not proven.
- A worker can change the checks that judge it.
- Trying it needs an API key, a cluster, a database server or a public endpoint.
- It copies or moves a person's login tokens anywhere.
- The web app is a wall of charts instead of a place to act.
- A person has to sit and approve steps for the work to move.

## 4. Constraints

- Open core, Apache-2.0. Embed only MIT and Apache-2.0 code. **(owner)**
- One maintainer. Each runner is thin. OpenAgentix is a governance provider, not a separate system.
- No work here may delay OpenAgentix 0.3. Nothing in v0.1 needs the platform.
- No paid model calls in tests. A stub harness exists for tests and demo mode.
- The factory launches the official `claude` and `codex` programs in place. It never reads, copies or forwards their login tokens.
- A run never holds a credential that can merge or deploy.
- Ticket and pull request text is untrusted input.
- Test first. A decision record comes before a new public contract.
- Docs in plain English. Public claims only for what a run has proven.
- Name: OpenAgentix Software Factory. Site and API group: `factory.openagentix.org`. Language: Go. **(owner)** Command: `oxf` **(call)**.

**To check before any public claim:** whether the terms of Claude and Codex subscriptions allow unattended runs started by another program. Not checked. If they do not, unattended runs on a subscription are dropped, taking the wheel remains for subscription users, and unattended runs need an API key.

## 5. Approaches

**A. Our own orchestrator binary.** One program polls the git host, starts work and serves the web app. Works with any git host later. The same engine runs everywhere. The trust ledger and live view have a home.

**B. GitHub Actions as the orchestrator.** No server. But GitHub only, no use of a person's own logins, no road to Kubernetes or OpenAgentix, no live view, and it competes with GitHub's own Agentic Workflows.

**C. Build on Fabro or Kelos.** A head start, but a different model in each case, and a roadmap tied to another one-maintainer project.

**Chosen: A, built from scratch, in Go. (owner)** One idea is kept from B for later: `oxf run --once` executes one run and exits, so the engine can run inside GitHub Actions or a Tekton step.

How agents connect, considered:

- *Spawn only*: the factory starts the local agent program. A real factory, but nothing for the person who wants to drive.
- *Pull only*: a person's session pulls work. Safe and familiar, but it is one more task queue for agents, a crowded field.
- **Both, on one run model. (owner)** Unattended runs are the product. They are built first because they force the whole engine to exist and have the shape of an OpenAgentix run. Pulling work into a person's own session stays as an action called "take the wheel", not as the normal way of working.

## 6. Gates, not prompts, and the dial

Every piece of work is a **run** with three stages: work, verify, publish. Every run is unattended.

**Why people press yes all day, and what replaces it**

| Interruption today | What the factory does instead |
|---|---|
| A permission prompt: "may I run this command?" | **Policy decides.** The run has an allowlist and a sandboxed worktree. Inside the policy it runs. Outside, it is denied and recorded. Nobody is asked |
| A checkpoint: "approve this plan?", "continue?" | **The agent rules, the factory records.** If the choice can be undone and is inside the ticket, the agent decides and writes a ruling: what, why, and the cost if wrong. The verify stage is the real approval |
| A real question that only the person can answer | **It goes to the inbox.** The run ends. Other work goes on. The person answers a batch in one sitting, and the answer starts the next run |

The rule: **people decide at gates, never at prompts.**

**The autonomy dial.** One setting per task. It sets how many gates there are.

| Level | Name | The factory stops for a person at |
|---|---|---|
| 0 | Off | The task does not run |
| 1 | Guided | Every phase gate: the spec, the plan, and every pull request |
| 2 | Autopilot | The pull request only |
| 3 | Autopilot with merge | Nothing, for a class of change that has earned it. **Shown, not active, in v0.1** |

Defaults **(call)**: every task at 1 for a new repository. A person turns a task up when its record supports it.

Rules for the dial:

- The repository configuration sets a **ceiling** per task. Changing it is a pull request, so it is shared and has a history.
- In the web app a person moves the dial at or below the ceiling. It takes effect at once and the change is logged.
- The trust ledger sits beside the dial. In v0.1 it informs. It does not move the dial.
- On OpenAgentix the platform policy sets the ceiling and enforces it. Policy can only tighten. Same dial, stronger hand.

**The review gate is a decision, not a button.** A gate that a person passes without reading is one more "yes". So the factory writes a **decision card** for every pull request: what you are approving, what was decided for you, what was not verified, and where an independent review by the other model disagrees. Risk sets the effort: one action for low risk, a confirmation per item for high risk. Detail in decision record 0001, Decision 13.

```
YOU ARE APPROVING                                risk: MEDIUM
 1. Public contract changes: the result file gains "rulings"
 2. Two new risks accepted in the threat model
DECIDED FOR YOU (3 rulings)                      [overturn]
NOT VERIFIED
 - nothing in this change has been run
 - the summary does not mention 2 changed files
INDEPENDENT REVIEW (other model): agrees, 1 concern
```

**Trust comes from outcomes, not from merges.** A merge without engagement never raises trust. The ledger counts engaged reviews and, above all, what happened after the merge: not reverted, no follow-up fix, the default branch stayed green. Automatic merge opens per **class of change**, when that class has a clean record, proposed by the factory and switched on by a person. One bad outcome closes it again.

**The attention record.** The trust ledger counts what a person feels: touches per merged pull request, questions asked, rulings made, rulings a person overturned. Many overturned rulings say turn the dial down. Few say it can go up.

**Bring your own method. (owner)** A task names a method: the built-in prompts, or a prompt file of the user's own. The agent is the user's own install, so their skills are present, and a method file can say "use superpowers" or "use Open GSD". Nobody is made to give up the method they use.

The factory puts one **unattended contract** in front of any method:

> You run unattended. Never ask for confirmation. Where your method says to ask a person: if the choice can be undone and is inside the ticket, decide and record a ruling. Otherwise write the question to the result and stop.

Changing how a method behaves in this way is accepted. **(owner)** A method with its own phases, such as Open GSD, may own a whole phase: one run yields the spec and the tickets, and the factory builds them. The factory judges the result contract, not the route. v0.1 ships the built-in method and documented recipes for superpowers and Open GSD. **(call)**

**Take the wheel.** A person can claim any ticket into their own Claude Code or Codex session and hand the result back. `oxf` serves a Model Context Protocol endpoint for this: list ready work, claim a ticket (which makes a worktree and a branch and returns the brief), ask a question, submit. Verify, evidence and publish are the same. This is also the fallback if subscription terms do not allow unattended runs. **(call)**

**Several machines.** State is in GitHub, so each teammate can run `oxf` on their own machine. A run claims its ticket with a label and a marker comment, reads back, and the earliest claim wins. No shared server is needed. **(call)**

## 7. The pipeline

Seven phases. v0.1 covers the first four as far as a pull request.

```
Goal -> Spec -> Plan -> Build -> Verify -> Ship -> Observe
                                              (feedback returns as tickets)
```

**The rule that keeps it small:** all durable state lives in the git host. A run never waits for a person. It ends. The person's action on the git host or in the web app triggers the next run. There is no workflow engine. A pipeline is a chain of triggers.

| Phase | Task | Trigger | Writes | Person re-enters by |
|---|---|---|---|---|
| Setup | `onboard` | Repository added | Pull request with `.factory/` configuration and a readiness report | Merging it |
| Goal to spec | `brainstorm` | Goal ticket created, or a new answer on it | Questions as a comment, or a spec file as a pull request | Answering. Merging the spec |
| Spec to plan | `decompose` | Spec pull request merged | Tickets linked to the goal, each with acceptance criteria and dependencies | Editing or closing tickets |
| Build | `implement` | Ticket is ready and its dependencies are closed | Draft pull request with an evidence bundle | Reviewing |
| Build | `review` | Agent pull request opened | A cold review comment against the ticket's acceptance criteria | Reading it |
| Build | `fix-ci` | A required check failed on an agent pull request | A fix commit. At most two tries | Taking over after the cap |

Where things live:

- A **goal** is a GitHub issue with the label `factory:goal`.
- A **spec** is a markdown file in the repository, `docs/specs/<slug>.md`. **Merging the spec pull request is the approval.**
- A **question** is a comment on the goal issue, shown in the inbox. An answer typed in the web app is posted as a comment.
- A **ticket** is a sub-issue of the goal with the label `factory:planned`. The factory promotes it to `factory:ready` when its dependencies are closed and the review cap allows.
- **Acceptance criteria** are a checklist in each ticket, copied from the spec.

Rules **(calls)**:

- `brainstorm` asks its questions in batches: at most a few per round and at most three rounds. Then it writes the spec with its open points listed. A person who prefers to talk it through can take the wheel.
- `decompose` proposes at most twelve tickets per spec.
- The **review cap**: at most three open agent pull requests waiting for a person by default. Above it, tickets stay planned and the web app shows why.
- Cross-model review: when both harnesses are present, `review` uses the one that did not write the change.
- A failure in a fix run's own work triggers nothing. Fixes never chain.

**Later phases, for direction only**

- Verify: acceptance criteria become executable checks written by a run that cannot see the implementation, kept on protected paths. A retro task turns reverts and repeated review comments into proposed checks.
- Ship: the factory triggers and watches the team's own delivery, to a cloud, to Kubernetes or to a hosted platform, and verifies after deploy. It does not become a deploy tool. **(owner named the targets)**
- Observe: an alert becomes a ticket. The loop closes.

## 8. Inside one run

1. **Work.** `oxf` makes a worktree, writes the input files, and starts the harness with no terminal, a deadline, a permission policy and its strictest workable sandbox setting. When a person takes the wheel, their session works in the worktree instead.
2. **Verify.** `oxf` computes the diff, fails the run on a change to a protected path, runs the task's checks on a clean checkout of the commit with no model access, and builds the evidence bundle with a rule-based risk level.
3. **Publish.** `oxf` pushes a branch, opens a draft pull request, comments, labels or creates tickets, with a GitHub token that can read and propose. A fixed list of actions. It never merges and never pushes to the default branch.

The evidence bundle is rendered in the pull request body as Summary, Evidence and Merge danger, and in the web app. It lists the **rulings** the agent made ("decisions made for you") and anything the policy denied, so a person can judge them in one pass at the gate.

**Usage has three states**, never a guess:

| State | When | Shown as |
|---|---|---|
| Metered | An API key or gateway | Dollars and tokens |
| Plan | A subscription login | Tokens, "included in plan" |
| Unknown | The harness reported nothing | "unknown" |

**What local mode does not give.** There is no container. The agent runs as the person, on a machine that holds their git credentials. The factory starts it with those credentials removed from its environment where it can, inside a worktree, with the harness sandbox on. That is best effort. The honest statement is: the same trust as running Claude Code or Codex yourself. Container isolation and enforced limits are what the Kubernetes and OpenAgentix rungs add.

## 9. Configuration

- In the target repository under `.factory/`: `factory.yaml`, `tasks/*.yaml`, `prompts/*.md`. Read from the default branch only. A protected path for every task except `onboard`.
- The binary carries default tasks and prompts.
- Machine settings (which harnesses are installed, the GitHub token, the listen address) live in the user's home directory, never in the repository.
- GitHub access in v0.1 is a fine-grained personal access token limited to the repository. A GitHub App comes later. **(call)**
- On a dedicated machine `oxf` installs as a service and the web app can listen on the local network behind a login token. On a laptop it listens on localhost. Runs are safe to repeat, so sleep or a restart loses only the run in flight. **(call)**

## 10. The web app

Stack: the OpenAgentix console stack, so it can later mount in the console. Svelte 5 with runes, SvelteKit as a single-page app, Tailwind 4 with theme tokens, no component kit, strict content security policy, embedded in the binary. Live updates by server-sent events.

Principle: **a place to act, not a dashboard.** The first screen answers "what needs me" and "what is happening".

```
+ Goal: Let customers reschedule bookings ---------------------+
| Spec ok   Plan ok (6)   Build > 2 of 6   Verify   Ship       |
+ NEEDS YOU (2) -----------------------------------------------+
| ? Can a booking move after it started?           [answer]    |
| * PR 14 ready, risk low, evidence                [review]    |
+ LIVE --------------------------------------------------------+
| > 9 reschedule API, claude, 4m, running tests                |
+--------------------------------------------------------------+
```

The dial, one per task:

```
 implement                                  ceiling: autopilot
   off      guided     [AUTOPILOT]    merge (locked)
    o---------o------------O- - - - - - - x
 last 30 days: 14 runs, 11 merged, 2 reworked, 0 reverted
 your attention: 1.2 touches per merged pull request
 4 questions, 31 rulings, 2 overturned
 merge is locked: needs an enforced rung and a longer record
```

Screens in v0.1:

| Screen | Shows | Actions |
|---|---|---|
| Home | Each goal as a pipeline strip. The inbox. Live runs | New goal. Answer. Open |
| Goal | Phases, the spec, tickets with state, runs, usage | Answer questions. Open the spec pull request |
| Run | Live transcript, stage, evidence bundle with rulings and denials, usage, limits | Cancel. Take the wheel |
| Review | The decision card for one pull request: approving, decided for you, not verified, independent review, risk | Confirm items. Overturn a ruling. Open on GitHub |
| Inbox | Everything that waits at a gate, batched: questions, specs and plans to approve, pull requests to review, runs that need a person, held tickets with the reason | Answer. Open on GitHub |
| Autonomy | A dial per task with its ceiling, the trust ledger and the attention record beside each | Move a dial. Pause all |
| Tasks | Each task as a card: trigger, reads and writes, next step, where a person re-enters, what stops double work, method | View the file |
| Settings | Repository, harnesses found, review cap, readiness report | Edit machine settings |

Wherever a control is only advisory on this rung, the app marks it "enforced on OpenAgentix".

The look is the owner's to decide. Mockups come before the web app is planned.

## 11. Architecture

One Go binary, `oxf`.

| Part | Job |
|---|---|
| `api` | Shared types, strict parsing, validation. **Built.** |
| `store` | SQLite: run records shaped like the planned OpenAgentix `AgentRun`, dial settings and their log |
| `githost` | Interface, a GitHub implementation and a fake. Polls, reads, and performs the fixed publish actions |
| `runner` | Interface. v0.1: a **local** runner (a process in a worktree) and a fake. Later: container, Kubernetes Job |
| `harness` | Thin adapters that build the command line for `claude`, `codex` and the stub, and read their event streams |
| `governance` | Interface. v0.1: local, with usage as reported and the three usage states |
| `verify` | Diff, protected paths with real glob matching, checks, risk rules, evidence bundle |
| `orchestrator` | Polls, matches triggers to tasks, applies the dial, throttles, the review cap and claims, drives the three stages |
| `policy` | The permission policy for a run: what is allowed, what is denied and recorded |
| `wheel` | The Model Context Protocol endpoint for taking the wheel: list, claim, ask, submit |
| `server` | JSON API, server-sent events, the embedded web app |
| `web/` | The Svelte app |

Seams for Kubernetes and OpenAgentix, all present in v0.1 as interfaces:

| Seam | v0.1 | Later |
|---|---|---|
| Run record | SQLite | `AgentRun` |
| Runner | Local process | Container, Job, `Workspace` |
| Governance | Reported usage | Gateway key per run, enforced budget, audit |
| Dial ceiling | Repository file | `PlatformPolicy`, enforced |
| Credentials | Token held by `oxf` only | `CredentialGrant` |
| Triggers | Polling, local clock | `Trigger`, `Schedule` |
| Evidence | JSON | Signed provenance |
| Web app | Own binary | Console plugin |

## 12. Releases

| Release | Content |
|---|---|
| v0.1 | Local runner with existing logins. Unattended runs with gates. The dial, trust ledger and attention record. Take the wheel. Goal to spec to tickets to evidenced pull requests. Web app. Demo mode |
| v0.2 | Checks from the spec that the builder cannot touch. Learning loop. Container runner with an API key. Starter template |
| v0.3 | Ship phase. `oxf run --once`. Kubernetes Job runner |
| v0.4 | Observe phase. OpenAgentix governance provider, as far as the platform allows then |

Build order inside v0.1 **(call)**:

1. Unattended slice: a ready ticket, the local `claude` or `codex` in a worktree under a permission policy, verify, an evidenced draft pull request.
2. The web app on top: pipeline, inbox, live run, evidence, the dial.
3. The front half: goal to spec to tickets, with batched questions.
4. Take the wheel: the endpoint and the plugin.

There is a useful product after step 2 if later steps slip.

## 13. Limits and risks, stated plainly

- Whether subscription terms allow unattended runs is not checked. See section 4.
- Local mode has no container. See section 8.
- Subscription rate limits cap how much the factory can do. For a small team that also acts as a brake.
- Building features with agents is the least proven use in the research. Maintenance work is where factories succeed today. Expect rework. The trust ledger exists to show it.
- No research in hand covers teams of two to twenty. That audience is a bet.
- Hosted app builders already serve founders who want a prototype. We serve people who want to own and trust the result.
- Headless Claude Code and Codex are confirmed by flag, not by run. So is running them under a permission policy with no prompts.
- An agent can make a wrong ruling. Rulings are shown, and the verify stage is outside the agent, but a bad choice that passes the checks reaches the reviewer.
- This is a large v0.1 for one maintainer. The build order protects against that.

## 14. Settled after a builder's review

A fresh reviewer read revision 1 as the person who must plan it. Its questions were answered as calls. Detail in decision record 0001, Decisions 10 and 11.

| Question | Call |
|---|---|
| Which compilers does a run have? | Local mode uses what is on the machine. Container mode names a base image in `factory.yaml`. `onboard` proposes the checks |
| How do tasks without a code change write results? | The worker writes files (`questions`, `issues.json`, `review.md`). The publish step acts on them. The worker never calls GitHub |
| Which events trigger runs, and how are loops avoided? | Labels, comments and merges, only from users with write access. The factory's own comments carry a marker and never trigger |
| How can `onboard` write `.factory/` if it is protected? | It is the one exception. Configuration is read from the default branch only |
| What do `fix-ci` and `review` work on? | Agent pull requests only, known by the `factory/` branch prefix and a label. No fork pull requests in v0.1 |
| Does a limit stop a run? | The deadline always does. A cost limit applies to metered usage, by cancelling, best effort |
| What is demo mode? | A fake git host with a seeded repository and a scripted story. No login or token. Labelled as a demo |
| Who opens the pull requests? | The token's user. The docs recommend a separate machine user. A GitHub App comes later |

Kept open on purpose, so later products are not blocked: more than one repository per install, a store interface that another database can back, and an auth seam in the web app.

## 15. Left to the builder

File layout inside packages, SQLite schema detail, API routes, the exact prompt text, the polling method, the exact harness flags, component breakdown of the web app, and the test design, within the rules of this document and the two decision records.
