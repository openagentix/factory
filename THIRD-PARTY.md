# Third-party dependencies

Every dependency that is built into `oxf`, with its licence. The project embeds only MIT and Apache-2.0 code. Update this file in the same pull request that adds or removes a dependency.

## Go modules

| Module | Version | Licence | Used for |
|---|---|---|---|
| `gopkg.in/yaml.v3` | v3.0.1 | MIT and Apache-2.0 | Reading task and factory files |
| `github.com/ncruces/go-sqlite3` | v0.32.0 | MIT | Pure Go SQLite driver for the run record store. It embeds SQLite as a WebAssembly build. SQLite is in the public domain. The build adds the wasi-libc library, which has permissive licences (see its notice in the module) |
| `github.com/ncruces/julianday` | v1.0.0 | MIT | Time conversion, used by the SQLite driver |
| `github.com/tetratelabs/wazero` | v1.11.0 | Apache-2.0 | WebAssembly runtime with no C code, used by the SQLite driver |
| `golang.org/x/sys` | v0.41.0 | BSD-3-Clause | System calls, used by the SQLite driver and wazero |

## Programs that are started, not shipped

OpenAgentix Software Factory starts these programs when they are installed on the user's machine. They are not part of this repository or of any build from it. Each has its own licence and terms.

| Program | Licence |
|---|---|
| Claude Code CLI | Proprietary, Anthropic |
| Codex CLI | Apache-2.0, OpenAI |
| git | GPL-2.0, run as a separate program |
