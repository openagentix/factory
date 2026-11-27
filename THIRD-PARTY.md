# Third-party dependencies

Every dependency that is built into `oxf`, with its licence. The project embeds MIT and Apache-2.0 code, plus BSD and public domain code for underlying modules of the Go ecosystem and for SQLite, as `AGENTS.md` says. Update this file in the same pull request that adds or removes a dependency.

## Go modules

| Module | Version | Licence | Used for |
|---|---|---|---|
| `gopkg.in/yaml.v3` | v3.0.1 | MIT and Apache-2.0 | Reading task and factory files |
| `github.com/ncruces/go-sqlite3` | v0.32.0 | MIT | Pure Go SQLite driver for the run record store. It embeds a WebAssembly build of SQLite (see below) |
| `github.com/ncruces/julianday` | v1.0.0 | MIT | Time conversion, used by the SQLite driver |
| `github.com/tetratelabs/wazero` | v1.11.0 | Apache-2.0 | WebAssembly runtime with no C code, used by the SQLite driver |
| `golang.org/x/sys` | v0.41.0 | BSD-3-Clause | System calls, used by the SQLite driver and wazero |

### Inside the embedded SQLite build

The file `embed/sqlite3.wasm` in `go-sqlite3` is built from C code with `-nostdlib`, so it holds no wasi-libc. It holds these components. Only the Go modules above are linked into `oxf`, and the build of the wasm file is not run by this project.

| Component | Version | Licence | Note |
|---|---|---|---|
| SQLite, with the `ext/misc` extensions the build includes (`anycollseq`, `base64`, `decimal`, `ieee754`, `regexp`, `series`, `spellfix`, `uint`) | 3.51.x | Public domain | Accepted by the licence rule in `AGENTS.md` |
| The module's own C glue and minimal libc (`sqlite3/*.c`, `sqlite3/libc/`) | as the module | MIT | Maths is done on the Go side |
| Doug Lea's `malloc` (`sqlite3/libc/malloc.c`) | 2.8.6 | MIT-0 | The allocator. It is relicensed from CC0 by its author in 2023 |

## Web app (planned, not built)

Record 0004 plans these for the web app. Nothing is installed yet. Versions are to be confirmed at install time against the lockfile. Their licence notices will ship with the binary.

| Part | Licence | Note |
|---|---|---|
| The web framework's runtime (`svelte`) | MIT | Shipped in the binary |
| The router's runtime (`@sveltejs/kit`) | MIT | Shipped in the binary. These two are the only web code shipped; the build tools are not |
| Barlow Condensed font files | SIL Open Font Licence 1.1 | Font data, the one exception to the licence rule |
| IBM Plex Sans and IBM Plex Mono font files | SIL Open Font Licence 1.1, reserved name "Plex" | The publisher's own unmodified files |

## Programs that are started, not shipped

OpenAgentix Software Factory starts these programs when they are installed on the user's machine. They are not part of this repository or of any build from it. Each has its own licence and terms.

| Program | Licence |
|---|---|
| Claude Code CLI | Proprietary, Anthropic |
| Codex CLI | Apache-2.0, OpenAI |
| git | GPL-2.0, run as a separate program |
