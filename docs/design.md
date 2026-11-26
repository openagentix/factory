# OpenAgentix Software Factory: design

Date: 2026-11-26. Revision 7: agrees with decision record 0001. Planning is interactive and has no limit; after the plan is approved the factory works alone. Quick and standard lanes, acceptance checks, a machine gate, staging with Accept and Undo, and trajectory. Status: working draft, published for comment. Nothing here is proven by a run.

Lines marked **(owner)** were decided by the owner. Lines marked **(call)** are the assistant's choices and can be changed.

Related: `docs/decisions/0001-core-contracts.md` (contracts), `docs/decisions/0002-threat-model.md`.

## 1. Intent

A solo founder or a small team states a business goal. The factory turns it into a spec, the spec into tickets, the tickets into verified changes, and later into deployed, observed software whose problems come back as tickets. A person steps in only where needed, and less over time as the factory earns trust. **(owner)**

**The factory does not own the agents. It owns the work.** People keep the Claude Code and Codex they already have, with the logins they already pay for, on their own machine. The factory orchestrates: it holds the pipeline, hands out work, checks results, records evidence and shows it all. **(owner)**

**The product line: "Spend your time deciding what to build. None watching it get built."** **(owner)** Planning is a live conversation with no limit on rounds or questions, in the person's own Claude Code or Codex session or in the web app. Once the plan is approved, the factory works alone with a reliable way to judge the result. People who use coding agents today spend the day answering "yes" to prompts while the work is built. That is the problem this product removes.

**Designed for the person who will not review.** Twice on the first day, the owner merged a change unread, and was right to: a founder cannot judge code and should not have to. So the factory does the judging with acceptance checks and an independent review, combines passing work on a staging branch, and shows the person outcomes in plain words: what works, what does not, what could not be confirmed. The person decides three things: what they want, whether this is what they meant, and whether to accept it. Detail in decision record 0001, "Three moments and two views".

**Trajectory matters most.** **(owner)** Work on a good course tends to stay good. Work on a bad course is hard to steer back. So planning and verification get the most care, and the factory watches the course of each goal and stops early.

Why now: tools that turn one ticket into one pull request are free. Practitioners report that the pain has moved to what surrounds that step: reviewers are overloaded, agents edit tests to pass them, cost per outcome is unknown, and there is no basis for deciding how far to trust an agent. These are reports, not our findings. Hosted app builders go from idea to a deployed app, but they are closed, they hold the code and the hosting, and they verify little.

What makes this product different:

1. **Your agents, your logins, your repository, your machines.** Open source, Apache-2.0. Designed to need no API key, if subscription terms allow unattended runs started by another program, which is not checked (section 4).
2. **Gates, not prompts, after planning.** No run after the plan stops to ask for a yes. The machine gate judges each change. A person is interrupted only by an exception.
3. **An autonomy dial per task.** It sets where the factory stops for a person. Visible, and moved by a person.
4. **Evidence for every change.** The factory, not the agent, records what changed, what was checked and what it used.
5. **A trust ledger** that shows whether a higher dial setting has been earned by outcomes after Accept, and how much of a person's attention the factory used.
6. **Verification from the spec.** Acceptance checks are written from the person's own words before the code, and the builder cannot edit them.
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

An existing repository starts with an **onboard** run that reads the repository, finds its test and lint commands and its CI, and proposes the factory configuration with a readiness report. It takes effect after Accept, like any other change. A new product starts from any template repository that has tests and CI, then onboards. v0.1 does not scaffold projects. **(call)**

## 3. Goals, non-goals, anti-goals

**Goals for v0.1**

- Works with the Claude Code and Codex already installed and logged in. Designed to need no API key (see section 4), no Docker, no cluster.
- A person plans a goal for as long as they need, approves the acceptance list, then sees the list filling in, then Accept.
- After the plan is approved, no run waits at a prompt. A person is asked only at Accept and at exceptions (section 6).
- Any workflow method can be used inside a run: superpowers, Open GSD, the user's own. **(owner)**
- An autonomy dial per task in the web app, with the trust ledger beside it. Acceptance checks, the machine gate, staging, Accept and Undo.
- First run within ten minutes of download. A demo mode needs no login and no GitHub token.
- A web app good enough to be the reason someone tries the product. **(owner: the visual interface is very important)**
- Every seam that Kubernetes and OpenAgentix will fill later exists as an interface now.

**Non-goals for v0.1**

- Container and Kubernetes runners, and the OpenAgentix governance provider. The interfaces exist. The backends come later.
- Deploying, observing production, and the learning loop.
- Automatic Accept. The top dial setting is shown but not active.
- Git hosts other than GitHub. User accounts in the web app.
- A workflow engine with branches, joins and waits.

**Anti-goals: it fails if**

- It asks a person to read code to make progress.
- It claims autonomy or isolation that a run has not proven.
- A worker can change the checks that judge it.
- Trying it needs an API key, a cluster, a database server or a public endpoint.
- It copies or moves a person's login tokens anywhere.
- The web app is a wall of charts instead of a place to act.
- A person has to sit and approve steps for the work to move after the plan is approved.

## 4. Constraints

- Open core, Apache-2.0. Embed only MIT and Apache-2.0 code. **(owner)**
- One maintainer. Each runner is thin. OpenAgentix is a governance provider, not a separate system.
- No work here may delay OpenAgentix 0.3. Nothing in v0.1 needs the platform.
- No paid model calls in tests. A stub harness exists for tests and demo mode.
- The factory launches the official `claude` and `codex` programs in place. It never reads, copies or forwards their login tokens.
- The factory never gives a run a git host credential. The publish step holds a propose token, and an accept token only if the person grants one. In local mode the agent runs as the same operating system user as `oxf` and can read what that user can read (section 8).
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

Planning is with the person and has no limit. After the plan is approved, every piece of work is an unattended **run** with three stages: work, verify, publish.

**Why people press yes all day, and what replaces it**

| Interruption today | What the factory does instead |
|---|---|
| A permission prompt: "may I run this command?" | **Policy decides.** The run has an allowlist and a sandboxed worktree. Inside the policy it runs. Outside, it is denied and recorded. Nobody is asked |
| A checkpoint while the work is built: "continue?", "is this approach fine?" | **The agent rules, the factory records.** If the choice can be undone and is inside the ticket, the agent decides and writes a ruling: what, why, and the cost if wrong. The machine gate is the real approval |
| A real question that only the person can answer | **It goes to the inbox.** The run ends. Other work goes on. The person answers a batch in one sitting, and the answer starts the next run |

The rule: **people decide at gates, never at prompts.**

**The machine gate.** Before a change is combined, it must pass three things in order: the verify stage (no protected path touched, the task's checks pass, no acceptance check that was working now fails), an independent review that agrees, and a risk level that is not high. Risk is about what changed (risky paths, an existing test changed or deleted), never about the review. If verify or review fails, the points go back to the builder; no person can pass a change whose review did not agree. A ticket gets three work-stage starts by default, shared by `implement` and `fix-ci`. When they run out, the goal is off course and the person is told, in plain words, that those lines are not working yet. A same-harness review may pass a change, but marks it single-opinion. Detail in decision record 0001, "The machine gate and risk".

**Exceptions are the only interruptions.** Each reaches the person as one plain question about what they want, or a plain statement that a line is not working yet. Never a request to read a change. A change that is high risk and has an agreeing review is put as its consequence: "this changes how other programs talk to yours. Is that what you want?". A yes combines it.

**The autonomy dial.** One setting per unattended task. It sets where the factory stops for a person after the plan is approved. Planning is not on the dial.

| Level | Name | A person approves |
|---|---|---|
| 0 | Off | The task does not run |
| 1 | Guided | Each change before it is combined. The engineering profile |
| 2 | Autopilot | Nothing until Accept |
| 3 | Automatic Accept | Nothing, for a class of change that has earned it. **Shown, not active, in v0.1** |

Default **(call)**: every task at 2, so after planning a person meets only Accept and exceptions. A developer who wants to steer sets a task to 1.

Rules for the dial:

- The repository configuration sets a **ceiling** per task. It changes only through the default branch, so it is shared and has a history.
- In the web app a person moves the dial at or below the ceiling. It takes effect at once and the change is logged.
- The trust ledger sits beside the dial. In v0.1 it informs. It does not move the dial.
- On OpenAgentix the platform policy sets the ceiling and enforces it. Policy can only tighten. Same dial, stronger hand.

**The decision card** is the engineering view of one change, computed from structured inputs, not from a model's prose: what changed under risky paths, what was decided for you, what was not confirmed, the independent review, the risk. At level 1 it is where a developer approves a change.

```
CHANGE 14: reschedule endpoint                    risk: MEDIUM
 Changed: internal/booking/move.go, move_test.go (new)
DECIDED FOR YOU (3 rulings)                      [overturn]
NOT CONFIRMED
 - 1 changed file is not covered by any claim
 - criterion 5 "works on a phone": no check
INDEPENDENT REVIEW (other model): agrees, 1 minor point
```

**Trust comes from outcomes, not from Accept.** Pressing Accept is never evidence. What counts is what happened after: not reverted, no follow-up fix, the default branch stayed green, and the change was actually exercised. A change nobody exercised is silent, and silence is not success. Automatic Accept opens per **class of change**, only for low-risk classes reviewed by the other harness, when the class has a clean record. The factory proposes it and a person switches it on. One bad outcome closes it again. With one maintainer this record is honest but not independent, and the factory says so.

**The attention record.** The trust ledger counts what a person feels: touches per accepted goal, questions asked, rulings made, rulings a person overturned. Many overturned rulings say turn the dial down. Few say it can go up.

**Bring your own method. (owner)** A task names a method: `builtin`, `plugin:<name>` for an installed workflow plugin, or a prompt file of the user's own. Nobody is made to give up the method they use.

After the plan is approved, the factory puts one **unattended contract** in front of any method:

> You run unattended. Never ask for confirmation. Where your method says to ask a person: if the choice can be undone and is inside the ticket, decide and record a ruling. Otherwise write the question to the result and stop.

Changing how a method behaves in this way is accepted for unattended runs. **(owner)** During planning a plugin's interactive steps are used as they are. The factory judges the result contract, not the route. **(call)**

**Take the wheel.** A person can plan a goal, or claim any ticket, in their own Claude Code or Codex session and hand the result back. `oxf` serves a Model Context Protocol endpoint for this: list goals and ready work, claim one (which makes a worktree and a branch and returns the brief), ask a question, submit a spec, a plan or a change. Verify, the machine gate, evidence and publish are the same. This is also the fallback if subscription terms do not allow unattended runs. **(call)**

**Several machines.** State is in GitHub, so each teammate can run `oxf` on their own machine. A run claims its ticket with a label and a marker comment, reads back, and the earliest claim wins. A claim expires after a set time, so a crashed machine does not hold it for ever. One machine at a time holds the staging claim for a repository, by the same rule, and only it combines, rebuilds and accepts. No shared server is needed. **(call)**

### Lanes: the process fits the size of the job

v0.1 has two lanes. **Quick**: a short planning conversation gives the goal and one to three acceptance lines; when the person confirms them, the factory writes a short spec and one ticket itself and builds alone. **Standard**: planning goes through a spec and then tickets, and the person approves both. Planning shrinks with the size of the job. Checking never does. A goal that goes off course is planned again with the person, and a quick goal is proposed for the standard lane. It never moves down without the person saying so. **(owner: a heavy method is too slow for a quick job, and a light one is not enough for a large build)**

A **deep** lane, for a new product or a large rework, is direction only and not in v0.1: research, a roadmap and phases, built one phase at a time. It is likely to be driven by an installed plugin such as Open GSD, after a trial.

### Methods: keep the one you use

How the agent works is a **method**, and the factory does not own it. People who use superpowers or Open GSD keep doing so: the factory is designed to run the plugin they have installed, by name. This has not been tried. It ships and copies none of their text. With no plugin installed, or a named plugin missing, short built-in prompts are used, and the run records both what was asked and what ran. A plugin's own verification is information only. The factory's acceptance checks and gate decide. **(owner)**

Planning is where a person's attention goes, for as long as they need. After the plan is approved the factory works with the lights off. **(owner)**

## 7. The pipeline

Seven phases. v0.1 covers Goal, Spec, Plan, Build and Verify, as far as Accept.

```
Goal -> Spec -> Plan -> Build -> Verify -> Ship -> Observe
                                              (feedback returns as tickets)
```

**The rule that keeps it small:** durable state lives in the git host and the factory's own store. After the plan is approved, a run never waits for a person. It ends. The next event, on the git host or in the web app, triggers the next run. There is no workflow engine. A pipeline is a chain of triggers.

| Phase | Task | Started by | Writes | Passes when |
|---|---|---|---|---|
| Setup | `onboard` | Repository added | A `.factory/` configuration and a readiness report | Review agrees. Takes effect after Accept. Attempts per repository |
| Goal to spec | `brainstorm` (standard lane) | The person, in a planning conversation | A spec with numbered acceptance criteria | The person approves it, after seeing its review |
| Spec to plan | `decompose` (standard lane) | The person, after the spec | Tickets linked to the goal, each naming the criteria it serves, with dependencies | The person approves it, after seeing its review |
| Plan to checks | `acceptance` | The plan is approved (quick lane: the lines are confirmed) | One executable check per criterion, and a manifest per goal, on a protected path | Review agrees. Attempts per goal; uncovered lines become "could not confirm" |
| Build | `implement` | Ticket ready: its checks and dependencies are in staging | One change, with an evidence bundle | The machine gate |
| Build | `review` | A spec, plan, set of checks or change is ready | A structured review against the person's criteria | Not gated |
| Build | `fix-ci` | A required check failed on an open agent pull request | A fix commit. It shares the ticket's attempts with `implement` | The machine gate, again |

In the quick lane there is no `brainstorm` or `decompose` run: the factory writes the short spec and the one ticket itself.

Where things live:

- A **goal** is a GitHub issue with the label `factory:goal`.
- A **spec** is a markdown file in the repository, `docs/specs/<slug>.md`. The person approves it in the planning conversation, where they see it as the acceptance list. It reaches the default branch with the work, on Accept.
- A **question** is a comment on the goal issue, shown in the inbox. An answer typed in the web app is posted as a comment.
- A **ticket** is a sub-issue of the goal with the label `factory:planned`. The factory promotes it to `factory:ready` when its goal's acceptance checks and its dependencies are in staging and the cap allows.
- **Acceptance checks** live under `acceptance/<goal>/` with one manifest per goal: criterion id, the person's wording, the command. Only the `acceptance` task may write there.

Rules **(calls)**:

- Planning has no cap on rounds or questions. The person can plan in the web app or in their own session.
- `decompose` proposes at most twelve tickets per spec.
- The **cap**: at most three agent changes open and not yet combined, by default. Above it, tickets stay planned and the web app shows why.
- Cross-model review: when both harnesses are present, `review` uses the one that did not write the subject.
- A failure in a fix run's own work triggers nothing. Fixes never chain.

**Later phases, for direction only**

- Verify: holdout scenarios that the builder never sees. A retro task turns reverts and repeated review points into proposed checks.
- Ship: the factory triggers and watches the team's own delivery, to a cloud, to Kubernetes or to a hosted platform, and verifies after deploy. It does not become a deploy tool. **(owner named the targets)**
- Observe: an alert becomes a ticket. The loop closes.

## 8. Inside one run, and staging

1. **Work.** `oxf` makes a worktree from the current staging state, writes the input files, and starts the harness with no terminal, a deadline, a permission policy, its strictest workable sandbox setting and no git host credential in its environment. When a person takes the wheel, their session works in the worktree instead.
2. **Verify.** `oxf` computes the change, fails the run on a change to a protected path, runs the task's checks and the acceptance checks on a clean checkout of the commit with no model access, in the same scrubbed environment and sandbox as the worker, and builds the evidence bundle.
3. **Publish.** `oxf` pushes a `factory/` branch, opens a draft pull request into staging, comments, labels or creates tickets, with a **propose token**. A fixed list of actions, none of which writes to the default branch.

The evidence bundle is rendered in the pull request body as Summary, Evidence and Risk, and in the engineering view. It lists the **rulings** the agent made ("decisions made for you") and anything the policy denied.

**Staging, Accept and Undo.** A change that passes the machine gate is added to a list, and the branch `factory-staging` is rebuilt from the default branch plus each change on the list, in order, one commit per change. Staging is never patched. It is rebuilt whenever a change passes, a change is undone, or the default branch moves. The checks and acceptance checks run again on every rebuild. A change that no longer fits is taken out and built again on its ticket, which uses an attempt. The acceptance list the person sees is computed from that staging state.

- **Accept** takes the whole of staging, the exact state the person saw, and always needs the web app's login token. While it is pending, staging is frozen. If the person granted an **accept token**, the factory merges staging into the default branch. If not, the outcome view says "ask a developer to finish this step", and a developer merges the staging pull request without squashing, or the changes' outcomes cannot be attributed.
- **Undo** works on any change, not only the last. Before Accept it takes the change, and any that depend on it, off the list and rebuilds. After Accept it is a revert on the default branch, which needs the accept token or a developer. An undone change's ticket is closed as taken back.

**Trajectory.** Each goal is on course, drifting or off course, by rules with starting thresholds that are untested. Off course means a ticket used all its attempts, the same criterion stopped working on two rebuilds, or two changes of the goal were undone. Then the factory stops building on that goal, takes that goal's changes back to its own last good point (other goals' changes stay), tells the person which lines are not working yet, and plans again with them. For a quick goal it proposes the standard lane.

**Usage has three states**, never a guess:

| State | When | Shown as |
|---|---|---|
| Metered | An API key or gateway | Dollars and tokens |
| Plan | A subscription login | Tokens, "included in plan" |
| Unknown | The harness reported nothing | "unknown" |

**What local mode does not give.** There is no container. The agent runs as the same operating system user as `oxf`, on a machine that holds the person's git credentials, and can read what that user can read. The factory removes credentials from its environment, keeps its own tokens in the operating system's credential store, and turns the harness sandbox on. That is best effort. On a dedicated machine, running `oxf` under its own operating system user is the recommended setup; v0.1 does not build it. The honest statement is: the same trust as running Claude Code or Codex yourself. Container isolation and enforced limits are what the Kubernetes and OpenAgentix rungs add.

## 9. Configuration

- In the target repository under `.factory/`: `factory.yaml`, `tasks/*.yaml`, `prompts/*.md`. Read from the default branch only. A protected path for every task except `onboard`.
- The binary carries default tasks and prompts.
- Machine settings (which harnesses are installed, the listen address, thresholds) live in the user's home directory, never in the repository. The GitHub tokens and the login token are kept in the operating system's credential store.
- GitHub access in v0.1 is a fine-grained personal access token limited to the repository, as the propose token, and optionally a second one as the accept token. Branch protection must stop the propose token's user from updating the default branch. For `factory-staging`, restrict who may push; do not forbid force pushes, which every rebuild needs. A GitHub App comes later. **(call)**
- On a dedicated machine `oxf` installs as a service and the web app can listen on the local network behind a login token. On a laptop it listens on localhost. The web app has no accounts, so Accept and Undo always need the login token, on localhost too. Runs are designed to be safe to repeat, so sleep or a restart loses only the run in flight. **(call)**

## 10. The web app

Stack: the OpenAgentix console stack, so it can later mount in the console. Svelte 5 with runes, SvelteKit as a single-page app, Tailwind 4 with theme tokens, no component kit, strict content security policy, embedded in the binary. Live updates by server-sent events.

Principle: **outcomes first, detail on demand.** The default is the **outcome view**. It never uses the words pull request, merge, diff, review, branch or commit. The **engineering view** shows everything, for developers who want to steer. **(owner)**

```
 Let customers reschedule bookings              ON COURSE

  ok  A customer can move a booking to another day
  ok  A booking that already started cannot be moved
  ok  The customer gets a confirmation email
  ..  Old bookings still show the old time   not working yet  (being worked on)
  ?   Works on a phone                    could not confirm

  [ Try it ]      [ Accept ]      [ Undo a change ]

 Try it runs code the factory wrote. No person has read it.

 ONE QUESTION FOR YOU
  Should a customer be able to move a booking more than once?
```

The engineering view, for those who want it:

```
+ Goal: Let customers reschedule bookings ---------------------+
| Spec ok   Plan ok (6)   Build > 2 of 6   Verify   Ship       |
+ NEEDS YOU (2) -----------------------------------------------+
| ? Can a booking move after it started?           [answer]    |
| ! Change 15 is high risk: a public contract      [card]      |
+ LIVE --------------------------------------------------------+
| > 9 reschedule API, claude, 4m, running tests                |
+--------------------------------------------------------------+
```

The dial, one per task:

```
 implement                                  ceiling: autopilot
   off      guided     [AUTOPILOT]    auto-accept (not active)
    o---------o------------O- - - - - - - x
 last 30 days: 14 changes accepted, 2 reworked, 0 undone
 your attention: 1.2 touches per accepted goal
 4 questions, 31 rulings, 2 overturned
 auto-accept is not active in v0.1
```

Screens in v0.1:

| Screen | Shows | Actions |
|---|---|---|
| Outcomes (default) | Each goal with its acceptance list in three states, its trajectory, and plain questions | New goal. Plan. Answer. Try it. Accept. Undo a change |
| Planning | The live planning conversation, the spec or plan so far, and the independent review's points | Talk. Change the lane. Approve |
| Home (engineering) | Each goal as a pipeline strip. The inbox. Live runs | Open |
| Goal | Phases, the spec, tickets with state, staging, runs, usage | Answer questions |
| Run | Live transcript, stage, evidence bundle with rulings and denials, usage, limits | Cancel. Take the wheel |
| Change | The decision card for one change: what changed, decided for you, not confirmed, independent review, risk | Approve at level 1. Overturn a ruling. Undo. Open on GitHub |
| Inbox | Everything that waits for a person, batched: questions, exceptions, plans waiting for approval, changes at level 1, held tickets with the reason | Answer. Open on GitHub |
| Autonomy | A dial per task with its ceiling, the trust ledger and the attention record beside each | Move a dial. Pause all |
| Tasks | Each task as a card: trigger, reads and writes, next step, where a person re-enters, what stops double work, method | View the file |
| Settings | Repository, harnesses found, the cap, whether an accept token is granted, readiness report | Edit machine settings |

Wherever a control is only advisory on this rung, the app marks it "enforced on OpenAgentix".

The look is the owner's to decide. Mockups come before the web app is planned.

## 11. Architecture

One Go binary, `oxf`.

| Part | Job |
|---|---|
| `api` | Shared types, strict parsing, validation. **Built.** |
| `store` | SQLite: run records shaped like the planned OpenAgentix `AgentRun`, staging states, outcomes, dial settings and their log |
| `githost` | Interface, a GitHub implementation and a fake. Polls, reads, and performs the fixed publish actions with the propose or accept token |
| `runner` | Interface. v0.1: a **local** runner (a process in a worktree) and a fake. Later: container, Kubernetes Job |
| `harness` | Thin adapters that build the command line for `claude`, `codex` and the stub, and read their event streams |
| `governance` | Interface. v0.1: local, with usage as reported and the three usage states |
| `verify` | Diff, protected paths with real glob matching, checks, acceptance checks and criterion states, risk rules, the machine gate, evidence bundle |
| `staging` | The list of passed changes and the combine action: rebuild staging from the default branch and the list, take out what no longer fits, freeze during Accept, Accept and Undo |
| `trajectory` | Course per goal by rules, the per-goal last good point, and the stop and replan with the person on off course |
| `planning` | The live planning conversation in the web app, relayed to a harness session, and the quick-lane spec and ticket |
| `orchestrator` | Polls, matches triggers to tasks, applies the dial, throttles, the cap and claims, drives the three stages |
| `policy` | The permission policy for a run: what is allowed, what is denied and recorded |
| `wheel` | The Model Context Protocol endpoint for planning or taking the wheel in a person's own session: list, claim, ask, submit |
| `server` | JSON API, server-sent events, the embedded web app |
| `web/` | The Svelte app |

Seams for Kubernetes and OpenAgentix, all present in v0.1 as interfaces:

| Seam | v0.1 | Later |
|---|---|---|
| Run record | SQLite | `AgentRun` |
| Runner | Local process | Container, Job, `Workspace` |
| Governance | Reported usage | Gateway key per run, enforced budget, audit |
| Dial ceiling | Repository file | `PlatformPolicy`, enforced |
| Credentials | Propose and accept tokens held by the publish step only | `CredentialGrant` |
| Triggers | Polling, local clock | `Trigger`, `Schedule` |
| Evidence | JSON | Signed provenance |
| Web app | Own binary | Console plugin |

## 12. Releases

Release content and the build order live in one place: [ROADMAP.md](../ROADMAP.md). In short, v0.1 is the quick and standard lanes on one machine, built in this order: the quick lane unattended, the web app, the standard lane, take the wheel. The deep lane, container and Kubernetes runners, deploying and automatic Accept come later.

## 13. Limits and risks, stated plainly

- Whether subscription terms allow unattended runs is not checked. See section 4.
- Local mode has no container. The agent runs as the same user as `oxf` and can read that user's files. See section 8.
- Try it runs code no person has read on the person's machine, in the checks' sandbox.
- Loading an installed plugin for one run and calling its skill by name worked once in a trial. Driving a whole method that way is not tried.
- Subscription rate limits cap how much the factory can do. For a small team that also acts as a brake.
- Building features with agents is the least proven use in the research. Maintenance work is where factories succeed today. Expect rework. The trust ledger exists to show it.
- No research in hand covers teams of two to twenty. That audience is a bet.
- Hosted app builders already serve founders who want a prototype. We serve people who want to own and trust the result.
- Unattended starts of Claude Code and Codex were tried by hand, four runs on tiny tasks. See `docs/trials/2026-11-26-unattended-runs.md`. That shows the mechanism works. It says nothing about how often a real job succeeds.
- An agent can make a wrong ruling. Rulings are shown, and verify and review are outside the agent, but a bad choice that passes the gate reaches staging.
- The whole weight is on the factory's own checking. "Working" is only as strong as the acceptance check behind it. Where the factory could not confirm something it says so and never ticks the line.
- All thresholds (including the claim expiry), the trajectory rules, how well "exercised" can be computed, and staging rebuilds on a large repository are not settled. The first real runs decide them.
- Try it needs a start command from the project. Otherwise the person sees the acceptance list alone.
- This is a large v0.1 for one maintainer. The build order protects against that.

## 14. Settled after a builder's review

A fresh reviewer read revision 1 as the person who must plan it. Its questions were answered as calls. Detail in decision record 0001.

| Question | Call |
|---|---|
| Which compilers does a run have? | Local mode uses what is on the machine. Container mode names a base image in `factory.yaml`. `onboard` proposes the checks |
| How do tasks without a code change write results? | The worker writes files (`questions` in `result.json`, `issues.json`, `review.json`). The publish step acts on them. The worker never calls GitHub |
| Which events trigger runs, and how are loops avoided? | Labels, comments, merges and web app actions, only from users with write access or the factory itself. The factory's own comments carry a marker and never trigger |
| How can `onboard` write `.factory/` if it is protected? | It is the one exception. Configuration is read from the default branch only, so it takes effect after Accept |
| What do `fix-ci` and `review` work on? | `fix-ci`: open agent pull requests only, known by the `factory/` branch prefix and a label, never `factory-staging`. It shares the ticket's attempts with `implement`. `review`: specs, plans, acceptance checks and changes. No fork pull requests in v0.1 |
| Does a limit stop a run? | The deadline always does. A cost limit applies to metered usage, by cancelling, best effort |
| What is demo mode? | A fake git host with a seeded repository and a scripted story. No login or token. Labelled as a demo |
| Who opens the pull requests? | The propose token's user. The docs recommend a separate machine user. A GitHub App comes later |

Kept open on purpose, so later products are not blocked: more than one repository per install, a store interface that another database can back, and an auth seam in the web app.

## 15. Left to the builder

File layout inside packages, SQLite schema detail, API routes, the exact prompt text, the polling method, the exact harness flags, component breakdown of the web app, and the test design, within the rules of this document and the two decision records.
