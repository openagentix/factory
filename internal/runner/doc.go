// Package runner is the work stage of a run, on one machine. It turns "do
// this task on this commit" into one unattended run of an installed coding
// agent, and gives back the result and one new commit.
//
// For one run the local runner:
//
//   - makes a fresh run directory (mode 0700) under a data directory the
//     caller names, with the subdirectories workspace, in, out and tmp;
//   - writes the files of the commit into workspace with repo.Export. The
//     workspace is a plain directory with no .git;
//   - writes task.json, prompt.md and trigger.json into in, read-only;
//   - starts the agent with no shell, in its own process group, with the
//     workspace as working directory and an environment built from an
//     allowlist (see below);
//   - reads standard output through the harness stream reader as it comes,
//     hands each event to the caller and records it in its own events file;
//   - keeps standard error in a size-capped log;
//   - ends the whole process group at the deadline (terminating signal,
//     grace time, then kill), and again after the agent exits, so nothing
//     the agent started in the group keeps running;
//   - reads out/result.json only through api.ParseResult. A missing,
//     unreadable or invalid result fails the run. Free text decides nothing;
//   - imports the workspace with repo.Import as one commit on the same
//     parent, and reports the commit and whether anything changed;
//   - returns one Outcome, then removes the run directory, unless the caller
//     asked to keep failed runs.
//
// # Environment
//
// The agent gets PATH, HOME, USER, LOGNAME, LANG, LC_ALL, TERM and SHELL
// from the factory's environment (when set), a TMPDIR inside the run
// directory, and the variables the adapter adds. Nothing else passes. A
// model key variable passes only when the caller says the run is metered and
// names it, and never when its name looks like a git host, credential or SSH
// agent variable. Git host tokens and agent sockets never reach the agent.
//
// # What the runner guarantees
//
// The agent controls every byte under workspace and out, what it prints, and
// what processes it leaves behind. So:
//
//   - result.json is read without following a link, from a real out
//     directory, only if it is a regular file with one hard link, and never
//     over the size limit. A named pipe, a directory or a huge file fails the
//     run and does not hang it or fill memory;
//   - the events file is the runner's own, made new in the run directory,
//     outside out. A file the agent writes at out/events.jsonl is never read;
//   - standard output past a limit is read and thrown away, a single huge
//     line is dropped by the stream reader, standard error is cut at a limit,
//     and the events file is cut at a limit. Memory and disk use of the
//     runner stay bounded;
//   - a workspace that was removed or swapped for a link fails the import,
//     and the run fails with no commit;
//   - inputs are read-only and the runner never reads them back.
//
// # Limits
//
// There is no operating system sandbox here.
//
//   - The agent runs as the same user as the factory. It can read what that
//     user can read, including the home directory. HOME is passed because the
//     harness needs the person's login. It can write outside the workspace
//     too, if the harness allows it.
//   - A process that left the process group, for example with a new session,
//     cannot be reached. The runner notices one case: such a process still
//     holds the output pipes after the run, and a note says so. A process
//     that closed them goes unnoticed.
//   - Files the agent writes under the run directory are not limited in
//     size, except by what Import accepts. A full disk is not prevented.
//   - A swap of a file between a check and its open is narrowed, not ruled
//     out. The group kill uses the leader's process id after it was reaped.
//     The id could in theory be reused by another group. This is very
//     unlikely in the short time involved.
//   - The deadline and the cost limit are best effort. Usage is what the
//     harness reports.
package runner
