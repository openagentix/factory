# 0003. Running on one machine and in a team

- Status: proposed, 2026-11-27. Designed, not built. Nothing here has been run.
- Scope: `oxf run` on one machine, and teammates' machines sharing one GitHub repository, in the first release. Record 0001 is the authority. Where this record changes it, "Changes to record 0001" says so, and 0001 was edited to agree.
- **(call)** marks a call by the assistant, open to change. **(owner)** marks the owner's call.

## Part 1. One machine

### The command

```
oxf run --repo owner/name [--once]
oxf run --demo [--once]
```

- `--repo` is required outside demo mode. It is the only repository the factory reads, fetches and writes, and the git remote is built from it. `spec.repository` in `factory.yaml` must be equal in ASCII lowercase, and the propose token must have push permission on it, or nothing runs.
- `--once` runs one poll cycle and exits: 0 when it completed, 1 when a read failed or the start was refused, 2 for a usage error.
- `--demo` uses the fake git host, the stub harness, no token and a new temporary data directory. Every printed line starts with `DEMO`.
- No other flags. Without `--once` it polls until a signal, then starts nothing new, cancels the run in flight, which is cut off, and exits.

**Configuration** comes from the default branch only. Each cycle fetches it into the factory's own repository and reads fixed paths from that commit: `.factory/factory.yaml`, `.factory/tasks/<task>.yaml` and the prompts the tasks name. Never a local file, a working copy or another branch. A failed read or validation starts nothing. A missing task file means that task does not run; `oxf run` never falls back to the defaults in the binary. **(call)**

**One `oxf run` per data directory**, by an exclusive non-blocking lock on `<dataDir>/oxf.lock`, taken before any other read. A second process exits 1. One cycle at a time. **One run at a time per machine** in the first version. **(owner)**

### Machine settings

`<config dir>/oxf/settings.yaml`, where the config dir is what Go's `os.UserConfigDir` returns (`~/Library/Application Support` on macOS, `$XDG_CONFIG_HOME` or `~/.config` on Linux). Written by hand for now.

```yaml
apiVersion: factory.openagentix.org/v1alpha1
kind: MachineSettings
machineId: alice-laptop                  # 1 to 64 of A-Z a-z 0-9 . _ -
dataDir: /Users/alice/.local/share/oxf   # absolute
harnesses:                               # one or two; stub is refused
  - { name: claude-code, path: /usr/local/bin/claude }
  - { name: codex, path: /usr/local/bin/codex }
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
- The token is read from the tool's output. It never appears in an argument list, a log, an event, the database or an error.
- Never from an environment variable, never from a file. No fallback. A missing or failing tool stops the start.

Limits: any process of the same user, the agent included, can run the same read command. On macOS the first read after login can show a dialog, which stalls an unattended start. On Linux the tool needs a desktop secret service, which a headless server usually lacks. Headless Linux and other systems are not supported yet.

### `trigger.json`

Written by the orchestrator to `/factory/in/trigger.json`. Never read back.

```json
{
  "schemaVersion": 1,
  "run": "r_01H8Z", "task": "implement", "attempt": 2,
  "trigger": { "kind": "issueLabeled", "ref": "#42" },
  "goal": { "number": 40, "criteria": [ { "id": 1, "text": "A customer can move a booking to another day" } ] },
  "ticket": { "number": 42, "title": "Move endpoint", "body": "Let a customer move a booking.", "truncated": false, "criteria": [1] },
  "answers": [ { "question": "Keep the old endpoint?", "answer": "Yes, for one release." } ],
  "rework": { "previousRun": "r_01H8Y", "reason": "check_failed",
              "checks": [ { "name": "test", "exitCode": 1, "timedOut": false } ], "reviewPoints": [] }
}
```

- Every `title`, `body`, `text`, `question` and `answer` is untrusted. The prompt marks it so.
- `body` is cut at 32 KiB, and `truncated` says so. **(call)** `answers` holds only answers that count (Part 2).
- `rework` is `null` on a first attempt. `reason` is one of `check_failed`, `protected_path`, `acceptance_regressed`, `review_disagrees`, `no_change`, `bad_result`, `deadline`. It never holds check output.

### Run record additions

| Field | Meaning |
|---|---|
| `heldReason` | Required in `held`, empty otherwise. One of `cap`, `throttle`, `staging_frozen`, `level_unset`, `level_off`, `level_guided`, `read_failed`, `key_busy`, `untrusted_trigger`, `too_many_cut_offs`, `needs_human` |
| `concurrencyKey` | The resolved key. At most one unfinished run holds a key |
| `input.base` | The full commit id the work was built on |
| `input.subject` | For `review`: `{ run, commit, attempt }` of the subject |
| `cutOff` | True when the run was cut off. The phase is then `failed` |

At start, before the first cycle: a `working` run is cut off; a `verifying` run is verified again from its stored commit; a `publishing` run continues its steps from the record. A `working` run with no live process while `oxf` kept running is failed and uses an attempt.

### Cut-off runs

A cut-off run still uses no attempt, as 0001 says. A separate limit is added: **three cut-offs per ticket, then `factory:needs-human`.** **(call)** Why: the agent runs as the same user as `oxf` and can stop it, so without a limit each stop is a free try. The cost: a laptop that sleeps often stalls a ticket sooner.

### The dial

- On first start, `oxf run` writes level 2 for each task it loads, logs it as "set at setup" and prints it. **(call)** After that, a missing or unreadable level means do not run. A lost database never means autopilot.
- **Interim limit:** at level 1 (guided) no work starts until there is a way to approve a guided output, so guided work cannot pile up that nobody can release.

### Labels

| Label | On | Set by | Means |
|---|---|---|---|
| `factory:needs-human` | A ticket | The factory, or a trusted person | The ticket stops for a person. A trusted person removes it to let the factory try again |
| `factory:approved` | A goal | A trusted person | The person confirms the acceptance lines. The plan is approved |

- A trusted person is defined in Part 2.
- A label change counts only when its latest event came from the factory's own user or a trusted person. The actor is read from the issue's events. `factory:ready` counts only when the factory added it.
- Removing `factory:needs-human` resets the cut-off count, never the attempts.

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
- The issue is open, `factory:goal` and `factory:approved` were both set by a trusted person, and the body was not edited after the label was added, by the host's times.

Anything else means not approved. The factory posts one fixed comment, "These lines could not be read, so the goal is not approved", and starts nothing. Once approved, the lines are recorded, and later edits change nothing for this goal.

### When an agent ends with a question

**(owner)** The run ends `needs_human` and has used an attempt. That ticket stops and is labelled `factory:needs-human`. The questions go in one comment on the goal issue, in a code block. Other work goes on. A trusted person answers in a comment and removes the label. The next run gets both in `answers`.

### Publish steps

Before each side effect the step is stored `planned`, with its slot. After it, `done`, with the host's reference. Never `done` without one. On resume a `planned` step is done again:

- Push `factory/<run id>`: the recorded commit again.
- Pull request: update the open one for that branch. A recorded one that is now closed is never reopened; the run ends as an exception.
- Comment: the last line is `<!-- oxf:run id=<run id>.<slot> -->`. The factory searches its own comments for it before posting.
- Label: repeating it changes nothing.

A run checks its claim before each step (Part 2).

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

Three states only: Working, Not working yet, Could not confirm. Plain words, never pull request, merge, diff, review, branch or commit. The factory finds the comment by its stored id, and posts a new one if that is lost.

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

- A missing or empty list means nobody acts. Never everyone, never the token's own user.
- `login` is 1 to 39 ASCII letters, digits and `-`, with no `-` at either end and no `--`. `id` is the account's numeric id, above 0. No duplicate login or id. Logins are compared in ASCII lowercase, never with Unicode case folding.
- Bots and placeholders are refused: any login with `[bot]`, and `ghost`. At start the factory reads each id from the host. A login that does not match its id, or an account that is not a user, stops the start.
- The token's own user must be on the list, by id, or `oxf run` refuses to start.
- The list is under `.factory/`, so any agent run that changes it fails, `onboard` included. **(call)**

A **trusted person** is on the list, matched by id, and has write permission.

### Which comments count

A marker, an answer or an approval counts only when its author is a trusted person and the comment was not edited after it was created. A marker's body must also be exact. An answer is a comment on the goal issue after the factory's question.

Anyone else's marker is plain text. An answer or approval from someone with write access who is not on the list is shown as context and does not count. **(owner)**

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
- After posting, the factory reads, waits five seconds and reads again. **(call)** It has won only when both reads agree and the winner's author is its own user.
- **The loser releases at once**, or its dead claim would win after the winner releases.
- A run checks its claim again before work and before each publish step. Lost or expired: it stops with nothing published.
- Its own claim missing from a read is an error, never "free". If a read fails after posting, it tries one release.
- Only the claimer's own login may release. One teammate cannot free another's ticket yet; a crashed machine's claim ends by expiry.

**Expiry.** `claimExpiry` is in `factory.yaml`, so every machine agrees. Default 2 hours. **(owner)** It must exceed the longest task deadline plus ten minutes, or validation fails. Time is the host's: the `Date` header of the list response, and a missing header is an error. A time inside a marker is never read, and the local clock alone is never used.

### Reading markers

Comments are read ascending, with `since` at the host's time minus `claimExpiry` minus five minutes. A permission call is made only for authors on the list, at most 20, cached up to five minutes. A read that hits the page or size cap is an error: the ticket is blocked, never built twice.

The honest cost: the host has no author filter, so every comment in the window is downloaded. A flood costs rate limit and can block a ticket until it is locked.

### Locked tickets

On a public repository the factory locks each ticket it creates, so only collaborators with write access can comment. `lockTickets: false` turns this off. It stops floods and hidden text from people without write access on factory tickets. It does not stop writers who are not on the list, edits of old comments or reactions, and it does not cover the goal issue. Outsiders lose a place to comment.

### Commits and logins

A commit a teammate's machine makes is signed off in that teammate's own name, from their own git configuration. No machine signs off for another person. No login or token leaves a machine.

## Changes to record 0001

1. **Triggers**: an event counts only from a trusted person or the factory, not from any writer. This resolves the contradiction with "Team work".
2. **Claims**: lowest comment id wins, not the earliest marker. The expiry moves to `factory.yaml`, default 2 hours.
3. **`maxAttempts`**: three cut-offs per ticket stop it.
4. **Questions**: the ticket stops until a person answers. No run waits.
5. **The dial**: a missing level means do not run. Level 1 starts nothing for now.
6. **Run record**: new fields, and `held` has a reason code.
7. **Worker contract**: `worker` and `usage` in the evidence never come from `result.json`.
8. **Propose token**: it may also update its own comments and lock a factory ticket.

## Limits

- Designed, not built. No rule here is tested by a run.
- One `oxf run` per user and one run at a time: work is serial, one repository per machine.
- A same-user agent can read the tokens through the credential tool, and can stop `oxf`. The cut-off limit bounds only the second.
- Writers not on the list can edit or delete comments and cause wasted work. They cannot win a claim.
- Removing a login from the list does not revoke its token. The permission cache adds up to five minutes.
- A trusted person's own typed or pasted marker counts, because the person and their machine share a login.
- A dead loser that never released blocks the ticket until its claim expires.
- A machine's sign-off says its person takes responsibility. Whether that meets a project's own sign-off rules is for that project.

## Not settled

- Whether a reaction, hiding a comment, locking or a transfer changes an edit time on the host. If it does, anyone could void a marker or an approval.
- Whether comment ids are strictly ordered by posting time. This record assumes so.
- A second hard lock, such as a ticket branch pushed without force.
- Signed markers, with a key per machine.
- The five-second settle time and the 32 KiB cut.
