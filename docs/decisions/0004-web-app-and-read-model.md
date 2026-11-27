# 0004. The web app and its read model

- Status: proposed, 2026-11-27. Designed, not built. No screen, route or rule here has been run.
- Scope: the web app of the first release. Records 0001 and 0003 are the authority. Where this record changes them, "Changes to records 0001 and 0003" says so, and both were edited to agree.
- **(owner)** marks the owner's call. **(call)** marks a call by the assistant, open to change.

## Context

Record 0001 asks for an outcome view in plain words and an engineering view one step away. The owner approved a design for both: `docs/design/factory-floor-mockup.html`, a mockup with made-up data.

The approved design is the contract for look, words and behaviour. **(owner)** It is a plant control screen. Its home is the Floor: a signal tower with one plain sentence, five gauges, and the line with seven numbered stations. Work moves along the line as tags. A stuck station shows hazard stripes and a reason in words. Goals are lanes on the same line. "Needs a person" is answered in place. A shift log says what happened. A page of cards with checklists was rejected as looking like a toy: the screen must show a factory at work. **(owner)**

The web app comes forward in the build order. It is built side by side with the rest of the backend, fed first by demo mode driving real factory code. Then each backend piece makes one more station real. **(owner)**

Where the real app differs from the mockup, this record wins: one Accept for the whole holding area, one name ("Pause the line"), reduced motion that never stops the line, Plant and Record with no numbers, a teammate's machine shown from its claim, bundled fonts, and no inline styles. Every number comes from the read model, or the screen says why not.

## The screens

Nothing is built yet.

| Group | Screen | Shows | Status |
|---|---|---|---|
| Factory | Floor (home) | Tower, sentence, gauges, the line, goal lanes, Needs a person, shift log | Designed, not built |
| Factory | Needs you | Every open call, oldest first, answered in place | Designed, not built |
| Factory | Goals | Each goal, its lane, its course, its lines in three states | Designed, not built |
| Factory | Live | What this machine does now; a teammate's machine from its claim | Designed, not built |
| Factory | Team | Trusted people, their machines, what each holds | Designed, not built |
| Engineering | Runs and changes | Runs, evidence, the decision card | Designed, not built |
| Engineering | Autonomy | A dial per task, its ceiling, its record, Pause the line | Designed, not built |
| Organisation | Plant | Every line in an organisation | Designed, not built. A description only in the first release **(call)** |
| Organisation | Record | Results by kind of change; who decided what | Designed, not built. A description only in the first release **(call)** |

Plant and Record show no numbers, not even as an illustration: made-up numbers break the rule on public claims.

"Outcome view" in record 0001 means Floor, Needs you and Goals. "Engineering view" means Runs and changes and Autonomy.

The seven stations are a view of the first release from goal to Accept: 01 Plan, 02 Write checks, 03 Build, 04 Check, 05 Second opinion, 06 Holding area, 07 Accepted. They are not the seven phases of the pipeline in the design.

## The read model

`GET /api/v1/floor` returns one snapshot. Other screens have their own routes, built the same way.

The snapshot is **computed only, never stored**: one pure function of the run records, staging states, gate decisions, acceptance reports, dial levels and git host reads, tested for purity as the gate is. A stored copy would drift from the run record. Only a journal of what happened keeps new state, for the shift log and live updates.

```ts
type Floor = {
  schemaVersion: 1;
  seq: number;                 // journal position
  generatedAt: string;         // server time, RFC 3339 UTC
  mode: "demo" | "live";
  demo: null | { story: string; note: "made_up_project_fake_host" };
  repository: string;
  machine: { id: string };
  sources: Record<SourceKey, "ok" | "not_built" | "read_failed">;
  signal: {
    lamp: "red" | "amber" | "green" | "off" | "unknown";
    inputs: { moving: number; stuck: number; waitingForPerson: number; inHoldingArea: number;
              oldestStuck: null | { piece: string; station: StationNo; since: string };
              paused: boolean; projectChecksFailing: boolean; unknownSources: SourceKey[] };
  };
  gauges: Gauge[];             // always five, in a fixed order
  stations: Station[];         // always seven
  pieces: Piece[];
  goals: Goal[];
  calls: Call[];               // oldest first
  log: JournalEntry[];         // the latest 50
};
type SourceKey = "host" | "store" | "staging" | "gate" | "acceptance" | "trajectory" | "team" | "config";
type StationNo = 1 | 2 | 3 | 4 | 5 | 6 | 7;
type Gauge = {
  key: "in_work" | "accepted_today" | "right_first_time" | "goal_to_holding" | "attention";
  state: "ok" | "not_enough_yet" | "not_built" | "read_failed";
  value: number | null; samples: number | null; window: "now" | "today" | "7d" | "30d";
  series?: number[];           // accepted per day, last 7 days
};
type Station = {
  no: StationNo;
  key: "plan" | "write_checks" | "build" | "check" | "second_opinion" | "holding_area" | "accepted";
  who: "person" | "agent" | "factory";
  source: "real" | "partly_real" | "scripted" | "not_built";
};
type PieceState = "working" | "waiting_machine" | "waiting_person" | "stuck" | "ready";
type Piece = {
  id: string;                  // "t57" ticket, "p40" plan of goal 40, "k40" checks of goal 40
  kind: "plan" | "checks" | "ticket";
  goal: number; ticket: number | null; title: Untrusted;
  station: StationNo; state: PieceState;
  reason: null | { code: ReasonCode; attempt?: number; maxAttempts?: number;
                   criteria?: number[]; machine?: string };
  since: string;               // when it entered this state
  machine: null | { id: string; mine: boolean };
  run: string | null; call: string | null;
};
type Goal = {
  goal: number; title: Untrusted; lane: "quick" | "standard" | null;
  course: "on_course" | "drifting" | "off_course" | "not_started" | null;  // null: not built
  lines: { id: number; text: Untrusted; state: "working" | "not_working" | "could_not_confirm";
           note: null | { code: ReasonCode; ticket?: number } }[] | null; // null: not built
  linesAsOf: null | { stagingSeq: number };
};
type Call = {
  id: string;                  // stable, for example "q-58-<comment id>"
  kind: "question" | "consequence" | "plan" | "accept" | "statement";
  who: "anyone_trusted" | "this_machine" | "a_developer";
  since: string; goal: number | null; ticket: number | null;
  question: Untrusted | null;
  facts: Record<string, number | string>;  // fills the fixed sentence for this kind
  actions: ActionName[]; version: string;  // sent back with any action
};
type Untrusted = { text: string; truncated: boolean };
type JournalEntry = { seq: number; time: string; kind: JournalKind;
                      goal?: number; ticket?: number; facts?: Record<string, number | string> };
```

**Three honest states.** A source that is not built shows "Not built yet". A source that failed to read shows "Could not read". A gauge with too few cases shows "Not enough yet". None of them is ever a zero, a default value or a green lamp. A station whose `source` is `not_built` is a dashed, empty bay with the words "Not built yet", and never a count.

**Ages** are computed from the server's `generatedAt` plus the time since the fetch, never from the browser's clock. **(call)**

**The lamp**, in this order:

1. `unknown` when a source the Floor needs failed to read: "Could not read".
2. `red` when a piece is stuck or the project's own checks fail: "Stuck".
3. `amber` when a call is open: "Needs a person".
4. `green` when a piece moves and the line is not paused: "Running".
5. `off` otherwise: "Paused on this machine" or "Idle".

**Piece states.** The mockup's "queued" is `waiting_machine`, and its "done" is `ready`.

| State | Rule |
|---|---|
| working | A run for the piece is `working`, `verifying` or `publishing`, or its checks or review run is |
| waiting_machine | `queued`; `held` with `cap`, `throttle`, `key_busy`, `staging_frozen`, `paused`, `level_off` or `level_guided`; the one run at a time is busy; or it waits for a dependency or its goal's checks |
| waiting_person | `needs_human`, an open high-risk consequence, or a plan not yet approved |
| stuck | Its last try failed here and no later try passed; `held` with `read_failed`, `level_unset`, `too_many_cut_offs`, `untrusted_trigger` or `goal_not_approved`; its attempts are used up; or it was taken out of the holding area and the rebuild has not passed |
| ready | Applied on the latest published staging state (06), or accepted (07) |

**Stations.** A goal before approval is one `plan` piece at 01. Its checks are one `checks` piece at 02.

| Station | A ticket is here when |
|---|---|
| 02 | Its goal's acceptance manifest is not yet on staging |
| 03 | It is ready, building or reworking |
| 04 | Its run is `verifying`, or the gate's verify step failed |
| 05 | The gate waits for the review, or a high-risk consequence is open |
| 06 | It is applied on staging and not accepted |
| 07 | It was accepted today |

A bay takes its worst piece's state: stuck, then waiting for a person, then working. A stuck bay shows hazard stripes and its oldest stuck piece's reason in words.

**Reason codes** are the `heldReason` and `endReason` codes of record 0003, as they are, plus `check_failed`, `acceptance_regressed`, `protected_path`, `review_disagrees`, `no_review`, `person_said_no`, `taken_out`, `attempts_used_up`, `question_open`, `consequence_open`, `plan_waits`, `waiting_dependency`, `waiting_checks`, `machine_busy`, `claimed_elsewhere`, `project_checks_failing`, `paused` and `unknown`. An unknown code reads "The factory could not say why", never a pass.

**The five gauges.** Every gauge with a threshold needs five cases. **(call)**

| Gauge | Definition | "Not enough yet" when |
|---|---|---|
| In work | Pieces at stations 01 to 06. Below it, the machines: this one, plus others with a live claim | Never; it is a count |
| Accepted today | Changes in Accepts finished since midnight, server time, with the last 7 days as a line | Never; "Not built yet" until staging is built |
| Right first time | Of changes that reached the gate in 30 days, the share whose first attempt passed | Fewer than 5 changes |
| Goal to holding area | Median time over 30 days from goal approval to the goal's first change on a published staging state | Fewer than 5 goals |
| Your attention | Person actions (answers, yes or no, plan approvals, Accepts, Undos, overturned rulings) per goal with an accepted change, over 7 days | Fewer than 5 goals |

**Missing data.** Not stored anywhere yet: a phase-change time on runs (a run has created, started and finished times only), the accept time, the pause flag, a goal's approval time, and the lane (quick until the standard lane exists). A goal's course needs trajectory rules, now added to the backend order; until then the chip reads "Course: not built yet".

## Sentences

Every sentence on the plain screens is built in the web app from one table of fixed phrases, keyed by reason code, call kind or journal kind, with numbers filled in: "Checks failed twice. One try left." The sentence builder accepts numbers and codes only, by its types.

Text from tickets or agents (`Untrusted`) never enters a sentence. It appears only in a quoted box that says who wrote it, as text, never as markup. The server cuts it, shows control and direction characters visibly, and redacts known secrets. No link is made from it; host links are built from the repository and a number.

## Events

`GET /api/v1/events` is a stream of server-sent events read with `fetch`, so the key travels in a header and never in a cookie or a URL query. **(call)** The client reconnects by itself, with back-off.

| Event | Carries |
|---|---|
| `hello` | `seq`, `mode` |
| `journal` | One journal entry; the event id is its `seq` |
| `floor` | `seq`: a hint, merged with others, to fetch the snapshot again |
| `live` | `run`, `time`, `kind`, `text` as `Untrusted`; only with `?topics=live` |
| `reset` | Fetch the snapshot again |
| `: ping` | A comment every 15 seconds |

- **Resume.** The client sends `Last-Event-ID`. An id that is not a number, or older than the kept 1,000 entries, gets `reset`.
- The client fetches the snapshot again, at most every 250 ms, and animates a tag whose station changed.
- **No secrets.** Events carry codes, numbers and redacted `Untrusted` text. Never an error's text, check output or a command line.
- **Limits.** 8 streams; a write deadline of 10 seconds; slow readers are dropped; a stream closes after 30 minutes.
- **Journal kinds** **(call)**: `plan_approved`, `checks_written`, `entered_holding_area`, `check_failed`, `taken_out`, `question_asked`, `answered`, `consequence_answered`, `could_not_confirm`, `accepted`, `accept_cancelled`, `undone`, `dial_moved`, `paused`, `resumed`.
- The journal is kept in memory first, and in a store table later. **(call)**

## Actions

**The key rule.** Every `/api/v1/` route needs the login token, reads included, on every address, the person's own laptop too. **(call)** Only static files and `/healthz` do not. Other users of a machine can reach loopback, ticket text is private, and a rule with no exceptions is easier to test. The plain screens call the login token "the key".

- **On loopback** the key is 32 random bytes, made fresh in memory at each start and never stored. It is printed once as `http://127.0.0.1:<port>/#t=<key>`. The page moves it from the fragment into session storage and clears the address bar. **(call)**
- **For a service on another address** it is kept in the operating system's credential store. **(call)**
- Constant-time compare; 429 after 10 failures in a minute.

| Action | Route | The client sends | Written to the git host, so a teammate's machine sees it |
|---|---|---|---|
| Answer a question | `POST /calls/{id}/answer` | `text`, `version` | A comment ending with `<!-- oxf:answer -->`, then removal of `factory:needs-human` |
| Yes or no on a consequence | `POST /calls/{id}/yes`, `/no` | `version` | A consequence comment (below) |
| Approve a plan, quick lane | `POST /calls/{id}/approve` | `version`; the goal's lines are unchanged | The label `factory:approved`, as in record 0003 |
| Accept | `POST /holding-area/accept` | `seq`, `commit` | Record 0001's Accept: a merge with the accept token, or the developer step |
| Cancel a pending Accept | `POST /holding-area/cancel` | `seq` | The staging freeze ends |
| Undo a change, before Accept | `POST /changes/{id}/undo` | `seq` | Staging is rebuilt and pushed |
| Move a dial | `PUT /autonomy/{task}` | `level`, `from` | Nothing: the level is per machine |
| Pause and resume | `POST /line/pause`, `/line/resume` | Nothing | Nothing: a flag on this machine; new runs are held with `paused` |

- **Accept is one action for everything in the holding area**, listing each goal inside it. To leave a goal out, undo its changes first. **(call)** It is refused when `seq` or `commit` moved or the state is not published.
- Accept, Cancel and Undo run only on the machine that holds staging; elsewhere they are disabled.
- A level above the ceiling, level 3 or an unknown task gets 422, and nothing changes. The ceiling stays shared in the repository. **(call)**
- **Pause the line** stops this machine only, and says so. It is the one name on every screen. **(call)**
- **Idempotency.** Every write carries an `Idempotency-Key`, stored `planned` then `done` like a publish step, so a double click posts once.
- Text sent to the host passes the git host's safe-text rules: no marker, no mention, no closing keyword. An answer over 4 KiB gets 413.
- Every action writes a journal entry for the shift log.
- Actions not built are shown disabled with "Not built yet": Try it, Keep planning, Plan a new goal, Undo after Accept. **(call)**

**The consequence comment** **(call)**, with "No, send it back." and `answer=no` for a no:

```
Yes, that is what I want.

<!-- oxf:consequence run=<run id> attempt=<n> commit=<full commit id> answer=yes -->
```

The body is exact. It counts only when its author is a trusted person, it is unedited, and run, attempt and commit match the open exception. The machine that holds staging applies it through the gate's person decision. A no is rework with the reason `person_said_no`, and uses an attempt.

**Answers from the web app** end with the `oxf:answer` marker and count when the author is a trusted person and the comment is unedited. **(call)** They are posted as the propose token's user, who must be trusted (record 0003).

## Stack and dependencies

The stack is Svelte 5 with runes, SvelteKit as a static single-page app, Vite and strict TypeScript, installed with pnpm on Node 22, with no Tailwind for now. **(call)** The design's CSS variables are the tokens.

All are `devDependencies`, used at build time, pinned exactly, installed from the lockfile with install scripts off. Only the framework's runtime ships in the binary. **To be confirmed at install time against the lockfile.**

| Package | Version | Licence |
|---|---|---|
| `svelte` | 5.59.1 | MIT |
| `@sveltejs/kit` | 3.4.0 | MIT |
| `@sveltejs/adapter-static` | 4.0.1 | MIT |
| `@sveltejs/vite-plugin-svelte` | 7.3.2 | MIT |
| `vite` | 8.3.8 | MIT |
| `typescript` | 6.0.3 | Apache-2.0 |
| `svelte-check` | 4.7.8 | MIT |
| `vitest` | 5.1.1 | MIT |
| `jsdom` | 31.0.1 | MIT |
| `@testing-library/svelte` | 5.4.2 | MIT |
| `@testing-library/user-event` | 14.6.13 | MIT |
| `@playwright/test` | 1.65.1 | Apache-2.0 |
| `prettier`, `prettier-plugin-svelte` | 3.10.1, 3.5.2 | MIT |
| Left out: `axe-core` | | MPL-2.0; role queries and our own checks instead |
| Left out: `tailwindcss` | | Pulls in an MPL-2.0 build tool |

A build-time package under another licence in the lockfile is listed in `THIRD-PARTY.md` as build-time only, or replaced. No accessibility library with a licence outside the rule. **(call)**

**Fonts** ship as bundled data files under the SIL Open Font Licence 1.1, never fetched from a font host. This is an exception to the MIT and Apache-2.0 rule, for fonts only. **(call)** The licence texts ship beside them. The design's system font stacks are the fallback.

| Font | Licence | Note |
|---|---|---|
| Barlow Condensed 500, 600, 700 | OFL-1.1 | No reserved name; a Latin subset is allowed |
| IBM Plex Sans 400, 500, 600; IBM Plex Mono 400, 500 | OFL-1.1, reserved name "Plex" | The publisher's own unmodified files; our own subset could not keep the name |

**Files in the binary.** The built web files are embedded with `go:embed` and not committed. **(call)** A plain `go build` embeds a placeholder page that says how to build the web app, and the API still works. `make build` and `make test` need Go only. A `go install` user gets the placeholder. This is decided again before the first release.

## The server

**Headers on every response:** `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, and `Cache-Control: no-store` on the API. Never an `Access-Control-Allow-*` header. The built page has no inline script and no `style` attribute; a test reads the embedded page.

**Listening.** `127.0.0.1` on a fixed default port, changeable in the machine settings. A busy port stops the start; `oxf` never picks another. **(call)** Another address needs a listen address in the machine settings and a stored key, or the start is refused. `--demo` always uses loopback.

**Request checks**, before routing:

- The `Host` header must be on an allow-list, or 421. This stops a hostile name that resolves to loopback.
- `Origin` is absent or this host. `Sec-Fetch-Site` is absent, `same-origin` or `none`.
- A write needs `Content-Type: application/json`. Bodies are at most 16 KiB, decoded strictly: an unknown or repeated key is refused.
- Ids match a strict pattern, or 404. Errors are fixed codes only.

## Demo marking

`oxf run --demo` (record 0003) serves a made-up project, `demo/booking`, on the fake git host, with no token. Real factory code moves the line: the local runner with the stub harness, the checks, the gate and staging. A small demo driver stands in for the orchestrator until it exists. The demo waits for the person: it never answers or accepts by itself, and does not loop. **(call)**

- A band on every screen: "DEMO. A made-up project on a fake GitHub. The factory code that moves this line is real, except stations marked scripted."
- A DEMO plate on the rail. The page title starts with "DEMO".
- `mode: "demo"` in every API response. The repository chip reads "demo/booking (fake)".
- A "scripted" tag on each station whose `source` is `scripted`.
- Every printed line starts with `DEMO`. The link is printed within 2 seconds of start.

## Words

The plain screens (Floor, Needs you, Goals, Live, Team, Plant, Record) never use: pull request, merge, diff, review, branch, commit, staging, gate, harness, run id, token, evidence. A quoted box shows what someone else wrote, as written. Runs and changes and Autonomy use engineering words and say so.

| Inside the factory | On the plain screens |
|---|---|
| Staging | Holding area |
| Review | Second opinion |
| Rulings | Decided for you |
| `could_not_confirm` | Could not confirm |
| Acceptance criterion | Line |
| Attempt | Try |
| Merged into the default branch | Part of the product |
| Undo, revert | Undo a change, take back |
| Removed on a rebuild | Taken out of the holding area |
| Login token | The key |
| Task | Kind of job |
| Held with `paused` | Paused on this machine |

## Accessibility

The aim is WCAG 2.2 AA.

- **State is never shown by colour alone.** Each state has an icon shape and a word. A stuck station has hazard stripes and a reason in words.
- **Keyboard.** Every tag is a button. The drawer traps focus, closes on Escape and returns focus. The dial is a radio group.
- **Reduced motion** stops decoration only, the belt and the arrival animation, never the factory. The mockup pauses the whole line; the real app must not.
- **Forced colours** are supported. Lamps and stripes keep an outline and their words.
- **One announcement per change of signal.** The sentence is a polite live region. It speaks when the lamp or sentence changes, never once per moving tag.

## Designed, not built

Beyond the screens above: Plant and Record with data, Try it, planning (Keep planning, Plan a new goal), Undo after Accept, the goal's course, the attention gauge, a stored journal, and Settings and Tasks screens, which the approved design leaves out. A teammate's machine is shown from its claim, such as "working on ticket 55 since 12:40", with no progress notes on tickets. **(call)**

## Changes to records 0001 and 0003

1. **0001, Three moments and two views**: the outcome view is Floor, Needs you and Goals; the engineering view is Runs and changes and Autonomy.
2. **0001, Triggers**: an approval in the web app is the `factory:approved` label, not an answer marker.
3. **0001, Demo mode**: the local runner with the stub harness, not a fake runner; the story waits for the person.
4. **0001, Listening**: every data route needs the login token on every address, localhost too. Record 0002, threats 11 and L5, asks for less, so it still holds.
5. **0001, Configuration**: on loopback the login token is made fresh at each start and never stored; for a service on another address it is in the credential store.
6. **0001, The dial**: the level is per machine; the ceiling stays shared.
7. **0001, Stack**: no utility style framework for now, the design's CSS variables are the tokens, live events are read with `fetch`, and bundled fonts may be under the SIL Open Font Licence 1.1.
8. **0003, When an agent ends with a question**: a comment ending with the `oxf:answer` marker is an answer when its author is a trusted person and it is unedited.
9. **0003, Run record additions**: `paused` joins the held reasons.
10. **0003, `trigger.json`**: `person_said_no` joins the rework reasons.
11. **0003, Machine settings**: an optional `listen` field, an address and port; the default is loopback on the fixed port.

## Limits

- Designed, not built. No rule here is tested by a run.
- **An agent running as the same user can find the key** in session storage, browser history or the terminal. With it, it can answer its own question or press Accept. An Accept is still bound to its `seq` and commit and journaled. As record 0002 says, running `oxf` as its own user narrows this.
- **The dial and Pause the line are per machine.** Two teammates may run one task at different levels, and pausing one machine does not pause another.
- **The demo is a made-up project** on a fake host. It proves nothing about a real job.
- The journal is in memory at first; a restart loses the shift log.

## Not settled

- The default port number, and the credential store account for a stored key.
- Routes and payloads of the other screens, written down when built, under this record's rules.
- Whether the build minifies with an MPL-2.0 tool by default; if so, it is switched off or recorded.
- Whether to commit the built web files before the first release.
- Whether five cases suits every gauge.
- How a stuck bay reads when its pieces have different reasons.
