// Package acceptance runs the acceptance checks of a goal and turns the
// results into the three states a person sees: working, not working yet and
// could not confirm.
//
// # Layout
//
// Under the acceptance path of the factory file (default "acceptance") each
// goal has one directory, named by the goal number. It holds manifest.json
// and the files the checks need:
//
//	acceptance/40/manifest.json
//	acceptance/40/move_booking_test.go
//
// A check is the command in the manifest entry. It runs with the root of the
// repository as its working directory.
//
// # The acceptance source
//
// The caller names two commits. The judged commit is the work under test.
// The acceptance source is the commit where the reviewed checks landed. The
// manifest is read from the source, never from the judged commit. Before a
// check runs, the whole acceptance path of the judged commit is replaced
// with the acceptance path of the source, so a change that rewrites a check
// has no effect. See Run.
//
// # How a state is set
//
// The state of a criterion comes from the exit code of its check and from
// api.ComputeState, and from nothing else. Output is not read. No text from
// an agent can set a state.
//
// # For the gate's author
//
// Use Compare's regression list, never a test for one state, to decide
// whether something stopped working. A criterion can stop working by going
// to "not working yet" or to "could not confirm". Only a judged report
// (JudgedStates, EvidenceEntries) may feed the gate: a baseline and a run at
// the source are marked and refused there.
//
// A check that cannot run (exit 126 or 127, or a signal other than the
// deadline) on the judged commit is run once at the source. If it runs
// there, the change stopped it and the state is "not working yet". If it
// cannot run there either, the check is broken and the state is "could not
// confirm". Result.Cause records which.
//
// # Limits
//
// These are stated here and again in EngineeringNotes.
//
//   - A check can pass for the wrong reason. A check that already passes
//     before building proves nothing about the work to come: Baseline finds
//     that case, for criteria about new behaviour.
//   - The builder can change the code a check calls so the check passes
//     without the work being done. This package cannot prevent that.
//   - A check is a shell command. A command that reaches outside its copy
//     (an absolute path, "cd /", "..") cannot be made safe by looking at it.
//     This package sets the working directory to the copy and passes the
//     check runner's allowlisted environment with an empty home, and does
//     nothing more. There is no operating system sandbox (see package
//     verify).
//   - Two criteria may name the same command. That is allowed and reported.
//   - Each overlay makes a commit object in the factory repository that no
//     ref points to. The repository's prune removes it later.
//
// # Programs
//
// This package starts no program. Checks go through package verify and git
// goes through package repo.
package acceptance
