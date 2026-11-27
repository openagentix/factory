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
package floor
