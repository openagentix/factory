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

## Web app

The web app is in `web/`. It is installed with pnpm from the lockfile, with install scripts off, and built into plain files. Nothing the app needs at run time is fetched: the fonts are bundled. Versions are the exact ones in `web/package.json`. Licences were read from the installed packages with `pnpm licenses list --json`.

### Direct dependencies

All are `devDependencies`. The built app contains the compiled output of `svelte` and `@sveltejs/kit` (their small runtimes) and the font files. No other package ships.

| Package | Version | Licence | Use |
|---|---|---|---|
| `svelte` | 5.59.1 | MIT | Build time. Its runtime is compiled into the shipped JavaScript |
| `@sveltejs/kit` | 3.4.0 | MIT | Build time. Its client runtime is compiled into the shipped JavaScript |
| `@sveltejs/adapter-static` | 4.0.1 | MIT | Build time only |
| `@sveltejs/vite-plugin-svelte` | 7.3.2 | MIT | Build time only |
| `vite` | 8.3.8 | MIT | Build time only |
| `typescript` | 6.0.3 | Apache-2.0 | Build time only |
| `svelte-check` | 4.7.8 | MIT | Build time only |
| `vitest` | 5.1.1 | MIT | Tests only |
| `jsdom` | 31.0.1 | MIT | Tests only |
| `@testing-library/svelte` | 5.4.2 | MIT | Tests only |
| `@testing-library/user-event` | 14.6.13 | MIT | Tests only |
| `prettier` | 3.10.1 | MIT | Formatting only |
| `prettier-plugin-svelte` | 3.5.2 | MIT | Formatting only |
| `@ibm/plex-sans` | 2.0.0 | OFL-1.1 | Build time. The build copies three font files and the licence text |
| `@ibm/plex-mono` | 2.5.0 | OFL-1.1 | Build time. The build copies two font files and the licence text |
| `@fontsource/barlow-condensed` | 5.3.0 | OFL-1.1 | Build time. The build copies three font files and the licence text |

### Fonts that ship

Font data is the one exception to the licence rule (record 0004). The licence texts ship in the built app under `fonts/`.

| Font | Files | Licence | Note |
|---|---|---|---|
| Barlow Condensed | 500, 600, 700, Latin | SIL Open Font Licence 1.1 | No reserved name |
| IBM Plex Sans | 400, 500, 600 | SIL Open Font Licence 1.1, reserved name "Plex" | The publisher's own unmodified files |
| IBM Plex Mono | 400, 500 | SIL Open Font Licence 1.1, reserved name "Plex" | The publisher's own unmodified files |

### All installed web packages, by licence

118 packages in the lockfile, direct and indirect, counted once per package name.

| Licence | Packages |
|---|---|
| MIT | 99 |
| Apache-2.0 | 7 (`typescript`, `@ibm/telemetry-js`, `aria-query`, `axobject-query`, `detect-libc`, `expect-type`, `xml-name-validator`) |
| OFL-1.1 | 3 (the three font packages above) |
| BSD-2-Clause | 2 (`entities`, `webidl-conversions`) |
| BSD-3-Clause | 2 (`source-map-js`, `tough-cookie`) |
| ISC | 2 (`picocolors`, `saxes`) |
| BlueOak-1.0.0 | 1 (`lru-cache`) |
| MPL-2.0 | 2 (`lightningcss`, `lightningcss-darwin-arm64`). Build time only: the CSS minifier of the build tool. Its code is not in the built app. This needs a decision (record 0004, "Not settled") |

`@ibm/telemetry-js` is a dependency of the two IBM font packages. It is installed but never run: install scripts are off, and nothing imports it.

## Programs that are started, not shipped

OpenAgentix Software Factory starts these programs when they are installed on the user's machine. They are not part of this repository or of any build from it. Each has its own licence and terms.

| Program | Licence |
|---|---|
| Claude Code CLI | Proprietary, Anthropic |
| Codex CLI | Apache-2.0, OpenAI |
| git | GPL-2.0, run as a separate program |
