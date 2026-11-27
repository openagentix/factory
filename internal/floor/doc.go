// Package floor is the read model behind the Floor screen (record 0004).
//
// Project is a pure function. It takes Inputs, a plain value that someone else
// already read, and returns a Floor. The package reads no file, no clock and no
// network, starts no goroutine and keeps no state. A test keeps it that way.
//
// Three rules shape every type here.
//
//   - Unknown is not zero. An input that may be missing is an Opt, and a source
//     that is missing, failed or not built is shown as such, never as a zero, a
//     default or a green lamp.
//   - Text from outside is never a sentence. Titles and questions are Untrusted
//     values. Sentences are built by the web app from Phrases, filled with Fact
//     values only.
//   - Output is deterministic. Input slices are put in one canonical order first,
//     and every map that reaches the output has string keys, which encoding/json
//     sorts.
//
// Where record 0004 is silent or contradicts itself, the package takes the
// fail-closed reading: a piece is shown as "could not read" or as waiting for a
// person, never as moving or ready. Each such place carries a comment that
// starts with "RULING".
//
// # What the caller must supply
//
// The input contract, in plain words:
//
//   - Every gate fact carries the run id and attempt it judged. A fact for
//     another run, or with no run, does not count.
//   - Attempt numbers of one ticket never repeat across its build tasks.
//     implement and fix-ci share one counter. A second-opinion run names the
//     build run and attempt it judged.
//   - Every applied entry and every accepted entry carries its time and its
//     ticket. An accepted change with no ticket or no time cannot be counted.
//   - Times are in one clock per source, as record 0004 says: claims and the
//     host's time are the host's clock, runs and everything else the server's.
//   - A source that is ok must have all its fields known. A field that is
//     unknown is read_failed for the whole source.
//
// A caller that breaks this gets invalid_facts on the affected pieces. That is
// by design: the Floor will not guess.
//
// When the gate is not built, a run that succeeded waits at station 05 with
// the reason not_built. It is waiting for a machine, never working, and it
// never counts as moving or turns the lamp green or red.
package floor
