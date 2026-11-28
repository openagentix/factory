# 0003. Running on one machine and in a team

- Status: proposed, 2026-11-27. Designed. Built and tested, and not used by anything yet: the machine settings file, the token source and the redaction filter. Nothing here has been run.
- Scope: `oxf init` and `oxf run` on one machine, and teammates' machines sharing one GitHub repository, in the first release. Record 0001 is the authority. Where this record changes it, "Changes to record 0001" says so, and 0001 was edited to agree.
- **(call)** marks a call by the assistant, open to change. **(owner)** marks the owner's call.

## Part 1. One machine

### The commands

```
oxf init --repo owner/name
oxf run  --repo owner/name [--once]
oxf run  --demo [--once]
```

- `--repo` is the only repository the factory reads, fetches and writes, and the git remote is built from it. `spec.repository` in `factory.yaml` must be equal in ASCII lowercase, and the propose token must have push permission on it, or nothing runs.
- **`oxf init`** is run once by a person, per machine and data directory. It checks the settings, the tokens and the trusted list. For each task with no level it prints the level it would write, `min(2, spec.autonomy.ceiling)`, and writes them only after the person types yes at a terminal, or with `--yes`. It never changes a level that exists, so a task added later gets a level the same way. If the default branch has no `.factory/`, it prints starter files instead: a `factory.yaml` whose `trustedLogins` holds only the token's own user, read from the host, and task files for `acceptance`, `implement` and `review`. It writes nothing into the repository.
- **`oxf run`** never writes a level. `--once` runs one poll cycle and exits: 0 when it completed, 1 when a read failed or the start was refused, 2 for a usage error. Without `--once` it polls until a signal. On the first interrupt or terminate signal it starts nothing new, lets the run in flight finish its stage and be recorded, within the run's own deadline, and exits. That is not a cut-off. A second signal, a kill, a crash or a power loss cuts the run off.
- `--demo` uses the fake git host, the stub harness, no token and a new temporary data directory. Every printed line starts with `DEMO`.
- No other flags.

**Configuration** comes from the default branch only. Each cycle fetches it into the factory's own repository and reads fixed paths from that commit: `.factory/factory.yaml`, `.factory/tasks/<task>.yaml` and the prompts the tasks name. Never a local file, a working copy or another branch. A failed read or validation starts nothing. The prompts of the `builtin` method ship in the binary and are always available. Task files are never taken from the binary: a repository with no `.factory/` runs nothing, and a missing task file means that task does not run. Until `onboard` is built, a person commits `.factory/` by hand.

**Locks.** One `oxf` process per operating system user. Both commands first take an exclusive, non-blocking lock on `<config dir>/oxf/oxf.lock`, then read the settings, then lock `<dataDir>/oxf.lock`. A second process exits 1, so `oxf init` cannot run while `oxf run` holds the locks. One cycle at a time. **One run at a time per machine** in the first version. **(owner)**

### Machine settings

`<config dir>/oxf/settings.yaml`, where the config dir is what Go's `os.UserConfigDir` returns (`~/Library/Application Support` on macOS, `$XDG_CONFIG_HOME` or `~/.config` on Linux). Written by hand for now.

```yaml
apiVersion: factory.openagentix.org/v1alpha1
kind: MachineSettings
machineId: alice-laptop                  # 1 to 64 of A-Z a-z 0-9 . _ -, unique per login
dataDir: /Users/alice/.local/share/oxf   # absolute
harnesses:                               # one or two; stub is refused
  - { name: claude-code, path: /usr/local/bin/claude }
  - { name: codex, path: /usr/local/bin/codex }
# webPort: <port>                       # optional; 1024 to 65535, the web app's port on 127.0.0.1 (record 0004)
```

- Strict: an unknown or missing field, a duplicate harness or a relative path stops the start.
- The file must belong to the user and not be writable by group or others.
- A value that every machine of a team must share, such as the claim expiry, belongs in `factory.yaml`, not here.

### Tokens

From the operating system's own credential tool, behind one interface, with no new dependency. **(call)**

| System | Read | Store, by hand |
|---|---|---|
| macOS | `/usr/bin/security find-generic-password -s oxf -a <account> -w` | `security add-generic-password -s oxf -a <account> -w`, `-w` last so it prompts |
| Linux | `/usr/bin/secret-tool lookup service oxf account <account>` | `secret-tool store --label=oxf service oxf account <account>`, from standard input |

- The account is `<owner>/<name>/propose` or `<owner>/<name>/accept`. **(call)** The tool path is fixed, so `PATH` is not used. Tests inject a fake tool.
- The propose token is required. The accept token is optional, and its absence is never an error. A missing or failing tool, or a missing propose token, stops the start.
- The token is read from the tool's output. It never appears in an argument list, a log, an event, the database or an error.
- Never from an environment variable, never from a file. No fallback.
- The accept token is refused when it equals the propose token, or when the propose token cannot be read at that moment. One token stored twice would give a worker a credential that can merge.
- The tool gets a fixed, small environment. On Linux it gets no `HOME`.
- On macOS only exit status 44 with no output means "not stored". On Linux exit status 1 with no output and no error text means "not stored". Anything else is a failure.

**Directories.** Every directory above the config directory and the data directory is checked. It must belong to the user or to root. One that others can write to is refused unless it has the sticky bit. One that its group can write to is also refused, with two exceptions: it has the sticky bit, or it belongs to the user and its group is the user's primary group and has the same name as the user. Only the name is checked, not who is in the group. A symbolic link is refused unless root owns it. The config directory and the data directory are never links. `webPort` is written as a plain decimal number with no leading zero.

**Redaction.** The filter is given the token values. It replaces them in text and in streams, also when a value is split across writes. It does not know encoded forms of a token, such as base64. A filter cannot learn a new value after it is made. So this is a rule for the code that will use it, not built yet: a stream is opened only after every token it could carry was read, and when the accept token is read later (record 0002), the filter and its streams are made again.

On macOS the first read after login can show a dialog, which stalls an unattended start. On Linux the tool needs a desktop secret service, which a headless server usually lacks. Headless Linux and other systems are not supported yet.

### `trigger.json`

Written by the orchestrator to `/factory/in/trigger.json`. Never read back.

```json
{
  "schemaVersion": 1,
  "run": "r_01JDQ8Z3K4M5N6P7Q8R9S0T1V2", "task": "implement", "attempt": 2,
  "trigger": { "kind": "issueLabeled", "ref": "#42" },
  "goal": { "number": 40, "criteria": [ { "id": 1, "text": "A customer can move a booking to another day" } ] },
  "ticket": { "number": 42, "title": "Move endpoint", "body": "Let a customer move a booking.", "truncated": false, "criteria": [1] },
  "answers": [ { "question": "Keep the old endpoint?", "answer": "Yes, for one release." } ],
  "rework": { "previousRun": "r_01JDQ7...", "reason": "check_failed",
              "checks": [ { "name": "test", "exitCode": 1, "timedOut": false } ],
              "reviewPoints": [ { "severity": "minor", "text": "The error message is vague", "file": "internal/booking/move.go" } ] }
}
```

- `trigger` is the same as in the run record of 0001.
- Every `title`, `body`, `text`, `question`, `answer` and `reviewPoints[].text` is untrusted. The prompt marks it so. `reviewPoints` has the schema of `points` in `review.json`.
- `body` is cut at 32 KiB, and `truncated` says so. **(call)** `answers` holds only answers that count (Part 2).
- `rework` is `null` on a first attempt. `reason` is one of `check_failed`, `protected_path`, `acceptance_regressed`, `review_disagrees`, `person_said_no` (record 0004), `no_change`, `bad_result`, `deadline`, `pull_request_closed`. It never holds check output.

### Run record additions

| Field | Meaning |
|---|---|
| `id` | Now a time-ordered random id (ULID style) made on the machine, so run ids are unique across machines |
| `heldReason` | Required in `held`, empty otherwise. One of `cap`, `throttle`, `staging_frozen`, `level_unset`, `level_off`, `level_guided`, `read_failed`, `key_busy`, `untrusted_trigger`, `too_many_cut_offs`, `needs_human`, `goal_not_approved`, `paused` (record 0004: Pause the line on this machine) |
| `endReason` | Required in `failed`. A `rework.reason`, or `claim_lost`, `goal_not_approved`, `cut_off` or `no_process` |
| `concurrencyKey` | The resolved key. At most one unfinished run holds a key |
| `input.base` | The full commit id the work was built on |
| `input.subject` | For `review`: `{ run, commit, attempt }` of the subject |
| `claim` | `{ comment, released }`: the claim comment id, and whether its release was posted |
| `startedOnHost` | The host's time when the run started |

At start, before the first cycle:

- A `working` run is cut off. A `verifying` run is verified again from its stored commit. A `publishing` run continues its steps from the record.
- A finished run whose claim was not released is released now.
- A `working` run with no live process while `oxf` kept running ends `failed` with `no_process` and uses an attempt.

### Cut-off runs

A cut-off run still uses no attempt, as 0001 says. A separate limit is added: **three cut-offs per ticket, then `factory:needs-human`.** **(call)** Why: the agent runs as the same user as `oxf` and can stop it, so without a limit each stop is a free try.

- Every cut-off counts, whatever stopped the factory. The factory cannot tell who sent a signal. An ordinary stop is harmless because the first signal lets the run finish its stage (see The commands).
- Three `claim_lost` endings on one ticket hold it the same way.
- The counts are in the store. They reset only when a trusted person removes `factory:needs-human`.
- The cost: a laptop that sleeps often stalls a ticket sooner.

### The dial

- Only `oxf init` writes a first level. A missing or unreadable level means the task is held with `level_unset`. A lost store never means autopilot without a person seeing the levels and confirming them. `store.GetLevel` returns the default level for a missing row today; that must change.
- Level 1 (guided) is the engineering profile by design. **Interim limit:** until a guided output can be approved, a task at level 1 starts no work (`level_guided`).

### Labels

| Label | On | Counts when added by | Means |
|---|---|---|---|
| `factory:needs-human` | A ticket | The factory or a trusted person | The ticket stops for a person. A trusted person removes it to let the factory try again |
| `factory:approved` | A goal | A trusted person | The person confirms the acceptance lines. The plan is approved |
| `factory:ready` | A ticket | Any trusted person's login | The ticket may be claimed. A machine acts as its person |
| `factory:claimed` | A ticket | Anyone | A hint for people and for cheap filtering. It never decides a claim; only markers do |

- The **effective state** of a factory label, `factory:goal` included, is its state after its latest event by a trusted person (Part 2) or the factory, read from the issue's events. An add or removal by anyone else changes nothing for the factory, which restores the label on the host to the effective state at its next cycle.
- The label alone never starts a run. At claim time the claiming machine checks again the dependencies, the acceptance manifest, the cap, and that the goal is still approved.
- Removing `factory:needs-human` resets the cut-off count, never the attempts. When attempts run out, 0001's off-course rule applies.

### Approving a quick-lane goal

Until the web app exists, the goal issue body holds:

```markdown
## How we will know it works

1. A customer can move a booking to another day.
2. The customer gets an email after a move.
```

and a trusted person adds `factory:approved`. Parsing:

- `\r\n` and `\n` are accepted. Nothing else is changed first.
- Exactly one line equals `## How we will know it works`. The section runs to the next line starting with `#`, or the end.
- The section holds only blank lines and one to three lines starting `1. `, `2. `, `3. ` in order, each with 1 to 200 characters of text: no leading or trailing space, no control or format characters, no `<!--`.

The factory reads, in one GraphQL response, the body, its `lastEditedAt` (null means never edited, so the creation time is used) and the label events with actor and time. The goal is approved only when it is open, `factory:goal` and `factory:approved` are in effect, the latest add of `factory:approved` is later than the body's last edit, and the section parses. A failed, partial or capped read means not approved.

- The lines are recorded from that same response, and never rewritten. On approval the factory posts one comment that repeats them exactly: "These are the lines the factory will build to:" and the list.
- Anything else means not approved. The factory posts one fixed comment per add of the label, "These lines could not be read, so the goal is not approved", and starts nothing.
- Removing `factory:approved`, or closing the goal, holds every unfinished ticket of the goal (`goal_not_approved`). Nothing already combined is undone.
- Adding the label again resumes with the recorded lines if the body's lines are unchanged. If they differ, the add is refused with one fixed comment: changed lines need a new goal.
- A run checks that its goal is still approved before each publish step, as it checks its claim. If not, it ends `failed` with `goal_not_approved`, uses no attempt and publishes nothing more.
- The echo comment and every fixed comment pass the same safe-text rule as other factory comments: no mentions, no closing keywords.

### When an agent ends with a question

**(owner)** The run ends `needs_human` and uses an attempt, as 0001 counts work-stage starts. That ticket stops and is labelled `factory:needs-human`. The questions go in one comment on the ticket issue, in a code block, with the run marker. Other work goes on.

The answers are the unedited comments by a trusted person on that ticket after the question comment and before the removal of `factory:needs-human`, read from the question's comment id onward. A comment whose last line is an `oxf:` marker is never an answer, except the `oxf:answer` marker the web app posts: that comment is an answer when its author is a trusted person and it is unedited (record 0004). Each one is one `answers` entry, with `question` set to the text of the question comment. If the label is removed and no answer remains, the factory puts it back with one fixed comment, "No answer was found", and starts nothing.

### Publish steps

Before each side effect the step is stored `planned`, with its slot. After it, `done`, with the host's id for what it made. Never `done` without one. The store is the main guard against a second post. On resume a `planned` step is done again:

- Push `factory/<run id>`: the recorded commit again.
- Pull request: update the open one for that branch. If a recorded one is now closed, the run ends `failed` with `pull_request_closed`, uses the attempt, and the ticket takes its normal rework path.
- Comment: the last line is `<!-- oxf:run id=<run id>.<slot> -->`. After a crash between the post and the record, the factory searches its own comments for that marker, from the run's `startedOnHost` onward.
- Label: repeating it changes nothing.

An answer posted from the web app has no run slot. It is found again by its exact body, by the propose token's user and unedited, after the question comment (record 0004).

Before each step the run checks its claim (Part 2). If the claim has expired or was lost, the run publishes nothing more. It ends `failed` with `claim_lost`, uses no attempt and releases. A branch already pushed is left, and named in the run record.

### The evidence bundle

- `worker` comes from the adapter the orchestrator started: its name, the version it read from the program, the model from the task file. `usage` comes from what the adapter read from the harness's event stream, with `source: harness`.
- `harness` and `usage` in the agent's `result.json` are its own report, never used as fact.
- The stub harness is refused outside tests and demo mode. A task file naming `stub` stops the start.

### The acceptance list

Designed, not built. Until the web app exists, one comment on the goal issue, updated after each staging state:

```markdown
How we will know it works, as of the latest version you can try:

1. A customer can move a booking to another day: Working
2. The customer gets an email after a move: Not working yet

<!-- oxf:run id=goal-40.list -->
```

Three states only: Working, Not working yet, Could not confirm. Plain words, never pull request, merge, diff, review, branch or commit. The factory finds the comment by its stored id.

## Part 2. A team

### Trusted logins

```yaml
spec:
  trustedLogins:              # required, 1 to 20
    - { login: alice, id: 1001 }
    - { login: bob, id: 1002 }
  claimExpiry: 2h             # default 2h
  lockTickets: true           # default true
```

- A missing or empty list is a validation error that stops the start. Nobody acts. Never everyone, never the token's own user.
- `login` is 1 to 39 ASCII letters, digits and `-`, with no `-` at either end and no `--`. `id` is the account's numeric id, above 0. No duplicate login or id. Logins are compared in ASCII lowercase, never with Unicode case folding.
- Bots and placeholders are refused: any login with `[bot]`, and `ghost`. At start the factory reads each id from the host. A login that does not match its id, or an account that is not a user, stops the start.
- The token's own user must be on the list, by id, or the start is refused.
- `onboard` may change `.factory/`, as 0001 says, but never `spec.trustedLogins`, `spec.claimExpiry` or `spec.lockTickets`. A change to those by any agent run fails verify. Only a person changes them, and a change takes effect only after Accept.

A **trusted person** is on the list, matched by id, and has write permission.

### Which comments count

A marker, an answer or an approval counts only when its author is a trusted person and it was not edited. A marker's body must also be exact. Anyone else's marker is plain text. An answer or approval from someone with write access who is not on the list is shown as context and does not count. **(owner)**

Edits are judged by GraphQL `lastEditedAt` of each candidate comment, read in one query. It is meant to change on body edits only. If a real run shows that a reaction or hiding a comment changes it, markers move to a hard lock, such as a ticket branch pushed without force, before team mode is used on a public repository. Until a recorded trial confirms it, team mode on a public repository is not proven, and `oxf run` on a public repository with more than one trusted login prints a standing warning at start: markers may be voidable by outsiders.

### Claims

```
Claimed by a factory run.

<!-- oxf:claim machine=<machine id> run=<run id> -->
```

```
Claim released by a factory run.

<!-- oxf:release machine=<machine id> run=<run id> -->
```

- Exact bodies, single ASCII spaces. An extra field, such as a time, is not a marker.
- A claim is **live** while it counts, its creation time plus `claimExpiry` is after the host's time, and no later release by the same login names its machine and run.
- **The lowest comment id among live claims wins.** Creation time is used only for expiry.
- After posting, the factory reads, waits five seconds and reads again. **(call)** It has won only when both reads agree and the winning marker names its own machine id and its own run id, and its author is its own user. A second machine of one login with the same `machineId` is that person's error; the run id still stops a double build.
- **The loser releases at once**, or its dead claim would win after the winner releases.
- **Every run releases its claim when it ends**, whatever the outcome. A release that fails is retried at the next start, from the run record.
- A run checks its claim again before work and before each publish step.
- Its own claim missing from a read is an error, never "free".
- Only the claimer's own login may release. One teammate cannot free another's ticket yet; a crashed machine's claim ends by expiry.
- The staging claim (not built) will follow the same rules, on the staging pull request, and its conversation is locked the same way.

**Expiry.** `claimExpiry` is in `factory.yaml`, so every machine agrees. Default 2 hours. **(owner)** It must exceed the longest task deadline plus 30 minutes, for verify and publish, or validation fails. Time is the host's: the `Date` header of the list response, and a missing header is an error. A time inside a marker is never read, and the local clock alone is never used.

### Reading markers

Comments are read ascending, with `since` at the host's time minus `claimExpiry` minus five minutes. A permission call is made only for authors on the list, at most 20, cached up to five minutes. A read that hits the page or size cap is an error: the ticket is blocked, never built twice.

The cost: the host has no author filter, so every comment in the window is downloaded. A flood costs rate limit and can block a ticket until it is locked.

### Locked conversations

With `lockTickets` (on by default; `false` turns it off), on a public repository the factory locks each ticket it creates, and the goal issue once it is approved. Then only collaborators with write access can comment. A failed lock call holds the ticket or the approval; the factory never goes on unlocked. This stops floods and hidden text from people without write access. It does not stop writers who are not on the list, edits of old comments, or reactions, and outsiders lose a place to comment.

### Commits and logins

A commit a teammate's machine makes is signed off in that teammate's own name, from their own git configuration. No machine signs off for another person. No login or token leaves a machine.

## Changes to record 0001

1. **Triggers**: an event counts only from a trusted person or the factory, not from any writer. This resolves the contradiction with "Team work".
2. **Claims**: lowest comment id wins, not the earliest marker, and the marker must name this machine and run. Every run releases its claim. `factory:claimed` is a hint only. The expiry moves to `factory.yaml`, default 2 hours. The staging claim follows the same rules.
3. **`maxAttempts`**: three cut-offs, or three `claim_lost` endings, per ticket stop it.
4. **Questions**: the ticket stops until a trusted person answers on the ticket and removes `factory:needs-human`. No run waits.
5. **The dial**: only `oxf init` writes a first level; a missing level holds the task. Level 1 starts nothing for now.
6. **Run record**: run ids are unique across machines, new fields, and `held` and `failed` have reason codes.
7. **Worker contract**: `worker` and `usage` in the evidence never come from `result.json`.
8. **Propose token**: it may also update its own comments and lock the conversations named above.
9. **Configuration**: task files never come from the binary; a repository with no `.factory/` runs nothing. The `factory.yaml` example has the new fields.
10. **Protected paths**: no agent run, `onboard` included, may change `spec.trustedLogins`, `spec.claimExpiry` or `spec.lockTickets`.

## Limits

- Designed. Built and tested so far: the machine settings file with its locks, the token source and the redaction filter. Nothing uses them yet, and no rule here is tested by a run on a real repository. The token source has been tested with a fake tool only, never with the real credential tool.
- On Linux, "not stored" is told from a failure only by exit status 1 with no output. Whether a dismissed unlock prompt looks the same is not checked. If it does, it reads as "no accept token", which gives less power.
- A directory above the settings or the data that a shared group can write to is refused. The message names the directory. A user known only through a network directory service, in a build without cgo, is treated the same way, because the group's name cannot be looked up.
- The guard test that keeps these packages from reading the environment or starting other programs is a source scan. It does not catch everything.
- One `oxf` per user and one run at a time: work is serial, one repository per machine.
- **A hostile agent running as the same user can read the propose token through the credential tool.** With it, it can do anything the token's trusted login can do on the host: approve, answer, claim, release, add or remove a label. It could also run `oxf init` itself. Running `oxf` under its own operating system user, or on a dedicated machine, narrows this. Neither is built. Later **(call)**: a dedicated factory account for the token, which is not itself trusted for approvals and answers.
- The cut-off limit holds against an agent that stops the factory. It does not hold against one that also takes the token, because it can remove the label as the token's user. Deleting the store also loses the count.
- The factory cannot tell who sent a signal. One signal from an agent only ends the factory after the run in flight finishes its stage. Two signals or a kill from an agent cut the run off, and that counts toward the three.
- Writers not on the list can edit or delete comments and cause wasted work. They cannot win a claim.
- Removing a login from the list does not revoke its token. The permission cache adds up to five minutes.
- A trusted person's own typed or pasted marker counts, because the person and their machine share a login.
- A dead loser that never released blocks the ticket until its claim expires. When `claimExpiry` is lowered, machines may disagree for one old expiry period and double work is possible; the check before each publish step still stops a double publish.
- A lost store can post a second acceptance-list comment.
- A machine's sign-off says its person takes responsibility. Whether that meets a project's own sign-off rules is for that project.
- **Reads of the git host built so far (`internal/githost`), not yet used.** Who last put a label on an issue is read from the issue's events, in a second request after the issue itself, not in the one response this record asks for. The answer is "unknown" when the label is not on the issue, when the latest event is a removal, when events are out of order or cut short, and when the actor is `ghost` or a `[bot]` login. Label names are compared in ASCII lowercase only. The read also says whether the actor can write to the repository or is the factory's own user. That is not the list of trusted logins: the caller must check the list.
- **The factory's own pull requests are found by author, branch prefix, base and the head repository's name**, not by a label and not by the host's fork flag. A list that is cut short means the count is unknown, and the caller must hold. A pull request closed while the list is read can be missed or seen twice.
- **A repository that was renamed or moved** is noticed from the names in the host's answers and stops these reads. One case is not noticed: between the two requests that read the default branch, the repository is renamed and a new one is made at the old name.
- None of these reads has run against a real host. What the host sends for a label put on when an issue is made, for a forked repository and for a bot's permission is not confirmed.

## Not settled

- Whether a reaction, hiding a comment, locking or a transfer changes `lastEditedAt`, and whether the GraphQL read of body, edit time and label events behaves as described. To be confirmed by a real run before team mode is merged.
- Whether comment ids are strictly ordered by posting time. This record assumes so.
- A second hard lock, such as a ticket branch pushed without force.
- Signed markers, with a key per machine.
- The five-second settle time and the 32 KiB cut.
- A trial of the edit-time behaviour will be recorded under `docs/trials/` before team mode is called working.

## History

On 2026-11-27 record 0004 changed these rules: an `oxf:answer` comment from a trusted person, unedited, is an answer; `paused` is a held reason; `person_said_no` is a rework reason; machine settings have an optional `webPort` field.

On 2026-11-28 the Limits gained the git host reads built for the orchestrator: label actor, the factory's own pull requests, the default branch.
