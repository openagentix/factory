// Package staging builds, publishes and accepts the one staging branch of the
// factory's own repository, on one machine.
//
// Staging is never patched. Every rebuild starts from the default commit,
// read from the mirror ref (repo.MirrorRef), and replays each passed change of
// the stored list in order, one commit per change, with repo.Replay. After
// each step it checks the step on its own diff: the protected paths of the
// task, the goal's acceptance directory against its reviewed source, the task
// checks, and the acceptance criteria against the step before. A change that
// fails any of these, no longer applies, or is already in the default branch
// is taken out, and the removal is recorded with a reason, the criteria and
// the state that made it. A change that was taken out never comes back by
// itself: its id and commit cannot be added again.
//
// The package does not fetch the default branch, run builds, run the gate,
// compute the trajectory, or claim staging across machines. It records the
// facts the trajectory needs (Removal and State) and nothing more.
//
// # Order of events
//
// A state is recorded before it is pushed, and marked published after. Only a
// published state can be accepted. An Accept is recorded as pending before
// the merge. Resolve reconciles the records with the host after a crash.
// While an Accept is pending, staging is frozen: Add (which keeps the change
// as waiting), Rebuild and Undo change nothing and return ErrFrozen. Without
// an accept token the Accept opens or updates the staging pull request and
// stays frozen until it is merged or closed, or a person cancels it.
//
// # Who writes staging
//
// Only the publish step of this package calls PushStaging or sets the staging
// ref. A test parses the module to keep it so.
//
// # Limits
//
//   - One machine. Two processes on one store are kept apart only at the
//     pending Accept (the store's compare and swap). The rebuild lock is in
//     memory.
//   - The check that a person may accept (the login token) belongs to the
//     layer above. These methods trust their caller.
//   - A check script runs as the same user and can write anywhere that user
//     can. The repository guard and the compare and swap on the staging ref
//     catch a change to the repository between steps. They cannot stop one
//     that is undone before the next call.
//   - The host's staging head is read only through the staging pull request.
//   - One acceptance change for each goal is expected. A second one is
//     compared with the same source and is likely to be taken out.
//   - After an Accept the caller must fetch the default branch into the mirror
//     ref before the next rebuild. Until then Rebuild returns ErrMirrorBehind.
//   - Undo after Accept is not built (ErrNotBuilt).
//   - A change is taken out as bad only for the two definite answers of
//     package repo (repo.ErrReplayParent and repo.ErrReplayGitlink). Any other
//     error is returned and nothing is recorded. Add tries the change once on
//     the default commit first, so a commit that is missing or that Replay
//     refuses without a typed error is refused at Add and never stored.
//   - Each Add costs one rebuild of the whole list: every change is replayed
//     and checked again. Config.MaxEntries (default 50) limits the list.
//     Batching is a later change.
//   - When a step cannot be exported for the task checks (too many entries,
//     too large, a name collision), package repo and package verify give no
//     typed error, so it is a machine fault: the rebuild fails and records
//     nothing. Undo of a change is the way out.
//   - A merge of the staging pull request that the factory did not start is
//     noticed by Resolve, which always reads the remembered pull request. At
//     the commit of a published state it accepts that state. Anywhere else it
//     is an exception: Add, Rebuild, Undo and Accept return ErrMergedUnseen
//     until a person calls ClearException.
//   - A process of the same user can still move the mirror ref to a descendant
//     of the default commit during a rebuild, and the rebuild will follow it.
package staging
