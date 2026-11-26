# Trial: unattended runs of Claude Code and Codex

Date: 2026-11-26. Four short runs by hand on one laptop (macOS), before any factory code existed. They test the main assumption of the design: that an installed coding agent can be started with no terminal and nobody watching, under a permission policy, and leave a result a program can read.

Versions: Claude Code 2.1.326, Codex CLI 0.170.0, superpowers plugin 7.0.2.

## Setup

A scratch git repository with two small Python files and one passing test. Each run got a prompt that began with the unattended contract, then a small task ("add a function, tests first, write a result file with this shape"). Standard input was closed. Git host token variables and the agent socket were removed from the environment.

## Run 1: Claude Code, built-in prompt, deny-by-default policy

Flags: `-p`, `--output-format stream-json --verbose`, `--permission-mode dontAsk`, an allowlist with `--allowedTools` (file tools, `python3 -m pytest`, a few git commands), `--add-dir` for the output directory, `--max-budget-usd`, `--no-session-persistence`.

Seen:

- Exit code 0 after 22 seconds and 9 turns. Tests written first, 5 tests pass, one commit.
- A valid result file with two claims, each listing its files, and **one ruling**: the task did not say what counts as a letter, so the agent chose ASCII and wrote down the cost if wrong.
- **One command was outside the policy** (a shell redirect to write a file). It was denied with no prompt, the agent used the file tool instead, and the denial is listed in the final event under `permission_denials`.
- The final event carries turns, duration, token counts and a cost figure.
- The first event carries `apiKeySource`. It said an API key was in use, because one was set in the shell.
- Some read-only shell commands that were not on the allowlist ran anyway. The harness treats them as safe by itself.

## Run 2: Claude Code with a workflow plugin loaded by name

Added `--plugin-dir <path to the installed superpowers plugin>` and `Skill` in the allowlist. The prompt said: use the `superpowers:test-driven-development` skill.

Seen:

- The plugin loaded for that one run, though it is disabled in the user's settings. The agent called the skill by name as its first action.
- One shell command was denied (an in-place edit with `sed`). The agent then concluded that **all** shell use was denied, changed nothing, and ended with status `needs_human` and a question asking for permission.
- So the "stop and write a question" path works as designed: exit code 0, nothing changed, a clear question in the result file. But the stop was wrong. The policy did allow the test command.

## Run 3: Claude Code on a subscription login

The same start with the API key variables removed, and a one-word task.

Seen: exit code 0, `apiKeySource` is `none`, the reply came back. So an unattended start works with a login and no key, and a program can tell which of the two was used. A cost figure is still printed in this case. It is an estimate, not a charge.

## Run 4: Codex CLI

Flags: `exec --json --sandbox workspace-write --add-dir <output directory> --ephemeral -o <file>`.

Seen:

- Exit code 0 after 105 seconds. A valid result file with status `needs_human`.
- **The sandbox blocks writes to git's own files**, so the agent could not stage or commit. It asked for a person to commit.
- The agent picked up the user's own global instructions and tools from the home directory and spent many steps on them.
- The event stream reports tokens only, with no cost figure.

## What this changes in the design

1. **The worker does not commit. The orchestrator does.** It works on every harness, and it takes git out of the permission policy.
2. **The unattended contract must state the policy.** It lists what the run may do, and says that a denied command is denied alone, so the agent tries an allowed way.
3. **Usage state comes from the harness.** For Claude Code, `apiKeySource` of `none` means a subscription plan. No dollar figure is shown for a plan, even though the harness prints one.
4. **A plugin can be loaded per run by path.** The factory does not need the person to enable it globally.
5. **A person's global setup leaks into runs.** It costs tokens and changes behaviour. Whether to shut it out, and how, is open.

## Limits of this trial

- Four runs, one machine, tiny tasks. Nothing here says how often a run succeeds on real work.
- The policy was not attacked. The trial shows that a denial is recorded, not that the policy is safe.
- No run tested Open GSD, a planning step, a long task, or a run cut off part way.
- Whether subscription terms allow unattended runs started by another program is still not checked. The trial shows only that it works.
- Codex was not tried with a different sandbox setting, or with a workflow plugin.
