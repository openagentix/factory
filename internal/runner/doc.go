// Package runner is the work stage of a run, on one machine. It turns "do
// this task on this commit" into one unattended run of an installed coding
// agent, and gives back the result and one new commit.
//
// For one run the local runner:
//
//   - makes a fresh run directory (mode 0700) in DataDir/runs, a folder of
//     mode 0700, under a long random name, with the subdirectories
//     workspace, in, out and tmp. It opens the run directory, and then the
//     workspace, once each, as handles, before the agent starts. It reads,
//     writes, imports and removes only through those handles;
//   - writes the files of the commit into the workspace with repo.ExportRoot,
//     through the workspace handle. The workspace is a plain directory with
//     no .git;
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
//   - imports the workspace with repo.ImportRoot, through the workspace
//     handle, as one commit on the same parent, and reports the commit and whether anything changed;
//   - returns one Outcome, then removes the run directory, unless the caller
//     asked to keep failed runs. The Outcome has one Status, decided from the
//     result, the exit code, the import and the flags together. Its zero
//     value means failed. A result that contradicts the files (no_change
//     with edits) fails the run, and a failed or needs_human run is never
//     imported, so it leaves no commit.
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
//   - the import reads only through the workspace handle, opened before the
//     agent started. The agent can rename its run directory or its workspace
//     and put a link or another directory at the name, in a loop, during the
//     import. What the runner reads does not change: it is the directory it
//     opened, never what the name points to now. Files of another run, or of
//     any other directory, cannot get into the commit this way. The tests
//     race such swaps hundreds of times and count the leaks;
//   - the runner also checks, through handles and never through a path, that
//     the run directory's name still names the run directory and the
//     workspace's name still names the held workspace, before and after the
//     import. A workspace that was removed, moved or replaced, or a handle
//     that is closed or missing, fails the run with no commit. An agent that
//     renames its workspace away and back during the import is not stopped:
//     the import is still of the held directory, so it is still the agent's
//     own files;
//   - links inside the workspace are never followed by the import;
//   - inputs are read-only and the runner never reads them back;
//   - result.json and the runner's own files are reached through the run
//     directory handle. Cleanup empties the directory through the handle,
//     removes a link left at the name and a moved copy in the runs folder, and
//     reports a copy it cannot find. The only paths the runner still looks at
//     after the agent starts are named under Limits;
//   - the kept events and stderr paths of a failed run (Outcome.EventsPath and
//     Outcome.StderrPath) are untrusted: the agent can replace them with a
//     link. The runner never reads them back and adds a note when one is not
//     a regular file. A caller must open them without following links;
//   - every file and handle a run opens is closed when the run ends, on
//     every path, without waiting for the garbage collector;
//   - a callback that panics fails the run and ends the group, and a slow
//     one loses events instead of holding up the reading.
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
//     cannot be reached. The runner notices such a process when it still
//     holds the output pipes after the run. On Linux it also looks in /proc
//     for processes that use the run directory, and lists them without
//     stopping them. On other systems it cannot look, and every outcome says
//     so in a note.
//   - After the agent starts, the runner looks at two paths only. It looks at
//     the run directory's path once, for the note that says where a kept run
//     is. On Linux it also looks in /proc for strays, by the run directory's
//     resolved path. Both feed notes and decide nothing. A swap can make a
//     note wrong. The agent's own working directory and the paths in its
//     arguments are names given to it at the start.
//   - A file with more than one hard link is refused at import. A hard link
//     whose other name was removed before the import is imported like any
//     file. The agent could copy the file anyway, because it runs as the same
//     user.
//   - The read-only modes on the input directory are advice. The same user
//     can change them and root ignores them. The runner never reads the
//     inputs back.
//   - The runs folder keeps other users out. The same user, so the agent,
//     can still list it.
//   - Files the agent writes under the run directory are not limited in
//     size, except by what Import accepts. A full disk is not prevented.
//   - A swap of a file between a check and its open is narrowed, not ruled
//     out. The group kill uses the leader's process id after it was reaped.
//     The id could in theory be reused by another group. This is very
//     unlikely in the short time involved.
//   - The deadline and the cost limit are best effort. Usage is what the
//     harness reports.
package runner
