# 0004. The web app and its read model

- Status: proposed, 2026-11-27. Designed, not built. No screen, route or rule here has been run.
- Scope: the web app of the first release. Records 0001, 0002 and 0003 are the authority. Where this record changes them, "Changes to records 0001, 0002 and 0003" says so, and each was edited to agree.
- **(owner)** marks the owner's call. **(call)** marks a call by the assistant, open to change.

## Context

Record 0001 asks for an outcome view in plain words and an engineering view one step away. The owner approved a design for both: `docs/design/factory-floor-mockup.html`, a mockup with made-up data.

The approved design is the contract for look, words and behaviour. **(owner)** It is a plant control screen. Its home is the Floor: a signal tower with one plain sentence, five gauges, the line with seven numbered stations and work as tags that move, hazard stripes and a reason on a stuck station, goals as lanes, "Needs a person" answered in place, and a shift log. A page of cards with checklists was rejected as looking like a toy: the screen must show a factory at work. **(owner)**

The web app comes forward in the build order. It is built side by side with the rest of the backend, fed first by demo mode driving real factory code. Then each backend piece makes one more station real. **(owner)**

**Where the real app differs from the mockup**, this record wins:

| The mockup | The real app |
|---|---|
| Accept per goal | One Accept for the whole holding area |
| "Stop the line" on Autonomy | "Pause the line", the one name |
| Fonts from a font host | Bundled fonts |
| Reduced motion pauses the whole line | Reduced motion stops decoration only |
| Queued pieces count as moving | Only `working` pieces move |
| The sentence names the first stuck piece | It names the oldest stuck piece |
| Titles inside call sentences | Numbers in sentences; titles in their own element |
| A bespoke consequence sentence | One fixed phrase per risk reason |
| Record shows usage in tokens | Allowed: Record and Runs and changes may name usage units |
| The drawer shows route, rulings, unconfirmed items | Read from `GET /api/v1/pieces/{id}` |
| Team shows a switched-off machine; Live shows a teammate's feed and "Now: <step>" | Another machine is known only from its claim |
| Inline styles; made-up numbers on Plant and Record | Classes only; Plant and Record show a description and no numbers |

## The screens

Nothing is built yet.

| Group | Screen | Status |
|---|---|---|
| Factory | Floor (home) | Designed, not built |
| Factory | Needs you | Designed, not built |
| Factory | Goals | Designed, not built |
| Factory | Live | Designed, not built |
| Factory | Team | Designed, not built |
| Engineering | Runs and changes | Designed, not built |
| Engineering | Autonomy | Designed, not built |
| Organisation | Plant | Designed, not built. A description only in the first release **(call)** |
| Organisation | Record | Designed, not built. A description only in the first release **(call)** |

"Outcome view" in record 0001 means Floor, Needs you and Goals. "Engineering view" means Runs and changes and Autonomy.

The seven stations are a view of the first release from goal to Accept: 01 Plan, 02 Write checks, 03 Build, 04 Check, 05 Second opinion, 06 Holding area, 07 Accepted. They are not the seven phases of the pipeline in the design.

## The read model

`GET /api/v1/floor` returns one snapshot; `GET /api/v1/pieces/{id}` returns one piece in detail. Other screens have their own routes, built the same way.

The snapshot is **computed only, never stored**: one pure function of the run records, staging states, gate decisions, acceptance reports, dial levels and git host reads, tested for purity as the gate is. A stored copy would drift from the run record. Only a journal of what happened keeps new state, for the shift log and live updates.

```ts
type Floor = {
  schemaVersion: 1;
  journalSeq: number;          // in memory, events only; never binds an action
  generatedAt: string;         // server time, RFC 3339 UTC
  hostTime: string | null;     // the git host's time at the last read
  mode: "demo" | "live";
  demo: null | { story: string; note: "made_up_project_fake_host" };
  repository: string;
  machine: { id: MachineId };
  sources: Record<SourceKey, "ok" | "not_built" | "read_failed">;
  signal: {
    lamp: "red" | "amber" | "green" | "off" | "unknown";
    inputs: { moving: number; stuck: number; waitingForPerson: number; inHoldingArea: number;
              oldestStuck: null | { piece: string; station: StationNo; since: string };
              paused: boolean; projectChecksFailing: boolean; unknownSources: SourceKey[] };
  };
  holdingArea: null | { stagingSeq: number; commit: string; published: boolean; frozen: boolean;
                        pendingAccept: null | { since: string; mode: "merge" | "developer";
                                                pullRequest: number } };
  gauges: Gauge[];             // always five
  stations: Station[];         // always seven
  pieces: Piece[];
  goals: Goal[];
  calls: Call[];               // oldest first
  log: JournalEntry[];         // the latest 50
};
type SourceKey = "host" | "store" | "staging" | "gate" | "acceptance" | "trajectory" | "team" | "config";
type StationNo = 1 | 2 | 3 | 4 | 5 | 6 | 7;
type StationKey = "plan" | "write_checks" | "build" | "check" | "second_opinion" | "holding_area" | "accepted";
type Fact = number | ReasonCode | MachineId | StationKey;   // each validated; never a free string
type Gauge = {
  key: "in_work" | "accepted_today" | "right_first_time" | "goal_to_holding" | "attention";
  state: "ok" | "not_enough_yet" | "not_built" | "read_failed";
  value: number | null; samples: number | null; window: "now" | "today" | "7d" | "30d";
  series?: number[];           // accepted per day, last 7 days
};
type Station = { no: StationNo; key: StationKey; who: "person" | "agent" | "factory";
                 source: "real" | "partly_real" | "scripted" | "not_built" };
type PieceState = "working" | "waiting_machine" | "waiting_person" | "stuck" | "ready";
type Piece = {
  id: string;                  // "t57" ticket, "p40" plan of goal 40, "k40" checks of goal 40
  kind: "plan" | "checks" | "ticket";
  goal: number; ticket: number | null; title: Untrusted;
  station: StationNo; state: PieceState;
  reason: null | { code: ReasonCode; attempt?: number; maxAttempts?: number; criteria?: number[] };
  since: string;               // when it entered this state
  blockedBy: string | null;    // the piece it waits on
  machine: null | { id: MachineId; mine: boolean };
  claim: null | { since: string; expires: string };   // the git host's time
  run: string | null; call: string | null;
};
type PieceDetail = {
  piece: Piece;
  route: { station: StationNo; at: string | null }[];   // a time per station where known
  rulings: Untrusted[];
  unconfirmed: Untrusted[];
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
  question: Untrusted | null;  // the agent's own question, for a question only
  facts: Record<string, Fact>; // fills the fixed sentence for this kind
  actions: ActionName[]; version: string;
};
type Untrusted = { text: string; truncated: boolean };
type JournalEntry = { journalSeq: number; time: string; kind: JournalKind;
                      goal?: number; ticket?: number; facts?: Record<string, Fact> };
```

**Three honest states**: "Not built yet", "Could not read", "Not enough yet". None is ever a zero, a default value or a green lamp. A station whose `source` is `not_built` is a dashed, empty bay with "Not built yet", and never a count.

**Clocks.** Claim ages and expiries use the git host's time; local run ages use the server's clock; one subtraction never mixes them. The browser adds only the time since the fetch. **(call)**

**The lamp**, in this order:

1. `unknown` when a source the Floor needs failed to read: "Could not read".
2. `red` when a piece is stuck or the project's own checks fail: "Stuck".
3. `amber` when a call is open: "Needs a person".
4. `green` when a piece is `working` and the line is not paused: "Running".
5. `off` otherwise: "Paused on this machine" or "Idle".

`signal.inputs.moving` counts pieces in state `working` only. A queue with no working piece is not moving.

### Piece states

**Precedence.** A piece with a run in `working`, `verifying` or `publishing` is `working`. Otherwise it is `stuck` if any rule makes it stuck; otherwise `waiting_person`; otherwise `waiting_machine`; otherwise `ready`. The mockup's "queued" is `waiting_machine`, and its "done" is `ready`.

Every reason code maps to exactly one state:

| State | Reason codes |
|---|---|
| stuck | `check_failed`, `protected_path`, `acceptance_regressed`, `review_disagrees`, `person_said_no`, `no_change`, `bad_result`, `deadline`, `pull_request_closed`, `no_process`, `no_review`, `invalid_facts`, `setup_not_finished`, `acceptance_unconfirmed`, `attempts_used_up`, `taken_out`, `untrusted_trigger`, `too_many_cut_offs`, `too_many_claim_lost`, `read_failed`, `unknown` |
| waiting_person | `needs_human`, `question_open`, `consequence_open`, `plan_waits`, `accept_pending`, `project_checks_failing`, `level_off`, `level_guided`, `level_unset`, `goal_not_approved`, `paused` |
| waiting_machine | `cap`, `throttle`, `key_busy`, `staging_frozen`, `machine_busy`, `waiting_dependency`, `waiting_checks`, `waiting_combine`, `cut_off`, `claim_lost`, `claim_expired` |
| ready | none |

- A failed try leaves the piece `stuck` at its station until a later try runs.
- `level_off`, `level_guided`, `level_unset` and `goal_not_approved` wait for anyone trusted on this machine: a person set, or must set, something. `paused` reads "The line is paused on this machine".
- **Waiting on another piece.** With `waiting_dependency` or `waiting_checks` the piece names it in `blockedBy`. When that piece is stuck or waits for a person, this one takes the same state.
- **Another machine's claim.** A ticket claimed by another machine is `working`, with `machine.mine = false` and `claim` from the git host's time. The screen says "claimed by <machine> since <time>" and never shows a step. An expired claim makes it `waiting_machine` with `claim_expired`.
- A run cut off by a restart is `waiting_machine` with `cut_off`; it starts again by itself.
- **Pending Accept.** While an Accept is pending, every piece at 06 is `waiting_person` with `accept_pending`. In developer mode the call says a developer must finish the step.
- **The project's own checks** failing is a line fact, not a piece state. It turns the lamp red, adds the statement "The project's own checks are failing. A developer is needed.", and makes every piece at 03 to 06 `waiting_person` with `project_checks_failing`.
- A piece with `unknown` or `read_failed`, or whose source failed to read, is drawn with "Could not read", never as moving.

### Stations

A goal before approval is one `plan` piece at 01. Its checks are one `checks` piece at 02.

| Station | A ticket is here when |
|---|---|
| 02 | Its goal's acceptance manifest is not yet on staging |
| 03 | It is ready, building or reworking |
| 04 | Its run is `verifying`, or the gate's verify step failed |
| 05 | The gate waits for the review, a consequence is open, another exception holds it (stuck, with its code), or it passed the gate and is not yet on a published staging state (`waiting_combine`). A run that is publishing or succeeded stays here until both are true |
| 06 | It is applied on the latest published staging state and not accepted |
| 07 | It was accepted today |

**Bay state.** The worst piece wins, in this order: stuck, waiting for a person, working, waiting for a machine, ready, empty. A stuck bay shows hazard stripes and its oldest stuck piece's reason in words.

An unknown reason code reads "The factory could not say why", never a pass.

### The five gauges

Every gauge with a threshold needs five cases. **(call)**

| Gauge | Definition | "Not enough yet" when |
|---|---|---|
| In work | Pieces at stations 01 to 06. Below it, the machines: this one, plus others with a live claim | Never; it is a count |
| Accepted today | Changes in Accepts finished since midnight, server time, with the last 7 days as a line | Never; "Not built yet" until staging is built |
| Right first time | Of every ticket whose first attempt ended in 30 days, whatever stage it failed at, the share whose first attempt reached the holding area | Fewer than 5 tickets |
| Goal to holding area | Median time over 30 days from goal approval to the goal's first change on a published staging state | Fewer than 5 goals |
| Your attention | Person actions per goal with an accepted change, over 7 days | Fewer than 5 goals. "Not built yet" until the journal is stored and actions taken directly on the git host are counted |

## Sentences and untrusted text

Every sentence, call text, reason line, log line and status line is built in the web app from one table of fixed phrases, keyed by reason code, call kind or journal kind, filled with `Fact` values only: "Ticket 57: checks failed twice. One try left." It refers to a goal or ticket by its number. It never contains a title or any agent text.

A title may appear only as the label of a tag (cut at 120 characters), of a lane (120) or of the drawer heading (250), each in its own element, beside or beneath the sentence. **(call)** These count as quoted places. Other `Untrusted` text, such as an agent's question or rulings, appears only in a quoted box that says who wrote it. All of it is text, never markup. The server cuts it, shows control and direction characters visibly, and redacts known secrets. No link is made from it; host links are built from the repository and a number.

## Events

`GET /api/v1/events` is a stream of server-sent events read with `fetch`, so the key travels in a header and never in a cookie or a URL query. **(call)** The client reconnects by itself, with back-off.

| Event | Carries |
|---|---|
| `hello` | `journalSeq`, `mode` |
| `journal` | One journal entry; the event id is its `journalSeq` |
| `floor` | `journalSeq`: a hint, merged with others, to fetch the snapshot again |
| `live` | `run`, `time`, `kind`, `text` as `Untrusted`; only with `?topics=live` |
| `reset` | Fetch the snapshot again |
| `: ping` | A comment every 15 seconds |

- **Resume.** The client sends `Last-Event-ID`; an id that is not a number, or older than the kept 1,000 entries, gets `reset`. The client refetches at most every 250 ms and animates a tag whose station changed.
- **No secrets.** Events carry codes, numbers and redacted `Untrusted` text. Never an error's text, check output or a command line.
- **Limits.** 8 streams; a write deadline of 10 seconds; slow readers are dropped; a stream closes after 30 minutes.
- **Journal kinds** **(call)**: `plan_approved`, `checks_written`, `entered_holding_area`, `check_failed`, `taken_out`, `question_asked`, `answered`, `consequence_answered`, `could_not_confirm`, `accepted`, `accept_cancelled`, `undone`, `dial_moved`, `paused`, `resumed`.
- The journal is kept in memory first, and in a store table later. **(call)**

## Actions

**The key rule.** Every `/api/v1/` route needs the login token, reads included, also on the person's own laptop. **(call)** Only static files and `/healthz` do not. The plain screens call the login token "the key".

- The key is 32 random bytes, made fresh in memory at each start and never stored. **(call)**
- When standard error is a terminal, the link `http://127.0.0.1:<port>/#t=<key>` is printed there once. Otherwise nothing with the key is printed: the link is written to a file with mode 0600 in the data directory, and only the file's path is printed. The key never goes to a log file, an event, a response body, an error or the store.
- The page moves the key from the fragment into session storage and clears the address bar.
- Constant-time compare. 429 after 10 requests with a wrong key in a minute, counted per remote address. A request refused for its `Host`, `Origin` or fetch site does not count, and gets no detail.

| Action | Route | The client sends | Written to the git host, so a teammate's machine sees it |
|---|---|---|---|
| Answer a question | `POST /calls/{id}/answer` | `text`, `version` | A comment ending with `<!-- oxf:answer -->`, then removal of `factory:needs-human` |
| Yes or no on a consequence | `POST /calls/{id}/yes`, `/no` | `version` | The consequence answer (below) |
| Approve a plan, quick lane | `POST /calls/{id}/approve` | `version` | The label `factory:approved`, as in record 0003 |
| Accept | `POST /holding-area/accept` | `stagingSeq`, `commit` | Record 0001's Accept: a merge with the accept token, or the developer step |
| Cancel a pending Accept | `POST /holding-area/cancel` | `stagingSeq`, `commit` | The staging freeze ends |
| Undo a change, before Accept | `POST /changes/{id}/undo` | `stagingSeq`, `commit`, `change`, `alsoRemoved` | Staging is rebuilt and pushed |
| Move a dial | `PUT /autonomy/{task}` | `level`, `from` | Nothing: the level is per machine |
| Pause and resume | `POST /line/pause`, `/line/resume` | Nothing | Nothing: a flag on this machine; new runs are held with `paused` |

- **Two numbers.** `journalSeq` is in memory and only orders events. `stagingSeq` is the stored staging state number. Accept and Cancel are refused unless `stagingSeq` and `commit` match the latest published state.
- **Undo** first reads `GET /api/v1/changes/{id}/undo-preview`, which lists the changes that depend on this one and leave with it. The page shows that list to the person and sends it back as `alsoRemoved`. The server refuses if the list it computes differs.
- **Version.** A call's `version` is a hash over exactly what the person was shown and what the action depends on: for a plan, the recorded lines and the body's last edit time on the host; for a question, the question comment id; for a consequence, run, attempt and commit; for an Accept, `stagingSeq` and commit. Before any action the server reads the host again and computes the version again. A difference is refused with "This changed. Look again."
- **Accept is one action for everything in the holding area**, listing each goal inside it. To leave a goal out, undo its changes first. **(call)**
- Accept, Cancel and Undo run only on the machine that holds staging; elsewhere they are disabled.
- A level above the ceiling, level 3 or an unknown task gets 422, and nothing changes. The ceiling stays shared in the repository. **(call)**
- **Pause the line** stops this machine only, and says so. It is the one name on every screen. **(call)**
- **Idempotency.** Every write carries an `Idempotency-Key`, stored `planned` then `done` like a publish step, and writes a journal entry.
- Actions not built are shown disabled with "Not built yet": Try it, Keep planning, Plan a new goal, Undo after Accept. **(call)**

**Answers from the web app** pass the same safe-text function as every other factory comment (no marker, no mention, no closing keyword) and are at most 4 KiB; more gets 413. The `oxf:answer` marker line must be the last line and exact. The text before it is free. The answer counts when its author is a trusted person and the comment is unedited. **(call)** It is posted as the propose token's user, who must be trusted (record 0003).

### Consequences

The gate keeps no state. The machine that ran the gate stores the sealed decision with the run record, and only that machine can apply a person's answer.

The question is posted on the ticket as a factory comment with the run marker, in a code block, as one fixed phrase chosen by the risk reason, so every machine's Floor can show the call:

| Risk reason | Phrase |
|---|---|
| A contract path changed | This changes how other programs talk to yours. Is that what you want? |
| A security path changed | This changes who may do what in your product. Is that what you want? |
| A dependency changed | This changes a part your product is built from. Is that what you want? |
| An existing test was changed or deleted | This changes or removes a check that already existed. Is that what you want? |

Any trusted person may answer, from any machine, with this exact comment ("No, send it back." and `answer=no` for a no): **(call)**

```
Yes, that is what I want.

<!-- oxf:consequence run=<run id> attempt=<n> commit=<full commit id> answer=yes -->
```

It counts only when its author is a trusted person, it is unedited, and run, attempt and commit match the open exception. If more than one valid answer exists, the lowest comment id counts. The machine that holds the decision applies it at its next cycle. If that machine is gone, the call stays open and names the machine that must come back. A no is rework with `person_said_no` and uses an attempt; with no attempt left the goal is off course, as the gate already does.

## Code that must change

Checked against `internal/` on 2026-11-27:

- The fixed label list lacks `factory:approved` and `factory:needs-human`.
- Markers count only from the factory's own user, not from trusted people. `oxf:answer` is recognised only for the factory's own login.
- The earliest claim is picked by creation time, not by lowest comment id. There is no parser for claim releases.
- Edit detection uses an update time that record 0003 lists as unproven.
- `GetLevel` returns autopilot when no level is set.
- Runs have no phase-change time, held reason or end reason.
- Risk reasons are free text with paths, not codes, so a phrase cannot be chosen from them yet.
- Not stored anywhere: the sealed gate decision, the accept time, the pause flag, a goal's approval time, and the lane (quick until the standard lane exists). A goal's course needs trajectory rules, added to the backend order; until then the chip reads "Course: not built yet".

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

Any other licence in the lockfile is listed in `THIRD-PARTY.md` as build-time only, or the package is replaced. No accessibility library with a licence outside the rule. **(call)**

**Fonts** ship as bundled data files under the SIL Open Font Licence 1.1, never fetched. This is an exception to the MIT and Apache-2.0 rule, for fonts only. **(call)**

| Font | Licence | Note |
|---|---|---|
| Barlow Condensed 500, 600, 700 | OFL-1.1 | No reserved name; a Latin subset is allowed |
| IBM Plex Sans 400, 500, 600; IBM Plex Mono 400, 500 | OFL-1.1, reserved name "Plex" | The publisher's own unmodified files; our own subset could not keep the name |

The fonts and the framework's runtime are listed in `THIRD-PARTY.md`. Their licence notices ship with the binary.

**Files in the binary.** The built web files are embedded with `go:embed` and not committed. **(call)** A plain `go build` embeds a placeholder page that says how to build the web app, and the API still works. `make build` and `make test` need Go only. A `go install` user gets the placeholder. This is decided again before the first release.

## The server

**Headers on every response:** `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, and `Cache-Control: no-store` on the API. Never an `Access-Control-Allow-*` header.

**No inline script.** A build step moves the start script into its own file. A test parses the built page and fails on any inline script or `style` attribute.

**Listening.** In the first release the web app listens on loopback only, `127.0.0.1`, on a fixed default port that the machine settings can change. A busy port stops the start; `oxf` never picks another. Any other listen address is refused at start as not built. **(call)** To reach a dedicated machine, a person sets up a tunnel, for example over ssh. `--demo` uses the same rules.

**Request checks**, before routing:

- The `Host` header is exactly `127.0.0.1:<port>`, `localhost:<port>` or `[::1]:<port>`, or 421. This stops a hostile name that resolves to loopback.
- `Origin` is absent or this host. `Sec-Fetch-Site` is absent, `same-origin` or `none`.
- A write needs `Content-Type: application/json`. Bodies are at most 16 KiB, decoded strictly: an unknown or repeated key is refused.
- Ids match a strict pattern, or 404. Errors are fixed codes only.

## Demo marking

`oxf run --demo` (record 0003) serves a made-up project, `demo/booking`, on the fake git host, with no token. Real factory code moves the line, with a small driver in place of the orchestrator until it exists. The demo waits for the person: it never answers or accepts by itself, and does not loop. **(call)**

- A band on every screen: "DEMO. A made-up project on a fake GitHub. The factory code that moves this line is real, except stations marked scripted."
- A DEMO plate, a page title starting with "DEMO", `mode: "demo"` in every response, the chip "demo/booking (fake)", and a "scripted" tag on each scripted station.
- Every printed line starts with `DEMO`. The link, or the path of the link file, is printed within 2 seconds of start.

## Words

The plain screens (Floor, Needs you, Goals, Live, Team, Plant, Record) never use: pull request, merge, diff, review, branch, commit, staging, gate, harness, run id, token, evidence. One exception: Record may name usage units, such as tokens, as Runs and changes may. A quoted place shows what someone else wrote, as written. Runs and changes and Autonomy use engineering words and say so.

| Inside the factory | On the plain screens |
|---|---|
| Staging | Holding area |
| Review | Second opinion |
| Rulings | Decided for you |
| Acceptance criterion | Line |
| Attempt | Try |
| Merged into the default branch | Part of the product |
| Undo, revert | Undo a change, take back |
| Removed on a rebuild | Taken out of the holding area |
| Login token | The key |

## Accessibility

The aim is WCAG 2.2 AA.

- **State is never shown by colour alone.** Each state has an icon shape and a word. A stuck station has hazard stripes and a reason in words.
- **Keyboard.** Every tag is a button. The drawer traps focus, closes on Escape and returns focus. The dial is a radio group.
- **Reduced motion** stops decoration only, the belt and the arrival animation, never the factory.
- **Forced colours** are supported. Lamps and stripes keep an outline and their words.
- **One announcement per change of signal.** The sentence is a polite live region, never one announcement per moving tag.

## Designed, not built

Beyond the screens above: listening on another address, with encryption; Plant and Record with data; Try it; planning (Keep planning, Plan a new goal); Undo after Accept; the goal's course; the attention gauge; a stored journal; Settings and Tasks screens, which the approved design leaves out. Another machine is shown from its claim only, such as "claimed by laptop-b since 12:40", with no progress notes on tickets. **(call)**

## Changes to records 0001, 0002 and 0003

1. **0001, Two views**: mapped to screens as above.
2. **0001, Triggers**: an approval in the web app is the `factory:approved` label, not an answer marker.
3. **0001, Demo mode**: the local runner with the stub harness, not a fake runner; the story waits for the person.
4. **0001, Listening**: loopback only in the first release; every data route needs the login token; another machine is reached by a tunnel the person sets up.
5. **0001, Configuration**: the login token is made fresh at each start and never stored. Machine settings hold the web app's port, not a listen address.
6. **0001, The dial**: the level is per machine; the ceiling stays shared.
7. **0001, Stack**: no utility style framework, CSS variables as tokens, events read with `fetch`, bundled OFL-1.1 fonts.
8. **0002, threats 11 and L5**: the web app listens on loopback only in the first release.
9. **0003, When an agent ends with a question**: a comment ending with the `oxf:answer` marker is an answer when its author is a trusted person and it is unedited.
10. **0003, Run record additions**: `paused` joins the held reasons.
11. **0003, `trigger.json`**: `person_said_no` joins the rework reasons.
12. **0003, Machine settings**: an optional `webPort` field.

## Limits

- **An agent running as the same user can find the key**: in the link file, session storage, browser history or the terminal. With it, it can answer its own question or press Accept. An Accept is still bound to `stagingSeq` and commit and journaled. As record 0002 says, running `oxf` as its own user narrows this.
- **The dial and Pause the line are per machine.** Teammates may run one task at different levels.
- **A consequence waits for the machine that ran the gate.** If it never comes back, the call stays open.
- **The demo is a made-up project** and proves nothing about a real job.
- The journal is in memory at first; a restart loses the shift log.

## Not settled

- The default port number.
- Routes and payloads of the other screens, written down when built.
- Whether the build minifies with an MPL-2.0 tool by default; if so, it is switched off or recorded.
- Whether to commit the built web files before the first release.
